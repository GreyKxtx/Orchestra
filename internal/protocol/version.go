package protocol

// Versioning for the vNext JSON contract.
//
// These values are part of the stable client↔core contract.
const (
	// ProtocolVersion is the version of JSON-RPC methods / schemas.
	// v3: added agent-level streaming events (tool_call_completed, step_done,
	//     pending_ops, recoverable_error) and bidirectional permission/request.
	ProtocolVersion = 3

	// OpsVersion is the version of Internal Ops.
	OpsVersion = 1

	// ToolsVersion is the version of tool interfaces (inputs/outputs).
	// v5: added lsp.definition, lsp.references, lsp.hover, lsp.diagnostics, lsp.rename;
	//     added diagnostics field to fs.write and fs.edit responses.
	ToolsVersion = 5

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
