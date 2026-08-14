package gtm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type SEOIndexInspection struct {
	URL             string `json:"url"`
	Verdict         string `json:"verdict,omitempty"`
	CoverageState   string `json:"coverage_state,omitempty"`
	RobotsState     string `json:"robots_state,omitempty"`
	IndexingState   string `json:"indexing_state,omitempty"`
	PageFetchState  string `json:"page_fetch_state,omitempty"`
	GoogleCanonical string `json:"google_canonical,omitempty"`
	UserCanonical   string `json:"user_canonical,omitempty"`
}

type SEOPageSpeed struct {
	URL           string  `json:"url"`
	Performance   float64 `json:"performance,omitempty"`
	Accessibility float64 `json:"accessibility,omitempty"`
	BestPractices float64 `json:"best_practices,omitempty"`
	SEO           float64 `json:"seo,omitempty"`
}

type SEOMeasurementRow struct {
	Query   string               `json:"query,omitempty"`
	Page    string               `json:"page"`
	Date    string               `json:"date,omitempty"`
	Device  string               `json:"device,omitempty"`
	Country string               `json:"country,omitempty"`
	Metrics SEOFirstPartyMetrics `json:"metrics"`
}

type SEOMeasurementReport struct {
	SchemaVersion int                  `json:"schema_version"`
	ID            string               `json:"id"`
	Generated     string               `json:"generated"`
	Project       string               `json:"project"`
	Provenance    string               `json:"provenance"`
	ResearchID    string               `json:"research_id,omitempty"`
	PublishID     string               `json:"publish_id,omitempty"`
	Providers     []string             `json:"providers"`
	Rows          []SEOMeasurementRow  `json:"rows"`
	Inspections   []SEOIndexInspection `json:"inspections,omitempty"`
	PageSpeed     []SEOPageSpeed       `json:"pagespeed,omitempty"`
	Coverage      float64              `json:"coverage"`
	Findings      []SEOFinding         `json:"findings"`
	Passed        bool                 `json:"passed"`
	Artifact      *SEOArtifactRef      `json:"artifact,omitempty"`
}

type SEOMeasurementOptions struct {
	FixturePath string
	StartDate   string
	EndDate     string
	Now         func() time.Time
	Client      *http.Client
}

func MeasureSEO(ctx context.Context, cfg SEOProjectConfig, research *SEOResearchReport, publish *SEOPublishManifest, opts SEOMeasurementOptions) (*SEOMeasurementReport, error) {
	generated := nowSEO(opts.Now)
	report := &SEOMeasurementReport{SchemaVersion: SEOSchemaVersion, Generated: generated, Project: cfg.Project, Provenance: "live"}
	if research != nil {
		report.ResearchID = research.ID
	}
	if publish != nil {
		if publish.Artifact != nil {
			report.PublishID = publish.Artifact.ID
		}
	}
	if strings.TrimSpace(opts.FixturePath) != "" {
		researchID, publishID := report.ResearchID, report.PublishID
		b, err := os.ReadFile(opts.FixturePath)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, report); err != nil {
			return nil, fmt.Errorf("parse SEO measurement fixture: %w", err)
		}
		report.SchemaVersion = SEOSchemaVersion
		report.Generated = generated
		report.Project = cfg.Project
		report.Provenance = "fixture"
		report.ResearchID = researchID
		report.PublishID = publishID
		finalizeSEOMeasurement(report, publish, cfg)
		return report, nil
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	start, end := seoMeasurementDates(opts, cfg.FirstParty.LookbackDays)
	startDate, startErr := time.Parse("2006-01-02", start)
	endDate, endErr := time.Parse("2006-01-02", end)
	if startErr != nil || endErr != nil || startDate.After(endDate) {
		return nil, fmt.Errorf("SEO measurement dates must be valid YYYY-MM-DD values with start <= end")
	}
	if cfg.FirstParty.SearchConsoleSite != "" {
		token := strings.TrimSpace(os.Getenv("GOOGLE_SEARCH_CONSOLE_ACCESS_TOKEN"))
		if token == "" {
			report.Findings = append(report.Findings, SEOFinding{Code: "GSC_TOKEN_MISSING", Severity: "blocker", Message: "GOOGLE_SEARCH_CONSOLE_ACCESS_TOKEN is required for the configured property"})
		} else {
			rows, err := fetchSearchConsole(ctx, client, cfg.FirstParty.SearchConsoleSite, start, end, token)
			if err != nil {
				report.Findings = append(report.Findings, SEOFinding{Code: "GSC_QUERY_FAILED", Severity: "blocker", Message: err.Error()})
			} else {
				report.Rows = append(report.Rows, rows...)
				report.Providers = append(report.Providers, "search-console")
			}
		}
	} else {
		report.Findings = append(report.Findings, SEOFinding{Code: "GSC_NOT_CONFIGURED", Severity: "warning", Message: "first_party.search_console_site is not configured"})
	}
	if cfg.FirstParty.GA4Property != "" {
		token := firstNonEmpty(strings.TrimSpace(os.Getenv("GOOGLE_ANALYTICS_ACCESS_TOKEN")), strings.TrimSpace(os.Getenv("GOOGLE_SEARCH_CONSOLE_ACCESS_TOKEN")))
		if token == "" {
			report.Findings = append(report.Findings, SEOFinding{Code: "GA4_TOKEN_MISSING", Severity: "blocker", Message: "GOOGLE_ANALYTICS_ACCESS_TOKEN is required for the configured property"})
		} else {
			rows, err := fetchGA4(ctx, client, cfg.FirstParty.GA4Property, start, end, token)
			if err != nil {
				report.Findings = append(report.Findings, SEOFinding{Code: "GA4_QUERY_FAILED", Severity: "blocker", Message: err.Error()})
			} else {
				report.Rows = mergeSEOMeasurementRows(report.Rows, rows)
				report.Providers = append(report.Providers, "ga4")
			}
		}
	}
	urls := publishedSEOURLs(publish)
	if cfg.FirstParty.RequireInspection {
		token := strings.TrimSpace(os.Getenv("GOOGLE_SEARCH_CONSOLE_ACCESS_TOKEN"))
		if len(urls) == 0 {
			report.Findings = append(report.Findings, SEOFinding{Code: "URL_INSPECTION_NO_PAGES", Severity: "blocker", Message: "URL Inspection is required but no published canonical URLs are available"})
		} else if token == "" || cfg.FirstParty.SearchConsoleSite == "" {
			report.Findings = append(report.Findings, SEOFinding{Code: "URL_INSPECTION_UNAVAILABLE", Severity: "blocker", Message: "URL Inspection requires a configured Search Console property and token"})
		} else {
			for _, pageURL := range urls {
				inspection, err := fetchURLInspection(ctx, client, pageURL, cfg.FirstParty.SearchConsoleSite, token)
				if err != nil {
					report.Findings = append(report.Findings, SEOFinding{Code: "URL_INSPECTION_FAILED", Severity: "blocker", Path: pageURL, Message: err.Error()})
					continue
				}
				report.Inspections = append(report.Inspections, inspection)
			}
			if len(report.Inspections) > 0 {
				report.Providers = append(report.Providers, "url-inspection")
			}
		}
	}
	if cfg.FirstParty.RequirePageSpeed {
		if len(urls) == 0 {
			report.Findings = append(report.Findings, SEOFinding{Code: "PAGESPEED_NO_PAGES", Severity: "blocker", Message: "PageSpeed is required but no published canonical URLs are available"})
		} else {
			for _, pageURL := range urls {
				ps, err := fetchPageSpeed(ctx, client, pageURL, strings.TrimSpace(os.Getenv("PAGESPEED_API_KEY")))
				if err != nil {
					report.Findings = append(report.Findings, SEOFinding{Code: "PAGESPEED_FAILED", Severity: "blocker", Path: pageURL, Message: err.Error()})
					continue
				}
				report.PageSpeed = append(report.PageSpeed, ps)
			}
			if len(report.PageSpeed) > 0 {
				report.Providers = append(report.Providers, "pagespeed")
			}
		}
	}
	finalizeSEOMeasurement(report, publish, cfg)
	return report, nil
}

func fetchSearchConsole(ctx context.Context, client *http.Client, site, start, end, token string) ([]SEOMeasurementRow, error) {
	base := strings.TrimRight(firstNonEmpty(os.Getenv("GSC_API_BASE_URL"), "https://www.googleapis.com/webmasters/v3"), "/")
	endpoint := base + "/sites/" + url.PathEscape(site) + "/searchAnalytics/query"
	body := map[string]any{"startDate": start, "endDate": end, "dimensions": []string{"query", "page", "date", "device", "country"}, "rowLimit": 25000}
	var response struct {
		Rows []struct {
			Keys        []string `json:"keys"`
			Clicks      float64  `json:"clicks"`
			Impressions float64  `json:"impressions"`
			CTR         float64  `json:"ctr"`
			Position    float64  `json:"position"`
		} `json:"rows"`
	}
	if err := seoJSONRequest(ctx, client, http.MethodPost, endpoint, token, body, &response); err != nil {
		return nil, err
	}
	rows := make([]SEOMeasurementRow, 0, len(response.Rows))
	for _, item := range response.Rows {
		keys := append(append([]string{}, item.Keys...), "", "", "", "", "")
		rows = append(rows, SEOMeasurementRow{Query: keys[0], Page: keys[1], Date: keys[2], Device: keys[3], Country: keys[4], Metrics: SEOFirstPartyMetrics{Clicks: item.Clicks, Impressions: item.Impressions, CTR: item.CTR, Position: item.Position}})
	}
	return rows, nil
}

func fetchGA4(ctx context.Context, client *http.Client, property, start, end, token string) ([]SEOMeasurementRow, error) {
	base := strings.TrimRight(firstNonEmpty(os.Getenv("GA4_API_BASE_URL"), "https://analyticsdata.googleapis.com/v1beta"), "/")
	property = strings.TrimPrefix(strings.TrimSpace(property), "properties/")
	endpoint := base + "/properties/" + url.PathEscape(property) + ":runReport"
	body := map[string]any{
		"dateRanges": []map[string]string{{"startDate": start, "endDate": end}},
		"dimensions": []map[string]string{{"name": "landingPagePlusQueryString"}},
		"metrics":    []map[string]string{{"name": "sessions"}, {"name": "engagedSessions"}, {"name": "keyEvents"}, {"name": "totalRevenue"}},
		"limit":      100000,
	}
	var response struct {
		Rows []struct {
			Dimensions []struct {
				Value string `json:"value"`
			} `json:"dimensionValues"`
			Metrics []struct {
				Value string `json:"value"`
			} `json:"metricValues"`
		} `json:"rows"`
	}
	if err := seoJSONRequest(ctx, client, http.MethodPost, endpoint, token, body, &response); err != nil {
		return nil, err
	}
	var rows []SEOMeasurementRow
	for _, item := range response.Rows {
		page := valueAtDimension(item.Dimensions, 0)
		rows = append(rows, SEOMeasurementRow{Page: page, Metrics: SEOFirstPartyMetrics{
			Sessions: parseSEOFloat(valueAtMetric(item.Metrics, 0)), EngagedSessions: parseSEOFloat(valueAtMetric(item.Metrics, 1)),
			KeyEvents: parseSEOFloat(valueAtMetric(item.Metrics, 2)), Revenue: parseSEOFloat(valueAtMetric(item.Metrics, 3)),
		}})
	}
	return rows, nil
}

func fetchURLInspection(ctx context.Context, client *http.Client, pageURL, site, token string) (SEOIndexInspection, error) {
	base := strings.TrimRight(firstNonEmpty(os.Getenv("GSC_INSPECTION_API_BASE_URL"), "https://searchconsole.googleapis.com/v1"), "/")
	var response struct {
		InspectionResult struct {
			IndexStatus struct {
				Verdict, CoverageState, RobotsTxtState, IndexingState, PageFetchState, GoogleCanonical, UserCanonical string
			} `json:"indexStatusResult"`
		} `json:"inspectionResult"`
	}
	if err := seoJSONRequest(ctx, client, http.MethodPost, base+"/urlInspection/index:inspect", token, map[string]string{"inspectionUrl": pageURL, "siteUrl": site}, &response); err != nil {
		return SEOIndexInspection{}, err
	}
	r := response.InspectionResult.IndexStatus
	return SEOIndexInspection{URL: pageURL, Verdict: r.Verdict, CoverageState: r.CoverageState, RobotsState: r.RobotsTxtState, IndexingState: r.IndexingState, PageFetchState: r.PageFetchState, GoogleCanonical: r.GoogleCanonical, UserCanonical: r.UserCanonical}, nil
}

func fetchPageSpeed(ctx context.Context, client *http.Client, pageURL, key string) (SEOPageSpeed, error) {
	base := firstNonEmpty(os.Getenv("PAGESPEED_API_BASE_URL"), "https://www.googleapis.com/pagespeedonline/v5/runPagespeed")
	q := url.Values{"url": []string{pageURL}, "category": []string{"PERFORMANCE", "ACCESSIBILITY", "BEST_PRACTICES", "SEO"}}
	if key != "" {
		q.Set("key", key)
	}
	var response struct {
		Lighthouse struct {
			Categories map[string]struct {
				Score float64 `json:"score"`
			} `json:"categories"`
		} `json:"lighthouseResult"`
	}
	if err := seoJSONRequest(ctx, client, http.MethodGet, base+"?"+q.Encode(), "", nil, &response); err != nil {
		return SEOPageSpeed{}, err
	}
	return SEOPageSpeed{URL: pageURL, Performance: response.Lighthouse.Categories["performance"].Score, Accessibility: response.Lighthouse.Categories["accessibility"].Score, BestPractices: response.Lighthouse.Categories["best-practices"].Score, SEO: response.Lighthouse.Categories["seo"].Score}, nil
}

func seoJSONRequest(ctx context.Context, client *http.Client, method, endpoint, token string, payload, out any) error {
	safeEndpoint := redactSEOEndpoint(endpoint)
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", safeEndpoint, resp.StatusCode)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode %s response: %w", safeEndpoint, err)
	}
	return nil
}

func redactSEOEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "configured SEO endpoint"
	}
	q := u.Query()
	for _, key := range []string{"key", "api_key", "access_token", "token"} {
		if q.Has(key) {
			q.Set(key, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func seoMeasurementDates(opts SEOMeasurementOptions, lookback int) (string, string) {
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	end := opts.EndDate
	if end == "" {
		end = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	start := opts.StartDate
	if start == "" {
		start = now.AddDate(0, 0, -lookback).Format("2006-01-02")
	}
	return start, end
}

func finalizeSEOMeasurement(report *SEOMeasurementReport, publish *SEOPublishManifest, cfg SEOProjectConfig) {
	report.Providers = normalizeStrings(report.Providers)
	pages := publishedSEOURLs(publish)
	seen := map[string]bool{}
	for _, row := range report.Rows {
		page := strings.TrimSpace(row.Page)
		if page != "" && row.Metrics.Present() {
			seen[page] = true
		}
	}
	if len(pages) > 0 {
		matched := 0
		for _, page := range pages {
			for measured := range seen {
				if seoMeasurementPageMatches(page, measured) {
					matched++
					break
				}
			}
		}
		report.Coverage = float64(matched) / float64(len(pages))
	} else if len(seen) > 0 {
		report.Coverage = 1
	}
	if cfg.Requirements.RequireFirstParty && report.Coverage < cfg.Requirements.MinimumCoverage {
		report.Findings = append(report.Findings, SEOFinding{
			Code: "MEASUREMENT_COVERAGE_LOW", Severity: "blocker",
			Message: fmt.Sprintf("first-party measurement coverage %.0f%% is below required %.0f%%", report.Coverage*100, cfg.Requirements.MinimumCoverage*100),
		})
	}
	report.Passed = !hasSEOBlockers(report.Findings)
	identity := *report
	identity.ID = ""
	identity.Artifact = nil
	id, _ := digestSEOValue(identity)
	report.ID = "seomeasure:" + strings.TrimPrefix(id, "sha256:")[:16]
}

func seoMeasurementPageMatches(published, measured string) bool {
	canonicalize := func(raw string) (host, path string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", ""
		}
		u, err := url.Parse(raw)
		if err == nil && u.Hostname() != "" {
			return normalizeDomain(u.Hostname()), strings.TrimRight(u.EscapedPath(), "/")
		}
		if strings.HasPrefix(raw, "/") {
			if u, err := url.Parse(raw); err == nil {
				return "", strings.TrimRight(u.EscapedPath(), "/")
			}
		}
		return "", ""
	}
	pubHost, pubPath := canonicalize(published)
	measuredHost, measuredPath := canonicalize(measured)
	if pubPath == "" || measuredPath == "" || pubPath != measuredPath {
		return false
	}
	return measuredHost == "" || pubHost == measuredHost
}

func publishedSEOURLs(publish *SEOPublishManifest) []string {
	if publish == nil {
		return nil
	}
	var out []string
	for _, page := range publish.Pages {
		if page.Canonical != "" {
			out = append(out, page.Canonical)
		}
	}
	return normalizeStrings(out)
}

func mergeSEOMeasurementRows(dst, src []SEOMeasurementRow) []SEOMeasurementRow {
	for _, incoming := range src {
		matched := false
		for i := range dst {
			// Analytics is page-scoped, not query-scoped. Merge only into an
			// existing page-only row; attaching the same sessions to every Search
			// Console query row would multiply first-party outcomes.
			if dst[i].Query == "" && (dst[i].Page == incoming.Page || strings.HasSuffix(dst[i].Page, incoming.Page)) {
				dst[i].Metrics.Sessions += incoming.Metrics.Sessions
				dst[i].Metrics.EngagedSessions += incoming.Metrics.EngagedSessions
				dst[i].Metrics.KeyEvents += incoming.Metrics.KeyEvents
				dst[i].Metrics.Revenue += incoming.Metrics.Revenue
				matched = true
				break
			}
		}
		if !matched {
			dst = append(dst, incoming)
		}
	}
	return dst
}

func valueAtDimension(in []struct {
	Value string `json:"value"`
}, i int) string {
	if i < len(in) {
		return in[i].Value
	}
	return ""
}

func valueAtMetric(in []struct {
	Value string `json:"value"`
}, i int) string {
	if i < len(in) {
		return in[i].Value
	}
	return ""
}

func parseSEOFloat(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
