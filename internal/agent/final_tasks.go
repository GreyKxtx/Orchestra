package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// A top-level agent used to be able to finish while its children were still
// working: relayed workers and task_spawn children kept staging edits after
// the final had flushed whatever half-done state existed at that moment, and
// what they staged later was lost with the turn. The final now waits for the
// turn's tasks — first by asking the model to collect them, then, if it tries
// to finish again, by collecting them itself.

// unfinishedTasks lists this turn's tasks that have no result yet. Only the
// top-level agent asks: it owns every task of the turn, the workers a Lead's
// batch relayed to it included.
func (a *Agent) unfinishedTasks() []TaskBoardEntry {
	if a == nil || a.opts.IsChild || a.opts.SubtaskRunner == nil {
		return nil
	}
	ar, ok := a.opts.SubtaskRunner.(AgencyRunner)
	if !ok {
		return nil
	}
	var out []TaskBoardEntry
	for _, e := range ar.Board() {
		if !e.Finished {
			out = append(out, e)
		}
	}
	return out
}

// finalWithRunningTasks handles a final that arrived while tasks run. It
// returns the message to append and true when the final must wait.
func (a *Agent) finalWithRunningTasks(ctx context.Context) (llm.Message, bool) {
	pending := a.unfinishedTasks()
	if len(pending) == 0 {
		return llm.Message{}, false
	}
	a.finalsWhileTasksRun++
	ids := make([]string, 0, len(pending))
	var list strings.Builder
	for _, e := range pending {
		ids = append(ids, e.TaskID)
		fmt.Fprintf(&list, "- %s (%s, %s): %s\n", e.TaskID, e.Agent, e.Status, e.Goal)
	}
	if a.finalsWhileTasksRun == 1 {
		return llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf(
			"Not final yet: %d task(s) of this turn are still running, and finishing now would drop their work.\n%s"+
				"Collect them with task_wait{task_ids:[...]} and use their results, or task_cancel the ones you no longer need; then finish.",
			len(pending), list.String())}, true
	}
	out, err := a.handleTaskWaitMany(ctx, ids, a.childTimeoutMS())
	body := string(out)
	if err != nil {
		body = "error: " + err.Error()
	}
	return llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf(
		"You tried to finish while these tasks ran:\n%sThe runtime waited for them; their results follow. Take them into account, then finish.\n%s",
		list.String(), body)}, true
}
