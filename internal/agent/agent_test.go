package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

type scriptedLLM struct {
	steps []string
	i     int
}

// mockQuestionAsker records questions asked and returns preset answers.
type mockQuestionAsker struct {
	gotQuestions []tools.QuestionItem
	answers      []string
	err          error
}

func (m *mockQuestionAsker) Ask(_ context.Context, questions []tools.QuestionItem) ([]string, error) {
	m.gotQuestions = append(m.gotQuestions, questions...)
	if m.err != nil {
		return nil, m.err
	}
	return m.answers, nil
}

func (s *scriptedLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (s *scriptedLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	_ = req
	if s.i >= len(s.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := s.steps[s.i]
	s.i++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: out}}, nil
}

type policyRetryLLM struct {
	fileHash string
	stage    int
}

func (p *policyRetryLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (p *policyRetryLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	// Check for tool messages with denied/error status
	hasDeniedTool := false
	hasToolResult := false
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			hasToolResult = true
			// Check for denied status (may be formatted as "denied" or "status":"denied")
			if strings.Contains(m.Content, `"status":"denied"`) ||
				strings.Contains(m.Content, `"status": "denied"`) ||
				strings.Contains(m.Content, `"denied"`) {
				hasDeniedTool = true
			}
			// Also check for successful tool results
			if strings.Contains(m.Content, `"entries"`) || strings.Contains(m.Content, `"content"`) {
				// Tool succeeded (fs.read returns content, fs.list returns entries)
			}
		}
	}

	switch p.stage {
	case 0:
		if hasDeniedTool {
			return nil, fmt.Errorf("unexpected denied tool in first request")
		}
		p.stage++
		// Return tool call with tool_call_id for proper tool calling loop
		return &llm.CompleteResponse{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "bash",
							Arguments: llm.ToolArguments([]byte(`{"command":"echo","args":["hello"]}`)),
						},
					},
				},
			},
		}, nil

	case 1:
		if !hasDeniedTool {
			return nil, fmt.Errorf("expected denied tool message in retry request")
		}
		p.stage++
		// Return tool call for fs.read
		return &llm.CompleteResponse{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_2",
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      "read",
							Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`)),
						},
					},
				},
			},
		}, nil

	case 2:
		if !hasToolResult {
			return nil, fmt.Errorf("expected tool result message in prompt")
		}
		p.stage++
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + p.fileHash + `"}]}}`}}, nil

	default:
		// Be safe: keep returning a valid final if called again.
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + p.fileHash + `"}]}}`}}, nil
	}
}

func TestAgent_Run_ToolCallThenFinal_Applies(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello old world\n"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := cache.ComputeSHA256([]byte("hello old world\n"))

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llm := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"read","input":{"path":"a.txt"}}}`,
			`{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"old","replace":"new","file_hash":"` + h + `"}]}}`,
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps: 10,
		Apply:    true,
		Backup:   false,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "replace old with new")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res == nil || res.ApplyResponse == nil {
		t.Fatalf("expected apply response")
	}

	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "hello new world\n" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_Run_ExecDenied_IsRetriedInsideNextStep_AndDoesNotBurnStep(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello old world\n"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := cache.ComputeSHA256([]byte("hello old world\n"))

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llm := &policyRetryLLM{fileHash: h}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:          10,
		MaxInvalidRetries: 2, // enough for policy retry within nextStep
		AllowExec:         false,
		Apply:             true,
		Backup:            false,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "replace old with new")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// With proper tool calling loop: exec.run denied (step 1) -> fs.read (step 2) -> final (step 3)
	// The denied tool call is a proper step with assistant message + tool message, not a retry.
	if res.Steps != 3 {
		t.Fatalf("expected 3 steps (exec.run denied + fs.read + final). got=%d", res.Steps)
	}
	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "hello new world\n" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_Run_InvalidJSON_Retries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	fileHash := cache.ComputeSHA256([]byte("x"))
	llm := &scriptedLLM{
		steps: []string{
			`not json`,
			`{"type":"tool_call","tool":{"name":"ls","input":{}}}`,
			fmt.Sprintf(`{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"x","replace":"y","file_hash":%q}]}}`, fileHash),
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:          10,
		MaxInvalidRetries: 3,
		Apply:             true,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "change x to y")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(after) != "y" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestAgent_Run_ExecDenied_RepeatsThenStopsEarly(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llm := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"bash","input":{"command":"echo","args":["hello"]}}}`,
			`{"type":"tool_call","tool":{"name":"bash","input":{"command":"echo","args":["hello"]}}}`,
			`{"type":"tool_call","tool":{"name":"bash","input":{"command":"echo","args":["hello"]}}}`,
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:             10,
		AllowExec:            false,
		MaxDeniedToolRepeats: 2,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "run echo hello")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.InvalidLLMOutput {
		t.Fatalf("expected code %s, got %s", protocol.InvalidLLMOutput, coreErr.Code)
	}
}

func TestAgent_Run_PlanMode_WriteGuard_BlocksNonPlanFile(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"write","input":{"path":"bad.go","content":"package main"}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ag, err := New(llmClient, v, tr, Options{
		MaxSteps: 10,
		Mode:     ModePlan,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "write some code")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result")
	}
	if _, statErr := os.Stat(filepath.Join(root, "bad.go")); !os.IsNotExist(statErr) {
		t.Fatalf("bad.go must not be created in plan mode")
	}
}

func TestAgent_Run_PlanMode_WriteGuard_AllowsPlanMd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatalf("mkdir .orchestra: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"write","input":{"path":".orchestra/plan.md","content":"# Plan\n","must_not_exist":true}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ag, err := New(llmClient, v, tr, Options{
		MaxSteps: 10,
		Mode:     ModePlan,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "write a plan")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result")
	}
	content, readErr := os.ReadFile(filepath.Join(root, ".orchestra", "plan.md"))
	if readErr != nil {
		t.Fatalf(".orchestra/plan.md should have been created: %v", readErr)
	}
	if string(content) != "# Plan\n" {
		t.Fatalf("unexpected plan.md content: %q", string(content))
	}
}

func TestAgent_Run_QuestionTool_CallsAsker(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	asker := &mockQuestionAsker{answers: []string{"option A"}}

	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"question","input":{"questions":[{"question":"Which approach?","options":["A","B"]}]}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ag, err := New(llmClient, v, tr, Options{
		MaxSteps:      10,
		Mode:          ModePlan,
		QuestionAsker: asker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, res, err := ag.Run(context.Background(), nil, "plan something")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result")
	}
	if len(asker.gotQuestions) != 1 {
		t.Fatalf("expected 1 question asked, got %d", len(asker.gotQuestions))
	}
	if asker.gotQuestions[0].Question != "Which approach?" {
		t.Fatalf("unexpected question: %q", asker.gotQuestions[0].Question)
	}
}

func TestAgent_Run_FinalResolveFailure_RepeatsThenStopsEarly(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	// Empty "search" passes schema, but resolver rejects it.
	badFinal := `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"a.txt","search":"","replace":"x","file_hash":"sha256:deadbeef"}]}}`

	llm := &scriptedLLM{
		steps: []string{
			badFinal,
			badFinal,
			badFinal,
		},
	}

	ag, err := New(llm, v, tr, Options{
		MaxSteps:         10,
		MaxFinalFailures: 2,
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "do something")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.InvalidLLMOutput {
		t.Fatalf("expected code %s, got %s", protocol.InvalidLLMOutput, coreErr.Code)
	}
}

// eventCollector captures AgentEvents thread-safely.
type eventCollector struct {
	mu     sync.Mutex
	events []AgentEvent
}

func (ec *eventCollector) Collect(ev AgentEvent) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, ev)
}

func (ec *eventCollector) All() []AgentEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	out := make([]AgentEvent, len(ec.events))
	copy(out, ec.events)
	return out
}

func (ec *eventCollector) ByKind(kind llm.StreamEventKind) []AgentEvent {
	all := ec.All()
	var out []AgentEvent
	for _, e := range all {
		if e.Stream.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// capturingLLM records every CompleteRequest for inspection.
type capturingLLM struct{ requests []llm.CompleteRequest }

func (c *capturingLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }
func (c *capturingLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	c.requests = append(c.requests, req)
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func TestAgent_Run_SystemPromptOverride_ReachesLLM(t *testing.T) {
	root := t.TempDir()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	const wantPrompt = "you are a custom review agent"

	cap := &capturingLLM{}
	ag, err := New(cap, v, tr, Options{
		MaxSteps:             10,
		SystemPromptOverride: wantPrompt,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, _ = ag.Run(context.Background(), nil, "do something")

	if len(cap.requests) == 0 {
		t.Fatal("LLM was never called")
	}
	msgs := cap.requests[0].Messages
	if len(msgs) == 0 {
		t.Fatal("no messages in first LLM request")
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Fatalf("messages[0].Role = %q, want %q", msgs[0].Role, llm.RoleSystem)
	}
	if !strings.Contains(msgs[0].Content, wantPrompt) {
		t.Errorf("system prompt = %q, want it to contain %q", msgs[0].Content, wantPrompt)
	}
}

// ---------------------------------------------------------------------------
// Phase 0 streaming event tests
// ---------------------------------------------------------------------------

// TestAgent_OnEvent_ToolCallCompleted verifies that a StreamEventToolCallCompleted
// event is emitted (with the correct ToolCallName) after every tools.Call.
func TestAgent_OnEvent_ToolCallCompleted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	// One tool call (read), then final-empty.
	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"read","input":{"path":"a.txt"}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ec := &eventCollector{}
	ag, err := New(llmClient, v, tr, Options{
		MaxSteps: 10,
		OnEvent:  ec.Collect,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "read the file")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	completed := ec.ByKind(llm.StreamEventToolCallCompleted)
	if len(completed) == 0 {
		t.Fatal("expected at least one StreamEventToolCallCompleted event, got none")
	}
	// The agent normalises "read" → the canonical fs.read tool name; accept either.
	name := completed[0].Stream.ToolCallName
	if name != "read" && name != "fs.read" {
		t.Errorf("ToolCallName = %q, want \"read\" or \"fs.read\"", name)
	}
}

// TestAgent_OnEvent_StepDone_Reasons verifies that exactly two StreamEventStepDone
// events are emitted for a single tool_call → final-empty sequence, with reasons
// "tool_call" and "final" respectively.
func TestAgent_OnEvent_StepDone_Reasons(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("hi"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"read","input":{"path":"b.txt"}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ec := &eventCollector{}
	ag, err := New(llmClient, v, tr, Options{
		MaxSteps: 10,
		OnEvent:  ec.Collect,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "read and do nothing")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepDones := ec.ByKind(llm.StreamEventStepDone)
	if len(stepDones) != 2 {
		reasons := make([]string, len(stepDones))
		for i, e := range stepDones {
			reasons[i] = e.Stream.Content
		}
		t.Fatalf("expected exactly 2 step_done events, got %d: %v", len(stepDones), reasons)
	}
	if stepDones[0].Stream.Content != "tool_call" {
		t.Errorf("step_done[0].Content = %q, want \"tool_call\"", stepDones[0].Stream.Content)
	}
	if stepDones[1].Stream.Content != "final" {
		t.Errorf("step_done[1].Content = %q, want \"final\"", stepDones[1].Stream.Content)
	}
}

// TestAgent_OnEvent_RecoverableError_StaleHash verifies that StreamEventRecoverableError
// is emitted when a final patch carries a wrong file_hash (causing resolver/applier failure).
func TestAgent_OnEvent_RecoverableError_StaleHash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	// First final: valid search but stale (wrong) file_hash → recoverable error.
	// Second final: empty patches → terminates normally.
	badFinal := `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"c.txt","search":"hello","replace":"hi","file_hash":"sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}]}}`

	llmClient := &scriptedLLM{
		steps: []string{
			badFinal,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}

	ec := &eventCollector{}
	ag, err := New(llmClient, v, tr, Options{
		MaxSteps:         10,
		MaxFinalFailures: 6,
		OnEvent:          ec.Collect,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "replace hello with hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	recoverableErrors := ec.ByKind(llm.StreamEventRecoverableError)
	if len(recoverableErrors) == 0 {
		t.Fatal("expected at least one StreamEventRecoverableError event, got none")
	}
}

// TestAgent_OnEvent_PendingOps verifies that exactly one StreamEventPendingOps event
// is emitted in dry-run mode (Apply: false) after a successful patch resolution,
// and that its Content is valid JSON with the expected shape.
func TestAgent_OnEvent_PendingOps(t *testing.T) {
	root := t.TempDir()
	content := []byte("hello old world\n")
	if err := os.WriteFile(filepath.Join(root, "d.txt"), content, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	h := cache.ComputeSHA256(content)

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	finalStep := fmt.Sprintf(
		`{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"d.txt","search":"old","replace":"new","file_hash":%q}]}}`,
		h,
	)
	llmClient := &scriptedLLM{steps: []string{finalStep}}

	ec := &eventCollector{}
	ag, err := New(llmClient, v, tr, Options{
		MaxSteps: 10,
		Apply:    false, // dry-run
		OnEvent:  ec.Collect,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "replace old with new")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	pendingOps := ec.ByKind(llm.StreamEventPendingOps)
	if len(pendingOps) != 1 {
		t.Fatalf("expected exactly 1 StreamEventPendingOps event, got %d", len(pendingOps))
	}

	// Parse the JSON payload and verify expected keys.
	var payload map[string]any
	if err := json.Unmarshal([]byte(pendingOps[0].Stream.Content), &payload); err != nil {
		t.Fatalf("StreamEventPendingOps Content is not valid JSON: %v\ncontent=%s", err, pendingOps[0].Stream.Content)
	}
	for _, key := range []string{"ops", "diff", "applied"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("StreamEventPendingOps payload missing key %q; keys=%v", key, keys(payload))
		}
	}
	applied, _ := payload["applied"].(bool)
	if applied {
		t.Errorf("expected applied=false in dry-run mode, got true")
	}
}

// keys returns the map keys as a sorted slice for error messages.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
