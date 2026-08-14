package gtm

import (
	"strings"
	"testing"
)

// Real wbsearchentities result sets that caused the wrong-entity grounding bug:
// "acp" resolved to an intergovernmental org, "keyring" to a UK charity. The
// descriptions mirror the entities seen in the 2026-06-07 brand packets.
var acpCandidates = []wikidataCandidate{
	{ID: "Q15749243", Label: "Applied Cardiopulmonary Pathophysiology", Description: "academic journal"},
	{ID: "Q15756319", Label: "ACP Journal Club", Description: "American journal (1991-2008)"},
	{ID: "Q2716043", Label: "American University of Paris", Description: "private university college in Paris"},
	{ID: "Q294278", Label: "Organisation of African, Caribbean and Pacific States", Description: "intergovernmental organization of countries in Africa, the Caribbean, and the Pacific"},
	{ID: "Q757509", Label: "Atmospheric Chemistry and Physics", Description: "scientific journal"},
}

var keyringCandidates = []wikidataCandidate{
	{ID: "Q16988970", Label: "KeyRing: Supported Living", Description: "British charitable organization"},
	{ID: "Q107381138", Label: "keyring", Description: "Python library"},
	{ID: "Q6398371", Label: "keyring", Description: "cryptography key store"},
	{ID: "Q51338", Label: "keychain", Description: "connects a small item to a keyring"},
	{ID: "Q2627622", Label: "Alexander Keirincx", Description: "Flemish painter (1600-1652)"},
}

func TestChooseWikidataEntity_KeyringPrefersSoftware(t *testing.T) {
	// The charity (Q16988970) used to win because its description contained
	// "organization". Now a software entity must win.
	got := chooseWikidataEntity(keyringCandidates, "")
	if got == "Q16988970" {
		t.Fatalf("resolver still picked the charity Q16988970 — the bug")
	}
	if got != "Q107381138" && got != "Q6398371" {
		t.Fatalf("expected a software entity (Q107381138/Q6398371), got %q", got)
	}
}

func TestChooseWikidataEntity_ACPAllOffDomain(t *testing.T) {
	// No software entity exists for "acp" in Wikidata; the resolver can only
	// return one of the off-domain candidates — the WARNING (below) is the
	// safety net, not entity selection. It must still return a real candidate.
	got := chooseWikidataEntity(acpCandidates, "")
	found := false
	for _, c := range acpCandidates {
		if c.ID == got {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolver returned an id not in the candidate set: %q", got)
	}
}

func TestChooseWikidataEntity_CategoryHintWins(t *testing.T) {
	// Without a hint, the "software company" (first, generic-tech) wins. With a
	// matching category, the on-category entity must overtake it.
	cands := []wikidataCandidate{
		{ID: "Q1", Label: "x", Description: "a software company"},
		{ID: "Q2", Label: "x", Description: "a password manager app"},
	}
	if got := chooseWikidataEntity(cands, ""); got != "Q1" {
		t.Fatalf("baseline: expected Q1, got %q", got)
	}
	if got := chooseWikidataEntity(cands, "password manager"); got != "Q2" {
		t.Fatalf("category hint should pick Q2 (password manager), got %q", got)
	}
}

func TestInstanceOfDomain(t *testing.T) {
	cases := []struct {
		labels          string
		wantOn, wantOff bool
	}{
		{"intergovernmental organization", false, true},
		{"organization, charitable organization", false, true},
		{"academic journal", false, true},
		{"private university", false, true},
		{"gene", false, true}, // the second wrong "acp" match — must be off-domain
		{"Python library", true, false},
		{"Python package", true, false},
		{"free software, software library", true, false},
		{"business software company", true, false},
		{"web application", true, false},
	}
	for _, c := range cases {
		on, off := instanceOfDomain(c.labels)
		if on != c.wantOn || off != c.wantOff {
			t.Errorf("instanceOfDomain(%q) = on:%v off:%v, want on:%v off:%v", c.labels, on, off, c.wantOn, c.wantOff)
		}
	}
}

func TestWikidataDisambiguationWarning_ACPFiresKeyringClean(t *testing.T) {
	// acp: P31 is off-domain (intergovernmental org) → warning fires, names the entity.
	acpEv := []Evidence{
		{ID: "wikidata-claims:P31", Value: "intergovernmental organization", Title: "instance of: intergovernmental organization", URL: "https://www.wikidata.org/wiki/Q294278"},
	}
	w := wikidataDisambiguationWarning(acpEv, "acp", "")
	if w == "" {
		t.Fatal("expected a disambiguation warning for off-domain acp grounding")
	}
	for _, want := range []string{"disambiguation", "Q294278", "LOW-CONFIDENCE", "--category"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning missing %q: %s", want, w)
		}
	}

	// acp also matched a gene (Q62191808) in a live run — a denylist would miss
	// it; "not on-domain" must still fire the warning.
	geneEv := []Evidence{{ID: "wikidata-claims:P31", Value: "gene", URL: "https://www.wikidata.org/wiki/Q62191808"}}
	if w := wikidataDisambiguationWarning(geneEv, "acp", ""); w == "" {
		t.Error("expected a warning when acp grounds to a gene")
	}

	// keyring resolved to a software entity (Python package/library) → no warning.
	keyringEv := []Evidence{
		{ID: "wikidata-claims:P31", Value: "Python package", URL: "https://www.wikidata.org/wiki/Q107381138"},
	}
	if w := wikidataDisambiguationWarning(keyringEv, "keyring", ""); w != "" {
		t.Errorf("expected no warning for on-domain software grounding, got: %s", w)
	}

	// No instance-of evidence → no warning (can't judge).
	if w := wikidataDisambiguationWarning([]Evidence{{ID: "hackernews:0"}}, "x", ""); w != "" {
		t.Errorf("expected no warning when no P31 evidence present, got: %s", w)
	}
}

func TestWikidataDisambiguationWarning_CategoryHintMentioned(t *testing.T) {
	ev := []Evidence{{ID: "wikidata-claims:P31", Value: "charitable organization", URL: "https://www.wikidata.org/wiki/Q16988970"}}
	w := wikidataDisambiguationWarning(ev, "keyring", "password manager")
	if !strings.Contains(w, "the --category hint matched no software entity") {
		t.Errorf("with a category supplied, the hint text should change; got: %s", w)
	}
}
