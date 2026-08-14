package gtmcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSEOLifecycleCLIEndToEnd(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	config := filepath.Join(root, "seo.yaml")
	researchFixture, err := filepath.Abs(filepath.Join("..", "gtm", "evaldata", "seo-quality-v2-research.json"))
	if err != nil {
		t.Fatal(err)
	}
	measurementFixture, err := filepath.Abs(filepath.Join("..", "gtm", "evaldata", "seo-quality-v2-measurement.json"))
	if err != nil {
		t.Fatal(err)
	}
	configBody := `schema_version: 2
project: seo-cli-e2e
product: SEO CLI E2E
domain: example.com
seed_keywords: [seo automation pipeline]
locales:
  - name: us-en-desktop
    language_code: en
    location_code: 2840
    device: desktop
requirements:
  require_serp: true
  require_volume: true
  minimum_coverage: 0.8
scoring: {demand: 0.2, attainability: 0.15, intent: 0.1, trend: 0.1, business_relevance: 0.2, content_gap: 0.15, first_party: 0.1}
providers: {tier: free, max_keywords: 20}
first_party: {lookback_days: 28}
publishing:
  canonical_base_url: https://example.com
  minimum_word_count: 100
  minimum_unique_value_chars: 80
  require_sitemap: true
  require_structured_data: true
`
	if err := os.WriteFile(config, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append(args, "--config", config, "--workspace", workspace, "--json")
		code := Dispatch("ngtm", append([]string{"seo"}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, _, stderr := run("research", "SEO CLI E2E", "--fixture", researchFixture, "--strict"); code != 0 {
		t.Fatalf("research code=%d stderr=%s", code, stderr)
	}
	unique := "A reproducible original benchmark connects scoped evidence, guarded local publishing, observed outcomes, and retro decisions for SEO operators."
	if code, _, stderr := run("brief", "SEO CLI E2E", "--keyword", "seo automation pipeline", "--unique-value", unique, "--strict"); code != 0 {
		t.Fatalf("brief code=%d stderr=%s", code, stderr)
	}
	body := strings.Repeat("Useful original evidence explains implementation details, tradeoffs, workflow, and the operator's next action. ", 20)
	if code, output, stderr := run("publish", "SEO CLI E2E", "--body", body, "--approved", "--index", "--strict"); code != 0 {
		t.Fatalf("publish code=%d stderr=%s output=%s", code, stderr, output)
	}
	if code, _, stderr := run("measure", "SEO CLI E2E", "--fixture", measurementFixture, "--strict"); code != 0 {
		t.Fatalf("measure code=%d stderr=%s", code, stderr)
	}
	if code, output, stderr := run("retro", "SEO CLI E2E", "--strict"); code != 0 || !strings.Contains(output, `"decision": "double-down"`) {
		t.Fatalf("retro code=%d stderr=%s output=%s", code, stderr, output)
	}
	if code, output, stderr := run("audit", "SEO CLI E2E", "--strict"); code != 0 || !strings.Contains(output, `"passed": true`) {
		t.Fatalf("audit code=%d stderr=%s output=%s", code, stderr, output)
	}
}

func TestLegacySEOStrictFailsClosedWithoutLiveEvidence(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Dispatch("ngtm", []string{"seo", "fixture-product", "--offline", "--strict", "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("strict offline SEO code=%d want 3; stderr=%s output=%s", code, stderr.String(), stdout.String())
	}
	for _, want := range []string{`"seo_serp_coverage": 0`, `"seo_volume_coverage": 0`, "strict SEO evidence gate failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("strict output missing %q: %s", want, stdout.String())
		}
	}
}

func TestLegacySEOPagesAreNoindex(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Dispatch("ngtm", []string{"seo", "Product", "--pages", "--keywords", "useful keyword", "--out-dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pages code=%d stderr=%s", code, stderr.String())
	}
	for _, name := range []string{"useful-keyword.html", "index.html"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `name="robots" content="noindex, nofollow"`) {
			t.Fatalf("%s is not noindex", name)
		}
	}
}

func TestMCPCallSEOEval(t *testing.T) {
	text, isErr := callMCPTool([]byte(`{"name":"gtm_seo_eval","arguments":{"strict":true}}`))
	if isErr || !strings.Contains(text, `"passed": true`) {
		t.Fatalf("SEO eval MCP failed: isErr=%t text=%s", isErr, text)
	}
}
