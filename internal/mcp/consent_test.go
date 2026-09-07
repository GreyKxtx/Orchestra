package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func sampleParams(t *testing.T, maxTokens int) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"maxTokens":    maxTokens,
		"systemPrompt": "be brief",
		"messages": []map[string]any{
			{"role": "user", "content": map[string]any{"type": "text", "text": "hello"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// recordingSampler answers sampling and remembers whether it was reached at
// all — "the model was never called" is the assertion that matters for every
// refusal path.
type recordingSampler struct {
	calls int
	last  SamplingRequest
}

func (s *recordingSampler) sample(_ context.Context, req SamplingRequest) (SamplingResult, error) {
	s.calls++
	s.last = req
	return SamplingResult{Model: "test-model", Text: "hi"}, nil
}

// A server not opted into sampling must not reach the model. This is the
// difference between a feature and a way for a server to spend someone's
// money.
func TestInboundHandler_SamplingRefusedWhenNotEnabled(t *testing.T) {
	s := &recordingSampler{}
	h := newInboundHandler("linear", InboundOptions{
		AllowSampling: false,
		Sample:        s.sample,
		Consent:       func(context.Context, ConsentRequest) bool { return true },
	})

	res, rerr := h(context.Background(), "sampling/createMessage", sampleParams(t, 100))
	if rerr == nil {
		t.Fatalf("a server with sampling disabled got a result: %+v", res)
	}
	if s.calls != 0 {
		t.Error("the model was called for a server that never opted in")
	}
	if !strings.Contains(strings.ToLower(rerr.Message), "sampling") {
		t.Errorf("error should name what was refused: %q", rerr.Message)
	}
}

// Enabled in config is permission to ASK, not permission to proceed.
func TestInboundHandler_SamplingRefusedWhenConsentDenied(t *testing.T) {
	s := &recordingSampler{}
	var asked ConsentRequest
	h := newInboundHandler("linear", InboundOptions{
		AllowSampling: true,
		Sample:        s.sample,
		Consent: func(_ context.Context, req ConsentRequest) bool {
			asked = req
			return false
		},
	})

	if _, rerr := h(context.Background(), "sampling/createMessage", sampleParams(t, 100)); rerr == nil {
		t.Fatal("a denied request was served anyway")
	}
	if s.calls != 0 {
		t.Error("the model was called after consent was denied")
	}
	if asked.Server != "linear" {
		t.Errorf("consent prompt did not name the server: %+v", asked)
	}
	if asked.Kind != ConsentSampling {
		t.Errorf("consent kind = %q, want %q", asked.Kind, ConsentSampling)
	}
}

func TestInboundHandler_SamplingServedWhenAllowed(t *testing.T) {
	s := &recordingSampler{}
	h := newInboundHandler("linear", InboundOptions{
		AllowSampling: true,
		Sample:        s.sample,
		Consent:       func(context.Context, ConsentRequest) bool { return true },
	})

	res, rerr := h(context.Background(), "sampling/createMessage", sampleParams(t, 100))
	if rerr != nil {
		t.Fatalf("refused an allowed request: %+v", rerr)
	}
	if s.calls != 1 {
		t.Fatalf("sampler called %d times, want 1", s.calls)
	}
	if len(s.last.Messages) != 1 || s.last.Messages[0].Text != "hello" {
		t.Errorf("messages did not survive decoding: %+v", s.last.Messages)
	}
	if s.last.SystemPrompt != "be brief" {
		t.Errorf("systemPrompt = %q", s.last.SystemPrompt)
	}
	// The reply must carry the fields the MCP spec requires of a sampling
	// result, or the server cannot read it.
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"model"`, `"role"`, `"content"`, "test-model", "hi"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("result is missing %s: %s", want, b)
		}
	}
}

// A server asking for a million tokens is asking to spend a lot of someone
// else's money on one call.
func TestInboundHandler_SamplingCapsMaxTokens(t *testing.T) {
	s := &recordingSampler{}
	h := newInboundHandler("linear", InboundOptions{
		AllowSampling: true,
		Sample:        s.sample,
		Consent:       func(context.Context, ConsentRequest) bool { return true },
	})

	if _, rerr := h(context.Background(), "sampling/createMessage", sampleParams(t, 10_000_000)); rerr != nil {
		t.Fatal(rerr)
	}
	if s.last.MaxTokens > maxSamplingTokens {
		t.Errorf("MaxTokens = %d, want it capped at %d", s.last.MaxTokens, maxSamplingTokens)
	}
	if s.last.MaxTokens <= 0 {
		t.Errorf("MaxTokens = %d, the cap must not zero the request", s.last.MaxTokens)
	}
}

func TestInboundHandler_ElicitationRefusedWhenNotEnabled(t *testing.T) {
	called := false
	h := newInboundHandler("linear", InboundOptions{
		AllowElicitation: false,
		Elicit: func(context.Context, ElicitationRequest) (ElicitationResult, error) {
			called = true
			return ElicitationResult{}, nil
		},
		Consent: func(context.Context, ConsentRequest) bool { return true },
	})

	if _, rerr := h(context.Background(), "elicitation/create", json.RawMessage(`{"message":"api key?"}`)); rerr == nil {
		t.Fatal("elicitation was served for a server that never opted in")
	}
	if called {
		t.Error("the user was prompted by a server that never opted in")
	}
}

// Enabled but nobody to ask — orchestra core with no interactive channel.
// Declining is the spec's own answer for "the user did not accept", and it
// lets the server carry on; an error would look like a client fault.
func TestInboundHandler_ElicitationDeclinesWithNoInteractiveChannel(t *testing.T) {
	h := newInboundHandler("linear", InboundOptions{
		AllowElicitation: true,
		Elicit:           nil, // no way to ask anyone
		Consent:          func(context.Context, ConsentRequest) bool { return true },
	})

	res, rerr := h(context.Background(), "elicitation/create", json.RawMessage(`{"message":"api key?"}`))
	if rerr != nil {
		t.Fatalf("want a decline result, got an error: %+v", rerr)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"decline"`) {
		t.Errorf("result = %s, want action decline", b)
	}
}

func TestInboundHandler_ElicitationServedWhenAllowed(t *testing.T) {
	var got ElicitationRequest
	h := newInboundHandler("linear", InboundOptions{
		AllowElicitation: true,
		Elicit: func(_ context.Context, req ElicitationRequest) (ElicitationResult, error) {
			got = req
			return ElicitationResult{Action: "accept", Content: map[string]any{"name": "andrey"}}, nil
		},
		Consent: func(context.Context, ConsentRequest) bool { return true },
	})

	res, rerr := h(context.Background(), "elicitation/create",
		json.RawMessage(`{"message":"your name?","requestedSchema":{"type":"object"}}`))
	if rerr != nil {
		t.Fatalf("refused an allowed elicitation: %+v", rerr)
	}
	if got.Message != "your name?" {
		t.Errorf("message did not survive decoding: %q", got.Message)
	}
	if got.Server != "linear" {
		t.Errorf("the prompt must name the server asking: %+v", got)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "andrey") {
		t.Errorf("result = %s, want the submitted content", b)
	}
}

func TestInboundHandler_UnknownMethodIsMethodNotFound(t *testing.T) {
	h := newInboundHandler("linear", InboundOptions{})
	_, rerr := h(context.Background(), "roots/list", nil)
	if rerr == nil || rerr.Code != rpcMethodNotFound {
		t.Fatalf("want method-not-found, got %+v", rerr)
	}
}

// No consent function at all means nothing can grant consent. Failing closed
// is the only safe default for a seam that spends money and prompts people.
func TestInboundHandler_NoConsentFuncRefuses(t *testing.T) {
	s := &recordingSampler{}
	h := newInboundHandler("linear", InboundOptions{
		AllowSampling: true,
		Sample:        s.sample,
		Consent:       nil,
	})
	if _, rerr := h(context.Background(), "sampling/createMessage", sampleParams(t, 10)); rerr == nil {
		t.Fatal("served a request with no way to obtain consent")
	}
	if s.calls != 0 {
		t.Error("the model was called with no way to obtain consent")
	}
}
