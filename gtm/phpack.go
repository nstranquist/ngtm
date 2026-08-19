package gtm

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

const (
	shotPackSchema        = "nicos.browser.shot-pack.v1"
	productHuntComposer   = "https://www.producthunt.com/posts/new"
	productHuntThumbW     = 240
	productHuntThumbH     = 240
	productHuntGalleryW   = 1270
	productHuntGalleryH   = 760
	productHuntImageBytes = 5 << 20
)

// PHAsset is one file mapped onto a Product Hunt upload slot.
type PHAsset struct {
	Slot   string `json:"slot"`
	Role   string `json:"role"`
	Path   string `json:"path"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Bytes  int    `json:"bytes,omitempty"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// PHAssetsKit is the fail-closed checklist for posting from an ndev browser
// shot pack. The live PRODUCTHUNT_API_TOKEN can read posts and follow users;
// it cannot create a post or upload media, so this kit feeds the public composer.
type PHAssetsKit struct {
	Product     string    `json:"product"`
	PackDir     string    `json:"pack_dir"`
	Title       string    `json:"title,omitempty"`
	SourceURL   string    `json:"source_url,omitempty"`
	ComposerURL string    `json:"composer_url"`
	Assets      []PHAsset `json:"assets"`
	Ready       bool      `json:"ready"`
	Errors      []string  `json:"errors,omitempty"`
}

type shotPackManifest struct {
	Schema     string `json:"schema"`
	SourceURL  string `json:"source_url"`
	Title      string `json:"title"`
	Artifacts  []struct {
		Role   string `json:"role"`
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Bytes  int    `json:"bytes"`
	} `json:"artifacts"`
}

// InspectProductHuntPack maps a nicos.browser.shot-pack.v1 directory onto
// Product Hunt's thumbnail (240×240) + two 1270×760 gallery stills.
func InspectProductHuntPack(product, dir string) (PHAssetsKit, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return PHAssetsKit{}, fmt.Errorf("pack directory is required")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return PHAssetsKit{}, fmt.Errorf("read pack manifest: %w", err)
	}
	var man shotPackManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return PHAssetsKit{}, fmt.Errorf("decode pack manifest: %w", err)
	}
	if man.Schema != "" && man.Schema != shotPackSchema {
		return PHAssetsKit{}, fmt.Errorf("unsupported pack schema %q (want %s)", man.Schema, shotPackSchema)
	}
	kit := PHAssetsKit{
		Product:     product,
		PackDir:     dir,
		Title:       man.Title,
		SourceURL:   man.SourceURL,
		ComposerURL: productHuntComposer,
	}
	abs := func(rel string) string {
		if filepath.IsAbs(rel) {
			return rel
		}
		return filepath.Join(dir, rel)
	}
	var galleries []PHAsset
	for _, a := range man.Artifacts {
		path := abs(a.Path)
		switch {
		case strings.Contains(a.Role, "thumbnail"):
			kit.Assets = append(kit.Assets, checkPHStill("thumbnail", a.Role, path, productHuntThumbW, productHuntThumbH))
		case strings.Contains(a.Role, "gallery"):
			galleries = append(galleries, checkPHStill("gallery", a.Role, path, productHuntGalleryW, productHuntGalleryH))
		case strings.HasSuffix(a.Path, ".mp4"):
			kit.Assets = append(kit.Assets, checkPHFile("video", a.Role, path))
		case strings.HasSuffix(a.Path, ".gif"):
			kit.Assets = append(kit.Assets, checkPHFile("gif", a.Role, path))
		}
	}
	kit.Assets = append(kit.Assets, galleries...)
	var thumbs, okGallery int
	for _, a := range kit.Assets {
		if !a.OK && a.Error != "" {
			kit.Errors = append(kit.Errors, a.Error)
		}
		if a.Slot == "thumbnail" && a.OK {
			thumbs++
		}
		if a.Slot == "gallery" && a.OK {
			okGallery++
		}
	}
	if thumbs == 0 {
		kit.Errors = append(kit.Errors, "missing 240×240 thumbnail — capture with ndev browser shot --preset product-hunt")
	}
	if okGallery < 2 {
		kit.Errors = append(kit.Errors, fmt.Sprintf("gallery needs 2+ images at 1270×760 (have %d)", okGallery))
	}
	kit.Ready = len(kit.Errors) == 0
	return kit, nil
}

func checkPHStill(slot, role, path string, wantW, wantH int) PHAsset {
	a := checkPHFile(slot, role, path)
	if !a.OK {
		return a
	}
	f, err := os.Open(path)
	if err != nil {
		a.OK = false
		a.Error = err.Error()
		return a
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		a.OK = false
		a.Error = fmt.Sprintf("%s: decode: %v", path, err)
		return a
	}
	a.Width, a.Height = cfg.Width, cfg.Height
	if cfg.Width != wantW || cfg.Height != wantH {
		a.OK = false
		a.Error = fmt.Sprintf("%s: got %dx%d, want %dx%d", path, cfg.Width, cfg.Height, wantW, wantH)
	}
	if a.Bytes > productHuntImageBytes {
		a.OK = false
		a.Error = fmt.Sprintf("%s: %d bytes exceeds 5MB gallery cap", path, a.Bytes)
	}
	return a
}

func checkPHFile(slot, role, path string) PHAsset {
	a := PHAsset{Slot: slot, Role: role, Path: path, OK: true}
	info, err := os.Stat(path)
	if err != nil {
		a.OK = false
		a.Error = fmt.Sprintf("%s: %v", path, err)
		return a
	}
	a.Bytes = int(info.Size())
	if a.Bytes == 0 {
		a.OK = false
		a.Error = fmt.Sprintf("%s: empty file", path)
	}
	return a
}
