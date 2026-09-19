package guard

import "testing"

// A parallel batch is one step of the model's. Counting every failed call in
// it let one step trip the breaker: a Lead read nine files in parallel to see
// which existed yet, six were NotFound, and the run stopped for "repeatedly
// failing tool calls" (2026-09-18).
func TestRecordToolErrorBatch_CountsOneStep(t *testing.T) {
	var classified []string
	cb := NewCircuitBreaker(2, 6, 6, 3)
	cb.SetOnClassified(func(kind ErrorKind, meta RecordMeta) {
		classified = append(classified, meta.ToolName)
	})

	names := []string{"read", "read", "read", "read", "read", "read", "read"}
	if err := cb.RecordToolErrorBatch(names); err != nil {
		t.Fatalf("one batch of seven failures must not trip a limit of six: %v", err)
	}
	if cb.consecutiveToolErrs != 1 {
		t.Fatalf("a batch moves the counter once, got %d", cb.consecutiveToolErrs)
	}
	if len(classified) != len(names) {
		t.Fatalf("every failure is still classified for the log: %d of %d", len(classified), len(names))
	}

	// Six failing steps in a row still trip it on the seventh.
	for i := 0; i < 5; i++ {
		if err := cb.RecordToolErrorBatch([]string{"read"}); err != nil {
			t.Fatalf("step %d: %v", i+2, err)
		}
	}
	if err := cb.RecordToolErrorBatch([]string{"read"}); err == nil {
		t.Fatal("the seventh consecutive failing step must trip the breaker")
	}

	if cb.RecordToolErrorBatch(nil) != nil {
		t.Fatal("an empty batch is a no-op")
	}
}
