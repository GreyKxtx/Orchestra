package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// prefixRecorderLLM records the leading (cacheable) messages of every request
// so a test can assert they stay byte-identical across steps.
type prefixRecorderLLM struct {
	steps    []*llm.CompleteResponse
	i        int
	prefixes []string
	tails    []string
}

func (l *prefixRecorderLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *prefixRecorderLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if len(req.Messages) >= 2 {
		l.prefixes = append(l.prefixes, req.Messages[0].Content+"\x00"+req.Messages[1].Content)
		l.tails = append(l.tails, req.Messages[len(req.Messages)-1].Content)
	}
	if l.i >= len(l.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := l.steps[l.i]
	l.i++
	return out, nil
}

// TestAgent_PromptPrefixStableAcrossSteps guards the provider prompt cache:
// system + first user message must not change between steps, otherwise the
// cached prefix is invalidated on every call and the whole history is re-billed.
func TestAgent_PromptPrefixStableAcrossSteps(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	mkCall := func(id string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))},
			}},
		}}
	}
	client := &prefixRecorderLLM{steps: []*llm.CompleteResponse{
		mkCall("c1"), mkCall("c2"), mkCall("c3"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(client, v, runner, Options{MaxSteps: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "list the workspace"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(client.prefixes) < 3 {
		t.Fatalf("expected at least 3 recorded steps, got %d", len(client.prefixes))
	}
	// Step 1 legitimately differs (CKG context is injected once); from step 2
	// on the prefix must be stable.
	for i := 2; i < len(client.prefixes); i++ {
		if client.prefixes[i] != client.prefixes[1] {
			t.Fatalf("prompt prefix changed between step 2 and step %d — prompt cache would miss every step", i+1)
		}
	}
}

// TestAgent_VolatileBlockGoesLast keeps the working-state injection behind the
// history: in front of it, it breaks the cacheable prefix.
func TestAgent_VolatileBlockGoesLast(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	mkCall := func(id string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))},
			}},
		}}
	}
	client := &prefixRecorderLLM{steps: []*llm.CompleteResponse{mkCall("c1"), mkCall("c2")}}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	on := true
	ag, err := New(client, v, runner, Options{MaxSteps: 4, WorkingState: &on})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "do something"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.tails) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(client.tails))
	}
	// The block must actually be produced (otherwise this test is vacuous)…
	found := false
	for _, tail := range client.tails {
		if strings.Contains(tail, "<working_state>") {
			found = true
		}
	}
	if !found {
		t.Fatal("working state was never injected — test cannot prove where it lands")
	}
	// …and it must never sit in the cacheable prefix.
	for i, p := range client.prefixes {
		if strings.Contains(p, "<working_state>") {
			t.Fatalf("step %d: working state must not be in the leading user message", i+1)
		}
	}
}

// TestAgent_SystemPromptStableWhenMemoryChangesMidRun is DATA-2: the system
// prompt carries injected memory, and it used to be rebuilt on every step. A
// memory_write mid-turn — the model's own, or a session auto-note after an
// explore — changed it, so in session mode the provider's prompt cache missed
// on every step, and Anthropic billed a cache write for each one. The test
// above ran without a session and never saw it.
func TestAgent_SystemPromptStableWhenMemoryChangesMidRun(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	memCfg := memory.DefaultConfig()
	memCfg.GlobalEnabled = false
	runner.SetMemoryContext("s1", memCfg)
	// Memory that exists when the turn starts is injected.
	if _, err := memory.NewStore(dir, "s1", memCfg).AppendEntry("project", memory.TypeProject, "the parser lives in internal/parse"); err != nil {
		t.Fatal(err)
	}

	call := func(id, name, args string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: id, Type: "function",
				Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments([]byte(args))},
			}},
		}}
	}
	client := &prefixRecorderLLM{steps: []*llm.CompleteResponse{
		call("c1", "ls", `{"path":"."}`),
		call("c2", "memory_write", `{"scope":"session","content":"step two found the lexer in internal/lex"}`),
		call("c3", "memory_write", `{"scope":"project","content":"integration tests need the docker compose stack"}`),
		call("c4", "ls", `{"path":"."}`),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(client, v, runner, Options{MaxSteps: 10, SessionID: "s1", Memory: memCfg})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "survey the parser"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.prefixes) < 5 {
		t.Fatalf("expected 5 steps, got %d", len(client.prefixes))
	}
	system := func(i int) string { s, _, _ := strings.Cut(client.prefixes[i], "\x00"); return s }
	if !strings.Contains(system(0), "the parser lives in internal/parse") {
		t.Fatal("memory present at the start of the turn must be injected — test cannot prove anything otherwise")
	}
	for i := 1; i < len(client.prefixes); i++ {
		if system(i) != system(0) {
			t.Fatalf("the system prompt changed at step %d after a mid-turn memory write — the prompt cache misses from there on", i+1)
		}
	}
	// What was written mid-turn is not lost: it is on disk for the next turn.
	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if !strings.Contains(string(data), "docker compose stack") {
		t.Fatal("the mid-turn memory write must still reach the file")
	}
}

// The system prompt is built with the lessons of the department the turn
// starts in. When the files it works on point at another one, that
// department's lessons arrive in the volatile tail instead of changing the
// system message.
func TestLessonsUpdate_ArrivesInTheTailWhenTheDepartmentChanges(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	if err := lessons.Append(dir, lessons.Entry{Dept: "typescript_engineering", Kind: lessons.KindAgentNote, Note: "run tsc with --noEmit before claiming green"}); err != nil {
		t.Fatal(err)
	}
	a := &Agent{opts: Options{Mode: ModeBuild}, tools: runner, working: working.New("goal")}
	if got := a.lessonsUpdate(); got != "" {
		t.Fatalf("no files yet: nothing to add, got %q", got)
	}
	a.working.ObserveTool("read", []byte(`{"path":"web/app.ts"}`), []byte("x"), nil)
	got := a.lessonsUpdate()
	if !strings.Contains(got, "run tsc with --noEmit") {
		t.Fatalf("the new department's lessons must reach the tail, got %q", got)
	}
	if strings.Contains(a.turnSystemPrompt(), "run tsc with --noEmit") {
		t.Fatal("the system prompt of the turn must not change")
	}
}
