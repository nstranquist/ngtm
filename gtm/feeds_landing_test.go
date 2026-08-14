package gtm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractors(t *testing.T) {
	body := `<html><head><title>Infisical — Secrets</title></head><body>
	  <h1 class="hero">  Secure Secrets, Certificates, and AI&nbsp;Agents  </h1>
	  <p>plans from $18 / user / mo</p></body></html>`
	if got := extractH1(body); got != "Secure Secrets, Certificates, and AI Agents" {
		t.Errorf("extractH1 = %q", got)
	}
	if got := extractTitle(body); got != "Infisical — Secrets" {
		t.Errorf("extractTitle = %q", got)
	}
	if got := extractPrice(body); !strings.HasPrefix(got, "$18") {
		t.Errorf("extractPrice = %q", got)
	}
}

// stubLandingServer mimics a scrape provider: it reads the requested ?url= and
// returns the homepage HTML, or the pricing HTML when the url ends with /pricing.
func stubLandingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("url")
		if strings.HasSuffix(target, "/pricing") {
			w.Write([]byte(`<html><body><h2>Plans</h2><div>Team — $18 / user / mo</div></body></html>`))
			return
		}
		w.Write([]byte(`<html><head><title>Infisical</title></head>
		  <body><h1>Secure Secrets, Certificates, and AI Agents</h1>
		  <p>Infisical helps teams manage secrets.</p></body></html>`))
	}))
}

func TestLandingFeed_ViaScrapeProvider(t *testing.T) {
	srv := stubLandingServer()
	defer srv.Close()
	t.Setenv("SCRAPE_API_URL", srv.URL)
	t.Setenv("SCRAPE_API_KEY", "test")

	f := &landingFeed{now: fixedNow}
	ev, err := f.Query(context.Background(), FeedQuery{Subject: "Infisical"})
	if err != nil {
		t.Fatalf("landing query: %v", err)
	}
	var h1, price *Evidence
	for i := range ev {
		switch ev[i].Metric {
		case "h1":
			h1 = &ev[i]
		case "pricing":
			price = &ev[i]
		}
	}
	if h1 == nil || !strings.Contains(h1.Value, "Secure Secrets") {
		t.Fatalf("expected homepage H1 evidence, got %+v", ev)
	}
	if price == nil || !strings.Contains(price.Value, "$18") {
		t.Fatalf("expected pricing evidence, got %+v", ev)
	}
}

func TestCompare_LandingResolvesCorpusClaims(t *testing.T) {
	srv := stubLandingServer()
	defer srv.Close()
	t.Setenv("SCRAPE_API_URL", srv.URL)
	t.Setenv("SCRAPE_API_KEY", "test")

	reg := &FeedRegistry{now: fixedNow}
	reg.Register(&landingFeed{now: fixedNow}) // the real landing feed, scrape-backed
	eng := NewEngineWith(reg, offlineGenerator{}, fixedNow)

	rep, err := eng.Compare(context.Background(), []string{"Infisical"}, Options{Tiers: []FeedTier{TierCheap}})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	row := rowBySubject(rep, "Infisical")
	if row == nil {
		t.Fatal("missing Infisical row")
	}
	if !strings.Contains(row.H1, "Secure Secrets") {
		t.Errorf("teardown H1 not sourced from homepage: %q", row.H1)
	}
	// The H1 claim ("Secure Secrets…") and the $18 pricing claim must now be
	// CONFIRMED from the source page, citing landing evidence.
	var h1Confirmed, priceConfirmed bool
	for _, c := range row.ClaimChecks {
		if strings.Contains(c.Text, "Secure Secrets") && c.Status == StatusConfirmed && len(c.Citations) > 0 {
			h1Confirmed = true
		}
		if strings.Contains(c.Text, "$18") && c.Status == StatusConfirmed && len(c.Citations) > 0 {
			priceConfirmed = true
		}
	}
	if !h1Confirmed {
		t.Errorf("H1 corpus claim should be confirmed from the homepage: %+v", row.ClaimChecks)
	}
	if !priceConfirmed {
		t.Errorf("pricing corpus claim should be confirmed from /pricing: %+v", row.ClaimChecks)
	}
}
