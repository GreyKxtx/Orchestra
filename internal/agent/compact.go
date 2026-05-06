package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
)

// historyBytes returns the approximate size of the history in bytes.
// Counts content strings and tool call argument blobs (the bulk of long histories).
func historyBytes(history []llm.Message) int {
	total := 0
	for _, m := range history {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Name)
			total += len(tc.Function.Arguments.Raw())
		}
	}
	return total
}

// compactHistory calls the LLM in compaction mode to summarize the conversation
// and returns a new single-message history containing only the summary.
// On failure, returns an error so the caller can fall back to truncation.
func (a *Agent) compactHistory(ctx context.Context, userQuery string, history []llm.Message) ([]llm.Message, error) {
	family := a.opts.PromptFamily
	sysprompt := promptpkg.BuildSystemPromptForMode(ModeCompaction, family)

	// Serialize history as a plain-text conversation for the compaction LLM call.
	var sb strings.Builder
	sb.WriteString("Original user request: ")
	sb.WriteString(userQuery)
	sb.WriteString("\n\nConversation history to compress:\n\n")
	for _, m := range history {
		role := string(m.Role)
		if m.Content != "" {
			sb.WriteString(role)
			sb.WriteString(": ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		for _, tc := range m.ToolCalls {
			args := string(tc.Function.Arguments.Raw())
			sb.WriteString(role)
			sb.WriteString(" [tool_call ")
			sb.WriteString(tc.Function.Name)
			sb.WriteString("]: ")
			sb.WriteString(args)
			sb.WriteString("\n")
		}
	}

	req := llm.CompleteRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sysprompt},
			{Role: llm.RoleUser, Content: sb.String()},
		},
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("compaction LLM call: %w", err)
	}

	summary := strings.TrimSpace(resp.Message.Content)
	if summary == "" {
		return nil, fmt.Errorf("compaction returned empty summary")
	}

	// Replace the entire history with a single synthetic user message containing the summary.
	compacted := []llm.Message{
		{
			Role:    llm.RoleUser,
			Content: "[История разговора сжата]\n\n" + summary,
		},
	}
	return compacted, nil
}
