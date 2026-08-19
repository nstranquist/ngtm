package gtm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSearXNGURL_EnvWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".nicos-dev", "searxng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, searxngURLRelPath), []byte("http://from-file:8888\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEARXNG_URL", "http://from-env:9999/")
	if got := ResolveSearXNGURL(); got != "http://from-env:9999" {
		t.Fatalf("env should win: %q", got)
	}
}

func TestResolveSearXNGURL_FileFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SEARXNG_URL", "")
	if got := ResolveSearXNGURL(); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".nicos-dev", "searxng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, searxngURLRelPath), []byte("http://localhost:8888/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSearXNGURL(); got != "http://localhost:8888" {
		t.Fatalf("file fallback: %q", got)
	}
}
