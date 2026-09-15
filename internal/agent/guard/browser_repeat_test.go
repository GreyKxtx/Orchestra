package guard

import "testing"

// Browser calls are not duplicates on sight, but a model stuck on one still
// stops: the repeat budget applies, and only a page action resets page reads.
func TestBrowserRepeats_ActionsRefreshPageReadsAndLoopsStillStop(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	snap, click := []byte(`{}`), []byte(`{"ref":"e9"}`)

	if cb.IsDuplicateCall("browser.snapshot", snap) {
		t.Fatal("a browser call is treated as a duplicate on sight")
	}
	_, block := cb.readOnlyLimits()
	for i := 0; i < block; i++ {
		cb.RecordReadOnlyCall("browser.snapshot", snap)
	}
	if !cb.IsReadOnlyBlocked("browser.snapshot", snap) {
		t.Error("snapshots repeated with nothing done to the page never stop")
	}
	cb.RecordReadOnlyCall("browser.click", click)
	if cb.IsReadOnlyBlocked("browser.snapshot", snap) {
		t.Error("after a click the page is new, but looking at it is still blocked")
	}

	for i := 0; i < block; i++ {
		cb.RecordReadOnlyCall("browser.click", click)
	}
	if !cb.IsReadOnlyBlocked("browser.click", click) {
		t.Error("the same click repeated without end is never stopped")
	}
}
