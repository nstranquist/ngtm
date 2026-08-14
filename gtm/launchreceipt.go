package gtm

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxReceiptBodyBytes = 1 << 20

// ReceiptVerification is one live, semantic placement check. URL syntax is
// validated at ledger write time; this opt-in layer proves a public endpoint,
// the expected channel host, a successful response, and product identity.
type ReceiptVerification struct {
	Product       string `json:"product"`
	Channel       string `json:"channel"`
	URL           string `json:"url"`
	FinalURL      string `json:"final_url,omitempty"`
	StatusCode    int    `json:"status_code,omitempty"`
	ReceiptMarker string `json:"receipt_marker"`
	Verified      bool   `json:"verified"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

// VerifyLaunchReceipts performs opt-in network verification for every posted
// event. The client rejects loopback/private/link-local targets and redirects.
func VerifyLaunchReceipts(ctx context.Context, events []LaunchEvent) []ReceiptVerification {
	client := newPublicReceiptClient()
	var out []ReceiptVerification
	for _, ev := range events {
		if ev.Type != EventPosted {
			continue
		}
		out = append(out, verifyLaunchReceipt(ctx, client, ev, true))
	}
	return out
}

func newPublicReceiptClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           publicReceiptDialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       15 * time.Second,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   12 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validatePublicReceiptTarget(req.URL)
		},
	}
}

func publicReceiptDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, addr := range addrs {
		if !publicReceiptIP(addr.IP) {
			return nil, fmt.Errorf("receipt host %s resolves to non-public address %s", host, addr.IP)
		}
	}
	dialer := net.Dialer{Timeout: 8 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].IP.String(), port))
}

func publicReceiptIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// RFC 6598 shared address space is not considered private by every Go
	// release but is not a public placement target.
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return !shared.Contains(ip)
}

func validatePublicReceiptTarget(u *url.URL) error {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return fmt.Errorf("receipt target must be absolute http(s)")
	}
	if u.User != nil {
		return fmt.Errorf("receipt target must not contain URL credentials")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("receipt target must be public, not localhost")
	}
	if ip := net.ParseIP(host); ip != nil && !publicReceiptIP(ip) {
		return fmt.Errorf("receipt target must use a public address")
	}
	return nil
}

func verifyLaunchReceipt(ctx context.Context, client *http.Client, ev LaunchEvent, enforcePublic bool) ReceiptVerification {
	marker := strings.TrimSpace(ev.ReceiptMarker)
	if marker == "" {
		marker = ev.Product
	}
	result := ReceiptVerification{
		Product: ev.Product, Channel: ev.Channel, URL: ev.URL, ReceiptMarker: marker,
		Code: "receipt_unverified", Message: "receipt has not been verified",
	}
	u, err := url.Parse(ev.URL)
	if err != nil {
		result.Code, result.Message = "receipt_invalid_url", err.Error()
		return result
	}
	if enforcePublic {
		if err := validatePublicReceiptTarget(u); err != nil {
			result.Code, result.Message = "receipt_non_public", err.Error()
			return result
		}
	}
	if !receiptChannelHostMatches(ev.Channel, u.Hostname()) {
		result.Code = "receipt_host_mismatch"
		result.Message = fmt.Sprintf("channel %s does not accept receipt host %s", ev.Channel, u.Hostname())
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ev.URL, nil)
	if err != nil {
		result.Code, result.Message = "receipt_request_failed", err.Error()
		return result
	}
	req.Header.Set("User-Agent", "ngtm-receipt-verifier/0.3")
	resp, err := client.Do(req)
	if err != nil {
		result.Code, result.Message = "receipt_unreachable", err.Error()
		return result
	}
	result.StatusCode = resp.StatusCode
	if resp.Request != nil && resp.Request.URL != nil {
		result.FinalURL = resp.Request.URL.String()
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxReceiptBodyBytes+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		result.Code, result.Message = "receipt_read_failed", fmt.Sprintf("read=%v close=%v", readErr, closeErr)
		return result
	}
	if len(body) > maxReceiptBodyBytes {
		result.Code, result.Message = "receipt_too_large", "receipt body exceeds 1 MiB verification limit"
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Code, result.Message = "receipt_http_status", fmt.Sprintf("receipt returned HTTP %d", resp.StatusCode)
		return result
	}
	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL
	}
	if enforcePublic {
		if err := validatePublicReceiptTarget(finalURL); err != nil {
			result.Code, result.Message = "receipt_non_public_redirect", err.Error()
			return result
		}
	}
	if !receiptChannelHostMatches(ev.Channel, finalURL.Hostname()) {
		result.Code, result.Message = "receipt_host_mismatch", fmt.Sprintf("channel %s redirected to disallowed host %s", ev.Channel, finalURL.Hostname())
		return result
	}
	if !receiptContainsMarker(finalURL.String(), body, marker) {
		result.Code, result.Message = "receipt_identity_mismatch", fmt.Sprintf("receipt does not contain expected marker %q", marker)
		return result
	}
	result.Verified = true
	result.Code = "verified"
	result.Message = "public receipt is reachable and contains the expected placement identity"
	return result
}

func receiptContainsMarker(rawURL string, body []byte, marker string) bool {
	haystack := strings.ToLower(rawURL + "\n" + string(body))
	needle := strings.ToLower(strings.TrimSpace(marker))
	if needle == "" {
		return false
	}
	if strings.Contains(haystack, needle) {
		return true
	}
	words := strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(needle)
	return words != needle && strings.Contains(haystack, words)
}

func receiptChannelHostMatches(channel, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	allowed := map[string][]string{
		"show-hn":      {"news.ycombinator.com"},
		"producthunt":  {"producthunt.com"},
		"reddit":       {"reddit.com"},
		"x":            {"x.com", "twitter.com"},
		"linkedin":     {"linkedin.com"},
		"indiehackers": {"indiehackers.com"},
	}[channel]
	if len(allowed) == 0 {
		return true
	}
	for _, suffix := range allowed {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
