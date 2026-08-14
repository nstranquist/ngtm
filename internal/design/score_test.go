package design

import (
	"context"
	"testing"
)

// criticalTextLabels are the contrast checks that MUST pass AA for a theme to
// be considered accessible (the non-text/large ones are softer).
var criticalTextLabels = map[string]bool{
	"body text on bg":       true,
	"body text on surface":  true,
	"muted text on bg":      true,
	"muted text on surface": true,
	"primary button label":  true,
	"accent button label":   true,
}

func TestGeneratedThemesPassAAByConstruction(t *testing.T) {
	cases := []Options{
		{Name: "garrid"},
		{Name: "Cadence", Seed: "#7ee787"},
		{Name: "nvault", Seed: "#79c0ff"},
		{Name: "acme", Seed: "#f0883e", Harmony: HarmonyComplementary},
		{Name: "gray-brand", Seed: "#888888"},
		{Name: "deep", Seed: "#123456", Harmony: HarmonyTriadic, Modes: []Mode{ModeDark, ModeLight}},
	}
	for _, o := range cases {
		o.Modes = appendMode(o.Modes, ModeDark, ModeLight)
		theme := Generate(o)
		for _, mode := range []Mode{ModeDark, ModeLight} {
			sc := Score(theme, mode)
			for _, c := range sc.Contrast {
				if criticalTextLabels[c.Label] && !c.Pass {
					t.Errorf("%s [%s]: %q = %.2f:1, below AA %.1f", o.Name, mode, c.Label, c.Ratio, c.Need)
				}
			}
			t.Logf("%-11s %-5s overall=%.2f  (%s)", o.Name, mode, sc.Overall, theme.Harmony)
		}
	}
}

func TestTuneSelectsBest(t *testing.T) {
	res, err := Tune(context.Background(), TuneOptions{
		Base:      Options{Name: "garrid", Modes: []Mode{ModeDark}},
		ScoreMode: ModeDark,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.All) == 0 {
		t.Fatal("no candidates evaluated")
	}
	for _, c := range res.All {
		if c.Score > res.Best.Score {
			t.Errorf("candidate round %d scored %.2f > best %.2f", c.Round, c.Score, res.Best.Score)
		}
	}
	if res.Best.Score < 8.5 {
		t.Errorf("best score %.2f unexpectedly low", res.Best.Score)
	}
	t.Logf("TUNE: %s", res.SummaryLine())
	for _, c := range res.TopN(3) {
		t.Logf("  top: %.2f  %s hue%+.0f ratio %.3f", c.Score, c.Params.Harmony, c.Params.HueShift, c.Params.TypeRatio)
	}
}

func TestHarmonyRespected(t *testing.T) {
	theme := Generate(Options{Name: "x", Seed: "#3366cc", Harmony: HarmonyComplementary})
	prim, _ := FromHex(theme.Dark.Primary)
	acc, _ := FromHex(theme.Dark.Accent)
	gap := hueDiff(prim.ToOKLCH().H, acc.ToOKLCH().H)
	if gap < 150 {
		t.Errorf("complementary accent gap %.0f°, expected ~180°", gap)
	}
}

func appendMode(ms []Mode, add ...Mode) []Mode {
	have := map[Mode]bool{}
	for _, m := range ms {
		have[m] = true
	}
	for _, m := range add {
		if !have[m] {
			ms = append(ms, m)
			have[m] = true
		}
	}
	return ms
}
