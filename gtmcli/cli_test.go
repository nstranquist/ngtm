package gtmcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

type doctorTestFeed struct {
	name      string
	tier      gtm.FeedTier
	key       string
	available bool
}

func (f doctorTestFeed) Name() string       { return f.name }
func (f doctorTestFeed) Tier() gtm.FeedTier { return f.tier }
func (f doctorTestFeed) KeyEnv() string     { return f.key }
func (f doctorTestFeed) Available() bool    { return f.available }
func (f doctorTestFeed) Query(context.Context, gtm.FeedQuery) ([]gtm.Evidence, error) {
	return nil, nil
}

func TestFeedDoctorDistinguishesConfiguredFromLiveProbed(t *testing.T) {
	feeds := []gtm.Feed{
		doctorTestFeed{name: "hackernews", tier: gtm.TierFree, available: true},
		doctorTestFeed{name: "reddit", tier: gtm.TierFree, available: true},
		doctorTestFeed{name: "serper", tier: gtm.TierCheap, key: "SERPER_API_KEY", available: true},
		doctorTestFeed{name: "brave", tier: gtm.TierCheap, key: "BRAVE_API_KEY", available: false},
	}
	rows := probeFeedDoctorRows(context.Background(), feeds, false, func(_ context.Context, f gtm.Feed, _ gtm.FeedQuery) error {
		if f.Name() == "reddit" {
			return errors.New("HTTP 403")
		}
		return nil
	})
	byName := map[string]feedDoctorRow{}
	for _, row := range rows {
		byName[row.Name] = row
	}
	if byName["hackernews"].ProbeStatus != "live" || !byName["hackernews"].Reachable {
		t.Fatalf("free successful probe = %+v", byName["hackernews"])
	}
	if byName["reddit"].ProbeStatus != "failed" || byName["reddit"].Reachable {
		t.Fatalf("failed probe = %+v", byName["reddit"])
	}
	if byName["serper"].ProbeStatus != "unprobed" || !byName["serper"].Configured {
		t.Fatalf("configured paid feed = %+v", byName["serper"])
	}
	if byName["brave"].ProbeStatus != "unconfigured" || byName["brave"].Configured {
		t.Fatalf("unconfigured feed = %+v", byName["brave"])
	}
}

func TestFeedDoctorDistinguishesRateLimitFromOutage(t *testing.T) {
	feeds := []gtm.Feed{doctorTestFeed{name: "reddit", tier: gtm.TierFree, available: true}}
	rows := probeFeedDoctorRows(context.Background(), feeds, false, func(context.Context, gtm.Feed, gtm.FeedQuery) error {
		return &gtm.FeedRateLimitError{Feed: "reddit", RetryAfter: 58 * time.Second}
	})
	if len(rows) != 1 || rows[0].ProbeStatus != "rate_limited" || !rows[0].Reachable || rows[0].RetryAfterSeconds != 58 {
		t.Fatalf("rate-limited probe = %+v", rows)
	}
}

func TestFeedDoctorRateLimitedSearXNGDoesNotClaimLiveGrounding(t *testing.T) {
	status := feedDoctorGrounding([]feedDoctorRow{{
		Name: "searxng", Tier: string(gtm.TierFree), Configured: true,
		ProbeStatus: "rate_limited", Reachable: true, SerpClass: true,
	}})
	if status.LiveSERP || len(status.Sources) != 0 {
		t.Fatalf("rate-limited SearXNG claimed live grounding: %+v", status)
	}
}

func TestDispatch_LeadingJSONFeeds(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"--json", "feeds"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("leading --json feeds rc=%d err=%s", code, errOut.String())
	}
	var payload struct {
		Feeds []map[string]any `json:"feeds"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("leading --json feeds is not JSON: %v\n%s", err, out.String())
	}
	if len(payload.Feeds) == 0 {
		t.Fatal("feeds list is empty")
	}
}

func TestDispatch_SEOMeasureOfflineRequiresFixture(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"--offline", "seo", "measure", "nicos-gtm"}, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "--fixture") {
		t.Fatalf("offline seo measure without --fixture must fail closed, rc=%d err=%s out=%s", code, errOut.String(), out.String())
	}
}

func TestDispatch_LeadingOfflineRejectedOnFeeds(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"--offline", "feeds"}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "--offline") {
		t.Fatalf("leading --offline feeds should exit 2, got %d err=%s", code, errOut.String())
	}
}

func TestDispatch_LeadingOfflineEconomics(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"--offline", "economics", "ngtm-10", "--json", "--acv", "30000", "--cac", "9000"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("leading --offline economics rc=%d err=%s", code, errOut.String())
	}
	var payload struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("offline economics is not JSON: %v\n%s", err, out.String())
	}
	if payload.Provider != "offline" {
		t.Fatalf("provider=%q, want offline", payload.Provider)
	}
}

func TestSocialEvalGoldenGate(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"social", "eval", "--strict", "--json"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), `"passed": true`) || !strings.Contains(out.String(), `"stable": true`) {
		t.Fatalf("social eval failed (%d): %s%s", code, out.String(), errOut.String())
	}
}

// seed state where Infisical's embedded H1 claim is currently "confirmed", so an
// offline run (which can confirm nothing) produces a confirmed→unverified
// regression.
const seedState = `{"generated":"2026-06-01T00:00:00Z","subjects":["Infisical"],` +
	`"verdicts":{"infisical":{"H1 \"Secure Secrets, Certificates, and AI Agents\"":{"status":"confirmed"}}}}`

func writeSeed(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(seedState), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDispatch_WatchRegressionExits3(t *testing.T) {
	state := writeSeed(t)
	var out, errb bytes.Buffer
	code := Dispatch("ngtm", []string{
		"business", "--compare", "Infisical", "--offline", "--watch", state, "--fail-on", "regression",
	}, &out, &errb)
	if code != 3 {
		t.Fatalf("expected exit 3 on regression, got %d\nout:%s\nerr:%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "regression") {
		t.Errorf("drift report should mention a regression:\n%s", out.String())
	}
}

func TestDispatch_WatchFailOnNoneExits0(t *testing.T) {
	state := writeSeed(t)
	var out, errb bytes.Buffer
	code := Dispatch("ngtm", []string{
		"business", "--compare", "Infisical", "--offline", "--watch", state, "--fail-on", "none",
	}, &out, &errb)
	if code != 0 {
		t.Fatalf("--fail-on none should exit 0 despite drift, got %d\nerr:%s", code, errb.String())
	}
}

func TestDispatch_VerticalWatchRoutes(t *testing.T) {
	// `seo <subject> --watch` must route to the single-vertical drift path (not
	// compare mode), produce a drift report, write the state, and exit 0 on a
	// first run.
	state := filepath.Join(t.TempDir(), "seo-state.json")
	var out, errb bytes.Buffer
	code := Dispatch("ngtm", []string{"seo", "nvault", "--offline", "--watch", state}, &out, &errb)
	if code != 0 {
		t.Fatalf("first vertical watch run should exit 0, got %d err:%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "GTM drift") {
		t.Errorf("expected a drift report, got: %s", out.String())
	}
	if _, err := os.Stat(state); err != nil {
		t.Errorf("vertical watch did not write the state file: %v", err)
	}
}

func TestDispatch_WatchBadFailOn(t *testing.T) {
	state := writeSeed(t)
	var out, errb bytes.Buffer
	code := Dispatch("ngtm", []string{
		"business", "--compare", "Infisical", "--offline", "--watch", state, "--fail-on", "bogus",
	}, &out, &errb)
	if code != 2 {
		t.Fatalf("unknown --fail-on should exit 2, got %d", code)
	}
}
