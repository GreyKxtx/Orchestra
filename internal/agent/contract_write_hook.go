package agent

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/contract"
)

// ContractInvalidator is implemented by the task runner (tasks.TaskRunner):
// cancel running worker tasks whose contract_refs no longer match EPOCH.yaml.
// Detected by type assertion so the SubtaskRunner interface stays unchanged.
type ContractInvalidator interface {
	InvalidateStaleContractTasks(ctx context.Context) []string
}

// afterContractArtifactWrite is the deterministic epoch hook (spec §5.3):
// when a Lead successfully writes a contract artifact, the runtime — not the
// model — re-hashes it into EPOCH.yaml (version++, epoch++) and cancels
// running workers pinned to the old hashes.
//
// Fires only when the write reached disk (apply mode; dry-run keeps changes
// in the staging overlay so the on-disk hash is unchanged) and only when the
// contract layer is adopted (EPOCH.yaml exists — the initial freeze at stage
// 2.5 is explicit via contract.FreezeAll, not a side effect of a write).
func (a *Agent) afterContractArtifactWrite(ctx context.Context, relPath string) {
	name, ok := contractArtifactFileName(relPath)
	if !ok || !a.opts.Apply {
		return
	}
	root := a.tools.WorkspaceRoot()
	if _, found, err := contract.Load(root); err != nil || !found {
		return
	}
	e, err := contract.UpdateArtifact(root, name, "")
	if err != nil {
		a.logf("contract epoch update %s failed: %v", name, err)
		return
	}
	a.logf("contract epoch update %s: epoch=%d version=%d", name, e.Epoch, e.Artifacts[name].Version)
	inv, ok := a.opts.SubtaskRunner.(ContractInvalidator)
	if !ok || inv == nil {
		return
	}
	if cancelled := inv.InvalidateStaleContractTasks(ctx); len(cancelled) > 0 {
		a.logf("contract epoch change cancelled stale workers: %s", strings.Join(cancelled, ", "))
	}
}

// contractArtifactFileName reports whether relPath is a contract artifact
// file (directly inside .orchestra/contract/, not EPOCH.yaml itself) and
// returns its bare file name.
func contractArtifactFileName(relPath string) (string, bool) {
	p := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(relPath)), "./")
	rest, ok := strings.CutPrefix(p, contract.DirRel+"/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	if rest == filepath.Base(contract.EpochFileRel) {
		return "", false
	}
	return rest, true
}
