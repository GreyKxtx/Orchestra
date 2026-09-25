package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the workspace trust store at a temporary file: a test that
// saves a trusted config records it there, never in the developer's
// ~/.orchestra.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "orchestra-trust-")
	if err != nil {
		panic(err)
	}
	os.Setenv("ORCHESTRA_TRUST_STORE", filepath.Join(dir, "trusted-workspaces.json"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
