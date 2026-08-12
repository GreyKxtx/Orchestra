package config

import (
	"os"
	"path/filepath"
	"testing"
)

const routingFixture = `
version: 1

roles:
  L5:
    label: "Orchestrator"
    provider: anthropic
    model: claude-opus
  L4:
    label: "Department Lead"
    provider: anthropic
    model: claude-sonnet
  L4_product:
    label: "Product Lead"
    provider: anthropic
    model: claude-sonnet-product
    subagent_type: product
  L3:
    label: "Focused worker"
    provider: lmstudio
    model: qwen-coder-32b
  L2:
    label: "Context explorer"
    provider: google
    model: gemini-flash
  L1:
    label: "Micro fixer"
    provider: lmstudio
    model: qwen-coder-7b

legacy_map:
  planner: L5
  complex: L3
  focused: L3
  micro: L1
  explore: L2

routing:
  explore_codebase:    { required_tier: L2, subagent_type: explore }
  write_function:      { required_tier: L3, subagent_type: worker, tier: focused }
  multi_file_refactor: { required_tier: L3, subagent_type: worker, tier: complex }
  architecture_review: { required_tier: L4, subagent_type: architecture }
`

func writeRoutingFile(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, RoutingFileName), []byte(contents), 0o600); err != nil {
		t.Fatalf("write routing file: %v", err)
	}
}

func loadRoutingFixture(t *testing.T) *OrchestraRouting {
	t.Helper()
	dir := t.TempDir()
	writeRoutingFile(t, dir, routingFixture)
	r, err := LoadOrchestraRouting(dir)
	if err != nil {
		t.Fatalf("LoadOrchestraRouting: %v", err)
	}
	if r == nil {
		t.Fatal("expected routing, got nil")
	}
	return r
}

func TestLoadOrchestraRoutingMissingFile(t *testing.T) {
	r, err := LoadOrchestraRouting(t.TempDir())
	if err != nil {
		t.Fatalf("missing file must not error, got %v", err)
	}
	if r != nil {
		t.Fatalf("missing file must yield nil, got %+v", r)
	}
}

func TestLoadOrchestraRoutingParse(t *testing.T) {
	r := loadRoutingFixture(t)
	if r.Version != 1 {
		t.Fatalf("version = %d", r.Version)
	}
	if len(r.Roles) != 6 {
		t.Fatalf("roles = %d", len(r.Roles))
	}
	if len(r.Routing) != 4 {
		t.Fatalf("routing rules = %d", len(r.Routing))
	}
}

func TestRoutingValidate(t *testing.T) {
	providers := map[string]LLMConfig{"anthropic": {}, "lmstudio": {}, "google": {}}
	r := loadRoutingFixture(t)
	if err := r.Validate(providers); err != nil {
		t.Fatalf("valid fixture must pass: %v", err)
	}

	bad := &OrchestraRouting{Roles: map[string]RoutingRole{"opus": {}}}
	if err := bad.Validate(nil); err == nil {
		t.Fatal("role key 'opus' must fail validation")
	}

	bad = &OrchestraRouting{Roles: map[string]RoutingRole{"L3": {Provider: "nope"}}}
	if err := bad.Validate(providers); err == nil {
		t.Fatal("unknown provider must fail validation")
	}

	bad = &OrchestraRouting{LegacyMap: map[string]string{"focused": "opus"}}
	if err := bad.Validate(nil); err == nil {
		t.Fatal("legacy_map target 'opus' must fail validation")
	}

	bad = &OrchestraRouting{Routing: map[string]RoutingRule{"write_function": {RequiredTier: "big"}}}
	if err := bad.Validate(nil); err == nil {
		t.Fatal("required_tier 'big' must fail validation")
	}
}

func TestRoutingMapLegacy(t *testing.T) {
	r := loadRoutingFixture(t)
	cases := map[string]string{
		"planner": "L5",
		"complex": "L3",
		"focused": "L3",
		"micro":   "L1",
		"explore": "L2",
		"L4":      "L4", // L-form passes through
		"unknown": "",
	}
	for in, want := range cases {
		if got := r.MapLegacy(in); got != want {
			t.Errorf("MapLegacy(%q) = %q, want %q", in, got, want)
		}
	}
	// Built-in defaults apply when the file omits legacy_map.
	var empty *OrchestraRouting
	empty = &OrchestraRouting{}
	if got := empty.MapLegacy("planner"); got != "L5" {
		t.Errorf("default MapLegacy(planner) = %q, want L5", got)
	}
}

func TestRoutingResolveRole(t *testing.T) {
	r := loadRoutingFixture(t)

	role, ok := r.ResolveRole("L4_product")
	if !ok || role.Model != "claude-sonnet-product" {
		t.Fatalf("L4_product = %+v ok=%v", role, ok)
	}
	// Undefined specialization falls back to base tier.
	role, ok = r.ResolveRole("L4_docs")
	if !ok || role.Model != "claude-sonnet" {
		t.Fatalf("L4_docs fallback = %+v ok=%v", role, ok)
	}
	if _, ok := r.ResolveRole("L9"); ok {
		t.Fatal("L9 must not resolve")
	}
	// Case-insensitive lookup.
	if _, ok := r.ResolveRole("l3"); !ok {
		t.Fatal("l3 must resolve case-insensitively")
	}
	var nilR *OrchestraRouting
	if _, ok := nilR.ResolveRole("L3"); ok {
		t.Fatal("nil routing must not resolve")
	}
}

func TestRoutingRoute(t *testing.T) {
	r := loadRoutingFixture(t)
	rule, ok := r.Route("write_function")
	if !ok || rule.RequiredTier != "L3" || rule.SubagentType != "worker" || rule.Tier != "focused" {
		t.Fatalf("write_function = %+v ok=%v", rule, ok)
	}
	if _, ok := r.Route("no_such_type"); ok {
		t.Fatal("unknown task_type must not route")
	}
	var nilR *OrchestraRouting
	if _, ok := nilR.Route("write_function"); ok {
		t.Fatal("nil routing must not route")
	}
}

func TestResolveTierBindingPrecedence(t *testing.T) {
	cfg := &ProjectConfig{}
	cfg.Orchestra.Tiers = []OrchestraTier{
		{Name: "focused", Provider: "legacy-prov", Model: "legacy-model"},
	}
	cfg.Routing = loadRoutingFixture(t)

	// Legacy band defined in orchestra.tiers wins (backward compatibility).
	p, m, ok := cfg.ResolveTierBinding("focused")
	if !ok || p != "legacy-prov" || m != "legacy-model" {
		t.Fatalf("focused = %s/%s ok=%v", p, m, ok)
	}
	// Legacy band absent from orchestra.tiers falls through to routing roles.
	p, m, ok = cfg.ResolveTierBinding("micro")
	if !ok || p != "lmstudio" || m != "qwen-coder-7b" {
		t.Fatalf("micro = %s/%s ok=%v", p, m, ok)
	}
	// L-names resolve directly from routing roles.
	p, m, ok = cfg.ResolveTierBinding("L2")
	if !ok || p != "google" || m != "gemini-flash" {
		t.Fatalf("L2 = %s/%s ok=%v", p, m, ok)
	}
	// Empty name → default tier (focused) → legacy binding.
	p, m, ok = cfg.ResolveTierBinding("")
	if !ok || p != "legacy-prov" {
		t.Fatalf("default = %s/%s ok=%v", p, m, ok)
	}
	// No routing file and no legacy match → not resolved.
	bare := &ProjectConfig{}
	if _, _, ok := bare.ResolveTierBinding("L3"); ok {
		t.Fatal("bare config must not resolve L3")
	}
}

func TestResolveTierBindingEmptyDefaultViaRouting(t *testing.T) {
	// No orchestra.tiers at all: "" resolves default band through legacy_map.
	cfg := &ProjectConfig{}
	cfg.Routing = loadRoutingFixture(t)
	p, m, ok := cfg.ResolveTierBinding("")
	if !ok || p != "lmstudio" || m != "qwen-coder-32b" {
		t.Fatalf("default via routing = %s/%s ok=%v", p, m, ok)
	}
}

func TestLoadConfigPicksUpRoutingFile(t *testing.T) {
	dir := t.TempDir()
	cfgYAML := `
project_root: .
llm:
  provider: openai
  api_base: http://localhost:1234/v1
  model: test
providers:
  anthropic: {}
  lmstudio: {}
  google: {}
`
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	writeRoutingFile(t, dir, routingFixture)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Routing == nil {
		t.Fatal("cfg.Routing must be loaded from orchestra_routing.yaml")
	}
	if _, ok := cfg.Routing.Route("write_function"); !ok {
		t.Fatal("routing rules must be available after Load")
	}

	// Invalid routing file must fail Load fail-closed.
	writeRoutingFile(t, dir, "roles:\n  opus: {}\n")
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("invalid routing file must fail Load")
	}
}
