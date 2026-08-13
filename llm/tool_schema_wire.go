package llm

import "encoding/json"

// Wire-level tool schema sanitization.
//
// Anthropic (and some other strict providers behind OpenRouter) reject
// input_schema with oneOf/allOf/anyOf at the TOP level ("input_schema does
// not support oneOf, allOf, or anyOf at the top level"). Our task/task_spawn
// tools and arbitrary third-party MCP servers use such schemas. We strip the
// combinators on the wire only — canonical schemas keep validating tool
// inputs at runtime, so nothing is lost except provider-side hinting.

// sanitizeWireToolSchema returns params without top-level oneOf/allOf/anyOf
// and reports whether anything was removed.
func sanitizeWireToolSchema(params json.RawMessage) (json.RawMessage, bool) {
	if len(params) == 0 {
		return params, false
	}
	var m map[string]any
	if err := json.Unmarshal(params, &m); err != nil {
		return params, false
	}
	changed := false
	for _, k := range []string{"oneOf", "allOf", "anyOf"} {
		if _, ok := m[k]; ok {
			delete(m, k)
			changed = true
		}
	}
	if !changed {
		return params, false
	}
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	out, err := json.Marshal(m)
	if err != nil {
		return params, false
	}
	return out, true
}

// sanitizeWireToolSchemas applies sanitizeWireToolSchema to every tool,
// copying the slice lazily (only when at least one schema changed).
func sanitizeWireToolSchemas(tools []ToolDef) []ToolDef {
	var out []ToolDef
	for i, t := range tools {
		clean, changed := sanitizeWireToolSchema(t.Function.Parameters)
		if !changed {
			continue
		}
		if out == nil {
			out = make([]ToolDef, len(tools))
			copy(out, tools)
		}
		out[i].Function.Parameters = clean
	}
	if out == nil {
		return tools
	}
	return out
}
