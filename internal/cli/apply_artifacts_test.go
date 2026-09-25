package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plan.json is what --from-plan replays; a write that fails must reach the
// user instead of leaving "Plan saved to:" pointing at a stale file. The
// diff, the result and the run log are for reading and only warn.
func TestWriteApplyArtifacts_ReportsAnUnwritablePlan(t *testing.T) {
	root := t.TempDir()
	// A directory where plan.json should be: neither the rename nor the
	// in-place fallback can replace it.
	if err := os.MkdirAll(filepath.Join(root, ".orchestra", "plan.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err := writeApplyArtifacts(root, planArtifact{Query: "q"}, nil, true, now, now, "direct", 1, nil)
	if err == nil || !strings.Contains(err.Error(), "plan.json") {
		t.Fatalf("err = %v, want the plan.json write failure", err)
	}
}

func TestWriteApplyArtifacts_WritesThePlan(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	if err := writeApplyArtifacts(root, planArtifact{Query: "q"}, nil, true, now, now, "direct", 1, nil); err != nil {
		t.Fatalf("writeApplyArtifacts: %v", err)
	}
	for _, name := range []string{"plan.json", "diff.txt", "last_result.json", "last_run.jsonl"} {
		if _, err := os.Stat(filepath.Join(root, ".orchestra", name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
