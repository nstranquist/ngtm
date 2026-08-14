//nolint:errcheck // MCP protocol writes are best-effort; request/ledger errors are returned in-band.
package gtmcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

// MCP stdio server for the GTM factory. Newline-delimited JSON-RPC 2.0 (the
// MCP stdio transport), no external dependencies — it drives the in-process
// engine directly rather than shelling out. Surfaced as `ngtm mcp` /
// `ndev gtm mcp`. The protocol version tracks the MCP stdio baseline.
const mcpProtocolVersion = "2024-11-05"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cmdMCP runs the stdio server.
func cmdMCP(_ []string, in io.Reader, out, errOut io.Writer) int {
	if err := ServeMCP(in, out); err != nil {
		fmt.Fprintln(errOut, "gtm mcp:", err)
		return 1
	}
	return 0
}

// ServeMCP reads JSON-RPC messages line-by-line and writes responses.
func ServeMCP(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // ignore malformed frames
		}
		resp, respond := handleRPC(req)
		if respond {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// verticalToolSchema is the shared input contract for a vertical tool.
func verticalToolSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"subject":  map[string]any{"type": "string", "description": "Product/company/market to analyze"},
			"keywords": map[string]any{"type": "string", "description": "Comma-separated seed keywords"},
			"category": map[string]any{"type": "string", "description": "Disambiguation hint for entity resolution (e.g. \"developer tools\", \"password manager\") — prefers the matching real-world entity over homonyms"},
			"tier":     map[string]any{"type": "string", "description": "Feed tier: free | cheap | premium | all | none", "default": "free"},
			"offline":  map[string]any{"type": "boolean", "description": "Hermetic: no LLM, no network (fixtures only)"},
			"since":    map[string]any{"type": "string", "description": "Path to a prior watch-state file: diff this run's claim statuses + metrics (SERP rank, volume, mentions) against it, update it, and return a drift report"},
		},
		"required": []string{"subject"},
	}
}

// seoLifecycleToolSchema is the shared local-artifact contract for the closed
// loop SEO tools. Individual tools use only the fields relevant to their verb;
// all mutations are bounded to workspace/output paths supplied by the caller.
func seoLifecycleToolSchema(requireProduct bool) map[string]any {
	properties := map[string]any{
		"product":       map[string]any{"type": "string", "description": "Product/project name"},
		"config":        map[string]any{"type": "string", "description": "Tracked SEO project YAML"},
		"workspace":     map[string]any{"type": "string", "description": "Exact local artifact workspace"},
		"keywords":      map[string]any{"type": "string", "description": "Comma-separated seed keywords"},
		"keyword":       map[string]any{"type": "string", "description": "One researched keyword"},
		"unique_value":  map[string]any{"type": "string", "description": "Concrete original value, evidence, tool, or asset"},
		"audience":      map[string]any{"type": "string", "description": "Specific reader"},
		"fixture":       map[string]any{"type": "string", "description": "Typed local fixture path"},
		"research":      map[string]any{"type": "string", "description": "Research artifact path or sha256 ID"},
		"brief":         map[string]any{"type": "string", "description": "Brief artifact path or sha256 ID"},
		"body":          map[string]any{"type": "string", "description": "Page body text"},
		"body_file":     map[string]any{"type": "string", "description": "Page body file"},
		"title":         map[string]any{"type": "string", "description": "Page title"},
		"description":   map[string]any{"type": "string", "description": "Meta description"},
		"slug":          map[string]any{"type": "string", "description": "Page slug"},
		"out_dir":       map[string]any{"type": "string", "description": "Local publish output directory"},
		"domain":        map[string]any{"type": "string", "description": "Owned domain"},
		"site_url":      map[string]any{"type": "string", "description": "Owned absolute site URL"},
		"content_dir":   map[string]any{"type": "string", "description": "Bounded local content root"},
		"locale":        map[string]any{"type": "string", "description": "Single locale name override"},
		"language":      map[string]any{"type": "string", "description": "Single language code override"},
		"location_code": map[string]any{"type": "number", "description": "Single provider location code override"},
		"device":        map[string]any{"type": "string", "description": "desktop | mobile | tablet"},
		"tier":          map[string]any{"type": "string", "description": "free | cheap | premium | all | none"},
		"start":         map[string]any{"type": "string", "description": "Measurement start date YYYY-MM-DD"},
		"end":           map[string]any{"type": "string", "description": "Measurement end date YYYY-MM-DD"},
		"approved":      map[string]any{"type": "boolean", "description": "Human approval acknowledgement"},
		"index":         map[string]any{"type": "boolean", "description": "Request indexable output; requires approved and all quality gates"},
		"strict":        map[string]any{"type": "boolean", "description": "Return an MCP error when blockers remain"},
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if requireProduct {
		schema["required"] = []string{"product"}
	}
	return schema
}

// modelInputKeys are the numeric assumptions the model-driven verticals accept.
var modelInputKeys = []string{
	"acv", "arpa", "gross_margin", "monthly_churn", "cac", "expansion",
	"customers", "penetration", "next_best_price", "diff_value", "value_capture", "negatives",
	"one_time", "growth_rate", "profit_margin", "net_new_arr", "net_burn", "sm_spend", "gained_arr", "lost_arr",
}

// modelToolSchema is verticalToolSchema plus the numeric model inputs, for the
// economics/pricing/motion tools. Supplied inputs are treated as real
// assumptions; omitted ones fall back to analyst-benchmark defaults.
func modelToolSchema() map[string]any {
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"subject":         map[string]any{"type": "string", "description": "Product/company to analyze"},
			"offline":         map[string]any{"type": "boolean", "description": "Hermetic: no LLM (deterministic narrative)"},
			"acv":             num("Annual contract value ($)"),
			"arpa":            num("Monthly revenue per account ($)"),
			"gross_margin":    num("Gross margin 0..1"),
			"monthly_churn":   num("Monthly revenue churn 0..1"),
			"cac":             num("Fully-loaded customer acquisition cost ($)"),
			"expansion":       num("Monthly net expansion 0..1 (drives NRR)"),
			"customers":       num("# addressable ICP accounts (economics bottom-up sizing)"),
			"penetration":     num("Realistic near-term share 0..1 (economics SOM)"),
			"next_best_price": num("Next-best alternative price ($) (pricing)"),
			"diff_value":      num("Quantified value of differentiation ($) (pricing)"),
			"value_capture":   num("Share of differentiation value to capture 0..1 (pricing)"),
			"negatives":       num("Switching-cost/negative value ($) (pricing)"),
			"one_time":        num("1 = model as a one-time purchase (no churn/NRR) (economics)"),
			"growth_rate":     num("Annual revenue growth fraction → Rule of 40 (economics)"),
			"profit_margin":   num("Operating/FCF margin fraction → Rule of 40 (economics)"),
			"net_new_arr":     num("Net new ARR ($) → Burn Multiple / Magic Number (economics)"),
			"net_burn":        num("Net cash burned ($) → Burn Multiple (economics)"),
			"sm_spend":        num("Sales & marketing spend ($) → Magic Number (economics)"),
			"gained_arr":      num("New+expansion ARR ($) → SaaS Quick Ratio (economics)"),
			"lost_arr":        num("Churned+contraction ARR ($) → SaaS Quick Ratio (economics)"),
		},
		"required": []string{"subject"},
	}
}

// landingToolSchema is the input contract for the publish/landing tool.
func landingToolSchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	tierItem := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name":      str("Tier name"),
			"price":     str("Pre-formatted price, e.g. $39 or $28.5K"),
			"period":    str("'' | one-time | /mo | /yr"),
			"note":      str("One-line role/positioning"),
			"features":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Bullet list"},
			"cta_url":   str("Button href (falls back to buy_url)"),
			"cta_label": str("Button label"),
			"featured":  map[string]any{"type": "boolean", "description": "Highlight as the anchor/most-popular tier"},
		},
	}
	featItem := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"title": str("Card title"), "body": str("Card body")},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"subject":         str("Product/subject to publish a page for"),
			"product":         str("Display/brand name (defaults to subject)"),
			"headline":        str("Hero H1 line"),
			"tagline":         str("Hero subhead/positioning"),
			"badge":           str("Eyebrow chip"),
			"cta":             str("Primary button label"),
			"buy_url":         str("Checkout/signup href the buttons point at"),
			"grounding":       str("Grounding/confidence caveat under pricing"),
			"features_head":   str("Features section heading"),
			"pricing_head":    str("Pricing section heading"),
			"price":           num("Single headline price ($) — shorthand for one tier"),
			"one_time":        map[string]any{"type": "boolean", "description": "Render `price` as one-time (else /mo)"},
			"tier_name":       str("Tier name when using `price` (default Pro)"),
			"next_best_price": num("Next-best alternative price ($) — derives good-better-best tiers"),
			"diff_value":      num("Value of differentiation ($) — derives tiers"),
			"value_capture":   num("Share of differentiation captured 0..1 — derives tiers"),
			"features":        map[string]any{"type": "array", "items": featItem, "description": "Value cards"},
			"tiers":           map[string]any{"type": "array", "items": tierItem, "description": "Explicit pricing tiers (overrides derivation/price)"},
			"brand":           map[string]any{"type": "boolean", "description": "Run the brand vertical for grounded hero copy (needs a provider/feeds)"},
			"format":          map[string]any{"type": "string", "description": "html | json (default html)", "default": "html"},
		},
		"required": []string{"subject"},
	}
}

// mcpTools is the tool manifest. inputSchema is a JSONSchema object.
func mcpTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "gtm_seo",
			"description": "Run the GTM SEO & positioning vertical: gather live SERP/keyword/entity feeds, derive grounded facts, propose a positioning wedge, and stress-test it with an adversarial panel. Returns a citation-grounded JSON report. Defaults to the free feed tier (no cost).",
			"inputSchema": verticalToolSchema(),
		},
		{
			"name":        "gtm_seo_research",
			"description": "Build and persist a scoped, evidence-backed SEO research snapshot with owned-content inventory, keyword expansion, transparent opportunity scoring, coverage, and strict blockers.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_opportunities",
			"description": "Read a durable SEO research artifact and return its ranked opportunities and coverage.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_brief",
			"description": "Create and persist an evidence-citing SEO brief. The brief is blocked until a concrete unique-value statement meets policy.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_publish",
			"description": "Render a local SEO draft. Output is noindex by default; indexable output requires approved=true, index=true, a passing brief, canonical, useful depth, structured data, and sitemap.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_measure",
			"description": "Collect or fixture-load typed Search Console, GA4, URL Inspection, and PageSpeed outcomes into a durable local measurement artifact.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_retro",
			"description": "Join latest research and measurement artifacts into deterministic double-down, iterate, refresh, consolidate, or retire decisions.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_audit",
			"description": "Audit SEO artifact digests, latest pointers, event order, evidence requirements, publish metadata, canonical, structured data, sitemap, and measurement integrity.",
			"inputSchema": seoLifecycleToolSchema(true),
		},
		{
			"name":        "gtm_seo_eval",
			"description": "Run the embedded deterministic SEO quality-v2 lifecycle fixture and return every quality dimension.",
			"inputSchema": seoLifecycleToolSchema(false),
		},
		{
			"name":        "gtm_business",
			"description": "Run the GTM business-plan + SWOT vertical: gather company/market feeds (Wikidata claims, HN/Reddit mentions; Crunchbase/PDL when keyed), produce a SWOT (Strengths/Weaknesses grounded, Opportunities/Threats inferred), TAM/SAM/SOM sized only as evidence allows, and an investor shark-tank panel. Citation-grounded JSON.",
			"inputSchema": verticalToolSchema(),
		},
		{
			"name":        "gtm_brand",
			"description": "Run the GTM brand & assets vertical: grounded brand context, an inferred naming/positioning concept, a Recraft logo brief (live SVG when RECRAFT_API_KEY is set), and landing copy. Citation-grounded JSON.",
			"inputSchema": verticalToolSchema(),
		},
		{
			"name":        "gtm_economics",
			"description": "Run the GTM unit-economics vertical: from ACV/CAC/gross-margin/churn/expansion inputs (analyst defaults for any omitted), compute LTV, LTV:CAC, CAC-payback, NRR/GRR, a GO/CONDITIONAL/NO-GO gate, the break-even levers ('what needs to be true'), conservative/base/stretch scenarios, optional bottom-up SAM/SOM (with `customers`+`penetration`), and a CFO panel. Provenance is exact: operator inputs are grounded, defaults speculative, computed numbers inferred. Citation-grounded JSON.",
			"inputSchema": modelToolSchema(),
		},
		{
			"name":        "gtm_pricing",
			"description": "Run the GTM pricing vertical: derive a value-based price from the next-best alternative price + quantified differentiation value (WTP ceiling), recommend a price that leaves customer surplus, build good-better-best tiers, advise the value metric, and emit a Van Westendorp WTP survey + a pricing panel. Citation-grounded JSON.",
			"inputSchema": modelToolSchema(),
		},
		{
			"name":        "gtm_motion",
			"description": "Run the GTM motion vertical: from ACV, select the go-to-market motion (product-led / hybrid / sales-led), specify the funnel with PQL/MQL benchmarks, channel loops, the beachhead/ICP discipline, and a customer-discovery + Sean-Ellis-40% PMF validation plan, plus a motion panel. Citation-grounded JSON.",
			"inputSchema": modelToolSchema(),
		},
		{
			"name":        "gtm_social",
			"description": "Run the GTM distribution-content vertical: channel-native launch/social drafts (Show HN, Product Hunt, Reddit, X thread, LinkedIn, Indie Hackers) built around a one-line pitch, linted against each channel's typed posting contract (title limits, 'Show HN:' prefix, no marketing superlatives), with grounded community-voice claims from HN/Reddit mention feeds and a distribution calendar. Unknown facts render as explicit [FILL:] slots — never invented. Citation-grounded JSON.",
			"inputSchema": socialToolSchema(),
		},
		{
			"name":        "gtm_launch",
			"description": "Read the launch loop (append-only launch ledger): `cohort` renders the weekly board (fill vs the ship-N-a-week target, per-product stage/score/verdict), `show` is a per-product drill-in (posts, signals, provenance), `verdict` computes the traction gate (DOUBLE-DOWN / ITERATE / KILL / TOO-EARLY / WATCH / NOT-DISTRIBUTED / UNMEASURED) from weighted signals where operator-observed conversions outrank community noise. NOT-DISTRIBUTED means the attempt was placed only on channels that reach no new audience (e.g. a release tag) — it is NOT a product verdict and must not be reported as one. UNMEASURED means it WAS distributed but the destination surface cannot see arrivals (`ndev endpoints analytics`), so the score measures our instrumentation rather than demand — also not a product verdict; report it as a measurement gap, never as failed demand. `channels` lists the typed channel registry. Read-only; recording events stays on the CLI (`ngtm launch plan/kit/posted/signal/signals/price-test --record`).",
			"inputSchema": launchToolSchema(),
		},
		{
			"name":        "gtm_ideate",
			"description": "Ideate new products in a space: mine HN/Reddit pain evidence into grounded Demand Signals, cross it with buildable product archetypes (CLI/menu-bar/realtime-web/metered-API/agent-service, each mapped to a factory scaffold lane), and return ranked idea cards (pitch/ICP/why-now/demand score) plus the Build Order feeding the scaffold→launch-cohort pipeline. Demand claims are grounded; ideas are inferred/speculative — never presented as validated. Args: subject (the space), keywords, count (default 5), avoid (comma-separated existing products).",
			"inputSchema": ideateToolSchema(),
		},
		{
			"name":        "gtm_compare",
			"description": "GTM competitor teardown: gather business-vertical evidence across a competitor set and emit one grounded table (firmographics + mentions; H1/pricing when SERP/landing feeds run) plus corpus-claim checks (confirmed/contradicted/unverified). Provide `subjects`, or a `claims` source to auto-derive them. With `rewrite:true` and a markdown `claims` corpus, returns the corpus annotated inline with verdicts. Citation-grounded.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"subjects": map[string]any{"type": "string", "description": "Comma-separated competitor set; optional when `claims` supplies competitors"},
					"category": map[string]any{"type": "string", "description": "Disambiguation hint for entity resolution (e.g. \"developer tools\")"},
					"tier":     map[string]any{"type": "string", "description": "Feed tier: free | cheap | premium | all | none", "default": "free"},
					"offline":  map[string]any{"type": "boolean", "description": "Hermetic: no network (fixtures only)"},
					"claims":   map[string]any{"type": "string", "description": "Path to a claims source (.yaml/.json/.md) to verify; defaults to the embedded 06-02 set. Competitors are auto-derived when `subjects` is omitted."},
					"rewrite":  map[string]any{"type": "boolean", "description": "Return the markdown `claims` corpus annotated inline with verdicts + sources, instead of the JSON report"},
					"since":    map[string]any{"type": "string", "description": "Path to a prior watch-state file: diff this run's verdicts against it, update it, and return a drift report (flips/new/removed)"},
				},
			},
		},
		{
			"name":        "gtm_landing",
			"description": "Publish stage: compose the research verticals into a self-contained, conversion-shaped HTML sales page (hero + CTA, value cards, good-better-best pricing with per-tier buy buttons, grounding caveat, provenance footer). Pricing tiers come from the value-based model (next_best_price/diff_value/value_capture), an explicit `tiers` array, or a single `price`(+`one_time`); hero copy comes from headline/tagline overrides or, with `brand:true`, the brand vertical. Returns HTML (default) or the page model JSON (`format:json`).",
			"inputSchema": landingToolSchema(),
		},
		{
			"name":        "gtm_design",
			"description": "Generate an accessible, theory-grounded design system for a brand/product: an OKLCH-derived color palette (semantic tokens for bg/surface/border/fg/muted/primary/accent/state colors, in dark and light), WCAG-checked contrast that passes AA by construction, and modular type & spacing scales. Scores the system /10 against a computable rubric (accessibility, harmony, neutral-ramp evenness, state coverage, color discipline, type/spacing, cohesion). With `tune:true` it runs a deterministic self-review loop over the candidate grid (harmony × hue × ratio) and returns the best-scoring system. Returns JSON: theme tokens + scorecard + a ready-to-paste CSS `:root` block. Args: subject (required), seed (#hex, optional), harmony (analogous|complementary|triadic|split|mono), mode (dark|light), tune (bool).",
			"inputSchema": designToolSchema(),
		},
		{
			"name":        "gtm_feeds",
			"description": "List the GTM data feeds and whether each is available on this machine (free feeds are always on; cheap feeds require their API key).",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]any{},
			},
		},
	}
}

func handleRPC(req rpcRequest) (rpcResponse, bool) {
	// Notifications carry no id and expect no response.
	if len(req.ID) == 0 {
		return rpcResponse{}, false
	}
	base := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ngtm", "version": Version},
		}
	case "ping":
		base.Result = map[string]any{}
	case "tools/list":
		base.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		text, isErr := callMCPTool(req.Params)
		base.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		}
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return base, true
}

// callMCPTool dispatches a tools/call. Returns (text, isError).
func callMCPTool(params json.RawMessage) (string, bool) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "invalid params: " + err.Error(), true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	switch p.Name {
	case "gtm_seo":
		return runVerticalMCP(ctx, "seo", p.Arguments)
	case "gtm_seo_research":
		return runSEOLifecycleMCP("research", p.Arguments)
	case "gtm_seo_opportunities":
		return runSEOLifecycleMCP("opportunities", p.Arguments)
	case "gtm_seo_brief":
		return runSEOLifecycleMCP("brief", p.Arguments)
	case "gtm_seo_publish":
		return runSEOLifecycleMCP("publish", p.Arguments)
	case "gtm_seo_measure":
		return runSEOLifecycleMCP("measure", p.Arguments)
	case "gtm_seo_retro":
		return runSEOLifecycleMCP("retro", p.Arguments)
	case "gtm_seo_audit":
		return runSEOLifecycleMCP("audit", p.Arguments)
	case "gtm_seo_eval":
		return runSEOLifecycleMCP("eval", p.Arguments)
	case "gtm_business":
		return runVerticalMCP(ctx, "business", p.Arguments)
	case "gtm_brand":
		return runVerticalMCP(ctx, "brand", p.Arguments)
	case "gtm_economics":
		return runVerticalMCP(ctx, "economics", p.Arguments)
	case "gtm_pricing":
		return runVerticalMCP(ctx, "pricing", p.Arguments)
	case "gtm_motion":
		return runVerticalMCP(ctx, "motion", p.Arguments)
	case "gtm_social":
		return runVerticalMCP(ctx, "social", p.Arguments)
	case "gtm_ideate":
		return runVerticalMCP(ctx, "ideate", p.Arguments)
	case "gtm_launch":
		return runLaunchMCP(p.Arguments)
	case "gtm_compare":
		return runCompareMCP(ctx, p.Arguments)
	case "gtm_design":
		return runDesignMCP(ctx, p.Arguments)
	case "gtm_landing":
		return runLandingMCP(ctx, p.Arguments)

	case "gtm_feeds":
		eng, err := gtm.NewEngine(gtm.Options{Subject: "_", Offline: true}, time.Now)
		if err != nil {
			return err.Error(), true
		}
		type feedRow struct {
			Name      string `json:"name"`
			Tier      string `json:"tier"`
			KeyEnv    string `json:"key_env,omitempty"`
			Available bool   `json:"available"`
		}
		var rows []feedRow
		for _, f := range eng.Registry().Feeds() {
			rows = append(rows, feedRow{f.Name(), string(f.Tier()), f.KeyEnv(), f.Available()})
		}
		b, _ := json.MarshalIndent(map[string]any{"feeds": rows}, "", "  ")
		return string(b), false

	default:
		return "unknown tool: " + p.Name, true
	}
}

func runSEOLifecycleMCP(verb string, args map[string]any) (string, bool) {
	cliArgs := []string{verb}
	if product, _ := args["product"].(string); strings.TrimSpace(product) != "" && verb != "eval" {
		cliArgs = append(cliArgs, product)
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		if key != "product" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := "--" + strings.ReplaceAll(key, "_", "-")
		switch value := args[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				cliArgs = append(cliArgs, name, value)
			}
		case bool:
			if value {
				cliArgs = append(cliArgs, name)
			}
		case float64:
			cliArgs = append(cliArgs, name, fmt.Sprintf("%v", value))
		}
	}
	cliArgs = append(cliArgs, "--json")
	var stdout, stderr bytes.Buffer
	code := cmdSEO("ngtm", cliArgs, &stdout, &stderr)
	if code != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			message = fmt.Sprintf("ngtm seo %s exited %d", verb, code)
		}
		return message, true
	}
	return strings.TrimSpace(stdout.String()), false
}

// runCompareMCP runs the competitor teardown from MCP arguments.
func runCompareMCP(ctx context.Context, args map[string]any) (string, bool) {
	subjectsStr, _ := args["subjects"].(string)
	offline, _ := args["offline"].(bool)
	rewrite, _ := args["rewrite"].(bool)
	tierStr, _ := args["tier"].(string)
	if tierStr == "" {
		tierStr = "free"
	}
	tiers, noFeeds, err := parseTiers(tierStr, false)
	if err != nil {
		return err.Error(), true
	}
	category, _ := args["category"].(string)
	opts := gtm.Options{Tiers: tiers, Offline: offline, NoFeeds: noFeeds || offline, Category: strings.TrimSpace(category)}
	subjects := splitCSV(subjectsStr)
	corpusMd := ""
	if claimsPath, _ := args["claims"].(string); strings.TrimSpace(claimsPath) != "" {
		cs, err := gtm.LoadClaims(claimsPath)
		if err != nil {
			return err.Error(), true
		}
		opts.Claims = cs.Claims
		if len(subjects) == 0 {
			subjects = cs.Subjects // auto-derive
		}
		if rewrite && gtm.IsMarkdownClaims(claimsPath) {
			raw, err := os.ReadFile(claimsPath)
			if err != nil {
				return err.Error(), true
			}
			corpusMd = string(raw)
		}
	}
	if len(subjects) == 0 {
		return "subjects is required (or a claims source containing competitors)", true
	}
	eng, err := gtm.NewEngine(opts, time.Now)
	if err != nil {
		return err.Error(), true
	}
	rep, err := eng.Compare(ctx, subjects, opts)
	if err != nil {
		return err.Error(), true
	}
	// Drift detection against a prior state file (`since`).
	if since, _ := args["since"].(string); strings.TrimSpace(since) != "" {
		prev, err := gtm.LoadWatchState(since)
		if err != nil {
			return err.Error(), true
		}
		cur := gtm.StateFromReport(rep)
		drift := gtm.DiffStates(prev, cur)
		if err := gtm.SaveWatchState(since, cur); err != nil {
			return err.Error(), true
		}
		return drift.Markdown(), false
	}
	if rewrite {
		if corpusMd != "" {
			return gtm.AnnotateCorpus(corpusMd, rep), false
		}
		return rep.Markdown(), false
	}
	b, err := rep.JSON()
	if err != nil {
		return err.Error(), true
	}
	return string(b), false
}

// runLandingMCP builds and renders a landing page from MCP arguments. Mirrors
// cmdLanding: explicit tiers > single price > value-model derivation; hero
// overrides win, with the brand vertical filling blanks when `brand:true`.
func runLandingMCP(ctx context.Context, args map[string]any) (string, bool) {
	subject, _ := args["subject"].(string)
	if strings.TrimSpace(subject) == "" {
		return "subject is required", true
	}
	str := func(k string) string { s, _ := args[k].(string); return strings.TrimSpace(s) }
	buyURL := str("buy_url")

	// Pricing-derivation inputs (only the supplied ones, preserving provenance).
	inputs := map[string]float64{}
	for argKey, inKey := range map[string]string{
		"next_best_price": "next_best_price", "diff_value": "diff_value", "value_capture": "value_capture",
	} {
		if v, ok := args[argKey]; ok {
			if f, ok := toFloat(v); ok {
				inputs[inKey] = f
			}
		}
	}
	opts := gtm.Options{Subject: subject, Inputs: inputs}

	cfg := gtm.LandingConfig{
		Product:      str("product"),
		Headline:     str("headline"),
		Subhead:      str("tagline"),
		Badge:        str("badge"),
		HeroCTALabel: str("cta"),
		HeroCTAURL:   buyURL,
		Grounding:    str("grounding"),
		FeaturesHead: str("features_head"),
		PricingHead:  str("pricing_head"),
		Features:     landingFeaturesFromArgs(args["features"]),
	}
	if tiers := landingTiersFromArgs(args["tiers"], buyURL); len(tiers) > 0 {
		cfg.Tiers = tiers
	} else if v, ok := args["price"]; ok {
		if price, ok := toFloat(v); ok && price > 0 {
			period := "/mo"
			if b, _ := args["one_time"].(bool); b {
				period = "one-time"
			}
			name := str("tier_name")
			if name == "" {
				name = "Pro"
			}
			cfg.Tiers = []gtm.LandingTier{{Name: name, Price: fmtPrice(price), Period: period, CTAURL: buyURL, Featured: true}}
		}
	}

	provLabel := "offline"
	var warnings []string
	if b, _ := args["brand"].(bool); b {
		eng, err := gtm.NewEngine(opts, time.Now)
		if err != nil {
			return err.Error(), true
		}
		if rep, err := eng.Run(ctx, "brand", opts); err != nil {
			warnings = append(warnings, "brand copy unavailable: "+err.Error())
		} else {
			provLabel = rep.Provider
			hl, sh, _, bullets := gtm.LandingCopyFromReport(rep)
			if cfg.Headline == "" {
				cfg.Headline = hl
			}
			if cfg.Subhead == "" {
				cfg.Subhead = sh
			}
			if len(cfg.Features) == 0 {
				cfg.Features = featuresFromBullets(bullets)
			}
			warnings = append(warnings, rep.Warnings...)
		}
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	page := gtm.BuildLandingFromConfig(cfg, opts, now, provLabel)
	page.Warnings = append(page.Warnings, warnings...)

	if strings.EqualFold(str("format"), "json") {
		b, err := page.JSON()
		if err != nil {
			return err.Error(), true
		}
		return string(b), false
	}
	return gtm.RenderLandingHTML(page), false
}

// landingFeaturesFromArgs converts a JSON array of {title, body} into cards.
func landingFeaturesFromArgs(v any) []gtm.LandingFeature {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []gtm.LandingFeature
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		title, _ := m["title"].(string)
		body, _ := m["body"].(string)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
			continue
		}
		out = append(out, gtm.LandingFeature{Title: strings.TrimSpace(title), Body: strings.TrimSpace(body)})
	}
	return out
}

// landingTiersFromArgs converts a JSON array of tier objects into LandingTiers.
func landingTiersFromArgs(v any, defaultURL string) []gtm.LandingTier {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []gtm.LandingTier
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		s := func(k string) string { x, _ := m[k].(string); return strings.TrimSpace(x) }
		t := gtm.LandingTier{
			Name:     s("name"),
			Price:    s("price"),
			Period:   s("period"),
			Note:     s("note"),
			CTAURL:   firstNonBlank(s("cta_url"), defaultURL),
			CTALabel: s("cta_label"),
		}
		if b, _ := m["featured"].(bool); b {
			t.Featured = true
		}
		if feats, ok := m["features"].([]any); ok {
			for _, f := range feats {
				if fs, ok := f.(string); ok && strings.TrimSpace(fs) != "" {
					t.Features = append(t.Features, strings.TrimSpace(fs))
				}
			}
		}
		out = append(out, t)
	}
	return out
}

// modelInputsFromArgs extracts the numeric model assumptions an operator
// supplied via MCP args (JSON numbers arrive as float64). Only present keys are
// returned, preserving the provided-vs-default provenance.
func modelInputsFromArgs(args map[string]any) map[string]float64 {
	var out map[string]float64
	for _, k := range modelInputKeys {
		v, ok := args[k]
		if !ok {
			continue
		}
		f, ok := toFloat(v)
		if !ok {
			continue
		}
		if out == nil {
			out = map[string]float64{}
		}
		out[k] = f
	}
	return out
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// runVerticalMCP runs any vertical from MCP arguments and returns its JSON report.
func runVerticalMCP(ctx context.Context, vertical string, args map[string]any) (string, bool) {
	subject, _ := args["subject"].(string)
	if subject == "" {
		return "subject is required", true
	}
	offline, _ := args["offline"].(bool)
	tierStr, _ := args["tier"].(string)
	if tierStr == "" {
		tierStr = "free"
	}
	tiers, noFeeds, err := parseTiers(tierStr, false)
	if err != nil {
		return err.Error(), true
	}
	kw, _ := args["keywords"].(string)
	category, _ := args["category"].(string)
	pitch, _ := args["pitch"].(string)
	chans, _ := args["channels"].(string)
	avoid, _ := args["avoid"].(string)
	opts := gtm.Options{
		Subject: subject, Keywords: splitCSV(kw), Category: strings.TrimSpace(category), Tiers: tiers,
		Offline: offline, NoFeeds: noFeeds || offline,
		Inputs: modelInputsFromArgs(args),
		Pitch:  strings.TrimSpace(pitch), Channels: splitCSV(chans),
		Avoid: splitCSV(avoid),
	}
	if v, ok := toFloat(args["count"]); ok && v > 0 {
		opts.IdeaCount = int(v)
	}
	if b, _ := args["tune"].(bool); b {
		opts.Tune = true
	}
	eng, err := gtm.NewEngine(opts, time.Now)
	if err != nil {
		return err.Error(), true
	}
	report, err := eng.Run(ctx, vertical, opts)
	if err != nil {
		return err.Error(), true
	}
	// Drift detection against a prior state (`since`): status + metric deltas.
	if since, _ := args["since"].(string); strings.TrimSpace(since) != "" {
		prev, err := gtm.LoadWatchState(since)
		if err != nil {
			return err.Error(), true
		}
		cur := gtm.StateFromVertical(report)
		drift := gtm.DiffStates(prev, cur)
		if err := gtm.SaveWatchState(since, cur); err != nil {
			return err.Error(), true
		}
		return drift.Markdown(), false
	}
	b, err := report.JSON()
	if err != nil {
		return err.Error(), true
	}
	return string(b), false
}

// ideateToolSchema is verticalToolSchema plus the idea-slate knobs.
func ideateToolSchema() map[string]any {
	s := verticalToolSchema()
	props := schemaProperties(s)
	props["count"] = map[string]any{"type": "number", "description": "Idea slate size (default 5)"}
	props["avoid"] = map[string]any{"type": "string", "description": "Comma-separated existing product names the slate must not re-propose"}
	return s
}

// socialToolSchema is verticalToolSchema plus the distribution-content knobs.
func socialToolSchema() map[string]any {
	s := verticalToolSchema()
	props := schemaProperties(s)
	props["pitch"] = map[string]any{"type": "string", "description": "One-line value proposition the drafts build around (blank renders an explicit [FILL:] slot, never an invented benefit)"}
	props["channels"] = map[string]any{"type": "string", "description": "Comma-separated channel keys: show-hn,producthunt,reddit,x,linkedin,indiehackers (default: all)"}
	return s
}

// schemaProperties quarantines the untyped JSON Schema boundary. A future
// refactor of verticalToolSchema cannot turn tools/list into a process panic.
func schemaProperties(schema map[string]any) map[string]any {
	if properties, ok := schema["properties"].(map[string]any); ok {
		return properties
	}
	properties := map[string]any{}
	schema["properties"] = properties
	return properties
}

// launchToolSchema is the read-only launch-loop input contract.
func launchToolSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "description": "cohort | show | verdict | retro | audit | channels", "default": "cohort"},
			"product": map[string]any{"type": "string", "description": "Product slug (required for show/verdict)"},
			"week":    map[string]any{"type": "string", "description": "Filter the board to one ISO week, e.g. 2026-W24 (cohort)"},
			"target":  map[string]any{"type": "number", "description": "Ship-N-a-week goal the cohort fill is measured against (default 20)"},
			"ledger":  map[string]any{"type": "string", "description": "Ledger path override (default $NGTM_LAUNCH_LEDGER or ~/.nicos-dev/gtm/launch-ledger.jsonl)"},
		},
	}
}

// runLaunchMCP serves the read-only launch-loop views over the ledger.
func runLaunchMCP(args map[string]any) (string, bool) {
	action, _ := args["action"].(string)
	if strings.TrimSpace(action) == "" {
		action = "cohort"
	}
	if action == "channels" {
		b, _ := json.MarshalIndent(map[string]any{"channels": gtm.Channels}, "", "  ")
		return string(b), false
	}
	ledger, _ := args["ledger"].(string)
	if strings.TrimSpace(ledger) == "" {
		ledger = gtm.DefaultLaunchLedgerPath()
	}
	if action == "audit" {
		report, err := (gtm.LaunchLedger{Path: ledger}).ReadWithIssues()
		if err != nil {
			return err.Error(), true
		}
		b, _ := json.MarshalIndent(map[string]any{
			"ledger": ledger, "events": len(report.Events), "corrupt_rows": len(report.Issues),
			"anomalies": gtm.AuditLaunchLedgerRead(report, time.Now()),
		}, "", "  ")
		return string(b), false
	}
	events, err := (gtm.LaunchLedger{Path: ledger}).Read()
	if err != nil {
		return err.Error(), true
	}
	launches := gtm.BuildLaunchesWithCoverage(events, time.Now(), launchCoverage())
	switch action {
	case "retro":
		week, _ := args["week"].(string)
		target := 20
		if v, ok := toFloat(args["target"]); ok && v > 0 {
			target = int(v)
		}
		b, _ := json.MarshalIndent(gtm.BuildRetro(launches, strings.TrimSpace(week), target), "", "  ")
		return string(b), false
	case "cohort":
		target := 20
		if v, ok := toFloat(args["target"]); ok && v > 0 {
			target = int(v)
		}
		cohorts := gtm.BuildCohorts(launches, target)
		if week, _ := args["week"].(string); strings.TrimSpace(week) != "" {
			var filtered []gtm.LaunchCohort
			for _, c := range cohorts {
				if c.Week == strings.TrimSpace(week) {
					filtered = append(filtered, c)
				}
			}
			cohorts = filtered
		}
		b, _ := json.MarshalIndent(map[string]any{"ledger": ledger, "cohorts": cohorts}, "", "  ")
		return string(b), false
	case "show", "verdict":
		product, _ := args["product"].(string)
		if strings.TrimSpace(product) == "" {
			return "product is required for " + action, true
		}
		for _, p := range launches {
			if strings.EqualFold(p.Product, strings.TrimSpace(product)) {
				b, _ := json.MarshalIndent(p, "", "  ")
				return string(b), false
			}
		}
		return fmt.Sprintf("no ledger events for %q (ledger %s)", product, ledger), true
	default:
		return "unknown action " + action + " (use cohort|show|verdict|retro|audit|channels)", true
	}
}
