package eval

import (
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

func TestLoopMetrics_CountsEditsAndRecoverableErrors(t *testing.T) {
	m := NewLoopMetrics()
	m.OnAgentEvent(agent.AgentEvent{Stream: llm.StreamEvent{Kind: llm.StreamEventToolCallCompleted, ToolCallName: "edit"}})
	m.OnAgentEvent(agent.AgentEvent{Stream: llm.StreamEvent{Kind: llm.StreamEventToolCallCompleted, ToolCallName: "read"}})
	m.OnAgentEvent(agent.AgentEvent{Stream: llm.StreamEvent{Kind: llm.StreamEventRecoverableError, Content: "lsp_errors: edit"}})
	m.OnAgentEvent(agent.AgentEvent{Stream: llm.StreamEvent{Kind: llm.StreamEventRecoverableError, Content: string(protocol.AmbiguousMatch) + ": duplicate"}})

	if got := m.EditWriteCount(); got != 1 {
		t.Fatalf("EditWriteCount = %d, want 1", got)
	}
	if got := m.LSPIterationCount(); got != 1 {
		t.Fatalf("LSPIterationCount = %d, want 1", got)
	}
	if got := m.LSPHintCount(); got != 1 {
		t.Fatalf("LSPHintCount = %d, want 1", got)
	}
	if got := m.AmbiguousMatchCount(); got != 1 {
		t.Fatalf("AmbiguousMatchCount = %d, want 1", got)
	}
	if !m.WithinLSPIterationBudget() {
		t.Fatal("expected within budget after one edit")
	}
}
