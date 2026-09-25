package nav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

// Step-1 context answers from the graph as it is and refreshes behind the
// answer: a run no longer waits for a scan, and the next run sees the tree.
func TestFetchCKGContext_RefreshesInTheBackground(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "foo.go"), []byte("package foo\n\nfunc HelloWorld() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ckg.NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snap := CKGAccess{Store: store, Provider: ckg.NewProvider(store, tmp)}
	c := NewClient(tmp, nil, config.EmbedConfig{}, func() (CKGAccess, func()) { return snap, func() {} }, nil)

	ctx := context.Background()
	start := time.Now()
	first := c.FetchCKGContext(ctx, "hello world")
	if first != "" {
		t.Fatalf("an empty graph has no context to give; the refresh runs behind: %q", first)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the call waited for the scan: %s", time.Since(start))
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if out := c.FetchCKGContext(ctx, "hello world"); strings.Contains(out, "HelloWorld") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the background pass never indexed HelloWorld")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
