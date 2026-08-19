package gtm

import (
	"strings"
	"testing"
	"time"
)

func TestBuildGEOMeasureIgnoresFailedEngines(t *testing.T) {
	cfg := testGEOConfig()
	probe := GEOProbeReport{
		Rows: []GEOProbeRow{
			{PromptID: "p1", Prompt: cfg.Prompts[0].Text, Engine: GEOEngineOpenAIChat, Error: "gemini HTTP 404"},
			{PromptID: "p1", Prompt: cfg.Prompts[0].Text, Engine: GEOEngineOpenAIChat, Mentioned: false, Competitors: []string{"Dash"}},
		},
	}
	got := BuildGEOMeasure(cfg, probe, func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) })
	row := got.Rows[0]
	if row.OK != 1 || row.Errors != 1 || row.Visibility != 0 || row.Unmeasured {
		t.Fatalf("row=%+v", row)
	}
	table := FormatGEOTable(*got)
	if strings.Contains(table, "0/2") {
		t.Fatalf("table still treats the failed engine as a live run:\n%s", table)
	}
	if !strings.Contains(table, "0/1") {
		t.Fatalf("table=%s", table)
	}
}

func TestBuildGEOMeasureAllErrorsUnmeasured(t *testing.T) {
	cfg := testGEOConfig()
	probe := GEOProbeReport{
		Rows: []GEOProbeRow{
			{PromptID: "p1", Prompt: cfg.Prompts[0].Text, Engine: GEOEngineGemini, Error: "timeout"},
		},
	}
	got := BuildGEOMeasure(cfg, probe, func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) })
	if got.Passed || !got.Rows[0].Unmeasured || got.MentionRate != 0 {
		t.Fatalf("report=%+v row=%+v", got, got.Rows[0])
	}
	if !strings.Contains(FormatGEOTable(*got), "n/a") {
		t.Fatalf("expected n/a visibility:\n%s", FormatGEOTable(*got))
	}
}
