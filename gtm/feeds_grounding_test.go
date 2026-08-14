package gtm

import (
	"strings"
	"testing"
)

func TestGroundingAdvisory(t *testing.T) {
	// No SERP at all → speculative, with the web-up fix.
	st := GroundingAdvisory(false, false, nil)
	if st.LiveSERP {
		t.Error("expected no live SERP")
	}
	if !strings.Contains(st.Advisory, "ndev ask deep web-up") || !strings.Contains(st.Advisory, "SPECULATIVE") {
		t.Errorf("advisory should point to web-up and flag speculative: %s", st.Advisory)
	}

	// SEARXNG_URL set but unreachable → distinct "start it" message.
	st = GroundingAdvisory(true, false, nil)
	if st.LiveSERP {
		t.Error("unreachable SearXNG is not a live SERP")
	}
	if !strings.Contains(st.Advisory, "UNREACHABLE") {
		t.Errorf("expected an unreachable message: %s", st.Advisory)
	}

	// SearXNG reachable → grounded, source listed.
	st = GroundingAdvisory(true, true, nil)
	if !st.LiveSERP || len(st.Sources) != 1 || st.Sources[0] != "searxng" {
		t.Fatalf("expected searxng as a live source: %+v", st)
	}
	if !strings.Contains(st.Advisory, "GROUND") {
		t.Errorf("expected a grounded advisory: %s", st.Advisory)
	}

	// A cheap SERP key alone grounds only with --tier cheap; the default free
	// run is still speculative, and the advisory must say so.
	st = GroundingAdvisory(false, false, []string{"tavily"})
	if !st.LiveSERP || st.Sources[0] != "tavily" {
		t.Fatalf("expected tavily as a live source: %+v", st)
	}
	if !strings.Contains(st.Advisory, "--tier cheap") || !strings.Contains(st.Advisory, "SPECULATIVE") {
		t.Errorf("cheap-only advisory should flag the tier requirement + default-speculative: %s", st.Advisory)
	}
}

func TestIsSerpFeed(t *testing.T) {
	for _, s := range []string{"searxng", "serper", "brave", "tavily", "dataforseo"} {
		if !IsSerpFeed(s) {
			t.Errorf("%s should be a SERP feed", s)
		}
	}
	for _, s := range []string{"wikidata", "hackernews", "reddit", "crunchbase"} {
		if IsSerpFeed(s) {
			t.Errorf("%s should NOT be a SERP feed", s)
		}
	}
}
