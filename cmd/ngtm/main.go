// ngtm is the go-to-market factory — the business/marketing counterpart to the
// app-development factory. It pairs live data feeds with an LLM to produce
// citation-grounded GTM reports.
//
// The standalone binary and `ndev gtm` share gtmcli.Dispatch so the surfaces
// never drift. Style mirrors stdlib flag, hand-rolled dispatch, no Cobra.
package main

import (
	"os"

	"github.com/nstranquist/ngtm/gtmcli"
)

func main() {
	os.Exit(gtmcli.Dispatch("ngtm", os.Args[1:], os.Stdout, os.Stderr))
}
