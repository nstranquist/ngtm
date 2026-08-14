package gtm

import (
	"context"
	"fmt"
	"time"
)

// Options configures one factory run.
type Options struct {
	Subject  string     // product/company/market under analysis (required)
	Query    string     // free-text research question (optional; defaults to Subject)
	Keywords []string   // seed keywords/topics (optional)
	Category string     // disambiguation hint for entity resolution (optional; e.g. "developer tools")
	Tiers    []FeedTier // allowed feed tiers; empty => {free}
	Provider string     // LLM provider for narrative ("" => offline)
	Model    string     // LLM model ("" => provider default)
	Offline  bool       // force offline generator (no LLM)
	NoFeeds  bool       // skip all live feeds (hermetic; fixtures only)
	Limit    int        // per-feed evidence cap (0 => feed default)
	// Claims overrides the embedded 06-02 competitor-claim set for compare mode
	// (keyed by competitor first-token, lowercased). Nil => embedded default.
	Claims map[string][]CorpusClaim
	// Pitch is the one-line value proposition the social vertical builds its
	// channel drafts around. Optional; blank renders an explicit [FILL:] slot
	// rather than an invented benefit.
	Pitch string
	// Channels selects distribution channels for the social vertical by key
	// (see Channels registry); empty => all.
	Channels []string
	// Tune runs the social vertical's self-review loop: per channel, generate a
	// candidate per hook archetype, score each against the rubric, keep the best.
	Tune bool
	// IdeaCount caps the ideate vertical's slate (0 => 5).
	IdeaCount int
	// Avoid lists existing product names the ideate vertical must not re-propose.
	Avoid []string
	// Inputs carries numeric assumptions for the model-driven verticals
	// (economics / pricing / motion): e.g. "acv", "cac", "gross_margin",
	// "monthly_churn", "expansion", "customers". A key being present means the
	// operator supplied it (a real assumption); a missing key is filled from an
	// analyst-benchmark default and labeled as such — that provenance is what
	// keeps the anti-hallucination contract intact for computed numbers.
	Inputs map[string]float64
}

// Input returns the supplied value for key and whether the operator provided it
// (vs. an analyst default the caller should substitute).
func (o Options) Input(key string) (float64, bool) {
	if o.Inputs == nil {
		return 0, false
	}
	v, ok := o.Inputs[key]
	return v, ok
}

// Engine binds the feed registry, the generator, and a clock. It is the single
// entry point every vertical and every surface (ndev gtm / ngtm / MCP) drives.
type Engine struct {
	reg *FeedRegistry
	gen Generator
	now func() time.Time
}

// NewEngine builds an engine from options, wiring the canonical feed registry
// and selecting a generator (offline unless a provider is named).
func NewEngine(opts Options, now func() time.Time) (*Engine, error) {
	if now == nil {
		now = time.Now
	}
	gen, err := NewGenerator(opts.Provider, opts.Model, opts.Offline)
	if err != nil {
		return nil, err
	}
	return &Engine{reg: NewFeedRegistry(now), gen: gen, now: now}, nil
}

// NewEngineWith injects a registry and generator directly — used by tests and
// by callers that want a custom feed set.
func NewEngineWith(reg *FeedRegistry, gen Generator, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{reg: reg, gen: gen, now: now}
}

// Registry exposes the feed registry (for `feeds` doctor output).
func (e *Engine) Registry() *FeedRegistry { return e.reg }

// Verticals are the supported analysis tracks.
var Verticals = []string{"seo", "business", "brand", "economics", "pricing", "motion", "social", "ideate"}

// Run dispatches to a vertical and returns a validated report.
func (e *Engine) Run(ctx context.Context, vertical string, opts Options) (*Report, error) {
	var (
		rep *Report
		err error
	)
	switch vertical {
	case "seo":
		rep, err = e.runSEO(ctx, opts)
	case "business":
		rep, err = e.runBusiness(ctx, opts)
	case "brand":
		rep, err = e.runBrand(ctx, opts)
	case "economics":
		rep, err = e.runEconomics(ctx, opts)
	case "pricing":
		rep, err = e.runPricing(ctx, opts)
	case "motion":
		rep, err = e.runMotion(ctx, opts)
	case "social":
		rep, err = e.runSocial(ctx, opts)
	case "ideate":
		rep, err = e.runIdeate(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown vertical %q (available: %v)", vertical, Verticals)
	}
	if err != nil || rep == nil {
		return rep, err
	}
	// Copyright/trademark advisories (distinct from citation-integrity in Validate);
	// surfaced as Warnings so a reviewer screens/rephrases before acting. See ipguard.go.
	if w := rep.ipWarnings(); len(w) > 0 {
		rep.Warnings = append(rep.Warnings, w...)
	}
	return rep, nil
}

func tierSet(tiers []FeedTier) map[FeedTier]bool {
	if len(tiers) == 0 {
		return map[FeedTier]bool{TierFree: true}
	}
	m := make(map[FeedTier]bool, len(tiers))
	for _, t := range tiers {
		m[t] = true
	}
	return m
}

func tierList(m map[FeedTier]bool) []FeedTier {
	var out []FeedTier
	for _, t := range []FeedTier{TierFree, TierCheap, TierPremium} {
		if m[t] {
			out = append(out, t)
		}
	}
	return out
}
