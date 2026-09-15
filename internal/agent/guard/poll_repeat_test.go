package guard

import "testing"

// bash.output is a poll: the same {"bg_id":"bg_1"} returns new output as a job
// runs. The duplicate guard refused the second identical poll, and the eval's
// background-job task died on it every run. A poll is never a duplicate and
// takes no repeat budget — a build polled every ten seconds for two minutes is
// a dozen identical calls. bash.output waits server-side, so this is not a
// hot loop, and the job's own timeout and max_steps still bound it.
func TestPolls_AreNeverDuplicatesOrRepeats(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	poll := []byte(`{"bg_id":"bg_1"}`)
	for i := 0; i < 20; i++ {
		if cb.IsDuplicateCall("bash.output", poll) {
			t.Fatalf("poll %d refused as a duplicate", i+1)
		}
		if cb.IsReadOnlyBlocked("bash.output", poll) {
			t.Fatalf("poll %d refused as a repeat", i+1)
		}
		if hint := cb.RecordReadOnlyCall("bash.output", poll); hint != "" {
			t.Fatalf("poll %d told the model it already has the result: %s", i+1, hint)
		}
		_ = cb.RecordSuccessfulCall("bash.output", poll)
	}
}
