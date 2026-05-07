package ckg

import (
	"context"
	"testing"
)

func TestStore_DB_NotNil(t *testing.T) {
	s := newTestStore(t)
	if s.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

func TestStore_GetFileHash_Missing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	hash, err := s.GetFileHash(ctx, "nonexistent.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "" {
		t.Fatalf("expected empty hash for missing file, got %q", hash)
	}
}

func TestStore_GetFileHash_Present(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{{FQN: "pkg.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 3}}
	if err := s.SaveFileNodes(ctx, "foo.go", "sha256:abc", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	hash, err := s.GetFileHash(ctx, "foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "sha256:abc" {
		t.Fatalf("expected sha256:abc, got %q", hash)
	}
}

func TestStore_DeleteFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{{FQN: "pkg.Bar", ShortName: "Bar", Kind: "func", LineStart: 1, LineEnd: 2}}
	if err := s.SaveFileNodes(ctx, "bar.go", "sha256:def", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	// Verify file exists.
	hash, err := s.GetFileHash(ctx, "bar.go")
	if err != nil || hash == "" {
		t.Fatalf("file should exist before deletion, hash=%q err=%v", hash, err)
	}

	// Delete.
	if err := s.DeleteFile(ctx, "bar.go"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	// Verify gone.
	hash, err = s.GetFileHash(ctx, "bar.go")
	if err != nil {
		t.Fatalf("unexpected error after delete: %v", err)
	}
	if hash != "" {
		t.Fatalf("expected empty hash after delete, got %q", hash)
	}
}

func TestStore_DeleteFile_Nonexistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Deleting a non-existent path should not error.
	if err := s.DeleteFile(ctx, "ghost.go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
