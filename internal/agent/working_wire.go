package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent/working"
)

func (a *Agent) workingStateEnabled() bool {
	if a.opts.WorkingState == nil {
		return true
	}
	return *a.opts.WorkingState
}

func (a *Agent) initWorkingState(userQuery string) {
	if !a.workingStateEnabled() {
		a.working = nil
		return
	}
	a.working = working.New(userQuery)
	a.syncWorkingTodos()
}

func (a *Agent) syncWorkingTodos() {
	if a.working == nil {
		return
	}
	views := make([]working.TodoView, len(a.todos))
	for i, t := range a.todos {
		views[i] = working.TodoView{Content: t.Content, Status: string(t.Status)}
	}
	a.working.SetTodos(views)
}

func (a *Agent) observeWorkingTool(name string, input json.RawMessage, out []byte, callErr error) {
	if a.working == nil {
		return
	}
	a.working.ObserveTool(name, input, out, callErr)
	if name == "todowrite" {
		a.syncWorkingTodos()
	}
}

func (a *Agent) injectWorkingPromptBlocks() string {
	var parts []string
	if a.opts.Mode == ModeOrchestra {
		if sp := readOrchestraScratchpad(a.tools.WorkspaceRoot()); sp != "" {
			parts = append(parts, sp)
		}
	}
	if a.working != nil {
		if ws := a.working.FormatWorkingState(); ws != "" {
			parts = append(parts, ws)
		}
	}
	if a.opts.TurnDigestKeep > 0 && a.opts.SessionID != "" {
		if dig := working.FormatRecentTurnDigests(a.tools.WorkspaceRoot(), a.opts.SessionID, a.opts.TurnDigestKeep); dig != "" {
			parts = append(parts, dig)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "\n\n" + parts[i]
	}
	return out
}

func (a *Agent) persistWorkingTurnDigest() {
	if a.working == nil || a.opts.TurnDigestKeep <= 0 || a.opts.SessionID == "" {
		return
	}
	digest := a.working.BuildTurnDigest(0)
	_ = working.PersistTurnDigest(a.tools.WorkspaceRoot(), a.opts.SessionID, digest)
}

// maybePersistMicroDigest writes a mid-run checkpoint every TurnDigestEveryN steps.
// History is not compacted or rewritten — only a local artifact is appended.
func (a *Agent) maybePersistMicroDigest(step int) {
	n := a.opts.TurnDigestEveryN
	if n <= 0 || step <= 0 || step%n != 0 {
		return
	}
	if a.working == nil || a.opts.TurnDigestKeep <= 0 || a.opts.SessionID == "" {
		return
	}
	digest := a.working.BuildTurnDigest(step)
	_ = working.PersistTurnDigest(a.tools.WorkspaceRoot(), a.opts.SessionID, digest)
	a.logf("turn_digest micro step=%d bytes=%d", step, len(digest))
}
