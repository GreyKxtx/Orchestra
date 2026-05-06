package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/cache"
)

func TestScanOnce_RehashesOnlyWhenMetadataChanged(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "a.txt")
	content := []byte("hello\n")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	st, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	size := st.Size()
	mtime := st.ModTime().UnixNano()

	pid, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID failed: %v", err)
	}
	cfgHash, err := cache.ComputeConfigHash([]string{".git"}, true, DefaultMaxCacheFileBytes, ProtocolVersion)
	if err != nil {
		t.Fatalf("ComputeConfigHash failed: %v", err)
	}

	// Seed with same size but different mtime -> should force re-hash.
	seed := map[string]cache.FileRef{
		"a.txt": {Size: size, MTime: mtime - int64(time.Second), Hash: "sha256:bad"},
	}
	state, err := NewState(root, pid, cfgHash, []string{".git", ".orchestra"}, true, DefaultMaxCacheFileBytes, seed)
	if err != nil {
		t.Fatalf("NewState failed: %v", err)
	}

	if _, err := state.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce failed: %v", err)
	}

	f, ok := state.files["a.txt"]
	if !ok {
		t.Fatalf("expected a.txt in state")
	}
	expectedHash := cache.ComputeSHA256(content)
	if f.Hash != expectedHash {
		t.Fatalf("expected hash %q, got %q", expectedHash, f.Hash)
	}
}
