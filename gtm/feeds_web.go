package gtm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// feedHTTP is the shared client for live feeds: bounded timeout, small
// exponential backoff on 5xx/transport errors. Modeled on the repo's existing
// external-API patterns (cmd/github-starred-export, .../provision/retry.go).
var feedHTTP = &http.Client{Timeout: 20 * time.Second}

func limitOr(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// doJSON issues an HTTP request with up to 3 attempts and decodes a JSON body.
func doJSON(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(150*(1<<attempt)) * time.Millisecond):
			}
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
		if err != nil {
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := feedHTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		if err := resp.Body.Close(); err != nil {
			return fmt.Errorf("close %s response: %w", method, err)
		}
		if readErr != nil {
			return fmt.Errorf("read %s response: %w", method, readErr)
		}
		if len(data) > 4<<20 {
			return fmt.Errorf("%s response exceeds 4 MiB limit", method)
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("%s %d: %s", method, resp.StatusCode, snippet(data, 200))
			continue // 5xx and 429 (rate limit) are retryable with backoff
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%s %d: %s", method, resp.StatusCode, snippet(data, 200))
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode %s: %w", rawURL, err)
			}
		}
		return nil
	}
	return lastErr
}

func snippet(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// --- Wikidata (free, no key) -------------------------------------------------
// Entity search grounds "who/what is this" — useful for company/market context.

type wikidataFeed struct{ now func() time.Time }

func (wikidataFeed) Name() string    { return "wikidata" }
func (wikidataFeed) Tier() FeedTier  { return TierFree }
func (wikidataFeed) KeyEnv() string  { return "" }
func (wikidataFeed) Available() bool { return true }

func (f *wikidataFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	u := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action":   {"wbsearchentities"},
		"search":   {q.Subject},
		"language": {"en"},
		"format":   {"json"},
		"limit":    {fmt.Sprint(limitOr(q.Limit, 5))},
	}.Encode()
	var resp struct {
		Search []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
			ConceptURI  string `json:"concepturi"`
		} `json:"search"`
	}
	if err := doJSON(ctx, http.MethodGet, u, map[string]string{"User-Agent": "nicos-gtm/0.1"}, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, s := range resp.Search {
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("wikidata:%s", s.ID), Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title:   s.Label,
			Snippet: s.Description,
			URL:     s.ConceptURI,
			Extra:   map[string]string{"rank": fmt.Sprint(i)},
		})
	}
	return ev, nil
}

// --- Serper.dev (cheap, SERPER_API_KEY) --------------------------------------
// Google SERP organic results — the ground truth for "who currently ranks".

type serperFeed struct{ now func() time.Time }

func (serperFeed) Name() string    { return "serper" }
func (serperFeed) Tier() FeedTier  { return TierCheap }
func (serperFeed) KeyEnv() string  { return "SERPER_API_KEY" }
func (serperFeed) Available() bool { return os.Getenv("SERPER_API_KEY") != "" }

func (f *serperFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	key := os.Getenv("SERPER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("SERPER_API_KEY not set")
	}
	query := q.Subject
	if len(q.Keywords) > 0 {
		query = strings.Join(q.Keywords, " ")
	}
	request := map[string]any{"q": query, "num": limitOr(q.Limit, 10)}
	if q.LanguageCode != "" {
		request["hl"] = q.LanguageCode
	}
	if country := seoCountryCode(q); country != "" {
		request["gl"] = country
	}
	if q.Device != "" {
		request["device"] = q.Device
	}
	body, _ := json.Marshal(request)
	var resp struct {
		Organic []struct {
			Title    string `json:"title"`
			Link     string `json:"link"`
			Snippet  string `json:"snippet"`
			Position int    `json:"position"`
		} `json:"organic"`
	}
	headers := map[string]string{"X-API-KEY": key, "Content-Type": "application/json"}
	endpoint := firstNonEmpty(os.Getenv("SERPER_API_URL"), "https://google.serper.dev/search")
	if err := doJSON(ctx, http.MethodPost, endpoint, headers, body, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, o := range resp.Organic {
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("serper:%d", i), Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   o.Title,
			Snippet: o.Snippet,
			URL:     o.Link,
			Metric:  "serp_rank", Value: fmt.Sprint(o.Position),
		})
	}
	return ev, nil
}

// --- Brave Search (cheap, BRAVE_API_KEY) -------------------------------------

type braveFeed struct{ now func() time.Time }

func (braveFeed) Name() string    { return "brave" }
func (braveFeed) Tier() FeedTier  { return TierCheap }
func (braveFeed) KeyEnv() string  { return "BRAVE_API_KEY" }
func (braveFeed) Available() bool { return os.Getenv("BRAVE_API_KEY") != "" }

func (f *braveFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	key := os.Getenv("BRAVE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("BRAVE_API_KEY not set")
	}
	query := q.Subject
	if len(q.Keywords) > 0 {
		query = strings.Join(q.Keywords, " ")
	}
	values := url.Values{
		"q":     {query},
		"count": {fmt.Sprint(limitOr(q.Limit, 10))},
	}
	if q.LanguageCode != "" {
		values.Set("search_lang", q.LanguageCode)
	}
	if country := seoCountryCode(q); country != "" {
		values.Set("country", strings.ToUpper(country))
	}
	u := strings.TrimRight(firstNonEmpty(os.Getenv("BRAVE_API_URL"), "https://api.search.brave.com/res/v1/web/search"), "?") + "?" + values.Encode()
	var resp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	headers := map[string]string{"X-Subscription-Token": key, "Accept": "application/json"}
	if err := doJSON(ctx, http.MethodGet, u, headers, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, rr := range resp.Web.Results {
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("brave:%d", i), Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   rr.Title,
			Snippet: rr.Description,
			URL:     rr.URL,
			Metric:  "serp_rank", Value: fmt.Sprint(i + 1),
		})
	}
	return ev, nil
}

// --- Tavily (cheap, TAVILY_API_KEY) ------------------------------------------
// LLM-oriented web search — a self-owned alternative to Serper for "who ranks /
// what's out there" grounding (you likely already hold the key). Endpoint is
// overridable via TAVILY_API_URL (tests / self-hosted proxy).

type tavilyFeed struct{ now func() time.Time }

func (tavilyFeed) Name() string    { return "tavily" }
func (tavilyFeed) Tier() FeedTier  { return TierCheap }
func (tavilyFeed) KeyEnv() string  { return "TAVILY_API_KEY" }
func (tavilyFeed) Available() bool { return os.Getenv("TAVILY_API_KEY") != "" }

func (f *tavilyFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	key := os.Getenv("TAVILY_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY not set")
	}
	query := q.Subject
	if len(q.Keywords) > 0 {
		query = strings.Join(q.Keywords, " ")
	}
	endpoint := os.Getenv("TAVILY_API_URL")
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	request := map[string]any{
		"api_key":      key,
		"query":        query,
		"search_depth": "basic",
		"max_results":  limitOr(q.Limit, 10),
	}
	if country := seoCountryCode(q); country != "" {
		request["country"] = country
	}
	body, _ := json.Marshal(request)
	var resp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if err := doJSON(ctx, http.MethodPost, endpoint, headers, body, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, r := range resp.Results {
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("tavily:%d", i), Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   r.Title,
			Snippet: r.Content,
			URL:     r.URL,
			Metric:  "serp_rank", Value: fmt.Sprint(i + 1),
		})
	}
	return ev, nil
}

// --- SearXNG (free, self-hosted: SEARXNG_URL) --------------------------------
// Your OWN metasearch instance (open-source, self-hosted via Docker) — the
// "build our own Serper" path: zero marginal cost, no third-party key. It
// aggregates Google/Bing/DuckDuckGo/Brave and returns JSON. Active only when
// SEARXNG_URL points at your instance; runs on the free tier when it does.

type searxngFeed struct{ now func() time.Time }

func (searxngFeed) Name() string   { return "searxng" }
func (searxngFeed) Tier() FeedTier { return TierFree }

// KeyEnv reports the env var that ENABLES this feed. SearXNG needs no secret —
// just SEARXNG_URL pointing at your self-hosted instance — but returning it here
// lets `ngtm feeds` print an actionable "set SEARXNG_URL" hint.
func (searxngFeed) KeyEnv() string  { return "SEARXNG_URL" }
func (searxngFeed) Available() bool { return os.Getenv("SEARXNG_URL") != "" }

func (f *searxngFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	base := strings.TrimRight(os.Getenv("SEARXNG_URL"), "/")
	if base == "" {
		return nil, fmt.Errorf("SEARXNG_URL not set (self-host SearXNG: https://github.com/searxng/searxng)")
	}
	query := q.Subject
	if len(q.Keywords) > 0 {
		query = strings.Join(q.Keywords, " ")
	}
	values := url.Values{
		"q":      {query},
		"format": {"json"},
	}
	if q.LanguageCode != "" {
		values.Set("language", q.LanguageCode)
	}
	u := base + "/search?" + values.Encode()
	var resp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
			Engine  string `json:"engine"`
		} `json:"results"`
	}
	if err := doJSON(ctx, http.MethodGet, u, map[string]string{"Accept": "application/json"}, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	limit := limitOr(q.Limit, 10)
	var ev []Evidence
	for i, r := range resp.Results {
		if i >= limit {
			break
		}
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("searxng:%d", i), Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title:   r.Title,
			Snippet: r.Content,
			URL:     r.URL,
			Metric:  "serp_rank", Value: fmt.Sprint(i + 1),
			Extra: map[string]string{"engine": r.Engine},
		})
	}
	return ev, nil
}

// --- DataForSEO (cheap, DATAFORSEO_LOGIN/PASSWORD) ---------------------------
// Keyword search volume / difficulty. Registered so it surfaces in `feeds`
// doctor with clear setup guidance; returns a useful error until wired.

type dataForSEOFeed struct{ now func() time.Time }

func (dataForSEOFeed) Name() string   { return "dataforseo" }
func (dataForSEOFeed) Tier() FeedTier { return TierCheap }
func (dataForSEOFeed) KeyEnv() string { return "DATAFORSEO_LOGIN" }
func (dataForSEOFeed) Available() bool {
	return os.Getenv("DATAFORSEO_LOGIN") != "" && os.Getenv("DATAFORSEO_PASSWORD") != ""
}

func (f *dataForSEOFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	if !f.Available() {
		return nil, fmt.Errorf("set DATAFORSEO_LOGIN and DATAFORSEO_PASSWORD (https://dataforseo.com) for keyword-volume evidence")
	}
	// Keyword Data → Google Ads search volume (live).
	kws := q.Keywords
	if len(kws) == 0 {
		kws = []string{q.Subject}
	}
	language := firstNonEmpty(q.LanguageCode, "en")
	location := q.LocationCode
	if location == 0 {
		location = 2840
	}
	body, _ := json.Marshal([]map[string]any{{"keywords": kws, "language_code": language, "location_code": location}})
	headers := map[string]string{
		"Authorization": "Basic " + basicAuth(os.Getenv("DATAFORSEO_LOGIN"), os.Getenv("DATAFORSEO_PASSWORD")),
		"Content-Type":  "application/json",
	}
	var resp struct {
		Tasks []struct {
			Result []struct {
				Keyword      string `json:"keyword"`
				SearchVolume int    `json:"search_volume"`
				Competition  string `json:"competition"`
			} `json:"result"`
		} `json:"tasks"`
	}
	endpoint := firstNonEmpty(os.Getenv("DATAFORSEO_KEYWORDS_API_URL"), "https://api.dataforseo.com/v3/keywords_data/google_ads/search_volume/live")
	if err := doJSON(ctx, http.MethodPost, endpoint, headers, body, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	i := 0
	for _, t := range resp.Tasks {
		for _, r := range t.Result {
			ev = append(ev, Evidence{
				ID: fmt.Sprintf("dataforseo:%d", i), Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
				Title:   r.Keyword,
				Snippet: fmt.Sprintf("monthly search volume %d (competition: %s)", r.SearchVolume, r.Competition),
				Metric:  "search_volume", Value: fmt.Sprint(r.SearchVolume),
				Extra: map[string]string{"competition": r.Competition},
			})
			i++
		}
	}
	return ev, nil
}

func seoCountryCode(q FeedQuery) string {
	name := strings.ToLower(strings.TrimSpace(q.Locale))
	if len(name) >= 2 {
		candidate := name[:2]
		if candidate[0] >= 'a' && candidate[0] <= 'z' && candidate[1] >= 'a' && candidate[1] <= 'z' {
			return candidate
		}
	}
	switch q.LocationCode {
	case 2840:
		return "us"
	case 2826:
		return "gb"
	case 2124:
		return "ca"
	case 2036:
		return "au"
	default:
		return ""
	}
}

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
