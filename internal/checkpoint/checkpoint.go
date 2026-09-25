// Package checkpoint keeps what a running turn needs to go on after its core
// dies: the parameters it was started with, the top-level agent's history,
// the turn's staged edits and its task graph.
//
// A turn used to live only in memory. Killed mid-way — a crash, an OOM kill,
// a laptop lid — it left the run log of what had happened and nothing that
// could continue it: the staged edits of every task that had finished, and
// the tasks themselves, were gone, and the user started again from nothing.
//
// The checkpoint is one JSON document per run, rewritten atomically whenever
// the turn moves: after each step of the top-level agent and each change of
// its task graph. A torn write leaves the previous checkpoint, never half of
// one. Resuming reads it back (core.AgentRun with resume): the staged edits
// return, finished tasks keep their results without running again, tasks the
// crash interrupted start again under their own ids, and the agent continues
// from its last completed step.
package checkpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// Version is the checkpoint format. A checkpoint of another version is not
// resumed: its fields may not mean what this build reads them as.
const Version = 1

// Status says where the run stands.
const (
	// StatusRunning: the run was going when this was written. A checkpoint
	// still saying so after its core is gone is a crash — resumable.
	StatusRunning = "running"
	// StatusFailed: the run ended with an error. Resumable: it goes on from
	// its last completed step.
	StatusFailed = "failed"
	// StatusDone: the run finished. Nothing to resume.
	StatusDone = "done"
)

// StagedFile is one staged edit of the turn: the file's content and the disk
// version it was made against, which the final apply still checks.
type StagedFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	DiskHash string `json:"disk_hash,omitempty"`
	IsNew    bool   `json:"is_new,omitempty"`
}

// Checkpoint is one run's resumable state.
type Checkpoint struct {
	Version     int    `json:"version"`
	RunID       string `json:"run_id"`
	ProjectRoot string `json:"project_root"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updated_at"`
	// Error is why a failed run ended.
	Error string `json:"error,omitempty"`

	// Params are what the run was started with — the query, the mode, apply —
	// as the core records them. Consent (exec, web, browser) is not taken
	// from here on resume: it is the resuming request's to give.
	Params json.RawMessage `json:"params"`
	// History is the top-level agent's history after its last completed step.
	History []llm.Message `json:"history,omitempty"`
	// Staged is the turn's staged edits: its own and those its tasks
	// committed. A task's uncommitted edits die with it.
	Staged []StagedFile `json:"staged,omitempty"`
	// Tasks is the task graph, as the task runner records it.
	Tasks json.RawMessage `json:"tasks,omitempty"`
	// Resumes counts how many times the run was resumed.
	Resumes int `json:"resumes,omitempty"`
}

// Resumable reports whether the run can be continued.
func (c *Checkpoint) Resumable() bool {
	return c != nil && c.Status != StatusDone
}

// Dir is where checkpoints live, beside the run logs.
func Dir(projectRoot string) string {
	return filepath.Join(projectRoot, ".orchestra", "runs")
}

// Path is a run's checkpoint file.
func Path(projectRoot, runID string) string {
	return filepath.Join(Dir(projectRoot), runID+".checkpoint.json")
}

// Save writes c atomically.
func Save(projectRoot string, c *Checkpoint) error {
	if c == nil {
		return errors.New("checkpoint: nil")
	}
	if err := sessionfile.CheckID(c.RunID); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	c.Version = Version
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal: %w", err)
	}
	return fsutil.AtomicWriteFile(Path(projectRoot, c.RunID), data, 0o600)
}

// ErrNotFound is a run with no checkpoint.
var ErrNotFound = errors.New("checkpoint: no checkpoint for this run")

// Load reads a run's checkpoint.
func Load(projectRoot, runID string) (*Checkpoint, error) {
	if err := sessionfile.CheckID(runID); err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}
	data, err := os.ReadFile(Path(projectRoot, runID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}
	var c Checkpoint
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("checkpoint %s: %w", runID, err)
	}
	if c.Version != Version {
		return nil, fmt.Errorf("checkpoint %s: format version %d, this build reads %d", runID, c.Version, Version)
	}
	return &c, nil
}

// Latest returns the most recent resumable checkpoint, or ErrNotFound.
// Run ids begin with their UTC start time, so name order is age order.
func Latest(projectRoot string) (*Checkpoint, error) {
	entries, err := os.ReadDir(Dir(projectRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".checkpoint.json"); ok && !e.IsDir() {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, id := range ids {
		c, err := Load(projectRoot, id)
		if err == nil && c.Resumable() {
			return c, nil
		}
	}
	return nil, ErrNotFound
}

// MaxKeep is how many checkpoints .orchestra/runs keeps.
const MaxKeep = 50

// Prune removes the oldest checkpoints so that at most keep remain.
// Best-effort, like the run logs beside them.
func Prune(projectRoot string, keep int) {
	entries, err := os.ReadDir(Dir(projectRoot))
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".checkpoint.json") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(Dir(projectRoot), n))
	}
}
