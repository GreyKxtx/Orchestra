# Multi-Provider Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow defining multiple named LLM providers in `.orchestra.yml` and selecting them per-agent or at runtime via `--provider <name>`.

**Architecture:** Add a `providers: map[string]LLMConfig` section to config; a centralized `llm.NewClient(cfg)` factory replaces the duplicated switch in `core.go` and `apply.go`; `AgentDefinition` gets a `provider:` field that looks up the named provider; `orchestra apply --provider <name>` overrides the provider for one run. No changes to protocol version — this is purely configuration and client selection.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, cobra, `internal/llm` (OpenAIClient, AnthropicClient)

---

## File Map

| File | Action | What changes |
|------|--------|-------------|
| `internal/llm/factory.go` | Create | `NewClient(cfg) Client` factory — central switch on Provider |
| `internal/llm/factory_test.go` | Create | Unit tests for NewClient |
| `internal/config/config.go` | Modify | Add `Providers map[string]LLMConfig` to `ProjectConfig`; add `FindProvider` method; add `Provider string` to `AgentDefinition` |
| `internal/config/config_test.go` | Modify or Create | Tests for `FindProvider` |
| `internal/core/core.go` | Modify | Use `llm.NewClient` in New(); update `resolveCustomAgentOpts` for provider + model override |
| `internal/cli/apply.go` | Modify | Use `llm.NewClient` everywhere; add `--provider` flag; handle `AgentDefinition.Provider` |

---

## Key Context

**Current state:** Two providers exist (`OpenAIClient`, `AnthropicClient`). Client creation is a duplicated `switch cfg.LLM.Provider` in both `core.go:93-103` and `apply.go:362-372` (direct) + `apply.go:243-251` (pipeline). Custom agent model override hardcodes `llm.NewOpenAIClient` at `core.go:562` and `apply.go:434`.

**`AgentDefinition`** (config.go:223) currently has: `Name`, `SystemPrompt`, `Tools []string`, `Model string`. We add `Provider string`.

**`ProjectConfig`** (config.go:291) has `LLM LLMConfig`. We add `Providers map[string]LLMConfig`.

**Logger pattern:** After creating any client, do a type assertion to set the logger — `if oc, ok := client.(*llm.OpenAIClient); ok { oc.SetLogger(logger) }`. Anthropic clients have no logger — that's fine.

**apply.go direct mode** path: line ~362-372 creates llmClient. Line ~420-423 extracts `agentLogger` from the client. Line ~428-437 handles custom agent model override.

---

## Task 1: `llm.NewClient` factory + `config.Providers` + `FindProvider`

**Files:**
- Create: `internal/llm/factory.go`
- Create: `internal/llm/factory_test.go`
- Modify: `internal/config/config.go` (add `Providers` field + `FindProvider`)
- Modify: `internal/core/core.go` (lines 93-103: replace switch with `llm.NewClient`)
- Modify: `internal/cli/apply.go` (lines 247, 366-372: replace `llm.NewOpenAIClient` with `llm.NewClient`)

- [ ] **Step 1: Write failing tests for `llm.NewClient` and `FindProvider`**

Create `internal/llm/factory_test.go`:

```go
package llm_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
)

func TestNewClient_DefaultIsOpenAI(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient for empty provider, got %T", c)
	}
}

func TestNewClient_OpenAI(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "openai"})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient, got %T", c)
	}
}

func TestNewClient_Anthropic(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "anthropic"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient, got %T", c)
	}
}

func TestNewClient_CaseInsensitive(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "Anthropic"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient for 'Anthropic' (mixed case), got %T", c)
	}
}
```

Also create `internal/config/config_test.go` (or append if it exists) with `FindProvider` tests:

```go
package config_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestFindProvider_Found(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"anthropic": {Provider: "anthropic", APIKey: "sk-ant-test"},
		},
	}
	prov, ok := cfg.FindProvider("anthropic")
	if !ok {
		t.Fatal("FindProvider: expected true for 'anthropic'")
	}
	if prov.Provider != "anthropic" {
		t.Fatalf("FindProvider: expected Provider='anthropic', got %q", prov.Provider)
	}
	if prov.APIKey != "sk-ant-test" {
		t.Fatalf("FindProvider: expected APIKey='sk-ant-test', got %q", prov.APIKey)
	}
}

func TestFindProvider_NotFound(t *testing.T) {
	cfg := &config.ProjectConfig{}
	_, ok := cfg.FindProvider("missing")
	if ok {
		t.Fatal("FindProvider: expected false for missing provider")
	}
}

func TestFindProvider_NilMap(t *testing.T) {
	cfg := &config.ProjectConfig{Providers: nil}
	_, ok := cfg.FindProvider("any")
	if ok {
		t.Fatal("FindProvider: expected false when Providers is nil")
	}
}
```

- [ ] **Step 2: Run tests — verify they FAIL**

```
go test ./internal/llm -run TestNewClient -v
go test ./internal/config -run TestFindProvider -v
```

Expected: `FAIL` — `undefined: llm.NewClient` and `undefined: cfg.FindProvider` / `cfg.Providers`

- [ ] **Step 3: Create `internal/llm/factory.go`**

```go
package llm

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
)

// NewClient creates an LLM client based on cfg.Provider.
// "anthropic" → AnthropicClient. Any other value → OpenAIClient.
func NewClient(cfg config.LLMConfig) Client {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic":
		return NewAnthropicClient(cfg)
	default:
		return NewOpenAIClient(cfg)
	}
}
```

- [ ] **Step 4: Add `Providers` and `FindProvider` to `internal/config/config.go`**

In `ProjectConfig` struct (around line 308, after `LLM LLMConfig`), add:

```go
// Providers is an optional map of named LLM provider configurations.
// Use in agents: via provider: <name> or with --provider <name> CLI flag.
//
// Example:
//   providers:
//     anthropic:
//       provider: anthropic
//       api_key: "sk-ant-..."
//       model: claude-3-5-sonnet-20241022
//       max_tokens: 8192
Providers map[string]LLMConfig `yaml:"providers,omitempty"`
```

After the `FindAgent` method (around line 314), add:

```go
// FindProvider looks up a named provider from the providers: map.
// Returns (LLMConfig{}, false) when not found or when the map is nil.
func (c *ProjectConfig) FindProvider(name string) (LLMConfig, bool) {
	if c.Providers == nil {
		return LLMConfig{}, false
	}
	cfg, ok := c.Providers[name]
	return cfg, ok
}
```

- [ ] **Step 5: Run tests — verify they PASS**

```
go test ./internal/llm -run TestNewClient -v
go test ./internal/config -run TestFindProvider -v
```

Expected: all 4 + 3 = 7 tests PASS.

- [ ] **Step 6: Replace `NewOpenAIClient` with `NewClient` in `internal/core/core.go`**

Find the block at lines ~93-103:

```go
// BEFORE:
switch strings.ToLower(cfg.LLM.Provider) {
case "anthropic":
    llmClient = llm.NewAnthropicClient(cfg.LLM)
default:
    oc := llm.NewOpenAIClient(cfg.LLM)
    logger := llm.NewLogger(rootAbs)
    oc.SetLogger(logger)
    llmClient = oc
}

// AFTER:
llmClient = llm.NewClient(cfg.LLM)
if oc, ok := llmClient.(*llm.OpenAIClient); ok {
    oc.SetLogger(llm.NewLogger(rootAbs))
}
```

Also in `resolveCustomAgentOpts` (around line 562), change model-override client creation:

```go
// BEFORE:
if def.Model != "" {
    overrideCfg := c.cfg.LLM
    overrideCfg.Model = def.Model
    overrideClient := llm.NewOpenAIClient(overrideCfg)
    if agentLogger != nil {
        overrideClient.SetLogger(agentLogger)
    }
    result.llmClient = overrideClient
}

// AFTER:
if def.Model != "" {
    overrideCfg := c.cfg.LLM
    overrideCfg.Model = def.Model
    newClient := llm.NewClient(overrideCfg)
    if oc, ok := newClient.(*llm.OpenAIClient); ok && agentLogger != nil {
        oc.SetLogger(agentLogger)
    }
    result.llmClient = newClient
}
```

Remove the `strings` import from core.go if it was only used for the Provider switch (check before removing — `strings` may still be used elsewhere).

- [ ] **Step 7: Replace `NewOpenAIClient` with `NewClient` in `internal/cli/apply.go`**

**Direct mode** (line ~366):
```go
// BEFORE:
llmClient = llm.NewOpenAIClient(cfg.LLM)
if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
    logger := llm.NewLogger(cfg.ProjectRoot)
    openAIClient.SetLogger(logger)
}

// AFTER:
llmClient = llm.NewClient(cfg.LLM)
if oc, ok := llmClient.(*llm.OpenAIClient); ok {
    oc.SetLogger(llm.NewLogger(cfg.ProjectRoot))
}
```

**Pipeline mode** (line ~247):
```go
// BEFORE:
llmClient = llm.NewOpenAIClient(cfg.LLM)
if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
    logger := llm.NewLogger(cfg.ProjectRoot)
    openAIClient.SetLogger(logger)
}

// AFTER:
llmClient = llm.NewClient(cfg.LLM)
if oc, ok := llmClient.(*llm.OpenAIClient); ok {
    oc.SetLogger(llm.NewLogger(cfg.ProjectRoot))
}
```

**Custom agent model override in apply.go** (line ~434):
```go
// BEFORE:
overrideClient := llm.NewOpenAIClient(overrideCfg)
overrideClient.SetLogger(agentLogger)
llmClient = overrideClient

// AFTER:
newClient := llm.NewClient(overrideCfg)
if oc, ok := newClient.(*llm.OpenAIClient); ok {
    oc.SetLogger(agentLogger)
}
llmClient = newClient
```

- [ ] **Step 8: Run full test suite**

```
go test ./...
```

Expected: all pass. If `strings` import is unused in core.go, `go build ./...` will also catch it.

- [ ] **Step 9: Commit**

```
git add internal/llm/factory.go internal/llm/factory_test.go
git add internal/config/config.go internal/config/config_test.go
git add internal/core/core.go internal/cli/apply.go
git commit -m "feat(llm): add NewClient factory + config.Providers map; centralize provider switch"
```

---

## Task 2: `AgentDefinition.Provider` field + per-agent provider selection

**Files:**
- Modify: `internal/config/config.go` (add `Provider` to `AgentDefinition`)
- Modify: `internal/core/core.go` (update `resolveCustomAgentOpts` to honour `def.Provider`)
- Modify: `internal/cli/apply.go` (update custom agent block to honour `def.Provider`)
- Modify: `internal/config/config_test.go` (add `AgentDefinition.Provider` test)

**Context:** `resolveCustomAgentOpts` is in `core.go` around line 547. The custom agent block in apply.go is around line 428. After this task, a custom agent can set `provider: anthropic` in `.orchestra.yml` to use a different provider than the top-level `llm:`.

- [ ] **Step 1: Write failing test for AgentDefinition.Provider**

Append to `internal/config/config_test.go`:

```go
func TestFindAgent_WithProvider(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"anthropic": {Provider: "anthropic", APIKey: "key"},
		},
		Agents: []config.AgentDefinition{
			{Name: "reviewer", Provider: "anthropic", Model: "claude-3-5-sonnet-20241022"},
		},
	}
	agent := cfg.FindAgent("reviewer")
	if agent == nil {
		t.Fatal("FindAgent: expected to find 'reviewer'")
	}
	if agent.Provider != "anthropic" {
		t.Fatalf("expected Provider='anthropic', got %q", agent.Provider)
	}
	prov, ok := cfg.FindProvider(agent.Provider)
	if !ok {
		t.Fatal("FindProvider: expected to find provider referenced by agent")
	}
	if prov.APIKey != "key" {
		t.Fatalf("expected APIKey='key', got %q", prov.APIKey)
	}
}
```

- [ ] **Step 2: Run test — verify FAIL**

```
go test ./internal/config -run TestFindAgent_WithProvider -v
```

Expected: FAIL — `AgentDefinition` has no `Provider` field (compile error).

- [ ] **Step 3: Add `Provider` to `AgentDefinition` in `internal/config/config.go`**

Find `AgentDefinition` struct (around line 223). Add after `Model string`:

```go
// Provider references a named entry from the top-level providers: map.
// When set, overrides the top-level llm: credentials for this agent.
// Model: (if also set) further overrides the model within that provider.
Provider string `yaml:"provider,omitempty"`
```

The full struct after change:
```go
type AgentDefinition struct {
	Name         string   `yaml:"name"`
	SystemPrompt string   `yaml:"system_prompt,omitempty"`
	Tools        []string `yaml:"tools,omitempty"`
	Model        string   `yaml:"model,omitempty"`
	Provider     string   `yaml:"provider,omitempty"`
}
```

- [ ] **Step 4: Run test — verify PASS**

```
go test ./internal/config -run TestFindAgent_WithProvider -v
```

Expected: PASS.

- [ ] **Step 5: Update `resolveCustomAgentOpts` in `internal/core/core.go`**

Find the method around line 547. Replace the `if def.Model != ""` block with:

```go
// Provider override: look up named provider, then optionally apply Model.
if def.Provider != "" {
    if provCfg, ok := c.cfg.FindProvider(def.Provider); ok {
        if def.Model != "" {
            provCfg.Model = def.Model
        }
        newClient := llm.NewClient(provCfg)
        if oc, ok2 := newClient.(*llm.OpenAIClient); ok2 && agentLogger != nil {
            oc.SetLogger(agentLogger)
        }
        result.llmClient = newClient
    } else {
        fmt.Fprintf(os.Stderr, "orchestra: agent %q: provider %q not found in providers:, using default\n",
            def.Name, def.Provider)
    }
} else if def.Model != "" {
    overrideCfg := c.cfg.LLM
    overrideCfg.Model = def.Model
    newClient := llm.NewClient(overrideCfg)
    if oc, ok := newClient.(*llm.OpenAIClient); ok && agentLogger != nil {
        oc.SetLogger(agentLogger)
    }
    result.llmClient = newClient
}
```

- [ ] **Step 6: Update custom agent block in `internal/cli/apply.go`**

Find the block starting at line ~428 (`if agentMode != "" {`). Replace the `def.Model != ""` sub-block with:

```go
// Provider override: named provider from providers: map.
if def.Provider != "" && testLLMClient == nil {
    if provCfg, ok := cfg.FindProvider(def.Provider); ok {
        if def.Model != "" {
            provCfg.Model = def.Model
        }
        newClient := llm.NewClient(provCfg)
        if oc, ok2 := newClient.(*llm.OpenAIClient); ok2 {
            oc.SetLogger(agentLogger)
        }
        llmClient = newClient
    } else {
        fmt.Fprintf(os.Stderr, "orchestra: agent %q: provider %q not found in providers:, using default\n",
            agentMode, def.Provider)
    }
} else if def.Model != "" && testLLMClient == nil {
    overrideCfg := cfg.LLM
    overrideCfg.Model = def.Model
    newClient := llm.NewClient(overrideCfg)
    if oc, ok := newClient.(*llm.OpenAIClient); ok {
        oc.SetLogger(agentLogger)
    }
    llmClient = newClient
}
```

- [ ] **Step 7: Run full test suite**

```
go test ./...
```

Expected: all pass.

- [ ] **Step 8: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git add internal/core/core.go internal/cli/apply.go
git commit -m "feat(config): add AgentDefinition.Provider for per-agent provider selection"
```

---

## Task 3: `--provider <name>` CLI flag on `orchestra apply`

**Files:**
- Modify: `internal/cli/apply.go` (add flag, validate, override `cfg.LLM`)
- Modify: `internal/config/config_test.go` (add test for flag validation logic via FindProvider)

**Context:** `--provider <name>` overrides the provider for the entire `orchestra apply` run (direct mode). It replaces `cfg.LLM` with the named provider's config before any client is created. The override happens immediately after config loading, so it affects direct mode, pipeline mode, and custom agent override identically.

- [ ] **Step 1: Write test for flag validation logic**

Append to `internal/config/config_test.go`:

```go
func TestFindProvider_EmptyName(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"anthropic": {Provider: "anthropic"},
		},
	}
	_, ok := cfg.FindProvider("")
	if ok {
		t.Fatal("FindProvider: empty name should not match any provider")
	}
}
```

- [ ] **Step 2: Run test — verify PASS** (FindProvider already handles this; this is a regression check)

```
go test ./internal/config -run TestFindProvider_EmptyName -v
```

Expected: PASS immediately (empty string key won't be in the map unless explicitly added).

- [ ] **Step 3: Add `--provider` flag variable and registration in `internal/cli/apply.go`**

Find where other flag variables are declared at package level (around line 30-50). Add:

```go
var applyProvider string
```

In `init()` (after the existing `applyCmd.Flags()` calls), add:

```go
applyCmd.Flags().StringVar(&applyProvider, "provider", "", "Use a named provider from .orchestra.yml providers: section")
```

- [ ] **Step 4: Apply provider override in `runApply` after config loading**

In `runApply`, after the config load block (line ~98, after `cfg, err := config.Load(...)` and its error check), add:

```go
// --provider flag: override cfg.LLM with a named provider from providers: map.
if applyProvider != "" {
    provCfg, ok := cfg.FindProvider(applyProvider)
    if !ok {
        return fmt.Errorf("provider %q not found in .orchestra.yml providers: section\n"+
            "Available providers: %s",
            applyProvider, providerNames(cfg))
    }
    cfg.LLM = provCfg
}
```

Add the helper function at the bottom of `apply.go` (after `runApply`):

```go
// providerNames returns a comma-separated list of configured provider names,
// or "(none configured)" when the providers: map is empty.
func providerNames(cfg *config.ProjectConfig) string {
	if len(cfg.Providers) == 0 {
		return "(none configured)"
	}
	names := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
```

You will need to add `"sort"` to the imports in `apply.go`.

- [ ] **Step 5: Run full test suite**

```
go build ./...
go test ./...
```

Expected: all pass. Build must succeed (catches missing `sort` import).

- [ ] **Step 6: Manual smoke test** (optional but recommended)

```
# In a project with .orchestra.yml — verify flag shows in help
orchestra apply --help | grep provider
```

Expected output includes: `--provider string   Use a named provider from .orchestra.yml providers: section`

- [ ] **Step 7: Commit**

```
git add internal/cli/apply.go internal/config/config_test.go
git commit -m "feat(cli): add --provider flag to orchestra apply for named provider selection"
```

---

## Config example after all 3 tasks

```yaml
llm:
  api_base: http://10.5.0.2:1234/v1
  model: qwen3.6-27b
  api_key: ""
  response_format_type: "json_object"

providers:
  anthropic:
    provider: anthropic
    api_key: "sk-ant-..."
    model: claude-3-5-sonnet-20241022
    max_tokens: 8192
  openai-fast:
    provider: openai
    api_base: https://api.openai.com/v1
    api_key: "sk-..."
    model: gpt-4o-mini

agents:
  - name: reviewer
    provider: anthropic          # uses the anthropic provider credentials
    system_prompt: "You are a thorough code reviewer. Focus on correctness and security."

  - name: quick-check
    provider: openai-fast        # uses gpt-4o-mini for fast checks
    model: gpt-4o                # further overrides the model within the provider
```

Usage:
```bash
orchestra apply "refactor auth module"                    # uses top-level llm: (qwen3.6-27b)
orchestra apply --provider anthropic "review this PR"     # overrides with anthropic provider
orchestra apply --mode reviewer "check security"          # uses the reviewer agent (anthropic)
```
