package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscovery_WriteRead(t *testing.T) {
	root := t.TempDir()

	info := DiscoveryInfo{
		ProtocolVersion: ProtocolVersion,
		ProjectRoot:     root,
		ProjectID:       "sha256:test",
		URL:             "http://127.0.0.1:8080",
		PID:             123,
		StartedAt:       1,
	}
	if err := WriteDiscovery(root, info); err != nil {
		t.Fatalf("WriteDiscovery failed: %v", err)
	}

	got, ok, err := ReadDiscovery(root)
	if err != nil {
		t.Fatalf("ReadDiscovery failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol_version: %d", got.ProtocolVersion)
	}
	if got.ProjectRoot != root {
		t.Fatalf("unexpected project_root: %q", got.ProjectRoot)
	}
	if got.ProjectID != "sha256:test" {
		t.Fatalf("unexpected project_id: %q", got.ProjectID)
	}
	if got.URL != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected url: %q", got.URL)
	}
	if got.PID != 123 {
		t.Fatalf("unexpected pid: %d", got.PID)
	}
}

func TestDiscovery_ReadMissing(t *testing.T) {
	root := t.TempDir()
	_, ok, err := ReadDiscovery(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestDiscovery_BrokenJSON(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, ".orchestra", "daemon.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(p, []byte("{\n  \"protocol_version\": 1,"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, ok, err := ReadDiscovery(root)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
}
