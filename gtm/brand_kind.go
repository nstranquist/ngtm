package gtm

import (
	"regexp"
	"strings"
)

const (
	BrandKindProduct = "product"
	BrandKindEntity  = "entity"
)

// legalEntitySuffix matches a legal-form suffix that means the subject is a
// company name, not a product to logo/land.
var legalEntitySuffix = regexp.MustCompile(`(?i)\b(l\.?l\.?c\.?|inc\.?|gmbh|ltd\.?|llp|plc|corp\.?|corporation)\b`)

// ResolveBrandKind returns "entity" or "product".
// explicit wins when it is one of those two; otherwise the subject is scanned
// for a legal suffix (LLC, Inc, GmbH, …).
func ResolveBrandKind(explicit, subject string) string {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case BrandKindEntity, "legal", "llc", "company":
		return BrandKindEntity
	case BrandKindProduct, "app":
		return BrandKindProduct
	}
	if legalEntitySuffix.MatchString(subject) {
		return BrandKindEntity
	}
	return BrandKindProduct
}
