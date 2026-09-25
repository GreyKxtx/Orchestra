package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// An MCP server tool is third-party code that runs for real: it is outside the
// staging overlay, so a preview turn that "changes nothing" could still write
// files through server-filesystem. Unless its server marks it read-only
// (annotations.readOnlyHint, carried as ToolDef.Mutating=false), a call needs
// the user's consent, the way bash does — and a mode that only reads is not
// offered it at all.

func isMCPToolName(name string) bool {
	return strings.HasPrefix(name, "mcp:")
}

// modeOffersWritingMCPTools reports whether mode may be offered MCP tools that
// can change things. These are the modes whose job is changing the project with
// no path scope; plan, architecture, product and documentation write only
// their own documents, and the rest only read.
func modeOffersWritingMCPTools(mode Mode) bool {
	switch mode {
	case "", ModeBuild, ModeAgent, ModeDebug, ModeGeneral, ModeWorker:
		return true
	}
	return false
}

// withoutWritingMCPTools drops the MCP tools not marked read-only.
func withoutWritingMCPTools(defs []llm.ToolDef) []llm.ToolDef {
	out := defs[:0:0]
	for _, d := range defs {
		if isMCPToolName(d.Function.Name) && d.Mutating {
			continue
		}
		out = append(out, d)
	}
	return out
}

// mcpToolReadOnly reports whether name is an MCP tool this run offered as
// read-only. A name it did not offer is not: the model can name any tool, and
// only its server's word lets a call through unasked.
func (a *Agent) mcpToolReadOnly(name string) bool {
	for _, d := range a.buildToolDefs() {
		if strings.EqualFold(d.Function.Name, name) {
			return !d.Mutating
		}
	}
	return false
}

// mcpCallNeedsConsent reports whether name is an MCP tool call that has to be
// allowed before it runs.
func (a *Agent) mcpCallNeedsConsent(name string) bool {
	return isMCPToolName(name) && !a.mcpToolReadOnly(name)
}

// mcpModeRefusal refuses an MCP tool that can change things in a mode that is
// not offered one. Asking the user there would put a write inside a turn they
// started in order not to have any.
func (a *Agent) mcpModeRefusal(name string) error {
	if !a.mcpCallNeedsConsent(name) || len(a.opts.CustomTools) > 0 || modeOffersWritingMCPTools(a.opts.Mode) {
		return nil
	}
	return fmt.Errorf("%s is not available in %s mode: its MCP server does not mark it read-only, and this mode does not change anything", name, a.opts.Mode)
}

// requestMCPConsent asks the user to allow one call. With nobody to ask it
// refuses, and says how to allow the tool without asking.
func (a *Agent) requestMCPConsent(ctx context.Context, name, input string) (approved bool, reason string) {
	if a.mcpAlwaysAllowed[strings.ToLower(name)] {
		return true, ""
	}
	if a.opts.PermissionRequester == nil {
		return false, fmt.Sprintf("%s needs the user's consent: its MCP server does not mark it read-only, and this run has no one to ask. "+
			"Tell the user; they can allow it with a permissions rule {tool: %q, action: allow}", name, name)
	}
	resp, err := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
		Tool:        name,
		Kind:        "mcp.tool",
		Description: permissionText(input),
		Reason:      "its MCP server does not mark this tool read-only, and it runs for real, even in a turn that does not apply changes",
	})
	if err != nil {
		return false, err.Error()
	}
	if !resp.Approved {
		if resp.Reason != "" {
			return false, resp.Reason
		}
		return false, name + " denied by the user"
	}
	if resp.Always {
		if a.mcpAlwaysAllowed == nil {
			a.mcpAlwaysAllowed = map[string]bool{}
		}
		a.mcpAlwaysAllowed[strings.ToLower(name)] = true
	}
	return true, ""
}
