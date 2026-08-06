package agent

import "github.com/orchestra/orchestra/internal/llm"

// estimatePromptTokens approximates the next LLM prompt size from history bytes
// plus fixed overhead (system prompt, tool defs, user shell). Emitted every
// agent step so the TUI ctx bar stays current even when the provider omits
// stream usage (common with LM Studio). Real usage overwrites when available.
func estimatePromptTokens(history []llm.Message, maxPromptBytes int) int {
	base := historyBytes(history)
	overhead := 32 * 1024 // ~8k tokens minimum for system + tools
	if maxPromptBytes > 0 {
		if oh := maxPromptBytes / 5; oh > overhead {
			overhead = oh
		}
	}
	tokens := (base + overhead) / bytesPerContextToken
	if tokens < 0 {
		return 0
	}
	return tokens
}

const bytesPerContextToken = 4
