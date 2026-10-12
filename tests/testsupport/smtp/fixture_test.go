package smtpfixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryRootUsesCurrentCheckout(t *testing.T) {
	for _, tt := range []struct{ name, relative string }{
		{"root", "."},
		{"package", "tests/testsupport/smtp"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/LunaDeerTech/agenteam\n\ngo 1.27.1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cwd := filepath.Join(root, tt.relative)
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			actual, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			got, err := repositoryRoot(actual)
			if err != nil || got != root {
				t.Fatalf("current checkout root = %q, %v; want %q", got, err, root)
			}
		})
	}
}

func TestRepositoryRootRejectsInvalidModule(t *testing.T) {
	t.Run("wrong-module-shadows-valid-parent", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/LunaDeerTech/agenteam\n"), 0600); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(root, "foreign")
		if err := os.Mkdir(nested, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module github.com/LunaDeerTech/agenteam-other\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := repositoryRoot(nested)
		if got != "" || err == nil || !strings.Contains(err.Error(), "module mismatch") {
			t.Fatalf("foreign module must not fall through to parent: %q, %v", got, err)
		}
	})
	t.Run("missing-module", func(t *testing.T) {
		// A checkout-local TMPDIR would make its go.mod a valid ancestor.
		t.Setenv("TMPDIR", "")
		got, err := repositoryRoot(t.TempDir())
		if got != "" || err == nil || !strings.Contains(err.Error(), "was not found") {
			t.Fatalf("missing module must not fall back to build checkout: %q, %v", got, err)
		}
	})
	t.Run("unreadable-module", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "go.mod"), 0700); err != nil {
			t.Fatal(err)
		}
		got, err := repositoryRoot(root)
		if got != "" || err == nil || !strings.Contains(err.Error(), "repository module:") {
			t.Fatalf("module read error must not be ignored: %q, %v", got, err)
		}
	})
}
