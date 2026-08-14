package gtm

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// landingFeed fetches a competitor's OWN homepage (and /pricing) and extracts
// the H1 and a visible price — the source of truth for the corpus's H1/pricing
// claims, stronger than SERP snippets. Free of charge via direct HTTP fetch;
// it sits in the cheap tier because it makes best-effort outbound requests to
// resolved domains (opt-in via --paid), and upgrades to a scrape provider
// (SCRAPE_API_URL + SCRAPE_API_KEY, e.g. ScraperAPI/ScrapingBee) for JS-heavy
// sites. Homepage resolution: Serper top result when SERPER_API_KEY is set,
// else slug-TLD guessing. A relevance guard rejects a fetched page that doesn't
// actually mention the subject.
type landingFeed struct{ now func() time.Time }

func (landingFeed) Name() string    { return "landing" }
func (landingFeed) Tier() FeedTier  { return TierCheap }
func (landingFeed) KeyEnv() string  { return "SCRAPE_API_KEY" } // optional upgrade
func (landingFeed) Available() bool { return true }             // direct fetch always works

var landingHTTP = &http.Client{Timeout: 8 * time.Second}

func (f *landingFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	home, body, ok := f.resolveHomepage(ctx, q.Subject)
	if !ok {
		return nil, nil // best-effort: no confident homepage → no evidence (not an error)
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	if h1 := extractH1(body); h1 != "" {
		ev = append(ev, Evidence{
			ID: "landing:h1", Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   "H1: " + h1,
			Snippet: "Homepage H1 of " + extractTitle(body),
			URL:     home,
			Metric:  "h1", Value: h1,
		})
	}
	// Full (static) homepage text so body-stat claims can be checked against it.
	if page := cleanText(body); page != "" {
		if len(page) > 4000 {
			page = page[:4000]
		}
		ev = append(ev, Evidence{
			ID: "landing:page", Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   "Homepage text",
			Snippet: page,
			URL:     home,
			Metric:  "page",
		})
	}
	pricingURL := strings.TrimRight(home, "/") + "/pricing"
	if phtml, ok := f.fetch(ctx, pricingURL); ok {
		if price := extractPrice(phtml); price != "" {
			ev = append(ev, Evidence{
				ID: "landing:pricing", Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
				Title:   "Pricing: " + price,
				Snippet: "Visible price on the pricing page",
				URL:     pricingURL,
				Metric:  "pricing", Value: price,
			})
		}
	}
	return ev, nil
}

// resolveHomepage returns the homepage URL + body when it can confidently find a
// page that actually mentions the subject.
func (f *landingFeed) resolveHomepage(ctx context.Context, subject string) (string, string, bool) {
	var candidates []string
	if u := f.serperResolveURL(ctx, subject); u != "" {
		candidates = append(candidates, u)
	}
	if slug := slugify(subject); slug != "" {
		candidates = append(candidates, "https://"+slug+".com", "https://"+slug+".io", "https://"+slug+".dev")
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if body, ok := f.fetch(ctx, c); ok && pageRelevant(body, subject) {
			return c, body, true
		}
	}
	return "", "", false
}

// fetch retrieves a URL as text — via the scrape provider when configured
// (renders JS), else a direct best-effort GET with a browser UA.
func (f *landingFeed) fetch(ctx context.Context, target string) (string, bool) {
	if base, key := os.Getenv("SCRAPE_API_URL"), os.Getenv("SCRAPE_API_KEY"); base != "" && key != "" {
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		u := base + sep + url.Values{"api_key": {key}, "url": {target}}.Encode()
		return httpGetText(ctx, u, nil)
	}
	return httpGetText(ctx, target, map[string]string{
		"User-Agent": "Mozilla/5.0 (compatible; nicos-gtm/0.1; +https://nicos.tools)",
	})
}

// serperResolveURL uses Serper (when keyed) to find the subject's official site.
func (f *landingFeed) serperResolveURL(ctx context.Context, subject string) string {
	key := os.Getenv("SERPER_API_KEY")
	if key == "" {
		return ""
	}
	body, _ := json.Marshal(map[string]any{"q": subject + " official site", "num": 3})
	var resp struct {
		Organic []struct {
			Link string `json:"link"`
		} `json:"organic"`
	}
	headers := map[string]string{"X-API-KEY": key, "Content-Type": "application/json"}
	if err := doJSON(ctx, http.MethodPost, "https://google.serper.dev/search", headers, body, &resp); err != nil {
		return ""
	}
	if len(resp.Organic) == 0 {
		return ""
	}
	if u, err := url.Parse(resp.Organic[0].Link); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

func httpGetText(ctx context.Context, target string, headers map[string]string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", false
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := landingHTTP.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // cap at 512 KB
	return string(body), true
}

// --- HTML extraction ---

var (
	h1Re    = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	tagRe   = regexp.MustCompile(`(?s)<[^>]+>`)
	// Unicode-aware: also collapse non-breaking spaces (U+00A0 from &nbsp;) and
	// other Unicode space separators, which `\s` (ASCII-only) misses.
	wsRe    = regexp.MustCompile(`[\s\p{Zs}]+`)
	priceRe = regexp.MustCompile(`\$\s?\d[\d,]*(?:\.\d{2})?(?:\s*/\s*(?:mo|month|user|seat|yr|year))?`)
)

func extractH1(htmlStr string) string {
	m := h1Re.FindStringSubmatch(htmlStr)
	if m == nil {
		return ""
	}
	return cleanText(m[1])
}

func extractTitle(htmlStr string) string {
	m := titleRe.FindStringSubmatch(htmlStr)
	if m == nil {
		return ""
	}
	return cleanText(m[1])
}

func extractPrice(htmlStr string) string {
	return strings.TrimSpace(priceRe.FindString(cleanText(htmlStr)))
}

func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func pageRelevant(htmlStr, subject string) bool {
	t := strings.ToLower(htmlStr)
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		return false
	}
	if strings.Contains(t, s) {
		return true
	}
	tok := longestToken(s)
	return len(tok) >= 4 && strings.Contains(t, tok)
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) < 2 {
		return ""
	}
	return out
}
