package config

// ResolvedToolDigestBytes returns max raw tool output kept in history (0 = disabled).
func (a AgentConfig) ResolvedToolDigestBytes() int {
	if a.ToolDigestKB < 0 {
		return 0
	}
	kb := a.ToolDigestKB
	if kb == 0 {
		kb = 16
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
