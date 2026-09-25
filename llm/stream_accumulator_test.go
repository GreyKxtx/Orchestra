package llm

import (
	"context"
	"strings"
	"testing"
)

// collectEvents drains a StreamEvent channel and returns all events.
func collectEvents(ch <-chan StreamEvent) []StreamEvent {
	var out []StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestParseSSEStream_OpenAICloud replays a realistic OpenAI cloud SSE response
// where the tool call arguments are split across five separate chunks.
func TestParseSSEStream_OpenAICloud(t *testing.T) {
	// arguments parts: {"pa + th + ":"main.go"} → {"path":"main.go"}
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\",\"type\":\"function\",\"function\":{\"name\":\"fs.read\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"pa\"}}]},\"finish_reason\":null}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"th\"}}]},\"finish_reason\":null}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\":\\\"main.go\\\"}\"}}]},\"finish_reason\":null}]}\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n" +
			"data: [DONE]\n",
	)

	events := collectEvents(ParseSSEStream(context.Background(), fixture))

	// Expect: ToolCallStart (1) + ToolCallDelta (3) + Done (1) = 5
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}

	if events[0].Kind != StreamEventToolCallStart {
		t.Errorf("event[0] kind: want ToolCallStart, got %v", events[0].Kind)
	}
	if events[0].ToolCallName != "fs.read" {
		t.Errorf("event[0] name: want fs.read, got %q", events[0].ToolCallName)
	}
	if events[0].ToolCallID != "call_abc" {
		t.Errorf("event[0] id: want call_abc, got %q", events[0].ToolCallID)
	}

	for i := 1; i <= 3; i++ {
		if events[i].Kind != StreamEventToolCallDelta {
			t.Errorf("event[%d] kind: want ToolCallDelta, got %v", i, events[i].Kind)
		}
		// id must come from the accumulator, not from tc.ID (which is empty on later chunks)
		if events[i].ToolCallID != "call_abc" {
			t.Errorf("event[%d] id: want call_abc, got %q", i, events[i].ToolCallID)
		}
	}

	done := events[4]
	if done.Kind != StreamEventDone {
		t.Fatalf("event[4] kind: want Done, got %v", done.Kind)
	}
	if done.Response == nil {
		t.Fatal("Done.Response is nil")
	}
	if len(done.Response.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: want 1, got %d", len(done.Response.Message.ToolCalls))
	}
	tc := done.Response.Message.ToolCalls[0]
	if tc.Function.Name != "fs.read" {
		t.Errorf("tool name: want fs.read, got %q", tc.Function.Name)
	}
	wantArgs := `{"path":"main.go"}`
	gotArgs := string(tc.Function.Arguments.Raw())
	if gotArgs != wantArgs {
		t.Errorf("tool args: want %q, got %q", wantArgs, gotArgs)
	}
}

// TestParseSSEStream_VLLMOneShot replays a vLLM-style response where the entire
// tool call (name + full arguments) arrives in a single chunk.
func TestParseSSEStream_VLLMOneShot(t *testing.T) {
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_xyz\",\"type\":\"function\",\"function\":{\"name\":\"fs.list\",\"arguments\":\"{\\\"path\\\":\\\".\\\",\\\"depth\\\":2}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n" +
			"data: [DONE]\n",
	)

	events := collectEvents(ParseSSEStream(context.Background(), fixture))

	// Expect: ToolCallStart + ToolCallDelta + Done = 3
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Kind != StreamEventToolCallStart {
		t.Errorf("event[0] kind: want ToolCallStart, got %v", events[0].Kind)
	}
	if events[0].ToolCallName != "fs.list" {
		t.Errorf("event[0] name: want fs.list, got %q", events[0].ToolCallName)
	}
	if events[1].Kind != StreamEventToolCallDelta {
		t.Errorf("event[1] kind: want ToolCallDelta, got %v", events[1].Kind)
	}
	wantArgs := `{"path":".","depth":2}`
	if events[1].ArgsDelta != wantArgs {
		t.Errorf("event[1] argsDelta: want %q, got %q", wantArgs, events[1].ArgsDelta)
	}
	if events[2].Kind != StreamEventDone {
		t.Fatalf("event[2] kind: want Done, got %v", events[2].Kind)
	}
	if events[2].Response == nil || len(events[2].Response.Message.ToolCalls) != 1 {
		t.Fatal("Done.Response missing tool call")
	}
	got := events[2].Response.Message.ToolCalls[0]
	if got.Function.Name != "fs.list" {
		t.Errorf("tool name: want fs.list, got %q", got.Function.Name)
	}
}

// TestParseSSEStream_NonStreamingFallback replays the case where a provider
// sends the complete response in one SSE data line (non-streaming emulation).
func TestParseSSEStream_NonStreamingFallback(t *testing.T) {
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_fb\",\"type\":\"function\",\"function\":{\"name\":\"search.text\",\"arguments\":\"{\\\"query\\\":\\\"TODO\\\",\\\"path\\\":\\\".\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n" +
			"data: [DONE]\n",
	)

	events := collectEvents(ParseSSEStream(context.Background(), fixture))

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	done := events[2]
	if done.Kind != StreamEventDone {
		t.Fatalf("last event: want Done, got %v", done.Kind)
	}
	if done.Response == nil || len(done.Response.Message.ToolCalls) != 1 {
		t.Fatal("Done.Response missing tool call")
	}
	tc := done.Response.Message.ToolCalls[0]
	if tc.Function.Name != "search.text" {
		t.Errorf("tool name: want search.text, got %q", tc.Function.Name)
	}
	wantArgs := `{"query":"TODO","path":"."}`
	gotArgs := string(tc.Function.Arguments.Raw())
	if gotArgs != wantArgs {
		t.Errorf("tool args: want %q, got %q", wantArgs, gotArgs)
	}
}

// TestParseSSEStream_NoDONE replays a stream that ends without a [DONE] terminator
// (some proxies strip it). The parser must synthesise a Done event from scanner EOF.
// A proxy that strips [DONE] still passes the last chunk's finish_reason:
// the answer is whole.
func TestParseSSEStream_NoDONE(t *testing.T) {
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n",
		// no [DONE]
	)

	events := collectEvents(ParseSSEStream(context.Background(), fixture))

	// Expect: MessageDelta × 2 + Done = 3
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Kind != StreamEventMessageDelta || events[0].Content != "Hello " {
		t.Errorf("event[0]: want MessageDelta 'Hello ', got %+v", events[0])
	}
	if events[1].Kind != StreamEventMessageDelta || events[1].Content != "world" {
		t.Errorf("event[1]: want MessageDelta 'world', got %+v", events[1])
	}
	done := events[2]
	if done.Kind != StreamEventDone {
		t.Fatalf("event[2] kind: want Done, got %v", done.Kind)
	}
	if done.Response == nil {
		t.Fatal("Done.Response is nil")
	}
	if done.Response.Message.Content != "Hello world" {
		t.Errorf("assembled content: want 'Hello world', got %q", done.Response.Message.Content)
	}
}

// TestParseSSEStream_MultiToolCall checks that two parallel tool calls are
// assembled correctly when their chunks interleave by index. Both calls are
// surfaced as separate start/delta events so the agent layer can either fan
// them out concurrently (when ParallelSafe) or fall back to serial.
func TestParseSSEStream_MultiToolCall(t *testing.T) {
	// Two tool calls at index 0 and 1; arguments arrive in later chunks.
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"fs.read\",\"arguments\":\"\"}}]}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"c2\",\"type\":\"function\",\"function\":{\"name\":\"fs.list\",\"arguments\":\"\"}}]}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"a.go\\\"}\"}}]}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"{\\\"path\\\":\\\".\\\"}\"}}]}}]}\n" +
			"data: [DONE]\n",
	)

	events := collectEvents(ParseSSEStream(context.Background(), fixture))

	// Expect: ToolCallStart×2 + ToolCallDelta×2 + Done = 5
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}
	if events[0].Kind != StreamEventToolCallStart || events[0].ToolCallName != "fs.read" {
		t.Errorf("event[0]: want ToolCallStart fs.read, got %+v", events[0])
	}
	if events[1].Kind != StreamEventToolCallStart || events[1].ToolCallName != "fs.list" {
		t.Errorf("event[1]: want ToolCallStart fs.list, got %+v", events[1])
	}
	if events[2].Kind != StreamEventToolCallDelta || events[2].ToolCallIndex != 0 {
		t.Errorf("event[2]: want ToolCallDelta index=0, got %+v", events[2])
	}
	if events[3].Kind != StreamEventToolCallDelta || events[3].ToolCallIndex != 1 {
		t.Errorf("event[3]: want ToolCallDelta index=1, got %+v", events[3])
	}

	done := events[4]
	if done.Kind != StreamEventDone || done.Response == nil {
		t.Fatal("event[4]: want Done with response")
	}
	tcs := done.Response.Message.ToolCalls
	if len(tcs) != 2 {
		t.Fatalf("want 2 tool calls, got %d", len(tcs))
	}
	if tcs[0].Function.Name != "fs.read" || tcs[1].Function.Name != "fs.list" {
		t.Errorf("tool call names: got %q, %q", tcs[0].Function.Name, tcs[1].Function.Name)
	}
	if string(tcs[0].Function.Arguments.Raw()) != `{"path":"a.go"}` {
		t.Errorf("tcs[0] args: got %q", string(tcs[0].Function.Arguments.Raw()))
	}
	if string(tcs[1].Function.Arguments.Raw()) != `{"path":"."}` {
		t.Errorf("tcs[1] args: got %q", string(tcs[1].Function.Arguments.Raw()))
	}
}

// TestToolCallAccumulator_StableID checks that ToolCallID is stable across chunks
// even when the provider omits the id field on all but the first chunk.
func TestToolCallAccumulator_StableID(t *testing.T) {
	acc := newToolCallAccumulator()

	isNew, name, id := acc.FeedToolCall(0, "call_stable", "fs.read", "")
	if !isNew || name != "fs.read" || id != "call_stable" {
		t.Fatalf("first feed: isNew=%v name=%q id=%q", isNew, name, id)
	}

	// Second chunk: id is empty (provider didn't repeat it)
	isNew2, _, id2 := acc.FeedToolCall(0, "", "", `{"path":"x"}`)
	if isNew2 {
		t.Error("second feed should not be isNew")
	}
	if id2 != "call_stable" {
		t.Errorf("second feed id: want call_stable, got %q", id2)
	}

	resp := acc.BuildResponse()
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatal("expected 1 tool call")
	}
	if resp.Message.ToolCalls[0].ID != "call_stable" {
		t.Errorf("tool call id: want call_stable, got %q", resp.Message.ToolCalls[0].ID)
	}
	if string(resp.Message.ToolCalls[0].Function.Arguments.Raw()) != `{"path":"x"}` {
		t.Errorf("args: got %q", string(resp.Message.ToolCalls[0].Function.Arguments.Raw()))
	}
}

// TestToolCallAccumulator_EmptyArgs checks that BuildResponse uses {} when no args received.
func TestToolCallAccumulator_EmptyArgs(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.FeedToolCall(0, "cid", "fs.list", "")

	resp := acc.BuildResponse()
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatal("expected 1 tool call")
	}
	if string(resp.Message.ToolCalls[0].Function.Arguments.Raw()) != "{}" {
		t.Errorf("empty args: want {}, got %q", string(resp.Message.ToolCalls[0].Function.Arguments.Raw()))
	}
}

// A stream that ends with neither [DONE] nor a finish_reason was cut off
// mid-answer (LLM-8). It used to be returned as the model's whole answer —
// half a sentence, or a tool call with truncated arguments. It is a
// retryable error now.
func TestParseSSEStream_CutOffIsAnError(t *testing.T) {
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"write\",\"arguments\":\"{\\\"path\\\": \\\"a.go\"}}]}}]}\n",
	)
	events := collectEvents(ParseSSEStream(context.Background(), fixture))
	last := events[len(events)-1]
	if last.Kind != StreamEventError || !IsTransientLLMError(last.Err) {
		t.Fatalf("a cut-off stream is a retryable error, got %+v", last)
	}
}

// A server that ignores stream:true answers with one ordinary completion; its
// words used to be dropped as a stream with no data.
func TestParseSSEStream_PlainCompletion(t *testing.T) {
	fixture := strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	events := collectEvents(ParseSSEStream(context.Background(), fixture))
	last := events[len(events)-1]
	if last.Kind != StreamEventDone || last.Response.Message.Content != "pong" || last.Response.StopReason != StopEnd || last.Response.Usage.TotalTokens != 4 {
		t.Fatalf("plain completion: %+v", last)
	}
}

func TestParseSSEStream_StopReason(t *testing.T) {
	fixture := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"cut\"},\"finish_reason\":\"length\"}]}\n" +
			"data: [DONE]\n")
	events := collectEvents(ParseSSEStream(context.Background(), fixture))
	if last := events[len(events)-1]; last.Kind != StreamEventDone || last.Response.StopReason != StopMaxTokens {
		t.Fatalf("finish_reason length is max_tokens: %+v", last)
	}
}

// An error sent as a plain JSON body is the server's error, not a cut-off.
func TestParseSSEStream_PlainError(t *testing.T) {
	events := collectEvents(ParseSSEStream(context.Background(), strings.NewReader(`{"error":{"message":"model not loaded"}}`)))
	last := events[len(events)-1]
	if last.Kind != StreamEventError || !strings.Contains(last.Err.Error(), "model not loaded") {
		t.Fatalf("plain error body: %+v", last)
	}
}
