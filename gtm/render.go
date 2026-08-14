package gtm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON renders the report as indented JSON (the machine surface for MCP/agents).
func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Markdown renders the human-facing report. Claims are labeled by confidence so
// a reader can instantly separate measured facts (grounded) from strategy
// (inferred) from guesses (speculative) — the whole point of the factory.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# GTM Report — %s (%s)\n\n", r.Subject, r.Vertical)
	model := r.Provider
	if r.Model != "" {
		model = r.Provider + "/" + r.Model
	}
	fmt.Fprintf(&b, "_%s · narrative: %s · tiers: %s · %d evidence items_\n\n",
		r.Generated, model, joinTiers(r.Tiers), len(r.Evidence))

	if len(r.Warnings) > 0 {
		b.WriteString("> ⚠️ **Caveats**\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "> - %s\n", w)
		}
		b.WriteString("\n")
	}

	for _, s := range r.Sections {
		fmt.Fprintf(&b, "## %s\n\n", s.Title)
		if strings.TrimSpace(s.Body) != "" {
			b.WriteString(strings.TrimSpace(s.Body))
			b.WriteString("\n\n")
		}
		if len(s.Claims) > 0 {
			b.WriteString("**Claims**\n")
			for _, c := range s.Claims {
				cites := ""
				if len(c.Citations) > 0 {
					cites = " — sources: " + strings.Join(bracket(c.Citations), ", ")
				}
				fmt.Fprintf(&b, "- `[%s]` %s%s\n", c.Confidence, c.Text, cites)
			}
			b.WriteString("\n")
		}
	}

	if r.Panel != nil {
		status := "REJECTED"
		if r.Panel.Survives {
			status = "SURVIVES"
		}
		panelTitle := r.Panel.Title
		if panelTitle == "" {
			panelTitle = "Shark-Tank Panel"
		}
		fmt.Fprintf(&b, "## %s — median %.1f/10 — %s\n\n", panelTitle, r.Panel.MedianScore, status)
		for _, v := range r.Panel.Verdicts {
			fmt.Fprintf(&b, "- **%s** (%d/10): %s\n", v.Critic, v.Score, v.Rationale)
			for _, k := range v.Kills {
				fmt.Fprintf(&b, "  - 💀 %s\n", k)
			}
		}
		if len(r.Panel.TopKills) > 0 {
			b.WriteString("\n**Top objections to resolve before launch:**\n")
			for _, k := range r.Panel.TopKills {
				fmt.Fprintf(&b, "1. %s\n", k)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Evidence\n\n")
	for _, e := range r.Evidence {
		tag := string(e.Tier)
		if e.Synthetic {
			tag = "synthetic"
		}
		line := fmt.Sprintf("- `[%s]` (%s/%s) **%s**", e.ID, e.Feed, tag, e.Title)
		if e.Snippet != "" {
			line += " — " + e.Snippet
		}
		if e.URL != "" {
			line += " <" + e.URL + ">"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func joinTiers(tiers []FeedTier) string {
	ss := make([]string, len(tiers))
	for i, t := range tiers {
		ss[i] = string(t)
	}
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ",")
}

func bracket(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "`[" + id + "]`"
	}
	return out
}
