package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// TurnBudget caps a turn's tree of tasks as a whole (agent.turn_budget).
// Every child has its own step and time limits; nothing bounded the tree, so
// a Lead could respawn the same failing WorkOrder, or keep fanning out, until
// someone noticed (audit ORC-10). Zero fields are no cap.
type TurnBudget struct {
	// MaxTasks is how many tasks the turn may start.
	MaxTasks int
	// MaxTokens stops new tasks once the turn (root and children, as the
	// shared usage tracker counts them) has spent this many tokens.
	MaxTokens int
	// MaxWall is how long the tree may run from its first task; tasks still
	// running then are cancelled.
	MaxWall time.Duration
}

// ErrCauseTurnBudget marks tasks cancelled because the turn's time budget ran out.
var ErrCauseTurnBudget = errors.New("cancelled: the turn's time budget ran out (agent.turn_budget.max_wall_s)")

// maxIdenticalFailures is how many times the same task may fail in one turn
// before starting it again is refused.
const maxIdenticalFailures = 2

// tokenTotaler is the part of usage.Tracker the budget reads.
type tokenTotaler interface {
	Total() (calls, prompt, completion, total int, costUSD float64)
}

// taskFingerprint says when two spawns are the same task: the same role and
// the same goal. Whitespace does not count, and a WorkOrder's JSON is compared
// by content, not by key order or formatting.
func taskFingerprint(role, goal string) string {
	goal = strings.TrimSpace(goal)
	var v any
	if json.Valid([]byte(goal)) && json.Unmarshal([]byte(goal), &v) == nil {
		if canon, err := json.Marshal(v); err == nil {
			goal = string(canon)
		}
	} else {
		goal = strings.Join(strings.Fields(goal), " ")
	}
	return strings.ToLower(strings.TrimSpace(role)) + "\x00" + goal
}

// admitLocked decides whether a new task may start: within the turn's budget,
// not a copy of one still running, and not a third try of one that already
// failed twice. Caller holds r.mu.
func (r *TaskRunner) admitLocked(fingerprint string) error {
	b := r.child.Budget
	if b.MaxTasks > 0 && len(r.all) >= b.MaxTasks {
		return fmt.Errorf("turn budget: this turn already started %d tasks (agent.turn_budget.max_tasks) — finish with the results you have, or report what is left", len(r.all))
	}
	if b.MaxWall > 0 && !r.firstSpawn.IsZero() && time.Since(r.firstSpawn) >= b.MaxWall {
		return fmt.Errorf("turn budget: the turn's tasks have run for %s (agent.turn_budget.max_wall_s) — finish with the results you have", b.MaxWall)
	}
	if b.MaxTokens > 0 {
		if tt, ok := r.child.UsageTracker.(tokenTotaler); ok {
			if _, _, _, total, _ := tt.Total(); total >= b.MaxTokens {
				return fmt.Errorf("turn budget: the turn has spent %d tokens (agent.turn_budget.max_tokens %d) — finish with the results you have", total, b.MaxTokens)
			}
		}
	}
	var failed []string
	for _, e := range r.all {
		if e.fingerprint != fingerprint {
			continue
		}
		if e.finished.IsZero() {
			return fmt.Errorf("an identical task is already running as %s: collect it with task_wait instead of starting it again", e.id)
		}
		if e.status != "done" {
			failed = append(failed, e.id)
		}
	}
	if len(failed) >= maxIdenticalFailures {
		return fmt.Errorf("this exact task already failed %d times this turn (%s): change the goal or the WorkOrder, try another approach, or report the blocker — repeating it will fail the same way", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// startWallClockLocked starts the tree's wall clock at its first task.
// Caller holds r.mu.
func (r *TaskRunner) startWallClockLocked() {
	if !r.firstSpawn.IsZero() {
		return
	}
	r.firstSpawn = time.Now()
	if d := r.child.Budget.MaxWall; d > 0 {
		r.wallTimer = time.AfterFunc(d, r.wallClockExpired)
	}
}

// wallClockExpired cancels every task still running when the time budget
// runs out. New spawns are refused from then on by admitLocked.
func (r *TaskRunner) wallClockExpired() {
	r.mu.Lock()
	var running []*taskEntry
	for _, e := range r.all {
		if e.finished.IsZero() {
			running = append(running, e)
		}
	}
	r.mu.Unlock()
	for _, e := range running {
		e.cancel(ErrCauseTurnBudget)
	}
}

// BudgetFromConfig resolves agent.turn_budget.
func BudgetFromConfig(b config.TurnBudgetConfig) TurnBudget {
	return TurnBudget{
		MaxTasks:  b.ResolvedMaxTasks(),
		MaxTokens: b.ResolvedMaxTokens(),
		MaxWall:   b.ResolvedMaxWall(),
	}
}
