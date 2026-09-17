package tasks

import (
	"testing"

	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

// The phase gate (orchestrastate.GuardSpawn) and the child tool surface
// (childToolsForSubagent) are decided in two packages that never reference
// each other. They drifted: the gate named only "worker", while general and
// debug children carry write/edit — and an unrecognised subagent_type falls
// through tools.ListToolsForMode's default arm to the full build surface. So
// "no code changes before the contract is frozen" held for exactly one child
// type and could be bypassed by a typo.
//
// This test ties the two together: any child whose tools can mutate a
// production file must be gated, unless its writes are confined to the
// artifacts a phase exists to produce. A new writing mode added without a
// gate decision fails here.

// productionWriteTools mutate a path the child chooses. lsp.rename edits
// source across files; fs.delete/fs.rename move it. diff.preview and the
// read-only LSP calls do not appear.
var productionWriteTools = map[string]bool{
	"write": true, "edit": true, "fs.delete": true, "fs.rename": true, "lsp.rename": true,
}

// scopedWriters carry write tools whose target is confined by the agent layer
// (agent.checkProductEditScope, agent.checkDocsEditScope, and the
// plan.IsDeptLeadWritablePath arm of tool_dispatch). They produce the
// artifacts a phase is about, so gating them would deadlock the phase that
// needs them: the PRD gate's own unblock path is "spawn product".
var scopedWriters = map[string]bool{
	"architecture": true, "product": true, "documentation": true,
}

// everySubagentType is every type modeForSubagent names, plus the two ways an
// unlisted one arrives: a custom agent from .orchestra.yml, and a typo.
var everySubagentType = []string{
	"", "explore", "ask", "debug", "architecture", "general",
	"worker", "verifier", "product", "documentation",
	"my-custom-agent", "wrker",
}

func childCanWriteProductionFiles(t *testing.T, subagentType string) bool {
	t.Helper()
	for _, def := range childToolsForSubagent(subagentType, tools.Capabilities{Exec: true, Web: true}) {
		if productionWriteTools[def.Function.Name] {
			return true
		}
	}
	return false
}

// discoveryWorkspace is a project mid-discovery: production code must not
// change yet. The guard is inactive without a state file, so writing one is
// what makes the question meaningful. Save is used rather than a hand-written
// file because the frontmatter nests under an "orchestra:" key — a flat one
// parses as phase "unset", and the gate then blocks on the PRD instead, which
// would make these cases pass for the wrong reason.
func discoveryWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := orchestrastate.Save(root, &orchestrastate.State{Phase: orchestrastate.PhaseDiscovery}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPhaseGateCoversEveryChildThatCanWrite(t *testing.T) {
	root := discoveryWorkspace(t)

	for _, subagentType := range everySubagentType {
		label := subagentType
		if label == "" {
			label = "(empty)"
		}
		t.Run(label, func(t *testing.T) {
			canWrite := childCanWriteProductionFiles(t, subagentType)
			scoped := scopedWriters[subagentType]
			err := orchestrastate.GuardSpawn(root, orchestrastate.EnforcementStrict, subagentType)
			gated := err != nil

			switch {
			case canWrite && !scoped && !gated:
				t.Errorf("%s can write production files and spawns in phase=discovery unchecked; "+
					"either confine its writes or add it to the gated set", label)
			case !canWrite && gated:
				t.Errorf("%s has no write tool but is blocked in phase=discovery: %v", label, err)
			case scoped && gated:
				t.Errorf("%s writes only phase artifacts and must not be gated (the PRD gate's "+
					"unblock path is to spawn one): %v", label, err)
			}
		})
	}
}

// The regression the gate was opened by: a debug child editing source before
// the contract is frozen, and an unknown type doing the same.
func TestPhaseGateBlocksWritersOtherThanWorker(t *testing.T) {
	root := discoveryWorkspace(t)
	for _, subagentType := range []string{"debug", "general", "my-custom-agent"} {
		if err := orchestrastate.GuardSpawn(root, orchestrastate.EnforcementStrict, subagentType); err == nil {
			t.Errorf("%s spawned in phase=discovery; it holds write and edit", subagentType)
		}
	}
}

// Gating more child types must not gate a project that never opted into the
// orchestra state machine, nor one running prompt_only.
func TestPhaseGateStaysInactiveWithoutOrchestration(t *testing.T) {
	plain := t.TempDir()
	for _, subagentType := range everySubagentType {
		if err := orchestrastate.GuardSpawn(plain, orchestrastate.EnforcementStrict, subagentType); err != nil {
			t.Errorf("no state file, yet %q was blocked: %v", subagentType, err)
		}
	}

	root := discoveryWorkspace(t)
	for _, subagentType := range everySubagentType {
		if err := orchestrastate.GuardSpawn(root, orchestrastate.EnforcementPromptOnly, subagentType); err != nil {
			t.Errorf("prompt_only, yet %q was blocked: %v", subagentType, err)
		}
	}
}
