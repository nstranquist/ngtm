package gtm

import (
	"strings"
	"testing"
)

func TestVerbatimOverlap(t *testing.T) {
	src := "we build the most secure secrets manager for modern engineering teams everywhere on earth"
	// Reproduces an 8-word run verbatim → flagged.
	gen := "Our pitch: we build the most secure secrets manager for modern teams today."
	if run := verbatimOverlap(gen, src, 8); run == "" {
		t.Errorf("expected a verbatim overlap, got none")
	}
	// A genuine paraphrase shares no 8-word run → not flagged.
	para := "Our product keeps your credentials private for engineering organizations of every size."
	if run := verbatimOverlap(para, src, 8); run != "" {
		t.Errorf("paraphrase should not flag, got %q", run)
	}
	// Punctuation/case differences don't defeat detection.
	gen2 := "WE BUILD, the most-secure secrets manager! for modern engineering teams."
	if run := verbatimOverlap(gen2, src, 6); run == "" {
		t.Errorf("expected overlap despite case/punctuation")
	}
}

func TestIPWarningsBrandAdvisory(t *testing.T) {
	r := &Report{Vertical: "brand"}
	w := r.ipWarnings()
	if !containsSubstr(w, "trademark") {
		t.Errorf("brand report should carry a trademark/availability advisory, got %v", w)
	}
	// Non-brand verticals get no brand advisory.
	if containsSubstr((&Report{Vertical: "seo"}).ipWarnings(), "trademark") {
		t.Errorf("non-brand vertical should not carry the brand advisory")
	}
}

func TestIPWarningsVerbatimReproduction(t *testing.T) {
	r := &Report{
		Vertical: "seo",
		Sections: []Section{{
			Title: "Positioning",
			Body:  "Lead with this: we build the most secure secrets manager for modern engineering teams.",
		}},
		Evidence: []Evidence{{
			ID: "landing:page", Metric: "page", URL: "https://competitor.example",
			Snippet: "we build the most secure secrets manager for modern engineering teams everywhere on earth",
		}},
	}
	w := r.ipWarnings()
	if !containsSubstr(w, "near-verbatim") {
		t.Errorf("expected a near-verbatim reproduction warning, got %v", w)
	}
	// A paraphrased section against the same source → no warning.
	r.Sections[0].Body = "We keep your credentials private for engineering organizations of every size."
	if containsSubstr(r.ipWarnings(), "near-verbatim") {
		t.Errorf("paraphrased section should not be flagged")
	}
}

func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
