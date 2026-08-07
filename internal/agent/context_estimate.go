package agent

import "github.com/orchestra/orchestra/internal/llm"

// estimatePromptTokens approximates the next LLM prompt size from history bytes
// plus fixed overhead (system prompt, tool defs, user shell). Emitted every
// agent step so the TUI ctx bar stays current even when the provider omits
// stream usage (common with LM Studio). Real usage overwrites when available.
func estimatePromptTokens(history []llm.Message, maxPromptBytes int) int {
	return estimatePromptTokensWithFactor(history, maxPromptBytes, bytesPerContextToken)
}

// estimatePromptTokensWithFactor converts (history bytes + fixed overhead)
// into tokens using bytesPerTok — the estimate calibrated from real provider
// usage (calibrateFromRealPrompt) when available, else the family default.
//
// This intentionally does NOT reuse llm.EstimateTokensFromBytes (the wire
// client's ~2-bytes/token safety-margin formula for sizing max_tokens):
// that formula is deliberately far more pessimistic than real tokenization
// to protect a single request's max_tokens clamp, and folding it in here
// made the compaction trigger fire roughly 2x too early against a
// calibrated bytesPerTok. The two estimators share the same conversion
// function (llm.EstimateTokensFromBytes) at the call site that actually
// needs its pessimism — see estimateRequestTokens in internal/llm/client.go
// — so they no longer drift on FORMULA, only on how much safety margin each
// consumer needs.
func estimatePromptTokensWithFactor(history []llm.Message, maxPromptBytes, bytesPerTok int) int {
	if bytesPerTok <= 0 {
		bytesPerTok = bytesPerContextToken
	}
	base := historyBytes(history)
	overhead := 32 * 1024 // ~8k tokens minimum for system + tools
	if maxPromptBytes > 0 {
		if oh := maxPromptBytes / 5; oh > overhead {
			overhead = oh
		}
	}
	tokens := (base + overhead) / bytesPerTok
	if tokens < 0 {
		return 0
	}
	return tokens
}

// DefaultBytesPerContextToken is the heuristic when no family calibration is set.
const DefaultBytesPerContextToken = 4

const bytesPerContextToken = DefaultBytesPerContextToken

// shouldCompactHistory triggers when history bytes OR estimated/real prompt
// tokens exceed CompactThresholdPct of the budget.
func shouldCompactHistory(history []llm.Message, maxPromptBytes, compactPct int) bool {
	return shouldCompactHistoryEx(history, maxPromptBytes, compactPct, 0, 0, 0, bytesPerContextToken)
}

// shouldCompactHistoryEx adds optional real usage + model context + completion
// reserve + bytes/token factor.
//
// When modelCtxTokens > 0, the token budget is PromptBudgetTokens (vLLM rule:
// prompt + max_tokens ≤ max_model_len), not the raw window. Real lastPromptTokens
// from the previous step force compact when the next request would not fit.
func shouldCompactHistoryEx(history []llm.Message, maxPromptBytes, compactPct, lastPromptTokens, modelCtxTokens, completionMaxTokens, bytesPerTok int) bool {
	if compactPct <= 0 || maxPromptBytes <= 0 {
		return false
	}
	if bytesPerTok <= 0 {
		bytesPerTok = bytesPerContextToken
	}
	thresholdBytes := maxPromptBytes * compactPct / 100
	if historyBytes(history) > thresholdBytes {
		return true
	}

	est := estimatePromptTokensWithFactor(history, maxPromptBytes, bytesPerTok)

	// Prefer the larger of estimate and last real usage (history only grew since).
	promptEst := est
	if lastPromptTokens > promptEst {
		promptEst = lastPromptTokens
	}

	if modelCtxTokens > 0 {
		// Hard vLLM gate: if even the last known prompt cannot leave room for
		// the configured completion budget, compact before the next call.
		if lastPromptTokens > 0 && !llm.FitsContext(modelCtxTokens, lastPromptTokens, completionMaxTokens) {
			return true
		}
		if !llm.FitsContext(modelCtxTokens, promptEst, completionMaxTokens) {
			return true
		}
		budgetTok := llm.PromptBudgetTokens(modelCtxTokens, completionMaxTokens)
		if budgetTok <= 0 {
			return false
		}
		tokThreshold := budgetTok * compactPct / 100
		return promptEst > tokThreshold
	}

	budgetTok := maxPromptBytes / bytesPerTok
	if budgetTok <= 0 {
		return false
	}
	tokThreshold := budgetTok * compactPct / 100
	return promptEst > tokThreshold
}
