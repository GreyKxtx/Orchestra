package config

import "testing"

func TestOrchestraBarrierAndStateDefaults(t *testing.T) {
	var o OrchestraConfig
	if o.ResolvedMaxClarificationRounds() != 2 {
		t.Fatal("max_clarification_rounds default must be 2 (ADR-5)")
	}
	if o.ResolvedRelayViaLLM() {
		t.Fatal("relay_via_llm default must be false — the runtime barrier is the default")
	}
	if o.ResolvedStateMaxBytes() != 16*1024 {
		t.Fatal("state_max_bytes default must be 16384")
	}
	rv := true
	o = OrchestraConfig{MaxClarificationRounds: 5, RelayViaLLM: &rv, StateMaxBytes: 1024}
	if o.ResolvedMaxClarificationRounds() != 5 || !o.ResolvedRelayViaLLM() || o.ResolvedStateMaxBytes() != 1024 {
		t.Fatal("explicit values must win")
	}
}

func TestPhaseTimeoutsDefaults(t *testing.T) {
	var p PhaseTimeoutsConfig
	if p.ResolvedDiscoveryS() != 900 || p.ResolvedContractS() != 900 ||
		p.ResolvedLeadBriefS() != 600 || p.ResolvedBlockedEscalateS() != 300 {
		t.Fatal("phase_timeouts defaults must be 900/900/600/300 (spec §4.5)")
	}
	p = PhaseTimeoutsConfig{DiscoveryS: 60, ContractS: -1, LeadBriefS: 120, BlockedEscalateS: -1}
	if p.ResolvedDiscoveryS() != 60 || p.ResolvedLeadBriefS() != 120 {
		t.Fatal("explicit values must win")
	}
	if p.ResolvedContractS() != 0 || p.ResolvedBlockedEscalateS() != 0 {
		t.Fatal("negative values must disable the timeout")
	}
}

func TestGateRequiredAndRequiredGates(t *testing.T) {
	var empty OrchestraConfig
	if empty.GateRequired(GateGitPush) {
		t.Fatal("no gates configured → nothing required")
	}
	if empty.RequiredGates() != nil {
		t.Fatal("no gates configured → nil map")
	}

	o := OrchestraConfig{Gates: map[string]string{
		GateGitPush:   "required",
		GateGitCommit: "off",
	}}
	if !o.GateRequired(GateGitPush) {
		t.Fatal("git_push must be required")
	}
	if o.GateRequired(GateGitCommit) {
		t.Fatal("git_commit is off")
	}
	req := o.RequiredGates()
	if len(req) != 1 || !req[GateGitPush] {
		t.Fatalf("RequiredGates = %v, want only git_push", req)
	}
}

func TestTierEscalationConfigDefaults(t *testing.T) {
	var te TierEscalationConfig
	if te.ResolvedEnabled() {
		t.Fatal("escalation must be off by default (spends the expensive model)")
	}
	if te.ResolvedFailuresBeforeEscalation() != 2 || te.ResolvedMaxEscalatedRetries() != 1 || te.ResolvedEscalationTier() != "complex" {
		t.Fatalf("unexpected defaults: %d %d %q",
			te.ResolvedFailuresBeforeEscalation(), te.ResolvedMaxEscalatedRetries(), te.ResolvedEscalationTier())
	}
	on := true
	te = TierEscalationConfig{Enabled: &on, WorkerFailuresBeforeL4: 3, MaxL4Retries: 2, EscalationTier: "L4"}
	if !te.ResolvedEnabled() || te.ResolvedFailuresBeforeEscalation() != 3 || te.ResolvedMaxEscalatedRetries() != 2 || te.ResolvedEscalationTier() != "L4" {
		t.Fatalf("explicit values not honored: %+v", te)
	}
}

func TestValidateOrchestraGates(t *testing.T) {
	base := func(gates map[string]string) *ProjectConfig {
		c := &ProjectConfig{}
		c.Orchestra.Gates = gates
		return c
	}
	if err := base(map[string]string{GateGitPush: "required"}).validateOrchestra(); err != nil {
		t.Fatalf("valid gates rejected: %v", err)
	}
	if err := base(map[string]string{"deploy": "required"}).validateOrchestra(); err == nil {
		t.Fatal("unknown gate key must be rejected")
	}
	if err := base(map[string]string{GateGitCommit: "maybe"}).validateOrchestra(); err == nil {
		t.Fatal("invalid gate value must be rejected")
	}
}
