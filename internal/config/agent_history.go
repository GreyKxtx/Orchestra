package config

// ResolvedToolDigestBytes returns max raw tool output kept in history (0 = disabled).
func (a AgentConfig) ResolvedToolDigestBytes() int {
	if a.ToolDigestKB < 0 {
		return 0
	}
	kb := a.ToolDigestKB
	if kb == 0 {
		kb = 48
	}
	return kb * 1024
}

// ResolvedAutoSessionMemory reports whether explore/grep auto-notes session memory.
func (a AgentConfig) ResolvedAutoSessionMemory() bool {
	if a.AutoSessionMemory == nil {
		return true
	}
	return *a.AutoSessionMemory
}

// ResolvedHistoryPruneKeepRecent returns how many recent tool atoms stay full during retroactive prune.
func (a AgentConfig) ResolvedHistoryPruneKeepRecent() int {
	if a.HistoryPruneKeepRecent <= 0 {
		return 2
	}
	return a.HistoryPruneKeepRecent
}

// ResolvedChildMaxSteps returns the clamp for task/task_spawn child MaxSteps (default 12).
func (a AgentConfig) ResolvedChildMaxSteps() int {
	if a.ChildMaxSteps <= 0 {
		return 12
	}
	return a.ChildMaxSteps
}

// ResolvedBytesPerContextToken returns the estimate calibration (default 4).
func (a AgentConfig) ResolvedBytesPerContextToken() int {
	if a.BytesPerContextToken <= 0 {
		return 4
	}
	return a.BytesPerContextToken
}

// ResolvedAutoSummaryMemory reports whether long turns auto-write a project memory summary.
func (a AgentConfig) ResolvedAutoSummaryMemory() bool {
	if a.AutoSummaryMemory == nil {
		return true
	}
	return *a.AutoSummaryMemory
}

// ResolvedCompactThresholdPct returns the compact trigger percent.
// 0 means disabled (after Normalize converted -1 → 0). Positive values as-is.
func (a AgentConfig) ResolvedCompactThresholdPct() int {
	if a.CompactThresholdPct < 0 {
		return 0
	}
	return a.CompactThresholdPct
}

// ResolvedWorkingState reports whether <working_state> inject is enabled (default true).
func (a AgentConfig) ResolvedWorkingState() bool {
	if a.WorkingState == nil {
		return true
	}
	return *a.WorkingState
}

// ResolvedTurnDigestKeep returns how many turn digests to inject (default 3).
// nil → 3; 0 or negative → off (no persist/inject).
func (a AgentConfig) ResolvedTurnDigestKeep() int {
	if a.TurnDigestKeep == nil {
		return 3
	}
	if *a.TurnDigestKeep <= 0 {
		return 0
	}
	return *a.TurnDigestKeep
}

// ResolvedTurnDigestEveryN returns mid-run micro-digest interval (default 6).
// nil → 6; 0 or negative → end-of-run only.
func (a AgentConfig) ResolvedTurnDigestEveryN() int {
	if a.TurnDigestEveryN == nil {
		return 6
	}
	if *a.TurnDigestEveryN <= 0 {
		return 0
	}
	return *a.TurnDigestEveryN
}

// ResolvedSemanticAutoExplore reports whether semantic_search auto-runs explore on top hits.
func (e EmbedConfig) ResolvedSemanticAutoExplore() bool {
	if e.Model == "" {
		return false
	}
	if e.SemanticAutoExplore == nil {
		return true
	}
	return *e.SemanticAutoExplore
}

// ResolvedSemanticAutoExploreTopK returns explore enrichment count for semantic_search.
func (e EmbedConfig) ResolvedSemanticAutoExploreTopK() int {
	if e.SemanticAutoExploreTopK <= 0 {
		return 2
	}
	if e.SemanticAutoExploreTopK > 5 {
		return 5
	}
	return e.SemanticAutoExploreTopK
}
