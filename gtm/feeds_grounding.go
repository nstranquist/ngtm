package gtm

import (
	"fmt"
	"strings"
)

// Grounding readiness for the SERP-dependent verticals. brand/seo can only emit
// GROUNDED facts when a live SERP-class feed (self-hosted SearXNG, or a cheap
// keyed feed) is available; otherwise they fall back to speculative fixtures.
// This summarizes that state and tells the operator the exact one-command fix —
// the bridge between the gtm factory and the existing `ndev ask deep web-up`
// SearXNG bring-up. Pure + deterministic so `ngtm feeds doctor` is testable.

// SerpFeeds are the feed names that yield real SERP/positioning evidence.
var SerpFeeds = []string{"searxng", "serper", "brave", "tavily", "dataforseo"}

// GroundingStatus reports whether brand/seo can ground on a live SERP feed.
type GroundingStatus struct {
	LiveSERP bool     `json:"live_serp"`
	Sources  []string `json:"sources"`  // live SERP-class feeds
	Advisory string   `json:"advisory"` // human-facing guidance / fix
}

// GroundingAdvisory computes grounding readiness from SearXNG reachability and
// the set of cheap SERP feeds whose key is configured.
//
//	searxngSet:       SEARXNG_URL is set
//	searxngReachable: the SearXNG instance actually responded
//	cheapSerpLive:    names of cheap SERP feeds with their key set (serper/brave/tavily/dataforseo)
func GroundingAdvisory(searxngSet, searxngReachable bool, cheapSerpLive []string) GroundingStatus {
	var sources []string
	if searxngReachable {
		sources = append(sources, "searxng")
	}
	sources = append(sources, cheapSerpLive...)

	st := GroundingStatus{LiveSERP: len(sources) > 0, Sources: sources}
	switch {
	case searxngReachable:
		// SearXNG is free-tier → brand/seo ground on the DEFAULT (free) run.
		msg := "brand/seo GROUND by default — live SearXNG (free tier)"
		if len(cheapSerpLive) > 0 {
			msg += ", plus " + strings.Join(cheapSerpLive, ", ") + " with --tier cheap"
		}
		st.Advisory = msg + ". Facts cite real results (e.g. [searxng:N]) instead of speculative fixtures."
	case len(cheapSerpLive) > 0:
		// Only cheap SERP keys are set → they ground, but ONLY with --tier cheap;
		// the default free run is still speculative.
		st.Advisory = fmt.Sprintf(
			"brand/seo will GROUND with `--tier cheap` (sources: %s) — but the DEFAULT (free) run stays SPECULATIVE. Add --paid/--tier cheap, or run `ndev ask deep web-up` for free default grounding.",
			strings.Join(cheapSerpLive, ", "))
	case searxngSet && !searxngReachable:
		st.Advisory = "SEARXNG_URL is set but the instance is UNREACHABLE — start it with `ndev ask deep web-up` (or check `docker ps`). Until a SERP feed is live, brand/seo stay SPECULATIVE."
	default:
		st.Advisory = "brand/seo will be SPECULATIVE — no live SERP feed. Fastest free fix: `ndev ask deep web-up` (starts a local SearXNG + writes SEARXNG_URL), then `export SEARXNG_URL=http://localhost:8888`. Or set TAVILY_API_KEY / SERPER_API_KEY (--tier cheap)."
	}
	return st
}

// IsSerpFeed reports whether a feed name yields SERP-class evidence.
func IsSerpFeed(name string) bool {
	for _, s := range SerpFeeds {
		if s == name {
			return true
		}
	}
	return false
}
