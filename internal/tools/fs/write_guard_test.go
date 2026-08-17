package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/protocol"
)

func TestFSEdit_ExternalChange_StaleContentPreservesBak(t *testing.T) {
	r, root := newEditRunner(t)
	original := "hello world\n"
	writeTestFile(t, root, "handler.go", original)
	h1 := cache.ComputeSHA256([]byte(original))

	bakPath := filepath.Join(root, "handler.go.orchestra.bak")
	bakBody := []byte("prior-backup-must-survive\n")
	if err := os.WriteFile(bakPath, bakBody, 0o644); err != nil {
		t.Fatalf("seed bak: %v", err)
	}

	changed := "hello world\nchanged-by-external-process\n"
	writeTestFile(t, root, "handler.go", changed)

	_, err := r.FSEdit(context.Background(), tools.FSEditRequest{
		Path:     "handler.go",
		Search:   "hello world",
		Replace:  "patched",
		FileHash: h1,
		Backup:   true,
	})
	if err == nil {
		t.Fatal("expected StaleContent after external modification")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.StaleContent {
		if !strings.Contains(err.Error(), "StaleContent") && !strings.Contains(err.Error(), "hash") {
			t.Fatalf("want StaleContent, got %v", err)
		}
	}

	got := readTestFile(t, root, "handler.go")
	if got != changed {
		t.Fatalf("edit must not apply; disk=%q", got)
	}
	bak, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("bak missing: %v", err)
	}
	if string(bak) != string(bakBody) {
		t.Fatalf("bak corrupted: %q", bak)
	}
}

func TestFSWrite_ExternalChange_StaleContentPreservesBak(t *testing.T) {
	r, root := newWriteRunner(t)
	original := "version-one\n"
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := cache.ComputeSHA256([]byte(original))

	bakPath := filepath.Join(root, "service.go.orchestra.bak")
	bakBody := []byte("write-bak-seed\n")
	if err := os.WriteFile(bakPath, bakBody, 0o644); err != nil {
		t.Fatal(err)
	}

	changed := "version-two-external\n"
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:     "service.go",
		Content:  "worker-overwrite\n",
		FileHash: h1,
		Backup:   true,
	})
	if err == nil {
		t.Fatal("expected StaleContent after external modification")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.StaleContent {
		if !strings.Contains(err.Error(), "StaleContent") && !strings.Contains(err.Error(), "hash") {
			t.Fatalf("want StaleContent, got %v", err)
		}
	}

	got, err := os.ReadFile(filepath.Join(root, "service.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != changed {
		t.Fatalf("write must not apply; disk=%q", got)
	}
	bak, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != string(bakBody) {
		t.Fatalf("bak corrupted: %q", bak)
	}
}
