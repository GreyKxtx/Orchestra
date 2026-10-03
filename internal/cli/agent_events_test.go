package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol/wire"
)

// The core notifies with typed wire.AgentEvent values. The eval harness and
// apply's router line both read them as map[string]any, so neither ever saw
// one: every answer_matches / answer_not_empty check failed whatever the model
// said, and the "[auto_router]" line was never printed.

func TestAnswerCollector_ReadsTypedDeltas(t *testing.T) {
	onEvent, answer := answerCollector()
	onEvent(wire.NotifyAgentEvent, wire.AgentEvent{Type: "message_delta", Content: "Reverse returns "})
	onEvent(wire.NotifyAgentEvent, wire.AgentEvent{Type: "message_delta", Content: "s backwards."})
	onEvent(wire.NotifyAgentEvent, wire.AgentEvent{Type: "message_delta", Content: " (child)", Scope: "child"})
	onEvent(wire.NotifyAgentEvent, wire.AgentEvent{Type: "tool_call_completed", Content: "{}"})
	if got := answer(); got != "Reverse returns s backwards." {
		t.Fatalf("answer = %q", got)
	}
}

func TestPrintModeRoute_ReadsTypedEvent(t *testing.T) {
	var b bytes.Buffer
	printModeRouteTo(&b, wire.NotifyAgentEvent, wire.AgentEvent{
		Type: "mode_route",
		Data: wire.ModeRoute{From: "agent", To: "ask", Reason: "a question", Confidence: 0.9},
	})
	if got := b.String(); !strings.Contains(got, "agent → ask (90%) a question") {
		t.Fatalf("router line = %q", got)
	}
}
