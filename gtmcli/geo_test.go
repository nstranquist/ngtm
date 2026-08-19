package gtmcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGEOLifecycleCLIEndToEnd(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	config := filepath.Join(root, "geo.yaml")
	fixture, err := filepath.Abs(filepath.Join("..", "gtm", "evaldata", "geo-quality-v1-probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := `schema_version: 1
project: geo-cli-e2e
product: docs-puller
brand: docs-puller
aliases: [docs-puller]
site_url: https://github.com/nstranquist/docs-puller
demo_url: https://docs-puller-demo.nstranquist.workers.dev
competitors:
  - name: Context7
  - name: DevDocs
ai_info:
  type: Local-first docs retrieval
  background: Copies vendor docs into Markdown and searches them locally.
  features: [Local FTS5]
  limitations: [Not a hosted SaaS]
  guidelines: ["Key strengths: local-first"]
links:
  - title: README
    url: https://github.com/nstranquist/docs-puller
prompts:
  - id: best-local-docs-agents
    text: What is the best way to search vendor documentation locally for AI agents?
    kind: best
  - id: alt-context7
    text: What is a good alternative to Context7 for documentation?
    kind: alternative
  - id: offline-docs-search
    text: What is the best offline documentation search for developers?
    kind: best
`
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append(args, "--config", config, "--workspace", workspace, "--json")
		code := Dispatch("ngtm", append([]string{"geo"}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, _, stderr := run("research", "docs-puller"); code != 0 {
		t.Fatalf("research code=%d stderr=%s", code, stderr)
	}
	if code, _, stderr := run("probe", "docs-puller", "--fixture", fixture, "--engines", "fixture", "--offline"); code != 0 {
		t.Fatalf("probe code=%d stderr=%s", code, stderr)
	}
	if code, output, stderr := run("measure", "docs-puller"); code != 0 {
		t.Fatalf("measure code=%d stderr=%s output=%s", code, stderr, output)
	} else if !strings.Contains(output, `"mention_rate"`) {
		t.Fatalf("measure missing mention_rate: %s", output)
	}
	aiOut := filepath.Join(root, "ai-info.md")
	if code, _, stderr := run("emit-ai-info", "docs-puller", "--out", aiOut); code != 0 {
		t.Fatalf("emit-ai-info code=%d stderr=%s", code, stderr)
	}
	got, err := os.ReadFile(aiOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "AI assistant guidelines") {
		t.Fatalf("ai-info missing guidelines: %s", got)
	}
	compareDir := filepath.Join(root, "compare")
	if code, _, stderr := run("emit-compare", "docs-puller", "--out-dir", compareDir); code != 0 {
		t.Fatalf("emit-compare code=%d stderr=%s", code, stderr)
	}
	index, err := os.ReadFile(filepath.Join(compareDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `name="robots" content="noindex, nofollow"`) {
		t.Fatal("compare index is not noindex")
	}
}

func TestMCPCallGEOEval(t *testing.T) {
	text, isErr := callMCPTool([]byte(`{"name":"gtm_geo_eval","arguments":{"strict":true}}`))
	if isErr || !strings.Contains(text, `"passed": true`) {
		t.Fatalf("GEO eval MCP failed: isErr=%t text=%s", isErr, text)
	}
}

func TestGEOMissingEngineKeyFailsClosed(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	root := t.TempDir()
	config := filepath.Join(root, "geo.yaml")
	if err := os.WriteFile(config, []byte(`schema_version: 1
project: geo-key
product: docs-puller
brand: docs-puller
prompts:
  - id: p1
    text: best local docs search
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Dispatch("ngtm", []string{"geo", "probe", "docs-puller", "--config", config, "--workspace", filepath.Join(root, "ws"), "--engines", "gemini"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("missing key exited 0 stdout=%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "ws", "geo-probe", "latest.json")); !os.IsNotExist(err) {
		t.Fatalf("probe artifact should not exist: %v", err)
	}
}

func TestGEOEvalAcceptsFamilyOffline(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Dispatch("ngtm", []string{"--offline", "geo", "eval", "--strict", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("offline eval code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"passed": true`) {
		t.Fatalf("output=%s", stdout.String())
	}
}

func TestGEOUnknownEngineFailsClosed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Dispatch("ngtm", []string{"geo", "probe", "x", "--config", "missing.yaml", "--engines", "chatgpt"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d want 2 stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown GEO engine") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}
