package e2e_agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// TestAgent_E2E_PreToolHookFires verifies that a pre-tool hook subprocess is
// invoked for each tool call the model makes. The hook script appends a line
// to a log file we read after the run.
func TestAgent_E2E_PreToolHookFires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script hook not portable on Windows")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, "hook.log")
	scriptPath := filepath.Join(root, "hook.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
printf '%s|%s\n' "$ORCH_TOOL_NAME" "$ORCH_TOOL_INPUT" >> "$0.log"
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	// Hook log path is derived from the script path so the script doesn't need a
	// hardcoded absolute path baked in.
	if err := os.Symlink(logPath, scriptPath+".log"); err != nil {
		// Symlink may fail on some sandboxed FS; fall back to direct path replacement.
		body, _ := os.ReadFile(scriptPath)
		_ = os.WriteFile(scriptPath, []byte(strings.Replace(string(body), `"$0.log"`, `"`+logPath+`"`, 1)), 0o755)
	}

	hookRunner := hooks.New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{{Command: []string{scriptPath}}},
		TimeoutMS: 5000,
	}, root)
	if hookRunner == nil {
		t.Fatal("expected hook runner")
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps:    5,
		HooksRunner: hookRunner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "do nothing"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := readLines(t, logPath)
	if len(lines) == 0 {
		t.Fatal("expected at least one hook invocation, got 0")
	}
	// First line must be for the read tool the fake LLM called.
	if !strings.HasPrefix(lines[0], "read|") {
		t.Fatalf("expected first hook line to be for 'read', got %q", lines[0])
	}
	// Hook input must be the JSON object passed to the tool.
	parts := strings.SplitN(lines[0], "|", 2)
	if len(parts) != 2 || !strings.Contains(parts[1], `"path":"a.txt"`) {
		t.Fatalf("expected ORCH_TOOL_INPUT to carry path, got %q", lines[0])
	}
	if !json.Valid([]byte(parts[1])) {
		t.Fatalf("ORCH_TOOL_INPUT not valid JSON: %q", parts[1])
	}
}

// TestAgent_E2E_PreToolHookDenies verifies that a non-zero hook exit code
// blocks the tool call: the model receives a tool error and the file stays
// untouched.
func TestAgent_E2E_PreToolHookDenies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell denial script not portable on Windows")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(root, "deny.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
echo "hook says no" >&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}

	hookRunner := hooks.New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{{Command: []string{scriptPath}}},
		TimeoutMS: 5000,
	}, root)

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps:    5,
		Apply:       true,
		HooksRunner: hookRunner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "try to read"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "original\n" {
		t.Fatalf("file should be untouched, got %q", string(got))
	}
}

// recordingHookRunner is an in-memory HooksRunner that captures every
// pre/post call. Used by the cross-platform wiring test that doesn't rely on
// invoking real subprocess scripts.
type recordingHookRunner struct {
	pre        []hookCall
	post       []hookCall
	lifecycle  []hookCall
	denyTool   string          // when non-empty, RunPreTool denies this tool
	denyReason string          // the hook's own words for that denial
	rewriteTo  json.RawMessage // when non-empty, replaces the tool input
}

type hookCall struct {
	Tool    string
	Payload string
}

func (r *recordingHookRunner) RunPreTool(_ context.Context, name string, in json.RawMessage) agent.HookDecision {
	r.pre = append(r.pre, hookCall{Tool: name, Payload: string(in)})
	if r.denyTool != "" && name == r.denyTool {
		reason := r.denyReason
		if reason == "" {
			reason = "denied by recording hook"
		}
		return agent.HookDecision{Denied: true, Reason: reason}
	}
	if len(r.rewriteTo) > 0 {
		return agent.HookDecision{Input: r.rewriteTo}
	}
	return agent.HookDecision{}
}

func (r *recordingHookRunner) RunPostTool(_ context.Context, name string, out json.RawMessage) {
	r.post = append(r.post, hookCall{Tool: name, Payload: string(out)})
}

func (r *recordingHookRunner) RunLifecycle(_ context.Context, event string, payload json.RawMessage) agent.HookDecision {
	r.lifecycle = append(r.lifecycle, hookCall{Tool: event, Payload: string(payload)})
	return agent.HookDecision{}
}

// capturingLLM is a fakeLLM that keeps the messages of the last request, so a
// test can assert on what the model was actually told.
type capturingLLM struct {
	responses []*llm.CompleteResponse
	i         int
	last      []llm.Message
}

func (f *capturingLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (f *capturingLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	f.last = append([]llm.Message(nil), req.Messages...)
	if f.i >= len(f.responses) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"patches":[]}`}}, nil
	}
	out := f.responses[f.i]
	f.i++
	return out, nil
}

func (f *capturingLLM) toolMessages() string {
	var b strings.Builder
	for _, m := range f.last {
		if m.Role == llm.RoleTool {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// A hook that denies without saying why leaves the model to guess the rule, so
// it retries the same call. The hook's own sentence has to travel.
func TestAgent_E2E_HookDenialReasonReachesTheModel(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordingHookRunner{denyTool: "read", denyReason: "reads are frozen during the release"}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &capturingLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{MaxSteps: 5, HooksRunner: rec})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "read the file"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := llmClient.toolMessages()
	if !strings.Contains(got, "reads are frozen during the release") {
		t.Fatalf("the hook's reason never reached the model, tool messages were:\n%s", got)
	}
}

// A hook that can only refuse forces the model to guess the acceptable call. A
// rewritten input runs instead, and the history has to show what actually ran
// rather than what the model asked for.
func TestAgent_E2E_HookRewritesToolInput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("classified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public.txt"), []byte("published\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordingHookRunner{rewriteTo: json.RawMessage(`{"path":"public.txt"}`)}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &capturingLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"secret.txt"}`))},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{MaxSteps: 5, HooksRunner: rec})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "read the file"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := llmClient.toolMessages()
	if !strings.Contains(got, "published") {
		t.Fatalf("the rewritten path was not the one read:\n%s", got)
	}
	if strings.Contains(got, "classified") {
		t.Fatalf("the original path was read despite the rewrite:\n%s", got)
	}
}

// TestAgent_E2E_HooksWiring_AllPlatforms verifies that the agent loop calls
// RunPreTool / RunPostTool for every tool invocation, regardless of platform.
// Complements the shell-script tests above which only exercise the subprocess
// transport.
func TestAgent_E2E_HooksWiring_AllPlatforms(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordingHookRunner{}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps:    5,
		HooksRunner: rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "do nothing"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(rec.pre) == 0 {
		t.Fatal("pre-tool hook was not invoked for any tool call")
	}
	if rec.pre[0].Tool != "read" {
		t.Fatalf("expected first pre-hook for 'read', got %q", rec.pre[0].Tool)
	}
	if !strings.Contains(rec.pre[0].Payload, `"path":"a.txt"`) {
		t.Fatalf("pre-hook payload missing path: %q", rec.pre[0].Payload)
	}
	if len(rec.post) == 0 {
		t.Fatal("post-tool hook was not invoked")
	}
	if rec.post[0].Tool != "read" {
		t.Fatalf("expected post-hook for 'read', got %q", rec.post[0].Tool)
	}
}

// TestAgent_E2E_HookDenies_AllPlatforms covers the deny path without a shell.
func TestAgent_E2E_HookDenies_AllPlatforms(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := computeFileHashForTest(t, filepath.Join(root, "a.txt"))

	rec := &recordingHookRunner{denyTool: "edit"}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &fakeLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "edit",
					Arguments: llm.ToolArguments([]byte(`{"path":"a.txt","search":"original","replace":"new","file_hash":"` + h + `"}`)),
				},
			}},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	ag, err := agent.New(llmClient, v, tr, agent.Options{
		MaxSteps:    5,
		Apply:       true,
		HooksRunner: rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "edit file"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "original\n" {
		t.Fatalf("denied edit must leave file unchanged, got %q", string(got))
	}
	// Post-hook should NOT fire for a denied call.
	for _, p := range rec.post {
		if p.Tool == "edit" {
			t.Fatal("post-tool hook fired for a denied edit")
		}
	}
}

// computeFileHashForTest is a tiny helper that returns sha256 of a file's bytes
// in hex; matches the format ckg.ComputeSHA256 produces and the resolver expects.
func computeFileHashForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cache.ComputeSHA256(b)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
