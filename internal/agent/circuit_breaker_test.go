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
