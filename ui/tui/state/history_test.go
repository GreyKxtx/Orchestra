package state_test

import "testing"
import "github.com/orchestra/orchestra/ui/tui/state"

func TestHistory_PushAndUp(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("first")
	h.Push("second")

	got := h.Up("draft")
	if got != "second" {
		t.Fatalf("want 'second', got %q", got)
	}
	got = h.Up("second")
	if got != "first" {
		t.Fatalf("want 'first', got %q", got)
	}
}

func TestHistory_DownRestoresDraft(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("hello")
	h.Up("draft") // navigate to "hello", saving "draft"
	got := h.Down()
	if got != "draft" {
		t.Fatalf("want draft restored 'draft', got %q", got)
	}
}

func TestHistory_UpAtOldestReturnsOldest(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("only")
	h.Up("")
	got := h.Up("") // already at oldest
	if got != "only" {
		t.Fatalf("want 'only', got %q", got)
	}
}

func TestHistory_DownAtNewestReturnsDraft(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("x")
	h.Up("")
	got := h.Down()
	if got != "" {
		t.Fatalf("want empty draft, got %q", got)
	}
}

func TestHistory_NoDuplicateConsecutive(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("same")
	h.Push("same")
	h.Up("")
	got := h.Up("same")
	if got != "same" {
		t.Fatalf("want 'same', got %q", got)
	}
	// only one entry exists, so Up again should still return "same"
	got = h.Up("same")
	if got != "same" {
		t.Fatalf("should clamp at oldest, got %q", got)
	}
}

func TestHistory_MaxSize(t *testing.T) {
	h := state.NewInputHistory(3)
	h.Push("a")
	h.Push("b")
	h.Push("c")
	h.Push("d") // pushes out "a"

	got := h.Up("")
	if got != "d" {
		t.Fatalf("want 'd', got %q", got)
	}
	h.Up("d")
	h.Up("c")
	oldest := h.Up("b")
	if oldest != "b" {
		t.Fatalf("want 'b' as oldest (a was evicted), got %q", oldest)
	}
}

func TestHistory_DownWithoutUpReturnsCurrent(t *testing.T) {
	h := state.NewInputHistory(10)
	h.Push("x")
	got := h.Down()
	if got != "" {
		t.Fatalf("Down without Up should return empty string, got %q", got)
	}
}
