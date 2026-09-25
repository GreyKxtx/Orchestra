package plan_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/plan"
)

func TestIsOrchestraLeadWritablePath(t *testing.T) {
	if !plan.IsOrchestraLeadWritablePath(".orchestra/plans/foo.md", "") {
		t.Fatal("plans should be writable")
	}
	if !plan.IsOrchestraLeadWritablePath(".orchestra/playbooks/local/frontend.md", "") {
		t.Fatal("local playbook overlays should be writable")
	}
	// state.md and the scratchpads change through update_working_state,
	// which guards the phase and keeps the user's waivers.
	for _, p := range []string{".orchestra/state.md", ".orchestra/depts/frontend@web.md", "internal/foo.go"} {
		if plan.IsOrchestraLeadWritablePath(p, "") {
			t.Errorf("%s must not be writable with write/edit", p)
		}
	}
}

func TestIsRuntimeOwnedPath(t *testing.T) {
	owned := []string{
		".orchestra/state.md",
		"./.orchestra/state.md",
		"src/../.orchestra/state.md",
		".Orchestra/State.md",
		".orchestra/depts/frontend@web.md",
		".orchestra/depts",
		".orchestra/decisions.md",
		".orchestra/contract/EPOCH.yaml",
		".orchestra/agency/threads/t1.jsonl",
	}
	for _, p := range owned {
		if !plan.IsRuntimeOwnedPath(p) {
			t.Errorf("%s is the runtime's", p)
		}
	}
	free := []string{
		".orchestra/plans/a.md",
		".orchestra/contract/NFR.md",
		".orchestra/specs/backend/brief.md",
		".orchestra/state.md.bak/x",
		"docs/decisions.md",
		".orchestra",
	}
	for _, p := range free {
		if plan.IsRuntimeOwnedPath(p) {
			t.Errorf("%s is not a runtime record", p)
		}
	}
	for _, p := range []string{".orchestra", ".", ".orchestra/contract", ".ORCHESTRA/depts"} {
		if !plan.HoldsRuntimeOwnedPath(p) {
			t.Errorf("deleting %s takes runtime records with it", p)
		}
	}
	for _, p := range []string{".orchestra/plans", "src", ".orchestra/specs"} {
		if plan.HoldsRuntimeOwnedPath(p) {
			t.Errorf("%s holds no runtime record", p)
		}
	}
}
