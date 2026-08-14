package gtm

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestComputeEcon_KnownNumbers(t *testing.T) {
	// ARPA 100 (ACV 1200), 80% margin, 2%/mo churn, CAC 1200, 1%/mo expansion.
	opts := Options{Inputs: map[string]float64{
		"acv": 1200, "gross_margin": 0.80, "monthly_churn": 0.02, "cac": 1200, "expansion": 0.01,
	}}
	m := ComputeEcon(ResolveEconInputs(opts))
	if !approx(m.MarginPerMonth, 80, 0.01) {
		t.Errorf("margin/mo = %v, want 80", m.MarginPerMonth)
	}
	if !approx(m.LTV, 4000, 0.01) {
		t.Errorf("LTV = %v, want 4000", m.LTV)
	}
	if !approx(m.LTVtoCAC, 3.333, 0.01) {
		t.Errorf("LTV:CAC = %v, want ~3.33", m.LTVtoCAC)
	}
	if !approx(m.PaybackMonths, 15, 0.01) {
		t.Errorf("payback = %v, want 15", m.PaybackMonths)
	}
	if !approx(m.NRRAnnual, math.Pow(0.99, 12), 0.0001) {
		t.Errorf("NRR = %v", m.NRRAnnual)
	}
}

func TestEvaluateGate_Verdicts(t *testing.T) {
	// GO case: strong ratio, fast payback, expansion.
	go1 := ComputeEcon(ResolveEconInputs(Options{Inputs: map[string]float64{
		"acv": 30000, "gross_margin": 0.85, "monthly_churn": 0.02, "cac": 9000, "expansion": 0.03,
	}}))
	if g := EvaluateGate(go1); g.Verdict != "GO" {
		t.Errorf("expected GO, got %s (ltvcac=%v payback=%v nrr=%v)", g.Verdict, go1.LTVtoCAC, go1.PaybackMonths, go1.NRRAnnual)
	}
	// CONDITIONAL: ratio ok but slow payback / churning base.
	cond := ComputeEcon(ResolveEconInputs(Options{Inputs: map[string]float64{
		"acv": 1200, "gross_margin": 0.80, "monthly_churn": 0.02, "cac": 1200, "expansion": 0.01,
	}}))
	if g := EvaluateGate(cond); g.Verdict != "CONDITIONAL" {
		t.Errorf("expected CONDITIONAL, got %s", g.Verdict)
	}
	// NO-GO: CAC > LTV.
	nogo := ComputeEcon(ResolveEconInputs(Options{Inputs: map[string]float64{
		"acv": 1200, "gross_margin": 0.50, "monthly_churn": 0.10, "cac": 5000, "expansion": 0.0,
	}}))
	if g := EvaluateGate(nogo); g.Verdict != "NO-GO" {
		t.Errorf("expected NO-GO, got %s (ltvcac=%v)", g.Verdict, nogo.LTVtoCAC)
	}
}

func TestComputeLevers_BackSolve(t *testing.T) {
	m := ComputeEcon(ResolveEconInputs(Options{Inputs: map[string]float64{
		"acv": 1200, "gross_margin": 0.80, "monthly_churn": 0.02, "cac": 1200, "expansion": 0.01,
	}}))
	lv := ComputeLevers(m)
	// Max CAC for 3× = LTV/3 = 4000/3 = 1333.3
	if !approx(lv.MaxCACForRatio3, 4000.0/3, 0.5) {
		t.Errorf("MaxCACForRatio3 = %v, want ~1333", lv.MaxCACForRatio3)
	}
	// Min ARPA for 12-mo payback = CAC/(12*GM) = 1200/(12*0.8) = 125
	if !approx(lv.MinARPAForPayback12, 125, 0.01) {
		t.Errorf("MinARPAForPayback12 = %v, want 125", lv.MinARPAForPayback12)
	}
}

func TestResolveEconInputs_Provenance(t *testing.T) {
	in := ResolveEconInputs(Options{Inputs: map[string]float64{"acv": 30000, "cac": 9000}})
	prov := map[string]bool{}
	for _, e := range in {
		prov[e.Key] = e.Provided
	}
	if !prov["acv"] || !prov["cac"] {
		t.Error("acv/cac should be provided")
	}
	if !prov["arpa"] {
		t.Error("arpa derived from acv should count as provided")
	}
	if prov["gross_margin"] || prov["monthly_churn"] {
		t.Error("unset inputs should be analyst defaults (not provided)")
	}
}
