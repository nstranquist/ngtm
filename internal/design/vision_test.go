package design

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestScorePNG_RequestAndParse exercises the full vision request build + response
// parse against a stub Anthropic endpoint (via ANTHROPIC_BASE_URL) — no real key
// or network. It asserts the wire shape the Messages API expects (image block,
// headers, output_config.format) and that the JSON verdict parses + clamps.
func TestScorePNG_RequestAndParse(t *testing.T) {
	var gotAuth, gotVersion, gotCT string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotCT = r.Header.Get("content-type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		// Anthropic-shaped response; content[0].text is the forced-JSON verdict.
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"{\"score\":12,\"hierarchy\":7.5,\"polish\":8,\"harmony\":9,\"reasons\":[\"strong contrast\",\"even ramp\"]}"}],"usage":{"input_tokens":1500,"output_tokens":80}}`)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL+"/v1")

	vs, err := ScorePNG(context.Background(), []byte("\x89PNGfake"), VisionOptions{})
	if err != nil {
		t.Fatalf("ScorePNG: %v", err)
	}

	// headers
	if gotAuth != "test-key" {
		t.Errorf("x-api-key = %q", gotAuth)
	}
	if gotVersion != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, anthropicVersion)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}

	// request shape: model, an image block, and output_config.format
	if body["model"] != "claude-haiku-4-5" {
		t.Errorf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("no messages in request")
	}
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	var sawImage bool
	for _, b := range content {
		if m, ok := b.(map[string]any); ok && m["type"] == "image" {
			src, _ := m["source"].(map[string]any)
			if src["type"] == "base64" && src["media_type"] == "image/png" && src["data"] != "" {
				sawImage = true
			}
		}
	}
	if !sawImage {
		t.Errorf("request missing a base64 image/png block")
	}
	if oc, ok := body["output_config"].(map[string]any); !ok {
		t.Errorf("request missing output_config")
	} else if f, _ := oc["format"].(map[string]any); f["type"] != "json_schema" {
		t.Errorf("output_config.format.type = %v, want json_schema", f["type"])
	}

	// parse + clamp (score 12 → 10)
	if vs.Score != 10 {
		t.Errorf("score = %.1f, want clamped 10", vs.Score)
	}
	if vs.Hierarchy != 7.5 || vs.Polish != 8 || vs.Harmony != 9 {
		t.Errorf("sub-scores = h%.1f p%.1f c%.1f", vs.Hierarchy, vs.Polish, vs.Harmony)
	}
	if len(vs.Reasons) != 2 || vs.Model != "claude-haiku-4-5" {
		t.Errorf("reasons=%v model=%q", vs.Reasons, vs.Model)
	}
}

func TestBlend(t *testing.T) {
	// 0.7*9.8 + 0.3*7.0 = 6.86 + 2.1 = 8.96
	if got := Blend(9.8, 7.0); got != 8.96 {
		t.Errorf("Blend = %.2f, want 8.96", got)
	}
}
