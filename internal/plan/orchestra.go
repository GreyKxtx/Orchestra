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

// IsOrchestraLeadWritablePath reports paths the orchestra Lead may write with
// write and edit: plans and local playbook overlays. state.md and the
// department scratchpads are the runtime's, changed through
// update_working_state; the contract artifact it owns is granted by its
// ownership (contract.OwnedBy).
func IsOrchestraLeadWritablePath(path, assignedPlan string) bool {
	if IsWritablePath(path, assignedPlan) {
		return true
	}
	_, ok := playbooks.ParseLocalOverlayPath(NormalizeRelPath(path))
	return ok
}

// runtimeOwnedFiles and runtimeOwnedDirs are the runtime's own records under
// .orchestra/ (lower case, see IsRuntimeOwnedPath).
var (
	runtimeOwnedFiles = []string{OrchestraStateRelPath, ".orchestra/decisions.md", ".orchestra/contract/epoch.yaml"}
	runtimeOwnedDirs  = []string{strings.TrimSuffix(OrchestraDeptsRelDir, "/"), ".orchestra/agency"}
)

// IsRuntimeOwnedPath reports the runtime's own records under .orchestra/:
// state.md, the department scratchpads, the decision log, the contract epoch
// and the agency's inboxes and threads. Each has a tool that keeps its rules
// (update_working_state, the Question Barrier, contract_freeze, send_message,
// agent_post); no mode changes them with write, edit, delete, rename or a
// final patch. The comparison ignores case, as Windows and macOS do.
func IsRuntimeOwnedPath(path string) bool {
	p := strings.ToLower(NormalizeRelPath(path))
	for _, f := range runtimeOwnedFiles {
		if p == f {
			return true
		}
	}
	for _, d := range runtimeOwnedDirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// HoldsRuntimeOwnedPath reports a path that is one of the runtime's records or
// a directory above one: deleting or renaming it takes the records with it.
func HoldsRuntimeOwnedPath(path string) bool {
	if IsRuntimeOwnedPath(path) {
		return true
	}
	p := strings.ToLower(NormalizeRelPath(path))
	if p == "" {
		return false
	}
	if p == "." {
		return true
	}
	for _, f := range append(append([]string(nil), runtimeOwnedFiles...), runtimeOwnedDirs...) {
		if strings.HasPrefix(f, p+"/") {
			return true
		}
	}
	return false
}

// Dept Lead (architecture subagent) L2 surface (spec §6.1): per-dept
// playbooks and epic specs. conventions.md (L1) stays with the Docs Lead.
const (
	OrchestraPlaybooksRelDir = ".orchestra/playbooks/"
	OrchestraConventionsRel  = ".orchestra/playbooks/conventions.md"
	OrchestraSpecsRelDir     = ".orchestra/specs/"
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
