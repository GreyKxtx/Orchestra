package llm

import (
	"regexp"
	"strconv"
	"strings"
)

// Context budget helpers aligned with vLLM OpenAI server validation:
//
//	len(prompt_token_ids) + max_tokens ≤ max_model_len
//
// See vllm/entrypoints/openai/serving_engine.py (_validate_input).
// vLLM rejects with HTTP 400 instead of clamping max_tokens for the client.

// ContextSafetyTokens is slack for chat-template / tool schemas / tokenizer
// drift between our estimates and the server's real count.
const ContextSafetyTokens = 2048

// EstimateTokensFromBytes is the single pessimistic byte→token conversion
// shared by the wire-level clamp (OpenAIClient.maxTokensForRequest) and the
// agent's compaction trigger (shouldCompactHistoryEx). Before this, the two
// call sites used independently-tuned formulas that could disagree on
// whether a given history was "too big" — the client could silently shrink
// max_tokens on a request the agent had judged safe. ~2 bytes/token is
// pessimistic for Latin code; CJK/tool JSON tokens are often denser.
func EstimateTokensFromBytes(nbytes int) int {
	if nbytes < 0 {
		return 0
	}
	return nbytes/2 + nbytes/20 + 1024
}

// MinCompletionTokens is the smallest completion budget we will send or reserve.
const MinCompletionTokens = 256

// CompletionRoom returns how many completion tokens fit after a prompt of
// promptTok inside a context window of contextLen (vLLM invariant).
func CompletionRoom(contextLen, promptTok int) int {
	if contextLen <= 0 {
		return 0
	}
	room := contextLen - promptTok - ContextSafetyTokens
	if room < MinCompletionTokens {
		return MinCompletionTokens
	}
	return room
}

// PromptBudgetTokens is the largest prompt size that still leaves room for
// wantCompletion tokens inside contextLen. Used by agent compaction so history
// shrinks before the wire request would 400.
func PromptBudgetTokens(contextLen, wantCompletion int) int {
	if contextLen <= 0 {
		return 0
	}
	if wantCompletion <= 0 {
		wantCompletion = defaultMaxTokens
	}
	// Cap completion reserve the same way the client caps wire max_tokens.
	wantCompletion = effectiveMaxTokens(wantCompletion, contextLen)
	budget := contextLen - wantCompletion - ContextSafetyTokens
	floor := contextLen / 4
	if floor < MinCompletionTokens {
		floor = MinCompletionTokens
	}
	if budget < floor {
		return floor
	}
	return budget
}

// FitsContext reports whether promptTok + wantCompletion can fit in contextLen
// under the vLLM check (with safety slack).
func FitsContext(contextLen, promptTok, wantCompletion int) bool {
	if contextLen <= 0 {
		return true
	}
	if wantCompletion <= 0 {
		wantCompletion = defaultMaxTokens
	}
	return promptTok+wantCompletion+ContextSafetyTokens <= contextLen
}

// ContextOverflow carries the server-reported numbers from a context-overflow
// rejection so callers (the agent loop) can compact history and retry the step
// instead of failing the turn.
type ContextOverflow struct {
	// ContextTokens is the model window reported by the provider (max_model_len).
	ContextTokens int
	// PromptTokens is the prompt size the provider measured with its tokenizer.
	PromptTokens int
}

// ParseContextOverflow extracts provider numbers from a context-overflow error.
// Recognises vLLM/OpenAI wordings, our own local pre-flight rejection, and —
// as a fallback — any HTTP 400 whose body reads like a context-window
// rejection from a server we don't have a specific regex for (llama.cpp
// server, TGI, a custom OpenAI-compatible proxy, or a future vLLM wording
// change). The fallback may not recover exact numbers; ContextOverflow zero
// fields are safe — recoverFromOverflow only uses them when > 0 and still
// runs its byte-budget compaction/retry regardless.
func ParseContextOverflow(err error) (ContextOverflow, bool) {
	if err == nil {
		return ContextOverflow{}, false
	}
	msg := err.Error()
	if ctxLen, promptTok, ok := parseContextLengthError(msg); ok {
		return ContextOverflow{ContextTokens: ctxLen, PromptTokens: promptTok}, true
	}
	if ctxLen, promptTok, ok := parseLocalPromptTooLarge(msg); ok {
		return ContextOverflow{ContextTokens: ctxLen, PromptTokens: promptTok}, true
	}
	if ctxLen, promptTok, ok := parseGenericContextOverflow(msg); ok {
		return ContextOverflow{ContextTokens: ctxLen, PromptTokens: promptTok}, true
	}
	return ContextOverflow{}, false
}

// genericContextOverflowKeywords are substrings (checked case-insensitively)
// that reliably co-occur with a context-window rejection across OpenAI-
// compatible servers, even when the exact sentence differs from the vLLM
// wordings parseContextLengthError knows about.
var genericContextOverflowKeywords = []string{
	"context length", "context window", "context_length_exceeded",
	"maximum context", "max_model_len", "model's maximum context",
	"exceeds the model", "exceeds context", "too many tokens",
	"context is too long", "reduce the length",
}

// genericOverflowNumberRe finds bare integers of 3+ digits — used to recover
// best-effort ctxLen/promptTok when the message doesn't match a known
// wording. Numbers are heuristic; see doc comment on ParseContextOverflow.
var genericOverflowNumberRe = regexp.MustCompile(`\d{3,}`)

// statusPrefixRe matches the "status NNN" fragment formatAPIError prepends,
// stripped before number-scanning so the HTTP status code itself isn't
// mistaken for a token count.
var statusPrefixRe = regexp.MustCompile(`(?i)status\s+\d+`)

// parseGenericContextOverflow is a last-resort match: HTTP 400 + a phrase
// that strongly implies a context-window rejection. Only fires when none of
// the specific vLLM regexes matched, so it never overrides a precise parse.
func parseGenericContextOverflow(msg string) (ctxLen, promptTok int, ok bool) {
	if extractStatusCode(msg) != 400 {
		return 0, 0, false
	}
	low := strings.ToLower(msg)
	matched := false
	for _, kw := range genericContextOverflowKeywords {
		if strings.Contains(low, kw) {
			matched = true
			break
		}
	}
	if !matched {
		return 0, 0, false
	}
	// Best-effort: the two largest numbers mentioned are usually
	// (context window, requested/prompt tokens) in either order. Strip the
	// "status 400" prefix first so the HTTP status itself isn't mistaken
	// for one of them.
	stripped := statusPrefixRe.ReplaceAllString(msg, "")
	nums := genericOverflowNumberRe.FindAllString(stripped, -1)
	var parsed []int
	for _, n := range nums {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			parsed = append(parsed, v)
		}
	}
	switch {
	case len(parsed) >= 2:
		a, b := parsed[0], parsed[1]
		if a < b {
			a, b = b, a
		}
		ctxLen, promptTok = a, b
	case len(parsed) == 1:
		ctxLen = parsed[0]
	}
	return ctxLen, promptTok, true
}

// IsContextOverflowError reports whether err is a context-window rejection.
func IsContextOverflowError(err error) bool {
	_, ok := ParseContextOverflow(err)
	return ok
}

// localPromptTooLargeRe matches maxTokensForRequest's pre-flight rejection.
var localPromptTooLargeRe = regexp.MustCompile(
	`prompt too large \(~(\d+) tokens\) for model context (\d+)`)

func parseLocalPromptTooLarge(msg string) (ctxLen, promptTok int, ok bool) {
	m := localPromptTooLargeRe.FindStringSubmatch(msg)
	if len(m) != 3 {
		return 0, 0, false
	}
	promptTok, err1 := strconv.Atoi(m[1])
	ctxLen, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || ctxLen <= 0 || promptTok <= 0 {
		return 0, 0, false
	}
	return ctxLen, promptTok, true
}
