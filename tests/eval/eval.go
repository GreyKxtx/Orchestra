// Package eval provides the Orchestra eval harness.
// Tasks are defined in YAML, run against the agent, and scored by checks.
package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Task is a single eval task definition.
type Task struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	// Files are written to a temp workspace before the agent runs.
	Files  map[string]string `yaml:"files"`
	// Query is the prompt sent to the agent.
	Query  string            `yaml:"query"`
	// Checks define success criteria evaluated after the agent run.
	Checks []Check           `yaml:"checks"`
	// MaxSteps caps the agent loop for this task (default 12).
	MaxSteps int             `yaml:"max_steps"`
}

// Check is a single success criterion.
type Check struct {
	// Type: "file_contains", "file_exists", "file_not_contains", "file_not_exists".
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Content string `yaml:"content,omitempty"`
}

// Result records the outcome of running one task.
type Result struct {
	TaskName string
	Passed   bool
	Steps    int
	Duration time.Duration
	Error    error
	Failures []string // descriptions of failed checks
}

// Runner executes eval tasks.
type Runner struct {
	// RunAgent is the function that runs the agent on a task workspace.
	// Returns (steps, error).
	RunAgent func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error)
}

// RunTask runs a single task in an isolated temp workspace.
func (r *Runner) RunTask(ctx context.Context, task Task) Result {
	start := time.Now()
	result := Result{TaskName: task.Name}

	// Create temp workspace.
	tmpDir, err := os.MkdirTemp("", "orch-eval-*")
	if err != nil {
		result.Error = fmt.Errorf("create temp dir: %w", err)
		return result
	}
	defer os.RemoveAll(tmpDir)

	// Write initial files.
	for relPath, content := range task.Files {
		abs := filepath.Join(tmpDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			result.Error = fmt.Errorf("mkdir %s: %w", relPath, err)
			return result
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			result.Error = fmt.Errorf("write %s: %w", relPath, err)
			return result
		}
	}

	maxSteps := task.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}

	// Run agent.
	steps, err := r.RunAgent(ctx, tmpDir, task.Query, maxSteps, true)
	result.Steps = steps
	result.Duration = time.Since(start)
	if err != nil {
		result.Error = err
		return result
	}

	// Evaluate checks.
	var failures []string
	for _, check := range task.Checks {
		if f := evaluateCheck(tmpDir, check); f != "" {
			failures = append(failures, f)
		}
	}
	result.Failures = failures
	result.Passed = len(failures) == 0
	return result
}

func evaluateCheck(workspaceRoot string, c Check) string {
	abs := filepath.Join(workspaceRoot, filepath.FromSlash(c.Path))
	switch c.Type {
	case "file_exists":
		if _, err := os.Stat(abs); err != nil {
			return fmt.Sprintf("file_exists %q: %v", c.Path, err)
		}
	case "file_not_exists":
		if _, err := os.Stat(abs); err == nil {
			return fmt.Sprintf("file_not_exists %q: file exists but should not", c.Path)
		}
	case "file_contains":
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("file_contains %q: read error: %v", c.Path, err)
		}
		if !strings.Contains(string(data), c.Content) {
			return fmt.Sprintf("file_contains %q: %q not found", c.Path, c.Content)
		}
	case "file_not_contains":
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("file_not_contains %q: read error: %v", c.Path, err)
		}
		if strings.Contains(string(data), c.Content) {
			return fmt.Sprintf("file_not_contains %q: %q should not be present", c.Path, c.Content)
		}
	default:
		return fmt.Sprintf("unknown check type: %q", c.Type)
	}
	return ""
}

// LoadTasks reads task definitions from a directory of YAML files.
func LoadTasks(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read eval dir %q: %w", dir, err)
	}
	var tasks []Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var t Task
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse %q: %w", e.Name(), err)
		}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
