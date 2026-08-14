package gtm

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// runIdeate is the front of the factory funnel: mine community evidence for
// pain in a space, cross it with buildable product archetypes, and emit ranked
// idea cards ready to feed the scaffold → launch-cohort pipeline. The same
// honesty contract applies upstream of any code existing: demand claims are
// grounded in mention evidence; the ideas themselves are inferred (shaped by
// that evidence) or speculative (keyword-derived), never presented as
// validated. The panel stress-tests the top idea so a weak slate is rejected,
// not rubber-stamped.
func (e *Engine) runIdeate(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject (the space/theme to ideate in) is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}
	count := opts.IdeaCount
	if count <= 0 {
		count = 5
	}
	tiers := tierSet(opts.Tiers)
	if opts.NoFeeds {
		tiers = map[FeedTier]bool{}
	}

	ev, warnings := e.reg.Gather(ctx, FeedQuery{
		Subject: opts.Subject, Keywords: opts.Keywords, Limit: opts.Limit, Category: opts.Category,
	}, tiers)
	realEv := nonSynthetic(ev)
	mentions := filterMetric(realEv, "mentions")

	var sections []Section

	// 1. Demand Signals — grounded: where the community is already in pain.
	if s, ok := demandSignalsSection(mentions); ok {
		sections = append(sections, s)
	}

	// 2. Idea cards — evidence-shaped first, keyword-derived to fill the count.
	ideas := proposeIdeas(opts.Subject, opts.Keywords, mentions, opts.Avoid, count)
	if e.gen.Provider() != "offline" {
		// LLM pass rewrites each card's narrative within the evidence; the
		// deterministic skeleton (name/wedge/citations) stays.
		for i := range ideas {
			if body, err := e.ideaNarrative(ctx, opts.Subject, ideas[i], mentions); err == nil {
				ideas[i].Narrative = body
			} else {
				warnings = append(warnings, fmt.Sprintf("idea %d narrative failed: %v", i+1, err))
			}
		}
	}
	rep := &Report{}
	for i, idea := range ideas {
		sections = append(sections, ideaSection(i+1, idea))
	}
	rep.SetMetric("idea_count", float64(len(ideas)))
	if len(ideas) > 0 {
		rep.SetMetric("top_demand", ideas[0].Demand)
	}

	// 3. Build order — how the slate feeds the factory.
	if len(ideas) > 0 {
		sections = append(sections, buildOrderSection(ideas))
	}

	thesis := opts.Subject + " contains an underserved niche worth a one-week build"
	if len(ideas) > 0 {
		thesis = ideas[0].Pitch
	}
	report := &Report{
		Vertical:  "ideate",
		Subject:   opts.Subject,
		Query:     query,
		Generated: e.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Provider:  e.gen.Provider(),
		Model:     e.gen.Model(),
		Tiers:     tierList(tiers),
		Evidence:  ev,
		Sections:  sections,
		Panel:     RunPanel(opts.Subject, thesis, ev),
		Warnings:  warnings,
		Metrics:   rep.Metrics,
	}
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

// ProductIdea is one ranked idea card.
type ProductIdea struct {
	Title     string   `json:"title"`     // working title (a suggestion, not a brand)
	Pitch     string   `json:"pitch"`     // one-line value proposition
	Archetype string   `json:"archetype"` // buildable shape (cli/menubar/api/agent/marketplace)
	ICP       string   `json:"icp"`       // who it's for
	WhyNow    string   `json:"why_now"`   // the demand rationale
	Demand    float64  `json:"demand"`    // weighted mention signal backing it
	Citations []string `json:"citations"` // evidence ids backing WhyNow
	Narrative string   `json:"narrative,omitempty"`
}

// ideaArchetypes are the product shapes the factory can actually ship in a
// week (each maps onto an existing scaffold lane or binary pattern).
var ideaArchetypes = []struct{ Shape, Suffix, Lane string }{
	{"CLI/agent tool", "CLI", "edge-sidecar (Go binary + CF Worker)"},
	{"menu-bar companion", "Bar", "native desktop (headless-smoke pattern)"},
	{"realtime web app", "Live", "convex-realtime (Slipwright default)"},
	{"metered API", "API", "edge-sidecar + agentcommerce metering"},
	{"agent-operated service", "Agent", "pw-harness ambient + ACP payments"},
}

// proposeIdeas builds the deterministic idea slate: one idea per pain mention
// (strongest first), crossed with a rotating archetype, then keyword-derived
// filler to reach count. Avoid-listed names (existing products) are skipped.
func proposeIdeas(space string, keywords []string, mentions []Evidence, avoid []string, count int) []ProductIdea {
	avoidSet := map[string]bool{}
	for _, a := range avoid {
		avoidSet[strings.ToLower(strings.TrimSpace(a))] = true
	}
	sorted := append([]Evidence{}, mentions...)
	sort.Slice(sorted, func(i, j int) bool { return parseFloatSafe(sorted[i].Value) > parseFloatSafe(sorted[j].Value) })

	var ideas []ProductIdea
	used := map[string]bool{}
	add := func(idea ProductIdea) {
		key := strings.ToLower(idea.Title)
		if used[key] || avoidSet[key] || len(ideas) >= count {
			return
		}
		used[key] = true
		ideas = append(ideas, idea)
	}

	for i, m := range sorted {
		arch := ideaArchetypes[i%len(ideaArchetypes)]
		pain := painPhrase(m.Title)
		add(ProductIdea{
			Title:     workingTitle(pain, arch.Suffix),
			Pitch:     fmt.Sprintf("a %s that turns %q into a solved default", arch.Shape, pain),
			Archetype: arch.Shape + " · lane: " + arch.Lane,
			ICP:       "[FILL: the person living this pain — name them precisely]",
			WhyNow:    fmt.Sprintf("measured community pain: %s (%s)", m.Title, m.Snippet),
			Demand:    parseFloatSafe(m.Value),
			Citations: []string{m.ID},
		})
	}
	// Keyword filler when evidence is thin — explicitly speculative.
	for i, kw := range keywords {
		arch := ideaArchetypes[(len(sorted)+i)%len(ideaArchetypes)]
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		add(ProductIdea{
			Title:     workingTitle(kw, arch.Suffix),
			Pitch:     fmt.Sprintf("a %s for %s in %s", arch.Shape, kw, space),
			Archetype: arch.Shape + " · lane: " + arch.Lane,
			ICP:       "[FILL: the person searching for " + kw + "]",
			WhyNow:    "keyword-derived (no demand evidence yet) — validate with `ngtm seo` before building",
		})
	}
	return ideas
}

// painPhrase trims community-post framing ("Ask HN:", "Show HN:") down to the
// pain itself.
func painPhrase(title string) string {
	for _, prefix := range []string{"Ask HN:", "Show HN:", "Tell HN:"} {
		title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
	}
	return title
}

// workingTitle derives a scannable placeholder name from the pain/keyword.
// It is a handle for discussion, not a brand — brand runs `ngtm brand`.
func workingTitle(phrase, suffix string) string {
	words := strings.Fields(slugWords(phrase))
	if len(words) > 2 {
		words = words[:2]
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	if len(words) == 0 {
		return "Untitled" + suffix
	}
	return strings.Join(words, "") + suffix
}

func slugWords(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func parseFloatSafe(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func demandSignalsSection(mentions []Evidence) (Section, bool) {
	if len(mentions) == 0 {
		return Section{
			Title: "Demand Signals",
			Body:  "No community pain evidence gathered — ideas below are keyword-derived and fully speculative. Run with live feeds (or `--tier cheap`) to mine real demand.",
		}, true
	}
	var claims []Claim
	var b strings.Builder
	b.WriteString("Measured community pain in this space (the raw material for ideas):\n")
	for _, m := range mentions {
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("%s — %s", m.Title, m.Snippet),
			Confidence: ConfGrounded,
			Citations:  []string{m.ID},
		})
		fmt.Fprintf(&b, "- **%s** — %s [%s]\n", m.Title, m.Snippet, m.Feed)
	}
	return Section{Title: "Demand Signals", Body: b.String(), Claims: claims}, true
}

func ideaSection(n int, idea ProductIdea) Section {
	var b strings.Builder
	fmt.Fprintf(&b, "**Pitch:** %s\n\n**Shape:** %s\n\n**ICP:** %s\n\n**Why now:** %s\n", idea.Pitch, idea.Archetype, idea.ICP, idea.WhyNow)
	if idea.Narrative != "" {
		b.WriteString("\n" + idea.Narrative + "\n")
	}
	conf := ConfSpeculative
	if len(idea.Citations) > 0 {
		conf = ConfInferred
	}
	claims := []Claim{{Text: idea.Pitch, Confidence: conf, Citations: idea.Citations}}
	if len(idea.Citations) > 0 {
		claims = append(claims, Claim{
			Text: "demand basis: " + idea.WhyNow, Confidence: ConfGrounded, Citations: idea.Citations,
		})
	}
	return Section{
		Title:  fmt.Sprintf("Idea %d: %s (demand %.0f)", n, idea.Title, idea.Demand),
		Body:   b.String(),
		Claims: claims,
	}
}

func buildOrderSection(ideas []ProductIdea) Section {
	var b strings.Builder
	b.WriteString("Ranked by measured demand; the factory path for the winner:\n\n")
	for i, idea := range ideas {
		marker := "  "
		if i == 0 {
			marker = "→ "
		}
		fmt.Fprintf(&b, "%s%d. %s — demand %.0f\n", marker, i+1, idea.Title, idea.Demand)
	}
	b.WriteString("\nNext commands for the top idea:\n")
	b.WriteString("1. `ngtm seo \"<idea keyword>\"` — validate search demand before any code\n")
	b.WriteString("2. `ngtm economics <idea> --acv … --cac …` — viability gate\n")
	b.WriteString("3. `nship new <slug>` / `ndev stack scaffold` — build it (one-week slice)\n")
	b.WriteString("4. `ngtm launch plan <slug>` — into next week's cohort; verdict decides its fate\n")
	return Section{Title: "Build Order", Body: b.String()}
}

// ideaNarrative asks the generator to deepen one card without inventing facts.
func (e *Engine) ideaNarrative(ctx context.Context, space string, idea ProductIdea, mentions []Evidence) (string, error) {
	var ebuf strings.Builder
	for _, m := range mentions {
		fmt.Fprintf(&ebuf, "[%s] %s — %s\n", m.ID, m.Title, m.Snippet)
	}
	sys := "You are a product strategist for a software factory that ships one-week product slices. Write 2-3 sentences sharpening this idea: the wedge, the first slice to ship, the riskiest assumption. STRICT: no invented users, numbers, or competitors — only what the EVIDENCE block supports; cite [id]s; mark unknowns as open questions."
	user := fmt.Sprintf("SPACE: %s\nIDEA: %s — %s (%s)\n\nEVIDENCE:\n%s", space, idea.Title, idea.Pitch, idea.Archetype, ebuf.String())
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user})
}
