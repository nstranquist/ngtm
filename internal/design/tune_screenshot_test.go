package design

import (
	"context"
	"strings"
	"testing"
)

func TestScreenshotThemeBytes_UsesBrowserHost(t *testing.T) {
	theme := Generate(Options{Name: "ngtm-shot", Seed: "#3b82f6"})
	_, err := ScreenshotThemeBytes(context.Background(), theme, ModeDark)
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "standalone ngtm") {
		t.Fatalf("screenshot still stubbed: %v", err)
	}
}
