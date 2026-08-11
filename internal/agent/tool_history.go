package agent

import (
	"encoding/json"
	"strings"
)

// prepareToolHistoryContent optionally digests large tool output and auto-writes session memory.
func (a *Agent) prepareToolHistoryContent(name string, input json.RawMessage, out []byte) string {
	a.observeWorkingTool(name, input, out, nil)
	content := string(out)
	if a.opts.Mode == ModeOrchestra {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "task", "task_wait":
			if compact := orchestraCompactTaskToolOutput(content); compact != "" {
				content = compact
				a.maybeAutoSessionMemory(name, input, content)
				return content
			}
		}
	}
	budget := a.opts.ToolDigestBytes
	if budget <= 0 {
		a.maybeAutoSessionMemory(name, input, content)
		return content
	}
	// Fresh reads/explores must stay full: write-time digest of a just-fetched
	// file is what forces "read in parts / re-grep" loops. Older results are
	// still shrunk by pruneRetroactiveToolHistory once they fall out of the
	// keep-recent window.
	if !skipWriteTimeDigest(name) && len(out) > budget {
		if digested, ok := DigestToolOutput(name, input, out, budget); ok {
			content = digested
		}
	}
	a.maybeAutoSessionMemory(name, input, content)
	return content
}

// skipWriteTimeDigest tools are kept intact when first appended to history.
func skipWriteTimeDigest(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "fs.read", "explore", "symbols", "repo_map":
		return true
	default:
		return false
	}
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
