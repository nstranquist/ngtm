package gtmcli

import "github.com/nstranquist/ngtm/gtm"

// Hosts inject a lookup that must re-read the ledger on each call. A nil
// lookup leaves coverage unknown (verdicts unchanged).
var launchCoverageOverride gtm.SurfaceCoverageLookup

// SetLaunchCoverage installs the host lookup. Pass a closure that reloads
// the ledger; do not snapshot the map at process start.
func SetLaunchCoverage(fn gtm.SurfaceCoverageLookup) {
	launchCoverageOverride = fn
}

func launchCoverage() gtm.SurfaceCoverageLookup {
	return launchCoverageOverride
}
