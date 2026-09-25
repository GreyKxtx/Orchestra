package wire

import "github.com/orchestra/orchestra/protocol"

// InitializeParams is the first request of a connection.
//
// Since ProtocolVersion 24 the client names the range of protocol versions
// it speaks and the core answers with the newest both sides have. Before
// that the number had to match exactly, so a client and a core from
// different releases could not connect at all — and tools_version, which
// moves with tools the client never calls, was checked the same way.
type InitializeParams struct {
	ProjectRoot string `json:"project_root"`
	ProjectID   string `json:"project_id"`

	// ProtocolVersion is the newest protocol version the client speaks.
	ProtocolVersion int `json:"protocol_version"`
	// MinProtocolVersion is the oldest it still speaks. Zero means
	// ProtocolVersion alone, which is what a client before v24 sends.
	MinProtocolVersion int `json:"min_protocol_version,omitempty"`

	// OpsVersion must match the core's when given: internal ops are what
	// reaches the disk.
	OpsVersion int `json:"ops_version,omitempty"`
	// ToolsVersion is informational: the core records it and answers with
	// its own. A client that needs a particular tool checks the answer.
	ToolsVersion int `json:"tools_version,omitempty"`
}

// InitializeResult answers initialize.
type InitializeResult struct {
	Status string `json:"status"`
	// ProtocolVersion is the version both sides speak from here on: the
	// newest in both ranges (ProtocolVersion 24).
	ProtocolVersion int `json:"protocol_version"`
	// ToolsVersion is the core's.
	ToolsVersion int `json:"tools_version"`
	// Capabilities is what this core serves, by name, so a client asks for
	// the feature it needs instead of comparing version numbers.
	Capabilities Capabilities    `json:"capabilities"`
	Health       protocol.Health `json:"health"`
}

// Capabilities names what a core serves.
type Capabilities struct {
	// Methods the core answers.
	Methods []string `json:"methods"`
	// Notifications the core sends.
	Notifications []string `json:"notifications"`
	// Requests the core makes of the client, which the client must answer.
	Requests []string `json:"requests"`
}

// Has reports whether name is among the methods, notifications or requests.
func (c Capabilities) Has(name string) bool {
	for _, list := range [][]string{c.Methods, c.Notifications, c.Requests} {
		for _, n := range list {
			if n == name {
				return true
			}
		}
	}
	return false
}

// VersionRange is the protocol versions one side speaks, inclusive.
type VersionRange struct {
	Min int
	Max int
}

// ClientRange is the range an InitializeParams names: protocol_version
// alone when min_protocol_version is absent or malformed.
func (p InitializeParams) ClientRange() VersionRange {
	r := VersionRange{Min: p.MinProtocolVersion, Max: p.ProtocolVersion}
	if r.Min <= 0 || r.Min > r.Max {
		r.Min = r.Max
	}
	return r
}

// CoreRange is the range this core speaks.
func CoreRange() VersionRange {
	return VersionRange{Min: protocol.MinProtocolVersion, Max: protocol.ProtocolVersion}
}

// Negotiate picks the newest protocol version both ranges contain. ok is
// false when they share none.
func Negotiate(client, core VersionRange) (v int, ok bool) {
	v = client.Max
	if core.Max < v {
		v = core.Max
	}
	if v <= 0 || v < client.Min || v < core.Min {
		return 0, false
	}
	return v, true
}
