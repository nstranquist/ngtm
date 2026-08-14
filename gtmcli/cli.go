// Package gtmcli is the shared command surface for the GTM factory. Both the
// `ndev gtm` domain and the standalone `ngtm` peer binary are thin wrappers
// over Dispatch, so the two surfaces never drift.
package gtmcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nstranquist/ngtm/gtm"
	"github.com/nstranquist/ngtm/internal/jsonl"
)

// Version of the GTM factory surface (bump on schema-affecting changes).
const Version = "0.4.0"

// Dispatch runs one gtm subcommand. prog is the program label used in usage
// text ("ndev gtm" or "ngtm"). Returns a process exit code.
func Dispatch(prog string, args []string, out, errOut io.Writer) int {
	family, args := peelLeadingFamilyFlags(args)
	if len(args) == 0 {
		printUsage(prog, out)
		return 0
	}
	if family.offline && !verbAcceptsOffline(args[0], args[1:]) {
		_, _ = fmt.Fprintf(errOut, "gtm: --offline is not valid for %q\n", args[0])
		return 2
	}
	args = attachFamilyFlags(args[0], family, args)
	switch args[0] {
	case "social":
		if len(args) > 1 && args[1] == "eval" {
			return cmdSocialEval(prog, args[2:], out, errOut)
		}
		return cmdVertical(prog, args[0], args[1:], out, errOut)
	case "seo":
		if len(args) > 1 && seoLifecycleVerbs[args[1]] {
			return cmdSEO(prog, args[1:], out, errOut)
		}
		return cmdVertical(prog, args[0], args[1:], out, errOut)
	case "business", "brand", "economics", "pricing", "motion", "ideate":
		return cmdVertical(prog, args[0], args[1:], out, errOut)
	case "launch":
		return cmdLaunch(prog, args[1:], out, errOut)
	case "landing":
		return cmdLanding(prog, args[1:], out, errOut)
	case "design":
		return cmdDesign(prog, args[1:], out, errOut)
	case "feeds":
		return cmdFeeds(args[1:], out, errOut)
	case "verticals":
		return cmdVerticals(args[1:], out)
	case "mcp":
		return cmdMCP(args[1:], os.Stdin, out, errOut)
	case "version", "--version":
		_, _ = fmt.Fprintln(out, Version)
		return 0
	case "help", "-h", "--help":
		printUsage(prog, out)
		return 0
	default:
		_, _ = fmt.Fprintf(errOut, "gtm: unknown subcommand %q\n\n", args[0])
		printUsage(prog, errOut)
		return 2
	}
}

// familyFlags are the ndev/nship-style leading flags agents put before the verb.
type familyFlags struct {
	json    bool
	offline bool
}

func peelLeadingFamilyFlags(args []string) (familyFlags, []string) {
	var f familyFlags
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--json":
			f.json = true
			i++
		case "--offline":
			f.offline = true
			i++
		default:
			return f, args[i:]
		}
	}
	return f, nil
}

func verbAcceptsJSON(verb string) bool {
	switch verb {
	case "social", "seo", "business", "brand", "economics", "pricing", "motion", "ideate",
		"launch", "landing", "design", "feeds":
		return true
	default:
		return false
	}
}

func verbAcceptsOffline(verb string, rest []string) bool {
	switch verb {
	case "social":
		return len(rest) == 0 || rest[0] != "eval"
	case "seo", "business", "brand", "economics", "pricing", "motion", "ideate", "landing", "design":
		return true
	case "launch":
		return len(rest) > 0 && rest[0] == "kit"
	default:
		return false
	}
}

func attachFamilyFlags(verb string, f familyFlags, args []string) []string {
	var extra []string
	if f.json && verbAcceptsJSON(verb) {
		extra = append(extra, "--json")
	}
	if f.offline && verbAcceptsOffline(verb, args[1:]) {
		extra = append(extra, "--offline")
	}
	if len(extra) == 0 {
		return args
	}
	out := make([]string, 0, len(args)+len(extra))
	out = append(out, args...)
	return append(out, extra...)
}

func printUsage(prog string, w io.Writer) {
	_, _ = fmt.Fprintf(w, `%[1]s — go-to-market factory (live data + LLM, citation-grounded)

USAGE
  %[1]s seo <subject> [flags]       Compact SEO & positioning vertical
  %[1]s seo <verb> ...              Evidence lifecycle: research → brief → publish → measure → retro → audit
  %[1]s business <subject> [flags]  Business-plan + SWOT + TAM/SAM/SOM (now incl. JTBD/VPC)
  %[1]s brand <subject> [flags]     Brand & assets (logo brief + landing copy)
  %[1]s economics <subject> [flags] Unit economics: LTV/CAC/payback/NRR + go/no-go gate + CFO panel
  %[1]s pricing <subject> [flags]   Value-based price + good-better-best tiers + Van Westendorp survey
  %[1]s motion <subject> [flags]    GTM motion by ACV + funnel/PQL + beachhead + PMF validation plan
  %[1]s social <product> [flags]    Distribution content: channel-native launch/social drafts, norm-linted + scored /10
  %[1]s social eval [flags]         Versioned golden quality gate with per-dimension thresholds
  %[1]s ideate <space> [flags]      Mine community pain into ranked, panel-gated product ideas (--count, --avoid)
  %[1]s launch <verb> ...           Launch loop: weekly cohorts + placement receipts + measured verdicts ("launch help")
  %[1]s landing <subject> [flags]   Publish: research → a self-contained HTML sales page (the last mile)
  %[1]s design <name> [flags]      Design system: OKLCH palette + WCAG contrast + type/spacing, scored /10
  %[1]s feeds [--json]              List data feeds and availability
  %[1]s feeds doctor [--json] [--probe-paid]
                                    Live-probe feeds + whether brand/seo can ground (+ the fix)
  %[1]s verticals                   List available analysis verticals
  %[1]s mcp                         Serve the factory as an MCP stdio server
  %[1]s version                     Print version

COMMON FLAGS
  --subject <s>      Subject (or pass positionally)
  --query <q>        Research question (defaults to subject)
  --keywords <list>  Comma-separated seed keywords
  --category <c>     Disambiguation hint for entity resolution (e.g. "developer tools") — brand/seo/business
  --tier <t>         free | cheap | premium | all | none   (default: free)
  --paid             Shorthand for --tier cheap (free + cheap)
  --provider <p>     LLM provider for narrative (ollama, openai, gemini, claude-code, ...)
  --model <m>        LLM model (defaults to provider default)
  --offline          Hermetic: no LLM, no network (fixtures only); may lead the verb
  --json             Emit JSON instead of Markdown; may lead the verb (ngtm --json feeds)
  --out <path>       Write the report to a file
  --compare <list>   Competitor set → one grounded teardown table (business)
  --claims/--rewrite/--watch/--fail-on   Compare-mode fact-check + drift detection

MODEL INPUTS (economics / pricing / motion — supplied values are "real", omitted ones use analyst defaults)
  --acv <$>            Annual contract value          --arpa <$>            Monthly revenue/account
  --gross-margin <0..1>  Gross margin                 --monthly-churn <0..1>  Monthly revenue churn
  --cac <$>            Fully-loaded CAC               --expansion <0..1>    Monthly net expansion (→ NRR)
  --customers <n>      ICP account count (bottom-up)  --penetration <0..1>  Near-term share (→ SOM)
  --next-best-price <$>  Alternative price (pricing)  --diff-value <$>      Value of differentiation
  --value-capture <0..1> Share of value to capture    --negatives <$>       Switching-cost value
  --one-time             Model a one-time purchase (no churn/NRR; gate on margin:CAC) [economics]

CLASSICAL INVESTOR / SHARK-TANK METRICS (economics — computed when supplied)
  --growth-rate <f> + --profit-margin <f>   → Rule of 40 (growth%% + margin%% >= 40)
  --net-burn <$> + --net-new-arr <$>        → Burn Multiple (<1 great, >2 poor)
  --net-new-arr <$> + --sm-spend <$>        → Magic Number (>0.75 efficient)
  --gained-arr <$> + --lost-arr <$>         → SaaS Quick Ratio (>4 great)

EXAMPLES
  %[1]s economics nvault --acv 30000 --cac 9000 --gross-margin 0.85 --monthly-churn 0.02 --expansion 0.02
  %[1]s economics nvault --acv 30000 --cac 9000 --customers 12000 --penetration 0.02   # + bottom-up SAM/SOM
  %[1]s economics nvault --acv 30000 --cac 9000 --growth-rate 1.5 --profit-margin -0.2 --net-burn 800000 --net-new-arr 1200000 --sm-spend 900000   # + Rule of 40 / burn multiple / magic number
  %[1]s economics cadence --acv 39 --cac 8 --gross-margin 0.92 --one-time             # one-time SKU
  %[1]s pricing nvault --next-best-price 21000 --diff-value 15000 --value-capture 0.5
  %[1]s motion nvault --acv 30000                                                       # → sales-led + funnel + PMF plan
  %[1]s business nvault --paid                                                          # SWOT + JTBD/VPC + shark-tank
  %[1]s landing cadence --product "Cadence" --price 39 --one-time --buy-url https://… --out docs/human/cadence.html
  %[1]s design garrid --tune --seed "#3b82f6" --both --out docs/human/garrid-design.html   # best-scoring system + preview

SOCIAL FLAGS (channel-native distribution content)
  --pitch <s>        One-line value proposition the drafts build around (blank → explicit [FILL:] slot)
  --channels <list>  Channel keys: show-hn,producthunt,reddit,x,linkedin,indiehackers (default: all)
  --tune             Self-review loop: best-scoring hook archetype per channel (scorecard always included)
  Drafts are linted against each channel's typed contract (title limits, "Show HN:" prefix,
  no marketing superlatives) and scored /10 (contract/grounding/specificity/completeness/cta);
  violations + score notes surface in the report. Unknown facts render as [FILL: …] slots —
  the factory never invents users, numbers, or testimonials.
  social eval [--fixture path] [--strict] runs the embedded golden-v1 contract and
  reports contract/grounding/specificity/completeness/CTA averages separately.

IDEATE FLAGS (new-product ideation, panel-gated)
  --count <n>        Idea slate size (default 5)        --avoid <list>  Existing products to skip
  Demand claims are grounded in HN/Reddit pain evidence; ideas are inferred/speculative, ranked
  by measured demand, each mapped to a factory scaffold lane + the build-order command path.

SEO PAGES (programmatic-SEO factory)
  %[1]s seo <product> --pages --keywords "kw1,kw2" --out-dir <dir> [--pitch s] [--buy-url u]
  One keyword-targeted landing page per keyword + an index, from the landing engine.
  Compatibility output is noindex by default. Use the guarded seo publish lifecycle for indexable pages.

SEO RESEARCH AUTOMATION
  %[1]s seo help
  Lifecycle: config → research → opportunities → brief → draft → approval → publish → measure → retro → audit.
  Strict lifecycle commands exit 3 on evidence or integrity blockers; artifacts are content-addressed.

LAUNCH LOOP (ship 20 a week, measure which lands)
  plan → kit → posted → signal/signals → cohort/show/verdict; append-only JSONL ledger.
  "%[1]s launch signals <p> --record" re-measures HN/Reddit for the LIVE product (observed),
  "%[1]s launch verdict <p>" gates it: DOUBLE-DOWN / ITERATE / KILL / TOO-EARLY / WATCH.
  Full reference: %[1]s launch help

DESIGN FLAGS (generate an accessible design system)
  --seed <#hex>        Brand color seed (derived from name when absent)
  --harmony <s>        analogous | complementary | triadic | split | mono   (default: analogous)
  --mode <m>           dark | light  (primary render mode; default: dark)   --both  Build both palettes
  --type-ratio <f>     Modular type scale ratio (1.2 | 1.25 | 1.333)
  --tune [--rounds N]  Self-review loop: score the candidate grid, keep the best (--audit <jsonl> for telemetry)
  --screenshot         Capture a PNG of the preview via headless Chrome (--screenshot-out <path>)
  --vision             Perceptual channel: screenshot → vision LLM scores it /10, blended with the rubric (needs ANTHROPIC_API_KEY)
  --css                Emit only the :root design-token block      --json  Emit tokens + scorecard as JSON
  --landing-css        Emit a :root block in the landing/garrid token vocabulary (to paste into a bespoke page)

LANDING FLAGS (publish a sales page)
  --product <name>     Display/brand name on the page (defaults to the subject)
  --headline <s>       Hero H1 line          --tagline <s>     Hero subhead/positioning
  --badge <s>          Eyebrow chip          --cta <label>     Primary button text (default "Get started")
  --buy-url <url>      Checkout/signup href the buttons point at
  --price <$>          Single headline price  --one-time        Render that price as one-time (else /mo)
  --tiers "<spec>"     Explicit tiers: Name|Price|Period|Note|f1~f2|ctaURL|featured|label, joined by ;;
  --features "<spec>"  Value cards: Title::Body, joined by ;;
  --design             Generate & apply an OKLCH design system as the page theme (--design-seed/--design-harmony/--design-tune)
  --by <name>          Footer attribution: "<Product> — by <name>" (default nicos-tools; use "garrid" for outward pages)
  --next-best-price/--diff-value/--value-capture   Derive good-better-best tiers from the value model
  --brand              Run the brand vertical for grounded hero copy (needs --provider/feeds)
  --out <path>         Write HTML to a file (default: stdout) · --json  Emit the page model instead
  --from <manifest>    PUBLISH FACTORY: batch-generate every page in a JSON app manifest (one file → all pages)
`, prog)
}

// logRun appends a dated line to ~/.nicos-dev/gtm/runs.jsonl so every GTM intel
// run is recorded with its timestamp, inputs, and verdict — a cheap ledger so a
// (potentially paid/LLM) run is never silently lost. Best-effort: any error is
// swallowed so logging can never break a run.
func logRun(fields map[string]any) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NGTM_RUNS_TELEMETRY"))) {
	case "0", "false", "off", "no":
		return
	}
	path := strings.TrimSpace(os.Getenv("NGTM_RUNS_TELEMETRY_PATH"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		path = filepath.Join(home, ".nicos-dev", "gtm", "runs.jsonl")
	}
	context := strings.TrimSpace(os.Getenv("NGTM_RUN_CONTEXT"))
	if context == "" {
		context = "operator"
	}
	fields["run_context"] = context
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	_ = jsonl.AppendLine(path, b, 0o644)
}

func cmdVertical(prog, vertical string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" "+vertical, flag.ContinueOnError)
	fs.SetOutput(errOut)
	var (
		subject       = fs.String("subject", "", "subject under analysis")
		query         = fs.String("query", "", "research question")
		keywords      = fs.String("keywords", "", "comma-separated seed keywords")
		category      = fs.String("category", "", "disambiguation hint for entity resolution (e.g. \"developer tools\")")
		tier          = fs.String("tier", "free", "feed tier: free|cheap|premium|all|none")
		paid          = fs.Bool("paid", false, "shorthand for --tier cheap")
		provider      = fs.String("provider", "", "LLM provider for narrative")
		model         = fs.String("model", "", "LLM model")
		offline       = fs.Bool("offline", false, "hermetic: no LLM, no network")
		limit         = fs.Int("limit", 0, "per-feed evidence cap")
		asJSON        = fs.Bool("json", false, "emit JSON")
		outPath       = fs.String("out", "", "write report to file")
		compare       = fs.String("compare", "", "comma-separated competitor set → grounded teardown table")
		pitch         = fs.String("pitch", "", "one-line value proposition for social drafts [social]")
		channels      = fs.String("channels", "", "comma-separated channel keys for social drafts (default: all) [social]")
		tune          = fs.Bool("tune", false, "self-review loop: try each hook archetype per channel, keep the best-scoring [social]")
		pages         = fs.Bool("pages", false, "programmatic-SEO factory: one landing page per --keywords entry [seo]")
		strictSEO     = fs.Bool("strict", false, "fail closed (exit 3) when SEO lacks live SERP or volume evidence [seo]")
		requireSERP   = fs.Bool("require-serp", false, "exit 3 when SEO lacks non-synthetic SERP evidence [seo]")
		requireVolume = fs.Bool("require-volume", false, "exit 3 when SEO lacks non-synthetic search-volume evidence [seo]")
		count         = fs.Int("count", 5, "idea slate size [ideate]")
		avoid         = fs.String("avoid", "", "comma-separated existing product names to skip [ideate]")
		outDir        = fs.String("out-dir", "", "output directory for --pages [seo]")
		buyURL        = fs.String("buy-url", "", "CTA href for --pages [seo]")
		claims        = fs.String("claims", "", "competitor-claims source (.yaml/.json/.md) — overrides the embedded 06-02 set")
		rewrite       = fs.String("rewrite", "", "annotate the --claims markdown corpus with verdicts → write to this path")
		watch         = fs.String("watch", "", "drift-detect: diff verdicts against this state file, report flips, and update it")
		failOn        = fs.String("fail-on", "regression", "exit non-zero + alert on: regression|any|none")
		// Numeric model inputs for the model-driven verticals (economics/pricing/
		// motion). Only the ones the operator actually sets are threaded into
		// Options.Inputs (via fs.Visit) so the model can tell a real assumption
		// from an analyst default — that provenance is the anti-hallucination
		// contract for computed numbers.
		fACV        = fs.Float64("acv", 0, "annual contract value ($) [economics/motion]")
		fARPA       = fs.Float64("arpa", 0, "monthly revenue per account ($) [economics]")
		fGrossM     = fs.Float64("gross-margin", 0, "gross margin 0..1 [economics]")
		fChurn      = fs.Float64("monthly-churn", 0, "monthly revenue churn 0..1 [economics]")
		fCAC        = fs.Float64("cac", 0, "fully-loaded CAC ($) [economics]")
		fExpansion  = fs.Float64("expansion", 0, "monthly net expansion 0..1 [economics]")
		fCustomers  = fs.Float64("customers", 0, "# addressable ICP accounts [economics bottom-up sizing]")
		fPenetrate  = fs.Float64("penetration", 0, "realistic near-term share 0..1 [economics SOM]")
		fNextBest   = fs.Float64("next-best-price", 0, "next-best alternative price ($) [pricing]")
		fDiffValue  = fs.Float64("diff-value", 0, "quantified value of differentiation ($) [pricing]")
		fValCapture = fs.Float64("value-capture", 0, "share of differentiation value to capture 0..1 [pricing]")
		fNegatives  = fs.Float64("negatives", 0, "switching-cost/negative value ($) [pricing]")
		// Classical investor / shark-tank metrics (economics; computed when supplied).
		fGrowthRate = fs.Float64("growth-rate", 0, "annual revenue growth fraction (1.0=100%) → Rule of 40 [economics]")
		fProfitMrgn = fs.Float64("profit-margin", 0, "operating/FCF margin fraction (-0.2=-20%) → Rule of 40 [economics]")
		fNetNewARR  = fs.Float64("net-new-arr", 0, "net new ARR in period ($) → Burn Multiple / Magic Number [economics]")
		fNetBurn    = fs.Float64("net-burn", 0, "net cash burned in period ($) → Burn Multiple [economics]")
		fSMSpend    = fs.Float64("sm-spend", 0, "sales & marketing spend in period ($) → Magic Number [economics]")
		fGainedARR  = fs.Float64("gained-arr", 0, "new+expansion ARR ($) → SaaS Quick Ratio [economics]")
		fLostARR    = fs.Float64("lost-arr", 0, "churned+contraction ARR ($) → SaaS Quick Ratio [economics]")
		fOneTime    = fs.Bool("one-time", false, "model as a one-time purchase (no churn/NRR; gate on margin:CAC) [economics]")
	)
	inputFlagKeys := map[string]*float64{
		"acv": fACV, "arpa": fARPA, "gross-margin": fGrossM, "monthly-churn": fChurn,
		"cac": fCAC, "expansion": fExpansion, "customers": fCustomers, "penetration": fPenetrate,
		"next-best-price": fNextBest, "diff-value": fDiffValue, "value-capture": fValCapture, "negatives": fNegatives,
		"growth-rate": fGrowthRate, "profit-margin": fProfitMrgn, "net-new-arr": fNetNewARR,
		"net-burn": fNetBurn, "sm-spend": fSMSpend, "gained-arr": fGainedARR, "lost-arr": fLostARR,
	}
	// Go's flag package stops at the first non-flag token, so pull any leading
	// positional subject words off the front before parsing the flags.
	var positional []string
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		positional = append(positional, args[i])
		i++
	}
	if err := fs.Parse(args[i:]); err != nil {
		return 2
	}
	// Collect only the model inputs the operator explicitly set, mapping the
	// dashed flag name to the underscore input key.
	var modelInputs map[string]float64
	fs.Visit(func(f *flag.Flag) {
		if p, ok := inputFlagKeys[f.Name]; ok {
			if modelInputs == nil {
				modelInputs = map[string]float64{}
			}
			modelInputs[strings.ReplaceAll(f.Name, "-", "_")] = *p
		}
	})
	if *fOneTime {
		if modelInputs == nil {
			modelInputs = map[string]float64{}
		}
		modelInputs["one_time"] = 1
	}
	subj := strings.TrimSpace(*subject)
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(positional, " "))
	}
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	tiers, noFeeds, err := parseTiers(*tier, *paid)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 2
	}

	// Compare mode: a grounded teardown across a competitor set. Entered when
	// --compare or --claims is given. (--watch alone routes to the single-subject
	// vertical below.)
	if strings.TrimSpace(*compare) != "" || strings.TrimSpace(*claims) != "" {
		subjects := splitCSV(*compare)
		if subj != "" {
			subjects = append([]string{subj}, subjects...)
		}
		opts := gtm.Options{
			Keywords: splitCSV(*keywords), Category: strings.TrimSpace(*category), Tiers: tiers,
			Offline: *offline, NoFeeds: noFeeds || *offline, Limit: *limit,
		}
		corpusMd := ""
		if strings.TrimSpace(*claims) != "" {
			cs, err := gtm.LoadClaims(*claims)
			if err != nil {
				_, _ = fmt.Fprintln(errOut, "gtm:", err)
				return 1
			}
			opts.Claims = cs.Claims
			if len(subjects) == 0 {
				subjects = cs.Subjects // (1) auto-derive the set from the doc
			}
			if strings.TrimSpace(*rewrite) != "" && gtm.IsMarkdownClaims(*claims) {
				raw, err := os.ReadFile(*claims)
				if err != nil {
					_, _ = fmt.Fprintln(errOut, "gtm:", err)
					return 1
				}
				corpusMd = string(raw)
			}
		}
		if strings.TrimSpace(*rewrite) != "" && strings.TrimSpace(*claims) == "" {
			_, _ = fmt.Fprintln(errOut, "gtm: --rewrite needs --claims <corpus.md>")
			return 2
		}
		if len(subjects) == 0 {
			_, _ = fmt.Fprintln(errOut, "gtm: no competitors — pass --compare A,B,C or a --claims source containing competitors")
			return 2
		}
		return runCompare(subjects, opts, *asJSON, *outPath, *rewrite, *watch, *failOn, corpusMd, out, errOut)
	}

	if subj == "" {
		_, _ = fmt.Fprintln(errOut, "gtm "+vertical+": subject is required (positionally or via --subject)")
		return 2
	}

	// Programmatic-SEO factory: one keyword → one generated landing page.
	if vertical == "seo" && *pages {
		return runSEOPages(prog, subj, splitCSV(*keywords), strings.TrimSpace(*pitch), strings.TrimSpace(*buyURL), strings.TrimSpace(*outDir), out, errOut)
	}

	opts := gtm.Options{
		Subject:   subj,
		Query:     strings.TrimSpace(*query),
		Keywords:  splitCSV(*keywords),
		Category:  strings.TrimSpace(*category),
		Tiers:     tiers,
		Provider:  strings.TrimSpace(*provider),
		Model:     strings.TrimSpace(*model),
		Offline:   *offline,
		NoFeeds:   noFeeds || *offline,
		Limit:     *limit,
		Inputs:    modelInputs,
		Pitch:     strings.TrimSpace(*pitch),
		Channels:  splitCSV(*channels),
		Tune:      *tune,
		IdeaCount: *count,
		Avoid:     splitCSV(*avoid),
	}

	eng, err := gtm.NewEngine(opts, time.Now)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	report, err := eng.Run(ctx, vertical, opts)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	strictExit := 0
	if vertical == "seo" {
		serp, volume, live := legacySEOEvidenceCoverage(report)
		report.SetMetric("seo_live_evidence", live)
		report.SetMetric("seo_serp_coverage", serp)
		report.SetMetric("seo_volume_coverage", volume)
		wantSERP := *requireSERP || *strictSEO
		wantVolume := *requireVolume || *strictSEO
		if (wantSERP && serp == 0) || (wantVolume && volume == 0) {
			strictExit = 3
			report.Warnings = append(report.Warnings, fmt.Sprintf("strict SEO evidence gate failed: serp=%.0f volume=%.0f", serp, volume))
		}
	}

	// Ledger the run (dated, best-effort) so no intel run is silently lost.
	panelMedian := -1.0
	if report.Panel != nil {
		panelMedian = report.Panel.MedianScore
	}
	logRun(map[string]any{
		"ts": report.Generated, "surface": prog, "vertical": vertical, "subject": subj,
		"tiers": joinFeedTiers(report.Tiers), "provider": report.Provider,
		"verdict": report.Verdict, "panel_median": panelMedian, "out": *outPath,
		"metrics": report.Metrics, // social scorecard etc. — regressable telemetry
	})

	// Drift detection on a single-subject vertical: track claim status (grounded
	// /inferred/speculative) + metrics (SERP rank, volume, mentions) over time.
	if strings.TrimSpace(*watch) != "" {
		return emitDrift(*watch, *failOn, gtm.StateFromVertical(report), *asJSON, *outPath, out, errOut)
	}

	var rendered []byte
	if *asJSON {
		rendered, err = report.JSON()
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
	} else {
		rendered = []byte(report.Markdown())
	}

	if *outPath != "" {
		if err := os.WriteFile(*outPath, rendered, 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes)\n", *outPath, len(rendered))
		return strictExit
	}
	_, _ = out.Write(rendered)
	if !*asJSON {
		_, _ = fmt.Fprintln(out)
	}
	return strictExit
}

func legacySEOEvidenceCoverage(report *gtm.Report) (serp, volume, live float64) {
	if report == nil || len(report.Evidence) == 0 {
		return 0, 0, 0
	}
	for _, ev := range report.Evidence {
		if ev.Synthetic {
			continue
		}
		live++
		if gtm.IsSerpFeed(ev.Feed) || ev.Metric == "serp_rank" || ev.Metric == "rank" {
			serp = 1
		}
		if ev.Metric == "search_volume" {
			volume = 1
		}
	}
	live /= float64(len(report.Evidence))
	return serp, volume, live
}

func runCompare(subjects []string, opts gtm.Options, asJSON bool, outPath, rewritePath, watchPath, failOn, corpusMd string, out, errOut io.Writer) int {
	eng, err := gtm.NewEngine(opts, time.Now)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	rep, err := eng.Compare(ctx, subjects, opts)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}

	// Self-heal: annotate the corpus markdown inline with verdicts (side output).
	if strings.TrimSpace(rewritePath) != "" {
		content := rep.Markdown() // fallback for non-markdown claim sources
		if corpusMd != "" {
			content = gtm.AnnotateCorpus(corpusMd, rep)
		}
		if err := os.WriteFile(rewritePath, []byte(content), 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes)\n", rewritePath, len(content))
	}

	// Drift detection: diff against the saved state, report flips, update state.
	if strings.TrimSpace(watchPath) != "" {
		return emitDrift(watchPath, failOn, gtm.StateFromReport(rep), asJSON, outPath, out, errOut)
	}
	if strings.TrimSpace(rewritePath) != "" {
		return 0 // rewrite was the requested output
	}

	var rendered []byte
	if asJSON {
		if rendered, err = rep.JSON(); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
	} else {
		rendered = []byte(rep.Markdown())
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes)\n", outPath, len(rendered))
		return 0
	}
	_, _ = out.Write(rendered)
	if !asJSON {
		_, _ = fmt.Fprintln(out)
	}
	return 0
}

// emitDrift diffs cur against the saved state, writes/prints the drift report,
// updates the state, and alerts (webhook + non-zero exit) per --fail-on. Shared
// by compare mode and the single-subject verticals.
func emitDrift(watchPath, failOn string, cur *gtm.WatchState, asJSON bool, outPath string, out, errOut io.Writer) int {
	prev, err := gtm.LoadWatchState(watchPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	drift := gtm.DiffStates(prev, cur)
	if err := gtm.SaveWatchState(watchPath, cur); err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	var content []byte
	if asJSON {
		content, _ = drift.JSON()
	} else {
		content = []byte(drift.Markdown())
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, content, 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes)\n", outPath, len(content))
	} else {
		_, _ = out.Write(content)
		if !asJSON {
			_, _ = fmt.Fprintln(out)
		}
	}

	alert, err := driftAlerts(drift, failOn)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 2
	}
	if alert {
		if hook := os.Getenv("GTM_DRIFT_WEBHOOK"); hook != "" {
			if err := gtm.PostDriftWebhook(context.Background(), hook, drift); err != nil {
				_, _ = fmt.Fprintln(errOut, "gtm: webhook:", err) // alert still fails the run
			} else {
				_, _ = fmt.Fprintln(errOut, "gtm: posted drift alert to GTM_DRIFT_WEBHOOK")
			}
		}
		return 3 // drift alert → non-zero so a scheduled job notices
	}
	return 0
}

// driftAlerts decides whether a drift report should alert (webhook + non-zero
// exit) under the --fail-on policy.
func driftAlerts(dr *gtm.DriftReport, failOn string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(failOn)) {
	case "", "regression":
		return dr.Regressions > 0, nil
	case "any":
		return dr.HasDrift(), nil
	case "none":
		return false, nil
	default:
		return false, fmt.Errorf("unknown --fail-on %q (use regression|any|none)", failOn)
	}
}

func cmdFeeds(args []string, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "doctor" {
		return cmdFeedsDoctor(args[1:], out, errOut)
	}
	fs := flag.NewFlagSet("feeds", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Build a registry via a throwaway engine (offline generator, real feeds).
	eng, err := gtm.NewEngine(gtm.Options{Subject: "_", Offline: true}, time.Now)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
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
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"feeds": rows}, "", "  ")
		_, _ = out.Write(b)
		_, _ = fmt.Fprintln(out)
		return 0
	}
	_, _ = fmt.Fprintln(out, "GTM data feeds:")
	for _, r := range rows {
		status := "✓ available"
		if !r.Available {
			status = "✗ set " + r.KeyEnv
		}
		key := r.KeyEnv
		if key == "" {
			key = "(no key)"
		}
		_, _ = fmt.Fprintf(out, "  %-12s %-7s %-22s %s\n", r.Name, r.Tier, key, status)
	}
	_, _ = fmt.Fprintln(out, "\nFree feeds need no key (except self-hosted searxng — set SEARXNG_URL), but configuration is not proof of liveness. Cheap feeds activate when their key is set via `ndev secrets` (tavily reuses your existing TAVILY_API_KEY).")
	_, _ = fmt.Fprintln(out, "Run `"+"ngtm feeds doctor"+"` to probe reachability and check whether brand/seo can ground.")
	return 0
}

type feedDoctorRow struct {
	Name              string `json:"name"`
	Tier              string `json:"tier"`
	KeyEnv            string `json:"key_env,omitempty"`
	Configured        bool   `json:"configured"`
	ProbeStatus       string `json:"probe_status"` // live | rate_limited | failed | unprobed | unconfigured
	Reachable         bool   `json:"reachable"`
	ProbeError        string `json:"probe_error,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	SerpClass         bool   `json:"serp_class"`
}

type feedProbe func(context.Context, gtm.Feed, gtm.FeedQuery) error

// probeFeedDoctorRows distinguishes configuration from observed liveness.
// Free/direct feeds are probed by default; credentialed paid feeds require the
// explicit --probe-paid cost boundary.
func probeFeedDoctorRows(ctx context.Context, feeds []gtm.Feed, probePaid bool, probe feedProbe) []feedDoctorRow {
	rows := make([]feedDoctorRow, len(feeds))
	var wg sync.WaitGroup
	for i, feed := range feeds {
		configured := feed.Available()
		rows[i] = feedDoctorRow{
			Name: feed.Name(), Tier: string(feed.Tier()), KeyEnv: feed.KeyEnv(),
			Configured: configured, ProbeStatus: "unconfigured", SerpClass: gtm.IsSerpFeed(feed.Name()),
		}
		if !configured {
			continue
		}
		shouldProbe := feed.Tier() == gtm.TierFree || feed.Name() == "landing" || probePaid
		if !shouldProbe {
			rows[i].ProbeStatus = "unprobed"
			continue
		}
		wg.Add(1)
		go func(index int, f gtm.Feed) {
			defer wg.Done()
			subject := "OpenAI"
			if f.Name() == "landing" {
				subject = "https://example.com"
			}
			query := gtm.FeedQuery{Subject: subject, Limit: 1, Category: "technology company"}
			if f.Name() == "searxng" {
				// Probe the JSON contract through one deterministic, fast engine;
				// a full metasearch can exceed the doctor's global timeout even
				// while the owned instance is healthy.
				query.Keywords = []string{"!wikipedia", subject}
			}
			err := probe(ctx, f, query)
			if err != nil {
				var rateLimit *gtm.FeedRateLimitError
				if errors.As(err, &rateLimit) {
					rows[index].ProbeStatus = "rate_limited"
					rows[index].Reachable = true
					rows[index].RetryAfterSeconds = int(rateLimit.RetryAfter.Round(time.Second) / time.Second)
				} else {
					rows[index].ProbeStatus = "failed"
				}
				rows[index].ProbeError = err.Error()
				return
			}
			rows[index].ProbeStatus = "live"
			rows[index].Reachable = true
		}(i, feed)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func queryFeedProbe(ctx context.Context, feed gtm.Feed, query gtm.FeedQuery) error {
	_, err := feed.Query(ctx, query)
	return err
}

func feedDoctorGrounding(rows []feedDoctorRow) gtm.GroundingStatus {
	var cheapSerpLive []string
	searxngSet := false
	searxngLive := false
	for _, row := range rows {
		if row.Name == "searxng" {
			searxngSet = row.Configured
			searxngLive = row.ProbeStatus == "live"
		}
		if row.SerpClass && row.Name != "searxng" && row.ProbeStatus == "live" {
			cheapSerpLive = append(cheapSerpLive, row.Name)
		}
	}
	return gtm.GroundingAdvisory(searxngSet, searxngLive, cheapSerpLive)
}

// cmdFeedsDoctor live-probes the default-cost feeds and reports paid feeds as
// configured-but-unprobed unless the operator explicitly passes --probe-paid.
func cmdFeedsDoctor(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("feeds doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	probePaid := fs.Bool("probe-paid", false, "live-probe configured cheap/premium feeds (may incur provider cost)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	eng, err := gtm.NewEngine(gtm.Options{Subject: "_", Offline: true}, time.Now)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	rows := probeFeedDoctorRows(ctx, eng.Registry().Feeds(), *probePaid, queryFeedProbe)
	status := feedDoctorGrounding(rows)

	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"feeds": rows, "grounding": status}, "", "  ")
		_, _ = out.Write(b)
		_, _ = fmt.Fprintln(out)
		return 0
	}

	_, _ = fmt.Fprintln(out, "GTM feed doctor:")
	for _, r := range rows {
		st := r.ProbeStatus
		switch r.ProbeStatus {
		case "live":
			st = "✓ live-probed"
		case "failed":
			st = "✗ probe failed: " + r.ProbeError
		case "unprobed":
			st = "⚠ configured, not probed (use --probe-paid)"
		case "unconfigured":
			if r.KeyEnv != "" {
				st = "✗ set " + r.KeyEnv
			}
		case "rate_limited":
			st = "⚠ reachable, rate limited: " + r.ProbeError
		}
		tag := ""
		if r.SerpClass {
			tag = "[SERP]"
		}
		_, _ = fmt.Fprintf(out, "  %-12s %-7s %-6s %s\n", r.Name, r.Tier, tag, st)
	}
	// ✓ only when the DEFAULT (free) run grounds — i.e. SearXNG is live. Cheap
	// keys ground only with --tier cheap, so they get ⚠ (achievable, not default).
	icon := "⚠"
	if status.LiveSERP && len(status.Sources) > 0 && status.Sources[0] == "searxng" {
		icon = "✓"
	}
	_, _ = fmt.Fprintf(out, "\n%s Grounding (brand/seo): %s\n", icon, status.Advisory)
	return 0
}

func cmdVerticals(_ []string, out io.Writer) int {
	_, _ = fmt.Fprintln(out, "Available verticals:")
	for _, v := range gtm.Verticals {
		_, _ = fmt.Fprintf(out, "  - %s\n", v)
	}
	_, _ = fmt.Fprintln(out, "\nModel-driven (operator inputs + analyst defaults): economics (LTV/CAC/payback/NRR + go/no-go), pricing (value-based + WTP), motion (ACV→motion + PMF plan).")
	return 0
}

// cmdLanding is the "publish" stage: it composes the research verticals into a
// self-contained HTML sales page (the factory's last mile). Pricing tiers come
// from the value-based model (or explicit --tiers); hero copy comes from
// --headline/--tagline overrides, or the brand vertical when --brand is set.
func cmdLanding(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" landing", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var (
		subject      = fs.String("subject", "", "subject under analysis")
		product      = fs.String("product", "", "display/brand name (defaults to subject)")
		badge        = fs.String("badge", "", "eyebrow chip")
		headline     = fs.String("headline", "", "hero H1 line")
		tagline      = fs.String("tagline", "", "hero subhead/positioning")
		cta          = fs.String("cta", "", "primary button label")
		buyURL       = fs.String("buy-url", "", "checkout/signup href")
		price        = fs.Float64("price", 0, "single headline price ($)")
		oneTime      = fs.Bool("one-time", false, "render --price as one-time (else /mo)")
		tierSpec     = fs.String("tiers", "", "explicit tiers: Name|Price|Period|Note|f1~f2|ctaURL|featured, joined by ;;")
		featSpec     = fs.String("features", "", "value cards: Title::Body, joined by ;;")
		featHead     = fs.String("features-head", "", "features section heading")
		priceHead    = fs.String("pricing-head", "", "pricing section heading")
		grounding    = fs.String("grounding", "", "grounding/confidence caveat under pricing")
		tierName     = fs.String("tier-name", "Pro", "tier name when using --price")
		nextBest     = fs.Float64("next-best-price", 0, "next-best alternative price ($) [tier derivation]")
		diffValue    = fs.Float64("diff-value", 0, "value of differentiation ($) [tier derivation]")
		valueCapture = fs.Float64("value-capture", 0, "share of differentiation captured 0..1 [tier derivation]")
		runBrand     = fs.Bool("brand", false, "run the brand vertical for grounded hero copy")
		category     = fs.String("category", "", "disambiguation hint for the brand run (e.g. \"developer tools\")")
		provider     = fs.String("provider", "", "LLM provider for brand copy")
		model        = fs.String("model", "", "LLM model")
		offline      = fs.Bool("offline", false, "hermetic: no LLM, no network")
		tier         = fs.String("tier", "free", "feed tier for brand run")
		paid         = fs.Bool("paid", false, "shorthand for --tier cheap")
		outPath      = fs.String("out", "", "write HTML to a file (default stdout)")
		asJSON       = fs.Bool("json", false, "emit the page model as JSON")
		designOn     = fs.Bool("design", false, "generate & apply an OKLCH design system as the page theme")
		designSeed   = fs.String("design-seed", "", "brand color seed #rrggbb for --design")
		designHarm   = fs.String("design-harmony", "analogous", "scheme for --design: analogous|complementary|triadic|split|mono")
		designTune   = fs.Bool("design-tune", false, "tune the design system to the best-scoring candidate before applying")
		byBrand      = fs.String("by", "", "footer attribution: \"<Product> — by <name>\" (default nicos-tools)")
		fromManifest = fs.String("from", "", "batch-generate every page in a JSON app manifest (the publish factory)")
	)
	var positional []string
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		positional = append(positional, args[i])
		i++
	}
	if err := fs.Parse(args[i:]); err != nil {
		return 2
	}
	// Batch factory: one manifest → every page. Scales the publish rail from
	// hand-written per-app invocations to "add an entry, run once".
	if p := strings.TrimSpace(*fromManifest); p != "" {
		return runLandingManifest(prog, p, out, errOut)
	}
	subj := strings.TrimSpace(*subject)
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(positional, " "))
	}
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if subj == "" {
		_, _ = fmt.Fprintln(errOut, "gtm landing: subject is required (positionally or via --subject)")
		return 2
	}

	// Pricing inputs feed ComputePricing when tiers are derived (not explicit).
	modelInputs := map[string]float64{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "next-best-price":
			modelInputs["next_best_price"] = *nextBest
		case "diff-value":
			modelInputs["diff_value"] = *diffValue
		case "value-capture":
			modelInputs["value_capture"] = *valueCapture
		}
	})

	tiers, noFeeds, err := parseTiers(*tier, *paid)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm landing:", err)
		return 2
	}
	opts := gtm.Options{
		Subject:  subj,
		Category: strings.TrimSpace(*category),
		Provider: strings.TrimSpace(*provider),
		Model:    strings.TrimSpace(*model),
		Offline:  *offline,
		NoFeeds:  noFeeds || *offline,
		Tiers:    tiers,
		Inputs:   modelInputs,
	}

	cfg := gtm.LandingConfig{
		Product:      strings.TrimSpace(*product),
		Brand:        strings.TrimSpace(*byBrand),
		Badge:        strings.TrimSpace(*badge),
		Headline:     strings.TrimSpace(*headline),
		Subhead:      strings.TrimSpace(*tagline),
		FeaturesHead: strings.TrimSpace(*featHead),
		PricingHead:  strings.TrimSpace(*priceHead),
		HeroCTALabel: strings.TrimSpace(*cta),
		HeroCTAURL:   strings.TrimSpace(*buyURL),
		Grounding:    strings.TrimSpace(*grounding),
		Features:     parseFeatureSpec(*featSpec),
	}
	if s := strings.TrimSpace(*tierSpec); s != "" {
		cfg.Tiers = parseTierSpec(s, *buyURL)
	} else if *price > 0 {
		period := "/mo"
		if *oneTime {
			period = "one-time"
		}
		cfg.Tiers = []gtm.LandingTier{{
			Name: *tierName, Price: fmtPrice(*price), Period: period,
			Note: "", CTAURL: *buyURL, Featured: true,
		}}
	}

	provLabel := "offline"
	var warnings []string
	// Grounded hero copy: only run the brand vertical when asked (it costs an
	// LLM/feed round-trip). Overrides always win, so we only fill blanks.
	if *runBrand && !*offline {
		eng, err := gtm.NewEngine(opts, time.Now)
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm landing:", err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		rep, err := eng.Run(ctx, "brand", opts)
		if err != nil {
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

	if *designOn {
		name := strings.TrimSpace(*product)
		if name == "" {
			name = subj
		}
		cfg.RootCSS = designRootForLanding(name, *designSeed, *designHarm, *designTune)
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	page := gtm.BuildLandingFromConfig(cfg, opts, now, provLabel)
	page.Warnings = append(page.Warnings, warnings...)

	var rendered []byte
	if *asJSON {
		rendered, err = page.JSON()
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm landing:", err)
			return 1
		}
	} else {
		rendered = []byte(gtm.RenderLandingHTML(page))
	}

	logRun(map[string]any{
		"ts": now, "surface": prog, "vertical": "landing", "subject": subj,
		"provider": provLabel, "out": *outPath, "tiers": len(page.Tiers),
	})

	if *outPath != "" {
		if err := os.WriteFile(*outPath, rendered, 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm landing:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes)\n", *outPath, len(rendered))
		return 0
	}
	_, _ = out.Write(rendered)
	if *asJSON {
		_, _ = fmt.Fprintln(out)
	}
	return 0
}

// landingManifest is the app-publish factory's input: one JSON file listing
// every generated page (and, optionally, the storefront index). Adding an app is
// a manifest entry, not a new code path.
type landingManifest struct {
	Storefront *storefrontCfg `json:"storefront,omitempty"`
	Apps       []landingSpec  `json:"apps"`
}

// storefrontCfg configures the generated portfolio index.
type storefrontCfg struct {
	Out      string            `json:"out"`
	Title    string            `json:"title"`
	Brand    string            `json:"brand"`
	Badge    string            `json:"badge"`
	Intro    string            `json:"intro"`
	Note     string            `json:"note"`
	RootSeed string            `json:"root_seed"` // design seed → storefront palette
	Groups   []storefrontGroup `json:"groups"`    // ordered group key → heading
}

type storefrontGroup struct {
	Key     string `json:"key"`
	Heading string `json:"heading"`
}

// landingSpec mirrors the cmdLanding flags as JSON fields, reusing the gtm
// landing types directly (so tiers/features unmarshal with no translation).
type landingSpec struct {
	Subject       string               `json:"subject"`
	CatalogID     string               `json:"catalog_id"` // cross-ref to the software catalog
	Status        string               `json:"status"`     // shipped | planned (informational)
	Product       string               `json:"product"`
	By            string               `json:"by"`
	Badge         string               `json:"badge"`
	Headline      string               `json:"headline"`
	Tagline       string               `json:"tagline"`
	FeaturesHead  string               `json:"features_head"`
	PricingHead   string               `json:"pricing_head"`
	CTA           string               `json:"cta"`
	BuyURL        string               `json:"buy_url"`
	Grounding     string               `json:"grounding"`
	Features      []gtm.LandingFeature `json:"features"`
	Tiers         []gtm.LandingTier    `json:"tiers"`
	Price         float64              `json:"price"`
	OneTime       bool                 `json:"one_time"`
	TierName      string               `json:"tier_name"`
	DesignSeed    string               `json:"design_seed"`
	DesignHarmony string               `json:"design_harmony"`
	DesignTune    bool                 `json:"design_tune"`
	Category      string               `json:"category"`
	NextBestPrice float64              `json:"next_best_price"`
	DiffValue     float64              `json:"diff_value"`
	ValueCapture  float64              `json:"value_capture"`
	Out           string               `json:"out"`
	// Storefront-card fields (how this app shows on the generated index).
	TierGroup  string   `json:"tier_group"`  // which storefront group it belongs to
	CardDesc   string   `json:"card_desc"`   // card pitch (falls back to tagline)
	CardPrice  string   `json:"card_price"`  // card priceline amount, e.g. "$28.5K"
	CardPeriod string   `json:"card_period"` // card priceline period, e.g. "/yr · Business anchor"
	Stats      []string `json:"stats"`       // card pills
	Href       string   `json:"href"`        // storefront link (defaults to basename of out)
	Generate   *bool    `json:"generate"`    // generate the page? default true (false for bespoke/hand pages)
}

// planned reports whether this is a not-yet-built app (coming-soon page, no
// pricing/checkout).
func (a landingSpec) planned() bool { return strings.EqualFold(strings.TrimSpace(a.Status), "planned") }

// shouldGenerate reports whether the factory produces this page (false skips it,
// e.g. a bespoke hand-maintained page the storefront still links to).
func (a landingSpec) shouldGenerate() bool { return a.Generate == nil || *a.Generate }

// buildLandingPage assembles a page from one spec, mirroring cmdLanding's
// single-app pipeline (config → tiers → design theme → page). Shared by the
// manifest path; kept deterministic (offline) so generation is reproducible.
func (a landingSpec) buildLandingPage(now string) (*gtm.LandingPage, error) {
	subj := strings.TrimSpace(a.Subject)
	if subj == "" {
		subj = strings.TrimSpace(a.Product)
	}
	if subj == "" {
		return nil, fmt.Errorf("entry missing subject/product")
	}
	comingSoon := a.planned()
	inputs := map[string]float64{}
	if a.NextBestPrice != 0 {
		inputs["next_best_price"] = a.NextBestPrice
	}
	if a.DiffValue != 0 {
		inputs["diff_value"] = a.DiffValue
	}
	if a.ValueCapture != 0 {
		inputs["value_capture"] = a.ValueCapture
	}
	opts := gtm.Options{Subject: subj, Category: strings.TrimSpace(a.Category), Offline: true, NoFeeds: true, Inputs: inputs}

	cfg := gtm.LandingConfig{
		Product:      strings.TrimSpace(a.Product),
		Brand:        strings.TrimSpace(a.By),
		Badge:        strings.TrimSpace(a.Badge),
		Headline:     strings.TrimSpace(a.Headline),
		Subhead:      strings.TrimSpace(a.Tagline),
		FeaturesHead: strings.TrimSpace(a.FeaturesHead),
		PricingHead:  strings.TrimSpace(a.PricingHead),
		HeroCTALabel: strings.TrimSpace(a.CTA),
		HeroCTAURL:   strings.TrimSpace(a.BuyURL),
		Grounding:    strings.TrimSpace(a.Grounding),
		Features:     a.Features,
		Tiers:        a.Tiers,
		ComingSoon:   comingSoon,
	}
	if len(cfg.Tiers) == 0 && a.Price > 0 {
		period := "/mo"
		if a.OneTime {
			period = "one-time"
		}
		name := a.TierName
		if name == "" {
			name = "Pro"
		}
		cfg.Tiers = []gtm.LandingTier{{Name: name, Price: fmtPrice(a.Price), Period: period, CTAURL: a.BuyURL, Featured: true}}
	}
	if seed := strings.TrimSpace(a.DesignSeed); seed != "" {
		name := strings.TrimSpace(a.Product)
		if name == "" {
			name = subj
		}
		harm := strings.TrimSpace(a.DesignHarmony)
		if harm == "" {
			harm = "analogous"
		}
		cfg.RootCSS = designRootForLanding(name, seed, harm, a.DesignTune)
	}
	return gtm.BuildLandingFromConfig(cfg, opts, now, "offline"), nil
}

// runLandingManifest is the publish factory: read a JSON manifest, generate
// every app's page. One file is the source of truth for the generated pages.
func runLandingManifest(prog, path string, out, errOut io.Writer) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm landing --from:", err)
		return 1
	}
	var man landingManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm landing --from: parse:", err)
		return 1
	}
	if len(man.Apps) == 0 {
		_, _ = fmt.Fprintln(errOut, "gtm landing --from: manifest has no apps")
		return 2
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	okCount, failCount := 0, 0
	for _, a := range man.Apps {
		label := strings.TrimSpace(a.Product)
		if label == "" {
			label = strings.TrimSpace(a.Subject)
		}
		if !a.shouldGenerate() {
			_, _ = fmt.Fprintf(out, "  · %-24s (hand-maintained, not generated)\n", label)
			continue
		}
		if strings.TrimSpace(a.Out) == "" {
			_, _ = fmt.Fprintf(errOut, "  skip %q: no \"out\" path\n", label)
			failCount++
			continue
		}
		page, err := a.buildLandingPage(now)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "  FAIL %q: %v\n", label, err)
			failCount++
			continue
		}
		html := gtm.RenderLandingHTML(page)
		if err := os.WriteFile(a.Out, []byte(html), 0o644); err != nil {
			_, _ = fmt.Fprintf(errOut, "  FAIL %s: %v\n", a.Out, err)
			failCount++
			continue
		}
		tag := ""
		if a.planned() {
			tag = " [coming soon]"
		}
		_, _ = fmt.Fprintf(out, "  ✓ %-24s → %s (%d bytes)%s\n", label, a.Out, len(html), tag)
		okCount++
	}

	// Storefront index, generated from the same manifest (auto-lists every app).
	if man.Storefront != nil && strings.TrimSpace(man.Storefront.Out) != "" {
		store := buildStorefront(man, now)
		shtml := gtm.RenderStorefrontHTML(store)
		if err := os.WriteFile(man.Storefront.Out, []byte(shtml), 0o644); err != nil {
			_, _ = fmt.Fprintf(errOut, "  FAIL storefront %s: %v\n", man.Storefront.Out, err)
			failCount++
		} else {
			_, _ = fmt.Fprintf(out, "  ✓ %-24s → %s (%d bytes)\n", "storefront", man.Storefront.Out, len(shtml))
		}
	}

	logRun(map[string]any{"ts": now, "surface": prog, "vertical": "landing", "subject": "--from " + path, "apps": okCount})
	if failCount > 0 {
		_, _ = fmt.Fprintf(out, "wrote %d page(s), %d failed, from %s\n", okCount, failCount, path)
		return 1
	}
	_, _ = fmt.Fprintf(out, "wrote %d page(s) from %s\n", okCount, path)
	return 0
}

// buildStorefront assembles the portfolio index from the manifest: apps grouped
// by tier_group in the manifest's declared group order, planned apps muted.
func buildStorefront(man landingManifest, now string) *gtm.StorefrontModel {
	sc := man.Storefront
	model := &gtm.StorefrontModel{
		Title:     firstNonBlank(sc.Title, "Products"),
		Brand:     firstNonBlank(sc.Brand, "garrid"),
		Badge:     sc.Badge,
		Intro:     sc.Intro,
		Note:      sc.Note,
		Generated: now,
	}
	if seed := strings.TrimSpace(sc.RootSeed); seed != "" {
		model.RootCSS = designRootForLanding(model.Brand, seed, "analogous", false)
	}
	for _, g := range sc.Groups {
		group := gtm.StorefrontGroup{Heading: g.Heading}
		for _, a := range man.Apps {
			if !strings.EqualFold(strings.TrimSpace(a.TierGroup), strings.TrimSpace(g.Key)) {
				continue
			}
			href := strings.TrimSpace(a.Href)
			if href == "" {
				href = filepath.Base(strings.TrimSpace(a.Out))
			}
			group.Cards = append(group.Cards, gtm.StorefrontCard{
				Name:     firstNonBlank(a.Product, a.Subject),
				Category: a.Badge,
				Desc:     firstNonBlank(a.CardDesc, a.Tagline),
				Price:    a.CardPrice,
				Period:   a.CardPeriod,
				Href:     href,
				Planned:  a.planned(),
				Stats:    a.Stats,
			})
		}
		model.Groups = append(model.Groups, group)
	}
	return model
}

// fmtPrice formats a price for a tier: whole dollars print without cents.
func fmtPrice(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("$%d", int64(v))
	}
	return fmt.Sprintf("$%.2f", v)
}

// parseTierSpec parses "Name|Price|Period|Note|f1~f2|ctaURL|featured|label"
// entries joined by ";;". Missing trailing fields are tolerated.
func parseTierSpec(spec, defaultURL string) []gtm.LandingTier {
	var out []gtm.LandingTier
	for _, raw := range strings.Split(spec, ";;") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		f := strings.Split(raw, "|")
		get := func(i int) string {
			if i < len(f) {
				return strings.TrimSpace(f[i])
			}
			return ""
		}
		t := gtm.LandingTier{
			Name:   get(0),
			Price:  get(1),
			Period: get(2),
			Note:   get(3),
		}
		if feats := get(4); feats != "" {
			for _, ft := range strings.Split(feats, "~") {
				if ft = strings.TrimSpace(ft); ft != "" {
					t.Features = append(t.Features, ft)
				}
			}
		}
		t.CTAURL = firstNonBlank(get(5), defaultURL)
		switch strings.ToLower(get(6)) {
		case "featured", "true", "*", "yes", "1":
			t.Featured = true
		}
		t.CTALabel = get(7) // optional explicit button label
		out = append(out, t)
	}
	return out
}

// parseFeatureSpec parses "Title::Body" cards joined by ";;".
func parseFeatureSpec(spec string) []gtm.LandingFeature {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var out []gtm.LandingFeature
	for _, raw := range strings.Split(spec, ";;") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		title, body, _ := strings.Cut(raw, "::")
		out = append(out, gtm.LandingFeature{Title: strings.TrimSpace(title), Body: strings.TrimSpace(body)})
	}
	return out
}

// featuresFromBullets turns hero bullets into value cards (title = bullet's lead
// clause, body = the remainder) so brand copy populates the cards section.
func featuresFromBullets(bullets []string) []gtm.LandingFeature {
	var out []gtm.LandingFeature
	for _, b := range bullets {
		title, body, found := strings.Cut(b, " — ")
		if !found {
			title, body, found = strings.Cut(b, ": ")
		}
		if !found {
			out = append(out, gtm.LandingFeature{Title: b})
			continue
		}
		out = append(out, gtm.LandingFeature{Title: strings.TrimSpace(title), Body: strings.TrimSpace(body)})
	}
	return out
}

func firstNonBlank(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// parseTiers resolves the tier flag into a tier slice and a NoFeeds flag.
func parseTiers(tier string, paid bool) ([]gtm.FeedTier, bool, error) {
	if paid {
		tier = "cheap"
	}
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "free":
		return []gtm.FeedTier{gtm.TierFree}, false, nil
	case "cheap", "paid":
		return []gtm.FeedTier{gtm.TierFree, gtm.TierCheap}, false, nil
	case "premium", "all":
		return []gtm.FeedTier{gtm.TierFree, gtm.TierCheap, gtm.TierPremium}, false, nil
	case "none":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown tier %q (use free|cheap|premium|all|none)", tier)
	}
}

func joinFeedTiers(tiers []gtm.FeedTier) string {
	ss := make([]string, len(tiers))
	for i, t := range tiers {
		ss[i] = string(t)
	}
	return strings.Join(ss, ",")
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
