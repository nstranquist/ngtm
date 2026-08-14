package gtm

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// LandingPage is the structured model a conversion landing page renders from.
// It is the "publish" stage of the factory: the verticals research a product
// (brand → hero copy, pricing → tiers); this turns that intelligence into a
// shipped, self-contained HTML sales page in docs/human/ — the last mile the
// factory was missing. Every field is plain data so the page can be emitted as
// JSON (--json) or HTML, and so the build is deterministic + testable offline.
type LandingPage struct {
	Product      string           `json:"product"`               // display / brand name
	Slug         string           `json:"slug"`                  // output filename stem
	Badge        string           `json:"badge"`                 // small eyebrow chip
	Headline     string           `json:"headline"`              // hero H1 line (defaults to Product)
	Subhead      string           `json:"subhead"`               // hero tagline
	HeroCTALabel string           `json:"hero_cta_label"`        // primary button text
	HeroCTAURL   string           `json:"hero_cta_url"`          // primary button href (checkout/signup)
	FeaturesHead string           `json:"features_head"`         // section heading for the cards
	Features     []LandingFeature `json:"features"`              // value cards
	PricingHead  string           `json:"pricing_head"`          // section heading for pricing
	Tiers        []LandingTier    `json:"tiers"`                 // pricing cards
	Grounding    string           `json:"grounding"`             // confidence/provenance caveat
	Generated    string           `json:"generated"`             // RFC3339 build time
	Provider     string           `json:"provider"`              // narrative provider, or "offline"
	SourceNote   string           `json:"source_note"`           // where prose/engine lives
	Brand        string           `json:"brand,omitempty"`       // footer attribution ("<Product> — by <Brand>"); default "nicos-tools"
	RootCSS      string           `json:"root_css,omitempty"`    // optional generated `:root{}` token block (from `ngtm design`); empty => default theme
	ComingSoon   bool             `json:"coming_soon,omitempty"` // planned/unbuilt: suppress pricing, show a "Coming soon" + notify CTA (no fake checkout)
	Warnings     []string         `json:"warnings,omitempty"`
}

// LandingFeature is one value card.
type LandingFeature struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// LandingTier is one pricing card.
type LandingTier struct {
	Name     string   `json:"name"`
	Price    string   `json:"price"`              // pre-formatted, e.g. "$39" / "$28.5K"
	Period   string   `json:"period"`             // "" | "one-time" | "/mo" | "/yr"
	Note     string   `json:"note"`               // one-line role/positioning
	Features []string `json:"features,omitempty"` // bullet list
	CTALabel string   `json:"cta_label"`
	CTAURL   string   `json:"cta_url"`
	Featured bool     `json:"featured"` // highlight as the anchor / most-popular
}

// LandingConfig is the operator-supplied shape for a page. Any field left empty
// falls back to a grounded default (or, for hero copy, to the brand vertical /
// a deterministic offline template). Tiers, when nil, are derived from the
// value-based pricing model (ComputePricing) so a page is never priceless.
type LandingConfig struct {
	Product      string
	Slug         string
	Badge        string
	Headline     string
	Subhead      string
	Features     []LandingFeature
	FeaturesHead string
	PricingHead  string
	Tiers        []LandingTier
	HeroCTALabel string
	HeroCTAURL   string
	Grounding    string
	SourceNote   string
	Brand        string // footer attribution; default "nicos-tools"
	RootCSS      string // optional generated `:root{}` token block (from `ngtm design`)
	ComingSoon   bool   // planned/unbuilt: no pricing, a "Coming soon" + notify CTA
}

// JSON renders the landing model (the machine surface).
func (p *LandingPage) JSON() ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// BuildLandingFromConfig assembles a LandingPage deterministically: it applies
// the operator overrides, derives pricing tiers from the value-based model when
// none are supplied, and fills sensible defaults. It performs NO network/LLM
// work — grounded hero copy from the brand vertical is layered in by the caller
// (cmdLanding) before this runs. Kept pure so it is hermetically testable.
func BuildLandingFromConfig(cfg LandingConfig, opts Options, now, provider string) *LandingPage {
	product := strings.TrimSpace(cfg.Product)
	if product == "" {
		product = titleCaseSubject(opts.Subject)
	}
	slug := strings.TrimSpace(cfg.Slug)
	if slug == "" {
		slug = slugify(product)
	}
	headline := strings.TrimSpace(cfg.Headline)
	if headline == "" {
		headline = product
	}
	subhead := strings.TrimSpace(cfg.Subhead)
	if subhead == "" {
		subhead = fmt.Sprintf("The focused way to do %s right.", product)
	}
	ctaLabel := strings.TrimSpace(cfg.HeroCTALabel)
	if ctaLabel == "" {
		ctaLabel = "Get started"
		if cfg.ComingSoon {
			ctaLabel = "Notify me"
		}
	}
	featHead := strings.TrimSpace(cfg.FeaturesHead)
	if featHead == "" {
		featHead = "What you get"
	}
	priceHead := strings.TrimSpace(cfg.PricingHead)
	if priceHead == "" {
		priceHead = "Pricing"
	}

	// Coming-soon (planned/unbuilt) pages carry NO pricing — never a fake
	// checkout. Otherwise tiers fall back to the value-based model so a page is
	// never priceless.
	tiers := cfg.Tiers
	if cfg.ComingSoon {
		tiers = nil
	} else if len(tiers) == 0 {
		tiers = tiersFromPricing(opts, cfg.HeroCTALabel, cfg.HeroCTAURL)
	}
	// Default each tier's CTA to the hero target/label when the tier didn't set
	// one. A blank label resolves per-tier: the featured tier echoes the hero
	// CTA; secondary tiers get a neutral "Choose <Name>" so they don't all read
	// like the headline offer.
	for i := range tiers {
		if strings.TrimSpace(tiers[i].CTAURL) == "" {
			tiers[i].CTAURL = cfg.HeroCTAURL
		}
		if strings.TrimSpace(tiers[i].CTALabel) == "" {
			if tiers[i].Featured {
				tiers[i].CTALabel = ctaLabel
			} else {
				tiers[i].CTALabel = "Choose " + tiers[i].Name
			}
		}
	}

	grounding := strings.TrimSpace(cfg.Grounding)
	if grounding == "" {
		grounding = "Pricing reflects a value-based model (next-best alternative + captured differentiation); validate willingness-to-pay before locking it. Copy is grounded to verified facts where a research feed was available, and labeled otherwise."
	}
	src := strings.TrimSpace(cfg.SourceNote)
	if src == "" {
		src = "Engine: github.com/nstranquist/ngtm. Regenerate with `ngtm landing` — do not hand-edit."
	}

	return &LandingPage{
		Product:      product,
		Slug:         slug,
		Badge:        firstNonEmpty(cfg.Badge, "nicos-tools"),
		Headline:     headline,
		Subhead:      subhead,
		HeroCTALabel: ctaLabel,
		HeroCTAURL:   firstNonEmpty(cfg.HeroCTAURL, "#pricing"),
		FeaturesHead: featHead,
		Features:     cfg.Features,
		PricingHead:  priceHead,
		Tiers:        tiers,
		Grounding:    grounding,
		Generated:    now,
		Provider:     firstNonEmpty(provider, "offline"),
		SourceNote:   src,
		Brand:        firstNonEmpty(strings.TrimSpace(cfg.Brand), "nicos-tools"),
		RootCSS:      cfg.RootCSS,
		ComingSoon:   cfg.ComingSoon,
	}
}

// tiersFromPricing derives a good-better-best pricing ladder from the value-based
// model. Annual SaaS framing; the middle (anchor) tier is featured.
func tiersFromPricing(opts Options, ctaLabel, ctaURL string) []LandingTier {
	m, _ := ComputePricing(opts)
	out := make([]LandingTier, 0, len(m.Tiers))
	for i, t := range m.Tiers {
		out = append(out, LandingTier{
			Name:     t.Name,
			Price:    fmtMoney(t.Price),
			Period:   "/yr",
			Note:     t.Note,
			CTALabel: ctaLabel,
			CTAURL:   ctaURL,
			Featured: i == 1, // the value-based anchor
		})
	}
	return out
}

// LandingCopyFromReport extracts hero copy from a brand vertical report by
// locating its "Landing Copy" section and parsing the markdown. Returns empty
// strings/slice when the section is absent so the caller falls back cleanly.
func LandingCopyFromReport(r *Report) (headline, subhead, cta string, bullets []string) {
	if r == nil {
		return "", "", "", nil
	}
	for _, s := range r.Sections {
		if strings.Contains(strings.ToLower(s.Title), "landing copy") {
			return parseLandingCopy(s.Body)
		}
	}
	return "", "", "", nil
}

// parseLandingCopy pulls a hero headline, subhead, feature bullets, and CTA out
// of the brand vertical's "Landing Copy" markdown section. Best-effort: returns
// whatever it can find so the caller can use any non-empty field as an override.
func parseLandingCopy(body string) (headline, subhead, cta string, bullets []string) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case hasCopyLabel(line, "Headline"):
			headline = stripCopyLabel(line, "Headline")
		case hasCopyLabel(line, "Subhead"):
			subhead = stripCopyLabel(line, "Subhead")
		case hasCopyLabel(line, "CTA"):
			cta = stripCopyLabel(line, "CTA")
		case strings.HasPrefix(line, "- "):
			if b := strings.TrimSpace(strings.TrimPrefix(line, "- ")); b != "" {
				bullets = append(bullets, b)
			}
		}
	}
	return headline, subhead, cta, bullets
}

func hasCopyLabel(line, label string) bool {
	l := strings.ToLower(line)
	return strings.HasPrefix(l, strings.ToLower("**"+label+":**")) ||
		strings.HasPrefix(l, strings.ToLower(label+":"))
}

func stripCopyLabel(line, label string) string {
	for _, pfx := range []string{"**" + label + ":**", label + ":", "**" + label + "**:"} {
		if len(line) >= len(pfx) && strings.EqualFold(line[:len(pfx)], pfx) {
			return strings.TrimSpace(line[len(pfx):])
		}
	}
	return strings.TrimSpace(line)
}

// RenderLandingHTML emits a single self-contained, dependency-free HTML page,
// matching the docs/human dark-theme design system. Every dynamic value is
// HTML-escaped. The page is conversion-shaped: hero + CTA, value cards, pricing
// with per-tier buy buttons, a grounding caveat, and a provenance footer.
func RenderLandingHTML(p *LandingPage) string {
	e := html.EscapeString
	var b strings.Builder
	w := func(s string) { b.WriteString(s) }
	wf := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	title := p.Product
	if p.Subhead != "" {
		title = p.Product + " — " + p.Subhead
	}

	w("<!doctype html>\n")
	w("<!--\n  " + e(p.Product) + " — generated by `ngtm landing`.\n")
	w("  Single-file, dependency-free. " + e(p.SourceNote) + "\n-->\n")
	w("<html lang=\"en\">\n<head>\n")
	w("  <meta charset=\"utf-8\" />\n  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n")
	wf("  <title>%s</title>\n", e(title))
	wf("  <meta name=\"description\" content=\"%s\" />\n", e(p.Subhead))
	w(landingStyle(p.RootCSS))
	w("</head>\n<body>\n<div class=\"wrap\">\n\n")

	// Hero.
	w("  <header class=\"hero\">\n")
	badge := p.Badge
	if p.ComingSoon {
		badge = firstNonEmpty(p.Badge, "garrid") + " · coming soon"
	}
	wf("    <span class=\"badge\">%s</span>\n", e(badge))
	wf("    <h1>%s</h1>\n", e(p.Headline))
	wf("    <p class=\"tagline\">%s</p>\n", e(p.Subhead))
	wf("    <p class=\"cta-row\"><a class=\"btn btn-primary\" href=\"%s\">%s</a>", e(p.HeroCTAURL), e(p.HeroCTALabel))
	if len(p.Tiers) > 0 {
		w(" <a class=\"btn btn-ghost\" href=\"#pricing\">See pricing</a>")
	}
	w("</p>\n  </header>\n\n")

	if p.ComingSoon {
		w("  <div class=\"note\">In development — this page is a preview. Drop your email to be first in line at launch.</div>\n\n")
	}

	// Value cards.
	if len(p.Features) > 0 {
		w("  <section>\n")
		wf("    <h2>%s</h2>\n", e(p.FeaturesHead))
		w("    <div class=\"grid\">\n")
		for _, f := range p.Features {
			wf("      <div class=\"card\"><h3>%s</h3><p>%s</p></div>\n", e(f.Title), e(f.Body))
		}
		w("    </div>\n  </section>\n\n")
	}

	// Pricing.
	if len(p.Tiers) > 0 {
		w("  <section id=\"pricing\">\n")
		wf("    <h2>%s</h2>\n", e(p.PricingHead))
		w("    <div class=\"price-grid\">\n")
		for _, t := range p.Tiers {
			cls := "tier"
			if t.Featured {
				cls = "tier featured"
			}
			wf("      <div class=\"%s\">\n", cls)
			if t.Featured {
				w("        <span class=\"tier-flag\">Most popular</span>\n")
			}
			wf("        <div class=\"tier-name\">%s</div>\n", e(t.Name))
			wf("        <div class=\"price\">%s", e(t.Price))
			if t.Period != "" {
				wf("<span class=\"period\">%s</span>", e(periodLabel(t.Period)))
			}
			w("</div>\n")
			if t.Note != "" {
				wf("        <p class=\"tier-note\">%s</p>\n", e(t.Note))
			}
			if len(t.Features) > 0 {
				w("        <ul class=\"tier-features\">\n")
				for _, tf := range t.Features {
					wf("          <li>%s</li>\n", e(tf))
				}
				w("        </ul>\n")
			}
			btnCls := "btn btn-ghost"
			if t.Featured {
				btnCls = "btn btn-primary"
			}
			wf("        <a class=\"%s\" href=\"%s\">%s</a>\n", btnCls, e(t.CTAURL), e(t.CTALabel))
			w("      </div>\n")
		}
		w("    </div>\n")
		if p.Grounding != "" {
			wf("    <div class=\"note\">%s</div>\n", e(p.Grounding))
		}
		w("  </section>\n\n")
	}

	// Footer / provenance.
	model := p.Provider
	w("  <footer>\n")
	wf("    %s — by %s. Generated by <code>ngtm landing</code> on %s · narrative: %s.<br />\n",
		e(p.Product), e(firstNonEmpty(p.Brand, "nicos-tools")), e(p.Generated), e(model))
	wf("    %s\n", e(p.SourceNote))
	if len(p.Warnings) > 0 {
		w("    <div class=\"note warn\" style=\"margin-top:14px\">\n")
		for _, wn := range p.Warnings {
			wf("      <div>⚠️ %s</div>\n", e(wn))
		}
		w("    </div>\n")
	}
	w("  </footer>\n\n")
	w("</div>\n</body>\n</html>\n")
	return b.String()
}

// titleCaseSubject upper-cases the first rune of each whitespace-separated word
// in a subject, leaving the rest untouched (so "nvault" → "Nvault", "agent
// control plane" → "Agent Control Plane"). Used only when no explicit display
// name is given; callers pass --product for an exact brand name.
func titleCaseSubject(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(w)
		if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
			r[0] -= 'a' - 'A'
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func periodLabel(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "one-time", "onetime", "once":
		return " one-time"
	default:
		if strings.HasPrefix(p, "/") {
			return p
		}
		return " " + p
	}
}

// landingStyle assembles the page stylesheet. rootCSS, when non-empty, replaces
// the default `:root{}` token block (e.g. a system generated by `ngtm design`);
// the rest of the rules reference those tokens, so the whole page re-themes from
// the single block. `--accent-fg` (text-on-accent) defaults to the green-theme
// near-black if the supplied block doesn't define it.
func landingStyle(rootCSS string) string {
	root := strings.TrimSpace(rootCSS)
	if root == "" {
		root = defaultLandingRoot
	}
	return "  <style>\n" + root + "\n" + landingStyleBody + "  </style>\n"
}

const defaultLandingRoot = `    :root {
      --bg: #0d1117; --panel: #161b22; --border: #30363d;
      --fg: #e6edf3; --muted: #9da7b3; --accent: #7ee787; --accent2: #79c0ff;
      --accent-fg: #07140a; --warn: #f0883e; --code: #1f2630;
      --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
      --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    }`

const landingStyleBody = `    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--fg);
      font-family: var(--sans); line-height: 1.6; font-size: 16px; }
    .wrap { max-width: 960px; margin: 0 auto; padding: 56px 24px 96px; }
    header.hero { border-bottom: 1px solid var(--border); padding-bottom: 36px; margin-bottom: 36px; }
    .badge { display: inline-block; font-family: var(--mono); font-size: 12px;
      color: var(--accent); border: 1px solid var(--border); border-radius: 999px;
      padding: 3px 12px; margin-bottom: 18px; }
    h1 { font-size: 46px; margin: 0 0 12px; letter-spacing: -0.6px; line-height: 1.1; }
    .tagline { font-size: 20px; color: var(--muted); margin: 0 0 26px; max-width: 64ch; }
    h2 { font-size: 28px; margin: 52px 0 16px; letter-spacing: -0.3px; }
    h3 { font-size: 18px; margin: 0 0 8px; color: var(--accent2); }
    p { margin: 12px 0; }
    a { color: var(--accent2); }
    code { font-family: var(--mono); background: var(--code); padding: 1px 6px;
      border-radius: 5px; font-size: 0.86em; }
    .cta-row { display: flex; flex-wrap: wrap; gap: 12px; margin: 0; }
    .btn { display: inline-block; font-family: var(--sans); font-size: 15px; font-weight: 600;
      padding: 11px 22px; border-radius: 10px; text-decoration: none; border: 1px solid var(--border);
      transition: transform .05s ease, border-color .15s ease; }
    .btn:active { transform: translateY(1px); }
    .btn-primary { background: var(--accent); color: var(--accent-fg, #07140a); border-color: var(--accent); }
    .btn-primary:hover { filter: brightness(1.06); }
    .btn-ghost { background: transparent; color: var(--fg); }
    .btn-ghost:hover { border-color: var(--accent2); color: var(--accent2); }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 16px; margin: 20px 0; }
    .card { background: var(--panel); border: 1px solid var(--border); border-radius: 12px;
      padding: 20px 22px; }
    .card p { color: var(--muted); font-size: 14.5px; margin: 6px 0 0; }
    .price-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
      gap: 18px; margin: 22px 0; align-items: start; }
    .tier { position: relative; background: var(--panel); border: 1px solid var(--border);
      border-radius: 14px; padding: 26px 24px; display: flex; flex-direction: column; gap: 12px; }
    .tier.featured { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent) inset; }
    .tier-flag { position: absolute; top: -11px; left: 24px; background: var(--accent); color: var(--accent-fg, #07140a);
      font-family: var(--mono); font-size: 11px; font-weight: 700; padding: 2px 10px; border-radius: 999px; }
    .tier-name { font-family: var(--mono); font-size: 13px; color: var(--muted); text-transform: uppercase;
      letter-spacing: 0.5px; }
    .price { font-size: 40px; font-weight: 700; letter-spacing: -1px; }
    .price .period { font-size: 15px; font-weight: 500; color: var(--muted); letter-spacing: 0; }
    .tier-note { color: var(--muted); font-size: 14px; margin: 0; }
    .tier-features { margin: 4px 0 8px; padding-left: 18px; color: var(--fg); font-size: 14.5px; }
    .tier-features li { margin: 5px 0; }
    .tier .btn { text-align: center; margin-top: auto; }
    .note { border-left: 3px solid var(--accent); background: var(--code);
      padding: 12px 16px; border-radius: 0 8px 8px 0; margin: 20px 0; color: var(--muted); font-size: 14px; }
    .note.warn { border-left-color: var(--warn); background: rgba(240,136,62,0.07); }
    footer { margin-top: 64px; padding-top: 20px; border-top: 1px solid var(--border);
      color: var(--muted); font-size: 13px; }
`
