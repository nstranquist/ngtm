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

//go:embed evaldata/seo-quality-v2-research.json
var seoEvalResearchFixture []byte

//go:embed evaldata/seo-quality-v2-measurement.json
var seoEvalMeasurementFixture []byte

type SEOEvalCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details"`
}

type SEOEvalReport struct {
	SchemaVersion int            `json:"schema_version"`
	Fixture       string         `json:"fixture"`
	Generated     string         `json:"generated"`
	Checks        []SEOEvalCheck `json:"checks"`
	Passed        bool           `json:"passed"`
}

func RunSEOEval(ctx context.Context) (*SEOEvalReport, error) {
	tmp, err := os.MkdirTemp("", "ngtm-seo-eval-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	researchPath := filepath.Join(tmp, "research.json")
	measurementPath := filepath.Join(tmp, "measurement.json")
	if err := os.WriteFile(researchPath, seoEvalResearchFixture, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(measurementPath, seoEvalMeasurementFixture, 0o600); err != nil {
		return nil, err
	}
	cfg := DefaultSEOProject("seo-quality-v2")
	cfg.Product, cfg.Domain, cfg.SiteURL = "SEO Quality v2", "example.com", "https://example.com"
	cfg.SeedKeywords = []string{"seo automation pipeline"}
	cfg.Publishing.CanonicalBaseURL = "https://example.com"
	cfg.Publishing.MinimumWordCount = 120
	cfg.FirstParty.RequireInspection = true
	cfg.FirstParty.RequirePageSpeed = true
	eng, err := NewEngine(Options{Subject: cfg.Product, Offline: true, NoFeeds: true}, time.Now)
	if err != nil {
		return nil, err
	}
	research, err := eng.RunSEOResearch(ctx, SEOResearchRunOptions{Config: cfg, Offline: true, FixturePath: researchPath, Now: func() time.Time { return time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		return nil, err
	}
	unique := "A reproducible fixture connects scoped SERP evidence, transparent opportunity scoring, guarded publishing, first-party measurement, and deterministic retro decisions."
	brief, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: unique}, func() string { return "2026-07-14T00:00:00Z" })
	if err != nil {
		return nil, err
	}
	body := strings.Repeat("This fixture demonstrates evidence-backed SEO automation with deterministic quality gates and useful original analysis. ", 18)
	publish, err := PublishSEOPage(cfg, *brief, SEOPublishRequest{Body: body, Approved: true, Index: true}, filepath.Join(tmp, "site"), func() string { return "2026-07-14T00:00:00Z" })
	if err != nil {
		return nil, err
	}
	measure, err := MeasureSEO(ctx, cfg, research, publish, SEOMeasurementOptions{FixturePath: measurementPath, Now: func() time.Time { return time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		return nil, err
	}
	retro := BuildSEORetro(*research, *measure, func() string { return "2026-07-14T00:00:00Z" })
	report := &SEOEvalReport{SchemaVersion: SEOSchemaVersion, Fixture: "seo-quality-v2", Generated: nowSEO(nil)}
	report.Checks = []SEOEvalCheck{
		{Name: "research evidence coverage", Passed: research.Passed && research.Coverage.SERP == 1 && research.Coverage.Volume == 1 && research.Coverage.LiveEvidence == 0, Details: fmt.Sprintf("serp=%.2f volume=%.2f live=%.2f (fixture must not count as live)", research.Coverage.SERP, research.Coverage.Volume, research.Coverage.LiveEvidence)},
		{Name: "transparent opportunity score", Passed: len(research.Keywords) == 1 && research.Keywords[0].Opportunity.Score > 0 && research.Keywords[0].Opportunity.Confidence >= 0.70, Details: fmt.Sprintf("score=%.3f confidence=%.3f", research.Keywords[0].Opportunity.Score, research.Keywords[0].Opportunity.Confidence)},
		{Name: "brief unique value and evidence", Passed: brief.Passed && len(brief.EvidenceIDs) > 0, Details: fmt.Sprintf("evidence=%d", len(brief.EvidenceIDs))},
		{Name: "guarded indexable publish", Passed: publish.Passed && publish.Pages[0].Indexable && publish.SitemapPath != "", Details: publish.Pages[0].Robots},
		{Name: "measurement fixture", Passed: measure.Passed && measure.Provenance == "fixture" && len(measure.Rows) == 1 && measure.Coverage == 1, Details: fmt.Sprintf("coverage=%.2f published=%s measured=%s", measure.Coverage, publish.Pages[0].Canonical, measure.Rows[0].Page)},
		{Name: "closed-loop retro", Passed: retro.Passed && len(retro.Decisions) == 1 && retro.Decisions[0].Decision == "double-down", Details: retro.Decisions[0].Decision},
	}
	report.Passed = true
	for _, check := range report.Checks {
		report.Passed = report.Passed && check.Passed
	}
	return report, nil
}

func (r *SEOEvalReport) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO quality eval: %s\n\nStatus: **%s**\n\n", r.Fixture, passLabel(r.Passed))
	for _, check := range r.Checks {
		fmt.Fprintf(&out, "- [%s] %s — %s\n", map[bool]string{true: "x", false: " "}[check.Passed], check.Name, check.Details)
	}
	return out.String()
}
