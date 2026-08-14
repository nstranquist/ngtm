package gtm

import (
	"context"
	"fmt"
	"strings"
)

// runSocial is the distribution-content vertical: it turns a product + its
// gathered evidence into channel-native launch/social drafts, one per selected
// ChannelSpec. The channel's typed norms are the contract — the generator
// (offline template or LLM) must satisfy them and lintDraft verifies it,
// surfacing violations as report warnings. Facts stay provenance-tagged:
// community-mention claims are grounded; the drafts themselves are inferred
// (evidence-shaped) or speculative (no evidence), and unknown specifics are
// rendered as explicit [FILL: …] slots rather than invented.
func (e *Engine) runSocial(ctx context.Context, opts Options) (*Report, error) {
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

	specs, unknown := SelectChannels(opts.Channels)
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown channel(s) %s (known: %s)", strings.Join(unknown, ", "), strings.Join(channelKeys(Channels), ", "))
	}

	realEv := nonSynthetic(ev)
	mentions := filterMetric(realEv, "mentions")
	pitch := socialPitch(opts)

	var sections []Section

	// 1. Community Voice — grounded: what people already say about this space.
	if s, ok := communityVoiceSection(mentions); ok {
		sections = append(sections, s)
	}

	// 2. One channel-native draft per selected channel, linted + scored against
	//    its spec. --tune explores the hook archetypes and keeps the best-scoring
	//    candidate per channel (the deterministic self-review loop).
	archetypes := []string{"outcome"}
	if opts.Tune {
		archetypes = hookArchetypes
	}
	var scores []DraftScore
	rep := &Report{} // metrics accumulator, threaded into the final report below
	for _, spec := range specs {
		var best ChannelDraft
		var bestScore DraftScore
		for i, arch := range archetypes {
			draft, w := e.channelDraft(ctx, spec, opts.Subject, pitch, mentions, arch)
			warnings = append(warnings, w...)
			score := ScoreSocialDraft(spec, draft, mentions)
			if i == 0 || score.Total > bestScore.Total {
				best, bestScore = draft, score
			}
		}
		warnings = append(warnings, lintDraft(spec, best)...)
		scores = append(scores, bestScore)
		rep.SetMetric("score_"+spec.Key, bestScore.Total)
		sections = append(sections, draftSection(spec, best, mentions))
	}

	// 3. Scorecard — the computable self-eval over the pack.
	if len(scores) > 0 {
		sections = append(sections, scorecardSection(scores))
		sum := 0.0
		dimensionSums := map[string]float64{}
		for _, s := range scores {
			sum += s.Total
			for dimension, value := range s.Parts {
				dimensionSums[dimension] += value
			}
		}
		rep.SetMetric("social_score", sum/float64(len(scores)))
		rep.SetMetric("social_channel_count", float64(len(scores)))
		for _, dimension := range []string{"contract", "grounding", "specificity", "completeness", "cta"} {
			rep.SetMetric("social_"+dimension, dimensionSums[dimension]/float64(len(scores)))
		}
	}

	// 4. Distribution calendar — when to place each channel.
	sections = append(sections, calendarSection(specs))

	thesis := fmt.Sprintf("%s can earn attention on builder channels with the angle: %s", opts.Subject, pitch)
	report := &Report{
		Vertical:  "social",
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

// socialPitch resolves the one-line value proposition the drafts are built
// around: explicit --pitch wins, then a query distinct from the subject, then
// an explicit fill slot (never an invented benefit).
func socialPitch(opts Options) string {
	if p := strings.TrimSpace(opts.Pitch); p != "" {
		return p
	}
	if q := strings.TrimSpace(opts.Query); q != "" && !strings.EqualFold(q, opts.Subject) {
		return q
	}
	return "[FILL: one-line outcome this product delivers]"
}

// communityVoiceSection turns mention evidence (HN/Reddit) into grounded
// claims — the raw material the drafts can echo without inventing sentiment.
func communityVoiceSection(mentions []Evidence) (Section, bool) {
	if len(mentions) == 0 {
		return Section{}, false
	}
	var claims []Claim
	var b strings.Builder
	b.WriteString("What the community already says (measured, citable in drafts):\n")
	for _, m := range mentions {
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("%s — %s", m.Title, m.Snippet),
			Confidence: ConfGrounded,
			Citations:  []string{m.ID},
		})
		fmt.Fprintf(&b, "- **%s** — %s [%s]\n", m.Title, m.Snippet, m.Feed)
	}
	return Section{Title: "Community Voice", Body: b.String(), Claims: claims}, true
}

// channelDraft produces one channel's draft for a given hook archetype.
// Offline (or when the LLM fails) it falls back to the deterministic template;
// with a live provider it asks the generator to write within the channel spec,
// the evidence block, and the archetype framing.
func (e *Engine) channelDraft(ctx context.Context, spec ChannelSpec, subject, pitch string, mentions []Evidence, archetype string) (ChannelDraft, []string) {
	tmpl := templateDraft(spec, subject, applyArchetype(archetype, pitch), mentions)
	if e.gen.Provider() == "offline" {
		return tmpl, nil
	}
	var ebuf strings.Builder
	for _, m := range mentions {
		fmt.Fprintf(&ebuf, "[%s] %s — %s\n", m.ID, m.Title, m.Snippet)
	}
	if ebuf.Len() == 0 {
		ebuf.WriteString("(none gathered)\n")
	}
	sys := fmt.Sprintf(`You write %s posts for indie product launches.
HARD CONTRACT for this channel:
- title rule: %s (title limit %d chars; body ceiling %d chars)
- norms: %s
STRICT RULES: do not invent users, numbers, testimonials, or facts not present in the EVIDENCE block; write [FILL: …] for anything you'd need to know. No marketing superlatives. Return the title on the first line (or skip if the channel has no title), then a blank line, then the body.`,
		spec.Label, orDash(spec.TitleRule), spec.TitleMax, spec.BodyMax, strings.Join(spec.Norms, "; "))
	user := fmt.Sprintf("PRODUCT: %s\nPITCH: %s\nHOOK ARCHETYPE: %s (outcome = lead with the concrete result; problem = lead with the pain; question = open with a question that creates a loop)\n\nEVIDENCE:\n%s", subject, pitch, archetype, ebuf.String())
	out, err := e.gen.Generate(ctx, GenPrompt{System: sys, User: user})
	if err != nil {
		return tmpl, []string{spec.Key + ": draft generation failed, using template: " + err.Error()}
	}
	title, body, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if spec.TitleMax == 0 { // channel has no title — whole output is body
		return ChannelDraft{Channel: spec.Key, Body: strings.TrimSpace(out)}, nil
	}
	return ChannelDraft{Channel: spec.Key, Title: strings.TrimSpace(title), Body: strings.TrimSpace(body)}, nil
}

// applyArchetype reframes the pitch line per hook archetype, staying honest:
// the problem framing introduces an explicit fill slot rather than inventing a
// pain point, so tune's rubric correctly discounts it until an operator (or a
// live LLM run) supplies the real pain.
func applyArchetype(archetype, pitch string) string {
	switch archetype {
	case "problem":
		return "[FILL: the pain, in one clause] — fixed: " + pitch
	case "question":
		if pitch == "" || strings.HasPrefix(pitch, "[") {
			return pitch
		}
		return "what if " + strings.ToLower(string(pitch[0])) + pitch[1:] + "?"
	default: // "outcome"
		return pitch
	}
}

// templateDraft is the deterministic offline draft: honest scaffolding with
// explicit fill slots, woven with grounded mention facts when available.
func templateDraft(spec ChannelSpec, subject, pitch string, mentions []Evidence) ChannelDraft {
	voice := ""
	if len(mentions) > 0 {
		m := mentions[0]
		voice = fmt.Sprintf("\n\nThere's already conversation in this space: %s (%s).", m.Title, m.Snippet)
	}
	switch spec.Key {
	case "show-hn":
		return ChannelDraft{
			Channel: spec.Key,
			Title:   showHNTitle(subject, pitch, spec.TitleMax),
			Body: fmt.Sprintf("I built %s. %s\n\nHow it works: [FILL: 2-3 sentences of technical substance — stack, approach, the hard part].\n\nWhy I built it: [FILL: the itch you scratched].%s\n\nIt's live at [FILL: url]. I'll be in the comments — what would make this useful for you?",
				subject, pitch, voice),
		}
	case "producthunt":
		return ChannelDraft{
			Channel: spec.Key,
			Title:   clampRunes(pitch, spec.TitleMax),
			Body: fmt.Sprintf("Maker first-comment:\n\nHi PH — I'm the maker of %s. %s\n\nThe origin story: [FILL: why this exists].\n\nWhat's different: [FILL: the one mechanic competitors don't have].%s\n\nI'd love feedback on [FILL: the specific open question].",
				subject, pitch, voice),
		}
	case "reddit":
		return ChannelDraft{
			Channel: spec.Key,
			Title:   clampRunes("I built a tool for [FILL: the problem] — here's what I learned", spec.TitleMax),
			Body: fmt.Sprintf("The problem: [FILL: the pain in this sub's vocabulary].\n\nWhat I tried first: [FILL: honest pre-history].\n\nWhat I learned building %s: [FILL: 2-3 genuinely useful takeaways that stand alone without the product].%s\n\nThe tool itself (%s) is linked in the comments per sub rules — happy to answer anything.",
				subject, voice, pitch),
		}
	case "x":
		return ChannelDraft{
			Channel: spec.Key,
			Title:   clampRunes(fmt.Sprintf("I shipped %s this week: %s\n\nHere's how it works 🧵", subject, pitch), spec.TitleMax),
			Body: fmt.Sprintf("2/ The problem: [FILL: concrete pain, one sentence].\n\n3/ %s — [FILL: the core mechanic].\n\n4/ [FILL: screenshot or 20s demo video].\n\n5/ The hard part was [FILL: technical detail builders respect].%s\n\n6/ It's live: [FILL: url]. Try it and tell me what breaks.",
				subject, voice),
		}
	case "linkedin":
		return ChannelDraft{
			Channel: spec.Key,
			Body: fmt.Sprintf("[FILL: one-line hook about the problem — must work before the fold].\n\nFor months: [FILL: the costly status quo].\n\nSo I built %s.\n\n%s\n\nWhat changed: [FILL: the before/after].%s\n\nLink in the first comment.",
				subject, pitch, voice),
		}
	case "indiehackers":
		return ChannelDraft{
			Channel: spec.Key,
			Title:   clampRunes(fmt.Sprintf("Shipped %s — [FILL: the milestone, with a real number]", subject), spec.TitleMax),
			Body: fmt.Sprintf("What it is: %s\n\nNumbers so far: [FILL: users / revenue / conversion — real ones, IH rewards transparency].\n\nWhat worked: [FILL]. What didn't: [FILL].%s\n\nQuestion for IH: [FILL: a genuine ask].",
				pitch, voice),
		}
	default:
		return ChannelDraft{Channel: spec.Key, Title: clampRunes(subject+" — "+pitch, spec.TitleMax),
			Body: fmt.Sprintf("%s. [FILL: channel-appropriate body].%s", pitch, voice)}
	}
}

// draftSection renders one channel draft as a report section. The draft claim
// is inferred when community evidence shaped it, speculative otherwise — a
// draft is never presented as a grounded fact.
func draftSection(spec ChannelSpec, d ChannelDraft, mentions []Evidence) Section {
	var b strings.Builder
	if d.Title != "" {
		b.WriteString("**Title:** " + d.Title + "\n\n")
	}
	b.WriteString(d.Body)
	b.WriteString("\n\n*Channel contract:* ")
	if spec.TitleRule != "" {
		b.WriteString(spec.TitleRule + "; ")
	}
	b.WriteString(strings.Join(spec.Norms, "; "))
	fmt.Fprintf(&b, "\n*Best slot:* %s", spec.BestSlot)

	conf := ConfSpeculative
	var cites []string
	if len(mentions) > 0 {
		conf = ConfInferred
		cites = ids(mentions)
	}
	return Section{
		Title: fmt.Sprintf("%s draft (%s)", spec.Label, spec.Kind),
		Body:  b.String(),
		Claims: []Claim{{
			Text:       fmt.Sprintf("Proposed %s angle for %s", spec.Label, d.Channel),
			Confidence: conf,
			Citations:  cites,
		}},
	}
}

// calendarSection lays the selected channels onto a launch-week schedule:
// launch-kind placements first (in registry order), social-kind echoes after.
func calendarSection(specs []ChannelSpec) Section {
	var b strings.Builder
	b.WriteString("Launch-week placement order (launch channels first, social echoes after):\n")
	day := 1
	for _, kind := range []string{"launch", "social"} {
		for _, s := range specs {
			if s.Kind != kind {
				continue
			}
			fmt.Fprintf(&b, "- Day %d — **%s** · %s\n", day, s.Label, s.BestSlot)
			day++
		}
	}
	b.WriteString("\nRecord each placement with `ngtm launch posted <product> --channel <key> --url <post-url>` so signals and verdicts attribute correctly.")
	return Section{Title: "Distribution Calendar", Body: b.String()}
}

func channelKeys(specs []ChannelSpec) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.Key
	}
	return out
}

// clampRunes truncates s to at most n runes (rune-safe, with ellipsis).
func clampRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n > 1 {
		return string(r[:n-1]) + "…"
	}
	return string(r[:n])
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// showHNTitle builds a contract-valid Show HN title without doubling the
// product name or the "Show HN:" prefix when the pitch already carries them.
func showHNTitle(subject, pitch string, max int) string {
	subject = strings.TrimSpace(subject)
	pitch = strings.TrimSpace(pitch)
	if pitch == "" {
		pitch = "[FILL: one-line outcome this product delivers]"
	}
	if strings.HasPrefix(pitch, "Show HN:") {
		return clampRunes(pitch, max)
	}
	foldedPitch := strings.ToLower(pitch)
	foldedSubject := strings.ToLower(subject)
	alreadyNamed := foldedSubject != "" && (strings.HasPrefix(foldedPitch, foldedSubject+" ") ||
		strings.HasPrefix(foldedPitch, foldedSubject+"–") ||
		strings.HasPrefix(foldedPitch, foldedSubject+" -") ||
		strings.HasPrefix(foldedPitch, foldedSubject+" —"))
	if !alreadyNamed && strings.HasPrefix(foldedPitch, "nicos gtm") {
		alreadyNamed = true
	}
	if alreadyNamed {
		return clampRunes("Show HN: "+pitch, max)
	}
	if subject == "" {
		return clampRunes("Show HN: "+pitch, max)
	}
	return clampRunes("Show HN: "+subject+" – "+pitch, max)
}

// OfflineDraft exposes the deterministic template for one channel — used by
// `launch open` to prefill submission/intent URLs without an engine run.
func OfflineDraft(spec ChannelSpec, subject, pitch string) ChannelDraft {
	if strings.TrimSpace(pitch) == "" {
		pitch = "[FILL: one-line outcome this product delivers]"
	}
	return templateDraft(spec, subject, pitch, nil)
}
