package gtm

import "testing"

func testGEOConfig() GEOProductConfig {
	cfg := GEOProductConfig{
		SchemaVersion: 1,
		Project:       "docs-puller",
		Product:       "docs-puller",
		Brand:         "docs-puller",
		Aliases:       []string{"docs-puller", "docs puller"},
		Competitors: []GEOCompetitor{
			{Name: "Context7", Aliases: []string{"context7"}},
			{Name: "DevDocs", Aliases: []string{"devdocs"}},
			{Name: "Dash", Aliases: []string{"dash"}},
		},
		Prompts: []GEOPrompt{{ID: "p1", Text: "best local docs search"}},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		panic(err)
	}
	return cfg
}

func TestScoreGEOAnswerRanksFirstMention(t *testing.T) {
	text := "Context7 is common. For a local corpus, docs-puller is the best option. DevDocs is online-only."
	got := ScoreGEOAnswer(testGEOConfig(), text)
	if !got.Mentioned || got.Position != 2 {
		t.Fatalf("position=%d mentioned=%v, want position 2", got.Position, got.Mentioned)
	}
	if got.Visibility != 1 {
		t.Fatalf("visibility=%v", got.Visibility)
	}
	if len(got.Competitors) != 2 || got.Competitors[0] != "Context7" || got.Competitors[1] != "DevDocs" {
		t.Fatalf("competitors=%v", got.Competitors)
	}
	if got.Sentiment < 70 {
		t.Fatalf("sentiment=%d, want recommended window", got.Sentiment)
	}
}

func TestScoreGEOAnswerUnmentioned(t *testing.T) {
	got := ScoreGEOAnswer(testGEOConfig(), "Use Context7 or DevDocs.")
	if got.Mentioned || got.Position != 0 || got.Visibility != 0 || got.Sentiment != 0 {
		t.Fatalf("unmentioned score=%+v", got)
	}
	if len(got.Competitors) != 2 {
		t.Fatalf("competitors=%v", got.Competitors)
	}
}

func TestScoreGEOAnswerIgnoresPartialTokens(t *testing.T) {
	got := ScoreGEOAnswer(testGEOConfig(), "The dashboard is not Dash.")
	if !containsGEO(got.Competitors, "Dash") {
		t.Fatalf("expected Dash from token Dash; %+v", got)
	}
	got = ScoreGEOAnswer(testGEOConfig(), "The dashboard has no local index.")
	if containsGEO(got.Competitors, "Dash") {
		t.Fatalf("dashboard should not match Dash: %+v", got)
	}
}

func TestLoadTrackedDocsPullerGEOConfig(t *testing.T) {
	cfg, err := LoadGEOProductConfig("../docs/geo/docs-puller.geo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "docs-puller" || len(cfg.Prompts) != 20 {
		t.Fatalf("project=%s prompts=%d", cfg.Project, len(cfg.Prompts))
	}
}

func TestExtractGEOURLs(t *testing.T) {
	got := extractGEOURLs("See https://github.com/nstranquist/docs-puller and https://devdocs.io/.")
	if len(got) != 2 || got[0] != "https://github.com/nstranquist/docs-puller" {
		t.Fatalf("urls=%v", got)
	}
}

func containsGEO(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
