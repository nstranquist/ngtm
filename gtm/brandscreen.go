package gtm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Brand availability screening — upgrades the blanket ipWarnings #1 advisory
// ("names are NOT screened for trademark/domain collision") into a concrete,
// best-effort check: is the brand's .com / .dev actually registered, and does a
// same-name software entity already exist? It is network-bound, so it runs as a
// brand-vertical step (not in the pure ipWarnings). Lookups are injectable for
// hermetic tests; any probe failure degrades to "unknown" and never fails the
// run. This is a SCREEN, not legal trademark clearance — the ipWarnings caveat
// still stands. Method mirrors the manual flagship-naming workflow: .com via
// Verisign RDAP (404 == free), new-gTLDs via DNS NS lookup (no NS == free).

type domainStatus string

const (
	domainAvailable  domainStatus = "available"
	domainRegistered domainStatus = "registered"
	domainUnknown    domainStatus = "unknown"
)

// domainResult is one domain's availability probe outcome.
type domainResult struct {
	Domain string
	Status domainStatus
	Method string // "rdap" | "dns-ns"
}

// brandLookups are the network probes behind screening, injectable for tests.
type brandLookups struct {
	dotCom  func(ctx context.Context, label string) domainResult      // Verisign RDAP
	newGTLD func(ctx context.Context, label, tld string) domainResult // DNS NS
}

func defaultBrandLookups() brandLookups {
	return brandLookups{dotCom: rdapDotCom, newGTLD: dnsNewGTLD}
}

// screenBrandDomains probes <slug>.com and <slug>.dev for the brand label.
func screenBrandDomains(ctx context.Context, label string, lk brandLookups) []domainResult {
	slug := slugify(label)
	if slug == "" {
		return nil
	}
	return []domainResult{
		lk.dotCom(ctx, slug),
		lk.newGTLD(ctx, slug, "dev"),
	}
}

// rdapDotCom checks <label>.com via Verisign RDAP: 404 == available,
// 200 == registered, anything else == unknown.
func rdapDotCom(ctx context.Context, label string) domainResult {
	res := domainResult{Domain: label + ".com", Status: domainUnknown, Method: "rdap"}
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	u := "https://rdap.verisign.com/com/v1/domain/" + strings.ToUpper(label) + ".COM"
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return res
	}
	req.Header.Set("User-Agent", "nicos-gtm/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		res.Status = domainAvailable
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.Status = domainRegistered
	}
	return res
}

// dnsNewGTLD checks <label>.<tld> by NS lookup: NXDOMAIN == available,
// NS records present == registered, other errors == unknown.
func dnsNewGTLD(ctx context.Context, label, tld string) domainResult {
	host := label + "." + tld
	res := domainResult{Domain: host, Status: domainUnknown, Method: "dns-ns"}
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ns, err := net.DefaultResolver.LookupNS(cctx, host)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			res.Status = domainAvailable
		}
		return res
	}
	if len(ns) > 0 {
		res.Status = domainRegistered
	} else {
		res.Status = domainAvailable
	}
	return res
}

// brandCollisionFromEvidence derives a name-collision signal from the resolved
// Wikidata entity: if the brand name already maps to an on-domain
// software/company/product entity, that is a real brand-collision risk. Returns
// "" when no on-domain entity was resolved (the disambiguation warning covers
// the off-domain case). Reuses the instance-of classifier from disambiguation.
func brandCollisionFromEvidence(ev []Evidence, subject string) string {
	for _, e := range ev {
		if e.ID != "wikidata-claims:P31" {
			continue
		}
		labels := e.Value
		if strings.TrimSpace(labels) == "" {
			labels = strings.TrimSpace(strings.TrimPrefix(e.Title, "instance of:"))
		}
		if on, _ := instanceOfDomain(labels); on {
			ent := wikidataQIDFromURL(e.URL)
			if ent == "" {
				ent = "an existing entity"
			}
			return fmt.Sprintf("%s (instance of %q)", ent, strings.TrimSpace(labels))
		}
	}
	return ""
}

// brandScreenSection renders the availability + collision findings as a report
// section. Always returns a section (even all-unknown) so the screen is visible.
func brandScreenSection(label string, results []domainResult, collision string) Section {
	var b strings.Builder
	b.WriteString("Best-effort availability screen for the brand name (NOT legal trademark clearance — still verify USPTO/TESS + a likeness check before registering):\n\n")
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{r.Domain, domainStatusLabel(r.Status), r.Method})
	}
	if len(rows) > 0 {
		b.WriteString(mdTable([]string{"Domain", "Status", "Method"}, rows))
		b.WriteString("\n")
	}
	if collision != "" {
		fmt.Fprintf(&b, "\n**Name collision:** a software/company entity already uses this name — %s. Expect a brand fight; consider a more ownable name.\n", collision)
	} else {
		b.WriteString("\nNo same-name software/company entity was found in the structured sources (a SERP feed sharpens this — see caveats).\n")
	}
	conf := ConfInferred
	if allUnknown(results) {
		conf = ConfSpeculative
	}
	return Section{
		Title:  "Brand Availability & IP (advisory)",
		Body:   b.String(),
		Claims: []Claim{{Text: "Brand availability screen for " + label, Confidence: conf}},
	}
}

// brandScreenWarnings surfaces the actionable findings as report warnings.
func brandScreenWarnings(label string, results []domainResult, collision string) []string {
	var w []string
	for _, r := range results {
		if r.Status == domainRegistered {
			w = append(w, fmt.Sprintf("brand: %s is already REGISTERED — the brand's primary domain is taken; pick a name whose .com/.dev is free or plan a modern-TLD/alt-domain.", r.Domain))
		}
	}
	if collision != "" {
		w = append(w, fmt.Sprintf("brand: name collision — a software/company entity already uses %q (%s). High trademark/SEO-collision risk; screen USPTO/TESS before committing.", label, collision))
	}
	return w
}

func domainStatusLabel(s domainStatus) string {
	switch s {
	case domainAvailable:
		return "✓ available"
	case domainRegistered:
		return "✗ registered"
	default:
		return "? unknown"
	}
}

func allUnknown(results []domainResult) bool {
	for _, r := range results {
		if r.Status != domainUnknown {
			return false
		}
	}
	return true
}
