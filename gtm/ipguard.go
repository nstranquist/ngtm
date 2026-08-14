package gtm

import (
	"fmt"
	"strings"
	"unicode"
)

// IP guard — copyright / trademark safeguards, distinct from the citation-integrity
// (factual grounding) checks in Validate. Two exposures (see
// docs/active/06-06-2140-gtm-copyright-ip-gaps.md):
//
//   1. brand names + logo direction are emitted with no trademark/domain screening;
//   2. up to 4000 chars of a competitor's homepage (the landing:page Evidence) feed
//      the LLM that writes our marketing copy, with nothing forbidding near-verbatim
//      reproduction.
//
// ipWarnings deterministically surfaces both as Warnings (never a hard failure) so a
// reviewer rephrases / screens before acting on the output. A future keyed feed
// (registrar + USPTO/TESS) can upgrade #1 from an advisory to a real availability check.

// ipWarnings returns copyright/trademark advisories for a finished report.
func (r *Report) ipWarnings() []string {
	var w []string

	// #1 — unscreened brand IP. Surfaced for the brand vertical, which proposes
	// names + a logo mark with no availability/trademark lookup.
	if r.Vertical == "brand" {
		w = append(w, "IP: proposed brand names and the logo direction are NOT screened for "+
			"trademark or domain collision — verify domain availability + USPTO/TESS (and a "+
			"likeness check for the logo) before registering or publishing any of them.")
	}

	// #2 — near-verbatim reproduction of scraped competitor page text. Only the
	// landing:page Evidence (the long, verbatim homepage dump) is a copyright
	// concern; short SERP snippets are provider-licensed and excluded.
	const minRun = 8 // an 8-word run almost never coincides by chance
	for _, s := range r.Sections {
		if s.Body == "" {
			continue
		}
		for _, e := range r.Evidence {
			if e.Metric != "page" || e.Snippet == "" {
				continue
			}
			if run := verbatimOverlap(s.Body, e.Snippet, minRun); run != "" {
				src := e.URL
				if src == "" {
					src = e.ID
				}
				w = append(w, fmt.Sprintf(
					"IP: section %q reproduces competitor text near-verbatim (%q…) from %s — "+
						"rephrase to avoid derivative marketing copy.", s.Title, truncateRun(run, 60), src))
				break // one warning per section is enough
			}
		}
	}
	return w
}

// verbatimOverlap returns the first run of >= minRun consecutive words shared
// between `generated` and `source` (case/punctuation-insensitive), or "" if none.
func verbatimOverlap(generated, source string, minRun int) string {
	g := wordsOf(generated)
	s := wordsOf(source)
	if len(g) < minRun || len(s) < minRun {
		return ""
	}
	seen := make(map[string]bool, len(s))
	for i := 0; i+minRun <= len(s); i++ {
		seen[strings.Join(s[i:i+minRun], " ")] = true
	}
	for i := 0; i+minRun <= len(g); i++ {
		run := strings.Join(g[i:i+minRun], " ")
		if seen[run] {
			return run
		}
	}
	return ""
}

// wordsOf lowercases and splits text into alphanumeric word tokens.
func wordsOf(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func truncateRun(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
