package cli

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// applyScriptLLM writes hello.txt, then finishes, and records the tools each
// request offered.
type applyScriptLLM struct {
	mu    sync.Mutex
	calls int
	tools [][]string
}

func (s *applyScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *applyScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	var names []string
	for _, d := range req.Tools {
		names = append(names, d.Function.Name)
	}
	s.tools = append(s.tools, names)
	if s.calls == 1 {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "w1", Type: "function",
			Function: llm.ToolCallFunc{Name: "write", Arguments: llm.ToolArguments(`{"path":"hello.txt","content":"hi\n"}`)},
		}}}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

// applyProject writes a trusted project whose config allows `go` commands.
func applyProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ORCHESTRA_TRUST_STORE", filepath.Join(t.TempDir(), "trusted.json"))
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	cfg := `project_root: "` + filepath.ToSlash(dir) + `"
llm:
  api_base: http://127.0.0.1:1/v1
  model: test-model
  timeout_s: 30
exec:
  timeout_s: 30
  output_limit_kb: 100
  allow: [go]
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.TrustWorkspace(cfgPath); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

func runApplyWith(t *testing.T, mock llm.Client, apply bool) error {
	t.Helper()
	SetTestClient(mock)
	t.Cleanup(ResetTestClient)
	orig := applyFlag
	applyFlag = apply
	t.Cleanup(func() { applyFlag = orig })
	return runApply(applyCmd, []string{"write hello.txt"})
}

// `orchestra apply` runs its turn through the core, in process — the launch
// agent.run gets over RPC — instead of assembling an agent of its own. The
// direct path used to drift from the core's: exec.allow never reached the
// agent, so bash was not offered to a project that allowed `go`.
func TestRunApply_DirectRunsThroughTheCore(t *testing.T) {
	dir := applyProject(t)
	mock := &applyScriptLLM{}
	if err := runApplyWith(t, mock, false); err != nil {
		t.Fatalf("apply: %v", err)
	}

	runs, _ := filepath.Glob(filepath.Join(dir, ".orchestra", "runs", "*.events.jsonl"))
	if len(runs) == 0 {
		t.Error("no run journal: the turn did not go through the core's launch")
	}
	if len(mock.tools) == 0 {
		t.Fatal("the model was never called")
	}
	offered := map[string]bool{}
	for _, n := range mock.tools[0] {
		offered[n] = true
	}
	if !offered["bash"] {
		t.Errorf("exec.allow: [go] must offer bash, got %v", mock.tools[0])
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err == nil {
		t.Error("a dry run wrote hello.txt")
	}
	if _, err := os.Stat(filepath.Join(dir, ".orchestra", "plan.json")); err != nil {
		t.Errorf("plan.json: %v", err)
	}
}

func TestRunApply_DirectApplyWrites(t *testing.T) {
	dir := applyProject(t)
	if err := runApplyWith(t, &applyScriptLLM{}, true); err != nil {
		t.Fatalf("apply --apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil || string(got) != "hi\n" {
		t.Fatalf("hello.txt = %q, %v", got, err)
	}
}
