package eval

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// taskFiles is embedded so that the task set is part of the BUILD, and it is
// embedded for that reason before it is used for any other.
//
// go test caches a passing run keyed partly on the files the test opened.
// LoadTasks opens every task, so editing one invalidates the cache — but it
// FINDS them with os.ReadDir, and a directory read is not tracked. Adding a
// task therefore did not invalidate anything: the validator answered
// "ok (cached)" for a task file it had never opened. That is the same defect
// as a fixture that cannot link and a {plan} check that cannot fail — a check
// that reports success without having looked.
//
//go:embed tasks
var taskFiles embed.FS

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
	"answer_matches": true, "answer_not_matches": true,
	"answer_not_empty": true, "answer_invents_no_path": true,
}

// And since the files are embedded anyway, they may as well be counted: a
// task LoadTasks does not pick up is a task nobody runs, and it fails
// silently — the suite simply reports one fewer result than the directory
// holds, which nobody notices.
func TestTasks_EveryFileInTheDirectoryIsLoaded(t *testing.T) {
	entries, err := taskFiles.ReadDir("tasks")
	if err != nil {
		t.Fatalf("read embedded tasks: %v", err)
	}
	var onDisk []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
			onDisk = append(onDisk, e.Name())
		}
	}
	loaded := loadRepoTasks(t)
	if len(loaded) != len(onDisk) {
		t.Errorf("the directory holds %d task files and LoadTasks returned %d — something is being skipped\nfiles: %s",
			len(onDisk), len(loaded), strings.Join(onDisk, ", "))
	}
	seen := map[string]bool{}
	for _, task := range loaded {
		if seen[task.Name] {
			t.Errorf("two tasks are both named %q, so one of them is reported under the other's name", task.Name)
		}
		seen[task.Name] = true
	}
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
			// A fixture that says `package main` and defines no `func main`
			// cannot be linked, so its go_build check fails on the untouched
			// workspace and on every workspace the model could produce. This
			// is the static half of the net — it also covers the task that is
			// ALLOWED not to compile, where the compile check cannot tell the
			// intended error from this one.
			declaresMain, definesMain := false, false
			for _, body := range task.Files {
				if strings.Contains(body, "package main") {
					declaresMain = true
				}
				if strings.Contains(body, "func main(") {
					definesMain = true
				}
			}
			if declaresMain && !definesMain {
				t.Error("the fixture is package main with no func main, so it cannot link")
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
				case "answer_matches", "answer_not_matches":
					if c.Pattern == "" {
						t.Errorf("%s has no pattern", c.Type)
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

// startsBroken names the tasks whose fixture is MEANT not to compile, because
// making it compile is the task. Every other fixture must build.
var startsBroken = map[string]bool{"fix_compile_error": true}

// The fixture has to be in the state the task's wording claims, in both
// directions — and both directions have now cost a run.
//
// fix_compile_error says the project does not build: if its fixture were fine,
// the task would ask the model to fix nothing.
//
// The other way round is the one that got through. Three fixtures declared
// `package main` and defined no `func main`, so `go build ./...` failed on the
// untouched workspace with "function main is undeclared in the main package" —
// and went on failing whatever the model wrote. They ran as three FAILs in a
// thirteen-task suite and read exactly like model errors.
//
// TestTasks_AreNotAlreadySatisfiedByTheirOwnFixture cannot see this: it asks
// for at least one check to fail on the fixture, and a go_build that can never
// pass satisfies that requirement perfectly.
func TestTasks_FixturesCompileUnlessTheTaskIsToFixTheBuild(t *testing.T) {
	for _, task := range loadRepoTasks(t) {
		t.Run(task.Name, func(t *testing.T) {
			if _, ok := task.Files["go.mod"]; !ok {
				t.Skip("not a Go module, so there is nothing to compile")
			}
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
			out, err := runGo(root, "build", "./...")
			if startsBroken[task.Name] {
				if err == nil {
					t.Error("the fixture compiles, so the task asks the model to fix nothing")
				}
				return
			}
			if err != nil {
				t.Errorf("the fixture does not compile, so go_build can never pass: %s (%v)",
					firstToolchainError(out), err)
			}
		})
	}
}
