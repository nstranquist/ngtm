package design

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nstranquist/pageskein/browser"
)

// TuneOptions configures the self-review search.
type TuneOptions struct {
	Base          Options // base generation options (Name, Seed, Modes, ...)
	Rounds        int     // candidates to evaluate; 0 = full deterministic grid
	ScoreMode     Mode    // mode to optimize for; default dark
	AuditPath     string  // JSONL telemetry path; empty = no file (still returned)
	Screenshot    bool    // capture a PNG of the winning preview via headless Chrome
	ScreenshotOut string  // PNG path; default temp file
}

// CandidateParams are the knobs the loop varies.
type CandidateParams struct {
	Harmony   HarmonyStrategy `json:"harmony"`
	HueShift  float64         `json:"hue_shift"`
	TypeRatio float64         `json:"type_ratio"`
}

// Candidate is one evaluated design system.
type Candidate struct {
	Round  int             `json:"round"`
	Params CandidateParams `json:"params"`
	Score  float64         `json:"overall"`
	Card   Scorecard       `json:"-"`
	Theme  Theme           `json:"-"`
}

// TuneResult is the loop outcome.
type TuneResult struct {
	Best           Candidate
	All            []Candidate
	Rounds         int
	Improvements   int
	ScreenshotPath string
	ScreenshotErr  error
}

// candidateGrid is the deterministic search space: every harmony × a few hue
// nudges × the three classic modular ratios. Deterministic (no RNG) so a given
// brand always converges to the same best system and the telemetry is reproducible.
func candidateGrid() []CandidateParams {
	hueShifts := []float64{0, 12, -12, 24, -24}
	ratios := []float64{1.2, 1.25, 1.333}
	var out []CandidateParams
	for _, h := range allHarmonies {
		for _, hs := range hueShifts {
			for _, r := range ratios {
				out = append(out, CandidateParams{Harmony: h, HueShift: hs, TypeRatio: r})
			}
		}
	}
	return out
}

// Tune runs the deterministic self-review loop: generate each candidate, score
// it on the chosen mode, keep the best, and emit per-round telemetry. Optionally
// screenshots the winning preview for the perceptual channel / human review.
func Tune(ctx context.Context, opts TuneOptions) (TuneResult, error) {
	mode := opts.ScoreMode
	if mode == "" {
		mode = ModeDark
	}
	baseSeed := resolveSeed(opts.Base)
	grid := candidateGrid()
	if opts.Rounds > 0 && opts.Rounds < len(grid) {
		grid = grid[:opts.Rounds]
	}

	var audit *os.File
	if opts.AuditPath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.AuditPath), 0o755); err == nil {
			audit, _ = os.OpenFile(opts.AuditPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if audit != nil {
				defer audit.Close()
			}
		}
	}

	res := TuneResult{}
	bestScore := -1.0
	for i, params := range grid {
		seed := baseSeed
		seed.H = wrapHue(seed.H + params.HueShift)
		co := opts.Base
		co.Harmony = params.Harmony
		co.TypeRatio = params.TypeRatio
		theme := GenerateSeeded(seed, co)
		card := Score(theme, mode)
		cand := Candidate{Round: i + 1, Params: params, Score: card.Overall, Card: card, Theme: theme}
		res.All = append(res.All, cand)
		if card.Overall > bestScore {
			bestScore = card.Overall
			res.Best = cand
			if i > 0 {
				res.Improvements++
			}
		}
		writeAudit(audit, cand, res.Best.Score)
	}
	res.Rounds = len(grid)

	if opts.Screenshot {
		path, err := ScreenshotTheme(ctx, res.Best.Theme, mode, opts.ScreenshotOut)
		res.ScreenshotPath, res.ScreenshotErr = path, err
	}
	return res, nil
}

// auditRecord is one JSONL telemetry line.
type auditRecord struct {
	TS         string             `json:"ts"`
	Round      int                `json:"round"`
	Params     CandidateParams    `json:"params"`
	Overall    float64            `json:"overall"`
	BestSoFar  float64            `json:"best_so_far"`
	Dimensions map[string]float64 `json:"dimensions"`
}

func writeAudit(f *os.File, c Candidate, bestSoFar float64) {
	if f == nil {
		return
	}
	dims := make(map[string]float64, len(c.Card.Dimensions))
	for _, d := range c.Card.Dimensions {
		dims[d.Name] = d.Score
	}
	rec := auditRecord{
		TS:         time.Now().UTC().Format(time.RFC3339),
		Round:      c.Round,
		Params:     c.Params,
		Overall:    c.Score,
		BestSoFar:  bestSoFar,
		Dimensions: dims,
	}
	if b, err := json.Marshal(rec); err == nil {
		f.Write(append(b, '\n'))
	}
}

// ScreenshotTheme renders the preview, serves it locally, and captures a PNG to
// a file through the shared headless-Chrome surface. Best-effort: a missing
// browser returns an error without failing the caller.
func ScreenshotTheme(ctx context.Context, t Theme, mode Mode, out string) (string, error) {
	if out == "" {
		safe := t.Name
		if safe == "" {
			safe = "design"
		}
		out = filepath.Join(os.TempDir(), fmt.Sprintf("design-%s-%s.png", slug(safe), mode))
	}
	png, err := ScreenshotThemeBytes(ctx, t, mode)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, png, 0o644); err != nil {
		return "", err
	}
	return out, nil
}

// ScreenshotThemeBytes renders the preview and returns the PNG bytes directly —
// used by the perceptual (vision) channel, which feeds the image to an LLM.
func ScreenshotThemeBytes(ctx context.Context, t Theme, mode Mode) ([]byte, error) {
	html := RenderPreviewHTML(t, mode)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	defer srv.Close()
	lo := browser.DefaultLaunch()
	lo.Viewport = browser.Viewport{Width: 1120, Height: 1400}
	return browser.Screenshot(ctx, lo, srv.URL)
}

func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r == ' ' || r == '-' || r == '_':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "design"
	}
	return string(out)
}

// SummaryLine is a compact one-line description of a tune result for CLI/logs.
func (r TuneResult) SummaryLine() string {
	return fmt.Sprintf("best %.1f/10 — %s, hue%+.0f°, ratio %.3f (%d candidates, %d improvements)",
		r.Best.Score, r.Best.Params.Harmony, r.Best.Params.HueShift, r.Best.Params.TypeRatio,
		r.Rounds, r.Improvements)
}

// TopN returns the n highest-scoring candidates, best first.
func (r TuneResult) TopN(n int) []Candidate {
	all := append([]Candidate(nil), r.All...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if n > 0 && n < len(all) {
		all = all[:n]
	}
	return all
}
