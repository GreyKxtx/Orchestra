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

// TestSessionfileDoesNotImportTrajectory keeps the session-storage layer below
// the trajectory layer. internal/trajectory reads and writes files beside the
// session snapshot, so it depends on sessionfile's layout; the reverse import
// would invert that and becomes a cycle the moment trajectory needs anything
// from sessionfile. sessionfile.Delete therefore spells the sidecar's name by
// hand, and internal/trajectory has a test pinning the two spellings together.
func TestSessionfileDoesNotImportTrajectory(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/sessionfile/...") {
		for _, imp := range e.Imports {
			if imp == "github.com/orchestra/orchestra/internal/trajectory" ||
				strings.HasPrefix(imp, "github.com/orchestra/orchestra/internal/trajectory/") {
				t.Errorf("%s must not import %s", e.ImportPath, imp)
			}
		}
	}
}

const modulePath = "github.com/orchestra/orchestra"

// listDeps returns every package pkg links, itself included.
func listDeps(t *testing.T, root, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

// TestConfigStaysALeaf keeps internal/config below everything that reads it.
// It is imported nearly everywhere, so whatever it imports is linked into
// every package, and anything that ever needs config itself becomes a cycle.
// Memory settings are resolved in internal/memory (memory.ConfigFrom), not
// here, for that reason.
func TestConfigStaysALeaf(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{
		modulePath + "/internal/config":     true,
		modulePath + "/internal/execpolicy": true,
		modulePath + "/internal/authstore":  true,
		modulePath + "/internal/llmauth":    true,
		modulePath + "/internal/toolspec":   true,
	}
	for _, dep := range listDeps(t, root, "./internal/config") {
		if strings.HasPrefix(dep, modulePath+"/internal/") && !allowed[dep] {
			t.Errorf("internal/config links %s; resolve that package's settings in the package itself", dep)
		}
	}
}

// TestContractDoesNotRunTools keeps the contract layer to reading and checking
// artifacts. Running an external linter over them (spectral) is the agent's
// business: internal/agent/contract_spectral.go.
func TestContractDoesNotRunTools(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./internal/contract/...") {
		for _, imp := range e.Imports {
			for _, banned := range []string{"/internal/tools", "/internal/agent", "/internal/tasks", "/internal/core"} {
				if imp == modulePath+banned || strings.HasPrefix(imp, modulePath+banned+"/") {
					t.Errorf("%s must not import %s", e.ImportPath, imp)
				}
			}
		}
	}
}

// TestBinaryLinksNoTestTrees keeps test harnesses and fixtures out of the
// shipped binary. `orchestra eval` needs its harness, so the harness lives in
// internal/eval; tests/ holds only tests.
func TestBinaryLinksNoTestTrees(t *testing.T) {
	root := repoRoot(t)
	for _, dep := range listDeps(t, root, "./cmd/orchestra") {
		if strings.HasPrefix(dep, modulePath+"/tests/") {
			t.Errorf("cmd/orchestra links %s", dep)
		}
	}
}

// TestExamplesAreAssetsOnly lets the binary embed docs/examples (the templates
// `orchestra init` writes) on the condition that the package stays data: it
// imports nothing from the module, so documentation never grows code paths.
func TestExamplesAreAssetsOnly(t *testing.T) {
	root := repoRoot(t)
	for _, e := range listPackages(t, root, "./docs/examples/...") {
		for _, imp := range e.Imports {
			if strings.HasPrefix(imp, modulePath+"/") {
				t.Errorf("%s must not import %s", e.ImportPath, imp)
			}
		}
	}
}
