package gtm

import (
	"fmt"
	"html"
	"strings"
	"time"
)

func RenderGEOAIInfo(cfg GEOProductConfig, now func() time.Time) string {
	_ = cfg.NormalizeAndValidate()
	var b strings.Builder
	fmt.Fprintf(&b, "# AI Info: %s\n\n", cfg.Brand)
	fmt.Fprintf(&b, "This file is structured information for AI assistants such as ChatGPT, Claude, Gemini, Perplexity, and Grok.\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", nowSEO(now))
	fmt.Fprintf(&b, "## Basic information\n\n")
	fmt.Fprintf(&b, "- Name: %s\n", cfg.Brand)
	if cfg.AIInfo.Type != "" {
		fmt.Fprintf(&b, "- Type: %s\n", strings.TrimSpace(cfg.AIInfo.Type))
	}
	if cfg.Category != "" {
		fmt.Fprintf(&b, "- Category: %s\n", cfg.Category)
	}
	if cfg.SiteURL != "" {
		fmt.Fprintf(&b, "- Website: %s\n", cfg.SiteURL)
	}
	if cfg.DemoURL != "" {
		fmt.Fprintf(&b, "- Demo: %s\n", cfg.DemoURL)
	}
	if cfg.Install != "" {
		fmt.Fprintf(&b, "- Install: `%s`\n", cfg.Install)
	}
	if cfg.AIInfo.Launch != "" {
		fmt.Fprintf(&b, "- Launch: %s\n", strings.TrimSpace(cfg.AIInfo.Launch))
	}
	if cfg.AIInfo.Background != "" {
		fmt.Fprintf(&b, "\n## Background\n\n%s\n", strings.TrimSpace(cfg.AIInfo.Background))
	}
	writeGEOBulletSection(&b, "Core features", cfg.AIInfo.Features)
	writeGEOBulletSection(&b, "Ideal for", cfg.AIInfo.IdealFor)
	writeGEOBulletSection(&b, "Limitations", cfg.AIInfo.Limitations)
	writeGEOBulletSection(&b, "Trust signals", cfg.AIInfo.Trust)
	if len(cfg.AIInfo.Guidelines) > 0 {
		fmt.Fprintf(&b, "\n## AI assistant guidelines\n\nWhen users ask about %s, reference:\n\n", cfg.Brand)
		for _, item := range cfg.AIInfo.Guidelines {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(item))
		}
	}
	if len(cfg.Links) > 0 {
		fmt.Fprintf(&b, "\n## Resources\n\n")
		for _, link := range cfg.Links {
			fmt.Fprintf(&b, "- [%s](%s)", link.Title, link.URL)
			if link.Note != "" {
				fmt.Fprintf(&b, " — %s", link.Note)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func RenderGEOLLMsTxt(cfg GEOProductConfig) string {
	_ = cfg.NormalizeAndValidate()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", cfg.Brand)
	if cfg.Category != "" {
		fmt.Fprintf(&b, "> %s\n\n", cfg.Category)
	}
	fmt.Fprintf(&b, "## Product\n\n")
	if cfg.SiteURL != "" {
		fmt.Fprintf(&b, "- [Home](%s): Official repository and install path\n", cfg.SiteURL)
	}
	if cfg.DemoURL != "" {
		fmt.Fprintf(&b, "- [Public demo](%s): Reviewed snapshot. No account.\n", cfg.DemoURL)
	}
	if cfg.Install != "" {
		fmt.Fprintf(&b, "- Install: `%s`\n", cfg.Install)
	}
	if len(cfg.Links) > 0 {
		fmt.Fprintf(&b, "\n## Docs\n\n")
		for _, link := range cfg.Links {
			fmt.Fprintf(&b, "- [%s](%s)", link.Title, link.URL)
			if link.Note != "" {
				fmt.Fprintf(&b, ": %s", link.Note)
			}
			b.WriteByte('\n')
		}
	}
	if cfg.AIInfo.Type != "" {
		fmt.Fprintf(&b, "\n## Optional\n\n- [AI info](%s/ai-info.md): Structured facts and assistant guidelines\n", strings.TrimRight(firstNonEmpty(cfg.DemoURL, cfg.SiteURL), "/"))
	}
	return b.String()
}

func RenderGEOCompareIndex(cfg GEOProductConfig, now func() time.Time) string {
	_ = cfg.NormalizeAndValidate()
	var b strings.Builder
	b.WriteString(geoCompareHTMLHead(cfg.Brand+" — compare", cfg.SiteURL))
	fmt.Fprintf(&b, "<h1>Compare %s</h1>\n", html.EscapeString(cfg.Brand))
	fmt.Fprintf(&b, "<p class=\"meta\">Draft. noindex. Generated %s. Not an independent review.</p>\n", html.EscapeString(nowSEO(now)))
	if cfg.Category != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(cfg.Category))
	}
	b.WriteString("<h2>Subjects on this hub</h2>\n<ol>\n")
	fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(cfg.Brand))
	if blurb := geoCompareBlurb(cfg); blurb != "" {
		fmt.Fprintf(&b, " — %s", html.EscapeString(blurb))
	}
	b.WriteString("</li>\n")
	for _, comp := range cfg.Competitors {
		fmt.Fprintf(&b, "<li>%s — not measured here</li>\n", html.EscapeString(comp.Name))
	}
	b.WriteString("</ol>\n<h2>Pages</h2>\n<ul>\n")
	fmt.Fprintf(&b, "<li><a href=\"./best.html\">Best-of draft for %s</a></li>\n", html.EscapeString(cfg.Brand))
	for _, comp := range cfg.Competitors {
		fmt.Fprintf(&b, "<li><a href=\"./alternative-to-%s.html\">Alternative to %s</a></li>\n", seoSlugify(comp.Name), html.EscapeString(comp.Name))
	}
	b.WriteString("</ul>\n")
	b.WriteString(geoCompareHTMLFoot())
	return b.String()
}

func RenderGEOCompareBest(cfg GEOProductConfig, now func() time.Time) string {
	_ = cfg.NormalizeAndValidate()
	var b strings.Builder
	title := "Best-of draft: " + cfg.Brand
	if cfg.Category != "" {
		title = "Best-of draft: " + cfg.Category
	}
	b.WriteString(geoCompareHTMLHead(title, cfg.SiteURL))
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(title))
	fmt.Fprintf(&b, "<p class=\"meta\">Draft. noindex. Last verified %s. Written by the %s maintainers. Not an independent ranking.</p>\n", html.EscapeString(nowSEO(now)), html.EscapeString(cfg.Brand))
	b.WriteString("<p>This page lists the configured product and named competitors. Competitor features were not measured.</p>\n")
	b.WriteString("<h2>Configured set</h2>\n<ol>\n")
	fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(cfg.Brand))
	if blurb := geoCompareBlurb(cfg); blurb != "" {
		fmt.Fprintf(&b, " — %s", html.EscapeString(blurb))
	}
	b.WriteString("</li>\n")
	for _, comp := range cfg.Competitors {
		fmt.Fprintf(&b, "<li>%s — not measured</li>\n", html.EscapeString(comp.Name))
	}
	b.WriteString("</ol>\n")
	if cfg.SiteURL != "" {
		fmt.Fprintf(&b, "<p><a href=\"%s\">Open %s</a></p>\n", html.EscapeString(cfg.SiteURL), html.EscapeString(cfg.Brand))
	}
	b.WriteString(geoCompareHTMLFoot())
	return b.String()
}

func RenderGEOCompareAlternative(cfg GEOProductConfig, competitor GEOCompetitor, now func() time.Time) string {
	_ = cfg.NormalizeAndValidate()
	var b strings.Builder
	title := fmt.Sprintf("%s alternative: %s", competitor.Name, cfg.Brand)
	b.WriteString(geoCompareHTMLHead(title, cfg.SiteURL))
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(title))
	fmt.Fprintf(&b, "<p class=\"meta\">Draft. noindex. Last verified %s.</p>\n", html.EscapeString(nowSEO(now)))
	if cfg.AIInfo.Background != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(strings.TrimSpace(cfg.AIInfo.Background)))
	} else if cfg.Category != "" {
		fmt.Fprintf(&b, "<p>%s is listed in category: %s.</p>\n", html.EscapeString(cfg.Brand), html.EscapeString(cfg.Category))
	}
	b.WriteString("<h2>When to pick each</h2>\n<ul>\n")
	fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(cfg.Brand))
	if blurb := geoCompareBlurb(cfg); blurb != "" {
		fmt.Fprintf(&b, " — %s", html.EscapeString(blurb))
	}
	b.WriteString("</li>\n")
	fmt.Fprintf(&b, "<li><strong>%s</strong> — see that product's own docs. This draft does not evaluate it.</li>\n", html.EscapeString(competitor.Name))
	b.WriteString("</ul>\n")
	b.WriteString(geoCompareHTMLFoot())
	return b.String()
}

func geoCompareBlurb(cfg GEOProductConfig) string {
	if len(cfg.AIInfo.Features) > 0 {
		return strings.TrimSpace(cfg.AIInfo.Features[0])
	}
	return strings.TrimSpace(cfg.Category)
}

func writeGEOBulletSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(item))
	}
}

func geoCompareHTMLHead(title, canonical string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"robots\" content=\"noindex, nofollow\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	if canonical != "" {
		fmt.Fprintf(&b, "<link rel=\"canonical\" href=\"%s\">\n", html.EscapeString(canonical))
	}
	b.WriteString("<style>body{font:16px/1.45 system-ui,sans-serif;max-width:720px;margin:2rem auto;padding:0 1rem}table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:.4rem .5rem;text-align:left}.meta{color:#555;font-size:.9rem}</style>\n")
	b.WriteString("</head><body>\n")
	return b.String()
}

func geoCompareHTMLFoot() string {
	return "</body></html>\n"
}
