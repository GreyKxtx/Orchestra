package plan

import "testing"

func TestIsDeptLeadWritablePath(t *testing.T) {
	assigned := ".orchestra/plans/abc.md"
	allowed := []string{
		".orchestra/plans/abc.md",
		".orchestra/plans/other.md",
		".orchestra/playbooks/frontend.md",
		".orchestra/playbooks/frontend@web.md",
		".orchestra/playbooks/local/frontend.md",
		".orchestra/playbooks/local/frontend@web.md",
		".orchestra/specs/frontend/brief.md",
		".orchestra/specs/backend/tz-epic-3.md",
	}
	for _, p := range allowed {
		if !IsDeptLeadWritablePath(p, assigned) {
			t.Errorf("%s must be writable for Dept Lead", p)
		}
	}
	denied := []string{
		".orchestra/playbooks/conventions.md", // L1 — Docs Lead only
		".orchestra/playbooks/sub/deep.md",    // no nesting
		".orchestra/playbooks/local/nested/x.md",
		".orchestra/playbooks/frontend.txt",   // .md only
		".orchestra/state.md",                 // orchestrator only
		".orchestra/depts/frontend.md",        // scratchpad via update_working_state
		".orchestra/contract/Domain_Model.md",
		"internal/core/core.go",
	}
	for _, p := range denied {
		if IsDeptLeadWritablePath(p, assigned) {
			t.Errorf("%s must be denied for Dept Lead", p)
		}
	}
}
