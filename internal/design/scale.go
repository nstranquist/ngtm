package design

import "math"

// TypeScale is a modular (geometric) type ramp. Steps are derived as
// base * ratio^n, the classic "modular scale" — sizes relate by a constant
// multiplier so headings feel proportional rather than arbitrary.
type TypeScale struct {
	Base  float64            // base body size, px
	Ratio float64            // step ratio (1.2 minor third, 1.25 major third, 1.333 perfect fourth)
	Steps map[string]float64 // name -> px
}

// typeStepOrder is the canonical ordering for rendering/scoring.
var typeStepOrder = []struct {
	Name string
	N    int
}{
	{"xs", -2}, {"sm", -1}, {"base", 0}, {"lg", 1},
	{"xl", 2}, {"2xl", 3}, {"3xl", 4}, {"4xl", 5},
}

// NewTypeScale builds a modular type scale.
func NewTypeScale(base, ratio float64) TypeScale {
	if base <= 0 {
		base = 16
	}
	if ratio <= 1 {
		ratio = 1.25
	}
	steps := make(map[string]float64, len(typeStepOrder))
	for _, s := range typeStepOrder {
		steps[s.Name] = round1(base * math.Pow(ratio, float64(s.N)))
	}
	return TypeScale{Base: base, Ratio: ratio, Steps: steps}
}

// SpacingScale is a base-unit spacing system on a geometric-ish ramp.
// A consistent spacing unit (typically a 4px base) produces visual rhythm.
type SpacingScale struct {
	Base  float64   // base unit, px (typically 4)
	Steps []float64 // px values, ascending
}

// NewSpacingScale builds a spacing ramp from a base unit using a familiar
// 4/8-point progression (the de-facto standard for UI density).
func NewSpacingScale(base float64) SpacingScale {
	if base <= 0 {
		base = 4
	}
	mult := []float64{0.5, 1, 2, 3, 4, 6, 8, 12, 16, 24}
	steps := make([]float64, len(mult))
	for i, m := range mult {
		steps[i] = round1(base * m)
	}
	return SpacingScale{Base: base, Steps: steps}
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
