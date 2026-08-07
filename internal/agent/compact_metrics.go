package agent

// CompactMetrics tracks compaction outcomes for diagnostics / Phase 4.
type CompactMetrics struct {
	Attempts       int
	Successes      int
	TruncateFalls  int // failed compact or <20% shrink → truncate
	BytesBeforeSum int64
	BytesAfterSum  int64
}

// CompactStats returns a copy of compaction counters for this agent.
func (a *Agent) CompactStats() CompactMetrics {
	if a == nil {
		return CompactMetrics{}
	}
	return a.compactMetrics
}

func (a *Agent) recordCompactMetrics(before, after int, success bool) {
	if a == nil {
		return
	}
	a.compactMetrics.Attempts++
	a.compactMetrics.BytesBeforeSum += int64(before)
	a.compactMetrics.BytesAfterSum += int64(after)
	if success {
		a.compactMetrics.Successes++
	} else {
		a.compactMetrics.TruncateFalls++
	}
}
