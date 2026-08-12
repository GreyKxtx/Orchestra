package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// workerResultStatuses is the closed status set for WorkOrder-driven workers
// (spec checklist 31: schema-enforced task_result for local L3 models).
var workerResultStatuses = map[string]bool{
	"success": true, "ok": true, "done": true, "error": true, "blocked": true,
}

// checkWorkerResultSchema rejects a WorkOrder-driven worker task_result that
// is not a JSON object with a valid status. Local L3/L1 models drift into
// free-text summaries; the Lead's parser then misroutes them, so the runtime
// forces the shape before accepting the result.
func (a *Agent) checkWorkerResultSchema(content string) error {
	if a == nil || a.opts.Mode != ModeWorker || !a.opts.WorkerStrictResult {
		return nil
	}
	const template = `task_result content must be a JSON object: {"status":"success|error|blocked","path":"<main file>","summary":"<1-3 sentences>","blocked_reason":"<taxonomy, only when blocked>"}. Resend task_result with that JSON.`
	content = strings.TrimSpace(content)
	if content == "" || !strings.HasPrefix(content, "{") || !json.Valid([]byte(content)) {
		return fmt.Errorf("task_result blocked: not a JSON object. %s", template)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return fmt.Errorf("task_result blocked: not a JSON object. %s", template)
	}
	st, _ := m["status"].(string)
	st = strings.ToLower(strings.TrimSpace(st))
	if !workerResultStatuses[st] {
		return fmt.Errorf("task_result blocked: status %q is not in success|ok|done|error|blocked. %s", st, template)
	}
	if st == "blocked" {
		if br, _ := m["blocked_reason"].(string); strings.TrimSpace(br) == "" {
			return fmt.Errorf("task_result blocked: status=blocked requires blocked_reason from the taxonomy [%s]. %s",
				strings.Join(BlockedReasons(), " | "), template)
		}
	}
	return nil
}

// blockWorkerTaskResult rejects task_result when the worker reports success
// but staged edits still carry LSP error-severity diagnostics (orchestra
// "forced LSP resolution" gate — docs/architecture/orchestra-vnext.md §6).
func (a *Agent) blockWorkerTaskResult(content string) error {
	if a == nil || a.opts.Mode != ModeWorker || a.diags == nil {
		return nil
	}
	if !workerResultClaimsSuccess(content) {
		return nil
	}
	paths := a.diags.PathsWithErrors()
	if len(paths) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("task_result blocked: your code still has LSP compile errors. Fix them before reporting success to the Lead.\n")
	for _, p := range paths {
		if h := a.diags.ErrorHint(p); h != "" {
			b.WriteString(h)
			b.WriteByte('\n')
		} else {
			fmt.Fprintf(&b, "  %s: unresolved compile errors\n", p)
		}
	}
	return fmt.Errorf("%s", strings.TrimSpace(b.String()))
}

func workerResultClaimsSuccess(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return true
	}
	if !json.Valid([]byte(content)) {
		return true
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return true
	}
	st, _ := m["status"].(string)
	st = strings.ToLower(strings.TrimSpace(st))
	if st == "" || st == "success" || st == "ok" || st == "done" {
		return true
	}
	return false
}
