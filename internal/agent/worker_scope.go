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

// productScopePrefix is the only writable location in ModeProduct
// (spec §7.1: Product subagent — «только .orchestra/product/*»).
const productScopePrefix = ".orchestra/product/"

// checkProductEditScope denies edit/write outside .orchestra/product/ for the
// Product Lead subagent. Reads stay unrestricted (brownfield context).
func (a *Agent) checkProductEditScope(name string, input json.RawMessage) error {
	if a == nil || a.opts.Mode != ModeProduct {
		return nil
	}
	if name != "edit" && name != "write" {
		return nil
	}
	path := extractWriteOrEditPath(input)
	if path == "" {
		return nil // runner rejects pathless writes on its own
	}
	if strings.HasPrefix(normalizeWorkerEditPath(path), productScopePrefix) {
		return nil
	}
	return fmt.Errorf(
		"product scope violation: %q is outside %s. Product Lead writes PRD.md / User_Stories.md there only; return task_result if the task requires other files",
		path, productScopePrefix,
	)
}

// Docs Lead write scope (spec §2.3.2): L1 conventions, the docs MANIFEST and
// the project docs tree — except operations runbooks (Platform-owned). Not
// production code, not PRD, not the contract, not per-dept playbooks.
const (
	docsConventionsRelPath = ".orchestra/playbooks/conventions.md"
	docsManifestDirPrefix  = ".orchestra/docs/"
	docsTreePrefix         = "docs/"
	docsRunbooksPrefix     = "docs/operations/runbooks/"
)

// checkDocsEditScope denies edit/write outside the Docs Lead surface for the
// documentation subagent. Reads stay unrestricted.
func (a *Agent) checkDocsEditScope(name string, input json.RawMessage) error {
	if a == nil || a.opts.Mode != ModeDocs {
		return nil
	}
	if name != "edit" && name != "write" {
		return nil
	}
	path := extractWriteOrEditPath(input)
	if path == "" {
		return nil // runner rejects pathless writes on its own
	}
	p := normalizeWorkerEditPath(path)
	switch {
	case p == docsConventionsRelPath:
		return nil
	case strings.HasPrefix(p, docsManifestDirPrefix):
		return nil
	case strings.HasPrefix(p, docsRunbooksPrefix):
		return fmt.Errorf("docs scope violation: %q — docs/operations/runbooks/ belongs to Platform Lead; return the need via task_result", path)
	case strings.HasPrefix(p, docsTreePrefix):
		return nil
	}
	return fmt.Errorf(
		"docs scope violation: %q is outside the Docs Lead surface (%s, %s*, %s* minus runbooks). Per-dept playbooks belong to their Leads; return task_result if the task requires other files",
		path, docsConventionsRelPath, docsManifestDirPrefix, docsTreePrefix,
	)
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
