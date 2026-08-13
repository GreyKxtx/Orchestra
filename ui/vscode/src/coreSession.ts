import * as crypto from "crypto";
import { EventEmitter } from "events";
import * as fs from "fs";
import * as path from "path";
import * as vscode from "vscode";
import {
  coreBinaryCandidates,
  coreExecutableName,
  pickExistingBinary,
} from "./coreBinary";
import type {
  AgentEventParams,
  ConnectionStatus,
  ExecChunkPayload,
  PermissionRequestPayload,
  QuestionItemPayload,
  ToolDiagnosticPayload,
  WorkflowStagePayload,
} from "./protocol/events";
import type { AssistantTurnProjection, RawUIMessage } from "./chat/turnProjection";
import { RpcClient } from "./rpc/client";

/** Must match internal/protocol/version.go */
const PROTOCOL_VERSION = 13;
const OPS_VERSION = 1;
const TOOLS_VERSION = 13;

/** session.message can run a long agent turn (orchestrated multi-department runs). */
const MESSAGE_TIMEOUT_MS = 60 * 60 * 1000;

export interface HealthResult {
  ok: boolean;
  raw: unknown;
}

export interface SessionMeta {
  id: string;
  title: string;
  model?: string;
  msg_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface SessionView {
  sessionId: string;
  title: string;
  model: string;
  uiMessages: RawUIMessage[];
}

export interface ModelInfo {
  id: string;
  owned_by?: string;
  context_tokens?: number;
}

export interface ProviderEntry {
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
  models?: ModelInfo[];
  models_error?: string;
  model_count: number;
}

export interface OrchestraRoleView {
  key: string;
  label: string;
  /** Canonical Orchestra tier (L1–L5) resolved by core via legacy_map. */
  tier?: string;
  provider: string;
  model: string;
  models?: string[];
}

export interface OrchestraNamedProviderView {
  key: string;
  apiBase: string;
  apiKeySet: boolean;
  model: string;
  needsKey: boolean;
  label: string;
  configured: boolean;
}

export interface OrchestraConfigView {
  roles: OrchestraRoleView[];
  defaultTier: string;
  maxWorkerRetries: number;
  workerVerifyEnabled: boolean;
  maxWorkerVerifyRetries: number;
  workerLLMVerifyEnabled: boolean;
  mainProvider: string;
  mainModel: string;
  fastProvider: string;
  named: Record<string, OrchestraNamedProviderView>;
}

export interface CoreSessionEvents {
  status: (status: ConnectionStatus, detail?: string) => void;
  agentEvent: (event: AgentEventParams) => void;
  execChunk: (payload: ExecChunkPayload) => void;
  workflowStage: (phase: "start" | "done", stage: WorkflowStagePayload) => void;
  stderr: (line: string) => void;
  permissionRequest: (request: PermissionRequestPayload) => void;
  questionAsk: (questions: QuestionItemPayload[]) => void;
}

/**
 * Long-lived orchestra core session: ensure → initialize → session.start → session.message.
 */
export class CoreSession extends EventEmitter implements vscode.Disposable {
  private static readonly LAST_SESSION_KEY = "orchestra.lastSessionId";

  private client: RpcClient | undefined;
  private readonly output: vscode.OutputChannel;
  private readonly extensionPath: string;
  private readonly context: vscode.ExtensionContext | undefined;
  private workspaceRoot: string | undefined;
  private sessionId: string | undefined;
  private coreInitialized = false;
  private status: ConnectionStatus = "idle";
  private ensurePromise: Promise<void> | undefined;
  /**
   * Server-request resolvers keyed by JSON-RPC id. A single slot deadlocks the
   * core when two permission/question requests overlap (parallel subagents).
   * FIFO order is preserved by Map insertion order.
   */
  private readonly permissionResolvers = new Map<
    number | string,
    (result: Record<string, unknown>) => void
  >();
  private readonly questionResolvers = new Map<
    number | string,
    (result: Record<string, unknown>) => void
  >();
  /** In-flight session.message JSON-RPC id (for $/cancelRequest). */
  private messageRequestId: number | undefined;

  constructor(
    output: vscode.OutputChannel,
    extensionPath: string,
    context?: vscode.ExtensionContext
  ) {
    super();
    this.output = output;
    this.extensionPath = extensionPath;
    this.context = context;
  }

  dispose(): void {
    this.stop();
  }

  getConnectionStatus(): ConnectionStatus {
    return this.status;
  }

  getSessionId(): string | undefined {
    return this.sessionId;
  }

  private getPersistedSessionId(): string | undefined {
    const id = this.context?.workspaceState.get<string>(CoreSession.LAST_SESSION_KEY);
    const trimmed = typeof id === "string" ? id.trim() : "";
    return trimmed || undefined;
  }

  private async persistSessionId(id: string): Promise<void> {
    const trimmed = id.trim();
    if (!trimmed || !this.context) {
      return;
    }
    await this.context.workspaceState.update(CoreSession.LAST_SESSION_KEY, trimmed);
  }

  /** Pick the most recently updated on-disk session. */
  private async mostRecentSessionId(): Promise<string | undefined> {
    const sessions = await this.listSessions();
    if (sessions.length === 0) {
      return undefined;
    }
    const sorted = [...sessions].sort((a, b) =>
      (b.updated_at || "").localeCompare(a.updated_at || "")
    );
    const id = sorted[0]?.id?.trim();
    return id || undefined;
  }

  stop(): void {
    this.teardownClient();
    this.ensurePromise = undefined;
    this.setStatus("idle");
  }

  private teardownClient(): void {
    this.rejectPendingResolvers("core client torn down");
    if (this.client) {
      this.client.dispose();
      this.client = undefined;
      this.output.appendLine("[orchestra] core stopped");
    }
    this.sessionId = undefined;
    this.workspaceRoot = undefined;
    this.coreInitialized = false;
  }

  /**
   * Ensure core is running and initialized for the current project root.
   * Restarts if project root changed or process died.
   */
  async ensure(): Promise<void> {
    if (this.ensurePromise) {
      await this.ensurePromise;
      return;
    }
    this.ensurePromise = this.ensureInner();
    try {
      await this.ensurePromise;
    } finally {
      this.ensurePromise = undefined;
    }
  }

  /**
   * Attach a session:
   * - no args → reuse current or create new
   * - { forceNew: true } → always create new
   * - { sessionId } → reopen that id
   */
  async startSession(options?: { sessionId?: string; forceNew?: boolean }): Promise<string> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing after ensure");
    }

    const forceNew = options?.forceNew === true;
    let reopenId = options?.sessionId?.trim() || "";

    if (!forceNew && !reopenId) {
      reopenId = this.getPersistedSessionId() || "";
    }
    if (!forceNew && !reopenId) {
      reopenId = (await this.mostRecentSessionId()) || "";
    }

    if (!forceNew && !reopenId && this.sessionId) {
      this.setStatus("ready", this.sessionId);
      return this.sessionId;
    }
    if (!forceNew && reopenId && this.sessionId === reopenId) {
      this.setStatus("ready", this.sessionId);
      return this.sessionId;
    }

    const params: Record<string, unknown> = {};
    if (forceNew) {
      this.sessionId = undefined;
    } else if (reopenId) {
      params.session_id = reopenId;
    }

    this.output.appendLine(
      forceNew
        ? "[orchestra] session.start (new)…"
        : reopenId
          ? `[orchestra] session.start reopen ${reopenId}…`
          : "[orchestra] session.start…"
    );
    const result = (await this.client.request("session.start", params, 30_000)) as {
      session_id?: string;
    };
    const id = typeof result.session_id === "string" ? result.session_id.trim() : "";
    if (!id) {
      throw new Error(`session.start returned no session_id: ${JSON.stringify(result)}`);
    }
    this.sessionId = id;
    this.output.appendLine(`[orchestra] session_id: ${id}`);
    await this.persistSessionId(id);
    this.setStatus("ready", id);
    return id;
  }

  /**
   * Append or update the assistant turn in ui_messages (TUI-compatible projection).
   */
  async syncUIProjection(projection?: AssistantTurnProjection | string): Promise<void> {
    const id = this.sessionId;
    if (!id || !this.client) {
      return;
    }
    const turn: AssistantTurnProjection | undefined =
      typeof projection === "string"
        ? { text: projection.trim() }
        : projection;
    if (!turn || (!turn.text && !turn.reasoning && !turn.tool_blocks?.length)) {
      return;
    }
    // One retry with backoff: a failed ui_sync silently drops the last
    // assistant answer from persisted history.
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const view = await this.getSession(id);
        const health = await this.healthInfo();
        const ui: RawUIMessage[] = [...view.uiMessages];
        const msg: RawUIMessage = {
          role: "assistant",
          text: turn.text,
          reasoning: turn.reasoning,
          tool_blocks: turn.tool_blocks,
          prompt_ctx: turn.prompt_ctx,
          tokens_in: turn.tokens_in,
          tokens_out: turn.tokens_out,
        };
        const last = ui[ui.length - 1];
        const lastRole = String(last?.role || "").toLowerCase();
        const lastText = String(last?.text || last?.content || "").trim();
        if (lastRole === "assistant" && lastText === turn.text) {
          ui[ui.length - 1] = { ...last, ...msg };
        } else {
          ui.push(msg);
        }
        await this.client.request(
          "session.ui_sync",
          {
            session_id: id,
            title: view.title || undefined,
            model: health.model || view.model || undefined,
            ui_messages: ui,
          },
          30_000
        );
        return;
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        this.output.appendLine(
          `[orchestra] ui_sync attempt ${attempt + 1} failed: ${message}`
        );
        if (attempt === 0 && this.client) {
          await new Promise((r) => setTimeout(r, 1_000));
          continue;
        }
        // Surface the problem instead of silently losing the turn from history.
        this.emit("stderr", `ui_sync failed — последний ответ может не сохраниться в истории: ${message}\n`);
      }
    }
  }

  async listSessions(): Promise<SessionMeta[]> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const result = (await this.client.request("session.list", {}, 30_000)) as {
      sessions?: SessionMeta[];
    };
    return Array.isArray(result.sessions) ? result.sessions : [];
  }

  async getSession(sessionId?: string): Promise<SessionView> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const id = (sessionId || this.sessionId || "").trim();
    if (!id) {
      throw new Error("session_id required");
    }
    const result = (await this.client.request(
      "session.get",
      { session_id: id },
      30_000
    )) as {
      session_id?: string;
      title?: string;
      model?: string;
      ui_messages?: RawUIMessage[];
    };
    return {
      sessionId: result.session_id || id,
      title: (result.title || "").trim(),
      model: (result.model || "").trim(),
      uiMessages: Array.isArray(result.ui_messages) ? result.ui_messages : [],
    };
  }

  async closeSession(sessionId?: string): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const id = (sessionId || this.sessionId || "").trim();
    if (!id) {
      throw new Error("session_id required");
    }
    await this.client.request("session.close", { session_id: id }, 30_000);
    if (this.sessionId === id) {
      this.sessionId = "";
    }
  }

  /** Best-effort: set human title when session still untitled (preserves ui_messages). */
  async maybeSetSessionTitle(title: string): Promise<void> {
    const id = this.sessionId;
    if (!id || !this.client) {
      return;
    }
    const clean = title.trim().replace(/\s+/g, " ");
    if (!clean) {
      return;
    }
    try {
      const raw = (await this.client.request(
        "session.get",
        { session_id: id },
        15_000
      )) as {
        title?: string;
        model?: string;
        ui_messages?: unknown;
      };
      if (typeof raw.title === "string" && raw.title.trim() !== "") {
        return;
      }
      const short = clean.length > 48 ? clean.slice(0, 45) + "…" : clean;
      await this.client.request(
        "session.ui_sync",
        {
          session_id: id,
          title: short,
          model: raw.model || undefined,
          ui_messages: Array.isArray(raw.ui_messages) ? raw.ui_messages : [],
        },
        15_000
      );
    } catch (err) {
      this.output.appendLine(
        `[orchestra] set title skipped: ${err instanceof Error ? err.message : String(err)}`
      );
    }
  }

  async healthInfo(): Promise<{ model: string; provider: string; raw: unknown }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const health = (await this.client.request("core.health", {}, 10_000)) as {
      model?: string;
      provider?: string;
    };
    return {
      model: typeof health.model === "string" ? health.model : "",
      provider: typeof health.provider === "string" ? health.provider : "",
      raw: health,
    };
  }

  async listModels(provider?: string): Promise<{ models: ModelInfo[]; current: string; provider: string }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = {};
    if (provider?.trim()) {
      params.provider = provider.trim();
    }
    const result = (await this.client.request("runtime.list_models", params, 60_000)) as {
      models?: ModelInfo[];
      current?: string;
      provider?: string;
    };
    return {
      models: Array.isArray(result.models) ? result.models : [],
      current: typeof result.current === "string" ? result.current : "",
      provider: typeof result.provider === "string" ? result.provider : "",
    };
  }

  async listProviders(options?: {
    probe?: boolean;
    probeKey?: string;
    includeSecrets?: boolean;
  }): Promise<{
    providers: ProviderEntry[];
    activeProvider: string;
    activeModel: string;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = {};
    if (options?.probe) {
      params.probe = true;
    }
    if (options?.probeKey?.trim()) {
      params.probe_key = options.probeKey.trim();
    }
    if (options?.includeSecrets) {
      params.include_secrets = true;
    }
    const result = (await this.client.request("runtime.list_providers", params, 90_000)) as {
      providers?: ProviderEntry[];
      active_provider?: string;
      active_model?: string;
    };
    return {
      providers: Array.isArray(result.providers) ? result.providers : [],
      activeProvider: typeof result.active_provider === "string" ? result.active_provider : "",
      activeModel: typeof result.active_model === "string" ? result.active_model : "",
    };
  }

  async setModel(
    model: string,
    options?: { persist?: boolean; provider?: string }
  ): Promise<{ model: string; persisted: boolean }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const persist = options?.persist ?? true;
    const params: Record<string, unknown> = {
      model: model.trim(),
      persist,
    };
    if (options?.provider) {
      params.provider = options.provider;
    }
    this.output.appendLine(`[orchestra] runtime.set_model ${model}…`);
    const result = (await this.client.request("runtime.set_model", params, 60_000)) as {
      model?: string;
      persisted?: boolean;
    };
    return {
      model: typeof result.model === "string" ? result.model : model,
      persisted: Boolean(result.persisted),
    };
  }

  async getLLM(): Promise<{
    provider: string;
    apiBase: string;
    model: string;
    apiKeySet: boolean;
    apiKeyHint: string;
    temperature: number;
    maxTokens: number;
    timeoutS: number;
    promptFamily: string;
    multimodal: boolean;
    numCtx: number;
    contextTokens: number;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("runtime.get_llm", {}, 15_000)) as {
      provider?: string;
      api_base?: string;
      model?: string;
      api_key_set?: boolean;
      api_key_hint?: string;
      temperature?: number;
      max_tokens?: number;
      timeout_s?: number;
      prompt_family?: string;
      multimodal?: boolean;
      num_ctx?: number;
      context_tokens?: number;
    };
    return {
      provider: r.provider || "",
      apiBase: r.api_base || "",
      model: r.model || "",
      apiKeySet: Boolean(r.api_key_set),
      apiKeyHint: r.api_key_hint || "",
      temperature: typeof r.temperature === "number" ? r.temperature : 0,
      maxTokens: typeof r.max_tokens === "number" ? r.max_tokens : 0,
      timeoutS: typeof r.timeout_s === "number" ? r.timeout_s : 0,
      promptFamily: r.prompt_family || "",
      multimodal: Boolean(r.multimodal),
      numCtx: typeof r.num_ctx === "number" ? r.num_ctx : 0,
      contextTokens: typeof r.context_tokens === "number" ? r.context_tokens : 0,
    };
  }

  async configureLLM(input: {
    provider?: string;
    apiBase?: string;
    apiKey?: string;
    model?: string;
    temperature?: number;
    maxTokens?: number;
    timeoutS?: number;
    promptFamily?: string;
    multimodal?: boolean;
    persist?: boolean;
  }): Promise<{ model: string; apiBase: string; persisted: boolean }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = {
      persist: input.persist ?? true,
    };
    if (input.provider) params.provider = input.provider;
    if (input.apiBase) params.api_base = input.apiBase;
    if (input.apiKey) params.api_key = input.apiKey;
    if (input.model) params.model = input.model;
    if (input.temperature !== undefined) params.temperature = input.temperature;
    if (input.maxTokens !== undefined) params.max_tokens = input.maxTokens;
    if (input.timeoutS !== undefined) params.timeout_s = input.timeoutS;
    if (input.promptFamily !== undefined) params.prompt_family = input.promptFamily;
    if (input.multimodal !== undefined) params.multimodal = input.multimodal;
    this.output.appendLine("[orchestra] runtime.configure_llm…");
    const r = (await this.client.request("runtime.configure_llm", params, 60_000)) as {
      model?: string;
      api_base?: string;
      persisted?: boolean;
    };
    return {
      model: r.model || input.model || "",
      apiBase: r.api_base || input.apiBase || "",
      persisted: Boolean(r.persisted),
    };
  }

  async getOrchestra(): Promise<OrchestraConfigView> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("runtime.get_orchestra", {}, 15_000)) as Record<string, unknown>;
    const rolesRaw = Array.isArray(r.roles) ? r.roles : [];
    const roles: OrchestraRoleView[] = rolesRaw.map((row) => {
      const o = row as Record<string, unknown>;
      const modelsRaw = Array.isArray(o.models) ? o.models : [];
      const models = modelsRaw.filter((m): m is string => typeof m === "string");
      return {
        key: String(o.key || ""),
        label: String(o.label || ""),
        tier: String(o.tier || ""),
        provider: String(o.provider || ""),
        model: String(o.model || ""),
        models,
      };
    });
    const namedRaw = r.named && typeof r.named === "object" ? (r.named as Record<string, unknown>) : {};
    const named: Record<string, OrchestraNamedProviderView> = {};
    for (const [k, v] of Object.entries(namedRaw)) {
      const o = v as Record<string, unknown>;
      named[k] = {
        key: k,
        apiBase: String(o.api_base || ""),
        apiKeySet: Boolean(o.api_key_set),
        model: String(o.model || ""),
        needsKey: Boolean(o.needs_key),
        label: String(o.label || k),
        configured: Boolean(o.configured),
      };
    }
    return {
      roles,
      defaultTier: String(r.default_tier || "focused"),
      maxWorkerRetries: typeof r.max_worker_retries === "number" ? r.max_worker_retries : 3,
      workerVerifyEnabled: r.worker_verify_enabled !== false,
      maxWorkerVerifyRetries:
        typeof r.max_worker_verify_retries === "number" ? r.max_worker_verify_retries : 1,
      workerLLMVerifyEnabled: Boolean(r.worker_llm_verify_enabled),
      mainProvider: String(r.main_provider || ""),
      mainModel: String(r.main_model || ""),
      fastProvider: String(r.fast_provider || ""),
      named,
    };
  }

  async configureOrchestra(input: {
    roles: OrchestraRoleView[];
    defaultTier?: string;
    maxWorkerRetries?: number;
    workerVerifyEnabled?: boolean;
    maxWorkerVerifyRetries?: number;
    workerLLMVerifyEnabled?: boolean;
  }): Promise<{ saved: boolean }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = {
      roles: input.roles.map((r) => ({
        key: r.key,
        label: r.label,
        provider: r.provider,
        model: r.model,
        models: r.models?.length ? r.models : undefined,
      })),
      persist: true,
    };
    if (input.defaultTier) params.default_tier = input.defaultTier;
    if (input.maxWorkerRetries !== undefined) params.max_worker_retries = input.maxWorkerRetries;
    if (input.workerVerifyEnabled !== undefined) params.worker_verify_enabled = input.workerVerifyEnabled;
    if (input.maxWorkerVerifyRetries !== undefined) {
      params.max_worker_verify_retries = input.maxWorkerVerifyRetries;
    }
    if (input.workerLLMVerifyEnabled !== undefined) {
      params.worker_llm_verify_enabled = input.workerLLMVerifyEnabled;
    }
    const r = (await this.client.request("runtime.configure_orchestra", params, 30_000)) as {
      saved?: boolean;
    };
    return { saved: Boolean(r.saved) };
  }

  async getSystemPrompt(): Promise<{
    content: string;
    hasOverride: boolean;
    promptFamily: string;
    path: string;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("runtime.get_system_prompt", {}, 15_000)) as {
      content?: string;
      has_override?: boolean;
      prompt_family?: string;
      path?: string;
    };
    return {
      content: r.content || "",
      hasOverride: Boolean(r.has_override),
      promptFamily: r.prompt_family || "",
      path: r.path || "",
    };
  }

  async setSystemPrompt(input: {
    content?: string;
    clear?: boolean;
    promptFamily?: string;
  }): Promise<{ hasOverride: boolean; promptFamily: string; persisted: boolean }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = { persist: true };
    if (input.clear) {
      params.clear = true;
    } else if (input.content !== undefined) {
      params.content = input.content;
    }
    if (input.promptFamily !== undefined) {
      params.prompt_family = input.promptFamily;
    }
    const r = (await this.client.request("runtime.set_system_prompt", params, 30_000)) as {
      has_override?: boolean;
      prompt_family?: string;
      persisted?: boolean;
    };
    return {
      hasOverride: Boolean(r.has_override),
      promptFamily: r.prompt_family || "",
      persisted: Boolean(r.persisted),
    };
  }

  async listAgents(): Promise<{
    agents: Array<{
      name: string;
      system_prompt?: string;
      tools?: string[];
      model?: string;
      provider?: string;
    }>;
    builtInModes: string[];
    availableTools: string[];
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("agents.list", {}, 15_000)) as {
      agents?: Array<{
        name: string;
        system_prompt?: string;
        tools?: string[];
        model?: string;
        provider?: string;
      }>;
      built_in_modes?: string[];
      available_tools?: string[];
    };
    return {
      agents: Array.isArray(r.agents) ? r.agents : [],
      builtInModes: Array.isArray(r.built_in_modes) ? r.built_in_modes : [],
      availableTools: Array.isArray(r.available_tools) ? r.available_tools : [],
    };
  }

  async upsertAgent(agent: {
    name: string;
    system_prompt?: string;
    tools?: string[];
    model?: string;
    provider?: string;
  }): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    await this.client.request("agents.upsert", { agent, persist: true }, 30_000);
  }

  async deleteAgent(name: string): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    await this.client.request("agents.delete", { name, persist: true }, 30_000);
  }

  async listMCP(): Promise<{
    servers: Array<{
      name: string;
      command: string[];
      env?: Record<string, string>;
      disabled: boolean;
      call_timeout_s?: number;
      allowed_tools?: string[];
      status: string;
      tool_count: number;
      tools?: string[];
      error?: string;
    }>;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("mcp.list", {}, 30_000)) as {
      servers?: Array<{
        name: string;
        command: string[];
        env?: Record<string, string>;
        disabled: boolean;
        call_timeout_s?: number;
        allowed_tools?: string[];
        status: string;
        tool_count: number;
        tools?: string[];
        error?: string;
      }>;
    };
    return { servers: Array.isArray(r.servers) ? r.servers : [] };
  }

  async upsertMCP(server: {
    name: string;
    command: string[];
    env?: Record<string, string>;
    disabled?: boolean;
    call_timeout_s?: number;
    allowed_tools?: string[];
  }): Promise<{ warnings: string[] }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request(
      "mcp.upsert",
      { server, persist: true },
      120_000
    )) as { warnings?: string[] };
    return { warnings: Array.isArray(r.warnings) ? r.warnings : [] };
  }

  async deleteMCP(name: string): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    await this.client.request("mcp.delete", { name, persist: true }, 60_000);
  }

  async setMCPDisabled(name: string, disabled: boolean): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    await this.client.request("mcp.set_disabled", { name, disabled, persist: true }, 60_000);
  }

  async testMCP(input: {
    name?: string;
    server?: {
      name: string;
      command: string[];
      env?: Record<string, string>;
      call_timeout_s?: number;
      allowed_tools?: string[];
    };
  }): Promise<{ ok: boolean; name: string; tools: string[]; error: string; elapsed: string }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("mcp.test", input, 60_000)) as {
      ok?: boolean;
      name?: string;
      tools?: string[];
      error?: string;
      elapsed?: string;
    };
    return {
      ok: Boolean(r.ok),
      name: r.name || "",
      tools: Array.isArray(r.tools) ? r.tools : [],
      error: r.error || "",
      elapsed: r.elapsed || "",
    };
  }

  async getIndexStatus(): Promise<{
    projectRoot: string;
    excludeDirs: string[];
    contextLimitKB: number;
    limits: { context_kb?: number; max_files?: number; max_bytes_per_file?: number };
    embed: {
      provider?: string;
      api_base?: string;
      model?: string;
      batch_size?: number;
      timeout_s?: number;
      semantic_auto_explore?: boolean;
      semantic_auto_explore_top_k?: number;
    };
    graph: {
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
      langs: Record<string, number>;
    };
    graphUIPort: number;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("index.status", {}, 30_000)) as Record<string, unknown>;
    const graph = (r.graph as Record<string, unknown>) || {};
    const embed = (r.embed as Record<string, unknown>) || {};
    const limits = (r.limits as Record<string, unknown>) || {};
    return {
      projectRoot: String(r.project_root || ""),
      excludeDirs: Array.isArray(r.exclude_dirs) ? (r.exclude_dirs as string[]) : [],
      contextLimitKB: Number(r.context_limit_kb) || 0,
      limits: {
        context_kb: Number(limits.context_kb) || undefined,
        max_files: Number(limits.max_files) || undefined,
        max_bytes_per_file: Number(limits.max_bytes_per_file) || undefined,
      },
      embed: {
        provider: String(embed.provider || ""),
        api_base: String(embed.api_base || ""),
        model: String(embed.model || ""),
        batch_size: Number(embed.batch_size) || undefined,
        timeout_s: Number(embed.timeout_s) || undefined,
        semantic_auto_explore:
          embed.semantic_auto_explore === undefined
            ? undefined
            : Boolean(embed.semantic_auto_explore),
        semantic_auto_explore_top_k: Number(embed.semantic_auto_explore_top_k) || undefined,
      },
      graph: {
        available: Boolean(graph.available),
        db_path: String(graph.db_path || ""),
        files: Number(graph.files) || 0,
        nodes: Number(graph.nodes) || 0,
        edges: Number(graph.edges) || 0,
        embeddings: Number(graph.embeddings) || 0,
        missing_embeddings: Number(graph.missing_embeddings) || 0,
        funcs: Number(graph.funcs) || 0,
        types: Number(graph.types) || 0,
        packages: Number(graph.packages) || 0,
        tests: Number(graph.tests) || 0,
        langs:
          graph.langs && typeof graph.langs === "object"
            ? (graph.langs as Record<string, number>)
            : {},
      },
      graphUIPort: Number(r.graph_ui_port) || 6061,
    };
  }

  async configureIndex(input: {
    excludeDirs?: string[];
    contextLimitKB?: number;
    limitsMaxFiles?: number;
    limitsMaxBytesPerFile?: number;
    embedAPIBase?: string;
    embedAPIKey?: string;
    embedModel?: string;
    embedBatchSize?: number;
    embedTimeoutS?: number;
    semanticAutoExplore?: boolean;
    semanticAutoExploreTopK?: number;
  }): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = { persist: true };
    if (input.excludeDirs) {
      params.exclude_dirs = input.excludeDirs;
    }
    if (input.contextLimitKB !== undefined) {
      params.context_limit_kb = input.contextLimitKB;
    }
    if (input.limitsMaxFiles !== undefined) {
      params.limits_max_files = input.limitsMaxFiles;
    }
    if (input.limitsMaxBytesPerFile !== undefined) {
      params.limits_max_bytes_per_file = input.limitsMaxBytesPerFile;
    }
    if (input.embedAPIBase) {
      params.embed_api_base = input.embedAPIBase;
    }
    if (input.embedAPIKey) {
      params.embed_api_key = input.embedAPIKey;
    }
    if (input.embedModel) {
      params.embed_model = input.embedModel;
    }
    if (input.embedBatchSize !== undefined) {
      params.embed_batch_size = input.embedBatchSize;
    }
    if (input.embedTimeoutS !== undefined) {
      params.embed_timeout_s = input.embedTimeoutS;
    }
    if (input.semanticAutoExplore !== undefined) {
      params.semantic_auto_explore = input.semanticAutoExplore;
    }
    if (input.semanticAutoExploreTopK !== undefined) {
      params.semantic_auto_explore_top_k = input.semanticAutoExploreTopK;
    }
    await this.client.request("index.configure", params, 60_000);
  }

  async rebuildIndex(): Promise<{
    files: number;
    nodes: number;
    edges: number;
    embeddings: number;
    missing_embeddings: number;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("index.rebuild", {}, 5 * 60_000)) as {
      graph?: Record<string, unknown>;
    };
    const g = r.graph || {};
    return {
      files: Number(g.files) || 0,
      nodes: Number(g.nodes) || 0,
      edges: Number(g.edges) || 0,
      embeddings: Number(g.embeddings) || 0,
      missing_embeddings: Number(g.missing_embeddings) || 0,
    };
  }

  async embedIndex(options?: { rebuild?: boolean; limit?: number }): Promise<{
    model: string;
    embedded: number;
    total: number;
    remaining: number;
    elapsed: string;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const params: Record<string, unknown> = {};
    if (options?.rebuild) {
      params.rebuild = true;
    }
    if (options?.limit) {
      params.limit = options.limit;
    }
    const r = (await this.client.request("index.embed", params, 10 * 60_000)) as {
      model?: string;
      embedded?: number;
      total?: number;
      remaining?: number;
      elapsed?: string;
    };
    return {
      model: r.model || "",
      embedded: Number(r.embedded) || 0,
      total: Number(r.total) || 0,
      remaining: Number(r.remaining) || 0,
      elapsed: r.elapsed || "",
    };
  }

  async listSkills(): Promise<
    Array<{ name: string; description: string; origin?: string }>
  > {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const r = (await this.client.request("skill.list", {}, 30_000)) as {
      skills?: Array<{ name?: string; description?: string; origin?: string }>;
    };
    const skills = Array.isArray(r.skills) ? r.skills : [];
    return skills.map((s) => ({
      name: s.name || "",
      description: s.description || "",
      origin: s.origin,
    }));
  }

  /**
   * One agent turn. Streams agent/event via "agentEvent" while in flight.
   */
  async sendMessage(
    content: string,
    options?: {
      apply?: boolean;
      mode?: string;
      profile?: string;
      allowExec?: boolean;
      attachments?: Array<{
        name: string;
        path?: string;
        ext?: string;
        kind?: string;
      }>;
    }
  ): Promise<unknown> {
    const text = content.trim();
    const attachments =
      options?.attachments
        ?.filter((f) => typeof f.path === "string" && f.path.trim() !== "")
        .map((f) => ({
          path: f.path!.trim(),
          kind: f.kind || "file",
          name: f.name,
          mime: f.kind === "image" ? mimeForExt(f.ext || f.name) : undefined,
        })) ?? [];
    if (!text && attachments.length === 0) {
      throw new Error("empty message");
    }
    const sessionId = await this.startSession();
    if (!this.client) {
      throw new Error("core client missing");
    }

    this.setStatus("running");
    this.output.appendLine(
      `[orchestra] session.message (${text.length} chars)` +
        (options?.mode ? ` mode=${options.mode}` : "") +
        (options?.profile ? ` profile=${options.profile}` : "") +
        "…"
    );
    this.messageRequestId = undefined;
    try {
      const params: Record<string, unknown> = {
        session_id: sessionId,
        content: text,
        apply: options?.apply ?? false,
        backup: options?.apply ?? false,
        allow_exec: options?.allowExec ?? false,
      };
      if (options?.mode && options.mode.trim() !== "") {
        params.mode = options.mode.trim();
      }
      if (options?.profile && options.profile.trim() !== "") {
        params.profile = options.profile.trim();
      }
      if (attachments.length > 0) {
        params.attachments = attachments;
      }
      // If a previous Stop left the server mid-unwind, wait out "session is busy"
      // passively. Do NOT auto-send session.cancel here: it would silently kill
      // a legitimately running long turn (panel already calls clearServerBusy()
      // once before each send for the stale-flag case).
      let result: unknown;
      const busyDeadline = Date.now() + 15_000;
      for (;;) {
        try {
          result = await this.client.request(
            "session.message",
            params,
            MESSAGE_TIMEOUT_MS,
            (id) => {
              this.messageRequestId = id;
            }
          );
          break;
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          if (/session is busy/i.test(msg) && Date.now() < busyDeadline) {
            await new Promise((r) => setTimeout(r, 300));
            continue;
          }
          if (/session is busy/i.test(msg)) {
            throw new Error(
              "session is busy: предыдущий ход ещё выполняется. Нажмите Stop, чтобы прервать его."
            );
          }
          throw err;
        }
      }
      this.setStatus("ready", sessionId);
      return result;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (/cancel/i.test(msg)) {
        this.setStatus("ready", sessionId);
      } else {
        this.setStatus("error", msg);
      }
      throw err;
    } finally {
      this.messageRequestId = undefined;
    }
  }

  /** Abort the in-flight session turn (session.cancel first, then $/cancelRequest). */
  async cancelTurn(): Promise<void> {
    // Unblock the core if it is parked on a permission/question dialog.
    this.rejectPendingResolvers("turn cancelled");
    const sessionId = this.sessionId;
    const client = this.client;
    const reqId = this.messageRequestId;
    // Cancel the agent turn on the server BEFORE rejecting the local RPC
    // promise — otherwise UI thinks the turn is idle while runMu is still held
    // and the next session.message hangs forever on "Working…".
    if (sessionId && client && !client.isClosed) {
      try {
        await client.request("session.cancel", { session_id: sessionId }, 5_000);
      } catch {
        // idle session or already finished
      }
    }
    if (reqId !== undefined && client && !client.isClosed) {
      client.cancelRequest(reqId);
    }
    if (this.getConnectionStatus() === "running") {
      this.setStatus("ready", sessionId);
    }
    this.output.appendLine("[orchestra] turn cancelled");
  }

  /** Best-effort: clear a stuck busy flag without aborting a local promise. */
  async clearServerBusy(): Promise<void> {
    const sessionId = this.sessionId;
    const client = this.client;
    if (!sessionId || !client || client.isClosed) {
      return;
    }
    try {
      await client.request("session.cancel", { session_id: sessionId }, 3_000);
    } catch {
      /* idle */
    }
  }

  async applyPending(
    backup = true,
    paths?: string[]
  ): Promise<{ applied: boolean; remainingOps?: unknown[] }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const sessionId = (this.sessionId || "").trim();
    if (!sessionId) {
      throw new Error("session_id required");
    }
    const params: { session_id: string; backup: boolean; paths?: string[] } = {
      session_id: sessionId,
      backup,
    };
    if (paths && paths.length > 0) {
      params.paths = paths;
    }
    const r = (await this.client.request(
      "session.apply_pending",
      params,
      5 * 60_000
    )) as { applied?: boolean; remaining_ops?: unknown[] };
    return {
      applied: Boolean(r.applied),
      remainingOps: Array.isArray(r.remaining_ops) ? r.remaining_ops : undefined,
    };
  }

  async discardPending(): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const sessionId = (this.sessionId || "").trim();
    if (!sessionId) {
      throw new Error("session_id required");
    }
    await this.client.request(
      "session.discard_pending",
      { session_id: sessionId },
      30_000
    );
  }

  async applyOps(ops: unknown[], backup = true): Promise<void> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    if (!Array.isArray(ops) || ops.length === 0) {
      throw new Error("ops required");
    }
    await this.client.request(
      "ops.apply",
      { ops, backup },
      5 * 60_000
    );
  }

  async rewindSession(uiMessageIndex: number): Promise<{
    uiMessages: number;
    historyMessages: number;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const sessionId = (this.sessionId || "").trim();
    if (!sessionId) {
      throw new Error("session_id required");
    }
    const r = (await this.client.request(
      "session.rewind",
      { session_id: sessionId, ui_message_index: uiMessageIndex },
      60_000
    )) as { ui_messages?: number; history_messages?: number };
    return {
      uiMessages: typeof r.ui_messages === "number" ? r.ui_messages : 0,
      historyMessages: typeof r.history_messages === "number" ? r.history_messages : 0,
    };
  }

  async compactSession(query?: string): Promise<{
    beforeMsgs: number;
    afterMsgs: number;
  }> {
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    const sessionId = (this.sessionId || "").trim();
    if (!sessionId) {
      throw new Error("session_id required");
    }
    const params: Record<string, unknown> = { session_id: sessionId };
    if (query?.trim()) {
      params.query = query.trim();
    }
    const r = (await this.client.request("session.compact", params, 5 * 60_000)) as {
      before_msgs?: number;
      after_msgs?: number;
    };
    return {
      beforeMsgs: typeof r.before_msgs === "number" ? r.before_msgs : 0,
      afterMsgs: typeof r.after_msgs === "number" ? r.after_msgs : 0,
    };
  }

  /** Whether any permission/question dialog is still waiting for an answer. */
  hasPendingServerRequests(): boolean {
    return this.permissionResolvers.size > 0 || this.questionResolvers.size > 0;
  }

  resolvePermission(decision: { approved: boolean; always?: boolean; reason?: string }): void {
    // FIFO: answer the oldest outstanding request (UI shows them in order).
    const first = this.permissionResolvers.entries().next();
    if (first.done) {
      return;
    }
    const [id, resolve] = first.value;
    this.permissionResolvers.delete(id);
    resolve({
      approved: decision.approved,
      always: Boolean(decision.always),
      reason: decision.reason || (decision.approved ? "ok" : "denied"),
    });
  }

  resolveQuestion(answers: string[]): void {
    const first = this.questionResolvers.entries().next();
    if (first.done) {
      return;
    }
    const [id, resolve] = first.value;
    this.questionResolvers.delete(id);
    resolve({ answers });
  }

  /** Deny/blank out all outstanding server requests (cancel, teardown, core exit). */
  private rejectPendingResolvers(reason: string): void {
    for (const [, resolve] of this.permissionResolvers) {
      resolve({ approved: false, always: false, reason });
    }
    this.permissionResolvers.clear();
    for (const [, resolve] of this.questionResolvers) {
      resolve({ answers: [] });
    }
    this.questionResolvers.clear();
  }

  /**
   * Health smoke: ensure core, call core.health (does not tear down).
   */
  async ping(): Promise<HealthResult> {
    this.output.appendLine("— Orchestra: Ping Core —");
    await this.ensure();
    if (!this.client) {
      throw new Error("core client missing");
    }
    this.output.appendLine("[orchestra] core.health…");
    const health = await this.client.request("core.health", {}, 10_000);
    this.output.appendLine(`[orchestra] health: ${JSON.stringify(health, null, 2)}`);
    return { ok: true, raw: health };
  }

  private async ensureInner(): Promise<void> {
    const workspaceRoot = await resolveProjectRoot();
    if (
      this.client &&
      !this.client.isClosed &&
      this.workspaceRoot === workspaceRoot &&
      this.coreInitialized
    ) {
      return;
    }

    this.teardownClient();
    this.setStatus("connecting");

    const binary = resolveBinaryPath(workspaceRoot, this.extensionPath);
    this.output.appendLine(`[orchestra] binary: ${binary}`);
    this.output.appendLine(`[orchestra] workspace: ${workspaceRoot}`);

    const client = new RpcClient(binary, ["core", "--workspace-root", workspaceRoot], {
      cwd: workspaceRoot,
    });
    this.client = client;
    this.workspaceRoot = workspaceRoot;
    this.coreInitialized = false;

    // Every handler checks identity: after a restart, late events from the
    // torn-down client must not clobber state that now belongs to the new one
    // (otherwise the fresh core process leaks as an orphan with no reference).
    client.on("stderr", (line: string) => {
      if (this.client !== client) {
        return;
      }
      this.output.append(`[core stderr] ${line}`);
      this.emit("stderr", line);
    });
    client.on("error", (err: Error) => {
      if (this.client !== client) {
        return;
      }
      this.output.appendLine(`[orchestra] rpc error: ${err.message}`);
      this.setStatus("error", err.message);
    });
    client.on("exit", () => {
      if (this.client !== client) {
        return;
      }
      this.output.appendLine("[orchestra] core process exited");
      this.client = undefined;
      this.sessionId = undefined;
      this.coreInitialized = false;
      this.rejectPendingResolvers("core exited");
      this.setStatus("idle", "core exited");
    });
    client.on("notification", (method: string, params: unknown) => {
      if (this.client !== client) {
        return;
      }
      this.handleNotification(method, params);
    });
    client.setServerRequestHandler(async (method, params, id) => {
      if (this.client !== client) {
        throw new Error("stale core client");
      }
      return this.handleServerRequest(method, params, id);
    });

    try {
      this.output.appendLine("[orchestra] core.health (pre-init)…");
      const preHealth = (await client.request("core.health", {}, 10_000)) as {
        project_id?: string;
        protocol_version?: number;
      };
      if (
        typeof preHealth.protocol_version === "number" &&
        preHealth.protocol_version !== PROTOCOL_VERSION
      ) {
        throw new Error(
          `protocol_version mismatch: extension=${PROTOCOL_VERSION}, core=${preHealth.protocol_version}. ` +
            `Rebuild orchestra.exe (go build -o orchestra.exe ./cmd/orchestra) and reload the window.`
        );
      }
      const projectId =
        typeof preHealth.project_id === "string" && preHealth.project_id.trim() !== ""
          ? preHealth.project_id.trim()
          : computeProjectID(workspaceRoot);
      this.output.appendLine(`[orchestra] project_id: ${projectId}`);

      this.output.appendLine("[orchestra] initialize…");
      const initResult = await client.request(
        "initialize",
        {
          protocol_version: PROTOCOL_VERSION,
          ops_version: OPS_VERSION,
          tools_version: TOOLS_VERSION,
          project_root: workspaceRoot,
          project_id: projectId,
        },
        20_000
      );
      this.output.appendLine(`[orchestra] initialize ok: ${JSON.stringify(initResult)}`);
      this.coreInitialized = true;
      this.setStatus("ready");
    } catch (err) {
      this.teardownClient();
      throw err;
    }
  }

  private handleNotification(method: string, params: unknown): void {
    if (method === "agent/event") {
      const event = normalizeAgentEvent(params);
      if (event) {
        this.output.appendLine(
          `[agent/event] ${event.type}` +
            (event.tool_call_name ? ` ${event.tool_call_name}` : "") +
            (event.content ? ` ${truncate(event.content, 80)}` : "")
        );
        this.emit("agentEvent", event);
      }
      return;
    }
    if (method === "exec/output_chunk") {
      const chunk = parseExecChunk(params);
      if (chunk) {
        this.emit("execChunk", chunk);
      }
      return;
    }
    if (method === "workflow/stage_start") {
      const stage = parseWorkflowStage(params);
      if (stage) {
        this.emit("workflowStage", "start", stage);
      }
      return;
    }
    if (method === "workflow/stage_done") {
      const stage = parseWorkflowStage(params);
      if (stage) {
        this.emit("workflowStage", "done", stage);
      }
      return;
    }
    this.output.appendLine(`[notify] ${method} ${JSON.stringify(params)}`);
  }

  private handleServerRequest(method: string, params: unknown, id: number | string): Promise<unknown> {
    this.output.appendLine(
      `[server-req] ${method} id=${String(id)} ${JSON.stringify(params)}`
    );

    if (method === "permission/request") {
      const req = parsePermissionRequest(params);
      return new Promise((resolve) => {
        this.permissionResolvers.set(id, resolve);
        this.emit("permissionRequest", req);
      });
    }

    if (method === "question/ask") {
      const questions = parseQuestionRequest(params);
      return new Promise((resolve) => {
        this.questionResolvers.set(id, resolve);
        this.emit("questionAsk", questions);
      });
    }

    return Promise.reject(new Error(`unsupported server request: ${method}`));
  }

  private setStatus(status: ConnectionStatus, detail?: string): void {
    this.status = status;
    this.emit("status", status, detail);
  }
}

function normalizeAgentEvent(params: unknown): AgentEventParams | undefined {
  if (!params || typeof params !== "object") {
    return undefined;
  }
  const p = params as Record<string, unknown>;
  const type = typeof p.type === "string" ? p.type : undefined;
  if (!type) {
    return undefined;
  }
  let diagnostics: ToolDiagnosticPayload[] | undefined;
  if (p.diagnostics !== undefined && p.diagnostics !== null) {
    const raw = p.diagnostics;
    if (Array.isArray(raw)) {
      diagnostics = raw
        .map(parseDiagnostic)
        .filter((x): x is ToolDiagnosticPayload => x !== undefined);
    } else if (typeof raw === "string" && raw.trim() !== "" && raw.trim() !== "null") {
      try {
        const parsed = JSON.parse(raw) as unknown;
        if (Array.isArray(parsed)) {
          diagnostics = parsed
            .map(parseDiagnostic)
            .filter((x): x is ToolDiagnosticPayload => x !== undefined);
        }
      } catch {
        /* ignore */
      }
    }
  }
  return {
    type,
    step: typeof p.step === "number" ? p.step : undefined,
    content: typeof p.content === "string" ? p.content : undefined,
    data: p.data,
    session_id: typeof p.session_id === "string" ? p.session_id : undefined,
    turn_id: typeof p.turn_id === "string" ? p.turn_id : undefined,
    tool_call_id: typeof p.tool_call_id === "string" ? p.tool_call_id : undefined,
    tool_call_name: typeof p.tool_call_name === "string" ? p.tool_call_name : undefined,
    tool_call_index: typeof p.tool_call_index === "number" ? p.tool_call_index : undefined,
    args_delta: typeof p.args_delta === "string" ? p.args_delta : undefined,
    diagnostics,
    scope: typeof p.scope === "string" ? p.scope : undefined,
    task_id: typeof p.task_id === "string" ? p.task_id : undefined,
    parent_tool_call_id:
      typeof p.parent_tool_call_id === "string" ? p.parent_tool_call_id : undefined,
    subagent_type: typeof p.subagent_type === "string" ? p.subagent_type : undefined,
    tier: typeof p.tier === "string" ? p.tier : undefined,
    model: typeof p.model === "string" ? p.model : undefined,
    status: typeof p.status === "string" ? p.status : undefined,
    error: typeof p.error === "string" ? p.error : undefined,
  };
}

function parseDiagnostic(item: unknown): ToolDiagnosticPayload | undefined {
  if (!item || typeof item !== "object") {
    return undefined;
  }
  const d = item as Record<string, unknown>;
  const message = typeof d.message === "string" ? d.message : "";
  const severity = typeof d.severity === "string" ? d.severity : "warning";
  const startLine = typeof d.start_line === "number" ? d.start_line : 0;
  const startCol = typeof d.start_col === "number" ? d.start_col : 0;
  if (!message) {
    return undefined;
  }
  return {
    start_line: startLine,
    start_col: startCol,
    end_line: typeof d.end_line === "number" ? d.end_line : undefined,
    end_col: typeof d.end_col === "number" ? d.end_col : undefined,
    severity,
    source: typeof d.source === "string" ? d.source : undefined,
    message,
  };
}

function parseExecChunk(params: unknown): ExecChunkPayload | undefined {
  if (!params || typeof params !== "object") {
    return undefined;
  }
  const p = params as Record<string, unknown>;
  const step = typeof p.step === "number" ? p.step : undefined;
  const chunk = typeof p.chunk === "string" ? p.chunk : "";
  if (step === undefined) {
    return undefined;
  }
  return {
    step,
    chunk,
    session_id: typeof p.session_id === "string" ? p.session_id : undefined,
    turn_id: typeof p.turn_id === "string" ? p.turn_id : undefined,
  };
}

function parseWorkflowStage(params: unknown): WorkflowStagePayload | undefined {
  if (!params || typeof params !== "object") {
    return undefined;
  }
  const p = params as Record<string, unknown>;
  const name = typeof p.name === "string" ? p.name : "";
  const stageId = typeof p.stage_id === "string" ? p.stage_id : "";
  if (!name || !stageId) {
    return undefined;
  }
  return {
    name,
    stage_id: stageId,
    attempt: typeof p.attempt === "number" ? p.attempt : 0,
    marker: typeof p.marker === "string" ? p.marker : undefined,
    action: typeof p.action === "string" ? p.action : undefined,
    output_kb: typeof p.output_kb === "number" ? p.output_kb : undefined,
  };
}

function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n) + "…";
}

function parsePermissionRequest(params: unknown): PermissionRequestPayload {
  if (!params || typeof params !== "object") {
    return { tool: "unknown" };
  }
  const p = params as Record<string, unknown>;
  return {
    tool: typeof p.tool === "string" ? p.tool : "unknown",
    description: typeof p.description === "string" ? p.description : undefined,
    kind: typeof p.kind === "string" ? p.kind : undefined,
    reason: typeof p.reason === "string" ? p.reason : undefined,
  };
}

function parseQuestionRequest(params: unknown): QuestionItemPayload[] {
  if (!params || typeof params !== "object") {
    return [];
  }
  const raw = (params as { questions?: unknown }).questions;
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: QuestionItemPayload[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const q = item as Record<string, unknown>;
    const question = typeof q.question === "string" ? q.question : "";
    if (!question) {
      continue;
    }
    const options = Array.isArray(q.options)
      ? q.options.filter((x): x is string => typeof x === "string")
      : undefined;
    out.push({
      question,
      options,
      allow_multiple: q.allow_multiple === true,
    });
  }
  return out;
}

function computeProjectID(projectRoot: string): string {
  let abs = path.resolve(projectRoot);
  if (process.platform === "win32") {
    abs = abs.toLowerCase();
  }
  const sum = crypto.createHash("sha256").update(abs, "utf8").digest("hex");
  return `sha256:${sum}`;
}

export async function resolveProjectRoot(): Promise<string> {
  const configured = vscode.workspace
    .getConfiguration("orchestra")
    .get<string>("projectRoot")
    ?.trim();
  if (configured) {
    if (!fs.existsSync(configured)) {
      throw new Error(`orchestra.projectRoot not found: ${configured}`);
    }
    return path.resolve(configured);
  }

  const folder = vscode.workspace.workspaceFolders?.[0];
  if (folder) {
    return folder.uri.fsPath;
  }

  const picked = await vscode.window.showOpenDialog({
    canSelectFiles: false,
    canSelectFolders: true,
    canSelectMany: false,
    openLabel: "Use as Orchestra project root",
    title: "No folder open — pick a project for orchestra core",
  });
  const uri = picked?.[0];
  if (!uri) {
    throw new Error(
      "No workspace folder open. Open a folder (File → Open Folder) or set orchestra.projectRoot."
    );
  }
  return uri.fsPath;
}

export function resolveBinaryPath(workspaceRoot: string, extensionPath: string): string {
  const exeName = coreExecutableName();

  const configured = vscode.workspace
    .getConfiguration("orchestra")
    .get<string>("binaryPath")
    ?.trim();
  if (configured) {
    if (!fs.existsSync(configured)) {
      throw new Error(
        `orchestra.binaryPath not found: ${configured}\n` +
          `Set Settings → Orchestra: Binary Path to your built orchestra.exe`
      );
    }
    return configured;
  }

  const best = pickExistingBinary(coreBinaryCandidates(workspaceRoot, extensionPath));
  if (best) {
    return best;
  }

  throw new Error(
    `orchestra binary not found (looked for ${exeName} including bundled bin/${process.platform}-${process.arch}/).\n` +
      `Build: go build -o ${exeName} ./cmd/orchestra\n` +
      `Or run: npm run bundle:core (from ui/vscode)\n` +
      `Or set Settings → Orchestra: Binary Path.`
  );
}

function mimeForExt(extOrName: string): string | undefined {
  const ext = extOrName.includes(".")
    ? extOrName.replace(/^.*\./, "").toLowerCase()
    : extOrName.toLowerCase();
  switch (ext) {
    case "png":
      return "image/png";
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "gif":
      return "image/gif";
    case "webp":
      return "image/webp";
    case "bmp":
      return "image/bmp";
    case "avif":
      return "image/avif";
    default:
      return undefined;
  }
}
