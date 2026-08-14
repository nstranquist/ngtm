package gtm

import (
	"context"
	"strings"
	"testing"
)

func businessRegistry() *FeedRegistry {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "wikidata-claims", tier: TierFree, ev: []Evidence{
		{ID: "wikidata-claims:P452", Feed: "wikidata-claims", Tier: TierFree, Title: "industry: financial technology", Snippet: "Wikidata industry (P452)", URL: "https://www.wikidata.org/wiki/Q123", Metric: "company_fact", Value: "financial technology", Retrieved: "2026-06-02T12:00:00Z"},
		{ID: "wikidata-claims:P571", Feed: "wikidata-claims", Tier: TierFree, Title: "inception: 2010", Metric: "company_fact", Value: "2010", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	reg.Register(fakeFeed{name: "hackernews", tier: TierFree, ev: []Evidence{
		{ID: "hackernews:0", Feed: "hackernews", Tier: TierFree, Title: "Show HN: thing", Snippet: "120 points · 45 comments on Hacker News", URL: "https://news.ycombinator.com/item?id=1", Metric: "mentions", Value: "120", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	return reg
}

func TestRunBusiness_GroundedAndValidated(t *testing.T) {
	eng := NewEngineWith(businessRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "business", Options{Subject: "Stripe", Tiers: []FeedTier{TierFree}})
	if err != nil {
		t.Fatalf("runBusiness: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	// Strengths must be grounded in the company facts.
	s := sectionByTitle(rep, "Strengths")
	if s == nil || len(s.Claims) == 0 {
		t.Fatalf("expected grounded strengths, got %+v", s)
	}
	for _, c := range s.Claims {
		if c.Confidence != ConfGrounded || len(c.Citations) == 0 {
			t.Errorf("strength not grounded with citation: %+v", c)
		}
	}
	// SWOT + sizing sections exist.
	for _, title := range []string{"Weaknesses", "Opportunities", "Threats", "TAM / SAM / SOM"} {
		if sectionByTitle(rep, title) == nil {
			t.Errorf("missing section %q", title)
		}
	}
	// Opportunities/Threats are inferred (not grounded).
	for _, title := range []string{"Opportunities", "Threats"} {
		for _, c := range sectionByTitle(rep, title).Claims {
			if c.Confidence == ConfGrounded {
				t.Errorf("%s claim should not be grounded: %+v", title, c)
			}
		}
	}
	// Business panel has the unit-economics critics.
	if rep.Panel == nil || len(rep.Panel.Verdicts) != 6 {
		t.Fatalf("expected 6 business critics, got %+v", rep.Panel)
	}
	if !hasCritic(rep.Panel, "Unit Economics (CAC/LTV)") || !hasCritic(rep.Panel, "Margin / Pricing") {
		t.Errorf("missing unit-economics critics: %+v", rep.Panel.Verdicts)
	}
}

func TestRunBusiness_PremiumGroundedSizing(t *testing.T) {
	reg := businessRegistry()
	reg.Register(fakeFeed{name: "marketsizing", tier: TierPremium, ev: []Evidence{
		{ID: "marketsizing:tam", Feed: "marketsizing", Tier: TierPremium, Title: "TAM: $12.0B", Value: "$12.0B", URL: "https://provider/x", Metric: "market_size", Extra: map[string]string{"scope": "tam"}, Retrieved: "2026-06-02T12:00:00Z"},
		{ID: "marketsizing:som", Feed: "marketsizing", Tier: TierPremium, Title: "SOM: $150.0M", Value: "$150.0M", URL: "https://provider/x", Metric: "market_size", Extra: map[string]string{"scope": "som"}, Retrieved: "2026-06-02T12:00:00Z"},
	}})
	eng := NewEngineWith(reg, offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "business", Options{Subject: "Stripe", Tiers: []FeedTier{TierFree, TierPremium}})
	if err != nil {
		t.Fatalf("runBusiness premium: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	tam := sectionByTitle(rep, "TAM / SAM / SOM")
	if tam == nil {
		t.Fatal("missing TAM section")
	}
	var grounded int
	for _, c := range tam.Claims {
		if c.Confidence == ConfGrounded && len(c.Citations) > 0 && (c.Text == "TAM = $12.0B" || c.Text == "SOM = $150.0M") {
			grounded++
		}
	}
	if grounded != 2 {
		t.Fatalf("expected 2 grounded dollar claims, got %d: %+v", grounded, tam.Claims)
	}
}

func TestRunBusiness_OfflineSpeculative(t *testing.T) {
	eng := NewEngineWith(&FeedRegistry{now: fixedNow}, offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "business", Options{Subject: "nvault"})
	if err != nil {
		t.Fatalf("runBusiness offline: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("offline business report must validate, got: %v", v)
	}
	for _, e := range rep.Evidence {
		if !e.Synthetic {
			t.Errorf("expected only synthetic evidence offline: %+v", e)
		}
	}
	if rep.Panel.Survives {
		t.Errorf("offline business thesis must not survive the panel: median=%.1f", rep.Panel.MedianScore)
	}
}

func TestRunBrand_ValidatedAndPanel(t *testing.T) {
	eng := NewEngineWith(businessRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "brand", Options{Subject: "Stripe", Tiers: []FeedTier{TierFree}})
	if err != nil {
		t.Fatalf("runBrand: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}
	for _, title := range []string{"Brand Context", "Naming & Positioning (inferred)", "Logo Direction (inferred)", "Landing Copy (inferred)"} {
		if sectionByTitle(rep, title) == nil {
			t.Errorf("missing brand section %q", title)
		}
	}
	if rep.Panel == nil || !hasCritic(rep.Panel, "Distinctiveness") || !hasCritic(rep.Panel, "Category Clarity") {
		t.Fatalf("missing brand critics: %+v", rep.Panel)
	}
}

func TestRunBrand_OfflineSpeculative(t *testing.T) {
	t.Setenv("RECRAFT_API_KEY", "must-not-be-posted")
	eng := NewEngineWith(&FeedRegistry{now: fixedNow}, offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "brand", Options{Subject: "nvault", Offline: true, NoFeeds: true})
	if err != nil {
		t.Fatalf("runBrand offline: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("offline brand report must validate, got: %v", v)
	}
	logo := sectionByTitle(rep, "Logo Direction (inferred)")
	if logo == nil || logo.Body == "" {
		t.Fatalf("expected a logo brief offline")
	}
	if strings.Contains(logo.Body, "Generated mark:") {
		t.Fatal("offline brand posted Recraft despite Offline/NoFeeds")
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "recraft") {
			t.Fatalf("offline brand must not call Recraft: %s", w)
		}
	}
}

func hasCritic(p *PanelResult, name string) bool {
	for _, v := range p.Verdicts {
		if v.Critic == name {
			return true
		}
	}
	return false
}
