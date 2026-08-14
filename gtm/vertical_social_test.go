package gtm

import (
	"context"
	"strings"
	"testing"
)

func socialRegistry() *FeedRegistry {
	reg := &FeedRegistry{now: fixedNow}
	reg.Register(fakeFeed{name: "hackernews", tier: TierFree, ev: []Evidence{
		{ID: "hackernews:0", Feed: "hackernews", Tier: TierFree, Title: "Ask HN: secrets management pain",
			Snippet: "212 points · 145 comments on Hacker News", URL: "https://news.ycombinator.com/item?id=1",
			Metric: "mentions", Value: "212", Retrieved: "2026-06-02T12:00:00Z"},
	}})
	return reg
}

func TestRunSocial_DraftsLintedAndValidated(t *testing.T) {
	eng := NewEngineWith(socialRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "social", Options{
		Subject: "nvault",
		Pitch:   "offline-first secrets manager with E2EE sync",
		Tiers:   []FeedTier{TierFree},
	})
	if err != nil {
		t.Fatalf("runSocial: %v", err)
	}
	if v := rep.Validate(); len(v) != 0 {
		t.Fatalf("expected clean validation, got: %v", v)
	}

	// Community Voice must hold a grounded claim citing the HN mention.
	voice := sectionByTitle(rep, "Community Voice")
	if voice == nil || len(voice.Claims) != 1 {
		t.Fatalf("expected Community Voice with 1 claim, got %+v", voice)
	}
	if voice.Claims[0].Confidence != ConfGrounded || len(voice.Claims[0].Citations) == 0 {
		t.Errorf("community claim must be grounded+cited: %+v", voice.Claims[0])
	}

	// All six default channels render a draft section.
	for _, spec := range Channels {
		s := sectionByTitle(rep, spec.Label+" draft ("+spec.Kind+")")
		if s == nil {
			t.Fatalf("missing draft section for %s", spec.Key)
		}
		// Drafts are inferred (community evidence exists), never grounded.
		for _, c := range s.Claims {
			if c.Confidence == ConfGrounded {
				t.Errorf("%s: draft claim must not be grounded: %+v", spec.Key, c)
			}
		}
	}

	// Show HN title obeys the typed contract (prefix + length).
	hn := sectionByTitle(rep, "Show HN draft (launch)")
	if !strings.Contains(hn.Body, "**Title:** Show HN: ") {
		t.Errorf("show-hn draft missing required title prefix:\n%s", hn.Body)
	}

	// Offline templates never invent facts — fill slots are explicit.
	if !strings.Contains(hn.Body, "[FILL:") {
		t.Errorf("offline draft should carry explicit [FILL:] slots, got:\n%s", hn.Body)
	}

	// Calendar present, and no lint violations from our own templates.
	if sectionByTitle(rep, "Distribution Calendar") == nil {
		t.Fatal("missing Distribution Calendar section")
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "exceeds") || strings.Contains(w, "superlative") {
			t.Errorf("template draft violated its own channel contract: %s", w)
		}
	}
}

func TestRunSocial_ChannelSubsetAndUnknown(t *testing.T) {
	eng := NewEngineWith(socialRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "social", Options{
		Subject: "nvault", Channels: []string{"show-hn"},
	})
	if err != nil {
		t.Fatalf("runSocial: %v", err)
	}
	if s := sectionByTitle(rep, "Show HN draft (launch)"); s == nil {
		t.Fatal("selected channel show-hn missing")
	}
	if s := sectionByTitle(rep, "Product Hunt draft (launch)"); s != nil {
		t.Fatal("unselected channel producthunt should be absent")
	}
	if _, err := eng.Run(context.Background(), "social", Options{
		Subject: "nvault", Channels: []string{"show-hn", "nope"},
	}); err == nil || !strings.Contains(err.Error(), `unknown channel(s) nope`) {
		t.Fatalf("unknown channel must fail closed, got %v", err)
	}
}

func TestLintDraft_CatchesContractViolations(t *testing.T) {
	spec, _ := ChannelByKey("show-hn")
	v := lintDraft(spec, ChannelDraft{
		Channel: "show-hn",
		Title:   strings.Repeat("x", 90), // long AND missing prefix
		Body:    "This revolutionary tool is best-in-class.",
	})
	want := []string{"exceeds limit", "must start with", "revolutionary", "best-in-class"}
	for _, frag := range want {
		ok := false
		for _, msg := range v {
			if strings.Contains(msg, frag) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("lint missed %q in %v", frag, v)
		}
	}
	if v := lintDraft(spec, ChannelDraft{Channel: "show-hn", Title: "Show HN: nvault – secrets, offline", Body: "I built this."}); len(v) != 0 {
		t.Errorf("clean draft flagged: %v", v)
	}
}

func TestShowHNTitleDoesNotDoubleProductOrPrefix(t *testing.T) {
	spec, _ := ChannelByKey("show-hn")
	got := OfflineDraft(spec, "ngtm", "Nicos GTM – a local CLI that refuses to invent launch facts")
	if strings.Contains(got.Title, "ngtm – Nicos") || strings.Contains(got.Title, "Show HN: Show HN:") {
		t.Fatalf("doubled title: %q", got.Title)
	}
	if !strings.HasPrefix(got.Title, "Show HN: Nicos GTM") {
		t.Fatalf("want Nicos GTM after prefix, got %q", got.Title)
	}
	already := OfflineDraft(spec, "ngtm", "Show HN: Nicos GTM – a local CLI that refuses to invent launch facts")
	if strings.HasPrefix(already.Title, "Show HN: Show HN:") {
		t.Fatalf("double prefix: %q", already.Title)
	}
	plain := OfflineDraft(spec, "nvault", "offline-first secrets with E2EE sync")
	if plain.Title != "Show HN: nvault – offline-first secrets with E2EE sync" {
		t.Fatalf("plain title = %q", plain.Title)
	}
}

func TestRunSocial_ScorecardAndTune(t *testing.T) {
	eng := NewEngineWith(socialRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Run(context.Background(), "social", Options{
		Subject: "nvault", Pitch: "secrets sync across 3 devices with E2EE",
		Channels: []string{"show-hn", "x"}, Tune: true,
	})
	if err != nil {
		t.Fatalf("runSocial: %v", err)
	}
	if sectionByTitle(rep, "Content Scorecard") == nil {
		t.Fatal("missing Content Scorecard section")
	}
	for _, key := range []string{"score_show-hn", "score_x", "social_score", "social_channel_count", "social_contract", "social_grounding", "social_specificity", "social_completeness", "social_cta"} {
		if _, ok := rep.Metrics[key]; !ok {
			t.Errorf("missing metric %s in %v", key, rep.Metrics)
		}
	}
	// Pitch carries a number + community evidence is woven → decent floor.
	if rep.Metrics["score_show-hn"] < 5 {
		t.Errorf("show-hn score unexpectedly low: %v", rep.Metrics)
	}
}

func TestScoreSocialDraft_RubricDimensions(t *testing.T) {
	spec, _ := ChannelByKey("show-hn")
	clean := ChannelDraft{Channel: "show-hn",
		Title: "Show HN: nvault – secrets sync across 3 devices",
		Body:  "I built nvault. It syncs 3 devices with E2EE. What would make this useful for you?"}
	unsourced := ScoreSocialDraft(spec, clean, nil)
	if unsourced.Parts["specificity"] != 1 {
		t.Fatalf("unsourced numeric hook must not get the quantity point: %+v", unsourced.Parts)
	}
	sourced := ScoreSocialDraft(spec, clean, []Evidence{{Title: "syncs 3 devices", Snippet: "3"}})
	if sourced.Parts["specificity"] != 2 {
		t.Fatalf("evidence-backed quantity must score specificity 2: %+v", sourced.Parts)
	}
	yearOnly := ScoreSocialDraft(spec, clean, []Evidence{{Title: "launched in 2023", Snippet: "2023"}})
	if yearOnly.Parts["specificity"] != 1 {
		t.Fatalf("unrelated year in evidence must not source hook quantity 3: %+v", yearOnly.Parts)
	}
	if unsourced.Parts["contract"] != 2 || unsourced.Parts["completeness"] != 2 || unsourced.Parts["cta"] != 2 {
		t.Fatalf("clean draft parts wrong: %+v", unsourced)
	}
	if unsourced.Parts["grounding"] != 0 {
		t.Fatalf("no-evidence draft must score 0 grounding: %+v", unsourced)
	}
	dirty := ChannelDraft{Channel: "show-hn", Title: "revolutionary tool", Body: "[FILL: a] [FILL: b] [FILL: c] no ask here."}
	d := ScoreSocialDraft(spec, dirty, nil)
	if d.Total >= sourced.Total {
		t.Fatalf("dirty draft (%v) must score below sourced (%v)", d.Total, sourced.Total)
	}
	if d.Parts["cta"] != 0 || d.Parts["completeness"] >= 1 {
		t.Fatalf("dirty parts wrong: %+v", d)
	}
}
