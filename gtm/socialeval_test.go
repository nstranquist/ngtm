package gtm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultSocialEvalFixturePasses(t *testing.T) {
	report, err := EvaluateSocialFixture(DefaultSocialEvalFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || !report.Stable || report.CaseCount < report.MinimumCases || report.ChannelCount < report.MinimumChannels || report.Average < report.MinimumAverage {
		t.Fatalf("default social eval = %+v", report)
	}
	for _, dimension := range []string{"contract", "grounding", "specificity", "completeness", "cta"} {
		if report.Dimensions[dimension] < 1.5 {
			t.Errorf("dimension %s = %.1f, want >= 1.5", dimension, report.Dimensions[dimension])
		}
	}
}

func TestSocialEvalRejectsGameableThresholdContracts(t *testing.T) {
	var fixture SocialEvalFixture
	if err := json.Unmarshal(DefaultSocialEvalFixture(), &fixture); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SocialEvalFixture)
	}{
		{"negative total", func(f *SocialEvalFixture) { f.Cases[0].MinimumTotal = -1 }},
		{"missing dimension", func(f *SocialEvalFixture) { delete(f.Cases[0].MinimumParts, "cta") }},
		{"unknown dimension", func(f *SocialEvalFixture) { f.Cases[0].MinimumParts["vibes"] = 1 }},
		{"draft channel mismatch", func(f *SocialEvalFixture) { f.Cases[0].Draft.Channel = "reddit" }},
		{"non-canonical id", func(f *SocialEvalFixture) { f.Cases[0].ID = " padded" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cloneRaw, err := json.Marshal(fixture)
			if err != nil {
				t.Fatal(err)
			}
			var clone SocialEvalFixture
			if err := json.Unmarshal(cloneRaw, &clone); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&clone)
			raw, err := json.Marshal(clone)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := EvaluateSocialFixture(raw); err == nil {
				t.Fatal("invalid fixture contract must fail closed")
			}
		})
	}
}

func TestSocialEvalRequiresChannelDiversityForStability(t *testing.T) {
	var fixture SocialEvalFixture
	if err := json.Unmarshal(DefaultSocialEvalFixture(), &fixture); err != nil {
		t.Fatal(err)
	}
	for i := range fixture.Cases {
		fixture.Cases[i].Channel = "show-hn"
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateSocialFixture(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.ChannelCount != 1 || report.Stable || report.Passed {
		t.Fatalf("single-channel fixture reported stable: %+v", report)
	}
}

func TestSocialScoreDoesNotTreatGenericConversationPhraseAsGrounded(t *testing.T) {
	spec, ok := ChannelByKey("reddit")
	if !ok {
		t.Fatal("reddit channel missing")
	}
	score := ScoreSocialDraft(spec, ChannelDraft{
		Channel: "reddit",
		Title:   "Measured local docs retrieval",
		Body:    "A conversation in this space made me ask a better question?",
	}, []Evidence{{Title: "Specific community evidence"}})
	if score.Parts["grounding"] != 1 {
		t.Fatalf("generic phrase grounding = %.1f, want 1 until evidence is actually woven", score.Parts["grounding"])
	}
}

func TestSocialEvalRejectsUnknownSchemaFields(t *testing.T) {
	raw := strings.Replace(string(DefaultSocialEvalFixture()), `"schema_version": 1`, `"schema_version": 1, "surprise": true`, 1)
	if _, err := EvaluateSocialFixture([]byte(raw)); err == nil {
		t.Fatal("unknown fixture fields must fail closed")
	}
}
