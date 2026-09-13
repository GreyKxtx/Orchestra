package agent

import (
	"testing"
)

// The explore-first gate is satisfied by exactly the tools that run on the
// PARALLEL path — read, grep, ls, glob, explore, symbols, repo_map, the lsp
// queries — and it was marked only on the serial one. So a model that batches
// its reads into a single assistant message, which is the behaviour the tool
// schema encourages, could never open the gate at all.
//
// From the run that found it (architecture mode, orch-eval-2584006983):
//
//	repo_map + read   (parallel)
//	grep + ls         (parallel)
//	read + read       (parallel)
//	write             -> denied: "call read, grep, or explore ... before write"
//
// It had called read and grep four times. The turn recovered only when the
// model happened to issue one explore alone, six minutes in.
func TestExploreFirstGate_IsOpenedByReadsThatRanInParallel(t *testing.T) {
	for _, mode := range []Mode{ModeArchitecture, ModeOrchestra, ModeWorker} {
		a := &Agent{}
		a.opts.Mode = mode

		// What the parallel path does for each read-only call in a batch.
		a.markParallelExploreSatisfied([]ToolCall{
			{Name: "repo_map"},
			{Name: "read"},
		}, []bool{false, false}, []bool{false, false})

		if err := a.checkExploreFirstGate("write", nil); err != nil {
			t.Errorf("%s: reads that ran in parallel left the gate shut:\n%v", mode, err)
		}
	}
}

// A batch that failed or was denied proves nothing about the repository, and
// must not open the gate — otherwise the gate is satisfied by asking rather
// than by looking.
func TestExploreFirstGate_StaysShutWhenTheParallelCallsDidNotSucceed(t *testing.T) {
	a := &Agent{}
	a.opts.Mode = ModeArchitecture

	a.markParallelExploreSatisfied([]ToolCall{
		{Name: "read"},
		{Name: "grep"},
	}, []bool{true, false}, []bool{false, true})

	if err := a.checkExploreFirstGate("write", nil); err == nil {
		t.Error("a denied read and a failed grep opened the gate")
	}
}

// And a batch of tools that are not navigation must not open it either.
func TestExploreFirstGate_StaysShutForNonNavigationCalls(t *testing.T) {
	a := &Agent{}
	a.opts.Mode = ModeArchitecture

	a.markParallelExploreSatisfied([]ToolCall{
		{Name: "todoread"},
	}, []bool{false}, []bool{false})

	if err := a.checkExploreFirstGate("write", nil); err == nil {
		t.Error("todoread is not looking at the repository")
	}
}
