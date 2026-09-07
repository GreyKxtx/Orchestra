package core

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/llm"
)

// scriptedClient stands in for the connected TUI / IDE: it records every
// server-initiated request and answers from a script, so the tests see the
// exact wire shape the real client would.
type scriptedClient struct {
	mu     sync.Mutex
	calls  []scriptedCall
	answer func(method string, params any) (any, error)
}

type scriptedCall struct {
	Method string
	Params any
}

func (s *scriptedClient) request(_ context.Context, method string, params any, result any) error {
	s.mu.Lock()
	s.calls = append(s.calls, scriptedCall{Method: method, Params: params})
	s.mu.Unlock()
	v, err := s.answer(method, params)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

func (s *scriptedClient) recorded() []scriptedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scriptedCall(nil), s.calls...)
}

// approving answers every permission/request with approved (and always, when
// asked to) and every question/ask with the given answers.
func approving(always bool, answers ...string) func(string, any) (any, error) {
	return func(method string, _ any) (any, error) {
		switch method {
		case "permission/request":
			return permission.Response{Approved: true, Always: always}, nil
		case "question/ask":
			return map[string]any{"answers": answers}, nil
		}
		return nil, errors.New("unexpected method " + method)
	}
}

func boundHost(t *testing.T, client *scriptedClient, model func() (llm.Client, string)) *mcpHost {
	t.Helper()
	h := newMCPHost(model)
	if client != nil {
		h.bind(&rpcPermissionRequester{requestFn: client.request}, &rpcQuestionAsker{requestFn: client.request})
	}
	return h
}

// An MCP server's request travels to the user as a permission/request that
// names the server. "Something wants your model" is not a question a person
// can answer; "mcp:linear wants your model" is.
func TestMCPHost_ConsentAsksTheClientAndNamesTheServer(t *testing.T) {
	client := &scriptedClient{answer: approving(false)}
	h := boundHost(t, client, nil)

	ok := h.consent(context.Background(), mcp.ConsentRequest{
		Server:  "linear",
		Kind:    mcp.ConsentSampling,
		Summary: "2 message(s), up to 4096 tokens: summarise this issue",
	})
	if !ok {
		t.Fatal("an approved request must be granted")
	}
	calls := client.recorded()
	if len(calls) != 1 || calls[0].Method != "permission/request" {
		t.Fatalf("calls = %+v, want one permission/request", calls)
	}
	req, isReq := calls[0].Params.(permission.Request)
	if !isReq {
		t.Fatalf("params are %T, want permission.Request", calls[0].Params)
	}
	if req.Tool != "mcp:linear" {
		t.Errorf("Tool = %q, want mcp:linear — the prompt must say which server is asking", req.Tool)
	}
	if req.Kind != mcp.ConsentSampling {
		t.Errorf("Kind = %q, want %q", req.Kind, mcp.ConsentSampling)
	}
	if req.Description != "2 message(s), up to 4096 tokens: summarise this issue" {
		t.Errorf("Description = %q, want the summary", req.Description)
	}
}

// No client attached means no way to obtain consent, which is not consent.
func TestMCPHost_ConsentFailsClosedWithoutAClient(t *testing.T) {
	h := boundHost(t, nil, nil)
	if h.consent(context.Background(), mcp.ConsentRequest{Server: "x", Kind: mcp.ConsentSampling}) {
		t.Fatal("consent was granted with nobody to ask")
	}
}

func TestMCPHost_ConsentDeniedWhenTheClientErrors(t *testing.T) {
	client := &scriptedClient{answer: func(string, any) (any, error) {
		return nil, errors.New("method not found")
	}}
	h := boundHost(t, client, nil)
	if h.consent(context.Background(), mcp.ConsentRequest{Server: "x", Kind: mcp.ConsentSampling}) {
		t.Fatal("a failed request must read as refusal")
	}
}

// "Always" from the client is remembered per server AND per kind, for the
// life of the host. A server trusted with the model is not thereby trusted
// with the user's attention.
func TestMCPHost_ConsentRemembersAlwaysPerServerAndKind(t *testing.T) {
	client := &scriptedClient{answer: approving(true)}
	h := boundHost(t, client, nil)
	ctx := context.Background()

	if !h.consent(ctx, mcp.ConsentRequest{Server: "linear", Kind: mcp.ConsentSampling}) {
		t.Fatal("first ask must be granted")
	}
	h.consent(ctx, mcp.ConsentRequest{Server: "linear", Kind: mcp.ConsentSampling})
	if n := len(client.recorded()); n != 1 {
		t.Fatalf("the client was asked %d times for the same server+kind after 'always'; want 1", n)
	}
	h.consent(ctx, mcp.ConsentRequest{Server: "linear", Kind: mcp.ConsentElicitation})
	if n := len(client.recorded()); n != 2 {
		t.Fatalf("a different kind must ask again; client asked %d times, want 2", n)
	}
	h.consent(ctx, mcp.ConsentRequest{Server: "github", Kind: mcp.ConsentSampling})
	if n := len(client.recorded()); n != 3 {
		t.Fatalf("a different server must ask again; client asked %d times, want 3", n)
	}
}

// recordingLLM captures the request it was given and returns a fixed reply.
type recordingLLM struct {
	got   llm.CompleteRequest
	reply string
}

func (r *recordingLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	r.got = req
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: r.reply}}, nil
}

func (r *recordingLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func TestMCPHost_SampleRunsTheConfiguredModel(t *testing.T) {
	rec := &recordingLLM{reply: "Issue is about login timeouts."}
	h := boundHost(t, nil, func() (llm.Client, string) { return rec, "qwen2.5-coder" })

	res, err := h.sample(context.Background(), mcp.SamplingRequest{
		Server:       "linear",
		SystemPrompt: "Be brief.",
		Messages: []mcp.SamplingMessage{
			{Role: "user", Text: "Summarise: users cannot log in after 30s"},
			{Role: "assistant", Text: "Understood."},
			{Role: "user", Text: "Go on."},
		},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantMsgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "Be brief."},
		{Role: llm.RoleUser, Content: "Summarise: users cannot log in after 30s"},
		{Role: llm.RoleAssistant, Content: "Understood."},
		{Role: llm.RoleUser, Content: "Go on."},
	}
	if !reflect.DeepEqual(rec.got.Messages, wantMsgs) {
		t.Errorf("model got %+v\nwant %+v", rec.got.Messages, wantMsgs)
	}
	if len(rec.got.Tools) != 0 {
		t.Errorf("a sampling call must not expose Orchestra's tools to the server's prompt; got %d", len(rec.got.Tools))
	}
	if res.Text != "Issue is about login timeouts." || res.Model != "qwen2.5-coder" {
		t.Errorf("result = %+v", res)
	}
}

func TestMCPHost_SampleWithoutAModelErrors(t *testing.T) {
	h := boundHost(t, nil, func() (llm.Client, string) { return nil, "" })
	if _, err := h.sample(context.Background(), mcp.SamplingRequest{Server: "x"}); err == nil {
		t.Fatal("sampling with no model must fail, not return an empty completion")
	}
	h2 := boundHost(t, nil, nil)
	if _, err := h2.sample(context.Background(), mcp.SamplingRequest{Server: "x"}); err == nil {
		t.Fatal("a host built with no model resolver must fail the same way")
	}
}

const elicitSchema = `{
  "type": "object",
  "properties": {
    "name":  {"type": "string",  "title": "Project name"},
    "env":   {"type": "string",  "enum": ["dev", "prod"]},
    "force": {"type": "boolean", "description": "Overwrite existing files?"},
    "count": {"type": "integer"}
  },
  "required": ["name"]
}`

// The elicitation schema becomes one question per property, in the schema's
// own order, with enums and booleans offered as options so the TUI can render
// a pick list instead of a free-text box.
func TestElicitationQuestions_MapsSchemaToQuestions(t *testing.T) {
	fields, err := elicitationFields(json.RawMessage(elicitSchema))
	if err != nil {
		t.Fatal(err)
	}
	qs := elicitationQuestions("Need a few details to scaffold.", fields)
	if len(qs) != 4 {
		t.Fatalf("got %d questions, want 4:\n%+v", len(qs), qs)
	}
	if got := []string{fields[0].name, fields[1].name, fields[2].name, fields[3].name}; !reflect.DeepEqual(got, []string{"name", "env", "force", "count"}) {
		t.Errorf("field order = %v, want schema order", got)
	}
	if !contains(qs[0].Question, "Need a few details to scaffold.") || !contains(qs[0].Question, "Project name") {
		t.Errorf("first question must carry the server's message and the field title; got %q", qs[0].Question)
	}
	if contains(qs[1].Question, "Need a few details") {
		t.Errorf("the message belongs on the first question only; got %q", qs[1].Question)
	}
	if !reflect.DeepEqual(qs[1].Options, []string{"dev", "prod"}) {
		t.Errorf("enum options = %v", qs[1].Options)
	}
	if !reflect.DeepEqual(qs[2].Options, []string{"yes", "no"}) {
		t.Errorf("boolean options = %v", qs[2].Options)
	}
	if !contains(qs[2].Question, "Overwrite existing files?") {
		t.Errorf("description must be shown; got %q", qs[2].Question)
	}
	if len(qs[3].Options) != 0 {
		t.Errorf("an integer is free text, got options %v", qs[3].Options)
	}
}

// Both the TUI and the stdin asker send back whatever the user typed — "2"
// for the second option, "да" for yes — so the typed strings have to be
// coerced into the types the schema declared before the server sees them.
func TestElicitationContent_CoercesTypedAnswers(t *testing.T) {
	fields, _ := elicitationFields(json.RawMessage(elicitSchema))
	content, action := elicitationContent(fields, []string{"orchestra", "2", "да", "7"})
	if action != "accept" {
		t.Fatalf("action = %q, want accept", action)
	}
	want := map[string]any{"name": "orchestra", "env": "prod", "force": true, "count": int64(7)}
	if !reflect.DeepEqual(content, want) {
		t.Errorf("content = %#v\nwant %#v", content, want)
	}
}

func TestElicitationContent_OptionTextIsAcceptedAsIs(t *testing.T) {
	fields, _ := elicitationFields(json.RawMessage(elicitSchema))
	content, action := elicitationContent(fields, []string{"x", "PROD", "no", ""})
	if action != "accept" {
		t.Fatalf("action = %q, want accept", action)
	}
	if content["env"] != "prod" {
		t.Errorf("enum answer must canonicalise to the schema's spelling; got %#v", content["env"])
	}
	if content["force"] != false {
		t.Errorf("force = %#v, want false", content["force"])
	}
	if _, present := content["count"]; present {
		t.Errorf("an empty optional answer must be omitted, not sent as zero; got %#v", content["count"])
	}
}

func TestElicitationContent_EmptyRequiredAnswerDeclines(t *testing.T) {
	fields, _ := elicitationFields(json.RawMessage(elicitSchema))
	if _, action := elicitationContent(fields, []string{"", "dev", "no", "1"}); action != "decline" {
		t.Fatalf("action = %q, want decline — a required field left blank is a refusal, not an accept with holes", action)
	}
}

func TestElicitationContent_NoAnswersCancels(t *testing.T) {
	fields, _ := elicitationFields(json.RawMessage(elicitSchema))
	if _, action := elicitationContent(fields, nil); action != "cancel" {
		t.Fatalf("action = %q, want cancel — the client dismissed the dialog", action)
	}
}

// A schema with no properties is a confirmation: the message is the question.
func TestElicitationQuestions_NoPropertiesIsAConfirmation(t *testing.T) {
	fields, err := elicitationFields(json.RawMessage(`{"type":"object","properties":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	qs := elicitationQuestions("Proceed with deleting the branch?", fields)
	if len(qs) != 1 || !contains(qs[0].Question, "Proceed with deleting the branch?") {
		t.Fatalf("questions = %+v, want the message as a single confirmation", qs)
	}
	if !reflect.DeepEqual(qs[0].Options, []string{"ok", "decline"}) {
		t.Errorf("options = %v", qs[0].Options)
	}
	if content, action := elicitationContent(fields, []string{"ok"}); action != "accept" || len(content) != 0 {
		t.Errorf("ok → accept with empty content; got %q %#v", action, content)
	}
	if _, action := elicitationContent(fields, []string{"decline"}); action != "decline" {
		t.Errorf("decline → decline; got %q", action)
	}
}

func TestMCPHost_ElicitRoundTrip(t *testing.T) {
	client := &scriptedClient{answer: approving(false, "orchestra", "1", "yes", "3")}
	h := boundHost(t, client, nil)

	res, err := h.elicit(context.Background(), mcp.ElicitationRequest{
		Server:  "scaffold",
		Message: "Need a few details.",
		Schema:  json.RawMessage(elicitSchema),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "accept" {
		t.Fatalf("action = %q, want accept", res.Action)
	}
	want := map[string]any{"name": "orchestra", "env": "dev", "force": true, "count": int64(3)}
	if !reflect.DeepEqual(res.Content, want) {
		t.Errorf("content = %#v\nwant %#v", res.Content, want)
	}
	calls := client.recorded()
	if len(calls) != 1 || calls[0].Method != "question/ask" {
		t.Fatalf("calls = %+v, want exactly one question/ask", calls)
	}
}

// Nobody to ask is a decline, not an error: an error reads to the server as a
// client fault and invites a retry that will fail the same way.
func TestMCPHost_ElicitWithoutAClientDeclines(t *testing.T) {
	h := boundHost(t, nil, nil)
	res, err := h.elicit(context.Background(), mcp.ElicitationRequest{Server: "x", Message: "?", Schema: json.RawMessage(`{}`)})
	if err != nil || res.Action != "decline" {
		t.Fatalf("got (%+v, %v), want decline with no error", res, err)
	}
}

func TestMCPHost_ElicitMalformedSchemaErrors(t *testing.T) {
	client := &scriptedClient{answer: approving(false, "x")}
	h := boundHost(t, client, nil)
	if _, err := h.elicit(context.Background(), mcp.ElicitationRequest{Server: "x", Schema: json.RawMessage(`{"properties": 5}`)}); err == nil {
		t.Fatal("a schema that cannot be read must be reported, not silently asked as nothing")
	}
	if n := len(client.recorded()); n != 0 {
		t.Fatalf("the user was asked %d question(s) about a schema we could not read", n)
	}
}

// The RPC handler is where the client's request channel appears; attaching it
// there must reach the MCP hooks, or every server request fails closed forever
// while the config says the server is allowed.
func TestRPCHandler_SetRequesterBindsMCPHooks(t *testing.T) {
	c := &Core{mcpHost: newMCPHost(nil)}
	h := NewRPCHandler(c)
	client := &scriptedClient{answer: approving(false)}

	h.SetRequester(client.request)

	if !c.mcpHost.consent(context.Background(), mcp.ConsentRequest{Server: "s", Kind: mcp.ConsentSampling}) {
		t.Fatal("consent did not reach the client attached through SetRequester")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
