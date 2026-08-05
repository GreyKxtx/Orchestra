package prompt

import "testing"

func TestDetectPromptFamily(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"qwen/qwen3.6-27b", "local"},
		{"nvidia/nemotron-3-nano-4b", "local"},
		{"claude-sonnet-4", "anthropic"},
		{"gpt-4o", "gpt"},
		{"gemini-2.0-flash", "gemini"},
		{"kimi-k2", "kimi"},
		{"unknown-model-xyz", "default"},
	}
	for _, tc := range cases {
		if got := DetectPromptFamily(tc.model); got != tc.want {
			t.Errorf("DetectPromptFamily(%q)=%q want %q", tc.model, got, tc.want)
		}
	}
}

func TestResolvePromptFamily(t *testing.T) {
	if got := ResolvePromptFamily("", "qwen/qwen3"); got != "local" {
		t.Fatalf("auto detect: got %q", got)
	}
	if got := ResolvePromptFamily("gpt", "qwen/qwen3"); got != "gpt" {
		t.Fatalf("explicit wins: got %q", got)
	}
}
