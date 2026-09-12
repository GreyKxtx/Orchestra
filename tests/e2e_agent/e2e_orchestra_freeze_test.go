package e2e_agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// contract_freeze is the G6 gate: it verifies the contract artifacts, asks the
// User to approve, hashes everything into EPOCH.yaml and syncs the epoch into
// state.md so the phase machine can move on. All of that was implemented and
// reachable from nowhere — the tool sat in no tool list, and the Lead
// allowlist did not name it, so the only way past phase contract was a
// hand-written waiver.
//
// These tests drive a scripted Lead through the real agent loop and check the
// tool is offered, that it does its work, and that it fails closed.

// freezeLeadLLM calls contract_freeze once, then finishes.
type freezeLeadLLM struct {
	step        int
	sawTool     bool
	toolNames   []string
	freezeError string
}

func (l *freezeLeadLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *freezeLeadLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if l.step == 0 {
		for _, d := range req.Tools {
			l.toolNames = append(l.toolNames, d.Function.Name)
			if d.Function.Name == "contract_freeze" {
				l.sawTool = true
			}
		}
	}
	// Remember what the runtime said when the freeze was refused.
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "artifact_verify failed") {
			l.freezeError = m.Content
		}
	}

	if l.step == 0 {
		l.step++
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       "call_freeze",
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "contract_freeze", Arguments: llm.ToolArguments(`{}`)},
			}},
		}}, nil
	}
	l.step++
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

// answerAsker answers every gate with the same word.
type answerAsker struct {
	answer string
	asked  []string
}

func (a *answerAsker) Ask(_ context.Context, qs []session.QuestionItem) ([]string, error) {
	out := make([]string, len(qs))
	for i, q := range qs {
		a.asked = append(a.asked, q.Question)
		out[i] = a.answer
	}
	return out, nil
}

// contractFixture writes four artifacts that pass VerifyArtifacts.
func contractFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(contract.DirRel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		contract.ArtifactDomainModel: "# Domain\n\n## Order\nAn order a customer places.\n\n## Customer\nSomeone who orders.\n",
		contract.ArtifactNFR:         "# NFR\n\n## Latency\np99 under 200ms.\n\n## Availability\n99.9 percent monthly.\n",
		contract.ArtifactOpenAPI:     "openapi: 3.0.0\ninfo:\n  title: Orders\n  version: \"0.1\"\npaths:\n  /orders:\n    get:\n      responses:\n        \"200\":\n          description: ok\n",
		contract.ArtifactUITokens:    "{\n  \"color\": { \"primary\": \"#101010\" }\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newFreezeAgent(t *testing.T, root string, lead *freezeLeadLLM, asker *answerAsker) *agent.Agent {
	t.Helper()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	opts := agent.Options{
		Mode:       agent.ModeOrchestra,
		MaxSteps:   6,
		Apply:      true,
		HumanGates: map[string]bool{"contract_freeze": true},
	}
	if asker != nil {
		opts.QuestionAsker = asker
	}
	ag, err := agent.New(lead, v, tr, opts)
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}
	return ag
}

func TestOrchestra_E2E_ContractFreeze_ReachesTheLeadAndWritesTheEpoch(t *testing.T) {
	root := contractFixture(t)
	lead := &freezeLeadLLM{}
	asker := &answerAsker{answer: "yes"}
	ag := newFreezeAgent(t, root, lead, asker)

	if _, _, err := ag.Run(context.Background(), nil, "freeze the contract"); err != nil {
		t.Fatalf("orchestra run: %v", err)
	}

	if !lead.sawTool {
		t.Fatalf("contract_freeze was not offered to the Lead; its schema was: %v", lead.toolNames)
	}
	if len(asker.asked) == 0 {
		t.Error("the G6 gate must ask the user before freezing")
	} else if !strings.Contains(asker.asked[0], "G6") {
		t.Errorf("the question must identify the gate, got: %q", asker.asked[0])
	}

	epoch := filepath.Join(root, filepath.FromSlash(contract.EpochFileRel))
	body, err := os.ReadFile(epoch)
	if err != nil {
		t.Fatalf("EPOCH.yaml must be written: %v", err)
	}
	for _, name := range contract.RequiredArtifacts {
		if !strings.Contains(string(body), name) {
			t.Errorf("EPOCH.yaml must hash %s, got:\n%s", name, body)
		}
	}

	st, found, err := orchestrastate.Load(root)
	if err == nil && found && st.ContractEpoch == 0 {
		t.Error("the epoch must be mirrored into state.md so the phase machine sees it")
	}
}

// Fail closed: a declined gate must not freeze anything.
func TestOrchestra_E2E_ContractFreeze_DeclinedGateWritesNothing(t *testing.T) {
	root := contractFixture(t)
	lead := &freezeLeadLLM{}
	asker := &answerAsker{answer: "no"}
	ag := newFreezeAgent(t, root, lead, asker)

	_, _, _ = ag.Run(context.Background(), nil, "freeze the contract")

	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(contract.EpochFileRel))); err == nil {
		t.Error("a declined G6 gate must not write EPOCH.yaml")
	}
}

// Fail closed: broken artifacts must be reported to the model, not frozen.
func TestOrchestra_E2E_ContractFreeze_RefusesIncompleteArtifacts(t *testing.T) {
	root := contractFixture(t)
	// Empty the NFR: headings without content do not pass the gate.
	nfr := filepath.Join(root, filepath.FromSlash(contract.DirRel), contract.ArtifactNFR)
	if err := os.WriteFile(nfr, []byte("# NFR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lead := &freezeLeadLLM{}
	asker := &answerAsker{answer: "yes"}
	ag := newFreezeAgent(t, root, lead, asker)

	_, _, _ = ag.Run(context.Background(), nil, "freeze the contract")

	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(contract.EpochFileRel))); err == nil {
		t.Error("incomplete artifacts must not be frozen")
	}
	if lead.freezeError == "" {
		t.Error("the model must be told which artifact failed, so it can fix it and call again")
	} else if !strings.Contains(lead.freezeError, contract.ArtifactNFR) {
		t.Errorf("the refusal must name the offending artifact, got: %s", lead.freezeError)
	}
}

// update_working_state is the Lead's other gate tool: it writes state.md with
// a phase stamp that a plain write does not produce.
type scratchpadLeadLLM struct {
	step    int
	sawTool bool
}

func (l *scratchpadLeadLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *scratchpadLeadLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if l.step == 0 {
		for _, d := range req.Tools {
			if d.Function.Name == "update_working_state" {
				l.sawTool = true
			}
		}
		l.step++
		input, _ := json.Marshal(map[string]any{
			"content": "# Orchestra state\n\nphase: contract\n",
		})
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       "call_ws",
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "update_working_state", Arguments: llm.ToolArguments(input)},
			}},
		}}, nil
	}
	l.step++
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

func TestOrchestra_E2E_UpdateWorkingState_ReachesTheLeadAndWrites(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	lead := &scratchpadLeadLLM{}
	ag, err := agent.New(lead, v, tr, agent.Options{
		Mode: agent.ModeOrchestra, MaxSteps: 6, Apply: true,
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "record the phase"); err != nil {
		t.Fatalf("orchestra run: %v", err)
	}

	if !lead.sawTool {
		t.Fatal("update_working_state was not offered to the Lead")
	}
	body, err := os.ReadFile(filepath.Join(root, ".orchestra", "state.md"))
	if err != nil {
		t.Fatalf("state.md must be written: %v", err)
	}
	if !strings.Contains(string(body), "phase: contract") {
		t.Errorf("state.md must carry what the Lead wrote, got:\n%s", body)
	}
}
