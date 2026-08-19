package gtm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndRotateJSONL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NGTM_JSONL_MAX_BYTES", "80")
	path := filepath.Join(dir, "runs.jsonl")
	line := []byte(`{"ts":"2026-08-16T00:00:00Z","vertical":"brand","subject":"aaaaaaaaaaaaaaaa"}`)
	if err := appendAndRotateJSONL(path, line); err != nil {
		t.Fatal(err)
	}
	if err := appendAndRotateJSONL(path, line); err != nil {
		t.Fatal(err)
	}
	fam := jsonlFamily(path)
	if len(fam) < 2 {
		t.Fatalf("expected empty live + rotation, got %v", fam)
	}
	hot := jsonlHotBytes(path)
	if hot > 80 {
		t.Fatalf("hot file still over cap: %d", hot)
	}
	gzCount := 0
	for _, p := range fam {
		if strings.HasSuffix(p, ".gz") {
			gzCount++
		}
	}
	if gzCount < 1 {
		t.Fatalf("expected a .gz rotation, family=%v", fam)
	}
}

func TestQueryRunsLimitAndNoBlob(t *testing.T) {
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
	for _, sub := range []string{"alpha", "beta", "alpha-2"} {
		if err := s.InsertRun(map[string]any{"ts": "2026-08-16T00:00:00Z", "vertical": "brand", "subject": sub}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.QueryRuns(RunQuery{Vertical: "brand", Subject: "alpha", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("limit: %+v", rows)
	}
	if rows[0].Subject == "" || rows[0].Vertical != "brand" {
		t.Fatalf("row=%+v", rows[0])
	}
}

func TestJSONLFamilyFingerprintIncludesRotations(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "runs.jsonl")
	if err := os.WriteFile(live, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live+".1", []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := jsonlFamilyFingerprint(live)
	if fp == "" || len(jsonlFamily(live)) != 2 {
		t.Fatalf("family/fp %v %q", jsonlFamily(live), fp)
	}
}
