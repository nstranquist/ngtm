package gtm

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nstranquist/ngtm/internal/atomicfile"
)

type SEOPublishRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Slug        string `json:"slug,omitempty"`
	Approved    bool   `json:"approved"`
	Index       bool   `json:"index"`
}

type SEOPublishedPage struct {
	Keyword        string       `json:"keyword"`
	Slug           string       `json:"slug"`
	Path           string       `json:"path"`
	Canonical      string       `json:"canonical,omitempty"`
	Robots         string       `json:"robots"`
	WordCount      int          `json:"word_count"`
	Indexable      bool         `json:"indexable"`
	BriefID        string       `json:"brief_id"`
	EvidenceIDs    []string     `json:"evidence_ids"`
	Findings       []SEOFinding `json:"findings"`
	StructuredData bool         `json:"structured_data"`
}

type SEOPublishManifest struct {
	SchemaVersion  int                `json:"schema_version"`
	Generated      string             `json:"generated"`
	Project        string             `json:"project"`
	Approved       bool               `json:"approved"`
	IndexRequested bool               `json:"index_requested"`
	OutputDir      string             `json:"output_dir"`
	Pages          []SEOPublishedPage `json:"pages"`
	SitemapPath    string             `json:"sitemap_path,omitempty"`
	Findings       []SEOFinding       `json:"findings"`
	Passed         bool               `json:"passed"`
	Artifact       *SEOArtifactRef    `json:"artifact,omitempty"`
}

var seoHTMLTag = regexp.MustCompile(`(?s)<[^>]*>`)

func PublishSEOPage(cfg SEOProjectConfig, brief SEOBrief, req SEOPublishRequest, outputDir string, now func() string) (*SEOPublishManifest, error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = strings.TrimSpace(cfg.Publishing.OutputDir)
	}
	if outputDir == "" {
		return nil, errors.New("SEO publish output directory is required")
	}
	generated := nowSEO(nil)
	if now != nil {
		generated = now()
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = brief.SuggestedTitle
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = brief.UniqueValue
	}
	slug := seoSlugify(firstNonEmpty(req.Slug, brief.Keyword))
	if slug == "" {
		return nil, errors.New("SEO page slug is empty")
	}
	plain := strings.Join(strings.Fields(seoHTMLTag.ReplaceAllString(req.Body, " ")), " ")
	wordCount := len(seoWords(strings.Join([]string{title, description, brief.UniqueValue, plain}, " ")))
	canonical := ""
	if cfg.Publishing.CanonicalBaseURL != "" {
		canonical = strings.TrimRight(cfg.Publishing.CanonicalBaseURL, "/") + "/" + slug + "/"
	}
	page := SEOPublishedPage{
		Keyword: brief.Keyword, Slug: slug, BriefID: brief.ID, EvidenceIDs: brief.EvidenceIDs,
		Canonical: canonical, WordCount: wordCount, Robots: "noindex, nofollow",
	}
	if !brief.Passed {
		page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_BRIEF_BLOCKED", Severity: "blocker", Message: "brief must pass before an indexable page can be emitted"})
	}
	if len(brief.UniqueValue) < cfg.Publishing.MinimumUniqueValue {
		page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_UNIQUE_VALUE_MISSING", Severity: "blocker", Message: fmt.Sprintf("unique value must be at least %d characters", cfg.Publishing.MinimumUniqueValue)})
	}
	if wordCount < cfg.Publishing.MinimumWordCount {
		page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_THIN_CONTENT", Severity: "blocker", Message: fmt.Sprintf("page has %d words; policy requires %d", wordCount, cfg.Publishing.MinimumWordCount)})
	}
	if canonical == "" || !validSEOCanonical(canonical, cfg.Domain) {
		page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_CANONICAL_INVALID", Severity: "blocker", Message: "canonical must be absolute HTTP(S) on the configured domain"})
	}
	for _, existing := range brief.ExistingPages {
		if canonical != "" && (strings.EqualFold(existing.Canonical, canonical) || strings.EqualFold(existing.URL, canonical)) {
			page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_CANONICAL_DUPLICATE", Severity: "blocker", Path: firstNonEmpty(existing.Path, existing.URL), Message: "an owned page already claims the requested canonical"})
		}
		if existingSEOSlug(existing, slug) {
			page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_SLUG_DUPLICATE", Severity: "blocker", Path: firstNonEmpty(existing.Path, existing.URL), Message: "an owned page already claims the requested slug"})
		}
	}
	if req.Index && !req.Approved {
		page.Findings = append(page.Findings, SEOFinding{Code: "PUBLISH_APPROVAL_REQUIRED", Severity: "blocker", Message: "--index requires explicit --approved"})
	}
	page.Indexable = req.Index && req.Approved && !hasSEOBlockers(page.Findings)
	if page.Indexable {
		page.Robots = "index, follow"
	}
	page.StructuredData = true

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	page.Path = filepath.Join(outputDir, slug+".html")
	body := renderSEOPageHTML(cfg, brief, page, title, description, req.Body, generated)
	if err := atomicfile.WriteFile(page.Path, []byte(body), 0o644); err != nil {
		return nil, err
	}
	manifest := &SEOPublishManifest{
		SchemaVersion: SEOSchemaVersion, Generated: generated, Project: cfg.Project,
		Approved: req.Approved, IndexRequested: req.Index, OutputDir: outputDir, Pages: []SEOPublishedPage{page},
	}
	if page.Indexable && cfg.Publishing.RequireSitemap {
		manifest.SitemapPath = filepath.Join(outputDir, "sitemap.xml")
		sitemap := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\"><url><loc>%s</loc></url></urlset>\n", html.EscapeString(page.Canonical))
		if err := atomicfile.WriteFile(manifest.SitemapPath, []byte(sitemap), 0o644); err != nil {
			return nil, err
		}
	}
	manifest.Findings = append(manifest.Findings, page.Findings...)
	manifest.Passed = !hasSEOBlockers(manifest.Findings)
	manifestPath := filepath.Join(outputDir, "seo-publish-manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	if err := atomicfile.WriteFile(manifestPath, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return manifest, nil
}

func validSEOCanonical(raw, domain string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return normalizeDomain(u.Hostname()) == normalizeDomain(domain)
}

func existingSEOSlug(page SEOContentPage, slug string) bool {
	for _, raw := range []string{page.URL, page.Path} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if u, err := url.Parse(raw); err == nil && u.Path != "" {
			raw = u.Path
		}
		base := strings.TrimSuffix(filepath.Base(strings.TrimRight(filepath.ToSlash(raw), "/")), filepath.Ext(raw))
		if seoSlugify(base) == slug {
			return true
		}
	}
	return false
}

func renderSEOPageHTML(cfg SEOProjectConfig, brief SEOBrief, page SEOPublishedPage, title, description, body, generated string) string {
	e := html.EscapeString
	articleBody := strings.TrimSpace(body)
	if articleBody == "" {
		articleBody = brief.UniqueValue
	}
	paragraphs := strings.Split(articleBody, "\n\n")
	ld, _ := json.Marshal(map[string]any{
		"@context": "https://schema.org", "@type": "Article", "headline": title,
		"description": description, "dateModified": generated, "url": page.Canonical,
	})
	var out strings.Builder
	fmt.Fprintf(&out, "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n")
	fmt.Fprintf(&out, "<title>%s</title><meta name=\"description\" content=\"%s\"><meta name=\"robots\" content=\"%s\">\n", e(title), e(description), e(page.Robots))
	if page.Canonical != "" {
		fmt.Fprintf(&out, "<link rel=\"canonical\" href=\"%s\">\n", e(page.Canonical))
	}
	fmt.Fprintf(&out, "<meta name=\"ngtm-seo-brief\" content=\"%s\"><meta name=\"ngtm-seo-evidence\" content=\"%s\">\n", e(brief.ID), e(strings.Join(page.EvidenceIDs, ",")))
	fmt.Fprintf(&out, "<script type=\"application/ld+json\">%s</script></head><body><main><article><h1>%s</h1>\n", string(ld), e(title))
	for _, paragraph := range paragraphs {
		if p := strings.TrimSpace(paragraph); p != "" {
			fmt.Fprintf(&out, "<p>%s</p>\n", e(p))
		}
	}
	if len(brief.InternalLinks) > 0 {
		out.WriteString("<aside><h2>Related resources</h2><ul>")
		for _, link := range brief.InternalLinks {
			fmt.Fprintf(&out, "<li><a href=\"%s\">%s</a></li>", e(link), e(link))
		}
		out.WriteString("</ul></aside>")
	}
	fmt.Fprintf(&out, "</article></main><!-- ngtm seo project=%s generated=%s --></body></html>\n", e(cfg.Project), e(generated))
	return out.String()
}
