package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/protocol"
)

// Notifier sends server-initiated JSON-RPC notifications to the client.
// *jsonrpc.Server implements this interface.
type Notifier interface {
	Notify(method string, params any) error
}

// RPCHandler adapts Core to the jsonrpc.Handler interface.
type RPCHandler struct {
	core      *Core
	notifier  Notifier // optional; nil = no streaming notifications
	requester func(ctx context.Context, method string, params any, result any) error
}

func NewRPCHandler(c *Core) *RPCHandler {
	return &RPCHandler{core: c}
}

// SetNotifier attaches a Notifier so that agent.run can emit streaming events
// as JSON-RPC notifications. Call this after constructing both the Server and handler.
func (h *RPCHandler) SetNotifier(n Notifier) {
	h.notifier = n
}

// SetRequester attaches a request function so that agent.run can issue
// server-initiated requests (e.g. permission/request) to the client.
func (h *RPCHandler) SetRequester(fn func(ctx context.Context, method string, params any, result any) error) {
	h.requester = fn
}

func (h *RPCHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if h == nil || h.core == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	method = strings.TrimSpace(method)

	// Handshake requirement: initialize must be called before mutating / tool methods.
	if method != "core.health" && method != "initialize" && !h.core.IsInitialized() {
		return nil, protocol.NewError(protocol.NotInitialized, "initialize required", map[string]any{
			"method": method,
		})
	}

	switch method {
	case "core.health":
		return h.core.Health(), nil

	case "initialize":
		var p InitializeParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.Initialize(p)

	case "agent.run":
		var p AgentRunParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		// Wire streaming notifications when a notifier is available.
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
			p.QuestionAsker = &rpcQuestionAsker{requestFn: h.requester}
		}
		return h.core.AgentRun(ctx, p)

	case "tool.call":
		var p ToolCallParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		out, err := h.core.ToolCall(ctx, p)
		if err != nil {
			return nil, err
		}
		// Return the tool output as a JSON object, not as a JSON-encoded string.
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			return nil, fmt.Errorf("tool output is not valid json: %w", err)
		}
		return v, nil

	case "session.start":
		var p SessionStartParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionStart(p)

	case "session.get":
		var p SessionGetParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionGet(p)

	case "session.list":
		var p SessionListParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionList(p)

	case "session.ui_sync":
		var p SessionUISyncParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionUISync(p)

	case "session.message":
		var p SessionMessageParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
			p.QuestionAsker = &rpcQuestionAsker{requestFn: h.requester}
		}
		return h.core.SessionMessage(ctx, p)

	case "session.history":
		var p SessionHistoryParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionHistory(p)

	case "session.compact":
		var p SessionCompactParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionCompact(ctx, p)

	case "session.rewind":
		var p SessionRewindParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionRewind(p)

	case "runtime.set_model":
		var p RuntimeSetModelParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeSetModel(ctx, p)

	case "runtime.list_models":
		var p RuntimeListModelsParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeListModels(ctx, p)

	case "runtime.list_providers":
		var p RuntimeListProvidersParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeListProviders(ctx, p)

	case "runtime.get_llm":
		var p RuntimeGetLLMParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeGetLLM(p)

	case "runtime.configure_llm":
		var p RuntimeConfigureLLMParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeConfigureLLM(ctx, p)

	case "runtime.get_system_prompt":
		var p RuntimeGetSystemPromptParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeGetSystemPrompt(p)

	case "runtime.set_system_prompt":
		var p RuntimeSetSystemPromptParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.RuntimeSetSystemPrompt(p)

	case "mcp.list":
		var p MCPListParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.MCPList(p)

	case "mcp.upsert":
		var p MCPUpsertParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.MCPUpsert(ctx, p)

	case "mcp.delete":
		var p MCPDeleteParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.MCPDelete(ctx, p)

	case "mcp.set_disabled":
		var p MCPSetDisabledParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.MCPSetDisabled(ctx, p)

	case "mcp.test":
		var p MCPTestParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.MCPTest(ctx, p)

	case "agents.list":
		var p AgentsListParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.AgentsList(p)

	case "agents.upsert":
		var p AgentsUpsertParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.AgentsUpsert(p)

	case "agents.delete":
		var p AgentsDeleteParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.AgentsDelete(p)

	case "index.status":
		var p IndexStatusParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.IndexStatus(p)

	case "index.configure":
		var p IndexConfigureParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.IndexConfigure(p)

	case "index.rebuild":
		var p IndexRebuildParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.IndexRebuild(ctx, p)

	case "index.embed":
		var p IndexEmbedParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.IndexEmbed(ctx, p)

	case "ops.apply":
		var p OpsApplyParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), nil)
		}
		return h.core.OpsApply(ctx, p)

	case "session.cancel":
		var p SessionCancelParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return nil, h.core.SessionCancel(p)

	case "session.apply_pending":
		var p SessionApplyPendingParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionApplyPending(ctx, p)

	case "session.close":
		var p SessionCloseParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return nil, h.core.SessionClose(p)

	case "workflow.list":
		var p WorkflowListParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{"method": method})
		}
		return h.core.WorkflowList(p)

	case "workflow.run":
		var p WorkflowRunParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{"method": method})
		}
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
		}
		return h.core.WorkflowRun(ctx, p)

	case "skill.list":
		var p SkillListParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{"method": method})
		}
		return h.core.SkillList(p)

	case "skill.invoke":
		var p SkillInvokeParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{"method": method})
		}
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
		}
		return h.core.SkillInvoke(ctx, p)

	default:
		return nil, jsonrpc.MethodNotFound(method)
	}
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}
