package core

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/working"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// minHistoryForLLMSummary is the point below which a turn has not accumulated
// enough prose to be worth a summary call. Shorter turns are not skipped —
// they reach project memory through the rule-based digest note instead.
const minHistoryForLLMSummary = 4

// maybeAutoSummaryMemory writes a note about the finished turn to project
// memory when agent.auto_summary_memory is enabled.
//
// Two sources, in order of quality: a ModeSummary call for prose, and the turn
// digest already persisted by the agent. The digest path exists because the
// model path is the fragile one — it needs a reachable endpoint, and a run
// during an outage is exactly the run worth remembering. In the field this
// gap left one note across fifty-two sessions.
func (c *Core) maybeAutoSummaryMemory(ctx context.Context, sessionID string, hist []llm.Message, res *agent.Result) {
	if c == nil || c.cfg == nil || !c.cfg.Agent.ResolvedAutoSummaryMemory() {
		return
	}
	if res == nil || c.tools == nil {
		return
	}

	note := c.llmSummaryNote(ctx, hist)
	if note == "" {
		note = working.MemoryNoteFromDigest(working.LastTurnDigest(c.workspaceRoot, sessionID))
	}
	if note == "" {
		return
	}

	entry := fmt.Sprintf("[session:%s] %s", sessionID, note)
	if _, err := c.tools.MemoryWrite(ctx, tools.MemoryWriteRequest{Content: entry, Scope: "project"}); err != nil {
		fmt.Fprintf(os.Stderr, "orchestra: auto summary memory write: %v\n", err)
	}
}

// llmSummaryNote asks the compaction model to summarise the turn. It returns
// "" on every failure path so the caller falls back to the digest note.
func (c *Core) llmSummaryNote(ctx context.Context, hist []llm.Message) string {
	if c.llmClient == nil || len(hist) < minHistoryForLLMSummary {
		return ""
	}
	family := promptpkg.ResolvePromptFamily(c.cfg.LLM.PromptFamily, c.cfg.LLM.Model)
	sys := promptpkg.BuildSystemPromptForMode(string(agent.ModeSummary), family)
	var sb strings.Builder
	// Length is governed solely by the ModeSummary system prompt (summary.txt:
	// "1-3 sentences, no more than ~60 words"). A second numeric cap here
	// previously conflicted with it, leaving the model to guess which wins.
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
		fmt.Fprintf(os.Stderr, "orchestra: auto summary memory: %v (using turn digest instead)\n", err)
		return ""
	}
	return strings.TrimSpace(resp.Message.Content)
}
