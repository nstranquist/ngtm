//nolint:errcheck // Terminal presentation writes are best-effort; ledger writes are checked.
package gtmcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

// cmdLaunch is the launch-loop surface: weekly cohorts of product launches,
// recorded in an append-only ledger and gated by measured traction verdicts.
// plan → kit → posted → signal/signals → cohort/show/verdict, with retire for
// explicitly closing an unplaced attempt without deleting ledger history.
func cmdLaunch(prog string, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printLaunchUsage(prog, out)
		return 0
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "plan":
		return launchPlan(prog, rest, out, errOut)
	case "kit":
		return launchKit(prog, rest, out, errOut)
	case "posted":
		return launchPosted(prog, rest, out, errOut)
	case "signal":
		return launchSignal(prog, rest, out, errOut)
	case "price-test":
		return launchPriceTest(prog, rest, out, errOut)
	case "signals":
		return launchSignals(prog, rest, out, errOut)
	case "cohort":
		return launchCohort(prog, rest, out, errOut)
	case "show":
		return launchShow(prog, rest, out, errOut)
	case "verdict":
		return launchVerdict(prog, rest, out, errOut)
	case "retire":
		return launchRetire(prog, rest, out, errOut)
	case "channels":
		return launchChannels(rest, out, errOut)
	case "retro":
		return launchRetro(prog, rest, out, errOut)
	case "audit":
		return launchAudit(prog, rest, out, errOut)
	case "open":
		return launchOpen(prog, rest, out, errOut)
	case "import":
		return launchImport(prog, rest, out, errOut)
	default:
		_, _ = fmt.Fprintf(errOut, "%s launch: unknown subcommand %q\n\n", prog, verb)
		printLaunchUsage(prog, errOut)
		return 2
	}
}

func printLaunchUsage(prog string, w io.Writer) {
	_, _ = fmt.Fprintf(w, `%[1]s launch — the launch loop: weekly cohorts, placement receipts, measured verdicts

USAGE
  %[1]s launch plan <product> [--week 2026-W24]      Enter a product into a weekly cohort
  %[1]s launch kit <product> [--pitch s] [--channels a,b] [--out f]
                                                     Generate the channel content pack (social vertical) + record it
  %[1]s launch posted <product> --channel <key> --url <u>
                                                     Record a placement receipt (any channel key accepted)
  %[1]s launch signal <product> --metric <m> --value <v>
                                                     Record an operator-observed signal (signups|revenue_usd|clicks|...)
  %[1]s launch signals <product> [--record]          MEASURE community traction via the HN/Reddit feeds (observed provenance)
  %[1]s launch price-test <product> --price N --channel <key> --response <r>
                                                     Record a price TEST (offer shown + what came back); no --price lists them
  %[1]s launch cohort [--week W] [--target 20] [--include-retired]
                                                     The weekly board: fill vs target, stage, score, verdict
                                                     (retired attempts hidden by default; the count is always shown)
  %[1]s launch show <product>                        Per-product drill-in (posts, signals, rationale)
  %[1]s launch verdict <product> [--fail-on kill]    Traction gate: DOUBLE-DOWN | ITERATE | KILL | TOO-EARLY | WATCH
                                                     | NOT-DISTRIBUTED | UNMEASURED
  %[1]s launch retire <product> --disposition cancelled|abandoned --reason <text>
                                                     Close an unplaced attempt, preserving append-only history
  %[1]s launch retro [--week W]                      Weekly learning: channel leaderboard + kill/double-down rates + next-cohort recs
  %[1]s launch audit [--strict] [--verify-receipts]  Ledger integrity plus opt-in live semantic receipt proof
  %[1]s launch open <product> [--channel k] [--pitch s] [--url u]
                                                     Prefilled composer links (HN submitlink, X intent, Reddit submit, ...)
  %[1]s launch import <product> --file m.json|csv | --plausible <site> [--record]
                                                     Import external analytics as observed signals
  %[1]s launch channels                              List the typed channel registry (norms, limits, best slots)

COMMON FLAGS
  --ledger <path>   Ledger file (default $NGTM_LAUNCH_LEDGER or ~/.nicos-dev/gtm/launch-ledger.jsonl)
  --json            Machine-readable output

VERDICT GATE (the traction analog of the economics GO/NO-GO)
  DOUBLE-DOWN  score >= %.0f, or any operator-observed conversion (signups/revenue)
  ITERATE      score >= %.0f — signal exists, sharpen angle/channel
  KILL         below the bar %d+ days after first post — archive, redeploy the slot
  NOT-DISTRIBUTED
               placed only on channels that reach no new audience (e.g. a release
               tag). NOT a product verdict — the launch was never attempted.
               Positive evidence still wins: a conversion outranks the downgrade.
  UNMEASURED   distributed, but the destination surface cannot see arrivals
               (ndev endpoints analytics). The score measures our instrumentation,
               not demand, so the gate declines to judge. Also not a product verdict.
               Coverage the ledger cannot answer stays UNKNOWN and changes nothing.
  Signals are levels (latest per channel+metric+source wins); weights favor conversions over community noise.
  A price TEST (%[1]s launch price-test) is evidence; a modeled price is not.
`, prog, gtm.ScoreDoubleDown, gtm.ScoreIterate, gtm.KillAfterDays)
}

// launchFlagSet builds the common flag set; returns it plus the ledger flag.
func launchFlagSet(name string, errOut io.Writer) (*flag.FlagSet, *string, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	ledger := fs.String("ledger", gtm.DefaultLaunchLedgerPath(), "launch ledger path")
	asJSON := fs.Bool("json", false, "emit JSON")
	return fs, ledger, asJSON
}

// popProduct pulls the leading positional product slug off the args.
func popProduct(args []string) (string, []string) {
	var pos []string
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		pos = append(pos, args[i])
		i++
	}
	return strings.TrimSpace(strings.Join(pos, " ")), args[i:]
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func launchPlan(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, _ := launchFlagSet(prog+" launch plan", errOut)
	week := fs.String("week", "", "ISO week cohort key, e.g. 2026-W24 (default: current week)")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		_, _ = fmt.Fprintln(errOut, prog+" launch plan: product is required")
		return 2
	}
	w := strings.TrimSpace(*week)
	if w == "" {
		w = gtm.ISOWeek(time.Now())
	} else if !gtm.ValidISOWeek(w) {
		_, _ = fmt.Fprintf(errOut, "%s launch plan: --week %q is not ISO form YYYY-Www (e.g. 2026-W24)\n", prog, w)
		return 2
	}
	led := gtm.LaunchLedger{Path: *ledger}
	if err := led.Append(gtm.LaunchEvent{TS: nowRFC3339(), Product: product, Type: gtm.EventPlanned, Week: w, Note: strings.TrimSpace(*note)}); err != nil {
		fmt.Fprintln(errOut, prog+" launch plan:", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "planned %s into cohort %s (ledger %s)\n", product, w, *ledger)
	return 0
}

func launchKit(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch kit", errOut)
	var (
		pitch    = fs.String("pitch", "", "one-line value proposition the drafts build around")
		channels = fs.String("channels", "", "comma-separated channel keys (default: all)")
		provider = fs.String("provider", "", "LLM provider for channel-native prose (default: deterministic templates)")
		model    = fs.String("model", "", "LLM model")
		offline  = fs.Bool("offline", false, "hermetic: no LLM, no network")
		tier     = fs.String("tier", "free", "feed tier: free|cheap|premium|all|none")
		paid     = fs.Bool("paid", false, "shorthand for --tier cheap")
		tune     = fs.Bool("tune", false, "self-review loop: best-scoring hook archetype per channel")
		outPath  = fs.String("out", "", "write the content pack to a file (default stdout)")
	)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch kit: product is required")
		return 2
	}
	tiers, noFeeds, err := parseTiers(*tier, *paid)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch kit:", err)
		return 2
	}
	opts := gtm.Options{
		Subject: product, Pitch: strings.TrimSpace(*pitch), Channels: splitCSV(*channels),
		Tiers: tiers, Provider: strings.TrimSpace(*provider), Model: strings.TrimSpace(*model),
		Offline: *offline, NoFeeds: noFeeds || *offline, Tune: *tune,
	}
	eng, err := gtm.NewEngine(opts, time.Now)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch kit:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rep, err := eng.Run(ctx, "social", opts)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch kit:", err)
		return 1
	}

	led := gtm.LaunchLedger{Path: *ledger}
	if err := led.Append(gtm.LaunchEvent{TS: nowRFC3339(), Product: product, Type: gtm.EventKit, Note: "channels=" + firstNonBlank(*channels, "all")}); err != nil {
		fmt.Fprintln(errOut, prog+" launch kit: ledger:", err)
		return 1
	}
	logRun(map[string]any{"ts": rep.Generated, "surface": prog, "vertical": "social", "subject": product, "provider": rep.Provider, "out": *outPath, "metrics": rep.Metrics})

	var rendered []byte
	if *asJSON {
		if rendered, err = rep.JSON(); err != nil {
			fmt.Fprintln(errOut, prog+" launch kit:", err)
			return 1
		}
	} else {
		rendered = []byte(rep.Markdown())
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, rendered, 0o644); err != nil {
			fmt.Fprintln(errOut, prog+" launch kit:", err)
			return 1
		}
		_, _ = fmt.Fprintf(out, "wrote %s (%d bytes); kit recorded for %s\n", *outPath, len(rendered), product)
		return 0
	}
	_, _ = out.Write(rendered)
	if !*asJSON {
		fmt.Fprintln(out)
	}
	return 0
}

func launchPosted(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, _ := launchFlagSet(prog+" launch posted", errOut)
	channel := fs.String("channel", "", "channel key (see `launch channels`; any key accepted for niche directories)")
	url := fs.String("url", "", "the live post URL (the receipt)")
	expect := fs.String("expect", "", "text expected on the live receipt (default: product slug)")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	ch := strings.ToLower(strings.TrimSpace(*channel))
	receiptURL := strings.TrimSpace(*url)
	if product == "" || ch == "" || receiptURL == "" {
		fmt.Fprintln(errOut, prog+" launch posted: product, --channel, and --url receipt are required")
		return 2
	}
	if _, known := gtm.ChannelByKey(ch); !known {
		_, _ = fmt.Fprintf(errOut, "note: %q is not in the typed channel registry (%s) — recorded anyway\n", ch, strings.Join(channelRegistryKeys(), ", "))
	}
	led := gtm.LaunchLedger{Path: *ledger}
	marker := strings.TrimSpace(*expect)
	if marker == "" {
		marker = product
	}
	if err := led.Append(gtm.LaunchEvent{TS: nowRFC3339(), Product: product, Type: gtm.EventPosted, Channel: ch, URL: receiptURL, ReceiptMarker: marker, Note: strings.TrimSpace(*note)}); err != nil {
		fmt.Fprintln(errOut, prog+" launch posted:", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "recorded: %s posted on %s %s\n", product, ch, receiptURL)
	return 0
}

// filterRetiredFromCohorts drops retired attempts unless asked to keep them,
// returning the surviving cohorts and how many rows were removed. Cohorts left
// empty by the filter are dropped too, so a week made entirely of cancellations
// stops occupying the board.
func filterRetiredFromCohorts(cohorts []gtm.LaunchCohort, includeRetired bool) ([]gtm.LaunchCohort, int) {
	if includeRetired {
		return cohorts, 0
	}
	var kept []gtm.LaunchCohort
	hidden := 0
	for _, c := range cohorts {
		var active []gtm.ProductLaunch
		for _, p := range c.Products {
			if p.Disposition.Valid() {
				hidden++
				continue
			}
			active = append(active, p)
		}
		if len(active) == 0 {
			continue
		}
		c.Products = active
		kept = append(kept, c)
	}
	return kept, hidden
}

// launchPriceTest records that a price was SHOWN to someone and what came back.
// It is the falsifiable counterpart to a modeled price: `ngtm pricing` produces
// a number, this produces evidence. With no --price it reads back the history
// instead of recording, so one leaf covers both directions.
func launchPriceTest(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch price-test", errOut)
	price := fs.Float64("price", 0, "the price shown, in --currency units (omit to list recorded tests)")
	currency := fs.String("currency", "USD", "three-letter currency code")
	channel := fs.String("channel", "", "where the offer was shown (channel key, or any safe key)")
	response := fs.String("response", "", "what came back: "+joinResponses())
	offer := fs.String("offer", "", "what was offered at that price, in one line")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch price-test: product is required")
		return 2
	}
	if *price == 0 {
		return launchPriceTestShow(prog, product, *ledger, *asJSON, out, errOut)
	}

	ch := strings.ToLower(strings.TrimSpace(*channel))
	if ch == "" || strings.TrimSpace(*response) == "" {
		fmt.Fprintln(errOut, prog+" launch price-test: --channel and --response are required when recording (--response: "+joinResponses()+")")
		return 2
	}
	parsedResponse, err := gtm.ParsePriceTestResponse(*response)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch price-test:", err)
		return 2
	}
	ev := gtm.LaunchEvent{
		TS: nowRFC3339(), Product: product, Type: gtm.EventPriceTest,
		Channel: ch, Price: *price, Currency: strings.ToUpper(strings.TrimSpace(*currency)),
		Offer: strings.TrimSpace(*offer), Response: parsedResponse,
		Source: gtm.SourceOperator, Note: strings.TrimSpace(*note),
	}
	if err := (gtm.LaunchLedger{Path: *ledger}).Append(ev); err != nil {
		fmt.Fprintln(errOut, prog+" launch price-test:", err)
		return 1
	}
	logRun(map[string]any{
		"ts": nowRFC3339(), "surface": prog, "vertical": "launch.price_test",
		"subject": product, "channel": ch, "response": string(parsedResponse),
	})
	_, _ = fmt.Fprintf(out, "recorded: %s tested at %.2f %s on %s → %s\n",
		product, ev.Price, ev.Currency, ch, parsedResponse)
	return 0
}

func launchPriceTestShow(prog, product, ledgerPath string, asJSON bool, out, errOut io.Writer) int {
	events, err := (gtm.LaunchLedger{Path: ledgerPath}).Read()
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch price-test:", err)
		return 1
	}
	var tests []gtm.LaunchEvent
	for _, ev := range events {
		if ev.Type == gtm.EventPriceTest && ev.Product == product {
			tests = append(tests, ev)
		}
	}
	if asJSON {
		b, _ := json.MarshalIndent(map[string]any{
			"product": product, "price_tested": len(tests) > 0, "price_tests": tests,
		}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	if len(tests) == 0 {
		_, _ = fmt.Fprintf(out, "%s: no price test recorded — any price on this product is modeled, not tested\n", product)
		return 0
	}
	_, _ = fmt.Fprintf(out, "%s: %d price test(s)\n", product, len(tests))
	for _, ev := range tests {
		_, _ = fmt.Fprintf(out, "  %s  %10.2f %s  %-14s %-12s %s\n",
			ev.TS[:10], ev.Price, ev.Currency, ev.Channel, ev.Response, ev.Offer)
	}
	return 0
}

func joinResponses() string {
	known := gtm.KnownPriceTestResponses()
	parts := make([]string, len(known))
	for i, r := range known {
		parts[i] = string(r)
	}
	return strings.Join(parts, "|")
}

func launchSignal(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, _ := launchFlagSet(prog+" launch signal", errOut)
	metric := fs.String("metric", "", "signal metric: signups|revenue_usd|clicks|hn_points|reddit_score|mentions|...")
	value := fs.Float64("value", 0, "current level of the metric (latest value per key wins)")
	channel := fs.String("channel", "", "channel key the signal belongs to (optional)")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" || strings.TrimSpace(*metric) == "" {
		fmt.Fprintln(errOut, prog+" launch signal: product and --metric are required")
		return 2
	}
	typedMetric, err := gtm.ParseSignalMetric(*metric)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch signal:", err)
		return 2
	}
	led := gtm.LaunchLedger{Path: *ledger}
	ev := gtm.LaunchEvent{
		TS: nowRFC3339(), Product: product, Type: gtm.EventSignal,
		Channel: strings.ToLower(strings.TrimSpace(*channel)),
		Metric:  typedMetric, Value: *value,
		Source: gtm.SourceOperator, Note: strings.TrimSpace(*note),
	}
	if err := led.Append(ev); err != nil {
		fmt.Fprintln(errOut, prog+" launch signal:", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "recorded operator signal: %s %s=%.2f\n", product, ev.Metric, ev.Value)
	return 0
}

func launchSignals(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch signals", errOut)
	tier := fs.String("tier", "free", "feed tier: free|cheap|premium|all")
	paid := fs.Bool("paid", false, "shorthand for --tier cheap")
	record := fs.Bool("record", false, "append the measured signals to the ledger")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch signals: product is required")
		return 2
	}
	tiers, noFeeds, err := parseTiers(*tier, *paid)
	if err != nil || noFeeds {
		fmt.Fprintln(errOut, prog+" launch signals: needs live feeds (tier free|cheap|premium|all)")
		return 2
	}
	eng, err := gtm.NewEngine(gtm.Options{Subject: product, Offline: true, Tiers: tiers}, time.Now)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch signals:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sigs, warnings := eng.MeasureLaunchSignals(ctx, product, tiers)
	for _, w := range warnings {
		fmt.Fprintln(errOut, "warning:", w)
	}
	if *record {
		led := gtm.LaunchLedger{Path: *ledger}
		for _, s := range sigs {
			if err := led.Append(s); err != nil {
				fmt.Fprintln(errOut, prog+" launch signals: ledger:", err)
				return 1
			}
		}
	}
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"product": product, "signals": sigs, "recorded": *record}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	if len(sigs) == 0 {
		_, _ = fmt.Fprintf(out, "no community signals measured for %s (HN/Reddit returned nothing)\n", product)
		return 0
	}
	_, _ = fmt.Fprintf(out, "measured signals for %s (observed provenance):\n", product)
	for _, s := range sigs {
		_, _ = fmt.Fprintf(out, "  %-10s %-16s %8.1f  [%s]\n", s.Channel, s.Metric, s.Value, s.Source)
	}
	if *record {
		fmt.Fprintln(out, "recorded to ledger.")
	} else {
		fmt.Fprintln(out, "(dry run — pass --record to append to the ledger)")
	}
	return 0
}

func launchCohort(prog string, args []string, out, errOut io.Writer) int {
	fs, ledger, asJSON := launchFlagSet(prog+" launch cohort", errOut)
	week := fs.String("week", "", "show only this ISO week (e.g. 2026-W24)")
	target := fs.Int("target", 20, "ship-N-a-week goal the fill is measured against")
	includeRetired := fs.Bool("include-retired", false, "also show retired (cancelled/abandoned) attempts")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	events, err := (gtm.LaunchLedger{Path: *ledger}).Read()
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch cohort:", err)
		return 1
	}
	cohorts := gtm.BuildCohorts(gtm.BuildLaunchesWithCoverage(events, time.Now(), launchCoverage()), *target)
	if w := strings.TrimSpace(*week); w != "" {
		var filtered []gtm.LaunchCohort
		for _, c := range cohorts {
			if c.Week == w {
				filtered = append(filtered, c)
			}
		}
		cohorts = filtered
	}
	// Retired attempts dominate this ledger (55 retirements against 2 real
	// placements as of 2026-07-25), which buries every live row. They are hidden
	// by default and the hidden COUNT is always reported — a board that silently
	// drops rows is worse than a cluttered one.
	cohorts, hiddenRetired := filterRetiredFromCohorts(cohorts, *includeRetired)
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{
			"ledger": *ledger, "cohorts": cohorts,
			"hidden_retired": hiddenRetired, "include_retired": *includeRetired,
		}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	if len(cohorts) == 0 {
		if hiddenRetired > 0 {
			_, _ = fmt.Fprintf(out, "no active launch attempts (%d retired hidden — --include-retired to show)\n", hiddenRetired)
			return 0
		}
		_, _ = fmt.Fprintf(out, "no launch cohorts yet (ledger %s) — start with `%s launch plan <product>`\n", *ledger, prog)
		return 0
	}
	_, _ = fmt.Fprintf(out, "Launch cohorts (ledger %s)\n", *ledger)
	for _, c := range cohorts {
		_, _ = fmt.Fprintf(out, "\n%s — %d/%d planned\n", c.Week, len(c.Products), c.Target)
		_, _ = fmt.Fprintf(out, "  %-22s %-9s %8s  %-16s %s\n", "PRODUCT", "STAGE", "SCORE", "VERDICT", "POSTS")
		for _, p := range c.Products {
			_, _ = fmt.Fprintf(out, "  %-22s %-9s %8.1f  %-16s %d\n", p.Product, p.Stage(), p.Score, p.Verdict, len(p.Posts))
		}
	}
	if hiddenRetired > 0 {
		_, _ = fmt.Fprintf(out, "\n%d retired attempt(s) hidden — `%s launch cohort --include-retired` to show\n", hiddenRetired, prog)
	}
	return 0
}

func launchShow(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch show", errOut)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch show: product is required")
		return 2
	}
	p, code := loadLaunch(prog, *ledger, product, errOut)
	if code != 0 {
		return code
	}
	if *asJSON {
		b, _ := json.MarshalIndent(p, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	_, _ = fmt.Fprintf(out, "%s — cohort %s, stage %s\n", p.Product, p.Week, p.Stage())
	if p.KitAt != "" {
		_, _ = fmt.Fprintf(out, "  kit:     %s\n", p.KitAt)
	}
	for _, post := range p.Posts {
		_, _ = fmt.Fprintf(out, "  posted:  %-12s %s  %s\n", post.Channel, post.TS, post.URL)
	}
	for _, s := range p.Signals {
		src := s.Source
		if src == "" {
			src = "operator"
		}
		_, _ = fmt.Fprintf(out, "  signal:  %-12s %-16s %8.1f  [%s]  %s\n", s.Channel, s.Metric, s.Value, src, s.TS)
	}
	// Coverage is printed only when the endpoint ledger could answer. A blank
	// line for "" would read as a measurement of nothing rather than an unasked
	// question.
	if p.SurfaceCoverage != "" {
		_, _ = fmt.Fprintf(out, "  surface: %s (per `ndev endpoints analytics`)\n", p.SurfaceCoverage)
	}
	_, _ = fmt.Fprintf(out, "  score:   %.1f", p.Score)
	if len(p.ScoreParts) > 0 {
		b, _ := json.Marshal(p.ScoreParts)
		_, _ = fmt.Fprintf(out, "  %s", b)
	}
	_, _ = fmt.Fprintf(out, "\n  verdict: %s — %s\n", p.Verdict, p.Rationale)
	return 0
}

func launchVerdict(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch verdict", errOut)
	failOn := fs.String("fail-on", "none", "exit 3 on: kill|none")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch verdict: product is required")
		return 2
	}
	p, code := loadLaunch(prog, *ledger, product, errOut)
	if code != 0 {
		return code
	}
	if err := (gtm.LaunchLedger{Path: *ledger}).Append(gtm.LaunchEvent{
		TS: nowRFC3339(), Product: p.Product, Type: gtm.EventVerdict,
		Verdict: p.Verdict, Note: p.Rationale,
	}); err != nil {
		fmt.Fprintln(errOut, prog+" launch verdict: persist:", err)
		return 1
	}
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{
			"product": p.Product, "week": p.Week, "score": p.Score,
			"score_parts": p.ScoreParts, "verdict": p.Verdict, "rationale": p.Rationale,
			// Emitted with the verdict so a consumer reading UNMEASURED can see
			// which input produced it without a second command.
			"surface_coverage": p.SurfaceCoverage,
		}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
	} else {
		_, _ = fmt.Fprintf(out, "%s: %s\n  %s\n", p.Product, p.Verdict, p.Rationale)
	}
	if strings.EqualFold(strings.TrimSpace(*failOn), "kill") && p.Verdict == gtm.VerdictKill {
		return 3
	}
	return 0
}

func launchRetire(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch retire", errOut)
	disposition := fs.String("disposition", "", "retirement disposition: cancelled|abandoned")
	reason := fs.String("reason", "", "single-line operator reason (required)")
	week := fs.String("week", "", "active ISO week to retire (default: latest attempt)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" || strings.TrimSpace(*disposition) == "" || strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(errOut, prog+" launch retire: product, --disposition, and --reason are required")
		return 2
	}
	typedDisposition, err := gtm.ParseLaunchDisposition(*disposition)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch retire:", err)
		return 2
	}
	p, code := loadLaunch(prog, *ledger, product, errOut)
	if code != 0 {
		return code
	}
	if p.Disposition.Valid() {
		fmt.Fprintf(errOut, "%s launch retire: %s attempt %s is already %s\n", prog, p.Product, p.Week, p.Disposition)
		return 3
	}
	if len(p.Posts) > 0 {
		fmt.Fprintf(errOut, "%s launch retire: %s has a placement receipt; persist a verdict instead of retiring an executed launch\n", prog, p.Product)
		return 3
	}
	targetWeek := strings.TrimSpace(*week)
	if targetWeek == "" {
		targetWeek = p.Week
	}
	if targetWeek != p.Week {
		fmt.Fprintf(errOut, "%s launch retire: --week %s does not match active attempt %s\n", prog, targetWeek, p.Week)
		return 2
	}
	ev := gtm.LaunchEvent{
		TS: nowRFC3339(), Product: p.Product, Type: gtm.EventRetired, Week: targetWeek,
		Disposition: typedDisposition, Reason: strings.TrimSpace(*reason), Source: gtm.SourceOperator,
	}
	if err := (gtm.LaunchLedger{Path: *ledger}).Append(ev); err != nil {
		fmt.Fprintln(errOut, prog+" launch retire:", err)
		return 1
	}
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{
			"product": ev.Product, "week": ev.Week, "disposition": ev.Disposition,
			"reason": ev.Reason, "retired_at": ev.TS,
		}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	fmt.Fprintf(out, "retired %s cohort %s as %s: %s\n", ev.Product, ev.Week, ev.Disposition, ev.Reason)
	return 0
}

func launchChannels(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("launch channels", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"channels": gtm.Channels}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	fmt.Fprintln(out, "Typed channel registry (the content factory's placement contracts):")
	for _, c := range gtm.Channels {
		_, _ = fmt.Fprintf(out, "\n%-14s %s (%s) — best slot: %s\n", c.Key, c.Label, c.Kind, c.BestSlot)
		if c.TitleRule != "" {
			_, _ = fmt.Fprintf(out, "  title: %s (max %d)\n", c.TitleRule, c.TitleMax)
		}
		for _, n := range c.Norms {
			_, _ = fmt.Fprintf(out, "  - %s\n", n)
		}
	}
	return 0
}

// loadLaunch reads the ledger and projects one product's launch state.
func loadLaunch(prog, ledger, product string, errOut io.Writer) (gtm.ProductLaunch, int) {
	events, err := (gtm.LaunchLedger{Path: ledger}).Read()
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch:", err)
		return gtm.ProductLaunch{}, 1
	}
	for _, p := range gtm.BuildLaunchesWithCoverage(events, time.Now(), launchCoverage()) {
		if strings.EqualFold(p.Product, product) {
			return p, 0
		}
	}
	fmt.Fprintf(errOut, "%s launch: no ledger events for %q (ledger %s) — `launch plan %s` first\n", prog, product, ledger, product)
	return gtm.ProductLaunch{}, 3
}

func channelRegistryKeys() []string {
	out := make([]string, len(gtm.Channels))
	for i, c := range gtm.Channels {
		out[i] = c.Key
	}
	return out
}
