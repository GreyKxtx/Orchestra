/**
 * The wire contract as the extension consumes it. The shapes come from
 * ./wire.generated — generated from protocol/wire, the one definition the
 * core, the TUI, this extension and the web share (docs/PROTOCOL.md). This
 * file names the few the extension normalises before use, and the
 * extension's own webview messages.
 */
import type {
  AgentEvent,
  AgentEventType as WireAgentEventType,
  ExecOutputChunk,
  MemoryNote,
  TodoItem,
  ToolDiagnostic,
  WorkflowStage,
} from "./wire.generated";

/** A wire event type, or one this build does not know yet. */
export type AgentEventType = WireAgentEventType | (string & {});

/**
 * An agent/event as normalizeAgentEvent hands it on: the wire's AgentEvent
 * with every field optional but `type`, because the parse tolerates a core
 * that omits any of them.
 */
export type AgentEventParams = Partial<Omit<AgentEvent, "type">> & {
  type: AgentEventType;
};

export type ToolDiagnosticPayload = ToolDiagnostic;

export type TodoItemPayload = TodoItem;

export interface StepUsagePayload {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
  source?: string;
  /** Per-category context split (system / tools / rules / skills / conversation). */
  breakdown?: ContextBreakdownItem[];
}

/** One recorded (or live) notification, as session.trajectory returns it.
 * `type` is the JSON-RPC method ("agent/event", "exec/output_chunk", …) and
 * `data` its params. seq and time_ms are absent on a live event forwarded
 * mid-turn; the view renders blank times for those rather than a client clock. */
export interface TrajectoryEvent {
  seq?: number;
  time_ms?: number;
  type: string;
  source?: string;
  data?: unknown;
}

/** Per-(provider, model) usage row — orchestra tiers each get their own row. */
export interface UsageModelEntry {
  provider?: string;
  model?: string;
  calls?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
}

/** Whole-turn usage summary from session.message. */
export interface TurnUsagePayload {
  calls?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
  // Prompt-cache split the provider reported (Anthropic native or via a
  // gateway); 0/absent for providers without a cache — every local model.
  cached_prompt_tokens?: number;
  cache_write_tokens?: number;
  entries?: UsageModelEntry[];
}

/** What the end-of-turn memory writer did, from session.message. */
export type MemoryNotePayload = MemoryNote;

export interface ContextInfoPayload {
  contextLimit?: number;
  maxResponseTokens?: number;
  model?: string;
}

export interface ContextBreakdownItem {
  key: string;
  label: string;
  tokens: number;
  /** Optional — the webview maps key → color when omitted. */
  color?: string;
}

export type WorkflowStagePayload = WorkflowStage;

export type ExecChunkPayload = ExecOutputChunk;

export type ConnectionStatus =
  | "idle"
  | "connecting"
  | "ready"
  | "running"
  | "error";

export type ChatFileKind = "image" | "text" | "code" | "binary";

export interface ChatFileRef {
  name: string;
  path?: string;
  ext?: string;
  kind?: ChatFileKind;
  /** Webview-safe URI for image thumbnails. */
  previewUri?: string;
}

export interface SessionListItem {
  id: string;
  title: string;
  model?: string;
  msg_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface ChatHistoryToolBlock {
  id?: string;
  name: string;
  argsRaw?: string;
  status?: string;
  result?: string;
  diagnostics?: ToolDiagnosticPayload[];
  /** Reconstructed from tool args so diffs survive session reopen. */
  diffBefore?: string;
  diffAfter?: string;
}

export interface ChatHistoryMessage {
  role: string;
  text: string;
  /** Index in session ui_messages (user messages only — for rewind). */
  uiIndex?: number;
  files?: ChatFileRef[];
  reasoning?: string;
  toolBlocks?: ChatHistoryToolBlock[];
  promptCtx?: number;
  tokensIn?: number;
  tokensOut?: number;
}

export interface PendingFileDiff {
  path: string;
  before?: string;
  after?: string;
}

export interface PendingOpsPayload {
  ops?: unknown[];
  diff?: PendingFileDiff[];
  applied?: boolean;
}

export interface PermissionRequestPayload {
  tool: string;
  description?: string;
  kind?: string;
  reason?: string;
}

export interface QuestionItemPayload {
  question: string;
  options?: string[];
  allow_multiple?: boolean;
}

export interface ProviderModelEntry {
  id: string;
  owned_by?: string;
}

export interface ProviderPickerEntry {
  key: string;
  name: string;
  active: boolean;
  ready: boolean;
  models?: ProviderModelEntry[];
  models_error?: string;
  model_count?: number;
}

export interface QueuedSendPreview {
  id: string;
  preview: string;
  fileCount?: number;
}

/** Extension → Webview */
export type HostToWebview =
  | { type: "status"; status: ConnectionStatus; detail?: string }
  | { type: "ready"; sessionId: string }
  | {
      type: "header";
      sessionId: string;
      title: string;
      model: string;
      provider?: string;
      /** Accumulated session spend in USD; resets the webview cost chip on session switch. */
      sessionCost?: number;
    }
  | { type: "sessionList"; sessions: SessionListItem[] }
  | { type: "sessionTabs"; activeId: string; tabs: SessionListItem[] }
  | { type: "history"; messages: ChatHistoryMessage[] }
  /** Replace the Trajectory view from the session's log. recorded:false means the
   * session predates the log — a different answer from an empty log. error set
   * means the fetch failed; the view says so instead of claiming either. */
  | { type: "trajectory"; recorded: boolean; events: TrajectoryEvent[]; error?: string }
  /** Append one live notification to the Trajectory view. Reconciled by the
   * next "trajectory" message, which the host sends when the turn ends. */
  | { type: "trajectoryEvent"; event: { type: string; data: unknown } }
  | { type: "clearMessages" }
  | { type: "models"; models: Array<{ id: string }>; current: string }
  | {
      type: "providerModels";
      activeProvider: string;
      activeModel: string;
      providers: ProviderPickerEntry[];
    }
  | {
      type: "orchestraRoles";
      roles: Array<{
        key: string;
        label: string;
        tier?: string;
        provider: string;
        model: string;
        models?: string[];
      }>;
      defaultTier: string;
      error?: string;
    }
  | { type: "userEcho"; text: string; uiIndex?: number; files?: ChatFileRef[] }
  | { type: "delta"; content: string }
  | { type: "deltaSync"; content: string }
  | { type: "discardAssistantBubble" }
  | { type: "reasoningDelta"; content: string }
  | { type: "tool"; toolName: string; detail?: string; done?: boolean }
  | {
      type: "toolBlock";
      phase: "start" | "update" | "complete";
      toolCallId?: string;
      toolName: string;
      content?: string;
      argsDelta?: string;
      step?: number;
      diagnostics?: ToolDiagnosticPayload[];
      scope?: string;
      taskId?: string;
      parentToolCallId?: string;
      subagentType?: string;
    }
  | {
      type: "childLifecycle";
      phase: "started" | "done";
      taskId: string;
      parentToolCallId?: string;
      subagentType?: string;
      tier?: string;
      model?: string;
      status?: string;
      content?: string;
      error?: string;
      lessonPromoteSuggestion?: string;
      playbookPromoteSuggestion?: string;
    }
  | {
      type: "diffViewer";
      path: string;
      before: string;
      after: string;
      language: string;
    }
  | {
      type: "highlightResult";
      requestId: string;
      lines: string[];
    }
  | { type: "execChunk"; step: number; chunk: string }
  | { type: "todosUpdate"; todos: TodoItemPayload[] }
  | { type: "stepUsage"; usage: StepUsagePayload; scope?: string }
  /** A turn is still running — re-arm the busy UI after a webview reload. */
  | { type: "turnInFlight" }
  | { type: "turnUsage"; usage: TurnUsagePayload; sessionCost?: number }
  | { type: "credits"; supported: boolean; provider?: string; balance?: number }
  | {
      type: "workflowStage";
      phase: "start" | "done";
      stage: WorkflowStagePayload;
    }
  | { type: "healthStatus"; lspStatus?: string; model?: string; provider?: string }
  | { type: "contextInfo"; info: ContextInfoPayload }
  | { type: "systemNote"; text: string }
  | { type: "skillsList"; skills: Array<{ name: string; description: string }> }
  | { type: "mentionResults"; query: string; files: ChatFileRef[] }
  | { type: "pendingOps"; payload: PendingOpsPayload }
  | { type: "pendingCleared" }
  // The language the chat speaks. Sent once the webview says it is ready,
  // and again whenever the setting changes.
  | { type: "uiLang"; lang: string }
  // Diffs for changes the turn already wrote. No decision to make, so this
  // never raises the apply bar — it only lets the tool blocks draw the real
  // change instead of one rebuilt from the call's arguments.
  | { type: "appliedOps"; diff: PendingFileDiff[] }
  | { type: "permissionRequest"; request: PermissionRequestPayload }
  | { type: "questionAsk"; questions: QuestionItemPayload[] }
  | { type: "error"; message: string }
  | { type: "turnStart" }
  | { type: "turnComplete"; ok: boolean; queuedNext?: boolean }
  | { type: "queueUpdate"; items: QueuedSendPreview[] }
  | {
      type: "attachmentPreview";
      path: string;
      name: string;
      kind?: string;
      previewUri?: string;
    }
  | { type: "filesPicked"; files: ChatFileRef[] };
export type WebviewToHost =
  | { type: "ready" }
  | {
      type: "send";
      text: string;
      mode?: string;
      profile?: string;
      allowExec?: boolean;
      allowBrowser?: boolean;
      files?: ChatFileRef[];
    }
  | { type: "attach" }
  | { type: "listSessions" }
  | { type: "newSession" }
  | { type: "openSession"; sessionId: string }
  | { type: "closeSession"; sessionId: string }
  | { type: "deleteSession"; sessionId: string }
  | { type: "listModels" }
  | { type: "listProviderModels" }
  | { type: "listOrchestraRoles" }
  | { type: "setModel"; model: string; provider?: string }
  | { type: "applyPending"; paths?: string[]; ops?: unknown[] }
  | { type: "discardPending"; paths?: string[] }
  | { type: "togglePendingDiff" }
  | { type: "openSettings"; section?: string }
  | { type: "openOrchestraSettings" }
  | {
      type: "permissionReply";
      approved: boolean;
      always?: boolean;
    }
  | { type: "questionReply"; answers: string[] }
  | { type: "mentionSearch"; query: string }
  | { type: "rewindToMessage"; uiIndex: number }
  // Branch a new chat from that checkpoint, leaving this one intact.
  | { type: "forkFromMessage"; uiIndex: number }
  | { type: "compactSession"; query?: string }
  | { type: "slashCommand"; cmd: string; arg?: string }
  | { type: "cancelQueuedSend"; id: string }
  | { type: "cancelTurn" }
  | { type: "openFile"; path: string; focus?: boolean }
  | { type: "previewAttachment"; path: string; name?: string; kind?: string; focus?: boolean }
  | { type: "attachBytes"; name: string; mime?: string; dataBase64: string }
  | { type: "openDiff"; path: string; before?: string; after?: string; focus?: boolean; sideBySide?: boolean }
  | {
      type: "highlightCode";
      requestId: string;
      language: string;
      lines: string[];
    };
