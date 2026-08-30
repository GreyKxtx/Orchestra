package agent

import (
	"strings"

	"github.com/orchestra/orchestra/llm"
)

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
	// Fixed overhead for system prompt + tool schemas + user shell.
	// Do NOT scale with MaxPromptBytes/window — that made the status bar jump
	// ~+12k on every new turn (reopen → follow-up) before real usage arrived.
	_ = maxPromptBytes
	overhead := promptOverheadBytes
	tokens := (base + overhead) / bytesPerTok
	if tokens < 0 {
		return 0
	}
	return tokens
}

// promptOverheadBytes is a fixed stand-in for system + tools (~8k tokens at 4 B/tok).
const promptOverheadBytes = 32 * 1024

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
// prompt + max_tokens ≤ max_model_len), not the raw window. Comparing against
// raw FitsContext(prompt, unclamped max_tokens) is wrong: a Settings value like
// max_tokens≈num_ctx makes every non-trivial prompt "overflow" while the status
// bar still shows used/num_ctx ≈ 20%.
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
		budgetTok := llm.PromptBudgetTokens(modelCtxTokens, completionMaxTokens)
		if budgetTok <= 0 {
			return false
		}
		// Hard gate: last measured prompt already exceeds what fits with
		// completion reserve (after the same clamp the wire client uses).
		if lastPromptTokens > budgetTok {
			return true
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

// nonASCIIHeavyPct is the share of non-ASCII bytes above which a prompt is
// treated as predominantly non-Latin script.
const nonASCIIHeavyPct = 30

// nonLatinBytesPerContextToken is the pre-calibration estimate for such text.
//
// Latin text runs at roughly 4 bytes per token, which is what
// DefaultBytesPerContextToken assumes. Cyrillic and CJK cost 2-3 UTF-8 bytes
// per character and tokenize into shorter pieces, and how much shorter depends
// on the tokenizer — modern cloud tokenizers stay near 4 bytes/token, while the
// local models this project targets (Qwen, Llama derivatives) can drop to 2-3.
// Assuming 4 there under-counts tokens, i.e. over-fills the window, which is
// the failure direction that ends in a provider 400.
//
// This is a guess for step 1 only: from the first response on,
// calibrateFromRealPrompt replaces it with the provider's own numbers.
const nonLatinBytesPerContextToken = 3

// detectBytesPerToken estimates bytes-per-token from the script of a sample of
// the prompt. Returns DefaultBytesPerContextToken for ordinary Latin text.
func detectBytesPerToken(sample string) int {
	if sample == "" {
		return DefaultBytesPerContextToken
	}
	nonASCII := 0
	for i := 0; i < len(sample); i++ {
		if sample[i] >= 0x80 {
			nonASCII++
		}
	}
	if nonASCII*100/len(sample) >= nonASCIIHeavyPct {
		return nonLatinBytesPerContextToken
	}
	return DefaultBytesPerContextToken
}

// detectSampleMaxBytes caps how much of the prompt the script check reads.
const detectSampleMaxBytes = 64 * 1024

// sampleMessagesText concatenates message text up to detectSampleMaxBytes.
func sampleMessagesText(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Content == "" {
			continue
		}
		b.WriteString(m.Content)
		if b.Len() >= detectSampleMaxBytes {
			break
		}
	}
	return b.String()
}
