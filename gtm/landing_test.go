package gtm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildLandingFromConfig_ExplicitTiersAndDefaults(t *testing.T) {
	cfg := LandingConfig{
		Product:    "Cadence",
		Headline:   "Focus that follows you everywhere",
		Subhead:    "One timer, synced across web, mobile, and your menubar.",
		HeroCTAURL: "https://buy.example.com/cadence",
		Features:   []LandingFeature{{Title: "Menubar timer", Body: "Live countdown in the macOS menubar."}},
		Tiers: []LandingTier{
			{Name: "Pro", Price: "$39", Period: "one-time", Note: "Lifetime license", Featured: true},
		},
	}
	p := BuildLandingFromConfig(cfg, Options{Subject: "cadence"}, "2026-06-07T00:00:00Z", "offline")

	if p.Product != "Cadence" || p.Slug != "cadence" {
		t.Fatalf("product/slug: got %q/%q", p.Product, p.Slug)
	}
	if p.HeroCTALabel != "Get started" { // default applied
		t.Errorf("default CTA label not applied: %q", p.HeroCTALabel)
	}
	if len(p.Tiers) != 1 || p.Tiers[0].Price != "$39" {
		t.Fatalf("explicit tier not preserved: %+v", p.Tiers)
	}
	// Tier CTA URL should fall back to the hero target.
	if p.Tiers[0].CTAURL != "https://buy.example.com/cadence" {
		t.Errorf("tier CTA did not inherit hero URL: %q", p.Tiers[0].CTAURL)
	}
	if p.Grounding == "" {
		t.Error("expected a default grounding caveat")
	}
	if !strings.Contains(p.SourceNote, "github.com/nstranquist/ngtm") {
		t.Errorf("SourceNote = %q, want product module", p.SourceNote)
	}
}

func TestBuildLandingFromConfig_DerivesPricingTiers(t *testing.T) {
	// Supply the value model inputs so ComputePricing yields real tiers.
	opts := Options{Subject: "nvault", Inputs: map[string]float64{
		"next_best_price": 21000, "diff_value": 15000, "value_capture": 0.5,
	}}
	p := BuildLandingFromConfig(LandingConfig{Product: "nvault", HeroCTAURL: "/signup"}, opts, "t", "offline")
	if len(p.Tiers) != 3 {
		t.Fatalf("expected good-better-best (3 tiers), got %d", len(p.Tiers))
	}
	if !p.Tiers[1].Featured {
		t.Errorf("middle (anchor) tier should be featured: %+v", p.Tiers)
	}
	for _, tr := range p.Tiers {
		if !strings.HasPrefix(tr.Price, "$") {
			t.Errorf("tier price not money-formatted: %q", tr.Price)
		}
		if tr.CTAURL != "/signup" {
			t.Errorf("derived tier CTA URL = %q, want /signup", tr.CTAURL)
		}
	}
}

func TestRenderLandingHTML_ContainsConversionElements(t *testing.T) {
	cfg := LandingConfig{
		Product:      "Cadence",
		Headline:     "Focus that follows you everywhere",
		Subhead:      "Synced focus sessions.",
		Badge:        "nicos-tools · consumer",
		HeroCTAURL:   "https://buy.example.com/cadence",
		HeroCTALabel: "Buy Cadence — $39",
		Features:     []LandingFeature{{Title: "Menubar timer", Body: "Live countdown."}},
		Tiers: []LandingTier{
			{Name: "Pro", Price: "$39", Period: "one-time", Note: "Lifetime", Features: []string{"All platforms"}, Featured: true},
		},
	}
	p := BuildLandingFromConfig(cfg, Options{Subject: "cadence"}, "2026-06-07T12:00:00Z", "offline")
	out := RenderLandingHTML(p)

	mustContain := []string{
		"<!doctype html>",
		"Focus that follows you everywhere", // headline
		"Synced focus sessions.",            // subhead → also meta description
		"https://buy.example.com/cadence",   // buy URL in CTA href
		"Buy Cadence — $39",                 // CTA label
		"Menubar timer",                     // feature card
		`<div class="price">$39`,            // pricing
		"one-time",                          // period label
		"Most popular",                      // featured flag
		"ngtm landing",                      // provenance
		"github.com/nstranquist/ngtm",       // engine source note
		"2026-06-07T12:00:00Z",              // generated stamp
		`name="description"`,                // SEO meta
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("rendered HTML missing %q", m)
		}
	}
}

func TestRenderLandingHTML_DefaultThemeWhenNoRootCSS(t *testing.T) {
	p := BuildLandingFromConfig(LandingConfig{Product: "X"}, Options{Subject: "x"}, "2026-06-07T12:00:00Z", "offline")
	out := RenderLandingHTML(p)
	if !strings.Contains(out, "--bg: #0d1117") {
		t.Errorf("expected default theme :root when RootCSS empty")
	}
	if strings.Count(out, "</style>") != 1 {
		t.Errorf("expected exactly one </style>, got %d", strings.Count(out, "</style>"))
	}
}

func TestRenderLandingHTML_AppliesGeneratedRootCSS(t *testing.T) {
	root := "    :root {\n      --bg: #0c1016; --panel: #151a22; --border: #383d47;\n" +
		"      --fg: #e8edf6; --muted: #888c94; --accent: #77abff; --accent2: #b6aaff;\n" +
		"      --accent-fg: #17171a; --warn: #d29922; --code: #10151d;\n" +
		"      --mono: x; --sans: y;\n    }"
	cfg := LandingConfig{Product: "Garrid", RootCSS: root}
	p := BuildLandingFromConfig(cfg, Options{Subject: "garrid"}, "2026-06-07T12:00:00Z", "offline")
	out := RenderLandingHTML(p)
	if !strings.Contains(out, "--accent: #77abff") {
		t.Errorf("generated accent not applied")
	}
	if strings.Contains(out, "--bg: #0d1117") {
		t.Errorf("default theme leaked when RootCSS provided")
	}
	if strings.Count(out, ":root {") != 1 || strings.Count(out, "</style>") != 1 {
		t.Errorf("malformed stylesheet: %d :root, %d </style>", strings.Count(out, ":root {"), strings.Count(out, "</style>"))
	}
	// the rest of the stylesheet (which references the tokens) must still be present
	if !strings.Contains(out, ".btn-primary { background: var(--accent)") {
		t.Errorf("stylesheet body missing after root swap")
	}
}

func TestComingSoon_NoPricingNotifyCTA(t *testing.T) {
	cfg := LandingConfig{Product: "Echo", Subhead: "Voice → notes", ComingSoon: true, Brand: "garrid"}
	p := BuildLandingFromConfig(cfg, Options{Subject: "echo"}, "t", "offline")
	if len(p.Tiers) != 0 {
		t.Fatalf("coming-soon page must have NO tiers (no fake checkout), got %d", len(p.Tiers))
	}
	if p.HeroCTALabel != "Notify me" {
		t.Errorf("coming-soon CTA should default to 'Notify me', got %q", p.HeroCTALabel)
	}
	out := RenderLandingHTML(p)
	if strings.Contains(out, `id="pricing"`) {
		t.Error("coming-soon page leaked a pricing section")
	}
	if !strings.Contains(out, "coming soon") {
		t.Error("expected a 'coming soon' badge")
	}
	if !strings.Contains(out, "In development") {
		t.Error("expected the in-development notice")
	}
}

func TestRenderStorefrontHTML_GroupsAndPlanned(t *testing.T) {
	s := &StorefrontModel{
		Title: "Products", Brand: "garrid", Generated: "t",
		Groups: []StorefrontGroup{
			{Heading: "For teams", Cards: []StorefrontCard{
				{Name: "nvault", Href: "nvault.html", Desc: "secrets", Price: "$28.5K", Period: "/yr", Stats: []string{"GO", "LTV:CAC 11.8×"}},
			}},
			{Heading: "In the works", Cards: []StorefrontCard{
				{Name: "Echo", Href: "echo.html", Desc: "voice notes", Planned: true},
			}},
		},
	}
	out := RenderStorefrontHTML(s)
	for _, want := range []string{"<!doctype html>", "For teams", "In the works", "nvault.html", "$28.5K", "echo.html", "Coming soon", "pill go", "garrid — product portfolio", "Generated by"} {
		if !strings.Contains(out, want) {
			t.Errorf("storefront missing %q", want)
		}
	}
	// Planned card: muted class + Preview button, no price.
	if !strings.Contains(out, `class="product soon"`) {
		t.Error("planned card should get the 'soon' class")
	}
	if !strings.Contains(out, "Preview →") {
		t.Error("planned card should show a Preview button, not Buy/View")
	}
}

func TestRenderLandingHTML_EscapesUserContent(t *testing.T) {
	cfg := LandingConfig{
		Product:    `Evil<script>alert(1)</script>`,
		Headline:   `H "x" <b>`,
		HeroCTAURL: `https://x.test/?a="b"&c=<d>`,
		Tiers:      []LandingTier{{Name: "T", Price: "$1", Period: "/mo"}},
	}
	p := BuildLandingFromConfig(cfg, Options{Subject: "x"}, "t", "offline")
	out := RenderLandingHTML(p)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("unescaped <script> leaked into HTML (XSS)")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped script tag")
	}
	if strings.Contains(out, `href="https://x.test/?a="b"`) {
		t.Error("unescaped quote in attribute value broke out of href")
	}
}

func TestLandingCopyFromReport_ParsesBrandSection(t *testing.T) {
	body := `### nvault

**Headline:** Secrets you can prove are private.
**Subhead:** Zero-knowledge secrets and params for teams.

- Local-first — works fully offline.
- Zero-knowledge: the server never sees plaintext.
- Typed & versioned across machines.

**CTA:** Start free`
	rep := &Report{Sections: []Section{
		{Title: "Brand Context", Body: "..."},
		{Title: "Landing Copy (inferred)", Body: body},
	}}
	hl, sh, cta, bullets := LandingCopyFromReport(rep)
	if hl != "Secrets you can prove are private." {
		t.Errorf("headline = %q", hl)
	}
	if sh != "Zero-knowledge secrets and params for teams." {
		t.Errorf("subhead = %q", sh)
	}
	if cta != "Start free" {
		t.Errorf("cta = %q", cta)
	}
	if len(bullets) != 3 {
		t.Fatalf("bullets = %d (%v)", len(bullets), bullets)
	}
}

func TestLandingCopyFromReport_AbsentSection(t *testing.T) {
	rep := &Report{Sections: []Section{{Title: "Brand Context", Body: "x"}}}
	hl, _, _, bullets := LandingCopyFromReport(rep)
	if hl != "" || bullets != nil {
		t.Errorf("expected empty result for missing section, got %q / %v", hl, bullets)
	}
}

func TestLandingPage_JSONRoundTrips(t *testing.T) {
	p := BuildLandingFromConfig(LandingConfig{
		Product: "X", Tiers: []LandingTier{{Name: "Pro", Price: "$9", Period: "/mo"}},
	}, Options{Subject: "x"}, "t", "offline")
	b, err := p.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back LandingPage
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json did not round-trip: %v", err)
	}
	if back.Product != "X" || len(back.Tiers) != 1 {
		t.Errorf("round-trip lost data: %+v", back)
	}
}
