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
	"strings"
	"time"

	"github.com/nstranquist/ngtm/gtm"
)

var geoLifecycleVerbs = map[string]bool{
	"research": true, "probe": true, "measure": true,
	"emit-ai-info": true, "emit-llmstxt": true, "emit-compare": true,
	"eval": true, "help": true,
}

func cmdGEO(prog string, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		printGEOUsage(prog, out)
		return 0
	}
	switch args[0] {
	case "research":
		return cmdGEOResearch(prog, args[1:], out, errOut)
	case "probe":
		return cmdGEOProbe(prog, args[1:], out, errOut)
	case "measure":
		return cmdGEOMeasure(prog, args[1:], out, errOut)
	case "emit-ai-info":
		return cmdGEOEmitAIInfo(prog, args[1:], out, errOut)
	case "emit-llmstxt":
		return cmdGEOEmitLLMsTxt(prog, args[1:], out, errOut)
	case "emit-compare":
		return cmdGEOEmitCompare(prog, args[1:], out, errOut)
	case "eval":
		return cmdGEOEval(prog, args[1:], out, errOut)
	case "help", "-h", "--help":
		printGEOUsage(prog, out)
		return 0
	default:
		fmt.Fprintf(errOut, "%s geo: unknown verb %q\n", prog, args[0])
		return 2
	}
}

func printGEOUsage(prog string, w io.Writer) {
	fmt.Fprintf(w, `%[1]s geo — prompt-level AI visibility (Mentions-style)

USAGE
  %[1]s geo research <product> --config product.geo.yaml
  %[1]s geo probe <product> --config product.geo.yaml [--engines openai-chat,gemini] [--fixture path] [--limit N]
  %[1]s geo measure <product> [--workspace path] [--strict]
  %[1]s geo emit-ai-info <product> --config product.geo.yaml --out path
  %[1]s geo emit-llmstxt <product> --config product.geo.yaml --out path
  %[1]s geo emit-compare <product> --config product.geo.yaml --out-dir path
  %[1]s geo eval [--strict] [--json]

Engines are honest API labels: openai-chat, openai-search, gemini, grok.
A chat-completions call is not ChatGPT Search. Artifacts reuse the SEO store
(~/.nicos-dev/gtm/seo/<project>/) with kinds geo-prompt-set, geo-probe, geo-measure.
Compare pages are noindex drafts.
`, prog)
}

type geoCommonFlags struct {
	config, workspace string
	asJSON, strict, offline bool
}

func addGEOCommonFlags(fs *flag.FlagSet, c *geoCommonFlags) {
	fs.StringVar(&c.config, "config", "", "tracked GEO product YAML")
	fs.StringVar(&c.workspace, "workspace", "", "exact project artifact workspace")
	fs.BoolVar(&c.asJSON, "json", false, "emit JSON")
	fs.BoolVar(&c.strict, "strict", false, "exit 3 when blockers remain")
	fs.BoolVar(&c.offline, "offline", false, "hermetic: fixture only")
}

func parseGEOArgs(fs *flag.FlagSet, args []string) (string, error) {
	product := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		product = strings.TrimSpace(args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if product == "" && len(fs.Args()) > 0 {
		product = strings.TrimSpace(fs.Args()[0])
	}
	return product, nil
}

func resolveGEOConfig(product string, common geoCommonFlags) (gtm.GEOProductConfig, error) {
	if common.config == "" {
		return gtm.GEOProductConfig{}, errors.New("--config is required")
	}
	cfg, err := gtm.LoadGEOProductConfig(common.config)
	if err != nil {
		return cfg, err
	}
	if product != "" {
		cfg.Product = product
		if cfg.Project == "" {
			cfg.Project = gtm.DefaultSEOProject(product).Project
		}
	}
	return cfg, cfg.NormalizeAndValidate()
}

func resolveGEOStore(cfg gtm.GEOProductConfig, workspace string) (*gtm.SEOStore, error) {
	return gtm.NewSEOStore(strings.TrimSpace(workspace), cfg.Project)
}

func writeGEOJSON(out io.Writer, value any) int {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return 1
	}
	return 0
}

func cmdGEOResearch(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" geo research", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common geoCommonFlags
	addGEOCommonFlags(fs, &common)
	product, err := parseGEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveGEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo research:", err)
		return 2
	}
	store, err := resolveGEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo research:", err)
		return 2
	}
	ref, err := store.WriteArtifact(gtm.GEOKindPromptSet, cfg)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo research:", err)
		return 1
	}
	logRun(map[string]any{"ts": ref.CreatedAt, "surface": prog, "vertical": "geo.research", "subject": cfg.Product, "artifact_id": ref.ID, "prompts": len(cfg.Prompts)})
	if common.asJSON {
		return writeGEOJSON(out, map[string]any{"config": cfg, "artifact": ref})
	}
	fmt.Fprintf(out, "geo research  product=%s  prompts=%d  artifact=%s\n", cfg.Product, len(cfg.Prompts), ref.ID)
	return 0
}

func cmdGEOProbe(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" geo probe", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common geoCommonFlags
	addGEOCommonFlags(fs, &common)
	engines := fs.String("engines", "", "comma-separated engines: openai-chat,openai-search,gemini,grok,fixture")
	fixture := fs.String("fixture", "", "typed probe fixture JSON")
	limit := fs.Int("limit", 0, "probe only the first N prompts")
	model := fs.String("model", "", "override model for every selected engine")
	product, err := parseGEOArgs(fs, args)
	if err != nil {
		return 2
	}
	var engineIDs []gtm.GEOEngineID
	if strings.TrimSpace(*engines) != "" {
		engineIDs, err = gtm.ParseGEOEngines(*engines)
		if err != nil {
			fmt.Fprintln(errOut, "gtm geo probe:", err)
			return 2
		}
	}
	cfg, err := resolveGEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo probe:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	report, err := gtm.RunGEOProbe(ctx, gtm.GEOProbeOptions{
		Config:      cfg,
		Engines:     engineIDs,
		FixturePath: strings.TrimSpace(*fixture),
		Limit:       *limit,
		Model:       strings.TrimSpace(*model),
		Offline:     common.offline,
	})
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo probe:", err)
		return 1
	}
	store, err := resolveGEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo probe:", err)
		return 2
	}
	ref, err := store.WriteArtifact(gtm.GEOKindProbe, report)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo probe:", err)
		return 1
	}
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "geo.probe", "subject": cfg.Product, "artifact_id": ref.ID, "rows": len(report.Rows), "passed": report.Passed})
	code := 0
	if common.strict && !report.Passed {
		code = 3
	}
	if common.asJSON {
		if writeGEOJSON(out, map[string]any{"report": report, "artifact": ref}) != 0 {
			return 1
		}
		return code
	}
	fmt.Fprintf(out, "geo probe  product=%s  rows=%d  passed=%v  artifact=%s\n", cfg.Product, len(report.Rows), report.Passed, ref.ID)
	for _, finding := range report.Findings {
		fmt.Fprintf(out, "  %s  %s\n", finding.Code, finding.Message)
	}
	if !report.Passed && !common.strict {
		fmt.Fprintln(errOut, "geo probe: one or more engines failed; re-run with --strict to exit 3")
	}
	if code == 3 {
		fmt.Fprintln(errOut, "strict GEO probe gate failed")
	}
	return code
}

func cmdGEOMeasure(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" geo measure", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common geoCommonFlags
	addGEOCommonFlags(fs, &common)
	product, err := parseGEOArgs(fs, args)
	if err != nil {
		return 2
	}
	var cfg gtm.GEOProductConfig
	if common.config != "" {
		cfg, err = resolveGEOConfig(product, common)
		if err != nil {
			fmt.Fprintln(errOut, "gtm geo measure:", err)
			return 2
		}
	} else {
		if product == "" {
			fmt.Fprintln(errOut, "gtm geo measure: product or --config is required")
			return 2
		}
		cfg = gtm.GEOProductConfig{SchemaVersion: gtm.GEOSchemaVersion, Project: product, Product: product, Brand: product, Prompts: []gtm.GEOPrompt{{ID: "placeholder", Text: "placeholder"}}}
		if err := cfg.NormalizeAndValidate(); err != nil {
			fmt.Fprintln(errOut, "gtm geo measure:", err)
			return 2
		}
	}
	store, err := resolveGEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo measure:", err)
		return 2
	}
	var probe gtm.GEOProbeReport
	if _, err := store.LoadLatest(gtm.GEOKindProbe, &probe); err != nil {
		fmt.Fprintln(errOut, "gtm geo measure:", err)
		return 1
	}
	if common.config == "" {
		if _, err := store.LoadLatest(gtm.GEOKindPromptSet, &cfg); err != nil {
			fmt.Fprintln(errOut, "gtm geo measure:", err)
			return 1
		}
	}
	report := gtm.BuildGEOMeasure(cfg, probe, time.Now)
	ref, err := store.WriteArtifact(gtm.GEOKindMeasure, report)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo measure:", err)
		return 1
	}
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "geo.measure", "subject": cfg.Product, "artifact_id": ref.ID, "mention_rate": report.MentionRate})
	code := 0
	if common.strict && !report.Passed {
		code = 3
	}
	if common.asJSON {
		if writeGEOJSON(out, map[string]any{"report": report, "artifact": ref}) != 0 {
			return 1
		}
		return code
	}
	fmt.Fprint(out, gtm.FormatGEOTable(*report))
	fmt.Fprintf(out, "artifact=%s\n", ref.ID)
	return code
}

func cmdGEOEmitAIInfo(prog string, args []string, out, errOut io.Writer) int {
	return emitGEOFile(prog, "emit-ai-info", args, out, errOut, func(cfg gtm.GEOProductConfig) (string, string) {
		return gtm.RenderGEOAIInfo(cfg, time.Now), "ai-info.md"
	})
}

func cmdGEOEmitLLMsTxt(prog string, args []string, out, errOut io.Writer) int {
	return emitGEOFile(prog, "emit-llmstxt", args, out, errOut, func(cfg gtm.GEOProductConfig) (string, string) {
		return gtm.RenderGEOLLMsTxt(cfg), "llms.txt"
	})
}

func emitGEOFile(prog, verb string, args []string, out, errOut io.Writer, render func(gtm.GEOProductConfig) (string, string)) int {
	fs := flag.NewFlagSet(prog+" geo "+verb, flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common geoCommonFlags
	addGEOCommonFlags(fs, &common)
	outPath := fs.String("out", "", "write path")
	product, err := parseGEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveGEOConfig(product, common)
	if err != nil {
		fmt.Fprintf(errOut, "gtm geo %s: %v\n", verb, err)
		return 2
	}
	body, fallback := render(cfg)
	path := strings.TrimSpace(*outPath)
	if path == "" {
		path = fallback
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		fmt.Fprintf(errOut, "gtm geo %s: %v\n", verb, err)
		return 1
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintf(errOut, "gtm geo %s: %v\n", verb, err)
		return 1
	}
	if common.asJSON {
		return writeGEOJSON(out, map[string]any{"path": path, "bytes": len(body)})
	}
	fmt.Fprintf(out, "geo %s  path=%s  bytes=%d\n", verb, path, len(body))
	return 0
}

func cmdGEOEmitCompare(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" geo emit-compare", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common geoCommonFlags
	addGEOCommonFlags(fs, &common)
	outDir := fs.String("out-dir", "", "directory for noindex compare drafts")
	product, err := parseGEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveGEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo emit-compare:", err)
		return 2
	}
	dir := strings.TrimSpace(*outDir)
	if dir == "" {
		fmt.Fprintln(errOut, "gtm geo emit-compare: --out-dir is required")
		return 2
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(errOut, "gtm geo emit-compare:", err)
		return 1
	}
	files := map[string]string{
		"index.html": gtm.RenderGEOCompareIndex(cfg, time.Now),
		"best.html":  gtm.RenderGEOCompareBest(cfg, time.Now),
	}
	for _, comp := range cfg.Competitors {
		files["alternative-to-"+slugifyForFile(comp.Name)+".html"] = gtm.RenderGEOCompareAlternative(cfg, comp, time.Now)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			fmt.Fprintln(errOut, "gtm geo emit-compare:", err)
			return 1
		}
	}
	if common.asJSON {
		return writeGEOJSON(out, map[string]any{"out_dir": dir, "files": len(files), "noindex": true})
	}
	fmt.Fprintf(out, "geo emit-compare  dir=%s  files=%d  noindex=true\n", dir, len(files))
	return 0
}

func slugifyForFile(s string) string {
	return gtm.DefaultSEOProject(s).Project
}

func cmdGEOEval(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" geo eval", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "exit 3 when a check fails")
	_ = fs.Bool("offline", false, "accepted for family --offline; eval is already hermetic")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := gtm.RunGEOEval(context.Background())
	if err != nil {
		fmt.Fprintln(errOut, "gtm geo eval:", err)
		return 1
	}
	code := 0
	if *strict && !report.Passed {
		code = 3
	}
	if *asJSON {
		if writeGEOJSON(out, report) != 0 {
			return 1
		}
		return code
	}
	fmt.Fprintf(out, "geo eval  fixture=%s  passed=%v\n", report.Fixture, report.Passed)
	for _, check := range report.Checks {
		mark := "ok"
		if !check.Passed {
			mark = "FAIL"
		}
		fmt.Fprintf(out, "  %s  %s  %s\n", mark, check.Name, check.Details)
	}
	return code
}
