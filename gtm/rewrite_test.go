package gtm

import (
	"context"
	"strings"
	"testing"
)

func TestAnnotateCorpus_InlineVerdicts(t *testing.T) {
	srv := stubLandingServer()
	defer srv.Close()
	t.Setenv("SCRAPE_API_URL", srv.URL)
	t.Setenv("SCRAPE_API_KEY", "test")

	reg := &FeedRegistry{now: fixedNow}
	reg.Register(&landingFeed{now: fixedNow})
	eng := NewEngineWith(reg, offlineGenerator{}, fixedNow)

	// Auto-derive the subject set from the corpus, verify, then annotate it.
	cs := parseClaimsMarkdown(corpusFixture)
	if len(cs.Subjects) == 0 {
		t.Fatal("no subjects derived from corpus")
	}
	rep, err := eng.Compare(context.Background(), cs.Subjects, Options{Tiers: []FeedTier{TierCheap}, Claims: cs.Claims})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	out := AnnotateCorpus(corpusFixture, rep)

	// Original corpus content is preserved.
	if !strings.Contains(out, "**Infisical**") || !strings.Contains(out, "## nvault — secrets") {
		t.Errorf("annotated output dropped original corpus content")
	}
	// Infisical's H1 claim is confirmed inline from the live homepage, with source.
	if !strings.Contains(out, "**confirmed**") {
		t.Fatalf("expected a confirmed inline verdict:\n%s", out)
	}
	if !strings.Contains(out, "infisical.com") {
		t.Errorf("expected the cited source URL in the annotation:\n%s", out)
	}
	// The fact-check header banner is present.
	if !strings.Contains(out, "fact-checked by ngtm") {
		t.Errorf("missing fact-check banner")
	}
}

func TestAnnotateCorpus_NoFeedsAllUnverified(t *testing.T) {
	eng := NewEngineWith(&FeedRegistry{now: fixedNow}, offlineGenerator{}, fixedNow)
	cs := parseClaimsMarkdown(corpusFixture)
	rep, _ := eng.Compare(context.Background(), cs.Subjects, Options{NoFeeds: true, Claims: cs.Claims})
	out := AnnotateCorpus(corpusFixture, rep)
	if strings.Contains(out, "**confirmed**") || strings.Contains(out, "**contradicted**") {
		t.Errorf("offline: nothing should be confirmed/contradicted inline")
	}
	if !strings.Contains(out, "**unverified**") {
		t.Errorf("offline: expected unverified verdicts")
	}
}
