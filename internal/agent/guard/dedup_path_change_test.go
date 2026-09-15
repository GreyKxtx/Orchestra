package guard

import "testing"

// A repeated call is refused because it would do nothing new. That stops
// being true once another call changed the same path in between. Seen with the
// 27B in the TUI: fs.delete tmp/patches.json, write tmp/patches.json, then
// fs.delete tmp/patches.json again — refused as a duplicate while the file sat
// on disk, and the model reached for `del /q` in bash to get it removed.
func TestDedup_ACallOnAPathChangedSinceIsNotADuplicate(t *testing.T) {
	cb := NewCircuitBreaker(0, 0, 0, 0)
	del := []byte(`{"path":"tmp/patches.json"}`)
	write := []byte(`{"path":"tmp/patches.json","content":"{\"patches\":[]}"}`)

	cb.RecordSuccessfulCall("fs.delete", del)
	if !cb.IsDuplicateCall("fs.delete", del) {
		t.Fatal("an immediate repeat of the delete must still be a duplicate")
	}
	cb.RecordSuccessfulCall("write", write)
	if cb.IsDuplicateCall("fs.delete", del) {
		t.Error("the file was written again after the delete; deleting it again is not a duplicate")
	}
	if !cb.IsDuplicateCall("write", write) {
		t.Error("the write itself, repeated with nothing in between, is still a duplicate")
	}

	// The same path spelled differently is the same file.
	cb.RecordSuccessfulCall("fs.delete", []byte(`{"path":"./tmp/patches.json"}`))
	if cb.IsDuplicateCall("write", write) {
		t.Error("a delete of ./tmp/patches.json changed tmp/patches.json; writing it again is not a duplicate")
	}

	// A change to another path leaves the record alone.
	other := NewCircuitBreaker(0, 0, 0, 0)
	other.RecordSuccessfulCall("fs.delete", del)
	other.RecordSuccessfulCall("write", []byte(`{"path":"notes.txt","content":"x"}`))
	if !other.IsDuplicateCall("fs.delete", del) {
		t.Error("a write to another file must not reopen a repeated delete")
	}

	// A rename changes both of its paths.
	ren := NewCircuitBreaker(0, 0, 0, 0)
	ren.RecordSuccessfulCall("write", []byte(`{"path":"b.txt","content":"x"}`))
	ren.RecordSuccessfulCall("fs.rename", []byte(`{"path":"a.txt","new_path":"b.txt"}`))
	if ren.IsDuplicateCall("write", []byte(`{"path":"b.txt","content":"x"}`)) {
		t.Error("a rename onto b.txt changed it; writing b.txt again is not a duplicate")
	}
}
