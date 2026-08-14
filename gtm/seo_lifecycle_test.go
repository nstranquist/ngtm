package gtm

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSEOProjectConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seo.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 2\nproject: test\nunknown_secret: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSEOProjectConfig(path); err == nil || !strings.Contains(err.Error(), "unknown_secret") {
		t.Fatalf("expected strict unknown-field error, got %v", err)
	}
}

func TestSEOProjectConfigRejectsUnknownProviderTier(t *testing.T) {
	cfg := DefaultSEOProject("test")
	cfg.Providers.Tier = "mystery"
	if err := cfg.NormalizeAndValidate(); err == nil || !strings.Contains(err.Error(), "unknown SEO provider tier") {
		t.Fatalf("invalid tier error = %v", err)
	}
}

func TestSEOQueryIdentityScopesLocaleDeviceProvider(t *testing.T) {
	a := newSEOQueryIdentity("seo tools", SEOLocale{Name: "us", LanguageCode: "en", LocationCode: 2840, Device: "desktop"}, "serper")
	b := newSEOQueryIdentity("seo tools", SEOLocale{Name: "us-mobile", LanguageCode: "en", LocationCode: 2840, Device: "mobile"}, "serper")
	c := newSEOQueryIdentity("seo tools", SEOLocale{Name: "us", LanguageCode: "en", LocationCode: 2840, Device: "desktop"}, "brave")
	if a.ID == b.ID || a.ID == c.ID || b.ID == c.ID {
		t.Fatalf("query identity collision: %#v %#v %#v", a, b, c)
	}
}

func TestSEOStoreDetectsTampering(t *testing.T) {
	store, err := NewSEOStore(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.WriteArtifact("research", map[string]string{"value": "original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.Path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if _, err := store.LoadLatest("research", &out); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestSEOStoreRejectsMalformedContentAddress(t *testing.T) {
	store, err := NewSEOStore(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if _, err := store.LoadRef("sha256:../../outside", "research", &out); err == nil || !strings.Contains(err.Error(), "invalid SEO artifact") {
		t.Fatalf("malformed artifact ID error = %v", err)
	}
	if err := verifySEOArtifactRef(SEOArtifactRef{Kind: "research", ID: "sha256:" + strings.Repeat("a", 64), Digest: "sha256:" + strings.Repeat("b", 64), Path: "/tmp/a"}); err == nil {
		t.Fatal("mismatched artifact ID and digest must be rejected")
	}
}

func TestSEOBriefAndMeasurementIDsCoverMaterialContent(t *testing.T) {
	cfg := testSEOConfig()
	research := testSEOResearch(t, cfg)
	unique := strings.Repeat("original evidence ", 8)
	a, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: unique, Audience: "operators"}, func() string { return "2026-07-14T00:00:00Z" })
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: unique, Audience: "founders"}, func() string { return "2026-07-14T00:00:00Z" })
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("brief identity ignored material audience content")
	}
	m1 := &SEOMeasurementReport{SchemaVersion: SEOSchemaVersion, Generated: "2026-07-14T00:00:00Z", Project: cfg.Project, Rows: []SEOMeasurementRow{{Page: "https://example.com/a/", Metrics: SEOFirstPartyMetrics{Clicks: 1}}}}
	m2 := &SEOMeasurementReport{SchemaVersion: SEOSchemaVersion, Generated: m1.Generated, Project: cfg.Project, Rows: []SEOMeasurementRow{{Page: "https://example.com/a/", Metrics: SEOFirstPartyMetrics{Clicks: 2}}}}
	finalizeSEOMeasurement(m1, nil, cfg)
	finalizeSEOMeasurement(m2, nil, cfg)
	if m1.ID == m2.ID {
		t.Fatal("measurement identity ignored measured values")
	}
}

func TestSEOBriefAndPublishFailClosedThenPass(t *testing.T) {
	cfg := testSEOConfig()
	research := testSEOResearch(t, cfg)
	blocked, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: "too short"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Passed {
		t.Fatal("short unique value must block the brief")
	}
	unique := "An original benchmark and reproducible workflow connect every claim to scoped evidence and observed outcomes for operators."
	brief, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: unique}, nil)
	if err != nil || !brief.Passed {
		t.Fatalf("brief should pass: err=%v findings=%v", err, brief.Findings)
	}
	draft, err := PublishSEOPage(cfg, *brief, SEOPublishRequest{Body: "short draft", Approved: false, Index: false}, filepath.Join(t.TempDir(), "draft"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Pages[0].Indexable || draft.Pages[0].Robots != "noindex, nofollow" {
		t.Fatalf("draft escaped noindex: %+v", draft.Pages[0])
	}
	if draft.Passed {
		t.Fatal("thin draft should remain blocked even though it is safely noindex")
	}
	body := strings.Repeat("Original evidence explains the workflow, tradeoffs, implementation details, and useful next actions. ", 24)
	approved, err := PublishSEOPage(cfg, *brief, SEOPublishRequest{Body: body, Approved: true, Index: true}, filepath.Join(t.TempDir(), "approved"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Passed || !approved.Pages[0].Indexable || approved.Pages[0].Robots != "index, follow" || approved.SitemapPath == "" {
		t.Fatalf("approved page did not pass: %+v", approved)
	}
	html, err := os.ReadFile(approved.Pages[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="robots" content="index, follow"`, `rel="canonical"`, `application/ld+json`, `ngtm-seo-evidence`} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("published HTML missing %q", want)
		}
	}
}

func TestSEOPublishRejectsOwnedSlugCollision(t *testing.T) {
	cfg := testSEOConfig()
	research := testSEOResearch(t, cfg)
	brief, err := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: strings.Repeat("original evidence ", 8)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	brief.ExistingPages = []SEOContentPage{{Path: "/content/seo-automation-pipeline.md"}}
	manifest, err := PublishSEOPage(cfg, *brief, SEOPublishRequest{Body: strings.Repeat("useful original workflow evidence ", 60), Approved: true, Index: true}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Pages[0].Indexable || !hasSEOFindingCode(manifest.Findings, "PUBLISH_SLUG_DUPLICATE") {
		t.Fatalf("slug collision escaped gate: %+v", manifest)
	}
}

func TestSEOMeasurementFixtureCoverageAndRetro(t *testing.T) {
	cfg := testSEOConfig()
	research := testSEOResearch(t, cfg)
	brief, _ := BuildSEOBrief(cfg, *research, SEOBriefRequest{Keyword: cfg.SeedKeywords[0], UniqueValue: strings.Repeat("original evidence ", 8)}, nil)
	publish, err := PublishSEOPage(cfg, *brief, SEOPublishRequest{Body: strings.Repeat("useful original workflow evidence ", 60), Approved: true, Index: true}, filepath.Join(t.TempDir(), "site"), nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "measurement.json")
	pageURL := publish.Pages[0].Canonical
	b, _ := json.Marshal(map[string]any{"rows": []any{map[string]any{"query": cfg.SeedKeywords[0], "page": pageURL, "metrics": map[string]any{"clicks": 10, "impressions": 100, "ctr": .1, "position": 5, "key_events": 2}}}})
	if err := os.WriteFile(fixture, b, 0o600); err != nil {
		t.Fatal(err)
	}
	measure, err := MeasureSEO(context.Background(), cfg, research, publish, SEOMeasurementOptions{FixturePath: fixture})
	if err != nil {
		t.Fatal(err)
	}
	if measure.Coverage != 1 || !measure.Passed || measure.Provenance != "fixture" {
		t.Fatalf("bad measurement: %+v page=%s", measure, pageURL)
	}
	retro := BuildSEORetro(*research, *measure, nil)
	if !retro.Passed || retro.Decisions[0].Decision != "double-down" {
		t.Fatalf("bad retro: %+v", retro)
	}
}

func TestSEOMeasurementCoverageRejectsEmptyPagesAndRequiredGaps(t *testing.T) {
	cfg := testSEOConfig()
	cfg.Requirements.RequireFirstParty = true
	report := &SEOMeasurementReport{
		SchemaVersion: SEOSchemaVersion, Generated: "2026-07-14T00:00:00Z", Project: cfg.Project,
		Rows: []SEOMeasurementRow{{Page: "", Metrics: SEOFirstPartyMetrics{Impressions: 100}}},
	}
	publish := &SEOPublishManifest{Pages: []SEOPublishedPage{{Canonical: "https://example.com/seo-automation-pipeline/"}}}
	finalizeSEOMeasurement(report, publish, cfg)
	if report.Coverage != 0 || report.Passed || !hasSEOFindingCode(report.Findings, "MEASUREMENT_COVERAGE_LOW") {
		t.Fatalf("empty page falsely satisfied measurement coverage: %+v", report)
	}
}

func TestSEOMeasurementRequiredTechnicalChecksNeedPublishedURLs(t *testing.T) {
	cfg := testSEOConfig()
	cfg.FirstParty.RequireInspection = true
	cfg.FirstParty.RequirePageSpeed = true
	report, err := MeasureSEO(context.Background(), cfg, nil, nil, SEOMeasurementOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"URL_INSPECTION_NO_PAGES", "PAGESPEED_NO_PAGES"} {
		if !hasSEOFindingCode(report.Findings, code) {
			t.Fatalf("required technical check missing %s: %+v", code, report.Findings)
		}
	}
}

func TestSEOMeasurementOfficialAdapters(t *testing.T) {
	cfg := testSEOConfig()
	cfg.FirstParty.SearchConsoleSite = "sc-domain:example.com"
	cfg.FirstParty.GA4Property = "1234"
	cfg.FirstParty.RequireInspection = true
	cfg.FirstParty.RequirePageSpeed = true
	pageURL := "https://example.com/seo-automation-pipeline/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" && !strings.Contains(r.URL.Path, "runPagespeed") {
			t.Errorf("missing bearer token on %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "searchAnalytics/query"):
			_, _ = w.Write([]byte(`{"rows":[{"keys":["seo automation pipeline","https://example.com/seo-automation-pipeline/","2026-07-13","DESKTOP","usa"],"clicks":4,"impressions":100,"ctr":0.04,"position":8}]}`))
		case strings.Contains(r.URL.Path, "runReport"):
			_, _ = w.Write([]byte(`{"rows":[{"dimensionValues":[{"value":"https://example.com/seo-automation-pipeline/"}],"metricValues":[{"value":"9"},{"value":"7"},{"value":"2"},{"value":"30"}]}]}`))
		case strings.Contains(r.URL.Path, "urlInspection"):
			_, _ = w.Write([]byte(`{"inspectionResult":{"indexStatusResult":{"verdict":"PASS","coverageState":"Submitted and indexed","robotsTxtState":"ALLOWED","indexingState":"INDEXING_ALLOWED","pageFetchState":"SUCCESSFUL","googleCanonical":"https://example.com/seo-automation-pipeline/","userCanonical":"https://example.com/seo-automation-pipeline/"}}}`))
		case strings.Contains(r.URL.Path, "runPagespeed"):
			_, _ = w.Write([]byte(`{"lighthouseResult":{"categories":{"performance":{"score":0.9},"accessibility":{"score":1},"best-practices":{"score":0.95},"seo":{"score":1}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("GOOGLE_SEARCH_CONSOLE_ACCESS_TOKEN", "token")
	t.Setenv("GOOGLE_ANALYTICS_ACCESS_TOKEN", "token")
	t.Setenv("GSC_API_BASE_URL", server.URL)
	t.Setenv("GA4_API_BASE_URL", server.URL)
	t.Setenv("GSC_INSPECTION_API_BASE_URL", server.URL)
	t.Setenv("PAGESPEED_API_BASE_URL", server.URL+"/runPagespeed")
	publish := &SEOPublishManifest{Pages: []SEOPublishedPage{{Canonical: pageURL}}}
	report, err := MeasureSEO(context.Background(), cfg, nil, publish, SEOMeasurementOptions{Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Coverage != 1 || len(report.Rows) != 2 || len(report.Inspections) != 1 || len(report.PageSpeed) != 1 {
		t.Fatalf("typed adapters failed: %+v", report)
	}
	if report.Rows[1].Metrics.Sessions != 9 || report.Rows[1].Metrics.KeyEvents != 2 || report.Rows[0].Metrics.Sessions != 0 {
		t.Fatalf("GA4 page metrics were duplicated or lost: %+v", report.Rows)
	}
}

func TestDataForSEOExpansionParsesIntentDifficultyTrendAndLocale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tasks []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
			t.Fatal(err)
		}
		if tasks[0]["location_code"] != float64(2826) || tasks[0]["language_code"] != "en" {
			t.Errorf("locale was not propagated: %v", tasks[0])
		}
		_, _ = w.Write([]byte(`{"tasks":[{"status_code":20000,"result":[{"items":[{"keyword":"SEO Pipeline","keyword_info":{"search_volume":900,"cpc":5.2,"competition_level":"MEDIUM","monthly_searches":[{"year":2025,"month":1,"search_volume":100},{"year":2026,"month":1,"search_volume":150}]},"keyword_properties":{"keyword_difficulty":37},"search_intent_info":{"main_intent":"commercial"},"serp_info":{"item_types":["people_also_ask"]}}]}]}]}`))
	}))
	defer server.Close()
	t.Setenv("DATAFORSEO_LOGIN", "user")
	t.Setenv("DATAFORSEO_PASSWORD", "pass")
	t.Setenv("DATAFORSEO_LABS_API_URL", server.URL)
	cfg := testSEOConfig()
	cfg.Locales[0].LocationCode = 2826
	cfg.Providers.ExpansionSources = []string{"keyword_ideas"}
	got, findings := expandDataForSEOKeywords(context.Background(), cfg, cfg.SeedKeywords, "2026-07-14T00:00:00Z")
	if len(findings) != 0 || len(got) != 1 {
		t.Fatalf("expansion failed: got=%+v findings=%+v", got, findings)
	}
	if got[0].Difficulty == nil || *got[0].Difficulty != 37 || got[0].Intent != "commercial" || got[0].Trend == nil || *got[0].Trend != .5 {
		t.Fatalf("missing keyword dimensions: %+v", got[0])
	}
	if len(got[0].Evidence) < 5 || got[0].Evidence[0].Retrieved == "" {
		t.Fatalf("expanded dimensions lack evidence: %+v", got[0].Evidence)
	}
}

func TestDataForSEOVolumeUsesRequestedLocale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tasks []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
			t.Fatal(err)
		}
		if tasks[0]["location_code"] != float64(2826) || tasks[0]["language_code"] != "en" {
			t.Errorf("volume locale was not propagated: %v", tasks[0])
		}
		_, _ = w.Write([]byte(`{"tasks":[{"result":[{"keyword":"seo automation pipeline","search_volume":500,"competition":"MEDIUM"}]}]}`))
	}))
	defer server.Close()
	t.Setenv("DATAFORSEO_LOGIN", "user")
	t.Setenv("DATAFORSEO_PASSWORD", "pass")
	t.Setenv("DATAFORSEO_KEYWORDS_API_URL", server.URL)
	feed := &dataForSEOFeed{now: time.Now}
	evidence, err := feed.Query(context.Background(), FeedQuery{Keywords: []string{"seo automation pipeline"}, LanguageCode: "en", LocationCode: 2826})
	if err != nil || len(evidence) != 1 || evidence[0].Value != "500" {
		t.Fatalf("volume query failed: evidence=%+v err=%v", evidence, err)
	}
}

func TestSEOEndpointRedactsKeys(t *testing.T) {
	got := redactSEOEndpoint("https://example.test/run?url=https%3A%2F%2Fexample.com&key=super-secret")
	if strings.Contains(got, "super-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("endpoint key was not redacted: %s", got)
	}
}

func TestSEOCrawlerPinnedDialRejectsHostEscape(t *testing.T) {
	dial := pinnedSEODialContext("example.com", []net.IP{net.ParseIP("93.184.216.34")})
	if _, err := dial(context.Background(), "tcp", "attacker.example:443"); err == nil || !strings.Contains(err.Error(), "escaped validated host") {
		t.Fatalf("unexpected pinned-dial result: %v", err)
	}
}

func TestSEOTrendsAdapterAndPriorMeasurementCloseLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer trends-token" {
			t.Errorf("missing trends bearer token")
		}
		_, _ = w.Write([]byte(`{"series":[{"keyword":"seo automation pipeline","points":[{"date":"2025-01","value":40},{"date":"2026-01","value":60}]}]}`))
	}))
	defer server.Close()
	t.Setenv("GOOGLE_TRENDS_API_URL", server.URL)
	t.Setenv("GOOGLE_TRENDS_ACCESS_TOKEN", "trends-token")
	cfg := testSEOConfig()
	cfg.Providers.EnableTrends = true
	values, evidence, findings := fetchSEOTrends(context.Background(), cfg, cfg.SeedKeywords, "2026-07-14T00:00:00Z")
	if len(findings) != 0 || values[cfg.SeedKeywords[0]] != .5 || evidence[cfg.SeedKeywords[0]].Metric != "search_trend" {
		t.Fatalf("bad trends adapter: values=%v evidence=%v findings=%v", values, evidence, findings)
	}

	path := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(path, seoEvalResearchFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Options{Subject: cfg.Product, Offline: true, NoFeeds: true}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	prior := &SEOMeasurementReport{Rows: []SEOMeasurementRow{{Query: cfg.SeedKeywords[0], Page: "https://example.com/seo-automation-pipeline/", Metrics: SEOFirstPartyMetrics{Clicks: 12, Impressions: 200, KeyEvents: 3}}}}
	research, err := eng.RunSEOResearch(context.Background(), SEOResearchRunOptions{Config: cfg, FixturePath: path, Offline: true, PriorMeasurement: prior})
	if err != nil {
		t.Fatal(err)
	}
	if !research.Keywords[0].FirstParty.Present() || research.Coverage.FirstParty != 1 {
		t.Fatalf("prior measurement was not joined: %+v", research.Keywords[0])
	}
}

func TestSEOEvalEndToEnd(t *testing.T) {
	report, err := RunSEOEval(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("eval failed: %+v", report)
	}
	for _, check := range report.Checks {
		if !check.Passed {
			t.Fatalf("check failed: %+v", check)
		}
	}
}

func TestSEORetroUsesHundredPointOpportunityScale(t *testing.T) {
	research := SEOResearchReport{Project: "test", ID: "research", Keywords: []SEOKeyword{{Keyword: "low signal", Opportunity: SEOOpportunityScore{Score: 20}}}}
	measurement := SEOMeasurementReport{ID: "measurement"}
	retro := BuildSEORetro(research, measurement, func() string { return "2026-07-14T00:00:00Z" })
	if got := retro.Decisions[0].Decision; got != "retire" {
		t.Fatalf("low 0-100 score decision = %q, want retire", got)
	}
}

func TestSEOAuditRejectsInvalidSchemaEventTimestampAndIndexState(t *testing.T) {
	report := &SEOAuditReport{}
	auditSEOSchema(report, "research", 0)
	events := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(events, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	auditSEOEvents(report, events)
	cfg := testSEOConfig()
	cfg.Publishing.RequireSitemap = false
	outDir := t.TempDir()
	pagePath := filepath.Join(outDir, "seo-automation-pipeline.html")
	html := `<meta name="robots" content="index, follow"><link rel="canonical" href="https://example.com/seo-automation-pipeline/"><script type="application/ld+json">{}</script>`
	if err := os.WriteFile(pagePath, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	auditSEOPublish(report, cfg, SEOPublishManifest{OutputDir: outDir, Pages: []SEOPublishedPage{{
		Slug: "seo-automation-pipeline", Path: pagePath, Canonical: "https://example.com/seo-automation-pipeline/",
		Robots: "index, follow", Indexable: true, BriefID: "brief",
	}}}, "brief")
	for _, code := range []string{"AUDIT_SCHEMA_INVALID", "AUDIT_EVENT_TIMESTAMP_INVALID", "AUDIT_INDEXABILITY_INVALID"} {
		if !hasSEOFindingCode(report.Findings, code) {
			t.Fatalf("audit failed to report %s: %+v", code, report.Findings)
		}
	}
}

func hasSEOFindingCode(findings []SEOFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestSEOResearchOfflineDoesNotCrawlSiteURL(t *testing.T) {
	cfg := testSEOConfig()
	cfg.SiteURL = "https://example.com"
	path := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(path, seoEvalResearchFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Options{Subject: cfg.Product, Offline: true, NoFeeds: true}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := eng.RunSEOResearch(context.Background(), SEOResearchRunOptions{
		Config: cfg, Offline: true, FixturePath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inventory.Source == cfg.SiteURL {
		t.Fatal("offline/fixture research must not select site_url as inventory source")
	}
	if len(report.Inventory.Pages) != 0 {
		t.Fatalf("offline/fixture research must not populate live inventory pages: %+v", report.Inventory.Pages)
	}
}

func testSEOConfig() SEOProjectConfig {
	cfg := DefaultSEOProject("seo-test")
	cfg.Product = "SEO Test"
	cfg.Domain = "example.com"
	cfg.SiteURL = ""
	cfg.SeedKeywords = []string{"seo automation pipeline"}
	cfg.Publishing.CanonicalBaseURL = "https://example.com"
	cfg.Publishing.MinimumWordCount = 100
	return cfg
}

func testSEOResearch(t *testing.T, cfg SEOProjectConfig) *SEOResearchReport {
	t.Helper()
	path := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(path, seoEvalResearchFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Options{Subject: cfg.Product, Offline: true, NoFeeds: true}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := eng.RunSEOResearch(context.Background(), SEOResearchRunOptions{Config: cfg, FixturePath: path, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	return report
}
