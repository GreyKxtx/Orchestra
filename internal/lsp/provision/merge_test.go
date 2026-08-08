package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

func TestMergeServersForWorkspace_TSOnlyPrunesPolyglotConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	goE, _ := registry.ByID("gopls")
	tsE, _ := registry.ByID("typescript-language-server")
	pyE, _ := registry.ByID("basedpyright")
	configured := []provision.ServerSpec{
		provision.SpecFromEntry(goE),
		provision.SpecFromEntry(tsE),
		provision.SpecFromEntry(pyE),
	}
	merged := provision.MergeServersForWorkspace(configured, root)
	if len(merged) != 1 || merged[0].Language != "typescript" {
		t.Fatalf("want typescript only, got %+v", merged)
	}
}

func TestMergeServersForWorkspace_GoTSMonorepoKeepsBoth(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	goE, _ := registry.ByID("gopls")
	tsE, _ := registry.ByID("typescript-language-server")
	pyE, _ := registry.ByID("basedpyright")
	configured := []provision.ServerSpec{
		provision.SpecFromEntry(goE),
		provision.SpecFromEntry(tsE),
		provision.SpecFromEntry(pyE),
	}
	merged := provision.MergeServersForWorkspace(configured, root)
	langs := map[string]bool{}
	for _, s := range merged {
		langs[s.Language] = true
	}
	if !langs["go"] || !langs["typescript"] {
		t.Fatalf("want go+typescript, got %+v", merged)
	}
	if langs["python"] {
		t.Fatalf("python should be pruned, got %+v", merged)
	}
}

func TestMergeServersForWorkspace_EmptyRepoKeepsConfigured(t *testing.T) {
	root := t.TempDir()
	goE, _ := registry.ByID("gopls")
	configured := []provision.ServerSpec{provision.SpecFromEntry(goE)}
	merged := provision.MergeServersForWorkspace(configured, root)
	if len(merged) != 1 || merged[0].Language != "go" {
		t.Fatalf("%+v", merged)
	}
}
