package gtm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nstranquist/ngtm/internal/atomicfile"
	"github.com/nstranquist/ngtm/internal/jsonl"
)

type SEOArtifactRef struct {
	Kind      string `json:"kind" yaml:"kind"`
	ID        string `json:"id" yaml:"id"`
	Digest    string `json:"digest" yaml:"digest"`
	Path      string `json:"path" yaml:"path"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
}

type SEOStore struct {
	Root    string
	Project string
	Now     func() string
}

func DefaultSEOStoreRoot() (string, error) {
	if v := strings.TrimSpace(os.Getenv("NGTM_SEO_WORKSPACE")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nicos-dev", "gtm", "seo"), nil
}

// NewSEOStore creates a project store. workspace, when non-empty, is the exact
// project directory; otherwise the default root is joined with project.
func NewSEOStore(workspace, project string) (*SEOStore, error) {
	project = normalizeSEOProject(project)
	if project == "" {
		return nil, errors.New("SEO store project is required")
	}
	root := strings.TrimSpace(workspace)
	if root == "" {
		base, err := DefaultSEOStoreRoot()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(base, project)
	}
	return &SEOStore{Root: root, Project: project, Now: func() string { return nowSEO(nil) }}, nil
}

func (s *SEOStore) WriteArtifact(kind string, value any) (SEOArtifactRef, error) {
	kind = normalizeSEOProject(kind)
	if kind == "" {
		return SEOArtifactRef{}, errors.New("artifact kind is required")
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return SEOArtifactRef{}, fmt.Errorf("marshal %s artifact: %w", kind, err)
	}
	body = append(body, '\n')
	digest := sha256.Sum256(body)
	hexDigest := hex.EncodeToString(digest[:])
	id := "sha256:" + hexDigest
	dir := filepath.Join(s.Root, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SEOArtifactRef{}, err
	}
	path := filepath.Join(dir, hexDigest+".json")
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(body) {
			return SEOArtifactRef{}, fmt.Errorf("artifact digest collision at %s", path)
		}
	} else if !os.IsNotExist(err) {
		return SEOArtifactRef{}, err
	} else if err := atomicfile.WriteFile(path, body, 0o644); err != nil {
		return SEOArtifactRef{}, err
	}
	ref := SEOArtifactRef{Kind: kind, ID: id, Digest: id, Path: path, CreatedAt: s.Now()}
	latestBody, _ := json.MarshalIndent(ref, "", "  ")
	latestBody = append(latestBody, '\n')
	if err := atomicfile.WriteFile(filepath.Join(dir, "latest.json"), latestBody, 0o644); err != nil {
		return SEOArtifactRef{}, err
	}
	event, _ := json.Marshal(map[string]any{
		"schema_version": 1, "ts": ref.CreatedAt, "project": s.Project,
		"kind": kind, "artifact_id": id, "path": path,
	})
	if err := jsonl.AppendLineDurable(filepath.Join(s.Root, "events.jsonl"), event, 0o644); err != nil {
		return SEOArtifactRef{}, err
	}
	return ref, nil
}

func (s *SEOStore) LatestRef(kind string) (SEOArtifactRef, error) {
	kind = normalizeSEOProject(kind)
	path := filepath.Join(s.Root, kind, "latest.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return SEOArtifactRef{}, err
	}
	var ref SEOArtifactRef
	if err := json.Unmarshal(b, &ref); err != nil {
		return SEOArtifactRef{}, fmt.Errorf("parse latest %s: %w", kind, err)
	}
	if err := verifySEOArtifactRef(ref); err != nil {
		return SEOArtifactRef{}, err
	}
	if ref.Kind != kind {
		return SEOArtifactRef{}, fmt.Errorf("SEO latest pointer kind=%s, want %s", ref.Kind, kind)
	}
	wantDir, err := filepath.Abs(filepath.Join(s.Root, kind))
	if err != nil {
		return SEOArtifactRef{}, err
	}
	artifactPath, err := filepath.Abs(ref.Path)
	if err != nil {
		return SEOArtifactRef{}, err
	}
	if filepath.Dir(artifactPath) != wantDir || filepath.Ext(artifactPath) != ".json" {
		return SEOArtifactRef{}, fmt.Errorf("SEO latest pointer escapes %s", wantDir)
	}
	if filepath.Base(artifactPath) != strings.TrimPrefix(ref.Digest, "sha256:")+".json" {
		return SEOArtifactRef{}, errors.New("SEO latest pointer filename does not match its digest")
	}
	return ref, nil
}

func (s *SEOStore) LoadLatest(kind string, out any) (SEOArtifactRef, error) {
	ref, err := s.LatestRef(kind)
	if err != nil {
		return SEOArtifactRef{}, err
	}
	return ref, loadSEOArtifact(ref, out)
}

func (s *SEOStore) LoadRef(pathOrID, kind string, out any) (SEOArtifactRef, error) {
	pathOrID = strings.TrimSpace(pathOrID)
	if pathOrID == "" {
		return s.LoadLatest(kind, out)
	}
	path := pathOrID
	if strings.HasPrefix(pathOrID, "sha256:") {
		if !validSEOArtifactDigest(pathOrID) {
			return SEOArtifactRef{}, errors.New("invalid SEO artifact SHA-256 ID")
		}
		path = filepath.Join(s.Root, normalizeSEOProject(kind), strings.TrimPrefix(pathOrID, "sha256:")+".json")
	}
	ref := SEOArtifactRef{Kind: normalizeSEOProject(kind), Path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		return SEOArtifactRef{}, err
	}
	d := sha256.Sum256(b)
	ref.ID = "sha256:" + hex.EncodeToString(d[:])
	ref.Digest = ref.ID
	if err := json.Unmarshal(b, out); err != nil {
		return SEOArtifactRef{}, err
	}
	return ref, nil
}

func loadSEOArtifact(ref SEOArtifactRef, out any) error {
	b, err := os.ReadFile(ref.Path)
	if err != nil {
		return err
	}
	d := sha256.Sum256(b)
	got := "sha256:" + hex.EncodeToString(d[:])
	if got != ref.Digest {
		return fmt.Errorf("SEO artifact digest mismatch: got %s want %s", got, ref.Digest)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("parse SEO artifact %s: %w", ref.Path, err)
	}
	return nil
}

func verifySEOArtifactRef(ref SEOArtifactRef) error {
	if ref.Kind == "" || ref.Path == "" || !validSEOArtifactDigest(ref.Digest) {
		return errors.New("invalid SEO artifact reference")
	}
	if ref.ID != ref.Digest {
		return errors.New("SEO artifact reference ID does not match digest")
	}
	return nil
}

func validSEOArtifactDigest(value string) bool {
	hexValue := strings.TrimPrefix(value, "sha256:")
	if len(hexValue) != sha256.Size*2 || value != "sha256:"+hexValue {
		return false
	}
	decoded, err := hex.DecodeString(hexValue)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(hexValue) == hexValue
}

func digestSEOValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	d := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(d[:]), nil
}
