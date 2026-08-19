package gtm

import (
	"os"
	"path/filepath"
	"strings"
)

// Shared pointer with `ndev ask deep web-up` (nicos-dev/internal/research/web).
// Env wins; otherwise ~/.nicos-dev/searxng/url. Not a secret — localhost URL.
const searxngURLRelPath = ".nicos-dev/searxng/url"

// ResolveSearXNGURL returns the configured instance URL (no trailing slash).
func ResolveSearXNGURL() string {
	if v := strings.TrimSpace(os.Getenv("SEARXNG_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return readSearxngURLFile()
}

func readSearxngURLFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, searxngURLRelPath))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.TrimRight(line, "/")
	}
	return ""
}
