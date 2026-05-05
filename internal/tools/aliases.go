package tools

// aliasToCanonical maps LLM-facing short names to internal canonical
// tool names. The canonical form is what every dispatch, log, and stored
// artefact uses; the alias is what the LLM sees in tools[].
//
// Legacy callers may still send canonical names (e.g. fs.read); those
// pass through unchanged via resolveToolName.
var aliasToCanonical = map[string]string{
	"read":        "fs.read",
	"ls":          "fs.list",
	"glob":        "fs.glob",
	"write":       "fs.write",
	"edit":        "fs.edit",
	"grep":        "search.text",
	"symbols":     "code.symbols",
	"bash":        "exec.run",
	"explore":     "explore_codebase",
	"runtime":     "runtime.query",
	"todowrite":   "todo.write",
	"todoread":    "todo.read",
	"task_spawn":  "task.spawn",
	"task_wait":   "task.wait",
	"task_cancel": "task.cancel",
	"task_result": "task.result",
}

// canonicalToAlias is the inverse of aliasToCanonical, built once at init.
// Tools without an alias (plan_enter, plan_exit, question, fs.apply_ops)
// don't appear here; callers should fall back to the canonical name.
var canonicalToAlias = func() map[string]string {
	m := make(map[string]string, len(aliasToCanonical))
	for alias, canonical := range aliasToCanonical {
		m[canonical] = alias
	}
	return m
}()

// resolveToolName returns the canonical name for a tool, accepting either
// an alias ("read") or the canonical name itself ("fs.read"). Unknown
// names pass through unchanged so the dispatch switch can produce its
// usual "unknown tool" error.
func resolveToolName(name string) string {
	if canonical, ok := aliasToCanonical[name]; ok {
		return canonical
	}
	return name
}

// aliasFor returns the LLM-facing alias for a canonical tool name, or
// the canonical name itself if no alias exists (e.g. plan_enter).
func aliasFor(canonical string) string {
	if alias, ok := canonicalToAlias[canonical]; ok {
		return alias
	}
	return canonical
}
