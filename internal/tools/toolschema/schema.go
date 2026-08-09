// Package toolschema holds JSON-schema helpers for LLM tool definitions.
package toolschema

import (
	"encoding/json"
)

// MustSchema parses a JSON schema string or panics (tool defs are static).
func MustSchema(s string) json.RawMessage {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return json.RawMessage(s)
}
