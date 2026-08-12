package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

func TestDeptScratchpadRelPath(t *testing.T) {
	cases := []struct {
		name string
		wo   *WorkOrder
		want string
	}{
		{"nil workorder", nil, ""},
		{"no context", &WorkOrder{Intent: "x"}, ""},
		{"valid", &WorkOrder{Context: map[string]any{"scratchpad": ".orchestra/depts/frontend@web.md"}}, ".orchestra/depts/frontend@web.md"},
		{"valid no instance", &WorkOrder{Context: map[string]any{"scratchpad": "./.orchestra/depts/backend.md"}}, ".orchestra/depts/backend.md"},
		{"outside depts", &WorkOrder{Context: map[string]any{"scratchpad": ".orchestra/state.md"}}, ""},
		{"traversal", &WorkOrder{Context: map[string]any{"scratchpad": ".orchestra/depts/../../evil.md"}}, ""},
		{"nested dir", &WorkOrder{Context: map[string]any{"scratchpad": ".orchestra/depts/a/b.md"}}, ""},
		{"bad extension", &WorkOrder{Context: map[string]any{"scratchpad": ".orchestra/depts/x.txt"}}, ""},
		{"non-string", &WorkOrder{Context: map[string]any{"scratchpad": 42}}, ""},
	}
	for _, tc := range cases {
		if got := deptScratchpadRelPath(tc.wo); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAppendDeptScratchpadDone_CreatesAndAppends(t *testing.T) {
	root := t.TempDir()
	rel := ".orchestra/depts/frontend@web.md"

	if err := appendDeptScratchpadDone(root, rel, "wo1: worker done path=a.go"); err != nil {
		t.Fatalf("append (create): %v", err)
	}
	if err := appendDeptScratchpadDone(root, rel, "wo2: worker done path=b.go"); err != nil {
		t.Fatalf("append (existing): %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "# Dept scratchpad — frontend@web") {
		t.Fatalf("missing header:\n%s", got)
	}
	i1 := strings.Index(got, "- [x] wo1: worker done path=a.go")
	i2 := strings.Index(got, "- [x] wo2: worker done path=b.go")
	if i1 < 0 || i2 < 0 || i2 < i1 {
		t.Fatalf("expected both lines in order under ## Done:\n%s", got)
	}
}

// Worker completion must land in the dept scratchpad referenced by the
// WorkOrder context (spec §5.8) with no LLM involvement.
func TestWorker_RecordsResultToDeptScratchpad(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(&mockTaskResultLLM{result: "patched ok"}, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	wo := `{"task_id":"fe-1","intent":"edit widget","target_files":["web/widget.tsx"],` +
		`"context":{"scratchpad":".orchestra/depts/frontend@web.md"}}`
	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal: wo, SubagentType: "worker", MaxSteps: 2, TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := r.Wait(context.Background(), id, 30_000); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, ".orchestra", "depts", "frontend@web.md"))
	if err != nil {
		t.Fatalf("dept scratchpad not written: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "fe-1:") {
		t.Fatalf("expected task_id prefix in dept scratchpad:\n%s", got)
	}
}
