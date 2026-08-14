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

var seoLifecycleVerbs = map[string]bool{
	"research": true, "opportunities": true, "brief": true, "publish": true,
	"measure": true, "retro": true, "audit": true, "eval": true,
	"help": true,
}

func cmdSEO(prog string, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		printSEOUsage(prog, out)
		return 0
	}
	verb := args[0]
	switch verb {
	case "research":
		return cmdSEOResearch(prog, args[1:], out, errOut)
	case "opportunities":
		return cmdSEOOpportunities(prog, args[1:], out, errOut)
	case "brief":
		return cmdSEOBrief(prog, args[1:], out, errOut)
	case "publish":
		return cmdSEOPublish(prog, args[1:], out, errOut)
	case "measure":
		return cmdSEOMeasure(prog, args[1:], out, errOut)
	case "retro":
		return cmdSEORetro(prog, args[1:], out, errOut)
	case "audit":
		return cmdSEOAudit(prog, args[1:], out, errOut)
	case "eval":
		return cmdSEOEval(prog, args[1:], out, errOut)
	case "help", "-h", "--help":
		printSEOUsage(prog, out)
		return 0
	default:
		fmt.Fprintf(errOut, "%s seo: unknown lifecycle verb %q\n", prog, verb)
		return 2
	}
}

func printSEOUsage(prog string, w io.Writer) {
	fmt.Fprintf(w, `%[1]s seo — evidence-first SEO lifecycle

USAGE
  %[1]s seo research <product> [--config project.yaml] [--keywords a,b] [--fixture path] [--strict]
  %[1]s seo opportunities <product> [--workspace path] [--research ref]
  %[1]s seo brief <product> --keyword <keyword> --unique-value <original value or asset>
  %[1]s seo publish <product> [--body text|--body-file path] [--approved --index] [--out-dir path]
  %[1]s seo measure <product> [--fixture path] [--start YYYY-MM-DD --end YYYY-MM-DD] [--strict]
  %[1]s seo retro <product> [--strict]
  %[1]s seo audit <product> [--strict]
  %[1]s seo eval [--strict] [--json]

Every lifecycle artifact is content-addressed under --workspace or
~/.nicos-dev/gtm/seo/<project>. Draft publishing is noindex by default;
indexability requires --approved --index and all configured quality gates.
`, prog)
}

type seoCommonFlags struct {
	config, workspace, keywords, domain, siteURL, contentDir string
	locale, language, device                                 string
	locationCode                                             int
	asJSON, strict, offline                                  bool
}

func addSEOCommonFlags(fs *flag.FlagSet, c *seoCommonFlags) {
	fs.StringVar(&c.config, "config", "", "tracked SEO project YAML")
	fs.StringVar(&c.workspace, "workspace", "", "exact project artifact workspace")
	fs.StringVar(&c.keywords, "keywords", "", "comma-separated seed keywords")
	fs.StringVar(&c.domain, "domain", "", "owned domain")
	fs.StringVar(&c.siteURL, "site-url", "", "owned absolute site URL")
	fs.StringVar(&c.contentDir, "content-dir", "", "bounded local content root")
	fs.StringVar(&c.locale, "locale", "", "single locale name override")
	fs.StringVar(&c.language, "language", "", "single language code override")
	fs.IntVar(&c.locationCode, "location-code", 0, "single provider location code override")
	fs.StringVar(&c.device, "device", "", "single device override: desktop|mobile|tablet")
	fs.BoolVar(&c.asJSON, "json", false, "emit JSON")
	fs.BoolVar(&c.strict, "strict", false, "exit 3 when blockers remain")
	fs.BoolVar(&c.offline, "offline", false, "hermetic: no live feeds or site crawl")
}

func parseSEOArgs(fs *flag.FlagSet, args []string) (string, error) {
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

func resolveSEOConfig(product string, common seoCommonFlags) (gtm.SEOProjectConfig, error) {
	var cfg gtm.SEOProjectConfig
	var err error
	if common.config != "" {
		cfg, err = gtm.LoadSEOProjectConfig(common.config)
		if err != nil {
			return cfg, err
		}
	} else {
		if product == "" {
			return cfg, errors.New("product is required when --config is not supplied")
		}
		cfg = gtm.DefaultSEOProject(product)
	}
	if product != "" {
		cfg.Product = product
	}
	if common.keywords != "" {
		cfg.SeedKeywords = splitCSV(common.keywords)
	}
	if common.domain != "" {
		cfg.Domain = common.domain
	}
	if common.siteURL != "" {
		cfg.SiteURL = common.siteURL
	}
	if common.contentDir != "" {
		cfg.ContentDir = common.contentDir
	}
	if common.locale != "" || common.language != "" || common.locationCode != 0 || common.device != "" {
		locale := gtm.SEOLocale{}
		if len(cfg.Locales) > 0 {
			locale = cfg.Locales[0]
		}
		if common.locale != "" {
			locale.Name = common.locale
		}
		if common.language != "" {
			locale.LanguageCode = common.language
		}
		if common.locationCode != 0 {
			locale.LocationCode = common.locationCode
		}
		if common.device != "" {
			locale.Device = common.device
		}
		cfg.Locales = []gtm.SEOLocale{locale}
	}
	return cfg, cfg.NormalizeAndValidate()
}

func resolveSEOStore(cfg gtm.SEOProjectConfig, workspace string) (*gtm.SEOStore, error) {
	return gtm.NewSEOStore(strings.TrimSpace(workspace), cfg.Project)
}

func cmdSEOResearch(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo research", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	fixture := fs.String("fixture", "", "typed research fixture JSON/YAML")
	tier := fs.String("tier", "", "free|cheap|premium|all|none (defaults to project config)")
	maxPages := fs.Int("max-pages", 50, "bounded owned-content inventory size")
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 2
	}
	if len(cfg.SeedKeywords) == 0 {
		fmt.Fprintln(errOut, "gtm seo research: at least one seed keyword is required in config or --keywords")
		return 2
	}
	tierValue := cfg.Providers.Tier
	if *tier != "" {
		tierValue = *tier
	}
	tiers, noFeeds, err := parseTiers(tierValue, false)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 2
	}
	offline := common.offline || noFeeds
	if common.offline {
		noFeeds = true
		tiers = nil
	}
	eng, err := gtm.NewEngine(gtm.Options{Subject: cfg.Product, Keywords: cfg.SeedKeywords, Tiers: tiers, Offline: offline, NoFeeds: noFeeds || offline}, time.Now)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 1
	}
	var priorMeasurement gtm.SEOMeasurementReport
	var priorMeasurementPtr *gtm.SEOMeasurementReport
	if _, err := store.LoadLatest("measurement", &priorMeasurement); err == nil {
		priorMeasurementPtr = &priorMeasurement
	}
	started := time.Now()
	report, err := eng.RunSEOResearch(ctx, gtm.SEOResearchRunOptions{Config: cfg, Tiers: tiers, PriorMeasurement: priorMeasurementPtr, Offline: offline && *fixture == "", FixturePath: *fixture, MaxPages: *maxPages})
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 1
	}
	ref, err := store.WriteArtifact("research", report)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo research:", err)
		return 1
	}
	report.Artifact = &ref
	logSEOResearchRun(prog, report, time.Since(started))
	if err := emitSEOValue(report, report.Markdown(), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !report.Passed {
		return 3
	}
	return 0
}

func cmdSEOOpportunities(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo opportunities", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	researchRef := fs.String("research", "", "research artifact path or sha256 ID")
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo opportunities:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	var report gtm.SEOResearchReport
	if _, err := store.LoadRef(*researchRef, "research", &report); err != nil {
		fmt.Fprintln(errOut, "gtm seo opportunities:", err)
		return 1
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# SEO opportunities: %s\n\n| Keyword | Score | Confidence | Volume | Intent | Status |\n|---|---:|---:|---:|---|---|\n", report.Product)
	for _, kw := range report.Keywords {
		volume := "—"
		if kw.SearchVolume != nil {
			volume = fmt.Sprintf("%.0f", *kw.SearchVolume)
		}
		fmt.Fprintf(&md, "| %s | %.3f | %.3f | %s | %s | %s |\n", kw.Keyword, kw.Opportunity.Score, kw.Opportunity.Confidence, volume, firstSEO(kw.Intent, "unknown"), map[bool]string{true: "accepted", false: "hold"}[kw.Opportunity.Accepted])
	}
	if err := emitSEOValue(map[string]any{"research_id": report.ID, "project": report.Project, "coverage": report.Coverage, "opportunities": report.Keywords}, md.String(), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !report.Passed {
		return 3
	}
	return 0
}

func cmdSEOBrief(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo brief", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	keyword := fs.String("keyword", "", "researched keyword")
	unique := fs.String("unique-value", "", "concrete original value, evidence, tool, or asset")
	audience := fs.String("audience", "", "specific reader")
	format := fs.String("format", "", "content format override")
	researchRef := fs.String("research", "", "research artifact path or sha256 ID")
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo brief:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo brief:", err)
		return 1
	}
	var research gtm.SEOResearchReport
	if _, err := store.LoadRef(*researchRef, "research", &research); err != nil {
		fmt.Fprintln(errOut, "gtm seo brief:", err)
		return 1
	}
	brief, err := gtm.BuildSEOBrief(cfg, research, gtm.SEOBriefRequest{Keyword: *keyword, UniqueValue: *unique, Audience: *audience, Format: *format}, nil)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo brief:", err)
		return 1
	}
	ref, err := store.WriteArtifact("brief", brief)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	brief.Artifact = &ref
	if err := emitSEOValue(brief, brief.Markdown(), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !brief.Passed {
		return 3
	}
	return 0
}

func cmdSEOPublish(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo publish", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	body := fs.String("body", "", "page body text")
	bodyFile := fs.String("body-file", "", "page body text file")
	title := fs.String("title", "", "page title")
	description := fs.String("description", "", "meta description")
	slug := fs.String("slug", "", "page slug")
	approved := fs.Bool("approved", false, "human approval acknowledgement")
	index := fs.Bool("index", false, "request indexable output")
	outDir := fs.String("out-dir", "", "local output directory")
	briefRef := fs.String("brief", "", "brief artifact path or sha256 ID")
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo publish:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo publish:", err)
		return 1
	}
	var brief gtm.SEOBrief
	if _, err := store.LoadRef(*briefRef, "brief", &brief); err != nil {
		fmt.Fprintln(errOut, "gtm seo publish:", err)
		return 1
	}
	bodyText := *body
	if *bodyFile != "" {
		b, err := os.ReadFile(*bodyFile)
		if err != nil {
			fmt.Fprintln(errOut, "gtm seo publish:", err)
			return 1
		}
		bodyText = string(b)
	}
	dir := *outDir
	if dir == "" {
		dir = filepath.Join(store.Root, "site")
	}
	manifest, err := gtm.PublishSEOPage(cfg, brief, gtm.SEOPublishRequest{Title: *title, Description: *description, Body: bodyText, Slug: *slug, Approved: *approved, Index: *index}, dir, nil)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo publish:", err)
		return 1
	}
	ref, err := store.WriteArtifact("publish", manifest)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	manifest.Artifact = &ref
	indexable := 0
	for _, page := range manifest.Pages {
		if page.Indexable {
			indexable++
		}
	}
	logRun(map[string]any{"ts": manifest.Generated, "surface": prog, "vertical": "seo.publish", "subject": cfg.Product, "published": len(manifest.Pages), "indexable": indexable, "blockers": countSEOBlockers(manifest.Findings), "artifact_id": ref.ID})
	if err := emitSEOValue(manifest, renderSEOPublishMarkdown(manifest), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if (common.strict || *index) && !manifest.Passed {
		return 3
	}
	return 0
}

func cmdSEOMeasure(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo measure", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	fixture := fs.String("fixture", "", "typed measurement fixture JSON")
	start := fs.String("start", "", "start date YYYY-MM-DD")
	end := fs.String("end", "", "end date YYYY-MM-DD")
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	if common.offline && strings.TrimSpace(*fixture) == "" {
		fmt.Fprintln(errOut, "gtm seo measure: --offline requires --fixture (refuses live GSC/GA4/PageSpeed)")
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo measure:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo measure:", err)
		return 1
	}
	var research gtm.SEOResearchReport
	if _, err := store.LoadLatest("research", &research); err != nil {
		fmt.Fprintln(errOut, "gtm seo measure:", err)
		return 1
	}
	var publish gtm.SEOPublishManifest
	var publishPtr *gtm.SEOPublishManifest
	if publishRef, err := store.LoadLatest("publish", &publish); err == nil {
		publish.Artifact = &publishRef
		publishPtr = &publish
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := gtm.MeasureSEO(ctx, cfg, &research, publishPtr, gtm.SEOMeasurementOptions{FixturePath: *fixture, StartDate: *start, EndDate: *end})
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo measure:", err)
		return 1
	}
	ref, err := store.WriteArtifact("measurement", report)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	report.Artifact = &ref
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "seo.measure", "subject": cfg.Product, "providers": report.Providers, "measurement_coverage": report.Coverage, "blockers": countSEOBlockers(report.Findings), "artifact_id": ref.ID})
	if err := emitSEOValue(report, renderSEOMeasurementMarkdown(report), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !report.Passed {
		return 3
	}
	return 0
}

func cmdSEORetro(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo retro", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo retro:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo retro:", err)
		return 1
	}
	var research gtm.SEOResearchReport
	if _, err := store.LoadLatest("research", &research); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	var measurement gtm.SEOMeasurementReport
	if _, err := store.LoadLatest("measurement", &measurement); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	report := gtm.BuildSEORetro(research, measurement, nil)
	ref, err := store.WriteArtifact("retro", report)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	report.Artifact = &ref
	decisionCounts := map[string]int{}
	for _, decision := range report.Decisions {
		decisionCounts[decision.Decision]++
	}
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "seo.retro", "subject": cfg.Product, "decisions": decisionCounts, "blockers": countSEOBlockers(report.Findings), "artifact_id": ref.ID})
	if err := emitSEOValue(report, report.Markdown(), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !report.Passed {
		return 3
	}
	return 0
}

func cmdSEOAudit(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo audit", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var common seoCommonFlags
	addSEOCommonFlags(fs, &common)
	product, err := parseSEOArgs(fs, args)
	if err != nil {
		return 2
	}
	cfg, err := resolveSEOConfig(product, common)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo audit:", err)
		return 2
	}
	store, err := resolveSEOStore(cfg, common.workspace)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo audit:", err)
		return 1
	}
	report := gtm.AuditSEOStore(store, cfg)
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "seo.audit", "subject": cfg.Product, "blockers": report.Blockers, "warnings": report.Warnings, "artifacts": len(report.Artifacts)})
	if err := emitSEOValue(report, report.Markdown(), common.asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if common.strict && !report.Passed {
		return 3
	}
	return 0
}

func cmdSEOEval(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" seo eval", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "exit 3 on a failed quality check")
	_ = fs.Bool("offline", false, "accepted for family-flag peel; eval is always hermetic")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := gtm.RunSEOEval(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "gtm seo eval:", err)
		return 1
	}
	logRun(map[string]any{"ts": report.Generated, "surface": prog, "vertical": "seo.eval", "subject": report.Fixture, "passed": report.Passed, "checks": len(report.Checks)})
	if err := emitSEOValue(report, report.Markdown(), *asJSON, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if *strict && !report.Passed {
		return 3
	}
	return 0
}

func emitSEOValue(v any, markdown string, asJSON bool, out io.Writer) error {
	if asJSON {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	_, err := fmt.Fprint(out, markdown)
	if err == nil && !strings.HasSuffix(markdown, "\n") {
		_, err = fmt.Fprintln(out)
	}
	return err
}

func renderSEOPublishMarkdown(m *gtm.SEOPublishManifest) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO publish: %s\n\n- Status: %s\n- Output: `%s`\n\n", m.Project, map[bool]string{true: "PASS", false: "BLOCKED"}[m.Passed], m.OutputDir)
	for _, p := range m.Pages {
		fmt.Fprintf(&out, "- `%s` — robots `%s`, words %d, indexable %t\n", p.Path, p.Robots, p.WordCount, p.Indexable)
	}
	for _, f := range m.Findings {
		fmt.Fprintf(&out, "- **%s** `%s`: %s\n", strings.ToUpper(f.Severity), f.Code, f.Message)
	}
	return out.String()
}

func renderSEOMeasurementMarkdown(m *gtm.SEOMeasurementReport) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO measurement: %s\n\n- Status: %s\n- Provenance: %s\n- Coverage: %.1f%%\n- Rows: %d\n\n", m.Project, map[bool]string{true: "PASS", false: "BLOCKED"}[m.Passed], m.Provenance, 100*m.Coverage, len(m.Rows))
	for _, f := range m.Findings {
		fmt.Fprintf(&out, "- **%s** `%s`: %s\n", strings.ToUpper(f.Severity), f.Code, f.Message)
	}
	return out.String()
}

func logSEOResearchRun(prog string, r *gtm.SEOResearchReport, latency time.Duration) {
	artifact := ""
	if r.Artifact != nil {
		artifact = r.Artifact.ID
	}
	logRun(map[string]any{
		"ts": r.Generated, "surface": prog, "vertical": "seo.research", "subject": r.Product,
		"candidates": r.Coverage.Candidates, "accepted": r.Coverage.Accepted, "live_evidence": r.Coverage.LiveEvidence,
		"serp_coverage": r.Coverage.SERP, "volume_coverage": r.Coverage.Volume, "intent_coverage": r.Coverage.Intent,
		"difficulty_coverage": r.Coverage.Difficulty, "trend_coverage": r.Coverage.Trend, "first_party_coverage": r.Coverage.FirstParty,
		"average_opportunity_score": r.Coverage.AverageScore, "providers": r.Providers, "blockers": countSEOBlockers(r.Findings),
		"latency_ms": latency.Milliseconds(), "artifact_id": artifact,
	})
}

func countSEOBlockers(findings []gtm.SEOFinding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == "blocker" {
			n++
		}
	}
	return n
}

func firstSEO(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
