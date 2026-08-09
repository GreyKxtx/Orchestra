package llm

import (
	"context"
	"strings"
	"testing"
)

type recordingClient struct {
	name  string
	calls int
}

func (c *recordingClient) Complete(_ context.Context, _ CompleteRequest) (*CompleteResponse, error) {
	c.calls++
	return &CompleteResponse{Message: Message{Role: RoleAssistant, Content: c.name}}, nil
}

func (c *recordingClient) Plan(_ context.Context, _ string) (string, error) {
	c.calls++
	return c.name, nil
}

func TestRouterClient_SmallPromptGoesToFast(t *testing.T) {
	main := &recordingClient{name: "main"}
	fast := &recordingClient{name: "fast"}
	r := NewRouterClient(main, fast, 1000)
	req := CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	resp, _ := r.Complete(context.Background(), req)
	if resp.Message.Content != "fast" {
		t.Fatalf("expected fast, got %q", resp.Message.Content)
	}
	if fast.calls != 1 || main.calls != 0 {
		t.Fatalf("call distribution wrong: main=%d fast=%d", main.calls, fast.calls)
	}
}

func TestRouterClient_LargePromptGoesToMain(t *testing.T) {
	main := &recordingClient{name: "main"}
	fast := &recordingClient{name: "fast"}
	r := NewRouterClient(main, fast, 100)
	big := strings.Repeat("x", 500)
	req := CompleteRequest{Messages: []Message{{Role: RoleUser, Content: big}}}
	resp, _ := r.Complete(context.Background(), req)
	if resp.Message.Content != "main" {
		t.Fatalf("expected main, got %q", resp.Message.Content)
	}
}

func TestRouterClient_ToolsForceMain(t *testing.T) {
	main := &recordingClient{name: "main"}
	fast := &recordingClient{name: "fast"}
	r := NewRouterClient(main, fast, 1_000_000)
	req := CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []ToolDef{{Function: ToolFunctionDef{Name: "x"}}},
	}
	resp, _ := r.Complete(context.Background(), req)
	if resp.Message.Content != "main" {
		t.Fatalf("tools should force main, got %q", resp.Message.Content)
	}

	// With AlwaysMainOnTools=false, even tool calls can route to fast.
	r.AlwaysMainOnTools = false
	main2 := main.calls
	resp, _ = r.Complete(context.Background(), req)
	if resp.Message.Content != "fast" {
		t.Fatalf("with AlwaysMainOnTools=false expected fast, got %q", resp.Message.Content)
	}
	if main.calls != main2 {
		t.Fatal("main should NOT have been called again")
	}
}

func TestRouterClient_NilFastFallsThroughToMain(t *testing.T) {
	main := &recordingClient{name: "main"}
	r := NewRouterClient(main, nil, 10)
	resp, _ := r.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	})
	if resp.Message.Content != "main" {
		t.Fatalf("nil fast should fall through to main, got %q", resp.Message.Content)
	}
}

func TestRouterClient_PlanAlwaysMain(t *testing.T) {
	main := &recordingClient{name: "main"}
	fast := &recordingClient{name: "fast"}
	r := NewRouterClient(main, fast, 1_000_000) // huge threshold
	got, _ := r.Plan(context.Background(), "tiny")
	if got != "main" {
		t.Fatalf("plan should always be main, got %q", got)
	}
}
