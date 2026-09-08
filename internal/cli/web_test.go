package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
