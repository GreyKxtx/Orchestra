package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

func newArchAgent(t *testing.T, root string) *Agent {
	t.Helper()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return &Agent{opts: Options{Mode: ModeArchitecture}, tools: tr}
}

func TestCheckDeptPlaybookNarrowing(t *testing.T) {
	root := t.TempDir()
	a := newArchAgent(t, root)

	// No accepted_risks — always fine.
	clean := `{"path":".orchestra/playbooks/frontend.md","content":"---\nbrief_required_fields: [routes]\n---\n\n## Rules\nx\n"}`
	if err := a.checkDeptPlaybookNarrowing([]byte(clean)); err != nil {
		t.Fatalf("clean playbook must pass: %v", err)
	}

	// accepted_risks without an approval record in decisions.md — denied.
	risky := `{"path":".orchestra/playbooks/frontend.md","content":"---\naccepted_risks:\n  - skip a11y audit for MVP\n---\n\n## Rules\nx\n"}`
	if err := a.checkDeptPlaybookNarrowing([]byte(risky)); err == nil {
		t.Fatal("unapproved accepted_risk must be denied")
	}

	// Approval recorded in decisions.md — allowed.
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := "# Decision log\n\n## 2026-08-12 · waiver · frontend\n- Q: accepted_risk: skip a11y audit for MVP\n- A: approved by user\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "decisions.md"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.checkDeptPlaybookNarrowing([]byte(risky)); err != nil {
		t.Fatalf("approved accepted_risk must pass: %v", err)
	}

	// Non-playbook paths and other modes are exempt.
	spec := `{"path":".orchestra/specs/frontend/brief.md","content":"---\naccepted_risks: [whatever]\n---\n"}`
	if err := a.checkDeptPlaybookNarrowing([]byte(spec)); err != nil {
		t.Fatalf("specs path is not narrowing-checked: %v", err)
	}
	b := &Agent{opts: Options{Mode: ModeBuild}}
	if err := b.checkDeptPlaybookNarrowing([]byte(risky)); err != nil {
		t.Fatalf("build mode exempt: %v", err)
	}
}
