package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

func TestDetect_GoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := provision.Detect(root)
	found := false
	for _, e := range got {
		if e.ID == "gopls" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected gopls, got %+v", got)
	}
}

func TestDetect_PackageJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := provision.Detect(root)
	found := false
	for _, e := range got {
		if e.ID == "typescript-language-server" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected typescript-language-server, got %+v", got)
	}
}

func TestMergeServers_ConfigWins(t *testing.T) {
	e, _ := registry.ByID("gopls")
	configured := []provision.ServerSpec{{
		Language:   "go",
		Extensions: []string{".go"},
		Command:    []string{"/custom/gopls", "serve"},
	}}
	merged := provision.MergeServers(configured, []registry.Entry{e})
	if len(merged) != 1 || merged[0].Command[0] != "/custom/gopls" {
		t.Fatalf("%+v", merged)
	}
}

func TestMergeServers_AddsDetected(t *testing.T) {
	e, _ := registry.ByID("gopls")
	merged := provision.MergeServers(nil, []registry.Entry{e})
	if len(merged) != 1 || merged[0].Language != "go" {
		t.Fatalf("%+v", merged)
	}
}
