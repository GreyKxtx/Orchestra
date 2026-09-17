package toolpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A model that reads a file puts its content in the next prompt. So one
// `read .orchestra.env` hands every provider key to whatever model is
// running — including a free-tier one that trains on what it is sent.
//
// These two files are credentials end to end: the env file holds the keys,
// the local overlay holds whatever the user kept out of the shared config.
// No tool resolves a path to either, in any project.

func TestResolveWorkspacePath_RefusesTheCredentialFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".orchestra.env", ".orchestra.local.yml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("KEY=sk-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveWorkspacePath(root, name)
			if err == nil {
				t.Fatalf("%s resolved; a tool could read it", name)
			}
			if !strings.Contains(err.Error(), "credential") {
				t.Errorf("err = %q, want it to say why", err.Error())
			}
		})
	}
}

// Writing is refused for the same reason reading is: a tool that edits the
// env file destroys the keys, and one that appends to it can plant a value.
func TestResolveWorkspacePath_RefusesThemInSubdirectoriesToo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveWorkspacePath(root, "sub/.orchestra.env"); err == nil {
		t.Error("sub/.orchestra.env resolved")
	}
}

func TestResolveWorkspacePath_StillResolvesOrdinaryFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main.go", ".env.example", "orchestra.env.md", ".orchestra.yml"} {
		if _, rel, err := ResolveWorkspacePath(root, name); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		} else if rel != name {
			t.Errorf("%s resolved to %q", name, rel)
		}
	}
}
