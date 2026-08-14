package design

import (
	"math"
	"testing"
)

func TestHexRoundTrip(t *testing.T) {
	cases := []string{"#000000", "#ffffff", "#7ee787", "#79c0ff", "#0d1117", "#f0883e"}
	for _, h := range cases {
		c, ok := FromHex(h)
		if !ok {
			t.Fatalf("FromHex(%q) failed", h)
		}
		if got := c.Hex(); got != h {
			t.Errorf("hex round-trip %q -> %q", h, got)
		}
	}
}

func TestShorthandHex(t *testing.T) {
	c, ok := FromHex("#fff")
	if !ok || c.Hex() != "#ffffff" {
		t.Fatalf("shorthand #fff -> %v %q", ok, c.Hex())
	}
}

func TestOKLCHRoundTrip(t *testing.T) {
	// sRGB -> OKLCH -> sRGB should be near-identity for in-gamut colors.
	cases := []string{"#7ee787", "#79c0ff", "#f0883e", "#888888", "#123456"}
	for _, h := range cases {
		c, _ := FromHex(h)
		back := c.ToOKLCH().ToRGBGamut()
		if d := chanDist(c, back); d > 0.01 {
			t.Errorf("OKLCH round-trip %q drifted by %.4f (%s -> %s)", h, d, c.Hex(), back.Hex())
		}
	}
}

func TestWhiteIsLightestBlackIsDarkest(t *testing.T) {
	white, _ := FromHex("#ffffff")
	black, _ := FromHex("#000000")
	if l := white.ToOKLCH().L; math.Abs(l-1) > 0.02 {
		t.Errorf("white L = %.4f, want ~1.0", l)
	}
	if l := black.ToOKLCH().L; l > 0.02 {
		t.Errorf("black L = %.4f, want ~0.0", l)
	}
}

func TestContrastAnchors(t *testing.T) {
	white, _ := FromHex("#ffffff")
	black, _ := FromHex("#000000")
	if r := ContrastRatio(black, white); math.Abs(r-21) > 0.05 {
		t.Errorf("black/white contrast = %.3f, want 21.0", r)
	}
	if r := ContrastRatio(white, white); math.Abs(r-1) > 0.001 {
		t.Errorf("white/white contrast = %.3f, want 1.0", r)
	}
	// order independence
	if a, b := ContrastRatio(black, white), ContrastRatio(white, black); math.Abs(a-b) > 1e-9 {
		t.Errorf("contrast not symmetric: %.4f vs %.4f", a, b)
	}
}

func TestGamutMappingPreservesHue(t *testing.T) {
	// Request an absurdly high chroma; gamut mapping must keep it displayable
	// and not wildly shift hue.
	want := OKLCH{L: 0.65, C: 0.5, H: 150} // far outside sRGB
	got := want.ToRGBGamut().ToOKLCH()
	if !want.InGamutAfterMap(got) {
		t.Logf("mapped to L=%.3f C=%.3f H=%.3f", got.L, got.C, got.H)
	}
	if d := hueDiff(want.H, got.H); d > 8 {
		t.Errorf("gamut map shifted hue by %.1f deg (>8)", d)
	}
	if math.Abs(want.L-got.L) > 0.05 {
		t.Errorf("gamut map shifted L by %.3f (>0.05)", math.Abs(want.L-got.L))
	}
}

func TestEvaluatePolarity(t *testing.T) {
	white, _ := FromHex("#ffffff")
	black, _ := FromHex("#000000")
	// dark text on light bg => positive APCA
	if lc := APCALc(black, white); lc <= 0 {
		t.Errorf("dark-on-light APCA = %.1f, want > 0", lc)
	}
	// light text on dark bg => negative APCA
	if lc := APCALc(white, black); lc >= 0 {
		t.Errorf("light-on-dark APCA = %.1f, want < 0", lc)
	}
}

func chanDist(a, b RGB) float64 {
	return math.Sqrt((a.R-b.R)*(a.R-b.R) + (a.G-b.G)*(a.G-b.G) + (a.B-b.B)*(a.B-b.B))
}

// InGamutAfterMap is a tiny helper for the test only.
func (c OKLCH) InGamutAfterMap(got OKLCH) bool { return got.C <= c.C+1e-6 }
