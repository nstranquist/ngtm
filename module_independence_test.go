package ngtm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleDoesNotImportFactoryKernel walks this module's Go files and
// go.mod so a nicos-tools import cannot sneak back in without failing CI.
func TestModuleDoesNotImportFactoryKernel(t *testing.T) {
	root := moduleRoot(t)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	needle := "nicos.tools/" + "nicos-dev"
	if strings.Contains(string(mod), needle) {
		t.Fatal("go.mod must not require the factory kernel module")
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), needle) {
			t.Errorf("%s imports the factory kernel module", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
