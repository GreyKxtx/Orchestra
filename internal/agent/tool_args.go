package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/orchestra/orchestra/llm"
)

// A local model gets a tool's argument shape a little wrong in ways that are
// plain to read and that the strict decoder refuses: an array sent as a JSON
// string ({"todos": "[{...}]"}), "path" for a schema's "paths", an option at
// the top level instead of under "options". Each refusal cost a step, or put
// the same question to the user twice. coerceStringifiedArgs mends these
// against the tool's own schema before the call; anything it cannot place is
// left exactly as it came, for the tool to judge.

type argSchema struct {
	Type       any                  `json:"type"`
	Properties map[string]argSchema `json:"properties"`
}

// coerceStringifiedArgs returns input with the mendable mistakes mended.
func coerceStringifiedArgs(input json.RawMessage, def llm.ToolDef) json.RawMessage {
	if len(input) == 0 || len(def.Function.Parameters) == 0 {
		return input
	}
	var schema argSchema
	if json.Unmarshal(def.Function.Parameters, &schema) != nil || len(schema.Properties) == 0 {
		return input
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(input, &args) != nil {
		return input
	}
	changed := false
	for name, raw := range args {
		prop, known := schema.Properties[name]
		if !known {
			if mendUnknownArg(args, schema, name, raw) {
				changed = true
			}
			continue
		}
		if v, ok := unwrapStringified(raw, prop); ok {
			args[name] = v
			changed = true
		}
	}
	if !changed {
		return input
	}
	out, err := json.Marshal(args)
	if err != nil {
		return input
	}
	return out
}

// mendUnknownArg finds a home in the schema for an argument it does not name:
// the plural it meant ("path" → "paths", a string wrapped in an array), or the
// nested object the option belongs to ("context_lines" → options.context_lines).
func mendUnknownArg(args map[string]json.RawMessage, schema argSchema, name string, raw json.RawMessage) bool {
	if plural, ok := schema.Properties[name+"s"]; ok && hasType(plural.Type, "array") {
		if _, taken := args[name+"s"]; !taken {
			v := raw
			var s string
			if json.Unmarshal(raw, &s) == nil {
				v, _ = json.Marshal([]string{s})
			}
			args[name+"s"] = v
			delete(args, name)
			return true
		}
	}
	for parent, ps := range schema.Properties {
		if _, ok := ps.Properties[name]; !ok || !hasType(ps.Type, "object") {
			continue
		}
		nested := map[string]json.RawMessage{}
		if cur, ok := args[parent]; ok && json.Unmarshal(cur, &nested) != nil {
			return false
		}
		if _, taken := nested[name]; taken {
			return false
		}
		nested[name] = raw
		b, err := json.Marshal(nested)
		if err != nil {
			return false
		}
		args[parent] = b
		delete(args, name)
		return true
	}
	return false
}

// unwrapStringified returns the array or object a JSON string holds, when the
// schema wants one there.
func unwrapStringified(raw json.RawMessage, prop argSchema) (json.RawMessage, bool) {
	if !hasType(prop.Type, "array") && !hasType(prop.Type, "object") {
		return nil, false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil, false
	}
	s = repairLatin1Mojibake(strings.TrimSpace(s))
	if s == "" || (s[0] != '[' && s[0] != '{') || !json.Valid([]byte(s)) {
		return nil, false
	}
	return json.RawMessage(s), true
}

// hasType: a schema type that is want, alone or in a list.
func hasType(t any, want string) bool {
	switch v := t.(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// repairLatin1Mojibake undoes UTF-8 text that was read as Latin-1 on the way
// ("Ð¡Ð¾Ð·" for "Соз"): every rune is below 256, some are above 127, and as
// bytes they are valid UTF-8. Text that is really Latin-1 ("café") is not
// valid UTF-8 as bytes and is left alone.
func repairLatin1Mojibake(s string) string {
	high := false
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return s
		}
		if r > 0x7f {
			high = true
		}
		b = append(b, byte(r))
	}
	if !high || !utf8.Valid(b) || bytes.Equal(b, []byte(s)) {
		return s
	}
	return string(b)
}
