package tools

import (
	"context"
	"os/exec"
	"testing"
)

func TestGHPRCreate_EmptyTitle(t *testing.T) {
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHPRCreate(context.Background(), GHPRCreateRequest{Title: ""})
	if err == nil {
		t.Error("expected error for empty title, got nil")
	}
}

func TestGHPRCreate_WhitespaceTitle(t *testing.T) {
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHPRCreate(context.Background(), GHPRCreateRequest{Title: "   "})
	if err == nil {
		t.Error("expected error for whitespace title, got nil")
	}
}

func TestGHAvailable_DoesNotPanic(t *testing.T) {
	// Just verify the function runs and returns a consistent result.
	first := ghAvailable()
	second := ghAvailable()
	if first != second {
		t.Error("ghAvailable() must return consistent result (sync.Once)")
	}
}

// Integration tests — skipped when gh is not installed.

func TestGHPRList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHPRList(context.Background(), GHPRListRequest{})
	if err == nil {
		t.Error("expected error when running outside a git repo, got nil")
	}
}

func TestGHIssueList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	r := &Runner{workspaceRoot: t.TempDir()}
	_, err := r.GHIssueList(context.Background(), GHIssueListRequest{})
	if err == nil {
		t.Error("expected error when running outside a git repo, got nil")
	}
}
