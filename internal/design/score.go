package design

import (
	"fmt"
	"math"
	"sort"
)

// ContrastCheck is one foreground/background pairing the scorer evaluated.
type ContrastCheck struct {
	Label string  `json:"label"`
	FG    string  `json:"fg"`
	BG    string  `json:"bg"`
	Ratio float64 `json:"ratio"`
	APCA  float64 `json:"apca"`
	Need  float64 `json:"need"`
	Pass  bool    `json:"pass"`
}

// DimensionScore is one rubric dimension's result.
type DimensionScore struct {
	Name     string   `json:"name"`
	Weight   float64  `json:"weight"`
	Score    float64  `json:"score"` // 0..10
	Findings []string `json:"findings,omitempty"`
}

// Scorecard is the full /10 evaluation of a palette.
type Scorecard struct {
	Mode       Mode             `json:"mode"`
	Overall    float64          `json:"overall"` // 0..10, weighted
	Dimensions []DimensionScore `json:"dimensions"`
	Contrast   []ContrastCheck  `json:"contrast"`
}

// Findings flattens all dimension findings, worst dimensions first.
func (s Scorecard) Findings() []string {
	dims := append([]DimensionScore(nil), s.Dimensions...)
	sort.SliceStable(dims, func(i, j int) bool { return dims[i].Score < dims[j].Score })
	var out []string
	for _, d := range dims {
		for _, f := range d.Findings {
			out = append(out, fmt.Sprintf("[%s] %s", d.Name, f))
		}
	}
	return out
}

// Score evaluates one mode's palette against the rubric. The weights encode the
// priority order: accessibility dominates, then harmony and ramp quality, then
// the scale/coverage hygiene checks. Every sub-score is computed (not subjective),
// which is what makes the tuning loop able to "seek the best possible target".
func Score(t Theme, mode Mode) Scorecard {
	p := t.Dark
	if mode == ModeLight && t.Light != nil {
		p = *t.Light
	}
	sc := Scorecard{Mode: p.Mode}

	acc, checks := scoreAccessibility(p)
	sc.Contrast = checks
	dims := []DimensionScore{
		acc,                     // 0.30
		scoreHarmony(t, p),      // 0.15
		scoreNeutralRamp(p),     // 0.12
		scoreStateCoverage(p),   // 0.11
		scoreColorDiscipline(p), // 0.10
		scoreTypeScale(t.Type),  // 0.08
		scoreSpacing(t.Spacing), // 0.07
		scoreCohesion(t, p),     // 0.07
	}
	var wsum, w float64
	for _, d := range dims {
		wsum += d.Score * d.Weight
		w += d.Weight
	}
	sc.Dimensions = dims
	if w > 0 {
		sc.Overall = round2(wsum / w)
	}
	return sc
}

// --- dimension scorers ---

func scoreAccessibility(p Palette) (DimensionScore, []ContrastCheck) {
	d := DimensionScore{Name: "accessibility", Weight: 0.30}
	type pair struct {
		label   string
		fg, bg  string
		need    float64
		nonText bool
	}
	pairs := []pair{
		{"body text on bg", p.FG, p.BG, WCAGTextAA, false},
		{"body text on surface", p.FG, p.Surface, WCAGTextAA, false},
		{"muted text on bg", p.Muted, p.BG, WCAGTextAA, false},
		{"muted text on surface", p.Muted, p.Surface, WCAGTextAA, false},
		{"primary button label", p.PrimaryFg, p.Primary, WCAGTextAA, false},
		{"accent button label", p.AccentFg, p.Accent, WCAGTextAA, false},
		{"primary as link/large", p.Primary, p.BG, WCAGLargeAA, true},
		{"danger as text", p.Danger, p.BG, WCAGLargeAA, true},
		{"success as text", p.Success, p.BG, WCAGLargeAA, true},
		{"border on surface", p.Border, p.Surface, WCAGNonTextAA, true},
	}
	var checks []ContrastCheck
	var textPass, textTotal, aaaCount, aaaText int
	for _, pr := range pairs {
		fg, _ := FromHex(pr.fg)
		bg, _ := FromHex(pr.bg)
		r := ContrastRatio(fg, bg)
		pass := r >= pr.need
		checks = append(checks, ContrastCheck{
			Label: pr.label, FG: pr.fg, BG: pr.bg,
			Ratio: round2(r), APCA: round1(APCALc(fg, bg)), Need: pr.need, Pass: pass,
		})
		if !pr.nonText {
			textTotal++
			if pass {
				textPass++
			}
			aaaText++
			if r >= WCAGTextAAA {
				aaaCount++
			}
			if !pass {
				d.Findings = append(d.Findings, fmt.Sprintf("%s is %.2f:1 — below %.1f:1 AA", pr.label, r, pr.need))
			}
		} else if !pass {
			d.Findings = append(d.Findings, fmt.Sprintf("%s is %.2f:1 — below %.1f:1", pr.label, r, pr.need))
		}
	}
	// Base on AA text pass-rate; reward AAA coverage; small credit for non-text passes.
	aaFrac := 1.0
	if textTotal > 0 {
		aaFrac = float64(textPass) / float64(textTotal)
	}
	aaaFrac := 0.0
	if aaaText > 0 {
		aaaFrac = float64(aaaCount) / float64(aaaText)
	}
	nonTextPass, nonTextTotal := 0, 0
	for _, c := range checks {
		for _, pr := range pairs {
			if pr.label == c.Label && pr.nonText {
				nonTextTotal++
				if c.Pass {
					nonTextPass++
				}
			}
		}
	}
	nonTextFrac := 1.0
	if nonTextTotal > 0 {
		nonTextFrac = float64(nonTextPass) / float64(nonTextTotal)
	}
	// 8 pts for AA text, up to +1.2 AAA, +0.8 non-text.
	d.Score = clampScore(8*aaFrac + 1.2*aaaFrac + 0.8*nonTextFrac)
	return d, checks
}

func scoreHarmony(t Theme, p Palette) DimensionScore {
	d := DimensionScore{Name: "harmony", Weight: 0.15}
	prim, _ := FromHex(p.Primary)
	acc, _ := FromHex(p.Accent)
	ph := prim.ToOKLCH().H
	ah := acc.ToOKLCH().H
	got := hueDiff(ph, ah)
	want := t.Harmony.accentHueOffset()
	if want > 180 {
		want = 360 - want
	}
	dev := math.Abs(got - want)
	const tol = 18
	if dev <= tol {
		d.Score = clampScore(10 - dev/tol*1.0) // within tolerance: 9..10
	} else {
		d.Score = clampScore(9 - (dev-tol)/12)
		d.Findings = append(d.Findings, fmt.Sprintf("accent/primary hue gap %.0f° vs %s target %.0f°", got, t.Harmony, want))
	}
	return d
}

func scoreNeutralRamp(p Palette) DimensionScore {
	d := DimensionScore{Name: "neutral-ramp", Weight: 0.12}
	// The elevation ramp is bg → surface → surface-2. Border is a hairline, not
	// a surface, so it's checked for direction/distinctness but not evenness.
	ls := lightnesses(p.BG, p.Surface, p.Surface2)
	asc := p.Mode == ModeDark
	mono := true
	deltas := make([]float64, 0, len(ls)-1)
	for i := 1; i < len(ls); i++ {
		dl := ls[i] - ls[i-1]
		if asc && dl <= 0 {
			mono = false
		}
		if !asc && dl >= 0 {
			mono = false
		}
		deltas = append(deltas, math.Abs(dl))
	}
	// border should continue the ramp direction and stay distinct from surface-2.
	bl := lightnesses(p.Border)[0]
	last := ls[len(ls)-1]
	if (asc && bl <= last) || (!asc && bl >= last) {
		d.Findings = append(d.Findings, "border is not distinct from surface-2")
		mono = false
	}
	if !mono {
		d.Findings = append(d.Findings, "elevation ramp (bg→surface→surface-2) is not monotonic")
		d.Score = 5
		return d
	}
	// evenness: lower coefficient of variation -> higher score
	cv := coeffVar(deltas)
	d.Score = clampScore(10 - cv*6)
	if cv > 0.5 {
		d.Findings = append(d.Findings, fmt.Sprintf("uneven elevation steps (CV %.2f)", cv))
	}
	return d
}

func scoreStateCoverage(p Palette) DimensionScore {
	d := DimensionScore{Name: "state-coverage", Weight: 0.11}
	states := map[string]string{"success": p.Success, "warning": p.Warning, "danger": p.Danger, "info": p.Info}
	bg, _ := FromHex(p.BG)
	score := 10.0
	hues := map[string]float64{}
	for name, hexv := range states {
		if hexv == "" {
			d.Findings = append(d.Findings, name+" missing")
			score -= 2.5
			continue
		}
		c, _ := FromHex(hexv)
		hues[name] = c.ToOKLCH().H
		if ContrastRatio(c, bg) < WCAGLargeAA {
			d.Findings = append(d.Findings, fmt.Sprintf("%s state low contrast on bg", name))
			score -= 1.5
		}
	}
	// distinctness: each state pair should be hue-separated
	names := make([]string, 0, len(hues))
	for n := range hues {
		names = append(names, n)
	}
	sort.Strings(names)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if hueDiff(hues[names[i]], hues[names[j]]) < 22 {
				d.Findings = append(d.Findings, fmt.Sprintf("%s and %s states are too close in hue", names[i], names[j]))
				score -= 1.5
			}
		}
	}
	d.Score = clampScore(score)
	return d
}

func scoreColorDiscipline(p Palette) DimensionScore {
	d := DimensionScore{Name: "color-discipline", Weight: 0.10}
	score := 10.0
	// neutrals should stay near-neutral (low chroma)
	for _, n := range []struct{ name, hex string }{{"bg", p.BG}, {"surface", p.Surface}, {"fg", p.FG}} {
		c, _ := FromHex(n.hex)
		if ch := c.ToOKLCH().C; ch > 0.035 {
			d.Findings = append(d.Findings, fmt.Sprintf("%s carries visible tint (chroma %.3f)", n.name, ch))
			score -= 1.5
		}
	}
	// primary and accent should be distinguishable unless monochromatic by design
	prim, _ := FromHex(p.Primary)
	acc, _ := FromHex(p.Accent)
	if hueDiff(prim.ToOKLCH().H, acc.ToOKLCH().H) < 8 {
		// acceptable only if chroma/L differ enough (mono scheme)
		if math.Abs(prim.ToOKLCH().L-acc.ToOKLCH().L) < 0.06 {
			d.Findings = append(d.Findings, "primary and accent are nearly identical")
			score -= 2
		}
	}
	d.Score = clampScore(score)
	return d
}

func scoreTypeScale(ts TypeScale) DimensionScore {
	d := DimensionScore{Name: "type-scale", Weight: 0.08}
	vals := make([]float64, 0, len(typeStepOrder))
	for _, s := range typeStepOrder {
		vals = append(vals, ts.Steps[s.Name])
	}
	score := 10.0
	for i := 1; i < len(vals); i++ {
		if vals[i] <= vals[i-1] {
			d.Findings = append(d.Findings, "type steps not strictly increasing")
			score -= 3
			break
		}
		r := vals[i] / vals[i-1]
		if math.Abs(r-ts.Ratio) > 0.02 {
			score -= 0.5
		}
	}
	d.Score = clampScore(score)
	return d
}

func scoreSpacing(ss SpacingScale) DimensionScore {
	d := DimensionScore{Name: "spacing", Weight: 0.07}
	score := 10.0
	for i := 1; i < len(ss.Steps); i++ {
		if ss.Steps[i] <= ss.Steps[i-1] {
			d.Findings = append(d.Findings, "spacing steps not strictly increasing")
			score -= 3
			break
		}
	}
	d.Score = clampScore(score)
	return d
}

func scoreCohesion(t Theme, p Palette) DimensionScore {
	d := DimensionScore{Name: "cohesion", Weight: 0.07}
	prim, _ := FromHex(p.Primary)
	pc := prim.ToOKLCH()
	score := 10.0
	if pc.C < 0.05 {
		d.Findings = append(d.Findings, "primary reads washed-out (very low chroma)")
		score -= 2
	}
	if pc.C > 0.24 {
		d.Findings = append(d.Findings, "primary may clip/oversaturate")
		score -= 1
	}
	d.Score = clampScore(score)
	return d
}

// --- helpers ---

func lightnesses(hexes ...string) []float64 {
	out := make([]float64, len(hexes))
	for i, h := range hexes {
		c, _ := FromHex(h)
		out[i] = c.ToOKLCH().L
	}
	return out
}

func coeffVar(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var mean float64
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if mean == 0 {
		return 0
	}
	var v float64
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	v /= float64(len(xs))
	return math.Sqrt(v) / mean
}

func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 10 {
		return 10
	}
	return round2(s)
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
