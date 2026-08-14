package gtm

import (
	"fmt"
	"strings"
)

// AnnotateCorpus returns a copy of the markdown corpus with each verified claim
// tagged inline by verdict (✅ confirmed / ❌ contradicted / 🔴 unverified) and,
// where available, the cited source URL — so a stale GTM corpus fact-checks
// itself in one pass. Verdicts come from a CompareReport produced over the same
// corpus's claims. Each competitor's verdicts are inserted right beneath the
// line that introduces it (its teardown bullet or heading), once.
func AnnotateCorpus(corpusMarkdown string, rep *CompareReport) string {
	checks := map[string][]ClaimCheck{}
	urls := map[string]map[string]string{} // key → (evidenceID → URL)
	for _, row := range rep.Rows {
		key := compareKey(row.Subject)
		checks[key] = row.ClaimChecks
		m := map[string]string{}
		for _, e := range row.Evidence {
			if e.URL != "" {
				m[e.ID] = e.URL
			}
		}
		urls[key] = m
	}

	var out strings.Builder
	fmt.Fprintf(&out, "<!-- fact-checked by ngtm on %s — ✅ confirmed · ❌ contradicted · 🔴 unverified -->\n", rep.Generated)
	for _, line := range strings.Split(corpusMarkdown, "\n") {
		out.WriteString(line)
		out.WriteByte('\n')
		name := competitorFromLine(line)
		if name == "" {
			continue
		}
		key := compareKey(name)
		cs, ok := checks[key]
		if !ok || len(cs) == 0 {
			continue
		}
		indent := leadingSpaces(line) + "  "
		for _, c := range cs {
			src := ""
			for _, cite := range c.Citations {
				if u := urls[key][cite]; u != "" {
					src = " — source: <" + u + ">"
					break
				}
			}
			fmt.Fprintf(&out, "%s- %s **%s** — %s%s\n", indent, claimIcon(c.Status), c.Status, claimSnippet(c.Text), src)
		}
		delete(checks, key) // annotate each competitor only once
	}
	return out.String()
}

func competitorFromLine(line string) string {
	if m := mdBoldBullet.FindStringSubmatch(line); m != nil {
		return cleanInline(m[1])
	}
	if m := mdHeadingRe.FindStringSubmatch(line); m != nil {
		return cleanInline(m[1])
	}
	return ""
}

func leadingSpaces(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

func claimSnippet(text string) string {
	t := wsRe.ReplaceAllString(text, " ")
	t = strings.TrimSpace(t)
	if len(t) > 70 {
		t = t[:69] + "…"
	}
	return t
}
