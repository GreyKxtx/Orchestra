package agent

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/llm"
)

// maxOverflowRecoveries caps compact→retry cycles per Run so a history that
// cannot shrink enough fails fast instead of looping against the provider.
const maxOverflowRecoveries = 2

// contextWindowReporter is implemented by clients that know the server's real
// window (llm.OpenAIClient learns it from /v1/models or from a 400).
type contextWindowReporter interface {
	ContextTokens() int
}

// syncModelContextFromClient prefers the server-discovered window over the
// configured num_ctx, which may be stale in .orchestra.yml.
func (a *Agent) syncModelContextFromClient() {
	if a == nil || a.llm == nil {
		return
	}
	r, ok := a.llm.(contextWindowReporter)
	if !ok {
		return
	}
	if n := r.ContextTokens(); n > 0 && n != a.opts.ModelContextTokens {
		a.logf("model context window: %d (server) overrides %d (config)", n, a.opts.ModelContextTokens)
		a.opts.ModelContextTokens = n
	}
}

// recoverFromOverflow handles a provider context-window rejection the way
// OpenCode/Claude Code do: learn the real window from the error, shrink history
// (LLM checkpoint, else hard truncate to the token budget), and let the caller
// retry the same step instead of failing the turn.
//
// Returns the new history and ok=true when a retry is worth attempting.
func (a *Agent) recoverFromOverflow(ctx context.Context, userQuery string, hist []llm.Message, err error, step int) ([]llm.Message, bool) {
	if llm.IsUnreachableError(err) || a.llmInfraErr != nil {
		return hist, false
	}
	info, ok := llm.ParseContextOverflow(err)
	if !ok {
		return hist, false
	}
	if historyBytes(hist) == 0 {
		a.logf("context overflow on empty history — prompt itself exceeds the model window; skip compaction")
		return hist, false
	}
	if a.overflowRecoveries >= maxOverflowRecoveries {
		a.logf("context overflow: recovery budget exhausted (%d), failing turn", a.overflowRecoveries)
		return hist, false
	}
	a.overflowRecoveries++

	// Trust the provider's numbers for subsequent budgeting decisions.
	if info.ContextTokens > 0 {
		a.opts.ModelContextTokens = info.ContextTokens
	}
	if info.PromptTokens > 0 {
		a.lastPromptTokens = info.PromptTokens
		a.calibrateFromRealPrompt(info.PromptTokens)
	}

	before := historyBytes(hist)
	target := a.overflowTargetBytes()

	a.emitOverflowNotice(step, fmt.Sprintf(
		"контекст переполнен (%d prompt / %d ctx) — сжимаю историю",
		info.PromptTokens, info.ContextTokens))

	out := hist
	if a.llmInfraErr != nil {
		a.logf("overflow compaction skipped: previous LLM call was unreachable")
	} else if compacted, cerr := a.compactHistory(ctx, userQuery, hist); cerr == nil {
		out = compacted
	} else {
		if llm.IsUnreachableError(cerr) {
			a.llmInfraErr = cerr
			a.logf("overflow compaction aborted (LLM unreachable): %v", cerr)
			return hist, false
		}
		a.logf("overflow compaction failed (non-fatal): %v", cerr)
	}
	// Always enforce the hard byte target: a checkpoint that stayed too big
	// would 400 again on the retry.
	if target > 0 && historyBytes(out) > target {
		out = truncateMessages(out, target)
	}
	after := historyBytes(out)
	a.recordCompactMetrics(before, after, after < before)
	a.logf("context overflow recovery: history %d → %d bytes (target %d)", before, after, target)

	if after >= before && before > 0 {
		// Nothing was reclaimed — retrying would repeat the same request.
		return hist, false
	}
	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventRecoverableError,
			Content: "CONTEXT_COMPACTED",
		}})
	}
	return out, true
}

// overflowTargetBytes is the history byte ceiling that keeps the next request
// inside the model window (prompt budget minus fixed prompt overhead).
func (a *Agent) overflowTargetBytes() int {
	bpt := a.bytesPerToken()
	target := a.opts.MaxPromptBytes / 2
	if ctxTok := a.opts.ModelContextTokens; ctxTok > 0 {
		budgetTok := llm.PromptBudgetTokens(ctxTok, a.opts.CompletionMaxTokens)
		// Reserve the same fixed system/tool overhead estimatePromptTokens uses.
		overheadTok := promptOverheadBytes / bpt
		histTok := budgetTok - overheadTok
		if histTok < 512 {
			histTok = 512
		}
		if b := histTok * bpt; b > 0 && (target <= 0 || b < target) {
			target = b
		}
	}
	if target < 4*1024 {
		target = 4 * 1024
	}
	return target
}

func (a *Agent) emitOverflowNotice(step int, msg string) {
	if a.opts.OnEvent == nil {
		return
	}
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventRecoverableError,
		Content: msg,
	}})
}

// calibrateFromRealPrompt tunes bytes-per-token from a provider-reported prompt
// size so future budgeting tracks the real tokenizer instead of a fixed guess.
// Mirrors OpenCode's "estimate, then correct from usage" approach.
func (a *Agent) calibrateFromRealPrompt(realPromptTokens int) {
	if a == nil || realPromptTokens <= 0 || a.lastPromptBytes <= 0 {
		return
	}
	ratio := a.lastPromptBytes / realPromptTokens
	if ratio < minBytesPerContextToken {
		ratio = minBytesPerContextToken
	}
	if ratio > maxBytesPerContextToken {
		ratio = maxBytesPerContextToken
	}
	if a.calibratedBytesPerToken == 0 {
		a.calibratedBytesPerToken = ratio
		return
	}
	// Keep the more pessimistic (smaller bytes/token ⇒ more tokens estimated).
	if ratio < a.calibratedBytesPerToken {
		a.calibratedBytesPerToken = ratio
	}
}

const (
	minBytesPerContextToken = 2
	maxBytesPerContextToken = 6
)
