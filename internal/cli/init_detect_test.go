package cli

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// `orchestra init` used to hardcode http://localhost:1234/v1 and
// qwen2.5-coder-7b whether or not anything was listening there, so a user on
// Ollama got a config that could not talk to their server and no sign of it.
func TestApplyDetectedLocalServer_PrefillsFromTheLiveServer(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.LLM.APIBase = "http://localhost:1234/v1"
	cfg.LLM.Model = "qwen2.5-coder-7b"

	report := applyDetectedLocalServer(cfg, llm.LocalServer{
		APIBase: "http://localhost:11434/v1",
		Models:  []llm.RemoteModel{{ID: "qwen2.5-coder:14b"}, {ID: "llama3:8b"}},
	}, true)

	if cfg.LLM.APIBase != "http://localhost:11434/v1" {
		t.Errorf("api_base = %q, want the detected server", cfg.LLM.APIBase)
	}
	if cfg.LLM.Model != "qwen2.5-coder:14b" {
		t.Errorf("model = %q, want the server's first model", cfg.LLM.Model)
	}
	if !strings.Contains(report, "11434") {
		t.Errorf("report = %q, must name the endpoint it chose", report)
	}
	// Two models were offered and one was picked; the user has to be told the
	// other exists or they will not know the choice was made for them.
	if !strings.Contains(report, "llama3:8b") {
		t.Errorf("report = %q, must list the other models so the pick is visible", report)
	}
}

// Nothing detected must leave the static defaults exactly as they were: a
// probe that finds nothing is not a reason to change the config it did not
// look at.
func TestApplyDetectedLocalServer_LeavesDefaultsWhenNothingFound(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.LLM.APIBase = "http://localhost:1234/v1"
	cfg.LLM.Model = "qwen2.5-coder-7b"

	report := applyDetectedLocalServer(cfg, llm.LocalServer{}, false)

	if cfg.LLM.APIBase != "http://localhost:1234/v1" || cfg.LLM.Model != "qwen2.5-coder-7b" {
		t.Errorf("defaults were touched: api_base=%q model=%q", cfg.LLM.APIBase, cfg.LLM.Model)
	}
	if report != "" {
		t.Errorf("report = %q, want empty — init must not announce a detection that did not happen", report)
	}
}

// A detected server with no models cannot prefill a model, and an api_base
// without a model is worse than the default pair it would replace.
func TestApplyDetectedLocalServer_IgnoresAServerWithNoModels(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.LLM.APIBase = "http://localhost:1234/v1"
	cfg.LLM.Model = "qwen2.5-coder-7b"

	report := applyDetectedLocalServer(cfg, llm.LocalServer{APIBase: "http://localhost:8000/v1"}, true)

	if cfg.LLM.APIBase != "http://localhost:1234/v1" || cfg.LLM.Model != "qwen2.5-coder-7b" {
		t.Errorf("defaults were touched: api_base=%q model=%q", cfg.LLM.APIBase, cfg.LLM.Model)
	}
	if report != "" {
		t.Errorf("report = %q, want empty", report)
	}
}

// One model is the common case; the report must not trail an empty "others"
// clause.
func TestApplyDetectedLocalServer_SingleModelReportsCleanly(t *testing.T) {
	cfg := &config.ProjectConfig{}
	report := applyDetectedLocalServer(cfg, llm.LocalServer{
		APIBase: "http://localhost:1234/v1",
		Models:  []llm.RemoteModel{{ID: "qwen2.5-coder-7b"}},
	}, true)

	if !strings.Contains(report, "qwen2.5-coder-7b") {
		t.Errorf("report = %q, must name the model", report)
	}
	if strings.Contains(report, "ещё") || strings.HasSuffix(strings.TrimSpace(report), ",") {
		t.Errorf("report = %q, has a dangling list for a single model", report)
	}
}
