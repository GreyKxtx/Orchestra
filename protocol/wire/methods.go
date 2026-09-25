package wire

import "sort"

// The names on the wire. InitializeResult.Capabilities lists the ones a
// core serves; a client checks for the name it needs there.
const (
	MethodCoreHealth = "core.health"
	MethodInitialize = "initialize"
	MethodAgentRun   = "agent.run"
	MethodToolCall   = "tool.call"
	MethodOpsApply   = "ops.apply"

	MethodSessionStart          = "session.start"
	MethodSessionGet            = "session.get"
	MethodSessionList           = "session.list"
	MethodSessionUISync         = "session.ui_sync"
	MethodSessionMessage        = "session.message"
	MethodSessionHistory        = "session.history"
	MethodSessionCompact        = "session.compact"
	MethodSessionRewind         = "session.rewind"
	MethodSessionFork           = "session.fork"
	MethodSessionSearch         = "session.search"
	MethodSessionTrajectory     = "session.trajectory"
	MethodSessionCancel         = "session.cancel"
	MethodSessionApplyPending   = "session.apply_pending"
	MethodSessionDiscardPending = "session.discard_pending"
	MethodSessionClose          = "session.close"

	MethodRuntimeSetModel           = "runtime.set_model"
	MethodRuntimeListModels         = "runtime.list_models"
	MethodRuntimeListProviders      = "runtime.list_providers"
	MethodRuntimeGetLLM             = "runtime.get_llm"
	MethodRuntimeCredits            = "runtime.credits"
	MethodRuntimeConfigureLLM       = "runtime.configure_llm"
	MethodRuntimeGetOrchestra       = "runtime.get_orchestra"
	MethodRuntimeConfigureOrchestra = "runtime.configure_orchestra"
	MethodRuntimeGetSystemPrompt    = "runtime.get_system_prompt"
	MethodRuntimeSetSystemPrompt    = "runtime.set_system_prompt"

	MethodWorkspaceTrustStatus = "workspace.trust_status"
	MethodWorkspaceTrust       = "workspace.trust"

	MethodMCPList        = "mcp.list"
	MethodMCPPrompts     = "mcp.prompts"
	MethodMCPPromptGet   = "mcp.prompt.get"
	MethodMCPUpsert      = "mcp.upsert"
	MethodMCPDelete      = "mcp.delete"
	MethodMCPSetDisabled = "mcp.set_disabled"
	MethodMCPTest        = "mcp.test"

	MethodAgentsList   = "agents.list"
	MethodAgentsUpsert = "agents.upsert"
	MethodAgentsDelete = "agents.delete"

	MethodIndexStatus    = "index.status"
	MethodIndexConfigure = "index.configure"
	MethodIndexRebuild   = "index.rebuild"
	MethodIndexEmbed     = "index.embed"
	MethodIndexGraph     = "index.graph"
	MethodIndexOutline   = "index.outline"

	MethodAttachmentsStore = "attachments.store"

	MethodWorkflowList = "workflow.list"
	MethodWorkflowRun  = "workflow.run"
	MethodSkillList    = "skill.list"
	MethodSkillInvoke  = "skill.invoke"

	MethodLessonRuleRespond = "lesson.rule_respond"

	// MethodCancelRequest is answered by the transport (protocol/jsonrpc),
	// not by the core's handler: it cancels the request whose id it names.
	MethodCancelRequest = "$/cancelRequest"
)

// Notifications the core sends.
const (
	// NotifyAgentEvent carries an AgentEvent.
	NotifyAgentEvent = "agent/event"
	// NotifyExecOutputChunk carries an ExecOutputChunk.
	NotifyExecOutputChunk = "exec/output_chunk"
	// NotifyWorkflowStageStart and NotifyWorkflowStageDone carry a WorkflowStage.
	NotifyWorkflowStageStart = "workflow/stage_start"
	NotifyWorkflowStageDone  = "workflow/stage_done"
)

// Requests the core makes of the client.
const (
	// RequestPermission asks consent for a tool (PermissionRequest → PermissionDecision).
	RequestPermission = "permission/request"
	// RequestQuestionAsk puts the model's questions to the person (QuestionAsk → QuestionAnswers).
	RequestQuestionAsk = "question/ask"
	// RequestBrowserCall runs a browser op on the client's own browser view (BrowserCall).
	RequestBrowserCall = "browser/call"
)

var methods = []string{
	MethodCoreHealth, MethodInitialize, MethodAgentRun, MethodToolCall, MethodOpsApply,
	MethodSessionStart, MethodSessionGet, MethodSessionList, MethodSessionUISync, MethodSessionMessage,
	MethodSessionHistory, MethodSessionCompact, MethodSessionRewind, MethodSessionFork, MethodSessionSearch,
	MethodSessionTrajectory, MethodSessionCancel, MethodSessionApplyPending, MethodSessionDiscardPending, MethodSessionClose,
	MethodRuntimeSetModel, MethodRuntimeListModels, MethodRuntimeListProviders, MethodRuntimeGetLLM, MethodRuntimeCredits,
	MethodRuntimeConfigureLLM, MethodRuntimeGetOrchestra, MethodRuntimeConfigureOrchestra, MethodRuntimeGetSystemPrompt, MethodRuntimeSetSystemPrompt,
	MethodWorkspaceTrustStatus, MethodWorkspaceTrust,
	MethodMCPList, MethodMCPPrompts, MethodMCPPromptGet, MethodMCPUpsert, MethodMCPDelete, MethodMCPSetDisabled, MethodMCPTest,
	MethodAgentsList, MethodAgentsUpsert, MethodAgentsDelete,
	MethodIndexStatus, MethodIndexConfigure, MethodIndexRebuild, MethodIndexEmbed, MethodIndexGraph, MethodIndexOutline,
	MethodAttachmentsStore,
	MethodWorkflowList, MethodWorkflowRun, MethodSkillList, MethodSkillInvoke,
	MethodLessonRuleRespond,
	MethodCancelRequest,
}

var notifications = []string{
	NotifyAgentEvent, NotifyExecOutputChunk, NotifyWorkflowStageStart, NotifyWorkflowStageDone,
}

var requests = []string{
	RequestPermission, RequestQuestionAsk, RequestBrowserCall,
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Methods lists every method a connection answers, sorted.
func Methods() []string { return sorted(methods) }

// Notifications lists every notification the core sends, sorted.
func Notifications() []string { return sorted(notifications) }

// Requests lists every request the core makes of the client, sorted.
func Requests() []string { return sorted(requests) }

// CoreCapabilities is what this core serves.
func CoreCapabilities() Capabilities {
	return Capabilities{Methods: Methods(), Notifications: Notifications(), Requests: Requests()}
}
