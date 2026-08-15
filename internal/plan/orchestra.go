package plan

import (
	"strings"

	"github.com/orchestra/orchestra/internal/playbooks"
)

// OrchestraStateRelPath is the Lead scratchpad file (relative to project root).
const OrchestraStateRelPath = ".orchestra/state.md"

// OrchestraDeptsRelDir holds department-instance scratchpads (spec §5.8),
// one .md file per instance (frontend.md, frontend@web.md).
const OrchestraDeptsRelDir = ".orchestra/depts/"

// IsOrchestraLeadWritablePath reports paths orchestra Lead may write
// (plans + scratchpad + dept scratchpads).
func IsOrchestraLeadWritablePath(path, assignedPlan string) bool {
	if IsWritablePath(path, assignedPlan) {
		return true
	}
	p := NormalizeRelPath(path)
	if p == NormalizeRelPath(OrchestraStateRelPath) {
		return true
	}
	if _, ok := playbooks.ParseLocalOverlayPath(p); ok {
		return true
	}
	return strings.HasPrefix(p, OrchestraDeptsRelDir) && strings.HasSuffix(p, ".md") &&
		!strings.Contains(strings.TrimPrefix(p, OrchestraDeptsRelDir), "/")
}

// Dept Lead (architecture subagent) L2 surface (spec §6.1): per-dept
// playbooks and epic specs. conventions.md (L1) stays with the Docs Lead.
const (
	OrchestraPlaybooksRelDir   = ".orchestra/playbooks/"
	OrchestraConventionsRel    = ".orchestra/playbooks/conventions.md"
	OrchestraSpecsRelDir       = ".orchestra/specs/"
)

// IsDeptLeadWritablePath reports paths an architecture-mode Dept Lead may
// write: plan files, its L2 playbook `.orchestra/playbooks/{dept}.md`
// (never conventions.md — that is the Docs Lead's L1), and Brief/ТЗ files
// under `.orchestra/specs/`.
func IsDeptLeadWritablePath(path, assignedPlan string) bool {
	if IsWritablePath(path, assignedPlan) {
		return true
	}
	p := NormalizeRelPath(path)
	if strings.HasPrefix(p, OrchestraSpecsRelDir) && p != strings.TrimSuffix(OrchestraSpecsRelDir, "/") {
		return true
	}
	if p == NormalizeRelPath(OrchestraConventionsRel) {
		return false
	}
	if _, ok := playbooks.ParseLocalOverlayPath(p); ok {
		return true
	}
	return strings.HasPrefix(p, OrchestraPlaybooksRelDir) && strings.HasSuffix(p, ".md") &&
		!strings.Contains(strings.TrimPrefix(p, OrchestraPlaybooksRelDir), "/")
}

// DefaultOrchestraScratchpad is the initial template when state.md is created.
func DefaultOrchestraScratchpad(goal string) string {
	g := strings.TrimSpace(goal)
	if g == "" {
		g = "(set goal)"
	}
	return "## Goal\n" + g + "\n\n## Done\n\n## Next\n\n## Notes\n"
}
