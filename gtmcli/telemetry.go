package gtmcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nstranquist/ngtm/gtm"
)

func cmdTelemetry(prog string, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprintf(out, `Usage: %s telemetry <status|query|export|import> [flags]

SQLite is the full history. Do not rg ~/.nicos-dev/gtm/*.jsonl — that is how
agents burn a context window. Use query (capped, no extra_json).

The live JSONL shadow is rotated at 64KiB (NGTM_JSONL_MAX_BYTES; 0=off).
Rotations are kept as path.<UTC stamp>. Nothing is deleted.

  status [--json]
  query [--launch] [--product p] [--vertical v] [--subject s] [--limit n] [--json]
  export --runs|--launch [--out]
  import [--json]
`, prog)
		return 0
	}
	switch args[0] {
	case "status":
		return cmdTelemetryStatus(args[1:], out, errOut)
	case "query":
		return cmdTelemetryQuery(args[1:], out, errOut)
	case "export":
		return cmdTelemetryExport(args[1:], out, errOut)
	case "import":
		return cmdTelemetryImport(args[1:], out, errOut)
	default:
		_, _ = fmt.Fprintf(errOut, "gtm telemetry: unknown verb %q\n", args[0])
		return 2
	}
}

func cmdTelemetryStatus(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("telemetry status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	dbPath := fs.String("db", "", "sqlite path (default $NGTM_TELEMETRY_DB)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, err := gtm.OpenTelemetryStore(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	st, err := s.Status()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	if *asJSON {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = out.Write(b)
		_, _ = fmt.Fprintln(out)
		return 0
	}
	_, _ = fmt.Fprintf(out, "ngtm telemetry  %s\n", st.Path)
	_, _ = fmt.Fprintf(out, "  schema %d  imported_jsonl=%v %s  jsonl_shadow=%v  hot=%dB rotated=%d\n",
		st.SchemaVersion, st.ImportedJSONL, st.ImportedAt, st.JSONLShadow, st.JSONLHotBytes, st.JSONLRotated)
	_, _ = fmt.Fprintf(out, "  runs %d  launch_events %d  range %s .. %s\n", st.Runs, st.LaunchEvents, st.FirstRun, st.LastRun)
	for _, v := range st.ByVertical {
		_, _ = fmt.Fprintf(out, "    %-16s %4d  avg_panel %.1f\n", v.Vertical, v.Runs, v.AvgPanelMedian)
	}
	return 0
}

func cmdTelemetryQuery(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("telemetry query", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	dbPath := fs.String("db", "", "sqlite path")
	vertical := fs.String("vertical", "", "exact vertical filter (runs)")
	subject := fs.String("subject", "", "subject substring (runs)")
	product := fs.String("product", "", "exact product filter (launch)")
	launch := fs.Bool("launch", false, "query launch events instead of intel runs")
	limit := fs.Int("limit", 20, "max rows (cap 100)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, err := gtm.OpenTelemetryStore(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	if *launch {
		rows, err := s.QueryLaunch(gtm.LaunchQuery{Product: *product, Limit: *limit})
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		if *asJSON {
			b, err := json.MarshalIndent(map[string]any{"kind": "launch", "count": len(rows), "limit": *limit, "rows": rows}, "", "  ")
			if err != nil {
				_, _ = fmt.Fprintln(errOut, "gtm:", err)
				return 1
			}
			_, _ = out.Write(b)
			_, _ = fmt.Fprintln(out)
			return 0
		}
		_, _ = fmt.Fprintf(out, "ts\ttype\tproduct\tweek\tchannel\n")
		for _, r := range rows {
			_, _ = fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", r.TS, r.Type, r.Product, r.Week, r.Channel)
		}
		return 0
	}
	rows, err := s.QueryRuns(gtm.RunQuery{Vertical: *vertical, Subject: *subject, Limit: *limit})
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	if *asJSON {
		b, err := json.MarshalIndent(map[string]any{"kind": "runs", "count": len(rows), "limit": *limit, "rows": rows}, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = out.Write(b)
		_, _ = fmt.Fprintln(out)
		return 0
	}
	_, _ = fmt.Fprintf(out, "ts\tvertical\tsubject\tpanel\n")
	for _, r := range rows {
		panel := ""
		if r.PanelMedian != nil {
			panel = fmt.Sprintf("%.1f", *r.PanelMedian)
		}
		_, _ = fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", r.TS, r.Vertical, r.Subject, panel)
	}
	return 0
}

func cmdTelemetryExport(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("telemetry export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	runs := fs.Bool("runs", false, "export intel runs")
	launch := fs.Bool("launch", false, "export launch events")
	outPath := fs.String("out", "", "output path (default stdout)")
	dbPath := fs.String("db", "", "sqlite path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runs == *launch {
		_, _ = fmt.Fprintln(errOut, "gtm telemetry export: pass exactly one of --runs or --launch")
		return 2
	}
	s, err := gtm.OpenTelemetryStore(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	write := func(b []byte) error {
		_, err := fmt.Fprintf(out, "%s\n", strings.TrimRight(string(b), "\n"))
		return err
	}
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		defer func() { _ = f.Close() }()
		write = func(b []byte) error {
			_, err := fmt.Fprintf(f, "%s\n", strings.TrimRight(string(b), "\n"))
			return err
		}
	}
	if *runs {
		err = s.ExportRunsJSONL(write)
	} else {
		err = s.ExportLaunchJSONL(write)
	}
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	return 0
}

func cmdTelemetryImport(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("telemetry import", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	dbPath := fs.String("db", "", "sqlite path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, err := gtm.OpenTelemetryStore(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	before, err := s.Status()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	if err := s.ImportJSONL(); err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	after, err := s.Status()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "gtm:", err)
		return 1
	}
	addedRuns := after.Runs - before.Runs
	addedLaunch := after.LaunchEvents - before.LaunchEvents
	if *asJSON {
		b, err := json.MarshalIndent(map[string]any{
			"added_runs": addedRuns, "added_launch_events": addedLaunch, "status": after,
		}, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm:", err)
			return 1
		}
		_, _ = out.Write(b)
		_, _ = fmt.Fprintln(out)
		return 0
	}
	_, _ = fmt.Fprintf(out, "imported +%d runs +%d launch events (now %d / %d)\n",
		addedRuns, addedLaunch, after.Runs, after.LaunchEvents)
	return 0
}
