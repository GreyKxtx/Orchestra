package llm

import (
	"bytes"
	"context"
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
	model        string
	maxTokens    int
	baseURL      string
	client       *http.Client
	streamClient *http.Client
	// thinking is the resolved extended-thinking block, nil when off.
	thinking *anthropicThinking
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
	var thinking *anthropicThinking
	if r := resolveReasoning(cfg.Reasoning, cfg.Model); r != nil {
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
	return &AnthropicClient{
		apiKey:       cfg.APIKey,
		model:        cfg.Model,
		maxTokens:    maxTokens,
		thinking:     thinking,
		baseURL:      base,
		client:       &http.Client{Timeout: timeout},
		streamClient: &http.Client{Timeout: 0}, // per-request ctx controls stream lifetime
	}
}

// ── Anthropic wire types ──────────────────────────────────────────────────────

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    any                `json:"system,omitempty"` // string OR []anthropicSystemBlock
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

// anthropicSystemBlock is used when prompt caching is enabled.
// Pass as []anthropicSystemBlock to System to attach cache_control.
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
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        json.RawMessage        `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      string                 `json:"content,omitempty"` // tool_result text
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
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
		Stream:    true,
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := c.streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var errResp anthropicResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API status %d: %s", resp.StatusCode, string(respBody))
	}

	raw := ParseAnthropicSSEStream(ctx, resp.Body)
	out := make(chan StreamEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		for ev := range raw {
			if nameMapper != nil {
				if ev.Kind == StreamEventToolCallStart {
					ev.ToolCallName = nameMapper.Restore(ev.ToolCallName)
				}
				if ev.Kind == StreamEventDone {
					nameMapper.RestoreResponse(ev.Response)
				}
			}
			out <- ev
		}
	}()
	return out, nil
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
			// The agent appends a volatile block (working state, todos) as its
			// own user message after the tool results. Anthropic requires
			// alternating roles, so fold it into the preceding user message.
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(
					userContentBlocks(out[len(out)-1].Content),
					anthropicBlock{Type: "text", Text: msg.Content},
				)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: msg.Content})
		case RoleAssistant:
			var blocks []anthropicBlock
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
				Content:   msg.Content,
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
	if len(blocks) == 0 {
		return
	}
	blocks[len(blocks)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	m.Content = blocks
}
