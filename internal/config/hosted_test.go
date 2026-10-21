package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// num_ctx is a local server's setting; one left over from the model a
// project started on made a Claude project measure itself against 32K.
func TestEffectiveNumCtx_IgnoredForAHostedEndpoint(t *testing.T) {
	c := &ProjectConfig{LLM: LLMConfig{Provider: "anthropic", APIBase: "https://api.anthropic.com", Model: "claude-opus-5-5",
		ExtraBody: map[string]any{"num_ctx": 32768}}}
	if n := c.EffectiveNumCtx(); n != 0 {
		t.Fatalf("EffectiveNumCtx = %d on Anthropic, want 0", n)
	}
	local := &ProjectConfig{LLM: LLMConfig{APIBase: "http://localhost:1234/v1", Model: "qwen", ExtraBody: map[string]any{"num_ctx": 32768}}}
	if n := local.EffectiveNumCtx(); n != 32768 {
		t.Fatalf("EffectiveNumCtx = %d on a local server, want 32768", n)
	}
}

// Seen live: a key typed into the settings panel was saved into the
// project's .orchestra.yml, which is committed, next to a comment promising
// keys never go there.
func TestSave_KeysFromThePanelGoToTheEnvFile(t *testing.T) {
	isolateHome(t) // Save records a new config as trusted, in ~/.orchestra
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	c := &ProjectConfig{
		ProjectRoot: dir,
		LLM:         LLMConfig{Provider: "anthropic", APIBase: "https://api.anthropic.com", Model: "claude-opus-5-5", APIKey: "sk-ant-secret-1"},
		Providers:   map[string]LLMConfig{"anthropic": {Provider: "anthropic", APIKey: "sk-ant-secret-1"}},
	}
	if err := c.MoveKeysToEnvFile(); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	yml, _ := os.ReadFile(path)
	if strings.Contains(string(yml), "sk-ant-secret-1") {
		t.Fatalf(".orchestra.yml holds the key:\n%s", yml)
	}
	if !strings.Contains(string(yml), "${ORCHESTRA_API_KEY}") || !strings.Contains(string(yml), "${ANTHROPIC_API_KEY}") {
		t.Fatalf(".orchestra.yml does not refer to the variables:\n%s", yml)
	}
	env, _ := os.ReadFile(filepath.Join(dir, EnvFileName))
	if !strings.Contains(string(env), "ANTHROPIC_API_KEY=sk-ant-secret-1") || !strings.Contains(string(env), "ORCHESTRA_API_KEY=sk-ant-secret-1") {
		t.Fatalf("%s = %q", EnvFileName, env)
	}
	if c.LLM.APIKey != "sk-ant-secret-1" {
		t.Fatalf("the running config lost its key: %q", c.LLM.APIKey)
	}
	// Loading it back gives the key again, from the env file.
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.LLM.APIKey != "sk-ant-secret-1" {
		t.Fatalf("reloaded llm.api_key = %q", got.LLM.APIKey)
	}
}
