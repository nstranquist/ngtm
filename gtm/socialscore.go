package gtm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The social scorecard is the self-eval harness of the content factory: every
// channel draft is scored /10 against a computable rubric, the score lands in
// Report.Metrics (machine-readable, telemetry-able), and --tune uses it to
// pick the best hook archetype per channel. Like the design scorer, the rubric
// is deterministic — a number you can regress against, not a vibe.

// DraftScore is one channel draft's rubric result.
type DraftScore struct {
	Channel string             `json:"channel"`
	Total   float64            `json:"total"` // 0..10
	Parts   map[string]float64 `json:"parts"` // dimension → 0..2
	Notes   []string           `json:"notes,omitempty"`
}

// Hook archetypes --tune explores per channel. Each varies only the hook/title
// framing; body scaffolding stays identical so the rubric isolates the hook.
var hookArchetypes = []string{"outcome", "problem", "question"}

var digitRe = regexp.MustCompile(`[0-9]`)

// ScoreSocialDraft applies the rubric. Five dimensions, 0..2 each:
//
//	contract     — obeys the typed ChannelSpec (lint violations subtract)
//	grounding    — community evidence exists and is woven into the draft
//	specificity  — hook carries something concrete (number, $, %, named mechanism)
//	completeness — unresolved [FILL:] slots subtract (an unedited pack isn't ship-ready)
//	cta          — the draft asks for something (question / explicit ask)
func ScoreSocialDraft(spec ChannelSpec, d ChannelDraft, mentions []Evidence) DraftScore {
	parts := map[string]float64{}
	var notes []string

	// contract: start at 2, -1 per lint violation.
	violations := lintDraft(spec, d)
	parts["contract"] = clamp02(2 - float64(len(violations)))
	for _, v := range violations {
		notes = append(notes, "contract: "+v)
	}

	// grounding: 2 woven, 1 evidence existed but unwoven, 0 none.
	switch {
	case len(mentions) > 0 && mentionWoven(d, mentions):
		parts["grounding"] = 2
	case len(mentions) > 0:
		parts["grounding"] = 1
		notes = append(notes, "grounding: community evidence gathered but not woven into the draft")
	default:
		parts["grounding"] = 0
		notes = append(notes, "grounding: no community evidence — run with live feeds")
	}

	// specificity: judge the hook (title, or first body line for title-less channels).
	hook := d.Title
	if hook == "" {
		hook, _, _ = strings.Cut(d.Body, "\n")
	}
	spec2 := 0.0
	if hookHasSourcedQuantity(hook, mentions) {
		spec2 += 1
	} else if digitRe.MatchString(hook) || strings.ContainsAny(hook, "$%") {
		notes = append(notes, "specificity: numeric/$/% hook is not present in evidence")
	}
	if !strings.Contains(hook, "[FILL:") {
		spec2 += 1
	} else {
		notes = append(notes, "specificity: hook still carries a [FILL:] slot")
	}
	parts["specificity"] = clamp02(spec2)

	// completeness: each unresolved fill slot costs 0.4.
	fills := strings.Count(d.Title+d.Body, "[FILL:")
	parts["completeness"] = clamp02(2 - 0.4*float64(fills))
	if fills > 0 {
		notes = append(notes, fmt.Sprintf("completeness: %d [FILL:] slot(s) to resolve before posting", fills))
	}

	// cta: the draft must ask for something.
	if strings.Contains(d.Body, "?") || strings.Contains(strings.ToLower(d.Body), "try it") {
		parts["cta"] = 2
	} else {
		parts["cta"] = 0
		notes = append(notes, "cta: no question or explicit ask in the body")
	}

	total := 0.0
	for _, v := range parts {
		total += v
	}
	return DraftScore{Channel: spec.Key, Total: total, Parts: parts, Notes: notes}
}

var quantityTokenRe = regexp.MustCompile(`\$?\d+(?:\.\d+)?%?`)

func hookHasSourcedQuantity(hook string, mentions []Evidence) bool {
	tokens := quantityTokenRe.FindAllString(hook, -1)
	if len(tokens) == 0 && !strings.ContainsAny(hook, "$%") {
		return false
	}
	if len(tokens) == 0 {
		return false
	}
	ev := evidenceQuantityText(mentions)
	if ev == "" {
		return false
	}
	for _, tok := range tokens {
		if evidenceHasQuantityToken(ev, tok) {
			return true
		}
	}
	return false
}

func evidenceHasQuantityToken(ev, tok string) bool {
	re := regexp.MustCompile(`(?:^|[^0-9.])` + regexp.QuoteMeta(tok) + `(?:[^0-9.%]|$)`)
	return re.MatchString(ev)
}

func evidenceQuantityText(mentions []Evidence) string {
	var b strings.Builder
	for _, m := range mentions {
		if m.Title != "" {
			b.WriteString(m.Title)
			b.WriteByte(' ')
		}
		if m.Snippet != "" {
			b.WriteString(m.Snippet)
			b.WriteByte(' ')
		}
		if m.Value != "" {
			b.WriteString(m.Value)
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// mentionWoven reports whether any gathered mention's title appears in the draft.
func mentionWoven(d ChannelDraft, mentions []Evidence) bool {
	body := d.Title + " " + d.Body
	for _, m := range mentions {
		if m.Title != "" && strings.Contains(body, m.Title) {
			return true
		}
	}
	return false
}

func clamp02(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 2 {
		return 2
	}
	return v
}

// scorecardSection renders the per-channel rubric results as a report section,
// sorted by score descending so the strongest draft leads.
func scorecardSection(scores []DraftScore) Section {
	sorted := append([]DraftScore{}, scores...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Total > sorted[j].Total })
	var b strings.Builder
	b.WriteString("Rubric: contract + grounding + specificity + completeness + cta, 0–2 each (computable, regressable).\n\n")
	b.WriteString("| channel | score | contract | grounding | specificity | completeness | cta |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, s := range sorted {
		fmt.Fprintf(&b, "| %s | **%.1f**/10 | %.1f | %.1f | %.1f | %.1f | %.1f |\n",
			s.Channel, s.Total, s.Parts["contract"], s.Parts["grounding"], s.Parts["specificity"], s.Parts["completeness"], s.Parts["cta"])
	}
	seen := map[string]bool{}
	var noteLines []string
	for _, s := range sorted {
		for _, n := range s.Notes {
			key := s.Channel + n
			if !seen[key] {
				seen[key] = true
				noteLines = append(noteLines, fmt.Sprintf("- %s: %s", s.Channel, n))
			}
		}
	}
	if len(noteLines) > 0 {
		b.WriteString("\nWhat would raise the score:\n")
		b.WriteString(strings.Join(noteLines, "\n"))
	}
	return Section{Title: "Content Scorecard", Body: b.String()}
}
