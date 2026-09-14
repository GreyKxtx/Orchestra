package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// The rule this file enforces: a tool is advertised to exactly the modes whose
// handler will accept it.
//
// Breaking it in one direction cost a day. general mode advertised task_result
// to main runs, the runtime refuses task_result outside a child, and the model
// was left holding the only finishing move it had been offered and could not
// use — a turn that did its work and then could not end. It read as model
// weakness in the eval table.
//
// The other direction is quieter and worse: a tool the handler accepts but
// nobody is offered simply never runs, and nothing anywhere reports that.
//
// Four tools carry a mode check in their handler: lesson_promote and
// playbook_promote (architecture or orchestra), update_working_state and
// contract_freeze (orchestra only). Rather than restating that list — a second
// copy drifts from the first, which is how knownCheckTypes went stale — each
// case here ASKS the handler and compares the answer to the advertisement.

// gatedTools maps a tool to a call the handler can be asked to perform. The
// input only has to get past JSON decoding; a mode refusal comes first.
var gatedTools = map[string]json.RawMessage{
	"lesson_promote":       json.RawMessage(`{"dept":"frontend","note":"prefer composition"}`),
	"playbook_promote":     json.RawMessage(`{"dept":"frontend"}`),
	"update_working_state": json.RawMessage(`{"content":"working"}`),
	"contract_freeze":      json.RawMessage(`{}`),
}

// everyMode is every mode a run can be launched in, so a tool cannot hide in
// one nobody thought to list.
var everyMode = []string{
	"build", "plan", "explore", "ask", "debug", "architecture", "agent",
	"general", "orchestra", "worker", "verifier", "product", "documentation",
}

func agentInMode(t *testing.T, mode Mode) *Agent {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	ag, err := New(&todoScriptLLM{}, v, tr, Options{Mode: mode, MaxSteps: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag
}

// refusedForMode reports whether the handler turns the call away because of
// the mode. Any other error (a missing workspace file, an empty draft) means
// the mode was accepted and the call failed later, which is what we want to
// distinguish.
func refusedForMode(t *testing.T, mode Mode, name string) bool {
	t.Helper()
	ag := agentInMode(t, mode)
	var err error
	switch name {
	case "lesson_promote":
		_, err = ag.handleLessonPromote(context.Background(), gatedTools[name])
	case "playbook_promote":
		_, err = ag.handlePlaybookPromote(context.Background(), gatedTools[name])
	case "update_working_state":
		_, err = ag.handleUpdateWorkingState(gatedTools[name])
	case "contract_freeze":
		_, err = ag.handleContractFreeze(context.Background())
	default:
		t.Fatalf("no handler wired for %q", name)
	}
	if err == nil {
		return false
	}
	// The four handlers phrase the same refusal three ways — "is available only
	// to Dept Lead (architecture) or Orchestra Lead", "is only available in
	// orchestra Lead mode", "is available only to the Orchestra Lead" — so
	// matching one spelling silently reported every mode as accepting
	// update_working_state. Match the idea, not the sentence.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "available only") || strings.Contains(msg, "only available")
}

func advertisedIn(mode string, name string) bool {
	for _, d := range tools.ListToolsForMode(mode, tools.Capabilities{Exec: true, Web: true, Browser: true}, true, true) {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

func TestGatedTools_AreOfferedToExactlyTheModesThatAcceptThem(t *testing.T) {
	for name := range gatedTools {
		for _, mode := range everyMode {
			offered := advertisedIn(mode, name)
			refused := refusedForMode(t, Mode(mode), name)

			switch {
			case offered && refused:
				t.Errorf("%s is offered to mode %q and its handler refuses it there. That is "+
					"the task_result defect: the model spends a step on a tool it was given "+
					"and gets told it may not use it", name, mode)
			case !offered && !refused:
				t.Errorf("mode %q may call %s and is never offered it, so the tool is dead "+
					"code in that mode and nothing reports it", mode, name)
			}
		}
	}
}

// The same rule for the one tool whose refusal is not about the mode at all:
// plan_exit has no mode check, so every mode offered it can actually use it.
// This is here so that adding a check to the handler without narrowing the
// listing — or the reverse — is caught with the rest.
func TestGatedTools_PlanExitIsOfferedOnlyWhereAPlanCanBeApproved(t *testing.T) {
	var offered []string
	for _, mode := range everyMode {
		if advertisedIn(mode, "plan_exit") {
			offered = append(offered, mode)
		}
	}
	if len(offered) == 0 {
		t.Fatal("no mode offers plan_exit, so the plan -> build switch can never be asked for")
	}
	for _, mode := range offered {
		if mode != "plan" && mode != "architecture" {
			t.Errorf("mode %q offers plan_exit. It switches the run into build mode, which "+
				"only makes sense from a mode that was planning; offered to: %v", mode, offered)
		}
	}
}
