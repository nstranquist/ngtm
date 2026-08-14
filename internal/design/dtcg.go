package design

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// dtcg.go emits a generated Theme as a W3C / Design Tokens Community Group
// (DTCG) token document — the stable interchange format (first stable version
// Oct 2025) that Style Dictionary and Panda CSS consume natively. This is the
// "token spine" seam: `ngtm design --format dtcg` produces this JSON, then a
// downstream Style Dictionary build fans it out to CSS custom properties (web),
// JS objects, and a NativeWind theme (mobile) — one source of truth across every
// frontend lane.
//
// Color tokens carry an OKLCH string as their `$value` (perceptual, P3-capable,
// universally supported in evergreen browsers since 2023) with the gamut-mapped
// sRGB hex preserved under `$extensions` for any consumer that needs a fallback.
// Dimension/fontFamily values use the string form for maximum Style-Dictionary
// portability (the engine's own CSSVars output uses the same vocabulary).

// colorTokenOrder is the canonical semantic-token vocabulary, matching CSSVars
// so the generated `tokens.css` var names (--bg, --surface, …) are stable.
var colorTokenOrder = []struct {
	Name string
	Get  func(p Palette) string
}{
	{"bg", func(p Palette) string { return p.BG }},
	{"surface", func(p Palette) string { return p.Surface }},
	{"surface-2", func(p Palette) string { return p.Surface2 }},
	{"border", func(p Palette) string { return p.Border }},
	{"fg", func(p Palette) string { return p.FG }},
	{"muted", func(p Palette) string { return p.Muted }},
	{"primary", func(p Palette) string { return p.Primary }},
	{"primary-fg", func(p Palette) string { return p.PrimaryFg }},
	{"accent", func(p Palette) string { return p.Accent }},
	{"accent-fg", func(p Palette) string { return p.AccentFg }},
	{"success", func(p Palette) string { return p.Success }},
	{"warning", func(p Palette) string { return p.Warning }},
	{"danger", func(p Palette) string { return p.Danger }},
	{"info", func(p Palette) string { return p.Info }},
	{"code", func(p Palette) string { return p.Code }},
}

// DTCGDocument builds the DTCG token tree for a theme as an ordered Go value
// ready for JSON encoding. Always includes the dark palette; includes the light
// palette under color.light when present (else falls back to dark for both so the
// document is self-consistent).
func DTCGDocument(t Theme) map[string]any {
	name := t.Name
	if name == "" {
		name = "Untitled"
	}

	light := t.Dark
	if t.Light != nil {
		light = *t.Light
	}

	doc := map[string]any{
		"$description": fmt.Sprintf("%s — generated OKLCH design tokens (%s harmony). W3C/DTCG format.", name, t.Harmony),
		"$extensions": map[string]any{
			"tools.nicos.design": map[string]any{
				"name":      name,
				"harmony":   string(t.Harmony),
				"seed":      seedString(t.Seed),
				"generator": "ngtm design --format dtcg",
			},
		},
		"color": map[string]any{
			"$type":        "color",
			"$description": "Semantic color tokens. light = :root, dark = [data-theme=dark].",
			"light":        colorGroup(light),
			"dark":         colorGroup(t.Dark),
		},
		"radius": map[string]any{
			"$type":  "dimension",
			"$value": pxString(t.Radius),
		},
		"font": map[string]any{
			"$type": "fontFamily",
			"sans":  map[string]any{"$value": t.FontSans},
			"mono":  map[string]any{"$value": t.FontMono},
		},
		"text":  typeGroup(t.Type),
		"space": spaceGroup(t.Spacing),
	}
	return doc
}

// colorGroup builds one mode's color tokens with OKLCH `$value` and hex fallback.
func colorGroup(p Palette) map[string]any {
	g := map[string]any{}
	for _, c := range colorTokenOrder {
		hex := c.Get(p)
		tok := map[string]any{"$value": hex}
		if ok, exists := oklchFromHex(hex); exists {
			tok["$value"] = ok
			tok["$extensions"] = map[string]any{"tools.nicos.hex": hex}
		}
		g[c.Name] = tok
	}
	return g
}

// typeGroup builds the modular type scale as rem dimension tokens.
func typeGroup(ts TypeScale) map[string]any {
	g := map[string]any{"$type": "dimension"}
	for _, s := range typeStepOrder {
		px := ts.Steps[s.Name]
		g[s.Name] = map[string]any{"$value": remString(px / 16)}
	}
	return g
}

// spaceGroup builds the spacing ramp as px dimension tokens, 1-indexed to match
// CSSVars (--space-1 … --space-N).
func spaceGroup(ss SpacingScale) map[string]any {
	g := map[string]any{"$type": "dimension"}
	for i, v := range ss.Steps {
		g[strconv.Itoa(i+1)] = map[string]any{"$value": pxString(v)}
	}
	return g
}

// DTCG renders the DTCG token document as indented JSON.
func DTCG(t Theme) ([]byte, error) {
	b, err := json.MarshalIndent(DTCGDocument(t), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// --- value formatting helpers ---

// oklchFromHex converts an "#rrggbb" string to an `oklch(L C H)` CSS string.
func oklchFromHex(hex string) (string, bool) {
	rgb, ok := FromHex(hex)
	if !ok {
		return "", false
	}
	o := rgb.ToOKLCH()
	return fmt.Sprintf("oklch(%s %s %s)", trimNum(o.L, 4), trimNum(o.C, 4), trimNum(o.H, 2)), true
}

func pxString(v float64) string  { return trimNum(v, 2) + "px" }
func remString(v float64) string { return trimNum(v, 4) + "rem" }

// trimNum formats a float with up to prec decimals and trims trailing zeros so
// the emitted tokens read cleanly (8px not 8.00px, oklch(0.62 0.19 29.2)).
func trimNum(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// seedString renders the OKLCH seed compactly for provenance metadata.
func seedString(o OKLCH) string {
	return fmt.Sprintf("oklch(%s %s %s)", trimNum(o.L, 4), trimNum(o.C, 4), trimNum(o.H, 2))
}
