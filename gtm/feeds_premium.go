package gtm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// marketSizingFeed is the premium TAM/SAM/SOM data feed. It is provider-agnostic
// by design — the same pattern as the LLM provider registry: it calls a
// configurable endpoint (MARKETSIZING_API_URL) with a bearer key
// (MARKETSIZING_API_KEY) and expects a documented JSON contract:
//
//	{ "market": "...", "currency": "USD", "as_of": "2026",
//	  "tam_usd": 12000000000, "sam_usd": 3000000000, "som_usd": 150000000,
//	  "source_url": "https://..." }
//
// Statista/IBISWorld do not expose a public REST API, so they need a thin
// connector that emits this contract; a dedicated TAM-estimation API or an
// internal estimator can be pointed at directly. Without both env vars the feed
// is unavailable and TAM/SAM/SOM stays unsized (no dollar figure is invented).
type marketSizingFeed struct{ now func() time.Time }

func (marketSizingFeed) Name() string   { return "marketsizing" }
func (marketSizingFeed) Tier() FeedTier { return TierPremium }
func (marketSizingFeed) KeyEnv() string { return "MARKETSIZING_API_KEY" }
func (marketSizingFeed) Available() bool {
	return os.Getenv("MARKETSIZING_API_KEY") != "" && os.Getenv("MARKETSIZING_API_URL") != ""
}

func (f *marketSizingFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	base := os.Getenv("MARKETSIZING_API_URL")
	key := os.Getenv("MARKETSIZING_API_KEY")
	if base == "" || key == "" {
		return nil, fmt.Errorf("set MARKETSIZING_API_URL + MARKETSIZING_API_KEY for grounded TAM/SAM/SOM")
	}
	query := q.Subject
	if len(q.Keywords) > 0 {
		query = strings.Join(q.Keywords, " ")
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	u := base + sep + url.Values{"query": {query}}.Encode()
	var resp struct {
		Market    string  `json:"market"`
		Currency  string  `json:"currency"`
		AsOf      string  `json:"as_of"`
		TAMUSD    float64 `json:"tam_usd"`
		SAMUSD    float64 `json:"sam_usd"`
		SOMUSD    float64 `json:"som_usd"`
		SourceURL string  `json:"source_url"`
	}
	if err := doJSON(ctx, http.MethodGet, u, map[string]string{"Authorization": "Bearer " + key}, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	market := resp.Market
	if market == "" {
		market = q.Subject
	}
	var ev []Evidence
	add := func(scope string, v float64) {
		if v <= 0 {
			return
		}
		ev = append(ev, Evidence{
			ID: "marketsizing:" + scope, Feed: "marketsizing", Tier: TierPremium, Retrieved: ts,
			Title:   strings.ToUpper(scope) + ": " + formatUSD(v, resp.Currency),
			Snippet: fmt.Sprintf("%s market sizing (%s) for %q", strings.ToUpper(scope), defaultStr(resp.AsOf, "current"), market),
			URL:     resp.SourceURL,
			Metric:  "market_size", Value: formatUSD(v, resp.Currency),
			Extra: map[string]string{"scope": scope},
		})
	}
	add("tam", resp.TAMUSD)
	add("sam", resp.SAMUSD)
	add("som", resp.SOMUSD)
	if len(ev) == 0 {
		return nil, fmt.Errorf("marketsizing provider returned no tam/sam/som figures")
	}
	return ev, nil
}

func formatUSD(v float64, cur string) string {
	sym := "$"
	if cur != "" && cur != "USD" {
		sym = cur + " "
	}
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%s%.1fB", sym, v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%s%.1fM", sym, v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%s%.1fK", sym, v/1e3)
	default:
		return fmt.Sprintf("%s%.0f", sym, v)
	}
}

func defaultStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
