package gtmcli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDesignCLI_JSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"design", "garrid", "--seed", "#3b82f6", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("design exited %d: %s", code, errOut.String())
	}
	var payload struct {
		Name      string `json:"name"`
		Scorecard struct {
			Overall    float64 `json:"overall"`
			Dimensions []struct {
				Name  string  `json:"name"`
				Score float64 `json:"score"`
			} `json:"dimensions"`
			Contrast []struct {
				Label string  `json:"label"`
				Ratio float64 `json:"ratio"`
				Pass  bool    `json:"pass"`
			} `json:"contrast"`
		} `json:"scorecard"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if payload.Name != "garrid" {
		t.Errorf("name = %q, want garrid", payload.Name)
	}
	if payload.Scorecard.Overall < 8.5 {
		t.Errorf("overall %.2f unexpectedly low", payload.Scorecard.Overall)
	}
	// the four critical text pairs must pass AA
	crit := map[string]bool{"body text on bg": true, "muted text on surface": true, "primary button label": true}
	for _, c := range payload.Scorecard.Contrast {
		if crit[c.Label] && !c.Pass {
			t.Errorf("critical pair %q failed: %.2f:1", c.Label, c.Ratio)
		}
	}
}

func TestDesignCLI_HTMLDefault(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"design", "nvault", "--mode", "dark"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("design exited %d: %s", code, errOut.String())
	}
	html := out.String()
	if !strings.Contains(html, "<!doctype html>") || !strings.Contains(html, ":root {") {
		t.Errorf("expected self-contained HTML with token block, got %d bytes", len(html))
	}
	if !strings.Contains(errOut.String(), "overall:") {
		t.Errorf("expected scorecard on stderr")
	}
}

func TestMCP_CallDesign(t *testing.T) {
	params := json.RawMessage(`{"name":"gtm_design","arguments":{"subject":"garrid","seed":"#3b82f6","harmony":"complementary"}}`)
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`5`), Method: "tools/call", Params: params})
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", resp.Result)
	}
	content, _ := m["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatalf("no content in response: %+v", m)
	}
	text, _ := content[0]["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("design MCP did not return JSON: %v\n%s", err, text)
	}
	if _, ok := payload["scorecard"]; !ok {
		t.Errorf("missing scorecard in MCP payload")
	}
	if _, ok := payload["css"]; !ok {
		t.Errorf("missing css token block in MCP payload")
	}
}
