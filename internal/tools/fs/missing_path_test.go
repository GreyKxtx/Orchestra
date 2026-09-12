package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// A small local model that guesses a wrong path used to get the raw OS error
// back — "GetFileAttributesEx C:\...\scratchpad\eval\ws\evalws: The system
// cannot find the file specified." That answer is actively harmful: it names
// no relative path, suggests nothing, and hands the model fresh absolute-path
// fragments to guess with. In an evaluation run one model spent five ls calls
// walking pieces of that string before the circuit breaker ended the turn.
//
// What comes back must instead name the path the model asked for, and say
// what really is there.
func newMissingPathRunner(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func seedWorkspace(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"main.go", "strutil.go", "go.mod"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "store"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFSList_MissingPathNamesTheRelativePathAndWhatIsThere(t *testing.T) {
	r, root := newMissingPathRunner(t)
	seedWorkspace(t, root)

	_, err := r.FSList(context.Background(), tools.FSListRequest{Path: "evalws"})
	if err == nil {
		t.Fatal("listing a path that does not exist must fail")
	}
	msg := err.Error()

	if !strings.Contains(msg, "evalws") {
		t.Errorf("the error must name the path the model asked for, got: %s", msg)
	}
	if strings.Contains(msg, root) {
		t.Errorf("the error must not leak the absolute workspace root, got: %s", msg)
	}
	if strings.Contains(msg, "GetFileAttributesEx") || strings.Contains(msg, "no such file or directory") {
		t.Errorf("the raw OS error must not reach the model, got: %s", msg)
	}
	// The one thing that actually unblocks a lost model: what IS there.
	for _, want := range []string{"main.go", "internal"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error must list what the nearest existing directory holds (missing %q), got: %s", want, msg)
		}
	}
}

func TestFSList_MissingNestedPathPointsAtTheNearestRealDirectory(t *testing.T) {
	r, root := newMissingPathRunner(t)
	seedWorkspace(t, root)

	_, err := r.FSList(context.Background(), tools.FSListRequest{Path: "internal/telemetry/otel"})
	if err == nil {
		t.Fatal("listing a path that does not exist must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "internal/telemetry/otel") {
		t.Errorf("the error must name the requested path, got: %s", msg)
	}
	if !strings.Contains(msg, "store") {
		t.Errorf("the error must describe the nearest directory that does exist (internal/ holds store/), got: %s", msg)
	}
}

func TestFSList_OnAFileSaysToReadItInstead(t *testing.T) {
	r, root := newMissingPathRunner(t)
	seedWorkspace(t, root)

	_, err := r.FSList(context.Background(), tools.FSListRequest{Path: "main.go"})
	if err == nil {
		t.Fatal("listing a file must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "main.go") {
		t.Errorf("the error must name the path, got: %s", msg)
	}
	if !strings.Contains(msg, "read") {
		t.Errorf("the error must point at the tool that does work here, got: %s", msg)
	}
}

func TestFSRead_MissingFileNamesTheRelativePath(t *testing.T) {
	r, root := newMissingPathRunner(t)
	seedWorkspace(t, root)

	_, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "evalws/util.go"})
	if err == nil {
		t.Fatal("reading a file that does not exist must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "evalws/util.go") {
		t.Errorf("the error must name the path the model asked for, got: %s", msg)
	}
	if strings.Contains(msg, root) {
		t.Errorf("the error must not leak the absolute workspace root, got: %s", msg)
	}
	if !strings.Contains(msg, "main.go") {
		t.Errorf("the error must list what the nearest existing directory holds, got: %s", msg)
	}
}
