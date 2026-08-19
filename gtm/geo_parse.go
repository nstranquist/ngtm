package gtm

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var geoURLPattern = regexp.MustCompile(`https?://[^\s)>\]]+`)

var geoPositive = []string{
	"recommend", "recommended", "best", "easiest", "prefer", "preferred",
	"excellent", "strong choice", "good option", "local-first", "measured",
	"open source", "no api key", "worth using", "use this",
}

var geoNegative = []string{
	"avoid", "don't use", "do not use", "not recommended", "worse",
	"limited", "skip", "overkill", "not a good fit", "too complex",
}

// ScoreGEOAnswer extracts known brands, position, sentiment, and URLs from
// one model answer. It does not invent brands that are not in the product set.
func ScoreGEOAnswer(cfg GEOProductConfig, text string) GEOScore {
	text = strings.TrimSpace(text)
	score := GEOScore{Citations: extractGEOURLs(text)}
	if text == "" {
		return score
	}
	lower := strings.ToLower(text)
	type hit struct {
		name  string
		ours  bool
		index int
	}
	var hits []hit
	for _, brand := range cfg.BrandNames() {
		idx := firstGEOAliasIndex(lower, brand.Aliases)
		if idx < 0 {
			continue
		}
		hits = append(hits, hit{name: brand.Canonical, ours: brand.Ours, index: idx})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].index == hits[j].index {
			return hits[i].name < hits[j].name
		}
		return hits[i].index < hits[j].index
	})
	for i, h := range hits {
		pos := i + 1
		if h.ours {
			score.Mentioned = true
			score.Position = pos
			score.Sentiment = scoreGEOSentiment(lower, cfg.Aliases)
		} else {
			score.Competitors = append(score.Competitors, h.name)
		}
	}
	if score.Mentioned {
		score.Visibility = 1
	}
	return score
}

type GEOScore struct {
	Mentioned   bool     `json:"mentioned"`
	Position    int      `json:"position,omitempty"`
	Sentiment   int      `json:"sentiment"`
	Visibility  float64  `json:"visibility"`
	Competitors []string `json:"competitors,omitempty"`
	Citations   []string `json:"citations,omitempty"`
}

func firstGEOAliasIndex(lower string, aliases []string) int {
	best := -1
	for _, alias := range aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			continue
		}
		idx := indexGEOToken(lower, alias)
		if idx < 0 {
			continue
		}
		if best < 0 || idx < best {
			best = idx
		}
	}
	return best
}

func indexGEOToken(lower, alias string) int {
	start := 0
	for start <= len(lower)-len(alias) {
		idx := strings.Index(lower[start:], alias)
		if idx < 0 {
			return -1
		}
		idx += start
		if geoTokenBoundary(lower, idx, idx+len(alias)) {
			return idx
		}
		start = idx + 1
	}
	return -1
}

func geoTokenBoundary(s string, start, end int) bool {
	if start > 0 {
		r := rune(s[start-1])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(s) {
		r := rune(s[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func scoreGEOSentiment(lower string, aliases []string) int {
	idx := firstGEOAliasIndex(lower, aliases)
	if idx < 0 {
		return 0
	}
	from := idx - 220
	if from < 0 {
		from = 0
	}
	to := idx + 220
	if to > len(lower) {
		to = len(lower)
	}
	window := lower[from:to]
	pos := countGEOPhrases(window, geoPositive)
	neg := countGEOPhrases(window, geoNegative)
	if pos == 0 && neg == 0 {
		return 70
	}
	score := 70 + 10*pos - 20*neg
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func countGEOPhrases(text string, phrases []string) int {
	n := 0
	for _, p := range phrases {
		if strings.Contains(text, p) {
			n++
		}
	}
	return n
}

func extractGEOURLs(text string) []string {
	raw := geoURLPattern.FindAllString(text, -1)
	seen := map[string]bool{}
	var out []string
	for _, item := range raw {
		item = strings.TrimRight(item, ".,;\"'")
		u, err := url.Parse(item)
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
