package state

import "testing"

func TestDiffReview_ShowResetsCursor(t *testing.T) {
	var r DiffReview
	r.Move(0, 1) // no-op
	r.Arm(nil)
	r.Show()
	if !r.Shown() || r.Cursor() != 0 {
		t.Fatalf("shown=%v cursor=%d", r.Shown(), r.Cursor())
	}
	r.Hide()
	if r.Shown() {
		t.Fatal("hide must clear shown")
	}
}

func TestDiffReview_MoveWithinBounds(t *testing.T) {
	var r DiffReview
	if r.Move(-1, 3) {
		t.Fatal("cursor must not go below 0")
	}
	if !r.Move(+1, 3) || r.Cursor() != 1 {
		t.Fatalf("cursor=%d, want 1", r.Cursor())
	}
	if !r.Move(+1, 3) || r.Cursor() != 2 {
		t.Fatalf("cursor=%d, want 2", r.Cursor())
	}
	if r.Move(+1, 3) {
		t.Fatal("cursor must not exceed fileCount-1")
	}
}

func TestDiffReview_ClampAfterShrink(t *testing.T) {
	var r DiffReview
	r.Move(+1, 5)
	r.Move(+1, 5)
	r.Move(+1, 5)
	r.Move(+1, 5) // cursor = 4
	if got := r.Clamp(2); got != 1 {
		t.Fatalf("clamp=%d, want 1", got)
	}
}

func TestDiffReview_PendingLifecycle(t *testing.T) {
	var r DiffReview
	if r.PendingReview() || r.HasPendingOps() {
		t.Fatal("zero value must be idle")
	}

	// Diff-only pending: review mode without ops (nothing applicable).
	r.Arm(nil)
	if !r.PendingReview() || r.HasPendingOps() {
		t.Fatalf("pending=%v hasOps=%v", r.PendingReview(), r.HasPendingOps())
	}

	ops := []map[string]any{{"op": "file.write_atomic", "path": "a.go"}}
	r.Arm(ops)
	if !r.HasPendingOps() || len(r.PendingOps()) != 1 {
		t.Fatalf("hasOps=%v ops=%d", r.HasPendingOps(), len(r.PendingOps()))
	}
	// Arm copies the slice header: appending to the caller slice must not
	// grow PendingOps.
	ops = append(ops, map[string]any{"op": "extra"})
	_ = ops
	if len(r.PendingOps()) != 1 {
		t.Fatalf("caller append leaked into PendingOps: %d", len(r.PendingOps()))
	}

	r.ClearPending()
	if r.PendingReview() || len(r.PendingOps()) != 0 {
		t.Fatal("clear must leave review mode")
	}
}

func TestDiffReview_ResetDropsEverything(t *testing.T) {
	var r DiffReview
	r.Arm([]map[string]any{{"op": "x"}})
	r.Show()
	r.Move(+1, 3)
	r.Reset()
	if r.Shown() || r.Cursor() != 0 || r.PendingReview() || len(r.PendingOps()) != 0 {
		t.Fatalf("reset incomplete: %+v", r)
	}
}
