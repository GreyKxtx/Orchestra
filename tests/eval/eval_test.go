package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── evaluateCheck ────────────────────────────────────────────────────────────

func TestEvaluateCheck_FileContains_Pass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\nfunc Foo() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	result := evaluateCheck(dir, Check{Type: "file_contains", Path: "foo.go", Content: "func Foo"})
	if result != "" {
		t.Fatalf("expected pass (empty string), got: %q", result)
	}
}

func TestEvaluateCheck_FileContains_Fail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	result := evaluateCheck(dir, Check{Type: "file_contains", Path: "foo.go", Content: "func Bar"})
	if result == "" {
		t.Fatal("expected failure when content not present")
	}
}

func TestEvaluateCheck_FileContains_MissingFile(t *testing.T) {
	result := evaluateCheck(t.TempDir(), Check{Type: "file_contains", Path: "missing.go", Content: "x"})
	if result == "" {
		t.Fatal("expected failure for missing file")
	}
}

func TestEvaluateCheck_FileExists_Pass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "exists.go"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if result := evaluateCheck(dir, Check{Type: "file_exists", Path: "exists.go"}); result != "" {
		t.Fatalf("expected pass, got: %q", result)
	}
}

func TestEvaluateCheck_FileExists_Fail(t *testing.T) {
	if result := evaluateCheck(t.TempDir(), Check{Type: "file_exists", Path: "missing.go"}); result == "" {
		t.Fatal("expected failure for missing file")
	}
}

func TestEvaluateCheck_FileNotExists_Pass(t *testing.T) {
	if result := evaluateCheck(t.TempDir(), Check{Type: "file_not_exists", Path: "gone.go"}); result != "" {
		t.Fatalf("expected pass for absent file, got: %q", result)
	}
}

func TestEvaluateCheck_FileNotExists_Fail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "present.go"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if result := evaluateCheck(dir, Check{Type: "file_not_exists", Path: "present.go"}); result == "" {
		t.Fatal("expected failure when file exists but should not")
	}
}

func TestEvaluateCheck_FileNotContains_Pass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	if result := evaluateCheck(dir, Check{Type: "file_not_contains", Path: "f.go", Content: "forbidden"}); result != "" {
		t.Fatalf("expected pass, got: %q", result)
	}
}

func TestEvaluateCheck_FileNotContains_Fail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	if result := evaluateCheck(dir, Check{Type: "file_not_contains", Path: "f.go", Content: "hello"}); result == "" {
		t.Fatal("expected failure when forbidden content is present")
	}
}

func TestEvaluateCheck_UnknownType(t *testing.T) {
	result := evaluateCheck(t.TempDir(), Check{Type: "bogus_type", Path: "x"})
	if result == "" {
		t.Fatal("expected failure for unknown check type")
	}
}

// ── LoadTasks ────────────────────────────────────────────────────────────────

func TestLoadTasks_ReadsYAML(t *testing.T) {
	dir := t.TempDir()
	content := `name: my_task
description: test task
query: "do something"
max_steps: 5
files:
  go.mod: |
    module testpkg
    go 1.21
checks:
  - type: file_exists
    path: result.go
`
	if err := os.WriteFile(filepath.Join(dir, "my_task.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tasks, err := LoadTasks(dir)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Name != "my_task" {
		t.Fatalf("expected name=my_task, got %q", task.Name)
	}
	if task.Query != "do something" {
		t.Fatalf("expected query='do something', got %q", task.Query)
	}
	if task.MaxSteps != 5 {
		t.Fatalf("expected max_steps=5, got %d", task.MaxSteps)
	}
	if len(task.Checks) != 1 || task.Checks[0].Type != "file_exists" {
		t.Fatalf("unexpected checks: %+v", task.Checks)
	}
	if _, ok := task.Files["go.mod"]; !ok {
		t.Fatal("expected go.mod in files")
	}
}

func TestLoadTasks_FallbackNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	// Task without a name field — should use filename stem
	if err := os.WriteFile(filepath.Join(dir, "rename_func.yaml"), []byte(`query: "rename it"`), 0644); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadTasks(dir)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "rename_func" {
		t.Fatalf("expected name=rename_func from filename, got %q", tasks[0].Name)
	}
}

func TestLoadTasks_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a task"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte("name: t\nquery: q\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadTasks(dir)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	for _, task := range tasks {
		if strings.Contains(task.Name, "notes") {
			t.Fatal("expected .txt file to be skipped")
		}
	}
}

func TestLoadTasks_EmptyDir(t *testing.T) {
	tasks, err := LoadTasks(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestLoadTasks_InvalidDir(t *testing.T) {
	_, err := LoadTasks("/nonexistent/path/xyz123abc")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

// ── RunTask ──────────────────────────────────────────────────────────────────

func TestRunTask_PassesWhenChecksPass(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
			if err := os.WriteFile(filepath.Join(workspaceRoot, "result.go"), []byte("package main"), 0644); err != nil {
				return 0, err
			}
			return 3, nil
		},
	}

	task := Task{
		Name:     "test_pass",
		Query:    "do it",
		MaxSteps: 5,
		Checks:   []Check{{Type: "file_exists", Path: "result.go"}},
	}

	result := runner.RunTask(context.Background(), task)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.Passed {
		t.Fatalf("expected task to pass, failures: %v", result.Failures)
	}
	if result.Steps != 3 {
		t.Fatalf("expected 3 steps, got %d", result.Steps)
	}
}

func TestRunTask_FailsWhenChecksFail(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
			return 2, nil // doesn't create required file
		},
	}

	task := Task{
		Name:   "test_fail",
		Query:  "do it",
		Checks: []Check{{Type: "file_exists", Path: "missing.go"}},
	}

	result := runner.RunTask(context.Background(), task)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Passed {
		t.Fatal("expected task to fail")
	}
	if len(result.Failures) == 0 {
		t.Fatal("expected non-empty failures")
	}
}

func TestRunTask_WritesInitialFiles(t *testing.T) {
	var seenContent string
	runner := &Runner{
		RunAgent: func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
			data, err := os.ReadFile(filepath.Join(workspaceRoot, "main.go"))
			if err != nil {
				return 0, err
			}
			seenContent = string(data)
			return 1, nil
		},
	}

	task := Task{
		Name:  "file_setup",
		Query: "check it",
		Files: map[string]string{"main.go": "package main"},
	}

	result := runner.RunTask(context.Background(), task)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if seenContent != "package main" {
		t.Fatalf("expected file content 'package main', got %q", seenContent)
	}
}

func TestRunTask_DefaultMaxSteps(t *testing.T) {
	var capturedMaxSteps int
	runner := &Runner{
		RunAgent: func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
			capturedMaxSteps = maxSteps
			return 1, nil
		},
	}

	task := Task{
		Name:     "default_steps",
		Query:    "go",
		MaxSteps: 0, // should default to 12
	}

	runner.RunTask(context.Background(), task)
	if capturedMaxSteps != 12 {
		t.Fatalf("expected default max_steps=12, got %d", capturedMaxSteps)
	}
}

func TestRunTask_DurationIsSet(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
			return 1, nil
		},
	}
	task := Task{Name: "dur", Query: "go"}
	result := runner.RunTask(context.Background(), task)
	if result.Duration < 0 {
		t.Fatal("expected non-negative duration")
	}
}
