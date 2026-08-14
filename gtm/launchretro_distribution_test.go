package gtm

import (
	"strings"
	"testing"
	"time"
)

// Introducing NOT-DISTRIBUTED created a second-order bug: BuildRetro divided the
// kill and double-down rates by Posted, which counts tag-only attempts. A
// portfolio whose only "launch" was two release tags would therefore report a
// 0% kill rate and read as healthy — the same false-reading class the verdict
// change was meant to remove. Rates must divide by Distributed.
func TestBuildRetro_RatesExcludeNonDistributedAttempts(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		// Tag-only attempt: posted, but reached nobody.
		{TS: "2026-06-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
		// A real distributed attempt that earned a KILL.
		{TS: "2026-06-01T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/h"},
	}
	retro := BuildRetro(BuildLaunches(events, now), "2026-W29", 20)

	if retro.Posted != 2 {
		t.Fatalf("both attempts placed something; posted = %d", retro.Posted)
	}
	if retro.Distributed != 1 || retro.NotDistributed != 1 {
		t.Fatalf("distributed/not-distributed split = %d/%d, want 1/1", retro.Distributed, retro.NotDistributed)
	}
	// One KILL over one genuinely distributed attempt = 100%, not 50%.
	if retro.KillRate != 1.0 {
		t.Errorf("kill rate must divide by distributed (want 1.0), got %v", retro.KillRate)
	}
	md := retro.Markdown()
	if !strings.Contains(md, "distributed 1") {
		t.Errorf("markdown must surface the distributed count: %s", md)
	}
	if !strings.Contains(md, "non-distribution channels") {
		t.Errorf("markdown must explain the excluded attempts: %s", md)
	}
	var flagged bool
	for _, rec := range retro.Recommendations {
		if strings.Contains(rec, "a release tag is not a launch") {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("retro must recommend actually distributing them: %+v", retro.Recommendations)
	}
}

// With nothing genuinely distributed, the rates must stay zero rather than
// dividing by a denominator of tag-only attempts.
func TestBuildRetro_NoDistributionYieldsZeroRatesNotFalseHealth(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "docs-puller", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "docs-puller", Type: EventPosted, Channel: "github-release", URL: "https://example.com/r"},
	}
	retro := BuildRetro(BuildLaunches(events, now), "2026-W29", 20)
	if retro.Distributed != 0 {
		t.Fatalf("nothing was distributed, got %d", retro.Distributed)
	}
	if retro.KillRate != 0 || retro.DoubleDownRate != 0 {
		t.Errorf("rates must be zero with no distributed attempts, got kill=%v dd=%v", retro.KillRate, retro.DoubleDownRate)
	}
	if retro.NotDistributed != 1 {
		t.Errorf("the tag-only attempt must be counted as not-distributed, got %d", retro.NotDistributed)
	}
}

// Introducing UNMEASURED repeats the NOT-DISTRIBUTED second-order bug one step
// further along the funnel: a distributed launch onto a blind surface counts in
// Distributed but can never be KILLed, so dividing by Distributed deflates the
// kill rate from the bottom and the portfolio reads healthier than it is. The
// denominator must be attempts that could actually produce the verdict.
func TestBuildRetro_RatesExcludeUnmeasurableAttempts(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		// Distributed onto a surface nothing was watching.
		{TS: "2026-06-01T09:00:00Z", Product: "roamorph", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "roamorph", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/r"},
		// Distributed onto an instrumented surface, and nobody came.
		{TS: "2026-06-01T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/h"},
	}
	coverage := func(product string) string {
		if product == "roamorph" {
			return SurfaceBlind
		}
		return SurfaceInstrumented
	}
	retro := BuildRetro(BuildLaunchesWithCoverage(events, now, coverage), "2026-W29", 20)

	if retro.Distributed != 2 || retro.Unmeasured != 1 || retro.Judged != 1 {
		t.Fatalf("distributed/unmeasured/judged = %d/%d/%d, want 2/1/1", retro.Distributed, retro.Unmeasured, retro.Judged)
	}
	// One KILL over the one attempt we could measure = 100%, not 50%.
	if retro.KillRate != 1.0 {
		t.Errorf("kill rate must divide by judged (want 1.0), got %v", retro.KillRate)
	}
	md := retro.Markdown()
	if !strings.Contains(md, "judged 1") {
		t.Errorf("markdown must surface the honest denominator: %s", md)
	}
	if !strings.Contains(md, "cannot see arrivals") {
		t.Errorf("markdown must explain the excluded attempts: %s", md)
	}
	var flagged bool
	for _, rec := range retro.Recommendations {
		if strings.Contains(rec, "endpoints analytics --blind") {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("retro must recommend fixing the blindness: %+v", retro.Recommendations)
	}
}

// Without a coverage lookup nothing changes: unknown is not blind, so the
// denominator stays every distributed attempt and no rate moves.
func TestBuildRetro_UnknownCoverageLeavesRatesUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-06-01T09:00:00Z", Product: "roamorph", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "roamorph", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/r"},
		{TS: "2026-06-01T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W29"},
		{TS: "2026-06-01T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/h"},
	}
	retro := BuildRetro(BuildLaunches(events, now), "2026-W29", 20)
	if retro.Unmeasured != 0 || retro.Judged != 2 || retro.KillRate != 1.0 {
		t.Fatalf("unknown coverage must not move the denominator: unmeasured=%d judged=%d kill=%v",
			retro.Unmeasured, retro.Judged, retro.KillRate)
	}
}
