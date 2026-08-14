package design

import (
	"fmt"
	"sort"
	"strings"
)

// CSSVars renders just the `:root { --token: value; }` custom-property block for
// one mode. This is the integration seam: any page (e.g. the ngtm landing
// template) can drop these variables in and inherit the generated system.
func CSSVars(t Theme, mode Mode) string {
	p := t.Dark
	if mode == ModeLight && t.Light != nil {
		p = *t.Light
	}
	var b strings.Builder
	b.WriteString(":root {\n")
	rows := [][2]string{
		{"bg", p.BG}, {"surface", p.Surface}, {"surface-2", p.Surface2}, {"border", p.Border},
		{"fg", p.FG}, {"muted", p.Muted},
		{"primary", p.Primary}, {"primary-fg", p.PrimaryFg},
		{"accent", p.Accent}, {"accent-fg", p.AccentFg},
		{"success", p.Success}, {"warning", p.Warning}, {"danger", p.Danger}, {"info", p.Info},
		{"code", p.Code},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  --%s: %s;\n", r[0], r[1])
	}
	fmt.Fprintf(&b, "  --radius: %gpx;\n", t.Radius)
	fmt.Fprintf(&b, "  --font-sans: %s;\n", t.FontSans)
	fmt.Fprintf(&b, "  --font-mono: %s;\n", t.FontMono)
	// type scale
	for _, s := range typeStepOrder {
		fmt.Fprintf(&b, "  --text-%s: %grem;\n", s.Name, t.Type.Steps[s.Name]/16)
	}
	// spacing scale
	for i, v := range t.Spacing.Steps {
		fmt.Fprintf(&b, "  --space-%d: %gpx;\n", i+1, v)
	}
	b.WriteString("}\n")
	return b.String()
}

// LandingRootCSS renders a `:root{}` block in the legacy landing/garrid token
// vocabulary (--bg/--panel/--border/--fg/--muted/--accent/--accent2/--accent-fg/
// --warn/--code/--mono/--sans) from a generated theme. This is the seam that lets
// `ngtm landing` pages and the bespoke docs/human pages adopt the generated
// system: --accent maps to the brand primary (the action color), --accent2 to the
// secondary accent, --panel to surface. Indented to match the landing stylesheet.
func LandingRootCSS(t Theme, mode Mode) string {
	p := t.Dark
	if mode == ModeLight && t.Light != nil {
		p = *t.Light
	}
	var b strings.Builder
	b.WriteString("    :root {\n")
	fmt.Fprintf(&b, "      --bg: %s; --panel: %s; --border: %s;\n", p.BG, p.Surface, p.Border)
	fmt.Fprintf(&b, "      --fg: %s; --muted: %s; --accent: %s; --accent2: %s;\n", p.FG, p.Muted, p.Primary, p.Accent)
	fmt.Fprintf(&b, "      --accent-fg: %s; --warn: %s; --code: %s;\n", p.PrimaryFg, p.Warning, p.Code)
	fmt.Fprintf(&b, "      --mono: %s;\n", t.FontMono)
	fmt.Fprintf(&b, "      --sans: %s;\n", t.FontSans)
	b.WriteString("    }")
	return b.String()
}

// RenderPreviewHTML produces a single self-contained HTML page that exercises
// every token of the generated system — swatches, type ramp, buttons, inputs,
// cards, and state badges. It is the artifact screenshotted for the visual
// (perceptual) channel of the self-review loop, and a human-inspectable spec.
func RenderPreviewHTML(t Theme, mode Mode) string {
	p := t.Dark
	if mode == ModeLight && t.Light != nil {
		p = *t.Light
	}
	sc := Score(t, mode)
	name := t.Name
	if name == "" {
		name = "Untitled"
	}

	var b strings.Builder
	w := func(s string) { b.WriteString(s) }
	w("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\"/>\n")
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"/>\n")
	fmt.Fprintf(&b, "<title>%s — design system (%s)</title>\n", esc(name), p.Mode)
	w("<style>\n")
	w(CSSVars(t, mode))
	w(previewBaseCSS)
	w("</style>\n</head>\n<body>\n<div class=\"wrap\">\n")

	// header
	fmt.Fprintf(&b, `<header class="dh">
  <div class="dh-badge">%s · %s · %s scheme</div>
  <h1>%s</h1>
  <p class="dh-sub">Generated design system — OKLCH palette, WCAG-checked contrast, modular type &amp; spacing. Overall score <strong>%.1f / 10</strong>.</p>
</header>
`, esc(name), p.Mode, esc(string(t.Harmony)), esc(name), sc.Overall)

	// scorecard chips
	w(`<section><h2>Scorecard</h2><div class="chips">`)
	dims := append([]DimensionScore(nil), sc.Dimensions...)
	sort.SliceStable(dims, func(i, j int) bool { return dims[i].Score > dims[j].Score })
	for _, d := range dims {
		cls := "chip ok"
		if d.Score < 7 {
			cls = "chip warn"
		}
		if d.Score < 5 {
			cls = "chip bad"
		}
		fmt.Fprintf(&b, `<span class="%s">%s <b>%.1f</b></span>`, cls, esc(d.Name), d.Score)
	}
	w("</div></section>\n")

	// swatches
	w(`<section><h2>Color tokens</h2><div class="swatches">`)
	type sw struct{ name, hex, textOn string }
	swatches := []sw{
		{"bg", p.BG, p.FG}, {"surface", p.Surface, p.FG}, {"surface-2", p.Surface2, p.FG},
		{"border", p.Border, p.FG}, {"fg", p.FG, p.BG}, {"muted", p.Muted, p.BG},
		{"primary", p.Primary, p.PrimaryFg}, {"accent", p.Accent, p.AccentFg},
		{"success", p.Success, p.BG}, {"warning", p.Warning, p.BG},
		{"danger", p.Danger, p.BG}, {"info", p.Info, p.BG}, {"code", p.Code, p.FG},
	}
	for _, s := range swatches {
		fmt.Fprintf(&b, `<div class="swatch" style="background:%s;color:%s;border-color:var(--border)"><span class="sw-name">%s</span><span class="sw-hex">%s</span></div>`,
			s.hex, s.textOn, esc(s.name), esc(s.hex))
	}
	w("</div></section>\n")

	// type ramp
	w(`<section><h2>Type scale</h2><div class="type-ramp">`)
	for i := len(typeStepOrder) - 1; i >= 0; i-- {
		s := typeStepOrder[i]
		fmt.Fprintf(&b, `<div class="tr-row"><span class="tr-tag">%s · %gpx</span><span style="font-size:var(--text-%s);line-height:1.1">The quick brown fox</span></div>`,
			esc(s.Name), t.Type.Steps[s.Name], esc(s.Name))
	}
	w("</div></section>\n")

	// components
	w(`<section><h2>Components</h2><div class="comp-grid">`)
	w(`<div class="card"><h3>Card title</h3><p class="muted">Secondary copy uses the muted token and still clears AA contrast.</p>`)
	w(`<div class="btn-row"><button class="btn btn-primary">Primary</button><button class="btn btn-accent">Accent</button><button class="btn btn-ghost">Ghost</button></div></div>`)
	w(`<div class="card"><h3>Form</h3><label class="lbl">Email</label><input class="input" value="you@example.com"/><div class="btn-row"><button class="btn btn-primary">Save</button></div></div>`)
	w(`<div class="card"><h3>States</h3><div class="badges">`)
	for _, st := range [][2]string{{"success", p.Success}, {"warning", p.Warning}, {"danger", p.Danger}, {"info", p.Info}} {
		fmt.Fprintf(&b, `<span class="badge" style="color:%s;border-color:%s">%s</span>`, st[1], st[1], esc(st[0]))
	}
	w(`</div><pre class="code"><code>const ok = score &gt;= 9 // ship it</code></pre></div>`)
	w("</div></section>\n")

	// findings
	if f := sc.Findings(); len(f) > 0 {
		w(`<section><h2>Findings</h2><ul class="findings">`)
		for _, item := range f {
			fmt.Fprintf(&b, "<li>%s</li>", esc(item))
		}
		w("</ul></section>\n")
	}

	w("</div>\n</body>\n</html>\n")
	return b.String()
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

const previewBaseCSS = `
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--fg); font-family:var(--font-sans); line-height:1.6; font-size:16px; }
.wrap { max-width: 1080px; margin:0 auto; padding: 48px 28px 96px; }
h1 { font-size:var(--text-4xl); letter-spacing:-1px; margin:0 0 10px; }
h2 { font-size:var(--text-2xl); letter-spacing:-0.3px; margin:48px 0 16px; }
h3 { font-size:var(--text-lg); margin:0 0 8px; }
.muted, p.muted { color:var(--muted); }
.dh { border-bottom:1px solid var(--border); padding-bottom:28px; }
.dh-badge { display:inline-block; font-family:var(--font-mono); font-size:var(--text-xs); color:var(--primary); border:1px solid var(--border); border-radius:999px; padding:4px 12px; margin-bottom:14px; }
.dh-sub { color:var(--muted); max-width:64ch; }
.dh-sub strong { color:var(--fg); }
.chips { display:flex; flex-wrap:wrap; gap:8px; }
.chip { font-family:var(--font-mono); font-size:var(--text-xs); padding:5px 11px; border-radius:999px; border:1px solid var(--border); background:var(--surface); }
.chip b { margin-left:4px; }
.chip.ok b { color:var(--success); } .chip.warn b { color:var(--warning); } .chip.bad b { color:var(--danger); }
.swatches { display:grid; grid-template-columns:repeat(auto-fill, minmax(150px,1fr)); gap:12px; }
.swatch { border:1px solid; border-radius:var(--radius); padding:18px 16px; min-height:84px; display:flex; flex-direction:column; justify-content:space-between; }
.sw-name { font-weight:600; font-size:var(--text-sm); }
.sw-hex { font-family:var(--font-mono); font-size:var(--text-xs); opacity:0.85; }
.type-ramp { display:flex; flex-direction:column; gap:10px; }
.tr-row { display:flex; align-items:baseline; gap:18px; border-bottom:1px solid var(--border); padding-bottom:8px; }
.tr-tag { font-family:var(--font-mono); font-size:var(--text-xs); color:var(--muted); width:120px; flex:0 0 auto; }
.comp-grid { display:grid; grid-template-columns:repeat(auto-fit, minmax(260px,1fr)); gap:16px; }
.card { background:var(--surface); border:1px solid var(--border); border-radius:var(--radius); padding:22px; }
.btn-row { display:flex; gap:10px; flex-wrap:wrap; margin-top:14px; }
.btn { font-family:var(--font-sans); font-size:var(--text-sm); font-weight:600; padding:10px 18px; border-radius:calc(var(--radius) - 2px); border:1px solid var(--border); cursor:pointer; }
.btn-primary { background:var(--primary); color:var(--primary-fg); border-color:var(--primary); }
.btn-accent { background:var(--accent); color:var(--accent-fg); border-color:var(--accent); }
.btn-ghost { background:transparent; color:var(--fg); }
.lbl { display:block; font-size:var(--text-sm); color:var(--muted); margin-bottom:6px; }
.input { width:100%; background:var(--bg); color:var(--fg); border:1px solid var(--border); border-radius:calc(var(--radius) - 2px); padding:10px 12px; font-size:var(--text-base); }
.badges { display:flex; gap:8px; flex-wrap:wrap; }
.badge { font-family:var(--font-mono); font-size:var(--text-xs); padding:4px 10px; border:1px solid; border-radius:999px; }
.code { background:var(--code); border:1px solid var(--border); border-radius:var(--radius); padding:14px 16px; overflow:auto; margin-top:14px; }
.code code { font-family:var(--font-mono); font-size:var(--text-sm); color:var(--fg); }
.findings { color:var(--muted); font-size:var(--text-sm); }
.findings li { margin:6px 0; }
`
