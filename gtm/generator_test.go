package gtm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewGenerator_OfflineIgnoresHook(t *testing.T) {
	t.Cleanup(func() { SetNewGenerator(nil) })
	SetNewGenerator(func(provider, model string, offline bool) (Generator, error) {
		t.Fatal("hook must not run for offline")
		return nil, errors.New("unused")
	})
	g, err := NewGenerator("ollama", "llama3", true)
	if err != nil {
		t.Fatal(err)
	}
	if g.Label() != "offline" {
		t.Fatalf("label = %q", g.Label())
	}
}

func TestNewGenerator_NamedProviderRequiresHook(t *testing.T) {
	t.Cleanup(func() { SetNewGenerator(nil) })
	SetNewGenerator(nil)
	_, err := NewGenerator("ollama", "llama3", false)
	if err == nil {
		t.Fatal("expected error without hook")
	}
	if want := `named provider "ollama" requires a host inference registry`; !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want %q", err, want)
	}
}

type stubGen struct{ label string }

func (s stubGen) Label() string    { return s.label }
func (s stubGen) Provider() string { return "stub" }
func (s stubGen) Model() string    { return "m" }
func (s stubGen) Generate(context.Context, GenPrompt) (string, error) {
	return "ok", nil
}

func TestNewGenerator_NamedProviderUsesHook(t *testing.T) {
	t.Cleanup(func() { SetNewGenerator(nil) })
	var gotProvider string
	SetNewGenerator(func(provider, model string, offline bool) (Generator, error) {
		gotProvider = provider
		return stubGen{label: provider + "/" + model}, nil
	})
	g, err := NewGenerator("ollama", "llama3", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotProvider != "ollama" {
		t.Fatalf("hook provider = %q", gotProvider)
	}
	if g.Label() != "ollama/llama3" {
		t.Fatalf("label = %q", g.Label())
	}
}
