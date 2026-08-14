package gtm

import (
	"context"
	"strings"
	"testing"
)

func ideateRegistry() *FeedRegistry {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "hackernews", tier: TierFree, ev: []Evidence{
		{ID: "hackernews:0", Feed: "hackernews", Tier: TierFree, Title: "Ask HN: how do you track API costs across agents?",
			Snippet: "340 points · 210 comments on Hacker News", Metric: "mentions", Value: "340", Retrieved: "2026-06-02T12:00:00Z"},
		{ID: "hackernews:1", Feed: "hackernews", Tier: TierFree, Title: "Tell HN: launch checklists are scattered everywhere",
			Snippet: "55 points · 40 comments on Hacker News", Metric: "mentions", Value: "55", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	return reg
}

func TestRunIdeate_EvidenceShapedAndGated(t *testing.T) {
	eng := NewEngineWith(ideateRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "ideate", Options{
		Subject:   "AI developer tools",
		Keywords:  []string{"agent observability"},
		IdeaCount: 3,
	})
	if err != nil {
		t.Fatalf("runIdeate: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("validation: %v", v)
	}

	// Demand Signals grounded.
	ds := sectionByTitle(rep, "Demand Signals")
	if ds == nil || len(ds.Claims) != 2 || ds.Claims[0].Confidence != ConfGrounded {
		t.Fatalf("demand signals wrong: %+v", ds)
	}

	// Idea cards: strongest demand first, evidence-backed cards carry grounded
	// demand-basis claims; the idea itself never grounded.
	var ideaSections []*Section
	for i := range rep.Sections {
		if strings.HasPrefix(rep.Sections[i].Title, "Idea ") {
			ideaSections = append(ideaSections, &rep.Sections[i])
		}
	}
	if len(ideaSections) != 3 {
		t.Fatalf("want 3 idea cards, got %d", len(ideaSections))
	}
	if !strings.Contains(ideaSections[0].Title, "demand 340") {
		t.Errorf("ideas not demand-ranked: %s", ideaSections[0].Title)
	}
	for _, s := range ideaSections {
		for _, c := range s.Claims {
			if c.Confidence == ConfGrounded && !strings.HasPrefix(c.Text, "demand basis:") {
				t.Errorf("idea pitch must not be grounded: %+v", c)
			}
		}
	}
	// Keyword filler idea (3rd) is speculative and says so.
	if !strings.Contains(ideaSections[2].Body, "keyword-derived") {
		t.Errorf("filler idea must disclose speculation: %s", ideaSections[2].Body)
	}

	if sectionByTitle(rep, "Build Order") == nil {
		t.Fatal("missing Build Order section")
	}
	if rep.Metrics["idea_count"] != 3 || rep.Metrics["top_demand"] != 340 {
		t.Errorf("metrics wrong: %v", rep.Metrics)
	}
}

func TestProposeIdeas_AvoidAndCount(t *testing.T) {
	mentions := []Evidence{
		{ID: "hackernews:0", Feed: "hackernews", Title: "Ask HN: tracking api costs", Snippet: "100 points", Metric: "mentions", Value: "100"},
	}
	ideas := proposeIdeas("devtools", []string{"tracking api costs"}, mentions, []string{"TrackingApiCLI"}, 5)
	for _, i := range ideas {
		if strings.EqualFold(i.Title, "TrackingApiCLI") {
			t.Fatalf("avoid list ignored: %+v", ideas)
		}
	}
	if len(proposeIdeas("devtools", nil, mentions, nil, 1)) != 1 {
		t.Fatal("count cap ignored")
	}
}
