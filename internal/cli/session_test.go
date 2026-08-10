package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

func writeTestConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := config.DefaultConfig(dir)
	if err := config.Save(filepath.Join(dir, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSessionExportImportCLI(t *testing.T) {
	root := t.TempDir()
	writeTestConfig(t, root)
	origWD, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	id := sessionfile.NewID()
	snap := &sessionfile.Snapshot{
		ID:         id,
		Title:      "cli roundtrip",
		UIMessages: []sessionfile.UIMessage{{Role: "user", Text: "ping"}},
	}
	if err := sessionfile.Save(root, snap); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(root, "out.session.json")
	sessionExportOut = exportPath
	if err := runSessionExport(nil, []string{id}); err != nil {
		t.Fatalf("export: %v", err)
	}

	importRoot := t.TempDir()
	writeTestConfig(t, importRoot)
	if err := os.Chdir(importRoot); err != nil {
		t.Fatal(err)
	}

	sessionImportForce = false
	if err := runSessionImport(nil, []string{exportPath}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !sessionfile.SessionExists(importRoot, id) {
		t.Fatalf("session not imported")
	}
}
