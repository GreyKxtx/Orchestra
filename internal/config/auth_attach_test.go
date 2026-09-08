package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAuthConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_AttachesTokenSourceToNamedProvider(t *testing.T) {
	path := writeAuthConfig(t, `
project_root: .
llm:
  api_base: http://localhost:8000/v1
  model: local
providers:
  vertex:
    api_base: https://example.com/v1
    model: gemini
    auth:
      token_command: ["echo", "cmd-token"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.Providers["vertex"]
	if p.TokenSource == nil {
		t.Fatal("Load must attach TokenSource to a provider with an auth block")
	}
	got, err := p.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "cmd-token" {
		t.Fatalf("token = %q, want cmd-token", got)
	}
}

func TestLoad_AttachesTokenSourceToMainLLM(t *testing.T) {
	path := writeAuthConfig(t, `
project_root: .
llm:
  api_base: https://example.com/v1
  model: gpt
  auth:
    token_command: ["echo", "main-token"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.TokenSource == nil {
		t.Fatal("Load must attach TokenSource to the main llm block")
	}
	got, err := cfg.LLM.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "main-token" {
		t.Fatalf("token = %q, want main-token", got)
	}
}

func TestLoad_RejectsAnInvalidAuthBlock(t *testing.T) {
	path := writeAuthConfig(t, `
project_root: .
llm:
  api_base: https://example.com/v1
  model: gpt
providers:
  broken:
    api_base: https://example.com/v1
    auth:
      oauth:
        auth_url: https://a/authorize
        token_url: https://a/token
        client_id: c
      token_command: ["echo", "x"]
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must reject a provider with both auth mechanisms")
	}
	// Assert the reason, not merely that something failed: this test passed
	// against an unrelated validation error before project_root was set.
	if !strings.Contains(err.Error(), "exactly one of oauth or token_command") {
		t.Fatalf("err = %q, want it to name the auth block problem", err.Error())
	}
}

func TestFindProvider_InheritsTokenSourceFromLLM(t *testing.T) {
	path := writeAuthConfig(t, `
project_root: .
llm:
  api_base: https://example.com/v1
  model: gpt
  auth:
    token_command: ["echo", "main-token"]
providers:
  child:
    model: gpt-mini
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.FindProvider("child")
	if !ok {
		t.Fatal("provider child must resolve")
	}
	if p.TokenSource == nil {
		t.Fatal("a provider with no credential of its own must inherit the main llm token source")
	}
	got, err := p.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "main-token" {
		t.Fatalf("token = %q, want the inherited main-token", got)
	}
}

// A provider with its own auth block must keep it: inheriting the parent's
// token source over it would silently authenticate against the wrong
// identity.
func TestFindProvider_OwnAuthBlockIsNotOverwrittenByInheritance(t *testing.T) {
	path := writeAuthConfig(t, `
project_root: .
llm:
  api_base: https://example.com/v1
  model: gpt
  auth:
    token_command: ["echo", "main-token"]
providers:
  child:
    api_base: https://child.example.com/v1
    model: gpt-mini
    auth:
      token_command: ["echo", "child-token"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.FindProvider("child")
	if !ok {
		t.Fatal("provider child must resolve")
	}
	got, err := p.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "child-token" {
		t.Fatalf("token = %q, want the provider's own child-token", got)
	}
}
