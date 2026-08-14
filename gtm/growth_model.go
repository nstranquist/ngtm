package gtm

import "fmt"

// GrowthMetrics holds the classical investor / shark-tank efficiency metrics that
// sit ABOVE unit economics — the ones a VC or a Shark actually grills founders on.
// Each is computed only when its inputs are supplied (a *float64 left nil = "not
// provided", so we never fabricate a number). Sources: Rule of 40 (Brad Feld /
// SaaS canon), Burn Multiple (Bessemer / David Sacks), Magic Number (Scale/SaaS
// canon), SaaS Quick Ratio (Social Capital / Mamoon Hamid).
type GrowthMetrics struct {
	RuleOf40     *float64 // growth% + profit-margin% ; ≥ 40 healthy
	BurnMultiple *float64 // net burn ÷ net-new ARR ; <1 great, <1.5 ok, >2 poor
	MagicNumber  *float64 // net-new ARR ÷ prior-period S&M ; >0.75 efficient, >1 great
	QuickRatio   *float64 // (new+expansion ARR) ÷ (churned+contraction ARR) ; >4 great
}

func floatPtr(v float64) *float64 { return &v }

// ComputeGrowth derives the growth/efficiency metrics from whichever inputs the
// operator supplied:
//
//	--growth-rate + --profit-margin            → Rule of 40
//	--net-burn + --net-new-arr                 → Burn Multiple
//	--net-new-arr + --sm-spend                 → Magic Number
//	--gained-arr + --lost-arr                  → SaaS Quick Ratio
func ComputeGrowth(opts Options) GrowthMetrics {
	var g GrowthMetrics
	gr, grOK := opts.Input("growth_rate")
	pm, pmOK := opts.Input("profit_margin")
	if grOK && pmOK {
		g.RuleOf40 = floatPtr((gr + pm) * 100)
	}
	nb, nbOK := opts.Input("net_burn")
	nna, nnaOK := opts.Input("net_new_arr")
	if nbOK && nnaOK && nna != 0 {
		g.BurnMultiple = floatPtr(nb / nna)
	}
	sm, smOK := opts.Input("sm_spend")
	if nnaOK && smOK && sm != 0 {
		g.MagicNumber = floatPtr(nna / sm)
	}
	gained, gainOK := opts.Input("gained_arr")
	lost, lostOK := opts.Input("lost_arr")
	if gainOK && lostOK && lost != 0 {
		g.QuickRatio = floatPtr(gained / lost)
	}
	return g
}

// Any reports whether at least one growth metric was computed.
func (g GrowthMetrics) Any() bool {
	return g.RuleOf40 != nil || g.BurnMultiple != nil || g.MagicNumber != nil || g.QuickRatio != nil
}

// GrowthVerdicts returns one CFO-panel critic per computed growth metric.
func GrowthVerdicts(g GrowthMetrics) []Verdict {
	var v []Verdict
	if g.RuleOf40 != nil {
		r := *g.RuleOf40
		switch {
		case r >= 40:
			v = append(v, Verdict{Critic: "Rule of 40", Score: 8, Rationale: fmt.Sprintf("Rule of 40 = %.0f ≥ 40 — growth+margin balance is healthy.", r)})
		case r >= 20:
			v = append(v, Verdict{Critic: "Rule of 40", Score: 5, Kills: []string{fmt.Sprintf("Rule of 40 = %.0f (20–40) — under the bar.", r)}, Rationale: "Either grow faster or improve margin."})
		default:
			v = append(v, Verdict{Critic: "Rule of 40", Score: 3, Kills: []string{fmt.Sprintf("Rule of 40 = %.0f < 20 — inefficient growth.", r)}, Rationale: "Neither growth nor profitability is carrying the business."})
		}
	}
	if g.BurnMultiple != nil {
		b := *g.BurnMultiple
		switch {
		case b < 1:
			v = append(v, Verdict{Critic: "Burn Multiple", Score: 8, Rationale: fmt.Sprintf("Burn multiple %.2f < 1 — capital-efficient growth.", b)})
		case b <= 2:
			v = append(v, Verdict{Critic: "Burn Multiple", Score: 5, Kills: []string{fmt.Sprintf("Burn multiple %.2f (1–2) — burning a lot per $ of new ARR.", b)}, Rationale: "Acceptable in a land-grab, dangerous otherwise."})
		default:
			v = append(v, Verdict{Critic: "Burn Multiple", Score: 2, Kills: []string{fmt.Sprintf("Burn multiple %.2f > 2 — torching cash for growth.", b)}, Rationale: "Each new ARR dollar costs >$2 of burn; unsustainable."})
		}
	}
	if g.MagicNumber != nil {
		mn := *g.MagicNumber
		switch {
		case mn >= 0.75:
			v = append(v, Verdict{Critic: "Magic Number", Score: 7, Rationale: fmt.Sprintf("Magic number %.2f ≥ 0.75 — S&M spend is paying off; lean in.", mn)})
		case mn >= 0.5:
			v = append(v, Verdict{Critic: "Magic Number", Score: 5, Kills: []string{fmt.Sprintf("Magic number %.2f (0.5–0.75) — sales efficiency is soft.", mn)}, Rationale: "Tune the funnel before adding S&M headcount."})
		default:
			v = append(v, Verdict{Critic: "Magic Number", Score: 3, Kills: []string{fmt.Sprintf("Magic number %.2f < 0.5 — S&M isn't converting.", mn)}, Rationale: "Don't scale spend on an inefficient motion."})
		}
	}
	if g.QuickRatio != nil {
		q := *g.QuickRatio
		switch {
		case q >= 4:
			v = append(v, Verdict{Critic: "SaaS Quick Ratio", Score: 8, Rationale: fmt.Sprintf("Quick ratio %.1f ≥ 4 — growth far outpaces losses.", q)})
		case q >= 2:
			v = append(v, Verdict{Critic: "SaaS Quick Ratio", Score: 5, Kills: []string{fmt.Sprintf("Quick ratio %.1f (2–4) — growth only modestly beats churn.", q)}, Rationale: "Retention is taxing growth."})
		default:
			v = append(v, Verdict{Critic: "SaaS Quick Ratio", Score: 3, Kills: []string{fmt.Sprintf("Quick ratio %.1f < 2 — losses nearly cancel growth.", q)}, Rationale: "The bucket is leaking as fast as you fill it."})
		}
	}
	return v
}
