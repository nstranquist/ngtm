package design

import (
	"strings"
)

// Options drives theme generation.
type Options struct {
	Name        string          // brand/product name (also seeds hue when Seed is empty)
	Seed        string          // optional brand color "#rrggbb"; derived from Name when empty
	Harmony     HarmonyStrategy // accent scheme; default analogous
	Modes       []Mode          // palettes to build; default [dark]
	TypeRatio   float64         // modular type ratio; default 1.25
	SpacingBase float64         // spacing unit px; default 4
	Radius      float64         // base radius px; default 10
}

// Generate builds a complete design system from Options, applying color theory
// (OKLCH harmony) and accessibility targeting (WCAG-driven lightness search)
// so the result passes contrast by construction, not by luck.
func Generate(opts Options) Theme {
	return GenerateSeeded(resolveSeed(opts), opts)
}

// GenerateSeeded builds a theme from an explicit OKLCH seed (the tuning loop
// uses this to explore hue-shifted variants of the brand seed). Generate is the
// usual entry point; it resolves the seed from Options first.
func GenerateSeeded(seed OKLCH, opts Options) Theme {
	harmony := opts.Harmony
	if harmony == "" {
		harmony = HarmonyAnalogous
	}
	ratio := opts.TypeRatio
	if ratio <= 1 {
		ratio = 1.25
	}
	spacing := opts.SpacingBase
	if spacing <= 0 {
		spacing = 4
	}
	radius := opts.Radius
	if radius <= 0 {
		radius = 10
	}
	modes := opts.Modes
	if len(modes) == 0 {
		modes = []Mode{ModeDark}
	}

	t := Theme{
		Name:     strings.TrimSpace(opts.Name),
		Seed:     seed,
		Harmony:  harmony,
		Type:     NewTypeScale(16, ratio),
		Spacing:  NewSpacingScale(spacing),
		Radius:   radius,
		FontSans: defaultSans,
		FontMono: defaultMono,
	}
	for _, m := range modes {
		p := buildPalette(seed, harmony, m)
		switch m {
		case ModeLight:
			t.Light = &p
		default:
			t.Dark = p
		}
	}
	// Always ensure a dark palette exists (it's the default render target).
	if (t.Dark == Palette{}) {
		t.Dark = buildPalette(seed, harmony, ModeDark)
	}
	return t
}

// resolveSeed turns Options into a brand OKLCH seed.
func resolveSeed(opts Options) OKLCH {
	if c, ok := FromHex(strings.TrimSpace(opts.Seed)); ok {
		s := c.ToOKLCH()
		// Tame extreme seeds into a usable brand range without losing identity.
		s.C = clampF(s.C, 0.04, 0.22)
		if s.L < 0.35 {
			s.L = 0.55
		}
		if s.L > 0.85 {
			s.L = 0.72
		}
		return s
	}
	return seedFromName(opts.Name)
}

// seedFromName derives a stable, pleasant brand hue from the product name via
// an FNV-1a hash — deterministic so the same name always yields the same hue,
// which the tuning loop can then explore around.
func seedFromName(name string) OKLCH {
	var h uint32 = 2166136261
	for _, b := range []byte(strings.ToLower(strings.TrimSpace(name))) {
		h = (h ^ uint32(b)) * 16777619
	}
	hue := float64(h % 360)
	return OKLCH{L: 0.70, C: 0.14, H: hue}
}

// buildPalette is the contrast-targeting generator for one mode. Every text and
// action token is derived to clear its WCAG floor against the *worst-case*
// surface it can sit on, so the result passes AA by construction.
func buildPalette(seed OKLCH, harmony HarmonyStrategy, mode Mode) Palette {
	hue := seed.H
	accentHue := wrapHue(hue + harmony.accentHueOffset())
	if harmony == HarmonyMono {
		accentHue = hue
	}
	brandC := clampF(seed.C, 0.06, 0.20)
	neutralC := minF(seed.C*0.12, 0.018) // subtle brand tint in the grays

	p := Palette{Mode: mode}

	var bg, surface, surface2, border, code RGB
	var fgL, startPrim, startAcc float64
	if mode == ModeLight {
		bg = tint(0.985, neutralC*0.6, hue)
		surface = tint(0.965, neutralC*0.8, hue)
		surface2 = tint(0.935, neutralC, hue)
		border = tint(0.87, neutralC, hue)
		code = tint(0.955, neutralC, hue)
		fgL, startPrim, startAcc = 0.23, 0.55, 0.55
		p.FG = hexOf(tint(fgL, neutralC*1.4, hue))
	} else {
		bg = tint(0.17, neutralC*0.8, hue)
		surface = tint(0.215, neutralC, hue)
		surface2 = tint(0.265, neutralC, hue)
		border = tint(0.36, neutralC, hue)
		code = tint(0.195, neutralC, hue)
		fgL, startPrim, startAcc = 0.945, 0.74, 0.78
		p.FG = hexOf(tint(fgL, neutralC*0.7, hue))
	}
	p.BG, p.Surface, p.Surface2 = hexOf(bg), hexOf(surface), hexOf(surface2)
	p.Border, p.Code = hexOf(border), hexOf(code)

	// muted text appears on bg AND raised surfaces; target the lightest one
	// (surface2 in dark, where light text has the least contrast) so it passes
	// everywhere while staying the dimmest accessible neutral.
	worstSurface := surface2
	p.Muted = hexOf(dimmestNeutral(worstSurface, hue, WCAGTextAA, mode))

	// action fills must both stand out on the page and carry a legible label.
	prim, primFg := actionFill(bg, startPrim, brandC, hue, mode)
	acc, accFg := actionFill(bg, startAcc, brandC*0.95, accentHue, mode)
	p.Primary, p.PrimaryFg = hexOf(prim), hexOf(primFg)
	p.Accent, p.AccentFg = hexOf(acc), hexOf(accFg)

	p.Success = hexOf(semantic(bg, 145, mode))
	p.Warning = hexOf(semantic(bg, 80, mode))
	p.Danger = hexOf(semantic(bg, 27, mode))
	p.Info = hexOf(semantic(bg, 235, mode))
	return p
}

// actionFill returns a saturated fill color and a legible label color for it.
// It searches lightness so the fill clears the non-text floor vs the page bg AND
// the better of black/white text clears AA on the fill — guaranteeing buttons
// are both visible and readable, the failure mode pure mid-tones fall into.
func actionFill(bg RGB, startL, C, hue float64, mode Mode) (fill, label RGB) {
	L := startL
	const step = 0.01
	for i := 0; i < 130; i++ {
		fill = tint(L, C, hue)
		label = bestTextOn(fill)
		if ContrastRatio(label, fill) >= WCAGTextAA && ContrastRatio(fill, bg) >= WCAGLargeAA {
			return fill, label
		}
		if mode == ModeLight {
			L -= step // darker fill → white label gains contrast
		} else {
			L += step // brighter fill → dark label gains contrast
		}
		if L <= 0.05 || L >= 0.97 {
			break
		}
	}
	L = clampF(L, 0.05, 0.97)
	fill = tint(L, C, hue)
	return fill, bestTextOn(fill)
}

// --- generation helpers ---

// tint builds a gamut-mapped sRGB color from OKLCH components.
func tint(L, C, H float64) RGB { return OKLCH{L: L, C: C, H: H}.ToRGBGamut() }

func hexOf(c RGB) string { return c.Hex() }

// dimmestNeutral finds the neutral nearest the background (most subtle) that
// still meets minRatio — used for secondary "muted" text so it reads as
// secondary while remaining accessible.
func dimmestNeutral(bg RGB, hue, minRatio float64, mode Mode) RGB {
	if mode == ModeDark {
		for L := 0.40; L <= 1.0; L += 0.004 {
			c := tint(L, 0.012, hue)
			if ContrastRatio(c, bg) >= minRatio {
				return c
			}
		}
		return tint(1, 0.012, hue)
	}
	for L := 0.78; L >= 0.0; L -= 0.004 {
		c := tint(L, 0.014, hue)
		if ContrastRatio(c, bg) >= minRatio {
			return c
		}
	}
	return tint(0, 0.014, hue)
}

// vividWithFloor returns a saturated color at the requested L/C/H, nudging
// lightness toward the legible direction until it clears minRatio vs bg.
func vividWithFloor(bg RGB, L, C, hue, minRatio float64, mode Mode) RGB {
	c := tint(L, C, hue)
	for i := 0; i < 80 && ContrastRatio(c, bg) < minRatio; i++ {
		if mode == ModeDark {
			L += 0.008
		} else {
			L -= 0.008
		}
		if L >= 1 || L <= 0 {
			break
		}
		c = tint(L, C, hue)
	}
	return c
}

// semantic builds a state color at a fixed hue, contrast-floored vs bg.
func semantic(bg RGB, hue float64, mode Mode) RGB {
	if mode == ModeLight {
		return vividWithFloor(bg, 0.52, 0.13, hue, WCAGLargeAA, mode)
	}
	return vividWithFloor(bg, 0.72, 0.14, hue, WCAGLargeAA, mode)
}

// bestTextOn picks near-black or near-white for text on a colored fill,
// whichever yields more contrast.
func bestTextOn(bg RGB) RGB {
	white := RGB{R: 0.98, G: 0.98, B: 0.98}
	black := RGB{R: 0.09, G: 0.09, B: 0.10}
	if ContrastRatio(white, bg) >= ContrastRatio(black, bg) {
		return white
	}
	return black
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
