package gtm

import (
	"strings"
	"testing"
)

func TestResolveBrandKind(t *testing.T) {
	cases := []struct {
		explicit, subject, want string
	}{
		{"", "Cadence", BrandKindProduct},
		{"", "Nicos Software LLC", BrandKindEntity},
		{"", "Garrid Software L.L.C.", BrandKindEntity},
		{"", "PSPDFKit GmbH", BrandKindEntity},
		{"", "Flexibits Inc.", BrandKindEntity},
		{"entity", "Cadence", BrandKindEntity},
		{"product", "Nicos Software LLC", BrandKindProduct},
		{"legal", "whatever", BrandKindEntity},
	}
	for _, tc := range cases {
		if got := ResolveBrandKind(tc.explicit, tc.subject); got != tc.want {
			t.Errorf("ResolveBrandKind(%q, %q)=%q want %q", tc.explicit, tc.subject, got, tc.want)
		}
	}
}

func TestBrandScreenWarnings_EntityDoesNotVoteDomain(t *testing.T) {
	results := []domainResult{{Domain: "nicoslabs.com", Status: domainRegistered, Method: "rdap"}}
	w := brandScreenWarnings("Nicos Labs LLC", results, "", BrandKindEntity)
	if len(w) != 1 {
		t.Fatalf("got %v", w)
	}
	if !strings.Contains(w[0], "informational only") || !strings.Contains(w[0], "does not need a matching domain") {
		t.Errorf("entity warning wrong: %s", w[0])
	}
}
