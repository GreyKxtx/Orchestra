package orchestrastate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/wsview"
)

// staged shows files over the disk, as an agent's overlay does.
type staged struct {
	wsview.Disk
	files map[string]string
}

func (s staged) ReadFile(rel string) ([]byte, error) {
	if c, ok := s.files[rel]; ok {
		return []byte(c), nil
	}
	return s.Disk.ReadFile(rel)
}

// The PRD and the conventions are written by product and documentation, who
// stage what they write. The gates judge the project as the Lead sees it, so
// a phase opens in the turn its artifact was written — not a turn later, and
// not never in a run that previews (ORC-6).
func TestGates_ReadTheStagedArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, StateFileRel), []byte("---\norchestra:\n  phase: execution\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := wsview.Disk(root)
	view := staged{Disk: disk, files: map[string]string{
		PRDFileRel:         "---\nstatus: approved\n---\n# PRD\n",
		ConventionsFileRel: "# Conventions\n",
	}}

	if err := GuardSpawn(root, disk, EnforcementStrict, "worker"); err == nil {
		t.Fatal("with nothing approved the writer is refused")
	}
	if err := GuardSpawn(root, view, EnforcementStrict, "worker"); err != nil {
		t.Fatalf("the staged approved PRD opens the gate: %v", err)
	}

	next := &State{Phase: PhaseContract}
	if err := GuardPhaseTransition(root, disk, EnforcementStrict, PhaseDocumentation, PhaseContract, next); err == nil {
		t.Fatal("contract needs the conventions")
	}
	if err := GuardPhaseTransition(root, view, EnforcementStrict, PhaseDocumentation, PhaseContract, next); err != nil {
		t.Fatalf("the staged conventions open the contract phase: %v", err)
	}
	next = &State{Phase: PhaseDocumentation}
	if err := GuardPhaseTransition(root, view, EnforcementStrict, PhaseDiscovery, PhaseDocumentation, next); err != nil {
		t.Fatalf("the staged approved PRD opens documentation: %v", err)
	}
}
