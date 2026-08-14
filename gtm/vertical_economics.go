package gtm

import (
	"context"
	"fmt"
	"strings"
)

// runEconomics is the unit-economics vertical — the centerpiece that closes the
// biggest GTM-canon gap (no CAC/LTV/payback/NRR modeling). Unlike the feed
// verticals it is model-driven: operator assumptions (and analyst-benchmark
// defaults for anything missing) become Evidence, the pure model computes the
// panel, and a CFO panel + go/no-go gate stress-test it. Provenance is exact:
// operator inputs are real evidence (grounded), defaults are synthetic
// (speculative), and every computed number is "inferred" — never "grounded".
func (e *Engine) runEconomics(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}

	inputs := ResolveEconInputs(opts)
	model := ComputeEcon(inputs)
	if v, ok := opts.Input("one_time"); ok && v != 0 {
		model = model.AsOneTime()
	}
	growth := ComputeGrowth(opts)
	gate := EvaluateGate(model)
	levers := ComputeLevers(model)
	scenarios := Scenarios(model)

	// Inputs → Evidence (operator-supplied = real; analyst default = synthetic).
	var evidence []Evidence
	now := e.now().UTC().Format("2006-01-02T15:04:05Z07:00")
	for _, in := range inputs {
		if in.Key == "customers" && in.Value == 0 && !in.Provided {
			continue // sizing not requested
		}
		val := formatEconInput(in)
		evidence = append(evidence, Evidence{
			ID:        "input:" + in.Key,
			Feed:      pick(in.Provided, "operator", "benchmark"),
			Tier:      TierFree,
			Title:     in.Label + " = " + val,
			Snippet:   in.Source,
			Metric:    "econ_input",
			Value:     val,
			Retrieved: now,
			Synthetic: !in.Provided,
		})
	}

	core := []string{"acv", "gross_margin", "monthly_churn", "cac", "expansion"}
	providedCore := 0
	for _, k := range core {
		for _, in := range inputs {
			if in.Key == k && in.Provided {
				providedCore++
			}
		}
	}

	sections := []Section{
		econAssumptionsSection(inputs),
		econUnitSection(model),
		econGateSection(gate),
		econLeversSection(model, levers),
		econScenarioSection(scenarios),
	}
	if growth.Any() {
		sections = append(sections, econGrowthSection(growth))
	}
	if model.Customers > 0 {
		penetration, _ := opts.Input("penetration")
		sections = append(sections, econSizingSection(model, penetration))
	}

	thesis := econThesis(opts.Subject, model, gate)
	narr, err := e.economicsNarrative(ctx, opts.Subject, model, gate)
	var warnings []string
	if err != nil {
		warnings = append(warnings, "narrative generation failed: "+err.Error())
		narr = thesis
	}
	sections = append(sections, Section{
		Title:  "Economist's Read (inferred)",
		Body:   narr,
		Claims: []Claim{{Text: thesis, Confidence: ConfInferred, Citations: econCites(evidence)}},
	})

	report := &Report{
		Vertical:  "economics",
		Subject:   opts.Subject,
		Query:     query,
		Generated: now,
		Provider:  e.gen.Provider(),
		Model:     e.gen.Model(),
		Tiers:     []FeedTier{TierFree},
		Evidence:  evidence,
		Sections:  sections,
		Panel:     RunEconomicsPanel(model, gate, growth, providedCore, len(core)),
		Warnings:  warnings,
	}
	report.Verdict = gate.Verdict
	if model.OneTime {
		report.SetMetric("one_time", 1)
	}
	if growth.RuleOf40 != nil {
		report.SetMetric("rule_of_40", *growth.RuleOf40)
	}
	if growth.BurnMultiple != nil {
		report.SetMetric("burn_multiple", *growth.BurnMultiple)
	}
	if growth.MagicNumber != nil {
		report.SetMetric("magic_number", *growth.MagicNumber)
	}
	if growth.QuickRatio != nil {
		report.SetMetric("quick_ratio", *growth.QuickRatio)
	}
	report.SetMetric("ltv", model.LTV)
	report.SetMetric("ltv_cac", model.LTVtoCAC)
	report.SetMetric("cac_payback_months", model.PaybackMonths)
	report.SetMetric("nrr_annual", model.NRRAnnual)
	report.SetMetric("grr_annual", model.GRRAnnual)
	report.SetMetric("gross_margin", model.GrossMargin)
	report.SetMetric("monthly_churn", model.MonthlyChurn)
	report.SetMetric("acv", model.ACV)
	report.SetMetric("cac", model.CAC)
	if model.Customers > 0 {
		report.SetMetric("sam", model.Customers*model.ACV)
	}
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func econAssumptionsSection(inputs []EconInput) Section {
	var rows [][]string
	var claims []Claim
	for _, in := range inputs {
		if in.Key == "customers" && in.Value == 0 && !in.Provided {
			continue
		}
		prov := "analyst default"
		conf := ConfSpeculative
		if in.Provided {
			prov = "operator"
			conf = ConfGrounded
		}
		rows = append(rows, []string{in.Label, formatEconInput(in), prov, in.Source})
		claims = append(claims, Claim{
			Text:       fmt.Sprintf("%s = %s (%s)", in.Label, formatEconInput(in), prov),
			Confidence: conf,
			Citations:  []string{"input:" + in.Key},
		})
	}
	body := "Every number below is tagged by provenance. **Operator** = a real assumption you supplied; **analyst default** = a benchmark placeholder (these make the model *speculative* until replaced).\n\n" +
		mdTable([]string{"Assumption", "Value", "Provenance", "Source"}, rows)
	return Section{Title: "Assumptions & Provenance", Body: body, Claims: claims}
}

func econUnitSection(m EconModel) Section {
	var rows [][]string
	if m.OneTime {
		rows = [][]string{
			{"Mode", "one-time purchase", "no recurring revenue"},
			{"Per-sale margin", fmtMoney(m.LTV), "price × gross margin"},
			{"LTV : CAC (per sale)", fmtRatio(m.LTVtoCAC), "≥ 3.0 healthy"},
			{"CAC recovery", fmtMonths(m.PaybackMonths), "recovered at the sale if margin ≥ CAC"},
		}
	} else {
		rows = [][]string{
			{"Monthly margin / account", fmtMoneyExact(m.MarginPerMonth), "ARPA × gross margin"},
			{"Expected lifetime", fmtMonths(m.LifetimeMonths), "1 ÷ monthly churn"},
			{"LTV (margin-adjusted)", fmtMoney(m.LTV), "(ARPA × GM) ÷ churn"},
			{"LTV : CAC", fmtRatio(m.LTVtoCAC), "≥ 3.0 healthy"},
			{"CAC payback", fmtMonths(m.PaybackMonths), "≤ 12 mo capital-efficient"},
			{"NRR (annual)", fmtPct(m.NRRAnnual), "> 100% = expansion engine"},
			{"GRR (annual)", fmtPct(m.GRRAnnual), "pure retention, no expansion"},
		}
	}
	body := mdTable([]string{"Metric", "Value", "Benchmark"}, rows)
	cites := []string{"input:arpa", "input:gross_margin", "input:monthly_churn", "input:cac"}
	claims := []Claim{
		{Text: "LTV (margin-adjusted) = " + fmtMoney(m.LTV), Confidence: ConfInferred, Citations: []string{"input:arpa", "input:gross_margin", "input:monthly_churn"}},
		{Text: "LTV:CAC = " + fmtRatio(m.LTVtoCAC), Confidence: ConfInferred, Citations: cites},
		{Text: "CAC payback = " + fmtMonths(m.PaybackMonths), Confidence: ConfInferred, Citations: []string{"input:cac", "input:arpa", "input:gross_margin"}},
		{Text: "NRR (annual) = " + fmtPct(m.NRRAnnual), Confidence: ConfInferred, Citations: []string{"input:expansion", "input:monthly_churn"}},
	}
	return Section{Title: "Unit Economics", Body: body, Claims: claims}
}

func econGateSection(gate EconGate) Section {
	var b strings.Builder
	fmt.Fprintf(&b, "**Verdict: %s**\n\n", gate.Verdict)
	for _, r := range gate.Reasons {
		b.WriteString("- " + r + "\n")
	}
	b.WriteString("\nGate = GO only when LTV:CAC ≥ 3 **and** payback ≤ 12 mo **and** NRR ≥ 100%; NO-GO when LTV:CAC < 1 or payback > 24 mo; otherwise CONDITIONAL.")
	return Section{
		Title: "Go / No-Go Gate",
		Body:  b.String(),
		Claims: []Claim{{
			Text:       "Unit-economics gate verdict: " + gate.Verdict,
			Confidence: ConfInferred,
			Citations:  []string{"input:cac", "input:monthly_churn", "input:arpa"},
		}},
	}
}

func econLeversSection(m EconModel, lv EconLevers) Section {
	var b strings.Builder
	b.WriteString("What would have to be true to clear the healthy bar (holding everything else constant):\n\n")
	fmt.Fprintf(&b, "- **Max CAC for LTV:CAC ≥ 3:** %s (current CAC %s)\n", fmtMoney(lv.MaxCACForRatio3), fmtMoney(m.CAC))
	if !m.OneTime {
		fmt.Fprintf(&b, "- **Max monthly churn for LTV:CAC ≥ 3:** %s (current %s)\n", fmtPct(lv.MaxChurnForRatio3), fmtPct(m.MonthlyChurn))
		fmt.Fprintf(&b, "- **Min ARPA for ≤ 12-mo payback:** %s/mo (current %s/mo)\n", fmtMoney(lv.MinARPAForPayback12), fmtMoney(m.ARPA))
	} else {
		b.WriteString("- One-time SKU: the only lever is **margin-over-CAC** — raise price/margin or lower CAC. No churn/payback levers apply.\n")
	}
	return Section{Title: "What Needs To Be True (levers)", Body: b.String()}
}

func econGrowthSection(g GrowthMetrics) Section {
	var rows [][]string
	if g.RuleOf40 != nil {
		rows = append(rows, []string{"Rule of 40", fmt.Sprintf("%.0f", *g.RuleOf40), "growth% + profit-margin% ; ≥ 40 healthy"})
	}
	if g.BurnMultiple != nil {
		rows = append(rows, []string{"Burn Multiple", fmtRatio(*g.BurnMultiple), "net burn ÷ net-new ARR ; <1 great, >2 poor"})
	}
	if g.MagicNumber != nil {
		rows = append(rows, []string{"Magic Number", fmt.Sprintf("%.2f", *g.MagicNumber), "net-new ARR ÷ S&M ; >0.75 efficient, >1 great"})
	}
	if g.QuickRatio != nil {
		rows = append(rows, []string{"SaaS Quick Ratio", fmt.Sprintf("%.1f", *g.QuickRatio), "(new+exp) ÷ (churn+contraction) ; >4 great"})
	}
	body := "Company-level capital-efficiency & growth-quality metrics — the classical investor / shark-tank lenses that sit above unit economics:\n\n" +
		mdTable([]string{"Metric", "Value", "Benchmark"}, rows)
	return Section{Title: "Capital Efficiency & Growth", Body: body}
}

func econScenarioSection(scen []EconScenario) Section {
	var rows [][]string
	for _, s := range scen {
		rows = append(rows, []string{s.Name, fmtRatio(s.LTVtoCAC), fmtMonths(s.PaybackMonths), fmtPct(s.NRRAnnual)})
	}
	body := "Sensitivity: Conservative worsens churn/CAC +25% and ARPA −15%; Stretch improves each by the same.\n\n" +
		mdTable([]string{"Scenario", "LTV:CAC", "Payback", "NRR"}, rows)
	return Section{Title: "Scenarios (sensitivity)", Body: body}
}

func econSizingSection(m EconModel, penetration float64) Section {
	sam := m.Customers * m.ACV
	var b strings.Builder
	b.WriteString("Bottom-up sizing (the credible direction — `# ICP accounts × ACV`, not top-down %-of-a-huge-market):\n\n")
	fmt.Fprintf(&b, "- **SAM** = %.0f accounts × %s ACV = **%s**\n", m.Customers, fmtMoney(m.ACV), fmtMoney(sam))
	if penetration > 0 {
		som := sam * penetration
		fmt.Fprintf(&b, "- **SOM (year-1)** = SAM × %s penetration = **%s**\n", fmtPct(penetration), fmtMoney(som))
	} else {
		b.WriteString("- **SOM**: pass `--penetration <0..1>` (realistic near-term share) to size the obtainable slice.\n")
	}
	return Section{
		Title: "Bottom-up Market Sizing",
		Body:  b.String(),
		Claims: []Claim{{
			Text:       fmt.Sprintf("Bottom-up SAM = %s (%.0f accounts × %s ACV)", fmtMoney(sam), m.Customers, fmtMoney(m.ACV)),
			Confidence: ConfInferred,
			Citations:  []string{"input:customers", "input:acv"},
		}},
	}
}

func econThesis(subject string, m EconModel, gate EconGate) string {
	return fmt.Sprintf("%s unit economics: LTV %s on CAC %s = %s, payback %s, NRR %s → gate %s.",
		subject, fmtMoney(m.LTV), fmtMoney(m.CAC), fmtRatio(m.LTVtoCAC), fmtMonths(m.PaybackMonths), fmtPct(m.NRRAnnual), gate.Verdict)
}

func (e *Engine) economicsNarrative(ctx context.Context, subject string, m EconModel, gate EconGate) (string, error) {
	facts := fmt.Sprintf(
		"LTV=%s; CAC=%s; LTV:CAC=%s; CAC_payback=%s; NRR=%s; GRR=%s; gross_margin=%s; monthly_churn=%s; gate=%s",
		fmtMoney(m.LTV), fmtMoney(m.CAC), fmtRatio(m.LTVtoCAC), fmtMonths(m.PaybackMonths),
		fmtPct(m.NRRAnnual), fmtPct(m.GRRAnnual), fmtPct(m.GrossMargin), fmtPct(m.MonthlyChurn), gate.Verdict)
	if e.gen.Provider() == "offline" {
		return econThesis(subject, m, gate) + "\n\nDrivers: " + facts, nil
	}
	sys := "You are a precise SaaS CFO writing a 2-paragraph unit-economics read. STRICT RULE: use ONLY the numbers in the FACTS block — do not invent any figure, market size, or competitor. Explain what the gate verdict means and the single highest-leverage lever to improve it. Be blunt about weaknesses."
	user := fmt.Sprintf("SUBJECT: %s\n\nFACTS:\n%s", subject, facts)
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user, MaxTokens: 700})
}

// --- helpers ---

func formatEconInput(in EconInput) string {
	switch in.Key {
	case "gross_margin":
		return fmtPct(in.Value)
	case "monthly_churn", "expansion":
		return fmtPct(in.Value) + "/mo"
	case "customers":
		return fmt.Sprintf("%.0f", in.Value)
	default:
		return fmtMoney(in.Value)
	}
}

func econCites(ev []Evidence) []string {
	want := map[string]bool{"input:arpa": true, "input:cac": true, "input:monthly_churn": true, "input:gross_margin": true, "input:expansion": true}
	var out []string
	for _, e := range ev {
		if want[e.ID] {
			out = append(out, e.ID)
		}
	}
	return out
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
