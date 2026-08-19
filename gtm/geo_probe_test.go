package gtm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactGEOSecret(t *testing.T) {
	got := redactGEOSecret("gemini: Get \"https://example/?key=sekrit\": timeout", "sekrit")
	if strings.Contains(got, "sekrit") {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("missing redaction: %s", got)
	}
}

func TestRunGEOProbeMissingKeyFailsClosed(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	_, err := RunGEOProbe(context.Background(), GEOProbeOptions{
		Config:  testGEOConfig(),
		Engines: []GEOEngineID{GEOEngineGemini},
	})
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunGEOProbeFixtureDoesNotRelabelLiveEngine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fix.json")
	body := `{
  "schema_version": 1,
  "answers": [
    {"prompt_id":"p1","engine":"fixture","model":"fixture","text":"docs-puller is the best option."}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := RunGEOProbe(context.Background(), GEOProbeOptions{
		Config:      testGEOConfig(),
		Engines:     []GEOEngineID{GEOEngineOpenAIChat},
		FixturePath: path,
		Now:         func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("rows=%d", len(report.Rows))
	}
	row := report.Rows[0]
	if row.Error == "" || row.Engine == GEOEngineOpenAIChat && row.Error == "" {
		t.Fatalf("expected missing fixture row, got %+v", row)
	}
	if row.Error != "" && strings.Contains(row.Error, "openai-chat") && row.Mentioned {
		t.Fatalf("canned text labeled as live: %+v", row)
	}
	if row.Engine == GEOEngineOpenAIChat && row.Mentioned {
		t.Fatalf("fixture answer scored as openai-chat: %+v", row)
	}
}

func TestRunGEOProbeRedactsAskErrors(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "super-secret-gemini-key")
	report, err := RunGEOProbe(context.Background(), GEOProbeOptions{
		Config:  testGEOConfig(),
		Engines: []GEOEngineID{GEOEngineGemini},
		Ask: func(context.Context, GEOEngineSpec, string) (GEORawAnswer, error) {
			return GEORawAnswer{}, errors.New(`Get "https://generativelanguage.googleapis.com/v1beta/models/x:generateContent?key=super-secret-gemini-key": timeout`)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "super-secret-gemini-key") {
		t.Fatalf("secret in report JSON: %s", raw)
	}
	if !strings.Contains(report.Rows[0].Error, "[redacted]") {
		t.Fatalf("error=%q", report.Rows[0].Error)
	}
}

func TestOpenAISearchOmitsTemperature(t *testing.T) {
	var saw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&saw); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "gpt-4o-search-preview",
			"choices": []map[string]any{{"message": map[string]string{"content": "Use Dash."}}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	old := geoHTTPClient
	geoHTTPClient = srv.Client()
	t.Cleanup(func() { geoHTTPClient = old })

	// Rewrite askOpenAICompatible URL by calling through a custom transport.
	// askOpenAICompatible hardcodes api.openai.com, so exercise the payload
	// helper by posting to the test server via a patched client + host rewrite.
	geoHTTPClient = &http.Client{Transport: roundTripRewrite{base: srv.URL, next: http.DefaultTransport}}
	_, err := askOpenAICompatible(context.Background(), geoEngineRegistry[GEOEngineOpenAISearch], "best docs search")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saw["temperature"]; ok {
		t.Fatalf("search payload has temperature: %#v", saw)
	}
}

type roundTripRewrite struct {
	base string
	next http.RoundTripper
}

func (r roundTripRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := req.URL.Parse(r.base)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.Host = u.Host
	return r.next.RoundTrip(req)
}
