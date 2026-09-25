package wire

import (
	"strings"
)

// mcp.list, mcp.upsert, mcp.delete, mcp.set_disabled, mcp.test, mcp.prompts, mcp.prompt.get.

// MCPServerParams is the JSON shape for one MCP server (upsert / test).
type MCPServerParams struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
	CallTimeoutS int               `json:"call_timeout_s,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
}

// MCPListParams is reserved.
type MCPListParams struct{}

// MCPServerView is one row in mcp.list.
type MCPServerView struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	Disabled     bool              `json:"disabled"`
	CallTimeoutS int               `json:"call_timeout_s,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
	Status       string            `json:"status"` // running | disabled | error | stopped
	ToolCount    int               `json:"tool_count"`
	Tools        []string          `json:"tools,omitempty"` // discovered tool names (for settings toggles)
	Error        string            `json:"error,omitempty"`
}

// MCPListResult is returned by mcp.list.
type MCPListResult struct {
	Servers []MCPServerView `json:"servers"`
}

// MCPUpsertParams adds or replaces a server by name.
type MCPUpsertParams struct {
	Server  MCPServerParams `json:"server"`
	Persist *bool           `json:"persist,omitempty"` // default true
}

// MCPUpsertResult is returned after upsert + hot reload.
type MCPUpsertResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPDeleteParams removes a server by name.
type MCPDeleteParams struct {
	Name    string `json:"name"`
	Persist *bool  `json:"persist,omitempty"`
}

// MCPDeleteResult mirrors list after delete.
type MCPDeleteResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPSetDisabledParams toggles disabled.
type MCPSetDisabledParams struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Persist  *bool  `json:"persist,omitempty"`
}

// MCPSetDisabledResult mirrors list after toggle.
type MCPSetDisabledResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPTestParams probes a server config (or named cfg entry) without persisting.
type MCPTestParams struct {
	Name   string           `json:"name,omitempty"`   // use existing cfg entry
	Server *MCPServerParams `json:"server,omitempty"` // or ad-hoc config
}

// MCPTestResult lists tools from a temporary connection.
type MCPTestResult struct {
	OK      bool     `json:"ok"`
	Name    string   `json:"name"`
	Tools   []string `json:"tools,omitempty"`
	Error   string   `json:"error,omitempty"`
	Elapsed string   `json:"elapsed,omitempty"`
}

// MCPPromptArgView is one argument an MCP prompt accepts.
type MCPPromptArgView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// MCPPromptCommand is one MCP prompt as a slash command a person can run.
//
// An MCP prompt is the server's own recipe, meant for a human to pick — not
// for the model to call. So it belongs in the command palette next to /model
// and /skill, not in the tool list.
type MCPPromptCommand struct {
	Server      string             `json:"server"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Arguments   []MCPPromptArgView `json:"arguments,omitempty"`

	// Slash and Hint are the rendered palette row. They travel over the wire
	// so the TUI and the VS Code panel do not each re-derive the formatting
	// and drift apart.
	Slash string `json:"slash,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// Fill computes the rendered fields.
func (c *MCPPromptCommand) Fill() {
	c.Slash = c.Command()
	c.Hint = c.Describe()
}

// Command returns the slash form. The server is part of the name because two
// servers may well offer a prompt called "review".
func (c MCPPromptCommand) Command() string {
	return "/mcp:" + c.Server + ":" + c.Name
}

// Describe is the palette's right-hand column: the server's description plus
// the argument shape, so the user can see what to type without running it.
// Required arguments are <angled>, optional ones [square].
func (c MCPPromptCommand) Describe() string {
	var args []string
	for _, a := range c.Arguments {
		if a.Required {
			args = append(args, "<"+a.Name+">")
		} else {
			args = append(args, "["+a.Name+"]")
		}
	}
	desc := strings.TrimSpace(c.Description)
	if len(args) == 0 {
		return desc
	}
	shape := strings.Join(args, " ")
	if desc == "" {
		return shape
	}
	return desc + " — " + shape
}

// MCPPromptListParams is reserved.
type MCPPromptListParams struct{}

// MCPPromptListResult is returned by mcp.prompts.
type MCPPromptListResult struct {
	Prompts []MCPPromptCommand `json:"prompts"`
}

// MCPPromptGetParams names the prompt to render. Args is the raw text typed
// after the command; the core maps it onto the prompt's declared arguments.
type MCPPromptGetParams struct {
	Server string `json:"server"`
	Name   string `json:"name"`
	Args   string `json:"args,omitempty"`
}

// MCPPromptGetResult carries the text to send as the user's turn.
type MCPPromptGetResult struct {
	Text string `json:"text"`
}
