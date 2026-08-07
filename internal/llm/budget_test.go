package llm

import (
	"errors"
	"testing"
)

func TestCompletionRoom(t *testing.T) {
	// 51200 - 40000 - 2048 = 9152
	if got := CompletionRoom(51200, 40000); got != 9152 {
		t.Fatalf("got %d want 9152", got)
	}
	if got := CompletionRoom(51200, 50000); got != MinCompletionTokens {
		t.Fatalf("near-full prompt: got %d want %d", got, MinCompletionTokens)
	}
}

func TestPromptBudgetTokens(t *testing.T) {
	// want 8192 stays 8192 (no half-window tax)
	// budget = 51200 - 8192 - 2048 = 40960
	got := PromptBudgetTokens(51200, 8192)
	if got != 40960 {
		t.Fatalf("got %d want 40960", got)
	}
}

func TestFitsContext(t *testing.T) {
	if FitsContext(51200, 40000, 12054) {
		t.Fatal("40000+12054+2048 > 51200 should not fit")
	}
	if !FitsContext(51200, 20000, 8192) {
		t.Fatal("20000+8192+2048 ? 51200 should fit")
	}
}

func TestParseContextOverflow(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		ctxLen int
		prompt int
		wantOK bool
	}{
		{
			name:   "vllm out+in wording",
			msg:    "request failed (status 400): This model's maximum context length is 51200 tokens. However, you requested 12054 output tokens and your prompt contains at least 40000 input tokens, for a total of at least 52054 tokens.",
			ctxLen: 51200, prompt: 40000, wantOK: true,
		},
		{
			name:   "legacy in-the-messages wording",
			msg:    "maximum context length is 51200 tokens. However, you requested 53344 tokens (45152 in the messages, 8192 in the completion).",
			ctxLen: 51200, prompt: 45152, wantOK: true,
		},
		{
			name:   "local preflight rejection",
			msg:    "prompt too large (~52000 tokens) for model context 51200 - compact the conversation",
			ctxLen: 51200, prompt: 52000, wantOK: true,
		},
		{
			name: "unrelated failure",
			msg:  "connection reset by peer",
		},
		{
			name:   "generic 400 with context-window wording (unknown server)",
			msg:    "request failed (status 400): the request exceeds the model's maximum context length of 32768; please reduce the length of the messages",
			ctxLen: 32768, wantOK: true,
		},
		{
			name: "400 with unrelated body must not false-positive",
			msg:  "request failed (status 400): invalid value for 'temperature': must be between 0 and 2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseContextOverflow(errors.New(tc.msg))
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.ContextTokens != tc.ctxLen || got.PromptTokens != tc.prompt {
				t.Fatalf("got ctx=%d prompt=%d want ctx=%d prompt=%d",
					got.ContextTokens, got.PromptTokens, tc.ctxLen, tc.prompt)
			}
		})
	}
	if IsContextOverflowError(nil) {
		t.Fatal("nil error must not be an overflow")
	}
}
