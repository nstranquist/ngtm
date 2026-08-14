package gtm

import (
	"context"
	"fmt"
	"strings"
)

// Motion-selection thresholds (sources: David Skok; SaaS Capital ACV medians).
// The cost of the motion must fit the deal size: a human rep can't profitably
// close a tiny ACV; self-serve can't navigate a committee enterprise buy.
const (
	plgCeiling = 5000.0  // ACV below this → product-led / self-serve
	slgFloor   = 25000.0 // ACV above this → sales-led
)

// MotionModel is the recommended go-to-market motion and its operating system.
type MotionModel struct {
	ACV         float64
	ACVProvided bool
	Motion      string
	Rationale   string
	Funnel      []FunnelStage
	Channels    []string
}

// FunnelStage is one stage of the chosen motion's funnel/loop.
type FunnelStage struct {
	Name      string
	Metric    string
	Benchmark string
}

// SelectMotion maps ACV to a motion with its funnel and channel system.
func SelectMotion(acv float64) MotionModel {
	switch {
	case acv < plgCeiling:
		return MotionModel{
			ACV:    acv,
			Motion: "Product-Led Growth (self-serve)",
			Rationale: fmt.Sprintf("ACV %s < %s: a human sales motion can't pay for itself. The product must acquire, activate, convert, and expand itself; monetize individual adoption that spreads bottom-up.",
				fmtMoney(acv), fmtMoney(plgCeiling)),
			Funnel: []FunnelStage{
				{"Visitor → Signup", "signup rate", "freemium ~6% of visitors; trial 3–4%"},
				{"Signup → Activation (aha)", "activation rate, time-to-value", "define the 'aha' (e.g. Facebook's 7-friends)"},
				{"Activation → PQL", "product-qualified-lead rate", "score on in-product usage, not form-fills"},
				{"PQL → Paid", "conversion", "freemium ~5%; free trial ~17%; PQL ~25–30%"},
				{"Paid → Expansion", "NRR", ">100% via the value metric (seats/usage)"},
			},
			Channels: []string{"Content / SEO (compounding loop)", "Product virality / referral loop", "Community & developer relations", "Integrations / marketplace listings", "Free tools as top-of-funnel"},
		}
	case acv > slgFloor:
		return MotionModel{
			ACV:    acv,
			Motion: "Sales-Led Growth",
			Rationale: fmt.Sprintf("ACV %s > %s (and likely a buying committee): the deal supports — and the complexity requires — human multi-threading, demos, and procurement navigation.",
				fmtMoney(acv), fmtMoney(slgFloor)),
			Funnel: []FunnelStage{
				{"Lead", "leads captured", "visitor→lead 1–5%"},
				{"MQL", "marketing-qualified", "lead→MQL 25–35%"},
				{"SQL", "sales-qualified (BANT)", "MQL→SQL 13–26%"},
				{"Opportunity", "active deal w/ decision-maker", "SQL→Opp 50–62%"},
				{"Closed-Won", "bookings", "Opp→Won 15–30%"},
			},
			Channels: []string{"Founder-led / outbound SDR→AE", "Account-based marketing (ABM)", "Partnerships / channel / resellers", "Field events & executive dinners", "Inbound demo requests (from content)"},
		}
	default:
		return MotionModel{
			ACV:    acv,
			Motion: "Hybrid — PLG bottom-up + inside-sales overlay",
			Rationale: fmt.Sprintf("ACV %s sits in the mid-band (%s–%s): seed adoption self-serve, then route product-qualified accounts to inside sales to expand into a contract.",
				fmtMoney(acv), fmtMoney(plgCeiling), fmtMoney(slgFloor)),
			Funnel: []FunnelStage{
				{"Self-serve signup", "signup rate", "PLG top-of-funnel"},
				{"Activation (aha)", "activation rate", "fast time-to-value"},
				{"PQL → sales hand-off", "PQL rate", "behavior-triggered, not MQL form-fills"},
				{"Inside-sales close", "PQL→won", "AE expands the account to a contract"},
				{"Expansion", "NRR", "land-and-expand on the value metric"},
			},
			Channels: []string{"Content / SEO", "Product virality", "Targeted outbound to PQL accounts", "Community", "Partnerships"},
		}
	}
}

func (e *Engine) runMotion(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	query := opts.Query
	if query == "" {
		query = opts.Subject
	}
	acv, acvOK := opts.Input("acv")
	if !acvOK {
		acv = econDefaults["acv"].val
	}
	m := SelectMotion(acv)
	m.ACVProvided = acvOK
	now := e.now().UTC().Format("2006-01-02T15:04:05Z07:00")

	acvStr := fmtMoney(acv)
	evidence := []Evidence{{
		ID: "input:acv", Feed: pick(acvOK, "operator", "benchmark"), Tier: TierFree,
		Title: "Annual contract value = " + acvStr, Value: acvStr, Metric: "pricing",
		Snippet: pick(acvOK, "operator-supplied", econDefaults["acv"].source), Retrieved: now, Synthetic: !acvOK,
	}}

	sections := []Section{
		motionSection(m),
		motionFunnelSection(m),
		motionChannelSection(m),
		motionBeachheadSection(opts.Subject),
		motionValidationSection(opts.Subject),
	}
	thesis := fmt.Sprintf("%s at %s ACV → %s.", opts.Subject, acvStr, m.Motion)
	narr, err := e.motionNarrative(ctx, opts.Subject, m)
	var warnings []string
	if err != nil {
		warnings = append(warnings, "narrative generation failed: "+err.Error())
		narr = thesis
	}
	sections = append(sections, Section{Title: "GTM Strategist's Read (inferred)", Body: narr,
		Claims: []Claim{{Text: thesis, Confidence: ConfInferred, Citations: []string{"input:acv"}}}})

	report := &Report{
		Vertical: "motion", Subject: opts.Subject, Query: query, Generated: now,
		Provider: e.gen.Provider(), Model: e.gen.Model(), Tiers: []FeedTier{TierFree},
		Evidence: evidence, Sections: sections,
		Panel:    RunMotionPanel(m),
		Warnings: warnings,
	}
	report.Verdict = m.Motion
	report.SetMetric("acv", m.ACV)
	if v := report.Validate(); len(v) > 0 {
		report.Warnings = append(report.Warnings, v...)
	}
	return report, nil
}

func motionSection(m MotionModel) Section {
	body := fmt.Sprintf("**Recommended motion: %s**\n\n%s\n\nRule of thumb: ACV < %s → product-led; ACV > %s → sales-led; in between → hybrid.",
		m.Motion, m.Rationale, fmtMoney(plgCeiling), fmtMoney(slgFloor))
	conf := ConfInferred
	return Section{Title: "Motion Selection", Body: body, Claims: []Claim{
		{Text: "Recommended GTM motion: " + m.Motion, Confidence: conf, Citations: []string{"input:acv"}},
	}}
}

func motionFunnelSection(m MotionModel) Section {
	var rows [][]string
	for _, s := range m.Funnel {
		rows = append(rows, []string{s.Name, s.Metric, s.Benchmark})
	}
	body := "Instrument every stage; the boundary to watch is the hand-off (MQL→SQL in sales-led, PQL→sales in hybrid). Prefer behavior-triggered PQLs over form-fill MQLs.\n\n" +
		mdTable([]string{"Stage", "Metric", "Benchmark"}, rows)
	return Section{Title: "Funnel & Metrics", Body: body}
}

func motionChannelSection(m MotionModel) Section {
	var b strings.Builder
	b.WriteString("Channels that fit this motion (build compounding **loops** where possible, not just linear funnels):\n\n")
	for _, c := range m.Channels {
		b.WriteString("- " + c + "\n")
	}
	return Section{Title: "Channels", Body: b.String()}
}

func motionBeachheadSection(subject string) Section {
	body := fmt.Sprintf("Win one segment completely before expanding (Moore's beachhead / Aulet's \"big enough to matter, small enough to win\"). For %s:\n\n", subject) +
		"- **ICP (account):** derive from your best existing customers — highest NRR, lowest churn, fastest close (firmographic + technographic).\n" +
		"- **Persona (human):** the individual who feels the pain and can champion the buy.\n" +
		"- **Beachhead test:** dominate it, earn references, then bowling-pin into the adjacent segment.\n" +
		"- **Size it bottom-up:** `# ICP accounts × ACV` (use `ngtm economics --customers N --acv X`), never top-down %-of-a-huge-market."
	return Section{Title: "Beachhead & ICP", Body: body}
}

func motionValidationSection(subject string) Section {
	body := "GTM rests on validated demand, not asserted demand. Two instruments — run them before scaling spend:\n\n" +
		"**A. Customer-discovery interview script (problem-first, no pitching):**\n" +
		"1. Walk me through the last time you faced <the problem>. What did you do?\n" +
		"2. What's the hardest part of that? Why is it hard?\n" +
		"3. What have you tried to solve it? What did/didn't work?\n" +
		"4. If you had a magic wand, what would the ideal outcome look like?\n" +
		"5. (Only at the end) Here's what we're considering — what's missing / wrong?\n\n" +
		fmt.Sprintf("**B. PMF signal — the Sean Ellis 40%% test.** Once people use %s, survey: *\"How would you feel if you could no longer use %s?\"* (Very disappointed / Somewhat / Not). ", subject, subject) +
		"**≥ 40% \"very disappointed\" ⇒ likely product-market fit.** Pair with a flattening retention cohort curve. Do not scale acquisition until both clear."
	return Section{Title: "Customer Validation & PMF Plan", Body: body}
}

func (e *Engine) motionNarrative(ctx context.Context, subject string, m MotionModel) (string, error) {
	facts := fmt.Sprintf("acv=%s; recommended_motion=%s", fmtMoney(m.ACV), m.Motion)
	if e.gen.Provider() == "offline" {
		return m.Rationale, nil
	}
	sys := "You are a precise go-to-market strategist. Use ONLY the FACTS. In 2 short paragraphs justify the motion for this ACV, name the first channel to test, and the biggest execution risk. Invent no numbers."
	user := fmt.Sprintf("SUBJECT: %s\n\nFACTS:\n%s\n\nMOTION RATIONALE: %s", subject, facts, m.Rationale)
	return e.gen.Generate(ctx, GenPrompt{System: sys, User: user, MaxTokens: 600})
}
