package gtmcli

import "testing"

func TestLaunchCoverage_InvokesHostLookupPerCall(t *testing.T) {
	n := 0
	SetLaunchCoverage(func(product string) string {
		n++
		return product
	})
	t.Cleanup(func() { SetLaunchCoverage(nil) })
	fn := launchCoverage()
	if fn == nil {
		t.Fatal("expected host lookup")
	}
	if got := fn("a"); got != "a" {
		t.Fatalf("first = %q", got)
	}
	if got := fn("b"); got != "b" {
		t.Fatalf("second = %q", got)
	}
	if n != 2 {
		t.Fatalf("lookup calls = %d, want 2", n)
	}
}
