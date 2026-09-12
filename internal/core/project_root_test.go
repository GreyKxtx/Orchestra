package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
)

// The tools' root, and the project id, must follow the config file, not the
// process. `orchestra init` writes project_root: . — meaning the folder the
// file is in — and a core opened by `orchestra web` or the desktop shell for
// that folder used to resolve "." against the server process's working
// directory: its file tools, its CKG database and its memory notes all went
// into whatever directory the server had been started from.
func TestNew_ToolsRootFollowsTheConfigFileNotTheProcessCWD(t *testing.T) {
	root := t.TempDir()
	body := "project_root: .\nllm:\n  api_base: http://127.0.0.1:1/v1\n  model: m\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if got := c.tools.WorkspaceRoot(); !samePath(got, root) {
		t.Fatalf("tools root = %q, want the project %q", got, root)
	}
	want, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.projectID != want {
		t.Fatalf("project id was computed from %q, not from the project: %s != %s", c.cfg.ProjectRoot, c.projectID, want)
	}
	if !samePath(c.cfg.ProjectRoot, root) {
		t.Fatalf("cfg.ProjectRoot = %q, want %q", c.cfg.ProjectRoot, root)
	}
}
