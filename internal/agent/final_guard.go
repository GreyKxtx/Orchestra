package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// rejectPrematureFinal detects final steps that claim completion without any
// staged mutations when the turn still requires code changes — or when the
// model abandons an open todo checklist after a single edit (common local-model
// failure: edit package.json → {"patches":[]} → stop).
func (a *Agent) rejectPrematureFinal(userQuery string, step *Step, raw string, steps int) (hint string, reject bool) {
	if step == nil || step.Final == nil {
		return "", false
	}
	if len(step.Final.Patches) > 0 {
		return "", false
	}
	if a.tools != nil && len(a.tools.StagedOps()) > 0 {
		return "", false
	}

	// Open checklist always blocks final — even after successful edit/write.
	if n := countOpenTodos(a.todos); n > 0 {
		return fmt.Sprintf(
			"You still have %d open todo(s). Do not final yet — mark the finished item done with todowrite, continue the next with read/edit/write, or cancel abandoned todos. Only final when every todo is completed or cancelled.",
			n,
		), true
	}

	if a.turnMutatingTools > 0 {
		return "", false
	}

	hasTools := len(a.buildToolDefs()) > 0
	visible := strings.TrimSpace(stripThinkBlocks(raw))

	// Step 1 with tools advertised: empty response is never "done".
	// Patches-only JSON is rejected only when the query expects code changes —
	// prompts allow {"patches":[]} for pure Q&A without edit/write.
	if steps == 1 && hasTools {
		if visible == "" {
			return "Model returned an empty response. Call a tool (read, grep, ls) or answer in plain text.", true
		}
		if isContentOnlyPatchesJSON(raw) && queryRequiresCodeChanges(userQuery, a.todos, a.opts.Mode) {
			return "Do not emit {\"patches\":[]} without tool calls. Start with read on the target file(s), then edit or write if changes are needed.", true
		}
	}

	if !queryRequiresCodeChanges(userQuery, a.todos, a.opts.Mode) {
		return "", false
	}

	if isContentOnlyPatchesJSON(raw) {
		return "You sent {\"patches\":[]} without calling edit/write. Read the file, then use edit or write.", true
	}
	if visible == "" {
		return "Task requires code changes. Call read, then edit/write — reasoning alone is not enough.", true
	}
	return "Task requires code changes but no edit/write was performed. Call read, then edit or write.", true
}

func countOpenTodos(todos []tools.TodoItem) int {
	n := 0
	for _, t := range todos {
		switch t.Status {
		case tools.TodoPending, tools.TodoInProgress:
			n++
		}
	}
	return n
}

func isSilentPrematureFinalHint(hint string) bool {
	// Recoverable final-step validation is injected into LLM history only —
	// do not surface as user-visible errors while the agent retries.
	return strings.TrimSpace(hint) != ""
}

// isContentOnlyPatchesJSON reports whether visible content (after stripping
// thinking blocks) is exclusively a JSON object with a "patches" key.
func isContentOnlyPatchesJSON(raw string) bool {
	visible := strings.TrimSpace(stripThinkBlocks(raw))
	if visible == "" {
		return false
	}
	jsonStr := extractJSON(visible)
	if !strings.Contains(jsonStr, `"patches"`) {
		return false
	}
	var probe struct {
		Patches json.RawMessage `json:"patches"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &probe); err != nil {
		return false
	}
	withoutJSON := strings.TrimSpace(strings.Replace(visible, jsonStr, "", 1))
	return withoutJSON == ""
}
