package gtm

import "fmt"

// FeedTier classifies a data feed by what it costs to run. The factory
// defaults to free/local feeds; cheap pay-per-call feeds are opt-in; premium
// subscription feeds are a roadmap tier (not wired yet).
type FeedTier string

const (
	TierFree    FeedTier = "free"    // no key, no per-call cost (Wikidata, local heuristics)
	TierCheap   FeedTier = "cheap"   // pennies per call, key-gated (Serper, Brave, DataForSEO)
	TierPremium FeedTier = "premium" // subscription (Ahrefs/Semrush/SimilarWeb) — roadmap
)

// Confidence labels how an assertion in a report is justified. It is the
// load-bearing field of the whole factory: the entire point of pairing real
// feeds with an LLM (instead of a local LLM alone) is that we can tell the
// difference between a measured fact and a model's guess — and never present
// the second as the first.
type Confidence string

const (
	// ConfGrounded — backed by >=1 non-synthetic Evidence from a real feed.
	// Validate() rejects a grounded claim that cites nothing real.
	ConfGrounded Confidence = "grounded"
	// ConfInferred — a reasoned conclusion drawn from grounded evidence
	// (e.g. a positioning wedge). References evidence but is not itself a fact.
	ConfInferred Confidence = "inferred"
	// ConfSpeculative — no real evidence backs it (offline/fixture mode, or a
	// creative suggestion). Always labeled as such in the rendered report.
	ConfSpeculative Confidence = "speculative"
)

// Evidence is one atomic, attributable datum returned by a Feed. Every fact
// in a report traces back to an Evidence by ID.
type Evidence struct {
	ID        string            `json:"id"`      // stable id, e.g. "serper:2", "wikidata:Q95"
	Feed      string            `json:"feed"`    // feed name that produced it
	Tier      FeedTier          `json:"tier"`    // free | cheap | premium
	Title     string            `json:"title"`   // human label
	Snippet   string            `json:"snippet"` // the content / datum
	URL       string            `json:"url,omitempty"`
	Metric    string            `json:"metric,omitempty"` // e.g. "search_volume", "rank"
	Value     string            `json:"value,omitempty"`  // metric value as string
	Retrieved string            `json:"retrieved"`        // RFC3339
	Synthetic bool              `json:"synthetic"`        // true => may NOT back a grounded claim
	Extra     map[string]string `json:"extra,omitempty"`
}

// Claim is one assertion in a report. A grounded claim must cite at least one
// non-synthetic Evidence; the contract is enforced by Report.Validate.
type Claim struct {
	Text       string     `json:"text"`
	Confidence Confidence `json:"confidence"`
	Citations  []string   `json:"citations"` // Evidence IDs
}

// Section is a titled block of the report: prose narrative plus the claims it
// rests on. Prose is written by the Generator; claims are derived from feeds.
type Section struct {
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	Claims []Claim `json:"claims,omitempty"`
}

// Verdict is one adversarial critic's stress-test of the thesis.
type Verdict struct {
	Critic    string   `json:"critic"`
	Score     int      `json:"score"` // 0-10 conviction the thesis survives this lens
	Kills     []string `json:"kills"` // weaknesses that would sink it
	Rationale string   `json:"rationale"`
}

// PanelResult aggregates the adversarial panel.
type PanelResult struct {
	Title       string    `json:"title,omitempty"` // panel name (default "Shark-Tank Panel")
	Verdicts    []Verdict `json:"verdicts"`
	MedianScore float64   `json:"median_score"`
	TopKills    []string  `json:"top_kills"`
	Survives    bool      `json:"survives"` // median >= survival threshold
}

// Report is the full artifact a vertical produces.
type Report struct {
	Vertical  string       `json:"vertical"`  // "seo" | "business" | "brand"
	Subject   string       `json:"subject"`   // what we analyzed
	Query     string       `json:"query"`     // the research query
	Generated string       `json:"generated"` // RFC3339
	Provider  string       `json:"provider"`  // LLM provider used, or "offline"
	Model     string       `json:"model,omitempty"`
	Tiers     []FeedTier   `json:"tiers"`    // feed tiers consulted
	Evidence  []Evidence   `json:"evidence"` // every datum gathered
	Sections  []Section    `json:"sections"`
	Panel     *PanelResult `json:"panel,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"` // feed errors, validation issues
	// Metrics and Verdict are the machine-readable summary of a model-driven
	// vertical (economics/pricing/motion): the computed numbers and the headline
	// decision, hoisted out of section prose so callers can rank/compare ideas
	// without parsing Markdown. Only finite values are included (Inf/NaN would
	// break json.Marshal). Empty for feed verticals.
	Metrics map[string]float64 `json:"metrics,omitempty"`
	Verdict string             `json:"verdict,omitempty"`
}

// SetMetric records a model output for the JSON summary, skipping non-finite
// values (Inf/NaN) which are not valid JSON and would fail marshaling.
func (r *Report) SetMetric(key string, v float64) {
	if mathIsFinite(v) {
		if r.Metrics == nil {
			r.Metrics = map[string]float64{}
		}
		r.Metrics[key] = v
	}
}

// Validate enforces the citation-integrity contract and returns a list of
// human-readable violations (empty == clean). This is what makes the factory
// trustworthy: a "grounded" claim that cites nothing real, or cites only
// synthetic/fixture evidence, is a violation — exactly the failure mode that
// produces confident-but-false GTM claims.
func (r *Report) Validate() []string {
	real := make(map[string]bool, len(r.Evidence))
	known := make(map[string]bool, len(r.Evidence))
	for _, e := range r.Evidence {
		known[e.ID] = true
		if !e.Synthetic {
			real[e.ID] = true
		}
	}
	var violations []string
	for _, s := range r.Sections {
		for _, c := range s.Claims {
			for _, cite := range c.Citations {
				if !known[cite] {
					violations = append(violations, fmt.Sprintf(
						"section %q: claim cites unknown evidence %q: %s", s.Title, cite, c.Text))
				}
			}
			if c.Confidence == ConfGrounded {
				if !anyReal(c.Citations, real) {
					violations = append(violations, fmt.Sprintf(
						"section %q: GROUNDED claim has no real (non-synthetic) citation: %s", s.Title, c.Text))
				}
			}
		}
	}
	return violations
}

func anyReal(citations []string, real map[string]bool) bool {
	for _, c := range citations {
		if real[c] {
			return true
		}
	}
	return false
}
