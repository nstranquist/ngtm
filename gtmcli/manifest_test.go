package gtmcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLandingFromManifest_GeneratesEveryPage(t *testing.T) {
	dir := t.TempDir()
	man := `{
      "apps": [
        {
          "subject": "alpha", "product": "Alpha", "by": "garrid", "design_seed": "#3b82f6",
          "headline": "Alpha headline", "tagline": "Alpha tagline",
          "buy_url": "https://buy.test/alpha", "cta": "Buy Alpha",
          "tiers": [{"name":"Pro","price":"$9","period":"one-time","featured":true}],
          "out": "` + filepath.Join(dir, "alpha.html") + `"
        },
        {
          "subject": "beta", "product": "Beta", "price": 49, "one_time": true, "tier_name": "Beta Pro",
          "buy_url": "https://buy.test/beta",
          "out": "` + filepath.Join(dir, "beta.html") + `"
        }
      ]
    }`
	manPath := filepath.Join(dir, "apps.json")
	if err := os.WriteFile(manPath, []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"landing", "--from", manPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, errOut.String())
	}

	// alpha: explicit tier + design theme + by-garrid footer.
	alpha, err := os.ReadFile(filepath.Join(dir, "alpha.html"))
	if err != nil {
		t.Fatal(err)
	}
	a := string(alpha)
	for _, want := range []string{"<!doctype html>", "Alpha headline", "https://buy.test/alpha", "$9", "by garrid"} {
		if !strings.Contains(a, want) {
			t.Errorf("alpha.html missing %q", want)
		}
	}
	if strings.Contains(a, "--bg: #0d1117") {
		t.Errorf("design seed not applied — default theme leaked into alpha")
	}

	// beta: single-price shorthand → one-time tier.
	beta, err := os.ReadFile(filepath.Join(dir, "beta.html"))
	if err != nil {
		t.Fatal(err)
	}
	b := string(beta)
	if !strings.Contains(b, "$49") || !strings.Contains(b, "one-time") || !strings.Contains(b, "Beta Pro") {
		t.Errorf("beta.html single-price shorthand wrong:\n%s", b[:min(400, len(b))])
	}

	if !strings.Contains(out.String(), "wrote 2 page(s)") {
		t.Errorf("summary missing: %s", out.String())
	}
}

func TestLandingFromManifest_StorefrontAndPlannedStubs(t *testing.T) {
	dir := t.TempDir()
	man := `{
      "storefront": {
        "out": "` + filepath.Join(dir, "products.html") + `",
        "title": "Products", "brand": "garrid", "intro": "the portfolio",
        "groups": [
          {"key": "live", "heading": "Shipped"},
          {"key": "soon", "heading": "In the works"}
        ]
      },
      "apps": [
        {
          "subject": "alpha", "product": "Alpha", "by": "garrid", "status": "shipped", "tier_group": "live",
          "card_desc": "alpha pitch", "card_price": "$9", "card_period": "one-time", "stats": ["GO"],
          "tiers": [{"name":"Pro","price":"$9","period":"one-time","featured":true}],
          "buy_url": "https://buy.test/alpha",
          "out": "` + filepath.Join(dir, "alpha.html") + `"
        },
        {
          "subject": "zeta", "product": "Zeta", "by": "garrid", "status": "planned", "tier_group": "soon",
          "tagline": "Zeta does a thing.", "card_desc": "zeta pitch",
          "out": "` + filepath.Join(dir, "zeta.html") + `"
        },
        {
          "subject": "bespoke", "product": "Bespoke", "status": "shipped", "tier_group": "live", "generate": false,
          "card_desc": "hand page", "href": "bespoke.html"
        }
      ]
    }`
	manPath := filepath.Join(dir, "apps.json")
	if err := os.WriteFile(manPath, []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Dispatch("ngtm", []string{"landing", "--from", manPath}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, errOut.String())
	}

	// Planned stub: no pricing, coming-soon, notify.
	zeta, _ := os.ReadFile(filepath.Join(dir, "zeta.html"))
	z := string(zeta)
	if strings.Contains(z, `id="pricing"`) || !strings.Contains(z, "coming soon") || !strings.Contains(z, "Notify me") {
		t.Errorf("zeta stub wrong (pricing leaked / no coming-soon / no notify)")
	}

	// Bespoke (generate:false) must NOT have produced a page.
	if _, err := os.Stat(filepath.Join(dir, "bespoke.html")); err == nil {
		t.Error("generate:false should not write a page")
	}

	// Storefront lists all three, groups them, links the bespoke hand page,
	// and flags the planned one.
	store, _ := os.ReadFile(filepath.Join(dir, "products.html"))
	st := string(store)
	for _, want := range []string{"Shipped", "In the works", "Alpha", "Zeta", "bespoke.html", "Coming soon", "$9"} {
		if !strings.Contains(st, want) {
			t.Errorf("storefront missing %q", want)
		}
	}
}

func TestLandingFromManifest_Errors(t *testing.T) {
	var out, errOut bytes.Buffer
	// Missing file.
	if code := Dispatch("ngtm", []string{"landing", "--from", "/no/such/manifest.json"}, &out, &errOut); code == 0 {
		t.Error("expected non-zero for missing manifest")
	}
	// Empty apps.
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.json")
	_ = os.WriteFile(p, []byte(`{"apps":[]}`), 0o644)
	out.Reset()
	errOut.Reset()
	if code := Dispatch("ngtm", []string{"landing", "--from", p}, &out, &errOut); code == 0 {
		t.Error("expected non-zero for empty manifest")
	}
}
