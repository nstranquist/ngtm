package design

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultVisionModel is the economical model used for the perceptual scoring
// channel. Callers must explicitly opt into a more expensive vision model.
const DefaultVisionModel = "claude-haiku-4-5"

const anthropicVersion = "2023-06-01"

// VisionScore is the perceptual (LLM) judgment of a rendered design.
type VisionScore struct {
	Score     float64  `json:"score"`     // overall 0..10
	Hierarchy float64  `json:"hierarchy"` // visual hierarchy 0..10
	Polish    float64  `json:"polish"`    // craft/polish 0..10
	Harmony   float64  `json:"harmony"`   // color harmony 0..10
	Reasons   []string `json:"reasons"`   // concrete observations
	Model     string   `json:"model"`
}

// VisionOptions configures the vision call.
type VisionOptions struct {
	Model  string // default DefaultVisionModel
	Rubric string // extra rubric text appended to the system instruction
}

func anthropicKey() string {
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
}

func anthropicBaseURL() string {
	base := strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/")
	if base == "" {
		base = "https://api.anthropic.com/v1"
	}
	return base
}

// visionAvailable reports whether the perceptual channel can run (key present).
func visionAvailable() bool {
	return anthropicKey() != ""
}

// ScorePNG sends a PNG to a vision-capable Claude model and returns a
// structured /10 judgment. Key and base URL come from ANTHROPIC_API_KEY and
// ANTHROPIC_BASE_URL. Output is forced to JSON via output_config.format.
func ScorePNG(ctx context.Context, png []byte, opts VisionOptions) (VisionScore, error) {
	key := anthropicKey()
	if key == "" {
		return VisionScore{}, fmt.Errorf("ANTHROPIC_API_KEY not set (perceptual channel is opt-in)")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = DefaultVisionModel
	}
	base := anthropicBaseURL()

	system := "You are a senior product designer reviewing a screenshot of a generated UI design system " +
		"(color tokens, type ramp, buttons, cards, states). Judge ONLY what the image shows. " +
		"Score each dimension 0-10 (10 = shippable, world-class): overall, visual hierarchy, craft/polish, color harmony. " +
		"Be a tough but fair critic; reserve 9-10 for genuinely excellent work. List concrete, specific observations."
	if r := strings.TrimSpace(opts.Rubric); r != "" {
		system += "\n\nAdditional rubric:\n" + r
	}

	reqBody := map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"system":     system,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       base64.StdEncoding.EncodeToString(png),
						},
					},
					map[string]any{"type": "text", "text": "Score this design system. Return JSON only."},
				},
			},
		},
		"output_config": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"schema": visionSchema(),
			},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return VisionScore{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/messages", bytes.NewReader(body))
	if err != nil {
		return VisionScore{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", key)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return VisionScore{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VisionScore{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	if resp.StatusCode/100 != 2 || parsed.Error != nil {
		msg := strings.TrimSpace(string(raw))
		if parsed.Error != nil {
			msg = parsed.Error.Message
		}
		return VisionScore{}, fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(parsed.Content) == 0 {
		return VisionScore{}, fmt.Errorf("empty response content")
	}

	var vs VisionScore
	if err := json.Unmarshal([]byte(parsed.Content[0].Text), &vs); err != nil {
		return VisionScore{}, fmt.Errorf("parse vision verdict %q: %w", parsed.Content[0].Text, err)
	}
	vs.Score = clampScore(vs.Score)
	vs.Hierarchy = clampScore(vs.Hierarchy)
	vs.Polish = clampScore(vs.Polish)
	vs.Harmony = clampScore(vs.Harmony)
	vs.Model = model
	return vs, nil
}

// BlendWeightDeterministic is the deterministic channel's weight in the blended
// score; the perceptual (vision) channel gets the remainder. The computable
// rubric dominates because it's reproducible and accessibility-grounded; vision
// adds the subjective "does it actually look good" signal a rubric can't capture.
const BlendWeightDeterministic = 0.7

// VisionAvailable reports whether the perceptual channel can run on this machine.
func VisionAvailable() bool { return visionAvailable() }

// EvaluateVision screenshots the theme's preview and scores it with the vision
// model. Requires Chrome (for the screenshot) and ANTHROPIC_API_KEY.
func EvaluateVision(ctx context.Context, t Theme, mode Mode, opts VisionOptions) (VisionScore, error) {
	if !visionAvailable() {
		return VisionScore{}, fmt.Errorf("ANTHROPIC_API_KEY not set (perceptual channel is opt-in)")
	}
	png, err := ScreenshotThemeBytes(ctx, t, mode)
	if err != nil {
		return VisionScore{}, fmt.Errorf("screenshot: %w", err)
	}
	return ScorePNG(ctx, png, opts)
}

// Blend combines the deterministic /10 with the perceptual /10.
func Blend(deterministic, vision float64) float64 {
	return round2(BlendWeightDeterministic*deterministic + (1-BlendWeightDeterministic)*vision)
}

func visionSchema() map[string]any {
	num := map[string]any{"type": "number"}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"score", "hierarchy", "polish", "harmony", "reasons"},
		"properties": map[string]any{
			"score":     num,
			"hierarchy": num,
			"polish":    num,
			"harmony":   num,
			"reasons":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}
