package tools

import (
	"encoding/json"
	"testing"

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
			"plan_enter", "plan_exit", "question", "diff.preview"}, 24, false},
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

func TestListToolsForMode_BuildNoPlanEnter(t *testing.T) {
	for _, d := range ListToolsForMode("build", Capabilities{}, true, true) {
		if d.Function.Name == "plan_enter" {
			t.Fatal("build mode must not advertise plan_enter")
		}
	}
}

func TestListToolsForMode_OrchestraLeadSurface(t *testing.T) {
	names := make(map[string]bool)
	for _, d := range ListToolsForMode("orchestra", Capabilities{}, true, true) {
		names[d.Function.Name] = true
	}
	for _, want := range []string{"read", "write", "task", "repo_map", "explore"} {
		if !names[want] {
			t.Fatalf("orchestra mode missing tool %q", want)
		}
	}
	for _, forbid := range []string{"edit", "plan_enter"} {
		if names[forbid] {
			t.Fatalf("orchestra mode must not expose %q", forbid)
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
	// No code execution, no git mutation, no nested spawn.
	for _, forbid := range []string{"bash", "git.commit", "git.push", "task", "task_spawn", "plan_enter"} {
		if names[forbid] {
			t.Fatalf("product mode must not expose %q", forbid)
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
