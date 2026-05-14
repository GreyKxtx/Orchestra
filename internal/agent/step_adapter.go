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
// - OpenAI tool calls (message.tool_calls) -> StepToolCall (single or parallel batch)
// - Plain JSON (legacy): AgentStep {"type":"tool_call"|"final", ...}
// - Plain JSON (recommended final): PatchSet {"patches":[...]}
// - Plain text (no tool_calls, no JSON envelope): treated as final-with-no-patches.
//
// Parallel-batch selection: when the response carries ≥2 tool_calls AND every
// tool name maps to a ParallelSafe definition (per parallelSafeNames), the step
// is populated as a batch (Step.Tools). The agent fans these out concurrently.
// Otherwise the legacy single-tool path is used (Step.Tool = first call); any
// remaining parallel calls are silently dropped so the model isn't stuck in
// invalid-retry loops emitting the same parallel batch over and over.
func NormalizeLLM(v *schema.Validator, resp *llm.CompleteResponse) (*Step, string, error) {
	return NormalizeLLMWithDefs(v, resp, nil)
}

// NormalizeLLMWithDefs is the flag-aware variant used by the agent so it can
// classify batched tool_calls. defs is the active tool registry slice — when
// non-nil it lets us look up each call's ParallelSafe flag. Passing nil
// reproduces the legacy "first-call wins" behaviour.
func NormalizeLLMWithDefs(v *schema.Validator, resp *llm.CompleteResponse, defs []llm.ToolDef) (*Step, string, error) {
	if resp == nil {
		return nil, "", protocol.NewError(protocol.InvalidLLMOutput, "LLM response is nil", nil)
	}
	msg := resp.Message

	// Tool calling path (preferred).
	if len(msg.ToolCalls) > 0 {
		// Parallel batch fast-path: every call must be ParallelSafe per the
		// registry. If even one isn't (e.g. write/edit/bash), we fall back to
		// the legacy serial path (execute the first call, drop the rest).
		if len(msg.ToolCalls) > 1 && allParallelSafe(msg.ToolCalls, defs) {
			tools := make([]ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
				if name == "" {
					continue
				}
				tools = append(tools, ToolCall{
					ID:    tc.ID,
					Name:  name,
					Input: tc.Function.Arguments.Raw(),
				})
			}
			if len(tools) >= 2 {
				return &Step{Type: StepToolCall, Tools: tools}, strings.TrimSpace(msg.Content), nil
			}
		}

		// Serial path: take the first tool call, ignore the rest. Returning an
		// error on parallel mixed batches would force a retry, but the model
		// usually retries with the same shape — that's a budget burn with no
		// progress. Better to make progress on the first call and let the next
		// step re-request whatever else is still needed.
		tc := msg.ToolCalls[0]
		name := strings.TrimSpace(tc.Function.Name)
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

// allParallelSafe returns true iff every tool_call in calls maps to a
// ParallelSafe entry in defs. Used by NormalizeLLMWithDefs to decide whether
// a batched response can fan out concurrently. An unknown tool (not in defs)
// counts as NOT parallel-safe — the conservative default protects against
// accidentally racing on a tool whose semantics aren't classified yet.
func allParallelSafe(calls []llm.ToolCall, defs []llm.ToolDef) bool {
	if len(defs) == 0 {
		return false
	}
	flag := make(map[string]bool, len(defs))
	for _, d := range defs {
		flag[d.Function.Name] = d.ParallelSafe
	}
	for _, c := range calls {
		name := strings.TrimSpace(c.Function.Name)
		if !flag[name] {
			return false
		}
	}
	return true
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
