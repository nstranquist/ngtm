package gtm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const geoProbeTimeout = 45 * time.Second

var geoHTTPClient = &http.Client{Timeout: geoProbeTimeout}

// GEOEngineID is an honest label for the API we called. Never write "chatgpt"
// unless the ChatGPT product was the engine.
type GEOEngineID string

const (
	GEOEngineOpenAIChat   GEOEngineID = "openai-chat"
	GEOEngineOpenAISearch GEOEngineID = "openai-search"
	GEOEngineGemini       GEOEngineID = "gemini"
	GEOEngineGrok         GEOEngineID = "grok"
	GEOEngineFixture      GEOEngineID = "fixture"
)

type GEOEngineSpec struct {
	ID     GEOEngineID
	Kind   string
	EnvKey string
	Model  string
}

var geoEngineRegistry = map[GEOEngineID]GEOEngineSpec{
	GEOEngineOpenAIChat:   {ID: GEOEngineOpenAIChat, Kind: "api-completion", EnvKey: "OPENAI_API_KEY", Model: "gpt-4o-mini"},
	GEOEngineOpenAISearch: {ID: GEOEngineOpenAISearch, Kind: "api-search", EnvKey: "OPENAI_API_KEY", Model: "gpt-4o-search-preview"},
	GEOEngineGemini:       {ID: GEOEngineGemini, Kind: "api-completion", EnvKey: "GEMINI_API_KEY", Model: "gemini-2.5-flash"},
	GEOEngineGrok:         {ID: GEOEngineGrok, Kind: "api-completion", EnvKey: "XAI_API_KEY", Model: "grok-4-fast-non-reasoning"},
	GEOEngineFixture:      {ID: GEOEngineFixture, Kind: "fixture"},
}

type GEORawAnswer struct {
	PromptID  string      `json:"prompt_id"`
	Engine    GEOEngineID `json:"engine"`
	Model     string      `json:"model"`
	Text      string      `json:"text"`
	Citations []string    `json:"citations,omitempty"`
}

type GEOProbeRow struct {
	PromptID    string      `json:"prompt_id"`
	Prompt      string      `json:"prompt"`
	Topic       string      `json:"topic,omitempty"`
	Kind        string      `json:"kind,omitempty"`
	Engine      GEOEngineID `json:"engine"`
	EngineKind  string      `json:"engine_kind"`
	Model       string      `json:"model"`
	Mentioned   bool        `json:"mentioned"`
	Position    int         `json:"position,omitempty"`
	Sentiment   int         `json:"sentiment"`
	Visibility  float64     `json:"visibility"`
	Competitors []string    `json:"competitors,omitempty"`
	Citations   []string    `json:"citations,omitempty"`
	Excerpt     string      `json:"excerpt,omitempty"`
	AnswerSHA   string      `json:"answer_sha256"`
	Error       string      `json:"error,omitempty"`
	ProbedAt    string      `json:"probed_at"`
}

type GEOProbeReport struct {
	SchemaVersion int           `json:"schema_version"`
	Project       string        `json:"project"`
	Product       string        `json:"product"`
	Brand         string        `json:"brand"`
	Engines       []GEOEngineID `json:"engines"`
	Generated     string        `json:"generated"`
	Rows          []GEOProbeRow `json:"rows"`
	Findings      []GEOFinding  `json:"findings,omitempty"`
	Passed        bool          `json:"passed"`
}

type GEOProbeOptions struct {
	Config      GEOProductConfig
	Engines     []GEOEngineID
	FixturePath string
	Limit       int
	Model       string
	Offline     bool
	Now         func() time.Time
	Ask         func(ctx context.Context, spec GEOEngineSpec, prompt string) (GEORawAnswer, error)
}

type geoFixtureFile struct {
	SchemaVersion int            `json:"schema_version"`
	Answers       []GEORawAnswer `json:"answers"`
}

func ParseGEOEngines(csv string) ([]GEOEngineID, error) {
	var out []GEOEngineID
	seen := map[GEOEngineID]bool{}
	for _, part := range splitCSV(csv) {
		id := GEOEngineID(strings.ToLower(strings.TrimSpace(part)))
		if _, ok := geoEngineRegistry[id]; !ok {
			return nil, fmt.Errorf("unknown GEO engine %q (openai-chat, openai-search, gemini, grok, fixture)", part)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func DefaultLiveGEOEngines() []GEOEngineID {
	var out []GEOEngineID
	for _, id := range []GEOEngineID{GEOEngineOpenAIChat, GEOEngineGemini, GEOEngineGrok} {
		spec := geoEngineRegistry[id]
		if strings.TrimSpace(os.Getenv(spec.EnvKey)) != "" {
			out = append(out, id)
		}
	}
	return out
}

func RunGEOProbe(ctx context.Context, opts GEOProbeOptions) (*GEOProbeReport, error) {
	cfg := opts.Config
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	generated := now().UTC().Format(time.RFC3339)
	report := &GEOProbeReport{
		SchemaVersion: GEOSchemaVersion,
		Project:       cfg.Project,
		Product:       cfg.Product,
		Brand:         cfg.Brand,
		Generated:     generated,
		Passed:        true,
	}
	engines := opts.Engines
	if opts.Offline || opts.FixturePath != "" {
		if len(engines) == 0 {
			engines = []GEOEngineID{GEOEngineFixture}
		}
	}
	if len(engines) == 0 {
		engines = DefaultLiveGEOEngines()
	}
	if len(engines) == 0 {
		return nil, errors.New("no GEO engine is configured; set OPENAI_API_KEY, GEMINI_API_KEY, or XAI_API_KEY, or pass --fixture")
	}
	if strings.TrimSpace(opts.Model) != "" && len(engines) != 1 {
		return nil, errors.New("--model requires exactly one --engines value")
	}
	if opts.FixturePath == "" {
		for _, engine := range engines {
			spec := geoEngineRegistry[engine]
			if spec.EnvKey != "" && strings.TrimSpace(os.Getenv(spec.EnvKey)) == "" {
				return nil, fmt.Errorf("%s is not set for engine %s", spec.EnvKey, engine)
			}
		}
	}
	report.Engines = engines

	answers := map[string]GEORawAnswer{}
	if opts.FixturePath != "" {
		loaded, err := loadGEOFixture(opts.FixturePath)
		if err != nil {
			return nil, err
		}
		for _, ans := range loaded {
			if ans.Engine == "" {
				ans.Engine = GEOEngineFixture
			}
			answers[ans.PromptID+"\x00"+string(ans.Engine)] = ans
		}
	} else if opts.Offline {
		return nil, errors.New("GEO probe --offline requires --fixture")
	}

	ask := opts.Ask
	if ask == nil {
		ask = askGEOEngine
	}

	prompts := cfg.Prompts
	if opts.Limit > 0 && opts.Limit < len(prompts) {
		prompts = prompts[:opts.Limit]
	}

	for _, prompt := range prompts {
		for _, engine := range engines {
			spec := geoEngineRegistry[engine]
			if strings.TrimSpace(opts.Model) != "" {
				spec.Model = strings.TrimSpace(opts.Model)
			}
			row := GEOProbeRow{
				PromptID:   prompt.ID,
				Prompt:     prompt.Text,
				Topic:      prompt.Topic,
				Kind:       prompt.Kind,
				Engine:     engine,
				EngineKind: spec.Kind,
				ProbedAt:   generated,
			}
			var raw GEORawAnswer
			var err error
			if opts.FixturePath != "" {
				var ok bool
				raw, ok = answers[prompt.ID+"\x00"+string(engine)]
				if !ok {
					err = fmt.Errorf("fixture missing prompt %s engine %s", prompt.ID, engine)
				} else {
					row.Engine = raw.Engine
					if raw.Engine != "" {
						if fixtureSpec, known := geoEngineRegistry[raw.Engine]; known {
							row.EngineKind = fixtureSpec.Kind
						} else {
							row.EngineKind = "fixture"
						}
					}
				}
			} else {
				raw, err = ask(ctx, spec, prompt.Text)
			}
			if err != nil {
				msg := redactGEOSecret(err.Error(), os.Getenv(spec.EnvKey))
				row.Error = msg
				report.Passed = false
				report.Findings = append(report.Findings, GEOFinding{
					Code: "PROBE_FAILED", Severity: "blocker",
					Message: fmt.Sprintf("%s/%s: %s", prompt.ID, engine, msg),
				})
				report.Rows = append(report.Rows, row)
				continue
			}
			if raw.Model != "" {
				row.Model = raw.Model
			} else {
				row.Model = spec.Model
			}
			score := ScoreGEOAnswer(cfg, raw.Text)
			if len(raw.Citations) > 0 {
				score.Citations = uniqueStrings(append(score.Citations, raw.Citations...))
			}
			row.Mentioned = score.Mentioned
			row.Position = score.Position
			row.Sentiment = score.Sentiment
			row.Visibility = score.Visibility
			row.Competitors = score.Competitors
			row.Citations = score.Citations
			row.Excerpt = clipGEOExcerpt(raw.Text, 280)
			sum := sha256.Sum256([]byte(raw.Text))
			row.AnswerSHA = "sha256:" + hex.EncodeToString(sum[:])
			report.Rows = append(report.Rows, row)
		}
	}
	return report, nil
}

func loadGEOFixture(path string) ([]GEORawAnswer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file geoFixtureFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parse GEO fixture: %w", err)
	}
	if file.SchemaVersion != 0 && file.SchemaVersion != GEOSchemaVersion {
		return nil, fmt.Errorf("GEO fixture schema_version=%d, want %d", file.SchemaVersion, GEOSchemaVersion)
	}
	if len(file.Answers) == 0 {
		return nil, errors.New("GEO fixture has no answers")
	}
	return file.Answers, nil
}

func askGEOEngine(ctx context.Context, spec GEOEngineSpec, prompt string) (GEORawAnswer, error) {
	switch spec.ID {
	case GEOEngineOpenAIChat, GEOEngineOpenAISearch, GEOEngineGrok:
		return askOpenAICompatible(ctx, spec, prompt)
	case GEOEngineGemini:
		return askGemini(ctx, spec, prompt)
	default:
		return GEORawAnswer{}, fmt.Errorf("engine %s cannot be live-probed", spec.ID)
	}
}

func geoSystemPrompt() string {
	return "You recommend real software products for a working developer. Name specific products. Be concise. Do not invent products."
}

func askOpenAICompatible(ctx context.Context, spec GEOEngineSpec, prompt string) (GEORawAnswer, error) {
	key := strings.TrimSpace(os.Getenv(spec.EnvKey))
	if key == "" {
		return GEORawAnswer{}, fmt.Errorf("%s is not set", spec.EnvKey)
	}
	base := "https://api.openai.com/v1/chat/completions"
	if spec.ID == GEOEngineGrok {
		base = "https://api.x.ai/v1/chat/completions"
	}
	reqBody := map[string]any{
		"model": spec.Model,
		"messages": []map[string]string{
			{"role": "system", "content": geoSystemPrompt()},
			{"role": "user", "content": prompt},
		},
	}
	if spec.ID != GEOEngineOpenAISearch {
		reqBody["temperature"] = 0.2
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return GEORawAnswer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return GEORawAnswer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return GEORawAnswer{}, fmt.Errorf("%s: %s", spec.ID, redactGEOSecret(err.Error(), key))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GEORawAnswer{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GEORawAnswer{}, fmt.Errorf("%s HTTP %d: %s", spec.ID, resp.StatusCode, clipGEOExcerpt(redactGEOSecret(string(raw), key), 240))
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return GEORawAnswer{}, fmt.Errorf("decode %s: %w", spec.ID, err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return GEORawAnswer{}, fmt.Errorf("%s returned an empty answer", spec.ID)
	}
	return GEORawAnswer{
		Engine: spec.ID,
		Model:  firstNonEmpty(parsed.Model, spec.Model),
		Text:   parsed.Choices[0].Message.Content,
	}, nil
}

func askGemini(ctx context.Context, spec GEOEngineSpec, prompt string) (GEORawAnswer, error) {
	key := strings.TrimSpace(os.Getenv(spec.EnvKey))
	if key == "" {
		return GEORawAnswer{}, fmt.Errorf("%s is not set", spec.EnvKey)
	}
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", spec.Model)
	body, err := json.Marshal(map[string]any{
		"system_instruction": map[string]any{
			"parts": []map[string]string{{"text": geoSystemPrompt()}},
		},
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{"temperature": 0.2},
	})
	if err != nil {
		return GEORawAnswer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GEORawAnswer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key)
	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return GEORawAnswer{}, fmt.Errorf("gemini: %s", redactGEOSecret(err.Error(), key))
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GEORawAnswer{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GEORawAnswer{}, fmt.Errorf("gemini HTTP %d: %s", resp.StatusCode, clipGEOExcerpt(redactGEOSecret(string(payload), key), 240))
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return GEORawAnswer{}, fmt.Errorf("decode gemini: %w", err)
	}
	var text strings.Builder
	if len(parsed.Candidates) > 0 {
		for _, part := range parsed.Candidates[0].Content.Parts {
			text.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return GEORawAnswer{}, errors.New("gemini returned an empty answer")
	}
	return GEORawAnswer{Engine: spec.ID, Model: spec.Model, Text: text.String()}, nil
}

func redactGEOSecret(s, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" || !strings.Contains(s, secret) {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

func clipGEOExcerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
