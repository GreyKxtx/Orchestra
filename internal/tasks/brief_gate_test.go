package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/wsview"
)

func writeFileT(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func deptWorkOrder(instance string) *WorkOrder {
	return &WorkOrder{
		Intent:  "do it",
		Context: map[string]any{"scratchpad": ".orchestra/depts/" + instance + ".md"},
	}
}

func TestWorkOrderDeptInstance(t *testing.T) {
	cases := map[string]string{
		".orchestra/depts/frontend.md":       "frontend",
		"./.orchestra/depts/frontend@web.md": "frontend@web",
		".orchestra/state.md":                "",
		".orchestra/depts/a/b.md":            "",
		"":                                   "",
	}
	for sp, want := range cases {
		wo := &WorkOrder{Context: map[string]any{"scratchpad": sp}}
		if got := workOrderDeptInstance(wo); got != want {
			t.Errorf("scratchpad %q → %q, want %q", sp, got, want)
		}
	}
	if workOrderDeptInstance(&WorkOrder{}) != "" {
		t.Error("nil context → empty instance")
	}
}

func TestCheckBriefCompleteness(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n")

	// No playbook — gate inactive.
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err != nil {
		t.Fatalf("no playbook → inactive: %v", err)
	}

	// Playbook opts in, no brief — blocked.
	writeFileT(t, root, ".orchestra/playbooks/frontend.md",
		"---\nbrief_required_fields:\n  - routes\n  - api_contract_ref\n---\n\n## Rules\nx\n")
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err == nil {
		t.Fatal("missing brief must block worker spawn")
	}

	// Brief with a missing required section — blocked, names the section.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home, /cart\n\n## Something else\nx\n")
	err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend"))
	if err == nil {
		t.Fatal("incomplete brief must block")
	}

	// Complete brief (loose heading match) — green.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home, /cart\n\n## API contract ref\nOpenAPI.v0.yaml sha256:abc\n")
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err != nil {
		t.Fatalf("complete brief must pass: %v", err)
	}

	// Empty section body does not count.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home\n\n## API contract ref\n\n## Next\nx\n")
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err == nil {
		t.Fatal("empty required section must block")
	}

	// Instance falls back to dept-type playbook and brief.
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend@web")); err == nil {
		t.Fatal("instance inherits dept-type gate")
	}

	// Waiver unblocks.
	writeFileT(t, root, ".orchestra/state.md",
		"---\norchestra:\n  phase: execution\n  prd_status: approved\n  waivers: [brief_completeness]\n---\n")
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err != nil {
		t.Fatalf("waiver must unblock: %v", err)
	}

	// Maintenance bypasses.
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: maintenance\n  prd_status: approved\n---\n")
	if err := checkBriefCompleteness(root, wsview.Disk(root), deptWorkOrder("frontend")); err != nil {
		t.Fatalf("maintenance must bypass: %v", err)
	}

	// Non-dept-bound WorkOrder — inactive.
	if err := checkBriefCompleteness(root, wsview.Disk(root), &WorkOrder{Intent: "x"}); err != nil {
		t.Fatalf("unbound WorkOrder exempt: %v", err)
	}
}

// A Lead's brief is staged in the turn: the relay that spawns its workers
// comes in the same turn, before anything reaches disk. The gate read the
// disk, found no brief and refused every worker, so a department that opted
// into briefs could never start (ORC-6). It reads the spawner's view now.
func TestBriefGate_ReadsTheBriefTheLeadStaged(t *testing.T) {
	r, root := newAgencyRunner(t, &scriptedEdit{}, ChildAgentConfig{})
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n")
	stage := func(rel, content string) {
		t.Helper()
		if _, err := r.toolRunner.FSWrite(context.Background(), tools.FSWriteRequest{Path: rel, Content: content}); err != nil {
			t.Fatalf("stage %s: %v", rel, err)
		}
	}
	stage(".orchestra/playbooks/frontend.md", "---\nbrief_required_fields:\n  - routes\n---\n\n## Rules\nx\n")
	spawn := func() error {
		_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
			Goal:         `{"task_id":"wo-1","intent":"routes","target_files":["shared.go"],"context":{"scratchpad":".orchestra/depts/frontend.md"}}`,
			SubagentType: "worker", MaxSteps: 4, TimeoutMS: 30_000,
		})
		return err
	}
	if err := spawn(); err == nil || !strings.Contains(err.Error(), "brief_completeness") {
		t.Fatalf("a staged playbook that opts in is enforced: %v", err)
	}
	stage(".orchestra/specs/frontend/brief.md", "# Brief\n\n## Routes\n/home, /settings\n")
	if err := spawn(); err != nil {
		t.Fatalf("the staged brief satisfies the gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "specs", "frontend", "brief.md")); !os.IsNotExist(err) {
		t.Fatalf("the brief was only staged: %v", err)
	}
}
