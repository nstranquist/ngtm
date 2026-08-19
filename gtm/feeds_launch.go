package gtm

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultShowHNAPI         = "https://hn.algolia.com/api/v1/search_by_date"
	defaultProductHuntAtom   = "https://www.producthunt.com/feed"
	defaultProductHuntGraphQL = "https://api.producthunt.com/v2/api/graphql"
	productHuntTokenEnv       = "PRODUCTHUNT_API_TOKEN"
	metricLaunch           = "launch"
	feedShowHN             = "showhn"
	feedProductHunt        = "producthunt"
	launchPromoteReason    = "product-launch-radar"
	githubHostPrefix       = "https://github.com/"
)

var githubRepoPattern = regexp.MustCompile(`(?i)https?://(?:www\.)?github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)`)

var githubReservedOwners = map[string]struct{}{
	"about": {}, "account": {}, "apps": {}, "codespaces": {}, "collections": {},
	"copilot": {}, "customer-stories": {}, "enterprise": {}, "explore": {},
	"features": {}, "gist": {}, "issues": {}, "login": {}, "marketplace": {},
	"new": {}, "notifications": {}, "open-source": {}, "orgs": {},
	"organizations": {}, "pricing": {}, "pulls": {}, "readme": {}, "search": {},
	"security": {}, "settings": {}, "signup": {}, "sponsors": {}, "stars": {},
	"topics": {}, "watching": {},
}

// showHNFeed browses recent Show HN stories. It is not the mention search.
type showHNFeed struct {
	now    func() time.Time
	apiURL string
}

func (showHNFeed) Name() string    { return feedShowHN }
func (showHNFeed) Tier() FeedTier  { return TierFree }
func (showHNFeed) KeyEnv() string  { return "" }
func (showHNFeed) Available() bool { return true }

func (f *showHNFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	if !q.Browse && strings.TrimSpace(q.Subject) != "" {
		return nil, nil
	}
	base := strings.TrimRight(f.apiURL, "/")
	if base == "" {
		base = defaultShowHNAPI
	}
	u := base + "?" + url.Values{
		"tags":        {"show_hn"},
		"hitsPerPage": {fmt.Sprint(limitOr(q.Limit, 12))},
	}.Encode()
	raw, err := doGET(ctx, u, map[string]string{"User-Agent": "macos:tools.nicos.gtm:v0.5.0 (launch-radar)"})
	if err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	ev, err := ParseShowHNBrowse(raw, ts)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

type showHNBrowseResponse struct {
	Hits []showHNHit `json:"hits"`
}

type showHNHit struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	StoryText   string `json:"story_text"`
	Points      int    `json:"points"`
	NumComments int    `json:"num_comments"`
	ObjectID    string `json:"objectID"`
}

// ParseShowHNBrowse turns an Algolia Show HN payload into launch evidence.
func ParseShowHNBrowse(raw []byte, retrieved string) ([]Evidence, error) {
	var resp showHNBrowseResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode showhn: %w", err)
	}
	var ev []Evidence
	for i, h := range resp.Hits {
		if strings.TrimSpace(h.Title) == "" {
			continue
		}
		discussion := "https://news.ycombinator.com/item?id=" + h.ObjectID
		product := strings.TrimSpace(h.URL)
		item := Evidence{
			ID:        fmt.Sprintf("showhn:%s", nz(h.ObjectID, fmt.Sprint(i))),
			Feed:      feedShowHN,
			Tier:      TierFree,
			Title:     h.Title,
			Snippet:   fmt.Sprintf("%d points · %d comments on Show HN", h.Points, h.NumComments),
			URL:       discussion,
			Metric:    metricLaunch,
			Value:     fmt.Sprint(h.Points),
			Retrieved: retrieved,
			Extra: map[string]string{
				"comments":    fmt.Sprint(h.NumComments),
				"source":      feedShowHN,
				"product_url": product,
			},
		}
		if repo, ok := GitHubRepoFromText(product + " " + h.Title + " " + h.StoryText); ok {
			item.Extra["github_repo"] = repo
		}
		ev = append(ev, item)
	}
	return ev, nil
}

// productHuntFeed browses Product Hunt. GraphQL is used when
// PRODUCTHUNT_API_TOKEN is set; otherwise the official Atom feed.
type productHuntFeed struct {
	now        func() time.Time
	feedURL    string
	graphqlURL string
	token      string
}

func (productHuntFeed) Name() string   { return feedProductHunt }
func (productHuntFeed) Tier() FeedTier { return TierFree }
func (productHuntFeed) KeyEnv() string { return productHuntTokenEnv }
func (productHuntFeed) Available() bool {
	return true
}

func (f *productHuntFeed) tokenOrEnv() string {
	if strings.TrimSpace(f.token) != "" {
		return strings.TrimSpace(f.token)
	}
	return strings.TrimSpace(os.Getenv(productHuntTokenEnv))
}

func (f *productHuntFeed) Query(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	if !q.Browse && strings.TrimSpace(q.Subject) != "" {
		return nil, nil
	}
	if tok := f.tokenOrEnv(); tok != "" {
		return f.queryGraphQL(ctx, q, tok)
	}
	return f.queryAtom(ctx, q)
}

func (f *productHuntFeed) queryAtom(ctx context.Context, q FeedQuery) ([]Evidence, error) {
	u := strings.TrimSpace(f.feedURL)
	if u == "" {
		u = defaultProductHuntAtom
	}
	raw, err := doGET(ctx, u, map[string]string{"User-Agent": "macos:tools.nicos.gtm:v0.5.0 (launch-radar)"})
	if err != nil {
		return nil, err
	}
	ts := f.now().UTC().Format(time.RFC3339)
	ev, err := ParseProductHuntAtom(raw, ts)
	if err != nil {
		return nil, err
	}
	limit := limitOr(q.Limit, 12)
	if len(ev) > limit {
		ev = ev[:limit]
	}
	return ev, nil
}

const productHuntPostsQuery = `query($first:Int!,$postedAfter:DateTime){posts(first:$first,order:NEWEST,postedAfter:$postedAfter){edges{node{id name tagline votesCount commentsCount url website slug createdAt featuredAt}}}}`

func (f *productHuntFeed) queryGraphQL(ctx context.Context, q FeedQuery, token string) ([]Evidence, error) {
	endpoint := strings.TrimSpace(f.graphqlURL)
	if endpoint == "" {
		endpoint = defaultProductHuntGraphQL
	}
	limit := limitOr(q.Limit, 12)
	now := f.now()
	if now.IsZero() {
		now = time.Now()
	}
	postedAfter := now.In(radarLocation())
	postedAfter = time.Date(postedAfter.Year(), postedAfter.Month(), postedAfter.Day(), 0, 0, 0, 0, radarLocation())
	body, err := json.Marshal(map[string]any{
		"query": productHuntPostsQuery,
		"variables": map[string]any{
			"first":       limit,
			"postedAfter": postedAfter.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := doJSON(ctx, http.MethodPost, endpoint, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
		"User-Agent":    "macos:tools.nicos.gtm:v0.5.0 (launch-radar)",
	}, body, &raw); err != nil {
		return nil, err
	}
	ts := now.UTC().Format(time.RFC3339)
	ev, err := ParseProductHuntGraphQL(raw, ts)
	if err != nil {
		return nil, err
	}
	if len(ev) == 0 {
		// postedAfter can be empty early in the PH day; fall back to newest.
		body, err = json.Marshal(map[string]any{
			"query":     `query($first:Int!){posts(first:$first,order:NEWEST){edges{node{id name tagline votesCount commentsCount url website slug createdAt featuredAt}}}}`,
			"variables": map[string]any{"first": limit},
		})
		if err != nil {
			return nil, err
		}
		if err := doJSON(ctx, http.MethodPost, endpoint, map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  "application/json",
			"User-Agent":    "macos:tools.nicos.gtm:v0.5.0 (launch-radar)",
		}, body, &raw); err != nil {
			return nil, err
		}
		ev, err = ParseProductHuntGraphQL(raw, ts)
		if err != nil {
			return nil, err
		}
	}
	if len(ev) > limit {
		ev = ev[:limit]
	}
	return ev, nil
}

type productHuntGraphQLResponse struct {
	Data struct {
		Posts struct {
			Edges []struct {
				Node productHuntGraphQLPost `json:"node"`
			} `json:"edges"`
		} `json:"posts"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type productHuntGraphQLPost struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Tagline       string `json:"tagline"`
	VotesCount    int    `json:"votesCount"`
	CommentsCount int    `json:"commentsCount"`
	URL           string `json:"url"`
	Website       string `json:"website"`
	Slug          string `json:"slug"`
	CreatedAt     string `json:"createdAt"`
	FeaturedAt    string `json:"featuredAt"`
}

// ParseProductHuntGraphQL turns a PH v2 posts payload into launch evidence.
func ParseProductHuntGraphQL(raw []byte, retrieved string) ([]Evidence, error) {
	var resp productHuntGraphQLResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode producthunt graphql: %w", err)
	}
	if len(resp.Errors) > 0 && strings.TrimSpace(resp.Errors[0].Message) != "" {
		return nil, fmt.Errorf("producthunt graphql: %s", resp.Errors[0].Message)
	}
	var ev []Evidence
	for i, edge := range resp.Data.Posts.Edges {
		p := edge.Node
		title := strings.TrimSpace(p.Name)
		if title == "" {
			continue
		}
		discussion := strings.TrimSpace(p.URL)
		website := strings.TrimSpace(p.Website)
		item := Evidence{
			ID:        fmt.Sprintf("producthunt:%s", nz(p.ID, fmt.Sprint(i))),
			Feed:      feedProductHunt,
			Tier:      TierFree,
			Title:     title,
			Snippet:   strings.TrimSpace(p.Tagline),
			URL:       discussion,
			Metric:    metricLaunch,
			Value:     fmt.Sprint(p.VotesCount),
			Retrieved: retrieved,
			Extra: map[string]string{
				"source":      feedProductHunt,
				"transport":   "graphql",
				"votes":       fmt.Sprint(p.VotesCount),
				"comments":    fmt.Sprint(p.CommentsCount),
				"slug":        strings.TrimSpace(p.Slug),
				"published":   strings.TrimSpace(p.CreatedAt),
				"product_url": website,
			},
		}
		if repo, ok := GitHubRepoFromText(website + " " + discussion + " " + title + " " + p.Tagline); ok {
			item.Extra["github_repo"] = repo
		}
		ev = append(ev, item)
	}
	return ev, nil
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string      `xml:"id"`
	Title     string      `xml:"title"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
	Content   atomContent `xml:"content"`
	Author    atomAuthor  `xml:"author"`
	Links     []atomLink  `xml:"link"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Data string `xml:",innerxml"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

var productHuntRedirectPattern = regexp.MustCompile(`(?i)https://www\.producthunt\.com/r/p/(\d+)[^"<\s]*`)

func productHuntRedirects(raw []byte) map[string]string {
	out := map[string]string{}
	for _, m := range productHuntRedirectPattern.FindAllSubmatch(raw, -1) {
		if len(m) != 2 {
			continue
		}
		id := string(m[1])
		if _, exists := out[id]; exists {
			continue
		}
		out[id] = html.UnescapeString(string(m[0]))
	}
	return out
}

func productHuntPostID(id string) string {
	const marker = "Post/"
	if i := strings.LastIndex(id, marker); i >= 0 {
		return strings.TrimSpace(id[i+len(marker):])
	}
	return ""
}

// ParseProductHuntAtom turns the official PH Atom feed into launch evidence.
func ParseProductHuntAtom(raw []byte, retrieved string) ([]Evidence, error) {
	var feed atomFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("decode producthunt atom: %w", err)
	}
	redirects := productHuntRedirects(raw)
	var ev []Evidence
	for i, e := range feed.Entries {
		title := strings.TrimSpace(e.Title)
		if title == "" {
			continue
		}
		discussion := atomAlternate(e.Links)
		tagline := firstHTMLParagraph(e.Content.Data)
		item := Evidence{
			ID:        fmt.Sprintf("producthunt:%s", nz(e.ID, fmt.Sprint(i))),
			Feed:      feedProductHunt,
			Tier:      TierFree,
			Title:     title,
			Snippet:   tagline,
			URL:       discussion,
			Metric:    metricLaunch,
			Retrieved: retrieved,
			Extra: map[string]string{
				"source": feedProductHunt,
				"author": strings.TrimSpace(e.Author.Name),
			},
		}
		if tagline != "" {
			item.Extra["tagline"] = tagline
		}
		if published := strings.TrimSpace(e.Published); published != "" {
			item.Extra["published"] = published
		}
		if updated := strings.TrimSpace(e.Updated); updated != "" {
			item.Extra["updated"] = updated
		}
		if product := redirects[productHuntPostID(e.ID)]; product != "" && product != discussion {
			item.Extra["product_url"] = product
		} else if product := atomRedirectOrFirst(e.Content.Data, e.Links); product != "" && product != discussion {
			item.Extra["product_url"] = product
		}
		if repo, ok := GitHubRepoFromText(e.Content.Data + " " + discussion + " " + title); ok {
			item.Extra["github_repo"] = repo
		}
		ev = append(ev, item)
	}
	return ev, nil
}

func atomRedirectOrFirst(content string, links []atomLink) string {
	content = html.UnescapeString(content)
	lower := strings.ToLower(content)
	const marker = "https://www.producthunt.com/r/"
	if i := strings.Index(lower, marker); i >= 0 {
		rest := content[i:]
		end := strings.IndexAny(rest, `"< \t`)
		if end < 0 {
			end = len(rest)
		}
		href := strings.TrimSpace(html.UnescapeString(rest[:end]))
		if strings.HasPrefix(strings.ToLower(href), marker) {
			return href
		}
	}
	return atomAlternate(links)
}

func atomAlternate(links []atomLink) string {
	var first string
	for _, l := range links {
		if first == "" && strings.TrimSpace(l.Href) != "" {
			first = l.Href
		}
		if l.Rel == "alternate" && strings.TrimSpace(l.Href) != "" {
			return l.Href
		}
	}
	return first
}

func firstHTMLParagraph(raw string) string {
	s := html.UnescapeString(raw)
	s = strings.ReplaceAll(s, "\n", " ")
	lower := strings.ToLower(s)
	start := strings.Index(lower, "<p>")
	if start < 0 {
		return strings.TrimSpace(stripTags(s))
	}
	rest := s[start+3:]
	end := strings.Index(strings.ToLower(rest), "</p>")
	if end < 0 {
		return strings.TrimSpace(stripTags(rest))
	}
	return strings.TrimSpace(stripTags(rest[:end]))
}

var tagPattern = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	return strings.Join(strings.Fields(tagPattern.ReplaceAllString(s, " ")), " ")
}

// GitHubRepoFromText returns owner/name when a github.com repo URL is present.
func GitHubRepoFromText(text string) (string, bool) {
	m := githubRepoPattern.FindStringSubmatch(text)
	if len(m) != 3 {
		return "", false
	}
	owner := strings.TrimSuffix(m[1], ".git")
	name := strings.TrimSuffix(m[2], ".git")
	if owner == "" || name == "" || reservedGitHubOwner(owner) {
		return "", false
	}
	return owner + "/" + name, true
}

func reservedGitHubOwner(owner string) bool {
	_, ok := githubReservedOwners[strings.ToLower(strings.TrimSpace(owner))]
	return ok
}

// LaunchPromotePlan is the dry-run cref handoff for a launch item.
type LaunchPromotePlan struct {
	Status    string   `json:"status"`
	Repo      string   `json:"repo"`
	CloneSlug string   `json:"clone_slug"`
	Commands  []string `json:"commands"`
	Reason    string   `json:"reason"`
	SourceURL string   `json:"source_url,omitempty"`
}

// PromoteGitHubURL builds a refs cref command. Non-GitHub URLs fail closed.
func PromoteGitHubURL(raw string) (LaunchPromotePlan, error) {
	text := strings.TrimSpace(raw)
	repo, ok := GitHubRepoFromText(text)
	if !ok {
		if owner, name, splitOK := splitOwnerName(text); splitOK {
			repo = owner + "/" + name
			ok = true
		}
	}
	if !ok {
		return LaunchPromotePlan{}, fmt.Errorf("not a github.com owner/name URL: %q", raw)
	}
	_, name, _ := splitOwnerName(repo)
	cref := fmt.Sprintf("ndev refs cref %s%s --note %s",
		githubHostPrefix, repo, shellSingleQuote(launchPromoteReason))
	return LaunchPromotePlan{
		Status:    "ok",
		Repo:      repo,
		CloneSlug: name,
		Commands:  []string{cref},
		Reason:    launchPromoteReason,
		SourceURL: githubHostPrefix + repo,
	}, nil
}

func splitOwnerName(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, githubHostPrefix)
	s = strings.TrimPrefix(s, "http://github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, name := parts[0], parts[1]
	if owner == "" || name == "" || strings.Contains(owner, " ") || strings.Contains(name, " ") {
		return "", "", false
	}
	if reservedGitHubOwner(owner) {
		return "", "", false
	}
	return owner, name, true
}

func shellSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func nz(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
