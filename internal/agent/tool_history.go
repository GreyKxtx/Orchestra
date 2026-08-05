package agent

import (
	"encoding/json"
)

// prepareToolHistoryContent optionally digests large tool output and auto-writes session memory.
func (a *Agent) prepareToolHistoryContent(name string, input json.RawMessage, out []byte) string {
	content := string(out)
	budget := a.opts.ToolDigestBytes
	if budget <= 0 {
		a.maybeAutoSessionMemory(name, input, content)
		return content
	}
	if len(out) > budget {
		if digested, ok := DigestToolOutput(name, input, out, budget); ok {
			content = digested
		}
	}
	a.maybeAutoSessionMemory(name, input, content)
	return content
}

func (a *Agent) maybeAutoSessionMemory(name string, input json.RawMessage, content string) {
	if !a.opts.AutoSessionMemory || a.opts.SessionID == "" || !a.opts.Memory.SessionEnabled {
		return
	}
	note := AutoMemoryNote(name, input, content)
	if note == "" {
		return
	}
	_ = a.tools.AppendSessionMemory(note)
}
