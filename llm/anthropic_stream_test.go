package llm

import (
	"context"
	"strings"
	"testing"
)

func TestParseAnthropicSSEStream_TextDelta(t *testing.T) {
	fixture := strings.NewReader(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\"}}\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n",
	)

	events := collectEvents(ParseAnthropicSSEStream(context.Background(), fixture))
	var deltas int
	var done *CompleteResponse
	for _, ev := range events {
		switch ev.Kind {
		case StreamEventMessageDelta:
			deltas++
		case StreamEventDone:
			done = ev.Response
		}
	}
	if deltas != 1 {
		t.Fatalf("expected 1 message delta, got %d", deltas)
	}
	if done == nil || done.Message.Content != "Hello" {
		t.Fatalf("done response: %#v", done)
	}
}

func TestParseAnthropicSSEStream_ToolUse(t *testing.T) {
	fixture := strings.NewReader(
		"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"main.go\\\"}\"}}\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n",
	)

	events := collectEvents(ParseAnthropicSSEStream(context.Background(), fixture))
	var starts, deltas int
	var done *CompleteResponse
	for _, ev := range events {
		switch ev.Kind {
		case StreamEventToolCallStart:
			starts++
			if ev.ToolCallName != "read" {
				t.Fatalf("tool name=%q", ev.ToolCallName)
			}
		case StreamEventToolCallDelta:
			deltas++
		case StreamEventDone:
			done = ev.Response
		}
	}
	if starts != 1 || deltas != 1 {
		t.Fatalf("starts=%d deltas=%d", starts, deltas)
	}
	if done == nil || len(done.Message.ToolCalls) != 1 {
		t.Fatalf("done: %#v", done)
	}
	if got := string(done.Message.ToolCalls[0].Function.Arguments.Raw()); got != `{"path":"main.go"}` {
		t.Fatalf("args=%q", got)
	}
}

// A stream cut after a text delta — no message_delta stop reason, no
// message_stop — is a retryable error, not the model's answer (LLM-8).
func TestParseAnthropicSSEStream_CutOffIsAnError(t *testing.T) {
	fixture := strings.NewReader(
		"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"I will now\"}}\n",
	)
	events := collectEvents(ParseAnthropicSSEStream(context.Background(), fixture))
	last := events[len(events)-1]
	if last.Kind != StreamEventError || !IsTransientLLMError(last.Err) {
		t.Fatalf("a cut-off stream is a retryable error, got %+v", last)
	}
}

// The stop reason travels with the answer, normalised: max_tokens says the
// answer was cut by the budget, not finished.
func TestParseAnthropicSSEStream_StopReason(t *testing.T) {
	fixture := strings.NewReader(
		"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n",
		// no message_stop: the stop reason alone says the answer ended
	)
	events := collectEvents(ParseAnthropicSSEStream(context.Background(), fixture))
	last := events[len(events)-1]
	if last.Kind != StreamEventDone || last.Response.StopReason != StopMaxTokens {
		t.Fatalf("stop reason: %+v", last)
	}
}
