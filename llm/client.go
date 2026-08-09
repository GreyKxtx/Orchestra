package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// truncateID truncates an ID string for logging
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
}

// Client is an interface for LLM clients
type Client interface {
	Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error)
	Plan(ctx context.Context, prompt string) (string, error) // Returns JSON plan (legacy)
}

// OpenAIClient is an OpenAI-compatible LLM client
type OpenAIClient struct {
	baseURL       string
	apiKey        string
	model         string
	provider      string
	wantMaxTokens int // user-configured; may exceed safe cap until context is known
	maxTokens     int // effective value sent on the wire
	contextTokens int // server / num_ctx window; 0 = unknown
	temperature   float32
	toolChoice    string // resolved: auto | omit | none | required
	// toolChoiceImplicit is true when cfg.ToolChoice was left blank and
	// resolveToolChoice fell through to its provider-based guess. An implicit
	// "omit" is the single biggest cause of "the model just ignores my
	// tools" reports: nothing tells the model tool use is expected, and
	// nothing in our logs says we made that call for the user. Logged once
	// per client (see warnToolChoiceOnce) the first time a tool-bearing
	// request actually goes out.
	toolChoiceImplicit bool
	toolChoiceWarned   bool
	extraBody          map[string]any
	client             *http.Client
	streamClient       *http.Client  // no Timeout — relies on context cancellation for SSE connections
	requestTimeout     time.Duration // llm.timeout_s — also scales stream stall watchdog
	logger             *Logger

	// supportsJSONSchema: nil = unknown (send when requested; auto-disable on reject);
	// non-nil bool = explicit config. Mutated under supportsMu when auto-detect trips.
	supportsJSONSchema *bool
	supportsMu         sync.Mutex
}

// newLLMTransport returns an HTTP transport tuned for remote/tunnelled LLM
// servers (ngrok, vLLM behind a proxy). Default Transport has no header/dial
// timeouts, so a silently dropped tunnel connection hangs until the step
// timeout (up to 15 min). Keep-alives detect dead peers earlier.
//
// headerTimeout bounds time-to-first-byte (response headers). Slow local
// models behind ngrok can spend minutes accepting a large prompt before the
// first SSE byte — keep this aligned with llm.timeout_s, not a fixed 180s.
func newLLMTransport(headerTimeout time.Duration) *http.Transport {
	if headerTimeout < 60*time.Second {
		headerTimeout = 60 * time.Second
	}
	if headerTimeout > 15*time.Minute {
		headerTimeout = 15 * time.Minute
	}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(cfg LLMConfig) *OpenAIClient {
	timeout := 60 * time.Second
	if cfg.TimeoutS > 0 {
		timeout = time.Duration(cfg.TimeoutS) * time.Second
	}
	want := cfg.MaxTokens
	if want <= 0 {
		want = defaultMaxTokens
	}
	ctxLen := contextLenFromExtra(cfg.ExtraBody)
	transport := newLLMTransport(timeout)
	var supportsJSON *bool
	if cfg.SupportsJSONSchema != nil {
		v := *cfg.SupportsJSONSchema
		supportsJSON = &v
	}
	return &OpenAIClient{
		baseURL:            cfg.APIBase,
		apiKey:             cfg.APIKey,
		model:              cfg.Model,
		provider:           cfg.Provider,
		wantMaxTokens:      want,
		maxTokens:          effectiveMaxTokens(want, ctxLen),
		contextTokens:      ctxLen,
		temperature:        cfg.Temperature,
		toolChoice:         resolveToolChoice(cfg),
		toolChoiceImplicit: strings.TrimSpace(cfg.ToolChoice) == "",
		extraBody:          normalizeExtraBody(cfg.Provider, cfg.Model, cfg.ExtraBody),
		requestTimeout:     timeout,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		// SSE connections are long-lived; context + stall watchdog handle cancellation.
		streamClient:       &http.Client{Timeout: 0, Transport: newLLMTransport(timeout)},
		logger:             nil, // Set via SetLogger if needed
		supportsJSONSchema: supportsJSON,
	}
}

// SetContextTokens applies a server-discovered (or config) context window and
// reclamps max_tokens so completion fits inside the window.
func (c *OpenAIClient) SetContextTokens(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.contextTokens = n
	c.maxTokens = effectiveMaxTokens(c.wantMaxTokens, n)
}

// ContextTokens returns the known context window (0 if unknown).
func (c *OpenAIClient) ContextTokens() int {
	if c == nil {
		return 0
	}
	return c.contextTokens
}

// DiscoverAndApplyLimits fetches max_model_len from the server and reclamps
// max_tokens. Safe to call after New; no-op on failure.
func (c *OpenAIClient) DiscoverAndApplyLimits(ctx context.Context) (ModelLimits, error) {
	if c == nil {
		return ModelLimits{}, fmt.Errorf("nil client")
	}
	lim, err := DiscoverModelLimits(ctx, LLMConfig{
		APIBase: c.baseURL,
		APIKey:  c.apiKey,
		Model:   c.model,
	})
	if err != nil {
		return lim, err
	}
	if lim.ContextTokens > 0 {
		c.SetContextTokens(lim.ContextTokens)
	}
	return lim, nil
}

// defaultMaxTokens is used when config omits max_tokens. Omitting the field on
// the wire lets vLLM fall back to generation_config max_new_tokens (often
// ~50k), which then fails against smaller --max-model-len windows.
const defaultMaxTokens = 4096

// ContextTokensFromConfig returns the context window (num_ctx) configured for
// cfg, or 0 if unset. Exported so callers that build a *named* provider client
// (e.g. providers.fast for compaction) can learn its window without
// constructing a full OpenAIClient first — see agent.Options.CompactionContextTokens.
func ContextTokensFromConfig(cfg LLMConfig) int {
	return contextLenFromExtra(cfg.ExtraBody)
}

func contextLenFromExtra(extra map[string]any) int {
	if extra == nil {
		return 0
	}
	v, ok := extra["num_ctx"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// effectiveMaxTokens ensures we always send a finite max_tokens.
// Cap completion at ~20% of the window (LM Studio-style: most of num_ctx
// stays available for prompt/history). Users who need longer completions
// can still raise max_tokens up to this ceiling in Settings.
func effectiveMaxTokens(maxTokens, contextLen int) int {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if contextLen <= 0 {
		return maxTokens
	}
	capTok := contextLen / 5 // ~20% for the answer
	if capTok < MinCompletionTokens {
		capTok = MinCompletionTokens
	}
	// Never let completion alone exceed the window minus a tiny prompt floor.
	hard := contextLen - MinCompletionTokens
	if hard < MinCompletionTokens {
		hard = MinCompletionTokens
	}
	if capTok > hard {
		capTok = hard
	}
	if maxTokens > capTok {
		return capTok
	}
	return maxTokens
}

// estimateRequestBytes sums the serialized size of messages + tool schemas
// that will go on the wire. Shared by estimateRequestTokens (client clamp)
// and exposed indirectly via EstimateTokensFromBytes for the agent side so
// both layers convert the same byte count using the same formula.
func estimateRequestBytes(req CompleteRequest) int {
	bytes := 0
	for _, m := range req.Messages {
		bytes += len(m.Content)
		for _, tc := range m.ToolCalls {
			bytes += len(tc.Function.Name)
			bytes += len(tc.Function.Arguments.Raw())
		}
		if m.ToolCallID != "" {
			bytes += len(m.ToolCallID)
		}
	}
	for _, t := range req.Tools {
		bytes += len(t.Function.Name)
		bytes += len(t.Function.Description)
		bytes += len(t.Function.Parameters)
	}
	return bytes
}

// estimateRequestTokens approximates tokens for messages + tool schemas.
// Prefer over-estimate: under-counting lets clampMaxTokensForPrompt send a
// max_tokens that vLLM rejects (prompt_tokens + max_tokens > max_model_len).
// Uses EstimateTokensFromBytes so the client's clamp and the agent's
// compaction trigger (shouldCompactHistoryEx) agree on the same conversion.
func estimateRequestTokens(req CompleteRequest) int {
	return EstimateTokensFromBytes(estimateRequestBytes(req))
}

// clampMaxTokensForPrompt picks max_tokens so promptEst + max_tokens + safety
// fits in the model context window (vLLM hard-fails otherwise).
func clampMaxTokensForPrompt(want, contextLen, promptTok int) int {
	if want <= 0 {
		want = defaultMaxTokens
	}
	if contextLen <= 0 {
		return want
	}
	room := CompletionRoom(contextLen, promptTok)
	if want > room {
		want = room
	}
	return want
}

// maxTokensForRequest returns the wire max_tokens for this completion, or an
// error when the prompt alone cannot fit in the context window.
func (c *OpenAIClient) maxTokensForRequest(req CompleteRequest) (int, error) {
	want := c.wantMaxTokens
	if want <= 0 {
		want = c.maxTokens
	}
	if want <= 0 {
		want = defaultMaxTokens
	}
	want = effectiveMaxTokens(want, c.contextTokens)
	if c.contextTokens <= 0 {
		return want, nil
	}
	promptTok := estimateRequestTokens(req)
	if promptTok >= c.contextTokens-256 {
		return 0, fmt.Errorf(
			"prompt too large (~%d tokens) for model context %d — compact history or start a new session",
			promptTok, c.contextTokens,
		)
	}
	return clampMaxTokensForPrompt(want, c.contextTokens, promptTok), nil
}

// normalizeExtraBody prepares provider-specific extras for the wire format.
//   - num_ctx is Orchestra/LM-Studio budgeting only; strip for OpenAI-compatible remotes.
//   - Qwen3 defaults to thinking-on; without an explicit flag the model may return
//     empty content. Default enable_thinking=false unless the user set it.
func normalizeExtraBody(provider, model string, extra map[string]any) map[string]any {
	if len(extra) == 0 && !looksLikeQwen(model) {
		return extra
	}
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "ollama", "lmstudio":
		// keep num_ctx
	default:
		delete(out, "num_ctx")
	}
	if looksLikeQwen(model) {
		ensureEnableThinking(out, false)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func looksLikeQwen(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "qwen")
}

func ensureEnableThinking(extra map[string]any, defaultVal bool) {
	raw, ok := extra["chat_template_kwargs"]
	if !ok || raw == nil {
		extra["chat_template_kwargs"] = map[string]any{"enable_thinking": defaultVal}
		return
	}
	switch m := raw.(type) {
	case map[string]any:
		if _, exists := m["enable_thinking"]; !exists {
			m["enable_thinking"] = defaultVal
		}
	}
}

// reservedRequestKeys must not be overwritten by extra_body (config mistake
// or accidental max_tokens: N in extra_body would blow the context window).
var reservedRequestKeys = map[string]bool{
	"model": true, "messages": true, "tools": true, "tool_choice": true,
	"max_tokens": true, "temperature": true, "stream": true, "stream_options": true,
	"response_format": true,
}

// warnImplicitToolChoiceOnce logs a single hint the first time a tool-bearing
// request is sent with an implicit (not user-configured) "omit" tool_choice —
// the setting most likely to make tool calling flaky/silently-ignored on
// self-hosted servers that DO support --enable-auto-tool-choice.
func (c *OpenAIClient) warnImplicitToolChoiceOnce(toolCount int) {
	if c == nil || c.toolChoiceWarned || !c.toolChoiceImplicit || c.toolChoice != "omit" || toolCount == 0 {
		return
	}
	c.toolChoiceWarned = true
	if c.logger != nil {
		c.logger.LogError(0, fmt.Sprintf(
			"tool_choice defaulted to \"omit\" for provider=%q model=%q (no llm.tool_choice set); "+
				"if the model ignores tools, set llm.tool_choice: auto (requires --enable-auto-tool-choice on vLLM)",
			c.provider, c.model), 0)
	}
}

// resolveToolChoice picks the tool_choice wire value.
// Self-hosted OpenAI-compatible servers (vLLM, custom, empty provider) often
// reject explicit "auto" unless started with --enable-auto-tool-choice and
// --tool-call-parser. Omitting the field keeps tools advertised without that
// requirement on many setups.
func resolveToolChoice(cfg LLMConfig) string {
	tc := strings.ToLower(strings.TrimSpace(cfg.ToolChoice))
	switch tc {
	case "omit", "off", "false", "none", "required", "auto":
		return tc
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "vllm", "custom", "ollama", "lmstudio", "":
		// Empty provider is common when only api_base is set (TUI / manual YAML).
		return "omit"
	}
	return "auto"
}

func applyToolChoice(req *chatCompletionRequest, mode string) {
	if len(req.Tools) == 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "omit", "off", "false":
		// leave ToolChoice empty → omitted from JSON
	case "none", "required":
		req.ToolChoice = strings.ToLower(mode)
	default:
		req.ToolChoice = "auto"
	}
}

// ── Retry / recovery helpers ─────────────────────────────────────────────────

// Tunables are package vars so tests can shrink them.
var (
	// llmRetryAttempts is the total number of tries for one logical request.
	llmRetryAttempts = 3
	// llmRetryBackoff is the base delay between retries (grows linearly).
	llmRetryBackoff = 1500 * time.Millisecond
	// streamStallTimeout aborts an SSE stream when no data arrives for this
	// long. Tunnelled connections (ngrok) can die silently while TCP stays up.
	streamStallTimeout = 120 * time.Second
)

// vllmCtxLenRe matches classic vLLM / OpenAI-style overflow errors, e.g.
// "maximum context length is 51200 tokens. However, you requested 53344 tokens
// (45152 in the messages, 8192 in the completion)."
var vllmCtxLenRe = regexp.MustCompile(
	`maximum context length is (\d+) tokens.*?(\d+)\s+(?:tokens\s+)?in the messages`)

// vllmCtxLenReOutIn matches newer vLLM wording:
// "maximum context length is 51200 tokens. However, you requested 12054 output
// tokens and your prompt contains at least 40000 input tokens, for a total of
// at least 52054 tokens."
var vllmCtxLenReOutIn = regexp.MustCompile(
	`maximum context length is (\d+) tokens.*?requested (\d+) output tokens.*?` +
		`(?:prompt contains at least (\d+) input tokens|total of at least (\d+) tokens)`)

// vllmCtxLenRePassed matches:
// "You passed 4 input tokens and requested 2045 output tokens. However, the
// model's context length is only 2048 tokens..."
var vllmCtxLenRePassed = regexp.MustCompile(
	`(?i)passed (\d+) input tokens and requested (\d+) output tokens.*?` +
		`context length is (?:only )?(\d+)`)

// parseContextLengthError extracts (contextLen, promptTokens) from a provider
// context-overflow error message. ok=false when the message doesn't match.
func parseContextLengthError(msg string) (ctxLen, promptTok int, ok bool) {
	if m := vllmCtxLenReOutIn.FindStringSubmatch(msg); len(m) == 5 {
		ctxLen, err1 := strconv.Atoi(m[1])
		outTok, errOut := strconv.Atoi(m[2])
		if err1 != nil || errOut != nil || ctxLen <= 0 {
			return 0, 0, false
		}
		if m[3] != "" {
			promptTok, _ = strconv.Atoi(m[3])
		} else if m[4] != "" {
			total, _ := strconv.Atoi(m[4])
			if total > outTok {
				promptTok = total - outTok
			}
		}
		if promptTok <= 0 {
			return 0, 0, false
		}
		return ctxLen, promptTok, true
	}
	if m := vllmCtxLenRe.FindStringSubmatch(msg); len(m) == 3 {
		ctxLen, err1 := strconv.Atoi(m[1])
		promptTok, err2 := strconv.Atoi(m[2])
		if err1 != nil || err2 != nil || ctxLen <= 0 || promptTok <= 0 {
			return 0, 0, false
		}
		return ctxLen, promptTok, true
	}
	if m := vllmCtxLenRePassed.FindStringSubmatch(msg); len(m) == 4 {
		promptTok, err1 := strconv.Atoi(m[1])
		ctxLen, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || ctxLen <= 0 || promptTok <= 0 {
			return 0, 0, false
		}
		return ctxLen, promptTok, true
	}
	return 0, 0, false
}

// IsTransientLLMError reports whether a request is worth retrying: network
// hiccups, tunnel drops, 429/5xx, stalled or truncated streams. Bare context
// cancellation / deadline (caller aborted the step) are permanent. Wrapped
// "SSE read error: context deadline exceeded" from a dead tunnel mid-body is
// still transient — streamStep checks ctx.Err() separately so intentional
// LLMStepTimeout does not get retried.
func IsTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	sseOrStall := strings.Contains(s, "sse read error") || strings.Contains(s, "stream stalled")
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if !sseOrStall {
			return false
		}
		// Fall through: treat wrapped stream deaths as retryable.
	}
	if code := extractStatusCode(err.Error()); code != 0 {
		return code == 429 || code == 408 || code >= 500
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return true
	}
	for _, marker := range []string{
		"connection reset", "connection refused", "broken pipe",
		"unexpected eof", "eof", "wsarecv", "forcibly closed",
		"stream stalled", "sse read error", "no choices in response",
		"stream ended without done", "bad gateway", "service unavailable",
		"gateway timeout", "tls handshake timeout", "no such host",
		"i/o timeout", "server closed idle connection",
		"timeout awaiting response headers",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// sleepBackoff waits attempt*llmRetryBackoff or until ctx is done.
func sleepBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(attempt) * llmRetryBackoff
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// fixMaxTokensFromError adjusts max_tokens after a context-overflow 400 using
// the server-reported prompt size. Returns the corrected value and ok=true
// when a retry makes sense; ok=false when the prompt alone doesn't fit.
func (c *OpenAIClient) fixMaxTokensFromError(errMsg string) (int, bool) {
	ctxLen, promptTok, ok := parseContextLengthError(errMsg)
	if !ok {
		return 0, false
	}
	// Trust the server: update our window for subsequent requests too.
	c.SetContextTokens(ctxLen)
	room := CompletionRoom(ctxLen, promptTok)
	if room <= MinCompletionTokens && promptTok+MinCompletionTokens+ContextSafetyTokens > ctxLen {
		return 0, false // prompt alone (almost) fills the window — retrying is pointless
	}
	return room, true
}

// formatAPIError enriches provider errors with actionable hints.
func formatAPIError(status int, body string) error {
	msg := strings.TrimSpace(body)
	var errorResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &errorResp); err == nil && errorResp.Error.Message != "" {
		msg = errorResp.Error.Message
	}
	if strings.Contains(msg, "enable-auto-tool-choice") || strings.Contains(msg, "tool-call-parser") {
		return fmt.Errorf("request failed (status %d): %s\n\n"+
			"vLLM: перезапусти сервер с tool calling, например для Qwen:\n"+
			"  --enable-auto-tool-choice --tool-call-parser hermes\n"+
			"(Qwen3-Coder: qwen3_xml). Либо в .orchestra.yml: llm.tool_choice: omit", status, msg)
	}
	if strings.Contains(msg, "maximum context length") || strings.Contains(msg, "max_tokens") && strings.Contains(msg, "too large") {
		return fmt.Errorf("request failed (status %d): %s\n\n"+
			"Подсказка: уменьши llm.max_tokens (или Max tokens в Settings) так, чтобы\n"+
			"prompt + max_tokens ≤ окно модели (--max-model-len). Типично max_tokens=4096–8192.", status, msg)
	}
	return fmt.Errorf("request failed (status %d): %s", status, msg)
}

// SetLogger sets the logger for this client (optional)
func (c *OpenAIClient) SetLogger(logger *Logger) {
	c.logger = logger
}

// setNgrokBypass adds the free-tier browser interstitial bypass when talking
// through an ngrok hostname (otherwise some tunnels return HTML/401).
func setNgrokBypass(req *http.Request, baseURL string) {
	u := strings.ToLower(baseURL)
	if strings.Contains(u, "ngrok") {
		req.Header.Set("ngrok-skip-browser-warning", "true")
	}
}

// GetLogger returns the logger attached to this client (may be nil).
func (c *OpenAIClient) GetLogger() *Logger {
	return c.logger
}

// mergeExtraBody merges extraBody fields into the serialized request.
// Reserved OpenAI fields already set on req are never overwritten.
func mergeExtraBody(req chatCompletionRequest, extraBody map[string]any) ([]byte, error) {
	base, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if len(extraBody) == 0 {
		return base, nil
	}
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range extraBody {
		if reservedRequestKeys[k] {
			continue
		}
		m[k] = v
	}
	return json.Marshal(m)
}

// chatCompletionRequest represents OpenAI chat completion request
type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []Message           `json:"messages"`
	Tools          []ToolDef           `json:"tools,omitempty"`
	ToolChoice     string              `json:"tool_choice,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    *float32            `json:"temperature,omitempty"`
	ResponseFormat *responseFormatWire `json:"response_format,omitempty"`
	Stream         bool                `json:"stream,omitempty"`
	StreamOptions  *streamOptions      `json:"stream_options,omitempty"`
}

// streamOptions toggles OpenAI streaming extras. include_usage asks the server
// to emit a final chunk containing the totals object (token accounting).
type streamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type responseFormatWire struct {
	Type       string          `json:"type"`
	JSONSchema *jsonSchemaSpec `json:"json_schema,omitempty"`
}

type jsonSchemaSpec struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

// chatCompletionResponse represents OpenAI chat completion response.
// The inner messageWithReasoning type captures the reasoning_content field
// that reasoning models (qwen3.6-27b, deepseek-r1) return alongside content.
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Message
			ReasoningContent string `json:"reasoning_content,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// buildChatBody assembles the wire JSON for one chat completion request.
func (c *OpenAIClient) buildChatBody(req CompleteRequest, maxTok int, stream bool) ([]byte, error) {
	reqBody := chatCompletionRequest{
		Model:     c.model,
		Messages:  req.Messages,
		MaxTokens: maxTok,
		Tools:     req.Tools,
		Stream:    stream,
	}
	if stream {
		reqBody.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if c.temperature != 0 {
		temp := c.temperature
		reqBody.Temperature = &temp
	}
	// If tools are provided, set tool_choice (or omit for vLLM compatibility).
	applyToolChoice(&reqBody, c.toolChoice)
	if rf := c.effectiveWireResponseFormat(req); rf != nil {
		reqBody.ResponseFormat = rf
	}
	return mergeExtraBody(reqBody, c.extraBody)
}

// effectiveWireResponseFormat maps CompleteRequest → wire response_format,
// honouring SupportsJSONSchema / auto-detect disable.
func (c *OpenAIClient) effectiveWireResponseFormat(req CompleteRequest) *responseFormatWire {
	rf := req.EffectiveResponseFormat()
	if rf == nil || rf.Type == "" {
		return nil
	}
	if rf.Type == "json_schema" && !c.jsonSchemaAllowed() {
		return nil
	}
	wf := &responseFormatWire{Type: rf.Type}
	if rf.Type == "json_schema" && len(rf.Schema) > 0 {
		name := rf.SchemaName
		if name == "" {
			name = "response"
		}
		wf.JSONSchema = &jsonSchemaSpec{
			Name:   name,
			Schema: json.RawMessage(rf.Schema),
			Strict: true,
		}
	}
	return wf
}

func (c *OpenAIClient) jsonSchemaAllowed() bool {
	if c == nil {
		return true
	}
	c.supportsMu.Lock()
	defer c.supportsMu.Unlock()
	if c.supportsJSONSchema == nil {
		return true // unknown → try; auto-disable on reject
	}
	return *c.supportsJSONSchema
}

// disableJSONSchema marks json_schema unsupported for this client process.
// No-op when the user explicitly set supports_json_schema: true.
func (c *OpenAIClient) disableJSONSchema(reason string) {
	if c == nil {
		return
	}
	c.supportsMu.Lock()
	defer c.supportsMu.Unlock()
	if c.supportsJSONSchema != nil && *c.supportsJSONSchema {
		return // explicit true — do not auto-disable
	}
	f := false
	c.supportsJSONSchema = &f
	if c.logger != nil {
		c.logger.LogError(400, "json_schema unsupported — omitting response_format: "+reason, 0)
	}
}

// isUnsupportedJSONSchemaError reports whether err looks like a provider
// rejecting response_format / json_schema.
func isUnsupportedJSONSchemaError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if !strings.Contains(s, "response_format") && !strings.Contains(s, "json_schema") &&
		!strings.Contains(s, "json schema") {
		return false
	}
	return strings.Contains(s, "unsupported") ||
		strings.Contains(s, "not supported") ||
		strings.Contains(s, "unknown") ||
		strings.Contains(s, "invalid") ||
		strings.Contains(s, "unexpected") ||
		strings.Contains(s, "400")
}

// requestUsedJSONSchema reports whether the outgoing request carried json_schema.
func (c *OpenAIClient) requestUsedJSONSchema(req CompleteRequest) bool {
	rf := req.EffectiveResponseFormat()
	return rf != nil && rf.Type == "json_schema" && c.jsonSchemaAllowed()
}

// chatCompletionsURL normalizes api_base into the full endpoint URL.
func (c *OpenAIClient) chatCompletionsURL() (string, error) {
	baseURL := strings.TrimSuffix(c.baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("api_base is empty")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = baseURL + "/v1"
	}
	return baseURL + "/chat/completions", nil
}

// Complete generates a single assistant turn (content and/or tool calls).
// Transient failures (network, 429/5xx, empty responses) are retried with
// backoff; a context-overflow 400 is retried once with the server-corrected
// max_tokens.
func (c *OpenAIClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	url, err := c.chatCompletionsURL()
	if err != nil {
		return nil, err
	}
	maxTok, err := c.maxTokensForRequest(req)
	if err != nil {
		return nil, err
	}
	c.warnImplicitToolChoiceOnce(len(req.Tools))

	var lastErr error
	ctxFixed := false
	schemaRetried := false
	for attempt := 1; attempt <= llmRetryAttempts; attempt++ {
		out, err := c.completeOnce(ctx, url, req, maxTok)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, err
		}
		// Capability-detect: backend rejected json_schema → disable and retry once.
		if !schemaRetried && c.requestUsedJSONSchema(req) && isUnsupportedJSONSchemaError(err) {
			schemaRetried = true
			c.disableJSONSchema(err.Error())
			continue
		}
		// Context overflow: recompute max_tokens from the server's own numbers.
		if !ctxFixed {
			if fixed, ok := c.fixMaxTokensFromError(err.Error()); ok {
				ctxFixed = true
				maxTok = fixed
				if c.logger != nil {
					c.logger.LogError(400, fmt.Sprintf("context overflow — retrying with max_tokens=%d", fixed), 0)
				}
				continue
			}
		}
		if !IsTransientLLMError(err) || attempt == llmRetryAttempts {
			return nil, err
		}
		if serr := sleepBackoff(ctx, attempt); serr != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// completeOnce performs a single non-streaming chat completion HTTP exchange.
func (c *OpenAIClient) completeOnce(ctx context.Context, url string, req CompleteRequest, maxTok int) (*CompleteResponse, error) {
	jsonData, err := c.buildChatBody(req, maxTok, false)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	requestBytes := len(jsonData)
	requestPreview := string(jsonData) // Will be sanitized in logger

	// Extract message roles for logging
	messageRoles := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		roleStr := string(msg.Role)
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			roleStr = fmt.Sprintf("%s(tool_calls=%d)", roleStr, len(msg.ToolCalls))
		}
		if msg.Role == RoleTool && msg.ToolCallID != "" {
			roleStr = fmt.Sprintf("%s(id=%s)", roleStr, truncateID(msg.ToolCallID, 12))
		}
		messageRoles = append(messageRoles, roleStr)
	}

	startTime := time.Now()
	if c.logger != nil {
		c.logger.LogRequest(url, c.model, int(c.client.Timeout.Seconds()), requestBytes, len(req.Tools), len(req.Messages), messageRoles, requestPreview)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	reqHTTP.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		reqHTTP.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	setNgrokBypass(reqHTTP, c.baseURL)

	resp, err := c.client.Do(reqHTTP)
	duration := time.Since(startTime)
	if err != nil {
		if c.logger != nil {
			c.logger.LogError(0, err.Error(), duration.Milliseconds())
		}
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if c.logger != nil {
			c.logger.LogError(resp.StatusCode, fmt.Sprintf("failed to read response: %v", err), duration.Milliseconds())
		}
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	responseBytes := len(body)
	responsePreview := string(body)

	if resp.StatusCode != http.StatusOK {
		if c.logger != nil {
			c.logger.LogError(resp.StatusCode, responsePreview, duration.Milliseconds())
		}
		return nil, formatAPIError(resp.StatusCode, string(body))
	}

	if c.logger != nil {
		c.logger.LogResponse(responseBytes, duration.Milliseconds(), responsePreview)
	}

	var apiResp chatCompletionResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := apiResp.Choices[0]
	msg := choice.Message.Message
	// Fold reasoning_content into content when the model returns blank content alongside
	// its thinking (qwen3.6-27b in LM Studio, deepseek-r1, etc.). Only fold when
	// there are no tool_calls — if the model is calling a tool, blank content is normal.
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(choice.Message.ReasoningContent) != "" && len(msg.ToolCalls) == 0 {
		msg.Content = strings.TrimSpace(choice.Message.ReasoningContent)
	}
	out := &CompleteResponse{Message: msg}
	if apiResp.Usage != nil {
		out.Usage = &TokenUsage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		}
	}
	return out, nil
}

// Plan generates a plan from LLM (same API as Complete, but with different prompt expectations)
func (c *OpenAIClient) Plan(ctx context.Context, prompt string) (string, error) {
	resp, err := c.Complete(ctx, CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// CompleteStream implements Streamer for OpenAIClient.
// It sends the request with stream:true and returns a channel of StreamEvents.
// The channel is closed when the stream ends or an error occurs.
// Setup failures (network, 429/5xx) are retried with backoff; a context
// overflow 400 is retried once with the server-corrected max_tokens. An idle
// watchdog aborts streams that stop producing data (dead ngrok tunnel) so the
// agent can retry the step instead of hanging until the step timeout.
func (c *OpenAIClient) CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error) {
	url, err := c.chatCompletionsURL()
	if err != nil {
		return nil, err
	}
	maxTok, err := c.maxTokensForRequest(req)
	if err != nil {
		return nil, err
	}

	var lastErr error
	ctxFixed := false
	schemaRetried := false
	for attempt := 1; attempt <= llmRetryAttempts; attempt++ {
		out, err := c.streamOnce(ctx, url, req, maxTok)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, err
		}
		if !schemaRetried && c.requestUsedJSONSchema(req) && isUnsupportedJSONSchemaError(err) {
			schemaRetried = true
			c.disableJSONSchema(err.Error())
			continue
		}
		if !ctxFixed {
			if fixed, ok := c.fixMaxTokensFromError(err.Error()); ok {
				ctxFixed = true
				maxTok = fixed
				if c.logger != nil {
					c.logger.LogError(400, fmt.Sprintf("context overflow — retrying stream with max_tokens=%d", fixed), 0)
				}
				continue
			}
		}
		if !IsTransientLLMError(err) || attempt == llmRetryAttempts {
			return nil, err
		}
		if serr := sleepBackoff(ctx, attempt); serr != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// streamOnce performs one streaming POST and wires the SSE parser with a
// stall watchdog.
func (c *OpenAIClient) streamOnce(ctx context.Context, url string, req CompleteRequest, maxTok int) (<-chan StreamEvent, error) {
	jsonData, err := c.buildChatBody(req, maxTok, true)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stream request: %w", err)
	}

	// Derived context lets the watchdog abort a stalled body read: cancelling
	// it forces the HTTP transport to close the connection, which unblocks
	// the scanner inside ParseSSEStream.
	streamCtx, cancelStream := context.WithCancel(ctx)

	httpReq, err := http.NewRequestWithContext(streamCtx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("failed to create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	setNgrokBypass(httpReq, c.baseURL)

	resp, err := c.streamClient.Do(httpReq)
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("failed to send stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancelStream()
		return nil, formatAPIError(resp.StatusCode, string(body))
	}

	// ParseSSEStream owns reading; we wrap its output to close the body on
	// finish and to detect stalls (no SSE data for stall duration).
	raw := ParseSSEStream(streamCtx, resp.Body)
	out := make(chan StreamEvent, 16)
	go func() {
		defer cancelStream()
		defer resp.Body.Close()
		defer close(out)
		stall := c.effectiveStreamStallTimeout()
		timer := time.NewTimer(stall)
		defer timer.Stop()
		for {
			select {
			case ev, ok := <-raw:
				if !ok {
					return
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stall)
				out <- ev
			case <-timer.C:
				// Force the transport to close the connection, then drain the
				// parser goroutine before reporting the stall.
				cancelStream()
				for range raw {
				}
				out <- StreamEvent{Kind: StreamEventError, Err: fmt.Errorf(
					"stream stalled: no data from server for %s (connection to vLLM/tunnel lost?)", stall)}
				return
			}
		}
	}()
	return out, nil
}

// effectiveStreamStallTimeout scales the idle-SSE watchdog with llm.timeout_s.
// Fixed 120s was too aggressive for large local models behind ngrok (long
// quiet gaps while the server is still generating). Cap at 5 minutes.
func (c *OpenAIClient) effectiveStreamStallTimeout() time.Duration {
	stall := streamStallTimeout
	if c == nil || c.requestTimeout <= 0 {
		return stall
	}
	if scaled := c.requestTimeout / 5; scaled > stall {
		stall = scaled
	}
	if stall > 5*time.Minute {
		stall = 5 * time.Minute
	}
	return stall
}
