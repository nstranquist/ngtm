package gtm

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const SEOSchemaVersion = 2

// SEOProjectConfig is the tracked, value-free contract for one product's SEO
// lifecycle. Credentials are resolved only from the process environment.
type SEOProjectConfig struct {
	SchemaVersion int                     `json:"schema_version" yaml:"schema_version"`
	Project       string                  `json:"project" yaml:"project"`
	Product       string                  `json:"product" yaml:"product"`
	Domain        string                  `json:"domain,omitempty" yaml:"domain,omitempty"`
	SiteURL       string                  `json:"site_url,omitempty" yaml:"site_url,omitempty"`
	ContentDir    string                  `json:"content_dir,omitempty" yaml:"content_dir,omitempty"`
	SeedKeywords  []string                `json:"seed_keywords" yaml:"seed_keywords"`
	Competitors   []string                `json:"competitors,omitempty" yaml:"competitors,omitempty"`
	BusinessTerms []string                `json:"business_terms,omitempty" yaml:"business_terms,omitempty"`
	Locales       []SEOLocale             `json:"locales" yaml:"locales"`
	Requirements  SEOEvidenceRequirements `json:"requirements" yaml:"requirements"`
	Scoring       SEOScoringWeights       `json:"scoring" yaml:"scoring"`
	Providers     SEOProviderPolicy       `json:"providers" yaml:"providers"`
	FirstParty    SEOFirstPartyConfig     `json:"first_party" yaml:"first_party"`
	Publishing    SEOPublishingPolicy     `json:"publishing" yaml:"publishing"`
}

type SEOLocale struct {
	Name         string `json:"name" yaml:"name"`
	LanguageCode string `json:"language_code" yaml:"language_code"`
	LocationCode int    `json:"location_code" yaml:"location_code"`
	Device       string `json:"device" yaml:"device"`
}

type SEOEvidenceRequirements struct {
	RequireSERP       bool    `json:"require_serp" yaml:"require_serp"`
	RequireVolume     bool    `json:"require_volume" yaml:"require_volume"`
	RequireIntent     bool    `json:"require_intent" yaml:"require_intent"`
	RequireDifficulty bool    `json:"require_difficulty" yaml:"require_difficulty"`
	RequireTrend      bool    `json:"require_trend" yaml:"require_trend"`
	RequireFirstParty bool    `json:"require_first_party" yaml:"require_first_party"`
	MinimumCoverage   float64 `json:"minimum_coverage" yaml:"minimum_coverage"`
}

type SEOScoringWeights struct {
	Demand            float64 `json:"demand" yaml:"demand"`
	Attainability     float64 `json:"attainability" yaml:"attainability"`
	Intent            float64 `json:"intent" yaml:"intent"`
	Trend             float64 `json:"trend" yaml:"trend"`
	BusinessRelevance float64 `json:"business_relevance" yaml:"business_relevance"`
	ContentGap        float64 `json:"content_gap" yaml:"content_gap"`
	FirstParty        float64 `json:"first_party" yaml:"first_party"`
}

type SEOProviderPolicy struct {
	Tier             string   `json:"tier" yaml:"tier"`
	Expand           bool     `json:"expand" yaml:"expand"`
	EnableTrends     bool     `json:"enable_trends" yaml:"enable_trends"`
	ExpansionSources []string `json:"expansion_sources,omitempty" yaml:"expansion_sources,omitempty"`
	MaxKeywords      int      `json:"max_keywords" yaml:"max_keywords"`
}

type SEOFirstPartyConfig struct {
	SearchConsoleSite string `json:"search_console_site,omitempty" yaml:"search_console_site,omitempty"`
	GA4Property       string `json:"ga4_property,omitempty" yaml:"ga4_property,omitempty"`
	LookbackDays      int    `json:"lookback_days" yaml:"lookback_days"`
	RequireInspection bool   `json:"require_inspection" yaml:"require_inspection"`
	RequirePageSpeed  bool   `json:"require_pagespeed" yaml:"require_pagespeed"`
}

type SEOPublishingPolicy struct {
	OutputDir             string `json:"output_dir,omitempty" yaml:"output_dir,omitempty"`
	CanonicalBaseURL      string `json:"canonical_base_url,omitempty" yaml:"canonical_base_url,omitempty"`
	MinimumWordCount      int    `json:"minimum_word_count" yaml:"minimum_word_count"`
	MinimumUniqueValue    int    `json:"minimum_unique_value_chars" yaml:"minimum_unique_value_chars"`
	RequireSitemap        bool   `json:"require_sitemap" yaml:"require_sitemap"`
	RequireStructuredData bool   `json:"require_structured_data" yaml:"require_structured_data"`
}

func DefaultSEOProject(project string) SEOProjectConfig {
	project = normalizeSEOProject(project)
	return SEOProjectConfig{
		SchemaVersion: SEOSchemaVersion,
		Project:       project,
		Product:       project,
		Locales: []SEOLocale{{
			Name: "us-en-desktop", LanguageCode: "en", LocationCode: 2840, Device: "desktop",
		}},
		Requirements: SEOEvidenceRequirements{
			RequireSERP: true, RequireVolume: true, MinimumCoverage: 0.80,
		},
		Scoring: SEOScoringWeights{
			Demand: 0.20, Attainability: 0.15, Intent: 0.10, Trend: 0.10,
			BusinessRelevance: 0.20, ContentGap: 0.15, FirstParty: 0.10,
		},
		Providers:  SEOProviderPolicy{Tier: "free", MaxKeywords: 100},
		FirstParty: SEOFirstPartyConfig{LookbackDays: 28},
		Publishing: SEOPublishingPolicy{
			MinimumWordCount: 300, MinimumUniqueValue: 80,
			RequireSitemap: true, RequireStructuredData: true,
		},
	}
}

func LoadSEOProjectConfig(path string) (SEOProjectConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SEOProjectConfig{}, err
	}
	var cfg SEOProjectConfig
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return SEOProjectConfig{}, fmt.Errorf("parse SEO project config: %w", err)
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return SEOProjectConfig{}, err
	}
	return cfg, nil
}

func (c *SEOProjectConfig) NormalizeAndValidate() error {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = SEOSchemaVersion
	}
	if c.SchemaVersion != SEOSchemaVersion {
		return fmt.Errorf("SEO project schema_version=%d, want %d", c.SchemaVersion, SEOSchemaVersion)
	}
	c.Project = normalizeSEOProject(c.Project)
	if c.Project == "" {
		return errors.New("SEO project is required")
	}
	c.Product = strings.TrimSpace(c.Product)
	if c.Product == "" {
		c.Product = c.Project
	}
	c.Domain = normalizeDomain(c.Domain)
	c.SiteURL = strings.TrimRight(strings.TrimSpace(c.SiteURL), "/")
	if c.SiteURL != "" {
		u, err := url.Parse(c.SiteURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return fmt.Errorf("site_url must be an absolute http(s) URL")
		}
		if c.Domain == "" {
			c.Domain = strings.ToLower(u.Hostname())
		}
	}
	c.SeedKeywords = normalizeKeywords(c.SeedKeywords)
	c.Competitors = normalizeStrings(c.Competitors)
	c.BusinessTerms = normalizeKeywords(c.BusinessTerms)
	if len(c.Locales) == 0 {
		c.Locales = DefaultSEOProject(c.Project).Locales
	}
	for i := range c.Locales {
		l := &c.Locales[i]
		l.Name = strings.TrimSpace(l.Name)
		l.LanguageCode = strings.ToLower(strings.TrimSpace(l.LanguageCode))
		l.Device = strings.ToLower(strings.TrimSpace(l.Device))
		if l.LanguageCode == "" {
			l.LanguageCode = "en"
		}
		if l.LocationCode == 0 {
			l.LocationCode = 2840
		}
		if l.Device == "" {
			l.Device = "desktop"
		}
		if l.Device != "desktop" && l.Device != "mobile" && l.Device != "tablet" {
			return fmt.Errorf("locale[%d].device must be desktop, mobile, or tablet", i)
		}
		if l.Name == "" {
			l.Name = fmt.Sprintf("%d-%s-%s", l.LocationCode, l.LanguageCode, l.Device)
		}
	}
	if c.Requirements.MinimumCoverage == 0 {
		c.Requirements.MinimumCoverage = 0.80
	}
	if c.Requirements.MinimumCoverage < 0 || c.Requirements.MinimumCoverage > 1 {
		return errors.New("requirements.minimum_coverage must be between 0 and 1")
	}
	if scoringWeightSum(c.Scoring) == 0 {
		c.Scoring = DefaultSEOProject(c.Project).Scoring
	}
	if err := c.Scoring.Validate(); err != nil {
		return err
	}
	c.Providers.Tier = strings.ToLower(strings.TrimSpace(c.Providers.Tier))
	if c.Providers.Tier == "" {
		c.Providers.Tier = "free"
	}
	if _, _, err := parseSEOTierForEngine(c.Providers.Tier); err != nil {
		return err
	}
	if c.Providers.MaxKeywords <= 0 {
		c.Providers.MaxKeywords = 100
	}
	if c.Providers.MaxKeywords > 1000 {
		return errors.New("providers.max_keywords must be <= 1000")
	}
	if c.FirstParty.LookbackDays <= 0 {
		c.FirstParty.LookbackDays = 28
	}
	if c.Publishing.MinimumWordCount <= 0 {
		c.Publishing.MinimumWordCount = 300
	}
	if c.Publishing.MinimumUniqueValue <= 0 {
		c.Publishing.MinimumUniqueValue = 80
	}
	c.Publishing.CanonicalBaseURL = strings.TrimRight(strings.TrimSpace(c.Publishing.CanonicalBaseURL), "/")
	if c.Publishing.CanonicalBaseURL == "" && c.SiteURL != "" {
		c.Publishing.CanonicalBaseURL = c.SiteURL
	}
	return nil
}

func (w SEOScoringWeights) Validate() error {
	vals := []float64{w.Demand, w.Attainability, w.Intent, w.Trend, w.BusinessRelevance, w.ContentGap, w.FirstParty}
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return errors.New("SEO scoring weights must be finite and non-negative")
		}
	}
	if scoringWeightSum(w) <= 0 {
		return errors.New("SEO scoring weights must sum to more than zero")
	}
	return nil
}

func scoringWeightSum(w SEOScoringWeights) float64 {
	return w.Demand + w.Attainability + w.Intent + w.Trend + w.BusinessRelevance + w.ContentGap + w.FirstParty
}

type SEOQueryIdentity struct {
	ID           string `json:"id" yaml:"id"`
	Keyword      string `json:"keyword" yaml:"keyword"`
	Locale       string `json:"locale" yaml:"locale"`
	LanguageCode string `json:"language_code" yaml:"language_code"`
	LocationCode int    `json:"location_code" yaml:"location_code"`
	Device       string `json:"device" yaml:"device"`
	Provider     string `json:"provider" yaml:"provider"`
	ScopeSupport string `json:"scope_support" yaml:"scope_support"`
}

type SEORanking struct {
	QueryID   string `json:"query_id" yaml:"query_id"`
	Provider  string `json:"provider" yaml:"provider"`
	Position  int    `json:"position" yaml:"position"`
	Title     string `json:"title" yaml:"title"`
	URL       string `json:"url" yaml:"url"`
	Domain    string `json:"domain" yaml:"domain"`
	Snippet   string `json:"snippet,omitempty" yaml:"snippet,omitempty"`
	Retrieved string `json:"retrieved" yaml:"retrieved"`
}

type SEOFirstPartyMetrics struct {
	Clicks          float64 `json:"clicks" yaml:"clicks"`
	Impressions     float64 `json:"impressions" yaml:"impressions"`
	CTR             float64 `json:"ctr" yaml:"ctr"`
	Position        float64 `json:"position" yaml:"position"`
	Sessions        float64 `json:"sessions" yaml:"sessions"`
	EngagedSessions float64 `json:"engaged_sessions" yaml:"engaged_sessions"`
	KeyEvents       float64 `json:"key_events" yaml:"key_events"`
	Revenue         float64 `json:"revenue" yaml:"revenue"`
}

func (m SEOFirstPartyMetrics) Present() bool {
	return m.Clicks != 0 || m.Impressions != 0 || m.CTR != 0 || m.Position != 0 ||
		m.Sessions != 0 || m.EngagedSessions != 0 || m.KeyEvents != 0 || m.Revenue != 0
}

type SEOOpportunityComponents struct {
	Demand            float64 `json:"demand" yaml:"demand"`
	Attainability     float64 `json:"attainability" yaml:"attainability"`
	Intent            float64 `json:"intent" yaml:"intent"`
	Trend             float64 `json:"trend" yaml:"trend"`
	BusinessRelevance float64 `json:"business_relevance" yaml:"business_relevance"`
	ContentGap        float64 `json:"content_gap" yaml:"content_gap"`
	FirstParty        float64 `json:"first_party" yaml:"first_party"`
}

type SEOOpportunityScore struct {
	Score      float64                  `json:"score" yaml:"score"`
	Confidence float64                  `json:"confidence" yaml:"confidence"`
	Accepted   bool                     `json:"accepted" yaml:"accepted"`
	Components SEOOpportunityComponents `json:"components" yaml:"components"`
	Reasons    []string                 `json:"reasons" yaml:"reasons"`
}

type SEOKeyword struct {
	Keyword       string               `json:"keyword" yaml:"keyword"`
	Source        string               `json:"source" yaml:"source"`
	Queries       []SEOQueryIdentity   `json:"queries" yaml:"queries"`
	Rankings      []SEORanking         `json:"rankings" yaml:"rankings"`
	SearchVolume  *float64             `json:"search_volume,omitempty" yaml:"search_volume,omitempty"`
	CPC           *float64             `json:"cpc,omitempty" yaml:"cpc,omitempty"`
	Difficulty    *float64             `json:"difficulty,omitempty" yaml:"difficulty,omitempty"`
	Intent        string               `json:"intent,omitempty" yaml:"intent,omitempty"`
	Trend         *float64             `json:"trend,omitempty" yaml:"trend,omitempty"`
	Competition   string               `json:"competition,omitempty" yaml:"competition,omitempty"`
	SERPFeatures  []string             `json:"serp_features,omitempty" yaml:"serp_features,omitempty"`
	FirstParty    SEOFirstPartyMetrics `json:"first_party" yaml:"first_party"`
	ExistingPages []SEOContentPage     `json:"existing_pages,omitempty" yaml:"existing_pages,omitempty"`
	Evidence      []Evidence           `json:"evidence" yaml:"evidence"`
	Opportunity   SEOOpportunityScore  `json:"opportunity" yaml:"opportunity"`
}

type SEOCoverage struct {
	Candidates        int     `json:"candidates" yaml:"candidates"`
	Accepted          int     `json:"accepted" yaml:"accepted"`
	LiveEvidence      float64 `json:"live_evidence" yaml:"live_evidence"`
	SERP              float64 `json:"serp" yaml:"serp"`
	Volume            float64 `json:"volume" yaml:"volume"`
	Intent            float64 `json:"intent" yaml:"intent"`
	Difficulty        float64 `json:"difficulty" yaml:"difficulty"`
	Trend             float64 `json:"trend" yaml:"trend"`
	FirstParty        float64 `json:"first_party" yaml:"first_party"`
	Content           float64 `json:"content" yaml:"content"`
	AverageScore      float64 `json:"average_score" yaml:"average_score"`
	AverageConfidence float64 `json:"average_confidence" yaml:"average_confidence"`
}

type SEOFinding struct {
	Code     string `json:"code" yaml:"code"`
	Severity string `json:"severity" yaml:"severity"`
	Message  string `json:"message" yaml:"message"`
	Keyword  string `json:"keyword,omitempty" yaml:"keyword,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
}

type SEOResearchReport struct {
	SchemaVersion int                 `json:"schema_version" yaml:"schema_version"`
	ID            string              `json:"id" yaml:"id"`
	Generated     string              `json:"generated" yaml:"generated"`
	Project       string              `json:"project" yaml:"project"`
	Product       string              `json:"product" yaml:"product"`
	ConfigDigest  string              `json:"config_digest" yaml:"config_digest"`
	Provenance    string              `json:"provenance" yaml:"provenance"`
	Providers     []string            `json:"providers" yaml:"providers"`
	Keywords      []SEOKeyword        `json:"keywords" yaml:"keywords"`
	Inventory     SEOContentInventory `json:"inventory" yaml:"inventory"`
	Coverage      SEOCoverage         `json:"coverage" yaml:"coverage"`
	Findings      []SEOFinding        `json:"findings" yaml:"findings"`
	Passed        bool                `json:"passed" yaml:"passed"`
	Artifact      *SEOArtifactRef     `json:"artifact,omitempty" yaml:"artifact,omitempty"`
}

func (r *SEOResearchReport) Sort() {
	sort.SliceStable(r.Keywords, func(i, j int) bool {
		if r.Keywords[i].Opportunity.Score == r.Keywords[j].Opportunity.Score {
			return r.Keywords[i].Keyword < r.Keywords[j].Keyword
		}
		return r.Keywords[i].Opportunity.Score > r.Keywords[j].Opportunity.Score
	})
}

func normalizeSEOProject(s string) string {
	return seoSlugify(s)
}

func seoSlugify(s string) string {
	var out strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func normalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimRight(s, "/")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "www.")
}

func normalizeKeywords(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func normalizeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func nowSEO(now func() time.Time) string {
	if now == nil {
		now = time.Now
	}
	return now().UTC().Format(time.RFC3339)
}
