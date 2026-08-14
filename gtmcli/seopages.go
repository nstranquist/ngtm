//nolint:errcheck // Terminal presentation writes are best-effort; page file writes are checked.
package gtmcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

// runSEOPages is the programmatic-SEO factory: one keyword → one
// keyword-targeted landing page, generated deterministically from the landing
// engine, plus an index. The point is owning many long-tail SERPs with pages
// that share one product spine — add a keyword, run once.
func runSEOPages(prog, subject string, keywords []string, pitch, buyURL, outDir string, out, errOut io.Writer) int {
	if len(keywords) == 0 {
		fmt.Fprintln(errOut, prog+" seo --pages: --keywords is required (one page per keyword)")
		return 2
	}
	if strings.TrimSpace(outDir) == "" {
		fmt.Fprintln(errOut, prog+" seo --pages: --out-dir is required")
		return 2
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(errOut, prog+" seo --pages:", err)
		return 1
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	opts := gtm.Options{Subject: subject, Offline: true, NoFeeds: true}

	group := gtm.StorefrontGroup{Heading: "Pages"}
	okCount := 0
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		slug := slugify(kw)
		cfg := gtm.LandingConfig{
			Product:      subject,
			Badge:        kw,
			Headline:     titleCaseKeyword(kw) + " — " + subject,
			Subhead:      pitch,
			HeroCTAURL:   buyURL,
			HeroCTALabel: "Get started",
			Grounding:    "Generated programmatic-SEO page targeting \"" + kw + "\"; review copy before indexing.",
		}
		page := gtm.BuildLandingFromConfig(cfg, opts, now, "offline")
		html := forceNoindexHTML(gtm.RenderLandingHTML(page))
		path := filepath.Join(outDir, slug+".html")
		if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
			fmt.Fprintf(errOut, "  FAIL %s: %v\n", path, err)
			continue
		}
		group.Cards = append(group.Cards, gtm.StorefrontCard{
			Name: titleCaseKeyword(kw), Category: subject, Desc: pitch, Href: slug + ".html",
		})
		fmt.Fprintf(out, "  ✓ %-40s → %s (%d bytes)\n", kw, path, len(html))
		okCount++
	}

	index := forceNoindexHTML(gtm.RenderStorefrontHTML(&gtm.StorefrontModel{
		Title: subject + " — keyword pages", Brand: subject, Generated: now,
		Intro:  "Programmatic-SEO page set; one page per target keyword.",
		Groups: []gtm.StorefrontGroup{group},
	}))
	idxPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(idxPath, []byte(index), 0o644); err != nil {
		fmt.Fprintln(errOut, prog+" seo --pages: index:", err)
		return 1
	}
	fmt.Fprintf(out, "  ✓ %-40s → %s\n", "index", idxPath)
	logRun(map[string]any{"ts": now, "surface": prog, "vertical": "seo.pages", "subject": subject, "pages": okCount, "out": outDir})
	fmt.Fprintf(out, "wrote %d page(s) + index to %s\n", okCount, outDir)
	return 0
}

// forceNoindexHTML keeps the legacy page fan-out as an explicitly review-only
// compatibility surface. The guarded lifecycle is the sole path that may emit
// indexable pages.
func forceNoindexHTML(body string) string {
	meta := "<meta name=\"robots\" content=\"noindex, nofollow\">\n"
	if strings.Contains(strings.ToLower(body), `name="robots"`) {
		return body
	}
	if i := strings.Index(strings.ToLower(body), "<head>"); i >= 0 {
		i += len("<head>")
		return body[:i] + "\n" + meta + body[i:]
	}
	return meta + body
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = slugRe.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}

func titleCaseKeyword(kw string) string {
	words := strings.Fields(kw)
	for i, w := range words {
		if len(w) > 3 || i == 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
