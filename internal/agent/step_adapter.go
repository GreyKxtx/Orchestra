package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
)

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

// NormalizeLLMWithDefs is the flag-aware variant used by the agent so it can
// classify batched tool_calls. defs is passed through to nextStep for parallel
// classification in Run(); when nil, parallel-vs-serial is decided at runtime
// from buildToolDefs().
func NormalizeLLMWithDefs(v *schema.Validator, resp *llm.CompleteResponse, defs []llm.ToolDef) (*Step, string, error) {
	if resp == nil {
		return nil, "", protocol.NewError(protocol.InvalidLLMOutput, "LLM response is nil", nil)
	}
	msg := resp.Message

	// Tool calling path (preferred).
	if len(msg.ToolCalls) > 0 {
		if len(msg.ToolCalls) > 1 {
			tools := make([]ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				name := normalizeToolName(tc.Function.Name)
				if name == "" {
					continue
				}
				tools = append(tools, ToolCall{
					ID:    tc.ID,
					Name:  name,
					Input: tc.Function.Arguments.Raw(),
				})
			}
			if len(tools) == 0 {
				return nil, strings.TrimSpace(msg.Content), protocol.NewError(protocol.InvalidLLMOutput, "tool call name is empty", nil)
			}
			return &Step{Type: StepToolCall, Tools: tools}, strings.TrimSpace(msg.Content), nil
		}

		tc := msg.ToolCalls[0]
		name := normalizeToolName(tc.Function.Name)
		if name == "" {
			return nil, strings.TrimSpace(msg.Content), protocol.NewError(protocol.InvalidLLMOutput, "tool call name is empty", nil)
		}
		step := Step{
			Type: StepToolCall,
			Tool: &ToolCall{
				ID:    tc.ID,
				Name:  name,
				Input: tc.Function.Arguments.Raw(),
			},
		}
		return &step, strings.TrimSpace(msg.Content), nil
	}

	raw := strings.TrimSpace(msg.Content)
	if raw == "" {
		// Empty content with no tool_calls: model is done (no changes needed).
		// This handles reasoning models that return blank content when they finish
		// (e.g. qwen3.6-27b after thinking — reasoning_content was already folded
		// into content upstream; if it's still empty, the agent is simply done).
		return &Step{Type: StepFinal, Final: &Final{Patches: nil}}, "", nil
	}

	// Extract JSON from text (some models add markdown or extra text)
	jsonStr := extractJSON(raw)

	// Legacy: AgentStep JSON.
	if v != nil {
		var step Step
		if coreErr := v.ValidateAndDecode(schema.KindAgentStep, jsonStr, &step); coreErr == nil {
			return &step, jsonStr, nil
		}
		// Recommended final: PatchSet JSON.
		var ps patches.PatchSet
		if coreErr := v.ValidateAndDecode(schema.KindExternalPatches, jsonStr, &ps); coreErr == nil {
			step := Step{
				Type: StepFinal,
				Final: &Final{
					Patches: ps.Patches,
				},
			}
			return &step, jsonStr, nil
		}
	}

	// Plain-text response with no tool_calls and no parseable JSON envelope.
	// Treat as a final step with no patches — same model opencode uses:
	// absence of tool_calls means the agent is done, the natural-language
	// answer was already streamed to the user via message_delta events.
	//
	// This applies to every mode (Build / Plan / Ask / Explore / etc): if the
	// model wants to make changes it emits tool_calls or the JSON envelope,
	// otherwise plain prose is its final answer. No more "expected JSON" retry
	// loop that left the spinner spinning forever on conversational replies.
	step := Step{
		Type:  StepFinal,
		Final: &Final{Patches: nil},
	}
	return &step, raw, nil
}

// extractJSON extracts the last valid JSON object from text.
// Searches from the end so that text answers before {"patches":[...]} are skipped correctly.
// Also strips markdown code fences and <think>...</think> blocks (Qwen3 thinking mode).
func extractJSON(text string) string {
	text = strings.TrimSpace(text)

	// Strip <think>...</think> blocks produced by reasoning models.
	text = stripThinkBlocks(text)
	text = strings.TrimSpace(text)

	// Strip markdown code fences.
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(text, fence) {
			text = strings.TrimPrefix(text, fence)
			if idx := strings.LastIndex(text, "```"); idx != -1 {
				text = text[:idx]
			}
			text = strings.TrimSpace(text)
			break
		}
	}

	// Find the last '}' and walk backwards to its matching '{'.
	// This lets the model write a text answer first and put JSON at the end.
	end := strings.LastIndex(text, "}")
	if end == -1 {
		return text
	}

	braceCount := 0
	start := -1
	for i := end; i >= 0; i-- {
		if text[i] == '}' {
			braceCount++
		} else if text[i] == '{' {
			braceCount--
			if braceCount == 0 {
				start = i
				break
			}
		}
	}

	if start != -1 {
		return strings.TrimSpace(text[start : end+1])
	}
	return strings.TrimSpace(text)
}

// stripThinkBlocks removes <think>...</think> sections inserted by reasoning models.
func stripThinkBlocks(s string) string {
	for {
		open := strings.Index(s, "<think>")
		if open == -1 {
			break
		}
		close := strings.Index(s[open:], "</think>")
		if close == -1 {
			// Unclosed block — drop everything from <think> onward.
			s = strings.TrimSpace(s[:open])
			break
		}
		s = s[:open] + s[open+close+len("</think>"):]
	}
	return s
}
