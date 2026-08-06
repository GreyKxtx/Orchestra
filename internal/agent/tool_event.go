package agent

import (
	"github.com/orchestra/orchestra/internal/agent/format"
	"github.com/orchestra/orchestra/internal/llm"
)

const toolCompletedPreviewMax = 256

// toolCallCompletedStreamEvent builds the UI-facing tool_call_completed event.
func toolCallCompletedStreamEvent(name, toolCallID string, out []byte, callErr error) llm.StreamEvent {
	ev := llm.StreamEvent{
		Kind:         llm.StreamEventToolCallCompleted,
		ToolCallName: name,
		ToolCallID:   toolCallID,
	}
	if callErr != nil {
		msg := "error: " + callErr.Error()
		if len(msg) > toolCompletedPreviewMax {
			msg = msg[:toolCompletedPreviewMax] + "...(truncated)"
		}
		ev.Content = msg
		return ev
	}
	if len(out) > 0 {
		if len(out) > toolCompletedPreviewMax {
			ev.Content = string(out[:toolCompletedPreviewMax]) + "...(truncated)"
		} else {
			ev.Content = string(out)
		}
	}
	if name == "write" || name == "edit" {
		ev.Diagnostics = format.ExtractDiagnosticsJSON(out)
	}
	return ev
}
