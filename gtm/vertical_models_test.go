package gtm

import (
	"context"
	"strings"
	"testing"
)

func modelEngine() *Engine {
	return NewEngineWith(&FeedRegistry{}, offlineGenerator{}, fixedNow)
}

func TestRunEconomics_ValidatedAndGated(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "economics", Options{
		Subject: "nvault",
		Inputs:  map[string]float64{"acv": 30000, "cac": 9000, "gross_margin": 0.85, "monthly_churn": 0.02, "expansion": 0.03},
	})
	if err != nil {
		t.Fatalf("runEconomics: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	if rep.Panel == nil || rep.Panel.Title != "CFO Panel" {
		t.Fatalf("expected CFO Panel, got %+v", rep.Panel)
	}
	if sectionByTitle(rep, "Go / No-Go Gate") == nil {
		t.Fatal("missing gate section")
	}
	if sectionByTitle(rep, "Unit Economics") == nil {
		t.Fatal("missing unit economics section")
	}
	// A provided input must yield a grounded assumption claim citing real evidence.
	assume := sectionByTitle(rep, "Assumptions & Provenance")
	foundGrounded := false
	for _, c := range assume.Claims {
		if c.Confidence == ConfGrounded && len(c.Citations) > 0 {
			foundGrounded = true
		}
	}
	if !foundGrounded {
		t.Error("expected at least one grounded (operator-supplied) assumption")
	}
}

func TestRunEconomics_AllDefaultsAreSpeculative(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "economics", Options{Subject: "mystery"})
	if err != nil {
		t.Fatalf("runEconomics: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("validation should be clean even with all defaults: %v", v)
	}
	// With no operator inputs the integrity critic must fire (score ≤ 1).
	var integrity *Verdict
	for i := range rep.Panel.Verdicts {
		if rep.Panel.Verdicts[i].Critic == "Assumption Integrity" {
			integrity = &rep.Panel.Verdicts[i]
		}
	}
	if integrity == nil || integrity.Score > 1 {
		t.Fatalf("all-default model must flag integrity, got %+v", integrity)
	}
}

func TestRunEconomics_BottomUpSizing(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "economics", Options{
		Subject: "nvault",
		Inputs:  map[string]float64{"acv": 30000, "cac": 9000, "customers": 12000, "penetration": 0.02},
	})
	if err != nil {
		t.Fatalf("runEconomics: %v", err)
	}
	s := sectionByTitle(rep, "Bottom-up Market Sizing")
	if s == nil || !strings.Contains(s.Body, "SAM") {
		t.Fatalf("expected bottom-up sizing section, got %+v", s)
	}
}

func TestEconomics_JSONSummaryAndInfiniteGuard(t *testing.T) {
	eng := modelEngine()
	// churn = 0 makes LTV and lifetime infinite — must NOT break json.Marshal.
	rep, err := eng.Run(context.Background(), "economics", Options{
		Subject: "zerochurn",
		Inputs:  map[string]float64{"acv": 1200, "cac": 1200, "gross_margin": 0.8, "monthly_churn": 0, "expansion": 0},
	})
	if err != nil {
		t.Fatalf("runEconomics: %v", err)
	}
	if _, err := rep.JSON(); err != nil {
		t.Fatalf("JSON marshal failed (infinite value leaked into Metrics?): %v", err)
	}
	if rep.Verdict == "" {
		t.Error("expected a gate verdict in the JSON summary")
	}
	if _, ok := rep.Metrics["ltv"]; ok {
		t.Error("infinite LTV must be omitted from Metrics")
	}
	if _, ok := rep.Metrics["cac"]; !ok {
		t.Error("finite metric (cac) should be present in the summary")
	}
}

func TestEconomics_OneTimeMode(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "economics", Options{
		Subject: "cadence",
		Inputs:  map[string]float64{"acv": 39, "cac": 8, "gross_margin": 0.92, "one_time": 1},
	})
	if err != nil {
		t.Fatalf("runEconomics one-time: %v", err)
	}
	if _, err := rep.JSON(); err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	// per-sale margin 35.88 / CAC 8 = 4.48 ≥ 3 → GO
	if rep.Verdict != "GO" {
		t.Errorf("expected GO for one-time, got %s", rep.Verdict)
	}
	if rep.Metrics["one_time"] != 1 {
		t.Error("expected one_time=1 in metrics")
	}
	if _, ok := rep.Metrics["nrr_annual"]; ok {
		t.Error("one-time model must not emit NRR")
	}
	// panel must not contain recurring-only critics
	for _, v := range rep.Panel.Verdicts {
		if v.Critic == "CAC Payback" || v.Critic == "Retention / NRR" {
			t.Errorf("one-time panel should omit %q", v.Critic)
		}
	}
}

func TestEconomics_GrowthMetrics(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "economics", Options{
		Subject: "nvault",
		Inputs: map[string]float64{
			"acv": 30000, "cac": 9000, "gross_margin": 0.85, "monthly_churn": 0.02, "expansion": 0.03,
			"growth_rate": 1.5, "profit_margin": -0.2, "net_burn": 800000, "net_new_arr": 1200000, "sm_spend": 900000,
		},
	})
	if err != nil {
		t.Fatalf("runEconomics growth: %v", err)
	}
	if got := rep.Metrics["rule_of_40"]; got != 130 { // (1.5 - 0.2) * 100
		t.Errorf("rule_of_40 = %v, want 130", got)
	}
	if got := rep.Metrics["burn_multiple"]; !approx(got, 0.667, 0.01) { // 800k/1200k
		t.Errorf("burn_multiple = %v, want ~0.667", got)
	}
	if got := rep.Metrics["magic_number"]; !approx(got, 1.333, 0.01) { // 1200k/900k
		t.Errorf("magic_number = %v, want ~1.333", got)
	}
	if sectionByTitle(rep, "Capital Efficiency & Growth") == nil {
		t.Error("missing growth section")
	}
	found := false
	for _, v := range rep.Panel.Verdicts {
		if v.Critic == "Rule of 40" {
			found = true
		}
	}
	if !found {
		t.Error("expected Rule of 40 critic in CFO panel")
	}
}

func TestRunPricing_ValueBased(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "pricing", Options{
		Subject: "nvault",
		Inputs:  map[string]float64{"next_best_price": 21000, "diff_value": 15000, "value_capture": 0.5},
	})
	if err != nil {
		t.Fatalf("runPricing: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	if rep.Panel == nil || rep.Panel.Title != "Pricing Panel" {
		t.Fatalf("expected Pricing Panel, got %+v", rep.Panel)
	}
	for _, title := range []string{"Value-Based Price", "Good-Better-Best Tiers", "WTP Validation (Van Westendorp survey)"} {
		if sectionByTitle(rep, title) == nil {
			t.Errorf("missing section %q", title)
		}
	}
}

func TestComputePricing_Math(t *testing.T) {
	m, _ := ComputePricing(Options{Inputs: map[string]float64{"next_best_price": 21000, "diff_value": 15000, "value_capture": 0.5}})
	if !approx(m.Ceiling, 36000, 0.01) {
		t.Errorf("ceiling = %v, want 36000", m.Ceiling)
	}
	if !approx(m.Recommended, 28500, 0.01) {
		t.Errorf("recommended = %v, want 28500", m.Recommended)
	}
	if len(m.Tiers) != 3 || !(m.Tiers[2].Price > m.Tiers[0].Price) {
		t.Errorf("tiers not spread: %+v", m.Tiers)
	}
}

func TestSelectMotion_ByACV(t *testing.T) {
	if SelectMotion(2000).Motion != "Product-Led Growth (self-serve)" {
		t.Error("low ACV should be product-led")
	}
	if SelectMotion(40000).Motion != "Sales-Led Growth" {
		t.Error("high ACV should be sales-led")
	}
	if !strings.HasPrefix(SelectMotion(12000).Motion, "Hybrid") {
		t.Error("mid ACV should be hybrid")
	}
}

func TestRunMotion_HasValidationPlan(t *testing.T) {
	eng := modelEngine()
	rep, err := eng.Run(context.Background(), "motion", Options{Subject: "nvault", Inputs: map[string]float64{"acv": 30000}})
	if err != nil {
		t.Fatalf("runMotion: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	if rep.Panel == nil || rep.Panel.Title != "GTM Motion Panel" {
		t.Fatalf("expected GTM Motion Panel, got %+v", rep.Panel)
	}
	s := sectionByTitle(rep, "Customer Validation & PMF Plan")
	if s == nil || !strings.Contains(s.Body, "40%") {
		t.Fatalf("expected PMF 40%% test in validation plan, got %+v", s)
	}
}
