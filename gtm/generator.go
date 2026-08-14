package gtm

import (
	"context"
	"fmt"
	"strings"
)

// Offline is the default. Hosts inject named providers via SetNewGenerator.

// NewGeneratorFunc builds a Generator for a named provider.
type NewGeneratorFunc func(provider, model string, offline bool) (Generator, error)

var newGeneratorHook NewGeneratorFunc

// SetNewGenerator installs the host constructor. A nil fn restores the
// standalone default (named providers rejected).
func SetNewGenerator(fn NewGeneratorFunc) {
	newGeneratorHook = fn
}

// GenPrompt is one narrative-generation request. Callers only ever ask the
// Generator to WRITE PROSE around facts it is given — never to supply facts.
type GenPrompt struct {
	System      string
	User        string
	MaxTokens   int
	Temperature float64
}

// Generator turns a constrained prompt into prose.
type Generator interface {
	Label() string
	Provider() string
	Model() string
	Generate(ctx context.Context, p GenPrompt) (string, error)
}

// OfflineGenerator echoes a deterministic, clearly-labeled rendering of the
// prompt's user content. It produces no facts of its own.
type OfflineGenerator struct{}

func (OfflineGenerator) Label() string    { return "offline" }
func (OfflineGenerator) Provider() string { return "offline" }
func (OfflineGenerator) Model() string    { return "" }

func (OfflineGenerator) Generate(_ context.Context, p GenPrompt) (string, error) {
	body := strings.TrimSpace(p.User)
	if body == "" {
		return "_(offline mode: no narrative generated; see claims below)_", nil
	}
	return body, nil
}

// offlineGenerator keeps existing engine tests that construct offlineGenerator{} compiling.
type offlineGenerator = OfflineGenerator

// NewGenerator selects a generator. Empty provider (or offline=true) yields the
// deterministic offline generator. Named providers need a host hook.
func NewGenerator(provider, model string, offline bool) (Generator, error) {
	if offline || provider == "" {
		return OfflineGenerator{}, nil
	}
	if newGeneratorHook != nil {
		return newGeneratorHook(provider, model, offline)
	}
	return nil, fmt.Errorf("named provider %q requires a host inference registry; use --offline", provider)
}
