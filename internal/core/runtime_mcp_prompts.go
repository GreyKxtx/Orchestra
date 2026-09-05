package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

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

// fill computes the rendered fields.
func (c *MCPPromptCommand) fill() {
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

// MCPPromptList returns every prompt offered by the running servers. No MCP
// servers is an empty list, not an error: the palette asks unconditionally.
func (c *Core) MCPPromptList(ctx context.Context, _ MCPPromptListParams) (*MCPPromptListResult, error) {
	res := &MCPPromptListResult{}
	if c == nil || c.mcpManager == nil {
		return res, nil
	}
	for _, p := range c.mcpManager.ListPrompts(ctx) {
		cmd := MCPPromptCommand{Server: p.Server, Name: p.Name, Description: p.Description}
		for _, a := range p.Arguments {
			cmd.Arguments = append(cmd.Arguments, MCPPromptArgView{
				Name: a.Name, Description: a.Description, Required: a.Required,
			})
		}
		cmd.fill()
		res.Prompts = append(res.Prompts, cmd)
	}
	return res, nil
}

// MCPPromptGet renders one prompt into the text to send.
func (c *Core) MCPPromptGet(ctx context.Context, p MCPPromptGetParams) (*MCPPromptGetResult, error) {
	if c == nil || c.mcpManager == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "no MCP servers are running", nil)
	}
	spec := c.promptArgSpec(ctx, p.Server, p.Name)
	args, err := parsePromptArgs(spec, p.Args)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	text, err := c.mcpManager.GetPrompt(ctx, p.Server, p.Name, args)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &MCPPromptGetResult{Text: text}, nil
}

// promptArgSpec finds one prompt's declared arguments, or nil.
func (c *Core) promptArgSpec(ctx context.Context, server, name string) []MCPPromptArgView {
	res, err := c.MCPPromptList(ctx, MCPPromptListParams{})
	if err != nil {
		return nil
	}
	for _, cmd := range res.Prompts {
		if cmd.Server == server && cmd.Name == name {
			return cmd.Arguments
		}
	}
	return nil
}

// parsePromptArgs maps the text a user typed after the command onto the
// prompt's declared arguments.
//
// The mapping is positional and deliberately simple: each argument takes one
// word, except the last, which takes everything that is left. That makes
// "/mcp:linear:triage ENG-1 looks flaky in CI" work the way a person expects,
// and a single-argument prompt take the whole line.
//
// A missing *required* argument is an error naming it, rather than a call
// with an empty string — the server would answer something confusing and the
// user would have no idea which field was blank.
func parsePromptArgs(spec []MCPPromptArgView, raw string) (map[string]string, error) {
	if len(spec) == 0 {
		return nil, nil
	}
	rest := strings.TrimSpace(raw)
	out := make(map[string]string, len(spec))
	for i, arg := range spec {
		if rest == "" {
			if arg.Required {
				return nil, fmt.Errorf("prompt argument %q is required", arg.Name)
			}
			continue
		}
		if i == len(spec)-1 {
			out[arg.Name] = rest
			rest = ""
			continue
		}
		word, remainder, _ := strings.Cut(rest, " ")
		out[arg.Name] = word
		rest = strings.TrimSpace(remainder)
	}
	return out, nil
}
