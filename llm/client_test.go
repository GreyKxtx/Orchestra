package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClient_BuildsToolsPayload_AndParsesToolCalls(t *testing.T) {
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = b

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "",
        "tool_calls": [
          {
            "id": "call_1",
            "type": "function",
            "function": {
              "name": "read",
              "arguments": "{\"path\":\"a.txt\",\"max_bytes\":123}"
            }
          }
        ]
      }
    }
  ]
}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{
		Provider:    "openai",
		APIBase:     srv.URL, // client will append /v1 if missing
		APIKey:      "test",
		Model:       "test-model",
		MaxTokens:   1234,
		Temperature: 0.0,
		TimeoutS:    5,
	})

	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "system"},
			{Role: RoleUser, Content: "user"},
		},
		Tools: []ToolDef{
			{
				Type: "function",
				Function: ToolFunctionDef{
					Name:       "read",
					Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Type != "function" {
		t.Fatalf("expected type=function, got %q", tc.Type)
	}
	if tc.Function.Name != "read" {
		t.Fatalf("expected tool name fs.read, got %q", tc.Function.Name)
	}
	if string(tc.Function.Arguments.Raw()) != `{"path":"a.txt","max_bytes":123}` {
		t.Fatalf("unexpected tool arguments: %s", string(tc.Function.Arguments.Raw()))
	}

	// Validate request payload contains tools + tool_choice=auto.
	var req map[string]any
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v\nbody=%s", err, string(gotBody))
	}
	if req["model"] != "test-model" {
		t.Fatalf("expected model=test-model, got %#v", req["model"])
	}
	if _, ok := req["tools"]; !ok {
		t.Fatalf("expected tools in request, got: %s", string(gotBody))
	}
	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice=auto, got %#v", req["tool_choice"])
	}
}

func TestOpenAIClient_VLLMOmitsToolChoice(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{
		Provider:  "vllm",
		APIBase:   srv.URL,
		Model:     "qwen",
		MaxTokens: 64,
		TimeoutS:  5,
	})
	_, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDef{{
			Type: "function",
			Function: ToolFunctionDef{
				Name:       "read",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if _, ok := req["tools"]; !ok {
		t.Fatal("expected tools")
	}
	if _, ok := req["tool_choice"]; ok {
		t.Fatalf("vllm should omit tool_choice, got %#v", req["tool_choice"])
	}
}

func TestResolveToolChoice(t *testing.T) {
	if got := resolveToolChoice(LLMConfig{Provider: "vllm"}); got != "omit" {
		t.Fatalf("vllm default = %q", got)
	}
	if got := resolveToolChoice(LLMConfig{}); got != "omit" {
		t.Fatalf("empty provider default = %q", got)
	}
	if got := resolveToolChoice(LLMConfig{Provider: "custom"}); got != "omit" {
		t.Fatalf("custom default = %q", got)
	}
	if got := resolveToolChoice(LLMConfig{Provider: "openai"}); got != "auto" {
		t.Fatalf("openai default = %q", got)
	}
	if got := resolveToolChoice(LLMConfig{Provider: "vllm", ToolChoice: "auto"}); got != "auto" {
		t.Fatalf("explicit auto = %q", got)
	}
}

func TestWarnImplicitToolChoiceOnce(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{Provider: "vllm"}) // no ToolChoice set → implicit "omit"
	if !c.toolChoiceImplicit {
		t.Fatal("expected toolChoiceImplicit=true when cfg.ToolChoice is blank")
	}
	if c.toolChoice != "omit" {
		t.Fatalf("toolChoice = %q, want omit", c.toolChoice)
	}
	// No tools on the request → no warning needed, flag stays false.
	c.warnImplicitToolChoiceOnce(0)
	if c.toolChoiceWarned {
		t.Fatal("should not warn when the request carries no tools")
	}
	c.warnImplicitToolChoiceOnce(3)
	if !c.toolChoiceWarned {
		t.Fatal("expected warning once a tool-bearing request is sent with implicit omit")
	}

	explicit := NewOpenAIClient(LLMConfig{Provider: "vllm", ToolChoice: "omit"})
	if explicit.toolChoiceImplicit {
		t.Fatal("explicit ToolChoice=omit in config must not be flagged as implicit")
	}
	explicit.warnImplicitToolChoiceOnce(3)
	if explicit.toolChoiceWarned {
		t.Fatal("should not warn when the user explicitly configured omit")
	}
}

func TestEffectiveMaxTokens(t *testing.T) {
	if got := effectiveMaxTokens(0, 0); got != defaultMaxTokens {
		t.Fatalf("default: got %d", got)
	}
	if got := effectiveMaxTokens(8192, 0); got != 8192 {
		t.Fatalf("passthrough: got %d", got)
	}
	// Cap completion at ~20% of the window (most stays for prompt/history).
	if got := effectiveMaxTokens(50000, 51200); got != 10240 {
		t.Fatalf("20%% completion cap: got %d want 10240", got)
	}
	wantCap := 51200 / 5
	if got := effectiveMaxTokens(60000, 51200); got != wantCap {
		t.Fatalf("ceiling: got %d want %d", got, wantCap)
	}
	if got := effectiveMaxTokens(8192, 51200); got != 8192 {
		t.Fatalf("under cap passthrough: got %d", got)
	}
}

func TestClampMaxTokensForPrompt_ShrinksWhenPromptGrows(t *testing.T) {
	// want 32k, context 51.2k, prompt ~25k → room ≈ 51200-25000-2048 = 24152
	got := clampMaxTokensForPrompt(32256, 51200, 25000)
	if got > 24152 || got < 256 {
		t.Fatalf("got %d", got)
	}
	// Small prompt keeps configured want (after it fits)
	got = clampMaxTokensForPrompt(8192, 51200, 2000)
	if got != 8192 {
		t.Fatalf("small prompt: got %d want 8192", got)
	}
}

func TestMaxTokensForRequest_Dynamic(t *testing.T) {
	c := &OpenAIClient{
		wantMaxTokens: 32256,
		maxTokens:     32256,
		contextTokens: 51200,
	}
	// Large synthetic prompt
	var big strings.Builder
	for i := 0; i < 80_000; i++ {
		big.WriteByte('a')
	}
	tok, err := c.maxTokensForRequest(CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: big.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok >= 32256 {
		t.Fatalf("expected dynamic shrink, got %d", tok)
	}
	promptEst := estimateRequestTokens(CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: big.String()}},
	})
	if tok+promptEst+512 > 51200 {
		t.Fatalf("tok(%d)+prompt(%d) overflows context", tok, promptEst)
	}
}

func TestMergeExtraBody_DoesNotOverrideMaxTokens(t *testing.T) {
	req := chatCompletionRequest{Model: "m", MaxTokens: 8192}
	b, err := mergeExtraBody(req, map[string]any{"max_tokens": 50000, "num_ctx": 20000, "foo": "bar"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if int(m["max_tokens"].(float64)) != 8192 {
		t.Fatalf("max_tokens overwritten: %#v", m["max_tokens"])
	}
	if m["foo"] != "bar" {
		t.Fatalf("expected foo merge, got %#v", m["foo"])
	}
}

func TestNormalizeExtraBody_QwenThinkingOff_StripsNumCtx(t *testing.T) {
	out := normalizeExtraBody("vllm", "Qwen/Qwen3.6-27B-FP8", map[string]any{"num_ctx": 51200})
	if _, ok := out["num_ctx"]; ok {
		t.Fatal("num_ctx must be stripped for vllm")
	}
	kw, ok := out["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat_template_kwargs, got %#v", out)
	}
	if kw["enable_thinking"] != false {
		t.Fatalf("enable_thinking = %#v, want false", kw["enable_thinking"])
	}
}

func TestOpenAIClient_DefaultsMaxTokensWhenZero(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{
		Provider: "vllm",
		APIBase:  srv.URL,
		Model:    "other-model",
		TimeoutS: 5,
	})
	if _, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if int(req["max_tokens"].(float64)) != defaultMaxTokens {
		t.Fatalf("max_tokens = %#v, want %d", req["max_tokens"], defaultMaxTokens)
	}
}
