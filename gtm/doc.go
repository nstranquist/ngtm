// Package gtm is the go-to-market factory engine — the business/marketing
// counterpart to the repo's application-development factory (scaffold → ship →
// orchestrate → nvault).
//
// The design principle is one rail, replicated per vertical:
//
//	feeds (live data) → Evidence (facts, deterministic) → grounded Claims
//	                                                          │
//	                          LLM writes PROSE ONLY, constrained to the claims
//	                                                          │
//	                        adversarial panel (shark-tank) → cited Report
//
// Facts come only from feeds and are deterministic; the LLM supplies wording,
// never data. A Claim can be marked ConfGrounded only if it cites a
// non-synthetic Evidence (Report.Validate enforces it), so offline/local-LLM
// runs can never present a guess as a measured fact. This is the structural
// fix for the failure mode that produces confident-but-false GTM claims.
//
// Feed tiers: free/local-first (Wikidata always on; self-hosted SearXNG SERP
// when SEARXNG_URL is set — zero-cost, owned), cheap pay-per-call (Tavily,
// Serper, Brave, DataForSEO; key-gated, opt-in via --tier cheap), and premium
// (Ahrefs/Semrush/SimilarWeb; roadmap). Named LLM providers are host-injected;
// offline is the default when no host registry is installed.
//
// Verticals: "seo" (SEO & positioning) ships first; "business" (plan + SWOT +
// shark-tank financials) and "brand" (logo + landing copy) reuse the same rail.
//
// Surfaces over this one engine: the `ndev gtm` domain, the `ngtm` peer binary,
// and an MCP server (`ngtm mcp`).
package gtm
