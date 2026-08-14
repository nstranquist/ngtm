package gtm

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// runSEO is the SEO & positioning vertical — the first rail proving the
// factory pattern: live SERP/keyword/context feeds → grounded facts → an
// inferred positioning wedge (LLM writes the prose, never the facts) →
// adversarial panel → a validated, cited report.
func (e *Engine) runSEO(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}
	tiers := tierSet(opts.Tiers)
	if opts.NoFeeds {
		tiers = map[FeedTier]bool{} // hermetic: no live feeds, fixtures only
	}

	ev, warnings := e.reg.Gather(ctx, FeedQuery{
		Subject: opts.Subject, Keywords: opts.Keywords, Limit: opts.Limit, Category: opts.Category,
	}, tiers)
	if w := wikidataDisambiguationWarning(ev, opts.Subject, opts.Category); w != "" {
		warnings = append(warnings, w)
	}

	// Grounded sections consume only real (non-synthetic) evidence — synthetic
	// fixtures live in the evidence list and feed the panel, but can never back
	// a grounded claim.
	realEv := nonSynthetic(ev)
	serp := filterMetric(realEv, "serp_rank")
	volume := filterMetric(realEv, "search_volume")
	entityCtx := filterFeed(realEv, "wikidata")

	var sections []Section

	// 1. SERP Reality — grounded: who actually ranks right now.
	sections = append(sections, serpSection(serp))

	// 2. Demand Signal — grounded: is anyone searching?
	sections = append(sections, demandSection(volume))

	// 3. Context — grounded: what the entity graph says this is.
	if s, ok := contextSection(entityCtx); ok {
		sections = append(sections, s)
	}

	// 4. Positioning Wedge — INFERRED. Facts come from SERP evidence; the LLM
	//    (or offline framing) only writes the strategic narrative around them.
	wedge := proposeWedge(opts.Subject, serp)
	wedgeBody, err := e.wedgeNarrative(ctx, opts.Subject, wedge, serp, volume)
	if err != nil {
		warnings = append(warnings, "narrative generation failed: "+err.Error())
		wedgeBody = wedge
	}
	sections = append(sections, Section{
		Title: "Positioning Wedge (inferred)",
		Body:  wedgeBody,
		Claims: []Claim{{
			Text:       wedge,
			Confidence: ConfInferred,
			Citations:  ids(serp),
		}},
	})

	report := &Report{
		Vertical:  "seo",
		Subject:   opts.Subject,
		Query:     query,
		Generated: e.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Provider:  e.gen.Provider(),
		Model:     e.gen.Model(),
		Tiers:     tierList(tiers),
		Evidence:  ev,
		Sections:  sections,
		Panel:     RunPanel(opts.Subject, wedge, ev),
		Warnings:  warnings,
	}
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func serpSection(serp []Evidence) Section {
	if len(serp) == 0 {
		return Section{
			Title: "SERP Reality",
			Body:  "No live SERP evidence was gathered. Run with `--tier cheap` and a SERPER_API_KEY (or BRAVE_API_KEY) to see who currently ranks — without it, every positioning claim is unverified.",
		}
	}
	var claims []Claim
	var b strings.Builder
	b.WriteString("Page-one incumbents currently ranking (measured, not assumed):\n")
	for _, e := range serp {
		host := hostOf(e.URL)
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("Rank #%s: %s (%s)", e.Value, e.Title, host),
			Confidence: ConfGrounded,
			Citations:  []string{e.ID},
		})
		fmt.Fprintf(&b, "- **#%s** %s — %s\n", e.Value, e.Title, host)
	}
	return Section{Title: "SERP Reality", Body: b.String(), Claims: claims}
}

func demandSection(volume []Evidence) Section {
	if len(volume) == 0 {
		return Section{
			Title: "Demand Signal",
			Body:  "No keyword search-volume evidence (free/local tier can't measure volume). Add DataForSEO (`DATAFORSEO_LOGIN`/`DATAFORSEO_PASSWORD`) before committing to a target term.",
		}
	}
	var claims []Claim
	var b strings.Builder
	for _, e := range volume {
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("%s — %s", e.Title, e.Snippet),
			Confidence: ConfGrounded,
			Citations:  []string{e.ID},
		})
		fmt.Fprintf(&b, "- **%s**: %s\n", e.Title, e.Snippet)
	}
	return Section{Title: "Demand Signal", Body: b.String(), Claims: claims}
}

func contextSection(ctxEv []Evidence) (Section, bool) {
	if len(ctxEv) == 0 {
		return Section{}, false
	}
	var claims []Claim
	var b strings.Builder
	for _, e := range ctxEv {
		desc := e.Snippet
		if desc == "" {
			desc = "(no description)"
		}
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("%s: %s", e.Title, desc),
			Confidence: ConfGrounded,
			Citations:  []string{e.ID},
		})
		fmt.Fprintf(&b, "- **%s** — %s\n", e.Title, desc)
	}
	return Section{Title: "Entity Context", Body: b.String(), Claims: claims}, true
}

// proposeWedge derives a baseline positioning angle deterministically from the
// SERP. It is intentionally an INFERENCE: a starting hypothesis the panel then
// stress-tests, not a fact.
func proposeWedge(subject string, serp []Evidence) string {
	hosts := dedupe(hostList(serp))
	if len(hosts) == 0 {
		return fmt.Sprintf("Own a narrow, specific frame for %s rather than the generic category term — no incumbents were measured, so claim a precise sub-niche and validate demand before broadening.", subject)
	}
	sort.Strings(hosts)
	shown := hosts
	if len(shown) > 4 {
		shown = shown[:4]
	}
	return fmt.Sprintf("Differentiate %s from the incumbents already ranking (%s) by owning a sharper, more specific frame than the head term they compete on — target the gap their generic positioning leaves open.", subject, strings.Join(shown, ", "))
}

// wedgeNarrative asks the generator to write the strategic prose around the
// wedge. The system prompt forbids introducing any fact not in the evidence —
// the model writes wording, the feeds supply truth.
func (e *Engine) wedgeNarrative(ctx context.Context, subject, wedge string, serp, volume []Evidence) (string, error) {
	// No real evidence → don't call the LLM (it would refuse to write grounded
	// prose with an empty evidence block); return the deterministic framing.
	if e.gen.Provider() == "offline" || len(serp)+len(volume) == 0 {
		var b strings.Builder
		b.WriteString(wedge)
		if len(serp) > 0 {
			b.WriteString("\n\nGrounded in the current SERP: ")
			b.WriteString(strings.Join(hostList(serp), ", "))
			b.WriteString(".")
		}
		return b.String(), nil
	}
	var ebuf strings.Builder
	for _, e := range append(append([]Evidence{}, serp...), volume...) {
		fmt.Fprintf(&ebuf, "[%s] %s — %s (%s)\n", e.ID, e.Title, e.Snippet, e.URL)
	}
	sys := "You are a precise GTM positioning strategist. Write 1-2 short paragraphs of strategic narrative for the proposed wedge. STRICT RULE: do not introduce any company, statistic, ranking, or claim that is not present in the EVIDENCE block. Cite supporting evidence inline using its [id]. If the evidence is thin, say so plainly."
	user := fmt.Sprintf("SUBJECT: %s\n\nPROPOSED WEDGE: %s\n\nEVIDENCE:\n%s", subject, wedge, ebuf.String())
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user})
}

// --- evidence helpers ---

func filterMetric(ev []Evidence, metric string) []Evidence {
	var out []Evidence
	for _, e := range ev {
		if e.Metric == metric {
			out = append(out, e)
		}
	}
	return out
}

func nonSynthetic(ev []Evidence) []Evidence {
	var out []Evidence
	for _, e := range ev {
		if !e.Synthetic {
			out = append(out, e)
		}
	}
	return out
}

func filterFeed(ev []Evidence, feed string) []Evidence {
	var out []Evidence
	for _, e := range ev {
		if e.Feed == feed {
			out = append(out, e)
		}
	}
	return out
}

func ids(ev []Evidence) []string {
	var out []string
	for _, e := range ev {
		out = append(out, e.ID)
	}
	return out
}

func hostList(ev []Evidence) []string {
	var out []string
	for _, e := range ev {
		if e.URL != "" && !e.Synthetic {
			out = append(out, hostOf(e.URL))
		}
	}
	return out
}
