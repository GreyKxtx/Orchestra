package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the workspace trust store at a temporary file: a test that
// saves a trusted config records it there, never in the developer's
// ~/.orchestra. The home directory goes there too: the developer's own
// ~/.orchestra/skills made a test of a workspace with no skills fail.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "orchestra-trust-")
	if err != nil {
		panic(err)
	}
	os.Setenv("ORCHESTRA_TRUST_STORE", filepath.Join(dir, "trusted-workspaces.json"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
