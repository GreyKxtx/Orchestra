package tasks

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/task"
)

// A child shares the parent's Runner, and therefore the parent's working tree.
// The registry says what follows from that where it gates the repo-mutating
// tools: "Only user-facing top-level modes get these. A subagent must not have
// them: children share the parent's working tree, so a git.checkout in one
// child switches the branch under every sibling and the parent."
//
// child_mode_surface_test.go pins that for four of the nine subagent types the
// task tool accepts. The other five — explore, ask, debug, architecture and
// general — were never checked, and general is a top-level mode reused as a
// child, which is exactly where the repo-mutating set would come from.
//
// So this reads the types out of the schema the model is shown, instead of
// listing them again by hand: a tenth type added to the enum is covered the
// day it is added. The same drift, spelled by hand, is what left five of nine
// unchecked here and three statuses missing from looksLikeWorkerResult.

func subagentTypesFromSchema(t *testing.T) []string {
	t.Helper()
	var schema struct {
		Properties struct {
			SubagentType struct {
				Enum []string `json:"enum"`
			} `json:"subagent_type"`
		} `json:"properties"`
	}
	raw, err := json.Marshal(task.ToolTask().Function.Parameters)
	if err != nil {
		t.Fatalf("marshal task schema: %v", err)
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("read subagent_type enum: %v\n%s", err, raw)
	}
	got := schema.Properties.SubagentType.Enum
	if len(got) < 2 {
		t.Fatalf("the subagent_type enum moved or lost its values; this guard now checks "+
			"nothing: %s", raw)
	}
	return got
}

// The tools a child is actually handed, through the same function the runner
// uses — not through ListToolsForMode, which is one layer earlier.
func childTools(t *testing.T, subagentType string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, d := range childToolsForSubagent(subagentType, tools.Capabilities{Exec: true, Web: true, Browser: true}) {
		out[d.Function.Name] = true
	}
	return out
}

func TestChildToolsForSubagent_NoTypeGetsRepoMutatingGitOrNestedSpawn(t *testing.T) {
	forbidden := []string{
		"git.commit", "git.checkout", "git.push", "git.branch",
		"git.worktree.add", "git.worktree.remove", "git.worktree.prune",
		"gh.pr.create",
		"task", "task_spawn", "task_wait", "task_cancel",
	}
	for _, typ := range subagentTypesFromSchema(t) {
		t.Run(typ, func(t *testing.T) {
			got := childTools(t, typ)
			for _, name := range forbidden {
				if got[name] {
					t.Errorf("a %q child is handed %q with every capability on. It shares the "+
						"parent's working tree: this switches branches, publishes, or spawns "+
						"depth under the parent and every sibling.", typ, name)
				}
			}
		})
	}
}

// The default. An empty subagent_type is legal — the schema says "default:
// explore" — and it takes its own branch in childToolsForSubagent, so it is
// not covered by the enum above.
func TestChildToolsForSubagent_TheDefaultTypeIsAsConstrainedAsTheNamedOnes(t *testing.T) {
	got := childTools(t, "")
	for _, name := range []string{"git.commit", "git.checkout", "git.push", "task_spawn"} {
		if got[name] {
			t.Errorf("a child spawned with no subagent_type is handed %q", name)
		}
	}
	if !got["task_result"] {
		t.Error("a default child cannot answer its parent: no task_result")
	}
}
