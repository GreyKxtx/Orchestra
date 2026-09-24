package config

import (
	"strings"
	"testing"
)

func TestParseAgencyFlows(t *testing.T) {
	flows, err := ParseAgencyFlows([]string{"frontend > backend > qa", "* > explore", " Product>scout "})
	if err != nil {
		t.Fatal(err)
	}
	want := []AgencyFlow{
		{From: "frontend", To: "backend"},
		{From: "backend", To: "qa"},
		{From: "*", To: "explore"},
		{From: "product", To: "scout"},
	}
	if len(flows) != len(want) {
		t.Fatalf("flows = %+v", flows)
	}
	for i := range want {
		if flows[i] != want[i] {
			t.Fatalf("flows[%d] = %+v, want %+v", i, flows[i], want[i])
		}
	}

	for line, wantErr := range map[string]string{
		"backend":                "expected",
		"backend > ":             "empty agent name",
		"backend > *":            `"*" is only allowed on the left`,
		"backend > orchestrator": "root",
		"qa > qa":                "itself",
		"qa > Bad Name":          "invalid agent name",
	} {
		if _, err := ParseAgencyFlows([]string{line}); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: err = %v, want it to mention %q", line, err, wantErr)
		}
	}
}

func TestValidateAgency(t *testing.T) {
	cfg := &ProjectConfig{
		Agents: []AgentDefinition{{Name: "billing-lead", Base: "architecture"}},
		Agency: AgencyConfig{Flows: []string{"billing-lead > worker", "frontend@web > backend", "orchestrator > billing-lead"}},
	}
	if err := cfg.validateAgency(); err != nil {
		t.Fatalf("valid flows refused: %v", err)
	}
	cfg.Agency.Flows = []string{"backend > build"}
	if err := cfg.validateAgency(); err == nil || !strings.Contains(err.Error(), "top-level mode") {
		t.Fatalf("a flow into a top-level mode must be refused, got %v", err)
	}
	cfg.Agency.Flows = nil
	cfg.Agency.MaxDepth = 9
	if err := cfg.validateAgency(); err == nil {
		t.Fatal("max_depth beyond 4 must be refused")
	}
}

func TestValidateAgents_Base(t *testing.T) {
	cfg := &ProjectConfig{Agents: []AgentDefinition{{Name: "x-lead", Base: "orchestra"}}}
	if err := cfg.validateAgents(); err == nil || !strings.Contains(err.Error(), "base") {
		t.Fatalf("base must be a spawnable role, got %v", err)
	}
	cfg.Agents[0].Base = "Architecture"
	if err := cfg.validateAgents(); err != nil {
		t.Fatalf("base is case-insensitive: %v", err)
	}
	if got := cfg.Agents[0].ResolvedBase(); got != "architecture" {
		t.Fatalf("ResolvedBase = %q", got)
	}
	if got := (AgentDefinition{}).ResolvedBase(); got != "general" {
		t.Fatalf("default base = %q, want general", got)
	}
	cfg.Agents = []AgentDefinition{{Name: "scout"}}
	if err := cfg.validateAgents(); err == nil {
		t.Fatal("scout is a built-in role now; a custom agent must not shadow it")
	}
}

func TestAgencyDefaults(t *testing.T) {
	var a AgencyConfig
	if !a.ResolvedEnabled("orchestra") || a.ResolvedEnabled("build") {
		t.Fatal("unset: on for orchestra, off elsewhere")
	}
	if a.ResolvedMaxDepth() != 2 || a.ResolvedMaxParallel() != 4 || a.ResolvedMaxMessages() != 64 {
		t.Fatalf("defaults: depth %d parallel %d messages %d", a.ResolvedMaxDepth(), a.ResolvedMaxParallel(), a.ResolvedMaxMessages())
	}
	if !a.ResolvedThreads() || !a.ResolvedRelayWorkOrders() || !a.ResolvedDefaultFlows() {
		t.Fatal("threads, relay and default flows are on by default")
	}
	off := false
	a = AgencyConfig{Enabled: &off, Flows: []string{"a > b"}, MaxParallel: -1, MaxMessages: -1}
	if a.ResolvedEnabled("orchestra") {
		t.Fatal("enabled: false wins over mode and flows")
	}
	if a.ResolvedMaxParallel() != 0 || a.ResolvedMaxMessages() != 0 {
		t.Fatal("negative means unlimited (0)")
	}
	a = AgencyConfig{Flows: []string{"a > b"}}
	if !a.ResolvedEnabled("build") {
		t.Fatal("flows turn the agency on in any mode")
	}
}
