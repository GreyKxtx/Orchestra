package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Call executes a tool by name with a JSON input object, returning JSON output.
func (r *Runner) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool name is empty")
	}

	// Route mcp:* calls to the registered MCP manager (use original name).
	if r.mcpCaller != nil && strings.HasPrefix(name, "mcp:") {
		return r.mcpCaller.Call(ctx, name, input)
	}

	// Accept both LLM-facing aliases (read, grep, bash, …) and legacy
	// canonical names (fs.read, search.text, exec.run, …). Internal
	// dispatch is keyed on the canonical form.
	name = resolveToolName(name)

	switch name {
	case "fs.list":
		var req FSListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.read":
		var req FSReadRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSRead(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.glob":
		var req FSGlobRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSGlob(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.write":
		var req FSWriteRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSWrite(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.edit":
		var req FSEditRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSEdit(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.apply_ops":
		// Normalize common LLM mistake: "type" → "op" in ops
		normalizedInput := normalizeOpsJSON(input)
		var req FSApplyOpsRequest
		if err := decodeToolInput(normalizedInput, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSApplyOps(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "search.text":
		var req SearchTextRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.SearchText(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "code.symbols":
		var req CodeSymbolsRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.CodeSymbols(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "explore_codebase":
		var req ExploreCodebaseRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.ExploreCodebase(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "exec.run":
		var req ExecRunRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.ExecRun(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "runtime.query":
		var req RuntimeQueryRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.RuntimeQuery(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// normalizeOpsJSON normalizes common LLM mistakes in ops JSON:
// - "type" field → "op" field (if "op" is missing)
func normalizeOpsJSON(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return input
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		return input // Return original if parsing fails
	}

	// Check if this is fs.apply_ops request with ops array
	if opsRaw, hasOps := raw["ops"]; hasOps {
		var opsArray []json.RawMessage
		if err := json.Unmarshal(opsRaw, &opsArray); err == nil {
			// Normalize each op: "type" → "op"
			normalized := make([]json.RawMessage, 0, len(opsArray))
			for _, opRaw := range opsArray {
				var opMap map[string]json.RawMessage
				if err := json.Unmarshal(opRaw, &opMap); err == nil {
					// If "op" is missing but "type" exists, use "type" as "op"
					if _, hasOp := opMap["op"]; !hasOp {
						if typeVal, hasType := opMap["type"]; hasType {
							opMap["op"] = typeVal
							// Remove "type" to avoid confusion
							delete(opMap, "type")
						}
					}
					// Re-encode normalized op
					if normalizedOp, err := json.Marshal(opMap); err == nil {
						normalized = append(normalized, normalizedOp)
					} else {
						normalized = append(normalized, opRaw) // Fallback to original
					}
				} else {
					normalized = append(normalized, opRaw) // Fallback to original
				}
			}
			// Re-encode ops array
			if normalizedOps, err := json.Marshal(normalized); err == nil {
				raw["ops"] = normalizedOps
				// Re-encode entire request
				if normalizedInput, err := json.Marshal(raw); err == nil {
					return normalizedInput
				}
			}
		}
	}

	return input // Return original if normalization not needed or failed
}

func decodeToolInput(input json.RawMessage, out any) error {
	if len(input) == 0 {
		// Treat missing input as empty object.
		input = []byte(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

func mustJSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
