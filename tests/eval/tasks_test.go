package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tasks themselves need checking, because a broken one is invisible: it
// runs, it reports, and the number it contributes means nothing.
//
// Three ways a task goes wrong, all of them found here rather than two hours
// into a run:
//
//   - a check type nobody implements, which reports "unknown check type" as
//     a failure and makes the task unpassable;
//   - a fixture that does not say what it means — a typo turned `return 30`
//     into `st = 30` in this very directory, which would have failed every
//     model on a defect in the task;
//   - a fixture that already satisfies the task's own checks, which passes
//     whatever the model does, including nothing.

func loadRepoTasks(t *testing.T) []Task {
	t.Helper()
	tasks, err := LoadTasks("tasks")
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no tasks found")
	}
	return tasks
}

// knownCheckTypes is the set evaluateCheck can answer. A task naming anything
// else can never pass.
var knownCheckTypes = map[string]bool{
	"file_exists": true, "file_not_exists": true,
	"file_contains": true, "file_not_contains": true,
	"file_matches": true, "file_not_matches": true,
	"go_build": true, "go_test": true,
	"file_unchanged": true, "workspace_unchanged": true,
}

func TestTasks_AreWellFormed(t *testing.T) {
	for _, task := range loadRepoTasks(t) {
		t.Run(task.Name, func(t *testing.T) {
			if strings.TrimSpace(task.Query) == "" {
				t.Error("a task with no query asks the model nothing")
			}
			if len(task.Checks) == 0 {
				t.Error("a task with no checks passes unconditionally")
			}
			if len(task.Files) == 0 {
				t.Error("a task with no files gives the model an empty workspace")
			}
			for _, c := range task.Checks {
				if !knownCheckTypes[c.Type] {
					t.Errorf("check type %q is not implemented, so this task can never pass", c.Type)
				}
				switch c.Type {
				case "file_matches", "file_not_matches":
					if c.Pattern == "" {
						t.Errorf("%s on %q has no pattern", c.Type, c.Path)
					}
				case "file_contains", "file_not_contains":
					if c.Content == "" {
						t.Errorf("%s on %q has no content", c.Type, c.Path)
					}
				case "file_exists", "file_not_exists", "file_unchanged":
					if c.Path == "" {
						t.Errorf("%s has no path", c.Type)
					}
				}
			}
		})
	}
}

// A fixture that already passes the task's own checks makes the task
// worthless: the model can do nothing at all and still be scored correct.
// This runs each task's checks against its UNTOUCHED fixture and requires at
// least one to fail.
func TestTasks_AreNotAlreadySatisfiedByTheirOwnFixture(t *testing.T) {
	for _, task := range loadRepoTasks(t) {
		t.Run(task.Name, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range task.Files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env := checkEnv{root: root, original: task.Files}
			var passed []string
			satisfied := true
			for _, c := range task.Checks {
				if failure := evaluateCheck(env, c); failure == "" {
					passed = append(passed, c.Type)
				} else {
					satisfied = false
				}
			}
			if satisfied {
				t.Errorf("the fixture already passes every check (%s) — the model has nothing to do",
					strings.Join(passed, ", "))
			}
		})
	}
}

// The fixture has to be in the state the task's wording claims. fix_bug says
// there is a bug; fix_compile_error says the project does not build. If the
// fixture is fine, the task is a lie and its result is noise.
func TestTasks_FixCompileErrorStartsWithAWorkspaceThatDoesNotBuild(t *testing.T) {
	var task *Task
	for _, candidate := range loadRepoTasks(t) {
		if candidate.Name == "fix_compile_error" {
			c := candidate
			task = &c
			break
		}
	}
	if task == nil {
		t.Skip("fix_compile_error is not in this task set")
	}
	root := t.TempDir()
	for rel, body := range task.Files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runGo(root, "build", "./..."); err == nil {
		t.Error("the fixture compiles, so the task asks the model to fix nothing")
	}
}
