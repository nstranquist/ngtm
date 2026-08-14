package gtm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// FeedQuery is the request handed to every Feed.
type FeedQuery struct {
	Subject  string   // the product/company/market under analysis
	Keywords []string // optional seed keywords/topics
	Limit    int      // max evidence items per feed (0 => feed default)
	// SEO scope is optional for general GTM calls. SEO lifecycle callers set all
	// fields so providers can honor every dimension they support; unsupported
	// scope remains explicit in the resulting query identity.
	Locale       string
	LanguageCode string
	LocationCode int
	Device       string
	// Category is an optional disambiguation hint (e.g. "developer tools",
	// "password manager"). Entity-resolving feeds (Wikidata) use it to prefer
	// the matching real-world entity over homonyms.
	Category string
}

// Feed is one live data source. Implementations must be safe for concurrent
// use and must honor ctx cancellation.
type Feed interface {
	Name() string
	Tier() FeedTier
	// KeyEnv is the env var holding this feed's credential, or "" if the feed
	// needs no key (free tier).
	KeyEnv() string
	// Available reports whether the feed can run on this machine right now
	// (free feeds are always available; keyed feeds require their KeyEnv set).
	Available() bool
	Query(ctx context.Context, q FeedQuery) ([]Evidence, error)
}

// FeedRateLimitError distinguishes a reachable provider that temporarily
// refused a query from a broken/unreachable feed.
type FeedRateLimitError struct {
	Feed       string
	RetryAfter time.Duration
}

func (e *FeedRateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s rate limited; retry after %s", e.Feed, e.RetryAfter.Round(time.Second))
	}
	return e.Feed + " rate limited"
}

// FeedRegistry holds the set of feeds the engine may consult.
type FeedRegistry struct {
	feeds []Feed
	now   func() time.Time
}

// NewFeedRegistry builds a registry with the canonical feed set. The clock is
// injectable for deterministic tests.
func NewFeedRegistry(now func() time.Time) *FeedRegistry {
	if now == nil {
		now = time.Now
	}
	r := &FeedRegistry{now: now}
	// Free / local-first tier (no key, no per-call cost).
	r.Register(&wikidataFeed{now: now})
	r.Register(&wikidataClaimsFeed{now: now})
	r.Register(&hackerNewsFeed{now: now})
	r.Register(&redditFeed{now: now})
	r.Register(&searxngFeed{now: now}) // self-hosted SERP (SEARXNG_URL); free, owned
	// Cheap pay-per-call tier (key-gated; opt-in).
	r.Register(&serperFeed{now: now})
	r.Register(&braveFeed{now: now})
	r.Register(&tavilyFeed{now: now}) // LLM search; likely-already-held TAVILY_API_KEY
	r.Register(&dataForSEOFeed{now: now})
	r.Register(&crunchbaseFeed{now: now})
	r.Register(&peopleDataLabsFeed{now: now})
	r.Register(&landingFeed{now: now})
	// Premium subscription tier (key + endpoint gated).
	r.Register(&marketSizingFeed{now: now})
	return r
}

// Register adds a feed.
func (r *FeedRegistry) Register(f Feed) { r.feeds = append(r.feeds, f) }

// Feeds returns all registered feeds (for `feeds` doctor output).
func (r *FeedRegistry) Feeds() []Feed { return r.feeds }

// Selectable returns available feeds whose tier is in the allowed set.
func (r *FeedRegistry) Selectable(tiers map[FeedTier]bool) []Feed {
	var out []Feed
	for _, f := range r.feeds {
		if tiers[f.Tier()] && f.Available() {
			out = append(out, f)
		}
	}
	return out
}

// Gather runs every selectable feed concurrently, aggregates their Evidence,
// and collects per-feed errors as warnings (a single feed failing never aborts
// the run). When no real feed yields anything, a synthetic fixture row is
// appended so the rail still produces a (clearly-labeled speculative) report.
func (r *FeedRegistry) Gather(ctx context.Context, q FeedQuery, tiers map[FeedTier]bool) ([]Evidence, []string) {
	feeds := r.Selectable(tiers)
	var (
		mu       sync.Mutex
		evidence []Evidence
		warnings []string
		wg       sync.WaitGroup
	)
	for _, f := range feeds {
		wg.Add(1)
		go func(f Feed) {
			defer wg.Done()
			ev, err := f.Query(ctx, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("feed %s: %v", f.Name(), err))
				return
			}
			evidence = append(evidence, ev...)
		}(f)
	}
	wg.Wait()

	// Deterministic ordering: tier (free first), then feed, then id.
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Tier != evidence[j].Tier {
			return tierRank(evidence[i].Tier) < tierRank(evidence[j].Tier)
		}
		if evidence[i].Feed != evidence[j].Feed {
			return evidence[i].Feed < evidence[j].Feed
		}
		return evidence[i].ID < evidence[j].ID
	})
	sort.Strings(warnings)

	hasReal := false
	for _, e := range evidence {
		if !e.Synthetic {
			hasReal = true
			break
		}
	}
	if !hasReal {
		evidence = append(evidence, fixtureEvidence(q, r.now())...)
		warnings = append(warnings, "no live evidence — using synthetic fixtures; all claims are speculative. Enable a SERP feed: self-host SearXNG (set SEARXNG_URL, free) or set TAVILY_API_KEY / SERPER_API_KEY / BRAVE_API_KEY (--tier cheap).")
	}
	return evidence, warnings
}

func tierRank(t FeedTier) int {
	switch t {
	case TierFree:
		return 0
	case TierCheap:
		return 1
	case TierPremium:
		return 2
	default:
		return 3
	}
}

// fixtureEvidence is the deterministic offline fallback: synthetic rows so the
// engine can demonstrate the rail with no network and no keys. Synthetic=true
// means these can never back a grounded claim (Report.Validate enforces it).
func fixtureEvidence(q FeedQuery, now time.Time) []Evidence {
	ts := now.UTC().Format(time.RFC3339)
	subj := strings.TrimSpace(q.Subject)
	if subj == "" {
		subj = "the subject"
	}
	return []Evidence{
		{
			ID: "fixture:0", Feed: "fixture", Tier: TierFree, Synthetic: true, Retrieved: ts,
			Title:   "synthetic SERP placeholder",
			Snippet: fmt.Sprintf("No live SERP data was fetched for %q. This is a placeholder so the report renders offline; do not treat it as a real ranking.", subj),
		},
		{
			ID: "fixture:1", Feed: "fixture", Tier: TierFree, Synthetic: true, Retrieved: ts,
			Title:   "synthetic keyword placeholder",
			Snippet: fmt.Sprintf("No live keyword-volume data for %q. Run with --tier cheap and a DataForSEO/Serper key for real numbers.", subj),
			Metric:  "search_volume", Value: "unknown",
		},
	}
}
