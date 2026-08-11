package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func normalizeWorkerEditPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	return p
}

func workerPathInEditScope(path string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	path = normalizeWorkerEditPath(path)
	if path == "" {
		return false
	}
	for _, a := range allowed {
		if path == normalizeWorkerEditPath(a) {
			return true
		}
	}
	return false
}

// checkWorkerEditScope denies edit/write outside WorkOrder target paths.
func (a *Agent) checkWorkerEditScope(name string, input json.RawMessage) error {
	if a == nil || a.opts.Mode != ModeWorker {
		return nil
	}
	if name != "edit" && name != "write" {
		return nil
	}
	allowed := a.opts.WorkerEditPaths
	if len(allowed) == 0 {
		return nil
	}
	path := extractWriteOrEditPath(input)
	if path == "" || workerPathInEditScope(path, allowed) {
		return nil
	}
	return fmt.Errorf(
		"edit scope violation: %q is outside WorkOrder target_file(s) [%s]. Edit only allowed paths or return task_result with status=error",
		path,
		strings.Join(allowed, ", "),
	)
}
