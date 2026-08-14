package gtm

import (
	"context"
	"fmt"
	"strings"
)

// PricingModel derives a value-based price from the economic value delivered,
// the next-best alternative, and how much of the differentiation value the
// vendor captures (the rest is customer surplus — the reason the buyer says yes).
type PricingModel struct {
	NextBestPrice float64
	DiffValue     float64
	Negatives     float64
	CaptureShare  float64 // 0..1 of differentiation value captured by price
	Ceiling       float64 // max WTP = nextBest + diff − negatives
	Recommended   float64 // nextBest + capture×diff − negatives
	Surplus       float64 // Ceiling − Recommended (left to the customer)
	Tiers         []PriceTier
}

// PriceTier is one good-better-best offer.
type PriceTier struct {
	Name  string
	Price float64
	Note  string
}

var pricingDefaults = map[string]struct {
	val    float64
	label  string
	source string
}{
	"next_best_price": {1000, "Next-best alternative price (annual)", "[analyst] placeholder — SUPPLY the real alternative price"},
	"diff_value":      {1000, "Quantified value of your differentiation", "[analyst] placeholder — quantify the buyer's gain (time/$ saved, risk avoided)"},
	"negatives":       {0, "Value of switching costs / negatives", "[analyst] assumed 0"},
	"value_capture":   {0.5, "Share of differentiation value captured by price", "[analyst] 50% capture leaves half the value as customer surplus"},
}

// ComputePricing builds the value-based price and a good-better-best ladder.
func ComputePricing(opts Options) (PricingModel, []EconInput) {
	resolve := func(key string) EconInput {
		d := pricingDefaults[key]
		if v, ok := opts.Input(key); ok {
			return EconInput{Key: key, Label: d.label, Value: v, Provided: true, Source: "operator-supplied"}
		}
		return EconInput{Key: key, Label: d.label, Value: d.val, Provided: false, Source: d.source}
	}
	nb := resolve("next_best_price")
	dv := resolve("diff_value")
	neg := resolve("negatives")
	cap := resolve("value_capture")

	m := PricingModel{
		NextBestPrice: nb.Value, DiffValue: dv.Value, Negatives: neg.Value, CaptureShare: cap.Value,
	}
	m.Ceiling = m.NextBestPrice + m.DiffValue - m.Negatives
	m.Recommended = m.NextBestPrice + m.CaptureShare*m.DiffValue - m.Negatives
	if m.Recommended < 0 {
		m.Recommended = 0
	}
	m.Surplus = m.Ceiling - m.Recommended
	good := roundPrice(0.6 * m.Recommended)
	best := roundPrice(1.8 * m.Recommended)
	m.Tiers = []PriceTier{
		{Name: "Good (land)", Price: good, Note: "entry tier to win the segment; undercuts the alternative on a narrow scope"},
		{Name: "Better (anchor)", Price: roundPrice(m.Recommended), Note: "the value-based price; make this the default/most-popular"},
		{Name: "Best (expand)", Price: best, Note: "captures high-WTP buyers; approaches the WTP ceiling " + fmtMoney(m.Ceiling)},
	}
	return m, []EconInput{nb, dv, neg, cap}
}

func roundPrice(v float64) float64 {
	if v <= 0 {
		return 0
	}
	// charm-round to a clean number
	switch {
	case v >= 1000:
		return float64(int(v/100+0.5)) * 100
	case v >= 100:
		return float64(int(v/10+0.5))*10 - 1 // e.g. 199
	default:
		return float64(int(v + 0.5))
	}
}

func (e *Engine) runPricing(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}
	m, inputs := ComputePricing(opts)
	now := e.now().UTC().Format("2006-01-02T15:04:05Z07:00")

	var evidence []Evidence
	provided := 0
	for _, in := range inputs {
		val := pricingInputVal(in)
		evidence = append(evidence, Evidence{
			ID: "input:" + in.Key, Feed: pick(in.Provided, "operator", "benchmark"), Tier: TierFree,
			Title: in.Label + " = " + val, Snippet: in.Source, Metric: "pricing", Value: val,
			Retrieved: now, Synthetic: !in.Provided,
		})
		if in.Provided && in.Key != "value_capture" && in.Key != "negatives" {
			provided++
		}
	}

	sections := []Section{
		pricingValueSection(m),
		pricingTierSection(m),
		pricingValueMetricSection(),
		pricingWTPSection(opts.Subject),
	}
	thesis := fmt.Sprintf("%s value-based price ≈ %s (WTP ceiling %s; %s left as customer surplus).",
		opts.Subject, fmtMoney(m.Recommended), fmtMoney(m.Ceiling), fmtMoney(m.Surplus))
	narr, err := e.pricingNarrative(ctx, opts.Subject, m)
	var warnings []string
	if err != nil {
		warnings = append(warnings, "narrative generation failed: "+err.Error())
		narr = thesis
	}
	sections = append(sections, Section{Title: "Pricing Strategist's Read (inferred)", Body: narr,
		Claims: []Claim{{Text: thesis, Confidence: ConfInferred, Citations: []string{"input:next_best_price", "input:diff_value"}}}})

	report := &Report{
		Vertical: "pricing", Subject: opts.Subject, Query: query, Generated: now,
		Provider: e.gen.Provider(), Model: e.gen.Model(), Tiers: []FeedTier{TierFree},
		Evidence: evidence, Sections: sections,
		Panel:    RunPricingPanel(m, provided),
		Warnings: warnings,
	}
	report.Verdict = fmt.Sprintf("recommend %s", fmtMoney(m.Recommended))
	report.SetMetric("wtp_ceiling", m.Ceiling)
	report.SetMetric("recommended_price", m.Recommended)
	report.SetMetric("customer_surplus", m.Surplus)
	report.SetMetric("capture_share", m.CaptureShare)
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func pricingValueSection(m PricingModel) Section {
	rows := [][]string{
		{"Next-best alternative", fmtMoney(m.NextBestPrice), "what the buyer uses today"},
		{"+ Value of differentiation", fmtMoney(m.DiffValue), "quantified buyer gain"},
		{"− Switching/negatives", fmtMoney(m.Negatives), "friction to adopt"},
		{"= WTP ceiling", fmtMoney(m.Ceiling), "max economic value to buyer"},
		{"Recommended price", fmtMoney(m.Recommended), fmt.Sprintf("captures %s of differentiation", fmtPct(m.CaptureShare))},
		{"Customer surplus left", fmtMoney(m.Surplus), "the reason they say yes"},
	}
	body := "Value-based pricing prices to the economic value delivered, then leaves enough surplus that the buyer wins too.\n\n" +
		mdTable([]string{"Component", "Value", "Meaning"}, rows)
	return Section{Title: "Value-Based Price", Body: body, Claims: []Claim{
		{Text: "WTP ceiling = " + fmtMoney(m.Ceiling), Confidence: ConfInferred, Citations: []string{"input:next_best_price", "input:diff_value"}},
		{Text: "Recommended price = " + fmtMoney(m.Recommended), Confidence: ConfInferred, Citations: []string{"input:next_best_price", "input:diff_value", "input:value_capture"}},
	}}
}

func pricingTierSection(m PricingModel) Section {
	var rows [][]string
	for _, t := range m.Tiers {
		rows = append(rows, []string{t.Name, fmtMoney(t.Price), t.Note})
	}
	body := "Good-Better-Best: three versioned offers spanning the WTP distribution. Anchor the middle as the default — most buyers pick it; the Best tier captures high-WTP accounts and makes Better look reasonable (the decoy/anchoring effect).\n\n" +
		mdTable([]string{"Tier", "Price", "Role"}, rows)
	return Section{Title: "Good-Better-Best Tiers", Body: body}
}

func pricingValueMetricSection() Section {
	body := "Charge for the **value metric** — the unit that scales with the value the customer gets, so revenue grows as they succeed (this is the mechanism behind expansion / negative churn). Pick 1–3 axes max:\n\n" +
		"- **Per-seat** — simple, predictable; decouples from value when usage ≠ headcount; can penalize adoption.\n" +
		"- **Usage-based** — tightest value alignment (API calls, GB, events); noisier to forecast, shifts volume risk to you.\n" +
		"- **Per-outcome / per-entity** — charge per the thing the product produces (leads tracked, deals closed, documents processed).\n" +
		"- **Flat tiers** — predictable; caps expansion.\n\n" +
		"Decision rule: the value metric should (1) align with customer value, (2) grow naturally with adoption, (3) be predictable to the buyer."
	return Section{Title: "Value Metric (how to charge)", Body: body}
}

func pricingWTPSection(subject string) Section {
	body := "Before committing, validate WTP — don't assert it. Cheapest instrument is the **Van Westendorp Price Sensitivity Meter**: survey target buyers with these four questions, then plot the cumulative curves to read an acceptable range + optimal point.\n\n" +
		fmt.Sprintf("1. At what monthly price would %s be **so expensive** you would not consider it?\n", subject) +
		fmt.Sprintf("2. At what price would %s be **expensive, but you'd still consider it**?\n", subject) +
		fmt.Sprintf("3. At what price would %s be **a bargain — a great buy**?\n", subject) +
		fmt.Sprintf("4. At what price would %s be **so cheap you'd doubt its quality**?\n\n", subject) +
		"For a chosen price point, follow up with **Gabor-Granger** (buy/no-buy at discrete prices → revenue-maximizing point); for tier/feature design use **conjoint** (trade-off choices → value of each feature). Van Westendorp = range, Gabor-Granger = point, conjoint = feature value."
	return Section{Title: "WTP Validation (Van Westendorp survey)", Body: body}
}

func (e *Engine) pricingNarrative(ctx context.Context, subject string, m PricingModel) (string, error) {
	facts := fmt.Sprintf("next_best=%s; diff_value=%s; wtp_ceiling=%s; recommended=%s; surplus=%s; capture=%s",
		fmtMoney(m.NextBestPrice), fmtMoney(m.DiffValue), fmtMoney(m.Ceiling), fmtMoney(m.Recommended), fmtMoney(m.Surplus), fmtPct(m.CaptureShare))
	if e.gen.Provider() == "offline" {
		return fmt.Sprintf("Price %s to value: recommend %s against a %s alternative, leaving %s of surplus.", subject, fmtMoney(m.Recommended), fmtMoney(m.NextBestPrice), fmtMoney(m.Surplus)) + "\n\nDrivers: " + facts, nil
	}
	sys := "You are a precise pricing strategist. Use ONLY the numbers in FACTS — invent no figures. In 2 short paragraphs explain the recommended price, why the surplus matters, and the single biggest pricing risk."
	user := fmt.Sprintf("SUBJECT: %s\n\nFACTS:\n%s", subject, facts)
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user, MaxTokens: 600})
}

func pricingInputVal(in EconInput) string {
	if in.Key == "value_capture" {
		return fmtPct(in.Value)
	}
	return fmtMoney(in.Value)
}
