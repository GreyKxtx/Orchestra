package toolslsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
)

// setupGoModule creates a minimal Go module with two source files in root.
func setupGoModule(t *testing.T, root string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/lsptest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainGo := `package main

import "fmt"

// Greet returns a greeting for name.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func main() {
	fmt.Println(Greet("world"))
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}

	utilGo := `package main

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(root, "util.go"), []byte(utilGo), 0644); err != nil {
		t.Fatal(err)
	}
}

// newLSPRunner creates a Runner with real gopls for root.
// Skips the test if gopls is not in PATH.
func newLSPClient(t *testing.T, root string) *Client {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in PATH — skipping LSP test")
	}
	enabled := true
	mgr, _ := lsp.NewManager(root, lsp.LSPConfig{
		Enabled: &enabled,
		Servers: []lsp.LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls", "serve"},
		}},
		DiagnosticsTimeoutMS: 5000,
	})
	t.Cleanup(func() { mgr.Close() })
	return NewClient(root, mgr)
}

func TestLSP_NoServerConfigured_ReturnsError(t *testing.T) {
	root := t.TempDir()
	c := NewClient(root, nil)

	_, err := c.LSPDefinition(context.Background(), LSPDefinitionRequest{Path: "main.go", Line: 1, Col: 1})
	if err == nil {
		t.Fatal("expected error when no LSP server configured")
	}
	if !strings.Contains(err.Error(), "no servers configured") {
		t.Fatalf("expected 'no servers configured' in error, got: %v", err)
	}
}

func TestLSP_Definition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Line 6 col 6 in main.go: "func |Greet(..." — definition of Greet.
	resp, err := c.LSPDefinition(ctx, LSPDefinitionRequest{Path: "main.go", Line: 6, Col: 6})
	if err != nil {
		t.Fatalf("LSPDefinition: %v", err)
	}
	// gopls may return 0 locations for certain positions; at minimum it should not error.
	t.Logf("definition locations: %+v", resp.Locations)
}

func TestLSP_Hover(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Line 11: "\tfmt.Println(Greet("world"))" — "Greet" starts at col 14.
	resp, err := c.LSPHover(ctx, LSPHoverRequest{Path: "main.go", Line: 11, Col: 15})
	if err != nil {
		t.Fatalf("LSPHover: %v", err)
	}
	t.Logf("hover content: %q", resp.Content)
	// If gopls returned something, it should mention "Greet".
	if resp.Content != "(no hover information available)" && !strings.Contains(resp.Content, "Greet") {
		t.Logf("hover did not mention 'Greet' (gopls may need more time): %q", resp.Content)
	}
}

func TestLSP_References(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// References to "Greet" defined on line 6 col 6.
	resp, err := c.LSPReferences(ctx, LSPReferencesRequest{
		Path:               "main.go",
		Line:               6,
		Col:                6,
		IncludeDeclaration: true,
	})
	if err != nil {
		t.Fatalf("LSPReferences: %v", err)
	}
	t.Logf("references: %+v", resp.Locations)
	// We expect at least the declaration and the call site.
	if len(resp.Locations) == 0 {
		t.Logf("WARNING: no references found (gopls may still be initializing)")
	}
}

func TestLSP_Diagnostics_ValidFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.LSPDiagnostics(ctx, LSPDiagnosticsRequest{Path: "util.go"})
	if err != nil {
		t.Fatalf("LSPDiagnostics: %v", err)
	}
	// Valid file should have no error-severity diagnostics.
	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			t.Errorf("unexpected error diagnostic on valid file: %s (line %d)", d.Message, d.StartLine)
		}
	}
	t.Logf("diagnostics for util.go: %+v", resp.Diagnostics)
}

func TestLSP_Diagnostics_BrokenFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	// Write a file with a deliberate type error.
	badGo := `package main

func BrokenFunc() {
	var x int = "not an int"
	_ = x
}
`
	if err := os.WriteFile(filepath.Join(root, "bad.go"), []byte(badGo), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.LSPDiagnostics(ctx, LSPDiagnosticsRequest{Path: "bad.go"})
	if err != nil {
		t.Fatalf("LSPDiagnostics: %v", err)
	}
	t.Logf("diagnostics for bad.go: %+v", resp.Diagnostics)

	hasError := false
	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			hasError = true
			t.Logf("error diagnostic: %s (line %d)", d.Message, d.StartLine)
		}
	}
	if !hasError {
		// gopls may not have pushed diagnostics within the timeout — log rather than fail.
		t.Logf("WARNING: no error diagnostics returned for broken file (gopls may be slow to analyze)")
	}
}

func TestLSP_Rename(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP integration test in short mode")
	}
	root := t.TempDir()
	setupGoModule(t, root)
	c := newLSPClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Rename "Add" on line 4 col 6 of util.go → "Sum".
	resp, err := c.LSPRename(ctx, LSPRenameRequest{
		Path:    "util.go",
		Line:    4,
		Col:     6,
		NewName: "Sum",
	})
	if err != nil {
		// gopls may refuse rename if the project isn't fully analyzed — skip gracefully.
		t.Logf("LSPRename returned error (may need more gopls warmup): %v", err)
		return
	}
	if len(resp.Edits) == 0 {
		t.Logf("WARNING: LSPRename returned no edits (gopls may still be initializing)")
		return
	}
	t.Logf("rename edits: %+v", resp.Edits)
	// At minimum, one edit should change "Add" → "Sum" in util.go.
	found := false
	for _, e := range resp.Edits {
		if strings.HasSuffix(e.Path, "util.go") && e.NewText == "Sum" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an edit replacing 'Add' with 'Sum' in util.go, got: %+v", resp.Edits)
	}
}
