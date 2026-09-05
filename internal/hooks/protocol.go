package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
)

// A hook used to have exactly one thing to say: exit 0 or don't. That is
// enough to block a tool call and nothing else — the model is told "pre-tool
// hook denied" and left to guess what the rule was, so it retries the same
// call. The JSON protocol gives the hook a sentence the model can act on, and
// a way to fix an input instead of only refusing it.
//
// Both directions stay optional. A hook that reads no stdin and prints
// nothing still works exactly as before; every hook written before this
// existed keeps its meaning.

// Decision is the outcome of running a hook chain.
type Decision struct {
	// Denied blocks the tool call (pre_tool) or the turn (user_prompt_submit).
	Denied bool
	// Reason is the hook's own words, passed to the model with the denial.
	Reason string
	// Input, when non-empty, replaces the tool input. Always a JSON object.
	Input json.RawMessage
	// Context is text a lifecycle hook wants appended to the turn.
	Context string
}

// event is the JSON written to a hook's stdin.
type event struct {
	Event         string          `json:"event"`
	Tool          string          `json:"tool,omitempty"`
	Input         json.RawMessage `json:"input,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	WorkspaceRoot string          `json:"workspace_root"`
}

// reply is the JSON a hook may write to stdout.
type reply struct {
	Decision string          `json:"decision"`
	Reason   string          `json:"reason"`
	Input    json.RawMessage `json:"input"`
	Context  string          `json:"context"`
}

// parseReply reads a hook's stdout as a decision. Output that is not JSON is
// not an answer — hooks log their progress, and reading a log line as a
// verdict would make every chatty hook a random gate.
//
// The last non-empty line is tried as well, so a hook that logs and then
// answers is still understood.
func parseReply(stdout []byte) (reply, bool) {
	if r, ok := decodeReply(stdout); ok {
		return r, true
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		return decodeReply([]byte(lines[i]))
	}
	return reply{}, false
}

func decodeReply(b []byte) (reply, bool) {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return reply{}, false
	}
	var r reply
	if err := json.Unmarshal(trimmed, &r); err != nil {
		return reply{}, false
	}
	if strings.TrimSpace(r.Decision) == "" && r.Input == nil && r.Context == "" {
		// A JSON object that says nothing about the decision is not one.
		return reply{}, false
	}
	return r, true
}

// apply folds one hook's reply into the running decision.
func (d *Decision) apply(r reply) {
	switch strings.ToLower(strings.TrimSpace(r.Decision)) {
	case "deny", "block":
		d.Denied = true
		d.Reason = strings.TrimSpace(r.Reason)
	}
	if c := strings.TrimSpace(r.Context); c != "" {
		if d.Context == "" {
			d.Context = c
		} else {
			d.Context += "\n" + c
		}
	}
	// A rewritten input has to be a JSON object: anything else fails tool
	// schema validation later, with an error that points at the model instead
	// of at the hook that caused it.
	if obj := bytes.TrimSpace(r.Input); len(obj) > 0 && obj[0] == '{' && json.Valid(obj) {
		d.Input = append(json.RawMessage(nil), obj...)
	}
}
