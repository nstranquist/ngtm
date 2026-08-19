package gtmcli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nstranquist/ngtm/gtm"
)

func TestTelemetryStatusJSON(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "t.sqlite")
	t.Setenv("NGTM_TELEMETRY_DB", db)
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "none-runs.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "none-launch.jsonl"))
	s, err := gtm.OpenTelemetryStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRun(map[string]any{"ts": "2026-08-16T12:00:00Z", "vertical": "seo", "subject": "x"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"telemetry", "status", "--json", "--db", db}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	var st gtm.TelemetryStatus
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Runs != 1 {
		t.Fatalf("runs=%d", st.Runs)
	}
}

func TestTelemetryQueryLaunchJSON(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "t.sqlite")
	t.Setenv("NGTM_TELEMETRY_DB", db)
	t.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "none-runs.jsonl"))
	t.Setenv("NGTM_LAUNCH_LEDGER", filepath.Join(dir, "none-launch.jsonl"))
	l := gtm.LaunchLedger{Path: db}
	if err := l.Append(gtm.LaunchEvent{Type: gtm.EventPlanned, Product: "cadence", Week: "2026-W33", TS: "2026-08-10T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(gtm.LaunchEvent{Type: gtm.EventPlanned, Product: "voice", Week: "2026-W33", TS: "2026-08-11T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"telemetry", "query", "--launch", "--product", "cadence", "--json", "--limit", "10", "--db", db}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if strings.Contains(out.String(), "payload_json") || strings.Contains(out.String(), "extra_json") {
		t.Fatalf("blob leaked: %s", out.String())
	}
	var envelope struct {
		Kind  string            `json:"kind"`
		Count int               `json:"count"`
		Rows  []gtm.LaunchRecord `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != "launch" || envelope.Count != 1 || envelope.Rows[0].Product != "cadence" {
		t.Fatalf("envelope=%+v", envelope)
	}
}
