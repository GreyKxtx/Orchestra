package orchestrastate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/contract"
)

func setupContractEpoch(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(contract.DirRel), contract.ArtifactNFR)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("## Latency\np95 < 200ms\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.UpdateArtifact(root, contract.ArtifactNFR, "orchestrator"); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	e, _, err := contract.Load(root)
	if err != nil {
		t.Fatalf("Load epoch: %v", err)
	}
	return e.Artifacts[contract.ArtifactNFR].SHA256
}

func TestGuardWorkOrderContract(t *testing.T) {
	t.Run("inactive without state file", func(t *testing.T) {
		if err := GuardWorkOrderContract(t.TempDir(), EnforcementStrict, nil); err != nil {
			t.Fatalf("no state file must disable the guard: %v", err)
		}
	})

	t.Run("inactive in prompt_only", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
			t.Fatal(err)
		}
		setupContractEpoch(t, root)
		if err := GuardWorkOrderContract(root, EnforcementPromptOnly, nil); err != nil {
			t.Fatalf("prompt_only must disable the guard: %v", err)
		}
	})

	t.Run("no epoch and no refs — layer not adopted", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
			t.Fatal(err)
		}
		if err := GuardWorkOrderContract(root, EnforcementStrict, nil); err != nil {
			t.Fatalf("no EPOCH + no refs must pass: %v", err)
		}
	})

	t.Run("refs without epoch fail", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
			t.Fatal(err)
		}
		err := GuardWorkOrderContract(root, EnforcementStrict, []contract.Ref{{Path: contract.ArtifactNFR, SHA256: "aa"}})
		if err == nil || !strings.Contains(err.Error(), "unblock") {
			t.Fatalf("refs without EPOCH must fail with unblock path: %v", err)
		}
	})

	t.Run("execution with epoch requires refs", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
			t.Fatal(err)
		}
		setupContractEpoch(t, root)
		err := GuardWorkOrderContract(root, EnforcementStrict, nil)
		if err == nil || !strings.Contains(err.Error(), "contract_refs") {
			t.Fatalf("empty refs in execution must fail: %v", err)
		}
	})

	t.Run("valid refs pass, stale refs fail", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseExecution}); err != nil {
			t.Fatal(err)
		}
		good := setupContractEpoch(t, root)
		if err := GuardWorkOrderContract(root, EnforcementStrict, []contract.Ref{{Path: contract.ArtifactNFR, SHA256: good}}); err != nil {
			t.Fatalf("valid refs must pass: %v", err)
		}
		err := GuardWorkOrderContract(root, EnforcementStrict, []contract.Ref{{Path: contract.ArtifactNFR, SHA256: "deadbeef"}})
		if err == nil || !strings.Contains(err.Error(), "stale_contract") {
			t.Fatalf("stale refs must fail with stale_contract: %v", err)
		}
	})

	t.Run("maintenance bypasses everything", func(t *testing.T) {
		root := t.TempDir()
		if err := Save(root, &State{Phase: PhaseMaintenance}); err != nil {
			t.Fatal(err)
		}
		setupContractEpoch(t, root)
		if err := GuardWorkOrderContract(root, EnforcementStrict, nil); err != nil {
			t.Fatalf("maintenance must bypass contract guard: %v", err)
		}
	})
}
