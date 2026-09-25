package llm

import (
	"errors"
	"testing"
)

// The Anthropic client names itself before the status; 429/529 must still be
// retried and its "prompt is too long" 400 must still read as an overflow.
func TestAnthropicErrorsAreClassified(t *testing.T) {
	for _, msg := range []string{
		"anthropic API error (status 429): rate limited",
		"anthropic API error (status 529): Overloaded",
		"anthropic API status 503: upstream",
		"anthropic stream: Overloaded",
	} {
		if !IsTransientLLMError(errors.New(msg)) {
			t.Errorf("%q must be transient", msg)
		}
	}
	if IsTransientLLMError(errors.New("anthropic API error (status 401): invalid x-api-key")) {
		t.Error("401 is not transient")
	}
	ov, ok := ParseContextOverflow(errors.New("anthropic API error (status 400): prompt is too long: 215000 tokens > 200000 maximum"))
	if !ok {
		t.Fatal("Anthropic's prompt-too-long must be an overflow")
	}
	if ov.ContextTokens != 200000 || ov.PromptTokens != 215000 {
		t.Fatalf("window and prompt swapped: %+v", ov)
	}
}

// OpenAI's wording names the window first and the prompt second; the prompt
// is the larger number.
func TestGenericOverflowKeepsTheWindowSmaller(t *testing.T) {
	ov, ok := ParseContextOverflow(errors.New("API error (status 400): This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens."))
	if !ok {
		t.Fatal("must parse")
	}
	if ov.ContextTokens != 128000 || ov.PromptTokens != 130000 {
		t.Fatalf("got %+v, want window 128000 and prompt 130000", ov)
	}
}
