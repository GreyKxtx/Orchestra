package protocol

// Versioning for the vNext JSON contract.
//
// These values are part of the stable client↔core contract.
const (
	// ProtocolVersion is the version of JSON-RPC methods / schemas.
	// v3: added agent-level streaming events (tool_call_completed, step_done,
	//     pending_ops, recoverable_error) and bidirectional permission/request.
	// v4: added workflow.list, workflow.run, skill.list, skill.invoke methods
	//     plus workflow/stage_start / workflow/stage_done notification events.
	ProtocolVersion = 4

	// OpsVersion is the version of Internal Ops.
	OpsVersion = 1

	// ToolsVersion is the version of tool interfaces (inputs/outputs).
	// v5: added lsp.definition, lsp.references, lsp.hover, lsp.diagnostics, lsp.rename;
	//     added diagnostics field to fs.write and fs.edit responses.
	// v6: added diff.preview tool.
	// v7: added fs.delete, fs.rename; git.status, git.log, git.diff (read, always);
	//     git.commit, git.branch, git.checkout, git.push (write, allowExec-gated).
	// v8: added browser.navigate, browser.snapshot, browser.screenshot, browser.click,
	//     browser.type, browser.fill, browser.select, browser.eval, browser.wait,
	//     browser.close (all allowBrowser-gated).
	// v9: added search.websearch tool.
	// v10: added gh.pr.list, gh.pr.create, gh.pr.view, gh.issue.list, gh.issue.view (allowExec-gated).
	ToolsVersion = 10

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
