package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
