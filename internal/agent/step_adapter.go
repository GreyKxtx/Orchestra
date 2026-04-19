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
