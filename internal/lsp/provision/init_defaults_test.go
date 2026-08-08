package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
)

func TestInitServerSpecs_DetectsGo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs := provision.InitServerSpecs(root)
	if len(specs) == 0 {
		t.Fatal("expected at least one server")
	}
	found := false
	for _, s := range specs {
		if s.Language == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected go server, got %+v", specs)
	}
}

func TestInitServerSpecs_EmptyRootPolyglotFallback(t *testing.T) {
	root := t.TempDir()
	specs := provision.InitServerSpecs(root)
	if len(specs) < 3 {
		t.Fatalf("expected polyglot fallback (go+ts+py), got %+v", specs)
	}
	langs := map[string]bool{}
	for _, s := range specs {
		langs[s.Language] = true
	}
	for _, want := range []string{"go", "typescript", "python"} {
		if !langs[want] {
			t.Fatalf("missing %q in %+v", want, specs)
		}
	}
}
