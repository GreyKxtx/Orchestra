package tools

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/llm"
)

// stepZeroBytes is what the model is charged before it has read a single file:
// the system prompt plus every tool schema.
func stepZeroBytes(t *testing.T, mode, family string, caps Capabilities) (sys, schema int) {
	t.Helper()
	sys = len(prompt.BuildSystemPromptForMode(mode, family))
	for _, d := range ListToolsForMode(mode, caps, false, false) {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal tool %q: %v", d.Function.Name, err)
		}
		schema += len(b)
	}
	return sys, schema
}

// A local model's whole working context is 20-25k tokens. At the ~3.7 bytes
// per token LM Studio actually charges, 20 KB of step-zero prompt is already
// a quarter of that window before the first file is read — and an evaluation
// run against a 20k model had every coding task refused at step zero.
//
// This is a budget, not a measurement: it fails when the prompt grows, so the
// growth is a decision someone makes rather than something that accumulates.
func TestStepZeroFitsASmallLocalWindow(t *testing.T) {
	const budget = 20_000 // bytes ≈ 5.4k tokens at 3.7 B/tok

	// The modes a local model is actually driven in from the chat UI.
	for _, mode := range []string{"build", "general", "ask", "plan", "debug"} {
		sys, schema := stepZeroBytes(t, mode, "local", Capabilities{})
		if total := sys + schema; total > budget {
			t.Errorf("mode %q step zero is %d bytes (system %d + schemas %d), over the %d budget for a small local window",
				mode, total, sys, schema, budget)
		}
	}
}

// toolsWithoutAMode are implemented and registered but handed to no mode.
// Each needs a reason, because the alternative is a tool nobody can call.
var toolsWithoutAMode = map[string]string{
	// Added by core.extraToolDefs / cli.namedLLMClient only when an embedding
	// model is configured; the index is useless without one.
	"semantic_search": "gated on llm.embed.model",
}

// Every tool in the registry should reach some mode, and every tool a mode
// offers must resolve by name (custom agents name tools in config).
func TestEveryRegisteredToolIsReachable(t *testing.T) {
	registry := allToolDefsMap()
	caps := Capabilities{Exec: true, Web: true, Browser: true}

	offered := map[string]bool{}
	add := func(defs []llm.ToolDef) {
		for _, d := range defs {
			offered[d.Function.Name] = true
		}
	}
	for _, mode := range promptModes {
		add(ListToolsForMode(mode, caps, true, true))
	}
	// agent_prompt.go reaches these two directly, not through a mode.
	add(ListTools(caps))
	add(ListToolsWithSubtasks(caps))
	add(ListToolsForChild())
	add(ListToolsForInvestigator())

	var unreachable []string
	for name := range registry {
		if !offered[name] {
			if _, known := toolsWithoutAMode[name]; !known {
				unreachable = append(unreachable, name)
			}
		}
	}
	sort.Strings(unreachable)
	for _, name := range unreachable {
		t.Errorf("tool %q is registered but no mode ever offers it — add it to a mode list or document why in toolsWithoutAMode", name)
	}

	// The inverse: a mode offering a tool that ResolveToolNames cannot find
	// means `agents:` config naming that tool fails with "unknown tool".
	for name := range offered {
		if _, ok := registry[name]; !ok {
			t.Errorf("tool %q is offered to a mode but missing from allToolDefsMap — it cannot be named in agents: config", name)
		}
	}

	// And the exception list must not outlive its exceptions.
	for name := range toolsWithoutAMode {
		if _, ok := registry[name]; !ok {
			t.Errorf("toolsWithoutAMode lists %q, which is not a registered tool", name)
		}
		if offered[name] {
			t.Errorf("toolsWithoutAMode still lists %q, but a mode now offers it — drop the exception", name)
		}
	}
}

// configOnlyToolNames are names config accepts that allToolDefsMap has no
// definition for — each one is a config file that loads and then fails at
// runtime, so the list should stay empty.
var configOnlyToolNames = map[string]string{}

// leadOnlyToolNames are deliberately not nameable in agents: config — their
// handlers refuse every caller but the Orchestra Lead, so accepting them in a
// custom agent would only produce a runtime error.
var leadOnlyToolNames = map[string]bool{
	"contract_freeze":      true,
	"update_working_state": true,
}

// Two hand-maintained name lists decide whether `agents:` config works:
// config.validAgentToolNames validates it at load, tools.allToolDefsMap
// resolves it at run. When they drift, a config either loads and then dies
// with "unknown tool", or is rejected for naming a tool that exists.
func TestConfigToolNamesMatchTheRegistry(t *testing.T) {
	registry := allToolDefsMap()

	valid := map[string]bool{}
	for _, name := range config.ValidAgentToolNames() {
		valid[name] = true
	}

	for name := range registry {
		if valid[name] || leadOnlyToolNames[name] {
			continue
		}
		t.Errorf("tool %q exists but config rejects it in agents: — add it to config.validAgentToolNames", name)
	}
	for name := range valid {
		if _, ok := registry[name]; ok {
			continue
		}
		if _, known := configOnlyToolNames[name]; !known {
			t.Errorf("config accepts %q in agents: but allToolDefsMap cannot resolve it — the run will fail with \"unknown tool\"", name)
		}
	}
}
