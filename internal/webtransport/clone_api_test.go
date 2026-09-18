package webtransport

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// startCloneServer is startRegistryServer with a scripted clone. `clone` may be
// nil, which is a server built without the ability — what `orchestra web`
// produces when the host cannot run git.
func startCloneServer(
	t *testing.T,
	known *projects.Store,
	clone func(ctx context.Context, url, parent string) (string, error),
) string {
	t.Helper()
	// Same ordering reason as startRegistryServer: the RemoveAll for every temp
	// dir of this test must be registered before reg.Shutdown, because an open
	// core holds .orchestra/ckg.db and Windows will not delete a held file.
	_ = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	reg := projects.NewRegistry(core.Options{})
	t.Cleanup(reg.Shutdown)

	base, stop, err := Serve(ctx, Options{
		Token:        "secret",
		Health:       map[string]any{"status": "ok"},
		Registry:     reg,
		Known:        known,
		CloneProject: clone,
		InitProject: func(_ context.Context, root string) error {
			return config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root))
		},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base
}

// The start screen's "Clone from GitHub…" has to end where its "Open folder…"
// does: a project the rail can switch to and still knows about tomorrow. A
// fresh clone has no .orchestra.yml, so the initialise step in the middle is
// the part that decides whether it lands as a project or as an error.
func TestClone_LandsAsAnOpenAndRememberedProject(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "projects.json")
	known, err := projects.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	parent := t.TempDir()
	dest := filepath.Join(parent, "cloned")
	base := startCloneServer(t, known, func(_ context.Context, _, gotParent string) (string, error) {
		if gotParent != parent {
			t.Errorf("clone parent = %q, want %q", gotParent, parent)
		}
		// What git leaves behind: a directory with no Orchestra config in it.
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return "", err
		}
		return dest, nil
	})

	status, body := doJSON(t, http.MethodPost, base+"/api/projects/clone", map[string]any{
		"url":    "https://example.invalid/repo.git",
		"parent": parent,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %v)", status, body)
	}
	if body["state"] != "ready" {
		t.Fatalf("state = %v, want ready — an uninitialised clone must be set up, not refused", body["state"])
	}
	if _, err := os.Stat(filepath.Join(dest, ".orchestra.yml")); err != nil {
		t.Fatalf("the clone was never initialised: %v", err)
	}
	remembered := false
	for _, p := range known.Paths() {
		if filepath.Clean(p) == filepath.Clean(dest) {
			remembered = true
		}
	}
	if !remembered {
		t.Fatalf("the clone was not remembered: %v", known.Paths())
	}
}

// git's own words are the useful half of a clone failure — "repository not
// found", "authentication failed". Flattening them to a category leaves the
// user with nothing to act on.
func TestClone_FailureKeepsGitsOwnWords(t *testing.T) {
	parent := t.TempDir()
	base := startCloneServer(t, nil, func(context.Context, string, string) (string, error) {
		return "", errors.New("repository not found")
	})

	status, body := doJSON(t, http.MethodPost, base+"/api/projects/clone", map[string]any{
		"url":    "https://example.invalid/nope.git",
		"parent": parent,
	})
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %v)", status, body)
	}
	if body["error"] != "clone_failed" {
		t.Fatalf("error = %v, want clone_failed", body["error"])
	}
	if detail, _ := body["detail"].(string); detail != "repository not found" {
		t.Fatalf("detail = %q, want git's own message", detail)
	}
}

// A server built without the ability must say so, so the page can hide the
// button rather than offer one that fails.
func TestClone_DisabledSaysSo(t *testing.T) {
	base := startCloneServer(t, nil, nil)

	status, body := doJSON(t, http.MethodPost, base+"/api/projects/clone", map[string]any{
		"url":    "https://example.invalid/repo.git",
		"parent": t.TempDir(),
	})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %v)", status, body)
	}
	if body["error"] != "clone_not_enabled" {
		t.Fatalf("error = %v, want clone_not_enabled", body["error"])
	}
}

// Neither field may be empty: a clone with no parent would land wherever the
// process happens to be.
func TestClone_RejectsAnIncompleteRequest(t *testing.T) {
	called := false
	base := startCloneServer(t, nil, func(context.Context, string, string) (string, error) {
		called = true
		return "", nil
	})

	for _, body := range []map[string]any{
		{"url": "", "parent": "/tmp"},
		{"url": "https://example.invalid/repo.git", "parent": ""},
		{"url": "   ", "parent": "   "},
	} {
		status, got := doJSON(t, http.MethodPost, base+"/api/projects/clone", body)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d for %v, want 400 (body %v)", status, body, got)
		}
	}
	if called {
		t.Fatal("an incomplete request reached git")
	}
}
