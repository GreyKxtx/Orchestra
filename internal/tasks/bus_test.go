package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/decisions"
)

// The bus (audit 4.5): a post is typed and carries its provenance; a
// question to the root is a question for the user, put through the barrier
// and answered in the receipt, on the record.

func childScope(address, dept, taskID string) agentScope {
	return agentScope{address: address, dept: dept, depth: 1, taskID: taskID, chain: []string{"orchestrator", address}}
}

func TestBus_AQuestionToTheRootIsAskedTheUserAndAnsweredInTheReceipt(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 0)
	asker := &scriptedAsker{answers: []string{"keep it 24 months"}}
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{QuestionAsker: asker}))

	receipt, err := r.post(context.Background(), childScope("backend@api", "backend", "task_1_1"), agent.AgentPostRequest{To: "lead", Kind: "question", Message: "How long do we keep audit logs?"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != "answered" || receipt.Answer != "keep it 24 months" || receipt.To != "user" {
		t.Fatalf("receipt: %+v", receipt)
	}
	if len(asker.asked) != 1 || len(asker.asked[0]) != 1 || !strings.Contains(asker.asked[0][0].Question, "[backend] How long") {
		t.Fatalf("asked: %+v", asker.asked)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if err != nil {
		t.Fatalf("decisions.md: %v", err)
	}
	if !strings.Contains(string(data), "How long do we keep audit logs?") || !strings.Contains(string(data), "keep it 24 months") {
		t.Fatalf("the question and its answer are not on the record:\n%s", data)
	}
	// The hub hears what was asked on its behalf, as the runtime's note.
	notes := r.DrainInbox()
	if len(notes) != 1 || notes[0].Origin != agent.InboxOriginRuntime || !strings.Contains(notes[0].Message, "backend@api asked the user") || !strings.Contains(notes[0].Message, "keep it 24 months") {
		t.Fatalf("root inbox: %+v", notes)
	}
	// The same question from another department is answered from the
	// first answer, and the user is not asked twice.
	receipt, err = r.post(context.Background(), childScope("frontend@web", "frontend", "task_2_1"), agent.AgentPostRequest{To: "root", Kind: "question", Message: "how long do we keep  audit logs?"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != "answered" || receipt.Answer != "keep it 24 months" || len(asker.asked) != 1 {
		t.Fatalf("second ask: receipt %+v, asked %d times", receipt, len(asker.asked))
	}
}

func TestBus_AQuestionWithNobodyToAskReachesTheOrchestrator(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 0)
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{}))
	receipt, err := r.post(context.Background(), childScope("backend@api", "backend", "task_1_1"), agent.AgentPostRequest{To: "lead", Kind: "question", Message: "Which region?"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != "live" || receipt.To != "orchestrator" || !strings.Contains(receipt.Note, "no interactive channel") {
		t.Fatalf("receipt: %+v", receipt)
	}
	notes := r.DrainInbox()
	if len(notes) != 1 || notes[0].Kind != "question" || notes[0].From != "backend@api" || notes[0].FromTaskID != "task_1_1" || notes[0].Depth != 1 || notes[0].Origin != "" {
		t.Fatalf("the question did not reach the root with its provenance: %+v", notes)
	}
}

func TestBus_AnExhaustedClarificationBudgetAnswersWithAnAssumption(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 2)
	asker := &scriptedAsker{answers: []string{"never asked"}}
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{QuestionAsker: asker, MaxClarificationRounds: 2}))
	receipt, err := r.post(context.Background(), childScope("backend@api", "backend", "task_1_1"), agent.AgentPostRequest{To: "lead", Kind: "question", Message: "Which region?"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != "unanswered" || receipt.Answer != "" || !strings.Contains(receipt.Note, "max_clarification_rounds") {
		t.Fatalf("receipt: %+v", receipt)
	}
	if len(asker.asked) != 0 {
		t.Fatal("the user was asked past the clarification budget")
	}
	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if !strings.Contains(string(data), "assumption") || !strings.Contains(string(data), "Which region?") {
		t.Fatalf("the forced assumption is not on the record:\n%s", data)
	}
}

func TestBus_AContractChangeRequestNeedsItsArtifactAndReachesTheHub(t *testing.T) {
	root := t.TempDir()
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{}))
	from := childScope("backend@api", "backend", "task_1_1")
	if _, err := r.post(context.Background(), from, agent.AgentPostRequest{To: "qa", Kind: "contract_change_request", Message: "add a field"}); err == nil || !strings.Contains(err.Error(), "needs artifact") {
		t.Fatalf("a change request without an artifact was accepted: %v", err)
	}
	receipt, err := r.post(context.Background(), from, agent.AgentPostRequest{To: "qa", Kind: "contract_change_request", Message: "add a field", Artifact: "docs/contract/API.md"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != "inbox" {
		t.Fatalf("qa is idle; receipt %+v", receipt)
	}
	notes := r.DrainInbox()
	if len(notes) != 1 || notes[0].Kind != "contract_change_request" || notes[0].Artifact != "docs/contract/API.md" || notes[0].To != "qa" {
		t.Fatalf("the hub did not get its copy: %+v", notes)
	}
}

func TestBus_RefusesWhatItCannotRoute(t *testing.T) {
	root := t.TempDir()
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{}))
	from := childScope("backend@api", "backend", "task_1_1")
	for _, tc := range []struct {
		req  agent.AgentPostRequest
		want string
	}{
		{agent.AgentPostRequest{To: "qa", Kind: "rumour", Message: "x"}, "kind \"rumour\" (want note|question|contract_change_request|finding|handoff)"},
		{agent.AgentPostRequest{To: "qa", Message: "  "}, "message is empty"},
		{agent.AgentPostRequest{To: "task_1_1", Message: "hi me"}, "you are task_1_1"},
		{agent.AgentPostRequest{To: "no such!", Message: "x"}, "not an agent address"},
		{agent.AgentPostRequest{To: "qa", Message: strings.Repeat("x", postMessageMaxBytes+1)}, "max 4000"},
	} {
		_, err := r.post(context.Background(), from, tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err %v, want %q", tc.req.To, err, tc.want)
		}
	}
	// The root talking to itself, and the agency off.
	if _, err := r.post(context.Background(), rootScope(), agent.AgentPostRequest{To: "lead", Message: "x"}); err == nil || !strings.Contains(err.Error(), "you are orchestrator") {
		t.Fatalf("root to root: %v", err)
	}
	off := barrierRunner(t, t.TempDir(), ChildAgentConfig{})
	if _, err := off.post(context.Background(), from, agent.AgentPostRequest{To: "qa", Message: "x"}); err == nil || !strings.Contains(err.Error(), "agency is off") {
		t.Fatalf("agency off: %v", err)
	}
}

func TestBus_ASubscriberHearsEveryPostOfItsKind(t *testing.T) {
	root := t.TempDir()
	r := barrierRunner(t, root, ChildAgencyConfigOn(ChildAgentConfig{}))
	var heard []post
	r.bus.subscribe(postFinding, func(p post) { heard = append(heard, p) })
	from := childScope("backend@api", "backend", "task_1_1")
	if _, err := r.post(context.Background(), from, agent.AgentPostRequest{To: "qa", Kind: "finding", Message: "the cache is stale"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.post(context.Background(), from, agent.AgentPostRequest{To: "qa", Kind: "note", Message: "not a finding"}); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || heard[0].message != "the cache is stale" || heard[0].from.taskID != "task_1_1" {
		t.Fatalf("subscriber heard %+v", heard)
	}
}

// ChildAgencyConfigOn is cfg with the agency on.
func ChildAgencyConfigOn(cfg ChildAgentConfig) ChildAgentConfig {
	cfg.Agency = agencyOn()
	return cfg
}
