package config

import "strings"

// AutoRouterConfig classifies user queries into build|plan|explore when mode=agent.
type AutoRouterConfig struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`  // default true when mode=agent
	Provider string `yaml:"provider,omitempty"` // named providers: entry; empty → fast_provider or main
	Model    string `yaml:"model,omitempty"`
}

// ResolvedEnabled reports whether auto-router LLM classification is enabled (default true).
func (a AutoRouterConfig) ResolvedEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// OrchestraRole is provider/model for Lead or a worker tier.
type OrchestraRole struct {
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

// OrchestraTier is one worker difficulty band (Claude-like opus/sonnet/haiku).
type OrchestraTier struct {
	Name     string   `yaml:"name"` // complex | focused | micro
	Provider string   `yaml:"provider,omitempty"`
	Model    string   `yaml:"model,omitempty"`
	Models   []string `yaml:"models,omitempty"` // optional pool from same provider (Model is primary)
}

// OrchestraConfig configures Lead + worker tiers for mode=orchestra.
type OrchestraConfig struct {
	Planner          OrchestraRole   `yaml:"planner,omitempty"`
	Tiers            []OrchestraTier `yaml:"tiers,omitempty"`
	DefaultTier      string          `yaml:"default_tier,omitempty"` // default focused
	// PhaseEnforcement controls the runtime phase guard: strict (default)
	// blocks worker spawn outside execution/maintenance when
	// .orchestra/state.md exists; prompt_only disables the runtime gate.
	PhaseEnforcement string `yaml:"phase_enforcement,omitempty"`
	// Gates configures human gates (spec §4.4): key → required | off.
	// Known keys: git_commit (G2), git_push (G3). Unset map = no gates
	// (backward compatible); unknown keys are rejected at Load.
	Gates            map[string]string `yaml:"gates,omitempty"`
	// TierEscalation re-runs a WorkOrder on a senior tier after repeated
	// verification failures (spec §5.5). Disabled unless enabled: true —
	// escalation spends the expensive model without explicit opt-in.
	TierEscalation   TierEscalationConfig `yaml:"tier_escalation,omitempty"`
	MaxWorkerRetries int             `yaml:"max_worker_retries,omitempty"`
	WorkerVerifyEnabled *bool         `yaml:"worker_verify_enabled,omitempty"`
	MaxWorkerVerifyRetries int       `yaml:"max_worker_verify_retries,omitempty"`
	WorkerLLMVerifyEnabled *bool      `yaml:"worker_llm_verify_enabled,omitempty"`
	// WorkerVerifyAffectedTests gates `go test` on worker-edited packages (default true).
	WorkerVerifyAffectedTests *bool `yaml:"worker_verify_affected_tests,omitempty"`
	// WorkerVerifyFrontendTypecheck gates `tsc --noEmit` after frontend edits (default true).
	WorkerVerifyFrontendTypecheck *bool `yaml:"worker_verify_frontend_typecheck,omitempty"`
	// MaxClarificationRounds caps Question Barrier user round-trips per
	// phase (spec §4.3, ADR-5; default 2). Beyond the budget the runtime
	// instructs Leads to proceed on recorded assumptions.
	MaxClarificationRounds int `yaml:"max_clarification_rounds,omitempty"`
	// RelayViaLLM disables the runtime Question Barrier so open_questions[]
	// stay in the task_result for the orchestrator (legacy/debug; default false).
	RelayViaLLM *bool `yaml:"relay_via_llm,omitempty"`
	// StateMaxBytes is the .orchestra/state.md size budget before the
	// runtime archives the older head to .orchestra/archive/ (default 16384).
	StateMaxBytes int `yaml:"state_max_bytes,omitempty"`
	// PhaseTimeouts are the deadlock breakers of spec §4.5 (checklist 28):
	// stale phases surface a warning, repeated blocked results escalate to
	// the user, Lead brief work gets a wall-clock cap.
	PhaseTimeouts PhaseTimeoutsConfig `yaml:"phase_timeouts,omitempty"`
}

// PhaseTimeoutsConfig is orchestra.phase_timeouts (spec §4.5). Zero values
// take the documented defaults; negative values disable the given timeout.
type PhaseTimeoutsConfig struct {
	DiscoveryS       int `yaml:"discovery_s,omitempty"`        // default 900
	ContractS        int `yaml:"contract_s,omitempty"`         // default 900
	LeadBriefS       int `yaml:"lead_brief_s,omitempty"`       // default 600
	BlockedEscalateS int `yaml:"blocked_escalate_s,omitempty"` // default 300
}

func resolveTimeout(v, def int) int {
	if v == 0 {
		return def
	}
	if v < 0 {
		return 0 // explicitly disabled
	}
	return v
}

// ResolvedDiscoveryS returns the discovery phase budget in seconds (default 900).
func (p PhaseTimeoutsConfig) ResolvedDiscoveryS() int { return resolveTimeout(p.DiscoveryS, 900) }

// ResolvedContractS returns the contract phase budget in seconds (default 900).
func (p PhaseTimeoutsConfig) ResolvedContractS() int { return resolveTimeout(p.ContractS, 900) }

// ResolvedLeadBriefS returns the Lead brief wall-clock cap in seconds (default 600).
func (p PhaseTimeoutsConfig) ResolvedLeadBriefS() int { return resolveTimeout(p.LeadBriefS, 600) }

// ResolvedBlockedEscalateS returns the blocked-escalation threshold in seconds (default 300).
func (p PhaseTimeoutsConfig) ResolvedBlockedEscalateS() int {
	return resolveTimeout(p.BlockedEscalateS, 300)
}

// ResolvedMaxClarificationRounds returns the Question Barrier budget (default 2).
func (o OrchestraConfig) ResolvedMaxClarificationRounds() int {
	if o.MaxClarificationRounds <= 0 {
		return 2
	}
	return o.MaxClarificationRounds
}

// ResolvedRelayViaLLM reports whether the runtime barrier is bypassed (default false).
func (o OrchestraConfig) ResolvedRelayViaLLM() bool {
	return o.RelayViaLLM != nil && *o.RelayViaLLM
}

// ResolvedStateMaxBytes returns the state.md size budget (default 16384).
func (o OrchestraConfig) ResolvedStateMaxBytes() int {
	if o.StateMaxBytes <= 0 {
		return 16 * 1024
	}
	return o.StateMaxBytes
}

// ResolvedDefaultTier returns the default worker tier name.
func (o OrchestraConfig) ResolvedDefaultTier() string {
	if strings.TrimSpace(o.DefaultTier) != "" {
		return strings.TrimSpace(o.DefaultTier)
	}
	return "focused"
}

// ResolvedPhaseEnforcement returns the phase guard mode (default strict).
func (o OrchestraConfig) ResolvedPhaseEnforcement() string {
	v := strings.ToLower(strings.TrimSpace(o.PhaseEnforcement))
	if v == "" {
		return "strict"
	}
	return v
}

// TierEscalationConfig is orchestra.tier_escalation (spec §5.5):
// attempts 1..worker_failures_before_l4 run on the assigned tier with
// verification hints; the next attempt re-runs the same WorkOrder on
// escalation_tier; when that fails too, the result stays blocked for replan.
type TierEscalationConfig struct {
	Enabled                *bool  `yaml:"enabled,omitempty"`
	WorkerFailuresBeforeL4 int    `yaml:"worker_failures_before_l4,omitempty"` // default 2
	MaxL4Retries           int    `yaml:"max_l4_retries,omitempty"`            // default 1
	EscalationTier         string `yaml:"escalation_tier,omitempty"`           // default "complex" (→ L4 via legacy_map)
}

// ResolvedEnabled reports whether tier escalation is active (default false).
func (t TierEscalationConfig) ResolvedEnabled() bool {
	return t.Enabled != nil && *t.Enabled
}

// ResolvedFailuresBeforeEscalation returns base-tier attempts before escalation (default 2).
func (t TierEscalationConfig) ResolvedFailuresBeforeEscalation() int {
	if t.WorkerFailuresBeforeL4 <= 0 {
		return 2
	}
	return t.WorkerFailuresBeforeL4
}

// ResolvedMaxEscalatedRetries returns attempts on the escalated tier (default 1).
func (t TierEscalationConfig) ResolvedMaxEscalatedRetries() int {
	if t.MaxL4Retries <= 0 {
		return 1
	}
	return t.MaxL4Retries
}

// ResolvedEscalationTier returns the tier name used for escalated attempts.
func (t TierEscalationConfig) ResolvedEscalationTier() string {
	if v := strings.TrimSpace(t.EscalationTier); v != "" {
		return v
	}
	return "complex"
}

// Human gate keys (spec §4.4). G1 (PRD) is enforced by the phase guard;
// G4 (deploy) rides on the exec consent gate (--allow-exec).
const (
	GateGitCommit      = "git_commit"      // G2
	GateGitPush        = "git_push"        // G3
	GateContractFreeze = "contract_freeze" // G6 — approve the stage-2.5 freeze
)

// GateRequired reports whether the named human gate is set to "required".
// Absent key or absent map = gate off (backward compatible).
func (o OrchestraConfig) GateRequired(name string) bool {
	v, ok := o.Gates[name]
	return ok && strings.EqualFold(strings.TrimSpace(v), "required")
}

// RequiredGates returns the set of gates configured as required (nil when none).
func (o OrchestraConfig) RequiredGates() map[string]bool {
	var out map[string]bool
	for k, v := range o.Gates {
		if strings.EqualFold(strings.TrimSpace(v), "required") {
			if out == nil {
				out = make(map[string]bool, len(o.Gates))
			}
			out[k] = true
		}
	}
	return out
}

// ResolvedMaxWorkerRetries returns worker validation retry budget (default 3).
func (o OrchestraConfig) ResolvedMaxWorkerRetries() int {
	if o.MaxWorkerRetries <= 0 {
		return 3
	}
	return o.MaxWorkerRetries
}

// ResolvedMaxWorkerVerifyRetries returns post-worker verify retry budget (default 1).
func (o OrchestraConfig) ResolvedMaxWorkerVerifyRetries() int {
	if o.MaxWorkerVerifyRetries <= 0 {
		return 1
	}
	return o.MaxWorkerVerifyRetries
}

// ResolvedWorkerVerifyEnabled reports whether deterministic worker verification runs (default true).
func (o OrchestraConfig) ResolvedWorkerVerifyEnabled() bool {
	if o.WorkerVerifyEnabled == nil {
		return true
	}
	return *o.WorkerVerifyEnabled
}

// ResolvedWorkerLLMVerifyEnabled reports whether an LLM verifier child runs after deterministic worker checks (default false).
func (o OrchestraConfig) ResolvedWorkerLLMVerifyEnabled() bool {
	if o.WorkerLLMVerifyEnabled == nil {
		return false
	}
	return *o.WorkerLLMVerifyEnabled
}

// FindTier looks up a worker tier by name (case-insensitive).
func (o OrchestraConfig) FindTier(name string) *OrchestraTier {
	want := strings.TrimSpace(name)
	if want == "" {
		want = o.ResolvedDefaultTier()
	}
	for i := range o.Tiers {
		if strings.EqualFold(strings.TrimSpace(o.Tiers[i].Name), want) {
			return &o.Tiers[i]
		}
	}
	return nil
}
