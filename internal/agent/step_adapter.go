package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
)

// NormalizeLLM converts an OpenAI-style completion into the Agent's internal Step.
//
// Supported inputs:
// - OpenAI tool calls (message.tool_calls) -> StepToolCall (single tool call only)
// - Plain JSON (legacy): AgentStep {"type":"tool_call"|"final", ...}
// - Plain JSON (recommended final): PatchSet {"patches":[...]}
func NormalizeLLM(v *schema.Validator, resp *llm.CompleteResponse) (*Step, string, error) {
	if resp == nil {
		return nil, "", protocol.NewError(protocol.InvalidLLMOutput, "LLM response is nil", nil)
	}
	msg := resp.Message

	// Tool calling path (preferred).
	if len(msg.ToolCalls) > 0 {
		if len(msg.ToolCalls) != 1 {
			return nil, strings.TrimSpace(msg.Content), protocol.NewError(protocol.InvalidLLMOutput, "model returned multiple tool calls; only one is supported per step", map[string]any{
				"count": len(msg.ToolCalls),
			})
		}
		tc := msg.ToolCalls[0]
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			return nil, strings.TrimSpace(msg.Content), protocol.NewError(protocol.InvalidLLMOutput, "tool call name is empty", nil)
		}
		step := Step{
			Type: StepToolCall,
			Tool: &ToolCall{
				Name:  name,
				Input: tc.Function.Arguments.Raw(),
			},
		}
		return &step, strings.TrimSpace(msg.Content), nil
	}

	raw := strings.TrimSpace(msg.Content)
	if raw == "" {
		return nil, "", protocol.NewError(protocol.InvalidLLMOutput, "empty assistant message content", nil)
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
		var ps externalpatch.PatchSet
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

	// Truncate for error message
	rawTruncated := raw
	if len(rawTruncated) > 400 {
		rawTruncated = rawTruncated[:400] + "..."
	}
	return nil, raw, protocol.NewError(protocol.InvalidLLMOutput, "invalid assistant output: expected tool_call, AgentStep JSON, or PatchSet JSON", map[string]any{
		"raw": rawTruncated,
	})
}

// extractJSON extracts JSON object from text, handling markdown code blocks and extra text.
// Returns the first valid JSON object found, or the original string if no JSON found.
func extractJSON(text string) string {
	text = strings.TrimSpace(text)

	// Remove markdown code blocks
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	// Find first '{' and last '}' to extract JSON object
	start := strings.Index(text, "{")
	if start == -1 {
		return text // No JSON found, return as-is
	}

	// Find matching closing brace
	braceCount := 0
	end := -1
	for i := start; i < len(text); i++ {
		if text[i] == '{' {
			braceCount++
		} else if text[i] == '}' {
			braceCount--
			if braceCount == 0 {
				end = i + 1
				break
			}
		}
	}

	if end > start {
		return strings.TrimSpace(text[start:end])
	}

	// Fallback: return text from first '{' to end (might be incomplete, but validator will catch it)
	return strings.TrimSpace(text[start:])
}
