package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orchestra/orchestra/internal/tools/web"
	"github.com/orchestra/orchestra/protocol/wire"
)

// rpcMethod serves one JSON-RPC method: it decodes the params and calls the
// core. RPCHandler.Handle looks the method up in rpcMethods, so the table is
// the list of what a connection answers; TestRPCHandler_ServesExactlyTheWireMethods
// holds it to wire.Methods().
type rpcMethod func(ctx context.Context, h *RPCHandler, raw json.RawMessage) (any, error)

// badParams is a params document that did not decode; Handle answers it
// with InvalidParams and the method's name.
type badParams struct{ err error }

func (e *badParams) Error() string { return e.err.Error() }

// serve builds an rpcMethod from a typed call. It is the one place params
// are decoded: Handle used to carry the same decode-and-refuse block once
// per method, 52 times.
func serve[P any](call func(ctx context.Context, h *RPCHandler, p P) (any, error)) rpcMethod {
	return func(ctx context.Context, h *RPCHandler, raw json.RawMessage) (any, error) {
		var p P
		if err := decodeParams(raw, &p); err != nil {
			return nil, &badParams{err: err}
		}
		return call(ctx, h, p)
	}
}

// onEvent is the notifier a turn streams to, or nil without a client.
func (h *RPCHandler) onEvent() func(method string, params any) {
	if h.notifier == nil {
		return nil
	}
	return func(method string, params any) {
		_ = h.notifier.Notify(method, params)
	}
}

// panelCtx is ctx with this connection's browser when the turn asked for it.
func (h *RPCHandler) panelCtx(ctx context.Context, wanted, drive, eval bool) context.Context {
	return h.browserPanelCtx(ctx, wanted, web.PanelPermits{Drive: drive, Eval: eval})
}

var rpcMethods = map[string]rpcMethod{
	wire.MethodCoreHealth: func(_ context.Context, h *RPCHandler, _ json.RawMessage) (any, error) {
		return h.core.Health(), nil
	},
	wire.MethodInitialize: serve(func(_ context.Context, h *RPCHandler, p InitializeParams) (any, error) {
		return h.core.Initialize(p)
	}),
	wire.MethodAgentRun: serve(func(ctx context.Context, h *RPCHandler, p AgentRunParams) (any, error) {
		p.OnEvent = h.onEvent()
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
			p.QuestionAsker = h.questionAskerForRun()
		}
		return h.core.AgentRun(h.panelCtx(ctx, p.BrowserPanel, p.AllowBrowserDrive, p.AllowBrowserEval), p)
	}),
	wire.MethodToolCall: serve(func(ctx context.Context, h *RPCHandler, p ToolCallParams) (any, error) {
		out, err := h.core.ToolCall(ctx, p)
		if err != nil {
			return nil, err
		}
		// The tool's output goes out as a JSON object, not as a JSON string.
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			return nil, fmt.Errorf("tool output is not valid json: %w", err)
		}
		return v, nil
	}),
	wire.MethodOpsApply: serve(func(ctx context.Context, h *RPCHandler, p OpsApplyParams) (any, error) {
		return h.core.OpsApply(ctx, p)
	}),

	wire.MethodSessionStart: serve(func(_ context.Context, h *RPCHandler, p SessionStartParams) (any, error) {
		return h.core.SessionStart(p)
	}),
	wire.MethodSessionGet: serve(func(_ context.Context, h *RPCHandler, p SessionGetParams) (any, error) {
		return h.core.SessionGet(p)
	}),
	wire.MethodSessionList: serve(func(_ context.Context, h *RPCHandler, p SessionListParams) (any, error) {
		return h.core.SessionList(p)
	}),
	wire.MethodSessionUISync: serve(func(_ context.Context, h *RPCHandler, p SessionUISyncParams) (any, error) {
		return h.core.SessionUISync(p)
	}),
	wire.MethodSessionMessage: serve(func(ctx context.Context, h *RPCHandler, p SessionMessageParams) (any, error) {
		p.OnEvent = h.onEvent()
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
			p.QuestionAsker = h.questionAskerForRun()
		}
		return h.core.SessionMessage(h.panelCtx(ctx, p.BrowserPanel, p.AllowBrowserDrive, p.AllowBrowserEval), p)
	}),
	wire.MethodSessionHistory: serve(func(_ context.Context, h *RPCHandler, p SessionHistoryParams) (any, error) {
		return h.core.SessionHistory(p)
	}),
	wire.MethodSessionCompact: serve(func(ctx context.Context, h *RPCHandler, p SessionCompactParams) (any, error) {
		return h.core.SessionCompact(ctx, p)
	}),
	wire.MethodSessionRewind: serve(func(_ context.Context, h *RPCHandler, p SessionRewindParams) (any, error) {
		return h.core.SessionRewind(p)
	}),
	wire.MethodSessionFork: serve(func(_ context.Context, h *RPCHandler, p SessionForkParams) (any, error) {
		return h.core.SessionFork(p)
	}),
	wire.MethodSessionSearch: serve(func(_ context.Context, h *RPCHandler, p SessionSearchParams) (any, error) {
		return h.core.SessionSearch(p)
	}),
	wire.MethodSessionTrajectory: serve(func(_ context.Context, h *RPCHandler, p SessionTrajectoryParams) (any, error) {
		return h.core.SessionTrajectory(p)
	}),
	wire.MethodSessionCancel: serve(func(_ context.Context, h *RPCHandler, p SessionCancelParams) (any, error) {
		return nil, h.core.SessionCancel(p)
	}),
	wire.MethodSessionApplyPending: serve(func(ctx context.Context, h *RPCHandler, p SessionApplyPendingParams) (any, error) {
		return h.core.SessionApplyPending(ctx, p)
	}),
	wire.MethodSessionDiscardPending: serve(func(_ context.Context, h *RPCHandler, p SessionDiscardPendingParams) (any, error) {
		return h.core.SessionDiscardPending(p)
	}),
	wire.MethodSessionClose: serve(func(_ context.Context, h *RPCHandler, p SessionCloseParams) (any, error) {
		return nil, h.core.SessionClose(p)
	}),

	wire.MethodRuntimeSetModel: serve(func(ctx context.Context, h *RPCHandler, p RuntimeSetModelParams) (any, error) {
		return h.core.RuntimeSetModel(ctx, p)
	}),
	wire.MethodRuntimeListModels: serve(func(ctx context.Context, h *RPCHandler, p RuntimeListModelsParams) (any, error) {
		return h.core.RuntimeListModels(ctx, p)
	}),
	wire.MethodRuntimeListProviders: serve(func(ctx context.Context, h *RPCHandler, p RuntimeListProvidersParams) (any, error) {
		return h.core.RuntimeListProviders(ctx, p)
	}),
	wire.MethodRuntimeGetLLM: serve(func(_ context.Context, h *RPCHandler, p RuntimeGetLLMParams) (any, error) {
		return h.core.RuntimeGetLLM(p)
	}),
	wire.MethodRuntimeCredits: serve(func(ctx context.Context, h *RPCHandler, p RuntimeCreditsParams) (any, error) {
		return h.core.RuntimeCredits(ctx, p)
	}),
	wire.MethodRuntimeConfigureLLM: serve(func(ctx context.Context, h *RPCHandler, p RuntimeConfigureLLMParams) (any, error) {
		return h.core.RuntimeConfigureLLM(ctx, p)
	}),
	wire.MethodRuntimeGetOrchestra: serve(func(_ context.Context, h *RPCHandler, p RuntimeGetOrchestraParams) (any, error) {
		return h.core.RuntimeGetOrchestra(p)
	}),
	wire.MethodRuntimeConfigureOrchestra: serve(func(_ context.Context, h *RPCHandler, p RuntimeConfigureOrchestraParams) (any, error) {
		return h.core.RuntimeConfigureOrchestra(p)
	}),
	wire.MethodRuntimeGetSystemPrompt: serve(func(_ context.Context, h *RPCHandler, p RuntimeGetSystemPromptParams) (any, error) {
		return h.core.RuntimeGetSystemPrompt(p)
	}),
	wire.MethodRuntimeSetSystemPrompt: serve(func(_ context.Context, h *RPCHandler, p RuntimeSetSystemPromptParams) (any, error) {
		return h.core.RuntimeSetSystemPrompt(p)
	}),

	wire.MethodWorkspaceTrustStatus: func(_ context.Context, h *RPCHandler, _ json.RawMessage) (any, error) {
		return h.core.WorkspaceTrustStatus()
	},
	wire.MethodWorkspaceTrust: serve(func(ctx context.Context, h *RPCHandler, p WorkspaceTrustParams) (any, error) {
		return h.core.WorkspaceTrust(ctx, p)
	}),

	wire.MethodMCPList: serve(func(_ context.Context, h *RPCHandler, p MCPListParams) (any, error) {
		return h.core.MCPList(p)
	}),
	wire.MethodMCPPrompts: serve(func(ctx context.Context, h *RPCHandler, p MCPPromptListParams) (any, error) {
		return h.core.MCPPromptList(ctx, p)
	}),
	wire.MethodMCPPromptGet: serve(func(ctx context.Context, h *RPCHandler, p MCPPromptGetParams) (any, error) {
		return h.core.MCPPromptGet(ctx, p)
	}),
	wire.MethodMCPUpsert: serve(func(ctx context.Context, h *RPCHandler, p MCPUpsertParams) (any, error) {
		return h.core.MCPUpsert(ctx, p)
	}),
	wire.MethodMCPDelete: serve(func(ctx context.Context, h *RPCHandler, p MCPDeleteParams) (any, error) {
		return h.core.MCPDelete(ctx, p)
	}),
	wire.MethodMCPSetDisabled: serve(func(ctx context.Context, h *RPCHandler, p MCPSetDisabledParams) (any, error) {
		return h.core.MCPSetDisabled(ctx, p)
	}),
	wire.MethodMCPTest: serve(func(ctx context.Context, h *RPCHandler, p MCPTestParams) (any, error) {
		return h.core.MCPTest(ctx, p)
	}),

	wire.MethodAgentsList: serve(func(_ context.Context, h *RPCHandler, p AgentsListParams) (any, error) {
		return h.core.AgentsList(p)
	}),
	wire.MethodAgentsUpsert: serve(func(_ context.Context, h *RPCHandler, p AgentsUpsertParams) (any, error) {
		return h.core.AgentsUpsert(p)
	}),
	wire.MethodAgentsDelete: serve(func(_ context.Context, h *RPCHandler, p AgentsDeleteParams) (any, error) {
		return h.core.AgentsDelete(p)
	}),

	wire.MethodIndexStatus: serve(func(_ context.Context, h *RPCHandler, p IndexStatusParams) (any, error) {
		return h.core.IndexStatus(p)
	}),
	wire.MethodIndexConfigure: serve(func(_ context.Context, h *RPCHandler, p IndexConfigureParams) (any, error) {
		return h.core.IndexConfigure(p)
	}),
	wire.MethodIndexRebuild: serve(func(ctx context.Context, h *RPCHandler, p IndexRebuildParams) (any, error) {
		return h.core.IndexRebuild(ctx, p)
	}),
	wire.MethodIndexEmbed: serve(func(ctx context.Context, h *RPCHandler, p IndexEmbedParams) (any, error) {
		return h.core.IndexEmbed(ctx, p)
	}),
	wire.MethodIndexGraph: serve(func(ctx context.Context, h *RPCHandler, p IndexGraphParams) (any, error) {
		return h.core.IndexGraph(ctx, p)
	}),
	wire.MethodIndexOutline: serve(func(ctx context.Context, h *RPCHandler, p IndexOutlineParams) (any, error) {
		return h.core.IndexOutline(ctx, p)
	}),

	wire.MethodAttachmentsStore: serve(func(_ context.Context, h *RPCHandler, p AttachmentsStoreParams) (any, error) {
		return h.core.AttachmentsStore(p)
	}),

	wire.MethodWorkflowList: serve(func(_ context.Context, h *RPCHandler, p WorkflowListParams) (any, error) {
		return h.core.WorkflowList(p)
	}),
	wire.MethodWorkflowRun: serve(func(ctx context.Context, h *RPCHandler, p WorkflowRunParams) (any, error) {
		p.OnEvent = h.onEvent()
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
		}
		return h.core.WorkflowRun(ctx, p)
	}),
	wire.MethodSkillList: serve(func(_ context.Context, h *RPCHandler, p SkillListParams) (any, error) {
		return h.core.SkillList(p)
	}),
	wire.MethodSkillInvoke: serve(func(ctx context.Context, h *RPCHandler, p SkillInvokeParams) (any, error) {
		p.OnEvent = h.onEvent()
		if h.requester != nil {
			p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
		}
		return h.core.SkillInvoke(ctx, p)
	}),

	wire.MethodLessonRuleRespond: serve(func(_ context.Context, h *RPCHandler, p RuleSuggestionRespondParams) (any, error) {
		return h.core.RuleSuggestionRespond(p)
	}),
}
