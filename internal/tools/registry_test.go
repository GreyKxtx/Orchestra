package tools

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/xeipuuv/gojsonschema"
)

func TestToolRegistry_AllowExecFalse_NoExecRun(t *testing.T) {
	defs := ListTools(Capabilities{Exec: false, Web: false, Browser: false})
	for _, d := range defs {
		if d.Function.Name == "bash" {
			t.Fatalf("bash must not be exposed when allowExec=false (got %q)", d.Function.Name)
		}
	}
}

// TestTaskSchemas_AcceptEitherGoalOrPrompt asserts task/task_spawn schemas
// accept a call carrying only "goal" OR only "prompt" (the runtime in
// handleTaskTool supports both as aliases), and reject a call with neither.
// A flat "required":["prompt"] previously rejected goal-only calls that the
// runtime actually supports — a real gap for providers doing schema-guided
// decoding (e.g. vLLM guided_json).
func TestTaskSchemas_AcceptEitherGoalOrPrompt(t *testing.T) {
	defs := map[string]json.RawMessage{}
	for _, d := range ListTools(Capabilities{}) {
		defs[d.Function.Name] = d.Function.Parameters
	}
	subtaskDefs := appendSubtaskTools(nil)
	for _, d := range subtaskDefs {
		defs[d.Function.Name] = d.Function.Parameters
	}

	cases := []struct {
		tool    string
		payload string
		wantOK  bool
	}{
		{"task", `{"prompt":"do the thing"}`, true},
		{"task", `{"goal":"do the thing"}`, true},
		{"task", `{}`, false},
		{"task_spawn", `{"goal":"do the thing"}`, true},
		{"task_spawn", `{"prompt":"do the thing"}`, true},
		{"task_spawn", `{}`, false},
	}
	for _, tc := range cases {
		schemaJSON, ok := defs[tc.tool]
		if !ok {
			t.Fatalf("tool %q not found in registry", tc.tool)
		}
		res, err := gojsonschema.Validate(
			gojsonschema.NewBytesLoader(schemaJSON),
			gojsonschema.NewStringLoader(tc.payload))
		if err != nil {
			t.Fatalf("%s %s: validate error: %v", tc.tool, tc.payload, err)
		}
		if res.Valid() != tc.wantOK {
			t.Errorf("%s %s: valid=%v want=%v errors=%v", tc.tool, tc.payload, res.Valid(), tc.wantOK, res.Errors())
		}
	}
}

func TestResolveToolNames(t *testing.T) {
	cases := []struct {
		name    string
		input   []string
		wantLen int
		wantErr bool
	}{
		{"single known", []string{"read"}, 1, false},
		{"multiple known", []string{"read", "grep", "write"}, 3, false},
		{"all tools", []string{"ls", "read", "glob", "write", "edit", "grep", "symbols",
			"explore", "bash", "webfetch", "todowrite", "todoread", "memory_write", "memory_read", "memory_search",
			"runtime_query", "task_spawn", "task_wait", "task_cancel", "task_result",
			"plan_exit", "question", "diff.preview", "lesson_promote", "playbook_promote"}, 25, false},
		{"unknown tool", []string{"read", "fly"}, 0, true},
		{"empty list", []string{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveToolNames(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
		})
	}
}

func TestResolveToolNamesWithPolicy_DropsGatedTools(t *testing.T) {
	// Skill requests bash + webfetch + read, but runtime denies exec and web.
	// Expect only `read` to remain; no error for the dropped ones.
	got, err := ResolveToolNamesWithPolicy(
		[]string{"bash", "webfetch", "read"},
		Capabilities{Exec: false, Web: false, Browser: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Function.Name != "read" {
		t.Fatalf("expected only [read], got %d tools: %+v", len(got), got)
	}
}

func TestResolveToolNamesWithPolicy_AllAllowed(t *testing.T) {
	got, err := ResolveToolNamesWithPolicy(
		[]string{"bash", "webfetch", "read", "git.commit"},
		Capabilities{Exec: true, Web: true, Browser: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected all 4 tools, got %d", len(got))
	}
}

func TestResolveToolNamesWithPolicy_UnknownStillErrors(t *testing.T) {
	_, err := ResolveToolNamesWithPolicy([]string{"read", "no-such-tool"}, Capabilities{Exec: true, Web: true, Browser: true})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestResolveToolNames_PreservesOrder(t *testing.T) {
	names := []string{"write", "read", "grep"}
	defs, err := ResolveToolNames(names)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range defs {
		if d.Function.Name != names[i] {
			t.Errorf("position %d: got %q, want %q", i, d.Function.Name, names[i])
		}
	}
}

func TestListToolsForMode_OrchestraLeadSurface(t *testing.T) {
	defs := ListToolsForMode("orchestra", Capabilities{Exec: true, Web: true, Browser: true}, true, true)
	if len(defs) > 16 {
		t.Fatalf("orchestra Lead must expose ≤16 tools, got %d", len(defs))
	}
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	// contract_freeze and update_working_state were on the forbidden list
	// while their handlers refused every mode BUT the Lead — so the G6 freeze
	// orchestrastate demands ("unblock: … + contract_freeze") could only be
	// reached by hand-writing a waiver. They are Lead tools now.
	for _, want := range []string{"read", "write", "grep", "explore", "repo_map", "task", "task_spawn", "task_wait", "task_cancel", "question", "memory_read", "memory_search", "lesson_promote", "playbook_promote", "contract_freeze", "update_working_state"} {
		if !names[want] {
			t.Fatalf("orchestra mode missing tool %q", want)
		}
	}
	for _, forbid := range []string{"edit", "plan_enter", "ls", "glob", "symbols", "bash", "task_result", "memory_write", "todowrite", "lsp.definition", "lsp.hover", "git.status", "diff.preview", "runtime_query"} {
		if names[forbid] {
			t.Fatalf("orchestra mode must not expose %q", forbid)
		}
	}
}

func TestFilterOrchestraLeadTools_DropsWorkerExtras(t *testing.T) {
	in := append(ListToolsForMode("orchestra", Capabilities{}, true, true),
		llm.ToolDef{Function: llm.ToolFunctionDef{Name: "edit"}},
		llm.ToolDef{Function: llm.ToolFunctionDef{Name: "semantic_search"}},
	)
	out := FilterOrchestraLeadTools(in)
	// 14 + the two Lead-only gate tools (contract_freeze, update_working_state).
	if len(out) > 16 {
		t.Fatalf("filtered Lead tools = %d, want ≤16", len(out))
	}
	for _, d := range out {
		if d.Function.Name == "edit" || d.Function.Name == "semantic_search" {
			t.Fatalf("Lead allowlist leaked %q", d.Function.Name)
		}
	}
}

// The Lead surface is a documented contract (docs/modes.md) and the strongest
// claim the orchestra mode makes: the Lead cannot touch code. docs/modes.md
// promised "строго 14" for a while after contract_freeze and
// update_working_state were added, so the count is pinned by name here rather
// than by an upper bound that silently absorbs the next addition.
func TestOrchestraLeadSurface_IsExactlyTheDocumentedSet(t *testing.T) {
	want := map[string]bool{
		"read": true, "grep": true, "explore": true, "repo_map": true, "write": true,
		"task": true, "task_spawn": true, "task_wait": true, "task_cancel": true,
		"question": true, "memory_read": true, "memory_search": true,
		"lesson_promote": true, "playbook_promote": true,
		"contract_freeze": true, "update_working_state": true,
	}
	got := map[string]bool{}
	for _, d := range FilterOrchestraLeadTools(ListToolsForMode("orchestra", Capabilities{Exec: true, Web: true}, true, true)) {
		got[d.Function.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("Lead surface lost %q; update docs/modes.md if that is intended", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("Lead surface gained %q; add it to docs/modes.md and to this list", name)
		}
	}
}

func TestListToolsForMode_ProductSurface(t *testing.T) {
	names := make(map[string]bool)
	for _, d := range ListToolsForMode("product", Capabilities{}, true, true) {
		names[d.Function.Name] = true
	}
	// Product Lead: reads + PRD writes + web research + question/task_result.
	for _, want := range []string{"read", "write", "edit", "websearch", "webfetch", "task_result", "question"} {
		if !names[want] {
			t.Fatalf("product mode missing tool %q", want)
		}
	}
	// No code execution, no git mutation.
	for _, forbid := range []string{"bash", "git.commit", "git.push", "plan_enter"} {
		if names[forbid] {
			t.Fatalf("product mode must not expose %q", forbid)
		}
	}
	// Spawn only with subtasks: stage 0 pairs the Product Lead with Market
	// Scouts (spec §2.1), and the agency grants it the product > scout edge.
	if !names["task_spawn"] || !names["task_wait"] {
		t.Fatal("with subtasks, product mode must be able to run its scouts in parallel")
	}
	for _, d := range ListToolsForMode("product", Capabilities{}, false, true) {
		if n := d.Function.Name; n == "task" || n == "task_spawn" {
			t.Fatalf("without subtasks, product mode must not expose %q", n)
		}
	}
}

func TestListToolsForMode_DocumentationSurface(t *testing.T) {
	names := make(map[string]bool)
	for _, d := range ListToolsForMode("documentation", Capabilities{}, true, true) {
		names[d.Function.Name] = true
	}
	// Docs Lead: repository reads + docs writes + git read-only + question/task_result.
	for _, want := range []string{"read", "write", "edit", "grep", "explore", "git.diff", "git.log", "task_result", "question"} {
		if !names[want] {
			t.Fatalf("documentation mode missing tool %q", want)
		}
	}
	// No web, no exec, no git mutation, no nested spawn.
	for _, forbid := range []string{"websearch", "webfetch", "bash", "git.commit", "git.push", "task", "task_spawn", "plan_enter"} {
		if names[forbid] {
			t.Fatalf("documentation mode must not expose %q", forbid)
		}
	}
}

func TestToolRegistry_SchemasAreValidJSON(t *testing.T) {
	defs := ListTools(Capabilities{Exec: true, Web: true, Browser: false})
	for _, d := range defs {
		if d.Type != "function" {
			t.Fatalf("unexpected tool type %q for %s", d.Type, d.Function.Name)
		}
		if d.Function.Name == "" {
			t.Fatalf("tool name is empty")
		}
		if len(d.Function.Parameters) == 0 {
			t.Fatalf("missing parameters schema for %s", d.Function.Name)
		}
		var v map[string]json.RawMessage
		if err := json.Unmarshal(d.Function.Parameters, &v); err != nil {
			t.Fatalf("invalid JSON schema for %s: %v", d.Function.Name, err)
		}
		if _, ok := v["type"]; !ok {
			t.Fatalf("schema for %s must have top-level 'type'", d.Function.Name)
		}
	}
}

// TestChildModesHaveNoRepoMutatingTools: exec consent is needed to run tests,
// and it used to advertise git.commit / git.push / gh.pr.create along with it.
// Subagents share the parent's working tree — a git.checkout in one child
// switches the branch under its siblings — and verifier's own prompt says
// read-only.
func TestChildModesHaveNoRepoMutatingTools(t *testing.T) {
	forbidden := map[string]bool{
		"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
		"git.worktree.add": true, "git.worktree.remove": true, "git.worktree.prune": true,
		"gh.pr.create": true,
	}
	caps := Capabilities{Exec: true, Web: true}
	for _, mode := range []string{"worker", "verifier", "product", "documentation", "explore", "ask", "plan"} {
		for _, name := range ToolNames(ListToolsForMode(mode, caps, true, true)) {
			if forbidden[name] {
				t.Errorf("mode %s must not advertise %s", mode, name)
			}
		}
	}
	// Verifier keeps the read-only git and exec it needs to check the work.
	verifier := map[string]bool{}
	for _, n := range ToolNames(ListToolsForMode("verifier", caps, false, false)) {
		verifier[n] = true
	}
	for _, want := range []string{"bash", "git.diff", "git.status", "read"} {
		if !verifier[want] {
			t.Errorf("verifier lost %s — it cannot verify without it", want)
		}
	}
	// Top-level user-facing modes keep them.
	build := map[string]bool{}
	for _, n := range ToolNames(ListToolsForMode("build", caps, true, true)) {
		build[n] = true
	}
	for _, want := range []string{"git.commit", "git.push", "gh.pr.create"} {
		if !build[want] {
			t.Errorf("build mode lost %s", want)
		}
	}
}

// ResolveToolNames maps short tool names to their ToolDef structs.
// Returns an error if any name is unknown. The list of valid names is the same
// set exposed in config.validAgentToolNames.
func ResolveToolNames(names []string) ([]llm.ToolDef, error) {
	return ResolveToolNamesWithPolicy(names, Capabilities{Exec: true, Web: true, Browser: true})
}
