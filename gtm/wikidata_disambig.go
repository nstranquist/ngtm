package gtm

import (
	"fmt"
	"strings"
)

// Wikidata entity disambiguation. The wbsearchentities endpoint ranks homonyms
// by generic relevance, so an ambiguous product name ("acp", "keyring") can
// resolve to the wrong real-world entity — an intergovernmental org, a charity,
// a journal — and then every "grounded" brand/seo fact describes that entity
// instead of the product. This module makes entity selection domain-aware
// (prefer software/technology, honor a category hint, penalize off-domain
// homonyms) and surfaces a low-confidence warning when even the best match is
// off-domain. Both halves are pure so they are unit-testable without network.

// wikidataCandidate is one wbsearchentities result used for domain-aware ranking.
type wikidataCandidate struct {
	ID          string
	Label       string
	Description string
}

// Domain keyword sets (substring match against a lowercased description/label).
// Long, unambiguous substrings only — short tokens like "app"/"api" are matched
// as whole words via wholeWordMatch to avoid "apple"/"happen" false positives.
var (
	techDescKeywords = []string{
		"software", "technolog", "application", "platform", "library", "framework",
		"operating system", "open-source", "open source", "website", "web service",
		"protocol", "developer", "programming", "computing", "computer program",
		"database", "video game", "mobile app", "command-line", "package manager",
		"python package", "npm package", "code", "plugin", "browser extension",
		"cryptograph", "encryption", "saas",
	}
	techWordKeywords    = []string{"app", "api", "cli", "sdk", "os", "package"}
	onDomainOrgKeywords = []string{"company", "business", "startup", "enterprise", "corporation", "brand", "product", "vendor"}
	genericOrgKeywords  = []string{"company", "corporation", "business", "startup", "firm", "enterprise", "brand", "product", "vendor"}
	// offDomainKeywords actively penalizes obvious non-product entities during
	// SELECTION. It is NOT the warning trigger (a denylist can never be
	// exhaustive — "acp" matched a gene, a journal, a university). The warning
	// fires on "not on-domain" instead; this list just sharpens entity choice.
	offDomainKeywords = []string{
		"charit", "intergovernmental", "non-profit", "nonprofit", "ngo",
		"journal", "magazine", "newspaper", "university", "college", "school",
		"political party", "human ", "person", "village", "town", "city",
		"municipality", "river", "mountain", "species", "genus", "painter",
		"musician", "singer", "songwriter", "footballer", "athlete", "actor",
		"band", "album", "film", "movie", "church", "diocese", "language",
		"deity", "given name", "surname", "family name", "village in",
		"gene", "protein", "enzyme", "molecule", "chemical compound", "mineral",
		"star", "asteroid", "crater", "disease", "syndrome",
	}
)

func descMatchesAny(d string, keywords []string) bool {
	for _, k := range keywords {
		if strings.Contains(d, k) {
			return true
		}
	}
	return false
}

func wholeWordMatch(d string, words []string) bool {
	toks := strings.FieldsFunc(strings.ToLower(d), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	set := map[string]bool{}
	for _, t := range toks {
		set[t] = true
	}
	for _, w := range words {
		if set[w] {
			return true
		}
	}
	return false
}

// categoryTokens splits a category hint into lowercased tokens of length >= 3.
func categoryTokens(category string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(category), func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '/' || r == ','
	}) {
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// entityDescScore scores how well a Wikidata entity description matches "a
// software/technology product (optionally in <category>)". Higher is better;
// off-domain homonyms score negative.
func entityDescScore(desc, category string) int {
	d := strings.ToLower(strings.TrimSpace(desc))
	score := 0
	for _, tok := range categoryTokens(category) {
		if strings.Contains(d, tok) {
			score += 40
		}
	}
	if descMatchesAny(d, techDescKeywords) || wholeWordMatch(d, techWordKeywords) {
		score += 25
	}
	if descMatchesAny(d, genericOrgKeywords) {
		score += 6
	}
	if descMatchesAny(d, offDomainKeywords) {
		score -= 30
	}
	return score
}

// chooseWikidataEntity picks the best entity id from search results, preferring
// software/technology entities (and any matching the category hint) over
// off-domain homonyms. Search rank is the tiebreaker, so a clean result set
// behaves exactly as the old "first result" logic did.
func chooseWikidataEntity(cands []wikidataCandidate, category string) string {
	if len(cands) == 0 {
		return ""
	}
	best := cands[0].ID
	bestScore := -1 << 30
	for i, c := range cands {
		// Score on the description, falling back to the label when a result has
		// no description. Subtract the rank so earlier (more relevant) results
		// win ties.
		basis := c.Description
		if strings.TrimSpace(basis) == "" {
			basis = c.Label
		}
		s := entityDescScore(basis, category) - i
		if s > bestScore {
			bestScore, best = s, c.ID
		}
	}
	return best
}

// instanceOfDomain classifies an entity's `instance of` (P31) labels as on- or
// off-domain. Bare "organization" is intentionally neither — it's the ambiguous
// token that let charities/intergovernmental bodies pass the old filter.
func instanceOfDomain(labels string) (onDomain, offDomain bool) {
	d := strings.ToLower(labels)
	onDomain = descMatchesAny(d, techDescKeywords) || wholeWordMatch(d, techWordKeywords) || descMatchesAny(d, onDomainOrgKeywords)
	offDomain = descMatchesAny(d, offDomainKeywords)
	return onDomain, offDomain
}

// wikidataDisambiguationWarning inspects gathered evidence for the resolved
// entity's `instance of` (P31) fact and returns a low-confidence warning unless
// that entity is positively on-domain (a software/technology/company/product).
// "Not on-domain" is the trigger — not a denylist match — because the wrong
// homonym can be anything (a gene, a journal, a charity, a person), and for a
// GTM brand/seo analysis any of those means the grounding is about the wrong
// thing. Returns "" when the entity is on-domain or no instance-of was gathered.
func wikidataDisambiguationWarning(ev []Evidence, subject, category string) string {
	for _, e := range ev {
		if e.ID != "wikidata-claims:P31" {
			continue
		}
		labels := e.Value
		if strings.TrimSpace(labels) == "" {
			labels = strings.TrimSpace(strings.TrimPrefix(e.Title, "instance of:"))
		}
		if onDomain, _ := instanceOfDomain(labels); onDomain {
			return "" // clearly a software/company/product entity — trust it
		}
		entity := wikidataQIDFromURL(e.URL)
		if entity == "" {
			entity = "an entity"
		}
		hint := `pass --category to disambiguate (e.g. --category "developer tools")`
		if strings.TrimSpace(category) != "" {
			hint = "the --category hint matched no software entity"
		}
		return fmt.Sprintf(
			"disambiguation: Wikidata resolved %q to %s — instance of %q, which is not a software/technology/company entity. Treat any brand/seo fact grounded on it as LOW-CONFIDENCE; %s, or wire a SERP feed so the entity resolves correctly.",
			subject, entity, strings.TrimSpace(labels), hint)
	}
	return ""
}

// wikidataQIDFromURL extracts a trailing Q-id from a Wikidata entity URL.
func wikidataQIDFromURL(u string) string {
	if i := strings.LastIndex(u, "/wiki/"); i >= 0 {
		return u[i+len("/wiki/"):]
	}
	return ""
}
