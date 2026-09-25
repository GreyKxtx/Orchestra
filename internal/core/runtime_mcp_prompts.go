package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

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
		cmd.Fill()
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

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	MCPPromptArgView    = wire.MCPPromptArgView
	MCPPromptCommand    = wire.MCPPromptCommand
	MCPPromptGetParams  = wire.MCPPromptGetParams
	MCPPromptGetResult  = wire.MCPPromptGetResult
	MCPPromptListParams = wire.MCPPromptListParams
	MCPPromptListResult = wire.MCPPromptListResult
)
