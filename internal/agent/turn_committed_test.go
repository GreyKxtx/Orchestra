package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/protocol/schema"
)

// With Apply each edit is committed the moment it lands, so by the final the
// overlay is empty. The result still has to name what the turn wrote: the CLI
// printed "Changed files: (none)" after a turn that had changed files.
func TestRun_ApplyReportsFilesCommittedDuringTheTurn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(mathBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module testpkg\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	client := &recordingLLM{steps: []string{
		readStep,
		editStep,
		`{"type":"tool_call","tool":{"name":"write","input":{"path":"notes.txt","content":"one\n"}}}`,
		`{"type":"final","final":{"patches":[]}}`,
	}}
	ag, err := New(client, v, tr, Options{MaxSteps: 8, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	// The core's turn: everything staged, committed as it lands (Apply).
	turn := tr.NewTurn(tools.TurnOptions{DryRun: true, Apply: true})
	defer turn.Close()
	_, res, err := ag.Run(tools.WithTurn(context.Background(), turn), nil, "Add a Multiply function to math.go and write notes.txt.")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil || res.ApplyResponse == nil {
		t.Fatalf("result carries no apply response:\n%s", recordedToolMessages(client))
	}
	got := map[string]bool{}
	for _, p := range res.ApplyResponse.ChangedFiles {
		got[p] = true
	}
	if !got["math.go"] || !got["notes.txt"] || len(got) != 2 {
		t.Fatalf("ChangedFiles = %v, want math.go and notes.txt", res.ApplyResponse.ChangedFiles)
	}
	if !res.Applied || !res.ApplyResponse.Applied {
		t.Fatalf("Applied = %v / %v, want true", res.Applied, res.ApplyResponse.Applied)
	}
}

func TestMergeApplyResponses_KeepsFirstBeforeAndLastAfter(t *testing.T) {
	a := &tools.FSApplyOpsResponse{Applied: true, ChangedFiles: []string{"a.go"},
		Diffs: []applier.FileDiff{{Path: "a.go", Before: "v0", After: "v1"}}}
	b := &tools.FSApplyOpsResponse{Applied: true, ChangedFiles: []string{"a.go", "b.go"},
		Diffs: []applier.FileDiff{{Path: "a.go", Before: "v1", After: "v2"}, {Path: "b.go", Before: "", After: "x"}}}
	m := mergeApplyResponses(a, b)
	if len(m.ChangedFiles) != 2 || m.ChangedFiles[0] != "a.go" || m.ChangedFiles[1] != "b.go" {
		t.Fatalf("ChangedFiles = %v", m.ChangedFiles)
	}
	if len(m.Diffs) != 2 || m.Diffs[0].Before != "v0" || m.Diffs[0].After != "v2" {
		t.Fatalf("Diffs = %+v", m.Diffs)
	}
	if mergeApplyResponses(nil, b) != b || mergeApplyResponses(a, nil) != a {
		t.Fatal("a nil side must return the other unchanged")
	}
}
