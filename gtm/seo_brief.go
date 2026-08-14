package gtm

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type SEOBriefRequest struct {
	Keyword     string `json:"keyword"`
	UniqueValue string `json:"unique_value"`
	Audience    string `json:"audience,omitempty"`
	Format      string `json:"format,omitempty"`
}

type SEOBrief struct {
	SchemaVersion    int              `json:"schema_version"`
	ID               string           `json:"id"`
	Generated        string           `json:"generated"`
	Project          string           `json:"project"`
	Keyword          string           `json:"keyword"`
	Intent           string           `json:"intent,omitempty"`
	Audience         string           `json:"audience,omitempty"`
	Format           string           `json:"format,omitempty"`
	UniqueValue      string           `json:"unique_value"`
	SuggestedTitle   string           `json:"suggested_title"`
	SuggestedOutline []string         `json:"suggested_outline"`
	CompetitorPages  []SEORanking     `json:"competitor_pages,omitempty"`
	ExistingPages    []SEOContentPage `json:"existing_pages,omitempty"`
	InternalLinks    []string         `json:"internal_links,omitempty"`
	EvidenceIDs      []string         `json:"evidence_ids"`
	ResearchID       string           `json:"research_id"`
	Findings         []SEOFinding     `json:"findings"`
	Passed           bool             `json:"passed"`
	Artifact         *SEOArtifactRef  `json:"artifact,omitempty"`
}

func BuildSEOBrief(cfg SEOProjectConfig, research SEOResearchReport, req SEOBriefRequest, now func() string) (*SEOBrief, error) {
	keyword := strings.ToLower(strings.Join(strings.Fields(req.Keyword), " "))
	if keyword == "" {
		return nil, errors.New("brief keyword is required")
	}
	var target *SEOKeyword
	for i := range research.Keywords {
		if strings.EqualFold(research.Keywords[i].Keyword, keyword) {
			target = &research.Keywords[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("keyword %q is not present in research %s", keyword, research.ID)
	}
	generated := nowSEO(nil)
	if now != nil {
		generated = now()
	}
	b := &SEOBrief{
		SchemaVersion: SEOSchemaVersion, Generated: generated, Project: cfg.Project,
		Keyword: keyword, Intent: target.Intent, Audience: strings.TrimSpace(req.Audience),
		Format: strings.TrimSpace(req.Format), UniqueValue: strings.TrimSpace(req.UniqueValue),
		SuggestedTitle: titleCaseSEO(keyword) + " | " + cfg.Product,
		ResearchID:     research.ID, ExistingPages: target.ExistingPages,
	}
	if b.Format == "" {
		b.Format = suggestedSEOFormat(target.Intent, target.SERPFeatures)
	}
	b.SuggestedOutline = []string{
		"Answer the primary intent immediately",
		"Show the concrete workflow or evidence",
		"Explain tradeoffs and alternatives",
		"Apply the unique value or original asset",
		"Give the reader a useful next action",
	}
	for _, rank := range target.Rankings {
		if cfg.Domain == "" || !strings.EqualFold(normalizeDomain(rank.Domain), cfg.Domain) {
			b.CompetitorPages = append(b.CompetitorPages, rank)
		}
		if len(b.CompetitorPages) == 5 {
			break
		}
	}
	for _, page := range research.Inventory.Pages {
		if page.URL != "" && len(page.InternalLinks) > 0 {
			b.InternalLinks = append(b.InternalLinks, page.URL)
		} else if page.URL != "" {
			b.InternalLinks = append(b.InternalLinks, page.URL)
		} else if page.Path != "" {
			b.InternalLinks = append(b.InternalLinks, page.Path)
		}
		if len(b.InternalLinks) == 5 {
			break
		}
	}
	for _, ev := range target.Evidence {
		if ev.ID != "" {
			b.EvidenceIDs = append(b.EvidenceIDs, ev.ID)
		}
	}
	b.EvidenceIDs = normalizeStrings(b.EvidenceIDs)
	if len(b.UniqueValue) < cfg.Publishing.MinimumUniqueValue {
		b.Findings = append(b.Findings, SEOFinding{
			Code: "BRIEF_UNIQUE_VALUE_MISSING", Severity: "blocker",
			Message: fmt.Sprintf("unique value must be at least %d characters", cfg.Publishing.MinimumUniqueValue),
		})
	}
	if len(b.EvidenceIDs) == 0 {
		b.Findings = append(b.Findings, SEOFinding{Code: "BRIEF_EVIDENCE_MISSING", Severity: "blocker", Message: "brief has no research evidence IDs"})
	}
	if len(b.ExistingPages) > 1 {
		b.Findings = append(b.Findings, SEOFinding{Code: "BRIEF_CANNIBALIZATION_RISK", Severity: "warning", Message: "multiple owned pages overlap this keyword; consolidate or differentiate before publishing"})
	}
	b.Passed = !hasSEOBlockers(b.Findings)
	identity := *b
	identity.ID = ""
	identity.Artifact = nil
	id, err := digestSEOValue(identity)
	if err != nil {
		return nil, err
	}
	b.ID = "seobrief:" + strings.TrimPrefix(id, "sha256:")[:16]
	return b, nil
}

func (b *SEOBrief) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO brief: %s\n\n", b.Keyword)
	fmt.Fprintf(&out, "- Brief: `%s`\n- Research: `%s`\n- Intent: %s\n- Format: %s\n- Status: %s\n\n", b.ID, b.ResearchID, firstNonEmpty(b.Intent, "unknown"), b.Format, passLabel(b.Passed))
	fmt.Fprintf(&out, "## Unique value\n\n%s\n\n## Suggested outline\n\n", firstNonEmpty(b.UniqueValue, "[BLOCKED: define original user value or asset]"))
	for _, item := range b.SuggestedOutline {
		fmt.Fprintf(&out, "- %s\n", item)
	}
	if len(b.EvidenceIDs) > 0 {
		out.WriteString("\n## Evidence\n\n")
		for _, id := range b.EvidenceIDs {
			fmt.Fprintf(&out, "- `%s`\n", id)
		}
	}
	if len(b.Findings) > 0 {
		out.WriteString("\n## Findings\n\n")
		for _, f := range b.Findings {
			fmt.Fprintf(&out, "- **%s** `%s`: %s\n", strings.ToUpper(f.Severity), f.Code, f.Message)
		}
	}
	return out.String()
}

func suggestedSEOFormat(intent string, features []string) string {
	for _, feature := range features {
		if strings.Contains(strings.ToLower(feature), "comparison") {
			return "comparison"
		}
	}
	switch strings.ToLower(intent) {
	case "transactional", "commercial":
		return "decision guide"
	case "navigational":
		return "product guide"
	default:
		return "how-to guide"
	}
}

func titleCaseSEO(s string) string {
	words := strings.Fields(s)
	for i := range words {
		if len(words[i]) > 0 {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return strings.Join(words, " ")
}

func passLabel(ok bool) string {
	if ok {
		return "PASS"
	}
	return "BLOCKED"
}

func sortSEOFindings(in []SEOFinding) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Severity == in[j].Severity {
			return in[i].Code < in[j].Code
		}
		return in[i].Severity < in[j].Severity
	})
}
