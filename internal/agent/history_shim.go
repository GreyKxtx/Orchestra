package agent

import (
	"github.com/orchestra/orchestra/internal/agent/history"
	"github.com/orchestra/orchestra/llm"
)

// FormatSubagentResult collapses a child agent run into one structured summary
// for the parent. Kept on the root agent package so callers (tasks) stay stable.
func FormatSubagentResult(subagentType, goal string, hist []llm.Message, taskResult string, digestBudget int) string {
	return history.FormatSubagentResult(subagentType, goal, hist, taskResult, digestBudget)
}

func truncateMessages(messages []llm.Message, maxBytes int) []llm.Message {
	return history.TruncateMessages(messages, maxBytes)
}

func estimateMessageSize(msg llm.Message) int {
	return history.EstimateMessageSize(msg)
}

func pruneRetroactiveToolHistory(messages []llm.Message, digestBudget, keepRecent int, protectPaths ...string) []llm.Message {
	return history.PruneRetroactiveToolHistory(messages, digestBudget, keepRecent, protectPaths...)
}

func formatToolsCatalog(defs []llm.ToolDef) string {
	return history.FormatToolsCatalog(defs)
}

const defaultHistoryPruneKeepRecent = history.DefaultHistoryPruneKeepRecent

// FormatSubagentProgress re-exports history.FormatSubagentProgress.
func FormatSubagentProgress(subagentType, goal string, hist []llm.Message, digestBudget int) string {
	return history.FormatSubagentProgress(subagentType, goal, hist, digestBudget)
}
