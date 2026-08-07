package agent

import (
	"encoding/json"
	"strings"
)

// rejectPrematureFinal detects final steps that claim completion without any
// staged mutations when the turn still requires code changes.
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
	if a.turnMutatingTools > 0 {
		return "", false
	}

	hasTools := len(a.buildToolDefs()) > 0
	visible := strings.TrimSpace(stripThinkBlocks(raw))

	// Step 1 with tools advertised: empty or patches-only JSON is never "done".
	if steps == 1 && hasTools {
		if visible == "" {
			return "Model returned an empty response. Call a tool (read, grep, ls) or answer in plain text.", true
		}
		if isContentOnlyPatchesJSON(raw) {
			return "Do not emit {\"patches\":[]} without tool calls. Call read, then edit or write.", true
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

func isSilentPrematureFinalHint(hint string) bool {
	return strings.Contains(hint, "Model returned an empty response")
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
