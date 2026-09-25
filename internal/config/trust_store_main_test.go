package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the workspace trust store at a temporary file: a test that
// saves a trusted config records it there, never in the developer's
// ~/.orchestra. The tests of this package exercise config layering, not
// trust, so the check is off unless a test turns it on (trust_test.go).
func TestMain(m *testing.M) {
	os.Setenv("ORCHESTRA_WORKSPACE_TRUST", "off")
	dir, err := os.MkdirTemp("", "orchestra-trust-")
	if err != nil {
		panic(err)
	}
	os.Setenv("ORCHESTRA_TRUST_STORE", filepath.Join(dir, "trusted-workspaces.json"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
