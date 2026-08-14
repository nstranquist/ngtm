package gtm

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Company/market feeds for the business vertical. Free: Hacker News + Reddit
// mentions (traction proxy) and Wikidata claims (structured company facts).
// Cheap, key-gated: Crunchbase + People Data Labs (firmographics). All run
// concurrently via the shared registry, so adding them costs ~0 wall-clock.

// --- Hacker News (free, Algolia search — no key) -----------------------------

type hackerNewsFeed struct{ now func() time.Time }

func (hackerNewsFeed) Name() string    { return "hackernews" }
func (hackerNewsFeed) Tier() FeedTier  { return TierFree }
func (hackerNewsFeed) KeyEnv() string  { return "" }
func (hackerNewsFeed) Available() bool { return true }

func (f *hackerNewsFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	u := "https://hn.algolia.com/api/v1/search?" + url.Values{
		"query":       {q.Subject},
		"tags":        {"story"},
		"hitsPerPage": {fmt.Sprint(limitOr(q.Limit, 8))},
	}.Encode()
	var resp struct {
		Hits []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Points      int    `json:"points"`
			NumComments int    `json:"num_comments"`
			ObjectID    string `json:"objectID"`
		} `json:"hits"`
	}
	if err := doJSON(ctx, http.MethodGet, u, nil, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, h := range resp.Hits {
		if h.Title == "" || !mentionRelevant(h.Title, q.Subject) {
			continue // drop fuzzy false positives (e.g. "nvAlt" for "nvault")
		}
		link := h.URL
		if link == "" {
			link = "https://news.ycombinator.com/item?id=" + h.ObjectID
		}
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("hackernews:%d", i), Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title:   h.Title,
			Snippet: fmt.Sprintf("%d points · %d comments on Hacker News", h.Points, h.NumComments),
			URL:     link,
			Metric:  "mentions", Value: fmt.Sprint(h.Points),
		})
	}
	return ev, nil
}

// --- Reddit (free, public search.json — no key, UA required) -----------------

type redditFeed struct {
	now     func() time.Time
	baseURL string
}

func (redditFeed) Name() string    { return "reddit" }
func (redditFeed) Tier() FeedTier  { return TierFree }
func (redditFeed) KeyEnv() string  { return "" }
func (redditFeed) Available() bool { return true }

func (f *redditFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	base := strings.TrimRight(f.baseURL, "/")
	if base == "" {
		base = "https://www.reddit.com"
	}
	u := base + "/search.json?" + url.Values{
		"q":     {q.Subject},
		"limit": {fmt.Sprint(limitOr(q.Limit, 8))},
		"sort":  {"relevance"},
	}.Encode()
	var resp struct {
		Data struct {
			Children []struct {
				Data struct {
					Title       string `json:"title"`
					Permalink   string `json:"permalink"`
					Score       int    `json:"score"`
					NumComments int    `json:"num_comments"`
					Subreddit   string `json:"subreddit"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := doJSON(ctx, http.MethodGet, u, map[string]string{"User-Agent": "macos:tools.nicos.gtm:v0.3.0 (by /u/nstranquist)"}, nil, &resp); err != nil {
		rss, rssErr := f.queryRSS(ctx, base, q)
		if rssErr != nil {
			return nil, errors.Join(err, fmt.Errorf("reddit RSS fallback: %w", rssErr))
		}
		return rss, nil
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, c := range resp.Data.Children {
		d := c.Data
		if d.Title == "" || !mentionRelevant(d.Title, q.Subject) {
			continue
		}
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("reddit:%d", i), Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title:   d.Title,
			Snippet: fmt.Sprintf("r/%s · %d upvotes · %d comments", d.Subreddit, d.Score, d.NumComments),
			URL:     "https://www.reddit.com" + d.Permalink,
			Metric:  "mentions", Value: fmt.Sprint(d.Score),
		})
	}
	return ev, nil
}

func (f *redditFeed) queryRSS(ctx context.Context, base string, q FeedQuery) ([]Evidence, error) {
	out, err := f.queryRSSOnce(ctx, base, q)
	var rateLimit *FeedRateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfter <= 0 || rateLimit.RetryAfter > 5*time.Second {
		return out, err
	}
	timer := time.NewTimer(rateLimit.RetryAfter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return f.queryRSSOnce(ctx, base, q)
	}
}

func (f *redditFeed) queryRSSOnce(ctx context.Context, base string, q FeedQuery) (out []Evidence, err error) {
	u := base + "/search.rss?" + url.Values{
		"q": {q.Subject}, "limit": {fmt.Sprint(limitOr(q.Limit, 8))}, "sort": {"relevance"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "macos:tools.nicos.gtm:v0.3.0 (by /u/nstranquist)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := time.Duration(0)
		for _, header := range []string{"Retry-After", "X-Ratelimit-Reset"} {
			if seconds, parseErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get(header))); parseErr == nil && seconds > 0 {
				retryAfter = time.Duration(seconds) * time.Second
				break
			}
		}
		return nil, &FeedRateLimitError{Feed: "reddit", RetryAfter: retryAfter}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %d", resp.StatusCode)
	}
	var feed struct {
		Entries []struct {
			Title string `xml:"title"`
			ID    string `xml:"id"`
			Link  struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Category struct {
				Term string `xml:"term,attr"`
			} `xml:"category"`
		} `xml:"entry"`
	}
	decoder := xml.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	if err := decoder.Decode(&feed); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	for i, entry := range feed.Entries {
		if entry.Title == "" || !mentionRelevant(entry.Title, q.Subject) {
			continue
		}
		id := entry.ID
		if id == "" {
			id = fmt.Sprintf("%d", i)
		}
		out = append(out, Evidence{
			ID: "reddit-rss:" + id, Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title: entry.Title, URL: entry.Link.Href, Metric: "mentions", Value: "1",
			Snippet: fmt.Sprintf("r/%s · public RSS mention (score unavailable)", entry.Category.Term),
			Extra:   map[string]string{"transport": "rss", "score_provenance": "unavailable"},
		})
	}
	return out, nil
}

// --- Wikidata claims (free) --------------------------------------------------
// Structured firmographics: industry, inception, employees, country, website.
// Two-step: resolve the top entity, fetch its claims, then batch-resolve the
// labels of entity-valued claims so the facts are human-readable.

type wikidataClaimsFeed struct{ now func() time.Time }

func (wikidataClaimsFeed) Name() string    { return "wikidata-claims" }
func (wikidataClaimsFeed) Tier() FeedTier  { return TierFree }
func (wikidataClaimsFeed) KeyEnv() string  { return "" }
func (wikidataClaimsFeed) Available() bool { return true }

// curated property → friendly label. Order is the display order.
var wikidataProps = []struct{ pid, name string }{
	{"P31", "instance of"},
	{"P452", "industry"},
	{"P571", "inception"},
	{"P1128", "employees"},
	{"P2139", "revenue"},
	{"P17", "country"},
	{"P159", "headquarters"},
	{"P856", "official website"},
}

func (f *wikidataClaimsFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	// 1. resolve the entity id — domain-aware: prefer a software/technology
	//    entity (and any matching q.Category) over off-domain homonyms — a
	//    charity, an intergovernmental org, a journal — that wbsearchentities
	//    would otherwise rank first for an ambiguous product name ("acp",
	//    "keyring"). See wikidata_disambig.go.
	searchURL := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action": {"wbsearchentities"}, "search": {q.Subject},
		"language": {"en"}, "format": {"json"}, "limit": {"7"},
	}.Encode()
	var sresp struct {
		Search []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"search"`
	}
	ua := map[string]string{"User-Agent": "nicos-gtm/0.1"}
	if err := doJSON(ctx, http.MethodGet, searchURL, ua, nil, &sresp); err != nil {
		return nil, err
	}
	if len(sresp.Search) == 0 {
		return nil, nil
	}
	cands := make([]wikidataCandidate, 0, len(sresp.Search))
	for _, s := range sresp.Search {
		cands = append(cands, wikidataCandidate{ID: s.ID, Label: s.Label, Description: s.Description})
	}
	qid := chooseWikidataEntity(cands, q.Category)

	// 2. fetch claims for that entity.
	claimsURL := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action": {"wbgetentities"}, "ids": {qid},
		"props": {"claims"}, "format": {"json"},
	}.Encode()
	// datavalue.value varies by type (object for entity/time/quantity, bare
	// string for "string"/"external-id"), so capture it raw and decode per type.
	var cresp struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					DataValue struct {
						Type  string          `json:"type"`
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
		} `json:"entities"`
	}
	if err := doJSON(ctx, http.MethodGet, claimsURL, ua, nil, &cresp); err != nil {
		return nil, err
	}
	ent, ok := cresp.Entities[qid]
	if !ok {
		return nil, nil
	}

	// Collect display values per curated prop; gather referenced QIDs to label.
	type factVal struct {
		display string
		refQID  string
	}
	facts := map[string][]factVal{}
	refIDs := map[string]bool{}
	for _, p := range wikidataProps {
		for _, c := range ent.Claims[p.pid] {
			dv := c.Mainsnak.DataValue
			disp, ref := decodeSnakValue(dv.Type, dv.Value)
			if disp == "" && ref == "" {
				continue
			}
			if ref != "" {
				refIDs[ref] = true
			}
			facts[p.pid] = append(facts[p.pid], factVal{display: disp, refQID: ref})
		}
	}

	// 3. batch-resolve labels for entity-valued claims (bounded).
	labels := resolveLabels(ctx, ua, keysOf(refIDs, 20))

	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	i := 0
	for _, p := range wikidataProps {
		vals := facts[p.pid]
		if len(vals) == 0 {
			continue
		}
		var parts []string
		for _, v := range vals {
			if v.refQID != "" {
				if lbl, ok := labels[v.refQID]; ok && lbl != "" {
					parts = append(parts, lbl)
				} else {
					parts = append(parts, v.refQID)
				}
			} else if v.display != "" {
				parts = append(parts, v.display)
			}
		}
		if len(parts) == 0 {
			continue
		}
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("wikidata-claims:%s", p.pid), Feed: f.Name(), Tier: TierFree, Retrieved: ts,
			Title:   fmt.Sprintf("%s: %s", p.name, strings.Join(dedupe(parts), ", ")),
			Snippet: fmt.Sprintf("Wikidata %s (%s) for %s", p.name, p.pid, qid),
			URL:     "https://www.wikidata.org/wiki/" + qid,
			Metric:  "company_fact", Value: strings.Join(dedupe(parts), ", "),
		})
		i++
	}
	return ev, nil
}

// decodeSnakValue renders a Wikidata datavalue into a display string (for
// literals) or a referenced QID (for entity values, resolved to a label later).
// The raw value's JSON shape depends on the datavalue type.
func decodeSnakValue(typ string, raw json.RawMessage) (display, refQID string) {
	if len(raw) == 0 {
		return "", ""
	}
	switch typ {
	case "wikibase-entityid":
		var v struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &v)
		return "", v.ID
	case "time":
		var v struct {
			Time string `json:"time"`
		}
		_ = json.Unmarshal(raw, &v)
		t := strings.TrimPrefix(v.Time, "+") // +2010-01-01T00:00:00Z → 2010
		if len(t) >= 4 {
			return t[:4], ""
		}
		return t, ""
	case "quantity":
		var v struct {
			Amount string `json:"amount"`
		}
		_ = json.Unmarshal(raw, &v)
		return strings.TrimPrefix(v.Amount, "+"), ""
	case "monolingualtext":
		var v struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &v)
		return v.Text, ""
	case "string", "external-id":
		var s string
		_ = json.Unmarshal(raw, &s)
		return s, ""
	}
	return "", ""
}

func resolveLabels(ctx context.Context, headers map[string]string, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	u := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action": {"wbgetentities"}, "ids": {strings.Join(ids, "|")},
		"props": {"labels"}, "languages": {"en"}, "format": {"json"},
	}.Encode()
	var resp struct {
		Entities map[string]struct {
			Labels struct {
				En struct {
					Value string `json:"value"`
				} `json:"en"`
			} `json:"labels"`
		} `json:"entities"`
	}
	if err := doJSON(ctx, http.MethodGet, u, headers, nil, &resp); err != nil {
		return out
	}
	for id, e := range resp.Entities {
		out[id] = e.Labels.En.Value
	}
	return out
}

// mentionRelevant is a precision guard for keyword-match feeds (HN/Reddit):
// keep a hit only if the subject (or its most significant token) appears in the
// title. Fuzzy search engines return near-misses ("nvAlt" for "nvault") that
// would otherwise ground false "traction" — precision matters more than recall
// for a grounding tool.
func mentionRelevant(title, subject string) bool {
	t := strings.ToLower(title)
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		return true
	}
	if strings.Contains(t, s) {
		return true
	}
	tok := longestToken(s)
	return len(tok) >= 4 && strings.Contains(t, tok)
}

func longestToken(s string) string {
	best := ""
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '/'
	}) {
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

func keysOf(m map[string]bool, max int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// --- Crunchbase (cheap, CRUNCHBASE_API_KEY) ----------------------------------

type crunchbaseFeed struct{ now func() time.Time }

func (crunchbaseFeed) Name() string    { return "crunchbase" }
func (crunchbaseFeed) Tier() FeedTier  { return TierCheap }
func (crunchbaseFeed) KeyEnv() string  { return "CRUNCHBASE_API_KEY" }
func (crunchbaseFeed) Available() bool { return os.Getenv("CRUNCHBASE_API_KEY") != "" }

func (f *crunchbaseFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	key := os.Getenv("CRUNCHBASE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("set CRUNCHBASE_API_KEY (https://data.crunchbase.com) for firmographics")
	}
	body := []byte(fmt.Sprintf(`{"field_ids":["identifier","short_description","categories","num_employees_enum","founded_on"],"query":[{"type":"predicate","field_id":"identifier","operator_id":"contains","values":[%q]}],"limit":3}`, q.Subject))
	var resp struct {
		Entities []struct {
			Properties struct {
				Identifier struct {
					Value string `json:"value"`
				} `json:"identifier"`
				ShortDescription string `json:"short_description"`
				NumEmployeesEnum string `json:"num_employees_enum"`
			} `json:"properties"`
		} `json:"entities"`
	}
	headers := map[string]string{"X-cb-user-key": key, "Content-Type": "application/json"}
	if err := doJSON(ctx, http.MethodPost, "https://api.crunchbase.com/api/v4/searches/organizations", headers, body, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	var ev []Evidence
	for i, e := range resp.Entities {
		p := e.Properties
		ev = append(ev, Evidence{
			ID: fmt.Sprintf("crunchbase:%d", i), Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
			Title:   p.Identifier.Value,
			Snippet: strings.TrimSpace(p.ShortDescription + " (" + p.NumEmployeesEnum + ")"),
			Metric:  "company_fact", Value: p.NumEmployeesEnum,
		})
	}
	return ev, nil
}

// --- People Data Labs (cheap, PDL_API_KEY) -----------------------------------

type peopleDataLabsFeed struct{ now func() time.Time }

func (peopleDataLabsFeed) Name() string    { return "peopledatalabs" }
func (peopleDataLabsFeed) Tier() FeedTier  { return TierCheap }
func (peopleDataLabsFeed) KeyEnv() string  { return "PDL_API_KEY" }
func (peopleDataLabsFeed) Available() bool { return os.Getenv("PDL_API_KEY") != "" }

func (f *peopleDataLabsFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	key := os.Getenv("PDL_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("set PDL_API_KEY (https://peopledatalabs.com) for company enrichment")
	}
	u := "https://api.peopledatalabs.com/v5/company/enrich?" + url.Values{"name": {q.Subject}}.Encode()
	var resp struct {
		Industry  string `json:"industry"`
		Size      string `json:"size"`
		Founded   int    `json:"founded"`
		Summary   string `json:"summary"`
		Website   string `json:"website"`
		Employees int    `json:"employee_count"`
	}
	headers := map[string]string{"X-Api-Key": key}
	if err := doJSON(ctx, http.MethodGet, u, headers, nil, &resp); err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	desc := resp.Summary
	if desc == "" {
		desc = fmt.Sprintf("industry %s · size %s · founded %d", resp.Industry, resp.Size, resp.Founded)
	}
	return []Evidence{{
		ID: "peopledatalabs:0", Feed: f.Name(), Tier: TierCheap, Retrieved: ts,
		Title:   q.Subject,
		Snippet: desc,
		URL:     resp.Website,
		Metric:  "company_fact", Value: fmt.Sprint(resp.Employees),
	}}, nil
}
