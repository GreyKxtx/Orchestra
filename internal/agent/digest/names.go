package digest

import "strings"

// toolNameAliases maps LLM-facing aliases to canonical tool names used by the
// agent loop and tools.Runner.Call. See docs/tools-status.md.
var toolNameAliases = map[string]string{
	"fs.read": "read", "fs.list": "ls", "fs.write": "write", "fs.edit": "edit",
	"fs.glob": "glob", "file.write_atomic": "write",
	"search.text": "grep", "code.symbols": "symbols", "explore_codebase": "explore",
	"exec.run": "bash", "bash_output": "bash.output", "bash_kill": "bash.kill",
	"todo.write": "todowrite", "todo.read": "todoread",
	"task.spawn": "task_spawn", "task.wait": "task_wait", "task.cancel": "task_cancel",
	"task.result": "task_result", "Task": "task",
	"web.fetch": "webfetch", "web.search": "websearch",
	"memory.write": "memory_write",
	"memory.read":  "memory_read",
}

// NormalizeToolName maps common LLM aliases to canonical registry names.
func NormalizeToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	key := strings.ToLower(name)
	if canon, ok := toolNameAliases[key]; ok {
		return canon
	}
	return key
}

// IsDigestedToolContent reports whether content was already replaced by a digest header.
func IsDigestedToolContent(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "[digest tool:")
}
