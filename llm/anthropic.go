package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicAPIBase = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"

// AnthropicClient implements llm.Client for the Anthropic Messages API.
type AnthropicClient struct {
	apiKey       string
	tokenSource  func() (string, error)
	model        string
	maxTokens    int
	baseURL      string
	client       *http.Client
	streamClient *http.Client
	// thinking is the resolved thinking block, nil when not asked for.
	thinking *anthropicThinking
	// output carries the effort level for adaptive thinking.
	output *anthropicOutputConfig
	// requestTimeout is llm.timeout_s; it scales the stall watchdog.
	requestTimeout time.Duration
}

// NewAnthropicClient creates an Anthropic client from config.
func NewAnthropicClient(cfg LLMConfig) *AnthropicClient {
	timeout := 120 * time.Second
	if cfg.TimeoutS > 0 {
		timeout = time.Duration(cfg.TimeoutS) * time.Second
	}
	base := strings.TrimRight(cfg.APIBase, "/")
	if base == "" {
		base = anthropicAPIBase
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	adaptive := anthropicAdaptiveThinking(cfg.Model)
	if cfg.MaxTokens <= 0 && adaptive {
		// These models think by default, and max_tokens caps thinking and
		// answer together: 4096 cut them off mid-answer.
		maxTokens = anthropicAdaptiveMaxTokens
	}
	var thinking *anthropicThinking
	var output *anthropicOutputConfig
	if r := resolveReasoning(cfg.Reasoning, cfg.Model); r != nil {
		if adaptive {
			// 4.7 and later reject {type:"enabled"} with a 400, and 4.6
			// deprecates it: depth is the effort level, and the thinking
			// is shown (display defaults to "omitted" on the newest).
			thinking = &anthropicThinking{Type: "adaptive", Display: "summarized"}
			if e := anthropicEffort(r); e != "" {
				output = &anthropicOutputConfig{Effort: e}
			}
		} else {
			budget := r.budget()
			thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
			// max_tokens must leave room for the answer on top of the thinking
			// budget; Anthropic rejects the request otherwise. Grow it rather
			// than shrink the budget the user asked for.
			if maxTokens <= budget {
				maxTokens = budget + cfg.MaxTokens
				if cfg.MaxTokens <= 0 {
					maxTokens = budget + 4096
				}
			}
		}
	}
	return &AnthropicClient{
		apiKey:         cfg.APIKey,
		tokenSource:    cfg.TokenSource,
		model:          cfg.Model,
		maxTokens:      maxTokens,
		thinking:       thinking,
		output:         output,
		baseURL:        base,
		client:         &http.Client{Timeout: timeout},
		streamClient:   &http.Client{Timeout: 0}, // per-request ctx controls stream lifetime
		requestTimeout: timeout,
	}
}

// ── Anthropic wire types ──────────────────────────────────────────────────────

// anthropicSystemBlock is used when prompt caching is enabled.
// CompleteStream sends a slice of them as "system" to attach cache_control.
type anthropicSystemBlock struct {
	Type         string                 `json:"type"` // "text"
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicBlock
}

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	// Content is a tool_result's: its text, or its blocks when it carries
	// an image.
	Content any `json:"content,omitempty"`
	// Thinking is a thinking block's text. A pointer: the field must be sent
	// even when empty, as it is for a model that hides its thinking.
	Thinking  *string `json:"thinking,omitempty"`
	Signature string  `json:"signature,omitempty"`
	// Data is a redacted_thinking block's.
	Data         string                 `json:"data,omitempty"`
	Source       *anthropicImageSource  `json:"source,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicImageSource is an image block's source: inline base64 or a URL.
type anthropicImageSource struct {
	Type      string `json:"type"` // "base64" | "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicBlock `json:"content"`
	Usage   *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage,omitempty"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ── Complete ──────────────────────────────────────────────────────────────────

func (c *AnthropicClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	ch, err := c.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	return DrainStreamEvents(ch)
}

// CompleteStream implements Streamer for the Anthropic Messages API.
func (c *AnthropicClient) CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error) {
	// Anthropic enforces ^[a-zA-Z0-9_-]{1,128}$ on tool names; MCP tools are
	// canonically "mcp:server:tool". Rename on the wire, restore on the way back.
	nameMapper := newToolNameMapper(req.Tools)
	system, msgs := convertToAnthropic(nameMapper.WireMessages(req.Messages))

	var systemField any = system
	if system != "" {
		systemField = []anthropicSystemBlock{{
			Type:         "text",
			Text:         system,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
	}

	tools := convertTools(sanitizeWireToolSchemas(nameMapper.WireTools(req.Tools)))
	markToolsCacheBreakpoint(tools)
	markPrefixCacheBreakpoint(msgs)

	body := anthropicStreamRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    systemField,
		Messages:  msgs,
		Tools:     tools,
		Thinking:  c.thinking,
		Output:    c.output,
		Stream:    true,
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal stream request: %w", err)
	}

	// Setup failures — network, 429, 529 overloaded, 5xx — are retried here,
	// waiting out the Retry-After the API sends. They were not retried at
	// all: one overloaded answer failed the step.
	var lastErr error
	for attempt := 1; attempt <= llmRetryAttempts; attempt++ {
		out, err := c.streamOnce(ctx, jsonData, nameMapper)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if ctx.Err() != nil || !IsTransientLLMError(err) {
			return nil, err
		}
		delay, ok := retryDelay(err, attempt)
		if !ok || attempt == llmRetryAttempts {
			return nil, markRetried(err, attempt)
		}
		if werr := waitRetry(ctx, delay); werr != nil {
			return nil, err
		}
	}
	return nil, markRetried(lastErr, llmRetryAttempts)
}

// streamOnce sends one streaming request and relays its events, restoring
// tool names, under the same stall watchdog as the OpenAI-compatible client:
// a connection that stopped sending held the step until its timeout.
func (c *AnthropicClient) streamOnce(ctx context.Context, jsonData []byte, nameMapper *toolNameMapper) (<-chan StreamEvent, error) {
	// Cancelling streamCtx closes the connection: how the watchdog unblocks
	// a parser stuck on a dead one.
	streamCtx, cancelStream := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(streamCtx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("anthropic: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	cred, err := resolveBearer(c.tokenSource, c.apiKey)
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("anthropic: resolve credential for %s: %w", c.baseURL, err)
	}
	httpReq.Header.Set("x-api-key", cred)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := c.streamClient.Do(httpReq)
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("anthropic: send stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancelStream()
		var errResp anthropicResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, newStatusError(resp.StatusCode, resp.Header, fmt.Sprintf("anthropic API error (status %d): %s", resp.StatusCode, errResp.Error.Message))
		}
		return nil, newStatusError(resp.StatusCode, resp.Header, fmt.Sprintf("anthropic API status %d: %s", resp.StatusCode, string(respBody)))
	}

	raw := ParseAnthropicSSEStream(streamCtx, resp.Body)
	done := func() {
		cancelStream()
		resp.Body.Close()
	}
	return relayStream(raw, stallTimeoutFor(c.requestTimeout), cancelStream, done, func(ev *StreamEvent) {
		if nameMapper == nil {
			return
		}
		if ev.Kind == StreamEventToolCallStart {
			ev.ToolCallName = nameMapper.Restore(ev.ToolCallName)
		}
		if ev.Kind == StreamEventDone {
			nameMapper.RestoreResponse(ev.Response)
		}
	}), nil
}

// Plan implements llm.Client (same as Complete with a simple user message).
func (c *AnthropicClient) Plan(ctx context.Context, prompt string) (string, error) {
	resp, err := c.Complete(ctx, CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// ── Message conversion: OpenAI → Anthropic ───────────────────────────────────

func convertToAnthropic(messages []Message) (system string, out []anthropicMessage) {
	var sysBlocks []string
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			sysBlocks = append(sysBlocks, msg.Content)
		case RoleUser:
			// Parts are the message when it has them — images, and with
			// --image the query's own text. Reading Content alone dropped
			// both (LLM-7).
			var content any = msg.Content
			if len(msg.Parts) > 0 {
				content = partsToAnthropic(msg.Parts)
			}
			// The agent appends a volatile block (working state, todos) as its
			// own user message after the tool results. Anthropic requires
			// alternating roles, so fold it into the preceding user message.
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(
					userContentBlocks(out[len(out)-1].Content),
					userContentBlocks(content)...,
				)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: content})
		case RoleAssistant:
			// Thinking first, as the model produced it: within a tool-use
			// turn the API wants the blocks back unmodified, in order,
			// before the tool_use they led to (LLM-6).
			blocks := thinkingToAnthropic(msg.Thinking)
			if msg.Content != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				input := tc.Function.Arguments.Raw()
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropicBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			if len(blocks) == 0 {
				blocks = []anthropicBlock{{Type: "text", Text: ""}}
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})
		case RoleTool:
			// Tool results must be user messages with tool_result blocks.
			// Group consecutive tool messages into one user message.
			block := anthropicBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
			}
			switch {
			case len(msg.Parts) > 0:
				block.Content = partsToAnthropic(msg.Parts)
			case msg.Content != "":
				block.Content = msg.Content
			}
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(userContentBlocks(out[len(out)-1].Content), block)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{block}})
		}
	}
	system = strings.Join(sysBlocks, "\n\n")
	return system, out
}

// ── Response conversion: Anthropic → OpenAI ──────────────────────────────────

func convertFromAnthropic(blocks []anthropicBlock) Message {
	msg := Message{Role: RoleAssistant}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			msg.Content += b.Text
		case "tool_use":
			args := ToolArguments(b.Input)
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: ToolCallFunc{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
	}
	return msg
}

// ── Tool conversion ───────────────────────────────────────────────────────────

func convertTools(defs []ToolDef) []anthropicTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(defs))
	for _, d := range defs {
		schema := d.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, anthropicTool{
			Name:        d.Function.Name,
			Description: d.Function.Description,
			InputSchema: schema,
		})
	}
	return out
}

// userContentBlocks normalises a user message's content (string or block list)
// into a block list so another block can be appended to it.
func userContentBlocks(content any) []anthropicBlock {
	switch v := content.(type) {
	case []anthropicBlock:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []anthropicBlock{{Type: "text", Text: v}}
	case nil:
		return nil
	default:
		return nil
	}
}

// thinkingToAnthropic renders a message's thinking blocks for the wire.
func thinkingToAnthropic(in []ThinkingBlock) []anthropicBlock {
	out := make([]anthropicBlock, 0, len(in))
	for _, t := range in {
		if t.Redacted != "" {
			out = append(out, anthropicBlock{Type: "redacted_thinking", Data: t.Redacted})
			continue
		}
		text := t.Text
		out = append(out, anthropicBlock{Type: "thinking", Thinking: &text, Signature: t.Signature})
	}
	return out
}

// partsToAnthropic renders multimodal parts as text and image blocks.
func partsToAnthropic(parts []ContentPart) []anthropicBlock {
	out := make([]anthropicBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Kind {
		case PartText:
			b := anthropicBlock{Type: "text", Text: p.Text}
			if p.CacheControl {
				b.CacheControl = &anthropicCacheControl{Type: "ephemeral"}
			}
			out = append(out, b)
		case PartImage:
			if src := anthropicImage(p); src != nil {
				out = append(out, anthropicBlock{Type: "image", Source: src})
			}
		}
	}
	return out
}

// anthropicImage is an image part's source: its bytes as base64, a data: URI
// unpacked the same way, or a remote URL.
func anthropicImage(p ContentPart) *anthropicImageSource {
	if len(p.ImageData) > 0 {
		mime := p.ImageMIME
		if mime == "" {
			mime = "image/png"
		}
		return &anthropicImageSource{Type: "base64", MediaType: mime, Data: base64.StdEncoding.EncodeToString(p.ImageData)}
	}
	u := strings.TrimSpace(p.ImageURL)
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		meta, data, found := strings.Cut(rest, ",")
		mime, isB64 := strings.CutSuffix(meta, ";base64")
		if !found || !isB64 || mime == "" {
			return nil
		}
		return &anthropicImageSource{Type: "base64", MediaType: mime, Data: data}
	}
	if u == "" {
		return nil
	}
	return &anthropicImageSource{Type: "url", URL: u}
}

// markToolsCacheBreakpoint caches the tool schemas, which are identical on
// every step of an agent run and are several KB of prompt.
func markToolsCacheBreakpoint(tools []anthropicTool) {
	if len(tools) == 0 {
		return
	}
	tools[len(tools)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
}

// markPrefixCacheBreakpoint caches the conversation up to (but not including)
// the last message.
//
// The agent rebuilds volatile context — working state, todos, reminders — on
// every step and appends it last, so the last message is the one part of the
// prompt that reliably differs between steps. Everything before it is a stable,
// append-only prefix: putting the breakpoint there makes each step read the
// previous step's history from cache and write only what was appended, instead
// of re-paying for the whole transcript.
func markPrefixCacheBreakpoint(msgs []anthropicMessage) {
	if len(msgs) < 2 {
		return
	}
	m := &msgs[len(msgs)-2]
	blocks := userContentBlocks(m.Content)
	if len(blocks) == 0 {
		if arr, ok := m.Content.([]anthropicBlock); ok {
			blocks = arr
		}
	}
	// Thinking blocks cannot carry cache_control: mark the last other one.
	for i := len(blocks) - 1; i >= 0; i-- {
		if t := blocks[i].Type; t == "thinking" || t == "redacted_thinking" {
			continue
		}
		blocks[i].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		m.Content = blocks
		return
	}
}
