package gtm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// runBrand is the brand & assets vertical. Same rail: feeds → grounded context
// → an inferred naming/positioning concept → a logo brief (rendered live via
// Recraft when RECRAFT_API_KEY is set, otherwise the brief itself) → landing
// copy (LLM prose constrained to grounded claims) → a brand panel. Creative
// output is labeled inferred/speculative; the category facts it leans on stay
// grounded.
func (e *Engine) runBrand(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}
	tiers := tierSet(opts.Tiers)
	if opts.NoFeeds {
		tiers = map[FeedTier]bool{}
	}

	ev, warnings := e.reg.Gather(ctx, FeedQuery{
		Subject: opts.Subject, Keywords: opts.Keywords, Limit: opts.Limit, Category: opts.Category,
	}, tiers)
	if w := wikidataDisambiguationWarning(ev, opts.Subject, opts.Category); w != "" {
		warnings = append(warnings, w)
	}

	realEv := nonSynthetic(ev)
	facts := filterMetric(realEv, "company_fact") // structured firmographics only
	haveReal := len(realEv) > 0
	conf := ConfInferred
	if !haveReal {
		conf = ConfSpeculative
	}
	category := factValue(facts, "industry")
	kind := ResolveBrandKind(opts.Kind, opts.Subject)

	var sections []Section

	// 1. Brand Context — grounded.
	sections = append(sections, contextOrEmpty("Brand Context", facts,
		"No grounded brand context yet. Identity direction below is speculative until a category/positioning is verified."))

	concept := proposeBrandConcept(opts.Subject, category)
	if kind == BrandKindEntity {
		concept = fmt.Sprintf("Treat %s as a legal/company name, not a product brand: collision and evidence matter; a matching .com is optional and is not a naming vote.", opts.Subject)
		sections = append(sections, Section{
			Title:  "Legal name (not a product brand)",
			Body:   concept,
			Claims: []Claim{{Text: concept, Confidence: conf, Citations: citeIDs(facts, 3)}},
		})
	} else {
		// 2. Naming & Positioning — inferred concept.
		sections = append(sections, Section{
			Title:  "Naming & Positioning (inferred)",
			Body:   concept,
			Claims: []Claim{{Text: concept, Confidence: conf, Citations: citeIDs(facts, 3)}},
		})

		// 3. Logo Direction — Recraft brief (+ live SVG when keyed and not offline).
		sections = append(sections, e.logoSection(ctx, opts, category, conf, &warnings))

		// 4. Landing Copy — LLM prose constrained to grounded claims.
		copyBody, err := e.landingCopy(ctx, opts.Subject, concept, facts)
		if err != nil {
			warnings = append(warnings, "landing copy generation failed: "+err.Error())
			copyBody = concept
		}
		sections = append(sections, Section{
			Title:  "Landing Copy (inferred)",
			Body:   copyBody,
			Claims: []Claim{{Text: "Landing value proposition for " + opts.Subject, Confidence: conf, Citations: citeIDs(facts, 3)}},
		})
	}

	// 5. Availability & IP — domain is advisory only (not a panel vote).
	//    Network-bound, so gated on the same NoFeeds switch as the feeds above.
	var collision string
	if !opts.NoFeeds {
		results := screenBrandDomains(ctx, opts.Subject, defaultBrandLookups())
		collision = brandCollisionFromEvidence(ev, opts.Subject)
		sections = append(sections, brandScreenSection(opts.Subject, results, collision))
		warnings = append(warnings, brandScreenWarnings(opts.Subject, results, collision, kind)...)
	}

	panel := RunBrandPanel(opts.Subject, concept, ev)
	if kind == BrandKindEntity {
		panel = RunEntityNamePanel(opts.Subject, ev, collision)
	}

	report := &Report{
		Vertical:  "brand",
		Subject:   opts.Subject,
		Query:     query,
		Generated: e.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Provider:  e.gen.Provider(),
		Model:     e.gen.Model(),
		Tiers:     tierList(tiers),
		Evidence:  ev,
		Sections:  sections,
		Panel:     panel,
		Warnings:  warnings,
	}
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func proposeBrandConcept(subject, category string) string {
	if category != "" {
		return fmt.Sprintf("Position %s as the clear, confident choice in %s: a name that signals precision and trust, with an identity that reads modern and technical rather than playful.", subject, category)
	}
	return fmt.Sprintf("Give %s a precise, ownable identity anchored to a single sharp promise — defer broad-category styling until the positioning is verified.", subject)
}

// logoSection builds the Recraft logo prompt and, if RECRAFT_API_KEY is set,
// generates a live vector mark and links it. Without a key it returns the brief.
func (e *Engine) logoSection(ctx context.Context, opts Options, category string, conf Confidence, warnings *[]string) Section {
	subject := opts.Subject
	cat := category
	if cat == "" {
		cat = "technology"
	}
	prompt := fmt.Sprintf("A modern, minimal vector logo mark for %q, a %s product. Clean geometric symbol, scalable, high contrast, professional, flat, transparent background. No text.", subject, cat)
	var b strings.Builder
	b.WriteString("**Recraft brief (vector / SVG):**\n\n```\n" + prompt + "\n```\n")
	claims := []Claim{{Text: "Logo direction: minimal geometric vector mark for " + subject, Confidence: conf}}

	if !opts.Offline && !opts.NoFeeds && os.Getenv("RECRAFT_API_KEY") != "" {
		if assetURL, err := generateLogo(ctx, prompt); err != nil {
			*warnings = append(*warnings, "recraft logo generation: "+err.Error())
			b.WriteString("\n_(live generation failed; brief above is usable as-is)_\n")
		} else if assetURL != "" {
			b.WriteString("\n**Generated mark:** " + assetURL + "\n")
		}
	} else if opts.Offline || opts.NoFeeds {
		b.WriteString("\n_Offline/hermetic run: logo brief only (Recraft not called)._\n")
	} else {
		b.WriteString("\n_Set RECRAFT_API_KEY to generate the SVG live; the brief above feeds any image model._\n")
	}
	return Section{Title: "Logo Direction (inferred)", Body: b.String(), Claims: claims}
}

// generateLogo calls Recraft to produce a vector mark, returning the asset URL.
func generateLogo(ctx context.Context, prompt string) (string, error) {
	key := os.Getenv("RECRAFT_API_KEY")
	if key == "" {
		return "", fmt.Errorf("RECRAFT_API_KEY not set")
	}
	body, _ := json.Marshal(map[string]any{
		"prompt": prompt,
		"style":  "vector_illustration",
		"size":   "1024x1024",
	})
	var resp struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	headers := map[string]string{"Authorization": "Bearer " + key, "Content-Type": "application/json"}
	if err := doJSON(ctx, http.MethodPost, "https://external.api.recraft.ai/v1/images/generations", headers, body, &resp); err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("recraft returned no image")
	}
	return resp.Data[0].URL, nil
}

func (e *Engine) landingCopy(ctx context.Context, subject, concept string, facts []Evidence) (string, error) {
	if e.gen.Provider() == "offline" || len(facts) == 0 {
		cat := factValue(facts, "industry")
		if cat == "" {
			cat = "your space"
		}
		return fmt.Sprintf(`### %s

**Headline:** %s — done right.
**Subhead:** The focused way to win in %s.

- Built for the job, not the demo.
- Clear, fast, and yours.
- No lock-in, no surprises.

**CTA:** Get started

_(offline framing — set --provider for LLM-authored copy; facts above stay grounded.)_`, subject, subject, cat), nil
	}
	var ebuf strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&ebuf, "[%s] %s — %s\n", f.ID, f.Title, f.Snippet)
	}
	sys := "You are a senior product marketer. Write landing-page copy: a hero headline, a one-line subhead, exactly 3 feature bullets, and a CTA. STRICT RULE: do not claim any capability, metric, or fact not present in the EVIDENCE block; if evidence is thin, keep the copy aspirational but non-specific. Markdown only."
	user := fmt.Sprintf("SUBJECT: %s\n\nPOSITIONING CONCEPT: %s\n\nEVIDENCE:\n%s", subject, concept, ebuf.String())
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user, MaxTokens: 700})
}
