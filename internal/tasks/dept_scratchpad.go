package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// Dept scratchpads (spec §5.8): .orchestra/state.md belongs to the
// Orchestrator; .orchestra/depts/{instance}.md belongs to a Dept Lead — one
// file per department instance. A WorkOrder may reference its instance
// scratchpad via context.scratchpad; the runtime appends the compact worker
// summary there so the Lead's file accumulates per-instance history without
// any LLM involvement.

var deptScratchpadFileRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(@[a-z0-9][a-z0-9_-]*)?\.md$`)

// deptScratchpadRelPath extracts and validates context.scratchpad from a
// WorkOrder. Returns the normalized workspace-relative path, or "" when the
// value is absent or is not a well-formed .orchestra/depts/{instance}.md
// reference (fail-closed: never write outside the depts dir).
func deptScratchpadRelPath(wo *WorkOrder) string {
	if wo == nil || wo.Context == nil {
		return ""
	}
	raw, _ := wo.Context["scratchpad"].(string)
	raw = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(raw)), "./")
	if raw == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(raw, agent.DeptScratchpadDir+"/")
	if !ok || !deptScratchpadFileRe.MatchString(rest) {
		return ""
	}
	return raw
}

// appendDeptScratchpadDone appends a one-line entry under `## Done` of the
// dept scratchpad, creating the file on first write. Best-effort: errors are
// returned for logging but must not fail the worker task.
func appendDeptScratchpadDone(workspaceRoot, relPath, line string) error {
	line = strings.TrimSpace(line)
	if line == "" || relPath == "" {
		return nil
	}
	abs := filepath.Join(workspaceRoot, filepath.FromSlash(relPath))
	var content string
	if b, err := os.ReadFile(abs); err == nil {
		content = string(b)
	} else if os.IsNotExist(err) {
		instance := strings.TrimSuffix(filepath.Base(abs), ".md")
		content = "# Dept scratchpad — " + instance + "\n\n## Done\n"
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
	} else {
		return err
	}
	updated := insertUnderDoneSection(content, "- [x] "+line)
	return fsutil.AtomicWriteFile(abs, []byte(strings.TrimRight(updated, "\n")+"\n"), 0o644)
}

// insertUnderDoneSection appends line at the end of the `## Done` section,
// creating the section when missing.
func insertUnderDoneSection(content, line string) string {
	content = strings.TrimRight(content, "\n")
	const marker = "## Done"
	idx := strings.Index(content, marker)
	if idx < 0 {
		return content + "\n\n" + marker + "\n" + line + "\n"
	}
	after := content[idx+len(marker):]
	nextRel := strings.Index(after, "\n## ")
	if nextRel < 0 {
		return content + "\n" + line + "\n"
	}
	insertAt := idx + len(marker) + nextRel
	return content[:insertAt] + "\n" + line + content[insertAt:]
}

// recordWorkerToDeptScratchpad appends the compact worker outcome to the dept
// scratchpad referenced by the WorkOrder, when present and valid.
func (r *TaskRunner) recordWorkerToDeptScratchpad(wo *WorkOrder, resultText, status, errMsg string) {
	rel := deptScratchpadRelPath(wo)
	if rel == "" {
		return
	}
	line := agent.CompactWorkerResultForLead(resultText, workerDeptSummaryMaxBytes)
	if strings.TrimSpace(line) == "" {
		line = "worker " + status
		if errMsg != "" {
			line += " — " + errMsg
		}
	}
	if wo != nil && strings.TrimSpace(wo.TaskID) != "" {
		line = wo.TaskID + ": " + line
	}
	_ = appendDeptScratchpadDone(r.toolRunner.WorkspaceRoot(), rel, line)
}

const workerDeptSummaryMaxBytes = 300
