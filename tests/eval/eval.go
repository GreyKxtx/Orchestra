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
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Files are written to a temp workspace before the agent runs.
	Files map[string]string `yaml:"files"`
	// Query is the prompt sent to the agent.
	Query string `yaml:"query"`
	// Checks define success criteria evaluated after the agent run.
	Checks []Check `yaml:"checks"`
	// MaxSteps caps the agent loop for this task (default 12).
	MaxSteps int `yaml:"max_steps"`
	// Mode selects the agent mode ("build", "ask", "plan", "debug", ...).
	// Empty leaves the core's default, which is what every task used before
	// modes were gradable.
	Mode string `yaml:"mode,omitempty"`
	// Apply decides whether the run writes to disk. A pointer because the
	// default is true and "apply: false" has to be distinguishable from
	// "apply not mentioned": a task that grades "you changed nothing" is only
	// worth anything if the run was allowed to try.
	Apply *bool `yaml:"apply,omitempty"`
}

// AgentRun is one request to the agent under test.
type AgentRun struct {
	WorkspaceRoot string
	Query         string
	MaxSteps      int
	Apply         bool
	Mode          string
}

// AgentOutcome is what came back.
//
// Answer is the prose the user would have seen, accumulated from the run's
// message_delta events exactly as the editor and web hosts accumulate it. It
// is not read off a result field, because there is none: neither agent.Result
// nor core.AgentRunResult carries the final text — the answer is streamed,
// and every consumer builds it the same way. A task that grades what the
// model SAID rather than what it wrote needs this and nothing else.
type AgentOutcome struct {
	Steps  int
	Answer string
}

// Check is a single success criterion.
type Check struct {
	// Type is one of:
	//
	//   file_exists / file_not_exists / file_contains / file_not_contains
	//       the substring checks; cheap, and unable to tell correct work
	//       from work that merely contains the right words.
	//   file_matches / file_not_matches
	//       regexp against Pattern, for asserting a shape — a method with
	//       the right receiver, a number converted rather than concatenated.
	//   go_build / go_test
	//       the workspace compiles, and its tests pass. Content optionally
	//       names the package pattern (default "./...").
	//   file_unchanged / workspace_unchanged
	//       the file, or every file the task wrote, is byte-identical to
	//       what the task wrote. What a read-only task needs to prove.
	//
	// See checks.go for why the mechanical ones exist.
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Content string `yaml:"content,omitempty"`
	Pattern string `yaml:"pattern,omitempty"`
}

// Result records the outcome of running one task.
type Result struct {
	TaskName       string
	Passed         bool
	Steps          int
	InvalidRetries int // validation_error events from llm_log
	ResolveFailed  int // resolve_failed events from llm_log
	ToolCalls      int
	Duration       time.Duration
	// Answer is the prose the model produced, for the checks that grade it.
	Answer   string
	Error    error
	Failures []string // descriptions of failed checks
}

// Runner executes eval tasks.
type Runner struct {
	// RunAgent runs the agent on a task workspace.
	RunAgent func(ctx context.Context, run AgentRun) (AgentOutcome, error)
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

	apply := true
	if task.Apply != nil {
		apply = *task.Apply
	}

	// Run agent.
	outcome, err := r.RunAgent(ctx, AgentRun{
		WorkspaceRoot: tmpDir,
		Query:         task.Query,
		MaxSteps:      maxSteps,
		Apply:         apply,
		Mode:          task.Mode,
	})
	result.Steps = outcome.Steps
	result.Answer = outcome.Answer
	result.Duration = time.Since(start)
	if err != nil {
		result.Error = err
		return result
	}

	if metrics, mErr := ParseLLMLog(filepath.Join(tmpDir, ".orchestra", "llm_log.jsonl")); mErr == nil {
		result.InvalidRetries = metrics.ValidationErrors
		result.ResolveFailed = metrics.ResolveFailed
		result.ToolCalls = metrics.ToolCalls
	}

	// Evaluate checks. The task's own Files are carried along so a check can
	// compare against what the workspace started as, not merely against what
	// it now contains.
	env := checkEnv{root: tmpDir, original: task.Files, answer: outcome.Answer}
	var failures []string
	for _, check := range task.Checks {
		if f := evaluateCheck(env, check); f != "" {
			failures = append(failures, f)
		}
	}
	result.Failures = failures
	result.Passed = len(failures) == 0
	return result
}

func evaluateCheck(env checkEnv, c Check) string {
	if failure, handled := evaluateMechanicalCheck(env, c); handled {
		return failure
	}
	abs := env.abs(c.Path)
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
