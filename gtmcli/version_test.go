package gtmcli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDispatchVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch("ngtm", []string{"version"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("Dispatch version exit %d stderr=%q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != Version {
		t.Fatalf("Dispatch version %q, want %q", got, Version)
	}
}
