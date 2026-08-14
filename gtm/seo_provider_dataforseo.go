package gtm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
)

type seoKeywordDatum struct {
	Keyword      string
	Source       string
	SearchVolume *float64
	CPC          *float64
	Difficulty   *float64
	Intent       string
	Trend        *float64
	Competition  string
	SERPFeatures []string
	Evidence     []Evidence
}

type dataForSEOLabsItem struct {
	Keyword     string `json:"keyword"`
	KeywordInfo struct {
		SearchVolume     *float64 `json:"search_volume"`
		CPC              *float64 `json:"cpc"`
		Competition      *float64 `json:"competition"`
		CompetitionLevel string   `json:"competition_level"`
		MonthlySearches  []struct {
			Year         int     `json:"year"`
			Month        int     `json:"month"`
			SearchVolume float64 `json:"search_volume"`
		} `json:"monthly_searches"`
	} `json:"keyword_info"`
	KeywordProperties struct {
		Difficulty *float64 `json:"keyword_difficulty"`
	} `json:"keyword_properties"`
	SearchIntentInfo struct {
		MainIntent string `json:"main_intent"`
	} `json:"search_intent_info"`
	SERPInfo struct {
		ItemTypes []string `json:"item_types"`
	} `json:"serp_info"`
}

type dataForSEOLabsResponse struct {
	Tasks []struct {
		StatusCode    int    `json:"status_code"`
		StatusMessage string `json:"status_message"`
		Result        []struct {
			Items []dataForSEOLabsItem `json:"items"`
		} `json:"result"`
	} `json:"tasks"`
}

func dataForSEOConfigured() bool {
	return strings.TrimSpace(os.Getenv("DATAFORSEO_LOGIN")) != "" && strings.TrimSpace(os.Getenv("DATAFORSEO_PASSWORD")) != ""
}

func expandDataForSEOKeywords(ctx context.Context, cfg SEOProjectConfig, seeds []string, generated string) ([]seoKeywordDatum, []SEOFinding) {
	if !dataForSEOConfigured() {
		return nil, []SEOFinding{{Code: "DATAFORSEO_UNCONFIGURED", Severity: "warning", Message: "DataForSEO Labs expansion requested but credentials are not configured"}}
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DATAFORSEO_LABS_API_URL")), "/")
	if base == "" {
		base = "https://api.dataforseo.com/v3/dataforseo_labs/google"
	}
	sources := normalizeStrings(cfg.Providers.ExpansionSources)
	if len(sources) == 0 {
		sources = []string{"keyword_ideas", "related_keywords", "keyword_suggestions"}
		if cfg.Domain != "" {
			sources = append(sources, "keywords_for_site")
		}
	}
	max := cfg.Providers.MaxKeywords
	if max <= 0 {
		max = 100
	}
	var all []seoKeywordDatum
	var findings []SEOFinding
	for _, source := range sources {
		body := dataForSEORequestBody(source, cfg, seeds, max)
		if body == nil {
			findings = append(findings, SEOFinding{Code: "EXPANSION_SOURCE_UNKNOWN", Severity: "warning", Message: "unknown DataForSEO expansion source " + source})
			continue
		}
		payload, _ := json.Marshal([]map[string]any{body})
		var resp dataForSEOLabsResponse
		headers := map[string]string{
			"Authorization": "Basic " + basicAuth(os.Getenv("DATAFORSEO_LOGIN"), os.Getenv("DATAFORSEO_PASSWORD")),
			"Content-Type":  "application/json",
		}
		endpoint := base + "/" + source + "/live"
		if err := doJSON(ctx, http.MethodPost, endpoint, headers, payload, &resp); err != nil {
			findings = append(findings, SEOFinding{Code: "DATAFORSEO_EXPANSION_FAILED", Severity: "warning", Message: source + ": " + err.Error()})
			continue
		}
		for _, task := range resp.Tasks {
			if task.StatusCode >= 40000 {
				findings = append(findings, SEOFinding{Code: "DATAFORSEO_TASK_FAILED", Severity: "warning", Message: fmt.Sprintf("%s: %d %s", source, task.StatusCode, task.StatusMessage)})
				continue
			}
			for _, result := range task.Result {
				for _, item := range result.Items {
					kw := strings.ToLower(strings.Join(strings.Fields(item.Keyword), " "))
					if kw == "" {
						continue
					}
					datum := seoKeywordDatum{
						Keyword: kw, Source: "dataforseo:" + source,
						SearchVolume: item.KeywordInfo.SearchVolume,
						CPC:          item.KeywordInfo.CPC,
						Difficulty:   item.KeywordProperties.Difficulty,
						Intent:       strings.ToLower(item.SearchIntentInfo.MainIntent),
						Trend:        trendFromMonthlySearches(item.KeywordInfo.MonthlySearches),
						Competition:  firstNonEmpty(item.KeywordInfo.CompetitionLevel, formatOptionalFloat(item.KeywordInfo.Competition)),
						SERPFeatures: normalizeStrings(item.SERPInfo.ItemTypes),
					}
					datum.Evidence = dataForSEOKeywordEvidence(datum, generated)
					all = append(all, datum)
				}
			}
		}
	}
	return mergeSEOKeywordData(all, max), findings
}

func dataForSEOKeywordEvidence(d seoKeywordDatum, generated string) []Evidence {
	base := "dataforseo-labs:" + strings.TrimPrefix(d.Source, "dataforseo:") + ":" + seoSlugify(d.Keyword)
	makeEvidence := func(metric, value string) Evidence {
		return Evidence{ID: base + ":" + metric, Feed: "dataforseo-labs", Tier: TierCheap, Title: d.Keyword, Snippet: metric + "=" + value, Metric: metric, Value: value, Retrieved: generated}
	}
	var out []Evidence
	if d.SearchVolume != nil {
		out = append(out, makeEvidence("search_volume", fmt.Sprintf("%.6g", *d.SearchVolume)))
	}
	if d.CPC != nil {
		out = append(out, makeEvidence("cpc", fmt.Sprintf("%.6g", *d.CPC)))
	}
	if d.Difficulty != nil {
		out = append(out, makeEvidence("organic_difficulty", fmt.Sprintf("%.6g", *d.Difficulty)))
	}
	if d.Intent != "" {
		out = append(out, makeEvidence("search_intent", d.Intent))
	}
	if d.Trend != nil {
		out = append(out, makeEvidence("search_trend", fmt.Sprintf("%.6g", *d.Trend)))
	}
	return out
}

func dataForSEORequestBody(source string, cfg SEOProjectConfig, seeds []string, max int) map[string]any {
	locale := cfg.Locales[0]
	base := map[string]any{
		"language_code": locale.LanguageCode, "location_code": locale.LocationCode,
		"limit": max, "include_serp_info": true,
	}
	switch source {
	case "keyword_ideas":
		base["keywords"] = seeds
	case "related_keywords", "keyword_suggestions":
		if len(seeds) == 0 {
			return nil
		}
		base["keyword"] = seeds[0]
	case "keywords_for_site":
		if cfg.Domain == "" {
			return nil
		}
		base["target"] = cfg.Domain
	default:
		return nil
	}
	return base
}

func trendFromMonthlySearches(in []struct {
	Year         int     `json:"year"`
	Month        int     `json:"month"`
	SearchVolume float64 `json:"search_volume"`
}) *float64 {
	if len(in) < 2 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].Year == in[j].Year {
			return in[i].Month < in[j].Month
		}
		return in[i].Year < in[j].Year
	})
	oldest, latest := in[0].SearchVolume, in[len(in)-1].SearchVolume
	denom := math.Max(math.Abs(oldest), 1)
	v := math.Max(-1, math.Min(1, (latest-oldest)/denom))
	return &v
}

func formatOptionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.3g", *v)
}

func mergeSEOKeywordData(in []seoKeywordDatum, limit int) []seoKeywordDatum {
	byKeyword := map[string]seoKeywordDatum{}
	for _, d := range in {
		cur, ok := byKeyword[d.Keyword]
		if !ok {
			byKeyword[d.Keyword] = d
			continue
		}
		cur.Source = strings.Trim(strings.Join(normalizeStrings([]string{cur.Source, d.Source}), ","), ",")
		if cur.SearchVolume == nil {
			cur.SearchVolume = d.SearchVolume
		}
		if cur.CPC == nil {
			cur.CPC = d.CPC
		}
		if cur.Difficulty == nil {
			cur.Difficulty = d.Difficulty
		}
		if cur.Intent == "" {
			cur.Intent = d.Intent
		}
		if cur.Trend == nil {
			cur.Trend = d.Trend
		}
		if cur.Competition == "" {
			cur.Competition = d.Competition
		}
		cur.SERPFeatures = normalizeStrings(append(cur.SERPFeatures, d.SERPFeatures...))
		cur.Evidence = append(cur.Evidence, d.Evidence...)
		byKeyword[d.Keyword] = cur
	}
	keys := make([]string, 0, len(byKeyword))
	for k := range byKeyword {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]seoKeywordDatum, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKeyword[k])
	}
	return out
}
