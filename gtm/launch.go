package gtm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/internal/jsonl"
	"github.com/nstranquist/ngtm/internal/lockfile"
)

// The launch loop treats product launches like build artifacts in a metered
// pipeline: planned → kit → posted → signal → verdict, recorded in an
// append-only JSONL ledger and grouped into weekly cohorts ("ship 20 a week,
// see which lands"). Signals carry provenance exactly like report claims do:
// feed-measured events (hackernews/reddit) are observed; operator-recorded
// conversions are operator; the verdict states what drove it.

// LaunchEventType is the closed set of rows accepted by the launch ledger.
type LaunchEventType string

// SignalMetric is a metric that may contribute to a traction verdict. Unknown
// metrics are rejected instead of receiving an accidental default weight.
type SignalMetric string

// SignalSource identifies who observed a signal. It is intentionally open to
// typed external importers (for example posthog-export), but shape-validated.
type SignalSource string

// LaunchVerdict is a persisted decision snapshot.
type LaunchVerdict string

// LaunchDisposition closes an unplaced launch attempt without deleting its
// history. Cancelled means the slot was intentionally withdrawn; abandoned
// means the attempt expired without execution and should become learning debt.
type LaunchDisposition string

// LaunchEvent is one append-only ledger row.
type LaunchEvent struct {
	TS            string            `json:"ts"`                // RFC3339
	Product       string            `json:"product"`           // slug
	Type          LaunchEventType   `json:"type"`              // planned | kit | posted | signal | verdict | retired
	Week          string            `json:"week,omitempty"`    // ISO week, e.g. "2026-W24" (cohort key; set on planned)
	Channel       string            `json:"channel,omitempty"` // ChannelSpec key for posted/signal
	URL           string            `json:"url,omitempty"`
	ReceiptMarker string            `json:"receipt_marker,omitempty"`
	Metric        SignalMetric      `json:"metric,omitempty"`
	Value         float64           `json:"value,omitempty"`
	Source        SignalSource      `json:"source,omitempty"`
	Verdict       LaunchVerdict     `json:"verdict,omitempty"`
	Disposition   LaunchDisposition `json:"disposition,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Note          string            `json:"note,omitempty"`
	// Price-test fields. Price is deliberately separate from Value: an offer
	// price and a signal measurement are different quantities, and sharing the
	// field would let a price leak into SignalScore as traction.
	Price    float64           `json:"price,omitempty"`
	Currency string            `json:"currency,omitempty"`
	Offer    string            `json:"offer,omitempty"`
	Response PriceTestResponse `json:"response,omitempty"`
}

// PriceTestResponse is the closed set of outcomes a shown offer can produce.
type PriceTestResponse string

// Price-test responses.
const (
	ResponseAccepted   PriceTestResponse = "accepted"
	ResponseDeclined   PriceTestResponse = "declined"
	ResponseCountered  PriceTestResponse = "countered"
	ResponseNoResponse PriceTestResponse = "no_response"
	ResponseDeferred   PriceTestResponse = "deferred"
)

var knownPriceTestResponses = []PriceTestResponse{
	ResponseAccepted, ResponseDeclined, ResponseCountered, ResponseNoResponse, ResponseDeferred,
}

// KnownPriceTestResponses returns the response registry in display order.
func KnownPriceTestResponses() []PriceTestResponse {
	return append([]PriceTestResponse(nil), knownPriceTestResponses...)
}

func (r PriceTestResponse) Valid() bool {
	for _, known := range knownPriceTestResponses {
		if r == known {
			return true
		}
	}
	return false
}

// ParsePriceTestResponse validates an operator-supplied response.
func ParsePriceTestResponse(raw string) (PriceTestResponse, error) {
	r := PriceTestResponse(strings.ToLower(strings.TrimSpace(raw)))
	if !r.Valid() {
		parts := make([]string, len(knownPriceTestResponses))
		for i, known := range knownPriceTestResponses {
			parts[i] = string(known)
		}
		return "", fmt.Errorf("unknown price-test response %q (known: %s)", raw, strings.Join(parts, ", "))
	}
	return r, nil
}

// currencyRE matches an ISO-4217-shaped code. The registry is not enumerated —
// the point is to reject free text, not to police which currencies exist.
var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)

// Ledger event types.
const (
	EventPlanned LaunchEventType = "planned"
	EventKit     LaunchEventType = "kit"
	EventPosted  LaunchEventType = "posted"
	EventSignal  LaunchEventType = "signal"
	EventVerdict LaunchEventType = "verdict"
	EventRetired LaunchEventType = "retired"
	// EventPriceTest records that a specific price was SHOWN to a specific
	// audience and what came back. It is the falsifiable counterpart to a
	// modeled price: a price nobody has been quoted is an assumption, not an
	// asset, and the catalog previously had no way to tell the two apart.
	EventPriceTest LaunchEventType = "price_test"
)

// Retirement dispositions accepted by the launch state machine.
const (
	DispositionCancelled LaunchDisposition = "cancelled"
	DispositionAbandoned LaunchDisposition = "abandoned"
)

// Signal metrics and sources accepted by the verdict engine.
const (
	MetricSignups        SignalMetric = "signups"
	MetricRevenueUSD     SignalMetric = "revenue_usd"
	MetricClicks         SignalMetric = "clicks"
	MetricHNPoints       SignalMetric = "hn_points"
	MetricHNMentions     SignalMetric = "hn_mentions"
	MetricRedditScore    SignalMetric = "reddit_score"
	MetricRedditMentions SignalMetric = "reddit_mentions"
	MetricMentions       SignalMetric = "mentions"
	SourceOperator       SignalSource = "operator"
	SourceHackerNews     SignalSource = "hackernews"
	SourceReddit         SignalSource = "reddit"
)

var (
	isoWeekRE       = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)
	identifierRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	knownSignalList = []SignalMetric{
		MetricSignups, MetricRevenueUSD, MetricClicks, MetricHNPoints,
		MetricHNMentions, MetricRedditScore, MetricRedditMentions, MetricMentions,
	}
)

// KnownSignalMetrics returns the stable metric registry in display order.
func KnownSignalMetrics() []SignalMetric {
	return append([]SignalMetric(nil), knownSignalList...)
}

// ParseSignalMetric validates an operator/importer metric against the registry.
func ParseSignalMetric(raw string) (SignalMetric, error) {
	m := SignalMetric(strings.ToLower(strings.TrimSpace(raw)))
	if !m.Valid() {
		return "", fmt.Errorf("unknown signal metric %q (known: %s)", raw, joinSignalMetrics(knownSignalList))
	}
	return m, nil
}

func (m SignalMetric) Valid() bool {
	_, ok := signalWeights[m]
	return ok
}

// ParseLaunchDisposition validates an operator retirement choice.
func ParseLaunchDisposition(raw string) (LaunchDisposition, error) {
	d := LaunchDisposition(strings.ToLower(strings.TrimSpace(raw)))
	if !d.Valid() {
		return "", fmt.Errorf("unknown launch disposition %q (want cancelled|abandoned)", raw)
	}
	return d, nil
}

func (d LaunchDisposition) Valid() bool {
	return d == DispositionCancelled || d == DispositionAbandoned
}

// ValidISOWeek validates the cohort key including the ISO week range.
func ValidISOWeek(week string) bool {
	m := isoWeekRE.FindStringSubmatch(strings.TrimSpace(week))
	if len(m) != 3 {
		return false
	}
	y, err := strconv.Atoi(m[1])
	if err != nil || y < 1 {
		return false
	}
	w, err := strconv.Atoi(m[2])
	if err != nil || w < 1 {
		return false
	}
	_, maxWeek := time.Date(y, time.December, 28, 0, 0, 0, 0, time.UTC).ISOWeek()
	return w <= maxWeek
}

func joinSignalMetrics(metrics []SignalMetric) string {
	parts := make([]string, len(metrics))
	for i, metric := range metrics {
		parts[i] = string(metric)
	}
	return strings.Join(parts, ", ")
}

func validIdentifier(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return raw == trimmed && identifierRE.MatchString(trimmed)
}

func (ev LaunchEvent) rejectUnexpectedFields(allowedFields ...string) error {
	allowed := make(map[string]bool, len(allowedFields))
	for _, field := range allowedFields {
		allowed[field] = true
	}
	fields := []struct {
		name    string
		present bool
	}{
		{"week", ev.Week != ""},
		{"channel", ev.Channel != ""},
		{"url", ev.URL != ""},
		{"receipt_marker", ev.ReceiptMarker != ""},
		{"metric", ev.Metric != ""},
		{"value", ev.Value != 0},
		{"source", ev.Source != ""},
		{"verdict", ev.Verdict != ""},
		{"disposition", ev.Disposition != ""},
		{"reason", ev.Reason != ""},
		{"price", ev.Price != 0},
		{"currency", ev.Currency != ""},
		{"offer", ev.Offer != ""},
		{"response", ev.Response != ""},
	}
	var unexpected []string
	for _, field := range fields {
		if field.present && !allowed[field.name] {
			unexpected = append(unexpected, field.name)
		}
	}
	if len(unexpected) > 0 {
		return fmt.Errorf("%s event has unexpected field(s): %s", ev.Type, strings.Join(unexpected, ", "))
	}
	return nil
}

// Validate rejects illegal ledger states before they can affect a cohort or
// verdict. It is also applied while reading so hand-edited/corrupt rows surface.
func (ev LaunchEvent) Validate() error {
	if !validIdentifier(ev.Product) {
		return errors.New("product must be a canonical lowercase slug matching [a-z0-9][a-z0-9._-]{0,63}")
	}
	if _, err := time.Parse(time.RFC3339, ev.TS); err != nil {
		return fmt.Errorf("ts must be RFC3339: %w", err)
	}
	switch ev.Type {
	case EventPlanned:
		if !ValidISOWeek(ev.Week) {
			return fmt.Errorf("planned event week %q is not ISO form YYYY-Www", ev.Week)
		}
		return ev.rejectUnexpectedFields("week")
	case EventKit:
		return ev.rejectUnexpectedFields()
	case EventPosted:
		if !validIdentifier(ev.Channel) {
			return fmt.Errorf("posted event channel %q is not a safe channel key", ev.Channel)
		}
		u, err := url.ParseRequestURI(strings.TrimSpace(ev.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("posted event needs an absolute http(s) receipt URL")
		}
		if err := validatePublicReceiptTarget(u); err != nil {
			return fmt.Errorf("posted event receipt must be a public target: %w", err)
		}
		if marker := ev.ReceiptMarker; marker != "" {
			trimmed := strings.TrimSpace(marker)
			if marker != trimmed || len(marker) > 128 || strings.ContainsAny(marker, "\r\n") {
				return errors.New("posted event receipt marker must be trimmed, single-line, and at most 128 bytes")
			}
		}
		return ev.rejectUnexpectedFields("channel", "url", "receipt_marker")
	case EventSignal:
		if !ev.Metric.Valid() {
			return fmt.Errorf("signal metric %q is not in the typed registry", ev.Metric)
		}
		if math.IsNaN(ev.Value) || math.IsInf(ev.Value, 0) || ev.Value < 0 {
			return errors.New("signal value must be finite and non-negative")
		}
		if !validIdentifier(string(ev.Source)) {
			return fmt.Errorf("signal source %q is not a safe source key", ev.Source)
		}
		if ev.Channel != "" && !validIdentifier(ev.Channel) {
			return fmt.Errorf("signal channel %q is not a safe channel key", ev.Channel)
		}
		return ev.rejectUnexpectedFields("channel", "metric", "value", "source")
	case EventVerdict:
		if !ev.Verdict.Valid() {
			return fmt.Errorf("verdict event has unknown verdict %q", ev.Verdict)
		}
		return ev.rejectUnexpectedFields("verdict")
	case EventRetired:
		if !ValidISOWeek(ev.Week) {
			return fmt.Errorf("retired event week %q is not ISO form YYYY-Www", ev.Week)
		}
		if !ev.Disposition.Valid() {
			return fmt.Errorf("retired event has unknown disposition %q", ev.Disposition)
		}
		if ev.Source != SourceOperator {
			return errors.New("retired event source must be operator")
		}
		reason := strings.TrimSpace(ev.Reason)
		if reason == "" || reason != ev.Reason || len(reason) > 500 || strings.ContainsAny(reason, "\r\n") {
			return errors.New("retired event reason must be trimmed, non-empty, single-line, and at most 500 bytes")
		}
		return ev.rejectUnexpectedFields("week", "source", "disposition", "reason")
	case EventPriceTest:
		if math.IsNaN(ev.Price) || math.IsInf(ev.Price, 0) || ev.Price <= 0 {
			return errors.New("price-test price must be finite and greater than zero")
		}
		if !currencyRE.MatchString(ev.Currency) {
			return fmt.Errorf("price-test currency %q must be a three-letter uppercase code", ev.Currency)
		}
		if !validIdentifier(ev.Channel) {
			return fmt.Errorf("price-test channel %q is not a safe channel key — record where the offer was shown", ev.Channel)
		}
		if !ev.Response.Valid() {
			return fmt.Errorf("price-test response %q is not in the typed registry", ev.Response)
		}
		if ev.Source != SourceOperator {
			return errors.New("price-test source must be operator — a price test is something a human did")
		}
		if offer := ev.Offer; offer != "" {
			trimmed := strings.TrimSpace(offer)
			if offer != trimmed || len(offer) > 500 || strings.ContainsAny(offer, "\r\n") {
				return errors.New("price-test offer must be trimmed, single-line, and at most 500 bytes")
			}
		}
		return ev.rejectUnexpectedFields("channel", "price", "currency", "offer", "response", "source")
	default:
		return fmt.Errorf("unknown launch event type %q", ev.Type)
	}
}

// LaunchLedger is the append-only JSONL store.
type LaunchLedger struct{ Path string }

// DefaultLaunchLedgerPath resolves the ledger location: $NGTM_LAUNCH_LEDGER
// wins, else ~/.nicos-dev/gtm/launch-ledger.jsonl (same dir as runs.jsonl).
func DefaultLaunchLedgerPath() string {
	if p := strings.TrimSpace(os.Getenv("NGTM_LAUNCH_LEDGER")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "launch-ledger.jsonl"
	}
	return filepath.Join(home, ".nicos-dev", "gtm", "launch-ledger.jsonl")
}

// Append writes one event (creating the directory/file on first use).
// Shape checks run before the lock; transition checks (plan/kill/retire)
// run under the same lock as the durable write so a concurrent append cannot
// sneak a zombie row in.
func (l LaunchLedger) Append(ev LaunchEvent) error {
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("invalid launch event: %w", err)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal launch event: %w", err)
	}
	return jsonl.AppendLineDurableChecked(l.Path, b, 0o644, func() error {
		report, err := l.readWithIssuesUnlocked()
		if err != nil {
			return err
		}
		if len(report.Issues) > 0 {
			return &LaunchLedgerCorruptionError{Path: l.Path, Issues: report.Issues}
		}
		if err := rejectWriteTransition(report.Events, ev); err != nil {
			return fmt.Errorf("invalid launch event: %w", err)
		}
		return nil
	})
}

// productAttemptState is the latest attempt for one product, replayed the
// same way AuditLaunchLedger treats re-plans (a new planned event clears
// posted/killed/retired).
type productAttemptState struct {
	planned bool
	posted  bool
	killed  bool
	retired bool
	week    string
}

func replayAttemptState(events []LaunchEvent, product string) productAttemptState {
	var st productAttemptState
	for _, ev := range events {
		if ev.Product != product {
			continue
		}
		switch ev.Type {
		case EventPlanned:
			st = productAttemptState{planned: true, week: ev.Week}
		case EventPosted:
			st.posted = true
		case EventVerdict:
			if ev.Verdict == VerdictKill || strings.EqualFold(ev.Note, string(VerdictKill)) {
				st.killed = true
			}
		case EventRetired:
			st.retired = true
		}
	}
	return st
}

func rejectWriteTransition(history []LaunchEvent, ev LaunchEvent) error {
	st := replayAttemptState(history, ev.Product)
	switch ev.Type {
	case EventPlanned:
		return nil
	case EventKit, EventPosted, EventVerdict, EventPriceTest:
		if !st.planned {
			return fmt.Errorf("%s_before_plan: %s without a planned event", ev.Type, ev.Type)
		}
		if st.killed {
			return fmt.Errorf("zombie_after_kill: %s after a KILL verdict — re-plan it into a new cohort first", ev.Type)
		}
		if st.retired {
			return fmt.Errorf("activity_after_retirement: %s after retirement — re-plan it into a new cohort first", ev.Type)
		}
	case EventSignal:
		if !st.planned {
			return fmt.Errorf("signal_before_plan: signal without a planned event")
		}
		if st.killed {
			return fmt.Errorf("zombie_after_kill: signal after a KILL verdict — re-plan it into a new cohort first")
		}
		if st.retired {
			return fmt.Errorf("activity_after_retirement: signal after retirement — re-plan it before new activity")
		}
		if !st.posted {
			return fmt.Errorf("signal_before_post: signal recorded before any placement")
		}
	case EventRetired:
		if !st.planned {
			return fmt.Errorf("retired_before_plan: retirement has no prior planned event")
		}
		if st.posted {
			return fmt.Errorf("retirement_after_post: executed launch was retired after a placement — persist a verdict instead")
		}
		if st.retired {
			return fmt.Errorf("duplicate_retirement: launch attempt was retired more than once")
		}
		if st.week != "" && ev.Week != st.week {
			return fmt.Errorf("retirement_week_mismatch: retired week %s but active attempt is %s", ev.Week, st.week)
		}
	}
	return nil
}

// LaunchLedgerIssue is one corrupt or invalid row. Raw row content is omitted
// deliberately: audit output stays useful without leaking notes or URLs.
type LaunchLedgerIssue struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Product string `json:"product,omitempty"`
	Message string `json:"message"`
}

// LaunchLedgerRead is the tolerant read shape used only by the audit path.
type LaunchLedgerRead struct {
	Events []LaunchEvent       `json:"events"`
	Issues []LaunchLedgerIssue `json:"issues,omitempty"`
}

// LaunchLedgerCorruptionError makes normal reads fail closed while preserving
// line-level diagnostics for callers that want to direct the operator to audit.
type LaunchLedgerCorruptionError struct {
	Path   string
	Issues []LaunchLedgerIssue
}

func (e *LaunchLedgerCorruptionError) Error() string {
	return fmt.Sprintf("launch ledger %s has %d corrupt/invalid row(s); run `ngtm launch audit --strict`", e.Path, len(e.Issues))
}

// Read returns every valid event in ledger order. A missing file is an empty
// ledger; any malformed or invalid row makes the normal read fail closed.
func (l LaunchLedger) Read() ([]LaunchEvent, error) {
	report, err := l.ReadWithIssues()
	if err != nil {
		return nil, err
	}
	if len(report.Issues) > 0 {
		return nil, &LaunchLedgerCorruptionError{Path: l.Path, Issues: report.Issues}
	}
	return report.Events, nil
}

// ReadWithIssues reads valid events and preserves diagnostics for every bad
// line. It is intentionally reserved for `launch audit`; normal reads use Read
// and fail closed when any issue exists.
func (l LaunchLedger) ReadWithIssues() (LaunchLedgerRead, error) {
	var report LaunchLedgerRead
	err := lockfile.WithFileLock(l.Path, func() error {
		var readErr error
		report, readErr = l.readWithIssuesUnlocked()
		return readErr
	})
	return report, err
}

func (l LaunchLedger) readWithIssuesUnlocked() (LaunchLedgerRead, error) {
	f, err := os.Open(l.Path)
	if os.IsNotExist(err) {
		return LaunchLedgerRead{}, nil
	}
	if err != nil {
		return LaunchLedgerRead{}, err
	}
	var out LaunchLedgerRead
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ev, err := decodeLaunchEventStrict([]byte(line))
		if err != nil {
			out.Issues = append(out.Issues, LaunchLedgerIssue{Line: lineNo, Code: "malformed_json", Message: err.Error()})
			continue
		}
		if err := ev.Validate(); err != nil {
			out.Issues = append(out.Issues, LaunchLedgerIssue{Line: lineNo, Code: "invalid_event", Product: ev.Product, Message: err.Error()})
			continue
		}
		out.Events = append(out.Events, ev)
	}
	readErr := sc.Err()
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return LaunchLedgerRead{}, err
	}
	return out, nil
}

func decodeLaunchEventStrict(raw []byte) (LaunchEvent, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var ev LaunchEvent
	if err := dec.Decode(&ev); err != nil {
		return LaunchEvent{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return LaunchEvent{}, errors.New("launch ledger row contains more than one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return LaunchEvent{}, fmt.Errorf("decode launch ledger row tail: %w", err)
	}
	return ev, nil
}

// ISOWeek formats t's ISO 8601 week as the cohort key, e.g. "2026-W24".
func ISOWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// ProductLaunch is the projected state of one product's launch, folded from
// its ledger events.
type ProductLaunch struct {
	// SurfaceCoverage is what `ndev endpoints analytics` says about the surface
	// this launch pointed at: "instrumented", "blind", or "" when nobody asked.
	//
	// Empty is deliberately NOT treated as blind. An un-asked question is not a
	// negative answer, and defaulting to blind would flip every historical
	// ledger row to UNMEASURED — inventing a finding out of missing metadata,
	// which is the same defect one more level out.
	SurfaceCoverage  string             `json:"surface_coverage,omitempty"`
	Product          string             `json:"product"`
	Week             string             `json:"week"`
	PlannedAt        string             `json:"planned_at,omitempty"`
	KitAt            string             `json:"kit_at,omitempty"`
	Posts            []LaunchEvent      `json:"posts,omitempty"`
	Signals          []LaunchEvent      `json:"signals,omitempty"`
	PriceTests       []LaunchEvent      `json:"price_tests,omitempty"`
	PriceTested      bool               `json:"price_tested"`
	Score            float64            `json:"score"`
	ScoreParts       map[string]float64 `json:"score_parts,omitempty"`
	Verdict          LaunchVerdict      `json:"verdict"`
	Rationale        string             `json:"rationale"`
	FirstPosted      string             `json:"first_posted,omitempty"`
	RetiredAt        string             `json:"retired_at,omitempty"`
	Disposition      LaunchDisposition  `json:"disposition,omitempty"`
	RetirementReason string             `json:"retirement_reason,omitempty"`
}

// Stage returns the highest stage reached, for board rendering.
func (p ProductLaunch) Stage() string {
	switch {
	case p.Disposition.Valid():
		return "retired"
	case len(p.Signals) > 0:
		return "measured"
	case len(p.Posts) > 0:
		return "posted"
	case p.KitAt != "":
		return "kit"
	default:
		return "planned"
	}
}

// LaunchCohort is one ISO week's batch.
type LaunchCohort struct {
	Week     string          `json:"week"`
	Target   int             `json:"target"` // the "ship N a week" goal
	Products []ProductLaunch `json:"products"`
}

// signalWeights converts heterogeneous signal metrics into one comparable
// score. Conversions (operator-observed money/users) dominate; community
// traction next; raw clicks least. Exported reasoning lives in the verdict
// rationale, so the weighting is inspectable, not a black box.
var signalWeights = map[SignalMetric]float64{
	MetricSignups:        10,
	MetricRevenueUSD:     1,
	MetricClicks:         0.2,
	MetricHNPoints:       2,
	MetricHNMentions:     3,
	MetricRedditScore:    1,
	MetricRedditMentions: 3,
	MetricMentions:       1,
}

// Verdict thresholds — the traction analog of the economics GO/NO-GO gate.
const (
	// ScoreDoubleDown: weighted signal at/above this → DOUBLE-DOWN.
	ScoreDoubleDown = 40.0
	// ScoreIterate: at/above this (but below DoubleDown) → ITERATE.
	ScoreIterate = 12.0
	// KillAfterDays: below ScoreIterate this long after first post → KILL.
	KillAfterDays = 7
	// tooEarlyDays: signals within this window can't condemn a launch yet.
	tooEarlyDays = 3
)

// Surface-coverage values, mirroring `ndev endpoints analytics` states. Only
// the two that change a verdict are modelled; everything else is "unknown".
const (
	SurfaceInstrumented = "instrumented"
	SurfaceBlind        = "blind"
)

// Verdict labels.
const (
	VerdictDoubleDown LaunchVerdict = "DOUBLE-DOWN"
	VerdictIterate    LaunchVerdict = "ITERATE"
	VerdictKill       LaunchVerdict = "KILL"
	VerdictTooEarly   LaunchVerdict = "TOO-EARLY"
	VerdictWatch      LaunchVerdict = "WATCH"
	// VerdictNotDistributed separates "we never reached anyone" from "we reached
	// people and they did not care". Both used to read as KILL once the kill
	// window elapsed, which blamed the product for an unattempted launch and
	// destroyed the ledger's only useful signal: whether demand was ever tested.
	VerdictNotDistributed LaunchVerdict = "NOT-DISTRIBUTED"
	// VerdictUnmeasured separates "they did not care" from "we could not see".
	// NOT-DISTRIBUTED fixed the case where we never reached anyone; this fixes
	// the successor case that fix created room for — a launch that DID reach a
	// surface nothing was watching. A low score from a blind surface is absence
	// of measurement, not evidence of absent demand, and KILLing on it destroys
	// the same signal NOT-DISTRIBUTED was invented to protect.
	// Coverage comes from `ndev endpoints analytics`.
	VerdictUnmeasured  LaunchVerdict = "UNMEASURED"
	VerdictNotLaunched LaunchVerdict = "NOT-LAUNCHED"
	VerdictCancelled   LaunchVerdict = "CANCELLED"
	VerdictAbandoned   LaunchVerdict = "ABANDONED"
)

func (v LaunchVerdict) Valid() bool {
	switch v {
	case VerdictDoubleDown, VerdictIterate, VerdictKill, VerdictTooEarly, VerdictWatch,
		VerdictNotDistributed, VerdictUnmeasured, VerdictNotLaunched, VerdictCancelled, VerdictAbandoned:
		return true
	default:
		return false
	}
}

// distributionPlacements splits a launch's placements into those on channels
// that could reach a new audience and those that could not.
func distributionPlacements(posts []LaunchEvent) (distributing, nonDistributing []LaunchEvent) {
	for _, p := range posts {
		if ChannelDistributes(p.Channel) {
			distributing = append(distributing, p)
		} else {
			nonDistributing = append(nonDistributing, p)
		}
	}
	return distributing, nonDistributing
}

// nonDistributionRationale explains which non-distribution channels were used
// and why they cannot condemn the product.
func nonDistributionRationale(posts []LaunchEvent) string {
	seen := map[string]bool{}
	var parts []string
	for _, p := range posts {
		if seen[p.Channel] {
			continue
		}
		seen[p.Channel] = true
		if spec, ok := NonDistributionChannelByKey(p.Channel); ok {
			parts = append(parts, fmt.Sprintf("%s (%s)", spec.Key, spec.Reason))
			continue
		}
		parts = append(parts, p.Channel)
	}
	return fmt.Sprintf("%d placement(s), none on a distribution channel: %s — this is not a product verdict; "+
		"the launch was never attempted. Place it on a real channel (see `ngtm launch channels`) before judging demand",
		len(posts), strings.Join(parts, "; "))
}

// latestSignals keeps the last valid event per (channel, metric, source).
func latestSignals(signals []LaunchEvent) map[string]LaunchEvent {
	latest := map[string]LaunchEvent{}
	for _, s := range signals { // ledger order == chronological; last write wins
		if !s.Metric.Valid() || math.IsNaN(s.Value) || math.IsInf(s.Value, 0) || s.Value < 0 {
			continue
		}
		latest[s.Channel+"|"+string(s.Metric)+"|"+string(s.Source)] = s
	}
	return latest
}

// SignalScore folds signal events into a weighted score. Signals are LEVEL
// measurements, not increments: for each (channel, metric, source) key the
// latest event wins, so re-measuring never double-counts.
func SignalScore(signals []LaunchEvent) (float64, map[string]float64) {
	parts := map[string]float64{}
	total := 0.0
	for _, s := range latestSignals(signals) {
		w := signalWeights[s.Metric]
		contrib := s.Value * w
		parts[string(s.Metric)] += contrib
		total += contrib
	}
	return total, parts
}

// hasConversion reports whether the latest operator-observed conversion
// (signups or revenue) is still positive. A later zero retracts the shortcut.
func hasConversion(signals []LaunchEvent) bool {
	for _, s := range latestSignals(signals) {
		if s.Source == SourceOperator && (s.Metric == MetricSignups || s.Metric == MetricRevenueUSD) && s.Value > 0 {
			return true
		}
	}
	return false
}

// TractionVerdict computes the gate for one product launch. The rationale
// always states which signals drove it and their provenance.
func TractionVerdict(p ProductLaunch, now time.Time) (LaunchVerdict, string) {
	if p.Disposition.Valid() {
		verdict := VerdictCancelled
		if p.Disposition == DispositionAbandoned {
			verdict = VerdictAbandoned
		}
		return verdict, fmt.Sprintf("launch attempt %s: %s", p.Disposition, p.RetirementReason)
	}
	if len(p.Posts) == 0 {
		return VerdictNotLaunched, "no posted placements recorded — run the kit, place it, record with `launch posted`"
	}
	days := -1.0
	if t, err := time.Parse(time.RFC3339, p.FirstPosted); err == nil {
		days = now.Sub(t).Hours() / 24
	}
	score, _ := SignalScore(p.Signals)
	feeds, ops := signalProvenance(p.Signals)
	prov := fmt.Sprintf("score %.1f from %d signal(s): %d feed-measured, %d operator-recorded", score, len(p.Signals), feeds, ops)
	distributing, nonDistributing := distributionPlacements(p.Posts)

	// Ordering is deliberate. Positive evidence is evaluated before the
	// distribution check because measured demand outranks channel taxonomy: if a
	// release tag somehow produced signups, that is real demand and the engine
	// must say so. NOT-DISTRIBUTED displaces only the negative verdicts, which
	// are the ones that would otherwise blame the product for a launch that
	// never happened.
	switch {
	case hasConversion(p.Signals):
		return VerdictDoubleDown, "operator-observed conversions (signups/revenue) — real demand; " + prov
	case score >= ScoreDoubleDown:
		return VerdictDoubleDown, prov + fmt.Sprintf(" — at/above the %.0f double-down bar", ScoreDoubleDown)
	case score >= ScoreIterate:
		return VerdictIterate, prov + " — signal exists; sharpen the angle or channel before scaling"
	case len(distributing) == 0:
		return VerdictNotDistributed, nonDistributionRationale(nonDistributing)
	case p.SurfaceCoverage == SurfaceBlind:
		// We distributed, and nothing was watching where it landed. The score is
		// therefore not a reading of demand — it is the absence of one, and the
		// negative verdicts below would blame the product for our blindness.
		return VerdictUnmeasured, "distribution happened but the destination surface cannot see arrivals — " +
			prov + "; this score measures our instrumentation, not demand. " +
			"Fix with `ndev endpoints analytics --blind`, then re-judge"
	case days >= 0 && days < tooEarlyDays:
		return VerdictTooEarly, fmt.Sprintf("first post %.1f day(s) ago (< %d) — measure again before judging; %s", days, tooEarlyDays, prov)
	case days >= KillAfterDays:
		return VerdictKill, prov + fmt.Sprintf(" — below the %.0f bar %d+ days post-launch; archive and redeploy the slot", ScoreIterate, KillAfterDays)
	default:
		return VerdictWatch, prov + fmt.Sprintf(" — between day %d and %d; keep measuring", tooEarlyDays, KillAfterDays)
	}
}

func signalProvenance(signals []LaunchEvent) (feeds, ops int) {
	for _, s := range signals {
		if s.Source == SourceOperator {
			ops++
		} else {
			feeds++
		}
	}
	return feeds, ops
}

// BuildLaunches folds ledger events into per-product launch states, sorted by
// week then product. Every planned event starts a fresh state, including a
// same-week re-plan after a verdict; a relaunch is a new attempt, not an
// amendment carrying old posts or signals forward.
// SurfaceCoverageLookup answers, for a product slug, what
// `ndev endpoints analytics` says about the surface its launches point at:
// "instrumented", "blind", or "" when it cannot say.
//
// It is an injected function rather than a direct dependency so the GTM cell
// does not import the endpoints cell — and so a caller that has no ledger
// simply supplies nothing, which correctly leaves coverage unknown rather than
// guessing.
type SurfaceCoverageLookup func(product string) string

// BuildLaunches folds the event log into per-product launches.
func BuildLaunches(events []LaunchEvent, now time.Time) []ProductLaunch {
	return BuildLaunchesWithCoverage(events, now, nil)
}

// BuildLaunchesWithCoverage is BuildLaunches plus the surface-coverage lookup
// that lets the verdict engine refuse to judge a blind surface.
//
// The producer is `endpoints.SurfaceCoverageLookupFromLedger` (built on
// SurfaceCoverageBySlug + NormalizeProductSlug, because the endpoint ledger
// names deploy projects and launches name products). Every command that
// projects a launch passes one: `gtmcli.launchCoverage` covers cohort, show,
// verdict, retro and the MCP tool; `nshipfactory.LaunchLoopStates` covers the
// nship cockpit, so the board cannot show a KILL that ngtm declines to judge.
//
// Callers with no ledger — tests, and any consumer that cannot resolve one —
// pass nil, which leaves coverage "": UNKNOWN, not blind, so the guard is inert
// rather than wrong. Defaulting to blind instead would flip the whole ledger to
// UNMEASURED on missing metadata, inventing a finding out of absent data. The
// same rule governs the producer, which returns nil rather than guessing
// whenever it cannot verify the shipped artifact.
func BuildLaunchesWithCoverage(events []LaunchEvent, now time.Time, coverage SurfaceCoverageLookup) []ProductLaunch {
	idx := map[string]*ProductLaunch{} // product → latest attempt
	var order []string
	for _, ev := range events {
		p, ok := idx[ev.Product]
		if !ok || ev.Type == EventPlanned {
			week := ev.Week
			if week == "" {
				week = weekOfEvent(ev, now)
			}
			p = &ProductLaunch{Product: ev.Product, Week: week}
			idx[ev.Product] = p
			order = append(order, ev.Product)
		}
		switch ev.Type {
		case EventPlanned:
			p.PlannedAt = ev.TS
			if ev.Week != "" {
				p.Week = ev.Week
			}
		case EventKit:
			p.KitAt = ev.TS
		case EventPosted:
			p.Posts = append(p.Posts, ev)
			if p.FirstPosted == "" {
				p.FirstPosted = ev.TS
			}
		case EventSignal:
			p.Signals = append(p.Signals, ev)
		case EventPriceTest:
			p.PriceTests = append(p.PriceTests, ev)
		case EventRetired:
			p.RetiredAt = ev.TS
			p.Disposition = ev.Disposition
			p.RetirementReason = ev.Reason
		}
	}
	var out []ProductLaunch
	for _, name := range dedupe(order) {
		p := idx[name]
		p.Score, p.ScoreParts = SignalScore(p.Signals)
		p.PriceTested = len(p.PriceTests) > 0
		if coverage != nil {
			p.SurfaceCoverage = coverage(name)
		}
		p.Verdict, p.Rationale = TractionVerdict(*p, now)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Week != out[j].Week {
			return out[i].Week < out[j].Week
		}
		return out[i].Product < out[j].Product
	})
	return out
}

func weekOfEvent(ev LaunchEvent, now time.Time) string {
	if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
		return ISOWeek(t)
	}
	return ISOWeek(now)
}

// BuildCohorts groups launches by ISO week. target is the ship-N-a-week goal
// (0 → 20, the factory's stated velocity).
func BuildCohorts(launches []ProductLaunch, target int) []LaunchCohort {
	if target <= 0 {
		target = 20
	}
	byWeek := map[string][]ProductLaunch{}
	var weeks []string
	for _, p := range launches {
		if _, ok := byWeek[p.Week]; !ok {
			weeks = append(weeks, p.Week)
		}
		byWeek[p.Week] = append(byWeek[p.Week], p)
	}
	sort.Strings(weeks)
	var out []LaunchCohort
	for _, w := range weeks {
		out = append(out, LaunchCohort{Week: w, Target: target, Products: byWeek[w]})
	}
	return out
}

// MeasureLaunchSignals re-queries the community feeds (hackernews/reddit) for
// the product and converts mention evidence into signal events — the same
// feeds that ground pre-launch research, reused post-launch to MEASURE
// traction. Returned events carry Source=<feed> (observed provenance); the
// caller decides whether to record them.
func (e *Engine) MeasureLaunchSignals(ctx context.Context, product string, tiers []FeedTier) ([]LaunchEvent, []string) {
	ev, warnings := e.reg.Gather(ctx, FeedQuery{Subject: product}, tierSet(tiers))
	ts := e.now().UTC().Format(time.RFC3339)
	type agg struct {
		count         int
		sum           float64
		scoredSamples int
	}
	per := map[string]*agg{} // feed → mentions aggregate
	for _, m := range nonSynthetic(ev) {
		if m.Metric != "mentions" {
			continue
		}
		a := per[m.Feed]
		if a == nil {
			a = &agg{}
			per[m.Feed] = a
		}
		a.count++
		if m.Extra["score_provenance"] == "unavailable" {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(m.Value), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			warnings = append(warnings, fmt.Sprintf("feed %s returned invalid mentions value %q", m.Feed, m.Value))
			continue
		}
		a.sum += v
		a.scoredSamples++
	}
	var out []LaunchEvent
	for _, feed := range []string{"hackernews", "reddit"} {
		a := per[feed]
		if a == nil {
			continue
		}
		mentionMetric, weightMetric := MetricMentions, MetricMentions
		source := SignalSource(feed)
		switch feed {
		case "hackernews":
			mentionMetric, weightMetric, source = MetricHNMentions, MetricHNPoints, SourceHackerNews
		case "reddit":
			mentionMetric, weightMetric, source = MetricRedditMentions, MetricRedditScore, SourceReddit
		}
		out = append(out, LaunchEvent{TS: ts, Product: product, Type: EventSignal, Channel: feed, Metric: mentionMetric, Value: float64(a.count), Source: source})
		if a.scoredSamples > 0 {
			out = append(out, LaunchEvent{TS: ts, Product: product, Type: EventSignal, Channel: feed, Metric: weightMetric, Value: a.sum, Source: source})
		}
	}
	return out, warnings
}
