package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type anthropicStreamRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    any                `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
}

type anthropicStreamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"content_block,omitempty"`
	Delta struct {
		Type         string `json:"type"`
		Text         string `json:"text,omitempty"`
		PartialJSON  string `json:"partial_json,omitempty"`
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta,omitempty"`
	Message struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ParseAnthropicSSEStream reads Anthropic Messages API SSE (event:/data: pairs)
// and emits Orchestra StreamEvents on the returned channel.
func ParseAnthropicSSEStream(ctx context.Context, body io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)

		acc := newToolCallAccumulator()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

		var pendingEvent string
		toolIndex := -1
		toolIndices := make(map[int]int) // anthropic block index → accumulator index

		emitDone := func() {
			resp := acc.BuildResponse()
			ch <- StreamEvent{Kind: StreamEventDone, Response: resp}
		}

		for scanner.Scan() {
			if ctx.Err() != nil {
				ch <- StreamEvent{Kind: StreamEventError, Err: ctx.Err()}
				return
			}
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				pendingEvent = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if strings.TrimSpace(data) == "" {
				continue
			}

			var ev anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			if ev.Error.Message != "" {
				ch <- StreamEvent{Kind: StreamEventError, Err: fmt.Errorf("anthropic stream: %s", ev.Error.Message)}
				return
			}

			switch ev.Type {
			case "content_block_start":
				block := ev.ContentBlock
				switch block.Type {
				case "tool_use":
					toolIndex++
					toolIndices[ev.Index] = toolIndex
					isNew, name, id := acc.FeedToolCall(toolIndex, block.ID, block.Name, "")
					if isNew {
						ch <- StreamEvent{
							Kind:          StreamEventToolCallStart,
							ToolCallIndex: toolIndex,
							ToolCallID:    id,
							ToolCallName:  name,
						}
					}
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						acc.AppendContent(ev.Delta.Text)
						ch <- StreamEvent{Kind: StreamEventMessageDelta, Content: ev.Delta.Text}
					}
				case "input_json_delta":
					idx, ok := toolIndices[ev.Index]
					if !ok {
						idx = ev.Index
					}
					if ev.Delta.PartialJSON != "" {
						acc.FeedToolCall(idx, "", "", ev.Delta.PartialJSON)
						ch <- StreamEvent{
							Kind:          StreamEventToolCallDelta,
							ToolCallIndex: idx,
							ArgsDelta:     ev.Delta.PartialJSON,
						}
					}
				}
			case "message_delta":
				if ev.Usage != nil {
					acc.usage = &TokenUsage{
						PromptTokens:     ev.Usage.InputTokens,
						CompletionTokens: ev.Usage.OutputTokens,
						TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
					}
				} else if ev.Message.Usage != nil {
					acc.usage = &TokenUsage{
						PromptTokens:     ev.Message.Usage.InputTokens,
						CompletionTokens: ev.Message.Usage.OutputTokens,
						TotalTokens:      ev.Message.Usage.InputTokens + ev.Message.Usage.OutputTokens,
					}
				}
			case "message_stop":
				emitDone()
				return
			case "error":
				if ev.Error.Message != "" {
					ch <- StreamEvent{Kind: StreamEventError, Err: fmt.Errorf("anthropic stream: %s", ev.Error.Message)}
					return
				}
			default:
				_ = pendingEvent
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Kind: StreamEventError, Err: err}
			return
		}
		emitDone()
	}()
	return ch
}
