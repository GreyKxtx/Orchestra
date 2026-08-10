package importrules

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type listEntry struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
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

func listPackages(t *testing.T, root string, pattern string) []listEntry {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pattern, err)
	}
	var entries []listEntry
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var e listEntry
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}

func importHasPrefix(imports []string, prefix string) bool {
	for _, imp := range imports {
		if imp == prefix || strings.HasPrefix(imp, prefix+"/") {
			return true
		}
	}
	return false
}

func isUIImport(imp string) bool {
	return imp == "github.com/orchestra/orchestra/ui/tui" ||
		strings.HasPrefix(imp, "github.com/orchestra/orchestra/ui/")
}

// TestNoUIImportsInCoreLayers ensures internal packages below the CLI do not
// import ui/* except internal/cli (which hosts the TUI entry).
func TestNoUIImportsInCoreLayers(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/...") {
		if e.ImportPath == "github.com/orchestra/orchestra/internal/cli" {
			continue
		}
		for _, imp := range e.Imports {
			if isUIImport(imp) {
				t.Errorf("%s must not import %s (see docs/architecture/modules.md)", e.ImportPath, imp)
			}
		}
	}
}

// TestAgentDoesNotImportCLI keeps the agent loop independent of CLI wiring.
func TestAgentDoesNotImportCLI(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/agent/...") {
		if importHasPrefix(e.Imports, "github.com/orchestra/orchestra/internal/cli") {
			t.Errorf("%s must not import internal/cli", e.ImportPath)
		}
	}
}

// TestCoreDoesNotImportUI is the hard rule called out in modules.md.
func TestCoreDoesNotImportUI(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/core/...") {
		for _, imp := range e.Imports {
			if isUIImport(imp) {
				t.Errorf("%s must not import %s", e.ImportPath, imp)
			}
		}
	}
}

// TestNoLegacyInternalSubmodules ensures deleted pre-modularization paths are not
// reintroduced (packages moved to protocol/, patch/, llm/ sub-modules).
func TestNoLegacyInternalSubmodules(t *testing.T) {
	root := repoRoot(t)
	banned := []string{
		"github.com/orchestra/orchestra/internal/protocol",
		"github.com/orchestra/orchestra/internal/jsonrpc",
		"github.com/orchestra/orchestra/internal/schema",
		"github.com/orchestra/orchestra/internal/ops",
		"github.com/orchestra/orchestra/internal/applier",
		"github.com/orchestra/orchestra/internal/patches",
		"github.com/orchestra/orchestra/internal/resolver",
		"github.com/orchestra/orchestra/internal/fsutil",
		"github.com/orchestra/orchestra/internal/cache",
		"github.com/orchestra/orchestra/internal/relpath",
		"github.com/orchestra/orchestra/internal/daemon",
		"github.com/orchestra/orchestra/internal/llm",
	}
	for _, e := range listPackages(t, root, "./...") {
		if strings.HasPrefix(e.ImportPath, "github.com/orchestra/orchestra/protocol") ||
			strings.HasPrefix(e.ImportPath, "github.com/orchestra/orchestra/patch") ||
			strings.HasPrefix(e.ImportPath, "github.com/orchestra/orchestra/llm") {
			continue
		}
		for _, imp := range e.Imports {
			for _, b := range banned {
				if imp == b || strings.HasPrefix(imp, b+"/") {
					t.Errorf("%s imports removed package %s (use sub-module)", e.ImportPath, imp)
				}
			}
		}
	}
}

// TestSessionstoreDoesNotImportUI mirrors Phase 0 uimodel extraction.
func TestSessionstoreDoesNotImportUI(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/sessionstore/...") {
		for _, imp := range e.Imports {
			if isUIImport(imp) {
				t.Errorf("%s must not import %s", e.ImportPath, imp)
			}
		}
	}
}
