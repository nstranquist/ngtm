package gtm

import (
	"context"
	"strings"
	"testing"
)

func TestScreenBrandDomains_UsesInjectedLookups(t *testing.T) {
	lk := brandLookups{
		dotCom: func(_ context.Context, label string) domainResult {
			return domainResult{Domain: label + ".com", Status: domainRegistered, Method: "rdap"}
		},
		newGTLD: func(_ context.Context, label, tld string) domainResult {
			return domainResult{Domain: label + "." + tld, Status: domainAvailable, Method: "dns-ns"}
		},
	}
	// slugify strips separators → the registrable label "agentcontrolplane".
	got := screenBrandDomains(context.Background(), "Agent Control Plane", lk)
	if len(got) != 2 {
		t.Fatalf("expected .com + .dev probes, got %d", len(got))
	}
	if got[0].Domain != "agentcontrolplane.com" || got[0].Status != domainRegistered {
		t.Errorf(".com probe wrong: %+v", got[0])
	}
	if got[1].Domain != "agentcontrolplane.dev" || got[1].Status != domainAvailable {
		t.Errorf(".dev probe wrong: %+v", got[1])
	}
}

func TestBrandScreenWarnings_RegisteredAndCollision(t *testing.T) {
	results := []domainResult{
		{Domain: "keyring.com", Status: domainRegistered, Method: "rdap"},
		{Domain: "keyring.dev", Status: domainAvailable, Method: "dns-ns"},
	}
	w := brandScreenWarnings("keyring", results, "Q107381138 (instance of \"Python package\")", BrandKindProduct)
	if len(w) != 2 {
		t.Fatalf("expected a registered-domain warning + a collision warning, got %d: %v", len(w), w)
	}
	joined := strings.Join(w, "\n")
	if !strings.Contains(joined, "keyring.com is already registered") {
		t.Errorf("missing registered-domain warning: %v", w)
	}
	if strings.Contains(joined, "pick a name whose") {
		t.Errorf("domain must not be a naming vote: %v", w)
	}
	if !strings.Contains(joined, "name collision") || !strings.Contains(joined, "Q107381138") {
		t.Errorf("missing collision warning: %v", w)
	}
}

func TestBrandScreenWarnings_AllClear(t *testing.T) {
	results := []domainResult{
		{Domain: "garrid.com", Status: domainAvailable, Method: "rdap"},
		{Domain: "garrid.dev", Status: domainAvailable, Method: "dns-ns"},
	}
	if w := brandScreenWarnings("garrid", results, "", BrandKindProduct); len(w) != 0 {
		t.Errorf("an all-available, no-collision name should warn nothing, got: %v", w)
	}
}

func TestBrandScreenSection_RendersStatusesAndCollision(t *testing.T) {
	results := []domainResult{
		{Domain: "keyring.com", Status: domainRegistered, Method: "rdap"},
		{Domain: "keyring.dev", Status: domainUnknown, Method: "dns-ns"},
	}
	s := brandScreenSection("keyring", results, "Q107381138 (instance of \"Python package\")")
	if s.Title != "Brand Availability & IP (advisory)" {
		t.Fatalf("unexpected section title %q", s.Title)
	}
	for _, want := range []string{"keyring.com", "registered", "Name collision", "Q107381138", "NOT legal trademark clearance"} {
		if !strings.Contains(s.Body, want) {
			t.Errorf("section body missing %q", want)
		}
	}
}

func TestBrandScreenSection_AllUnknownIsSpeculative(t *testing.T) {
	results := []domainResult{
		{Domain: "x.com", Status: domainUnknown, Method: "rdap"},
		{Domain: "x.dev", Status: domainUnknown, Method: "dns-ns"},
	}
	s := brandScreenSection("x", results, "")
	if len(s.Claims) != 1 || s.Claims[0].Confidence != ConfSpeculative {
		t.Errorf("all-unknown screen should be speculative, got %+v", s.Claims)
	}
}

func TestBrandCollisionFromEvidence(t *testing.T) {
	// On-domain software entity → collision.
	onEv := []Evidence{{ID: "wikidata-claims:P31", Value: "Python package", URL: "https://www.wikidata.org/wiki/Q107381138"}}
	if c := brandCollisionFromEvidence(onEv, "keyring"); !strings.Contains(c, "Q107381138") || !strings.Contains(c, "Python package") {
		t.Errorf("expected a collision naming the entity, got %q", c)
	}
	// Off-domain entity (charity/journal) → no brand collision (disambiguation
	// warning handles that case instead).
	offEv := []Evidence{{ID: "wikidata-claims:P31", Value: "charitable organization", URL: "https://www.wikidata.org/wiki/Q16988970"}}
	if c := brandCollisionFromEvidence(offEv, "keyring"); c != "" {
		t.Errorf("off-domain entity should not be a brand collision, got %q", c)
	}
	// No P31 evidence → no collision.
	if c := brandCollisionFromEvidence([]Evidence{{ID: "hackernews:0"}}, "x"); c != "" {
		t.Errorf("no P31 → no collision, got %q", c)
	}
}
