package gtm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ClaimVerdict is one claim's verdict at a point in time (persisted in state).
type ClaimVerdict struct {
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}

// MetricSample is one tracked numeric value at a point in time (e.g. a SERP
// rank, a keyword's search volume, a mention count, an employee headcount).
type MetricSample struct {
	Key   string  `json:"key"`  // stable identity, e.g. "serp_rank:infisical.com"
	Name  string  `json:"name"` // metric family, e.g. "serp_rank"
	Value float64 `json:"value"`
	Label string  `json:"label,omitempty"`
}

// WatchState is the persisted result of a prior run, used to detect drift:
// verdicts keyed by subject/competitor first-token then claim text, plus tracked
// metric samples per subject.
type WatchState struct {
	Generated string                             `json:"generated"`
	Subjects  []string                           `json:"subjects"`
	Verdicts  map[string]map[string]ClaimVerdict `json:"verdicts"`
	Metrics   map[string][]MetricSample          `json:"metrics,omitempty"`
}

// LoadWatchState reads a state file. A missing file is the first run — it
// returns an empty state, not an error.
func LoadWatchState(path string) (*WatchState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &WatchState{Verdicts: map[string]map[string]ClaimVerdict{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var ws WatchState
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse watch state: %w", err)
	}
	if ws.Verdicts == nil {
		ws.Verdicts = map[string]map[string]ClaimVerdict{}
	}
	return &ws, nil
}

// SaveWatchState writes a state file.
func SaveWatchState(path string, ws *WatchState) error {
	b, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// StateFromReport snapshots a CompareReport's verdicts + metrics into a state.
func StateFromReport(rep *CompareReport) *WatchState {
	ws := newWatchState(rep.Generated, rep.Subjects)
	for _, row := range rep.Rows {
		key := compareKey(row.Subject)
		urls := evidenceURLs(row.Evidence)
		m := map[string]ClaimVerdict{}
		for _, c := range row.ClaimChecks {
			m[c.Text] = ClaimVerdict{Status: string(c.Status), Source: firstCiteURL(c.Citations, urls)}
		}
		if len(m) > 0 {
			ws.Verdicts[key] = m
		}
		if ms := metricsFromEvidence(row.Evidence); len(ms) > 0 {
			ws.Metrics[key] = ms
		}
	}
	return ws
}

// StateFromVertical snapshots a single-subject vertical Report (seo/business/
// brand) into a state: claim verdicts use the claim Confidence (grounded /
// inferred / speculative); metrics come from the evidence.
func StateFromVertical(rep *Report) *WatchState {
	ws := newWatchState(rep.Generated, []string{rep.Subject})
	key := compareKey(rep.Subject)
	urls := evidenceURLs(rep.Evidence)
	m := map[string]ClaimVerdict{}
	for _, s := range rep.Sections {
		for _, c := range s.Claims {
			m[c.Text] = ClaimVerdict{Status: string(c.Confidence), Source: firstCiteURL(c.Citations, urls)}
		}
	}
	if len(m) > 0 {
		ws.Verdicts[key] = m
	}
	if ms := metricsFromEvidence(rep.Evidence); len(ms) > 0 {
		ws.Metrics[key] = ms
	}
	return ws
}

func newWatchState(generated string, subjects []string) *WatchState {
	return &WatchState{
		Generated: generated, Subjects: subjects,
		Verdicts: map[string]map[string]ClaimVerdict{},
		Metrics:  map[string][]MetricSample{},
	}
}

func evidenceURLs(ev []Evidence) map[string]string {
	urls := map[string]string{}
	for _, e := range ev {
		if e.URL != "" {
			urls[e.ID] = e.URL
		}
	}
	return urls
}

func firstCiteURL(citations []string, urls map[string]string) string {
	for _, cite := range citations {
		if u := urls[cite]; u != "" {
			return u
		}
	}
	return ""
}

var numMagRe = regexp.MustCompile(`([\d][\d,]*(?:\.\d+)?)\s*([KkMmBb])?`)

// parseMetricValue pulls the first numeric value out of an evidence Value,
// expanding K/M/B suffixes ("$12.0B" → 1.2e10, "2,500" → 2500, "1" → 1).
func parseMetricValue(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "$", "")
	m := numMagRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "k":
		f *= 1e3
	case "m":
		f *= 1e6
	case "b":
		f *= 1e9
	}
	return f, true
}

// metricsFromEvidence extracts trackable numeric metrics from evidence: SERP
// rank (per host), search volume (per keyword), aggregate mention count, and
// employee headcount.
func metricsFromEvidence(ev []Evidence) []MetricSample {
	var out []MetricSample
	mentions := 0
	for _, e := range ev {
		if e.Synthetic {
			continue
		}
		switch e.Metric {
		case "serp_rank":
			if v, ok := parseMetricValue(e.Value); ok {
				out = append(out, MetricSample{Key: "serp_rank:" + hostOf(e.URL), Name: "serp_rank", Value: v, Label: "rank · " + e.Title})
			}
		case "search_volume":
			if v, ok := parseMetricValue(e.Value); ok {
				out = append(out, MetricSample{Key: "search_volume:" + strings.ToLower(e.Title), Name: "search_volume", Value: v, Label: "volume · " + e.Title})
			}
		case "mentions":
			mentions++
		case "company_fact":
			if strings.Contains(strings.ToLower(e.Title), "employees") {
				if v, ok := parseMetricValue(e.Value); ok {
					out = append(out, MetricSample{Key: "employees", Name: "employees", Value: v, Label: "employees"})
				}
			}
		}
	}
	if mentions > 0 {
		out = append(out, MetricSample{Key: "mentions:count", Name: "mentions", Value: float64(mentions), Label: "public mentions"})
	}
	return out
}

// adverseMove decides whether a metric change is unfavorable: a SERP rank rising
// (you dropped), or search volume / mentions falling.
func adverseMove(metric string, from, to float64) bool {
	switch metric {
	case "serp_rank":
		return to > from
	case "search_volume", "mentions":
		return to < from
	default:
		return false
	}
}

// DriftEntry is one claim whose verdict changed (or appeared/disappeared).
type DriftEntry struct {
	Subject string `json:"subject"`
	Claim   string `json:"claim"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Source  string `json:"source,omitempty"`
}

// MetricDelta is one tracked metric that changed value between runs.
type MetricDelta struct {
	Subject string  `json:"subject"`
	Name    string  `json:"name"`
	Label   string  `json:"label,omitempty"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Adverse bool    `json:"adverse"`
}

// DriftReport is the diff between two fact-check runs.
type DriftReport struct {
	Since       string        `json:"since"`
	Flips       []DriftEntry  `json:"flips"`
	New         []DriftEntry  `json:"new"`
	Removed     []DriftEntry  `json:"removed"`
	Metrics     []MetricDelta `json:"metrics,omitempty"`
	Stable      int           `json:"stable"`
	Regressions int           `json:"regressions"` // status regressions + adverse metric moves
}

// HasDrift reports whether anything changed.
func (dr *DriftReport) HasDrift() bool {
	return len(dr.Flips) > 0 || len(dr.New) > 0 || len(dr.Removed) > 0 || len(dr.Metrics) > 0
}

// isStrong marks the "good" verdict states across surfaces: compare uses
// "confirmed"; the verticals use "grounded". A regression is a move away from
// strong.
func isStrong(s string) bool {
	return s == string(StatusConfirmed) || s == string(ConfGrounded)
}

// isRegression: a previously-strong claim that is no longer strong (✅→❌ or
// ✅→🔴, or grounded→inferred/speculative) — the case worth alerting on.
func isRegression(from, to string) bool {
	return isStrong(from) && !isStrong(to)
}

// DiffStates compares a previous and current state and reports the drift.
func DiffStates(prev, cur *WatchState) *DriftReport {
	dr := &DriftReport{Since: prev.Generated}
	for _, key := range sortedStateKeys(cur.Verdicts) {
		subject := displaySubject(cur, key)
		curClaims := cur.Verdicts[key]
		for _, claim := range sortedStateKeys2(curClaims) {
			cv := curClaims[claim]
			pv, existed := prevVerdict(prev, key, claim)
			switch {
			case !existed:
				dr.New = append(dr.New, DriftEntry{Subject: subject, Claim: claim, To: cv.Status, Source: cv.Source})
			case pv.Status != cv.Status:
				dr.Flips = append(dr.Flips, DriftEntry{Subject: subject, Claim: claim, From: pv.Status, To: cv.Status, Source: cv.Source})
				if isRegression(pv.Status, cv.Status) {
					dr.Regressions++
				}
			default:
				dr.Stable++
			}
		}
	}
	for _, key := range sortedStateKeys(prev.Verdicts) {
		for _, claim := range sortedStateKeys2(prev.Verdicts[key]) {
			if _, ok := cur.Verdicts[key][claim]; !ok {
				dr.Removed = append(dr.Removed, DriftEntry{Subject: displaySubject(prev, key), Claim: claim, From: prev.Verdicts[key][claim].Status})
			}
		}
	}

	// Metric-delta drift: a tracked value moved (e.g. a SERP rank you owned
	// dropped, or search volume fell). Adverse moves count as regressions.
	for _, key := range sortedMetricKeys(cur.Metrics) {
		subject := displaySubject(cur, key)
		prevByKey := indexSamples(prev.Metrics[key])
		for _, s := range cur.Metrics[key] {
			ps, ok := prevByKey[s.Key]
			if !ok || ps.Value == s.Value {
				continue
			}
			adverse := adverseMove(s.Name, ps.Value, s.Value)
			dr.Metrics = append(dr.Metrics, MetricDelta{Subject: subject, Name: s.Name, Label: s.Label, From: ps.Value, To: s.Value, Adverse: adverse})
			if adverse {
				dr.Regressions++
			}
		}
	}
	return dr
}

func indexSamples(samples []MetricSample) map[string]MetricSample {
	m := make(map[string]MetricSample, len(samples))
	for _, s := range samples {
		m[s.Key] = s
	}
	return m
}

func sortedMetricKeys(m map[string][]MetricSample) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Markdown renders a compact drift report.
func (dr *DriftReport) Markdown() string {
	var b strings.Builder
	since := dr.Since
	if since == "" {
		since = "first run (no prior state)"
	}
	fmt.Fprintf(&b, "# GTM drift — since %s\n\n", since)
	if dr.Regressions > 0 {
		fmt.Fprintf(&b, "> ⚠️ **%d regression(s)** — claims that were confirmed are no longer.\n\n", dr.Regressions)
	}
	if !dr.HasDrift() {
		fmt.Fprintf(&b, "✅ No drift — %d claims stable.\n", dr.Stable)
		return b.String()
	}
	if len(dr.Flips) > 0 {
		b.WriteString("## Flips\n")
		for _, e := range dr.Flips {
			line := fmt.Sprintf("- %s **%s** / %s: `%s` → `%s`", driftArrow(e.From, e.To), e.Subject, claimSnippet(e.Claim), e.From, e.To)
			if e.Source != "" {
				line += " — <" + e.Source + ">"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	if len(dr.New) > 0 {
		b.WriteString("## New claims\n")
		for _, e := range dr.New {
			fmt.Fprintf(&b, "- **%s** / %s: `%s`\n", e.Subject, claimSnippet(e.Claim), e.To)
		}
		b.WriteString("\n")
	}
	if len(dr.Removed) > 0 {
		b.WriteString("## Removed claims\n")
		for _, e := range dr.Removed {
			fmt.Fprintf(&b, "- **%s** / %s (was `%s`)\n", e.Subject, claimSnippet(e.Claim), e.From)
		}
		b.WriteString("\n")
	}
	if len(dr.Metrics) > 0 {
		b.WriteString("## Metric changes\n")
		for _, m := range dr.Metrics {
			icon := "🔄"
			if m.Adverse {
				icon = "⚠️"
			}
			fmt.Fprintf(&b, "- %s **%s** / %s: %s → %s%s\n", icon, m.Subject, dash(m.Label), fmtNum(m.From), fmtNum(m.To), adverseTag(m.Adverse))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "_%d stable · %d flipped (%d regressions) · %d new · %d removed · %d metric changes_\n",
		dr.Stable, len(dr.Flips), dr.Regressions, len(dr.New), len(dr.Removed), len(dr.Metrics))
	return b.String()
}

func fmtNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func adverseTag(adverse bool) string {
	if adverse {
		return " _(adverse)_"
	}
	return ""
}

// PostDriftWebhook POSTs the drift report to a Slack-compatible incoming webhook
// (`{"text": …}`) — so a scheduled run can alert instead of silently writing a
// file. No-op when webhookURL is empty.
func PostDriftWebhook(ctx context.Context, webhookURL string, dr *DriftReport) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"text": dr.Markdown()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := feedHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook responded %d", resp.StatusCode)
	}
	return nil
}

// JSON renders the drift report as indented JSON.
func (dr *DriftReport) JSON() ([]byte, error) { return json.MarshalIndent(dr, "", "  ") }

// driftArrow flags the direction: ⚠️ regression (toward less-verified), ✅
// improvement (now confirmed), 🔄 lateral.
func driftArrow(from, to string) string {
	switch {
	case to == string(StatusConfirmed):
		return "✅"
	case from == string(StatusConfirmed) && to != string(StatusConfirmed):
		return "⚠️"
	case to == string(StatusContradicted):
		return "⚠️"
	default:
		return "🔄"
	}
}

func prevVerdict(ws *WatchState, key, claim string) (ClaimVerdict, bool) {
	if m, ok := ws.Verdicts[key]; ok {
		if v, ok := m[claim]; ok {
			return v, true
		}
	}
	return ClaimVerdict{}, false
}

func displaySubject(ws *WatchState, key string) string {
	for _, s := range ws.Subjects {
		if compareKey(s) == key {
			return s
		}
	}
	return key
}

func sortedStateKeys(m map[string]map[string]ClaimVerdict) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStateKeys2(m map[string]ClaimVerdict) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
