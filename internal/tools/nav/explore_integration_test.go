package nav

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

func TestExploreCodebase_RepeatedCalls(t *testing.T) {
	tmp := t.TempDir()
	src := "package foo\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(tmp, "foo.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmp, ".orchestra", "ckg.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := ckg.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := ckg.NewProvider(store, tmp)
	snap := CKGAccess{Store: store, Provider: provider}
	openCount := 0
	c := NewClient(tmp, nil, config.EmbedConfig{}, func() (CKGAccess, func()) {
		openCount++
		return snap, func() {}
	}, nil)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := c.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: "Hello"}); err != nil {
			t.Fatalf("ExploreCodebase #%d: %v", i, err)
		}
	}
	if openCount != 5 {
		t.Errorf("expected 5 snapshot calls, got %d", openCount)
	}
}
