package gtm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nstranquist/ngtm/internal/lockfile"
)

func TestLaunchLedger_AppendRead(t *testing.T) {
	led := LaunchLedger{Path: filepath.Join(t.TempDir(), "ledger.jsonl")}
	if evs, err := led.Read(); err != nil || len(evs) != 0 {
		t.Fatalf("missing ledger should read empty, got %v / %v", evs, err)
	}
	want := []LaunchEvent{
		{TS: "2026-06-08T10:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-08T11:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://news.ycombinator.com/item?id=9"},
	}
	for _, ev := range want {
		if err := led.Append(ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := led.Append(LaunchEvent{Type: EventKit}); err == nil {
		t.Fatal("append without product must fail")
	}
	got, err := led.Read()
	if err != nil || len(got) != 2 {
		t.Fatalf("read: %v %v", got, err)
	}
	if got[1].Channel != "show-hn" || got[0].Week != "2026-W24" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestLaunchEventValidateRejectsNonCanonicalIdentifiersAndImpossibleWeek(t *testing.T) {
	if ValidISOWeek("2021-W53") {
		t.Fatal("2021 has only 52 ISO weeks")
	}
	if err := (LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: "p", Type: EventSignal, Metric: MetricClicks, Source: SignalSource("Operator")}).Validate(); err == nil {
		t.Fatal("mixed-case source must be rejected instead of changing provenance semantics")
	}
	for _, product := range []string{" Docs-Puller", "Docs-Puller", "docs puller"} {
		if err := (LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: product, Type: EventKit}).Validate(); err == nil {
			t.Errorf("non-canonical product %q must be rejected", product)
		}
	}
	if err := (LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: "p", Type: EventKit, URL: "https://example.com"}).Validate(); err == nil {
		t.Fatal("event-specific validation must reject a URL on a kit event")
	}
	validRetirement := LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: "p", Type: EventRetired, Week: "2026-W24", Disposition: DispositionAbandoned, Reason: "cohort superseded", Source: SourceOperator}
	if err := validRetirement.Validate(); err != nil {
		t.Fatalf("valid retirement rejected: %v", err)
	}
	invalidRetirement := validRetirement
	invalidRetirement.Source = "import"
	if err := invalidRetirement.Validate(); err == nil {
		t.Fatal("retirement without operator provenance must be rejected")
	}
}

func TestLaunchLedger_StrictDecodeRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(path, []byte(`{"ts":"2026-06-08T10:00:00Z","product":"p","type":"kit","surprise":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := (LaunchLedger{Path: path}).ReadWithIssues()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Events) != 0 || len(report.Issues) != 1 || report.Issues[0].Code != "malformed_json" || !strings.Contains(report.Issues[0].Message, "unknown field") {
		t.Fatalf("strict decode report = %+v", report)
	}
}

func TestLaunchLedger_CorruptionFailsClosedAndAuditSeesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	led := LaunchLedger{Path: path}
	if err := led.Append(LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W24"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not-json}\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = led.Read()
	var corruption *LaunchLedgerCorruptionError
	if !errors.As(err, &corruption) || len(corruption.Issues) != 1 {
		t.Fatalf("normal read must fail closed with one issue, got %v", err)
	}
	report, err := led.ReadWithIssues()
	if err != nil {
		t.Fatal(err)
	}
	anomalies := AuditLaunchLedgerRead(report, time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC))
	if len(anomalies) != 1 || anomalies[0].Code != "malformed_json" || anomalies[0].Line != 2 {
		t.Fatalf("audit did not expose corruption: %+v", anomalies)
	}
}

func TestLaunchLedger_ReadWaitsForConcurrentWriterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	led := LaunchLedger{Path: path}
	if err := led.Append(LaunchEvent{TS: "2026-06-08T10:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W24"}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockfile.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, readErr := led.ReadWithIssues()
		done <- readErr
	}()
	select {
	case err := <-done:
		t.Fatalf("read completed while writer lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not resume after writer lock released")
	}
}

func TestSignalScore_LatestPerKeyWins(t *testing.T) {
	signals := []LaunchEvent{
		{Product: "p", Type: EventSignal, Channel: "hackernews", Metric: "hn_points", Value: 10, Source: "hackernews"},
		// re-measure same key: must replace, not add
		{Product: "p", Type: EventSignal, Channel: "hackernews", Metric: "hn_points", Value: 14, Source: "hackernews"},
		{Product: "p", Type: EventSignal, Metric: "signups", Value: 2, Source: "operator"},
	}
	score, parts := SignalScore(signals)
	if want := 14*2 + 2*10.0; score != want {
		t.Fatalf("score = %v, want %v (parts %v)", score, want, parts)
	}
}

func TestSignalScore_UnknownMetricAndNonOperatorConversionCannotShortcut(t *testing.T) {
	signals := []LaunchEvent{{Metric: SignalMetric("made_up"), Value: 999, Source: SourceOperator}}
	if score, _ := SignalScore(signals); score != 0 {
		t.Fatalf("unknown metric contributed score %v", score)
	}
	if _, err := ParseSignalMetric("made_up"); err == nil {
		t.Fatal("unknown metric must be rejected at ingestion")
	}
	nonOperator := []LaunchEvent{{Metric: MetricSignups, Value: 0.1, Source: SignalSource("posthog-export")}}
	if hasConversion(nonOperator) {
		t.Fatal("non-operator signups must not trigger the conversion shortcut")
	}
}

func TestHasConversion_LatestZeroRetractsShortcut(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	posted := LaunchEvent{TS: now.AddDate(0, 0, -1).Format(time.RFC3339), Product: "p", Type: EventPosted, Channel: "show-hn"}
	up := LaunchEvent{TS: now.Add(-2 * time.Hour).Format(time.RFC3339), Product: "p", Type: EventSignal, Channel: "show-hn", Metric: MetricSignups, Value: 5, Source: SourceOperator}
	down := LaunchEvent{TS: now.Add(-time.Hour).Format(time.RFC3339), Product: "p", Type: EventSignal, Channel: "show-hn", Metric: MetricSignups, Value: 0, Source: SourceOperator}
	if !hasConversion([]LaunchEvent{up}) {
		t.Fatal("positive operator signups must take the shortcut")
	}
	if hasConversion([]LaunchEvent{up, down}) {
		t.Fatal("later operator signups=0 must retract the conversion shortcut")
	}
	v, rationale := TractionVerdict(ProductLaunch{
		Product: "p", Posts: []LaunchEvent{posted}, FirstPosted: posted.TS,
		Signals: []LaunchEvent{up, down},
	}, now)
	if v == VerdictDoubleDown && strings.Contains(rationale, "operator-observed conversions") {
		t.Fatalf("zeroed latest conversion still DOUBLE-DOWN via shortcut: %s (%s)", v, rationale)
	}
}

func TestTractionVerdict_Gates(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	post := func(daysAgo int) []LaunchEvent {
		return []LaunchEvent{{TS: now.AddDate(0, 0, -daysAgo).Format(time.RFC3339), Product: "p", Type: EventPosted, Channel: "show-hn"}}
	}
	sig := func(metric SignalMetric, v float64, src SignalSource) LaunchEvent {
		return LaunchEvent{Product: "p", Type: EventSignal, Metric: metric, Value: v, Source: src}
	}
	cases := []struct {
		name    string
		p       ProductLaunch
		verdict LaunchVerdict
	}{
		{"not launched", ProductLaunch{Product: "p"}, VerdictNotLaunched},
		{"conversions always double-down", ProductLaunch{Posts: post(1), FirstPosted: post(1)[0].TS,
			Signals: []LaunchEvent{sig("signups", 3, "operator")}}, VerdictDoubleDown},
		{"big community score double-down", ProductLaunch{Posts: post(2), FirstPosted: post(2)[0].TS,
			Signals: []LaunchEvent{sig("hn_points", 25, "hackernews")}}, VerdictDoubleDown},
		{"mid score iterate", ProductLaunch{Posts: post(5), FirstPosted: post(5)[0].TS,
			Signals: []LaunchEvent{sig("hn_points", 7, "hackernews")}}, VerdictIterate},
		{"quiet but fresh too-early", ProductLaunch{Posts: post(1), FirstPosted: post(1)[0].TS}, VerdictTooEarly},
		{"quiet mid-window watch", ProductLaunch{Posts: post(5), FirstPosted: post(5)[0].TS}, VerdictWatch},
		{"quiet after 7 days kill", ProductLaunch{Posts: post(8), FirstPosted: post(8)[0].TS}, VerdictKill},
	}
	for _, tc := range cases {
		v, rationale := TractionVerdict(tc.p, now)
		if v != tc.verdict {
			t.Errorf("%s: verdict = %s (%s), want %s", tc.name, v, rationale, tc.verdict)
		}
		if rationale == "" {
			t.Errorf("%s: empty rationale", tc.name)
		}
	}
}

func TestBuildLaunchesAndCohorts(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-08T10:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-08T10:05:00Z", Product: "cadence", Type: EventKit},
		{TS: "2026-06-08T11:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn"},
		{TS: "2026-06-09T09:00:00Z", Product: "cadence", Type: EventSignal, Channel: "hackernews", Metric: "hn_points", Value: 30, Source: "hackernews"},
		{TS: "2026-06-01T10:00:00Z", Product: "keyring", Type: EventPlanned, Week: "2026-W23"},
	}
	launches := BuildLaunches(events, now)
	if len(launches) != 2 {
		t.Fatalf("want 2 launches, got %+v", launches)
	}
	// Sorted by week: keyring (W23) first.
	if launches[0].Product != "keyring" || launches[0].Verdict != VerdictNotLaunched {
		t.Fatalf("keyring state wrong: %+v", launches[0])
	}
	cad := launches[1]
	if cad.Stage() != "measured" || cad.Score != 60 || cad.Verdict != VerdictDoubleDown {
		t.Fatalf("cadence state wrong: %+v", cad)
	}

	cohorts := BuildCohorts(launches, 0)
	if len(cohorts) != 2 || cohorts[0].Week != "2026-W23" || cohorts[1].Target != 20 {
		t.Fatalf("cohorts wrong: %+v", cohorts)
	}
}

func TestBuildLaunchesRetirementAndReplan(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-08T10:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-09T10:00:00Z", Product: "docs-puller", Type: EventRetired, Week: "2026-W24", Disposition: DispositionAbandoned, Reason: "cohort superseded", Source: SourceOperator},
	}
	launches := BuildLaunches(events, now)
	if len(launches) != 1 || launches[0].Stage() != "retired" || launches[0].Verdict != VerdictAbandoned {
		t.Fatalf("retired launch projection = %+v", launches)
	}
	events = append(events, LaunchEvent{TS: "2026-07-12T10:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W28"})
	launches = BuildLaunches(events, now)
	if len(launches) != 1 || launches[0].Stage() != "planned" || launches[0].Week != "2026-W28" || launches[0].Disposition != "" {
		t.Fatalf("re-plan must start a fresh active attempt: %+v", launches)
	}
}

func TestBuildLaunchesSameWeekReplanStartsFreshAttempt(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-07-10T10:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W28"},
		{TS: "2026-07-10T11:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "show-hn", URL: "https://news.ycombinator.com/item?id=1"},
		{TS: "2026-07-10T12:00:00Z", Product: "docs-puller", Type: EventSignal, Channel: "hackernews", Metric: MetricHNPoints, Value: 20, Source: SourceHackerNews},
		{TS: "2026-07-11T10:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W28"},
	}
	launches := BuildLaunches(events, now)
	if len(launches) != 1 || launches[0].Stage() != "planned" || len(launches[0].Posts) != 0 || len(launches[0].Signals) != 0 || launches[0].Score != 0 {
		t.Fatalf("same-week re-plan carried old attempt state: %+v", launches)
	}
}

func TestAuditLaunchLedgerCatchesIllegalTransitions(t *testing.T) {
	events := []LaunchEvent{
		{TS: "2026-07-10T09:00:00Z", Product: "kit-only", Type: EventKit},
		{TS: "2026-07-10T10:00:00Z", Product: "retired-post", Type: EventPlanned, Week: "2026-W28"},
		{TS: "2026-07-10T11:00:00Z", Product: "retired-post", Type: EventPosted, Channel: "show-hn", URL: "https://news.ycombinator.com/item?id=1"},
		{TS: "2026-07-10T12:00:00Z", Product: "retired-post", Type: EventRetired, Week: "2026-W28", Disposition: DispositionAbandoned, Reason: "invalid close", Source: SourceOperator},
		{TS: "2026-07-10T10:00:00Z", Product: "killed", Type: EventPlanned, Week: "2026-W28"},
		{TS: "2026-07-10T11:00:00Z", Product: "killed", Type: EventPosted, Channel: "show-hn", URL: "https://news.ycombinator.com/item?id=2"},
		{TS: "2026-07-10T12:00:00Z", Product: "killed", Type: EventVerdict, Verdict: VerdictKill},
		{TS: "2026-07-10T13:00:00Z", Product: "killed", Type: EventSignal, Channel: "hackernews", Metric: MetricHNPoints, Value: 1, Source: SourceHackerNews},
	}
	anomalies := AuditLaunchLedger(events, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	codes := map[string]bool{}
	for _, anomaly := range anomalies {
		codes[anomaly.Code] = true
	}
	for _, code := range []string{"kit_before_plan", "retirement_after_post", "zombie_after_kill"} {
		if !codes[code] {
			t.Errorf("missing %s in %+v", code, anomalies)
		}
	}
}

func TestMeasureLaunchSignals_FeedsBecomeObservedSignals(t *testing.T) {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "hackernews", tier: TierFree, ev: []Evidence{
		{ID: "hackernews:0", Feed: "hackernews", Tier: TierFree, Title: "cadence on HN",
			Snippet: "120 points", Metric: "mentions", Value: "120", Retrieved: "2026-06-02T12:00:00Z"},
		{ID: "hackernews:1", Feed: "hackernews", Tier: TierFree, Title: "cadence again",
			Snippet: "30 points", Metric: "mentions", Value: "30", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	reg.Register(fakeFeed{name: "reddit", tier: TierFree, ev: []Evidence{
		{ID: "reddit:0", Feed: "reddit", Tier: TierFree, Title: "r/macapps", Snippet: "55 score",
			Metric: "mentions", Value: "55", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	eng := NewEngineWith(reg, offlineGenerator{}, fixedNow)
	sigs, warnings := eng.MeasureLaunchSignals(context.Background(), "cadence", []FeedTier{TierFree})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got := map[string]float64{}
	for _, s := range sigs {
		if s.Type != EventSignal || s.Source == "operator" || s.Source == "" {
			t.Fatalf("measured signal must carry feed provenance: %+v", s)
		}
		got[string(s.Metric)] = s.Value
	}
	want := map[string]float64{"hn_mentions": 2, "hn_points": 150, "reddit_mentions": 1, "reddit_score": 55}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v (all %v)", k, got[k], v, got)
		}
	}
}

func TestMeasureLaunchSignals_RSSMentionDoesNotInventRedditScore(t *testing.T) {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "reddit", tier: TierFree, ev: []Evidence{{
		ID: "reddit-rss:t3_1", Feed: "reddit", Tier: TierFree, Title: "docs-puller mention",
		Metric: "mentions", Value: "1", Retrieved: "2026-06-02T12:00:00Z",
		Extra: map[string]string{"transport": "rss", "score_provenance": "unavailable"},
	}}})
	eng := NewEngineWith(reg, offlineGenerator{}, fixedNow)
	signals, warnings := eng.MeasureLaunchSignals(context.Background(), "docs-puller", []FeedTier{TierFree})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(signals) != 1 || signals[0].Metric != MetricRedditMentions || signals[0].Value != 1 {
		t.Fatalf("RSS fallback invented engagement: %+v", signals)
	}
}

func TestBuildRetro_ChannelLearning(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-08T10:00:00Z", Product: "a", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-08T11:00:00Z", Product: "a", Type: EventPosted, Channel: "show-hn"},
		{TS: "2026-06-09T09:00:00Z", Product: "a", Type: EventSignal, Channel: "hackernews", Metric: "hn_points", Value: 30, Source: "hackernews"},
		{TS: "2026-06-08T10:00:00Z", Product: "b", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-08T11:00:00Z", Product: "b", Type: EventPosted, Channel: "x"},
	}
	r := BuildRetro(BuildLaunches(events, now), "2026-W24", 20)
	if r.Planned != 2 || r.Posted != 2 || r.Measured != 1 {
		t.Fatalf("counts wrong: %+v", r)
	}
	if r.DoubleDownRate != 0.5 {
		t.Fatalf("double-down rate = %v, want 0.5", r.DoubleDownRate)
	}
	if len(r.Channels) == 0 || r.Channels[0].Channel != "hackernews" || r.Channels[0].Score != 60 {
		t.Fatalf("channel leaderboard wrong: %+v", r.Channels)
	}
	wantFrags := []string{"plan 18 more", "top channel: hackernews", "x: 1 post(s), zero measured signal", "1 posted product(s) unmeasured"}
	joined := ""
	for _, rec := range r.Recommendations {
		joined += rec + "\n"
	}
	for _, frag := range wantFrags {
		if !strings.Contains(joined, frag) {
			t.Errorf("recommendations missing %q in:\n%s", frag, joined)
		}
	}
}

func TestAuditLaunchLedger_Anomalies(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		// posted with no plan
		{TS: "2026-06-08T11:00:00Z", Product: "rogue", Type: EventPosted, Channel: "x"},
		// signal before any post
		{TS: "2026-06-08T11:00:00Z", Product: "ghost", Type: EventSignal, Metric: "signups", Value: 1, Source: "operator"},
		// stale plan (>7 days, never posted)
		{TS: "2026-05-20T10:00:00Z", Product: "stale", Type: EventPlanned, Week: "2026-W21"},
		// clean product
		{TS: "2026-06-08T10:00:00Z", Product: "clean", Type: EventPlanned, Week: "2026-W24"},
		{TS: "2026-06-08T11:00:00Z", Product: "clean", Type: EventPosted, Channel: "show-hn"},
	}
	got := map[string]string{}
	for _, a := range AuditLaunchLedger(events, now) {
		got[a.Product] = a.Code
	}
	want := map[string]string{"rogue": "posted_before_plan", "ghost": "signal_before_post", "stale": "stale_plan"}
	for p, code := range want {
		if got[p] != code {
			t.Errorf("%s: got %q want %q (all %v)", p, got[p], code, got)
		}
	}
	if _, bad := got["clean"]; bad {
		t.Errorf("clean product flagged: %v", got)
	}
}

func TestAuditLaunchLedger_PersistedKillMakesLaterPostZombie(t *testing.T) {
	events := []LaunchEvent{
		{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-01T11:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/first"},
		{TS: "2026-06-09T11:00:00Z", Product: "cadence", Type: EventVerdict, Verdict: VerdictKill},
		{TS: "2026-06-10T11:00:00Z", Product: "cadence", Type: EventPosted, Channel: "x", URL: "https://example.com/zombie"},
	}
	anomalies := AuditLaunchLedger(events, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC))
	if len(anomalies) != 1 || anomalies[0].Code != "zombie_after_kill" {
		t.Fatalf("persisted kill must make later activity visible: %+v", anomalies)
	}
}

func TestAuditLaunchLedgerRetirementClosesStaleAttempt(t *testing.T) {
	events := []LaunchEvent{
		{TS: "2026-06-01T10:00:00Z", Product: "retired", Type: EventPlanned, Week: "2026-W23"},
		{TS: "2026-06-10T10:00:00Z", Product: "retired", Type: EventRetired, Week: "2026-W23", Disposition: DispositionAbandoned, Reason: "missed launch window", Source: SourceOperator},
	}
	if got := AuditLaunchLedger(events, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("retired attempt must not remain stale: %+v", got)
	}
	events = append(events, LaunchEvent{TS: "2026-06-11T10:00:00Z", Product: "retired", Type: EventKit})
	got := AuditLaunchLedger(events, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 || got[0].Code != "activity_after_retirement" {
		t.Fatalf("post-retirement activity must fail audit: %+v", got)
	}
}

func TestLaunchEventValidateRejectsPrivatePostedReceipt(t *testing.T) {
	base := LaunchEvent{
		TS: "2026-08-14T10:00:00Z", Product: "ngtm", Type: EventPosted,
		Channel: "show-hn", URL: "https://example.com/post",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("public hostname must be accepted without DNS: %v", err)
	}
	for _, raw := range []string{
		"http://127.0.0.1/post",
		"http://localhost/post",
		"http://10.0.0.8/post",
		"http://192.168.1.4/post",
		"http://[::1]/post",
	} {
		ev := base
		ev.URL = raw
		if err := ev.Validate(); err == nil {
			t.Errorf("private receipt %s must be rejected", raw)
		}
	}
}

func TestLaunchLedgerAppendRejectsIllegalTransitions(t *testing.T) {
	led := LaunchLedger{Path: filepath.Join(t.TempDir(), "ledger.jsonl")}
	mustAppend := func(ev LaunchEvent) {
		t.Helper()
		if err := led.Append(ev); err != nil {
			t.Fatalf("append %+v: %v", ev, err)
		}
	}
	mustReject := func(ev LaunchEvent, code string) {
		t.Helper()
		err := led.Append(ev)
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Fatalf("append %+v = %v, want %s", ev, err, code)
		}
	}

	mustReject(LaunchEvent{TS: "2026-08-14T10:00:00Z", Product: "ngtm", Type: EventKit}, "kit_before_plan")
	mustReject(LaunchEvent{
		TS: "2026-08-14T10:00:01Z", Product: "ngtm", Type: EventPosted,
		Channel: "show-hn", URL: "https://example.com/a",
	}, "posted_before_plan")
	mustReject(LaunchEvent{
		TS: "2026-08-14T10:00:02Z", Product: "ngtm", Type: EventSignal,
		Metric: MetricSignups, Value: 1, Source: SourceOperator,
	}, "signal_before_plan")

	mustAppend(LaunchEvent{TS: "2026-08-14T10:01:00Z", Product: "ngtm", Type: EventPlanned, Week: "2026-W33"})
	mustReject(LaunchEvent{
		TS: "2026-08-14T10:01:01Z", Product: "ngtm", Type: EventSignal,
		Metric: MetricSignups, Value: 1, Source: SourceOperator,
	}, "signal_before_post")
	mustAppend(LaunchEvent{TS: "2026-08-14T10:01:02Z", Product: "ngtm", Type: EventKit})
	mustAppend(LaunchEvent{
		TS: "2026-08-14T10:01:03Z", Product: "ngtm", Type: EventPosted,
		Channel: "show-hn", URL: "https://example.com/a",
	})
	mustAppend(LaunchEvent{
		TS: "2026-08-14T10:01:04Z", Product: "ngtm", Type: EventSignal,
		Metric: MetricSignups, Value: 1, Source: SourceOperator,
	})

	mustAppend(LaunchEvent{TS: "2026-08-14T10:02:00Z", Product: "killed", Type: EventPlanned, Week: "2026-W33"})
	mustAppend(LaunchEvent{
		TS: "2026-08-14T10:02:01Z", Product: "killed", Type: EventPosted,
		Channel: "show-hn", URL: "https://example.com/k",
	})
	mustAppend(LaunchEvent{TS: "2026-08-14T10:02:02Z", Product: "killed", Type: EventVerdict, Verdict: VerdictKill})
	mustReject(LaunchEvent{
		TS: "2026-08-14T10:02:03Z", Product: "killed", Type: EventPosted,
		Channel: "x", URL: "https://example.com/zombie",
	}, "zombie_after_kill")
	mustAppend(LaunchEvent{TS: "2026-08-14T10:02:04Z", Product: "killed", Type: EventPlanned, Week: "2026-W33"})
	mustAppend(LaunchEvent{
		TS: "2026-08-14T10:02:05Z", Product: "killed", Type: EventPosted,
		Channel: "x", URL: "https://example.com/replan",
	})

	mustAppend(LaunchEvent{TS: "2026-08-14T10:03:00Z", Product: "retired", Type: EventPlanned, Week: "2026-W33"})
	mustAppend(LaunchEvent{
		TS: "2026-08-14T10:03:01Z", Product: "retired", Type: EventRetired,
		Week: "2026-W33", Disposition: DispositionAbandoned, Reason: "missed window", Source: SourceOperator,
	})
	mustReject(LaunchEvent{TS: "2026-08-14T10:03:02Z", Product: "retired", Type: EventKit}, "activity_after_retirement")
	mustAppend(LaunchEvent{TS: "2026-08-14T10:03:03Z", Product: "retired", Type: EventPlanned, Week: "2026-W33"})
	mustAppend(LaunchEvent{TS: "2026-08-14T10:03:04Z", Product: "retired", Type: EventKit})
}
