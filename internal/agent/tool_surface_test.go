package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func stagedPaths(ag *Agent) []string {
	var out []string
	for _, op := range ag.tools.StagedOps(context.Background()) {
		out = append(out, op.Path)
	}
	return out
}

func finalWithPatch(path string) *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[{"type":"file.write_atomic","path":"` + path + `","content":"package x\n"}]}}`}}
}

// An explore child has no write tool. Naming it anyway used to stage the file.
func TestUnofferedToolIsRefused(t *testing.T) {
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{{
		Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{writeCall("w1", "sneaky.go")}},
	}}}
	ag, tr := newTestAgent(t, llmClient, Options{Mode: ModeExplore, IsChild: true})
	tr.SetDryRun(true)
	hist, _, err := ag.Run(context.Background(), nil, "look around")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var reply string
	for _, m := range toolMessages(hist) {
		if m.ToolCallID == "w1" {
			reply = m.Content
		}
	}
	if !strings.Contains(reply, "not available in explore mode") {
		t.Fatalf("write from an explore child must be refused, got %q", reply)
	}
	if staged := stagedPaths(ag); len(staged) != 0 {
		t.Fatalf("nothing may be staged, got %v", staged)
	}
}

// A worker scoped to a.go returning a final patch for another file, and an
// explore child returning one at all: both used to be staged and flushed.
func TestFinalPatchesMeetTheWriteRules(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		path string
		want string
	}{
		{"worker outside target_files", Options{Mode: ModeWorker, IsChild: true, WorkerEditPaths: []string{"a.go"}}, "evil.go", "evil.go"},
		{"explore child", Options{Mode: ModeExplore, IsChild: true}, "evil.go", "does not change files"},
		{"plan mode", Options{Mode: ModePlan}, "main.go", "outside the files this mode may write"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			llmClient := &recordingPatchLLM{first: finalWithPatch(c.path)}
			ag, tr := newTestAgent(t, llmClient, c.opts)
			tr.SetDryRun(true)
			_, _, _ = ag.Run(context.Background(), nil, "do it")
			if staged := stagedPaths(ag); len(staged) != 0 {
				t.Fatalf("a refused final patch must not be staged, got %v", staged)
			}
			if !strings.Contains(llmClient.refusal, "final.patches refused") || !strings.Contains(llmClient.refusal, c.want) {
				t.Fatalf("the model must be told why, got %q", llmClient.refusal)
			}
		})
	}
}

// recordingPatchLLM returns one final patch, then records the refusal the
// agent sent back and ends with an empty final.
type recordingPatchLLM struct {
	first   *llm.CompleteResponse
	calls   int
	refusal string
}

func (r *recordingPatchLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (r *recordingPatchLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	r.calls++
	if r.calls == 1 {
		return r.first, nil
	}
	for _, m := range req.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "final.patches refused") {
			r.refusal = m.Content
		}
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

// Plan, architecture and ask at the top level promise not to change files;
// their delegation list shows only roles that keep that promise, and the
// requests they send carry it for the runner to enforce.
func TestReadOnlyModesDelegateOnlyToReaders(t *testing.T) {
	fake := &fakeAgency{info: AgencyInfo{
		Enabled: true,
		Self:    "orchestrator",
		Delegates: []AgentCard{
			{Name: "explore", Role: "explore", BuiltIn: true},
			{Name: "worker", Role: "worker", BuiltIn: true},
			{Name: "reviewer", Role: "verifier"},
		},
	}}
	ag, _ := newTestAgent(t, &toolCallSequenceLLM{}, Options{Mode: ModePlan, SubtaskRunner: fake})
	task, ok := toolByName(ag.buildToolDefs(), "task")
	if !ok {
		t.Fatal("plan mode keeps task for explore")
	}
	enum := string(task.Function.Parameters)
	if strings.Contains(enum, `"worker"`) || !strings.Contains(enum, `"explore"`) || !strings.Contains(enum, `"reviewer"`) {
		t.Fatalf("plan mode must be offered readers only: %s", enum)
	}
	if !ag.readOnlyChildren() {
		t.Fatal("plan mode spawns read-only children")
	}
	child, _ := newTestAgent(t, &toolCallSequenceLLM{}, Options{Mode: ModeArchitecture, IsChild: true, SubtaskRunner: fake})
	if child.readOnlyChildren() {
		t.Fatal("a Dept Lead hands out WorkOrders; its reach is agency.flows")
	}
}
