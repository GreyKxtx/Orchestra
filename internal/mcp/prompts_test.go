package mcp

import (
	"context"
	"strings"
	"testing"
)

// promptStub is a ServerClient that also serves prompts.
type promptStub struct {
	richStub
	name      string
	prompts   []MCPPrompt
	body      string
	gotArgs   map[string]string
	listCalls int
}

func (p *promptStub) ServerName() string { return p.name }
func (p *promptStub) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	p.listCalls++
	return p.prompts, nil
}
func (p *promptStub) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	p.gotArgs = args
	return p.body, nil
}

func newPromptManager() (*Manager, *promptStub) {
	stub := &promptStub{
		name: "linear",
		prompts: []MCPPrompt{
			{Name: "triage", Description: "Triage an issue", Arguments: []MCPPromptArg{{Name: "id", Required: true}}},
			{Name: "standup", Description: "Draft a standup note"},
		},
		body: "Please triage issue ENG-1.",
	}
	return &Manager{clients: []ServerClient{stub}}, stub
}

func TestManagerListPrompts_NamesTheServer(t *testing.T) {
	m, _ := newPromptManager()
	got := m.ListPrompts(context.Background())
	if len(got) != 2 {
		t.Fatalf("prompts = %d, want 2", len(got))
	}
	if got[0].Server != "linear" || got[0].Name != "triage" {
		t.Errorf("prompt = %+v", got[0])
	}
	if len(got[0].Arguments) != 1 || !got[0].Arguments[0].Required {
		t.Errorf("arguments = %+v", got[0].Arguments)
	}
}

func TestManagerListPrompts_AskedOnce(t *testing.T) {
	// Same reason as resources: this is read to build the command palette,
	// and re-querying every server on every refresh would be felt.
	m, stub := newPromptManager()
	for i := 0; i < 4; i++ {
		m.ListPrompts(context.Background())
	}
	if stub.listCalls != 1 {
		t.Fatalf("prompts/list called %d times, want 1", stub.listCalls)
	}
}

func TestManagerGetPrompt_PassesArgumentsThrough(t *testing.T) {
	m, stub := newPromptManager()
	text, err := m.GetPrompt(context.Background(), "linear", "triage", map[string]string{"id": "ENG-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "ENG-1") {
		t.Errorf("text = %q", text)
	}
	if stub.gotArgs["id"] != "ENG-1" {
		t.Errorf("args = %v", stub.gotArgs)
	}
}

func TestManagerGetPrompt_UnknownServerAndUnsupportedServerBothExplain(t *testing.T) {
	m, _ := newPromptManager()
	if _, err := m.GetPrompt(context.Background(), "nope", "x", nil); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown server: err = %v", err)
	}

	plain := &Manager{clients: []ServerClient{&richStub{}}}
	if got := plain.ListPrompts(context.Background()); len(got) != 0 {
		t.Errorf("prompts from a server without prompt support = %+v", got)
	}
	if _, err := plain.GetPrompt(context.Background(), "stub", "x", nil); err == nil {
		t.Error("a server without prompt support must say so")
	}
}

func TestPromptMessagesToText(t *testing.T) {
	raw := []byte(`{"messages":[
		{"role":"user","content":{"type":"text","text":"first"}},
		{"role":"user","content":{"type":"text","text":"second"}},
		{"role":"user","content":{"type":"image","data":"AAA"}}
	]}`)
	got, err := promptMessagesToText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("text = %q", got)
	}
	if !strings.Contains(got, "omitted") {
		t.Errorf("text = %q, want the non-text part accounted for", got)
	}
}
