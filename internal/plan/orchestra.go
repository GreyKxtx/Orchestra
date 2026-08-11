package plan

import "strings"

// OrchestraStateRelPath is the Lead scratchpad file (relative to project root).
const OrchestraStateRelPath = ".orchestra/state.md"

// IsOrchestraLeadWritablePath reports paths orchestra Lead may write (plans + scratchpad).
func IsOrchestraLeadWritablePath(path, assignedPlan string) bool {
	if IsWritablePath(path, assignedPlan) {
		return true
	}
	return NormalizeRelPath(path) == NormalizeRelPath(OrchestraStateRelPath)
}

// DefaultOrchestraScratchpad is the initial template when state.md is created.
func DefaultOrchestraScratchpad(goal string) string {
	g := strings.TrimSpace(goal)
	if g == "" {
		g = "(set goal)"
	}
	return "## Goal\n" + g + "\n\n## Done\n\n## Next\n\n## Notes\n"
}
