package gtm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTelemetryStore_InsertRunAndStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NGTM_TELEMETRY_DB", "")
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "missing-runs.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "missing-launch.jsonl"))
	path := filepath.Join(dir, "telemetry.sqlite")
	s, err := OpenTelemetryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.InsertRun(map[string]any{
		"ts": "2026-08-16T00:00:00Z", "vertical": "brand", "subject": "Cadence",
		"panel_median": 6.5, "surface": "ngtm",
	}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs != 1 || st.LaunchEvents != 0 {
		t.Fatalf("status=%+v", st)
	}
	if len(st.ByVertical) != 1 || st.ByVertical[0].Vertical != "brand" {
		t.Fatalf("by_vertical=%+v", st.ByVertical)
	}
}

func TestTelemetryStore_ImportJSONL(t *testing.T) {
	dir := t.TempDir()
	gtmDir := filepath.Join(dir, ".nicos-dev", "gtm")
	if err := os.MkdirAll(gtmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	runs := filepath.Join(gtmDir, "runs.jsonl")
	if err := os.WriteFile(runs, []byte(`{"ts":"2026-06-01T00:00:00Z","vertical":"social","subject":"docs-puller","panel_median":9}
{"ts":"2026-06-02T00:00:00Z","vertical":"brand","subject":"nvault"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(gtmDir, "launch-ledger.jsonl")
	if err := os.WriteFile(ledger, []byte(`{"type":"planned","product":"docs-puller","week":"2026-W24","ts":"2026-06-10T00:00:00Z"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", runs)
	t.Setenv("NGTM_LAUNCH_LEDGER", ledger)
	s, err := OpenTelemetryStore(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ImportedJSONL || st.Runs != 2 || st.LaunchEvents != 1 {
		t.Fatalf("import status=%+v", st)
	}
	// Re-open must not double-import.
	_ = s.Close()
	s2, err := OpenTelemetryStore(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	st2, err := s2.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st2.Runs != 2 || st2.LaunchEvents != 1 {
		t.Fatalf("double import? %+v", st2)
	}
}

func TestLaunchLedger_SQLiteTransitions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "none-runs.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "none-launch.jsonl"))
	path := filepath.Join(dir, "telemetry.sqlite")
	l := LaunchLedger{Path: path}
	plan := LaunchEvent{Type: EventPlanned, Product: "cadence", Week: "2026-W33", TS: time.Now().UTC().Format(time.RFC3339)}
	if err := l.Append(plan); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(LaunchEvent{Type: EventPosted, Product: "cadence", TS: time.Now().UTC().Format(time.RFC3339)}); err == nil {
		t.Fatal("posted without a public receipt must fail validation")
	}
	events, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventPlanned {
		t.Fatalf("events=%+v", events)
	}
}

func TestTelemetryStore_DualWriteAndReimport(t *testing.T) {
	dir := t.TempDir()
	gtmDir := filepath.Join(dir, ".nicos-dev", "gtm")
	if err := os.MkdirAll(gtmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	runs := filepath.Join(gtmDir, "runs.jsonl")
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", runs)
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(gtmDir, "launch-ledger.jsonl"))
	db := filepath.Join(dir, "t.sqlite")
	s, err := OpenTelemetryStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRun(map[string]any{"ts": "2026-08-16T01:00:00Z", "vertical": "brand", "subject": "once"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"subject":"once"`) {
		t.Fatalf("jsonl shadow missing insert: %s", raw)
	}
	if err := s.InsertRun(map[string]any{"ts": "2026-08-16T01:00:00Z", "vertical": "brand", "subject": "once"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs != 1 {
		t.Fatalf("duplicate insert: %+v", st)
	}
	if err := os.WriteFile(runs, append(raw, []byte("{\"ts\":\"2026-08-16T02:00:00Z\",\"vertical\":\"seo\",\"subject\":\"new\"}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportJSONL(); err != nil {
		t.Fatal(err)
	}
	st2, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st2.Runs != 2 {
		t.Fatalf("reimport did not pick up new jsonl row: %+v", st2)
	}
	_ = s.Close()
}

func TestQueryLaunchFiltersProductAndOmitsBlob(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "none.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "none-l.jsonl"))
	db := filepath.Join(dir, "t.sqlite")
	l := LaunchLedger{Path: db}
	older := LaunchEvent{Type: EventPlanned, Product: "cadence", Week: "2026-W32", TS: "2026-08-01T00:00:00Z"}
	newer := LaunchEvent{Type: EventPlanned, Product: "cadence", Week: "2026-W33", TS: "2026-08-10T00:00:00Z"}
	other := LaunchEvent{Type: EventPlanned, Product: "voice", Week: "2026-W33", TS: "2026-08-11T00:00:00Z"}
	for _, ev := range []LaunchEvent{older, newer, other} {
		if err := l.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenTelemetryStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	rows, err := s.QueryLaunch(LaunchQuery{Product: "cadence", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 cadence rows, got %+v", rows)
	}
	if rows[0].TS < rows[1].TS {
		t.Fatalf("not newest-first: %+v", rows)
	}
	if rows[0].Product != "cadence" || rows[1].Product != "cadence" {
		t.Fatalf("product leak: %+v", rows)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "payload_json") || strings.Contains(string(raw), "extra_json") {
		t.Fatalf("blob leaked: %s", raw)
	}
}

func TestImportCountsGzippedRotation(t *testing.T) {
	dir := t.TempDir()
	gtmDir := filepath.Join(dir, ".nicos-dev", "gtm")
	if err := os.MkdirAll(gtmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NGTM_JSONL_MAX_BYTES", "80")
	runs := filepath.Join(gtmDir, "runs.jsonl")
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", runs)
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(gtmDir, "none-l.jsonl"))
	line := []byte(`{"ts":"2026-08-16T00:00:00Z","vertical":"brand","subject":"aaaaaaaaaaaaaaaa"}`)
	if err := appendAndRotateJSONL(runs, line); err != nil {
		t.Fatal(err)
	}
	if err := appendAndRotateJSONL(runs, []byte(`{"ts":"2026-08-16T00:00:01Z","vertical":"seo","subject":"bbbbbbbbbbbbbbbb"}`)); err != nil {
		t.Fatal(err)
	}
	s, err := OpenTelemetryStore(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs < 2 {
		t.Fatalf("gzipped history not imported: %+v family=%v", st, jsonlFamily(runs))
	}
}

func TestTelemetryStore_MigratesV2Hash(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "none.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "none-l.jsonl"))
	s, err := OpenTelemetryStore(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.appliedSchema() != telemetrySchemaCurrent {
		t.Fatalf("schema=%d", s.appliedSchema())
	}
	ok, err := s.hasColumn("runs", "content_sha256")
	if err != nil || !ok {
		t.Fatalf("hash column: %v %v", ok, err)
	}
}
