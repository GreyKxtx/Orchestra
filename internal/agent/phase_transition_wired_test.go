package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

// A guard that works and is never called is the failure this codebase has
// already seen once: contract_freeze was implemented, wired to the phase
// machine and named in an unblock message while sitting in no tool list, so
// the freeze it demanded could only be bypassed. These tests cover the wiring
// — that update_working_state consults the transition gate — rather than the
// gate's own rules, which orchestrastate tests.

func orchestraLead(t *testing.T, enforcement string) (*Agent, string) {
	t.Helper()
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return &Agent{opts: Options{Mode: ModeOrchestra, PhaseEnforcement: enforcement}, tools: tr}, root
}

func stateDoc(phase string, extra ...string) string {
	lines := append([]string{"---", "orchestra:", "  phase: " + phase}, extra...)
	return strings.Join(lines, "\n") + "\n---\n\n## Goal\nship it\n"
}

func updateState(t *testing.T, a *Agent, content string) error {
	t.Helper()
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.handleUpdateWorkingState(json.RawMessage(body))
	return err
}

func TestUpdateWorkingState_RefusesATransitionWhoseConditionIsUnmet(t *testing.T) {
	a, root := orchestraLead(t, orchestrastate.EnforcementStrict)

	if err := updateState(t, a, stateDoc("contract")); err != nil {
		t.Fatalf("establishing the contract phase must succeed: %v", err)
	}
	// No EPOCH.yaml: the contract is not frozen, so execution stays shut.
	err := updateState(t, a, stateDoc("execution"))
	if err == nil {
		t.Fatal("execution was entered with no frozen contract")
	}
	if !strings.Contains(err.Error(), "unblock:") {
		t.Fatalf("refusal must carry an unblock path: %v", err)
	}

	// The refusal must also leave the file alone: a Lead that reads back the
	// phase it asked for would act as though the transition happened.
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		t.Fatalf("state file lost: found=%v err=%v", found, err)
	}
	if st.Phase != orchestrastate.PhaseContract {
		t.Fatalf("refused transition still changed the phase to %q", st.Phase)
	}
}

func TestUpdateWorkingState_AllowsATransitionWhoseConditionIsMet(t *testing.T) {
	a, root := orchestraLead(t, orchestrastate.EnforcementStrict)
	if err := updateState(t, a, stateDoc("execution")); err != nil {
		t.Fatalf("bootstrapping a session must never be blocked: %v", err)
	}
	if err := updateState(t, a, stateDoc("delivery")); err != nil {
		t.Fatalf("delivery with no doc debt must be allowed: %v", err)
	}
	st, _, err := orchestrastate.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != orchestrastate.PhaseDelivery {
		t.Fatalf("phase = %q, want delivery", st.Phase)
	}
}

func TestUpdateWorkingState_DocDebtHoldsDelivery(t *testing.T) {
	a, root := orchestraLead(t, orchestrastate.EnforcementStrict)
	if err := updateState(t, a, stateDoc("execution")); err != nil {
		t.Fatal(err)
	}
	if err := orchestrastate.AddDocDebt(root, "docs/api.md"); err != nil {
		t.Fatalf("AddDocDebt: %v", err)
	}
	// The Lead writes its own document, so the debt has to travel in it —
	// this is what a Lead that read the state file back would send.
	withDebt := stateDoc("delivery", "  doc_debt:", "    - docs/api.md")
	err := updateState(t, a, withDebt)
	if err == nil {
		t.Fatal("delivery was entered with outstanding doc debt")
	}
	if !strings.Contains(err.Error(), "docs/api.md") {
		t.Fatalf("refusal must name the file still owed: %v", err)
	}
}

func TestUpdateWorkingState_PromptOnlyLeavesTheMachineAdvisory(t *testing.T) {
	a, _ := orchestraLead(t, orchestrastate.EnforcementPromptOnly)
	if err := updateState(t, a, stateDoc("contract")); err != nil {
		t.Fatal(err)
	}
	if err := updateState(t, a, stateDoc("execution")); err != nil {
		t.Fatalf("prompt_only must not enforce transitions: %v", err)
	}
}

// state.md is a markdown document the Lead also uses for goals and epic notes.
// Content without frontmatter carries no phase, so it is not a transition and
// must still be written.
func TestUpdateWorkingState_ContentWithoutFrontmatterIsNotATransition(t *testing.T) {
	a, root := orchestraLead(t, orchestrastate.EnforcementStrict)
	if err := updateState(t, a, stateDoc("contract")); err != nil {
		t.Fatal(err)
	}
	if err := updateState(t, a, "## Goal\njust notes, no frontmatter\n"); err != nil {
		t.Fatalf("a plain note must not be refused: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".orchestra", "state.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "just notes") {
		t.Fatalf("note was not written: %q", string(b))
	}
}

// The Lead may size a job as small and waive prd/contract itself; the other
// waivers and the runtime's own fields are not its to write.
func TestUpdateWorkingState_KeepsRuntimeOwnedFields(t *testing.T) {
	a, root := orchestraLead(t, orchestrastate.EnforcementStrict)
	if err := updateState(t, a, stateDoc("execution", "  waivers: [prd, contract]")); err != nil {
		t.Fatalf("prd/contract waivers are the Lead's to declare: %v", err)
	}
	if err := orchestrastate.AddDocDebt(root, "docs/api.md"); err != nil {
		t.Fatal(err)
	}

	// Waiving doc_debt, or writing it away, does not open delivery.
	for _, extra := range [][]string{
		{"  waivers: [prd, contract, doc_debt]"},
		{"  waivers: [prd, contract]", "  doc_debt: []"},
	} {
		err := updateState(t, a, stateDoc("delivery", extra...))
		if err == nil || !strings.Contains(err.Error(), "doc_debt") {
			t.Fatalf("delivery with owed docs must be refused (%v), got %v", extra, err)
		}
	}
	st, _, err := orchestrastate.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.DocDebt) != 1 || st.HasWaiver(orchestrastate.WaiverDocDebt) {
		t.Fatalf("doc_debt must stay as the runtime recorded it: %+v", st)
	}

	// A waiver the user wrote into the file survives the Lead's rewrite.
	st.Waivers = append(st.Waivers, orchestrastate.WaiverDocDebt)
	if err := orchestrastate.Save(root, st); err != nil {
		t.Fatal(err)
	}
	if err := updateState(t, a, stateDoc("delivery", "  waivers: [prd, contract]")); err != nil {
		t.Fatalf("the user's doc_debt waiver opens delivery: %v", err)
	}
}
