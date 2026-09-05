package core

import (
	"context"
	"fmt"
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

// MemoryNoteStatus is what the end-of-turn memory writer did, returned to the
// client on session.message and mirrored into llm_log.jsonl as memory.note.
type MemoryNoteStatus struct {
	// Outcome: written | skipped | failed.
	Outcome string `json:"outcome"`
	// Source: model | digest — where the note text came from. Empty on a skip.
	Source string `json:"source,omitempty"`
	// Detail: the note itself when written, the reason otherwise.
	Detail string `json:"detail,omitempty"`
}

// maybeAutoSummaryMemory writes a note about the finished turn to project
// memory when agent.auto_summary_memory is enabled, and reports what it did.
// It returns nil only when the feature is off or the inputs are unusable.
//
// Two sources, in order of quality: a ModeSummary call for prose, and the turn
// digest already persisted by the agent. The digest path exists because the
// model path is the fragile one — it needs a reachable endpoint, and a run
// during an outage is exactly the run worth remembering. In the field this
// gap left one note across fifty-two sessions.
func (c *Core) maybeAutoSummaryMemory(ctx context.Context, sessionID string, hist []llm.Message, res *agent.Result) *MemoryNoteStatus {
	if c == nil || c.cfg == nil || !c.cfg.Agent.ResolvedAutoSummaryMemory() {
		return nil
	}
	if res == nil || c.tools == nil {
		return nil
	}

	st := c.buildMemoryNote(ctx, sessionID, hist)
	c.turnLogger().LogMemoryNote(st.Outcome, st.Source, st.Detail)
	return st
}

func (c *Core) buildMemoryNote(ctx context.Context, sessionID string, hist []llm.Message) *MemoryNoteStatus {
	source := "model"
	note, modelErr := c.llmSummaryNote(ctx, hist)
	if note == "" {
		source = "digest"
		note = working.MemoryNoteFromDigest(working.LastTurnDigest(c.workspaceRoot, sessionID))
	}
	if note == "" {
		reason := "turn changed no files"
		if modelErr != nil {
			reason = "model summary failed (" + modelErr.Error() + ") and " + reason
		}
		return &MemoryNoteStatus{Outcome: "skipped", Detail: reason}
	}

	entry := fmt.Sprintf("[session:%s] %s", sessionID, note)
	if _, err := c.tools.MemoryWrite(ctx, tools.MemoryWriteRequest{Content: entry, Scope: "project"}); err != nil {
		return &MemoryNoteStatus{Outcome: "failed", Source: source, Detail: err.Error()}
	}
	return &MemoryNoteStatus{Outcome: "written", Source: source, Detail: entry}
}

// turnLogger returns the llm_log.jsonl writer for this workspace: the one the
// LLM client already uses when it has one, otherwise a fresh handle on the same
// file, so memory events land next to the llm_* events regardless of client.
func (c *Core) turnLogger() *llm.Logger {
	if oc, ok := llm.AsOpenAIClient(c.llmClient); ok {
		if l := oc.GetLogger(); l != nil {
			return l
		}
	}
	return llm.NewLogger(c.workspaceRoot)
}

// llmSummaryNote asks the compaction model to summarise the turn. It returns
// "" on every failure path — with the error, so the caller can say why the
// note fell back to the digest — and "" with a nil error when the turn was
// too short to be worth a call.
func (c *Core) llmSummaryNote(ctx context.Context, hist []llm.Message) (string, error) {
	if c.llmClient == nil || len(hist) < minHistoryForLLMSummary {
		return "", nil
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
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(resp.Message.Content), nil
}
