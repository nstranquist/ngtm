package gtm

import (
	"fmt"
	"sort"
	"strings"
)

// survivalThreshold is the median conviction score (0-10) at or above which a
// thesis is judged to "survive" the panel.
const survivalThreshold = 6.0

// RunPanel stress-tests a thesis from several adversarial lenses. Crucially,
// each critic's KILLS are derived from real gaps in the gathered evidence —
// not invented by an LLM — so the panel is as trustworthy as the feeds. A
// thesis backed only by synthetic/fixture evidence cannot pass.
//
// The wedge string is the proposed positioning/thesis under test.
func RunPanel(subject, wedge string, ev []Evidence) *PanelResult {
	realVol := countReal(ev, func(e Evidence) bool { return e.Metric == "search_volume" })
	realRank := countReal(ev, func(e Evidence) bool { return e.Metric == "serp_rank" })
	competitors := distinctHosts(ev)
	anyReal := countReal(ev, func(Evidence) bool { return true }) > 0

	var verdicts []Verdict

	// 1. Demand — is anyone actually searching for this?
	verdicts = append(verdicts, demandVerdict(realVol))

	// 2. SERP saturation — how crowded is the space we'd compete in?
	verdicts = append(verdicts, serpVerdict(realRank, competitors))

	// 3. Differentiation — can we tell a distinct story vs. who's there?
	verdicts = append(verdicts, diffVerdict(competitors))

	// 4. Evidence integrity — is the thesis resting on data or on vibes?
	verdicts = append(verdicts, integrityVerdict(anyReal))

	// 5. Wedge specificity — is the positioning sharp or a platitude?
	verdicts = append(verdicts, wedgeVerdict(subject, wedge))

	return assemblePanel(verdicts)
}

// assemblePanel computes the aggregate verdict (median score, deduped kills,
// survival) from a set of critic verdicts. Shared by every vertical's panel.
func assemblePanel(verdicts []Verdict) *PanelResult {
	scores := make([]float64, len(verdicts))
	var kills []string
	for i, v := range verdicts {
		scores[i] = float64(v.Score)
		kills = append(kills, v.Kills...)
	}
	med := median(scores)
	return &PanelResult{
		Verdicts:    verdicts,
		MedianScore: med,
		TopKills:    dedupe(kills),
		Survives:    med >= survivalThreshold,
	}
}

// RunBusinessPanel stress-tests a business thesis from investor lenses. As with
// the SEO panel, every kill is derived from a real gap in the evidence — a plan
// with no traction, firmographic, or unit-economics evidence cannot pass. This
// is the "shark tank": the unit-economics critics fire by default precisely
// because CAC/LTV/margin are assumptions until a feed (or the founder) supplies
// real numbers.
func RunBusinessPanel(subject, thesis string, ev []Evidence) *PanelResult {
	facts := countReal(ev, func(e Evidence) bool { return e.Metric == "company_fact" })
	mentions := countReal(ev, func(e Evidence) bool { return e.Metric == "mentions" })
	unitEcon := countReal(ev, func(e Evidence) bool { return isUnitEconMetric(e.Metric) })
	margin := countReal(ev, func(e Evidence) bool { return e.Metric == "margin" || e.Metric == "pricing" })
	competitors := distinctHosts(ev)
	anyReal := countReal(ev, func(Evidence) bool { return true }) > 0

	verdicts := []Verdict{
		marketVerdict(facts),
		tractionVerdict(mentions),
		cacLtvVerdict(unitEcon),
		marginVerdict(margin),
		moatVerdict(competitors),
		integrityVerdict(anyReal),
	}
	return assemblePanel(verdicts)
}

func marketVerdict(facts int) Verdict {
	if facts >= 1 {
		return Verdict{Critic: "Market / Firmographics", Score: 7,
			Rationale: "Structured company/market facts are present to anchor sizing."}
	}
	return Verdict{Critic: "Market / Firmographics", Score: 3,
		Kills:     []string{"No firmographic/market data — TAM/SAM/SOM is unanchored."},
		Rationale: "No Wikidata/Crunchbase/PDL facts ran. Market sizing rests on assumptions."}
}

func tractionVerdict(mentions int) Verdict {
	switch {
	case mentions >= 4:
		return Verdict{Critic: "Traction", Score: 7,
			Rationale: "Multiple public mentions (HN/Reddit) — there is measurable interest."}
	case mentions >= 1:
		return Verdict{Critic: "Traction", Score: 5,
			Kills:     []string{"Thin public traction — few mentions found."},
			Rationale: "Some interest, but not enough signal to claim demand."}
	default:
		return Verdict{Critic: "Traction", Score: 3,
			Kills:     []string{"No public mentions — no evidence anyone is talking about this."},
			Rationale: "No HN/Reddit mentions surfaced. Traction is unproven."}
	}
}

func cacLtvVerdict(unitEcon int) Verdict {
	if unitEcon >= 1 {
		return Verdict{Critic: "Unit Economics (CAC/LTV)", Score: 7,
			Rationale: "Unit-economics evidence is present to test payback."}
	}
	return Verdict{Critic: "Unit Economics (CAC/LTV)", Score: 2,
		Kills:     []string{"No CAC/LTV evidence — payback and scalability are guesses."},
		Rationale: "No unit-economics feed supplied CAC, LTV, or ARPU. These are the numbers that kill startups; supply them before betting."}
}

func marginVerdict(margin int) Verdict {
	if margin >= 1 {
		return Verdict{Critic: "Margin / Pricing", Score: 7,
			Rationale: "Pricing/margin evidence is present."}
	}
	return Verdict{Critic: "Margin / Pricing", Score: 3,
		Kills:     []string{"No margin/pricing evidence — gross margin is unknown."},
		Rationale: "Without pricing or margin data, the model's profitability is unverified."}
}

func moatVerdict(competitors int) Verdict {
	switch {
	case competitors >= 4:
		return Verdict{Critic: "Moat / Defensibility", Score: 4,
			Kills:     []string{"Crowded field — defensibility is unclear against 4+ players."},
			Rationale: "Many comparable sources surfaced; the moat must be argued explicitly."}
	case competitors >= 1:
		return Verdict{Critic: "Moat / Defensibility", Score: 6,
			Rationale: "A competitive set exists — a defensibility story is possible."}
	default:
		return Verdict{Critic: "Moat / Defensibility", Score: 4,
			Kills:     []string{"No competitive evidence — either a blank space or thin data."},
			Rationale: "Can't assess a moat without knowing who else is in the market."}
	}
}

// RunBrandPanel stress-tests a brand/identity direction. Kills derive from
// evidence gaps just like the other panels: an undifferentiated, category-less
// brand resting on no evidence cannot pass.
func RunBrandPanel(subject, concept string, ev []Evidence) *PanelResult {
	competitors := distinctHosts(ev)
	hasCategory := countReal(ev, func(e Evidence) bool { return e.Metric == "company_fact" }) > 0
	anyReal := countReal(ev, func(Evidence) bool { return true }) > 0

	verdicts := []Verdict{
		distinctivenessVerdict(competitors),
		categoryClarityVerdict(hasCategory),
		wedgeVerdict(subject, concept), // reuse: is the concept specific & anchored?
		integrityVerdict(anyReal),
	}
	return assemblePanel(verdicts)
}

func distinctivenessVerdict(competitors int) Verdict {
	switch {
	case competitors >= 5:
		return Verdict{Critic: "Distinctiveness", Score: 4,
			Kills:     []string{"Crowded visual/competitive space — the mark must work hard to stand out."},
			Rationale: "Many comparable players surfaced; a generic identity will blend in."}
	case competitors >= 1:
		return Verdict{Critic: "Distinctiveness", Score: 6,
			Rationale: "A competitive set exists to differentiate against."}
	default:
		return Verdict{Critic: "Distinctiveness", Score: 5,
			Kills:     []string{"No competitive evidence — distinctiveness is asserted, not tested."},
			Rationale: "Without knowing the field, we can't verify the brand stands apart."}
	}
}

func categoryClarityVerdict(hasCategory bool) Verdict {
	if hasCategory {
		return Verdict{Critic: "Category Clarity", Score: 7,
			Rationale: "A grounded category anchors the brand's meaning."}
	}
	return Verdict{Critic: "Category Clarity", Score: 4,
		Kills:     []string{"No grounded category — the brand has nothing concrete to signal."},
		Rationale: "Identity work is premature without a verified category/positioning."}
}

// RunEntityNamePanel scores a legal/company name. Domain availability is not
// a critic. Collision + evidence integrity are.
func RunEntityNamePanel(subject string, ev []Evidence, collision string) *PanelResult {
	anyReal := countReal(ev, func(Evidence) bool { return true }) > 0
	verdicts := []Verdict{
		entityCollisionVerdict(subject, collision),
		integrityVerdict(anyReal),
	}
	p := assemblePanel(verdicts)
	p.Title = "Legal-name screen"
	return p
}

func entityCollisionVerdict(subject, collision string) Verdict {
	if collision != "" {
		return Verdict{Critic: "Name collision", Score: 3,
			Kills:     []string{"A software/company entity already uses this name — " + collision + "."},
			Rationale: "Legal-name collision is the load-bearing check; domains are not."}
	}
	return Verdict{Critic: "Name collision", Score: 7,
		Rationale: "No same-name software/company entity in the structured sources for " + subject + " (not USPTO clearance)."}
}

// RunEconomicsPanel is the "CFO panel" for the economics vertical. Each critic
// scores a real unit-economics threshold (LTV:CAC, payback, NRR, gross margin);
// the integrity critic fires when the model rests on analyst defaults rather
// than operator-supplied numbers — the same anti-hallucination discipline as the
// shark-tank panel, applied to a financial model.
func RunEconomicsPanel(m EconModel, gate EconGate, growth GrowthMetrics, providedCore, totalCore int) *PanelResult {
	verdicts := []Verdict{
		ratioVerdict(m.LTVtoCAC),
	}
	// One-time products have no recurring payback/NRR to score.
	if !m.OneTime {
		verdicts = append(verdicts, paybackVerdict(m.PaybackMonths), nrrVerdict(m.NRRAnnual))
	}
	verdicts = append(verdicts, grossMarginVerdict(m.GrossMargin))
	verdicts = append(verdicts, GrowthVerdicts(growth)...) // classical investor metrics, when supplied
	verdicts = append(verdicts, assumptionIntegrityVerdict(providedCore, totalCore))
	p := assemblePanel(verdicts)
	p.Title = "CFO Panel"
	return p
}

func ratioVerdict(r float64) Verdict {
	switch {
	case r >= ratioHealthy:
		return Verdict{Critic: "LTV:CAC Ratio", Score: 8,
			Rationale: "LTV:CAC " + fmtRatio(r) + " ≥ 3 — acquisition returns are healthy."}
	case r >= ratioFloor:
		return Verdict{Critic: "LTV:CAC Ratio", Score: 5,
			Kills:     []string{"LTV:CAC " + fmtRatio(r) + " is below 3 — acquisition under-returns."},
			Rationale: "Each customer pays back more than CAC but not the 3× the canon wants."}
	default:
		return Verdict{Critic: "LTV:CAC Ratio", Score: 2,
			Kills:     []string{"LTV:CAC " + fmtRatio(r) + " < 1 — every customer loses money."},
			Rationale: "Acquisition destroys value; fix CAC or retention before any spend."}
	}
}

func paybackVerdict(months float64) Verdict {
	switch {
	case months <= paybackHealthy:
		return Verdict{Critic: "CAC Payback", Score: 8,
			Rationale: "Payback " + fmtMonths(months) + " ≤ 12 mo — capital-efficient."}
	case months <= 18:
		return Verdict{Critic: "CAC Payback", Score: 5,
			Kills:     []string{"Payback " + fmtMonths(months) + " (12–18 mo) — cash-hungry."},
			Rationale: "Recovery is slow; needs more working capital to scale."}
	default:
		return Verdict{Critic: "CAC Payback", Score: 2,
			Kills:     []string{"Payback " + fmtMonths(months) + " > 18 mo — capital trap."},
			Rationale: "You may run out of cash before recovering acquisition cost."}
	}
}

func nrrVerdict(nrr float64) Verdict {
	switch {
	case nrr >= nrrHealthy:
		return Verdict{Critic: "Retention / NRR", Score: 8,
			Rationale: "NRR " + fmtPct(nrr) + " ≥ 110% — expansion outruns churn (negative churn)."}
	case nrr >= 1.0:
		return Verdict{Critic: "Retention / NRR", Score: 6,
			Rationale: "NRR " + fmtPct(nrr) + " ≥ 100% — the base holds; expansion is thin."}
	default:
		return Verdict{Critic: "Retention / NRR", Score: 3,
			Kills:     []string{"NRR " + fmtPct(nrr) + " < 100% — revenue leaks before any new sale."},
			Rationale: "A leaky base caps growth no matter how good acquisition is."}
	}
}

func grossMarginVerdict(gm float64) Verdict {
	switch {
	case gm >= 0.70:
		return Verdict{Critic: "Gross Margin", Score: 7,
			Rationale: "Gross margin " + fmtPct(gm) + " ≥ 70% — software-grade economics."}
	case gm >= 0.50:
		return Verdict{Critic: "Gross Margin", Score: 5,
			Kills:     []string{"Gross margin " + fmtPct(gm) + " (50–70%) — services-heavy drag."},
			Rationale: "Margin limits LTV and reinvestment capacity."}
	default:
		return Verdict{Critic: "Gross Margin", Score: 3,
			Kills:     []string{"Gross margin " + fmtPct(gm) + " < 50% — every LTV dollar is expensive."},
			Rationale: "Low margin makes the whole model fragile."}
	}
}

func assumptionIntegrityVerdict(provided, total int) Verdict {
	switch {
	case total > 0 && provided >= total:
		return Verdict{Critic: "Assumption Integrity", Score: 8,
			Rationale: fmt.Sprintf("All %d core inputs are operator-supplied — this is a model of real numbers.", total)}
	case provided == 0:
		return Verdict{Critic: "Assumption Integrity", Score: 1,
			Kills:     []string{"Every input is an analyst default — this is a guess, not a measurement."},
			Rationale: "Supply real ACV/CAC/churn/margin before trusting the verdict."}
	default:
		return Verdict{Critic: "Assumption Integrity", Score: 4,
			Kills:     []string{fmt.Sprintf("Only %d of %d core inputs are real — the rest are defaults.", provided, total)},
			Rationale: "Mixed provenance; replace the remaining defaults to harden the verdict."}
	}
}

// RunPricingPanel stress-tests a value-based price. Kills fire when the price
// rests on placeholder value anchors, captures implausibly little/much surplus,
// or has no WTP validation behind it.
func RunPricingPanel(m PricingModel, providedAnchors int) *PanelResult {
	var verdicts []Verdict

	// 1. Value anchor integrity — is the WTP ceiling real or placeholder?
	switch {
	case providedAnchors >= 2:
		verdicts = append(verdicts, Verdict{Critic: "Value Anchor", Score: 8,
			Rationale: "Both the next-best price and the quantified differentiation value are operator-supplied."})
	case providedAnchors == 1:
		verdicts = append(verdicts, Verdict{Critic: "Value Anchor", Score: 4,
			Kills:     []string{"Only one value anchor is real — the WTP ceiling is half a guess."},
			Rationale: "Supply both the alternative price and the quantified differentiation value."})
	default:
		verdicts = append(verdicts, Verdict{Critic: "Value Anchor", Score: 1,
			Kills:     []string{"Both value anchors are placeholders — this price is unvalidated."},
			Rationale: "Quantify the alternative price and the buyer's gain before pricing."})
	}

	// 2. Surplus split — are we leaving the buyer a reason to say yes?
	share := m.CaptureShare
	switch {
	case share >= 0.3 && share <= 0.7:
		verdicts = append(verdicts, Verdict{Critic: "Surplus Split", Score: 7,
			Rationale: fmt.Sprintf("Capturing %s of differentiation leaves the buyer real surplus.", fmtPct(share))})
	case share > 0.7:
		verdicts = append(verdicts, Verdict{Critic: "Surplus Split", Score: 4,
			Kills:     []string{fmt.Sprintf("Capturing %s leaves little surplus — adoption friction rises.", fmtPct(share))},
			Rationale: "Greedy capture slows the sale; consider leaving more value on the table early."})
	default:
		verdicts = append(verdicts, Verdict{Critic: "Surplus Split", Score: 5,
			Kills:     []string{fmt.Sprintf("Capturing only %s — you may be under-monetizing.", fmtPct(share))},
			Rationale: "Low capture wins deals but leaves revenue uncollected."})
	}

	// 3. WTP validation — always a standing objection until a survey runs.
	verdicts = append(verdicts, Verdict{Critic: "WTP Validation", Score: 4,
		Kills:     []string{"No willingness-to-pay survey behind the number — run Van Westendorp/Gabor-Granger."},
		Rationale: "Computed WTP is a model; a price is only validated by real buyer responses."})

	// 4. Tier coherence — does the ladder span the WTP distribution?
	if len(m.Tiers) >= 3 && m.Tiers[2].Price > m.Tiers[0].Price {
		verdicts = append(verdicts, Verdict{Critic: "Tier Coherence", Score: 7,
			Rationale: "Good-Better-Best ladder spans entry to high-WTP with a clear anchor."})
	} else {
		verdicts = append(verdicts, Verdict{Critic: "Tier Coherence", Score: 3,
			Kills:     []string{"Tier ladder is degenerate — no usable spread."},
			Rationale: "Tiers collapsed (likely a zero/placeholder price)."})
	}

	p := assemblePanel(verdicts)
	p.Title = "Pricing Panel"
	return p
}

// RunMotionPanel stress-tests a GTM-motion recommendation: is the ACV real, is
// the motion economically matched to it, is the funnel measurable, and is there
// a validation plan behind it.
func RunMotionPanel(m MotionModel) *PanelResult {
	var verdicts []Verdict

	if m.ACVProvided {
		verdicts = append(verdicts, Verdict{Critic: "Motion Fit (ACV)", Score: 8,
			Rationale: fmt.Sprintf("ACV %s is operator-supplied and the motion is matched to it.", fmtMoney(m.ACV))})
	} else {
		verdicts = append(verdicts, Verdict{Critic: "Motion Fit (ACV)", Score: 3,
			Kills:     []string{"ACV is a placeholder — motion choice is unanchored. Pass --acv."},
			Rationale: "The whole recommendation pivots on ACV; supply the real figure."})
	}

	verdicts = append(verdicts, Verdict{Critic: "Funnel Instrumentation", Score: 6,
		Rationale: fmt.Sprintf("A %d-stage funnel with metrics and benchmarks is specified — instrument it before scaling.", len(m.Funnel))})

	verdicts = append(verdicts, Verdict{Critic: "Validation Readiness", Score: 4,
		Kills:     []string{"Demand not yet validated — run the discovery interviews + Sean Ellis 40% test before scaling spend."},
		Rationale: "A motion executed before PMF just fills a leaky bucket faster."})

	verdicts = append(verdicts, Verdict{Critic: "Channel Loop Quality", Score: 6,
		Rationale: "Channels are listed; prioritize the ones that compound (content/SEO, virality) over purely linear paid."})

	p := assemblePanel(verdicts)
	p.Title = "GTM Motion Panel"
	return p
}

func isUnitEconMetric(m string) bool {
	switch m {
	case "cac", "ltv", "arpu", "unit_econ", "payback":
		return true
	default:
		return false
	}
}

func demandVerdict(realVol int) Verdict {
	switch {
	case realVol >= 1:
		return Verdict{Critic: "Demand", Score: 8,
			Rationale: "Live search-volume evidence is present; demand is measured, not assumed."}
	default:
		return Verdict{Critic: "Demand", Score: 3,
			Kills:     []string{"No search-volume data — we don't know anyone is looking for this."},
			Rationale: "No keyword-volume feed ran (free/local tier can't measure volume). Add DataForSEO/Serper before betting on this term."}
	}
}

func serpVerdict(realRank, competitors int) Verdict {
	switch {
	case realRank == 0:
		return Verdict{Critic: "SERP Saturation", Score: 3,
			Kills:     []string{"No live SERP data — competitive saturation is unknown."},
			Rationale: "No SERP feed ran. We can't see who already owns the page-one real estate."}
	case competitors >= 6:
		return Verdict{Critic: "SERP Saturation", Score: 4,
			Kills:     []string{"Crowded SERP: 6+ distinct incumbents already rank."},
			Rationale: "The space is contested; ranking will be slow and expensive without a sharper wedge."}
	default:
		return Verdict{Critic: "SERP Saturation", Score: 7,
			Rationale: "SERP is measured and not fully saturated — there is room to rank."}
	}
}

func diffVerdict(competitors int) Verdict {
	switch {
	case competitors >= 2:
		return Verdict{Critic: "Differentiation", Score: 7,
			Rationale: "Multiple incumbents identified — a contrast story is possible and necessary."}
	case competitors == 1:
		return Verdict{Critic: "Differentiation", Score: 5,
			Kills:     []string{"Only one competitor surfaced — either a thin space or thin data."},
			Rationale: "Single incumbent seen; gather more before claiming a clear gap."}
	default:
		return Verdict{Critic: "Differentiation", Score: 3,
			Kills:     []string{"No competitors surfaced — can't position against a void."},
			Rationale: "Without competitor evidence the differentiation claim is unfalsifiable."}
	}
}

func integrityVerdict(anyReal bool) Verdict {
	if anyReal {
		return Verdict{Critic: "Evidence Integrity", Score: 8,
			Rationale: "Thesis rests on at least some live evidence."}
	}
	return Verdict{Critic: "Evidence Integrity", Score: 1,
		Kills:     []string{"All evidence is synthetic — this is a vibe, not a finding."},
		Rationale: "Only fixture data is present. Every conclusion is speculative until a real feed runs."}
}

func wedgeVerdict(subject, wedge string) Verdict {
	w := strings.TrimSpace(wedge)
	mentionsSubject := subject != "" && strings.Contains(strings.ToLower(w), strings.ToLower(strings.Fields(subject + " ")[0]))
	switch {
	case len(w) < 24:
		return Verdict{Critic: "Wedge Specificity", Score: 3,
			Kills:     []string{"Positioning is too thin to be testable."},
			Rationale: "A wedge this short is a slogan, not a strategy."}
	case !mentionsSubject:
		return Verdict{Critic: "Wedge Specificity", Score: 5,
			Kills:     []string{"Wedge is generic — doesn't anchor to the subject."},
			Rationale: "Sharpen the wedge so it could only describe this product."}
	default:
		return Verdict{Critic: "Wedge Specificity", Score: 7,
			Rationale: "Wedge is specific and anchored to the subject."}
	}
}

// --- helpers ---

func countReal(ev []Evidence, pred func(Evidence) bool) int {
	n := 0
	for _, e := range ev {
		if !e.Synthetic && pred(e) {
			n++
		}
	}
	return n
}

func distinctHosts(ev []Evidence) int {
	hosts := map[string]bool{}
	for _, e := range ev {
		if e.Synthetic || e.URL == "" {
			continue
		}
		hosts[hostOf(e.URL)] = true
	}
	return len(hosts)
}

func hostOf(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "www.")
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}
