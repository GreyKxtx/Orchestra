package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// gateCall is one tool call on its way through the gates. The consent a gate
// grants — a permission rule that allows the call, a user who approves a
// command — is recorded here for the gates after it.
type gateCall struct {
	name  string
	input json.RawMessage

	allowExec    bool
	allowWeb     bool
	mcpConsented bool
	// userApproved is the user's yes for this very call: an allow rule that
	// matched it, or an approval asked for it. A tainted turn needs it for
	// calls that act for the user (gateTaint).
	userApproved bool
}

// toolGate refuses a call by returning the reason, or lets it through with "".
type toolGate func(a *Agent, ctx context.Context, c *gateCall, history []llm.Message) string

// toolGates is the one chain every tool call passes, on the serial path and
// the parallel one alike, in this order:
//
//  1. what the call is: browser tools without browser consent, an MCP tool
//     that can write in a mode that does not, a tool this agent was not given;
//  2. the explore-first gate;
//  3. permission rules (deny, allow — which is consent for the gates below —
//     or ask the user);
//  4. consent for an MCP tool that can write, for bash, for the web;
//  5. the tainted turn: per-call approval for what acts for the user, no
//     pinned or global memory (taint.go);
//  6. the mode's write scope (roles.Spec.Write);
//  7. the human gates on git.commit and git.push.
//
// It used to be two chains. The serial dispatcher ran its checks inline — the
// refusal block was written out 18 times — and the parallel batch ran only the
// browser check and PreTool hooks, relying on batchNeedsSerialGates to send
// anything else to the serial path. Each new gate had to be added in both, and
// SEC-4 (a tool the mode never offered ran by name) came from a check that
// neither path had.
var toolGates = []toolGate{
	(*Agent).gateSurface,
	(*Agent).gateExploreFirst,
	(*Agent).gatePermissionRules,
	(*Agent).gateMCPConsent,
	(*Agent).gateExecConsent,
	(*Agent).gateWebConsent,
	(*Agent).gateTaint,
	(*Agent).gateWriteScope,
	(*Agent).gateHuman,
}

// runToolGates passes c through every gate and returns the first refusal, or
// "" when the call may run.
func (a *Agent) runToolGates(ctx context.Context, c *gateCall, history []llm.Message) string {
	for _, gate := range toolGates {
		if reason := gate(a, ctx, c, history); reason != "" {
			return reason
		}
	}
	return ""
}

// newGateCall starts a call with the run's static consent.
func (a *Agent) newGateCall(name string, input json.RawMessage) *gateCall {
	return &gateCall{name: name, input: input, allowExec: a.opts.AllowExec, allowWeb: a.opts.AllowWeb}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *Agent) gateSurface(_ context.Context, c *gateCall, _ []llm.Message) string {
	if err := a.browserCallRefusal(c.name); err != nil {
		return err.Error()
	}
	if err := a.mcpModeRefusal(c.name); err != nil {
		return err.Error()
	}
	return errText(a.offeredToolRefusal(c.name))
}

func (a *Agent) gateExploreFirst(_ context.Context, c *gateCall, history []llm.Message) string {
	return errText(a.checkExploreFirstGate(c.name, history))
}

// gatePermissionRules applies the permission rules (cfg.Permissions.Rules):
// deny refuses, allow is consent for exec, web and MCP for this call, and ask
// puts the call to the user, whose yes is the MCP consent.
func (a *Agent) gatePermissionRules(ctx context.Context, c *gateCall, _ []llm.Message) string {
	if len(a.opts.PermissionRules) == 0 {
		return ""
	}
	subject := subjectForTool(c.name, c.input)
	act, matched := checkPermissions(a.opts.PermissionRules, c.name, subject)
	if !matched {
		return ""
	}
	switch act {
	case "deny":
		return "tool call denied by permission ruleset"
	case "allow":
		c.allowExec, c.allowWeb, c.mcpConsented = true, true, true
		c.userApproved = true
	case "ask":
		approved, err := a.requestInteractivePermission(ctx, c.name, subject, c.input)
		if err != nil {
			return err.Error()
		}
		if !approved {
			return "tool call denied by interactive permission requester"
		}
		c.mcpConsented, c.userApproved = true, true
	}
	return ""
}

func (a *Agent) gateMCPConsent(ctx context.Context, c *gateCall, _ []llm.Message) string {
	if c.mcpConsented || !a.mcpCallNeedsConsent(c.name) {
		return ""
	}
	if approved, reason := a.requestMCPConsent(ctx, c.name, string(c.input)); !approved {
		return reason
	}
	return ""
}

// gateExecConsent lets bash run on static consent, on the user's yes to this
// command, or on the exec allowlist. The whole command is what the user
// approves (permissionText).
func (a *Agent) gateExecConsent(ctx context.Context, c *gateCall, _ []llm.Message) string {
	if c.name != "bash" || c.allowExec {
		return ""
	}
	cmd, args := execCommandFromInput(c.input)
	if a.opts.PermissionRequester != nil {
		line := cmd
		if len(args) > 0 {
			line += " " + strings.Join(args, " ")
		}
		if strings.TrimSpace(line) == "" {
			line = string(c.input)
		}
		resp, err := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
			Tool:        "bash",
			Description: permissionText(line),
		})
		if err == nil && resp.Approved {
			c.allowExec, c.userApproved = true, true
			return ""
		}
		if err == nil {
			if resp.Reason != "" {
				return resp.Reason
			}
			return "exec.run denied by interactive permission requester"
		}
		// A requester that failed to ask leaves the allowlist to decide.
	}
	if ok, why := execCommandAllowed(cmd, args, a.opts.ExecAllow, a.opts.ExecDeny); !ok {
		if len(a.opts.ExecAllow) > 0 {
			return fmt.Sprintf("exec.run: command %q is not covered by the allowlist: %s", cmd, why)
		}
		return "exec.run requires user consent (use --allow-exec or configure exec.allow)"
	}
	return ""
}

func (a *Agent) gateWebConsent(_ context.Context, c *gateCall, _ []llm.Message) string {
	if isWebTool(c.name) && !c.allowWeb {
		return c.name + " requires user consent (use --allow-web, or web.confirm: false)"
	}
	return ""
}

func (a *Agent) gateWriteScope(_ context.Context, c *gateCall, _ []llm.Message) string {
	return errText(a.writeScopeRefusal(c.name, c.input))
}

func (a *Agent) gateHuman(ctx context.Context, c *gateCall, _ []llm.Message) string {
	return errText(a.confirmHumanGate(ctx, c.name, c.input))
}

// refuseCall answers a call a gate refused: the denial goes into history and
// the log, and counts toward the denied-repeat breaker.
func (a *Agent) refuseCall(cb *CircuitBreaker, history *[]llm.Message, toolCallID string, c *gateCall, reason string) (serialToolOutcome, error) {
	*history = append(*history, llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: toolCallID,
		Content:    a.deniedToolResult(c.name, c.input, reason),
	})
	if cbErr := cb.RecordDenied(c.name); cbErr != nil {
		return serialToolOutcome{}, cbErr
	}
	return serialToolOutcome{}, nil
}

// breakerErr turns a breaker's typed result into an error without making a
// nil *protocol.Error a non-nil error.
func breakerErr(e *protocol.Error) error {
	if e == nil {
		return nil
	}
	return e
}
