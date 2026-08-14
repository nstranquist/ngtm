package gtm

import (
	"context"
	"fmt"
	"strings"
)

// runBusiness is the business-plan + SWOT vertical. Same rail as SEO: company
// /market feeds → grounded facts → a SWOT (S/W grounded in evidence, O/T
// inferred from it) → TAM/SAM/SOM (sized only as far as evidence allows, never
// fabricated) → an investor "shark-tank" panel. Strengths/Weaknesses cite real
// facts; Opportunities/Threats are inferred conclusions that still cite the
// evidence they rest on.
func (e *Engine) runBusiness(ctx context.Context, opts Options) (*Report, error) {
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
	// Strengths/context lean on STRUCTURED firmographics (company_fact), not the
	// fuzzy entity search — that keeps disambiguation noise out of the SWOT.
	facts := filterMetric(realEv, "company_fact")
	mentions := filterMetric(realEv, "mentions")
	haveReal := len(realEv) > 0
	otConf := ConfInferred
	if !haveReal {
		otConf = ConfSpeculative
	}

	var sections []Section

	// 1. Company & Market Context — grounded.
	sections = append(sections, contextOrEmpty("Company & Market Context", facts,
		"No firmographic or entity evidence gathered. Add free Wikidata/HN/Reddit signal or cheap Crunchbase/PDL keys for grounded company facts."))

	// 2. SWOT.
	sections = append(sections, strengthsSection(facts))
	sections = append(sections, weaknessesSection(mentions, distinctHosts(ev)))
	sections = append(sections, otSection("Opportunities", opportunityClaims(opts.Subject, facts, mentions, otConf)))
	sections = append(sections, otSection("Threats", threatClaims(opts.Subject, mentions, ev, otConf)))

	// 3. TAM / SAM / SOM — grounded dollar ranges when a premium market-sizing
	//    feed ran; otherwise sized only as far as free/cheap evidence allows.
	sections = append(sections, tamSamSomSection(opts.Subject, facts, mentions, filterMetric(realEv, "market_size")))

	// 3b. Jobs-to-Be-Done + Value Proposition Canvas + quantified value prop —
	//     the customer-side framing the canon (JTBD, Osterwalder VPC) requires.
	sections = append(sections, jtbdVPCSection(opts.Subject, factValue(facts, "industry"), realEv, otConf))

	// 4. Business Plan narrative — LLM prose (or offline framing), facts fixed.
	thesis := proposeBusinessThesis(opts.Subject, facts, mentions)
	planBody, err := e.businessNarrative(ctx, opts.Subject, thesis, facts, mentions)
	if err != nil {
		warnings = append(warnings, "narrative generation failed: "+err.Error())
		planBody = thesis
	}
	sections = append(sections, Section{
		Title:  "Business Plan (inferred)",
		Body:   planBody,
		Claims: []Claim{{Text: thesis, Confidence: otConf, Citations: citeIDs(realEv, 4)}},
	})

	report := &Report{
		Vertical:  "business",
		Subject:   opts.Subject,
		Query:     query,
		Generated: e.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Provider:  e.gen.Provider(),
		Model:     e.gen.Model(),
		Tiers:     tierList(tiers),
		Evidence:  ev,
		Sections:  sections,
		Panel:     RunBusinessPanel(opts.Subject, thesis, ev),
		Warnings:  warnings,
	}
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func contextOrEmpty(title string, ev []Evidence, emptyNote string) Section {
	if len(ev) == 0 {
		return Section{Title: title, Body: emptyNote}
	}
	var claims []Claim
	var b strings.Builder
	for _, e := range ev {
		desc := e.Snippet
		if desc == "" {
			desc = e.Title
		}
		claims = append(claims, Claim{Text: fmt.Sprintf("%s — %s", e.Title, desc), Confidence: ConfGrounded, Citations: []string{e.ID}})
		fmt.Fprintf(&b, "- **%s** — %s\n", e.Title, desc)
	}
	return Section{Title: title, Body: b.String(), Claims: claims}
}

func strengthsSection(facts []Evidence) Section {
	if len(facts) == 0 {
		return Section{Title: "Strengths", Body: "No grounded strengths — no firmographic evidence available yet."}
	}
	var claims []Claim
	var b strings.Builder
	for _, e := range facts {
		t := e.Title
		claims = append(claims, Claim{Text: t, Confidence: ConfGrounded, Citations: []string{e.ID}})
		b.WriteString("- " + t + "\n")
	}
	return Section{Title: "Strengths", Body: b.String(), Claims: claims}
}

func weaknessesSection(mentions []Evidence, competitors int) Section {
	var claims []Claim
	var b strings.Builder
	switch {
	case len(mentions) == 0:
		b.WriteString("- No public mentions found (Hacker News / Reddit) — traction is unproven (no evidence to cite).\n")
	case len(mentions) < 4:
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("Limited public traction: only %d Hacker News / Reddit mentions surfaced", len(mentions)),
			Confidence: ConfGrounded, Citations: ids(mentions),
		})
		fmt.Fprintf(&b, "- Limited public traction: only %d mentions.\n", len(mentions))
	}
	if competitors >= 4 {
		fmt.Fprintf(&b, "- Crowded field: %d comparable sources surfaced — differentiation pressure.\n", competitors)
	}
	if b.Len() == 0 {
		b.WriteString("- No grounded weaknesses surfaced from the available evidence.\n")
	}
	return Section{Title: "Weaknesses", Body: b.String(), Claims: claims}
}

func otSection(title string, claims []Claim) Section {
	var b strings.Builder
	for _, c := range claims {
		b.WriteString("- " + c.Text + "\n")
	}
	if b.Len() == 0 {
		b.WriteString("- (none derived)\n")
	}
	return Section{Title: title, Body: b.String(), Claims: claims}
}

func opportunityClaims(subject string, facts, mentions []Evidence, conf Confidence) []Claim {
	cites := citeIDs(append(append([]Evidence{}, facts...), mentions...), 4)
	out := []Claim{
		{Text: fmt.Sprintf("Convert existing public interest in %s into a design-partner / early-adopter motion", subject), Confidence: conf, Citations: pickIDs(mentions, 3)},
	}
	if len(facts) > 0 {
		out = append(out, Claim{Text: fmt.Sprintf("Expand from %s's current category into adjacent segments the firmographics suggest", subject), Confidence: conf, Citations: pickIDs(facts, 3)})
	} else {
		out = append(out, Claim{Text: "Define a beachhead segment — no firmographic lock-in yet, so positioning is wide open", Confidence: conf, Citations: cites})
	}
	return out
}

func threatClaims(subject string, mentions, ev []Evidence, conf Confidence) []Claim {
	out := []Claim{
		{Text: fmt.Sprintf("Incumbents visible in the space could compress %s's differentiation before it lands", subject), Confidence: conf, Citations: citeIDs(ev, 3)},
	}
	if len(mentions) < 4 {
		out = append(out, Claim{Text: "Thin public traction signals adoption risk — interest may not exist at scale yet", Confidence: conf, Citations: pickIDs(mentions, 3)})
	}
	return out
}

func tamSamSomSection(subject string, facts, mentions, marketSize []Evidence) Section {
	var b strings.Builder
	var claims []Claim

	// Grounded dollar ranges — only when a premium market-sizing feed supplied
	// them. These are real facts (cited to the provider), never invented.
	if len(marketSize) > 0 {
		b.WriteString("**Grounded market sizing** (premium feed):\n")
		for _, e := range marketSize {
			scope := strings.ToUpper(e.Extra["scope"])
			claims = append(claims, Claim{Text: fmt.Sprintf("%s = %s", scope, e.Value), Confidence: ConfGrounded, Citations: []string{e.ID}})
			src := e.URL
			if src == "" {
				src = e.Feed
			}
			fmt.Fprintf(&b, "- **%s**: %s — source: %s\n", scope, e.Value, src)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("Sized only as far as the evidence allows — **no dollar figure is asserted without a market-sizing feed** (premium tier: set `MARKETSIZING_API_URL` + `MARKETSIZING_API_KEY`, run with `--tier premium`).\n\n")
	}

	industry := factValue(facts, "industry")
	if industry != "" {
		claims = append(claims, Claim{Text: fmt.Sprintf("Market category (TAM frame): %s", industry), Confidence: ConfGrounded, Citations: factCite(facts, "industry")})
		fmt.Fprintf(&b, "- **TAM frame**: category = %s (grounded).\n", industry)
	} else {
		b.WriteString("- **TAM**: category unverified — supply firmographics (Wikidata/Crunchbase/PDL) to anchor it.\n")
	}
	if len(mentions) > 0 {
		claims = append(claims, Claim{Text: fmt.Sprintf("Demand proxy (SOM signal): %d public mentions", len(mentions)), Confidence: ConfGrounded, Citations: ids(mentions)})
		fmt.Fprintf(&b, "- **SOM signal**: %d HN/Reddit mentions as a weak near-term demand proxy (grounded).\n", len(mentions))
	} else {
		b.WriteString("- **SAM/SOM**: no demand signal yet — mentions feed returned nothing.\n")
	}
	if len(marketSize) == 0 {
		b.WriteString("\nTo convert these frames into numbers, enable a market-sizing feed (`--tier premium`) or supply bottoms-up unit assumptions; the panel below will not let an unsized plan pass.")
	}
	return Section{Title: "TAM / SAM / SOM", Body: b.String(), Claims: claims}
}

// jtbdVPCSection adds the customer-side framing the canon requires but feeds
// can't supply: Jobs-to-Be-Done (functional/emotional/social), the Osterwalder
// Value Proposition Canvas scaffold, and a pointer to quantify the value prop.
// It is a structured scaffold (inferred), and cross-links to the economics /
// pricing verticals where the value becomes a number.
func jtbdVPCSection(subject, industry string, realEv []Evidence, conf Confidence) Section {
	cat := industry
	if cat == "" {
		cat = "its category"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Frame demand by the **job** the customer hires %s to do (Christensen), then map it on the **Value Proposition Canvas** (Osterwalder). Fill these from customer interviews — do not guess:\n\n", subject)
	b.WriteString("**Jobs-to-Be-Done** (define the circumstance of struggle):\n")
	b.WriteString("- _Functional job_: the practical task the customer is trying to get done.\n")
	b.WriteString("- _Emotional job_: how they want to feel (in control, safe, unembarrassed).\n")
	b.WriteString("- _Social job_: how they want to be perceived by others.\n")
	fmt.Fprintf(&b, "- _Competes against_: define the rivals by the **job**, not the product class (often a spreadsheet, an incumbent in %s, or \"do nothing\").\n\n", cat)
	b.WriteString("**Value Proposition Canvas**:\n\n")
	b.WriteString(mdTable(
		[]string{"Customer Profile", "↔", "Value Map"},
		[][]string{
			{"Jobs (above)", "↔", "Products & services that address them"},
			{"Pains (obstacles, risks, anxieties)", "↔", "Pain relievers"},
			{"Gains (desired outcomes, surprises)", "↔", "Gain creators"},
		}))
	b.WriteString("\n**Quantified value proposition**: express the gain in the customer's own metric (time saved, $ added, risk avoided). ")
	b.WriteString("Turn it into numbers with `ngtm pricing " + subject + " --next-best-price <$> --diff-value <$>` and test viability with `ngtm economics " + subject + " --acv <$> --cac <$>`.")
	return Section{
		Title: "Jobs-to-Be-Done & Value Proposition Canvas",
		Body:  b.String(),
		Claims: []Claim{{
			Text:       fmt.Sprintf("%s demand should be framed by the customer's job-to-be-done and a quantified value proposition, not by features", subject),
			Confidence: conf,
			Citations:  citeIDs(realEv, 3),
		}},
	}
}

func proposeBusinessThesis(subject string, facts, mentions []Evidence) string {
	industry := factValue(facts, "industry")
	switch {
	case industry != "" && len(mentions) > 0:
		return fmt.Sprintf("%s competes in %s with measurable early public interest; the wedge is to convert that interest into a defensible beachhead before incumbents react.", subject, industry)
	case industry != "":
		return fmt.Sprintf("%s sits in %s; with no public traction yet, the plan must first manufacture demand in a narrow beachhead.", subject, industry)
	default:
		return fmt.Sprintf("%s has no firmographic or traction evidence yet — the plan is a hypothesis until a beachhead and demand are validated.", subject)
	}
}

func (e *Engine) businessNarrative(ctx context.Context, subject, thesis string, facts, mentions []Evidence) (string, error) {
	if e.gen.Provider() == "offline" || len(facts)+len(mentions) == 0 {
		var b strings.Builder
		b.WriteString(thesis)
		if len(facts) > 0 {
			b.WriteString("\n\nGrounded facts: ")
			var ts []string
			for _, f := range facts {
				ts = append(ts, f.Title)
			}
			b.WriteString(strings.Join(ts, "; ") + ".")
		}
		return b.String(), nil
	}
	var ebuf strings.Builder
	for _, e := range append(append([]Evidence{}, facts...), mentions...) {
		fmt.Fprintf(&ebuf, "[%s] %s — %s (%s)\n", e.ID, e.Title, e.Snippet, e.URL)
	}
	sys := "You are a precise startup strategist writing a short business-plan narrative (2-3 paragraphs). STRICT RULE: do not introduce any company, statistic, market size, or claim that is not present in the EVIDENCE block. Cite supporting evidence inline as [id]. If financials or market size are absent, say plainly that they are unverified."
	user := fmt.Sprintf("SUBJECT: %s\n\nTHESIS: %s\n\nEVIDENCE:\n%s", subject, thesis, ebuf.String())
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user, MaxTokens: 1100})
}

// --- evidence helpers (business) ---

func citeIDs(ev []Evidence, n int) []string { return pickIDs(ev, n) }

func pickIDs(ev []Evidence, n int) []string {
	var out []string
	for _, e := range ev {
		out = append(out, e.ID)
		if len(out) >= n {
			break
		}
	}
	return out
}

func factValue(facts []Evidence, contains string) string {
	for _, e := range facts {
		if strings.Contains(strings.ToLower(e.Title), contains) {
			if e.Value != "" {
				return e.Value
			}
			return strings.TrimSpace(strings.TrimPrefix(strings.SplitN(e.Title, ":", 2)[len(strings.SplitN(e.Title, ":", 2))-1], " "))
		}
	}
	return ""
}

func factCite(facts []Evidence, contains string) []string {
	for _, e := range facts {
		if strings.Contains(strings.ToLower(e.Title), contains) {
			return []string{e.ID}
		}
	}
	return nil
}
