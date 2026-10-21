package app

import (
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// Seen live: an orchestra turn on Claude Opus, in a project whose llm: still
// named a local 32K model with no num_ctx for Claude, measured itself
// against a 50 KB history budget and announced "context nearly full" after a
// few file reads. The agent is sized to the client it actually runs on.
func TestFitWindow_SizesTheAgentToItsClient(t *testing.T) {
	claude := llm.NewAnthropicClient(llm.LLMConfig{Model: "claude-opus-5-5", APIKey: "k"})
	o := agent.Options{MaxPromptBytes: 50 * 1024, CompletionMaxTokens: 4096, BytesPerContextToken: 4}
	FitWindow(&o, claude)
	want := llm.ModelContextWindow("claude-opus-5-5")
	if want <= 0 || o.ModelContextTokens != want {
		t.Fatalf("ModelContextTokens = %d, want the model's window %d", o.ModelContextTokens, want)
	}
	if o.MaxPromptBytes <= 50*1024*4 {
		t.Fatalf("MaxPromptBytes = %d: the history budget must fill the model's window, not stay at 50 KB", o.MaxPromptBytes)
	}

	// A hosted OpenAI-compatible endpoint ignores a num_ctx left over from a
	// local model.
	hosted := llm.NewOpenAIClient(llm.LLMConfig{Provider: "openai", APIBase: "https://api.openai.com/v1", Model: "gpt-4.1",
		ExtraBody: map[string]any{"num_ctx": 32768}})
	o2 := agent.Options{}
	FitWindow(&o2, hosted)
	if o2.ModelContextTokens == 32768 || o2.ModelContextTokens <= 0 {
		t.Fatalf("hosted window = %d: num_ctx is a local server's setting", o2.ModelContextTokens)
	}

	// A local server keeps what it was told: its real window may be far
	// below the model's.
	local := llm.NewOpenAIClient(llm.LLMConfig{APIBase: "http://localhost:1234/v1", Model: "qwen2.5-coder-7b",
		ExtraBody: map[string]any{"num_ctx": 8192}})
	o3 := agent.Options{MaxPromptBytes: 50 * 1024}
	FitWindow(&o3, local)
	if o3.ModelContextTokens != 8192 {
		t.Fatalf("local window = %d, want num_ctx 8192", o3.ModelContextTokens)
	}
	if o3.MaxPromptBytes != 50*1024 {
		t.Fatalf("local budget = %d: a larger context_kb must not shrink", o3.MaxPromptBytes)
	}
}
