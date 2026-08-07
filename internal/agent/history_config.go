package agent

import (
	configpkg "github.com/orchestra/orchestra/internal/config"
)

// ApplyHistoryConfig fills history/memory knobs from project config.
// Call after constructing Options so CLI, core, and skill.invoke stay in sync.
// Does not overwrite MaxPromptBytes / MaxSteps (those have their own resolvers).
// Does not overwrite AutoSessionMemory when already set by the launch path
// (one-shot agent.run forces false; sessions resolve from config before this).
func ApplyHistoryConfig(opts *Options, cfg *configpkg.ProjectConfig) {
	if opts == nil || cfg == nil {
		return
	}
	opts.ToolDigestBytes = cfg.Agent.ResolvedToolDigestBytes()
	opts.HistoryPruneKeepRecent = cfg.Agent.ResolvedHistoryPruneKeepRecent()
	opts.BytesPerContextToken = cfg.Agent.ResolvedBytesPerContextToken()
	if opts.CompactThresholdPct == 0 {
		opts.CompactThresholdPct = cfg.Agent.ResolvedCompactThresholdPct()
	}
	if opts.TurnDigestKeep == 0 {
		opts.TurnDigestKeep = cfg.Agent.ResolvedTurnDigestKeep()
	}
	if opts.TurnDigestEveryN == 0 {
		opts.TurnDigestEveryN = cfg.Agent.ResolvedTurnDigestEveryN()
	}
	if opts.WorkingState == nil {
		v := cfg.Agent.ResolvedWorkingState()
		opts.WorkingState = &v
	}
	opts.Memory = cfg.Memory.Resolve()
}
