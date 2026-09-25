// AUTO-GENERATED from protocol/wire — do not edit.
// Regenerate: go generate ./protocol/wire/...  (a Go test fails while this file is stale)
//
// The client↔core contract: versions, the names on the wire, and the shape of
// every params, result and event. Field meanings: protocol/wire/*.go.

/** The newest protocol version this contract describes. */
export const PROTOCOL_VERSION = 24;
/** The oldest protocol version a core of this version still speaks. */
export const MIN_PROTOCOL_VERSION = 23;
/** Internal ops; must match the core's. */
export const OPS_VERSION = 1;
/** The tools this contract was written against; informational to the core. */
export const TOOLS_VERSION = 18;

/** Every method a connection answers. */
export const METHODS = [
  "$/cancelRequest",
  "agent.run",
  "agents.delete",
  "agents.list",
  "agents.upsert",
  "attachments.store",
  "core.health",
  "index.configure",
  "index.embed",
  "index.graph",
  "index.outline",
  "index.rebuild",
  "index.status",
  "initialize",
  "lesson.rule_respond",
  "mcp.delete",
  "mcp.list",
  "mcp.prompt.get",
  "mcp.prompts",
  "mcp.set_disabled",
  "mcp.test",
  "mcp.upsert",
  "ops.apply",
  "runtime.configure_llm",
  "runtime.configure_orchestra",
  "runtime.credits",
  "runtime.get_llm",
  "runtime.get_orchestra",
  "runtime.get_system_prompt",
  "runtime.list_models",
  "runtime.list_providers",
  "runtime.set_model",
  "runtime.set_system_prompt",
  "session.apply_pending",
  "session.cancel",
  "session.close",
  "session.compact",
  "session.discard_pending",
  "session.fork",
  "session.get",
  "session.history",
  "session.list",
  "session.message",
  "session.rewind",
  "session.search",
  "session.start",
  "session.trajectory",
  "session.ui_sync",
  "skill.invoke",
  "skill.list",
  "tool.call",
  "workflow.list",
  "workflow.run",
  "workspace.trust",
  "workspace.trust_status",
] as const;
export type Method = (typeof METHODS)[number];

/** Every notification the core sends. */
export const NOTIFICATIONS = [
  "agent/event",
  "exec/output_chunk",
  "workflow/stage_done",
  "workflow/stage_start",
] as const;
export type Notification = (typeof NOTIFICATIONS)[number];

/** Every request the core makes of the client. */
export const REQUESTS = [
  "browser/call",
  "permission/request",
  "question/ask",
] as const;
export type Request = (typeof REQUESTS)[number];

/** Every AgentEvent.type the core sends. */
export const EVENT_TYPES = [
  "message_delta",
  "reasoning_delta",
  "tool_call_start",
  "tool_call_delta",
  "tool_call_completed",
  "step_done",
  "pending_ops",
  "recoverable_error",
  "done",
  "error",
  "todos_updated",
  "step_usage",
  "context_estimate",
  "mode_route",
  "child_started",
  "child_queued",
  "child_done",
  "agent_message",
  "workorders_relayed",
  "integration_verify",
] as const;
export type AgentEventType = (typeof EVENT_TYPES)[number];

// ── protocol/wire/agents.go ──

/** AgentsListParams is reserved. */
export type AgentsListParams = Record<string, never>;

/** AgentsDeleteParams removes a custom agent by name. */
export interface AgentsDeleteParams {
  name: string;
  persist?: boolean;
}

// ── protocol/wire/attachments.go ──

/**
 * AttachmentsStoreParams carries a file's bytes from a client that has no
 * filesystem of its own — a browser, or the desktop shell's web view — to be
 * kept under the workspace, where a turn can refer to it. The editor's host
 * does this itself (ui/vscode/src/chat/panel.ts, "attachBytes"): this is the
 * same thing for hosts whose only reach into the workspace is the core.
 */
export interface AttachmentsStoreParams {
  name: string;
  mime?: string;
  data_base64: string;
}

/**
 * AttachmentsStoreResult is the stored file as a message attachment: the
 * same fields the renderer's file chips and session.message carry.
 */
export interface AttachmentsStoreResult {
  name: string;
  /** Path is absolute; Rel is workspace-relative with forward slashes. */
  path: string;
  rel: string;
  ext?: string;
  /** image | file */
  kind: string;
  size: number;
}

// ── protocol/wire/events.go ──

/**
 * AgentEvent is the params of an agent/event notification: one thing that
 * happened in a turn, as the client draws it. Type says which fields mean
 * something; the rest are absent.
 */
export interface AgentEvent {
  step: number;
  type: string;
  /**
   * Content is the text of a delta, a tool's result, a child's goal
   * (child_started) or summary (child_done), an agent message; the JSON
   * checklist for todos_updated.
   */
  content: string;
  /**
   * Data is the payload of pending_ops (PendingOps), step_usage and
   * context_estimate (Usage) and mode_route (ModeRoute). DecodeData reads
   * it on the client side.
   */
  data?: unknown;
  error?: string;
  /** SessionID is set for session.message turns; TurnID for every turn. */
  session_id?: string;
  turn_id?: string;
  /**
   * Tool calls: tool_call_start carries the name and id; tool_call_delta
   * the next piece of the arguments; tool_call_completed the result in
   * Content and the diagnostics an edit produced. ToolCallIndex tells
   * parallel calls of one step apart when a model sends no ids.
   */
  tool_call_id?: string;
  tool_call_name?: string;
  tool_call_index: number;
  args_delta?: string;
  diagnostics?: ToolDiagnostic[];
  /**
   * Child scope: Scope is "child" on every event a subagent emits and on
   * the child_* events, with the task and its parent.
   */
  scope?: string;
  task_id?: string;
  parent_tool_call_id?: string;
  parent_task_id?: string;
  subagent_type?: string;
  /** Tier and Model are the child's, on child_started. */
  tier?: string;
  model?: string;
  /**
   * Status is the child's result status on child_done, the verdict on
   * integration_verify.
   */
  status?: string;
  /** Depth, Agent and ParentAgent place the child in the delegation tree. */
  depth?: number;
  agent?: string;
  parent_agent?: string;
  /** WaitingFor and Reason say why a child_queued task waits. */
  waiting_for?: string[];
  reason?: string;
  /** Promote suggestions a child's task_result carried (child_done). */
  lesson_promote_suggestion?: string;
  playbook_promote_suggestion?: string;
  /** agent_message: Channel is send | reply | post. */
  channel?: string;
  from?: string;
  to?: string;
  kind?: string;
  /** workorders_relayed: the tasks spawned and how many orders were refused. */
  task_ids?: string[];
  rejected?: number;
  /** integration_verify: what was checked and the one-line summary. */
  workers?: number;
  files?: number;
  summary?: string;
}

/** ToolDiagnostic is one LSP diagnostic an edit produced, 1-based. */
export interface ToolDiagnostic {
  start_line: number;
  start_col: number;
  end_line?: number;
  end_col?: number;
  severity: string;
  source?: string;
  message: string;
}

/**
 * PendingOps is the Data of a pending_ops event: the internal ops a turn
 * staged (or applied, when Applied), with a before/after diff per file.
 * Ops are patch/ops objects (type, path, …), which the client sends back
 * as they are to session.apply_pending; the protocol module does not
 * decode them.
 */
export interface PendingOps {
  ops: (Record<string, unknown>)[];
  diff: FileDiff[];
  applied: boolean;
}

/** FileDiff is a file's content before and after a turn's edits. */
export interface FileDiff {
  path: string;
  before: string;
  after: string;
}

/**
 * Usage is token spend: the Data of step_usage (one LLM call, measured by
 * the provider) and context_estimate (a byte-derived estimate of the next
 * prompt, Source "estimate", with its Breakdown), and the turn's total in
 * agent.run and session.message results. The estimate and the measurement
 * must never be summed or substituted for one another.
 */
export interface Usage {
  calls?: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd?: number;
  source?: string;
  /**
   * CachedPromptTokens is the part of PromptTokens the provider served
   * from its prompt cache; CacheWriteTokens is what it charged to fill it.
   */
  cached_prompt_tokens?: number;
  cache_write_tokens?: number;
  /** Entries is the per-(provider, model) split of a turn. */
  entries?: UsageEntry[];
  /** Breakdown is the per-category split of an estimate. */
  breakdown?: ContextBreakdown[];
}

/** UsageEntry is one (provider, model) row of a turn's spend. */
export interface UsageEntry {
  provider: string;
  model: string;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd?: number;
  cached_prompt_tokens?: number;
  cache_write_tokens?: number;
}

/** ContextBreakdown is one category of a prompt-context estimate. */
export interface ContextBreakdown {
  key: string;
  label: string;
  tokens: number;
}

/** ModeRoute is the Data of a mode_route event. */
export interface ModeRoute {
  from: string;
  to: string;
  reason?: string;
  confidence?: number;
}

/**
 * MemoryNote says what the end-of-turn memory writer did. Outcome is
 * written | skipped | failed; Source is model | digest; Detail is the note
 * when written, the reason otherwise.
 */
export interface MemoryNote {
  outcome: string;
  source?: string;
  detail?: string;
}

/**
 * RuleSuggestion offers the person a project rule for a repeated
 * anti-pattern. Text is the chat-facing prompt; RuleLine is the exact line
 * lesson.rule_respond appends to ORCHESTRA.md on accept.
 */
export interface RuleSuggestion {
  dept: string;
  file: string;
  count: number;
  verify?: string;
  rule_line: string;
  text: string;
}

/** TodoItem is one row of the model's checklist. */
export interface TodoItem {
  id: string;
  content: string;
  status: string;
}

/** WorkflowStage is the params of workflow/stage_start and workflow/stage_done. */
export interface WorkflowStage {
  name: string;
  stage_id: string;
  attempt: number;
  marker?: string;
  action?: string;
  output_kb?: number;
}

/**
 * ExecOutputChunk is the params of exec/output_chunk: a piece of a running
 * command's output.
 */
export interface ExecOutputChunk {
  step: number;
  chunk: string;
  session_id?: string;
  turn_id?: string;
  /** Child scope, as on AgentEvent, when a subagent's command is running. */
  scope?: string;
  task_id?: string;
  parent_tool_call_id?: string;
  subagent_type?: string;
}

/**
 * PermissionRequest is the params of permission/request. Kind is "" or
 * "exec" for a shell command, "lsp.install" for a language server.
 */
export interface PermissionRequest {
  tool: string;
  description: string;
  reason?: string;
  kind?: string;
}

/**
 * PermissionDecision answers permission/request. Always asks the core to
 * remember the decision for the session.
 */
export interface PermissionDecision {
  approved: boolean;
  reason?: string;
  always?: boolean;
}

/** QuestionAsk is the params of question/ask. */
export interface QuestionAsk {
  questions: QuestionItem[];
}

/** QuestionItem is one question for the person. */
export interface QuestionItem {
  question: string;
  options?: string[];
}

/** QuestionAnswers answers question/ask, one answer per question. */
export interface QuestionAnswers {
  answers: string[];
}

/**
 * BrowserCall is the params of browser/call: a browser.* op for the
 * client's own browser view. The client answers with the op's result, or
 * {"error": reason} when it refuses.
 */
export interface BrowserCall {
  op: string;
  params: Record<string, unknown>;
}

// ── protocol/wire/handshake.go ──

/**
 * InitializeParams is the first request of a connection.
 *
 * Since ProtocolVersion 24 the client names the range of protocol versions
 * it speaks and the core answers with the newest both sides have. Before
 * that the number had to match exactly, so a client and a core from
 * different releases could not connect at all — and tools_version, which
 * moves with tools the client never calls, was checked the same way.
 */
export interface InitializeParams {
  project_root: string;
  project_id: string;
  /** ProtocolVersion is the newest protocol version the client speaks. */
  protocol_version: number;
  /**
   * MinProtocolVersion is the oldest it still speaks. Zero means
   * ProtocolVersion alone, which is what a client before v24 sends.
   */
  min_protocol_version?: number;
  /**
   * OpsVersion must match the core's when given: internal ops are what
   * reaches the disk.
   */
  ops_version?: number;
  /**
   * ToolsVersion is informational: the core records it and answers with
   * its own. A client that needs a particular tool checks the answer.
   */
  tools_version?: number;
}

/** InitializeResult answers initialize. */
export interface InitializeResult {
  status: string;
  /**
   * ProtocolVersion is the version both sides speak from here on: the
   * newest in both ranges (ProtocolVersion 24).
   */
  protocol_version: number;
  /** ToolsVersion is the core's. */
  tools_version: number;
  /**
   * Capabilities is what this core serves, by name, so a client asks for
   * the feature it needs instead of comparing version numbers.
   */
  capabilities: Capabilities;
  health: Health;
}

/** Capabilities names what a core serves. */
export interface Capabilities {
  /** Methods the core answers. */
  methods: string[];
  /** Notifications the core sends. */
  notifications: string[];
  /** Requests the core makes of the client, which the client must answer. */
  requests: string[];
}

/** VersionRange is the protocol versions one side speaks, inclusive. */
export interface VersionRange {
  Min: number;
  Max: number;
}

// ── protocol/wire/index.go ──

/** IndexStatusParams is empty (reserved). */
export type IndexStatusParams = Record<string, never>;

export interface CKGView {
  available: boolean;
  db_path?: string;
  files: number;
  nodes: number;
  edges: number;
  embeddings: number;
  missing_embeddings: number;
  funcs: number;
  types: number;
  packages: number;
  tests: number;
  langs?: Record<string, number>;
}

/** IndexConfigureParams updates scope + embed settings in .orchestra.yml. */
export interface IndexConfigureParams {
  exclude_dirs?: string[];
  context_limit_kb?: number;
  limits_context_kb?: number;
  limits_max_files?: number;
  limits_max_bytes_per_file?: number;
  embed_api_base?: string;
  embed_api_key?: string;
  embed_model?: string;
  embed_batch_size?: number;
  embed_timeout_s?: number;
  semantic_auto_explore?: boolean;
  semantic_auto_explore_top_k?: number;
  persist?: boolean;
}

/** IndexRebuildParams triggers a synchronous CKG rescan. */
export type IndexRebuildParams = Record<string, never>;

/** IndexRebuildResult reports post-rebuild stats. */
export interface IndexRebuildResult {
  graph: CKGView;
}

/** IndexEmbedParams runs vector indexing for CKG nodes. */
export interface IndexEmbedParams {
  rebuild?: boolean;
  limit?: number;
}

/** IndexEmbedResult summarizes the embed pass. */
export interface IndexEmbedResult {
  model: string;
  embedded: number;
  total: number;
  remaining: number;
  elapsed: string;
}

/**
 * IndexGraphParams selects the granularity of index.graph: "file" (default)
 * — folders, files and weighted file-to-file relations, what the Graph view
 * draws — or "symbol", every indexed symbol with its relations.
 */
export interface IndexGraphParams {
  level?: string;
}

/**
 * IndexOutlineParams asks for one file's symbols, by workspace-relative path
 * with forward slashes — the id of a file node in index.graph.
 */
export interface IndexOutlineParams {
  path: string;
  /** Preview off returns the symbol list without reading the file. */
  preview?: boolean;
}

// ── protocol/wire/lesson.go ──

export interface RuleSuggestionRespondParams {
  accept: boolean;
  dept: string;
  file: string;
  verify: string;
  rule_line: string;
}

export interface RuleSuggestionRespondResult {
  applied: boolean;
}

// ── protocol/wire/mcp.go ──

/** MCPServerParams is the JSON shape for one MCP server (upsert / test). */
export interface MCPServerParams {
  name: string;
  command: string[];
  env?: Record<string, string>;
  disabled?: boolean;
  call_timeout_s?: number;
  allowed_tools?: string[];
}

/** MCPListParams is reserved. */
export type MCPListParams = Record<string, never>;

/** MCPServerView is one row in mcp.list. */
export interface MCPServerView {
  name: string;
  command: string[];
  env?: Record<string, string>;
  disabled: boolean;
  call_timeout_s?: number;
  allowed_tools?: string[];
  /** running | disabled | error | stopped */
  status: string;
  tool_count: number;
  /** discovered tool names (for settings toggles) */
  tools?: string[];
  error?: string;
}

/** MCPListResult is returned by mcp.list. */
export interface MCPListResult {
  servers: MCPServerView[];
}

/** MCPUpsertParams adds or replaces a server by name. */
export interface MCPUpsertParams {
  server: MCPServerParams;
  /** default true */
  persist?: boolean;
}

/** MCPUpsertResult is returned after upsert + hot reload. */
export interface MCPUpsertResult {
  servers: MCPServerView[];
  persisted: boolean;
  warnings?: string[];
}

/** MCPDeleteParams removes a server by name. */
export interface MCPDeleteParams {
  name: string;
  persist?: boolean;
}

/** MCPDeleteResult mirrors list after delete. */
export interface MCPDeleteResult {
  servers: MCPServerView[];
  persisted: boolean;
  warnings?: string[];
}

/** MCPSetDisabledParams toggles disabled. */
export interface MCPSetDisabledParams {
  name: string;
  disabled: boolean;
  persist?: boolean;
}

/** MCPSetDisabledResult mirrors list after toggle. */
export interface MCPSetDisabledResult {
  servers: MCPServerView[];
  persisted: boolean;
  warnings?: string[];
}

/** MCPTestParams probes a server config (or named cfg entry) without persisting. */
export interface MCPTestParams {
  /** use existing cfg entry */
  name?: string;
  /** or ad-hoc config */
  server?: MCPServerParams;
}

/** MCPTestResult lists tools from a temporary connection. */
export interface MCPTestResult {
  ok: boolean;
  name: string;
  tools?: string[];
  error?: string;
  elapsed?: string;
}

/** MCPPromptArgView is one argument an MCP prompt accepts. */
export interface MCPPromptArgView {
  name: string;
  description?: string;
  required?: boolean;
}

/**
 * MCPPromptCommand is one MCP prompt as a slash command a person can run.
 *
 * An MCP prompt is the server's own recipe, meant for a human to pick — not
 * for the model to call. So it belongs in the command palette next to /model
 * and /skill, not in the tool list.
 */
export interface MCPPromptCommand {
  server: string;
  name: string;
  description?: string;
  arguments?: MCPPromptArgView[];
  /**
   * Slash and Hint are the rendered palette row. They travel over the wire
   * so the TUI and the VS Code panel do not each re-derive the formatting
   * and drift apart.
   */
  slash?: string;
  hint?: string;
}

/** MCPPromptListParams is reserved. */
export type MCPPromptListParams = Record<string, never>;

/** MCPPromptListResult is returned by mcp.prompts. */
export interface MCPPromptListResult {
  prompts: MCPPromptCommand[];
}

/**
 * MCPPromptGetParams names the prompt to render. Args is the raw text typed
 * after the command; the core maps it onto the prompt's declared arguments.
 */
export interface MCPPromptGetParams {
  server: string;
  name: string;
  args?: string;
}

/** MCPPromptGetResult carries the text to send as the user's turn. */
export interface MCPPromptGetResult {
  text: string;
}

// ── protocol/wire/ops.go ──

/** OpsApplyResult reports the result of applying pending ops. */
export interface OpsApplyResult {
  applied: boolean;
  changed_files: string[];
}

// ── protocol/wire/runtime_llm.go ──

/**
 * RuntimeSetModelParams switches the active LLM model for this core process.
 * Optionally persists to .orchestra.yml (default persist=true).
 */
export interface RuntimeSetModelParams {
  model: string;
  /** named providers: key; empty keeps current */
  provider?: string;
  /** Persist writes llm.model (and provider mirror) to disk. nil → true. */
  persist?: boolean;
}

/** RuntimeSetModelResult is returned by runtime.set_model. */
export interface RuntimeSetModelResult {
  model: string;
  provider: string;
  api_base: string;
  persisted: boolean;
  context_tokens?: number;
}

/** RuntimeListModelsParams selects which credential set to use for /models. */
export interface RuntimeListModelsParams {
  /** empty → current llm config */
  provider?: string;
}

/** RuntimeModelEntry is one remote model id. */
export interface RuntimeModelEntry {
  id: string;
  owned_by?: string;
  context_tokens?: number;
}

/** RuntimeListModelsResult is returned by runtime.list_models. */
export interface RuntimeListModelsResult {
  models: RuntimeModelEntry[];
  provider: string;
  api_base: string;
  current: string;
}

/**
 * RuntimeCreditsParams selects which provider's balance to query.
 * Empty Provider uses the primary llm config.
 */
export interface RuntimeCreditsParams {
  provider?: string;
}

/**
 * RuntimeCreditsResult is returned by runtime.credits. Supported=false means
 * the provider has no balance API we know (local servers, plain OpenAI base).
 */
export interface RuntimeCreditsResult {
  provider: string;
  supported: boolean;
  total_credits?: number;
  total_usage?: number;
  balance?: number;
}

/** RuntimeGetLLMParams is empty for now (reserved). */
export type RuntimeGetLLMParams = Record<string, never>;

/** RuntimeGetLLMResult exposes current LLM connection settings (key masked). */
export interface RuntimeGetLLMResult {
  provider: string;
  api_base: string;
  model: string;
  api_key_set: boolean;
  api_key_hint?: string;
  temperature: number;
  max_tokens: number;
  timeout_s: number;
  prompt_family?: string;
  multimodal: boolean;
  num_ctx?: number;
  context_tokens?: number;
}

/** RuntimeConfigureLLMParams updates connection fields. Empty api_key leaves the existing key. */
export interface RuntimeConfigureLLMParams {
  provider?: string;
  api_base?: string;
  api_key?: string;
  model?: string;
  temperature?: number;
  max_tokens?: number;
  timeout_s?: number;
  prompt_family?: string;
  multimodal?: boolean;
  /** default true */
  persist?: boolean;
}

/** RuntimeConfigureLLMResult mirrors set_model-ish outcome after configure. */
export interface RuntimeConfigureLLMResult {
  provider: string;
  api_base: string;
  model: string;
  persisted: boolean;
  api_key_set: boolean;
}

// ── protocol/wire/runtime_orchestra.go ──

/** RuntimeOrchestraRole is one editable orchestra role row. */
export interface RuntimeOrchestraRole {
  key: string;
  label: string;
  /** canonical L1–L5 tier (spec §1.4 / legacy_map) */
  tier?: string;
  provider?: string;
  model?: string;
  models?: string[];
}

/** RuntimeOrchestraNamedProvider is a named providers: entry snapshot for UI. */
export interface RuntimeOrchestraNamedProvider {
  key: string;
  api_base?: string;
  api_key_set: boolean;
  model?: string;
  needs_key: boolean;
  label?: string;
  configured: boolean;
}

/** RuntimeGetOrchestraParams is empty — reads current .orchestra.yml orchestra block. */
export type RuntimeGetOrchestraParams = Record<string, never>;

/** RuntimeGetOrchestraResult exposes orchestra planner/tiers for settings UI. */
export interface RuntimeGetOrchestraResult {
  roles: RuntimeOrchestraRole[];
  default_tier: string;
  max_worker_retries: number;
  worker_verify_enabled: boolean;
  max_worker_verify_retries: number;
  worker_llm_verify_enabled: boolean;
  main_provider: string;
  main_model: string;
  fast_provider?: string;
  named?: Record<string, RuntimeOrchestraNamedProvider>;
}

/** RuntimeConfigureOrchestraProviderPatch updates one named provider snapshot. */
export interface RuntimeConfigureOrchestraProviderPatch {
  key: string;
  api_base?: string;
  api_key?: string;
  model?: string;
}

/** RuntimeConfigureOrchestraParams writes orchestra planner/tiers to .orchestra.yml. */
export interface RuntimeConfigureOrchestraParams {
  roles: RuntimeOrchestraRole[];
  default_tier?: string;
  max_worker_retries?: number;
  worker_verify_enabled?: boolean;
  max_worker_verify_retries?: number;
  worker_llm_verify_enabled?: boolean;
  provider_patches?: RuntimeConfigureOrchestraProviderPatch[];
  persist?: boolean;
}

/** RuntimeConfigureOrchestraResult confirms save. */
export interface RuntimeConfigureOrchestraResult {
  saved: boolean;
}

// ── protocol/wire/runtime_prompt.go ──

/** RuntimeGetSystemPromptParams is reserved. */
export type RuntimeGetSystemPromptParams = Record<string, never>;

/** RuntimeGetSystemPromptResult exposes .orchestra/system.txt + prompt_family. */
export interface RuntimeGetSystemPromptResult {
  content: string;
  has_override: boolean;
  prompt_family: string;
  path: string;
}

/** RuntimeSetSystemPromptParams writes or clears the system override. */
export interface RuntimeSetSystemPromptParams {
  /** nil = leave file; "" = clear */
  content?: string;
  /** force delete override */
  clear?: boolean;
  /** set llm.prompt_family when non-nil */
  prompt_family?: string;
  /** persist prompt_family to yaml; default true */
  persist?: boolean;
}

/** RuntimeSetSystemPromptResult confirms write. */
export interface RuntimeSetSystemPromptResult {
  has_override: boolean;
  prompt_family: string;
  persisted: boolean;
  path: string;
}

// ── protocol/wire/runtime_providers.go ──

/** RuntimeListProvidersParams lists catalog + named providers. */
export interface RuntimeListProvidersParams {
  /** Probe fetches /models for each configured (ready) provider. Default false. */
  probe?: boolean;
  /** ProbeKey limits probe to one provider key (catalog or named). */
  probe_key?: string;
  /** IncludeSecrets returns api_key in entries (settings UI only — local trusted client). */
  include_secrets?: boolean;
}

/** RuntimeProviderEntry is one selectable provider in settings UI. */
export interface RuntimeProviderEntry {
  key: string;
  name: string;
  category: string;
  api_base: string;
  active: boolean;
  ready: boolean;
  configured: boolean;
  api_key_set: boolean;
  api_key?: string;
  needs_key: boolean;
  named: boolean;
  custom: boolean;
  current_model?: string;
  models?: RuntimeModelEntry[];
  models_error?: string;
  model_count: number;
}

/** RuntimeListProvidersResult is returned by runtime.list_providers. */
export interface RuntimeListProvidersResult {
  providers: RuntimeProviderEntry[];
  active_provider: string;
  active_model: string;
}

// ── protocol/wire/session.go ──

export interface SessionForkParams {
  session_id: string;
  /** exclusive; must point at role=user */
  ui_message_index: number;
}

export interface SessionForkResult {
  /** the new branch */
  session_id: string;
  parent_id: string;
  ui_messages: number;
  history_messages: number;
}

export interface SessionRewindParams {
  session_id: string;
  /** inclusive; must point at role=user */
  ui_message_index: number;
}

export interface SessionRewindResult {
  session_id: string;
  ui_messages: number;
  history_messages: number;
}

export interface SessionStartParams {
  /**
   * SessionID optionally reopens an existing on-disk session (v2 snapshot).
   * When empty, core allocates a new sortable id.
   */
  session_id?: string;
}

export interface SessionStartResult {
  session_id: string;
  restored?: boolean;
}

export interface SessionGetParams {
  session_id: string;
}

export type SessionListParams = Record<string, never>;

export interface SessionUISyncResult {
  session_id: string;
  saved: boolean;
}

export interface SessionApplyPendingParams {
  session_id: string;
  backup?: boolean;
  /** optional: apply only ops whose path matches one of these (workspace-relative) */
  paths?: string[];
}

export interface SessionDiscardPendingParams {
  session_id: string;
  /** optional: discard only ops whose path matches one of these (workspace-relative) */
  paths?: string[];
}

export interface SessionHistoryParams {
  session_id: string;
}

export interface SessionCompactParams {
  session_id: string;
  /** optional goal hint for the summary */
  query?: string;
}

export interface SessionCompactResult {
  session_id: string;
  before_msgs: number;
  after_msgs: number;
  before_bytes?: number;
  after_bytes?: number;
}

export interface SessionCancelParams {
  session_id: string;
}

export interface SessionCloseParams {
  session_id: string;
}

export interface SessionSearchParams {
  query: string;
  insensitive?: boolean;
  include_all?: boolean;
  limit?: number;
}

/** SessionTrajectoryParams selects the session whose log to read. */
export interface SessionTrajectoryParams {
  session_id: string;
}

// ── protocol/wire/signatures.go ──

/**
 * Signature names a method's params and result types on the wire. An empty
 * name means the method has none (core.health takes no params) or that the
 * type stays in internal/core because it carries a type of the patch module
 * (ops.AnyOp, patches.Patch), of the config, of the session snapshot or of
 * the code graph; docs/PROTOCOL.md describes those.
 */
export interface Signature {
  Method: string;
  Params: string;
  Result: string;
}

// ── protocol/wire/skill.go ──

export type SkillListParams = Record<string, never>;

export interface SkillListResult {
  skills: SkillSummary[];
}

export interface SkillSummary {
  name: string;
  description: string;
  tools?: string[];
  provider?: string;
  model?: string;
  completion_markers?: string[];
  origin?: string;
}

export interface SkillInvokeResult {
  skill: string;
  output: string;
  marker?: string;
  steps: number;
}

// ── protocol/wire/tool_call.go ──

export interface ToolCallParams {
  name: string;
  input: unknown;
}

// ── protocol/wire/workflow.go ──

export type WorkflowListParams = Record<string, never>;

export interface WorkflowListResult {
  workflows: WorkflowSummary[];
}

export interface WorkflowSummary {
  name: string;
  description: string;
  stages: string[];
  source?: string;
}

export interface WorkflowRunResult {
  name: string;
  outputs: Record<string, string>;
  final_stage?: string;
  failure_reason?: string;
  stages: StageRecord[];
  duration_ms: number;
}

export interface StageRecord {
  stage_id: string;
  attempt: number;
  marker?: string;
  action: string;
  output_kb: number;
}

// ── protocol/wire/workspace.go ──

/**
 * WorkspaceTrustParams is the workspace.trust request. Revoke forgets the
 * workspace instead of trusting it.
 */
export interface WorkspaceTrustParams {
  revoke?: boolean;
}

// ── protocol/version.go ──

/** Health is returned by core.health (and /health in HTTP mode). */
export interface Health {
  status: string;
  core_version: string;
  protocol_version: number;
  /**
   * MinProtocolVersion is the oldest protocol version the core speaks
   * (ProtocolVersion 24); absent from a core before that.
   */
  min_protocol_version?: number;
  ops_version: number;
  tools_version: number;
  workspace_root?: string;
  project_id?: string;
  model?: string;
  provider?: string;
  /** off | idle | installing | active */
  lsp_status?: string;
  /** LSPInstallProgress is set while a language server is being provisioned. */
  lsp_install_progress?: LSPInstallProgress;
}

/** LSPInstallProgress is download/install status for the TUI status bar. */
export interface LSPInstallProgress {
  id: string;
  phase?: string;
  percent: number;
  message?: string;
}

/** The params and result of each method, where they are on the wire. `unknown`
 * marks a type that stays in internal/core because it carries a patch or
 * config type; see docs/PROTOCOL.md for its shape. */
export interface MethodSignatures {
  "core.health": { params: unknown; result: Health };
  "initialize": { params: InitializeParams; result: InitializeResult };
  "agent.run": { params: unknown; result: unknown };
  "tool.call": { params: ToolCallParams; result: unknown };
  "ops.apply": { params: unknown; result: OpsApplyResult };
  "session.start": { params: SessionStartParams; result: SessionStartResult };
  "session.get": { params: SessionGetParams; result: unknown };
  "session.list": { params: SessionListParams; result: unknown };
  "session.ui_sync": { params: unknown; result: SessionUISyncResult };
  "session.message": { params: unknown; result: unknown };
  "session.history": { params: SessionHistoryParams; result: unknown };
  "session.compact": { params: SessionCompactParams; result: SessionCompactResult };
  "session.rewind": { params: SessionRewindParams; result: SessionRewindResult };
  "session.fork": { params: SessionForkParams; result: SessionForkResult };
  "session.search": { params: SessionSearchParams; result: unknown };
  "session.trajectory": { params: SessionTrajectoryParams; result: unknown };
  "session.cancel": { params: SessionCancelParams; result: unknown };
  "session.apply_pending": { params: SessionApplyPendingParams; result: unknown };
  "session.discard_pending": { params: SessionDiscardPendingParams; result: unknown };
  "session.close": { params: SessionCloseParams; result: unknown };
  "runtime.set_model": { params: RuntimeSetModelParams; result: RuntimeSetModelResult };
  "runtime.list_models": { params: RuntimeListModelsParams; result: RuntimeListModelsResult };
  "runtime.list_providers": { params: RuntimeListProvidersParams; result: RuntimeListProvidersResult };
  "runtime.get_llm": { params: RuntimeGetLLMParams; result: RuntimeGetLLMResult };
  "runtime.credits": { params: RuntimeCreditsParams; result: RuntimeCreditsResult };
  "runtime.configure_llm": { params: RuntimeConfigureLLMParams; result: RuntimeConfigureLLMResult };
  "runtime.get_orchestra": { params: RuntimeGetOrchestraParams; result: RuntimeGetOrchestraResult };
  "runtime.configure_orchestra": { params: RuntimeConfigureOrchestraParams; result: RuntimeConfigureOrchestraResult };
  "runtime.get_system_prompt": { params: RuntimeGetSystemPromptParams; result: RuntimeGetSystemPromptResult };
  "runtime.set_system_prompt": { params: RuntimeSetSystemPromptParams; result: RuntimeSetSystemPromptResult };
  "workspace.trust_status": { params: unknown; result: unknown };
  "workspace.trust": { params: WorkspaceTrustParams; result: unknown };
  "mcp.list": { params: MCPListParams; result: MCPListResult };
  "mcp.prompts": { params: MCPPromptListParams; result: MCPPromptListResult };
  "mcp.prompt.get": { params: MCPPromptGetParams; result: MCPPromptGetResult };
  "mcp.upsert": { params: MCPUpsertParams; result: MCPUpsertResult };
  "mcp.delete": { params: MCPDeleteParams; result: MCPDeleteResult };
  "mcp.set_disabled": { params: MCPSetDisabledParams; result: MCPSetDisabledResult };
  "mcp.test": { params: MCPTestParams; result: MCPTestResult };
  "agents.list": { params: AgentsListParams; result: unknown };
  "agents.upsert": { params: unknown; result: unknown };
  "agents.delete": { params: AgentsDeleteParams; result: unknown };
  "index.status": { params: IndexStatusParams; result: unknown };
  "index.configure": { params: IndexConfigureParams; result: unknown };
  "index.rebuild": { params: IndexRebuildParams; result: IndexRebuildResult };
  "index.embed": { params: IndexEmbedParams; result: IndexEmbedResult };
  "index.graph": { params: IndexGraphParams; result: unknown };
  "index.outline": { params: IndexOutlineParams; result: unknown };
  "attachments.store": { params: AttachmentsStoreParams; result: AttachmentsStoreResult };
  "workflow.list": { params: WorkflowListParams; result: WorkflowListResult };
  "workflow.run": { params: unknown; result: WorkflowRunResult };
  "skill.list": { params: SkillListParams; result: SkillListResult };
  "skill.invoke": { params: unknown; result: SkillInvokeResult };
  "lesson.rule_respond": { params: RuleSuggestionRespondParams; result: RuleSuggestionRespondResult };
}
