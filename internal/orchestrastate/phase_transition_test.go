package orchestrastate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/wsview"
)

// The Lead drives the session by writing state.md, and that write was
// unchecked: any phase could be set from any other. So "execution opens once
// the contract is frozen" and "delivery waits for doc_debt to clear" — both
// conditions the spec's transition table states — were a diagram the model
// could step around by writing the phase it wanted.

func writeConventions(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ConventionsFileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# conventions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeEpoch records a frozen contract the way contract_freeze does.
func writeEpoch(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(contract.EpochFileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("epoch: 1\nartifacts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPhaseTransitionMatrix(t *testing.T) {
	cases := []struct {
		name         string
		from, to     Phase
		state        State
		setup        func(t *testing.T, root string)
		wantBlocked  bool
		wantContains string
	}{
		{name: "documentation needs an approved PRD", from: PhaseDiscovery, to: PhaseDocumentation,
			wantBlocked: true, wantContains: "approved PRD"},
		{name: "documentation opens once the PRD is approved", from: PhaseDiscovery, to: PhaseDocumentation,
			state: State{PRDStatus: "approved"}},
		{name: "documentation opens under a PRD waiver", from: PhaseDiscovery, to: PhaseDocumentation,
			state: State{Waivers: []string{WaiverPRD}}},

		{name: "contract needs L1 conventions", from: PhaseDocumentation, to: PhaseContract,
			wantBlocked: true, wantContains: "conventions"},
		{name: "contract opens once conventions exist", from: PhaseDocumentation, to: PhaseContract,
			setup: writeConventions},
		{name: "contract opens under a playbooks waiver", from: PhaseDocumentation, to: PhaseContract,
			state: State{Waivers: []string{WaiverPlaybooks}}},

		{name: "execution needs a frozen contract", from: PhaseContract, to: PhaseExecution,
			wantBlocked: true, wantContains: "frozen contract"},
		{name: "execution opens once the contract is frozen", from: PhaseContract, to: PhaseExecution,
			setup: writeEpoch},
		{name: "execution opens under a contract waiver", from: PhaseContract, to: PhaseExecution,
			state: State{Waivers: []string{WaiverContract}}},

		{name: "delivery waits for doc_debt", from: PhaseExecution, to: PhaseDelivery,
			state: State{DocDebt: []string{"docs/api.md"}}, wantBlocked: true, wantContains: "doc_debt"},
		{name: "delivery names the files still owed", from: PhaseExecution, to: PhaseDelivery,
			state: State{DocDebt: []string{"docs/api.md"}}, wantBlocked: true, wantContains: "docs/api.md"},
		{name: "delivery opens with no debt", from: PhaseExecution, to: PhaseDelivery},
		{name: "delivery opens under a doc_debt waiver", from: PhaseExecution, to: PhaseDelivery,
			state: State{DocDebt: []string{"docs/api.md"}, Waivers: []string{WaiverDocDebt}}},

		// maintenance is itself an unblock path, and a scope change reopens
		// discovery from anywhere — neither is ever refused.
		{name: "maintenance is always reachable", from: PhaseContract, to: PhaseMaintenance},
		{name: "discovery reopens on scope change", from: PhaseExecution, to: PhaseDiscovery},

		// Not a transition: no phase change, and the first write of a state
		// file has no prior phase to move from.
		{name: "same phase is not a transition", from: PhaseContract, to: PhaseContract},
		{name: "bootstrapping a session is never blocked", from: "", to: PhaseExecution},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, root)
			}
			next := tc.state
			next.Phase = tc.to
			err := GuardPhaseTransition(root, wsview.Disk(root), EnforcementStrict, tc.from, tc.to, &next)
			if tc.wantBlocked {
				if err == nil {
					t.Fatalf("%s → %s: expected a refusal", tc.from, tc.to)
				}
				if !strings.Contains(err.Error(), "unblock:") {
					t.Fatalf("refusal must carry an unblock path: %v", err)
				}
				if tc.wantContains != "" && !strings.Contains(err.Error(), tc.wantContains) {
					t.Fatalf("refusal %q must mention %q", err.Error(), tc.wantContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s → %s: unexpected refusal: %v", tc.from, tc.to, err)
			}
		})
	}
}

// prompt_only keeps the state machine advisory, exactly as it does for spawns.
func TestPhaseTransitionPromptOnlyNeverBlocks(t *testing.T) {
	root := t.TempDir()
	next := State{Phase: PhaseExecution, DocDebt: []string{"docs/api.md"}}
	for _, to := range []Phase{PhaseDocumentation, PhaseContract, PhaseExecution, PhaseDelivery} {
		next.Phase = to
		if err := GuardPhaseTransition(root, wsview.Disk(root), EnforcementPromptOnly, PhaseDiscovery, to, &next); err != nil {
			t.Errorf("prompt_only blocked %s: %v", to, err)
		}
	}
}

// A project that never opted into the state machine writes no state.md, so the
// guard is reached only through update_working_state, which is Lead-only. This
// pins the other half: the PRD gate reads the incoming document, so a Lead
// that approves the PRD in the same write it changes phase is not refused.
func TestPhaseTransitionReadsTheIncomingDocument(t *testing.T) {
	root := t.TempDir()
	next := State{Phase: PhaseDocumentation, PRDStatus: "approved"}
	if err := GuardPhaseTransition(root, wsview.Disk(root), EnforcementStrict, PhaseDiscovery, PhaseDocumentation, &next); err != nil {
		t.Fatalf("approving the PRD in the same write must satisfy the gate: %v", err)
	}
}
