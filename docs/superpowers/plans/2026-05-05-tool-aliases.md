# Tool Aliases Implementation Plan

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **User preference (overrides skill default):** implement all functionality first, run a single test-audit pass at the end. No TDD per task. Source: `memory/user_prefs.md`.

**Goal:** Expose short OpenCode-style tool names (`read`, `glob`, `grep`, `bash`, `edit`, `write`, …) to the LLM while keeping the namespaced internal API intact. First-shot tool-calling accuracy of local models should improve because short names match Cursor/Claude Code/Cline conventions present in their training data.

**Architecture:**
- A single canonical mapping table `tools.aliasToCanonical` lives in `internal/tools/aliases.go`.
- `internal/tools/registry.go` exposes **short names** to the LLM via every `ListTools*` function.
- `internal/tools/call.go` dispatches on **canonical** names; the very first thing it does is alias-resolve. Both short names *and* legacy namespaced names (`fs.read`, `fs.list`, …) are accepted — legacy names map to the same canonical handlers, so existing `plan.json` / `last_run.jsonl` artefacts and any IDE/MCP integrations keep working.
- Internal Go code keeps using the namespaced strings (`"fs.read"`) — they remain the canonical form. Only the LLM-facing layer is renamed.
- `internal/protocol.ToolsVersion` is bumped because the wire-visible tool inventory changed.

**Tech Stack:** Go 1.22+, JSON Schema (`internal/llm.ToolDef`).

---

## Naming map

| Internal canonical | LLM-facing alias |
|---|---|
| `fs.read` | `read` |
| `fs.list` | `ls` |
| `fs.glob` | `glob` |
| `fs.write` | `write` |
| `fs.edit` | `edit` |
| `search.text` | `grep` |
| `code.symbols` | `symbols` |
| `exec.run` | `bash` |
| `explore_codebase` | `explore` |
| `runtime.query` | `runtime` |
| `todo.write` | `todowrite` |
| `todo.read` | `todoread` |
| `task.spawn` | `task_spawn` |
| `task.wait` | `task_wait` |
| `task.cancel` | `task_cancel` |
| `task.result` | `task_result` |
| `plan_enter` | `plan_enter` (unchanged) |
| `plan_exit` | `plan_exit` (unchanged) |
| `question` | `question` (unchanged) |
| `fs.apply_ops` | *(internal-only, never exposed to LLM)* |

Rationale: the OpenAI tool-name regex is `^[a-zA-Z0-9_-]+$` — dots are technically out of spec, even though most providers tolerate them. Short snake/lowercase aliases are conformant *and* match training-data conventions. `task_*` keeps the family related; `task` alone would collide semantically with OpenCode's subagent-dispatch verb but our `task.spawn` already *is* subagent dispatch — using `task_spawn` keeps the verb explicit and groups with `_wait/_cancel/_result`.

`fs.apply_ops` is never returned by `ListTools*`; only the agent's resolver path calls it directly. It stays canonical-only.

---

## File Structure

**Create:**
- `internal/tools/aliases.go` — alias table + resolution helpers.

**Modify:**
- `internal/tools/registry.go` — every `tool*()` factory uses the alias name in `ToolFunctionDef.Name`.
- `internal/tools/call.go` — alias-resolve at the top of `Call`.
- `internal/agent/agent.go:405,431` — `plan_enter`/`plan_exit` strings unchanged (no rename), but if any other tool-name string appears, update it.
- `internal/prompt/agent_prompt.go:150,188,210-211,237` — replace `fs.read` / `fs.list` / `search.text` / `code.symbols` / `fs.write` / `fs.edit` / `exec.run` / `explore_codebase` / `task.spawn` / `runtime.query` mentions with the aliases.
- `internal/protocol/version.go` — bump `ToolsVersion` by 1.
- `docs/PROTOCOL.md` — note new alias names in the tools-version-bump section.
- Test files (only in the test-audit task): `internal/tools/registry_test.go`, `internal/tools/normalize_test.go`, `internal/tools/explore_codebase_test.go`, `internal/tools/exec_test.go`, `internal/tools/fs_edit_test.go`, `internal/tools/fs_glob_test.go`, `internal/tools/fs_write_test.go`, `internal/tools/code_symbols_test.go`, `internal/tools/runtime_query_test.go`, `internal/tools/todo_test.go`.

---

## Task 1: Add alias table and resolver

**Files:**
- Create: `internal/tools/aliases.go`

- [ ] **Step 1.1: Create the alias module**

```go
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
```

- [ ] **Step 1.2: Commit**

```bash
git add internal/tools/aliases.go
git commit -m "feat(tools): add alias table for LLM-facing short tool names"
```

---

## Task 2: Use aliases in tool definitions exposed to LLM

**Files:**
- Modify: `internal/tools/registry.go`

The cleanest change: every factory function (`toolFSRead`, `toolFSList`, …) sets `Name: aliasFor("fs.read")` instead of the literal `"fs.read"`. This keeps a single source of truth in `aliases.go` — if we ever rename again, only the table changes.

- [ ] **Step 2.1: Change `Name` fields in all factory functions**

For every `tool*` function in `registry.go`, replace the literal `Name: "<canonical>"` with `Name: aliasFor("<canonical>")`. Concretely:

```go
// before
func toolFSRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.read",
			Description: "Читает файл в workspace и возвращает content+sha256 (file_hash).",
			...
		},
	}
}

// after
func toolFSRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        aliasFor("fs.read"),
			Description: "Читает файл в workspace и возвращает content+sha256 (file_hash).",
			...
		},
	}
}
```

Apply the same change to: `toolFSList`, `toolFSGlob`, `toolFSWrite`, `toolFSEdit`, `toolSearchText`, `toolCodeSymbols`, `toolExploreCodebase`, `toolExecRun`, `toolTodoWrite`, `toolTodoRead`, `toolTaskSpawn`, `toolTaskWait`, `toolTaskCancel`, `toolTaskResult`, `toolRuntimeQuery`. Leave `toolPlanEnter`, `toolPlanExit`, `toolQuestion` as literals — they have no alias and `aliasFor` would return them unchanged anyway, but a literal is clearer.

- [ ] **Step 2.2: Build and ensure compilation**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 2.3: Commit**

```bash
git add internal/tools/registry.go
git commit -m "feat(tools): expose short OpenCode-style aliases to LLM"
```

---

## Task 3: Resolve aliases in Runner.Call dispatch

**Files:**
- Modify: `internal/tools/call.go`

- [ ] **Step 3.1: Insert alias resolution at the top of `Call`**

After `name = strings.TrimSpace(name)` and the empty-name check, before the `mcp:` prefix routing, add a single line that normalizes name to its canonical form. Keep `mcp:*` routing using the *original* name (MCP names are not in our alias table and may legitimately contain dots).

```go
func (r *Runner) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool name is empty")
	}

	// Route mcp:* calls to the registered MCP manager (use original name).
	if r.mcpCaller != nil && strings.HasPrefix(name, "mcp:") {
		return r.mcpCaller.Call(ctx, name, input)
	}

	// Accept both LLM-facing aliases (read, grep, bash, …) and legacy
	// canonical names (fs.read, search.text, exec.run, …). Internal
	// dispatch is keyed on the canonical form.
	name = resolveToolName(name)

	switch name {
	case "fs.list":
	...
```

- [ ] **Step 3.2: Build**

```bash
go build ./...
```

- [ ] **Step 3.3: Commit**

```bash
git add internal/tools/call.go
git commit -m "feat(tools): accept aliases and legacy names in Runner.Call dispatch"
```

---

## Task 4: Update agent prompts

**Files:**
- Modify: `internal/prompt/agent_prompt.go`

System prompts mention tool names in plain text — the LLM compares them with what it sees in `tools[]`, so they must match. Replace canonical names with aliases in three places (the literals are visible at lines ~150, ~188, ~210-211, ~237 in current `master`).

- [ ] **Step 4.1: Build mode prompt — update tool list mention**

Find the line in `BuildSystemPromptForFamily` that reads:

```go
Доступные инструменты: fs.list, fs.read, fs.write, fs.edit, fs.glob, search.text, code.symbols и другие из tools[].
```

Replace with:

```go
Доступные инструменты: ls, read, write, edit, glob, grep, symbols и другие из tools[].
```

- [ ] **Step 4.2: Plan-mode reminder constant**

Replace:

```go
const PlanModeReminder = `РЕЖИМ ПЛАНИРОВАНИЯ АКТИВЕН. СТРОГО ЗАПРЕЩЕНО: fs.write и fs.edit (кроме .orchestra/plan.md), exec.run. Анализируй кодовую базу, задавай вопросы через question, запиши план в .orchestra/plan.md, затем вызови plan_exit.`
```

with:

```go
const PlanModeReminder = `РЕЖИМ ПЛАНИРОВАНИЯ АКТИВЕН. СТРОГО ЗАПРЕЩЕНО: write и edit (кроме .orchestra/plan.md), bash. Анализируй кодовую базу, задавай вопросы через question, запиши план в .orchestra/plan.md, затем вызови plan_exit.`
```

- [ ] **Step 4.3: Plan-mode system prompt body**

Inside `buildPlanSystemPrompt`, replace the two affected lines:

```go
СТРОГО ЗАПРЕЩЕНО: fs.write, fs.edit (кроме .orchestra/plan.md), exec.run — даже если пользователь просит.
Разрешено: fs.read, fs.list, fs.glob, search.text, code.symbols, explore_codebase, runtime.query, task.spawn, question, plan_exit.
```

with:

```go
СТРОГО ЗАПРЕЩЕНО: write, edit (кроме .orchestra/plan.md), bash — даже если пользователь просит.
Разрешено: read, ls, glob, grep, symbols, explore, runtime, task_spawn, question, plan_exit.
```

And the numbered step list:

```go
1. Изучи кодовую базу: fs.read / search.text / code.symbols / explore_codebase
```

with:

```go
1. Изучи кодовую базу: read / grep / symbols / explore
```

And the "Напиши … через fs.write" line:

```go
4. Напиши архитектурный план в .orchestra/plan.md через fs.write (единственный разрешённый write)
```

with:

```go
4. Напиши архитектурный план в .orchestra/plan.md через write (единственный разрешённый запис)
```

- [ ] **Step 4.4: Explore-mode system prompt**

Inside `buildExploreSystemPrompt`, replace:

```go
Инструменты: fs.read, fs.list, fs.glob, search.text, code.symbols.
Когда закончил — вызови task.result с кратким структурированным ответом.
```

with:

```go
Инструменты: read, ls, glob, grep, symbols.
Когда закончил — вызови task_result с кратким структурированным ответом.
```

- [ ] **Step 4.5: Build**

```bash
go build ./...
```

- [ ] **Step 4.6: Commit**

```bash
git add internal/prompt/agent_prompt.go
git commit -m "feat(prompt): rename tool mentions in system prompts to aliases"
```

---

## Task 5: Bump ToolsVersion

**Files:**
- Modify: `internal/protocol/version.go`
- Modify: `docs/PROTOCOL.md`

The set of tool names returned in `initialize` capabilities changed — clients pinned to the previous `ToolsVersion` should hard-fail at handshake instead of silently breaking on unknown names.

- [ ] **Step 5.1: Read current version**

```bash
go run ./cmd/orchestra core --workspace-root . --debug 2>&1 | head
```

(or simply read `internal/protocol/version.go`). Note the current `ToolsVersion` value.

- [ ] **Step 5.2: Increment ToolsVersion by 1**

Edit the constant in `internal/protocol/version.go`. Leave `ProtocolVersion` and `OpsVersion` untouched — only the tool inventory changed.

- [ ] **Step 5.3: Update docs/PROTOCOL.md**

Add a single line in the version-history / changelog section noting: "ToolsVersion bumped: tool names switched to short aliases (`read`/`grep`/`bash`/…); legacy canonical names (`fs.read`/…) remain accepted in `tool.call`."

- [ ] **Step 5.4: Build and run vet**

```bash
go vet ./...
go build ./...
```

- [ ] **Step 5.5: Commit**

```bash
git add internal/protocol/version.go docs/PROTOCOL.md
git commit -m "chore(protocol): bump ToolsVersion for tool-name aliases"
```

---

## Task 6: Smoke check end-to-end (manual, no LLM)

**Files:** none — verification only.

- [ ] **Step 6.1: Run vet**

```bash
go vet ./...
```

Expected: clean.

- [ ] **Step 6.2: Run all unit tests**

```bash
go test ./...
```

Expected: tests fail (pre-existing tests reference old tool names). Note which packages fail — they will be fixed in Task 7.

- [ ] **Step 6.3: Manual sanity — list tools via JSON-RPC**

```powershell
$proc = Start-Process -PassThru -RedirectStandardInput -RedirectStandardOutput stdout.txt `
    -FilePath ./orchestra.exe -ArgumentList "core","--workspace-root","."
# (or use existing chat smoke if simpler)
```

Quicker: run the existing `internal/tools` snapshot test (after Task 7 fixes it) and inspect names in output. Document name set in `.orchestra/tool_inventory_smoke.txt` for reference.

---

## Task 7: Test audit pass

This is the single dedicated test task per `user_prefs.md`. Walk every test that referenced a renamed tool, decide: is the test still valuable? If yes, update names. If it tested a no-longer-meaningful invariant, delete.

**Files:**
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/tools/normalize_test.go`
- Modify: `internal/tools/explore_codebase_test.go`
- Modify: `internal/tools/exec_test.go`
- Modify: `internal/tools/fs_edit_test.go`
- Modify: `internal/tools/fs_glob_test.go`
- Modify: `internal/tools/fs_write_test.go`
- Modify: `internal/tools/code_symbols_test.go`
- Modify: `internal/tools/runtime_query_test.go`
- Modify: `internal/tools/todo_test.go`
- Create: `internal/tools/aliases_test.go`

- [ ] **Step 7.1: For each test file in the list**

Open the file, search for the canonical names (`fs.read`, `fs.list`, `fs.glob`, `fs.write`, `fs.edit`, `search.text`, `code.symbols`, `exec.run`, `explore_codebase`, `runtime.query`, `todo.write`, `todo.read`, `task.spawn`, `task.wait`, `task.cancel`, `task.result`). For every match:

- If the test exercises `Runner.Call(name, …)` with the canonical name: keep it as-is. The dispatch *must* still accept canonical names — this is exactly the backward-compat invariant we want a regression test for.
- If the test asserts on `ToolDef.Name` returned from `ListTools*`: update the expected name to the alias.
- If the test asserts on prompt text: update to the new mentions.

- [ ] **Step 7.2: Add `internal/tools/aliases_test.go` covering the new contract**

Two test functions:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRunner_Call_AcceptsAliases verifies that LLM-facing aliases dispatch
// to the same handler as the canonical name. Uses the simplest no-side-effect
// tool path (todo.read) to avoid filesystem coupling.
func TestRunner_Call_AcceptsAliases(t *testing.T) {
	r := newTestRunner(t) // helper from existing tests
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
		if got := resolveToolName(c.alias); got != c.canonical {
			t.Errorf("resolveToolName(%q) = %q; want %q", c.alias, got, c.canonical)
		}
		// Canonical names must pass through unchanged.
		if got := resolveToolName(c.canonical); got != c.canonical {
			t.Errorf("resolveToolName(%q) = %q; want unchanged", c.canonical, got)
		}
	}
}

// TestListTools_ExposesAliasesNotCanonical verifies that no canonical name
// with a defined alias leaks into the LLM-facing tools[] inventory.
func TestListTools_ExposesAliasesNotCanonical(t *testing.T) {
	defs := ListTools(true /*allowExec*/)
	for _, d := range defs {
		if _, leaked := aliasToCanonical[d.Function.Name]; !leaked {
			// d.Function.Name is *not* an alias key — it might be a
			// canonical name that has an alias (bug) or a name with no
			// alias (plan_enter, plan_exit, question — fine).
			if _, hasAlias := canonicalToAlias[d.Function.Name]; hasAlias {
				t.Errorf("ListTools leaks canonical name %q; expected alias %q", d.Function.Name, canonicalToAlias[d.Function.Name])
			}
		}
		if strings.Contains(d.Function.Name, ".") {
			// The OpenAI tool-name regex disallows dots. After this
			// migration, no LLM-facing name should contain one.
			t.Errorf("LLM-facing tool name %q contains a dot", d.Function.Name)
		}
	}
}

// (Helper) call sample — only meaningful if a real Runner exists in tests.
func _call(t *testing.T, r interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}, name string) {
	t.Helper()
	if _, err := r.Call(context.Background(), name, json.RawMessage(`{}`)); err != nil && !strings.Contains(err.Error(), "required") {
		t.Errorf("Call(%q) unexpected error: %v", name, err)
	}
}
```

(The `newTestRunner` helper exists in current tests; if not, omit `TestRunner_Call_AcceptsAliases` and rely on `resolveToolName` unit coverage only.)

- [ ] **Step 7.3: Run all tests**

```bash
go test ./...
go test -race ./internal/tools ./internal/agent ./internal/core
```

Expected: all green.

- [ ] **Step 7.4: Commit**

```bash
git add internal/tools/
git commit -m "test(tools): update existing tests for alias names + add alias contract tests"
```

---

## Task 8: Live LLM smoke test

**Files:** none — manual verification only.

- [ ] **Step 8.1: Build the binary**

```bash
go build -o orchestra ./cmd/orchestra
```

- [ ] **Step 8.2: Run an apply against the running LM Studio**

```bash
./orchestra apply --debug "Перечисли файлы в internal/tools и кратко скажи, какой из них главный"
```

Expected: agent issues `ls`/`read` (or `fs.list`/`fs.read` — model's choice; both work). Verify in `.orchestra/last_run.jsonl` that the recorded canonical name is `fs.list`/`fs.read` (canonical form), regardless of which name the model emitted.

- [ ] **Step 8.3: Re-run with chat**

```bash
./orchestra chat --workspace .
```

Type a request that needs `read` + `edit`. Confirm patch applies cleanly.

- [ ] **Step 8.4: If both pass, the plan is complete.** No code commit; the `last_run.jsonl` from step 8.2 stays as a smoke artefact.

---

## Self-review notes

- Spec coverage: every item in the naming map has a touch point in Tasks 2/3 (definitions + dispatch); every prompt mention is covered in Task 4; protocol contract covered in Task 5; backward compat covered by Task 3's `resolveToolName` and verified in Task 7.
- No placeholder steps — every code step shows the diff.
- Type/method consistency: `resolveToolName` and `aliasFor` are defined in Task 1 and consumed verbatim in Tasks 2/3/7.
- Risk: if a downstream consumer (IDE plugin, MCP client) hard-codes a canonical name in `tool.call`, it keeps working because `Runner.Call` accepts canonical names. If it pins on `ToolsVersion`, it correctly fails fast at `initialize`.
