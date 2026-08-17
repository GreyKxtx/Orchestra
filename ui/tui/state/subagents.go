package state

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SubagentTask is the TUI model for one child worker / explore / debug / ask task.
type SubagentTask struct {
	TaskID        string
	Role          string // worker, explore, debug, ask
	Goal          string
	Status        string // queued, running, verifying, done, failed
	Iterations    int
	StartTime     time.Time
	Duration      time.Duration
	ResultSummary string
	WaitingReason string
}

// SubagentTracker aggregates child_started / child_queued / child_done and
// scoped child tool events for the current Lead turn.
type SubagentTracker struct {
	mu    sync.Mutex
	order []string
	byID  map[string]*trackedSubagent
}

type trackedSubagent struct {
	task SubagentTask
	logs []string
	done time.Time
}

// NewSubagentTracker returns an empty tracker.
func NewSubagentTracker() *SubagentTracker {
	return &SubagentTracker{byID: make(map[string]*trackedSubagent)}
}

// Reset clears all tracked children (call at the start of a new user turn).
func (t *SubagentTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.order = t.order[:0]
	t.byID = make(map[string]*trackedSubagent)
}

// HasActive reports whether any child is queued, running, or verifying.
func (t *SubagentTracker) HasActive() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range t.order {
		e := t.byID[id]
		if e == nil {
			continue
		}
		switch e.task.Status {
		case "queued", "running", "verifying":
			return true
		}
	}
	return false
}

// OnQueued records a child_queued event (overlapping target_files).
func (t *SubagentTracker) OnQueued(taskID, role, goal, reason string) {
	if t == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.ensureLocked(taskID, role, goal)
	if e.task.Status == "" || e.task.Status == "queued" {
		e.task.Status = "queued"
	}
	if reason != "" {
		e.task.WaitingReason = reason
	}
	if e.task.Goal == "" {
		e.task.Goal = shortSubagentGoal(goal)
	}
}

// OnStarted records a child_started event.
func (t *SubagentTracker) OnStarted(taskID, role, goal string, at time.Time) {
	if t == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.ensureLocked(taskID, role, goal)
	e.task.Status = "running"
	e.task.StartTime = at
	e.task.WaitingReason = ""
	if g := shortSubagentGoal(goal); g != "" {
		e.task.Goal = g
	}
}

// OnDone records a child_done event.
func (t *SubagentTracker) OnDone(taskID, role, childStatus, summary, errText string, at time.Time) {
	if t == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.ensureLocked(taskID, role, "")
	failed := childStatus != "" && childStatus != "done" && childStatus != "ok" && childStatus != "success"
	if failed {
		e.task.Status = "failed"
	} else {
		e.task.Status = "done"
	}
	if e.task.StartTime.IsZero() {
		e.task.StartTime = at
	}
	e.done = at
	e.task.Duration = at.Sub(e.task.StartTime)
	e.task.WaitingReason = ""
	if summary != "" {
		e.task.ResultSummary = truncateRunes(summary, 160)
	} else if errText != "" {
		e.task.ResultSummary = truncateRunes(errText, 160)
	}
}

// AppendLog stores a child-scoped log line without exposing it in Lead chat.
func (t *SubagentTracker) AppendLog(taskID, line string) {
	if t == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(line) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.ensureLocked(taskID, "", "")
	if len(e.logs) < 64 {
		e.logs = append(e.logs, truncateRunes(line, 240))
	}
}

// OnChildTool records a scoped child tool event (iterations / verifying).
func (t *SubagentTracker) OnChildTool(taskID, toolName, kind string) {
	if t == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.ensureLocked(taskID, "", "")
	if e.task.Status == "queued" {
		e.task.Status = "running"
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	if kind == "completed" && (strings.Contains(name, "lsp") || name == "bash") {
		e.task.Iterations++
		if strings.Contains(name, "lsp") && e.task.Status == "running" {
			e.task.Status = "verifying"
		}
	}
}

// Snapshot copies tasks with live Duration for running children.
func (t *SubagentTracker) Snapshot(now time.Time) []SubagentTask {
	if t == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.order) == 0 {
		return nil
	}
	out := make([]SubagentTask, 0, len(t.order))
	for _, id := range t.order {
		e := t.byID[id]
		if e == nil {
			continue
		}
		task := e.task
		switch task.Status {
		case "queued", "running", "verifying":
			if !task.StartTime.IsZero() {
				task.Duration = now.Sub(task.StartTime)
			}
		}
		out = append(out, task)
	}
	return out
}

func (t *SubagentTracker) ensureLocked(taskID, role, goal string) *trackedSubagent {
	if e, ok := t.byID[taskID]; ok {
		if role != "" && e.task.Role == "" {
			e.task.Role = normalizeSubagentRole(role)
		}
		return e
	}
	e := &trackedSubagent{task: SubagentTask{
		TaskID: taskID,
		Role:   normalizeSubagentRole(role),
		Goal:   shortSubagentGoal(goal),
		Status: "queued",
	}}
	t.byID[taskID] = e
	t.order = append(t.order, taskID)
	return e
}

func normalizeSubagentRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "explore":
		return "explore"
	case "debug":
		return "debug"
	case "ask":
		return "ask"
	case "worker", "":
		return "worker"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func shortSubagentGoal(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if json.Valid([]byte(raw)) {
		var wo struct {
			Intent      string   `json:"intent"`
			TargetFile  string   `json:"target_file"`
			TargetFiles []string `json:"target_files"`
		}
		if json.Unmarshal([]byte(raw), &wo) == nil {
			if wo.TargetFile != "" {
				return wo.TargetFile
			}
			if len(wo.TargetFiles) > 0 {
				return wo.TargetFiles[0]
			}
			if wo.Intent != "" {
				return wo.Intent
			}
		}
	}
	return truncateRunes(strings.ReplaceAll(raw, "\n", " "), 80)
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
