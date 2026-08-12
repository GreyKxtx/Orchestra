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
