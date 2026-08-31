package prompt

import (
	"strings"
	"testing"
)

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
	for _, alias := range []string{"qwen", "chatml", "llama", "llama-instruct"} {
		if got := ResolvePromptFamily(alias, "claude-sonnet"); got != "local" {
			t.Fatalf("alias %q → %q want local", alias, got)
		}
	}
}

func TestBuildSystemPromptForMode_AliasUsesLocal(t *testing.T) {
	local := BuildSystemPromptForMode("build", "local")
	viaAlias := BuildSystemPromptForMode("build", "qwen")
	if local == "" || viaAlias != local {
		t.Fatalf("qwen alias should load build-local.txt")
	}
	if !containsPreferSearchReplace(local) {
		t.Fatalf("build-local.txt missing search_replace preference")
	}
}

func containsPreferSearchReplace(s string) bool {
	return strings.Contains(s, "PREFER file.search_replace") ||
		strings.Contains(s, "(preferred")
}
