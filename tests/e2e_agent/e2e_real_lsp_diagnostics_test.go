package e2e_agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools"
)

// The one link in the diagnostics chain that nothing covered.
//
// "Realtime errors" is three things in a row: the fs client asks the LSP
// manager for diagnostics after a write, the manager gets them from a real
// language server, and the agent lifts them out of the tool result into a
// user message. The first and third are tested. The middle one — a real
// server, started, initialised, and answering in time — was always supplied
// by ForceDiagnosticsForTest, a seam that hands the tool a diagnostic it
// invented.
//
// So every existing test passes whether or not gopls works at all, and the
// part most likely to fail in a real run is the part standing in for itself.
// This drives the same path with no seam: a real server, a real module, and
// a file that genuinely does not compile.
//
// Gated, because it needs gopls on the machine and takes seconds rather than
// milliseconds:
//
//	ORCH_E2E_LSP=1 go test ./tests/e2e_agent -run TestRealLSP -v
func requireRealLSP(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH_E2E_LSP") != "1" {
		t.Skip("set ORCH_E2E_LSP=1 to run against a real language server")
	}
}

// realLSPWorkspace is a module that compiles, so the only diagnostic that can
// appear is the one the test introduces.
func realLSPWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":  "module evalws\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {\n\tprintln(Add(1, 2))\n}\n",
		"math.go": "package main\n\n// Add adds two integers.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// diagnosticsFromWrite returns what the write tool reported, the way the agent
// receives it.
func diagnosticsFromWrite(t *testing.T, r *tools.Runner, path, content string) (string, []lsp.ToolDiagnostic) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"path": path, "content": content, "must_not_exist": true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Call(context.Background(), "write", args)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	var res struct {
		Diagnostics []lsp.ToolDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("write result is not JSON the agent could read: %v\n%s", err, out)
	}
	return string(out), res.Diagnostics
}

// A file that does not compile must come back with the compiler's complaint
// attached. If this fails, every "the model was told about its mistake" claim
// in this repo rests on a stub.
func TestRealLSP_AWriteThatBreaksTheBuildReportsIt(t *testing.T) {
	requireRealLSP(t)
	root := realLSPWorkspace(t)

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	raw, diags := diagnosticsFromWrite(t, r, "broken.go",
		"package main\n\nfunc Broken() {\n\t_ = undefinedSymbol\n}\n")

	if len(diags) == 0 {
		t.Fatalf("a file referencing an undefined symbol produced no diagnostics — "+
			"the model is never told it broke the build.\nraw result:\n%s", raw)
	}
	var sawError bool
	for _, d := range diags {
		if strings.EqualFold(d.Severity, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("diagnostics came back but none is an error, so nothing stops the turn: %+v", diags)
	}
	if !strings.Contains(strings.ToLower(raw), "undefinedsymbol") {
		t.Errorf("the diagnostic must name the symbol the model got wrong, or it cannot act on it:\n%s", raw)
	}
}

// The other half, and the one that decides whether a hint is noise: a correct
// write must come back clean. A chain that reports errors for everything
// teaches the model to ignore the report.
func TestRealLSP_AWriteThatCompilesReportsNothing(t *testing.T) {
	requireRealLSP(t)
	root := realLSPWorkspace(t)

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	raw, diags := diagnosticsFromWrite(t, r, "mul.go",
		"package main\n\n// Multiply multiplies two integers.\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")

	for _, d := range diags {
		if strings.EqualFold(d.Severity, "error") {
			t.Errorf("a correct file was reported as an error, which trains the model to ignore diagnostics: %+v\n%s", d, raw)
		}
	}
}

// The other half of "realtime errors" is the tool the model can call for
// itself. lsp.diagnostics is fully wired — schema, dispatch, mode lists,
// permissions — and being wired is not the same as working: everything in
// this file was wired too, and returned nothing usable. So this asks the tool
// the question a model would ask, and requires an answer it could act on.
func TestRealLSP_TheDiagnosticsToolAnswersAboutABrokenFile(t *testing.T) {
	requireRealLSP(t)
	root := realLSPWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "bad.go"),
		[]byte("package main\n\nfunc Bad() {\n\t_ = undefinedSymbol\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	args, _ := json.Marshal(map[string]any{"path": "bad.go"})
	out, err := r.Call(context.Background(), "lsp.diagnostics", args)
	if err != nil {
		t.Fatalf("lsp.diagnostics: %v", err)
	}
	var res struct {
		Diagnostics []lsp.ToolDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("lsp.diagnostics answered with something the model cannot parse: %v\n%s", err, out)
	}
	var sawError bool
	for _, d := range res.Diagnostics {
		if strings.EqualFold(d.Severity, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("asked directly about a file that does not compile, the tool reported no error:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "undefinedsymbol") {
		t.Errorf("the answer must name the symbol, or it is not actionable:\n%s", out)
	}
}

// Editing a file the server already knows about is the common case — almost
// every change an agent makes is an edit, not a new file — and it is the case
// that has to be reliable. A new file may genuinely be outside any build until
// the server reloads; a file already in the build has no such excuse.
func TestRealLSP_AnEditThatBreaksAKnownFileIsReported(t *testing.T) {
	requireRealLSP(t)
	root := realLSPWorkspace(t)

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	// Read first so the server has the file open, exactly as an agent would.
	readArgs, _ := json.Marshal(map[string]any{"path": "math.go"})
	if _, err := r.Call(context.Background(), "read", readArgs); err != nil {
		t.Fatalf("read math.go: %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"path":    "math.go",
		"search":  "return a + b",
		"replace": "return a + undefinedSymbol",
	})
	out, err := r.Call(context.Background(), "edit", args)
	if err != nil {
		t.Fatalf("edit math.go: %v", err)
	}
	var res struct {
		Diagnostics []lsp.ToolDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("edit result is not JSON the agent could read: %v\n%s", err, out)
	}

	var sawError bool
	for _, d := range res.Diagnostics {
		if strings.EqualFold(d.Severity, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("breaking a file the server already knows produced no error diagnostic — "+
			"the agent is not told, and finishes satisfied.\nraw result:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "undefinedsymbol") {
		t.Errorf("the diagnostic must name the symbol, or the model cannot act on it:\n%s", out)
	}
}

// The failure that actually happened in an eval run: the model added
// Multiply, then wrote the whole file again with Multiply still in it, and
// declared the task done at step 2 of 8. Nothing in the workspace compiles
// afterwards. If the redeclaration is reported here, the agent had the fact
// it needed; if it is not, the agent could not have known.
func TestRealLSP_ARedeclaredFunctionIsReported(t *testing.T) {
	requireRealLSP(t)
	root := realLSPWorkspace(t)

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	raw, diags := diagnosticsFromWrite(t, r, "dup.go",
		"package main\n\n// Add adds two integers.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")

	if len(diags) == 0 {
		t.Fatalf("redeclaring Add produced no diagnostics, so a model that duplicates a "+
			"function is told nothing and finishes satisfied.\nraw result:\n%s", raw)
	}
	if !strings.Contains(strings.ToLower(raw), "redeclared") &&
		!strings.Contains(strings.ToLower(raw), "already declared") {
		t.Errorf("the diagnostic must say what is wrong, not merely that something is:\n%s", raw)
	}
}
