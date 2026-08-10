/**
 * Mirrors ui/tui/rpcclient EventKind / agent/event envelope (docs/PROTOCOL.md).
 */

export type AgentEventType =
  | "message_delta"
  | "reasoning_delta"
  | "tool_call_start"
  | "tool_call_delta"
  | "tool_call_completed"
  | "step_done"
  | "pending_ops"
  | "recoverable_error"
  | "done"
  | "error"
  | string;

export interface AgentEventParams {
  step?: number;
  type: AgentEventType;
  content?: string;
  data?: unknown;
  session_id?: string;
  turn_id?: string;
  tool_call_id?: string;
  tool_call_name?: string;
  tool_call_index?: number;
  args_delta?: string;
  diagnostics?: ToolDiagnosticPayload[];
  /** "child" when emitted by a subagent; omitted for parent agent. */
  scope?: string;
  task_id?: string;
  parent_tool_call_id?: string;
  subagent_type?: string;
  status?: string;
  error?: string;
}

export interface ToolDiagnosticPayload {
  start_line: number;
  start_col: number;
  end_line?: number;
  end_col?: number;
  severity: string;
  source?: string;
  message: string;
}

export interface TodoItemPayload {
  id: string;
  content: string;
  status: string;
}

export interface StepUsagePayload {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
  source?: string;
}

export interface ContextInfoPayload {
  contextLimit?: number;
  maxResponseTokens?: number;
  model?: string;
}

export interface ContextBreakdownItem {
  key: string;
  label: string;
  tokens: number;
  color: string;
}

export interface WorkflowStagePayload {
  name: string;
  stage_id: string;
  attempt: number;
  marker?: string;
  action?: string;
  output_kb?: number;
}

export interface ExecChunkPayload {
  step: number;
  chunk: string;
  session_id?: string;
  turn_id?: string;
}

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
    }
  | { type: "sessionList"; sessions: SessionListItem[] }
  | { type: "sessionTabs"; activeId: string; tabs: SessionListItem[] }
  | { type: "history"; messages: ChatHistoryMessage[] }
  | { type: "clearMessages" }
  | { type: "models"; models: Array<{ id: string }>; current: string }
  | {
      type: "providerModels";
      activeProvider: string;
      activeModel: string;
      providers: ProviderPickerEntry[];
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
      status?: string;
      content?: string;
      error?: string;
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
  | { type: "stepUsage"; usage: StepUsagePayload }
  | {
      type: "workflowStage";
      phase: "start" | "done";
      stage: WorkflowStagePayload;
    }
  | { type: "healthStatus"; lspStatus?: string; model?: string; provider?: string }
  | { type: "contextInfo"; info: ContextInfoPayload }
  | { type: "systemNote"; text: string }
  | { type: "mentionResults"; query: string; files: ChatFileRef[] }
  | { type: "pendingOps"; payload: PendingOpsPayload }
  | { type: "pendingCleared" }
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
      files?: ChatFileRef[];
    }
  | { type: "attach" }
  | { type: "listSessions" }
  | { type: "newSession" }
  | { type: "openSession"; sessionId: string }
  | { type: "closeSession"; sessionId: string }
  | { type: "listModels" }
  | { type: "listProviderModels" }
  | { type: "setModel"; model: string; provider?: string }
  | { type: "applyPending"; paths?: string[]; ops?: unknown[] }
  | { type: "discardPending" }
  | { type: "togglePendingDiff" }
  | { type: "openSettings" }
  | {
      type: "permissionReply";
      approved: boolean;
      always?: boolean;
    }
  | { type: "questionReply"; answers: string[] }
  | { type: "mentionSearch"; query: string }
  | { type: "rewindToMessage"; uiIndex: number }
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
