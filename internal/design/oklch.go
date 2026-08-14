// Package design is the color-systems & design-system engine behind the
// `ngtm design` vertical. It generates accessible, theory-grounded design
// systems (OKLCH palettes, WCAG-checked contrast, modular type/spacing scales)
// and scores them /10 against a computable rubric — the deterministic, offline
// channel of the design self-review loop.
//
// The color core uses OKLCH (the polar form of Björn Ottosson's Oklab), a
// perceptually-uniform space: equal numeric steps in lightness/chroma read as
// equal visual steps, which is what makes generated ramps look even and lets the
// scorer reason about palettes the way a human eye does. sRGB/HSL cannot do this.
package design

import "math"

// RGB is a linear-decoded? No — RGB holds non-linear sRGB channels in [0,1].
type RGB struct{ R, G, B float64 }

// OKLab is the Cartesian Oklab form: L in [0,1], a/b unbounded (typically ~±0.4).
type OKLab struct{ L, A, B float64 }

// OKLCH is the polar Oklab form: L lightness [0,1], C chroma (≥0), H hue degrees [0,360).
type OKLCH struct{ L, C, H float64 }

// --- sRGB transfer function (gamma) ---

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// --- linear sRGB <-> Oklab (Ottosson matrices) ---

func linearRGBToOKLab(r, g, b float64) OKLab {
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	return OKLab{
		L: 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		A: 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		B: 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_,
	}
}

func okLabToLinearRGB(lab OKLab) (r, g, b float64) {
	l_ := lab.L + 0.3963377774*lab.A + 0.2158037573*lab.B
	m_ := lab.L - 0.1055613458*lab.A - 0.0638541728*lab.B
	s_ := lab.L - 0.0894841775*lab.A - 1.2914855480*lab.B

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return r, g, b
}

// --- OKLab <-> OKLCH (Cartesian <-> polar) ---

// ToOKLCH converts an Oklab color to its polar OKLCH form.
func (lab OKLab) ToOKLCH() OKLCH {
	c := math.Hypot(lab.A, lab.B)
	h := math.Atan2(lab.B, lab.A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return OKLCH{L: lab.L, C: c, H: h}
}

// ToOKLab converts a polar OKLCH color back to Cartesian Oklab.
func (c OKLCH) ToOKLab() OKLab {
	rad := c.H * math.Pi / 180
	return OKLab{L: c.L, A: c.C * math.Cos(rad), B: c.C * math.Sin(rad)}
}

// --- top-level conversions ---

// FromHex parses "#rrggbb" / "rrggbb" / "#rgb" into sRGB. ok=false on malformed input.
func FromHex(s string) (RGB, bool) {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	switch len(s) {
	case 3:
		// shorthand #rgb -> #rrggbb
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return RGB{}, false
	}
	v, ok := parseHex6(s)
	if !ok {
		return RGB{}, false
	}
	return RGB{
		R: float64((v>>16)&0xff) / 255,
		G: float64((v>>8)&0xff) / 255,
		B: float64(v&0xff) / 255,
	}, true
}

func parseHex6(s string) (uint32, bool) {
	var v uint32
	for i := 0; i < 6; i++ {
		c := s[i]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}

// Hex renders an sRGB color as "#rrggbb", clamping out-of-range channels.
func (c RGB) Hex() string {
	return "#" + hex2(c.R) + hex2(c.G) + hex2(c.B)
}

func hex2(f float64) string {
	v := int(math.Round(clamp01(f) * 255))
	const digits = "0123456789abcdef"
	return string([]byte{digits[(v>>4)&0xf], digits[v&0xf]})
}

// ToOKLCH converts an sRGB color to OKLCH.
func (c RGB) ToOKLCH() OKLCH {
	lin := linearRGBToOKLab(srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B))
	return lin.ToOKLCH()
}

// ToRGB converts an OKLCH color to sRGB WITHOUT gamut mapping (may be out of range).
// Use ToRGBGamut for a displayable color.
func (c OKLCH) ToRGB() RGB {
	lr, lg, lb := okLabToLinearRGB(c.ToOKLab())
	return RGB{R: linearToSRGB(lr), G: linearToSRGB(lg), B: linearToSRGB(lb)}
}

// InGamut reports whether this OKLCH color lands inside the sRGB cube.
func (c OKLCH) InGamut() bool {
	lr, lg, lb := okLabToLinearRGB(c.ToOKLab())
	const eps = 1e-4
	return lr >= -eps && lr <= 1+eps &&
		lg >= -eps && lg <= 1+eps &&
		lb >= -eps && lb <= 1+eps
}

// ToRGBGamut returns a displayable sRGB color. If the requested OKLCH color is
// outside sRGB, chroma is reduced (preserving L and H) via binary search until it
// fits — the perceptually-correct gamut mapping, far better than clipping channels
// (which shifts hue and lightness). Falls back to a clamp on the residual.
func (c OKLCH) ToRGBGamut() RGB {
	if c.InGamut() {
		r := c.ToRGB()
		return RGB{clamp01(r.R), clamp01(r.G), clamp01(r.B)}
	}
	lo, hi := 0.0, c.C
	for i := 0; i < 24; i++ {
		mid := (lo + hi) / 2
		if (OKLCH{L: c.L, C: mid, H: c.H}).InGamut() {
			lo = mid
		} else {
			hi = mid
		}
	}
	r := (OKLCH{L: c.L, C: lo, H: c.H}).ToRGB()
	return RGB{clamp01(r.R), clamp01(r.G), clamp01(r.B)}
}

// HexOKLCH is the convenience round-trip: OKLCH -> gamut-mapped sRGB hex string.
func (c OKLCH) HexOKLCH() string { return c.ToRGBGamut().Hex() }

// --- small helpers ---

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// wrapHue normalizes a hue to [0,360).
func wrapHue(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	return h
}

// hueDiff returns the smallest absolute angular distance between two hues (0..180).
func hueDiff(a, b float64) float64 {
	d := math.Abs(wrapHue(a) - wrapHue(b))
	if d > 180 {
		d = 360 - d
	}
	return d
}
