package gtm

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type SEOContentPage struct {
	Path          string   `json:"path,omitempty" yaml:"path,omitempty"`
	URL           string   `json:"url,omitempty" yaml:"url,omitempty"`
	Title         string   `json:"title" yaml:"title"`
	Description   string   `json:"description,omitempty" yaml:"description,omitempty"`
	Canonical     string   `json:"canonical,omitempty" yaml:"canonical,omitempty"`
	Robots        string   `json:"robots,omitempty" yaml:"robots,omitempty"`
	WordCount     int      `json:"word_count" yaml:"word_count"`
	Headings      []string `json:"headings,omitempty" yaml:"headings,omitempty"`
	InternalLinks []string `json:"internal_links,omitempty" yaml:"internal_links,omitempty"`
	Terms         []string `json:"terms,omitempty" yaml:"terms,omitempty"`
	Digest        string   `json:"digest" yaml:"digest"`
}

type SEOContentInventory struct {
	Source      string           `json:"source,omitempty" yaml:"source,omitempty"`
	Generated   string           `json:"generated,omitempty" yaml:"generated,omitempty"`
	Pages       []SEOContentPage `json:"pages" yaml:"pages"`
	CrawlErrors []string         `json:"crawl_errors,omitempty" yaml:"crawl_errors,omitempty"`
}

var (
	titleTagRE     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTagRE      = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	linkTagRE      = regexp.MustCompile(`(?is)<a\s+[^>]*href\s*=\s*["']([^"']+)["'][^>]*>`)
	canonicalRE    = regexp.MustCompile(`(?is)<link\s+[^>]*rel\s*=\s*["'][^"']*canonical[^"']*["'][^>]*>`)
	headingTagRE   = regexp.MustCompile(`(?is)<h[1-3][^>]*>(.*?)</h[1-3]>`)
	markdownLinkRE = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	htmlTagRE      = regexp.MustCompile(`(?is)<[^>]+>`)
	attributeRE    = regexp.MustCompile(`(?i)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*["']([^"']*)["']`)
)

func BuildSEOContentInventory(ctx context.Context, cfg SEOProjectConfig, maxPages int, now func() time.Time, allowLive bool) SEOContentInventory {
	if maxPages <= 0 {
		maxPages = 50
	}
	inv := SEOContentInventory{Generated: nowSEO(now)}
	if strings.TrimSpace(cfg.ContentDir) != "" {
		inv.Source = cfg.ContentDir
		pages, errs := inventoryLocalContent(cfg.ContentDir, cfg.SiteURL, maxPages)
		inv.Pages, inv.CrawlErrors = pages, errs
		return inv
	}
	if allowLive && strings.TrimSpace(cfg.SiteURL) != "" {
		inv.Source = cfg.SiteURL
		pages, errs := inventoryLiveSite(ctx, cfg.SiteURL, maxPages)
		inv.Pages, inv.CrawlErrors = pages, errs
	}
	return inv
}

func inventoryLocalContent(root, siteURL string, maxPages int) ([]SEOContentPage, []string) {
	root = filepath.Clean(root)
	var pages []SEOContentPage
	var errs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(pages) >= maxPages {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".htm" && ext != ".md" && ext != ".mdx" {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, readErr))
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		page := parseSEOContentPage(b, ext, path)
		page.Path = path
		if siteURL != "" {
			u := strings.TrimRight(siteURL, "/") + "/" + strings.TrimSuffix(filepath.ToSlash(rel), ext)
			if strings.HasSuffix(u, "/index") {
				u = strings.TrimSuffix(u, "index")
			}
			page.URL = u
		}
		pages = append(pages, page)
		return nil
	})
	if err != nil {
		errs = append(errs, err.Error())
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages, errs
}

func inventoryLiveSite(ctx context.Context, raw string, maxPages int) ([]SEOContentPage, []string) {
	start, err := validatePublicSEOURL(ctx, raw)
	if err != nil {
		return nil, []string{err.Error()}
	}
	host := strings.ToLower(start.Hostname())
	addresses, err := resolvePublicSEOHost(ctx, host)
	if err != nil {
		return nil, []string{err.Error()}
	}
	transport := clonedDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = pinnedSEODialContext(host, addresses)
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout: 20 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !strings.EqualFold(req.URL.Hostname(), host) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	queue := []string{start.String()}
	seen := map[string]bool{}
	var pages []SEOContentPage
	var errs []string
	for len(queue) > 0 && len(pages) < maxPages {
		rawURL := queue[0]
		queue = queue[1:]
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		req.Header.Set("User-Agent", "nicos-tools-ngtm-seo/0.4 (+bounded owned-site inventory)")
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rawURL, err))
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: status=%d read=%v close=%v", rawURL, resp.StatusCode, readErr, closeErr))
			continue
		}
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			continue
		}
		page := parseSEOContentPage(body, ".html", rawURL)
		page.URL = rawURL
		pages = append(pages, page)
		base, _ := url.Parse(rawURL)
		for _, href := range page.InternalLinks {
			u, err := base.Parse(href)
			if err != nil || !strings.EqualFold(u.Hostname(), host) || (u.Scheme != "http" && u.Scheme != "https") {
				continue
			}
			u.Fragment = ""
			u.RawQuery = ""
			candidate := u.String()
			checked, validateErr := validatePublicSEOURL(ctx, candidate)
			if validateErr != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", candidate, validateErr))
				continue
			}
			if !seen[checked.String()] {
				queue = append(queue, checked.String())
			}
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].URL < pages[j].URL })
	return pages, errs
}

func parseSEOContentPage(body []byte, ext, source string) SEOContentPage {
	raw := string(body)
	digest := sha256.Sum256(body)
	page := SEOContentPage{Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if ext == ".md" || ext == ".mdx" {
		page.Title, page.Description, page.Canonical, page.Robots, page.Headings, page.InternalLinks = parseMarkdownSEO(raw)
		plain := stripMarkdownForSEO(raw)
		page.WordCount = len(seoWords(plain))
		page.Terms = topSEOTerms(plain, 80)
		return page
	}
	page.Title = cleanHTMLText(firstRegexGroup(titleTagRE, raw))
	for _, tag := range metaTagRE.FindAllString(raw, -1) {
		attrs := htmlAttributes(tag)
		switch strings.ToLower(attrs["name"]) {
		case "description":
			page.Description = attrs["content"]
		case "robots":
			page.Robots = strings.ToLower(attrs["content"])
		}
	}
	if tag := canonicalRE.FindString(raw); tag != "" {
		page.Canonical = htmlAttributes(tag)["href"]
	}
	for _, m := range headingTagRE.FindAllStringSubmatch(raw, -1) {
		if len(m) > 1 {
			if h := cleanHTMLText(m[1]); h != "" {
				page.Headings = append(page.Headings, h)
			}
		}
	}
	for _, m := range linkTagRE.FindAllStringSubmatch(raw, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			page.InternalLinks = append(page.InternalLinks, strings.TrimSpace(m[1]))
		}
	}
	plain := cleanHTMLText(raw)
	page.WordCount = len(seoWords(plain))
	page.Terms = topSEOTerms(plain, 80)
	if page.Title == "" {
		page.Title = filepath.Base(source)
	}
	return page
}

func parseMarkdownSEO(raw string) (string, string, string, string, []string, []string) {
	var title, desc, canonical, robots string
	var headings []string
	s := bufio.NewScanner(strings.NewReader(raw))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "#") {
			h := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if h != "" {
				headings = append(headings, h)
				if title == "" {
					title = h
				}
			}
		}
		lower := strings.ToLower(line)
		if i := strings.IndexByte(line, ':'); i >= 0 {
			value := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
			switch strings.TrimSpace(lower[:i]) {
			case "description":
				if desc == "" {
					desc = value
				}
			case "canonical":
				canonical = value
			case "robots":
				robots = strings.ToLower(value)
			}
		}
	}
	var links []string
	for _, match := range markdownLinkRE.FindAllStringSubmatch(raw, -1) {
		if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
			links = append(links, strings.TrimSpace(match[1]))
		}
	}
	return title, desc, canonical, robots, headings, normalizeStrings(links)
}

func stripMarkdownForSEO(s string) string {
	r := strings.NewReplacer("#", " ", "*", " ", "`", " ", "[", " ", "]", " ", "(", " ", ")", " ", "_", " ")
	return r.Replace(s)
}

func firstRegexGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func htmlAttributes(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attributeRE.FindAllStringSubmatch(tag, -1) {
		if len(m) > 2 {
			out[strings.ToLower(m[1])] = cleanHTMLText(m[2])
		}
	}
	return out
}

func cleanHTMLText(s string) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	repl := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return strings.Join(strings.Fields(repl.Replace(s)), " ")
}

func seoWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

var seoStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "your": true, "you": true, "are": true, "but": true, "not": true,
	"into": true, "its": true, "our": true, "has": true, "have": true, "all": true,
}

func topSEOTerms(s string, limit int) []string {
	counts := map[string]int{}
	for _, w := range seoWords(s) {
		if len(w) < 3 || seoStopWords[w] {
			continue
		}
		counts[w]++
	}
	type item struct {
		word  string
		count int
	}
	var items []item
	for w, c := range counts {
		items = append(items, item{w, c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].word < items[j].word
		}
		return items[i].count > items[j].count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].word
	}
	return out
}

func existingPagesForKeyword(keyword string, inv SEOContentInventory) []SEOContentPage {
	kw := map[string]bool{}
	for _, w := range seoWords(keyword) {
		if len(w) >= 3 && !seoStopWords[w] {
			kw[w] = true
		}
	}
	if len(kw) == 0 {
		return nil
	}
	var out []SEOContentPage
	for _, p := range inv.Pages {
		matched := 0
		terms := map[string]bool{}
		for _, t := range p.Terms {
			terms[t] = true
		}
		for w := range kw {
			if terms[w] || strings.Contains(strings.ToLower(p.Title), w) {
				matched++
			}
		}
		if float64(matched)/float64(len(kw)) >= 0.60 || strings.Contains(strings.ToLower(p.Title), strings.ToLower(keyword)) {
			out = append(out, p)
		}
	}
	return out
}

func validatePublicSEOURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("SEO crawl URL must be absolute http(s)")
	}
	if _, err := resolvePublicSEOHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

func resolvePublicSEOHost(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !publicSEOIP(ip) {
			return nil, errors.New("SEO crawl URL resolves to a non-public IP")
		}
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve SEO crawl host: %w", err)
	}
	if len(addrs) == 0 {
		return nil, errors.New("SEO crawl host resolved no addresses")
	}
	addresses := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if !publicSEOIP(a.IP) {
			return nil, errors.New("SEO crawl host resolves to a non-public IP")
		}
		addresses = append(addresses, append(net.IP(nil), a.IP...))
	}
	return addresses, nil
}

// clonedDefaultTransport clones http.DefaultTransport when it is still a
// *http.Transport; a globally replaced default transport gets an equivalent
// fresh transport instead of a panic.
func clonedDefaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func pinnedSEODialContext(host string, addresses []net.IP) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		requestedHost, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse SEO crawl dial address: %w", err)
		}
		if !strings.EqualFold(strings.Trim(requestedHost, "[]"), strings.Trim(host, "[]")) {
			return nil, fmt.Errorf("SEO crawl dial escaped validated host %s", host)
		}
		var lastErr error
		for _, ip := range addresses {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = errors.New("SEO crawl host has no pinned public addresses")
		}
		return nil, lastErr
	}
}

func publicSEOIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast()
}
