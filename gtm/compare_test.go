package gtm

import (
	"context"
	"strings"
	"testing"
)

func TestCompare_GroundedTeardownAndClaims(t *testing.T) {
	eng := NewEngineWith(businessRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Compare(context.Background(), []string{"Stripe", "nvault"}, Options{Tiers: []FeedTier{TierFree}})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rep.Rows))
	}

	// Stripe row: grounded firmographics + mentions from the fake feeds.
	stripe := rowBySubject(rep, "Stripe")
	if stripe == nil || stripe.Industry == "" || stripe.Mentions == 0 {
		t.Fatalf("Stripe row not grounded: %+v", stripe)
	}

	// nvault has a corpus claim (mentions). With HN mention evidence present in
	// the fake registry, it should be confirmed and cite real evidence.
	nv := rowBySubject(rep, "nvault")
	if nv == nil || len(nv.ClaimChecks) == 0 {
		t.Fatalf("nvault should carry corpus claim checks: %+v", nv)
	}
	var sawConfirmed bool
	for _, c := range nv.ClaimChecks {
		if c.Status == StatusConfirmed && len(c.Citations) > 0 {
			sawConfirmed = true
		}
	}
	if !sawConfirmed {
		t.Errorf("expected a confirmed nvault mentions claim with citations: %+v", nv.ClaimChecks)
	}

	// Markdown renders the table + claim checks.
	md := rep.Markdown()
	if !strings.Contains(md, "| Competitor |") || !strings.Contains(md, "corpus claim checks") {
		t.Errorf("markdown missing table/claim sections")
	}
}

func TestCompare_SerpClaimsUnverifiedWithoutSerpFeed(t *testing.T) {
	// businessRegistry has no serp_rank evidence → Infisical's H1/pricing claims
	// must come back unverified, never fabricated as confirmed.
	eng := NewEngineWith(businessRegistry(), offlineGenerator{}, fixedNow)
	rep, err := eng.Compare(context.Background(), []string{"Infisical"}, Options{Tiers: []FeedTier{TierFree}})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	row := rowBySubject(rep, "Infisical")
	if row == nil || len(row.ClaimChecks) == 0 {
		t.Fatalf("Infisical should carry corpus claims")
	}
	for _, c := range row.ClaimChecks {
		if c.Status == StatusConfirmed {
			t.Errorf("no SERP feed ran — H1/pricing claim must not be confirmed: %+v", c)
		}
	}
}

func TestCompare_OfflineHermetic(t *testing.T) {
	eng := NewEngineWith(&FeedRegistry{now: fixedNow}, offlineGenerator{}, fixedNow)
	rep, err := eng.Compare(context.Background(), []string{"nvault", "Doppler"}, Options{NoFeeds: true})
	if err != nil {
		t.Fatalf("Compare offline: %v", err)
	}
	for _, row := range rep.Rows {
		if row.Survives {
			t.Errorf("offline row should not survive the panel: %s", row.Subject)
		}
		for _, c := range row.ClaimChecks {
			if c.Status == StatusConfirmed {
				t.Errorf("offline: no real evidence, nothing should be confirmed: %+v", c)
			}
		}
	}
}

func rowBySubject(r *CompareReport, subject string) *CompareRow {
	for i := range r.Rows {
		if r.Rows[i].Subject == subject {
			return &r.Rows[i]
		}
	}
	return nil
}
