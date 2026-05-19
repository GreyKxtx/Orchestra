package agent

import "testing"

// TestCircuitBreaker_ParallelBatchDeniedTrips verifies that the bookkeeping
// pattern runParallelToolBatch uses after wg.Wait — record N denials in a
// loop and abort on the first cbErr — actually trips when the same tool is
// denied past MaxDeniedToolRepeats. C2 regression: previously this loop was
// not run at all in the parallel path.
func TestCircuitBreaker_ParallelBatchDeniedTrips(t *testing.T) {
	cb := NewCircuitBreaker(2 /*maxDenied*/, 6, 6, 3)

	// Simulate a 16-call parallel batch where every call is denied by the
	// pre-tool hook. The first 2 increments are within the cap (>maxDenied=2
	// trips at the 3rd record).
	if err := cb.RecordDenied("bash"); err != nil {
		t.Fatalf("1st denial should not trip, got %v", err)
	}
	if err := cb.RecordDenied("bash"); err != nil {
		t.Fatalf("2nd denial should not trip, got %v", err)
	}
	if err := cb.RecordDenied("bash"); err == nil {
		t.Fatal("3rd denial must trip (>maxDenied)")
	}
}

// TestCircuitBreaker_ParallelBatchErrorsTrip — same shape for tool errors.
func TestCircuitBreaker_ParallelBatchErrorsTrip(t *testing.T) {
	cb := NewCircuitBreaker(2, 3 /*maxToolErr*/, 6, 3)

	for i := 0; i < 3; i++ {
		if err := cb.RecordToolError("read"); err != nil {
			t.Fatalf("error %d should not trip, got %v", i+1, err)
		}
	}
	if err := cb.RecordToolError("read"); err == nil {
		t.Fatal("4th error must trip (>maxToolErr=3)")
	}
}

// TestCircuitBreaker_BatchMixedSuccessResetsErrors — successful calls in a
// batch should reset the consecutive-error counter (the parallel path calls
// ResetToolErrors when anyErr==false).
func TestCircuitBreaker_BatchMixedSuccessResetsErrors(t *testing.T) {
	cb := NewCircuitBreaker(2, 3, 6, 3)

	_ = cb.RecordToolError("read")
	_ = cb.RecordToolError("read")
	cb.ResetToolErrors()

	// After reset, three more errors must NOT trip (counter back to 0).
	for i := 0; i < 3; i++ {
		if err := cb.RecordToolError("read"); err != nil {
			t.Fatalf("after reset, error %d should not trip, got %v", i+1, err)
		}
	}
}

// TestCircuitBreaker_ResetDeniedForTool — N3 in audit ledger (Sprint 6).
// A successful call must reset that tool's denial counter so a permission
// rule change (skill_invoke --allow-exec, runtime config reload) doesn't
// inherit an old denial streak.
func TestCircuitBreaker_ResetDeniedForTool(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)

	if err := cb.RecordDenied("exec.run"); err != nil {
		t.Fatalf("1st denial trip: %v", err)
	}
	if err := cb.RecordDenied("exec.run"); err != nil {
		t.Fatalf("2nd denial trip: %v", err)
	}
	// Simulate the rule being lifted and a successful call:
	cb.ResetDeniedForTool("exec.run")

	// Now we should have a fresh budget of 2 more denials before tripping.
	if err := cb.RecordDenied("exec.run"); err != nil {
		t.Fatalf("after reset, 1st denial trip: %v", err)
	}
	if err := cb.RecordDenied("exec.run"); err != nil {
		t.Fatalf("after reset, 2nd denial trip: %v", err)
	}
	if err := cb.RecordDenied("exec.run"); err == nil {
		t.Fatal("after reset, 3rd denial must trip")
	}

	// Reset must be tool-scoped: another tool's denials aren't cleared.
	cb2 := NewCircuitBreaker(2, 6, 6, 3)
	_ = cb2.RecordDenied("exec.run")
	_ = cb2.RecordDenied("bash")
	cb2.ResetDeniedForTool("exec.run")
	if err := cb2.RecordDenied("bash"); err != nil {
		t.Fatalf("bash 2nd denial after exec reset shouldn't trip: %v", err)
	}
	if err := cb2.RecordDenied("bash"); err == nil {
		t.Fatal("bash 3rd denial must trip (exec.run reset is scoped)")
	}
}
