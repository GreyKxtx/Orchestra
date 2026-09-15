package browser

import (
	"path/filepath"
	"slices"
	"testing"
)

// The server writes a page snapshot after every action, console logs and
// screenshots, into a directory it resolves against its working directory.
// Started with the parent's working directory, a run put .playwright-mcp/
// wherever orchestra happened to be launched from — the project root on the
// CLI, anywhere at all under an IDE — outside the staging overlay and outside
// the gitignored .orchestra/. Seen live against @playwright/mcp.
func TestClient_KeepsTheServersFilesUnderTheProjectsOrchestraDir(t *testing.T) {
	root := t.TempDir()
	cmd := New(Config{Headless: true, WorkDir: root}).makeCmd()

	if cmd.Dir != root {
		t.Errorf("server starts in %q, want the project root %q", cmd.Dir, root)
	}
	i := slices.Index(cmd.Args, "--output-dir")
	if i < 0 || i+1 >= len(cmd.Args) {
		t.Fatalf("no --output-dir in %v", cmd.Args)
	}
	if want := filepath.Join(root, ".orchestra", "browser"); cmd.Args[i+1] != want {
		t.Errorf("--output-dir %q, want %q", cmd.Args[i+1], want)
	}
	if !slices.Contains(cmd.Args, PlaywrightMCPPackage) {
		t.Errorf("the pinned package is not what is started: %v", cmd.Args)
	}
}
