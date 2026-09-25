package agent

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/tools"
)

// ContractInvalidator is implemented by the task runner (tasks.TaskRunner):
// cancel running worker tasks whose contract_refs no longer match the
// contract they see. Detected by type assertion so the SubtaskRunner
// interface stays unchanged.
type ContractInvalidator interface {
	InvalidateStaleContractTasks(ctx context.Context) []string
}

// afterContractArtifactWrite is the deterministic epoch hook (spec §5.3):
// when an owner writes a contract artifact, the runtime — not the model —
// re-hashes it into EPOCH.yaml (version++, epoch++) and cancels running
// workers pinned to the old version.
//
// The hash is of the version this agent sees: the turn stages its writes and
// the disk has them only once it applies. A task's write is in its own layer
// and is not the contract yet — its runner records it when the layer commits
// (tasks.afterContractCommit). Nothing happens before the contract is
// adopted: the initial freeze is contract_freeze's, not a write's side effect.
func (a *Agent) afterContractArtifactWrite(ctx context.Context, relPath string) {
	if _, ok := contract.ArtifactFileName(relPath); !ok || tools.InLayer(ctx) {
		return
	}
	moved, err := contract.Refresh(a.tools.WorkspaceRoot(), a.tools.View(ctx), []string{relPath})
	if err != nil {
		a.logf("contract epoch update %s failed: %v", relPath, err)
		return
	}
	if len(moved) == 0 {
		return
	}
	a.logf("contract epoch update: %s", strings.Join(moved, ", "))
	if cancelled := a.invalidateStaleContractTasks(ctx); len(cancelled) > 0 {
		a.logf("contract epoch change cancelled stale workers: %s", strings.Join(cancelled, ", "))
	}
}

func (a *Agent) invalidateStaleContractTasks(ctx context.Context) []string {
	inv, ok := a.opts.SubtaskRunner.(ContractInvalidator)
	if !ok || inv == nil {
		return nil
	}
	return inv.InvalidateStaleContractTasks(ctx)
}
