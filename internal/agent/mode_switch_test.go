package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// Every mode can ask to become another: a read-only mode that needs an edit,
// a build turn that wants to plan first. The user answers; the turn goes on in
// the mode they agreed to, with its history, instead of ending on a refusal.

// scriptLLM answers each request with the next response in turn, then
// finishes.
type scriptLLM struct {
	steps []*llm.CompleteResponse
	calls int
}

func (s *scriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *scriptLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.calls++
	if s.calls <= len(s.steps) {
		return s.steps[s.calls-1], nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"patches":[]}`}}, nil
}

const greetEdit = `{"path":"greet.go","search":"\"hello\"","replace":"\"good morning\""}`

func TestModeSwitch_ApprovedSwitchGoesOnInTheNewModeAndTheChangeLands(t *testing.T) {
	tr, v, root := planWorkspace(t)
	client := &scriptLLM{steps: []*llm.CompleteResponse{
		toolCallResponse("s1", "mode_switch", `{"mode":"build","reason":"Greet must change"}`),
		toolCallResponse("s2", "edit", greetEdit), // the build half
	}}
	asker := &approvingAsker{answer: "Yes, switch to build"}
	opts := Options{Mode: ModeAsk, MaxSteps: 8, Apply: true, QuestionAsker: asker}

	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hist, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("ask turn: %v", err)
	}
	if res == nil || res.SwitchToMode != ModeBuild {
		t.Fatalf("an approved mode_switch must end the ask half asking for build, got %+v", res)
	}
	if asker.asked != 1 {
		t.Errorf("mode_switch must ask once, asked %d times", asker.asked)
	}
	if last := hist[len(hist)-1]; last.Role != llm.RoleTool || last.ToolCallID != "s1" {
		t.Errorf("the mode_switch call must be answered in history before the turn goes on, last = %+v", last)
	}

	_, merged, err := ContinueInSwitchedMode(context.Background(), client, v, tr, opts, hist, res)
	if err != nil {
		t.Fatalf("build half: %v", err)
	}
	if merged.SwitchToMode != "" {
		t.Errorf("the merged result must not ask for another switch: %q", merged.SwitchToMode)
	}
	b, _ := os.ReadFile(filepath.Join(root, "greet.go"))
	if !strings.Contains(string(b), "good morning") {
		t.Fatalf("the build half's edit never landed:\n%s", b)
	}
}

// A local model rarely thinks of mode_switch; it calls the tool it needs and
// is refused. The agent asks the user itself.
func TestModeSwitch_ARefusedWriteAsksTheUserToSwitch(t *testing.T) {
	tr, v, _ := planWorkspace(t)
	client := &scriptLLM{steps: []*llm.CompleteResponse{
		toolCallResponse("w1", "edit", greetEdit),
	}}
	asker := &approvingAsker{answer: "1"}
	ag, err := New(client, v, tr, Options{Mode: ModeAsk, MaxSteps: 8, QuestionAsker: asker})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hist, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.asked != 1 {
		t.Fatalf("a write refused for the mode must ask to switch, asked %d times", asker.asked)
	}
	if res == nil || res.SwitchToMode != ModeBuild {
		t.Fatalf("an approved switch must end the turn asking for build, got %+v", res)
	}
	last := hist[len(hist)-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "w1" || !strings.Contains(last.Content, "build") {
		t.Errorf("the refused call must be answered, saying the turn now goes on in build: %+v", last)
	}
}

// No means no for the rest of the turn: the model is told to stay, and a
// second refused call is not put to the user again.
func TestModeSwitch_ADeclinedSwitchIsNotAskedAgain(t *testing.T) {
	tr, v, _ := planWorkspace(t)
	client := &scriptLLM{steps: []*llm.CompleteResponse{
		toolCallResponse("w1", "edit", greetEdit),
		toolCallResponse("w2", "write", `{"path":"greet.go","content":"package main\n"}`),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "Change Greet's string to \"good morning\"."}},
	}}
	asker := &approvingAsker{answer: "No, stay in ask"}
	ag, err := New(client, v, tr, Options{Mode: ModeAsk, MaxSteps: 8, QuestionAsker: asker})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hist, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.asked != 1 {
		t.Errorf("a declined switch must not be asked again in the turn, asked %d times", asker.asked)
	}
	if res != nil && res.SwitchToMode != "" {
		t.Errorf("a declined switch must not switch: %q", res.SwitchToMode)
	}
	var told bool
	for _, m := range hist {
		if m.Role == llm.RoleTool && m.ToolCallID == "w1" && strings.Contains(m.Content, "declined") {
			told = true
		}
	}
	if !told {
		t.Error("the model must be told the user declined the switch")
	}
}

// Only a turn with someone to ask offers mode_switch, and only at the top:
// a child's mode is its caller's choice.
func TestModeSwitch_OfferedToTopLevelTurnsWithAUser(t *testing.T) {
	tr, v, _ := planWorkspace(t)
	for _, tc := range []struct {
		name string
		opts Options
		want bool
	}{
		{"ask with a user", Options{Mode: ModeAsk, QuestionAsker: &approvingAsker{}}, true},
		{"build with a user", Options{Mode: ModeBuild, QuestionAsker: &approvingAsker{}}, true},
		{"no user", Options{Mode: ModeAsk}, false},
		{"a child", Options{Mode: ModeExplore, IsChild: true, QuestionAsker: &approvingAsker{}}, false},
	} {
		ag, err := New(&scriptLLM{}, v, tr, tc.opts)
		if err != nil {
			t.Fatalf("%s: New: %v", tc.name, err)
		}
		if got := ag.offersTool("mode_switch"); got != tc.want {
			t.Errorf("%s: offers mode_switch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
