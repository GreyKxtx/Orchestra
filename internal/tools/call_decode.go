package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)
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
