package core

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"
)

// maybeAutoSummaryMemory writes a ModeSummary note to project memory after a
// long turn when agent.auto_summary_memory is enabled (Phase 2).
func (c *Core) maybeAutoSummaryMemory(ctx context.Context, sessionID string, hist []llm.Message, res *agent.Result) {
	if c == nil || c.cfg == nil || !c.cfg.Agent.ResolvedAutoSummaryMemory() {
		return
	}
	if len(hist) < 8 || res == nil || c.llmClient == nil || c.tools == nil {
		return
	}
	family := promptpkg.ResolvePromptFamily(c.cfg.LLM.PromptFamily, c.cfg.LLM.Model)
	sys := promptpkg.BuildSystemPromptForMode(string(agent.ModeSummary), family)
	var sb strings.Builder
	// Length is governed solely by the ModeSummary system prompt (summary.txt:
	// "1-3 предложения, не длиннее ~60 слов"). A second numeric cap here
	// previously conflicted with it (≤120 words vs 1-3 sentences), leaving the
	// model to guess which constraint wins.
	sb.WriteString("Summarize the completed work for project memory. Focus on durable facts, decisions, and file paths.\n\n")
	n := 0
	for i := len(hist) - 1; i >= 0 && n < 12; i-- {
		m := hist[i]
		if m.Content == "" {
			continue
		}
		content := m.Content
		if len(content) > 400 {
			content = content[:400] + "…"
		}
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		sb.WriteString(content)
		sb.WriteString("\n")
		n++
	}
	client := c.llmClient
	if cc := c.compactionClient(nil); cc != nil {
		client = cc
	}
	resp, err := client.Complete(ctx, llm.CompleteRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sys},
			{Role: llm.RoleUser, Content: sb.String()},
		},
	})
	if err != nil || resp == nil {
		fmt.Fprintf(os.Stderr, "orchestra: auto summary memory: %v\n", err)
		return
	}
	summary := strings.TrimSpace(resp.Message.Content)
	if summary == "" {
		return
	}
	note := fmt.Sprintf("[session:%s] %s", sessionID, summary)
	if _, err := c.tools.MemoryWrite(ctx, tools.MemoryWriteRequest{Content: note, Scope: "project"}); err != nil {
		fmt.Fprintf(os.Stderr, "orchestra: auto summary memory write: %v\n", err)
	}
}
