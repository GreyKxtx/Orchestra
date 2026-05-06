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

	"github.com/orchestra/orchestra/internal/config"
)

const anthropicAPIBase = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"

// AnthropicClient implements llm.Client for the Anthropic Messages API.
type AnthropicClient struct {
	apiKey    string
	model     string
	maxTokens int
	baseURL   string
	client    *http.Client
}

// NewAnthropicClient creates an Anthropic client from config.
func NewAnthropicClient(cfg config.LLMConfig) *AnthropicClient {
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
	return &AnthropicClient{
		apiKey:    cfg.APIKey,
		model:     cfg.Model,
		maxTokens: maxTokens,
		baseURL:   base,
		client:    &http.Client{Timeout: timeout},
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
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	Content    string          `json:"content,omitempty"` // tool_result text
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	Content []anthropicBlock `json:"content"`
	Error   struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ── Complete ──────────────────────────────────────────────────────────────────

func (c *AnthropicClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	system, msgs := convertToAnthropic(req.Messages)

	// Use structured system blocks with cache_control so Anthropic can cache the
	// system prompt across turns. Cache writes cost ~25% more, but reads save ~90%.
	// Over a 24-step session this is a net win from step 2 onward.
	var systemField any = system
	if system != "" {
		systemField = []anthropicSystemBlock{{
			Type:         "text",
			Text:         system,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
	}

	body := anthropicRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    systemField,
		Messages:  msgs,
		Tools:     convertTools(req.Tools),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp anthropicResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: parse response: %w", err)
	}
	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("anthropic error: %s", apiResp.Error.Message)
	}

	msg := convertFromAnthropic(apiResp.Content)
	return &CompleteResponse{Message: msg}, nil
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
				if arr, ok := out[len(out)-1].Content.([]anthropicBlock); ok && len(arr) > 0 && arr[0].Type == "tool_result" {
					out[len(out)-1].Content = append(arr, block)
					continue
				}
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
