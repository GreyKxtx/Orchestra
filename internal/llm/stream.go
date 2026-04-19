package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Streamer is a streaming interface for LLM providers that support SSE.
// It deliberately does NOT embed Client so that existing test mocks (which only
// implement Client) continue to compile without modification.
type Streamer interface {
	CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error)
}

// StreamEventKind identifies the type of a StreamEvent.
type StreamEventKind string

const (
	// StreamEventMessageDelta carries one token of assistant text content.
	StreamEventMessageDelta StreamEventKind = "message_delta"
	// StreamEventToolCallStart signals a new tool call; name and id are now known.
	StreamEventToolCallStart StreamEventKind = "tool_call_start"
	// StreamEventToolCallDelta carries more argument bytes for an in-progress call.
	StreamEventToolCallDelta StreamEventKind = "tool_call_delta"
	// StreamEventDone signals end of stream; Response holds the full assembled message.
	StreamEventDone StreamEventKind = "done"
	// StreamEventError signals a stream-level error.
	StreamEventError StreamEventKind = "error"
)

// StreamEvent is one event emitted during a streaming completion.
type StreamEvent struct {
	Kind StreamEventKind

	// MessageDelta: a chunk of assistant text.
	Content string

	// ToolCallStart / ToolCallDelta: tool call identification.
	// ToolCallID is always the value stored in the accumulator — stable across all chunks
	// even when the provider omits it on subsequent deltas (OpenAI only sends id once).
	ToolCallID    string
	ToolCallIndex int
	ToolCallName  string
	ArgsDelta     string

	// Done: the full assembled CompleteResponse.
	Response *CompleteResponse

	// Error: non-nil on StreamEventError.
	Err error
}

// sseChunk is the JSON payload of one SSE "data:" line in OpenAI-compatible streaming.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ParseSSEStream reads an OpenAI-compatible SSE response body and emits StreamEvents
// on the returned buffered channel, which is closed when the stream ends.
// body is NOT closed by this function — the caller is responsible.
//
// Context cancellation is checked between lines. A blocking Scan() during a slow
// provider will not interrupt immediately; it unblocks when the HTTP transport closes
// the body on context cancellation or when the next chunk arrives.
func ParseSSEStream(ctx context.Context, body io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)

		acc := newToolCallAccumulator()
		scanner := bufio.NewScanner(body)
		// Raise the line buffer to 8 MB so chunky arguments (e.g., a full file in
		// file.write_atomic) don't exceed bufio's default 64 KB line limit.
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

		for scanner.Scan() {
			if ctx.Err() != nil {
				ch <- StreamEvent{Kind: StreamEventError, Err: ctx.Err()}
				return
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue // skip comment lines, event: lines, empty lines
			}
			data := strings.TrimPrefix(line, "data: ")
			if strings.TrimSpace(data) == "[DONE]" {
				ch <- StreamEvent{Kind: StreamEventDone, Response: acc.BuildResponse()}
				return
			}

			var chunk sseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // skip malformed lines; some proxies inject non-JSON comments
			}
			if chunk.Error.Message != "" {
				ch <- StreamEvent{Kind: StreamEventError, Err: fmt.Errorf("stream error: %s", chunk.Error.Message)}
				return
			}
			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta

			if delta.Content != "" {
				acc.AppendContent(delta.Content)
				ch <- StreamEvent{Kind: StreamEventMessageDelta, Content: delta.Content}
			}

			for _, tc := range delta.ToolCalls {
				isNew, name, id := acc.FeedToolCall(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
				if isNew {
					ch <- StreamEvent{
						Kind:          StreamEventToolCallStart,
						ToolCallIndex: tc.Index,
						ToolCallID:    id,
						ToolCallName:  name,
					}
				}
				if tc.Function.Arguments != "" {
					ch <- StreamEvent{
						Kind:          StreamEventToolCallDelta,
						ToolCallIndex: tc.Index,
						ToolCallID:    id, // always from accumulator; stable even when tc.ID is empty
						ArgsDelta:     tc.Function.Arguments,
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamEvent{Kind: StreamEventError, Err: fmt.Errorf("SSE read error: %w", err)}
			return
		}
		// Scanner exhausted without a [DONE] line — some proxies strip it.
		ch <- StreamEvent{Kind: StreamEventDone, Response: acc.BuildResponse()}
	}()
	return ch
}
