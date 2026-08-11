package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/internal/agent/history"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/llm"
)

const workerLeadResultMaxBytes = 1200
const orchestraWorkerHistoryCompactBytes = 280

func orchestraScratchpadAbs(root string) string {
	return filepath.Join(root, filepath.FromSlash(plan.OrchestraStateRelPath))
}

// readOrchestraScratchpad loads .orchestra/state.md for prompt inject (or "").
func readOrchestraScratchpad(root string) string {
	b, err := os.ReadFile(orchestraScratchpadAbs(root))
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return ""
	}
	const maxScratchpadInject = 2400
	if len(body) > maxScratchpadInject {
		body = body[:maxScratchpadInject] + "\n...(truncated; use read on .orchestra/state.md for full file)"
	}
	return "<orchestra_scratchpad>\n" + body + "\n</orchestra_scratchpad>"
}

func (a *Agent) handleUpdateWorkingState(input json.RawMessage) (json.RawMessage, error) {
	if a.opts.Mode != ModeOrchestra {
		return nil, fmt.Errorf("update_working_state is only available in orchestra Lead mode")
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("update_working_state: invalid input: %w", err)
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("update_working_state: content is required")
	}
	path := orchestraScratchpadAbs(a.tools.WorkspaceRoot())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := fsutil.AtomicWriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		return nil, err
	}
	resp, _ := json.Marshal(map[string]any{
		"path":    plan.OrchestraStateRelPath,
		"written": len(content),
		"status":  "ok",
	})
	return resp, nil
}

// CompactWorkerResultForLead shrinks worker/verify JSON for Lead history.
func CompactWorkerResultForLead(raw string, maxBytes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if maxBytes <= 0 {
		maxBytes = workerLeadResultMaxBytes
	}
	if !json.Valid([]byte(raw)) {
		if len(raw) <= maxBytes {
			return raw
		}
		return raw[:maxBytes] + "..."
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	status, _ := m["status"].(string)
	path := extractWorkerPath(m)
	line := fmt.Sprintf("worker %s", strings.TrimSpace(status))
	if path != "" {
		line += " path=" + path
	}
	if msg := extractWorkerMessage(m); msg != "" {
		line += " — " + msg
	}
	if len(line) > maxBytes {
		line = line[:maxBytes] + "..."
	}
	return line
}

func extractWorkerPath(m map[string]any) string {
	if p, ok := m["path"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}
	if wr, ok := m["worker_result"].(map[string]any); ok {
		if p, ok := wr["path"].(string); ok {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

func extractWorkerMessage(m map[string]any) string {
	if msg, ok := m["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	if wr, ok := m["worker_result"].(map[string]any); ok {
		if msg, ok := wr["message"].(string); ok {
			return strings.TrimSpace(msg)
		}
	}
	if v, ok := m["verification"].(map[string]any); ok {
		if passed, ok := v["passed"].(bool); ok {
			if !passed {
				return "verification failed"
			}
			return "verified"
		}
	}
	return ""
}

func appendWorkerSummaryToScratchpad(root, summaryLine string) error {
	summaryLine = strings.TrimSpace(summaryLine)
	if summaryLine == "" {
		return nil
	}
	path := orchestraScratchpadAbs(root)
	var content string
	if b, err := os.ReadFile(path); err == nil {
		content = string(b)
	} else if os.IsNotExist(err) {
		content = plan.DefaultOrchestraScratchpad("")
	} else {
		return err
	}
	line := "- [x] " + summaryLine
	updated := appendScratchpadDoneLine(content, line)
	return fsutil.AtomicWriteFile(path, []byte(strings.TrimRight(updated, "\n")+"\n"), 0o644)
}

func appendScratchpadDoneLine(content, line string) string {
	content = strings.TrimRight(content, "\n")
	marker := "## Done"
	idx := strings.Index(content, marker)
	if idx < 0 {
		return content + "\n\n## Done\n" + line + "\n"
	}
	after := content[idx+len(marker):]
	nextRel := strings.Index(after, "\n## ")
	if nextRel < 0 {
		return content + "\n" + line + "\n"
	}
	insertAt := idx + len(marker) + nextRel
	return content[:insertAt] + "\n" + line + content[insertAt:]
}

func looksLikeWorkerResult(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "verified_success") || strings.Contains(raw, "verification_failed") {
		return true
	}
	if !json.Valid([]byte(raw)) {
		return false
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return false
	}
	st, _ := m["status"].(string)
	switch strings.ToLower(strings.TrimSpace(st)) {
	case "success", "ok", "done", "error", "verification_failed", "verified_success":
		_, hasPath := m["path"]
		_, hasWorker := m["worker_result"]
		return hasPath || hasWorker
	}
	return false
}

func (a *Agent) maybeRecordWorkerToScratchpad(subagentType, result string) {
	if a == nil || a.opts.Mode != ModeOrchestra {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(subagentType), "worker") {
		return
	}
	compact := CompactWorkerResultForLead(result, workerLeadResultMaxBytes)
	if compact == "" {
		return
	}
	_ = appendWorkerSummaryToScratchpad(a.tools.WorkspaceRoot(), compact)
}

// orchestraCompactTaskToolOutput shrinks worker result inside task/task_wait JSON.
func orchestraCompactTaskToolOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	res, _ := m["result"].(string)
	if res == "" || !looksLikeWorkerResult(res) {
		return ""
	}
	m["result"] = CompactWorkerResultForLead(res, orchestraWorkerHistoryCompactBytes)
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

func orchestraTaskToolCompactor(_ string, content string) (string, bool) {
	if compact := orchestraCompactTaskToolOutput(content); compact != "" {
		return compact, true
	}
	if looksLikeWorkerResult(content) {
		return CompactWorkerResultForLead(content, orchestraWorkerHistoryCompactBytes), true
	}
	return "", false
}

func collapseOrchestraWorkerTaskHistory(messages []llm.Message, keepRecent int) []llm.Message {
	return history.CollapseOrchestraWorkerTaskOutputs(messages, keepRecent, orchestraTaskToolCompactor)
}
