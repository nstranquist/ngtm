package gtm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestVerifyLaunchReceiptSemanticIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "Show HN: docs-puller — measured local documentation retrieval")
	}))
	t.Cleanup(server.Close)
	ev := LaunchEvent{Product: "docs-puller", Type: EventPosted, Channel: "test-directory", URL: server.URL, ReceiptMarker: "docs-puller"}
	got := verifyLaunchReceipt(context.Background(), server.Client(), ev, false)
	if !got.Verified || got.Code != "verified" {
		t.Fatalf("verification = %+v", got)
	}
	ev.ReceiptMarker = "unrelated-product"
	got = verifyLaunchReceipt(context.Background(), server.Client(), ev, false)
	if got.Verified || got.Code != "receipt_identity_mismatch" {
		t.Fatalf("identity mismatch = %+v", got)
	}
}

func TestReceiptVerificationRejectsWrongChannelHostAndPrivateTargets(t *testing.T) {
	ev := LaunchEvent{Product: "docs-puller", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/post", ReceiptMarker: "docs-puller"}
	got := verifyLaunchReceipt(context.Background(), http.DefaultClient, ev, true)
	if got.Code != "receipt_host_mismatch" {
		t.Fatalf("wrong Show HN host = %+v", got)
	}
	if err := validatePublicReceiptTarget(mustReceiptURL(t, "http://127.0.0.1/post")); err == nil {
		t.Fatal("loopback receipt target must be rejected")
	}
	if publicReceiptIP(net.ParseIP("100.64.0.1")) {
		t.Fatal("shared address space must not count as public")
	}
}

func mustReceiptURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
