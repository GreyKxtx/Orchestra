package git

import (
	"context"
	"os/exec"
	"testing"
)

func TestGHPRCreate_EmptyTitle(t *testing.T) {
	c := NewClient(t.TempDir())
	_, err := c.GHPRCreate(context.Background(), GHPRCreateRequest{Title: ""})
	if err == nil {
		t.Error("expected error for empty title, got nil")
	}
}

func TestGHPRCreate_WhitespaceTitle(t *testing.T) {
	c := NewClient(t.TempDir())
	_, err := c.GHPRCreate(context.Background(), GHPRCreateRequest{Title: "   "})
	if err == nil {
		t.Error("expected error for whitespace title, got nil")
	}
}

func TestGHAvailable_DoesNotPanic(t *testing.T) {
	first := GHAvailable()
	second := GHAvailable()
	if first != second {
		t.Error("GHAvailable() must return consistent result (sync.Once)")
	}
}

func TestGHPRList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	c := NewClient(t.TempDir())
	_, err := c.GHPRList(context.Background(), GHPRListRequest{})
	if err == nil {
		t.Error("expected error when running outside a git repo, got nil")
	}
}

func TestGHIssueList_SkipNoGH(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not in PATH")
	}
	c := NewClient(t.TempDir())
	_, err := c.GHIssueList(context.Background(), GHIssueListRequest{})
	if err == nil {
		t.Error("expected error when running outside a git repo, got nil")
	}
}
