package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initProject must work on a directory that is NOT the process's cwd — that is
// the whole point of the extraction, since the server initialises whichever path
// the user picked.
func TestInitProject_InitialisesAnArbitraryDirectory(t *testing.T) {
	root := t.TempDir()

	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("initProject: %v", err)
	}

	for _, rel := range []string{".orchestra.yml", ".gitignore", "ORCHESTRA.md"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("%s missing after init: %v", rel, err)
		}
	}

	// The cwd must be untouched: a server initialising a project must not
	// depend on, or change, where the process happens to be.
	cwd, _ := os.Getwd()
	if _, err := os.Stat(filepath.Join(cwd, ".orchestra.yml")); err == nil {
		t.Fatal("init wrote into the process cwd instead of the given root")
	}
}

func TestInitProject_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("first init: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("second init: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("re-running init rewrote an existing .orchestra.yml — it must be left untouched")
	}
}
