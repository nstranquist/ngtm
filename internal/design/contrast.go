package design

import "math"

// WCAG contrast thresholds (WCAG 2.1, SC 1.4.3 / 1.4.11).
const (
	WCAGTextAA    = 4.5 // normal body text
	WCAGTextAAA   = 7.0 // enhanced body text
	WCAGLargeAA   = 3.0 // large text (>=18.66px bold / 24px) and UI components
	WCAGNonTextAA = 3.0 // SC 1.4.11 non-text contrast (borders, icons, states)
)

// relativeLuminance is the WCAG 2.x relative luminance of an sRGB color.
func relativeLuminance(c RGB) float64 {
	r := srgbToLinear(clamp01(c.R))
	g := srgbToLinear(clamp01(c.G))
	b := srgbToLinear(clamp01(c.B))
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// ContrastRatio returns the WCAG 2.x contrast ratio between two colors in [1,21].
func ContrastRatio(fg, bg RGB) float64 {
	l1 := relativeLuminance(fg)
	l2 := relativeLuminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// APCALc returns an approximate APCA lightness-contrast value (Lc), the WCAG 3
// successor metric. Sign indicates polarity (positive = dark text on light bg).
// Magnitude guidance: |Lc| >= 75 for body text, >= 60 for larger/secondary,
// >= 45 for large headings, >= 30 for non-text/disabled. This is the well-known
// 0.0.98G approximation — reported alongside WCAG, not used as the hard gate.
func APCALc(text, bg RGB) float64 {
	const (
		sa, ba      = 0.55, 0.62
		sw, bw      = 0.57, 0.56
		blkThresh   = 0.022
		blkClamp    = 1.414
		scale       = 1.14
		loClip      = 0.1
		deltaYmin   = 0.0005
		loBoWoffset = 0.027
		loWoBoffset = 0.027
	)
	lum := func(c RGB) float64 {
		r := math.Pow(clamp01(c.R), 2.4)
		g := math.Pow(clamp01(c.G), 2.4)
		b := math.Pow(clamp01(c.B), 2.4)
		return 0.2126729*r + 0.7151522*g + 0.0721750*b
	}
	softClamp := func(y float64) float64 {
		if y < blkThresh {
			return y + math.Pow(blkThresh-y, blkClamp)
		}
		return y
	}
	ytxt := softClamp(lum(text))
	ybg := softClamp(lum(bg))
	if math.Abs(ytxt-ybg) < deltaYmin {
		return 0
	}
	var lc float64
	if ybg > ytxt { // normal polarity: dark text on light bg
		lc = (math.Pow(ybg, ba) - math.Pow(ytxt, sa)) * scale
		if lc < loClip {
			return 0
		}
		lc -= loBoWoffset
		return lc * 100
	}
	// reverse polarity: light text on dark bg
	lc = (math.Pow(ybg, bw) - math.Pow(ytxt, sw)) * scale
	if lc > -loClip {
		return 0
	}
	lc += loWoBoffset
	return lc * 100
}

// ContrastVerdict classifies a ratio for a given use.
type ContrastVerdict struct {
	Ratio   float64
	APCA    float64
	PassAA  bool // body text AA (4.5)
	PassAAA bool // body text AAA (7.0)
	PassUI  bool // large text / UI / non-text (3.0)
}

// Evaluate returns the contrast verdict for fg-on-bg.
func Evaluate(fg, bg RGB) ContrastVerdict {
	r := ContrastRatio(fg, bg)
	return ContrastVerdict{
		Ratio:   r,
		APCA:    APCALc(fg, bg),
		PassAA:  r >= WCAGTextAA,
		PassAAA: r >= WCAGTextAAA,
		PassUI:  r >= WCAGLargeAA,
	}
}
