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

// seoTrendsResponse is the stable adapter boundary used by the lifecycle and
// fixtures. GOOGLE_TRENDS_API_URL may point at an approved Google Trends API
// proxy/adapter; raw provider schema changes remain outside the scoring model.
type seoTrendsResponse struct {
	Series []struct {
		Keyword string `json:"keyword"`
		Points  []struct {
			Date  string  `json:"date"`
			Value float64 `json:"value"`
		} `json:"points"`
	} `json:"series"`
}

func fetchSEOTrends(ctx context.Context, cfg SEOProjectConfig, keywords []string, generated string) (map[string]float64, map[string]Evidence, []SEOFinding) {
	endpoint := strings.TrimSpace(os.Getenv("GOOGLE_TRENDS_API_URL"))
	if endpoint == "" {
		return nil, nil, []SEOFinding{{Code: "TRENDS_UNCONFIGURED", Severity: "warning", Message: "providers.enable_trends is true but GOOGLE_TRENDS_API_URL is not configured"}}
	}
	locale := cfg.Locales[0]
	payload, _ := json.Marshal(map[string]any{
		"keywords": keywords, "language_code": locale.LanguageCode,
		"location_code": locale.LocationCode, "locale": locale.Name,
	})
	headers := map[string]string{"Content-Type": "application/json"}
	if token := strings.TrimSpace(os.Getenv("GOOGLE_TRENDS_ACCESS_TOKEN")); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	var response seoTrendsResponse
	if err := doJSON(ctx, http.MethodPost, endpoint, headers, payload, &response); err != nil {
		return nil, nil, []SEOFinding{{Code: "TRENDS_QUERY_FAILED", Severity: "warning", Message: err.Error()}}
	}
	values := map[string]float64{}
	evidence := map[string]Evidence{}
	for _, series := range response.Series {
		keyword := strings.ToLower(strings.Join(strings.Fields(series.Keyword), " "))
		if keyword == "" || len(series.Points) < 2 {
			continue
		}
		sort.Slice(series.Points, func(i, j int) bool { return series.Points[i].Date < series.Points[j].Date })
		oldest, newest := series.Points[0], series.Points[len(series.Points)-1]
		trend := math.Max(-1, math.Min(1, (newest.Value-oldest.Value)/math.Max(math.Abs(oldest.Value), 1)))
		values[keyword] = trend
		evidence[keyword] = Evidence{
			ID: "trends:" + seoSlugify(keyword), Feed: "google-trends-adapter", Tier: TierFree,
			Title: keyword, Snippet: fmt.Sprintf("relative interest changed %.1f%% from %s to %s", trend*100, oldest.Date, newest.Date),
			Metric: "search_trend", Value: fmt.Sprintf("%.6f", trend), Retrieved: generated,
			Extra: map[string]string{"locale": locale.Name, "language_code": locale.LanguageCode, "location_code": fmt.Sprint(locale.LocationCode)},
		}
	}
	return values, evidence, nil
}
