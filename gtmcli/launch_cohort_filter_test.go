package gtmcli

import (
	"testing"

	"github.com/nstranquist/ngtm/gtm"
)

func cohortFixture() []gtm.LaunchCohort {
	return []gtm.LaunchCohort{
		{
			Week: "2026-W29", Target: 20,
			Products: []gtm.ProductLaunch{
				{Product: "live-one"},
				{Product: "cancelled-one", Disposition: gtm.DispositionCancelled},
				{Product: "abandoned-one", Disposition: gtm.DispositionAbandoned},
			},
		},
		{
			Week: "2026-W30", Target: 20,
			Products: []gtm.ProductLaunch{
				{Product: "all-dead-a", Disposition: gtm.DispositionCancelled},
				{Product: "all-dead-b", Disposition: gtm.DispositionCancelled},
			},
		},
	}
}

func TestFilterRetiredFromCohorts_HidesRetiredAndReportsCount(t *testing.T) {
	kept, hidden := filterRetiredFromCohorts(cohortFixture(), false)
	if hidden != 4 {
		t.Errorf("expected 4 hidden retirements, got %d", hidden)
	}
	if len(kept) != 1 {
		t.Fatalf("a week made entirely of retirements must drop off the board, got %d cohorts", len(kept))
	}
	if kept[0].Week != "2026-W29" || len(kept[0].Products) != 1 || kept[0].Products[0].Product != "live-one" {
		t.Fatalf("surviving cohort = %+v", kept[0])
	}
}

func TestFilterRetiredFromCohorts_IncludeRetiredIsLossless(t *testing.T) {
	kept, hidden := filterRetiredFromCohorts(cohortFixture(), true)
	if hidden != 0 {
		t.Errorf("nothing is hidden when --include-retired is set, got %d", hidden)
	}
	if len(kept) != 2 || len(kept[0].Products) != 3 || len(kept[1].Products) != 2 {
		t.Fatalf("--include-retired must be lossless, got %+v", kept)
	}
}

// The filter must not mutate the caller's slices — cohort rendering and the JSON
// projection read the same build.
func TestFilterRetiredFromCohorts_DoesNotMutateInput(t *testing.T) {
	in := cohortFixture()
	filterRetiredFromCohorts(in, false)
	if len(in[0].Products) != 3 || len(in[1].Products) != 2 {
		t.Fatalf("input was mutated: %+v", in)
	}
}
