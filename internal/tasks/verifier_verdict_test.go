package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// What does the verifier's verdict actually change?
//
// verifier is the one mode on the orchestra branch with no behavioural test at
// all — child_mode_surface_test.go pins which tools it is offered, and the unit
// tests pin VerifierResultPassed and the three wrap* functions in isolation.
// Nobody had checked that the verdict reaches the Lead, and the cost of being
// wrong there is the highest in the branch: the verdict is the last thing
// standing between a worker's claim and the user believing it.
//
// These drive the real TaskRunner with WorkerLLMVerifyEnabled on, so the
// arbitration in runWorkerRounds runs for real. The same scripted client plays
// both children — a verifier turn is recognised by the prompt
// formatLLMVerifierPrompt builds, which is the only thing the two runs do not
// share.

// verdictScriptLLM is a worker that does its job, followed by a verifier whose
// answer the test chooses.
type verdictScriptLLM struct {
	verdict      string // what the verifier writes in task_result
	verifierEdit bool   // the verifier tries to edit the code before answering
	workerCalls  int
	verifyCalls  int
}

func (s *verdictScriptLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func mkToolCall(id, name, args string) *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: id, Type: "function",
			Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(args)},
		}},
	}}
}

// isVerifierTurn: formatLLMVerifierPrompt opens with this line and nothing else
// in a worker run does, so it separates the two children reliably.
func isVerifierTurn(req llm.CompleteRequest) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "Verify the worker outcome against acceptance criteria") {
			return true
		}
	}
	return false
}

func (s *verdictScriptLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	if isVerifierTurn(req) {
		s.verifyCalls++
		// One edit attempt first, when the test asks for it: the verifier is
		// documented read-only, and a denial is only observable if something
		// actually tries.
		if s.verifierEdit && s.verifyCalls == 1 {
			return mkToolCall("v0", "edit", `{"path":"width.go","search":"return 1920","replace":"return 4096"}`), nil
		}
		return mkToolCall("v1", "task_result",
			`{"content":`+quoteJSON(s.verdict)+`}`), nil
	}
	s.workerCalls++
	// worker mode runs behind the explore-first gate: an edit before any read
	// of the WorkOrder scope is denied, so the read is load-bearing.
	if s.workerCalls == 1 {
		return mkToolCall("w0", "read", `{"path":"width.go"}`), nil
	}
	if s.workerCalls == 2 {
		return mkToolCall("w1", "edit", `{"path":"width.go","search":"return 640","replace":"return 1920"}`), nil
	}
	// task_result takes ONE argument, "content": the agent unmarshals into a
	// struct with that single field and ignores everything else, so a result
	// sent under any other key arrives empty.
	return mkToolCall("w2", "task_result",
		`{"content":"{\"status\":\"success\",\"path\":\"width.go\",\"summary\":\"Width now returns 1920\"}"}`), nil
}

// quoteJSON renders s as a JSON string literal.
func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// runVerifiedWorker spawns one worker with the LLM verifier enabled and returns
// what the Lead is handed, plus the workspace root so a test can look at it.
func runVerifiedWorker(t *testing.T, client *verdictScriptLLM) (result string, root string) {
	t.Helper()
	root = t.TempDir()
	const before = "package width\n\n// Width is the frame width in pixels.\nfunc Width() int {\n\treturn 640\n}\n"
	if err := os.WriteFile(filepath.Join(root, "width.go"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module evalws\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetDryRun(true)

	on := true
	r := New(client, v, tr, ChildAgentConfig{WorkerLLMVerifyEnabled: &on})
	t.Cleanup(func() { r.Close(); _ = tr.Close() })

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         "In width.go, change Width to return 1920.\ntarget_files: width.go",
		SubagentType: "worker", MaxSteps: 6, TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	res, err := r.Wait(context.Background(), id, 30_000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res == nil {
		t.Fatal("no result from the child")
	}
	if client.verifyCalls == 0 {
		t.Fatalf("the verifier never ran, so this test is measuring the wrong thing; "+
			"worker answered: %s", res.Result)
	}
	return res.Result, root
}

func TestVerifier_APassingVerdictIsWhatMarksTheWorkVerified(t *testing.T) {
	result, _ := runVerifiedWorker(t, &verdictScriptLLM{
		verdict: "Width returns 1920 as the WorkOrder asks.\n\n## VERIFICATION PASSED",
	})
	if !strings.Contains(result, `"status":"verified_success"`) {
		t.Errorf("a passing verdict must reach the Lead as verified_success:\n%s", result)
	}
	if !strings.Contains(result, `"llm_verified":true`) {
		t.Errorf("the Lead cannot tell a verifier-backed success from a checks-only one "+
			"unless llm_verified says so:\n%s", result)
	}
}

func TestVerifier_AFailingVerdictStopsTheWorkReadingAsDone(t *testing.T) {
	result, _ := runVerifiedWorker(t, &verdictScriptLLM{
		verdict: "Width still returns the old value.\n\n## VERIFICATION FAILED",
	})
	if strings.Contains(result, `"status":"verified_success"`) {
		t.Fatalf("the verifier rejected the work and the Lead was told it succeeded:\n%s", result)
	}
	if !strings.Contains(result, "llm_verification_failed") {
		t.Errorf("a red verdict must name itself; the Lead routes on status:\n%s", result)
	}
	// A rejection the Lead cannot act on is a dead end: the payload carries the
	// next move, not just the bad news.
	if !strings.Contains(result, "suggestion_for_lead") {
		t.Errorf("a failed verification gives the Lead nothing to do next:\n%s", result)
	}
}

// The fail-closed case, and the one a real 9B hits constantly: an approving
// answer that never writes the marker. VerifierResultPassed requires
// "## VERIFICATION PASSED" literally, so anything else — praise, an empty
// answer, a summary — must NOT come out as a pass.
func TestVerifier_AnApprovingAnswerWithoutTheMarkerIsNotAPass(t *testing.T) {
	result, _ := runVerifiedWorker(t, &verdictScriptLLM{
		verdict: "Looks good to me, the change is correct and complete.",
	})
	if strings.Contains(result, `"status":"verified_success"`) {
		t.Errorf("an answer with no verdict marker was accepted as a pass. The contract is "+
			"the marker, not the sentiment, or any friendly model output verifies "+
			"anything:\n%s", result)
	}
}

// The verifier judges code it must not be able to change. The tool surface
// already withholds edit; this asks the harder question — whether a model that
// calls it anyway is refused.
func TestVerifier_CannotEditTheCodeItIsJudging(t *testing.T) {
	client := &verdictScriptLLM{
		verdict:      "Width returns 1920.\n\n## VERIFICATION PASSED",
		verifierEdit: true,
	}
	result, root := runVerifiedWorker(t, client)

	onDisk, err := os.ReadFile(filepath.Join(root, "width.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "4096") {
		t.Fatalf("the verifier's own edit landed in the workspace:\n%s", onDisk)
	}
	// The denial must not derail the verdict either: a verifier that is refused
	// a tool still has to deliver its answer.
	if !strings.Contains(result, `"status":"verified_success"`) {
		t.Errorf("the verifier was denied an edit and its verdict never arrived:\n%s", result)
	}
}
