package orchestrastate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeState(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(StateFileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

const sampleState = `---
orchestra:
  phase: execution
  prd_status: approved
  contract_epoch: 2
  clarification_rounds: 1
---
## Goal
Build the thing.
`

func TestLoadMissingFile(t *testing.T) {
	st, found, err := Load(t.TempDir())
	if err != nil || found || st != nil {
		t.Fatalf("missing file: st=%v found=%v err=%v", st, found, err)
	}
}

func TestLoadParse(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, sampleState)
	st, found, err := Load(root)
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}
	if st.Phase != PhaseExecution || st.PRDStatus != "approved" || st.ContractEpoch != 2 {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(st.Body, "## Goal") {
		t.Fatalf("body = %q", st.Body)
	}
}

func TestAddDocDebt(t *testing.T) {
	root := t.TempDir()

	// No state file → no-op, no error, nothing created.
	if err := AddDocDebt(root, "docs/api/README.md"); err != nil {
		t.Fatalf("AddDocDebt without state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(StateFileRel))); !os.IsNotExist(err) {
		t.Fatal("state file must not be created by AddDocDebt")
	}

	writeState(t, root, sampleState)
	if err := AddDocDebt(root, "docs/api/README.md"); err != nil {
		t.Fatalf("AddDocDebt: %v", err)
	}
	// Idempotent.
	if err := AddDocDebt(root, "docs/api/README.md"); err != nil {
		t.Fatalf("AddDocDebt (repeat): %v", err)
	}
	if err := AddDocDebt(root, "docs/architecture/overview.md"); err != nil {
		t.Fatalf("AddDocDebt (second): %v", err)
	}

	st, found, err := Load(root)
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}
	if len(st.DocDebt) != 2 || st.DocDebt[0] != "docs/api/README.md" || st.DocDebt[1] != "docs/architecture/overview.md" {
		t.Fatalf("doc_debt = %v", st.DocDebt)
	}
	// Body and other frontmatter survive the roundtrip.
	if st.Phase != PhaseExecution || !strings.Contains(st.Body, "## Goal") {
		t.Fatalf("state mangled: %+v", st)
	}
}

func TestLoadRejectsUnknownPhase(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "---\norchestra:\n  phase: warp\n---\n")
	if _, _, err := Load(root); err == nil {
		t.Fatal("unknown phase must fail")
	}
}

func TestSaveRoundtrip(t *testing.T) {
	root := t.TempDir()
	in := &State{Phase: PhaseContract, PRDStatus: "approved", ContractEpoch: 3, Body: "## Goal\nX\n"}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, found, err := Load(root)
	if err != nil || !found {
		t.Fatalf("Load after Save: found=%v err=%v", found, err)
	}
	if out.Phase != in.Phase || out.PRDStatus != in.PRDStatus || out.ContractEpoch != in.ContractEpoch {
		t.Fatalf("roundtrip = %+v", out)
	}
	if !strings.Contains(out.Body, "## Goal") {
		t.Fatalf("body lost: %q", out.Body)
	}
}

func TestGuardSpawnMatrix(t *testing.T) {
	cases := []struct {
		name         string
		phase        Phase
		prdStatus    string
		subagent     string
		wantBlocked  bool
		wantContains string
	}{
		{"no gating for explore in discovery", PhaseDiscovery, "", "explore", false, ""},
		{"no gating for ask in contract", PhaseContract, "", "ask", false, ""},
		{"worker blocked in discovery", PhaseDiscovery, "", "worker", true, "PRD"},
		{"worker blocked without approved PRD", PhaseExecution, "draft", "worker", true, "PRD"},
		{"worker blocked in contract", PhaseContract, "approved", "worker", true, "contract not frozen"},
		{"worker blocked in documentation", PhaseDocumentation, "approved", "worker", true, "execution|maintenance"},
		{"worker blocked in delivery", PhaseDelivery, "approved", "worker", true, "execution|maintenance"},
		{"worker allowed in execution", PhaseExecution, "approved", "worker", false, ""},
		{"worker allowed in maintenance without PRD", PhaseMaintenance, "", "worker", false, ""},

		// The gate named only "worker" while these three carry write and edit,
		// so "no code changes before the contract is frozen" held for one child
		// type out of four. An unrecognised type is the widest of them: it
		// falls through tools.ListToolsForMode to the full build surface, so a
		// typo or a custom agent name used to walk straight past the phase
		// machine. See tasks.TestPhaseGateCoversEveryChildThatCanWrite, which
		// ties this list to the tool surfaces themselves.
		{"debug blocked in discovery", PhaseDiscovery, "", "debug", true, "PRD"},
		{"general blocked in documentation", PhaseDocumentation, "approved", "general", true, "execution|maintenance"},
		{"unknown child type blocked in discovery", PhaseDiscovery, "", "coder", true, "PRD"},
		{"unknown child type named in the refusal", PhaseDelivery, "approved", "coder", true, "unknown child type"},
		{"general allowed in execution", PhaseExecution, "approved", "general", false, ""},
		{"debug allowed in maintenance", PhaseMaintenance, "", "debug", false, ""},

		// Scoped writers produce the artifacts a phase is about. Gating them
		// would deadlock the phase that needs them — the PRD gate's own
		// unblock path is "spawn product".
		{"product allowed in discovery", PhaseDiscovery, "", "product", false, ""},
		{"documentation allowed in documentation", PhaseDocumentation, "", "documentation", false, ""},
		{"architecture allowed in contract", PhaseContract, "", "architecture", false, ""},
		{"verifier allowed in delivery", PhaseDelivery, "approved", "verifier", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := Save(root, &State{Phase: tc.phase, PRDStatus: tc.prdStatus}); err != nil {
				t.Fatalf("Save: %v", err)
			}
			err := GuardSpawn(root, EnforcementStrict, tc.subagent)
			if tc.wantBlocked {
				if err == nil {
					t.Fatal("expected guard block")
				}
				if !strings.Contains(err.Error(), "unblock:") {
					t.Fatalf("guard error must contain unblock path: %v", err)
				}
				if tc.wantContains != "" && !strings.Contains(err.Error(), tc.wantContains) {
					t.Fatalf("error %q must contain %q", err.Error(), tc.wantContains)
				}
			} else if err != nil {
				t.Fatalf("unexpected block: %v", err)
			}
		})
	}
}

func TestGuardSpawnInactiveWithoutStateFile(t *testing.T) {
	if err := GuardSpawn(t.TempDir(), EnforcementStrict, "worker"); err != nil {
		t.Fatalf("no state file must disable the guard, got %v", err)
	}
}

func TestGuardSpawnPromptOnly(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, &State{Phase: PhaseDiscovery}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := GuardSpawn(root, EnforcementPromptOnly, "worker"); err != nil {
		t.Fatalf("prompt_only must disable the guard, got %v", err)
	}
}

func TestGuardSpawnCorruptStateFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "no frontmatter here")
	err := GuardSpawn(root, EnforcementStrict, "worker")
	if err == nil {
		t.Fatal("corrupt state must fail closed")
	}
	if !strings.Contains(err.Error(), "unblock:") {
		t.Fatalf("error must contain unblock path: %v", err)
	}
}

func TestPRDApprovedFromPRDFile(t *testing.T) {
	root := t.TempDir()
	// State without prd_status, PRD.md carries the approval.
	if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	prdPath := filepath.Join(root, filepath.FromSlash(PRDFileRel))
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(prdPath, []byte("---\nstatus: approved\n---\n# PRD\n"), 0o644); err != nil {
		t.Fatalf("write PRD: %v", err)
	}
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err != nil {
		t.Fatalf("PRD.md approval must unblock worker, got %v", err)
	}
}

// The Lead decides how much ceremony a job gets — the prompt's "size the job
// first" step. The small shape it is told to declare has to actually pass
// every guard on the way to a worker, or the advice is a trap: 25 of the 51
// minutes of the first site-sandbox run went to ceremony for a 640-line brief.
func TestQuickShape_ExecutionWithWaiversAdmitsAWorker(t *testing.T) {
	root := t.TempDir()
	st := &State{Phase: PhaseExecution, Waivers: []string{WaiverPRD, WaiverContract}}

	if err := GuardPhaseTransition(root, EnforcementStrict, PhaseDiscovery, PhaseExecution, st); err != nil {
		t.Fatalf("declaring execution with a contract waiver must be allowed: %v", err)
	}
	if err := Save(root, st); err != nil {
		t.Fatal(err)
	}
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err != nil {
		t.Fatalf("no PRD and no frozen contract, but both waived: %v", err)
	}
	if err := GuardWorkOrderContract(root, EnforcementStrict, nil); err != nil {
		t.Fatalf("a WorkOrder with no contract_refs must pass under the contract waiver: %v", err)
	}

	// Without the waivers the same phase is refused, so the waivers are what
	// carries it, not a hole in the guard.
	bare := t.TempDir()
	if err := Save(bare, &State{Phase: PhaseExecution}); err != nil {
		t.Fatal(err)
	}
	if err := GuardSpawn(bare, EnforcementStrict, "worker"); err == nil {
		t.Fatal("execution without an approved PRD or a waiver must still be refused")
	}
}
