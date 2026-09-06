package tui

import (
	"context"
	"encoding/json"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// coreClient is the App-side seam over the core RPC connection. Production
// code always uses *rpcclient.Client (spawned subprocess + JSON-RPC stdio);
// tests substitute a scripted fake so App flows (turns, permissions, diff
// apply, workflows) can be exercised without spawning a process.
//
// Keep this interface in sync with what app_*.go actually calls — it is the
// complete inventory of the App→core surface, which also makes coupling
// reviews trivial.
type coreClient interface {
	// Events is the stream of core→TUI events consumed by listenForEvents.
	Events() <-chan rpcclient.Event
	Close() error

	// Session lifecycle / persistence.
	SessionStart(ctx context.Context, sessionID string) (id string, restored bool, err error)
	SessionGet(ctx context.Context, sessionID string) (*rpcclient.SessionGetResult, error)
	SessionUISync(ctx context.Context, sessionID, title, model string, ui []sessionfile.UIMessage, costUSD float64) error
	SessionRewind(ctx context.Context, sessionID string, uiMessageIndex int) (*rpcclient.SessionRewindResult, error)
	SessionFork(ctx context.Context, sessionID string, uiMessageIndex int) (*rpcclient.SessionForkResult, error)
	SessionCompact(ctx context.Context, sessionID, query string) (*rpcclient.SessionCompactResult, error)

	// Agent turns.
	SessionMessage(ctx context.Context, sessionID, query, mode string, opts rpcclient.AgentRunOptions) error
	AgentRun(ctx context.Context, query, mode string, opts rpcclient.AgentRunOptions) error

	// Direct ops / tools (diff review revert, manual apply).
	ApplyOps(ctx context.Context, rawOps []map[string]any) error
	ToolCall(ctx context.Context, tool string, input json.RawMessage) error

	// Workflows & skills.
	WorkflowList(ctx context.Context) ([]rpcclient.WorkflowSummary, error)
	WorkflowRun(ctx context.Context, name, arguments string, opts rpcclient.WorkflowRunOptions) (*rpcclient.WorkflowRunResult, error)
	SkillList(ctx context.Context) ([]rpcclient.SkillSummary, error)
	SkillInvoke(ctx context.Context, name, arguments string, opts rpcclient.SkillInvokeOptions) (*rpcclient.SkillInvokeResult, error)

	// MCP server prompts, offered in the slash palette.
	MCPPromptList(ctx context.Context) ([]rpcclient.MCPPromptCommand, error)
	MCPPromptGet(ctx context.Context, server, name, args string) (string, error)

	// Status polling.
	QueryLSPStatusDetail(ctx context.Context) (status string, percent int, id string, err error)

	// Answers to server-initiated permission/question requests.
	RespondPermission(reqID int64, approved bool)
	RespondPermissionDecision(reqID int64, d rpcclient.PermissionDecision)
	RespondQuestion(reqID int64, answers []string)

	// RespondRuleSuggestion answers a rule_suggestion event (repeated
	// anti-pattern on one file, offered as an ORCHESTRA.md rule).
	RespondRuleSuggestion(ctx context.Context, accept bool, s rpcclient.RuleSuggestion) error
}

// Compile-time proof that the production client satisfies the seam.
var _ coreClient = (*rpcclient.Client)(nil)
