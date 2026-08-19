package gtmcli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCP_Initialize(t *testing.T) {
	resp, respond := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"})
	if !respond {
		t.Fatal("initialize must respond")
	}
	m, ok := resp.Result.(map[string]any)
	if !ok || m["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("bad initialize result: %+v", resp.Result)
	}
}

func TestMCP_NotificationNoResponse(t *testing.T) {
	_, respond := handleRPC(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	if respond {
		t.Fatal("notifications (no id) must not produce a response")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"})
	m := resp.Result.(map[string]any)
	tools := m["tools"].([]map[string]any)
	if len(tools) != 25 {
		t.Fatalf("expected 25 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
	}
	for _, want := range []string{"gtm_seo", "gtm_seo_research", "gtm_seo_opportunities", "gtm_seo_brief", "gtm_seo_publish", "gtm_seo_measure", "gtm_seo_retro", "gtm_seo_audit", "gtm_seo_eval", "gtm_geo_research", "gtm_geo_probe", "gtm_geo_measure", "gtm_geo_eval", "gtm_business", "gtm_brand", "gtm_economics", "gtm_pricing", "gtm_motion", "gtm_social", "gtm_ideate", "gtm_launch", "gtm_compare", "gtm_landing", "gtm_design", "gtm_feeds"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

func TestSchemaPropertiesRepairsMissingProperties(t *testing.T) {
	schema := map[string]any{"type": "object"}
	properties := schemaProperties(schema)
	properties["subject"] = map[string]any{"type": "string"}

	got, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties has type %T, want map[string]any", schema["properties"])
	}
	if _, ok := got["subject"]; !ok {
		t.Fatal("schemaProperties returned a detached map")
	}
}

func TestMCP_CallLanding(t *testing.T) {
	params := json.RawMessage(`{"name":"gtm_landing","arguments":{"subject":"cadence","product":"Cadence","headline":"Deep focus everywhere","buy_url":"https://buy.test/cadence","price":39,"one_time":true,"tier_name":"Cadence Pro"}}`)
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`9`), Method: "tools/call", Params: params})
	m := resp.Result.(map[string]any)
	if m["isError"].(bool) {
		t.Fatalf("landing call should not error: %+v", m)
	}
	text := m["content"].([]map[string]any)[0]["text"].(string)
	for _, want := range []string{"<!doctype html>", "Deep focus everywhere", "https://buy.test/cadence", "$39", "one-time", "ngtm landing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("landing HTML missing %q", want)
		}
	}
	// JSON format path returns the model, not HTML.
	jp := json.RawMessage(`{"name":"gtm_landing","arguments":{"subject":"x","product":"X","price":9,"format":"json"}}`)
	jr, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`10`), Method: "tools/call", Params: jp})
	jt := jr.Result.(map[string]any)["content"].([]map[string]any)[0]["text"].(string)
	if !strings.Contains(jt, `"product": "X"`) || strings.Contains(jt, "<!doctype") {
		t.Fatalf("json format should return the page model, got: %s", jt[:min(120, len(jt))])
	}
}

func TestMCP_CallSEOOffline(t *testing.T) {
	params := json.RawMessage(`{"name":"gtm_seo","arguments":{"subject":"nvault","offline":true}}`)
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "tools/call", Params: params})
	m := resp.Result.(map[string]any)
	if m["isError"].(bool) {
		t.Fatalf("offline seo call should not error: %+v", m)
	}
	content := m["content"].([]map[string]any)
	text := content[0]["text"].(string)
	if !strings.Contains(text, `"vertical": "seo"`) || !strings.Contains(text, `"subject": "nvault"`) {
		t.Fatalf("unexpected report payload: %s", text[:min(200, len(text))])
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "bogus"})
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected method-not-found error, got %+v", resp.Error)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestMCP_CallSocialOffline(t *testing.T) {
	params := json.RawMessage(`{"name":"gtm_social","arguments":{"subject":"nvault","offline":true,"pitch":"offline-first secrets with E2EE sync","channels":"show-hn"}}`)
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`11`), Method: "tools/call", Params: params})
	m := resp.Result.(map[string]any)
	if m["isError"].(bool) {
		t.Fatalf("offline social call should not error: %+v", m)
	}
	text := m["content"].([]map[string]any)[0]["text"].(string)
	for _, want := range []string{`"vertical": "social"`, "Show HN draft", "Distribution Calendar"} {
		if !strings.Contains(text, want) {
			t.Fatalf("social report missing %q: %s", want, text[:min(300, len(text))])
		}
	}
	// Channel subset honored: producthunt section absent.
	if strings.Contains(text, "Product Hunt draft") {
		t.Fatal("channels arg not honored — producthunt draft present")
	}
}

func TestMCP_CallLaunchBoard(t *testing.T) {
	ledger := t.TempDir() + "/ledger.jsonl"
	var out, errOut strings.Builder
	if code := Dispatch("ngtm", []string{"launch", "plan", "cadence", "--week", "2026-W24", "--ledger", ledger}, &out, &errOut); code != 0 {
		t.Fatalf("plan: %s", errOut.String())
	}
	params := json.RawMessage(`{"name":"gtm_launch","arguments":{"action":"cohort","ledger":"` + ledger + `"}}`)
	resp, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`12`), Method: "tools/call", Params: params})
	m := resp.Result.(map[string]any)
	if m["isError"].(bool) {
		t.Fatalf("launch board call errored: %+v", m)
	}
	text := m["content"].([]map[string]any)[0]["text"].(string)
	if !strings.Contains(text, `"week": "2026-W24"`) || !strings.Contains(text, `"cadence"`) {
		t.Fatalf("board payload wrong: %s", text)
	}
	// show requires product.
	bad := json.RawMessage(`{"name":"gtm_launch","arguments":{"action":"show","ledger":"` + ledger + `"}}`)
	br, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`13`), Method: "tools/call", Params: bad})
	if !br.Result.(map[string]any)["isError"].(bool) {
		t.Fatal("show without product must error")
	}
}
