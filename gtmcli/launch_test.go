package gtmcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runLaunchCLI drives the full Dispatch path the way ngtm does.
func runLaunchCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestLaunchLoop_EndToEnd(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	kit := filepath.Join(t.TempDir(), "kit.md")

	if code, _, e := runLaunchCLI(t, "launch", "plan", "cadence", "--week", "2026-W24", "--ledger", ledger); code != 0 {
		t.Fatalf("plan failed (%d): %s", code, e)
	}
	// Bad week form fails closed.
	if code, _, _ := runLaunchCLI(t, "launch", "plan", "x", "--week", "next week", "--ledger", ledger); code != 2 {
		t.Fatal("malformed --week must exit 2")
	}

	code, o, e := runLaunchCLI(t, "launch", "kit", "cadence",
		"--pitch", "AI standup debriefs from your menu bar",
		"--channels", "show-hn,producthunt",
		"--offline", "--out", kit, "--ledger", ledger)
	if code != 0 {
		t.Fatalf("kit failed (%d): %s%s", code, o, e)
	}

	if code, _, e := runLaunchCLI(t, "launch", "posted", "cadence", "--channel", "show-hn",
		"--url", "https://news.ycombinator.com/item?id=1", "--ledger", ledger); code != 0 {
		t.Fatalf("posted failed (%d): %s", code, e)
	}
	if code, _, e := runLaunchCLI(t, "launch", "signal", "cadence", "--metric", "signups", "--value", "5", "--ledger", ledger); code != 0 {
		t.Fatalf("signal failed (%d): %s", code, e)
	}

	code, o, _ = runLaunchCLI(t, "launch", "cohort", "--json", "--ledger", ledger)
	if code != 0 {
		t.Fatalf("cohort failed: %s", o)
	}
	var board struct {
		Cohorts []struct {
			Week     string `json:"week"`
			Target   int    `json:"target"`
			Products []struct {
				Product string  `json:"product"`
				Score   float64 `json:"score"`
				Verdict string  `json:"verdict"`
			} `json:"products"`
		} `json:"cohorts"`
	}
	if err := json.Unmarshal([]byte(o), &board); err != nil {
		t.Fatalf("cohort JSON: %v\n%s", err, o)
	}
	if len(board.Cohorts) != 1 || board.Cohorts[0].Week != "2026-W24" || board.Cohorts[0].Target != 20 {
		t.Fatalf("board shape wrong: %+v", board)
	}
	p := board.Cohorts[0].Products[0]
	if p.Product != "cadence" || p.Verdict != "DOUBLE-DOWN" || p.Score != 50 {
		t.Fatalf("operator conversions must double-down: %+v", p)
	}

	code, o, _ = runLaunchCLI(t, "launch", "verdict", "cadence", "--json", "--ledger", ledger)
	if code != 0 || !strings.Contains(o, "DOUBLE-DOWN") || !strings.Contains(o, "operator-recorded") {
		t.Fatalf("verdict output wrong (%d): %s", code, o)
	}
	ledgerBytes, err := os.ReadFile(ledger)
	if err != nil || !strings.Contains(string(ledgerBytes), `"type":"verdict"`) || !strings.Contains(string(ledgerBytes), `"verdict":"DOUBLE-DOWN"`) {
		t.Fatalf("verdict snapshot not persisted: %v\n%s", err, ledgerBytes)
	}

	// show drill-in includes the placement receipt.
	_, o, _ = runLaunchCLI(t, "launch", "show", "cadence", "--ledger", ledger)
	if !strings.Contains(o, "show-hn") || !strings.Contains(o, "news.ycombinator.com") {
		t.Fatalf("show missing receipt: %s", o)
	}
}

func TestLaunchKit_WritesNormLintedPack(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	if code, _, e := runLaunchCLI(t, "launch", "plan", "nvault", "--week", "2026-W33", "--ledger", ledger); code != 0 {
		t.Fatalf("plan failed: %s", e)
	}
	code, o, e := runLaunchCLI(t, "launch", "kit", "nvault", "--pitch", "offline-first secrets with E2EE sync",
		"--offline", "--ledger", ledger)
	if code != 0 {
		t.Fatalf("kit failed (%d): %s", code, e)
	}
	for _, frag := range []string{"Show HN draft", "Distribution Calendar", "[FILL:"} {
		if !strings.Contains(o, frag) {
			t.Errorf("kit output missing %q", frag)
		}
	}
}

func TestLaunchGuards(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	if code, _, _ := runLaunchCLI(t, "launch", "bogus"); code != 2 {
		t.Fatal("unknown subcommand must exit 2")
	}
	if code, _, _ := runLaunchCLI(t, "launch", "posted", "cadence", "--ledger", ledger); code != 2 {
		t.Fatal("posted without --channel/--url must exit 2")
	}
	if code, _, e := runLaunchCLI(t, "launch", "posted", "cadence", "--channel", "x", "--url", "https://example.com/rogue", "--ledger", ledger); code != 1 || !strings.Contains(e, "posted_before_plan") {
		t.Fatalf("posted without a plan must fail closed, code=%d err=%s", code, e)
	}
	if code, _, _ := runLaunchCLI(t, "launch", "signal", "cadence", "--metric", "made_up", "--value", "4", "--ledger", ledger); code != 2 {
		t.Fatal("unknown signal metric must exit 2")
	}
	if code, _, _ := runLaunchCLI(t, "launch", "verdict", "ghost", "--ledger", ledger); code != 3 {
		t.Fatal("verdict for unknown product must exit 3")
	}
	if code, o, _ := runLaunchCLI(t, "launch", "channels"); code != 0 || !strings.Contains(o, "show-hn") {
		t.Fatalf("channels listing broken: %s", o)
	}
}

func TestLaunchRetireClosesUnplacedAttempt(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	if code, _, e := runLaunchCLI(t, "launch", "plan", "docs-puller", "--week", "2026-W24", "--ledger", ledger); code != 0 {
		t.Fatal(e)
	}
	code, out, errOut := runLaunchCLI(t, "launch", "retire", "docs-puller",
		"--disposition", "abandoned", "--reason", "cohort superseded", "--json", "--ledger", ledger)
	if code != 0 || !strings.Contains(out, `"disposition": "abandoned"`) {
		t.Fatalf("retire failed (%d): %s%s", code, out, errOut)
	}
	code, out, _ = runLaunchCLI(t, "launch", "show", "docs-puller", "--json", "--ledger", ledger)
	if code != 0 || !strings.Contains(out, `"verdict": "ABANDONED"`) || !strings.Contains(out, `"retirement_reason": "cohort superseded"`) {
		t.Fatalf("retired projection wrong (%d): %s", code, out)
	}
	if code, _, _ = runLaunchCLI(t, "launch", "audit", "--strict", "--ledger", ledger); code != 0 {
		t.Fatalf("retired attempt must satisfy strict audit, got %d", code)
	}
	if code, _, _ = runLaunchCLI(t, "launch", "retire", "docs-puller", "--disposition", "abandoned", "--reason", "again", "--ledger", ledger); code != 3 {
		t.Fatalf("duplicate retirement must exit 3, got %d", code)
	}
}

func TestLaunchAudit_CorruptionVisibleWhileNormalReadsFailClosed(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	if code, _, e := runLaunchCLI(t, "launch", "plan", "cadence", "--week", "2026-W24", "--ledger", ledger); code != 0 {
		t.Fatal(e)
	}
	f, err := os.OpenFile(ledger, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("broken-json\n")
	_ = f.Close()
	if code, _, e := runLaunchCLI(t, "launch", "show", "cadence", "--ledger", ledger); code != 1 || !strings.Contains(e, "corrupt/invalid") {
		t.Fatalf("normal read must fail closed, code=%d err=%s", code, e)
	}
	code, out, _ := runLaunchCLI(t, "launch", "audit", "--strict", "--json", "--ledger", ledger)
	if code != 3 || !strings.Contains(out, `"corrupt_rows": 1`) || !strings.Contains(out, `"line": 2`) {
		t.Fatalf("strict audit must expose corruption, code=%d out=%s", code, out)
	}
}

func TestLaunchAuditLiveReceiptHostMismatchFailsStrict(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	for _, args := range [][]string{
		{"launch", "plan", "docs-puller", "--week", "2026-W24", "--ledger", ledger},
		{"launch", "posted", "docs-puller", "--channel", "show-hn", "--url", "https://example.com/unrelated", "--ledger", ledger},
	} {
		if code, _, errOut := runLaunchCLI(t, args...); code != 0 {
			t.Fatalf("%v failed: %s", args, errOut)
		}
	}
	code, out, _ := runLaunchCLI(t, "launch", "audit", "--verify-receipts", "--strict", "--json", "--ledger", ledger)
	if code != 3 || !strings.Contains(out, `"code": "receipt_host_mismatch"`) {
		t.Fatalf("semantic receipt mismatch must fail strict audit (%d): %s", code, out)
	}
}

func TestLaunchRetroAuditOpenImport(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "ledger.jsonl")
	// Seed a healthy attempt through the write-time gates, then inject a
	// historical rogue row the CLI can no longer mint (audit still sees it).
	for _, args := range [][]string{
		{"launch", "plan", "cadence", "--week", "2026-W24", "--ledger", ledger},
		{"launch", "posted", "cadence", "--channel", "show-hn", "--url", "https://news.ycombinator.com/item?id=1", "--ledger", ledger},
		{"launch", "signal", "cadence", "--metric", "signups", "--value", "4", "--channel", "show-hn", "--ledger", ledger},
	} {
		if code, _, e := runLaunchCLI(t, args...); code != 0 {
			t.Fatalf("%v failed: %s", args, e)
		}
	}
	existing, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, append(existing, []byte(`{"ts":"2026-06-08T11:00:00Z","product":"rogue","type":"posted","channel":"x","url":"https://example.com/rogue"}`+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// retro: channel leaderboard + recommendations
	code, o, _ := runLaunchCLI(t, "launch", "retro", "--week", "2026-W24", "--ledger", ledger)
	if code != 0 || !strings.Contains(o, "Launch retro") || !strings.Contains(o, "show-hn") {
		t.Fatalf("retro output wrong (%d): %s", code, o)
	}

	// audit: rogue flagged; --strict exits 3
	code, o, _ = runLaunchCLI(t, "launch", "audit", "--ledger", ledger)
	if code != 0 || !strings.Contains(o, "posted_before_plan") {
		t.Fatalf("audit output wrong (%d): %s", code, o)
	}
	if code, _, _ = runLaunchCLI(t, "launch", "audit", "--strict", "--ledger", ledger); code != 3 {
		t.Fatalf("strict audit with anomalies must exit 3, got %d", code)
	}

	// open: prefilled composer links
	code, o, _ = runLaunchCLI(t, "launch", "open", "cadence", "--channel", "x", "--pitch", "AI debriefs in 30s")
	if code != 0 || !strings.Contains(o, "x.com/intent/post?text=") {
		t.Fatalf("open output wrong (%d): %s", code, o)
	}
	code, o, _ = runLaunchCLI(t, "--json", "launch", "open", "cadence", "--channel", "show-hn", "--url", "https://github.com/nstranquist/ncli")
	if code != 0 || !strings.Contains(o, `"channel": "show-hn"`) || !strings.Contains(o, "news.ycombinator.com/submitlink") {
		t.Fatalf("open --json output wrong (%d): %s", code, o)
	}
	code, o, _ = runLaunchCLI(t, "--json", "launch", "open", "ngtm", "--channel", "show-hn",
		"--pitch", "Nicos GTM – a local CLI that refuses to invent launch facts",
		"--url", "https://github.com/nstranquist/ngtm")
	if code != 0 {
		t.Fatalf("open nicos gtm failed (%d): %s", code, o)
	}
	if strings.Contains(o, "ngtm+%E2%80%93+Nicos") || strings.Contains(o, "Show+HN%3A+Show+HN") {
		t.Fatalf("open title doubled product name: %s", o)
	}
	if !strings.Contains(o, "Nicos+GTM") && !strings.Contains(o, "Nicos GTM") {
		t.Fatalf("open --json missing Nicos GTM: %s", o)
	}

	// import: JSON file → recorded signals raise the score
	metrics := filepath.Join(dir, "m.json")
	if err := os.WriteFile(metrics, []byte(`[{"metric":"clicks","value":300,"channel":"web","source":"posthog-export"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	spoof := filepath.Join(dir, "operator.json")
	if err := os.WriteFile(spoof, []byte(`[{"metric":"signups","value":1,"source":"operator"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := runLaunchCLI(t, "launch", "import", "cadence", "--file", spoof, "--record", "--ledger", ledger); code == 0 {
		t.Fatalf("import must reject source=operator, err=%s", e)
	} else if !strings.Contains(e, "operator") {
		t.Fatalf("import operator rejection missing from stderr: %s", e)
	}

	code, o, e := runLaunchCLI(t, "launch", "import", "cadence", "--file", metrics, "--record", "--ledger", ledger)
	if code != 0 || !strings.Contains(o, "recorded 1 imported signal") {
		t.Fatalf("import failed (%d): %s%s", code, o, e)
	}
	_, o, _ = runLaunchCLI(t, "launch", "show", "cadence", "--ledger", ledger)
	if !strings.Contains(o, "posthog-export") || !strings.Contains(o, "clicks") {
		t.Fatalf("imported signal missing from show: %s", o)
	}
}

func TestSEOPagesFactory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pages")
	code, o, e := runLaunchCLI(t, "seo", "nvault",
		"--pages", "--out-dir", dir,
		"--keywords", "offline secrets manager,e2ee secrets sync",
		"--pitch", "offline-first secrets with E2EE sync",
		"--buy-url", "https://buy.test/nvault")
	if code != 0 {
		t.Fatalf("seo --pages failed (%d): %s%s", code, o, e)
	}
	for _, f := range []string{"offline-secrets-manager.html", "e2ee-secrets-sync.html", "index.html"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		if f != "index.html" && !strings.Contains(string(b), "nvault") {
			t.Errorf("%s missing product spine", f)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "offline-secrets-manager.html"))
	if !strings.Contains(string(b), "Offline Secrets Manager — nvault") || !strings.Contains(string(b), "https://buy.test/nvault") {
		t.Errorf("keyword page not keyword-targeted:\n%.300s", b)
	}
}
