package gtm

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type GEOMeasureRow struct {
	PromptID    string   `json:"prompt_id"`
	Prompt      string   `json:"prompt"`
	Topic       string   `json:"topic,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Runs        int      `json:"runs"`
	Mentioned   int      `json:"mentioned"`
	Position    float64  `json:"position,omitempty"`
	Sentiment   float64  `json:"sentiment"`
	Visibility  float64  `json:"visibility"`
	Competitors []string `json:"competitors,omitempty"`
	Citations   []string `json:"citations,omitempty"`
	Engines     []string `json:"engines,omitempty"`
	Errors      int      `json:"errors"`
}

type GEOMeasureReport struct {
	SchemaVersion int              `json:"schema_version"`
	Project       string           `json:"project"`
	Product       string           `json:"product"`
	Brand         string           `json:"brand"`
	Generated     string           `json:"generated"`
	PromptCount   int              `json:"prompt_count"`
	MentionRate   float64          `json:"mention_rate"`
	Rows          []GEOMeasureRow  `json:"rows"`
	Findings      []GEOFinding     `json:"findings,omitempty"`
	Passed        bool             `json:"passed"`
}

func BuildGEOMeasure(cfg GEOProductConfig, probe GEOProbeReport, now func() time.Time) *GEOMeasureReport {
	generated := nowSEO(now)
	report := &GEOMeasureReport{
		SchemaVersion: GEOSchemaVersion,
		Project:       cfg.Project,
		Product:       cfg.Product,
		Brand:         cfg.Brand,
		Generated:     generated,
		Passed:        true,
	}
	byPrompt := map[string]*GEOMeasureRow{}
	order := make([]string, 0, len(cfg.Prompts))
	for _, p := range cfg.Prompts {
		row := &GEOMeasureRow{
			PromptID: p.ID,
			Prompt:   p.Text,
			Topic:    p.Topic,
			Kind:     p.Kind,
		}
		byPrompt[p.ID] = row
		order = append(order, p.ID)
	}
	for _, raw := range probe.Rows {
		row := byPrompt[raw.PromptID]
		if row == nil {
			row = &GEOMeasureRow{PromptID: raw.PromptID, Prompt: raw.Prompt, Topic: raw.Topic, Kind: raw.Kind}
			byPrompt[raw.PromptID] = row
			order = append(order, raw.PromptID)
		}
		row.Runs++
		if raw.Error != "" {
			row.Errors++
			continue
		}
		if raw.Mentioned {
			row.Mentioned++
			row.Position += float64(raw.Position)
			row.Sentiment += float64(raw.Sentiment)
		}
		row.Competitors = uniqueStrings(append(row.Competitors, raw.Competitors...))
		row.Citations = uniqueStrings(append(row.Citations, raw.Citations...))
		row.Engines = uniqueStrings(append(row.Engines, string(raw.Engine)))
	}
	var mentionedPrompts int
	for _, id := range order {
		row := *byPrompt[id]
		if row.Runs > 0 {
			ok := row.Runs - row.Errors
			if ok > 0 {
				row.Visibility = float64(row.Mentioned) / float64(ok)
			}
			if row.Mentioned > 0 {
				row.Position = row.Position / float64(row.Mentioned)
				row.Sentiment = row.Sentiment / float64(row.Mentioned)
				mentionedPrompts++
			}
		}
		report.Rows = append(report.Rows, row)
	}
	report.PromptCount = len(report.Rows)
	if report.PromptCount > 0 {
		report.MentionRate = float64(mentionedPrompts) / float64(report.PromptCount)
	}
	if report.PromptCount == 0 {
		report.Passed = false
		report.Findings = append(report.Findings, GEOFinding{
			Code: "NO_PROMPTS", Severity: "blocker", Message: "measure has no prompt rows",
		})
	}
	return report
}

func FormatGEOTable(report GEOMeasureReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GEO measure  product=%s  prompts=%d  mention_rate=%.0f%%\n",
		report.Product, report.PromptCount, report.MentionRate*100)
	fmt.Fprintf(&b, "%-44s %4s %8s %10s %11s  %s\n", "Prompt", "Pos", "Sent", "Vis", "Runs", "Competitors")
	for _, row := range report.Rows {
		pos := "—"
		if row.Mentioned > 0 {
			pos = fmt.Sprintf("#%.1f", row.Position)
		}
		vis := fmt.Sprintf("%.0f%%", row.Visibility*100)
		prompt := row.Prompt
		if len(prompt) > 44 {
			prompt = prompt[:41] + "…"
		}
		fmt.Fprintf(&b, "%-44s %4s %8s %10s %5d/%-4d  %s\n",
			prompt, pos, formatGEOAvg(row.Sentiment, row.Mentioned > 0), vis,
			row.Mentioned, row.Runs, strings.Join(row.Competitors, ", "))
	}
	return b.String()
}

func formatGEOAvg(v float64, ok bool) string {
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.0f", math.Round(v))
}


