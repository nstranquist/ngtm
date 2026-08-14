package gtmcli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain prevents CLI self-evaluation from mutating the operator's telemetry
// ledger. Tests still exercise logging against a disposable, test-tagged file.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ngtm-telemetry-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("NGTM_RUNS_TELEMETRY_PATH", filepath.Join(dir, "runs.jsonl"))
	_ = os.Setenv("NGTM_RUN_CONTEXT", "test")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
