package wire

// Signature names a method's params and result types on the wire. An empty
// name means the method has none (core.health takes no params) or that the
// type stays in internal/core because it carries a type of the patch module
// (ops.AnyOp, patches.Patch), of the config, of the session snapshot or of
// the code graph; docs/PROTOCOL.md describes those.
type Signature struct {
	Method string
	Params string
	Result string
}

var signatures = []Signature{
	{MethodCoreHealth, "", "Health"},
	{MethodInitialize, "InitializeParams", "InitializeResult"},
	{MethodAgentRun, "", ""},
	{MethodToolCall, "ToolCallParams", ""},
	{MethodOpsApply, "", "OpsApplyResult"},

	{MethodSessionStart, "SessionStartParams", "SessionStartResult"},
	{MethodSessionGet, "SessionGetParams", ""},
	{MethodSessionList, "SessionListParams", ""},
	{MethodSessionUISync, "", "SessionUISyncResult"},
	{MethodSessionMessage, "", ""},
	{MethodSessionHistory, "SessionHistoryParams", ""},
	{MethodSessionCompact, "SessionCompactParams", "SessionCompactResult"},
	{MethodSessionRewind, "SessionRewindParams", "SessionRewindResult"},
	{MethodSessionFork, "SessionForkParams", "SessionForkResult"},
	{MethodSessionSearch, "SessionSearchParams", ""},
	{MethodSessionTrajectory, "SessionTrajectoryParams", ""},
	{MethodSessionCancel, "SessionCancelParams", ""},
	{MethodSessionApplyPending, "SessionApplyPendingParams", ""},
	{MethodSessionDiscardPending, "SessionDiscardPendingParams", ""},
	{MethodSessionClose, "SessionCloseParams", ""},

	{MethodRuntimeSetModel, "RuntimeSetModelParams", "RuntimeSetModelResult"},
	{MethodRuntimeListModels, "RuntimeListModelsParams", "RuntimeListModelsResult"},
	{MethodRuntimeListProviders, "RuntimeListProvidersParams", "RuntimeListProvidersResult"},
	{MethodRuntimeGetLLM, "RuntimeGetLLMParams", "RuntimeGetLLMResult"},
	{MethodRuntimeCredits, "RuntimeCreditsParams", "RuntimeCreditsResult"},
	{MethodRuntimeConfigureLLM, "RuntimeConfigureLLMParams", "RuntimeConfigureLLMResult"},
	{MethodRuntimeGetOrchestra, "RuntimeGetOrchestraParams", "RuntimeGetOrchestraResult"},
	{MethodRuntimeConfigureOrchestra, "RuntimeConfigureOrchestraParams", "RuntimeConfigureOrchestraResult"},
	{MethodRuntimeGetSystemPrompt, "RuntimeGetSystemPromptParams", "RuntimeGetSystemPromptResult"},
	{MethodRuntimeSetSystemPrompt, "RuntimeSetSystemPromptParams", "RuntimeSetSystemPromptResult"},

	{MethodWorkspaceTrustStatus, "", ""},
	{MethodWorkspaceTrust, "WorkspaceTrustParams", ""},

	{MethodMCPList, "MCPListParams", "MCPListResult"},
	{MethodMCPPrompts, "MCPPromptListParams", "MCPPromptListResult"},
	{MethodMCPPromptGet, "MCPPromptGetParams", "MCPPromptGetResult"},
	{MethodMCPUpsert, "MCPUpsertParams", "MCPUpsertResult"},
	{MethodMCPDelete, "MCPDeleteParams", "MCPDeleteResult"},
	{MethodMCPSetDisabled, "MCPSetDisabledParams", "MCPSetDisabledResult"},
	{MethodMCPTest, "MCPTestParams", "MCPTestResult"},

	{MethodAgentsList, "AgentsListParams", ""},
	{MethodAgentsUpsert, "", ""},
	{MethodAgentsDelete, "AgentsDeleteParams", ""},

	{MethodIndexStatus, "IndexStatusParams", ""},
	{MethodIndexConfigure, "IndexConfigureParams", ""},
	{MethodIndexRebuild, "IndexRebuildParams", "IndexRebuildResult"},
	{MethodIndexEmbed, "IndexEmbedParams", "IndexEmbedResult"},
	{MethodIndexGraph, "IndexGraphParams", ""},
	{MethodIndexOutline, "IndexOutlineParams", ""},

	{MethodAttachmentsStore, "AttachmentsStoreParams", "AttachmentsStoreResult"},

	{MethodWorkflowList, "WorkflowListParams", "WorkflowListResult"},
	{MethodWorkflowRun, "", "WorkflowRunResult"},
	{MethodSkillList, "SkillListParams", "SkillListResult"},
	{MethodSkillInvoke, "", "SkillInvokeResult"},

	{MethodLessonRuleRespond, "RuleSuggestionRespondParams", "RuleSuggestionRespondResult"},
}

// Signatures lists every method with its params and result types, in the
// order of Methods.
func Signatures() []Signature {
	out := append([]Signature(nil), signatures...)
	return out
}
