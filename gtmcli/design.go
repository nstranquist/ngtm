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

	"github.com/nstranquist/ngtm/internal/design"
)

// cmdDesign is the design-system generator: it builds an accessible, theory-
// grounded color/design system (OKLCH palette, WCAG-checked contrast, modular
// type & spacing), scores it /10 against a computable rubric, and renders a
// self-contained preview page. With --tune it runs the deterministic self-review
// loop and keeps the best-scoring system, optionally screenshotting it via the
// shared headless-Chrome surface. Generative sibling of `landing`.
func cmdDesign(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" design", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var (
		subject  = fs.String("subject", "", "brand/product name (seeds the palette)")
		seed     = fs.String("seed", "", "brand color seed #rrggbb (optional; derived from name when absent)")
		harmonyS = fs.String("harmony", "analogous", "accent scheme: analogous|complementary|triadic|split|mono")
		modeS    = fs.String("mode", "dark", "primary render mode: dark|light")
		both     = fs.Bool("both", false, "generate both dark and light palettes")
		ratio    = fs.Float64("type-ratio", 1.25, "modular type scale ratio (1.2|1.25|1.333)")
		tune     = fs.Bool("tune", false, "run the self-review loop and keep the best-scoring system")
		rounds   = fs.Int("rounds", 0, "tune: candidate cap (0 = full deterministic grid)")
		shot     = fs.Bool("screenshot", false, "capture a PNG of the preview via headless Chrome")
		shotOut  = fs.String("screenshot-out", "", "screenshot PNG path (default temp file)")
		vision   = fs.Bool("vision", false, "perceptual channel: screenshot → vision LLM scores it /10, blended with the rubric (needs ANTHROPIC_API_KEY + Chrome)")
		visModel = fs.String("vision-model", design.DefaultVisionModel, "vision model (e.g. claude-haiku-4-5 for the cheap path)")
		audit    = fs.String("audit", "", "tune telemetry JSONL path")
		outPath  = fs.String("out", "", "write preview HTML to a file (default stdout)")
		cssOnly  = fs.Bool("css", false, "emit only the :root design-token block")
		landCSS  = fs.Bool("landing-css", false, "emit a :root block in the landing/garrid token vocabulary (--bg/--panel/--accent/...)")
		formatS  = fs.String("format", "", "output format: dtcg (W3C/DTCG design-token JSON for Style Dictionary / Panda)")
		asJSON   = fs.Bool("json", false, "emit theme tokens + scorecard as JSON")
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
	subj := strings.TrimSpace(*subject)
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(positional, " "))
	}
	if subj == "" {
		subj = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if subj == "" {
		_, _ = fmt.Fprintln(errOut, "gtm design: name is required (positionally or via --subject)")
		return 2
	}

	mode := design.ModeDark
	if strings.EqualFold(*modeS, "light") {
		mode = design.ModeLight
	}
	modes := []design.Mode{mode}
	if *both {
		modes = []design.Mode{design.ModeDark, design.ModeLight}
	}
	o := design.Options{
		Name:      subj,
		Seed:      strings.TrimSpace(*seed),
		Harmony:   parseHarmony(*harmonyS),
		Modes:     modes,
		TypeRatio: *ratio,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var theme design.Theme
	var tuneLine, shotPath string
	if *tune {
		res, err := design.Tune(ctx, design.TuneOptions{
			Base: o, ScoreMode: mode, Rounds: *rounds,
			AuditPath: strings.TrimSpace(*audit), Screenshot: *shot, ScreenshotOut: strings.TrimSpace(*shotOut),
		})
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
		theme = res.Best.Theme
		tuneLine = res.SummaryLine()
		shotPath = res.ScreenshotPath
		if res.ScreenshotErr != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design: screenshot skipped:", res.ScreenshotErr)
		}
	} else {
		theme = design.Generate(o)
		if *shot {
			p, err := design.ScreenshotTheme(ctx, theme, mode, strings.TrimSpace(*shotOut))
			if err != nil {
				_, _ = fmt.Fprintln(errOut, "gtm design: screenshot skipped:", err)
			} else {
				shotPath = p
			}
		}
	}
	card := design.Score(theme, mode)

	// Perceptual channel (opt-in): screenshot → vision LLM → blended score.
	var vis *design.VisionScore
	var blended float64
	if *vision {
		v, err := design.EvaluateVision(ctx, theme, mode, design.VisionOptions{Model: strings.TrimSpace(*visModel)})
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design: vision channel skipped:", err)
		} else {
			vis = &v
			blended = design.Blend(card.Overall, v.Score)
		}
	}

	switch {
	case strings.EqualFold(*formatS, "dtcg"):
		b, err := design.DTCG(theme)
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
		if err := writeOut(*outPath, string(b), out); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
	case *asJSON:
		payload := map[string]any{
			"name":      theme.Name,
			"harmony":   string(theme.Harmony),
			"mode":      string(mode),
			"theme":     theme,
			"scorecard": card,
		}
		if tuneLine != "" {
			payload["tune"] = tuneLine
		}
		if shotPath != "" {
			payload["screenshot"] = shotPath
		}
		if vis != nil {
			payload["vision"] = vis
			payload["blended"] = blended
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
	case *landCSS:
		if err := writeOut(*outPath, design.LandingRootCSS(theme, mode)+"\n", out); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
	case *cssOnly:
		if err := writeOut(*outPath, design.CSSVars(theme, mode), out); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
	default:
		if err := writeOut(*outPath, design.RenderPreviewHTML(theme, mode), out); err != nil {
			_, _ = fmt.Fprintln(errOut, "gtm design:", err)
			return 1
		}
	}

	// human scorecard to stderr (so stdout stays the artifact)
	printScorecard(errOut, subj, mode, card, tuneLine, shotPath, strings.TrimSpace(*outPath))
	if vis != nil {
		_, _ = fmt.Fprintf(errOut, "  vision (%s): %.1f  (hierarchy %.1f · polish %.1f · harmony %.1f)\n",
			vis.Model, vis.Score, vis.Hierarchy, vis.Polish, vis.Harmony)
		for _, r := range vis.Reasons {
			_, _ = fmt.Fprintf(errOut, "    · %s\n", r)
		}
		_, _ = fmt.Fprintf(errOut, "  BLENDED: %.2f / 10  (rubric %.0f%% + vision %.0f%%)\n",
			blended, design.BlendWeightDeterministic*100, (1-design.BlendWeightDeterministic)*100)
	}
	return 0
}

func printScorecard(w io.Writer, name string, mode design.Mode, card design.Scorecard, tuneLine, shot, outPath string) {
	_, _ = fmt.Fprintf(w, "\n  %s — design system (%s)\n", name, mode)
	_, _ = fmt.Fprintf(w, "  overall: %.2f / 10\n", card.Overall)
	for _, d := range card.Dimensions {
		_, _ = fmt.Fprintf(w, "    %-16s %.2f  (w%.2f)\n", d.Name, d.Score, d.Weight)
	}
	if f := card.Findings(); len(f) > 0 {
		_, _ = fmt.Fprintln(w, "  findings:")
		for _, item := range f {
			_, _ = fmt.Fprintf(w, "    - %s\n", item)
		}
	}
	if tuneLine != "" {
		_, _ = fmt.Fprintf(w, "  tune: %s\n", tuneLine)
	}
	if shot != "" {
		_, _ = fmt.Fprintf(w, "  screenshot: %s\n", shot)
	}
	if outPath != "" {
		_, _ = fmt.Fprintf(w, "  written: %s\n", outPath)
	}
}

func writeOut(path, content string, out io.Writer) error {
	if path == "" {
		_, err := io.WriteString(out, content)
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// designRootForLanding generates a design system for a brand and returns its
// `:root{}` token block in landing's vocabulary, so `ngtm landing --design`
// pages adopt the OKLCH system. Optionally tunes to the best-scoring candidate.
func designRootForLanding(name, seed, harmony string, tune bool) string {
	o := design.Options{
		Name:    strings.TrimSpace(name),
		Seed:    strings.TrimSpace(seed),
		Harmony: parseHarmony(harmony),
		Modes:   []design.Mode{design.ModeDark},
	}
	theme := design.Generate(o)
	if tune {
		if res, err := design.Tune(context.Background(), design.TuneOptions{Base: o, ScoreMode: design.ModeDark}); err == nil {
			theme = res.Best.Theme
		}
	}
	return design.LandingRootCSS(theme, design.ModeDark)
}

func parseHarmony(s string) design.HarmonyStrategy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mono", "monochromatic":
		return design.HarmonyMono
	case "complement", "complementary":
		return design.HarmonyComplementary
	case "triadic", "triad":
		return design.HarmonyTriadic
	case "split", "split-complement", "split-complementary":
		return design.HarmonySplit
	default:
		return design.HarmonyAnalogous
	}
}

// designToolSchema is the input contract for the gtm_design MCP tool.
func designToolSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"subject"},
		"properties": map[string]any{
			"subject": map[string]any{"type": "string", "description": "brand/product name (seeds the palette)"},
			"seed":    map[string]any{"type": "string", "description": "brand color seed as #rrggbb (optional; derived from name when absent)"},
			"harmony": map[string]any{"type": "string", "enum": []string{"analogous", "complementary", "triadic", "split", "mono"}, "description": "accent color scheme"},
			"mode":    map[string]any{"type": "string", "enum": []string{"dark", "light"}, "description": "mode to score/optimize for"},
			"tune":    map[string]any{"type": "boolean", "description": "run the self-review loop and return the best-scoring system"},
		},
	}
}

func argString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := args[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func argBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

// runDesignMCP builds a design system from MCP arguments and returns the
// scorecard + tokens as JSON. Mirrors runLandingMCP.
func runDesignMCP(ctx context.Context, args map[string]any) (string, bool) {
	name := strings.TrimSpace(argString(args, "subject", "name"))
	if name == "" {
		return "design: subject is required", true
	}
	mode := design.ModeDark
	if strings.EqualFold(argString(args, "mode"), "light") {
		mode = design.ModeLight
	}
	o := design.Options{
		Name:    name,
		Seed:    strings.TrimSpace(argString(args, "seed")),
		Harmony: parseHarmony(argString(args, "harmony")),
		Modes:   []design.Mode{design.ModeDark, design.ModeLight},
	}
	var theme design.Theme
	var tuneLine string
	if argBool(args, "tune") {
		res, err := design.Tune(ctx, design.TuneOptions{Base: o, ScoreMode: mode})
		if err != nil {
			return "design: " + err.Error(), true
		}
		theme = res.Best.Theme
		tuneLine = res.SummaryLine()
	} else {
		theme = design.Generate(o)
	}
	card := design.Score(theme, mode)
	payload := map[string]any{
		"name":      theme.Name,
		"harmony":   string(theme.Harmony),
		"mode":      string(mode),
		"theme":     theme,
		"scorecard": card,
		"css":       design.CSSVars(theme, mode),
	}
	if tuneLine != "" {
		payload["tune"] = tuneLine
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "design: " + err.Error(), true
	}
	return string(b), false
}
