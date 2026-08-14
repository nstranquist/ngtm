package gtm

import (
	"context"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) }

// fakeFeed returns canned evidence — keeps tests offline and deterministic.
type fakeFeed struct {
	name string
	tier FeedTier
	ev   []Evidence
}

func (f fakeFeed) Name() string                                         { return f.name }
func (f fakeFeed) Tier() FeedTier                                       { return f.tier }
func (f fakeFeed) KeyEnv() string                                       { return "" }
func (f fakeFeed) Available() bool                                      { return true }
func (f fakeFeed) Query(context.Context, FeedQuery) ([]Evidence, error) { return f.ev, nil }

func liveRegistry() *FeedRegistry {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "serper", tier: TierCheap, ev: []Evidence{
		{ID: "serper:0", Feed: "serper", Tier: TierCheap, Title: "Infisical — Secrets Management", URL: "https://infisical.com", Metric: "serp_rank", Value: "1", Retrieved: "2026-06-02T12:00:00Z"},
		{ID: "serper:1", Feed: "serper", Tier: TierCheap, Title: "Doppler — SecretOps", URL: "https://doppler.com", Metric: "serp_rank", Value: "2", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	reg.Register(fakeFeed{name: "dataforseo", tier: TierCheap, ev: []Evidence{
		{ID: "dataforseo:0", Feed: "dataforseo", Tier: TierCheap, Title: "secrets manager", Snippet: "monthly search volume 8100 (competition: HIGH)", Metric: "search_volume", Value: "8100", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	reg.Register(fakeFeed{name: "wikidata", tier: TierFree, ev: []Evidence{
		{ID: "wikidata:Q1", Feed: "wikidata", Tier: TierFree, Title: "Secrets management", Snippet: "practice of securely storing credentials", URL: "http://www.wikidata.org/entity/Q1", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	return reg
}

func TestRunSEO_GroundedAndValidated(t *testing.T) {
	eng := NewEngineWith(liveRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "seo", Options{
		Subject: "nvault", Keywords: []string{"secrets manager"},
		Tiers: []FeedTier{TierFree, TierCheap},
	})
	if err != nil {
		t.Fatalf("runSEO: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}

	// SERP Reality must hold grounded claims citing real (non-synthetic) evidence.
	serp := sectionByTitle(rep, "SERP Reality")
	if serp == nil || len(serp.Claims) != 2 {
		t.Fatalf("expected 2 SERP claims, got %+v", serp)
	}
	for _, c := range serp.Claims {
		if c.Confidence != ConfGrounded {
			t.Errorf("SERP claim not grounded: %+v", c)
		}
		if len(c.Citations) == 0 {
			t.Errorf("grounded claim missing citation: %+v", c)
		}
	}

	// No synthetic fixtures when real evidence exists.
	for _, e := range rep.Evidence {
		if e.Synthetic {
			t.Errorf("unexpected synthetic evidence with live feeds: %+v", e)
		}
	}

	// Panel ran with all five critics.
	if rep.Panel == nil || len(rep.Panel.Verdicts) != 5 {
		t.Fatalf("expected 5 panel verdicts, got %+v", rep.Panel)
	}
	// With volume + SERP + 2 competitors, this thesis should survive.
	if !rep.Panel.Survives {
		t.Errorf("expected thesis to survive panel, median=%.1f", rep.Panel.MedianScore)
	}
}

func TestValidate_CatchesGroundedClaimOnSyntheticEvidence(t *testing.T) {
	rep := &Report{
		Evidence: []Evidence{{ID: "fixture:0", Synthetic: true}},
		Sections: []Section{{
			Title:  "Bad",
			Claims: []Claim{{Text: "everyone searches this", Confidence: ConfGrounded, Citations: []string{"fixture:0"}}},
		}},
	}
	v := rep.Validate()
	if len(v) != 1 || !strings.Contains(v[0], "GROUNDED claim has no real") {
		t.Fatalf("expected ungrounded-claim violation, got: %v", v)
	}
}

func TestValidate_CatchesUnknownCitation(t *testing.T) {
	rep := &Report{
		Evidence: []Evidence{{ID: "serper:0"}},
		Sections: []Section{{
			Title:  "Bad",
			Claims: []Claim{{Text: "x", Confidence: ConfInferred, Citations: []string{"ghost:9"}}},
		}},
	}
	v := rep.Validate()
	if len(v) != 1 || !strings.Contains(v[0], "unknown evidence") {
		t.Fatalf("expected unknown-citation violation, got: %v", v)
	}
}

func TestRunSEO_OfflineFallbackIsSpeculative(t *testing.T) {
	empty := &FeedRegistry{now: fixedNow} // no feeds registered
	eng := NewEngineWith(empty, offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "seo", Options{Subject: "nvault"})
	if err != nil {
		t.Fatalf("runSEO offline: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("offline report should still validate (no grounded claims), got: %v", v)
	}
	// All evidence is synthetic.
	for _, e := range rep.Evidence {
		if !e.Synthetic {
			t.Errorf("expected only synthetic evidence offline, got real: %+v", e)
		}
	}
	// Panel must reject a vibe-only thesis on the integrity lens.
	if rep.Panel.Survives {
		t.Errorf("offline thesis must not survive the panel: median=%.1f", rep.Panel.MedianScore)
	}
	if len(rep.Warnings) == 0 {
		t.Errorf("expected a no-live-evidence warning")
	}
}

func TestUnknownVertical(t *testing.T) {
	eng := NewEngineWith(liveRegistry(), offlineGenerator{}, fixedNow)
	if _, err := eng.Run(context.Background(), "nope", Options{Subject: "x"}); err == nil {
		t.Fatal("expected error for unknown vertical")
	}
}

func sectionByTitle(r *Report, title string) *Section {
	for i := range r.Sections {
		if r.Sections[i].Title == title {
			return &r.Sections[i]
		}
	}
	return nil
}
