package orchestrastate

import (
	"fmt"
	"sync"
	"testing"
)

const updateSeed = "---\norchestra:\n  phase: execution\n---\n## Goal\nship it\n"

// Parallel workers each add doc debt; none is lost (ORC-4). AddDocDebt was a
// Load and a Save with nothing between them, so two at once kept one.
func TestUpdate_ConcurrentWritersLoseNothing(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, updateSeed)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := AddDocDebt(root, fmt.Sprintf("docs/page-%02d.md", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	st, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.DocDebt) != 20 {
		t.Fatalf("want 20 doc debt entries, got %d: %v", len(st.DocDebt), st.DocDebt)
	}
}

// The Question Barrier's shape: read, wait on the user, write. A change made
// while it waited survives, because the write is an Update of the state as it
// is then, not the copy read before asking.
func TestUpdate_AStaleCopyDoesNotWriteBack(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, updateSeed)
	before, _, _ := Load(root) // the barrier reads, then asks the user…
	if err := AddDocDebt(root, "docs/api.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(root, func(st *State) error { // …and counts the round
		st.ClarificationRounds = before.ClarificationRounds + 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, _, _ := Load(root)
	if st.ClarificationRounds != 1 || len(st.DocDebt) != 1 {
		t.Fatalf("the round is counted and the doc debt written meanwhile survives: %+v", st)
	}
}

func TestUpdate_NoStateNoWrite(t *testing.T) {
	root := t.TempDir()
	ran := false
	found, err := Update(root, func(*State) error { ran = true; return nil })
	if err != nil || found || ran {
		t.Fatalf("no state file: fn must not run (found=%v ran=%v err=%v)", found, ran, err)
	}
	if _, found, _ := Load(root); found {
		t.Fatal("Update must not create a state file")
	}
}
