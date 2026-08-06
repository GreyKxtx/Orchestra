package agent

import (
	configpkg "github.com/orchestra/orchestra/internal/config"
)

// ApplyHistoryConfig fills history/memory knobs from project config.
// Call after constructing Options so CLI, core, and skill.invoke stay in sync.
// Does not overwrite MaxPromptBytes / MaxSteps (those have their own resolvers).
func ApplyHistoryConfig(opts *Options, cfg *configpkg.ProjectConfig) {
	if opts == nil || cfg == nil {
		return
	}
	opts.ToolDigestBytes = cfg.Agent.ResolvedToolDigestBytes()
	opts.HistoryPruneKeepRecent = cfg.Agent.ResolvedHistoryPruneKeepRecent()
	if opts.CompactThresholdPct == 0 {
		opts.CompactThresholdPct = cfg.Agent.CompactThresholdPct
	}
	opts.Memory = cfg.Memory.Resolve()
}
