package core

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// A subagent with no model of its own runs on the turn's client. In an
// orchestra turn that is the planner, and the child must be announced as the
// planner's model — not as the project's llm:, which named the local
// placeholder while the architect ran on Claude.
func TestTurnChildrenAreNamedAfterThePlanner(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.Model = "qwen2.5-coder-7b"
	cfg.Providers = map[string]config.LLMConfig{
		"anthropic": {Provider: "anthropic", APIBase: "https://api.anthropic.com", APIKey: "sk-test", Model: "claude-sonnet-5-5"},
	}
	cfg.Orchestra.Planner.Provider = "anthropic"
	cfg.Orchestra.Planner.Model = "claude-opus-5-5"
	c := &Core{cfg: cfg, workspaceRoot: root}

	p := c.newTurnPrep(agentLaunchSpec{Mode: string(agent.ModeOrchestra)}, "")
	p.effectiveMode = string(agent.ModeOrchestra)
	if err := c.resolveTurnAgent(p); err != nil {
		t.Fatal(err)
	}
	c.newTurnTaskRunner(p)
	if got := p.childCfg.ModelLabel; got != "claude-opus-5-5" {
		t.Fatalf("children on the turn's client are named %q, want the planner's claude-opus-5-5", got)
	}
	if _, model := c.turnLabelsFor(p); model != "claude-opus-5-5" {
		t.Fatalf("the turn is named %q, want the planner's claude-opus-5-5", model)
	}

	// Without a planner the turn runs on llm:, and so do its children.
	cfg.Orchestra.Planner.Provider, cfg.Orchestra.Planner.Model = "", ""
	q := c.newTurnPrep(agentLaunchSpec{Mode: string(agent.ModeOrchestra)}, "")
	q.effectiveMode = string(agent.ModeOrchestra)
	if err := c.resolveTurnAgent(q); err != nil {
		t.Fatal(err)
	}
	c.newTurnTaskRunner(q)
	if got := q.childCfg.ModelLabel; got != "qwen2.5-coder-7b" {
		t.Fatalf("children are named %q, want llm:'s qwen2.5-coder-7b", got)
	}
}

// A project whose model was never picked starts no turn: it is refused with
// "no model selected" before the first step, not with a provider's 400.
func TestATurnWithNoModelIsRefusedBeforeItStarts(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase, cfg.LLM.Model = "", ""
	c := &Core{cfg: cfg, workspaceRoot: root, llmClient: llm.BuildClient(cfg.LLM, cfg.LLMRegistry(), nil)}

	p := c.newTurnPrep(agentLaunchSpec{}, "")
	err := c.resolveTurnAgent(p)
	if err == nil || !strings.Contains(err.Error(), "no model selected") {
		t.Fatalf("a turn with no model = %v, want no model selected", err)
	}
}
