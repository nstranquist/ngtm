package gtm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultGitHubSearchAPI = "https://api.github.com/search/repositories"
)

// GitHubHydrator looks up a high-confidence owner/name for a launch item.
type GitHubHydrator struct {
	SearchURL string
	Token     string
}

func githubToken() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

func (h GitHubHydrator) tokenOrEnv() string {
	if strings.TrimSpace(h.Token) != "" {
		return strings.TrimSpace(h.Token)
	}
	return githubToken()
}

func (h GitHubHydrator) searchURL() string {
	if strings.TrimSpace(h.SearchURL) != "" {
		return strings.TrimSpace(h.SearchURL)
	}
	return defaultGitHubSearchAPI
}

type githubSearchHit struct {
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Homepage    string `json:"homepage"`
	Stars       int    `json:"stargazers_count"`
}

// HydrateGitHubRepo returns owner/name when search has one confident match.
// It does not clone. Ambiguous or weak hits fail closed.
func (h GitHubHydrator) HydrateGitHubRepo(ctx context.Context, title, website string) (string, error) {
	if repo, ok := GitHubRepoFromText(website + " " + title); ok {
		return repo, nil
	}
	query := githubSearchQuery(title)
	if query == "" {
		return "", fmt.Errorf("no searchable product name")
	}
	u := h.searchURL() + "?" + url.Values{
		"q":        {query + " in:name"},
		"per_page": {"5"},
	}.Encode()
	headers := map[string]string{
		"Accept":     "application/vnd.github+json",
		"User-Agent": "macos:tools.nicos.gtm:v0.5.0 (launch-radar)",
	}
	if tok := h.tokenOrEnv(); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	var resp struct {
		Items []githubSearchHit `json:"items"`
	}
	if err := doJSON(ctx, http.MethodGet, u, headers, nil, &resp); err != nil {
		return "", err
	}
	want := normalizeProductName(title)
	var hits []githubSearchHit
	for _, it := range resp.Items {
		if !githubNameMatches(want, it.Name) {
			continue
		}
		hits = append(hits, it)
	}
	if len(hits) != 1 {
		return "", fmt.Errorf("github search ambiguous or empty for %q", title)
	}
	repo, ok := GitHubRepoFromText(hits[0].HTMLURL + " " + hits[0].FullName)
	if !ok {
		return "", fmt.Errorf("github search hit is not owner/name: %q", hits[0].FullName)
	}
	return repo, nil
}

// HydrateEvidence fills Extra.github_repo when missing. Existing repos stay.
func (h GitHubHydrator) HydrateEvidence(ctx context.Context, ev []Evidence) (hydrated int, warnings []string) {
	for i := range ev {
		if ev[i].Extra == nil {
			ev[i].Extra = map[string]string{}
		}
		if strings.TrimSpace(ev[i].Extra["github_repo"]) != "" {
			continue
		}
		website := ev[i].Extra["product_url"]
		repo, err := h.HydrateGitHubRepo(ctx, ev[i].Title, website+" "+ev[i].URL+" "+ev[i].Snippet)
		if err != nil {
			warnings = append(warnings, ev[i].Title+": "+err.Error())
			continue
		}
		ev[i].Extra["github_repo"] = repo
		ev[i].Extra["github_source"] = "search"
		hydrated++
	}
	return hydrated, warnings
}

func githubSearchQuery(title string) string {
	s := strings.TrimSpace(title)
	s = strings.TrimPrefix(s, "Show HN:")
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "–—:-|"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func normalizeProductName(s string) string {
	s = strings.ToLower(githubSearchQuery(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func githubNameMatches(want, repoName string) bool {
	got := normalizeProductName(repoName)
	return want != "" && got != "" && (want == got || strings.Contains(want, got) || strings.Contains(got, want))
}
