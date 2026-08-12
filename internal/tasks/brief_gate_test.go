package tasks

import (
	"os"
	"path/filepath"
	"testing"
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
		".orchestra/depts/frontend.md":     "frontend",
		"./.orchestra/depts/frontend@web.md": "frontend@web",
		".orchestra/state.md":              "",
		".orchestra/depts/a/b.md":          "",
		"":                                 "",
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
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err != nil {
		t.Fatalf("no playbook → inactive: %v", err)
	}

	// Playbook opts in, no brief — blocked.
	writeFileT(t, root, ".orchestra/playbooks/frontend.md",
		"---\nbrief_required_fields:\n  - routes\n  - api_contract_ref\n---\n\n## Rules\nx\n")
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err == nil {
		t.Fatal("missing brief must block worker spawn")
	}

	// Brief with a missing required section — blocked, names the section.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home, /cart\n\n## Something else\nx\n")
	err := checkBriefCompleteness(root, deptWorkOrder("frontend"))
	if err == nil {
		t.Fatal("incomplete brief must block")
	}

	// Complete brief (loose heading match) — green.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home, /cart\n\n## API contract ref\nOpenAPI.v0.yaml sha256:abc\n")
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err != nil {
		t.Fatalf("complete brief must pass: %v", err)
	}

	// Empty section body does not count.
	writeFileT(t, root, ".orchestra/specs/frontend/brief.md",
		"## Routes\n/home\n\n## API contract ref\n\n## Next\nx\n")
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err == nil {
		t.Fatal("empty required section must block")
	}

	// Instance falls back to dept-type playbook and brief.
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend@web")); err == nil {
		t.Fatal("instance inherits dept-type gate")
	}

	// Waiver unblocks.
	writeFileT(t, root, ".orchestra/state.md",
		"---\norchestra:\n  phase: execution\n  prd_status: approved\n  waivers: [brief_completeness]\n---\n")
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err != nil {
		t.Fatalf("waiver must unblock: %v", err)
	}

	// Maintenance bypasses.
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: maintenance\n  prd_status: approved\n---\n")
	if err := checkBriefCompleteness(root, deptWorkOrder("frontend")); err != nil {
		t.Fatalf("maintenance must bypass: %v", err)
	}

	// Non-dept-bound WorkOrder — inactive.
	if err := checkBriefCompleteness(root, &WorkOrder{Intent: "x"}); err != nil {
		t.Fatalf("unbound WorkOrder exempt: %v", err)
	}
}
