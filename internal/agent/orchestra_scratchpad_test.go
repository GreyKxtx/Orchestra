package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/wsview"
)

// The runtime's bookkeeping must not switch the phase machine on. It used to
// create state.md from the frontmatter-less template when the first worker
// finished, and GuardSpawn — fail-closed on a state file it cannot parse —
// refused the second worker with "missing YAML frontmatter". Live on the 27B
// (orchestra_delegates_two_edits) the Lead then spent its remaining steps
// rewriting state.md by hand and never got the second edit done.
func TestWorkerSummary_DoesNotCreateAStateFileTheGuardCannotParse(t *testing.T) {
	root := t.TempDir()
	if err := appendWorkerSummaryToScratchpad(root, "worker verified_success path=width.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "state.md")); !os.IsNotExist(err) {
		t.Fatalf("a worker summary created state.md in a session that had none (stat err=%v)", err)
	}
	if err := orchestrastate.GuardSpawn(root, wsview.Disk(root), orchestrastate.EnforcementStrict, "worker"); err != nil {
		t.Fatalf("the second worker must still be allowed to spawn: %v", err)
	}
}

// When the Lead has opened a scratchpad, the Done line lands in it and the
// file still parses afterwards.
func TestWorkerSummary_AppendsToAnExistingStateFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "---\norchestra:\n    phase: maintenance\n---\n\n## Goal\nmove to 1080p\n\n## Done\n\n## Next\n"
	if err := os.WriteFile(filepath.Join(dir, "state.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendWorkerSummaryToScratchpad(root, "worker verified_success path=width.go"); err != nil {
		t.Fatal(err)
	}
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		t.Fatalf("state.md must still parse after the append: found=%v err=%v", found, err)
	}
	if !strings.Contains(st.Body, "- [x] worker verified_success path=width.go") {
		t.Fatalf("the Done line is missing:\n%s", st.Body)
	}
	if st.Phase != orchestrastate.PhaseMaintenance {
		t.Fatalf("the append changed the phase: %q", st.Phase)
	}
}

func TestCompactWorkerResultForLead(t *testing.T) {
	raw := `{"status":"verified_success","worker_result":{"status":"success","path":"internal/api/handler.go"},"verification":{"passed":true}}`
	got := CompactWorkerResultForLead(raw, 500)
	if got == "" || len(got) > 500 {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "handler.go") {
		t.Fatalf("expected compact summary: %q", got)
	}
}

func TestAppendScratchpadDoneLine(t *testing.T) {
	in := "## Goal\nfix auth\n\n## Done\n\n## Next\n"
	got := appendScratchpadDoneLine(in, "- [x] worker done")
	if !strings.Contains(got, "- [x] worker done") {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeWorkerResult(t *testing.T) {
	if !looksLikeWorkerResult(`{"status":"success","path":"a.go"}`) {
		t.Fatal("expected worker shape")
	}
	if looksLikeWorkerResult(`{"status":"done","result":"explore findings"}`) {
		t.Fatal("explore result should not match")
	}
}
