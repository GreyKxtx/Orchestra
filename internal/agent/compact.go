package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/agent/history"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/llm"
)

// historyBytes returns the approximate size of the history in bytes.
// Uses the same estimator as truncation so compaction thresholds align with
// real MaxPromptBytes budgeting.
func historyBytes(hist []llm.Message) int {
	total := 0
	for _, m := range hist {
		total += estimateMessageSize(m)
	}
	return total
}

const checkpointHeader = "[Session checkpoint — structured summary]"

// compactHistory calls the LLM in compaction mode and returns a sticky
// checkpoint: a summary of the older history followed by the recent tail
// (kept verbatim, see splitHistoryForCompaction).
func (a *Agent) compactHistory(ctx context.Context, userQuery string, hist []llm.Message) ([]llm.Message, error) {
	if a.llmInfraErr != nil {
		return nil, fmt.Errorf("compaction skipped: %w", a.llmInfraErr)
	}
	family := a.opts.PromptFamily
	sysprompt := promptpkg.BuildSystemPromptForMode(string(ModeCompaction), family)

	// Compact the older half only: the recent tail stays in history verbatim.
	// Summarising *everything* and keeping two tool atoms left the agent with
	// less working memory right after compaction than it had on step one —
	// which is what makes a long run start re-reading files it already read.
	keep := a.opts.HistoryPruneKeepRecent
	if keep <= 0 {
		keep = defaultHistoryPruneKeepRecent
	}
	older, tail := splitHistoryForCompaction(hist, a.compactionTailBudget(), keep)
	if len(older) == 0 {
		// Nothing old enough to summarise (the tail alone is over budget);
		// let the caller fall back to truncation.
		return nil, fmt.Errorf("compaction skipped: no history older than the retained tail")
	}

	var sb strings.Builder
	sb.WriteString("Original user request: ")
	sb.WriteString(userQuery)
	sb.WriteString("\n\nConversation history to compress:\n\n")
	sb.WriteString(a.buildCompactionCorpus(older))
	if len(a.todos) > 0 {
		sb.WriteString("\nPinned todos:\n")
		for _, t := range a.todos {
			sb.WriteString("- [")
			sb.WriteString(string(t.Status))
			sb.WriteString("] ")
			sb.WriteString(t.Content)
			sb.WriteString("\n")
		}
	}

	req := llm.CompleteRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sysprompt},
			{Role: llm.RoleUser, Content: sb.String()},
		},
	}

	client := a.llm
	if a.opts.CompactionClient != nil {
		client = a.opts.CompactionClient
	}
	resp, err := client.Complete(ctx, req)
	if err != nil {
		err = fmt.Errorf("compaction LLM call: %w", err)
		if llm.IsUnreachableError(err) {
			a.llmInfraErr = err
		}
		return nil, err
	}
	if a.opts.UsageTracker != nil && resp != nil && resp.Usage != nil {
		a.opts.UsageTracker.RecordCost(a.opts.ProviderLabel, a.opts.ModelLabel,
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CostUSD)
	}

	summary := strings.TrimSpace(resp.Message.Content)
	if summary == "" {
		return nil, fmt.Errorf("compaction returned empty summary")
	}

	var body strings.Builder
	body.WriteString(checkpointHeader)
	body.WriteString("\n\n")
	body.WriteString("Goal: ")
	body.WriteString(strings.TrimSpace(userQuery))
	body.WriteString("\n\n")
	body.WriteString(summary)

	compacted := make([]llm.Message, 0, 1+len(tail))
	compacted = append(compacted, llm.Message{
		Role:    llm.RoleUser,
		Content: body.String(),
	})
	compacted = append(compacted, tail...)
	return compacted, nil
}

// CompactNow runs ModeCompaction on hist (summary of older history + verbatim tail).
func (a *Agent) CompactNow(ctx context.Context, userQuery string, hist []llm.Message) ([]llm.Message, error) {
	return a.compactHistory(ctx, userQuery, hist)
}

// compactEntryMaxChars caps a single message/tool payload inside the compaction
// prompt. Mirrors OpenCode: the summarizer needs the shape of a tool result,
// not its full body.
const compactEntryMaxChars = 2000

// compactCorpusEntry is one rendered history line plus whether it mentions a
// file the agent is still actively working on (see activeFiles below).
type compactCorpusEntry struct {
	text      string
	protected bool
}

// buildCompactionCorpus renders history for the summarizer, capping each entry
// and the total so the compaction request itself cannot overflow the model
// window (which would make compaction fail exactly when it is needed most).
//
// Entries mentioning a currently active file (a.working.ActiveFiles — the
// same set that protects retroactive tool-history pruning) are rescued from
// the oldest-first cut when possible: dropping the read/edit history of a
// file the agent is mid-edit-on produces summaries that "forget" what was
// already tried on that file, causing repeated/contradictory edits after
// compaction.
func (a *Agent) buildCompactionCorpus(hist []llm.Message) string {
	var activeFiles []string
	if a.working != nil {
		activeFiles = a.working.ActiveFiles()
	}
	isProtected := func(s string) bool {
		for _, p := range activeFiles {
			if p != "" && strings.Contains(s, p) {
				return true
			}
		}
		return false
	}

	entries := make([]compactCorpusEntry, 0, len(hist))
	for _, m := range hist {
		role := string(m.Role)
		var e strings.Builder
		if m.Content != "" {
			e.WriteString(role)
			e.WriteString(": ")
			e.WriteString(clipChars(m.Content, compactEntryMaxChars))
			e.WriteString("\n")
		}
		for _, tc := range m.ToolCalls {
			e.WriteString(role)
			e.WriteString(" [tool_call ")
			e.WriteString(tc.Function.Name)
			e.WriteString("]: ")
			e.WriteString(clipChars(string(tc.Function.Arguments.Raw()), compactEntryMaxChars))
			e.WriteString("\n")
		}
		if e.Len() > 0 {
			text := e.String()
			entries = append(entries, compactCorpusEntry{text: text, protected: isProtected(text)})
		}
	}

	budget := a.compactionCorpusBudget()
	total := 0
	start := 0
	// Keep the newest entries: walk backwards until the budget is spent.
	for i := len(entries) - 1; i >= 0; i-- {
		if total+len(entries[i].text) > budget {
			start = i + 1
			break
		}
		total += len(entries[i].text)
	}

	included := make([]bool, len(entries))
	for i := start; i < len(entries); i++ {
		included[i] = true
	}
	// Rescue older entries about active files, within a modest extra budget
	// so a busy compaction can't be starved back into overflow.
	if start > 0 && len(activeFiles) > 0 {
		rescueBudget := budget / 4
		used := 0
		for i := start - 1; i >= 0; i-- {
			if !entries[i].protected {
				continue
			}
			if used+len(entries[i].text) > rescueBudget {
				break
			}
			included[i] = true
			used += len(entries[i].text)
		}
	}

	omitted := 0
	for i := 0; i < start; i++ {
		if !included[i] {
			omitted++
		}
	}

	var sb strings.Builder
	if omitted > 0 {
		fmt.Fprintf(&sb, "[... %d earlier messages omitted to fit the summarizer context ...]\n\n", omitted)
	}
	for i, e := range entries {
		if included[i] {
			sb.WriteString(e.text)
		}
	}
	return sb.String()
}

// compactionCorpusBudget is the byte ceiling for the summarizer prompt.
//
// Uses CompactionContextTokens (the actual window of the model that will
// receive this corpus — e.g. providers.fast) when set, falling back to
// ModelContextTokens (the MAIN model's window) only when no compaction-
// specific window is known. Before this, a smaller/cheaper compaction
// provider's own window was ignored and the corpus could be sized for the
// main model's much larger context, overflowing the summarizer request.
func (a *Agent) compactionCorpusBudget() int {
	bpt := a.bytesPerToken()
	budget := a.opts.MaxPromptBytes
	ctxTok := a.opts.CompactionContextTokens
	if ctxTok <= 0 {
		ctxTok = a.opts.ModelContextTokens
	}
	if ctxTok > 0 {
		// The summarizer answer is small; reserve a modest completion budget.
		if b := llm.PromptBudgetTokens(ctxTok, 4096) * bpt / 2; b > 0 {
			if budget <= 0 || b < budget {
				budget = b
			}
		}
	}
	if budget <= 0 {
		budget = 64 * 1024
	}
	if budget < 8*1024 {
		budget = 8 * 1024
	}
	return budget
}

func clipChars(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…[clipped]"
}

// compactionTailRetainPct is the share of the prompt byte budget that survives
// compaction verbatim. The summary covers everything older.
const compactionTailRetainPct = 30

// compactionTailMaxSharePct caps the verbatim tail as a share of the history
// being compacted, so compaction always shrinks enough to clear the threshold
// (the run loop rejects a "compaction" that retained ≥80% and truncates instead).
const compactionTailMaxSharePct = 50

// compactionTailBudget is the byte ceiling for the verbatim tail kept after a
// compaction.
func (a *Agent) compactionTailBudget() int {
	budget := a.opts.MaxPromptBytes
	if budget <= 0 {
		budget = 64 * 1024
	}
	tail := budget * compactionTailRetainPct / 100
	if tail < 8*1024 {
		tail = 8 * 1024
	}
	return tail
}

// splitHistoryForCompaction cuts history at an atom boundary into the part to
// summarise (older) and the part to keep verbatim (tail).
//
// The tail gets whichever is smaller: tailBudget bytes, or half the history —
// but always at least minToolAtoms tool-bearing atoms, so the immediately
// preceding tool calls survive even under a tight budget. When the tail would
// swallow the whole history, older comes back empty and the caller falls back
// to truncation.
func splitHistoryForCompaction(msgs []llm.Message, tailBudget, minToolAtoms int) (older, tail []llm.Message) {
	if len(msgs) == 0 {
		return nil, nil
	}
	atoms := history.BuildHistoryAtoms(msgs)
	if len(atoms) <= 1 {
		return msgs, nil
	}

	sizes := make([]int, len(atoms))
	total := 0
	for i, at := range atoms {
		for _, m := range history.AtomMessages(at) {
			sizes[i] += estimateMessageSize(m)
		}
		total += sizes[i]
	}
	shareCap := total * compactionTailMaxSharePct / 100
	if tailBudget > shareCap {
		tailBudget = shareCap
	}

	used := 0
	toolAtoms := 0
	start := len(atoms) // first atom index belonging to the tail
	for i := len(atoms) - 1; i >= 1; i-- {
		fitsBudget := used+sizes[i] <= tailBudget
		needForMinAtoms := toolAtoms < minToolAtoms
		if !fitsBudget && !needForMinAtoms {
			break
		}
		start = i
		used += sizes[i]
		if history.AtomHasToolRole(atoms[i]) {
			toolAtoms++
		}
	}
	if start >= len(atoms) {
		// Not even one atom fits: keep the last one so the model still sees
		// what it was just doing.
		start = len(atoms) - 1
	}
	if start == 0 {
		return nil, msgs
	}

	for i := 0; i < start; i++ {
		older = append(older, history.AtomMessages(atoms[i])...)
	}
	for i := start; i < len(atoms); i++ {
		tail = append(tail, history.AtomMessages(atoms[i])...)
	}
	return older, tail
}
