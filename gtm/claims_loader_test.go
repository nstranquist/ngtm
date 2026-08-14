package gtm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const corpusFixture = `## nvault — secrets
- **Competitor teardown:**
  - **Infisical** — H1 "Secure Secrets, Certificates, and AI Agents"; "500M+ secrets daily"; **Gap: abandoned zero-knowledge**
  - **Doppler** — H1 "Secure secrets. Prevent breaches."; "76k+ orgs"; pricing ~$21/user/mo
  - **Akeyless** — "Runtime Identity Security at Agentic Scale"; enterprise/sales-led, expensive
`

func TestParseClaimsMarkdown(t *testing.T) {
	cs := parseClaimsMarkdown(corpusFixture)
	claims := cs.Claims

	// Subjects auto-derived in document order.
	if len(cs.Subjects) != 3 || cs.Subjects[0] != "Infisical" {
		t.Fatalf("expected ordered subjects [Infisical Doppler Akeyless], got %v", cs.Subjects)
	}

	inf := claims["infisical"]
	if len(inf) < 3 {
		t.Fatalf("expected 3 Infisical claims, got %+v", inf)
	}
	if !hasClaim(inf, "serp", "Secure Secrets, Certificates, and AI Agents") {
		t.Errorf("missing inferred H1/serp claim: %+v", inf)
	}
	if !hasClaim(inf, "stat", "500M+") {
		t.Errorf("missing inferred stat claim: %+v", inf)
	}

	dop := claims["doppler"]
	if !hasClaim(dop, "pricing", "21") {
		t.Errorf("missing inferred pricing claim for Doppler: %+v", dop)
	}
	if !hasClaim(dop, "stat", "76k+") {
		t.Errorf("missing inferred 76k stat for Doppler: %+v", dop)
	}

	ak := claims["akeyless"]
	if !hasKind(ak, "serp") {
		t.Errorf("Akeyless quoted headline should infer serp: %+v", ak)
	}
}

func TestLoadClaimsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claims.yaml")
	yaml := `Acme:
  - text: H1 "Acme — the best"
    kind: serp
    needle: the best
  - text: Plans from $9 / mo
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, err := LoadClaims(path)
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	acme := cs.Claims["acme"]
	if len(acme) != 2 {
		t.Fatalf("expected 2 Acme claims, got %+v", acme)
	}
	if acme[0].Kind != "serp" || acme[0].Needle != "the best" {
		t.Errorf("explicit yaml fields not preserved: %+v", acme[0])
	}
	// second claim has no kind → inferred pricing.
	if !hasClaim(acme, "pricing", "9") {
		t.Errorf("expected inferred pricing claim: %+v", acme)
	}
}

func TestLoadClaims_Markdown_RoundTripsThroughCompare(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.md")
	if err := os.WriteFile(path, []byte(corpusFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, err := LoadClaims(path)
	if err != nil {
		t.Fatalf("LoadClaims md: %v", err)
	}

	// External claims REPLACE the embedded set: a competitor present in the
	// override gets checks; one only in the embedded default does not.
	override := map[string][]CorpusClaim{"acme": {{Text: "x", Kind: "narrative"}}}
	eng := NewEngineWith(&FeedRegistry{now: fixedNow}, offlineGenerator{}, fixedNow)
	rep, err := eng.Compare(context.Background(), []string{"Acme", "Infisical"}, Options{NoFeeds: true, Claims: override})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if r := rowBySubject(rep, "Acme"); r == nil || len(r.ClaimChecks) == 0 {
		t.Errorf("Acme should carry override claims")
	}
	if r := rowBySubject(rep, "Infisical"); r == nil || len(r.ClaimChecks) != 0 {
		t.Errorf("Infisical embedded claims must be replaced by the override: %+v", r)
	}

	// And the parsed markdown is usable as an override too.
	rep2, _ := eng.Compare(context.Background(), []string{"Infisical"}, Options{NoFeeds: true, Claims: cs.Claims})
	if r := rowBySubject(rep2, "Infisical"); r == nil || len(r.ClaimChecks) < 3 {
		t.Errorf("markdown-loaded Infisical claims not applied: %+v", r)
	}
}

func hasClaim(cs []CorpusClaim, kind, needle string) bool {
	for _, c := range cs {
		if c.Kind == kind && c.Needle == needle {
			return true
		}
	}
	return false
}

func hasKind(cs []CorpusClaim, kind string) bool {
	for _, c := range cs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}
