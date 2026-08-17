package tools

import (
	"testing"

	"github.com/orchestra/orchestra/internal/tools/session"
)

// TestParallelFlags_AllBuiltinsClassified is the safety net for H1
// (architecture audit). It enumerates every built-in tool name the
// agent surfaces (via the maximal ListTools / mode-specific variants
// with every capability flag on) and asserts that each name appears
// in exactly one of parallelSafeTools or mutatingTools.
//
// A new built-in tool added without registering its concurrency
// profile fails here loud and immediate. MCP / plugin tools are not
// run through this gate — they go through ExtraTools and keep the
// conservative default by design.
func TestParallelFlags_AllBuiltinsClassified(t *testing.T) {
	// Maximal: every capability flag, every mode that adds new tools,
	// plus subtask and question-asker tools, plus conditional tools that
	// are only added when their feature is enabled (semantic_search,
	// skill_invoke).
	maximal := ListTools(Capabilities{Exec: true, Web: true, Browser: true})
	maximal = append(maximal, ListToolsWithSubtasks(Capabilities{Exec: true, Web: true, Browser: true})...)
	for _, mode := range []string{"build", "plan", "explore", "general", "orchestra", "worker", "verifier"} {
		maximal = append(maximal, ListToolsForMode(mode, Capabilities{Exec: true, Web: true, Browser: true}, true, true)...)
	}
	maximal = append(maximal, ToolSemanticSearch())
	maximal = append(maximal, ToolSkillInvoke([]string{"sample"}))
	// Orchestra Lead no longer advertises these, but custom agents / architecture
	// surfaces still resolve them via allToolDefsMap.
	maximal = append(maximal, session.ToolContractFreeze(), session.ToolUpdateWorkingState())

	seen := make(map[string]bool, len(maximal))
	for _, def := range maximal {
		seen[def.Function.Name] = true
	}

	var missing []string
	for name := range seen {
		if !parallelSafeTools[name] && !mutatingTools[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("built-in tool(s) missing from parallelSafeTools / mutatingTools: %v\n"+
			"add the name to the appropriate map in registry.go", missing)
	}

	// Symmetric check: a tool registered in the maps must actually exist
	// in the surface (catches stale entries after a tool is deleted).
	for name := range parallelSafeTools {
		if !seen[name] {
			t.Errorf("parallelSafeTools registers %q but no built-in tool produces it", name)
		}
	}
	for name := range mutatingTools {
		if !seen[name] {
			t.Errorf("mutatingTools registers %q but no built-in tool produces it", name)
		}
	}

	// Mutually exclusive: a tool can't be both.
	for name := range parallelSafeTools {
		if mutatingTools[name] {
			t.Errorf("%q registered in BOTH parallelSafeTools and mutatingTools", name)
		}
	}
}
