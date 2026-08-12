package orchestrastate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasWaiver(t *testing.T) {
	st := &State{Waivers: []string{"Contract", " prd "}}
	if !st.HasWaiver(WaiverContract) || !st.HasWaiver(WaiverPRD) {
		t.Fatal("case/space-insensitive waiver match expected")
	}
	if st.HasWaiver("deploy") {
		t.Fatal("names outside the closed waivable set must never match")
	}
	var nilState *State
	if nilState.HasWaiver(WaiverPRD) {
		t.Fatal("nil state has no waivers")
	}
}

func TestGuardSpawn_WaiverPRD(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "---\norchestra:\n  phase: execution\n  prd_status: draft\n---\n")
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err == nil {
		t.Fatal("draft PRD without waiver must block worker spawn")
	}
	writeState(t, root, "---\norchestra:\n  phase: execution\n  prd_status: draft\n  waivers: [prd]\n---\n")
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err != nil {
		t.Fatalf("waiver 'prd' must unblock: %v", err)
	}
}

func TestGuardSpawn_WaiverContractPhase(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "---\norchestra:\n  phase: contract\n  prd_status: approved\n---\n")
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err == nil {
		t.Fatal("phase contract without waiver must block workers")
	}
	writeState(t, root, "---\norchestra:\n  phase: contract\n  prd_status: approved\n  waivers: [contract]\n---\n")
	if err := GuardSpawn(root, EnforcementStrict, "worker"); err != nil {
		t.Fatalf("waiver 'contract' must unblock: %v", err)
	}
}

func TestGuardWorkOrderContract_Waiver(t *testing.T) {
	root := t.TempDir()
	// EPOCH exists → execution WorkOrder without refs is normally invalid.
	if err := os.MkdirAll(filepath.Join(root, ".orchestra", "contract"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "contract", "EPOCH.yaml"), []byte("epoch: 1\nartifacts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, root, "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n")
	if err := GuardWorkOrderContract(root, EnforcementStrict, nil); err == nil {
		t.Fatal("missing contract_refs in execution must block without waiver")
	}
	writeState(t, root, "---\norchestra:\n  phase: execution\n  prd_status: approved\n  waivers: [contract]\n---\n")
	if err := GuardWorkOrderContract(root, EnforcementStrict, nil); err != nil {
		t.Fatalf("waiver 'contract' must bypass the refs requirement: %v", err)
	}
}

func TestArchiveOverflow(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	body.WriteString("## Goal\nbuild the thing\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&body, "## Epic %d\n%s\n", i, strings.Repeat("x", 100))
	}
	writeState(t, root, "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n"+body.String())

	rel, err := ArchiveOverflow(root, 4096)
	if err != nil {
		t.Fatalf("ArchiveOverflow: %v", err)
	}
	if rel == "" {
		t.Fatal("oversized state must be archived")
	}
	arch, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("archive file: %v", err)
	}
	if !strings.Contains(string(arch), "## Epic 0") {
		t.Fatal("archive must contain the trimmed head (oldest epics)")
	}

	st, found, err := Load(root)
	if err != nil || !found {
		t.Fatalf("reload state: %v found=%v", err, found)
	}
	if st.Phase != PhaseExecution {
		t.Fatalf("frontmatter must survive archiving, got phase %q", st.Phase)
	}
	if !strings.Contains(st.Body, rel) {
		t.Fatal("state body must reference the archive file")
	}
	if !strings.Contains(st.Body, "## Epic 199") {
		t.Fatal("recent tail must stay in state.md")
	}
	if len(st.Body) > 4096 {
		t.Fatalf("trimmed body still too large: %d bytes", len(st.Body))
	}

	// Under budget → no-op.
	if rel2, err := ArchiveOverflow(root, 1<<20); err != nil || rel2 != "" {
		t.Fatalf("no-op expected under budget, got %q err=%v", rel2, err)
	}
}
