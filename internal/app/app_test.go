package app

import (
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

func testSettings() Settings {
	cfg := &config.ProjectConfig{}
	cfg.LLM.Model = "qwen2.5-coder:7b"
	cfg.LLM.TimeoutS = 300
	cfg.Agent.MaxSteps = 200
	cfg.Exec.Allow = []string{"go"}
	cfg.Permissions.Rules = []config.PermissionRule{{Tool: "bash", Action: "ask"}}
	return SettingsFrom(cfg)
}

// The order after the caller's wiring is the contract: a profile does not
// undo agent.max_steps, llm.timeout_s wins over it, and a named agent's prompt
// and tools survive it.
func TestTurnOptions_ProfileAndOverrides(t *testing.T) {
	s := testSettings()
	tools := []llm.ToolDef{{Function: llm.ToolFunctionDef{Name: "read"}}}
	o, err := TurnOptions(s, "fast", func(o *agent.Options) {
		o.Mode = agent.ModeBuild
		o.SystemPromptOverride = "you review"
		o.CustomTools = tools
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.MaxSteps != 200 {
		t.Errorf("max_steps from config must survive the profile, got %d", o.MaxSteps)
	}
	if o.LLMStepTimeout != 300*time.Second {
		t.Errorf("llm.timeout_s must win, got %v", o.LLMStepTimeout)
	}
	if o.SystemPromptOverride != "you review" || len(o.CustomTools) != 1 {
		t.Errorf("a named agent's prompt and tools must survive the profile")
	}
	if len(o.ExecAllow) != 1 || len(o.PermissionRules) != 1 || o.MaxDeniedToolRepeats == 0 {
		t.Errorf("config consent, rules and breakers reach the turn: %+v %+v %d", o.ExecAllow, o.PermissionRules, o.MaxDeniedToolRepeats)
	}
	if o.IsChild {
		t.Error("a turn is not a child")
	}
	if _, err := TurnOptions(s, "nonsense", nil); err == nil {
		t.Error("an unknown profile is an error")
	}
}

// A child answers through task_result, takes the rules and budgets of the
// turn, and speaks to its own model.
func TestChildOptions(t *testing.T) {
	s := testSettings()
	o := ChildOptions(&s, func(o *agent.Options) {
		o.ModelLabel = "claude-opus-4"
	})
	if !o.IsChild {
		t.Error("a child answers through task_result")
	}
	if len(o.PermissionRules) != 1 || o.LLMStepTimeout != 300*time.Second {
		t.Errorf("the turn's rules and step timeout reach the child")
	}
	if len(o.ExecAllow) != 0 {
		t.Error("exec consent is the caller's to pass down, not the config's")
	}
	if o.PromptFamily == s.PromptFamily {
		t.Errorf("the prompt family follows the child's model, not the turn's: %q", o.PromptFamily)
	}
	if bare := ChildOptions(nil, nil); !bare.IsChild || bare.MaxSteps != 0 {
		t.Errorf("without settings a child keeps the agent's defaults: %+v", bare)
	}
}

func TestClientFor(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.LLM = config.LLMConfig{APIBase: "http://127.0.0.1:1/v1", Model: "main"}
	cfg.Providers = map[string]config.LLMConfig{"fast": {APIBase: "http://127.0.0.1:2/v1", Model: "small"}}

	if _, used, err := ClientFor(cfg, "", "", nil); err != nil || used.Model != "main" {
		t.Errorf("no provider or model is the main endpoint: %v %v", used.Model, err)
	}
	if _, used, err := ClientFor(cfg, "", "big", nil); err != nil || used.Model != "big" || used.APIBase != cfg.LLM.APIBase {
		t.Errorf("a bare model runs on the main endpoint: %+v %v", used, err)
	}
	if _, used, err := ClientFor(cfg, "fast", "", nil); err != nil || used.Model != "small" || used.APIBase != "http://127.0.0.1:2/v1" {
		t.Errorf("a provider brings its own endpoint and model: %+v %v", used, err)
	}
	if _, _, err := ClientFor(cfg, "nope", "", nil); err == nil {
		t.Error("an unknown provider is an error")
	}
	if _, _, err := ClientFor(nil, "", "", nil); err == nil {
		t.Error("no config is an error")
	}
}
