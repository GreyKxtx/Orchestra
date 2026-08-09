package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// sseDebugWriter, when non-nil, receives every raw SSE "data: …" line so we can
// inspect what the provider actually sends. Activated when ORCH_STREAM_DEBUG is
// non-empty: it points at a path that will be appended to.
var (
	sseDebugMu     sync.Mutex
	sseDebugWriter *os.File
	sseDebugOnce   sync.Once
)

func sseDebugLog(line string) {
	sseDebugOnce.Do(func() {
		path := os.Getenv("ORCH_STREAM_DEBUG")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			sseDebugWriter = f
		}
	})
	if sseDebugWriter == nil {
		return
	}
	sseDebugMu.Lock()
	defer sseDebugMu.Unlock()
	_, _ = sseDebugWriter.WriteString(line + "\n")
}

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
	// StreamEventExecOutput carries an incremental chunk of exec.run stdout/stderr output.
	StreamEventExecOutput StreamEventKind = "exec_output"
	// StreamEventToolCallCompleted is emitted by the agent loop after a tools.Call returns.
	// Content holds a short result preview (truncated to 256 bytes).
	// ToolCallID/ToolCallName identify the call.
	StreamEventToolCallCompleted StreamEventKind = "tool_call_completed"
	// StreamEventStepDone is emitted at the end of each agent loop iteration.
	// Content holds the reason: "tool_call", "final", "invalid", "compaction".
	StreamEventStepDone StreamEventKind = "step_done"
	// StreamEventPendingOps is emitted when the agent produces final patches (dry-run or pre-apply).
	// Content holds a JSON-encoded {ops: [...], diff: "...", applied: bool} payload.
	StreamEventPendingOps StreamEventKind = "pending_ops"
	// StreamEventStepUsage carries per-LLM-step token totals (prompt/completion).
	StreamEventStepUsage StreamEventKind = "step_usage"
	// StreamEventTodosUpdated carries the full todo list after a successful todowrite.
	// Content is a JSON array of todo items.
	StreamEventTodosUpdated StreamEventKind = "todos_updated"
	// StreamEventRecoverableError is emitted when a non-fatal error (StaleContent, AmbiguousMatch,
	// schema validation failure) occurs and the loop will retry. Content holds a short message.
	StreamEventRecoverableError StreamEventKind = "recoverable_error"
	// StreamEventReasoningDelta carries chain-of-thought tokens from a dedicated
	// provider field (reasoning_content / thinking). Distinct from message_delta
	// so the TUI can append to Reasoning without think-tag parsing.
	StreamEventReasoningDelta StreamEventKind = "reasoning_delta"
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

	// Diagnostics: JSON array of LSP tool diagnostics (write/edit completed events).
	Diagnostics json.RawMessage
}

// sseChunk is the JSON payload of one SSE "data:" line in OpenAI-compatible streaming.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			// ReasoningContent / ThinkingContent carry chain-of-thought text emitted
			// by reasoning models (Qwen3, DeepSeek-R1 via LM Studio, etc.) as a
			// dedicated field instead of inline <think> tags.
			ReasoningContent string `json:"reasoning_content"`
			ThinkingContent  string `json:"thinking_content"`
			ToolCalls        []struct {
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
	// Usage is sent in the final SSE chunk when stream_options.include_usage=true.
	// Most chunks omit it; only the last one carries totals.
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
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

		// inReasoning tracks dedicated reasoning_content / thinking_content.
		// Accumulator still wraps with <think> for BuildResponse history;
		// the TUI receives StreamEventReasoningDelta separately.
		inReasoning := false

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
			sseDebugLog(data)
			if strings.TrimSpace(data) == "[DONE]" {
				if inReasoning {
					acc.AppendContent("</think>")
				}
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
			if chunk.Usage != nil {
				acc.SetUsage(&TokenUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				})
			}
			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta

			// Dedicated reasoning field (Qwen3, DeepSeek-R1 via LM Studio, etc.).
			// Emit reasoning_delta for the TUI; also wrap in <think> in the
			// accumulator so BuildResponse / history stay consistent with
			// models that embed think tags in content.
			rc := delta.ReasoningContent
			if rc == "" {
				rc = delta.ThinkingContent
			}
			if rc != "" {
				accRC := rc
				if !inReasoning {
					accRC = "<think>" + rc
					inReasoning = true
				}
				acc.AppendContent(accRC)
				ch <- StreamEvent{Kind: StreamEventReasoningDelta, Content: rc}
			}

			if delta.Content != "" {
				if inReasoning {
					// Close the think block before the actual answer starts.
					acc.AppendContent("</think>")
					inReasoning = false
				}
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
		if inReasoning {
			acc.AppendContent("</think>")
		}
		ch <- StreamEvent{Kind: StreamEventDone, Response: acc.BuildResponse()}
	}()
	return ch
}
