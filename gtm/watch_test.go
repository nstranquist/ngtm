package gtm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func state(gen string, v map[string]map[string]ClaimVerdict, subjects ...string) *WatchState {
	return &WatchState{Generated: gen, Subjects: subjects, Verdicts: v}
}

func TestDiffStates_Flips(t *testing.T) {
	prev := state("2026-06-01T00:00:00Z", map[string]map[string]ClaimVerdict{
		"infisical": {
			`H1 "Secure Secrets"`: {Status: "confirmed", Source: "https://infisical.com"},
			`Pricing $18`:         {Status: "confirmed"},
		},
	}, "Infisical")
	cur := state("2026-06-02T00:00:00Z", map[string]map[string]ClaimVerdict{
		"infisical": {
			`H1 "Secure Secrets"`: {Status: "confirmed", Source: "https://infisical.com"}, // unchanged
			`Pricing $18`:         {Status: "contradicted"},                               // flipped
			`New stat 500M+`:      {Status: "unverified"},                                 // new
		},
	}, "Infisical")

	dr := DiffStates(prev, cur)
	if dr.Since != "2026-06-01T00:00:00Z" {
		t.Errorf("since = %q", dr.Since)
	}
	if len(dr.Flips) != 1 || dr.Flips[0].From != "confirmed" || dr.Flips[0].To != "contradicted" {
		t.Fatalf("expected 1 confirmed→contradicted flip, got %+v", dr.Flips)
	}
	if len(dr.New) != 1 || dr.New[0].To != "unverified" {
		t.Errorf("expected 1 new claim, got %+v", dr.New)
	}
	if dr.Stable != 1 {
		t.Errorf("expected 1 stable, got %d", dr.Stable)
	}
	if !dr.HasDrift() {
		t.Errorf("expected drift")
	}
}

func TestDiffStates_FirstRunAllNew(t *testing.T) {
	prev := &WatchState{Verdicts: map[string]map[string]ClaimVerdict{}}
	cur := state("now", map[string]map[string]ClaimVerdict{
		"doppler": {`H1 "Prevent breaches"`: {Status: "confirmed"}},
	}, "Doppler")
	dr := DiffStates(prev, cur)
	if len(dr.New) != 1 || dr.Stable != 0 || len(dr.Flips) != 0 {
		t.Fatalf("first run should be all-new: %+v", dr)
	}
}

func TestDiffStates_NoDriftWhenIdentical(t *testing.T) {
	v := map[string]map[string]ClaimVerdict{"akeyless": {`H1 "Runtime"`: {Status: "confirmed"}}}
	prev := state("t1", v, "Akeyless")
	cur := state("t2", v, "Akeyless")
	dr := DiffStates(prev, cur)
	if dr.HasDrift() || dr.Stable != 1 {
		t.Fatalf("identical states should not drift: %+v", dr)
	}
}

func TestDiffStates_Removed(t *testing.T) {
	prev := state("t1", map[string]map[string]ClaimVerdict{"x": {"a": {Status: "confirmed"}, "b": {Status: "unverified"}}}, "X")
	cur := state("t2", map[string]map[string]ClaimVerdict{"x": {"a": {Status: "confirmed"}}}, "X")
	dr := DiffStates(prev, cur)
	if len(dr.Removed) != 1 || dr.Removed[0].Claim != "b" {
		t.Fatalf("expected 1 removed claim, got %+v", dr.Removed)
	}
}

func TestWatchState_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	// First load of a missing file is the empty first run, not an error.
	ws, err := LoadWatchState(path)
	if err != nil || len(ws.Verdicts) != 0 {
		t.Fatalf("missing state should load empty: %v %+v", err, ws)
	}
	ws = state("t", map[string]map[string]ClaimVerdict{"x": {"a": {Status: "confirmed", Source: "https://x"}}}, "X")
	if err := SaveWatchState(path, ws); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWatchState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdicts["x"]["a"].Status != "confirmed" || got.Verdicts["x"]["a"].Source != "https://x" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestDiffStates_RegressionCount(t *testing.T) {
	prev := state("t1", map[string]map[string]ClaimVerdict{"x": {
		"a": {Status: "confirmed"}, "b": {Status: "confirmed"}, "c": {Status: "unverified"},
	}}, "X")
	cur := state("t2", map[string]map[string]ClaimVerdict{"x": {
		"a": {Status: "unverified"}, // confirmed → unverified = regression
		"b": {Status: "confirmed"},  // stable
		"c": {Status: "confirmed"},  // unverified → confirmed = improvement (NOT a regression)
	}}, "X")
	dr := DiffStates(prev, cur)
	if dr.Regressions != 1 {
		t.Fatalf("expected 1 regression, got %d (flips %+v)", dr.Regressions, dr.Flips)
	}
	if dr.Stable != 1 {
		t.Errorf("expected 1 stable, got %d", dr.Stable)
	}
	if !strings.Contains(dr.Markdown(), "1 regression(s)") {
		t.Errorf("markdown should surface the regression count:\n%s", dr.Markdown())
	}
}

func TestPostDriftWebhook(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dr := &DriftReport{Since: "t", Regressions: 1, Flips: []DriftEntry{
		{Subject: "Infisical", Claim: "H1", From: "confirmed", To: "unverified"},
	}}
	if err := PostDriftWebhook(context.Background(), srv.URL, dr); err != nil {
		t.Fatalf("PostDriftWebhook: %v", err)
	}
	if !strings.Contains(got["text"], "regression") {
		t.Errorf("Slack payload missing the drift text: %+v", got)
	}
}

func TestPostDriftWebhook_EmptyIsNoop(t *testing.T) {
	if err := PostDriftWebhook(context.Background(), "", &DriftReport{}); err != nil {
		t.Errorf("empty webhook URL should be a no-op, got %v", err)
	}
}

func TestParseMetricValue(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"8100", 8100, true},
		{"$12.0B", 12e9, true},
		{"2,500", 2500, true},
		{"2500, 8000", 2500, true}, // range → first
		{"1", 1, true},
		{"$45.0M", 45e6, true},
		{"financial services", 0, false},
	}
	for _, c := range cases {
		got, ok := parseMetricValue(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseMetricValue(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestStateFromVertical(t *testing.T) {
	rep := &Report{
		Generated: "t", Subject: "nvault",
		Evidence: []Evidence{
			{ID: "serper:0", Metric: "serp_rank", Value: "1", URL: "https://infisical.com", Title: "Infisical"},
			{ID: "dataforseo:0", Metric: "search_volume", Value: "8100", Title: "secrets manager"},
		},
		Sections: []Section{{Title: "SERP Reality", Claims: []Claim{
			{Text: "Rank #1: Infisical (infisical.com)", Confidence: ConfGrounded, Citations: []string{"serper:0"}},
		}}},
	}
	ws := StateFromVertical(rep)
	if v := ws.Verdicts["nvault"]["Rank #1: Infisical (infisical.com)"]; v.Status != "grounded" || v.Source != "https://infisical.com" {
		t.Fatalf("vertical verdict not captured: %+v", ws.Verdicts)
	}
	keys := map[string]float64{}
	for _, s := range ws.Metrics["nvault"] {
		keys[s.Key] = s.Value
	}
	if keys["serp_rank:infisical.com"] != 1 || keys["search_volume:secrets manager"] != 8100 {
		t.Fatalf("vertical metrics not captured: %+v", ws.Metrics["nvault"])
	}
}

func TestDiffStates_MetricDelta(t *testing.T) {
	prev := &WatchState{Generated: "t1", Subjects: []string{"nvault"}, Verdicts: map[string]map[string]ClaimVerdict{},
		Metrics: map[string][]MetricSample{"nvault": {
			{Key: "serp_rank:x.com", Name: "serp_rank", Value: 1},
			{Key: "search_volume:k", Name: "search_volume", Value: 8100},
			{Key: "mentions:count", Name: "mentions", Value: 5},
		}}}
	cur := &WatchState{Generated: "t2", Subjects: []string{"nvault"}, Verdicts: map[string]map[string]ClaimVerdict{},
		Metrics: map[string][]MetricSample{"nvault": {
			{Key: "serp_rank:x.com", Name: "serp_rank", Value: 5},        // dropped (1→5) = adverse
			{Key: "search_volume:k", Name: "search_volume", Value: 9000}, // grew = not adverse
			{Key: "mentions:count", Name: "mentions", Value: 2},          // fell = adverse
		}}}
	dr := DiffStates(prev, cur)
	if len(dr.Metrics) != 3 {
		t.Fatalf("expected 3 metric deltas, got %+v", dr.Metrics)
	}
	if dr.Regressions != 2 { // rank-drop + mentions-fall (volume growth is not adverse)
		t.Fatalf("expected 2 adverse metric regressions, got %d: %+v", dr.Regressions, dr.Metrics)
	}
}

func TestDiffStates_GroundedRegression(t *testing.T) {
	prev := state("t1", map[string]map[string]ClaimVerdict{"nvault": {"c": {Status: "grounded"}}}, "nvault")
	cur := state("t2", map[string]map[string]ClaimVerdict{"nvault": {"c": {Status: "inferred"}}}, "nvault")
	dr := DiffStates(prev, cur)
	if dr.Regressions != 1 {
		t.Fatalf("grounded→inferred should be a regression: %+v", dr)
	}
}

func TestStateFromReport(t *testing.T) {
	rep := &CompareReport{
		Generated: "t",
		Subjects:  []string{"Infisical"},
		Rows: []CompareRow{{
			Subject:     "Infisical",
			Evidence:    []Evidence{{ID: "landing:h1", URL: "https://infisical.com"}},
			ClaimChecks: []ClaimCheck{{Text: `H1 "X"`, Status: StatusConfirmed, Citations: []string{"landing:h1"}}},
		}},
	}
	ws := StateFromReport(rep)
	v := ws.Verdicts["infisical"][`H1 "X"`]
	if v.Status != "confirmed" || v.Source != "https://infisical.com" {
		t.Fatalf("StateFromReport lost verdict/source: %+v", ws.Verdicts)
	}
}
