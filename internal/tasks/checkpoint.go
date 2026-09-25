package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// The task graph in a checkpoint (internal/checkpoint).
//
// A turn's tasks lived only in the runner's memory: a core that died took
// every finished task's result with it, and the turn could not go on. The
// runner records each task — what it was started with, where it stands, its
// result — and a resumed turn gets them back: a finished task keeps its
// result and does not run again, and a task the crash interrupted starts
// again under the id its spawner knows. Its layer, with any edits it had not
// committed, died with it.

// spawnInputs is what a task was started with.
type spawnInputs struct {
	req   agent.SubtaskSpawnRequest
	from  agentScope
	extra spawnExtra
}

// ScopeRecord is an agentScope as a checkpoint keeps it.
type ScopeRecord struct {
	Address      string   `json:"address"`
	Role         string   `json:"role,omitempty"`
	Name         string   `json:"name,omitempty"`
	Dept         string   `json:"dept,omitempty"`
	Depth        int      `json:"depth,omitempty"`
	Chain        []string `json:"chain,omitempty"`
	TaskID       string   `json:"task_id,omitempty"`
	ParentTaskID string   `json:"parent_task_id,omitempty"`
}

func scopeRecordOf(s agentScope) ScopeRecord {
	return ScopeRecord{Address: s.address, Role: s.role, Name: s.name, Dept: s.dept, Depth: s.depth,
		Chain: append([]string(nil), s.chain...), TaskID: s.taskID, ParentTaskID: s.parentTaskID}
}

func (s ScopeRecord) scope() agentScope {
	return agentScope{address: s.Address, role: s.Role, name: s.Name, dept: s.Dept, depth: s.Depth,
		chain: append([]string(nil), s.Chain...), taskID: s.TaskID, parentTaskID: s.ParentTaskID}
}

// TaskRecord is one task as a checkpoint keeps it: enough to show it, hand
// out its result, or start it again.
type TaskRecord struct {
	ID           string               `json:"id"`
	Key          string               `json:"key,omitempty"`
	Spawner      string               `json:"spawner,omitempty"`
	Status       string               `json:"status"`
	Result       *agent.SubtaskResult `json:"result,omitempty"`
	Address      string               `json:"address"`
	Role         string               `json:"role,omitempty"`
	Parent       string               `json:"parent,omitempty"`
	ParentTaskID string               `json:"parent_task_id,omitempty"`
	Depth        int                  `json:"depth,omitempty"`
	Worker       bool                 `json:"worker,omitempty"`
	Goal         string               `json:"goal,omitempty"`
	Fingerprint  string               `json:"fingerprint,omitempty"`
	Edited       []string             `json:"edited,omitempty"`
	Started      time.Time            `json:"started"`
	Finished     time.Time            `json:"finished,omitempty"`

	// What the task was started with.
	Request agent.SubtaskSpawnRequest `json:"request"`
	From    ScopeRecord               `json:"from"`
	Owner   *ScopeRecord              `json:"owner,omitempty"`
	History []llm.Message             `json:"history,omitempty"`
	Verb    string                    `json:"verb,omitempty"`
}

// finished reports whether the task had ended when it was recorded.
func (t TaskRecord) finished() bool {
	return !t.Finished.IsZero() && t.Result != nil
}

// Records returns every task of the turn, in the order they were started.
func (r *TaskRunner) Records() []TaskRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TaskRecord, 0, len(r.all))
	for _, e := range r.all {
		rec := TaskRecord{
			ID: e.id, Key: e.key, Spawner: e.spawner, Status: e.status,
			Address: e.address, Role: e.role, Parent: e.parent, ParentTaskID: e.parentTaskID,
			Depth: e.depth, Worker: e.worker, Goal: e.goal, Fingerprint: e.fingerprint,
			Edited: append([]string(nil), e.edited...), Started: e.started, Finished: e.finished,
			Request: e.spawned.req, From: scopeRecordOf(e.spawned.from),
			History: e.spawned.extra.history, Verb: e.spawned.extra.verb,
		}
		if e.result != nil {
			res := *e.result
			rec.Result = &res
		}
		if o := e.spawned.extra.owner; o != nil {
			sr := scopeRecordOf(*o)
			rec.Owner = &sr
		}
		out = append(out, rec)
	}
	return out
}

// RestoreReport says what Restore did with each task.
type RestoreReport struct {
	// Kept are finished tasks: their results stand, they do not run again.
	Kept []string
	// Restarted are interrupted tasks started again under their ids.
	Restarted []string
	// Dropped are interrupted tasks not started again: their spawner was
	// interrupted too and starts its own anew, or the top-level agent never
	// learned their id.
	Dropped []string
	// Failed are interrupted tasks that could not start again, with why.
	Failed map[string]string
}

// Restore puts a checkpoint's tasks back into an empty runner. known reports
// whether the top-level agent's restored history mentions a task id: a task
// it spawned in a step the crash cut short is one it will spawn again itself.
// ctx is the turn's; restarted tasks run under it.
func (r *TaskRunner) Restore(ctx context.Context, records []TaskRecord, known func(taskID string) bool) RestoreReport {
	var rep RestoreReport
	if r == nil {
		return rep
	}
	finished := map[string]bool{}
	r.mu.Lock()
	for _, rec := range records {
		if !rec.finished() {
			continue
		}
		finished[rec.ID] = true
		res := *rec.Result
		e := &taskEntry{
			id: rec.ID, done: make(chan struct{}), result: &res, status: rec.Status,
			key: rec.Key, spawner: rec.Spawner, address: rec.Address, role: rec.Role,
			parent: rec.Parent, parentTaskID: rec.ParentTaskID, depth: rec.Depth,
			worker: rec.Worker, goal: rec.Goal, fingerprint: rec.Fingerprint,
			edited: append([]string(nil), rec.Edited...), started: rec.Started, finished: rec.Finished,
			cancel: func(error) {},
		}
		close(e.done)
		r.all = append(r.all, e)
		if e.key != "" {
			r.byKey[keyOf(e.spawner, e.key)] = e
		}
		rep.Kept = append(rep.Kept, rec.ID)
	}
	if r.seq < len(records) {
		r.seq = len(records)
	}
	r.mu.Unlock()

	for _, rec := range records {
		if rec.finished() {
			continue
		}
		switch {
		case rec.Spawner == "" && (known == nil || !known(rec.ID)):
			rep.Dropped = append(rep.Dropped, rec.ID)
			continue
		case rec.Spawner != "" && !finished[rec.Spawner]:
			rep.Dropped = append(rep.Dropped, rec.ID)
			continue
		}
		extra := spawnExtra{id: rec.ID, history: rec.History, verb: rec.Verb}
		if rec.Owner != nil {
			o := rec.Owner.scope()
			extra.owner = &o
		}
		if _, err := r.spawnFrom(ctx, rec.From.scope(), rec.Request, extra); err != nil {
			if rep.Failed == nil {
				rep.Failed = map[string]string{}
			}
			rep.Failed[rec.ID] = err.Error()
			r.recordUnrestartable(rec, err)
			continue
		}
		rep.Restarted = append(rep.Restarted, rec.ID)
	}
	r.graphChanged()
	return rep
}

// recordUnrestartable keeps an interrupted task that could not start again
// as a failed one, so a spawner waiting for it gets an answer, not a hang.
func (r *TaskRunner) recordUnrestartable(rec TaskRecord, why error) {
	e := &taskEntry{
		id: rec.ID, done: make(chan struct{}), status: "error", key: rec.Key, spawner: rec.Spawner,
		address: rec.Address, role: rec.Role, parent: rec.Parent, parentTaskID: rec.ParentTaskID,
		depth: rec.Depth, worker: rec.Worker, goal: rec.Goal, fingerprint: rec.Fingerprint,
		started: rec.Started, finished: time.Now(), cancel: func(error) {},
		result: &agent.SubtaskResult{TaskID: rec.ID, Status: "error",
			Error: fmt.Sprintf("interrupted by a crash of the core, and could not start again: %v", why)},
	}
	close(e.done)
	r.mu.Lock()
	r.all = append(r.all, e)
	r.mu.Unlock()
}

// graphChanged tells the checkpoint the task graph moved.
func (r *TaskRunner) graphChanged() {
	if r != nil && r.child.OnGraphChange != nil {
		r.child.OnGraphChange()
	}
}
