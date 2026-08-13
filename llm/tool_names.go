package llm

// Wire-level tool-name sanitization.
//
// Orchestra registers MCP tools as "mcp:<server>:<tool>". OpenAI-compatible
// endpoints tolerate that, but Anthropic (and some strict gateways) require
// tool names to match ^[a-zA-Z0-9_-]{1,128}$ and reject the whole request
// with 400 otherwise. Instead of changing the canonical registry naming
// (which is part of the tools contract), we rename on the wire and map the
// model's tool calls back to canonical names.

const maxWireToolNameLen = 128

func isWireToolNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}

func wireToolNameValid(name string) bool {
	if name == "" || len(name) > maxWireToolNameLen {
		return false
	}
	for _, r := range name {
		if !isWireToolNameRune(r) {
			return false
		}
	}
	return true
}

func sanitizeWireToolName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if isWireToolNameRune(r) {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	s := string(out)
	if s == "" {
		s = "tool"
	}
	if len(s) > maxWireToolNameLen {
		s = s[:maxWireToolNameLen]
	}
	return s
}

// toolNameMapper renames invalid tool names for the wire and restores the
// canonical names on the way back. Nil when every name is already valid.
type toolNameMapper struct {
	toWire   map[string]string
	fromWire map[string]string
}

// newToolNameMapper inspects the request tools and returns a mapper, or nil
// when no renaming is needed (the common case — zero overhead).
func newToolNameMapper(tools []ToolDef) *toolNameMapper {
	var m *toolNameMapper
	for _, t := range tools {
		name := t.Function.Name
		if wireToolNameValid(name) {
			continue
		}
		if m == nil {
			m = &toolNameMapper{
				toWire:   make(map[string]string),
				fromWire: make(map[string]string, len(tools)),
			}
			// Reserve all valid names first so sanitized names cannot collide
			// with an existing tool.
			for _, vt := range tools {
				if wireToolNameValid(vt.Function.Name) {
					m.fromWire[vt.Function.Name] = vt.Function.Name
				}
			}
		}
		wire := sanitizeWireToolName(name)
		base := wire
		for i := 2; ; i++ {
			if _, taken := m.fromWire[wire]; !taken {
				break
			}
			suffix := "_" + itoa(i)
			if len(base)+len(suffix) > maxWireToolNameLen {
				wire = base[:maxWireToolNameLen-len(suffix)] + suffix
			} else {
				wire = base + suffix
			}
		}
		m.toWire[name] = wire
		m.fromWire[wire] = name
	}
	return m
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// WireName maps a canonical name to its wire form (identity when unmapped).
func (m *toolNameMapper) WireName(name string) string {
	if m == nil {
		return name
	}
	if w, ok := m.toWire[name]; ok {
		return w
	}
	return name
}

// Restore maps a wire name back to the canonical name (identity when unmapped).
func (m *toolNameMapper) Restore(name string) string {
	if m == nil {
		return name
	}
	if orig, ok := m.fromWire[name]; ok {
		return orig
	}
	return name
}

// WireTools returns a copy of tools with names renamed for the wire.
func (m *toolNameMapper) WireTools(tools []ToolDef) []ToolDef {
	if m == nil {
		return tools
	}
	out := make([]ToolDef, len(tools))
	copy(out, tools)
	for i := range out {
		out[i].Function.Name = m.WireName(out[i].Function.Name)
	}
	return out
}

// WireMessages returns messages with assistant tool_calls renamed for the
// wire (history replay must reference the same names the provider saw).
// The input slice is never mutated.
func (m *toolNameMapper) WireMessages(msgs []Message) []Message {
	if m == nil {
		return msgs
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		calls := make([]ToolCall, len(out[i].ToolCalls))
		copy(calls, out[i].ToolCalls)
		for j := range calls {
			calls[j].Function.Name = m.WireName(calls[j].Function.Name)
		}
		out[i].ToolCalls = calls
	}
	return out
}

// RestoreResponse rewrites tool call names in an assembled response back to
// canonical form. Safe to call with nil receiver or nil response.
func (m *toolNameMapper) RestoreResponse(resp *CompleteResponse) {
	if m == nil || resp == nil {
		return
	}
	for i := range resp.Message.ToolCalls {
		resp.Message.ToolCalls[i].Function.Name = m.Restore(resp.Message.ToolCalls[i].Function.Name)
	}
}
