package cli

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// A running local server gives the project its endpoint, never its model:
// the model is the user's pick among what the server lists.
func TestApplyDetectedLocalServer_TakesTheEndpointNotAModel(t *testing.T) {
	cfg := &config.ProjectConfig{}

	report := applyDetectedLocalServer(cfg, llm.LocalServer{
		APIBase: "http://localhost:11434/v1",
		Models:  []llm.RemoteModel{{ID: "qwen2.5-coder:14b"}, {ID: "llama3:8b"}},
	}, true)

	if cfg.LLM.APIBase != "http://localhost:11434/v1" {
		t.Errorf("api_base = %q, want the detected server", cfg.LLM.APIBase)
	}
	if cfg.LLM.Model != "" {
		t.Errorf("model = %q, want none: the user picks it", cfg.LLM.Model)
	}
	if !strings.Contains(report, "11434") {
		t.Errorf("report = %q, must name the endpoint it chose", report)
	}
	// The user picks among them, so they have to be named.
	if !strings.Contains(report, "qwen2.5-coder:14b") || !strings.Contains(report, "llama3:8b") {
		t.Errorf("report = %q, must list the server's models", report)
	}
}

// Nothing detected touches nothing and announces nothing.
func TestApplyDetectedLocalServer_LeavesTheConfigWhenNothingFound(t *testing.T) {
	cfg := &config.ProjectConfig{}

	report := applyDetectedLocalServer(cfg, llm.LocalServer{}, false)

	if cfg.LLM.APIBase != "" || cfg.LLM.Model != "" {
		t.Errorf("the config was touched: api_base=%q model=%q", cfg.LLM.APIBase, cfg.LLM.Model)
	}
	if report != "" {
		t.Errorf("report = %q, want empty — init must not announce a detection that did not happen", report)
	}
}

// A server with no models has nothing to offer the picker.
func TestApplyDetectedLocalServer_IgnoresAServerWithNoModels(t *testing.T) {
	cfg := &config.ProjectConfig{}

	report := applyDetectedLocalServer(cfg, llm.LocalServer{APIBase: "http://localhost:8000/v1"}, true)

	if cfg.LLM.APIBase != "" || cfg.LLM.Model != "" {
		t.Errorf("the config was touched: api_base=%q model=%q", cfg.LLM.APIBase, cfg.LLM.Model)
	}
	if report != "" {
		t.Errorf("report = %q, want empty", report)
	}
}

// Past the cap the report counts the rest instead of listing them.
func TestApplyDetectedLocalServer_CountsWhatItDoesNotList(t *testing.T) {
	cfg := &config.ProjectConfig{}
	models := []llm.RemoteModel{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"}}
	report := applyDetectedLocalServer(cfg, llm.LocalServer{APIBase: "http://localhost:1234/v1", Models: models}, true)

	if !strings.Contains(report, "и ещё 2") {
		t.Errorf("report = %q, must count the models it did not list", report)
	}
}
