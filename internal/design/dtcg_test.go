package design

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func dtcgTestTheme(t *testing.T) Theme {
	t.Helper()
	return Generate(Options{
		Name:    "Bakeoff",
		Seed:    "#3b5bdb",
		Harmony: HarmonyAnalogous,
		Modes:   []Mode{ModeDark, ModeLight},
	})
}

func TestDTCGStructure(t *testing.T) {
	b, err := DTCG(dtcgTestTheme(t))
	if err != nil {
		t.Fatalf("DTCG: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("emitted DTCG is not valid JSON: %v", err)
	}

	color, ok := doc["color"].(map[string]any)
	if !ok {
		t.Fatal("missing color group")
	}
	if color["$type"] != "color" {
		t.Errorf("color group $type = %v, want color", color["$type"])
	}
	for _, mode := range []string{"light", "dark"} {
		grp, ok := color[mode].(map[string]any)
		if !ok {
			t.Fatalf("missing color.%s group", mode)
		}
		bg, ok := grp["bg"].(map[string]any)
		if !ok {
			t.Fatalf("missing color.%s.bg token", mode)
		}
		val, _ := bg["$value"].(string)
		if !strings.HasPrefix(val, "oklch(") {
			t.Errorf("color.%s.bg $value = %q, want oklch(...) string", mode, val)
		}
		// every semantic token must be present
		for _, name := range []string{"surface", "border", "fg", "muted", "primary", "primary-fg", "accent", "success", "warning", "danger", "info", "code"} {
			if _, ok := grp[name].(map[string]any); !ok {
				t.Errorf("color.%s missing token %q", mode, name)
			}
		}
	}

	// scalar groups
	radius, ok := doc["radius"].(map[string]any)
	if !ok || radius["$type"] != "dimension" {
		t.Fatal("missing/invalid radius dimension token")
	}
	if rv, _ := radius["$value"].(string); !strings.HasSuffix(rv, "px") {
		t.Errorf("radius $value = %v, want px", radius["$value"])
	}

	text, ok := doc["text"].(map[string]any)
	if !ok {
		t.Fatal("missing text scale group")
	}
	base, ok := text["base"].(map[string]any)
	if !ok {
		t.Fatal("missing text.base")
	}
	if bv, _ := base["$value"].(string); !strings.HasSuffix(bv, "rem") {
		t.Errorf("text.base $value = %v, want rem", base["$value"])
	}

	space, ok := doc["space"].(map[string]any)
	if !ok {
		t.Fatal("missing space scale group")
	}
	if _, ok := space["1"].(map[string]any); !ok {
		t.Error("missing space.1 (spacing is 1-indexed to match CSSVars)")
	}

	font, ok := doc["font"].(map[string]any)
	if !ok || font["$type"] != "fontFamily" {
		t.Fatal("missing/invalid font group")
	}
	if _, ok := font["sans"].(map[string]any); !ok {
		t.Error("missing font.sans")
	}
}

func TestDTCGDeterministic(t *testing.T) {
	th := dtcgTestTheme(t)
	a, err := DTCG(th)
	if err != nil {
		t.Fatalf("DTCG: %v", err)
	}
	b, err := DTCG(th)
	if err != nil {
		t.Fatalf("DTCG: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("DTCG output is not deterministic across calls")
	}
}

func TestOKLCHFromHex(t *testing.T) {
	got, ok := oklchFromHex("#ffffff")
	if !ok {
		t.Fatal("oklchFromHex(#ffffff) failed")
	}
	// pure white is L≈1, C≈0
	if !strings.HasPrefix(got, "oklch(1 0") && !strings.HasPrefix(got, "oklch(0.99") {
		t.Errorf("white = %q, want ~oklch(1 0 ...)", got)
	}
	if _, ok := oklchFromHex("not-a-color"); ok {
		t.Error("oklchFromHex accepted malformed input")
	}
}
