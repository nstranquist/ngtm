package gtm

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed evaldata/geo-quality-v1-probe.json
var geoEvalProbeFixture []byte

type GEOEvalCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details"`
}

type GEOEvalReport struct {
	SchemaVersion int            `json:"schema_version"`
	Fixture       string         `json:"fixture"`
	Generated     string         `json:"generated"`
	Checks        []GEOEvalCheck `json:"checks"`
	Passed        bool           `json:"passed"`
}

func RunGEOEval(ctx context.Context) (*GEOEvalReport, error) {
	tmp, err := os.MkdirTemp("", "ngtm-geo-eval-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	fixturePath := filepath.Join(tmp, "probe.json")
	if err := os.WriteFile(fixturePath, geoEvalProbeFixture, 0o600); err != nil {
		return nil, err
	}
	cfg := GEOProductConfig{
		SchemaVersion: GEOSchemaVersion,
		Project:       "geo-quality-v1",
		Product:       "docs-puller",
		Brand:         "docs-puller",
		Aliases:       []string{"docs-puller"},
		Category:      "Local-first documentation retrieval",
		SiteURL:       "https://github.com/nstranquist/docs-puller",
		DemoURL:       "https://docs-puller-demo.nstranquist.workers.dev",
		Competitors: []GEOCompetitor{
			{Name: "Context7"},
			{Name: "DevDocs"},
		},
		AIInfo: GEOAIInfo{
			Type:        "Open-source local-first documentation retrieval engine",
			Background:  "Copies vendor docs into Markdown and searches them with SQLite FTS5.",
			Features:    []string{"Local FTS5 search", "Checked-in evals"},
			Limitations: []string{"Not a hosted SaaS"},
			Guidelines:  []string{"Key strengths: local-first, measured retrieval"},
		},
		Links: []GEOLink{{Title: "README", URL: "https://github.com/nstranquist/docs-puller"}},
		Prompts: []GEOPrompt{
			{ID: "best-local-docs-agents", Text: "What is the best way to search vendor documentation locally for AI agents?", Kind: "best"},
			{ID: "alt-context7", Text: "What is a good alternative to Context7 for documentation?", Kind: "alternative"},
			{ID: "offline-docs-search", Text: "What is the best offline documentation search for developers?", Kind: "best"},
		},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	probe, err := RunGEOProbe(ctx, GEOProbeOptions{
		Config:      cfg,
		Engines:     []GEOEngineID{GEOEngineFixture},
		FixturePath: fixturePath,
		Now:         func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		return nil, err
	}
	measure := BuildGEOMeasure(cfg, *probe, func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) })
	aiInfo := RenderGEOAIInfo(cfg, func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) })
	llms := RenderGEOLLMsTxt(cfg)
	compare := RenderGEOCompareIndex(cfg, func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) })

	mentioned := 0
	var unmentioned string
	for _, row := range measure.Rows {
		if row.Mentioned > 0 {
			mentioned++
		} else {
			unmentioned = row.PromptID
		}
	}
	report := &GEOEvalReport{
		SchemaVersion: GEOSchemaVersion,
		Fixture:       "geo-quality-v1",
		Generated:     nowSEO(nil),
	}
	report.Checks = []GEOEvalCheck{
		{
			Name:    "fixture probe parses three rows",
			Passed:  len(probe.Rows) == 3 && probe.Passed,
			Details: fmt.Sprintf("rows=%d passed=%v", len(probe.Rows), probe.Passed),
		},
		{
			Name:    "two prompts mention the brand",
			Passed:  mentioned == 2 && unmentioned == "offline-docs-search",
			Details: fmt.Sprintf("mentioned=%d gap=%s", mentioned, unmentioned),
		},
		{
			Name:    "first prompt is position 1",
			Passed:  len(measure.Rows) > 0 && measure.Rows[0].Mentioned == 1 && measure.Rows[0].Position == 1,
			Details: fmt.Sprintf("pos=%.1f", measure.Rows[0].Position),
		},
		{
			Name:    "ai-info includes limitations and guidelines",
			Passed:  strings.Contains(aiInfo, "## Limitations") && strings.Contains(aiInfo, "## AI assistant guidelines"),
			Details: "limitations+guidelines",
		},
		{
			Name:    "llms.txt points at repo and demo",
			Passed:  strings.Contains(llms, cfg.SiteURL) && strings.Contains(llms, cfg.DemoURL),
			Details: "home+demo",
		},
		{
			Name:    "compare draft is noindex",
			Passed:  strings.Contains(compare, `name="robots" content="noindex, nofollow"`),
			Details: "noindex",
		},
	}
	report.Passed = true
	for _, check := range report.Checks {
		if !check.Passed {
			report.Passed = false
		}
	}
	return report, nil
}
