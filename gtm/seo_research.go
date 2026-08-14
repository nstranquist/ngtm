package gtm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

type SEOResearchRunOptions struct {
	Config SEOProjectConfig
	Tiers  []FeedTier
	// PriorMeasurement closes the loop: observed query/page outcomes from the
	// latest measurement artifact become a first-party scoring dimension on the
	// next research run. Nil keeps first-party evidence explicitly absent.
	PriorMeasurement *SEOMeasurementReport
	Strict           bool
	Offline          bool
	FixturePath      string
	MaxPages         int
	Now              func() time.Time
}

type SEOResearchFixture struct {
	SchemaVersion int          `json:"schema_version" yaml:"schema_version"`
	Providers     []string     `json:"providers" yaml:"providers"`
	Keywords      []SEOKeyword `json:"keywords" yaml:"keywords"`
}

func (e *Engine) RunSEOResearch(ctx context.Context, opts SEOResearchRunOptions) (*SEOResearchReport, error) {
	cfg := opts.Config
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	generated := nowSEO(opts.Now)
	configDigest, err := digestSEOValue(cfg)
	if err != nil {
		return nil, err
	}
	report := &SEOResearchReport{
		SchemaVersion: SEOSchemaVersion, Generated: generated, Project: cfg.Project,
		Product: cfg.Product, ConfigDigest: configDigest, Provenance: "live",
	}
	allowLiveInventory := !opts.Offline && strings.TrimSpace(opts.FixturePath) == ""
	report.Inventory = BuildSEOContentInventory(ctx, cfg, opts.MaxPages, opts.Now, allowLiveInventory)
	for _, crawlErr := range report.Inventory.CrawlErrors {
		report.Findings = append(report.Findings, SEOFinding{Code: "CONTENT_CRAWL_WARNING", Severity: "warning", Message: crawlErr})
	}

	if strings.TrimSpace(opts.FixturePath) != "" {
		fixture, err := loadSEOResearchFixture(opts.FixturePath)
		if err != nil {
			return nil, err
		}
		report.Provenance = "fixture"
		report.Providers = normalizeStrings(fixture.Providers)
		report.Keywords = fixture.Keywords
		for i := range report.Keywords {
			report.Keywords[i].Keyword = strings.ToLower(strings.Join(strings.Fields(report.Keywords[i].Keyword), " "))
			if report.Keywords[i].Source == "" {
				report.Keywords[i].Source = "fixture"
			}
			for j := range report.Keywords[i].Evidence {
				report.Keywords[i].Evidence[j].Synthetic = true
			}
		}
	} else if opts.Offline {
		report.Provenance = "offline"
		for _, seed := range cfg.SeedKeywords {
			report.Keywords = append(report.Keywords, SEOKeyword{Keyword: seed, Source: "seed"})
		}
	} else {
		keywords, providers, findings := e.gatherSEOKeywords(ctx, cfg, opts.Tiers, generated)
		report.Keywords, report.Providers = keywords, providers
		report.Findings = append(report.Findings, findings...)
	}
	if len(report.Keywords) == 0 {
		for _, seed := range cfg.SeedKeywords {
			report.Keywords = append(report.Keywords, SEOKeyword{Keyword: seed, Source: "seed"})
		}
	}
	for i := range report.Keywords {
		kw := &report.Keywords[i]
		if opts.PriorMeasurement != nil {
			kw.FirstParty, _ = metricsForSEOKeyword(kw.Keyword, opts.PriorMeasurement.Rows)
		}
		kw.ExistingPages = existingPagesForKeyword(kw.Keyword, report.Inventory)
		kw.Opportunity = scoreSEOOpportunity(cfg, *kw)
	}
	report.Coverage = calculateSEOCoverage(report.Keywords)
	report.Findings = append(report.Findings, evaluateSEORequirements(cfg.Requirements, report.Keywords, report.Coverage)...)
	report.Passed = !hasSEOBlockers(report.Findings)
	report.Sort()
	report.ID, err = researchReportID(report)
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (e *Engine) gatherSEOKeywords(ctx context.Context, cfg SEOProjectConfig, tiers []FeedTier, generated string) ([]SEOKeyword, []string, []SEOFinding) {
	allowed := tierSet(tiers)
	if len(tiers) == 0 {
		parsed, _, _ := parseSEOTierForEngine(cfg.Providers.Tier)
		allowed = tierSet(parsed)
	}
	byKeyword := map[string]*SEOKeyword{}
	for _, seed := range cfg.SeedKeywords {
		byKeyword[seed] = &SEOKeyword{Keyword: seed, Source: "seed"}
	}
	var findings []SEOFinding
	if cfg.Providers.Expand && allowed[TierCheap] {
		expanded, f := expandDataForSEOKeywords(ctx, cfg, cfg.SeedKeywords, generated)
		findings = append(findings, f...)
		for _, d := range expanded {
			q := newSEOQueryIdentity(d.Keyword, cfg.Locales[0], "dataforseo-labs")
			d.Evidence = scopeSEOEvidence(d.Evidence, q, generated)
			kw := byKeyword[d.Keyword]
			if kw == nil {
				kw = &SEOKeyword{Keyword: d.Keyword, Source: d.Source}
				byKeyword[d.Keyword] = kw
			}
			mergeSEOKeywordDatum(kw, d)
			kw.Queries = append(kw.Queries, q)
		}
	}
	keys := make([]string, 0, len(byKeyword))
	for k := range byKeyword {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if max := cfg.Providers.MaxKeywords; max > 0 && len(keys) > max {
		keys = keys[:max]
	}
	trendsProviderUsed := false
	if cfg.Providers.EnableTrends && len(keys) > 0 {
		trends, trendEvidence, trendFindings := fetchSEOTrends(ctx, cfg, keys, generated)
		findings = append(findings, trendFindings...)
		trendsProviderUsed = len(trends) > 0
		for keyword, trend := range trends {
			if kw := byKeyword[keyword]; kw != nil {
				kw.Trend = floatPtr(trend)
				if ev, ok := trendEvidence[keyword]; ok {
					kw.Evidence = append(kw.Evidence, ev)
				}
			}
		}
	}

	var serpFeeds []Feed
	var volumeFeed Feed
	for _, f := range e.reg.Feeds() {
		if !allowed[f.Tier()] || !f.Available() {
			continue
		}
		if f.Name() == "dataforseo" {
			volumeFeed = f
			continue
		}
		if IsSerpFeed(f.Name()) {
			serpFeeds = append(serpFeeds, f)
		}
	}
	providerSet := map[string]bool{}
	for _, f := range serpFeeds {
		providerSet[f.Name()] = true
	}
	if volumeFeed != nil {
		providerSet[volumeFeed.Name()] = true
	}
	if trendsProviderUsed {
		providerSet["google-trends-adapter"] = true
	}

	// Execute each SERP provider once per keyword/locale tuple. Query IDs and
	// evidence IDs are scoped to that tuple so ranks cannot collide or be
	// misattributed when multiple seeds are researched in one run.
	type result struct {
		keyword  string
		query    SEOQueryIdentity
		evidence []Evidence
		err      error
	}
	results := make(chan result, len(keys)*len(cfg.Locales)*maxInt(1, len(serpFeeds)))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, keyword := range keys {
		for _, locale := range cfg.Locales {
			for _, feed := range serpFeeds {
				keyword, locale, feed := keyword, locale, feed
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case sem <- struct{}{}:
					case <-ctx.Done():
						results <- result{keyword: keyword, err: ctx.Err()}
						return
					}
					defer func() { <-sem }()
					q := newSEOQueryIdentity(keyword, locale, feed.Name())
					ev, err := feed.Query(ctx, FeedQuery{Subject: keyword, Limit: 10, Locale: locale.Name, LanguageCode: locale.LanguageCode, LocationCode: locale.LocationCode, Device: locale.Device})
					if err == nil {
						ev = scopeSEOEvidence(ev, q, generated)
					}
					results <- result{keyword: keyword, query: q, evidence: ev, err: err}
				}()
			}
		}
	}
	go func() { wg.Wait(); close(results) }()
	for r := range results {
		kw := byKeyword[r.keyword]
		if kw == nil {
			continue
		}
		if r.err != nil {
			findings = append(findings, SEOFinding{Code: "SERP_QUERY_FAILED", Severity: "warning", Keyword: r.keyword, Message: r.query.Provider + ": " + r.err.Error()})
			continue
		}
		kw.Queries = append(kw.Queries, r.query)
		if r.query.ScopeSupport != "locale-language-device" {
			findings = append(findings, SEOFinding{Code: "SERP_SCOPE_PARTIAL", Severity: "warning", Keyword: r.keyword, Message: r.query.Provider + " honored " + r.query.ScopeSupport + " scope; full locale/language/device scope was requested"})
		}
		kw.Evidence = append(kw.Evidence, r.evidence...)
		for _, ev := range r.evidence {
			if ev.Metric != "serp_rank" {
				continue
			}
			pos, _ := strconv.Atoi(ev.Value)
			kw.Rankings = append(kw.Rankings, SEORanking{
				QueryID: r.query.ID, Provider: r.query.Provider, Position: pos,
				Title: ev.Title, URL: ev.URL, Domain: hostOf(ev.URL), Snippet: ev.Snippet, Retrieved: ev.Retrieved,
			})
		}
	}
	if volumeFeed != nil && len(keys) > 0 {
		locale := cfg.Locales[0]
		ev, err := volumeFeed.Query(ctx, FeedQuery{Subject: cfg.Product, Keywords: keys, Limit: len(keys), Locale: locale.Name, LanguageCode: locale.LanguageCode, LocationCode: locale.LocationCode, Device: locale.Device})
		if err != nil {
			findings = append(findings, SEOFinding{Code: "KEYWORD_VOLUME_FAILED", Severity: "warning", Message: err.Error()})
		} else {
			for _, item := range ev {
				kw := byKeyword[strings.ToLower(strings.Join(strings.Fields(item.Title), " "))]
				if kw == nil {
					continue
				}
				v, ok := parseMetricValue(item.Value)
				if ok {
					kw.SearchVolume = floatPtr(v)
				}
				kw.Competition = item.Extra["competition"]
				q := newSEOQueryIdentity(kw.Keyword, cfg.Locales[0], volumeFeed.Name())
				item = scopeSEOEvidence([]Evidence{item}, q, generated)[0]
				kw.Queries = append(kw.Queries, q)
				kw.Evidence = append(kw.Evidence, item)
			}
		}
	}
	providers := make([]string, 0, len(providerSet))
	for p := range providerSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	out := make([]SEOKeyword, 0, len(keys))
	for _, k := range keys {
		kw := byKeyword[k]
		sort.Slice(kw.Queries, func(i, j int) bool { return kw.Queries[i].ID < kw.Queries[j].ID })
		sort.Slice(kw.Rankings, func(i, j int) bool {
			if kw.Rankings[i].QueryID == kw.Rankings[j].QueryID {
				return kw.Rankings[i].Position < kw.Rankings[j].Position
			}
			return kw.Rankings[i].QueryID < kw.Rankings[j].QueryID
		})
		out = append(out, *kw)
	}
	return out, providers, findings
}

func mergeSEOKeywordDatum(kw *SEOKeyword, d seoKeywordDatum) {
	if kw.Source == "" || kw.Source == "seed" {
		kw.Source = d.Source
	}
	if d.SearchVolume != nil {
		kw.SearchVolume = d.SearchVolume
	}
	if d.CPC != nil {
		kw.CPC = d.CPC
	}
	if d.Difficulty != nil {
		kw.Difficulty = d.Difficulty
	}
	if d.Intent != "" {
		kw.Intent = d.Intent
	}
	if d.Trend != nil {
		kw.Trend = d.Trend
	}
	if d.Competition != "" {
		kw.Competition = d.Competition
	}
	kw.SERPFeatures = normalizeStrings(append(kw.SERPFeatures, d.SERPFeatures...))
	kw.Evidence = append(kw.Evidence, d.Evidence...)
}

func newSEOQueryIdentity(keyword string, locale SEOLocale, provider string) SEOQueryIdentity {
	base := strings.Join([]string{strings.ToLower(keyword), locale.Name, locale.LanguageCode, strconv.Itoa(locale.LocationCode), locale.Device, provider}, "\x00")
	d := sha256.Sum256([]byte(base))
	return SEOQueryIdentity{ID: "seoq:" + hex.EncodeToString(d[:8]), Keyword: keyword, Locale: locale.Name, LanguageCode: locale.LanguageCode, LocationCode: locale.LocationCode, Device: locale.Device, Provider: provider, ScopeSupport: seoProviderScopeSupport(provider, locale)}
}

func seoProviderScopeSupport(provider string, locale SEOLocale) string {
	switch provider {
	case "serper":
		if seoCountryCode(FeedQuery{Locale: locale.Name, LocationCode: locale.LocationCode}) != "" {
			return "locale-language-device"
		}
		return "language-device"
	case "brave":
		return "locale-language"
	case "searxng":
		return "language"
	case "tavily":
		return "locale"
	case "dataforseo-labs", "dataforseo":
		return "locale-language"
	default:
		return "unknown"
	}
}

func scopeSEOEvidence(ev []Evidence, q SEOQueryIdentity, generated string) []Evidence {
	out := make([]Evidence, 0, len(ev))
	for _, item := range ev {
		item.ID = scopedSEOEvidenceID(item.ID, q)
		if item.Retrieved == "" {
			item.Retrieved = generated
		}
		if item.Extra == nil {
			item.Extra = map[string]string{}
		}
		item.Extra["query_id"] = q.ID
		item.Extra["keyword"] = q.Keyword
		item.Extra["locale"] = q.Locale
		item.Extra["language_code"] = q.LanguageCode
		item.Extra["location_code"] = strconv.Itoa(q.LocationCode)
		item.Extra["device"] = q.Device
		out = append(out, item)
	}
	return out
}

func scopedSEOEvidenceID(id string, q SEOQueryIdentity) string {
	return q.ID + ":" + strings.ReplaceAll(id, ":", "-")
}

func scoreSEOOpportunity(cfg SEOProjectConfig, kw SEOKeyword) SEOOpportunityScore {
	w := cfg.Scoring
	sum := scoringWeightSum(w)
	if sum <= 0 {
		sum = 1
	}
	components := SEOOpportunityComponents{}
	availableWeight := 0.0
	var reasons []string
	if kw.SearchVolume != nil {
		components.Demand = clamp01(math.Log10(*kw.SearchVolume+1) / 5)
		availableWeight += w.Demand
	} else {
		reasons = append(reasons, "missing search volume")
	}
	if kw.Difficulty != nil {
		components.Attainability = clamp01(1 - *kw.Difficulty/100)
		availableWeight += w.Attainability
	} else if len(kw.Rankings) > 0 {
		components.Attainability = serpAttainability(kw.Rankings, cfg.Domain)
		availableWeight += w.Attainability * 0.5
		reasons = append(reasons, "attainability estimated from SERP because organic difficulty is missing")
	} else {
		reasons = append(reasons, "missing organic difficulty")
	}
	if kw.Intent != "" {
		components.Intent = intentScore(kw.Intent)
		availableWeight += w.Intent
	} else {
		reasons = append(reasons, "missing search intent")
	}
	if kw.Trend != nil {
		components.Trend = clamp01((*kw.Trend + 1) / 2)
		availableWeight += w.Trend
	} else {
		reasons = append(reasons, "missing trend")
	}
	components.BusinessRelevance = businessRelevance(kw.Keyword, cfg)
	availableWeight += w.BusinessRelevance
	switch len(kw.ExistingPages) {
	case 0:
		components.ContentGap = 1
	case 1:
		components.ContentGap = 0.45
		reasons = append(reasons, "one overlapping owned page")
	default:
		components.ContentGap = 0.1
		reasons = append(reasons, "possible cannibalization across owned pages")
	}
	availableWeight += w.ContentGap
	if kw.FirstParty.Present() {
		components.FirstParty = firstPartyOpportunity(kw.FirstParty)
		availableWeight += w.FirstParty
	} else {
		reasons = append(reasons, "missing first-party outcome data")
	}
	raw := components.Demand*w.Demand + components.Attainability*w.Attainability + components.Intent*w.Intent + components.Trend*w.Trend + components.BusinessRelevance*w.BusinessRelevance + components.ContentGap*w.ContentGap + components.FirstParty*w.FirstParty
	score := 100 * raw / sum
	confidence := clamp01(availableWeight / sum)
	accepted := score >= 35 && confidence >= cfg.Requirements.MinimumCoverage
	if !accepted {
		reasons = append(reasons, "below opportunity score or evidence-confidence threshold")
	}
	return SEOOpportunityScore{Score: roundSEO(score), Confidence: roundSEO(confidence), Accepted: accepted, Components: components, Reasons: reasons}
}

func calculateSEOCoverage(kws []SEOKeyword) SEOCoverage {
	c := SEOCoverage{Candidates: len(kws)}
	if len(kws) == 0 {
		return c
	}
	var live, serp, volume, intent, difficulty, trend, fp, content float64
	var score, confidence float64
	for _, kw := range kws {
		if kw.Opportunity.Accepted {
			c.Accepted++
		}
		for _, e := range kw.Evidence {
			if !e.Synthetic {
				live++
				break
			}
		}
		if len(kw.Rankings) > 0 {
			serp++
		}
		if kw.SearchVolume != nil {
			volume++
		}
		if kw.Intent != "" {
			intent++
		}
		if kw.Difficulty != nil {
			difficulty++
		}
		if kw.Trend != nil {
			trend++
		}
		if kw.FirstParty.Present() {
			fp++
		}
		if len(kw.ExistingPages) > 0 {
			content++
		}
		score += kw.Opportunity.Score
		confidence += kw.Opportunity.Confidence
	}
	n := float64(len(kws))
	c.LiveEvidence = roundSEO(live / n)
	c.SERP = roundSEO(serp / n)
	c.Volume = roundSEO(volume / n)
	c.Intent = roundSEO(intent / n)
	c.Difficulty = roundSEO(difficulty / n)
	c.Trend = roundSEO(trend / n)
	c.FirstParty = roundSEO(fp / n)
	c.Content = roundSEO(content / n)
	c.AverageScore = roundSEO(score / n)
	c.AverageConfidence = roundSEO(confidence / n)
	return c
}

func evaluateSEORequirements(req SEOEvidenceRequirements, kws []SEOKeyword, c SEOCoverage) []SEOFinding {
	var f []SEOFinding
	threshold := req.MinimumCoverage
	check := func(required bool, value float64, code, label string) {
		if required && value < threshold {
			f = append(f, SEOFinding{Code: code, Severity: "blocker", Message: fmt.Sprintf("%s coverage %.0f%% is below required %.0f%%", label, value*100, threshold*100)})
		}
	}
	check(req.RequireSERP, c.SERP, "SERP_COVERAGE_LOW", "SERP")
	check(req.RequireVolume, c.Volume, "VOLUME_COVERAGE_LOW", "volume")
	check(req.RequireIntent, c.Intent, "INTENT_COVERAGE_LOW", "intent")
	check(req.RequireDifficulty, c.Difficulty, "DIFFICULTY_COVERAGE_LOW", "difficulty")
	check(req.RequireTrend, c.Trend, "TREND_COVERAGE_LOW", "trend")
	check(req.RequireFirstParty, c.FirstParty, "FIRST_PARTY_COVERAGE_LOW", "first-party")
	if len(kws) == 0 {
		f = append(f, SEOFinding{Code: "NO_KEYWORDS", Severity: "blocker", Message: "research produced no keyword candidates"})
	}
	return f
}

func loadSEOResearchFixture(path string) (SEOResearchFixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SEOResearchFixture{}, err
	}
	var f SEOResearchFixture
	if err := yaml.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("parse SEO fixture: %w", err)
	}
	if f.SchemaVersion != 0 && f.SchemaVersion != SEOSchemaVersion {
		return f, fmt.Errorf("SEO fixture schema_version=%d want %d", f.SchemaVersion, SEOSchemaVersion)
	}
	return f, nil
}

func researchReportID(r *SEOResearchReport) (string, error) {
	clone := *r
	clone.ID = ""
	clone.Artifact = nil
	b, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	d := sha256.Sum256(b)
	return "seor:" + hex.EncodeToString(d[:12]), nil
}

func hasSEOBlockers(f []SEOFinding) bool {
	for _, x := range f {
		if x.Severity == "blocker" {
			return true
		}
	}
	return false
}
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func roundSEO(v float64) float64 { return math.Round(v*1000) / 1000 }
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func businessRelevance(keyword string, cfg SEOProjectConfig) float64 {
	terms := append([]string{strings.ToLower(cfg.Product)}, cfg.BusinessTerms...)
	best := 0.25
	for _, t := range terms {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if strings.Contains(strings.ToLower(keyword), t) {
			return 1
		}
		overlap := termOverlap(keyword, t)
		if overlap > best {
			best = overlap
		}
	}
	return clamp01(best)
}
func termOverlap(a, b string) float64 {
	sa := map[string]bool{}
	for _, w := range seoWords(a) {
		sa[w] = true
	}
	if len(sa) == 0 {
		return 0
	}
	m := 0
	n := 0
	for _, w := range seoWords(b) {
		n++
		if sa[w] {
			m++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(m) / float64(n)
}
func intentScore(s string) float64 {
	switch strings.ToLower(s) {
	case "transactional":
		return 1
	case "commercial", "commercial investigation":
		return .9
	case "informational":
		return .65
	case "navigational":
		return .35
	default:
		return .5
	}
}
func serpAttainability(r []SEORanking, domain string) float64 {
	if len(r) == 0 {
		return 0
	}
	uniq := map[string]bool{}
	owned := false
	for _, x := range r {
		uniq[x.Domain] = true
		if normalizeDomain(x.Domain) == normalizeDomain(domain) && domain != "" {
			owned = true
		}
	}
	if owned {
		return .75
	}
	return clamp01(1 - float64(len(uniq))/12)
}
func firstPartyOpportunity(m SEOFirstPartyMetrics) float64 {
	if m.Impressions <= 0 {
		return clamp01(m.KeyEvents / 10)
	}
	ctrGap := clamp01((.08 - m.CTR) / .08)
	pos := 0.0
	if m.Position >= 4 && m.Position <= 20 {
		pos = 1 - math.Abs(m.Position-10)/10
	}
	conv := clamp01(m.KeyEvents / math.Max(m.Clicks, 1) * 5)
	return clamp01(.45*ctrGap + .35*pos + .2*conv)
}

func parseSEOTierForEngine(s string) ([]FeedTier, bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "free":
		return []FeedTier{TierFree}, false, nil
	case "cheap", "paid":
		return []FeedTier{TierFree, TierCheap}, false, nil
	case "premium":
		return []FeedTier{TierFree, TierCheap, TierPremium}, false, nil
	case "all":
		return []FeedTier{TierFree, TierCheap, TierPremium}, false, nil
	case "none":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown SEO provider tier %q", s)
	}
}

func (r SEOResearchReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# SEO research — %s\n\n", r.Product)
	fmt.Fprintf(&b, "_%s · `%s` · provenance: %s · providers: %s_\n\n", r.Generated, r.ID, r.Provenance, strings.Join(r.Providers, ", "))
	fmt.Fprintf(&b, "Coverage: SERP %.0f%% · volume %.0f%% · intent %.0f%% · difficulty %.0f%% · trend %.0f%% · first-party %.0f%% · accepted %d/%d\n\n", r.Coverage.SERP*100, r.Coverage.Volume*100, r.Coverage.Intent*100, r.Coverage.Difficulty*100, r.Coverage.Trend*100, r.Coverage.FirstParty*100, r.Coverage.Accepted, r.Coverage.Candidates)
	if len(r.Findings) > 0 {
		b.WriteString("## Findings\n\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "- **%s** `%s`: %s\n", strings.ToUpper(f.Severity), f.Code, f.Message)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Opportunities\n\n")
	for _, kw := range r.Keywords {
		status := "candidate"
		if kw.Opportunity.Accepted {
			status = "accepted"
		}
		fmt.Fprintf(&b, "- **%s** — %.1f/100, confidence %.0f%%, %s", kw.Keyword, kw.Opportunity.Score, kw.Opportunity.Confidence*100, status)
		if kw.SearchVolume != nil {
			fmt.Fprintf(&b, ", volume %.0f", *kw.SearchVolume)
		}
		if kw.Difficulty != nil {
			fmt.Fprintf(&b, ", difficulty %.0f", *kw.Difficulty)
		}
		if kw.Intent != "" {
			fmt.Fprintf(&b, ", %s intent", kw.Intent)
		}
		fmt.Fprintf(&b, ", %d SERP rows, %d owned overlaps\n", len(kw.Rankings), len(kw.ExistingPages))
	}
	return b.String()
}

// normalize URL host for SERP data that may omit a scheme.
func seoURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Hostname() != "" {
		return normalizeDomain(u.Hostname())
	}
	return normalizeDomain(raw)
}
