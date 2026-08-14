package gtm

import "math"

// This is the pure unit-economics model behind the `economics` vertical. It has
// no engine/feed dependencies so it can be unit-tested directly, and it is the
// piece that closes the biggest GTM-canon gap: turning ARPA/CAC/churn/margin
// assumptions into LTV, LTV:CAC, CAC-payback, NRR, a go/no-go gate, the levers
// that would flip a failing gate, and conservative/base/stretch scenarios.
//
// Provenance is first-class: every input is either operator-PROVIDED (a real
// assumption) or filled from an analyst BENCHMARK default. Computed outputs are
// only ever labeled "inferred", never "grounded" — a model is not a measurement.

// Gate thresholds (sources: David Skok / forEntrepreneurs "SaaS Metrics 2.0").
const (
	ratioHealthy   = 3.0  // LTV:CAC ≥ 3 is the canonical "healthy" bar
	ratioFloor     = 1.0  // LTV:CAC < 1 = each customer loses money
	paybackHealthy = 12.0 // CAC payback ≤ 12 months (best-in-class 5–7)
	paybackCeiling = 24.0 // payback > 24 months = capital-trap
	nrrHealthy     = 1.10 // NRR ≥ 110% = strong expansion engine
)

// EconInput is one resolved model assumption with its provenance.
type EconInput struct {
	Key      string  // "arpa","acv","gross_margin","monthly_churn","cac","expansion","customers"
	Label    string  // human label
	Value    float64 // resolved value
	Unit     string  // "$", "/mo", "x", "count"
	Provided bool    // operator supplied it (real) vs analyst default (synthetic)
	Source   string  // benchmark citation when defaulted; "operator-supplied" otherwise
}

// EconModel is the computed unit-economics panel.
type EconModel struct {
	ARPA             float64 // monthly recurring revenue per account ($)
	ACV              float64 // annual contract value ($)
	GrossMargin      float64 // 0..1
	MonthlyChurn     float64 // 0..1 revenue churn / month
	CAC              float64 // fully-loaded customer acquisition cost ($)
	MonthlyExpansion float64 // 0..1 net expansion / month
	Customers        float64 // optional: # addressable accounts (bottom-up sizing)

	OneTime bool // true → one-time purchase (no recurring churn/NRR); ACV is the price

	MarginPerMonth float64 // ARPA × gross margin
	LifetimeMonths float64 // 1 / churn
	LTV            float64 // margin-adjusted lifetime value
	LTVtoCAC       float64
	PaybackMonths  float64
	NRRAnnual      float64 // (1 + expansion − churn)^12
	GRRAnnual      float64 // (1 − churn)^12
}

// econDefaults are analyst benchmark fallbacks, each with a citation. They are
// deliberately conservative-typical SaaS figures; using them marks the model
// "speculative" so the integrity critic fires.
var econDefaults = map[string]struct {
	val    float64
	label  string
	unit   string
	source string
}{
	"acv":           {1200, "Annual contract value", "$", "[analyst] placeholder $100/mo — SUPPLY a real ACV"},
	"gross_margin":  {0.80, "Gross margin", "x", "[analyst] SaaS gross margin typically 70–85% (Skok)"},
	"monthly_churn": {0.03, "Monthly revenue churn", "/mo", "[analyst] ~3%/mo mid-market SaaS placeholder"},
	"cac":           {1200, "Fully-loaded CAC", "$", "[analyst] placeholder = 1× ACV — SUPPLY a real CAC"},
	"expansion":     {0.01, "Monthly net expansion", "/mo", "[analyst] ~1%/mo placeholder"},
	"customers":     {0, "Addressable accounts (ICP count)", "count", "[analyst] not supplied — bottom-up sizing skipped"},
}

// ResolveEconInputs applies operator inputs over analyst defaults and records
// provenance for every value. ARPA and ACV are reconciled: if only one is
// given, the other is derived (ACV = ARPA×12); a value derived from a real
// input counts as real.
func ResolveEconInputs(opts Options) []EconInput {
	arpaV, arpaOK := opts.Input("arpa")
	acvV, acvOK := opts.Input("acv")

	arpa := EconInput{Key: "arpa", Label: "Monthly revenue per account", Unit: "$"}
	acv := EconInput{Key: "acv", Label: "Annual contract value", Unit: "$"}
	switch {
	case arpaOK && acvOK:
		arpa.Value, arpa.Provided, arpa.Source = arpaV, true, "operator-supplied"
		acv.Value, acv.Provided, acv.Source = acvV, true, "operator-supplied"
	case arpaOK:
		arpa.Value, arpa.Provided, arpa.Source = arpaV, true, "operator-supplied"
		acv.Value, acv.Provided, acv.Source = arpaV*12, true, "derived from ARPA × 12"
	case acvOK:
		acv.Value, acv.Provided, acv.Source = acvV, true, "operator-supplied"
		arpa.Value, arpa.Provided, arpa.Source = acvV/12, true, "derived from ACV ÷ 12"
	default:
		d := econDefaults["acv"]
		acv.Value, acv.Provided, acv.Source = d.val, false, d.source
		arpa.Value, arpa.Provided, arpa.Source = d.val/12, false, "[analyst] = ACV ÷ 12 placeholder"
	}

	mk := func(key string) EconInput {
		d := econDefaults[key]
		in := EconInput{Key: key, Label: d.label, Unit: d.unit}
		if v, ok := opts.Input(key); ok {
			in.Value, in.Provided, in.Source = v, true, "operator-supplied"
		} else {
			in.Value, in.Provided, in.Source = d.val, false, d.source
		}
		return in
	}

	return []EconInput{
		arpa, acv,
		mk("gross_margin"), mk("monthly_churn"), mk("cac"), mk("expansion"), mk("customers"),
	}
}

func inputVal(inputs []EconInput, key string) float64 {
	for _, in := range inputs {
		if in.Key == key {
			return in.Value
		}
	}
	return 0
}

// ComputeEcon runs the model from resolved inputs.
func ComputeEcon(inputs []EconInput) EconModel {
	m := EconModel{
		ARPA:             inputVal(inputs, "arpa"),
		ACV:              inputVal(inputs, "acv"),
		GrossMargin:      inputVal(inputs, "gross_margin"),
		MonthlyChurn:     inputVal(inputs, "monthly_churn"),
		CAC:              inputVal(inputs, "cac"),
		MonthlyExpansion: inputVal(inputs, "expansion"),
		Customers:        inputVal(inputs, "customers"),
	}
	m.MarginPerMonth = m.ARPA * m.GrossMargin
	if m.MonthlyChurn > 0 {
		m.LifetimeMonths = 1 / m.MonthlyChurn
		m.LTV = m.MarginPerMonth / m.MonthlyChurn
	} else {
		m.LifetimeMonths = math.Inf(1)
		m.LTV = math.Inf(1)
	}
	if m.CAC > 0 {
		m.LTVtoCAC = m.LTV / m.CAC
	} else {
		m.LTVtoCAC = math.Inf(1)
	}
	if m.MarginPerMonth > 0 {
		m.PaybackMonths = m.CAC / m.MarginPerMonth
	} else {
		m.PaybackMonths = math.Inf(1)
	}
	m.NRRAnnual = math.Pow(1+m.MonthlyExpansion-m.MonthlyChurn, 12)
	m.GRRAnnual = math.Pow(1-m.MonthlyChurn, 12)
	return m
}

// AsOneTime recomputes the model for a one-time (non-subscription) purchase:
// LTV is a single purchase's gross-margin contribution, there is no churn-driven
// lifetime/NRR/GRR, and CAC is recovered at the sale when contribution-positive.
// This fixes the distortion of modeling a $39 one-time SKU as a subscription.
func (m EconModel) AsOneTime() EconModel {
	m.OneTime = true
	m.LifetimeMonths = math.NaN()
	m.NRRAnnual = math.NaN()
	m.GRRAnnual = math.NaN()
	m.LTV = m.ACV * m.GrossMargin // single-purchase contribution
	if m.CAC > 0 {
		m.LTVtoCAC = m.LTV / m.CAC
	} else {
		m.LTVtoCAC = math.Inf(1)
	}
	if m.LTV >= m.CAC {
		m.PaybackMonths = 0 // recovered at the moment of sale
	} else {
		m.PaybackMonths = math.Inf(1) // never recovers on a single sale
	}
	return m
}

// EconGate is the go/no-go verdict with per-metric reasoning.
type EconGate struct {
	Verdict string   // "GO" | "CONDITIONAL" | "NO-GO"
	Reasons []string // one line per metric, pass or fail
}

// EvaluateGate applies the canonical thresholds.
func EvaluateGate(m EconModel) EconGate {
	var reasons []string
	pass := func(ok bool, okMsg, failMsg string) bool {
		if ok {
			reasons = append(reasons, "✅ "+okMsg)
		} else {
			reasons = append(reasons, "❌ "+failMsg)
		}
		return ok
	}

	// One-time products have no recurring churn/NRR; the gate is purely whether
	// a single sale's margin covers CAC with enough headroom to fund the business.
	if m.OneTime {
		ratioOK := pass(m.LTVtoCAC >= ratioHealthy,
			"per-sale margin:CAC "+fmtRatio(m.LTVtoCAC)+" ≥ 3.0 (healthy)",
			"per-sale margin:CAC "+fmtRatio(m.LTVtoCAC)+" < 3.0 (thin margin over CAC)")
		pass(m.LTV >= m.CAC, "CAC recovered at the sale (contribution-positive)",
			"single sale does not cover CAC")
		switch {
		case m.LTVtoCAC < ratioFloor:
			return EconGate{Verdict: "NO-GO", Reasons: reasons}
		case ratioOK:
			return EconGate{Verdict: "GO", Reasons: reasons}
		default:
			return EconGate{Verdict: "CONDITIONAL", Reasons: reasons}
		}
	}
	ratioOK := pass(m.LTVtoCAC >= ratioHealthy,
		"LTV:CAC "+fmtRatio(m.LTVtoCAC)+" ≥ 3.0 (healthy)",
		"LTV:CAC "+fmtRatio(m.LTVtoCAC)+" < 3.0 (acquisition under-returns)")
	paybackOK := pass(m.PaybackMonths <= paybackHealthy,
		"CAC payback "+fmtMonths(m.PaybackMonths)+" ≤ 12 mo (capital-efficient)",
		"CAC payback "+fmtMonths(m.PaybackMonths)+" > 12 mo (cash-hungry)")
	nrrOK := pass(m.NRRAnnual >= 1.0,
		"NRR "+fmtPct(m.NRRAnnual-0)+" ≥ 100% (base grows or holds)",
		"NRR "+fmtPct(m.NRRAnnual-0)+" < 100% (revenue leaks even before new sales)")

	switch {
	case m.LTVtoCAC < ratioFloor || m.PaybackMonths > paybackCeiling:
		return EconGate{Verdict: "NO-GO", Reasons: reasons}
	case ratioOK && paybackOK && nrrOK:
		return EconGate{Verdict: "GO", Reasons: reasons}
	default:
		return EconGate{Verdict: "CONDITIONAL", Reasons: reasons}
	}
}

// EconLevers back-solves the assumptions that would move a failing gate to
// healthy — the "what needs to be true" half of an intelligent model.
type EconLevers struct {
	MaxCACForRatio3     float64 // hold LTV; the highest CAC that still clears 3×
	MaxChurnForRatio3   float64 // hold ARPA/GM/CAC; the churn that yields exactly 3×
	MinARPAForPayback12 float64 // hold GM/CAC; the ARPA that recovers CAC in 12 mo
}

// ComputeLevers derives the break-even levers from a model.
func ComputeLevers(m EconModel) EconLevers {
	lv := EconLevers{}
	if !math.IsInf(m.LTV, 1) {
		lv.MaxCACForRatio3 = m.LTV / ratioHealthy
	} else {
		lv.MaxCACForRatio3 = math.Inf(1)
	}
	if m.CAC > 0 {
		lv.MaxChurnForRatio3 = m.MarginPerMonth / (ratioHealthy * m.CAC)
	}
	if m.GrossMargin > 0 {
		lv.MinARPAForPayback12 = m.CAC / (paybackHealthy * m.GrossMargin)
	}
	return lv
}

// EconScenario is one stress case (conservative/base/stretch).
type EconScenario struct {
	Name          string
	LTVtoCAC      float64
	PaybackMonths float64
	NRRAnnual     float64
}

// Scenarios stress the model by scaling churn/CAC/ARPA. Conservative worsens
// each lever ~25%; stretch improves them. This is the sensitivity analysis the
// canon (file 06) calls for before betting.
func Scenarios(m EconModel) []EconScenario {
	scale := func(churnK, cacK, arpaK float64) EconScenario {
		s := m
		s.MonthlyChurn *= churnK
		s.CAC *= cacK
		s.ARPA *= arpaK
		s.MarginPerMonth = s.ARPA * s.GrossMargin
		out := EconScenario{}
		if s.MonthlyChurn > 0 {
			ltv := s.MarginPerMonth / s.MonthlyChurn
			if s.CAC > 0 {
				out.LTVtoCAC = ltv / s.CAC
			} else {
				out.LTVtoCAC = math.Inf(1)
			}
		}
		if s.MarginPerMonth > 0 {
			out.PaybackMonths = s.CAC / s.MarginPerMonth
		} else {
			out.PaybackMonths = math.Inf(1)
		}
		out.NRRAnnual = math.Pow(1+s.MonthlyExpansion-s.MonthlyChurn, 12)
		return out
	}
	c := scale(1.25, 1.25, 0.85)
	c.Name = "Conservative"
	b := scale(1.0, 1.0, 1.0)
	b.Name = "Base"
	s := scale(0.75, 0.75, 1.15)
	s.Name = "Stretch"
	return []EconScenario{c, b, s}
}
