package gtm

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 80, B: 180, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectProductHuntPackReady(t *testing.T) {
	dir := t.TempDir()
	thumb := filepath.Join("framed", "product-hunt-thumb.png")
	g1 := filepath.Join("framed", "product-hunt-01.png")
	g2 := filepath.Join("framed", "product-hunt-02.png")
	writePNG(t, filepath.Join(dir, thumb), 240, 240)
	writePNG(t, filepath.Join(dir, g1), 1270, 760)
	writePNG(t, filepath.Join(dir, g2), 1270, 760)
	man := map[string]any{
		"schema": shotPackSchema,
		"title":  "Demo",
		"artifacts": []map[string]any{
			{"role": "product-hunt.thumbnail", "path": thumb},
			{"role": "product-hunt.gallery", "path": g1},
			{"role": "product-hunt.gallery", "path": g2},
		},
	}
	raw, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	kit, err := InspectProductHuntPack("demo", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !kit.Ready {
		t.Fatalf("expected ready, errors=%v", kit.Errors)
	}
}

func TestInspectProductHuntPackMissingGallery(t *testing.T) {
	dir := t.TempDir()
	thumb := filepath.Join("framed", "product-hunt-thumb.png")
	writePNG(t, filepath.Join(dir, thumb), 240, 240)
	man := map[string]any{
		"schema": shotPackSchema,
		"artifacts": []map[string]any{
			{"role": "product-hunt.thumbnail", "path": thumb},
		},
	}
	raw, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	kit, err := InspectProductHuntPack("demo", dir)
	if err != nil {
		t.Fatal(err)
	}
	if kit.Ready {
		t.Fatal("single still must not be ready")
	}
}
