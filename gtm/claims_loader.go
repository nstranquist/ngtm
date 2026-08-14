package gtm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ClaimSet is a parsed competitor-claims source: the lookup map (keyed by
// competitor first-token, lowercased) plus the ordered list of competitor
// display names found in the source — so compare can auto-derive its subject
// set when --compare is omitted.
type ClaimSet struct {
	Subjects []string                 // display names, in document/sorted order
	Claims   map[string][]CorpusClaim // keyed by compareKey
}

// rawEntry is one (name, claims) pair before merge/normalization.
type rawEntry struct {
	name   string
	claims []CorpusClaim
}

// LoadClaims parses a competitor-claims source. Supported formats:
//
//   - .yaml/.yml or .json — explicit: {competitor: [{text, kind, needle}]}.
//     Kind/needle are inferred when omitted.
//   - .md / anything else — a markdown corpus parsed heuristically: a
//     "### <Competitor>" heading (then "- claim" bullets) or a
//     "- **<Competitor>** — claim; claim" teardown bullet. Each claim's
//     kind+needle are inferred (H1/headline → serp; $/pricing → pricing;
//     500M/76k → stat; else narrative). For precision, prefer YAML.
func LoadClaims(path string) (*ClaimSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		entries, err := yamlEntries(data)
		if err != nil {
			return nil, err
		}
		return buildClaimSet(entries), nil
	case ".json":
		entries, err := jsonEntries(data)
		if err != nil {
			return nil, err
		}
		return buildClaimSet(entries), nil
	default:
		return buildClaimSet(markdownEntries(string(data))), nil
	}
}

// IsMarkdownClaims reports whether a claims path is a markdown corpus (the only
// source --rewrite can annotate inline).
func IsMarkdownClaims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return false
	default:
		return true
	}
}

func buildClaimSet(entries []rawEntry) *ClaimSet {
	cs := &ClaimSet{Claims: map[string][]CorpusClaim{}}
	seen := map[string]bool{}
	for _, e := range entries {
		key := compareKey(e.name)
		if key == "" {
			continue
		}
		claims := e.claims
		for i := range claims {
			claims[i].Text = strings.TrimSpace(claims[i].Text)
			if claims[i].Kind == "" {
				k, n := inferKind(claims[i].Text)
				claims[i].Kind = k
				if claims[i].Needle == "" {
					claims[i].Needle = n
				}
			}
		}
		cs.Claims[key] = append(cs.Claims[key], claims...)
		if !seen[key] {
			seen[key] = true
			cs.Subjects = append(cs.Subjects, e.name)
		}
	}
	return cs
}

func yamlEntries(data []byte) ([]rawEntry, error) {
	var raw map[string][]CorpusClaim
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse claims yaml: %w", err)
	}
	return sortedEntries(raw), nil
}

func jsonEntries(data []byte) ([]rawEntry, error) {
	var raw map[string][]CorpusClaim
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse claims json: %w", err)
	}
	return sortedEntries(raw), nil
}

// sortedEntries gives map-based sources a deterministic subject order.
func sortedEntries(raw map[string][]CorpusClaim) []rawEntry {
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]rawEntry, 0, len(names))
	for _, n := range names {
		out = append(out, rawEntry{name: n, claims: raw[n]})
	}
	return out
}

var (
	mdHeadingRe   = regexp.MustCompile(`^#{2,6}\s+(.+?)\s*$`)
	mdBoldBullet  = regexp.MustCompile(`^\s*[-*]\s+\*\*(.+?)\*\*\s*[—–:-]\s*(.+)$`)
	mdPlainBullet = regexp.MustCompile(`^\s*[-*]\s+(.+)$`)
)

// markdownEntries supports two conventions. When the doc has "- **Name** — …"
// teardown bullets, ONLY those are competitors (so product-section headings like
// "## nvault" aren't mistaken for competitors). Otherwise it falls back to the
// "### Competitor" heading + bullet convention used by hand-written claim docs.
func markdownEntries(md string) []rawEntry {
	var bold, heading []rawEntry
	add := func(dst *[]rawEntry, name, text string) {
		text = cleanInline(text)
		if len(text) < 6 {
			return
		}
		*dst = append(*dst, rawEntry{name: name, claims: []CorpusClaim{{Text: text}}})
	}
	current := ""
	for _, line := range strings.Split(md, "\n") {
		if m := mdBoldBullet.FindStringSubmatch(line); m != nil {
			name := cleanInline(m[1])
			for _, frag := range splitClauses(m[2]) {
				add(&bold, name, frag)
			}
			continue
		}
		if m := mdHeadingRe.FindStringSubmatch(line); m != nil {
			current = cleanInline(m[1])
			continue
		}
		if current != "" {
			if m := mdPlainBullet.FindStringSubmatch(line); m != nil {
				add(&heading, current, cleanInline(m[1]))
			}
		}
	}
	if len(bold) > 0 {
		return bold
	}
	return heading
}

// parseClaimsMarkdown is a convenience wrapper used by tests.
func parseClaimsMarkdown(md string) *ClaimSet { return buildClaimSet(markdownEntries(md)) }

func splitClauses(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		if p := cleanInline(part); len(p) >= 6 {
			out = append(out, p)
		}
	}
	return out
}

func cleanInline(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var (
	quoteRe      = regexp.MustCompile(`["“]([^"”]{2,})["”]`)
	statTokenRe  = regexp.MustCompile(`\d+(?:\.\d+)?\s*[KkMmBb]\+?`)
	priceTokenRe = regexp.MustCompile(`\$\s?\d[\d,]*(?:\.\d+)?`)
	digitsRe     = regexp.MustCompile(`\d[\d,]*`)
)

// inferKind classifies a free-text claim and extracts the substring to look for.
func inferKind(text string) (kind, needle string) {
	lower := strings.ToLower(text)
	quoted := firstQuoted(text)
	switch {
	case strings.Contains(lower, "h1") || strings.Contains(lower, "headline") || strings.Contains(lower, "title tag") || strings.Contains(lower, "h2"):
		if quoted != "" {
			return "serp", quoted
		}
		return "serp", stripLabel(text)
	case strings.Contains(text, "$") || strings.Contains(lower, "pricing") || strings.Contains(lower, "/user") || strings.Contains(lower, "/mo") || strings.Contains(lower, "/month") || strings.Contains(lower, "per user") || strings.Contains(lower, "per seat"):
		if m := priceTokenRe.FindString(text); m != "" {
			return "pricing", strings.TrimSpace(digitsRe.FindString(m))
		}
		return "pricing", ""
	case statTokenRe.MatchString(text):
		return "stat", strings.TrimSpace(statTokenRe.FindString(text))
	case quoted != "" && len(quoted) >= len(strings.TrimSpace(text))/2:
		return "serp", quoted
	default:
		return "narrative", ""
	}
}

func firstQuoted(s string) string {
	if m := quoteRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func stripLabel(s string) string {
	s = cleanInline(s)
	for _, lbl := range []string{"H1 ", "h1 ", "Headline ", "headline ", "H2 "} {
		s = strings.TrimPrefix(s, lbl)
	}
	return strings.TrimSpace(s)
}
