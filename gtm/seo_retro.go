package gtm

import (
	"fmt"
	"strings"
)

type SEORetroDecision struct {
	Keyword  string  `json:"keyword"`
	Page     string  `json:"page,omitempty"`
	Decision string  `json:"decision"`
	Reason   string  `json:"reason"`
	Score    float64 `json:"score"`
}

type SEORetroReport struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Generated     string             `json:"generated"`
	Project       string             `json:"project"`
	ResearchID    string             `json:"research_id"`
	MeasurementID string             `json:"measurement_id"`
	Decisions     []SEORetroDecision `json:"decisions"`
	Findings      []SEOFinding       `json:"findings"`
	Passed        bool               `json:"passed"`
	Artifact      *SEOArtifactRef    `json:"artifact,omitempty"`
}

func BuildSEORetro(research SEOResearchReport, measurement SEOMeasurementReport, now func() string) *SEORetroReport {
	generated := nowSEO(nil)
	if now != nil {
		generated = now()
	}
	report := &SEORetroReport{SchemaVersion: SEOSchemaVersion, Generated: generated, Project: research.Project, ResearchID: research.ID, MeasurementID: measurement.ID}
	for _, keyword := range research.Keywords {
		metrics, page := metricsForSEOKeyword(keyword.Keyword, measurement.Rows)
		decision, reason := decideSEORetro(keyword, metrics)
		report.Decisions = append(report.Decisions, SEORetroDecision{Keyword: keyword.Keyword, Page: page, Decision: decision, Reason: reason, Score: keyword.Opportunity.Score})
	}
	if len(measurement.Rows) == 0 {
		report.Findings = append(report.Findings, SEOFinding{Code: "RETRO_MEASUREMENT_EMPTY", Severity: "blocker", Message: "retro requires measured first-party rows"})
	}
	report.Passed = !hasSEOBlockers(report.Findings)
	identity := *report
	identity.ID = ""
	identity.Artifact = nil
	id, _ := digestSEOValue(identity)
	report.ID = "seoretro:" + strings.TrimPrefix(id, "sha256:")[:16]
	return report
}

func decideSEORetro(keyword SEOKeyword, metrics SEOFirstPartyMetrics) (string, string) {
	if len(keyword.ExistingPages) > 1 {
		return "consolidate", "multiple owned pages overlap the same opportunity"
	}
	if metrics.KeyEvents > 0 || metrics.Revenue > 0 || (metrics.Clicks >= 20 && metrics.CTR >= 0.04) {
		return "double-down", "observed conversion or strong click-through signal"
	}
	if metrics.Impressions >= 100 && metrics.CTR < 0.02 {
		return "iterate", "meaningful impressions with click-through below 2%"
	}
	if metrics.Impressions >= 50 && metrics.Position >= 8 && metrics.Position <= 20 {
		return "refresh", "page is within striking distance of the first page"
	}
	if metrics.Impressions == 0 && metrics.Sessions == 0 {
		if keyword.Opportunity.Score < 35 {
			return "retire", "no observed signal and low modeled opportunity"
		}
		return "iterate", "no observed signal yet; improve or collect a longer window"
	}
	return "iterate", fmt.Sprintf("insufficient decisive signal (%.0f impressions, %.0f clicks)", metrics.Impressions, metrics.Clicks)
}

func metricsForSEOKeyword(keyword string, rows []SEOMeasurementRow) (SEOFirstPartyMetrics, string) {
	var total SEOFirstPartyMetrics
	page := ""
	needle := strings.ToLower(strings.ReplaceAll(keyword, " ", "-"))
	for _, row := range rows {
		if !strings.Contains(strings.ToLower(row.Query), strings.ToLower(keyword)) && !strings.Contains(strings.ToLower(row.Page), needle) {
			continue
		}
		page = row.Page
		total.Clicks += row.Metrics.Clicks
		total.Impressions += row.Metrics.Impressions
		total.Sessions += row.Metrics.Sessions
		total.EngagedSessions += row.Metrics.EngagedSessions
		total.KeyEvents += row.Metrics.KeyEvents
		total.Revenue += row.Metrics.Revenue
		if row.Metrics.Position > 0 && (total.Position == 0 || row.Metrics.Position < total.Position) {
			total.Position = row.Metrics.Position
		}
	}
	if total.Impressions > 0 {
		total.CTR = total.Clicks / total.Impressions
	}
	return total, page
}

func (r *SEORetroReport) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO retro: %s\n\n- Research: `%s`\n- Measurement: `%s`\n- Status: %s\n\n", r.Project, r.ResearchID, r.MeasurementID, passLabel(r.Passed))
	out.WriteString("| Keyword | Decision | Reason |\n|---|---|---|\n")
	for _, d := range r.Decisions {
		fmt.Fprintf(&out, "| %s | **%s** | %s |\n", d.Keyword, strings.ToUpper(d.Decision), d.Reason)
	}
	return out.String()
}
