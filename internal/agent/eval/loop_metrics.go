// Package eval provides lightweight acceptance metrics for agent E2E harnesses.
package eval

import (
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
)

// MaxLSPIterations is the Planner–Worker acceptance cap (planner-worker.md §11).
const MaxLSPIterations = 3

// LoopMetrics counts mutating tool calls and recoverable LSP / resolver errors
// via agent OnEvent hooks.
type LoopMetrics struct {
	mu sync.Mutex

	editWrites     int
	lspHints       int
	ambiguousMatch int
}

func NewLoopMetrics() *LoopMetrics {
	return &LoopMetrics{}
}

// OnAgentEvent implements the agent.Options.OnEvent callback.
func (m *LoopMetrics) OnAgentEvent(ev agent.AgentEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch ev.Stream.Kind {
	case llm.StreamEventToolCallCompleted:
		switch ev.Stream.ToolCallName {
		case "edit", "write", "fs.edit", "fs.write":
			m.editWrites++
		}
	case llm.StreamEventRecoverableError:
		content := ev.Stream.Content
		if strings.Contains(content, "lsp_errors") {
			m.lspHints++
		}
		if strings.Contains(content, string(protocol.AmbiguousMatch)) {
			m.ambiguousMatch++
		}
	}
}

func (m *LoopMetrics) EditWriteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.editWrites
}

func (m *LoopMetrics) LSPIterationCount() int {
	return m.EditWriteCount()
}

func (m *LoopMetrics) LSPHintCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lspHints
}

func (m *LoopMetrics) AmbiguousMatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ambiguousMatch
}

// WithinLSPIterationBudget reports whether edit/write count is within MaxLSPIterations.
func (m *LoopMetrics) WithinLSPIterationBudget() bool {
	return m.EditWriteCount() <= MaxLSPIterations
}
