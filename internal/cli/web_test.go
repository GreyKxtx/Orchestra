package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	webui "github.com/orchestra/orchestra/ui/web"
)

func TestWebAssetsContainThePage(t *testing.T) {
	for _, name := range []string{"index.html", "web.bundle.js", "chat.css", "logo.png"} {
		f, err := webui.Assets().Open(name)
		if err != nil {
			t.Fatalf("%s is not embedded (run node ui/web/scripts/bundle-web.mjs and commit ui/web/static/): %v", name, err)
		}
		_ = f.Close()
	}
}

func TestWriteWebDiscovery_ContentsAndPermissions(t *testing.T) {
	root := t.TempDir()

	path, err := writeWebDiscovery(root, webDiscovery{
		ProtocolVersion: 15,
		WorkspaceRoot:   root,
		URL:             "http://127.0.0.1:4321",
		Port:            4321,
		Token:           "s3cret",
		PID:             os.Getpid(),
	})
	if err != nil {
		t.Fatalf("writeWebDiscovery: %v", err)
	}
	if got, want := path, filepath.Join(root, ".orchestra", "web.json"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got webDiscovery
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Token != "s3cret" || got.Port != 4321 || got.ProtocolVersion != 15 {
		t.Fatalf("discovery round trip lost fields: %+v", got)
	}

	// The token is in plaintext, so the file must not be world-readable.
	// Windows does not model these bits; skip the assertion, not the test.
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := st.Mode().Perm(); perm != 0600 {
			t.Fatalf("mode = %o, want 600 — the file holds a plaintext token", perm)
		}
	}
}

func TestCleanupStaleDiscovery_RemovesDeadPIDFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra", "web.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// An obviously dead high PID: the cleanup's primary branch removes a file
	// whose recorded process is gone.
	b, _ := json.Marshal(webDiscovery{PID: 0x7FFFFFF0, StartedAtUnix: 1})
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cleanupStaleDiscovery(path); err != nil {
		t.Fatalf("cleanupStaleDiscovery: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale discovery file survived cleanup (err=%v)", err)
	}
}

func TestCleanupStaleDiscovery_KeepsLivePIDFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra", "web.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// This test process is alive, so its discovery file must survive — deleting
	// it would strand a running server's clients with no way to find it.
	b, _ := json.Marshal(webDiscovery{PID: os.Getpid(), StartedAtUnix: 1})
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cleanupStaleDiscovery(path); err != nil {
		t.Fatalf("cleanupStaleDiscovery: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a live process's discovery file was removed: %v", err)
	}
}

func TestRestoreProjects_OpensRememberedPathsAndRecordsFailures(t *testing.T) {
	good := t.TempDir()
	cfg := config.DefaultConfig(good)
	if err := config.Save(filepath.Join(good, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "was-deleted")

	reg := projects.NewRegistry(core.Options{})
	t.Cleanup(reg.Shutdown)

	restoreProjects(context.Background(), reg, []string{good, bad})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d, want 2 — a path that fails to open must still be listed, not dropped silently", len(list))
	}
	var sawReady, sawError bool
	for _, p := range list {
		switch p.State {
		case projects.StateReady:
			sawReady = true
			if p.Path != good {
				t.Fatalf("ready project path = %q, want %q", p.Path, good)
			}
		case projects.StateError:
			sawError = true
			if p.Error == "" {
				t.Fatal("an errored project must carry the reason it failed")
			}
		}
	}
	if !sawReady || !sawError {
		t.Fatalf("want one ready and one error, got %+v", list)
	}
}
