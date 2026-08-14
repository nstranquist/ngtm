package gtm

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The regression this whole slice exists for: docs-puller's only launch attempt
// was two GitHub release tags. Once the kill window elapsed the engine called it
// KILL — the same verdict a product earns after a real Show HN nobody cared
// about — which recorded a never-attempted launch as a product-quality failure.
func TestTractionVerdict_TagOnlyLaunchIsNotDistributedNotKill(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	p := ProductLaunch{
		Product:     "docs-puller",
		Week:        "2026-W29",
		FirstPosted: "2026-06-01T10:00:00Z", // well past KillAfterDays
		Posts: []LaunchEvent{
			{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://github.com/nstranquist/docs-puller/releases/tag/v0.1.0"},
			{TS: "2026-06-02T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://github.com/nstranquist/docs-puller/releases/tag/v0.2.0"},
		},
	}
	verdict, rationale := TractionVerdict(p, now)
	if verdict != VerdictNotDistributed {
		t.Fatalf("tag-only launch verdict = %q, want %q", verdict, VerdictNotDistributed)
	}
	if verdict == VerdictKill {
		t.Fatal("a launch that reached nobody must never be reported as a product KILL")
	}
	if !strings.Contains(rationale, "github-release") {
		t.Errorf("rationale must name the channel used, got %q", rationale)
	}
	if !strings.Contains(rationale, "not a product verdict") {
		t.Errorf("rationale must state this is not a product judgment, got %q", rationale)
	}
}

// Measured demand outranks channel taxonomy. If a release tag somehow produced
// signups, that is real demand and the engine must say so rather than hiding it
// behind the distribution downgrade.
func TestTractionVerdict_ConversionsOutrankNonDistributionDowngrade(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	p := ProductLaunch{
		Product:     "docs-puller",
		FirstPosted: "2026-06-01T10:00:00Z",
		Posts: []LaunchEvent{
			{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
		},
		Signals: []LaunchEvent{
			{TS: "2026-06-05T10:00:00Z", Product: "docs-puller", Type: EventSignal, Metric: MetricSignups, Value: 4, Source: SourceOperator},
		},
	}
	if verdict, _ := TractionVerdict(p, now); verdict != VerdictDoubleDown {
		t.Fatalf("operator-observed conversions must win, got %q", verdict)
	}
}

// The registry proves a channel does NOT distribute; it never assumes it.
// Downgrading an operator's real launch because we had not registered their
// channel yet would be a worse error than scoring an obscure channel generously.
func TestTractionVerdict_UnknownChannelFailsOpenAsDistributing(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	p := ProductLaunch{
		Product:     "cadence",
		FirstPosted: "2026-06-01T10:00:00Z",
		Posts: []LaunchEvent{
			{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "some-newsletter", URL: "https://example.com/n"},
		},
	}
	verdict, _ := TractionVerdict(p, now)
	if verdict == VerdictNotDistributed {
		t.Fatal("an unregistered channel must fail open as distribution-bearing")
	}
	if verdict != VerdictKill {
		t.Fatalf("a real placement with no signal past the kill window is still KILL, got %q", verdict)
	}
}

func TestTractionVerdict_OneDistributionChannelIsEnough(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	p := ProductLaunch{
		Product:     "cadence",
		FirstPosted: "2026-06-01T10:00:00Z",
		Posts: []LaunchEvent{
			{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
			{TS: "2026-06-01T11:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://news.ycombinator.com/item?id=1"},
		},
	}
	if verdict, _ := TractionVerdict(p, now); verdict == VerdictNotDistributed {
		t.Fatal("a launch with any real distribution placement was distributed")
	}
}

func TestChannelDistributes(t *testing.T) {
	for _, key := range []string{"github-release", "changelog", "internal"} {
		if ChannelDistributes(key) {
			t.Errorf("%q must be registered as non-distributing", key)
		}
	}
	for _, key := range []string{"show-hn", "reddit", "producthunt", "x", "linkedin", "indiehackers", "unregistered-thing", ""} {
		if !ChannelDistributes(key) {
			t.Errorf("%q must be treated as distribution-bearing", key)
		}
	}
	if _, ok := NonDistributionChannelByKey("GitHub-Release"); !ok {
		t.Error("non-distribution lookup must be case-insensitive")
	}
	for _, c := range NonDistributionChannels() {
		if strings.TrimSpace(c.Reason) == "" {
			t.Errorf("%q must carry a reason — verdicts and audits quote it", c.Key)
		}
	}
}

func TestAuditLaunchLedger_NotDistributedAnomaly(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	tagOnly := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
	}
	anomalies := AuditLaunchLedger(tagOnly, now)
	if !hasAnomalyCode(anomalies, "not_distributed") {
		t.Fatalf("tag-only launch must raise not_distributed, got %+v", anomalies)
	}

	distributed := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/r"},
	}
	if hasAnomalyCode(AuditLaunchLedger(distributed, now), "not_distributed") {
		t.Error("a real distribution placement must not raise not_distributed")
	}
}

// placements is tracked separately from the `posted` map precisely because the
// signal_before_post branch also sets `posted` in order to report once per
// product. Reusing it would invent a placement — and therefore a
// not_distributed finding — for a product that only ever recorded a stray signal.
func TestAuditLaunchLedger_StraySignalDoesNotSynthesizePlacement(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "echo", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T10:00:00Z", Product: "echo", Type: EventSignal, Metric: MetricClicks, Value: 3, Source: SourceOperator},
	}
	anomalies := AuditLaunchLedger(events, now)
	if !hasAnomalyCode(anomalies, "signal_before_post") {
		t.Fatalf("expected signal_before_post, got %+v", anomalies)
	}
	if hasAnomalyCode(anomalies, "not_distributed") {
		t.Error("a stray signal is not a placement and must not raise not_distributed")
	}
}

// A re-plan starts a fresh attempt, so distribution state must reset with it.
func TestAuditLaunchLedger_ReplanResetsDistributionState(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/a"},
		{TS: "2026-07-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W27"},
		{TS: "2026-07-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/b"},
	}
	if !hasAnomalyCode(AuditLaunchLedger(events, now), "not_distributed") {
		t.Fatal("the second attempt was tag-only — the first attempt's distribution must not carry forward")
	}
}

// The real ledger's shape, end to end through the tolerant reader.
func TestAuditLaunchLedgerRead_TagOnlyLedgerSurfacesFinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	led := LaunchLedger{Path: path}
	for _, ev := range []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
	} {
		if err := led.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	report, err := led.ReadWithIssues()
	if err != nil {
		t.Fatal(err)
	}
	if !hasAnomalyCode(AuditLaunchLedgerRead(report, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), "not_distributed") {
		t.Fatal("tolerant read path must surface not_distributed too")
	}
}

func hasAnomalyCode(anomalies []LaunchAnomaly, code string) bool {
	for _, a := range anomalies {
		if a.Code == code {
			return true
		}
	}
	return false
}

// A launch that reached a surface nobody was watching must not be KILLed.
//
// NOT-DISTRIBUTED fixed "we never reached anyone". This is the successor case
// that fix left room for: distribution DID happen, and the destination could
// not see arrivals — so the low score measures our instrumentation, not demand.
// KILLing on it destroys exactly the signal NOT-DISTRIBUTED was invented to
// protect, one level further along the funnel.
func TestTractionVerdictRefusesToKillAnUnmeasuredSurface(t *testing.T) {
	now := time.Now()
	base := ProductLaunch{
		Product:     "garrid",
		FirstPosted: now.Add(-time.Duration(KillAfterDays+5) * 24 * time.Hour).Format(time.RFC3339),
		Posts: []LaunchEvent{{
			TS: now.Format(time.RFC3339), Product: "garrid", Type: EventPosted,
			Channel: "hn", URL: "https://news.ycombinator.com/item?id=1",
		}},
	}

	blind := base
	blind.SurfaceCoverage = SurfaceBlind
	v, rationale := TractionVerdict(blind, now)
	if v != VerdictUnmeasured {
		t.Fatalf("a blind destination must not be judged: got %s (%s)", v, rationale)
	}
	if !strings.Contains(rationale, "not demand") {
		t.Fatalf("rationale must say the score measures instrumentation, got %q", rationale)
	}

	// The same launch against an instrumented surface IS judgeable — otherwise
	// the guard would simply disable the kill gate.
	seen := base
	seen.SurfaceCoverage = SurfaceInstrumented
	if v, _ := TractionVerdict(seen, now); v != VerdictKill {
		t.Fatalf("an instrumented surface past the kill window must still KILL, got %s", v)
	}

	// An un-asked question is not a negative answer: absent coverage metadata
	// must not flip every historical ledger row to UNMEASURED.
	if v, _ := TractionVerdict(base, now); v != VerdictKill {
		t.Fatalf("unknown coverage must not be treated as blind, got %s", v)
	}
}

// Measured demand outranks instrumentation state: if a blind surface somehow
// produced conversions, that is real and the engine must say so.
func TestTractionVerdictLetsRealDemandOutrankBlindness(t *testing.T) {
	now := time.Now()
	p := ProductLaunch{
		Product:         "garrid",
		SurfaceCoverage: SurfaceBlind,
		FirstPosted:     now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
		Posts: []LaunchEvent{{
			TS: now.Format(time.RFC3339), Product: "garrid", Type: EventPosted,
			Channel: "hn", URL: "https://news.ycombinator.com/item?id=1",
		}},
		Signals: []LaunchEvent{{
			TS: now.Format(time.RFC3339), Product: "garrid", Type: EventSignal,
			Metric: MetricSignups, Value: 3, Source: SourceOperator,
		}},
	}
	if v, _ := TractionVerdict(p, now); v != VerdictDoubleDown {
		t.Fatalf("observed conversions outrank a blind surface, got %s", v)
	}
}
