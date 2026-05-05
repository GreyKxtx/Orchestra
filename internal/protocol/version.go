package protocol

// Versioning for the vNext JSON contract.
//
// These values are part of the stable client↔core contract.
const (
	// ProtocolVersion is the version of JSON-RPC methods / schemas.
	ProtocolVersion = 1

	// OpsVersion is the version of Internal Ops.
	OpsVersion = 1

	// ToolsVersion is the version of tool interfaces (inputs/outputs).
	ToolsVersion = 4

	// CoreVersion is a human-friendly build/version string.
	CoreVersion = "vnext"
)

// Health is returned by core.health (and /health in HTTP mode).
type Health struct {
	Status          string `json:"status"`
	CoreVersion     string `json:"core_version"`
	ProtocolVersion int    `json:"protocol_version"`
	OpsVersion      int    `json:"ops_version"`
	ToolsVersion    int    `json:"tools_version"`

	WorkspaceRoot string `json:"workspace_root,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
}
