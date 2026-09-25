package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/checkpoint"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// resumableParams is what an agent.run checkpoint records of its request:
// what the run is, not what it may do. Consent — exec, web, browser — comes
// from the request that resumes it: a checkpoint lives in the project, and a
// project must not be able to hand a run the user's consent.
type resumableParams struct {
	Query             string `json:"query"`
	Mode              string `json:"mode,omitempty"`
	Profile           string `json:"profile,omitempty"`
	Apply             bool   `json:"apply,omitempty"`
	Backup            bool   `json:"backup,omitempty"`
	ApplyOutput       string `json:"apply_output,omitempty"`
	PatchPath         string `json:"patch_path,omitempty"`
	MaxSteps          int    `json:"max_steps,omitempty"`
	MaxInvalidRetries int    `json:"max_invalid_retries,omitempty"`
	MaxPromptBytes    int    `json:"max_prompt_bytes,omitempty"`
}

// applyTo makes req the run the checkpoint recorded, keeping the request's
// consent and callbacks. Attachments are not carried: their text is in the
// recorded query.
func (p resumableParams) applyTo(req AgentRunParams) AgentRunParams {
	req.Query = p.Query
	req.Mode = p.Mode
	req.Profile = p.Profile
	req.Apply = p.Apply
	req.Backup = p.Backup
	req.ApplyOutput = p.ApplyOutput
	req.PatchPath = p.PatchPath
	req.MaxSteps = p.MaxSteps
	req.MaxInvalidRetries = p.MaxInvalidRetries
	req.MaxPromptBytes = p.MaxPromptBytes
	req.Attachments = nil
	req.UserImages = nil
	return req
}

// turnCheckpoint keeps an agent.run's checkpoint current: after each step of
// the top-level agent and each change of its task graph it writes the
// history, the turn's staged edits and the graph (internal/checkpoint).
// Writing is best-effort — a checkpoint that cannot be written must not fail
// the work it records — and reported once.
type turnCheckpoint struct {
	mu     sync.Mutex
	root   string
	cp     checkpoint.Checkpoint
	turn   *tools.Turn
	tasks  *tasks.TaskRunner
	warned bool
}

func newTurnCheckpoint(root, runID string, params resumableParams, turn *tools.Turn) *turnCheckpoint {
	raw, _ := json.Marshal(params)
	return &turnCheckpoint{
		root: root,
		turn: turn,
		cp: checkpoint.Checkpoint{
			RunID:       runID,
			ProjectRoot: root,
			Status:      checkpoint.StatusRunning,
			Params:      raw,
		},
	}
}

// resumeFrom carries a resumed run's history and resume count forward.
func (k *turnCheckpoint) resumeFrom(prev *checkpoint.Checkpoint) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cp.History = prev.History
	k.cp.Resumes = prev.Resumes + 1
}

// setMode records the mode the run actually runs in — the router's choice
// for mode=agent — so a resumed run does not route again.
func (k *turnCheckpoint) setMode(mode string) {
	if strings.TrimSpace(mode) == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	var p resumableParams
	if json.Unmarshal(k.cp.Params, &p) == nil {
		p.Mode = mode
		if raw, err := json.Marshal(p); err == nil {
			k.cp.Params = raw
		}
	}
}

func (k *turnCheckpoint) setTasks(r *tasks.TaskRunner) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.tasks = r
}

// stepHistory is agent.Options.OnStepHistory: the top-level agent completed
// a step.
func (k *turnCheckpoint) stepHistory(_ int, history []llm.Message, _ bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cp.History = history
	k.saveLocked()
}

// graphChanged is tasks.ChildAgentConfig.OnGraphChange.
func (k *turnCheckpoint) graphChanged() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.saveLocked()
}

func (k *turnCheckpoint) save() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.saveLocked()
}

// finish records how the run ended. A failed run stays resumable: it goes on
// from its last completed step.
func (k *turnCheckpoint) finish(runErr error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if runErr != nil {
		k.cp.Status = checkpoint.StatusFailed
		k.cp.Error = runErr.Error()
	} else {
		k.cp.Status = checkpoint.StatusDone
		k.cp.Error = ""
	}
	k.saveLocked()
}

func (k *turnCheckpoint) saveLocked() {
	k.cp.Staged = stagedToCheckpoint(k.turn.StagedSnapshot())
	if k.tasks != nil {
		if raw, err := json.Marshal(k.tasks.Records()); err == nil {
			k.cp.Tasks = raw
		}
	}
	if err := checkpoint.Save(k.root, &k.cp); err != nil && !k.warned {
		k.warned = true
		fmt.Fprintf(os.Stderr, "core: run %s checkpoint not written (the run cannot be resumed after a crash): %v\n", k.cp.RunID, err)
	}
}

func stagedToCheckpoint(in []fs.StagedSnapshot) []checkpoint.StagedFile {
	out := make([]checkpoint.StagedFile, 0, len(in))
	for _, f := range in {
		out = append(out, checkpoint.StagedFile{Path: f.Path, Content: f.Content, DiskHash: f.DiskHash, IsNew: f.IsNew})
	}
	return out
}

func stagedFromCheckpoint(in []checkpoint.StagedFile) []fs.StagedSnapshot {
	out := make([]fs.StagedSnapshot, 0, len(in))
	for _, f := range in {
		out = append(out, fs.StagedSnapshot{Path: f.Path, Content: f.Content, DiskHash: f.DiskHash, IsNew: f.IsNew})
	}
	return out
}

// loadResumable finds the checkpoint agent.run's resume names: a run id, or
// "last" for the most recent run that can be resumed.
func (c *Core) loadResumable(ref string) (*checkpoint.Checkpoint, error) {
	var cp *checkpoint.Checkpoint
	var err error
	if strings.EqualFold(ref, "last") {
		cp, err = checkpoint.Latest(c.workspaceRoot)
	} else {
		cp, err = checkpoint.Load(c.workspaceRoot, ref)
	}
	if errors.Is(err, checkpoint.ErrNotFound) {
		return nil, protocol.NewError(protocol.InvalidParams, fmt.Sprintf("resume %q: no checkpoint of a run that can be resumed", ref), nil)
	}
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, fmt.Sprintf("resume %q: %v", ref, err), nil)
	}
	if !cp.Resumable() {
		return nil, protocol.NewError(protocol.InvalidParams, fmt.Sprintf("resume %q: the run finished; there is nothing to resume", cp.RunID), nil)
	}
	return cp, nil
}

// restoreRun puts a checkpoint back: the staged edits into the turn's
// overlay, the task graph into the runner. It returns the history the agent
// goes on from, with a note that says what came back.
func restoreRun(ctx context.Context, cp *checkpoint.Checkpoint, turn *tools.Turn, runner *tasks.TaskRunner) []llm.Message {
	turn.RestoreStaged(stagedFromCheckpoint(cp.Staged))
	var records []tasks.TaskRecord
	if len(cp.Tasks) > 0 {
		_ = json.Unmarshal(cp.Tasks, &records)
	}
	historyJSON, _ := json.Marshal(cp.History)
	// A whole id, not a prefix of a longer one: task_1_8 is not task_1_83.
	known := func(id string) bool {
		re, err := regexp.Compile(regexp.QuoteMeta(id) + `\b`)
		return err == nil && re.Match(historyJSON)
	}
	rep := runner.Restore(ctx, records, known)

	history := append([]llm.Message(nil), cp.History...)
	return append(history, llm.Message{Role: llm.RoleUser, Content: resumeNotice(len(cp.Staged), rep)})
}

func resumeNotice(staged int, rep tasks.RestoreReport) string {
	var b strings.Builder
	b.WriteString("<resume_notice>\nThe core stopped in the middle of this turn, and the turn was resumed from its checkpoint. Your history above is intact up to your last completed step; anything after it did not happen.\n")
	fmt.Fprintf(&b, "- staged edits restored: %d file(s)\n", staged)
	if len(rep.Kept) > 0 {
		fmt.Fprintf(&b, "- finished tasks, results kept (task_wait returns them): %s\n", strings.Join(rep.Kept, ", "))
	}
	if len(rep.Restarted) > 0 {
		fmt.Fprintf(&b, "- interrupted tasks, started again under the same ids: %s\n", strings.Join(rep.Restarted, ", "))
	}
	if len(rep.Dropped) > 0 {
		fmt.Fprintf(&b, "- interrupted tasks not restarted (spawn them again if you still need them): %s\n", strings.Join(rep.Dropped, ", "))
	}
	for id, why := range rep.Failed {
		fmt.Fprintf(&b, "- %s could not start again: %s\n", id, why)
	}
	b.WriteString("Continue the turn.\n</resume_notice>")
	return b.String()
}
