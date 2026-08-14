package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nstranquist/ngtm/gtmcli"
)

func TestVersionDispatchesShippedEngine(t *testing.T) {
	var out, errOut bytes.Buffer
	code := gtmcli.Dispatch("ngtm", []string{"version"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("version exit %d stderr=%q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("version stderr %q", errOut.String())
	}
	got := strings.TrimSpace(out.String())
	if got != gtmcli.Version || got == "" {
		t.Fatalf("version %q, want shipped engine %q", got, gtmcli.Version)
	}
}
