package gtm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

//go:embed evaldata/social-quality-v1.json
var defaultSocialEvalFixture []byte

// SocialEvalFixture is the versioned, human-reviewed quality contract for the
// deterministic social scorer. Cases are complete channel-native drafts with
// explicit evidence; thresholds make regressions actionable by dimension.
type SocialEvalFixture struct {
	SchemaVersion   int              `json:"schema_version"`
	Name            string           `json:"name"`
	MinimumCases    int              `json:"minimum_cases"`
	MinimumChannels int              `json:"minimum_channels"`
	MinimumAverage  float64          `json:"minimum_average"`
	Cases           []SocialEvalCase `json:"cases"`
}

type SocialEvalCase struct {
	ID           string             `json:"id"`
	Channel      string             `json:"channel"`
	Draft        ChannelDraft       `json:"draft"`
	Mentions     []Evidence         `json:"mentions"`
	MinimumTotal float64            `json:"minimum_total"`
	MinimumParts map[string]float64 `json:"minimum_parts"`
}

type SocialEvalCaseResult struct {
	ID       string     `json:"id"`
	Channel  string     `json:"channel"`
	Score    DraftScore `json:"score"`
	Passed   bool       `json:"passed"`
	Failures []string   `json:"failures,omitempty"`
}

type SocialEvalReport struct {
	SchemaVersion   int                    `json:"schema_version"`
	Name            string                 `json:"name"`
	Cases           []SocialEvalCaseResult `json:"cases"`
	CaseCount       int                    `json:"case_count"`
	MinimumCases    int                    `json:"minimum_cases"`
	ChannelCount    int                    `json:"channel_count"`
	MinimumChannels int                    `json:"minimum_channels"`
	Average         float64                `json:"average"`
	MinimumAverage  float64                `json:"minimum_average"`
	Dimensions      map[string]float64     `json:"dimensions"`
	Stable          bool                   `json:"stable"`
	Passed          bool                   `json:"passed"`
}

var socialEvalDimensions = []string{"contract", "grounding", "specificity", "completeness", "cta"}

// DefaultSocialEvalFixture returns a copy of the embedded v1 fixture.
func DefaultSocialEvalFixture() []byte {
	return append([]byte(nil), defaultSocialEvalFixture...)
}

func EvaluateSocialFixture(raw []byte) (SocialEvalReport, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fixture SocialEvalFixture
	if err := dec.Decode(&fixture); err != nil {
		return SocialEvalReport{}, fmt.Errorf("decode social eval fixture: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return SocialEvalReport{}, err
	}
	if fixture.SchemaVersion != 1 {
		return SocialEvalReport{}, fmt.Errorf("unsupported social eval schema_version %d (want 1)", fixture.SchemaVersion)
	}
	if strings.TrimSpace(fixture.Name) == "" || fixture.MinimumCases < 1 || fixture.MinimumChannels < 1 || fixture.MinimumChannels > len(Channels) || fixture.MinimumAverage <= 0 || fixture.MinimumAverage > 10 {
		return SocialEvalReport{}, fmt.Errorf("social eval fixture needs name, minimum_cases >= 1, minimum_channels in 1..%d, and minimum_average in (0,10]", len(Channels))
	}

	report := SocialEvalReport{
		SchemaVersion: fixture.SchemaVersion, Name: fixture.Name,
		CaseCount: len(fixture.Cases), MinimumCases: fixture.MinimumCases,
		MinimumChannels: fixture.MinimumChannels, MinimumAverage: fixture.MinimumAverage,
		Dimensions: map[string]float64{},
	}
	ids := map[string]bool{}
	channels := map[string]bool{}
	for _, c := range fixture.Cases {
		if strings.TrimSpace(c.ID) == "" || c.ID != strings.TrimSpace(c.ID) || strings.ContainsAny(c.ID, "\r\n|") || ids[c.ID] {
			return SocialEvalReport{}, fmt.Errorf("social eval case id %q is empty or duplicated", c.ID)
		}
		ids[c.ID] = true
		spec, ok := ChannelByKey(c.Channel)
		if !ok {
			return SocialEvalReport{}, fmt.Errorf("social eval case %s has unknown channel %q", c.ID, c.Channel)
		}
		channels[c.Channel] = true
		if c.Draft.Channel != "" && c.Draft.Channel != c.Channel {
			return SocialEvalReport{}, fmt.Errorf("social eval case %s draft channel %q does not match case channel %q", c.ID, c.Draft.Channel, c.Channel)
		}
		if c.MinimumTotal <= 0 || c.MinimumTotal > 10 {
			return SocialEvalReport{}, fmt.Errorf("social eval case %s minimum_total %.1f is outside (0,10]", c.ID, c.MinimumTotal)
		}
		if len(c.MinimumParts) != len(socialEvalDimensions) {
			return SocialEvalReport{}, fmt.Errorf("social eval case %s minimum_parts must define exactly %s", c.ID, strings.Join(socialEvalDimensions, ", "))
		}
		for dimension, minimum := range c.MinimumParts {
			if !containsSocialEvalDimension(dimension) {
				return SocialEvalReport{}, fmt.Errorf("social eval case %s has unknown minimum_parts dimension %q", c.ID, dimension)
			}
			if minimum < 0 || minimum > 2 {
				return SocialEvalReport{}, fmt.Errorf("social eval case %s dimension %s minimum %.1f is outside [0,2]", c.ID, dimension, minimum)
			}
		}
		c.Draft.Channel = c.Channel
		score := ScoreSocialDraft(spec, c.Draft, c.Mentions)
		result := SocialEvalCaseResult{ID: c.ID, Channel: c.Channel, Score: score, Passed: true}
		if score.Total < c.MinimumTotal {
			result.Passed = false
			result.Failures = append(result.Failures, fmt.Sprintf("total %.1f < %.1f", score.Total, c.MinimumTotal))
		}
		for dimension, minimum := range c.MinimumParts {
			if value := score.Parts[dimension]; value < minimum {
				result.Passed = false
				result.Failures = append(result.Failures, fmt.Sprintf("%s %.1f < %.1f", dimension, value, minimum))
			}
		}
		sort.Strings(result.Failures)
		report.Cases = append(report.Cases, result)
		report.Average += score.Total
		for dimension, value := range score.Parts {
			report.Dimensions[dimension] += value
		}
	}
	report.ChannelCount = len(channels)
	if report.CaseCount > 0 {
		report.Average /= float64(report.CaseCount)
		for dimension, sum := range report.Dimensions {
			report.Dimensions[dimension] = sum / float64(report.CaseCount)
		}
	}
	report.Stable = report.CaseCount >= report.MinimumCases && report.ChannelCount >= report.MinimumChannels
	report.Passed = report.Stable && report.Average >= report.MinimumAverage
	for _, result := range report.Cases {
		if !result.Passed {
			report.Passed = false
		}
	}
	return report, nil
}

func containsSocialEvalDimension(candidate string) bool {
	for _, dimension := range socialEvalDimensions {
		if candidate == dimension {
			return true
		}
	}
	return false
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("social eval fixture contains more than one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode social eval fixture tail: %w", err)
	}
	return nil
}

func (r SocialEvalReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Social quality eval — %s\n\n", r.Name)
	fmt.Fprintf(&b, "cases %d/%d · channels %d/%d · average %.1f/10 (minimum %.1f) · stable %t · passed %t\n\n", r.CaseCount, r.MinimumCases, r.ChannelCount, r.MinimumChannels, r.Average, r.MinimumAverage, r.Stable, r.Passed)
	b.WriteString("| dimension | average /2 |\n|---|---:|\n")
	for _, dimension := range socialEvalDimensions {
		fmt.Fprintf(&b, "| %s | %.1f |\n", dimension, r.Dimensions[dimension])
	}
	b.WriteString("\n| case | channel | score | result |\n|---|---|---:|---|\n")
	for _, c := range r.Cases {
		result := "PASS"
		if !c.Passed {
			result = "FAIL: " + strings.Join(c.Failures, "; ")
		}
		fmt.Fprintf(&b, "| %s | %s | %.1f | %s |\n", c.ID, c.Channel, c.Score.Total, result)
	}
	return b.String()
}
