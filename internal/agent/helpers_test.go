package agent

import (
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

func (a *Agent) buildSystemPrompt() string {
	return a.assembleSystemPrompt(a.buildSystemPromptParts())
}

// NormalizeLLM converts an OpenAI-style completion into the Agent's internal Step.
//
// Supported inputs:
// - OpenAI tool calls (message.tool_calls) -> StepToolCall (single or multi-call batch)
// - Plain JSON (legacy): AgentStep {"type":"tool_call"|"final", ...}
// - Plain JSON (recommended final): PatchSet {"patches":[...]}
// - Plain text (no tool_calls, no JSON envelope): treated as final-with-no-patches.
//
// Multi-call responses always populate Step.Tools with every call. The agent
// Run() loop chooses parallel vs serial execution via allParallelSafeCalls.
func NormalizeLLM(v *schema.Validator, resp *llm.CompleteResponse) (*Step, string, error) {
	return NormalizeLLMWithDefs(v, resp, nil)
}
