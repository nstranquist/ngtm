package gtm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Compare runs the business-vertical evidence gather across a set of subjects
// (competitors) and produces one grounded teardown table. It reuses the rail —
// the same feeds, evidence, and citation discipline — but skips the per-subject
// LLM narrative (a table doesn't need prose). For known competitors it also
// checks the embedded 06-02 GTM corpus claims against the live evidence and
// labels each confirmed / contradicted / unverified.

// ClaimStatus is the verdict of a corpus-claim check against live evidence.
type ClaimStatus string

const (
	StatusConfirmed    ClaimStatus = "confirmed"    // evidence supports the claim
	StatusContradicted ClaimStatus = "contradicted" // evidence is present and disagrees
	StatusUnverified   ClaimStatus = "unverified"   // no evidence to judge (feed unkeyed/empty)
)

// ClaimCheck is one corpus claim weighed against the evidence.
type ClaimCheck struct {
	Text      string      `json:"text"`
	Status    ClaimStatus `json:"status"`
	Citations []string    `json:"citations,omitempty"`
	Note      string      `json:"note,omitempty"`
}

// CompareRow is one competitor's grounded line in the teardown.
type CompareRow struct {
	Subject     string       `json:"subject"`
	Industry    string       `json:"industry,omitempty"`
	Employees   string       `json:"employees,omitempty"`
	HQ          string       `json:"hq,omitempty"`
	Founded     string       `json:"founded,omitempty"`
	Mentions    int          `json:"mentions"`
	H1          string       `json:"h1,omitempty"`      // top SERP title (when SERP feed keyed)
	Pricing     string       `json:"pricing,omitempty"` // pricing signal (when found)
	PanelMedian float64      `json:"panel_median"`
	Survives    bool         `json:"survives"`
	Evidence    []Evidence   `json:"evidence"`
	ClaimChecks []ClaimCheck `json:"claim_checks,omitempty"`
	Warnings    []string     `json:"warnings,omitempty"`
}

// CompareReport is the full teardown.
type CompareReport struct {
	Subjects  []string     `json:"subjects"`
	Generated string       `json:"generated"`
	Tiers     []FeedTier   `json:"tiers"`
	Rows      []CompareRow `json:"rows"`
}

// Compare gathers evidence per subject (sequentially — gentler on feed rate
// limits than N parallel sweeps) and builds the teardown.
func (e *Engine) Compare(ctx context.Context, subjects []string, opts Options) (*CompareReport, error) {
	subjects = dedupe(trimAll(subjects))
	if len(subjects) == 0 {
		return nil, fmt.Errorf("compare needs at least one subject")
	}
	tiers := tierSet(opts.Tiers)
	if opts.NoFeeds {
		tiers = map[FeedTier]bool{}
	}
	// Pluggable claim source: an external --claims map replaces the embedded
	// 06-02 default when provided.
	claimsMap := opts.Claims
	if claimsMap == nil {
		claimsMap = corpusCompetitorClaims
	}
	rep := &CompareReport{
		Subjects:  subjects,
		Generated: e.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Tiers:     tierList(tiers),
	}
	for _, subj := range subjects {
		ev, warnings := e.reg.Gather(ctx, FeedQuery{Subject: subj, Keywords: opts.Keywords, Limit: opts.Limit, Category: opts.Category}, tiers)
		realEv := nonSynthetic(ev)
		facts := filterMetric(realEv, "company_fact")
		mentions := filterMetric(realEv, "mentions")
		serp := filterMetric(realEv, "serp_rank")
		panel := RunBusinessPanel(subj, "", ev)
		row := CompareRow{
			Subject:   subj,
			Industry:  factValue(facts, "industry"),
			Employees: factValue(facts, "employees"),
			HQ:        factValue(facts, "headquarters"),
			Founded:   factValue(facts, "inception"),
			Mentions:  len(mentions),
			// Prefer the competitor's own homepage H1/price (landing feed) over
			// SERP snippets.
			H1:          firstNonEmpty(evValue(filterMetric(realEv, "h1")), topH1(serp)),
			Pricing:     firstNonEmpty(evValue(filterMetric(realEv, "pricing")), pricingSignal(serp)),
			PanelMedian: panel.MedianScore,
			Survives:    panel.Survives,
			Evidence:    ev,
			ClaimChecks: checkCorpusClaims(subj, realEv, claimsMap),
			Warnings:    warnings,
		}
		rep.Rows = append(rep.Rows, row)
	}
	return rep, nil
}

func evValue(ev []Evidence) string {
	for _, e := range ev {
		if strings.TrimSpace(e.Value) != "" {
			return e.Value
		}
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func topH1(serp []Evidence) string {
	for _, e := range serp {
		if e.Value == "1" {
			return e.Title
		}
	}
	if len(serp) > 0 {
		return serp[0].Title
	}
	return ""
}

func pricingSignal(serp []Evidence) string {
	for _, e := range serp {
		for _, hay := range []string{e.Title, e.Snippet} {
			if i := strings.IndexByte(hay, '$'); i >= 0 {
				frag := hay[i:]
				if len(frag) > 32 {
					frag = frag[:32]
				}
				return strings.TrimSpace(frag)
			}
		}
	}
	return ""
}

// --- 06-02 corpus competitor claims (from 02-positioning-and-seo.md) ---------

// CorpusClaim is one competitor assertion to verify against live evidence.
// Kind ∈ {serp, stat, pricing, mentions, narrative}; Needle is the substring to
// find in evidence of that kind ("" => existence / qualitative check).
type CorpusClaim struct {
	Text   string `json:"text" yaml:"text"`
	Kind   string `json:"kind" yaml:"kind"`
	Needle string `json:"needle" yaml:"needle"`
}

// corpusCompetitorClaims is the embedded 06-02 default, used when no external
// --claims source is supplied.
var corpusCompetitorClaims = map[string][]CorpusClaim{
	"infisical": {
		{Text: `H1 "Secure Secrets, Certificates, and AI Agents"`, Kind: "serp", Needle: "Secure Secrets"},
		{Text: `Claims "500M+ secrets daily"`, Kind: "stat", Needle: "500M"},
		{Text: `Pricing ~$18/user/mo`, Kind: "pricing", Needle: "18"},
		{Text: `Abandoned its zero-knowledge story`, Kind: "narrative", Needle: ""},
	},
	"doppler": {
		{Text: `H1 "Secure secrets. Prevent breaches."`, Kind: "serp", Needle: "Prevent breaches"},
		{Text: `Claims "76k+ orgs"`, Kind: "stat", Needle: "76k"},
		{Text: `Pricing ~$21/user/mo`, Kind: "pricing", Needle: "21"},
		{Text: `Is not zero-knowledge`, Kind: "narrative", Needle: ""},
	},
	"akeyless": {
		{Text: `H1 "Runtime Identity Security at Agentic Scale"`, Kind: "serp", Needle: "Runtime Identity Security"},
		{Text: `Enterprise/sales-led, expensive`, Kind: "pricing", Needle: ""},
	},
	"nvault": {
		{Text: `Public demand exists for the zero-knowledge SecretOps wedge`, Kind: "mentions", Needle: ""},
	},
}

func checkCorpusClaims(subject string, realEv []Evidence, claims map[string][]CorpusClaim) []ClaimCheck {
	cs := claims[compareKey(subject)]
	if len(cs) == 0 {
		return nil
	}
	out := make([]ClaimCheck, 0, len(cs))
	for _, c := range cs {
		out = append(out, verifyCorpusClaim(c, realEv))
	}
	return out
}

func verifyCorpusClaim(c CorpusClaim, realEv []Evidence) ClaimCheck {
	check := ClaimCheck{Text: c.Text}
	switch c.Kind {
	case "mentions":
		m := filterMetric(realEv, "mentions")
		if len(m) > 0 {
			check.Status = StatusConfirmed
			check.Citations = pickIDs(m, 3)
			return check
		}
		check.Status = StatusUnverified
		check.Note = "no relevant public mentions found (free HN; Reddit 403s unauthenticated)"
		return check
	case "serp":
		// Prefer the competitor's own homepage H1 (landing feed); fall back to
		// SERP snippets. The homepage H1 is the source of truth for H1 claims.
		h1 := filterMetric(realEv, "h1")
		ev := append(append([]Evidence{}, h1...), filterMetric(realEv, "serp_rank")...)
		if len(ev) == 0 {
			check.Status = StatusUnverified
			check.Note = "needs the landing feed (`--paid`) or SERPER_API_KEY to fetch the homepage / SERP H1"
			return check
		}
		if cites := findNeedle(ev, c.Needle); len(cites) > 0 {
			check.Status = StatusConfirmed
			check.Citations = cites
			return check
		}
		check.Status = StatusContradicted
		if len(h1) > 0 {
			check.Note = "homepage H1 fetched but the claimed text was not present"
		} else {
			check.Note = "SERP fetched but the claimed text was not in the top results"
		}
		check.Citations = ids(ev)
		return check
	case "stat":
		// Body-stat claims are checked against the full homepage text. Absence
		// from static HTML is NOT a contradiction (the stat may be JS-rendered);
		// it's unverified until a scrape provider renders the page.
		page := filterMetric(realEv, "page")
		if cites := findNeedle(page, c.Needle); len(cites) > 0 {
			check.Status = StatusConfirmed
			check.Citations = cites
			return check
		}
		check.Status = StatusUnverified
		if len(page) == 0 {
			check.Note = "needs the landing feed (`--paid`) to fetch the homepage"
		} else {
			check.Note = "not in the static homepage text — may be JS-rendered (set SCRAPE_API_KEY)"
		}
		return check
	case "pricing":
		price := filterMetric(realEv, "pricing")
		if len(price) == 0 {
			check.Status = StatusUnverified
			check.Note = "needs the landing feed (`--paid`) to fetch the /pricing page; pricing is time-sensitive"
			return check
		}
		if c.Needle == "" {
			check.Status = StatusUnverified
			check.Note = "qualitative pricing claim — review the fetched price manually"
			check.Citations = ids(price)
			return check
		}
		if cites := findNeedle(price, c.Needle); len(cites) > 0 {
			check.Status = StatusConfirmed
			check.Citations = cites
			return check
		}
		check.Status = StatusContradicted
		check.Note = "pricing page fetched but the claimed figure was not present"
		check.Citations = ids(price)
		return check
	default: // "narrative"
		check.Status = StatusUnverified
		check.Note = "stance/positioning claim — needs the competitor's own page (Serper) or manual review"
		return check
	}
}

func findNeedle(ev []Evidence, needle string) []string {
	if needle == "" {
		return nil
	}
	n := strings.ToLower(needle)
	var cites []string
	for _, e := range ev {
		if strings.Contains(strings.ToLower(e.Title), n) || strings.Contains(strings.ToLower(e.Snippet), n) {
			cites = append(cites, e.ID)
		}
	}
	return cites
}

func compareKey(subject string) string {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(subject)))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func trimAll(ss []string) []string {
	var out []string
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// --- rendering ---

// JSON renders the teardown as indented JSON.
func (r *CompareReport) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown renders the teardown table + per-competitor corpus-claim checks.
func (r *CompareReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Competitor Teardown — %s\n\n", strings.Join(r.Subjects, ", "))
	fmt.Fprintf(&b, "_%s · tiers: %s · grounded (firmographics/mentions from free feeds; H1/pricing need SERP keys)_\n\n", r.Generated, joinTiers(r.Tiers))

	b.WriteString("| Competitor | Industry | Employees | HQ | Founded | Mentions | H1 | Panel |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, row := range r.Rows {
		mark := "✗"
		if row.Survives {
			mark = "✓"
		}
		fmt.Fprintf(&b, "| **%s** | %s | %s | %s | %s | %d | %s | %.1f %s |\n",
			row.Subject, dash(row.Industry), dash(row.Employees), dash(row.HQ),
			dash(row.Founded), row.Mentions, dash(truncate(row.H1, 40)), row.PanelMedian, mark)
	}
	b.WriteString("\n")

	// Corpus claim checks per competitor.
	any := false
	for _, row := range r.Rows {
		if len(row.ClaimChecks) == 0 {
			continue
		}
		if !any {
			b.WriteString("## 06-02 corpus claim checks\n\n")
			b.WriteString("Legend: ✅ confirmed · ❌ contradicted · 🔴 unverified (no evidence to judge)\n\n")
			any = true
		}
		fmt.Fprintf(&b, "### %s\n", row.Subject)
		for _, c := range row.ClaimChecks {
			icon := claimIcon(c.Status)
			line := fmt.Sprintf("- %s **%s** — %s", icon, c.Status, c.Text)
			if len(c.Citations) > 0 {
				line += " — sources: " + strings.Join(bracket(c.Citations), ", ")
			}
			if c.Note != "" {
				line += " _(" + c.Note + ")_"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	// Roll-up.
	conf, con, unv := 0, 0, 0
	for _, row := range r.Rows {
		for _, c := range row.ClaimChecks {
			switch c.Status {
			case StatusConfirmed:
				conf++
			case StatusContradicted:
				con++
			default:
				unv++
			}
		}
	}
	if conf+con+unv > 0 {
		fmt.Fprintf(&b, "**Corpus-claim roll-up:** %d confirmed · %d contradicted · %d unverified. "+
			"Most unverified claims need the cheap SERP feeds (`SERPER_API_KEY` / `DATAFORSEO_*`) — wire them and re-run to convert assumptions into measured facts.\n", conf, con, unv)
	}
	return b.String()
}

func claimIcon(s ClaimStatus) string {
	switch s {
	case StatusConfirmed:
		return "✅"
	case StatusContradicted:
		return "❌"
	default:
		return "🔴"
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
