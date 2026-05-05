package tools

import (
	"strings"
	"testing"
)

// TestResolveToolName_Aliases verifies every alias resolves to its canonical
// form, and canonical names pass through unchanged.
func TestResolveToolName_Aliases(t *testing.T) {
	cases := []struct {
		alias     string
		canonical string
	}{
		{"read", "fs.read"},
		{"ls", "fs.list"},
		{"glob", "fs.glob"},
		{"write", "fs.write"},
		{"edit", "fs.edit"},
		{"grep", "search.text"},
		{"symbols", "code.symbols"},
		{"bash", "exec.run"},
		{"explore", "explore_codebase"},
		{"runtime", "runtime.query"},
		{"todowrite", "todo.write"},
		{"todoread", "todo.read"},
		{"task_spawn", "task.spawn"},
		{"task_wait", "task.wait"},
		{"task_cancel", "task.cancel"},
		{"task_result", "task.result"},
	}
	for _, c := range cases {
		if got := ResolveToolName(c.alias); got != c.canonical {
			t.Errorf("ResolveToolName(%q) = %q; want %q", c.alias, got, c.canonical)
		}
		// Canonical names must pass through unchanged (backward compatibility).
		if got := ResolveToolName(c.canonical); got != c.canonical {
			t.Errorf("ResolveToolName(%q) = %q; want unchanged", c.canonical, got)
		}
	}
}

// TestResolveToolName_UnknownPassthrough verifies unknown names pass
// through unchanged so the dispatch switch can produce its usual error.
func TestResolveToolName_UnknownPassthrough(t *testing.T) {
	for _, name := range []string{"", "totally_unknown", "mcp:server.tool", "plan_enter", "plan_exit", "question"} {
		if got := ResolveToolName(name); got != name {
			t.Errorf("ResolveToolName(%q) = %q; want unchanged", name, got)
		}
	}
}

// TestAliasFor_Inverse verifies aliasFor() returns the alias for any
// canonical name that has one, and the canonical name unchanged otherwise.
func TestAliasFor_Inverse(t *testing.T) {
	for alias, canonical := range aliasToCanonical {
		if got := aliasFor(canonical); got != alias {
			t.Errorf("aliasFor(%q) = %q; want %q", canonical, got, alias)
		}
	}
	// Names without an alias should pass through unchanged.
	for _, name := range []string{"plan_enter", "plan_exit", "question", "fs.apply_ops", "unknown"} {
		if got := aliasFor(name); got != name {
			t.Errorf("aliasFor(%q) = %q; want unchanged", name, got)
		}
	}
}

// TestListTools_ExposesAliasesNotCanonical verifies that no canonical name
// with a defined alias leaks into the LLM-facing tools[] inventory, and
// no LLM-facing name contains a dot (forbidden by the OpenAI tool-name
// regex ^[a-zA-Z0-9_-]+$).
func TestListTools_ExposesAliasesNotCanonical(t *testing.T) {
	defs := ListTools(true /*allowExec*/)
	for _, d := range defs {
		// If d.Function.Name is a canonical name that *has* an alias, we
		// leaked the canonical instead of the alias — that's a bug in
		// registry.go or the alias table.
		if alias, hasAlias := canonicalToAlias[d.Function.Name]; hasAlias {
			t.Errorf("ListTools leaks canonical name %q; expected alias %q", d.Function.Name, alias)
		}
		if strings.Contains(d.Function.Name, ".") {
			t.Errorf("LLM-facing tool name %q contains a dot (forbidden by OpenAI tool-name regex)", d.Function.Name)
		}
	}
}

// TestListToolsForMode_AliasesEverywhere covers the build/plan/explore
// flavors of ListToolsForMode plus subtask-enabled and question-enabled
// variants, ensuring every code path emits aliases.
func TestListToolsForMode_AliasesEverywhere(t *testing.T) {
	cases := []struct {
		mode             string
		allowExec        bool
		hasSubtasks      bool
		hasQuestionAsker bool
	}{
		{"build", true, true, true},
		{"build", false, false, false},
		{"plan", false, true, true},
		{"plan", false, false, false},
		{"explore", false, false, false},
	}
	for _, c := range cases {
		defs := ListToolsForMode(c.mode, c.allowExec, c.hasSubtasks, c.hasQuestionAsker)
		for _, d := range defs {
			if alias, hasAlias := canonicalToAlias[d.Function.Name]; hasAlias {
				t.Errorf("mode=%q allowExec=%v hasSubtasks=%v hasQuestionAsker=%v: leaks canonical %q (expected %q)",
					c.mode, c.allowExec, c.hasSubtasks, c.hasQuestionAsker, d.Function.Name, alias)
			}
			if strings.Contains(d.Function.Name, ".") {
				t.Errorf("mode=%q: LLM-facing name %q contains a dot", c.mode, d.Function.Name)
			}
		}
	}
}
