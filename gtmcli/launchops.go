//nolint:errcheck // Terminal presentation writes are best-effort; ledger/network writes are checked.
package gtmcli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

// launchRetro is the weekly self-improvement harness: aggregate which channels
// produced signal, compute kill/double-down rates, and emit next-cohort
// recommendations. Every retro run is appended to the runs ledger (telemetry).
func launchRetro(prog string, args []string, out, errOut io.Writer) int {
	fs, ledger, asJSON := launchFlagSet(prog+" launch retro", errOut)
	week := fs.String("week", "", "ISO week to retro (default: all weeks combined)")
	target := fs.Int("target", 20, "ship-N-a-week goal")
	outPath := fs.String("out", "", "write the retro to a file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	events, err := (gtm.LaunchLedger{Path: *ledger}).Read()
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch retro:", err)
		return 1
	}
	retro := gtm.BuildRetro(gtm.BuildLaunchesWithCoverage(events, time.Now(), launchCoverage()), strings.TrimSpace(*week), *target)
	logRun(map[string]any{
		"ts": nowRFC3339(), "surface": prog, "vertical": "launch.retro", "subject": retro.Week,
		"planned": retro.Planned, "posted": retro.Posted, "measured": retro.Measured,
		"kill_rate": retro.KillRate, "double_down_rate": retro.DoubleDownRate,
	})
	var rendered []byte
	if *asJSON {
		rendered, _ = json.MarshalIndent(retro, "", "  ")
	} else {
		rendered = []byte(retro.Markdown())
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, rendered, 0o644); err != nil {
			fmt.Fprintln(errOut, prog+" launch retro:", err)
			return 1
		}
		fmt.Fprintf(out, "wrote %s (%d bytes)\n", *outPath, len(rendered))
		return 0
	}
	_, _ = out.Write(rendered)
	fmt.Fprintln(out)
	return 0
}

// launchAudit runs ledger integrity checks. --strict exits 3 when anomalies
// exist, so the audit is schedulable as a gate.
func launchAudit(prog string, args []string, out, errOut io.Writer) int {
	fs, ledger, asJSON := launchFlagSet(prog+" launch audit", errOut)
	strict := fs.Bool("strict", false, "exit 3 when anomalies are found")
	verifyReceipts := fs.Bool("verify-receipts", false, "live-verify public receipt reachability, channel host, and product identity")
	receiptTimeout := fs.Duration("receipt-timeout", 30*time.Second, "overall live receipt verification timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *receiptTimeout <= 0 || *receiptTimeout > 2*time.Minute {
		fmt.Fprintln(errOut, prog+" launch audit: --receipt-timeout must be >0 and <=2m")
		return 2
	}
	report, err := (gtm.LaunchLedger{Path: *ledger}).ReadWithIssues()
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch audit:", err)
		return 1
	}
	anomalies := gtm.AuditLaunchLedgerRead(report, time.Now())
	var receiptVerifications []gtm.ReceiptVerification
	if *verifyReceipts {
		ctx, cancel := context.WithTimeout(context.Background(), *receiptTimeout)
		receiptVerifications = gtm.VerifyLaunchReceipts(ctx, report.Events)
		cancel()
		for _, verification := range receiptVerifications {
			if verification.Verified {
				continue
			}
			anomalies = append(anomalies, gtm.LaunchAnomaly{
				Product: verification.Product, Code: verification.Code, Message: verification.Message,
			})
		}
	}
	logRun(map[string]any{"ts": nowRFC3339(), "surface": prog, "vertical": "launch.audit", "events": len(report.Events), "anomalies": len(anomalies), "corrupt_rows": len(report.Issues), "receipt_verifications": len(receiptVerifications)})
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"ledger": *ledger, "events": len(report.Events), "corrupt_rows": len(report.Issues), "anomalies": anomalies, "receipt_verifications": receiptVerifications}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
	} else if len(anomalies) == 0 {
		fmt.Fprintf(out, "ledger clean: %d event(s), no anomalies\n", len(report.Events))
	} else {
		fmt.Fprintf(out, "%d anomaly(ies) over %d valid event(s):\n", len(anomalies), len(report.Events))
		for _, a := range anomalies {
			location := ""
			if a.Line > 0 {
				location = fmt.Sprintf("line %d ", a.Line)
			}
			fmt.Fprintf(out, "  %-20s %-20s %s%s\n", a.Product, a.Code, location, a.Message)
		}
	}
	if *verifyReceipts && len(receiptVerifications) > 0 && !*asJSON {
		fmt.Fprintln(out, "receipt verification:")
		for _, verification := range receiptVerifications {
			status := "FAIL"
			if verification.Verified {
				status = "PASS"
			}
			fmt.Fprintf(out, "  %-4s %-20s %-14s %s\n", status, verification.Product, verification.Code, verification.URL)
		}
	}
	if *strict && len(anomalies) > 0 {
		return 3
	}
	return 0
}

// launchOpen prints prefilled submission/intent URLs for a product's channels —
// the operator-in-loop middle ground between copy-paste and posting APIs: one
// click lands you in the channel's composer with the draft already in place.
func launchOpen(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, _, asJSON := launchFlagSet(prog+" launch open", errOut)
	channel := fs.String("channel", "", "one channel key (default: all launch channels)")
	pitch := fs.String("pitch", "", "one-line value proposition for the prefilled draft")
	link := fs.String("url", "", "the product URL (Show HN submit link target)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch open: product is required")
		return 2
	}
	var keys []string
	if strings.TrimSpace(*channel) != "" {
		keys = []string{*channel}
	}
	specs, unknown := gtm.SelectChannels(keys)
	if len(unknown) > 0 {
		fmt.Fprintf(errOut, "%s launch open: unknown channel(s) %s (known: %s)\n", prog, strings.Join(unknown, ","), strings.Join(channelRegistryKeys(), ", "))
		return 2
	}
	type openLink struct {
		Channel string `json:"channel"`
		URL     string `json:"url"`
	}
	var links []openLink
	for _, spec := range specs {
		d := gtm.OfflineDraft(spec, product, *pitch)
		u := composerURL(spec.Key, d, strings.TrimSpace(*link))
		if u == "" {
			continue
		}
		links = append(links, openLink{Channel: spec.Key, URL: u})
	}
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"product": product, "links": links}, "", "  ")
		_, _ = out.Write(b)
		fmt.Fprintln(out)
		return 0
	}
	for _, link := range links {
		fmt.Fprintf(out, "%-14s %s\n", link.Channel, link.URL)
	}
	fmt.Fprintln(out, "\nResolve [FILL:] slots before submitting; record receipts with `"+prog+" launch posted "+product+" --channel <key> --url <post-url>`.")
	return 0
}

// composerURL builds each channel's prefilled composer link. Channels without
// a prefill API return their plain composer page.
func composerURL(key string, d gtm.ChannelDraft, productURL string) string {
	switch key {
	case "show-hn":
		v := url.Values{"t": {d.Title}}
		if productURL != "" {
			v.Set("u", productURL)
		}
		return "https://news.ycombinator.com/submitlink?" + v.Encode()
	case "x":
		return "https://x.com/intent/post?" + url.Values{"text": {d.Title}}.Encode()
	case "reddit":
		return "https://www.reddit.com/submit?" + url.Values{"title": {d.Title}, "text": {d.Body}}.Encode()
	case "linkedin":
		return "https://www.linkedin.com/feed/?shareActive=true&" + url.Values{"text": {firstLines(d.Body, 4)}}.Encode()
	case "producthunt":
		return "https://www.producthunt.com/posts/new"
	case "indiehackers":
		return "https://www.indiehackers.com/new-post"
	default:
		return ""
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// launchImport brings external analytics into the signal stream: a JSON/CSV
// metrics file, or a live Plausible aggregate (visitors → clicks). Imported
// events carry the importer's name as Source (observed provenance).
func launchImport(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, ledger, asJSON := launchFlagSet(prog+" launch import", errOut)
	file := fs.String("file", "", "metrics file: JSON array [{metric,value,channel,source}] or CSV metric,value[,channel]")
	plausible := fs.String("plausible", "", "Plausible site_id (needs PLAUSIBLE_API_KEY; optional PLAUSIBLE_API_URL for self-hosted)")
	record := fs.Bool("record", false, "append imported signals to the ledger")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" || (strings.TrimSpace(*file) == "" && strings.TrimSpace(*plausible) == "") {
		fmt.Fprintln(errOut, prog+" launch import: product and --file or --plausible are required")
		return 2
	}
	ts := nowRFC3339()
	var sigs []gtm.LaunchEvent
	if p := strings.TrimSpace(*file); p != "" {
		fileSigs, err := readMetricsFile(p, product, ts)
		if err != nil {
			fmt.Fprintln(errOut, prog+" launch import:", err)
			return 1
		}
		sigs = append(sigs, fileSigs...)
	}
	if site := strings.TrimSpace(*plausible); site != "" {
		visitors, err := plausibleVisitors(site)
		if err != nil {
			fmt.Fprintln(errOut, prog+" launch import: plausible:", err)
			return 1
		}
		sigs = append(sigs, gtm.LaunchEvent{
			TS: ts, Product: product, Type: gtm.EventSignal, Channel: "web",
			Metric: gtm.MetricClicks, Value: visitors, Source: gtm.SignalSource("plausible"),
		})
	}
	if *record {
		led := gtm.LaunchLedger{Path: *ledger}
		for _, s := range sigs {
			if err := led.Append(s); err != nil {
				fmt.Fprintln(errOut, prog+" launch import: ledger:", err)
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
	for _, s := range sigs {
		fmt.Fprintf(out, "  %-10s %-16s %10.1f  [%s]\n", s.Channel, s.Metric, s.Value, s.Source)
	}
	if *record {
		fmt.Fprintf(out, "recorded %d imported signal(s) for %s\n", len(sigs), product)
	} else {
		fmt.Fprintf(out, "%d signal(s) parsed (dry run — pass --record)\n", len(sigs))
	}
	return 0
}

// readMetricsFile parses a JSON array or CSV of metric rows into signal events.
func readMetricsFile(path, product, ts string) ([]gtm.LaunchEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mk := func(metric string, value float64, channel, source string) (gtm.LaunchEvent, error) {
		typedMetric, err := gtm.ParseSignalMetric(metric)
		if err != nil {
			return gtm.LaunchEvent{}, err
		}
		if source == "" {
			source = "file-import"
		}
		normalized := strings.ToLower(strings.TrimSpace(source))
		if normalized == string(gtm.SourceOperator) {
			return gtm.LaunchEvent{}, fmt.Errorf("import cannot mint source=%s; use launch signal for operator conversions", gtm.SourceOperator)
		}
		ev := gtm.LaunchEvent{TS: ts, Product: product, Type: gtm.EventSignal, Channel: strings.ToLower(strings.TrimSpace(channel)), Metric: typedMetric, Value: value, Source: gtm.SignalSource(normalized)}
		if err := ev.Validate(); err != nil {
			return gtm.LaunchEvent{}, err
		}
		return ev, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var rows []struct {
			Metric  string  `json:"metric"`
			Value   float64 `json:"value"`
			Channel string  `json:"channel"`
			Source  string  `json:"source"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		var out []gtm.LaunchEvent
		for _, r := range rows {
			if r.Metric == "" {
				continue
			}
			ev, err := mk(r.Metric, r.Value, r.Channel, r.Source)
			if err != nil {
				return nil, fmt.Errorf("invalid JSON metric %q: %w", r.Metric, err)
			}
			out = append(out, ev)
		}
		return out, nil
	}
	// CSV: metric,value[,channel]; a header row is skipped if value won't parse.
	recs, err := csv.NewReader(strings.NewReader(trimmed)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	var out []gtm.LaunchEvent
	for _, rec := range recs {
		if len(rec) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64)
		if err != nil {
			continue // header or junk row
		}
		channel := ""
		if len(rec) > 2 {
			channel = strings.TrimSpace(rec[2])
		}
		ev, err := mk(strings.TrimSpace(rec[0]), v, channel, "")
		if err != nil {
			return nil, fmt.Errorf("invalid CSV metric %q: %w", strings.TrimSpace(rec[0]), err)
		}
		out = append(out, ev)
	}
	return out, nil
}

// plausibleVisitors fetches the 7-day visitor aggregate for a site.
func plausibleVisitors(site string) (float64, error) {
	key := strings.TrimSpace(os.Getenv("PLAUSIBLE_API_KEY"))
	if key == "" {
		return 0, fmt.Errorf("PLAUSIBLE_API_KEY not set (store it via `ndev secrets`)")
	}
	base := strings.TrimSpace(os.Getenv("PLAUSIBLE_API_URL"))
	if base == "" {
		base = "https://plausible.io"
	}
	u := strings.TrimRight(base, "/") + "/api/v1/stats/aggregate?" + url.Values{
		"site_id": {site}, "period": {"7d"}, "metrics": {"visitors"},
	}.Encode()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return 0, fmt.Errorf("GET %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Results struct {
			Visitors struct {
				Value float64 `json:"value"`
			} `json:"visitors"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Results.Visitors.Value, nil
}
