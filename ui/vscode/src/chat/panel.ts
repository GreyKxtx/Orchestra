import * as crypto from "crypto";
import * as fs from "fs/promises";
import * as path from "path";
import * as vscode from "vscode";
import { highlightLines } from "../highlight";
import type { CoreSession, SessionMeta } from "../coreSession";
import type {
  AgentEventParams,
  ChatFileKind,
  ChatFileRef,
  ConnectionStatus,
  HostToWebview,
  PendingOpsPayload,
  PermissionRequestPayload,
  QuestionItemPayload,
  StepUsagePayload,
  TodoItemPayload,
  WebviewToHost,
  WorkflowStagePayload,
} from "../protocol/events";
import { SettingsView } from "./settings";
import { stripFinalEnvelope, sanitizeAssistantStream, shouldSuppressStreamChunk, looksLikeCorruptedStream, isBenignTurnError } from "./streamSanitize";
import {
  buildAssistantProjection,
  effectiveContextLimit,
  estimatePromptTokensFromUI,
  joinAssistantStreamSegments,
  reasoningFromUIMessage,
  sumCompletionTokensFromUI,
  diffFromToolArgs,
  toolBlocksFromUIMessage,
  toolStatusFromResult,
  type TurnToolTracker,
} from "./turnProjection";
import { PendingHighlightManager } from "./pendingHighlight";

/** Matches core `attachments.MaxImageBytes` (20 MB). */
const MAX_ATTACHMENT_BYTES = 20 * 1024 * 1024;

/** Must match webview `send` payload (+ stable id for queue cancel). */
interface PendingSend {
  id: string;
  text: string;
  mode?: string;
  profile?: string;
  allowExec?: boolean;
  files?: ChatFileRef[];
}

export class ChatPanel implements vscode.Disposable, vscode.WebviewViewProvider {
  public static readonly viewType = "orchestra.chat";
  public static readonly sidebarViewType = "orchestra.chatSidebar";

  private panel: vscode.WebviewPanel | undefined;
  private sidebar: vscode.WebviewView | undefined;
  private view: "chat" | "settings" = "chat";
  private readonly session: CoreSession;
  private readonly extensionUri: vscode.Uri;
  private readonly settings: SettingsView;
  private readonly disposables: vscode.Disposable[] = [];
  /** Streamed assistant text for the active LLM step (reset after each tool batch). */
  private turnAssistantText = "";
  /** Committed prose segments before tool blocks within the same user turn. */
  private turnAssistantSegments: string[] = [];
  private turnReasoning = "";
  private readonly turnToolBlocks = new Map<string, TurnToolTracker>();
  private turnPromptCtx = 0;
  private turnTokensIn = 0;
  private turnTokensOut = 0;
  /** FIFO of user sends while an agent turn is in flight. */
  private readonly sendQueue: PendingSend[] = [];
  private sendInFlight = false;
  /** Prevent repeated auto-cancel on corrupted stream spam. */
  /** Parent dirs of chat attachments outside workspace — added to webview localResourceRoots. */
  private readonly extraResourceRoots: vscode.Uri[] = [];
  private rootsUpdateTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly pendingHighlights = new PendingHighlightManager();
  /** Webview whose html is currently the chat page (idempotent showChat). */
  private chatHtmlWebview: vscode.Webview | undefined;
  /** Coalesced streaming: deltaSync snapshots are flushed at most every 40 ms. */
  private deltaSyncTimer: ReturnType<typeof setTimeout> | undefined;
  /** Critical messages re-posted after a webview reload ("ready"). */
  private pendingPermission: PermissionRequestPayload | undefined;
  private pendingQuestions: QuestionItemPayload[] | undefined;
  private lastPendingOps: PendingOpsPayload | undefined;
  /**
   * Sessions hidden from the tab strip. Closing a tab must NOT delete the
   * chat from disk (that is session.close semantics in the core) — deletion
   * is a separate explicit action in the history menu.
   */
  private readonly hiddenTabIds = new Set<string>();
  private readonly workspaceState: vscode.Memento | undefined;
  private static readonly HIDDEN_TABS_KEY = "orchestra.hiddenTabIds";

  constructor(session: CoreSession, extensionUri: vscode.Uri, workspaceState?: vscode.Memento) {
    this.session = session;
    this.extensionUri = extensionUri;
    this.workspaceState = workspaceState;
    for (const id of workspaceState?.get<string[]>(ChatPanel.HIDDEN_TABS_KEY) ?? []) {
      this.hiddenTabIds.add(id);
    }
    this.settings = new SettingsView(session, extensionUri);
    this.settings.bindPost((msg) => {
      void this.webviewTarget()?.postMessage(msg);
    });

    const onAgent = (event: AgentEventParams): void => {
      // Always accumulate the turn projection (otherwise opening Settings
      // mid-stream permanently truncates the assistant turn saved to history);
      // only rendering is gated on the active view.
      this.forwardAgentEvent(event, this.view === "chat");
    };
    const onExecChunk = (payload: { step: number; chunk: string }): void => {
      if (this.view !== "chat") {
        return;
      }
      this.post({ type: "execChunk", step: payload.step, chunk: payload.chunk });
    };
    const onWorkflow = (phase: "start" | "done", stage: WorkflowStagePayload): void => {
      if (this.view !== "chat") {
        return;
      }
      this.post({ type: "workflowStage", phase, stage });
    };
    const onStatus = (status: ConnectionStatus, detail?: string): void => {
      if (this.view !== "chat") {
        return;
      }
      const pass =
        status === "error" || status === "connecting" ? detail : undefined;
      this.post({ type: "status", status, detail: pass });
    };
    this.session.on("agentEvent", onAgent);
    this.session.on("execChunk", onExecChunk);
    this.session.on("workflowStage", onWorkflow);
    this.session.on("status", onStatus);

    const onPermission = (request: PermissionRequestPayload): void => {
      if (this.view !== "chat") {
        void this.showPermissionDialog(request);
        return;
      }
      this.pendingPermission = request;
      this.post({ type: "permissionRequest", request });
    };
    const onQuestion = (questions: QuestionItemPayload[]): void => {
      if (this.view !== "chat") {
        void this.showQuestionDialog(questions);
        return;
      }
      this.pendingQuestions = questions;
      this.post({ type: "questionAsk", questions });
    };
    this.session.on("permissionRequest", onPermission);
    this.session.on("questionAsk", onQuestion);

    this.disposables.push({
      dispose: () => {
        this.session.off("agentEvent", onAgent);
        this.session.off("execChunk", onExecChunk);
        this.session.off("workflowStage", onWorkflow);
        this.session.off("status", onStatus);
        this.session.off("permissionRequest", onPermission);
        this.session.off("questionAsk", onQuestion);
      },
    });

    this.watchProjectConfig();
  }

  /**
   * Shared-config invariant: .orchestra.yml is the single source of truth for
   * this project — the TUI/CLI edit the same file. The core hot-reloads it per
   * RPC; here we refresh the open view so the UI reflects external changes.
   */
  private watchProjectConfig(): void {
    const root = this.workspaceRoot();
    if (!root) {
      return;
    }
    const watcher = vscode.workspace.createFileSystemWatcher(
      new vscode.RelativePattern(vscode.Uri.file(root), ".orchestra.yml")
    );
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onConfigChanged = (): void => {
      if (timer) {
        clearTimeout(timer);
      }
      // Debounce: our own persists also fire the watcher.
      timer = setTimeout(() => {
        timer = undefined;
        if (this.view === "settings") {
          void this.settings.pushState();
        } else if (
          this.session.getSessionId() &&
          this.session.getConnectionStatus() !== "running"
        ) {
          // Not mid-turn: safe to re-render header/history with fresh config.
          void this.refreshHeaderAndHistory().catch(() => undefined);
        }
      }, 700);
    };
    watcher.onDidChange(onConfigChanged, null, this.disposables);
    watcher.onDidCreate(onConfigChanged, null, this.disposables);
    this.disposables.push(watcher, {
      dispose: () => {
        if (timer) {
          clearTimeout(timer);
        }
      },
    });
  }

  dispose(): void {
    if (this.rootsUpdateTimer) {
      clearTimeout(this.rootsUpdateTimer);
      this.rootsUpdateTimer = undefined;
    }
    if (this.deltaSyncTimer) {
      clearTimeout(this.deltaSyncTimer);
      this.deltaSyncTimer = undefined;
    }
    this.pendingHighlights.dispose();
    this.panel?.dispose();
    this.sidebar = undefined;
    for (const d of this.disposables) {
      d.dispose();
    }
  }

  /** Activity Bar icon → sidebar chat webview. */
  resolveWebviewView(
    webviewView: vscode.WebviewView,
    _context: vscode.WebviewViewResolveContext,
    _token: vscode.CancellationToken
  ): void {
    this.sidebar = webviewView;
    // Single-surface invariant: post()/webviewTarget() prefer the sidebar, so a
    // leftover editor panel would silently stop receiving messages ("connecting…"
    // forever). Close it as soon as the sidebar takes over.
    if (this.panel) {
      this.panel.dispose();
    }
    webviewView.webview.options = {
      enableScripts: true,
      localResourceRoots: this.allLocalResourceRoots(),
    };
    webviewView.webview.onDidReceiveMessage(
      (msg: unknown) => {
        void this.onAnyMessage(msg);
      },
      null,
      this.disposables
    );
    webviewView.onDidDispose(
      () => {
        if (this.sidebar === webviewView) {
          this.sidebar = undefined;
        }
        if (this.chatHtmlWebview === webviewView.webview) {
          this.chatHtmlWebview = undefined;
        }
      },
      null,
      this.disposables
    );
    void this.showChat();
  }

  async show(): Promise<void> {
    if (this.sidebar) {
      this.sidebar.show?.(true);
      await this.showChat();
      return;
    }
    try {
      await vscode.commands.executeCommand(`${ChatPanel.sidebarViewType}.focus`);
    } catch {
      // Fall back to editor panel if sidebar is unavailable.
    }
    // The focus command resolves before resolveWebviewView() runs, so give the
    // sidebar a moment to attach. Creating an editor panel here while the
    // sidebar is coming up would leave two webviews with post() feeding only
    // one of them — the other would show "connecting…" forever.
    if (await this.waitForSidebar(2000)) {
      // resolveWebviewView() has already kicked off showChat().
      return;
    }
    await this.ensurePanel();
    await this.showChat();
  }

  /** Resolves true once resolveWebviewView() has attached the sidebar. */
  private waitForSidebar(timeoutMs: number): Promise<boolean> {
    if (this.sidebar) {
      return Promise.resolve(true);
    }
    return new Promise((resolve) => {
      const started = Date.now();
      const timer = setInterval(() => {
        if (this.sidebar) {
          clearInterval(timer);
          resolve(true);
        } else if (Date.now() - started >= timeoutMs) {
          clearInterval(timer);
          resolve(false);
        }
      }, 50);
    });
  }

  async showSettings(section = "general"): Promise<void> {
    const targetSection =
      section === "orchestra" || section === "plugins" ? "general" : section;
    this.settings.pendingSection = targetSection;
    const webview = this.webviewTarget();
    if (this.sidebar && webview) {
      this.view = "settings";
      this.sidebar.title = "Settings";
      webview.html = this.settings.getHtml(webview);
      this.chatHtmlWebview = undefined;
      this.sidebar.show?.(true);
      return;
    }
    await this.ensurePanel();
    this.view = "settings";
    if (this.panel) {
      this.panel.title = "Orchestra Settings";
      this.panel.webview.html = this.settings.getHtml(this.panel.webview);
      this.chatHtmlWebview = undefined;
      this.panel.reveal(vscode.ViewColumn.Beside);
    }
    // settings.js posts ready → pushState
  }

  private async showChat(): Promise<void> {
    this.view = "chat";
    const webview = this.webviewTarget();
    if (!webview) {
      return;
    }
    if (this.sidebar) {
      this.sidebar.title = "Chat";
    }
    if (this.panel) {
      this.panel.title = "Orchestra";
    }
    // Idempotent: re-assigning html rebuilds the whole DOM and loses the input
    // draft / scroll / streaming bubble. Only (re)load when this webview does
    // not already show the chat page.
    const needsHtml = this.chatHtmlWebview !== webview;
    if (needsHtml) {
      webview.html = this.getHtml(webview);
      this.chatHtmlWebview = webview;
    }
    this.panel?.reveal(vscode.ViewColumn.Beside);

    if (!needsHtml) {
      // Webview state is intact — just make sure the session is alive.
      try {
        await this.session.startSession();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        this.post({ type: "error", message });
        this.post({ type: "status", status: "error", detail: message });
      }
      return;
    }

    this.post({
      type: "status",
      status: "connecting",
      detail: "starting core…",
    });

    try {
      await this.session.startSession();
      await this.refreshHeaderAndHistory();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.post({ type: "error", message });
      this.post({ type: "status", status: "error", detail: message });
    }
  }

  private async ensurePanel(): Promise<void> {
    if (this.panel) {
      return;
    }
    this.panel = vscode.window.createWebviewPanel(
      ChatPanel.viewType,
      "Orchestra",
      vscode.ViewColumn.Beside,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: this.allLocalResourceRoots(),
      }
    );
    this.panel.onDidDispose(
      () => {
        if (this.chatHtmlWebview === this.panel?.webview) {
          this.chatHtmlWebview = undefined;
        }
        this.panel = undefined;
        this.view = "chat";
      },
      null,
      this.disposables
    );
    this.panel.webview.onDidReceiveMessage(
      (msg: unknown) => {
        void this.onAnyMessage(msg);
      },
      null,
      this.disposables
    );
  }

  private async onAnyMessage(raw: unknown): Promise<void> {
    if (this.view === "settings") {
      if (raw && typeof raw === "object" && (raw as { type?: string }).type === "backToChat") {
        await this.showChat();
        return;
      }
      await this.settings.handleMessage(raw);
      return;
    }
    await this.onWebviewMessage(raw as WebviewToHost);
  }

  private async refreshHeaderAndHistory(): Promise<void> {
    const id = this.session.getSessionId();
    if (!id) {
      return;
    }
    const [view, health, llm] = await Promise.all([
      this.session.getSession(id),
      this.session.healthInfo(),
      this.session.getLLM(),
    ]);
    const title = view.title || "New chat";
    const model = health.model || view.model || "Model";
    this.post({ type: "ready", sessionId: id });
    this.post({
      type: "header",
      sessionId: id,
      title,
      model,
      provider: health.provider,
    });
    this.post({ type: "status", status: "ready" });
    const healthRaw = health.raw as { lsp_status?: string } | undefined;
    this.post({
      type: "healthStatus",
      lspStatus: typeof healthRaw?.lsp_status === "string" ? healthRaw.lsp_status : undefined,
      model: health.model,
      provider: health.provider,
    });
    this.post({
      type: "contextInfo",
      info: {
        contextLimit: effectiveContextLimit(llm.numCtx, llm.contextTokens),
        maxResponseTokens: llm.maxTokens > 0 ? llm.maxTokens : 4096,
        model: health.model || llm.model,
      },
    });
    this.post({ type: "clearMessages" });
    const history: Array<{
      role: string;
      text: string;
      uiIndex?: number;
      files?: ChatFileRef[];
      reasoning?: string;
      toolBlocks?: Array<{
        id?: string;
        name: string;
        argsRaw?: string;
        status?: string;
        result?: string;
        diagnostics?: import("../protocol/events").ToolDiagnosticPayload[];
      }>;
      promptCtx?: number;
      tokensIn?: number;
      tokensOut?: number;
    }> = [];
    let restoredPrompt = 0;
    let restoredCompletion = 0;
    let restoredEstimated = false;
    for (let idx = 0; idx < view.uiMessages.length; idx++) {
      const m = view.uiMessages[idx];
      const raw = (m.text || m.content || "").trim();
      const role = (m.role || "assistant").toLowerCase();
      let files: ChatFileRef[] | undefined;
      if (role === "user" && Array.isArray(m.attachments) && m.attachments.length > 0) {
        files = await Promise.all(
          m.attachments.map((a) =>
            this.enrichFileRef({
              name: a.name || path.basename(a.path || "file"),
              path: a.path,
              ext: a.ext,
              kind: (a.kind as ChatFileKind) || "binary",
            })
          )
        );
      } else if (role === "user") {
        files = attachmentRefsFromText(raw);
        if (files.length > 0) {
          files = await Promise.all(files.map((f) => this.enrichFileRef(f)));
        } else {
          files = undefined;
        }
      }
      const text = uiDisplayText(raw);
      const reasoning = reasoningFromUIMessage(m);
      const toolBlocks = toolBlocksFromUIMessage(m).map((t) => {
        const diff = diffFromToolArgs(t.name, t.args_raw);
        return {
          id: t.id,
          name: t.name,
          argsRaw: t.args_raw,
          status: t.status,
          result: t.result,
          diagnostics: t.diagnostics,
          diffBefore: diff?.before,
          diffAfter: diff?.after,
        };
      });
      if (role === "assistant") {
        if (typeof m.prompt_ctx === "number" && m.prompt_ctx > 0) {
          restoredPrompt = m.prompt_ctx;
        }
        if (typeof m.tokens_out === "number" && m.tokens_out > 0) {
          restoredCompletion = m.tokens_out;
        } else if (typeof m.tokens_in === "number" && m.tokens_in > 0 && restoredPrompt === 0) {
          restoredPrompt = m.tokens_in;
        }
      }
      const hasTools = toolBlocks.length > 0;
      const hasReasoning = reasoning.length > 0;
      if (text.length > 0 || (files && files.length > 0) || hasTools || hasReasoning) {
        history.push({
          role,
          text,
          uiIndex: role === "user" ? idx : undefined,
          files: files?.length ? files : undefined,
          reasoning: hasReasoning ? reasoning : undefined,
          toolBlocks: hasTools ? toolBlocks : undefined,
          promptCtx: role === "assistant" ? m.prompt_ctx : undefined,
          tokensIn: role === "assistant" ? m.tokens_in : undefined,
          tokensOut: role === "assistant" ? m.tokens_out : undefined,
        });
      }
    }
    if (history.length > 0) {
      this.post({ type: "history", messages: history });
    }
    if (restoredPrompt <= 0 && view.uiMessages.length > 0) {
      restoredPrompt = estimatePromptTokensFromUI(view.uiMessages);
      restoredEstimated = restoredPrompt > 0;
    }
    if (restoredCompletion <= 0 && view.uiMessages.length > 0) {
      restoredCompletion = sumCompletionTokensFromUI(view.uiMessages);
    }
    this.post({
      type: "stepUsage",
      usage: {
        prompt_tokens: restoredPrompt,
        completion_tokens: restoredCompletion,
        source: restoredEstimated ? "estimate" : restoredPrompt > 0 ? "restored" : "session",
      },
    });
    await this.refreshSessionTabs();
  }

  private sortSessionsByRecent(sessions: SessionMeta[]): SessionMeta[] {
    return [...sessions].sort((a, b) => {
      const ta = a.updated_at || "";
      const tb = b.updated_at || "";
      return tb.localeCompare(ta);
    });
  }

  private async persistHiddenTabs(): Promise<void> {
    await this.workspaceState?.update(ChatPanel.HIDDEN_TABS_KEY, [...this.hiddenTabIds]);
  }

  private async refreshSessionTabs(): Promise<void> {
    const activeId = this.session.getSessionId();
    if (!activeId) {
      return;
    }
    // Opening a previously hidden chat (e.g. from the history menu) unhides it.
    if (this.hiddenTabIds.delete(activeId)) {
      await this.persistHiddenTabs();
    }
    const sessions = this.sortSessionsByRecent(await this.session.listSessions());
    const tabs = sessions
      .filter((s) => !this.hiddenTabIds.has(s.id))
      .slice(0, 16)
      .map((s) => ({
        id: s.id,
        title: (s.title || "New chat").trim() || "New chat",
        model: s.model,
        msg_count: s.msg_count,
      }));
    if (!tabs.some((t) => t.id === activeId)) {
      const view = await this.session.getSession(activeId);
      tabs.unshift({
        id: activeId,
        title: view.title || "New chat",
        model: view.model,
        msg_count: undefined,
      });
    }
    this.post({ type: "sessionTabs", activeId, tabs });
  }

  private async onWebviewMessage(msg: WebviewToHost): Promise<void> {
    try {
      switch (msg.type) {
        case "ready":
          if (this.session.getSessionId()) {
            await this.refreshHeaderAndHistory();
          }
          // Re-deliver dialogs/state that were posted while the webview was
          // being recreated — otherwise the core waits forever on an answer.
          if (this.pendingPermission && this.session.hasPendingServerRequests()) {
            this.post({ type: "permissionRequest", request: this.pendingPermission });
          }
          if (this.pendingQuestions && this.session.hasPendingServerRequests()) {
            this.post({ type: "questionAsk", questions: this.pendingQuestions });
          }
          if (this.lastPendingOps) {
            this.post({ type: "pendingOps", payload: this.lastPendingOps });
          }
          return;
        case "attach": {
          const picked = await vscode.window.showOpenDialog({
            canSelectMany: true,
            openLabel: "Attach to Orchestra chat",
            title: "Attach files",
          });
          if (picked && picked.length > 0) {
            const files: ChatFileRef[] = [];
            try {
              for (const u of picked) {
                files.push(await this.toFileRef(u));
              }
              this.post({ type: "filesPicked", files });
            } catch (err) {
              const message = err instanceof Error ? err.message : String(err);
              void vscode.window.showErrorMessage(`Orchestra: ${message}`);
            }
          }
          return;
        }
        case "attachBytes": {
          const name = (msg.name || "attachment").trim() || "attachment";
          const data = Buffer.from(msg.dataBase64 || "", "base64");
          if (data.length === 0) {
            return;
          }
          if (data.length > MAX_ATTACHMENT_BYTES) {
            void vscode.window.showErrorMessage(
              `Orchestra: attachment exceeds ${MAX_ATTACHMENT_BYTES / (1024 * 1024)} MB`
            );
            return;
          }
          const root = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
          if (!root) {
            void vscode.window.showErrorMessage("Orchestra: open a workspace folder to attach files");
            return;
          }
          const ext = path.extname(name) || (msg.mime?.includes("png") ? ".png" : ".bin");
          const safeBase = name.replace(/[^\w.\-()+ ]+/g, "_").replace(/\s+/g, "-");
          const dir = path.join(root, ".orchestra", "attachments");
          await fs.mkdir(dir, { recursive: true });
          const dest = path.join(dir, `${Date.now()}-${safeBase}${ext && !safeBase.includes(".") ? ext : ""}`);
          await fs.writeFile(dest, data);
          const ref = await this.toFileRef(vscode.Uri.file(dest));
          this.post({ type: "filesPicked", files: [ref] });
          return;
        }
        case "listSessions": {
          const sessions = this.sortSessionsByRecent(await this.session.listSessions());
          this.post({
            type: "sessionList",
            sessions: sessions.map((s) => ({
              id: s.id,
              title: s.title || "New chat",
              model: s.model,
              msg_count: s.msg_count,
              updated_at: s.updated_at,
              created_at: s.created_at,
            })),
          });
          return;
        }
        case "newSession": {
          this.clearSendQueue();
          this.resetTurnProjection();
          await this.session.startSession({ forceNew: true });
          await this.refreshHeaderAndHistory();
          return;
        }
        case "openSession": {
          this.clearSendQueue();
          this.resetTurnProjection();
          await this.session.startSession({ sessionId: msg.sessionId });
          await this.refreshHeaderAndHistory();
          return;
        }
        case "closeSession": {
          // Closing a tab only hides it — the chat stays on disk and remains
          // reachable from the history menu. Permanent removal is deleteSession.
          const sid = msg.sessionId.trim();
          if (!sid) {
            return;
          }
          this.hiddenTabIds.add(sid);
          await this.persistHiddenTabs();
          const active = this.session.getSessionId();
          if (sid === active) {
            const remaining = this.sortSessionsByRecent(await this.session.listSessions()).filter(
              (s) => !this.hiddenTabIds.has(s.id)
            );
            if (remaining.length > 0) {
              await this.session.startSession({ sessionId: remaining[0].id });
            } else {
              await this.session.startSession({ forceNew: true });
            }
            await this.refreshHeaderAndHistory();
          } else {
            await this.refreshSessionTabs();
          }
          return;
        }
        case "deleteSession": {
          const sid = msg.sessionId.trim();
          if (!sid) {
            return;
          }
          const sessions = await this.session.listSessions();
          const meta = sessions.find((s) => s.id === sid);
          const label = (meta?.title || "").trim() || sid;
          const choice = await vscode.window.showWarningMessage(
            `Delete chat "${label}"? This cannot be undone.`,
            { modal: true },
            "Delete"
          );
          if (choice !== "Delete") {
            return;
          }
          const active = this.session.getSessionId();
          await this.session.closeSession(sid);
          if (this.hiddenTabIds.delete(sid)) {
            await this.persistHiddenTabs();
          }
          if (sid === active) {
            const remaining = this.sortSessionsByRecent(await this.session.listSessions()).filter(
              (s) => !this.hiddenTabIds.has(s.id)
            );
            if (remaining.length > 0) {
              await this.session.startSession({ sessionId: remaining[0].id });
            } else {
              await this.session.startSession({ forceNew: true });
            }
            await this.refreshHeaderAndHistory();
          } else {
            await this.refreshSessionTabs();
          }
          // The history menu is open while deleting — push the updated list.
          const refreshed = this.sortSessionsByRecent(await this.session.listSessions());
          this.post({
            type: "sessionList",
            sessions: refreshed.map((s) => ({
              id: s.id,
              title: s.title || "New chat",
              model: s.model,
              msg_count: s.msg_count,
              updated_at: s.updated_at,
              created_at: s.created_at,
            })),
          });
          return;
        }
        case "listModels": {
          const listed = await this.session.listModels();
          this.post({
            type: "models",
            models: listed.models.map((m) => ({ id: m.id })),
            current: listed.current,
          });
          return;
        }
        case "listProviderModels": {
          const catalog = await this.session.listProviders({ probe: true });
          this.post({
            type: "providerModels",
            activeProvider: catalog.activeProvider,
            activeModel: catalog.activeModel,
            providers: catalog.providers
              .filter((p) => p.configured || p.active || (p.ready && (p.model_count > 0 || (p.models?.length ?? 0) > 0)))
              .map((p) => ({
                key: p.key,
                name: p.name,
                active: p.active,
                ready: p.ready,
                models: p.models,
                models_error: p.models_error,
                model_count: p.model_count,
              })),
          });
          return;
        }
        case "setModel": {
          const res = await this.session.setModel(msg.model, {
            persist: true,
            provider: msg.provider,
          });
          void vscode.window.showInformationMessage(
            `Orchestra model: ${res.model}${res.persisted ? " (saved)" : ""}`
          );
          await this.refreshHeaderAndHistory();
          return;
        }
        case "openSettings": {
          const section = typeof msg.section === "string" ? msg.section : "general";
          await this.showSettings(section);
          return;
        }
        case "openOrchestraSettings": {
          await this.showSettings("general");
          return;
        }
        case "listOrchestraRoles": {
          // Orchestra mode footer: the single-model pill is replaced by the
          // tier breakdown so the user sees which model each role really uses.
          try {
            const orch = await this.session.getOrchestra();
            this.post({
              type: "orchestraRoles",
              roles: orch.roles,
              defaultTier: orch.defaultTier,
            });
          } catch (err) {
            this.post({
              type: "orchestraRoles",
              roles: [],
              defaultTier: "",
              error: err instanceof Error ? err.message : String(err),
            });
          }
          return;
        }
        case "send": {
          await this.handleSendRequest({
            id: crypto.randomUUID(),
            text: msg.text,
            mode: msg.mode,
            profile: msg.profile,
            allowExec: msg.allowExec,
            files: msg.files,
          });
          return;
        }
        case "cancelQueuedSend": {
          const id = (msg.id || "").trim();
          if (!id) {
            return;
          }
          const before = this.sendQueue.length;
          this.sendQueue.splice(
            0,
            this.sendQueue.length,
            ...this.sendQueue.filter((q) => q.id !== id)
          );
          if (this.sendQueue.length !== before) {
            this.postQueueUpdate();
          }
          return;
        }
        case "cancelTurn": {
          this.clearSendQueue();
          try {
            await this.session.cancelTurn();
            this.post({ type: "systemNote", text: "Turn stopped." });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
          }
          return;
        }
        case "applyPending": {
          try {
            const paths = Array.isArray(msg.paths)
              ? msg.paths.filter((p): p is string => typeof p === "string" && p.trim() !== "")
              : undefined;
            const res = await this.session.applyPending(true, paths);
            if (res.applied) {
              void vscode.window.showInformationMessage("Orchestra: changes applied");
              const remaining = res.remainingOps;
              if (remaining && remaining.length > 0) {
                this.post({
                  type: "pendingOps",
                  payload: { ops: remaining, diff: [], applied: false },
                });
              } else {
                this.post({ type: "pendingCleared" });
              }
            } else {
              void vscode.window.showWarningMessage("Orchestra: no pending changes to apply");
            }
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
          }
          return;
        }
        case "discardPending": {
          try {
            await this.session.discardPending();
            this.post({ type: "pendingCleared" });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
          }
          return;
        }
        case "permissionReply": {
          this.pendingPermission = undefined;
          this.session.resolvePermission({
            approved: Boolean(msg.approved),
            always: Boolean(msg.always),
          });
          return;
        }
        case "questionReply": {
          this.pendingQuestions = undefined;
          const answers = Array.isArray(msg.answers)
            ? msg.answers.filter((a): a is string => typeof a === "string")
            : [];
          this.session.resolveQuestion(answers);
          return;
        }
        case "mentionSearch": {
          try {
            const files = await this.searchMentions(msg.query || "");
            this.post({ type: "mentionResults", query: msg.query || "", files });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "mentionResults", query: msg.query || "", files: [] });
            void vscode.window.showErrorMessage(`Orchestra mention search: ${message}`);
          }
          return;
        }
        case "rewindToMessage": {
          if (typeof msg.uiIndex !== "number" || msg.uiIndex < 0) {
            return;
          }
          try {
            if (this.sendInFlight) {
              this.clearSendQueue();
              await this.session.cancelTurn();
            }
            let res: { uiMessages: number; historyMessages: number } | undefined;
            let lastErr: unknown;
            for (let attempt = 0; attempt < 8; attempt++) {
              try {
                res = await this.session.rewindSession(msg.uiIndex);
                lastErr = undefined;
                break;
              } catch (err) {
                lastErr = err;
                const message = err instanceof Error ? err.message : String(err);
                if (attempt < 7 && /busy/i.test(message)) {
                  await new Promise((r) => setTimeout(r, 150));
                  continue;
                }
                throw err;
              }
            }
            if (!res) {
              throw lastErr instanceof Error ? lastErr : new Error("rewind failed");
            }
            void vscode.window.showInformationMessage(
              `Orchestra: rewound to message (${res.uiMessages} UI msgs)`
            );
            this.resetTurnProjection();
            await this.refreshHeaderAndHistory();
            this.post({ type: "pendingCleared" });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
            void vscode.window.showErrorMessage(`Orchestra rewind: ${message}`);
          }
          return;
        }
        case "compactSession": {
          try {
            const res = await this.session.compactSession(msg.query);
            void vscode.window.showInformationMessage(
              `Orchestra: compacted ${res.beforeMsgs} → ${res.afterMsgs} msgs`
            );
            await this.refreshHeaderAndHistory();
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
          }
          return;
        }
        case "slashCommand": {
          await this.handleSlashCommand(msg.cmd, msg.arg);
          return;
        }
        case "openFile": {
          const fp = (msg.path || "").trim();
          if (!fp) {
            return;
          }
          this.registerAttachmentPath(fp);
          await this.openInWorkspaceEditor(vscode.Uri.file(fp), {
            focus: msg.focus === true,
          });
          return;
        }
        case "previewAttachment": {
          const fp = (msg.path || "").trim();
          if (!fp) {
            return;
          }
          this.registerAttachmentPath(fp);
          await this.openInWorkspaceEditor(vscode.Uri.file(fp), {
            focus: msg.focus === true,
          });
          return;
        }
        case "openDiff": {
          const fp = (msg.path || "").trim();
          if (!fp) {
            return;
          }
          if (msg.sideBySide === true) {
            await this.openSideBySideDiff(
              fp,
              msg.before ?? "",
              msg.after ?? "",
              msg.focus === true
            );
          } else {
            await this.openFileWithChangeHighlights(
              fp,
              msg.before ?? "",
              msg.after ?? "",
              msg.focus !== false
            );
          }
          return;
        }
        case "highlightCode": {
          const reqId = msg.requestId || "";
          const lines = Array.isArray(msg.lines) ? msg.lines : [];
          const lang = msg.language || "plaintext";
          try {
            const highlighted = await highlightLines(lines, lang);
            this.post({ type: "highlightResult", requestId: reqId, lines: highlighted });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({
              type: "highlightResult",
              requestId: reqId,
              lines: lines.map((l) =>
                String(l)
                  .replace(/&/g, "&amp;")
                  .replace(/</g, "&lt;")
                  .replace(/>/g, "&gt;")
              ),
            });
            void vscode.window.showErrorMessage(`Orchestra highlight: ${message}`);
          }
          return;
        }
        default:
          return;
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.post({ type: "error", message });
      this.post({ type: "status", status: "error", detail: message });
    }
  }

  private resetTurnProjection(): void {
    this.flushDeltaSync(false);
    this.turnAssistantText = "";
    this.turnAssistantSegments = [];
    this.turnReasoning = "";
    this.turnToolBlocks.clear();
    this.turnPromptCtx = 0;
    this.turnTokensIn = 0;
    this.turnTokensOut = 0;
  }

  private clearSendQueue(): void {
    if (this.sendQueue.length === 0) {
      return;
    }
    this.sendQueue.length = 0;
    this.postQueueUpdate();
  }

  private queuePreview(item: PendingSend): string {
    const t = item.text.trim();
    if (t) {
      return t.length > 96 ? `${t.slice(0, 93)}…` : t;
    }
    const n = item.files?.length || 0;
    return n > 0 ? `${n} attachment${n === 1 ? "" : "s"}` : "";
  }

  private postQueueUpdate(): void {
    this.post({
      type: "queueUpdate",
      items: this.sendQueue.map((q) => ({
        id: q.id,
        preview: this.queuePreview(q),
        fileCount: q.files?.length || 0,
      })),
    });
  }

  private async handleSendRequest(item: PendingSend): Promise<void> {
    const userText = item.text.trim();
    const hasFiles = Boolean(item.files && item.files.length > 0);
    if (!userText && !hasFiles) {
      return;
    }
    if (this.sendInFlight) {
      this.sendQueue.push(item);
      this.postQueueUpdate();
      return;
    }
    this.sendInFlight = true;
    let drainOk = true;
    try {
      drainOk = await this.runSendTurn(item);
      while (this.sendQueue.length > 0) {
        const next = this.sendQueue.shift()!;
        this.postQueueUpdate();
        const ok = await this.runSendTurn(next);
        drainOk = drainOk && ok;
      }
    } finally {
      this.sendInFlight = false;
      this.postQueueUpdate();
      this.post({ type: "status", status: drainOk ? "ready" : "error" });
    }
  }

  private async runSendTurn(item: PendingSend): Promise<boolean> {
    const userText = item.text.trim();
    const hasFiles = Boolean(item.files && item.files.length > 0);
    const prepared = hasFiles
      ? await Promise.all(item.files!.map((f) => this.prepareAttachmentForSend(f)))
      : undefined;
    const echoFiles = prepared
      ? await Promise.all(prepared.map((f) => this.enrichFileRef(f)))
      : undefined;
    let echoUiIndex: number | undefined;
    try {
      const sid = this.session.getSessionId();
      if (sid) {
        const view = await this.session.getSession(sid);
        echoUiIndex = view.uiMessages.length;
      }
    } catch {
      echoUiIndex = undefined;
    }
    this.post({ type: "turnStart" });
    this.post({
      type: "userEcho",
      text: userText,
      files: echoFiles,
      uiIndex: echoUiIndex,
    });
    this.post({ type: "status", status: "running" });
    this.resetTurnProjection();
    let ok = true;
    try {
      // Clear any leftover busy turn from a previous Stop that didn't unwind.
      try {
        await this.session.clearServerBusy();
      } catch {
        /* idle */
      }
      await this.session.maybeSetSessionTitle(
        userText ||
          item.files?.map((f) => f.name).filter(Boolean).join(", ") ||
          "Attachment"
      );
      await this.session.sendMessage(userText, {
        apply: item.allowExec === true,
        allowExec: item.allowExec === true,
        mode: item.mode,
        profile: item.profile,
        attachments: prepared,
      });
      // Deliver the tail of the coalesced stream before finalizing the turn.
      this.flushDeltaSync();
      const id = this.session.getSessionId();
      if (id) {
        const [view, health] = await Promise.all([
          this.session.getSession(id),
          this.session.healthInfo(),
        ]);
        this.post({
          type: "header",
          sessionId: id,
          title: view.title || "New chat",
          model: health.model || view.model || "Model",
          provider: health.provider,
        });
      }
      await this.ensureTurnPromptEstimate(id);
      if (
        looksLikeCorruptedStream(this.turnAssistantText) &&
        this.turnToolBlocks.size === 0
      ) {
        this.post({ type: "discardAssistantBubble" });
        this.post({
          type: "systemNote",
          text:
            "Модель вернула некорректный поток вместо вызова edit/write. " +
            "Попробуйте ещё раз, уточните запрос или смените модель в composer.",
        });
      }
      await this.syncTurnProjection();
      this.resetTurnProjection();
    } catch (err) {
      ok = false;
      this.flushDeltaSync();
      const message = err instanceof Error ? err.message : String(err);
      if (!isBenignTurnError(message)) {
        this.post({ type: "error", message });
      }
      if (this.turnAssistantText.trim() || this.turnToolBlocks.size > 0) {
        await this.ensureTurnPromptEstimate();
        await this.syncTurnProjection();
      }
      this.resetTurnProjection();
    } finally {
      this.flushDeltaSync();
      const queuedNext = this.sendQueue.length > 0;
      this.post({ type: "turnComplete", ok, queuedNext });
    }
    return ok;
  }

  private currentStreamVisibleText(): string {
    return sanitizeAssistantStream(stripFinalEnvelope(this.turnAssistantText));
  }

  /** Finalize streamed prose before tool blocks (matches webview commitPreToolText). */
  private commitAssistantStreamSegment(): void {
    const seg = this.currentStreamVisibleText().trim();
    if (seg) {
      this.turnAssistantSegments.push(seg);
    }
    this.turnAssistantText = "";
  }

  private assistantVisibleText(): string {
    return joinAssistantStreamSegments(this.turnAssistantSegments, this.currentStreamVisibleText());
  }

  private async syncTurnProjection(): Promise<void> {
    const projection = buildAssistantProjection({
      text: this.assistantVisibleText(),
      reasoning: this.turnReasoning,
      tools: this.turnToolBlocks,
      promptCtx: this.turnPromptCtx,
      tokensIn: this.turnTokensIn,
      tokensOut: this.turnTokensOut,
    });
    if (projection) {
      await this.session.syncUIProjection(projection);
    }
  }

  /** Fill prompt_ctx when the provider omits stream usage (common with LM Studio). */
  private async ensureTurnPromptEstimate(sessionId?: string): Promise<void> {
    if (this.turnPromptCtx > 0) {
      return;
    }
    const id = (sessionId || this.session.getSessionId() || "").trim();
    if (!id) {
      return;
    }
    const view = await this.session.getSession(id);
    const proj = buildAssistantProjection({
      text: this.assistantVisibleText(),
      reasoning: this.turnReasoning,
      tools: this.turnToolBlocks,
      promptCtx: 0,
      tokensIn: this.turnTokensIn,
      tokensOut: this.turnTokensOut,
    });
    const ui = [...view.uiMessages];
    if (proj) {
      ui.push({
        role: "assistant",
        text: proj.text,
        reasoning: proj.reasoning,
        tool_blocks: proj.tool_blocks,
      });
    }
    const est = estimatePromptTokensFromUI(ui);
    if (est <= 0) {
      return;
    }
    this.turnPromptCtx = est;
    if (this.turnTokensOut <= 0 && proj?.text) {
      this.turnTokensOut = Math.max(1, Math.ceil(proj.text.length / 4));
    }
    this.post({
      type: "stepUsage",
      usage: {
        prompt_tokens: est,
        completion_tokens: this.turnTokensOut,
        source: "estimate",
      },
    });
  }

  /** Coalesce full-snapshot deltaSync posts (O(n²) traffic otherwise). */
  private scheduleDeltaSync(): void {
    if (this.deltaSyncTimer) {
      return;
    }
    this.deltaSyncTimer = setTimeout(() => {
      this.deltaSyncTimer = undefined;
      if (this.view === "chat") {
        this.post({ type: "deltaSync", content: this.currentStreamVisibleText() });
      }
    }, 40);
  }

  /** Flush (or drop) a scheduled deltaSync so ordered messages stay ordered. */
  private flushDeltaSync(emit = true): void {
    if (!this.deltaSyncTimer) {
      return;
    }
    clearTimeout(this.deltaSyncTimer);
    this.deltaSyncTimer = undefined;
    if (emit && this.view === "chat") {
      this.post({ type: "deltaSync", content: this.currentStreamVisibleText() });
    }
  }

  /**
   * Handle one agent/event. State (turn projection) is always accumulated;
   * `render` gates webview posts (false while the Settings view is open).
   */
  private forwardAgentEvent(event: AgentEventParams, render = true): void {
    const post = (msg: HostToWebview): void => {
      if (render) {
        this.post(msg);
      } else if (msg.type === "pendingOps" || msg.type === "pendingCleared") {
        // Keep pending-changes state in sync even without rendering.
        this.lastPendingOps = msg.type === "pendingOps" ? msg.payload : undefined;
      }
    };
    const childCtx = {
      scope: event.scope,
      taskId: event.task_id,
      parentToolCallId: event.parent_tool_call_id,
      subagentType: event.subagent_type,
    };
    switch (event.type) {
      case "child_started":
        post({
          type: "childLifecycle",
          phase: "started",
          taskId: event.task_id || "",
          parentToolCallId: event.parent_tool_call_id,
          subagentType: event.subagent_type,
          tier: event.tier,
          model: event.model,
          content: event.content,
        });
        break;
      case "child_done":
        post({
          type: "childLifecycle",
          phase: "done",
          taskId: event.task_id || "",
          parentToolCallId: event.parent_tool_call_id,
          subagentType: event.subagent_type,
          status: event.status,
          error: event.error,
        });
        break;
      case "message_delta":
        if (event.content && event.scope !== "child") {
          if (!shouldSuppressStreamChunk(event.content)) {
            this.turnAssistantText += event.content;
          }
          if (render) {
            this.scheduleDeltaSync();
          }
        }
        break;
      case "reasoning_delta":
        if (event.content && event.scope !== "child") {
          this.turnReasoning += event.content;
          post({ type: "reasoningDelta", content: event.content });
        }
        break;
      case "tool_call_start":
        if (event.scope !== "child") {
          // Ordered delivery: push the buffered stream text before the tool block.
          this.flushDeltaSync();
          this.commitAssistantStreamSegment();
        }
        if (event.scope !== "child" && event.tool_call_id) {
          this.turnToolBlocks.set(event.tool_call_id, {
            id: event.tool_call_id,
            name: event.tool_call_name || "tool",
            argsRaw: "",
            status: "running",
            result: "",
          });
        }
        post({
          type: "toolBlock",
          phase: "start",
          toolCallId: event.tool_call_id,
          toolName: event.tool_call_name || "tool",
          step: event.step,
          ...childCtx,
        });
        break;
      case "tool_call_delta":
        if (event.args_delta && event.tool_call_id) {
          const tb = this.turnToolBlocks.get(event.tool_call_id);
          if (tb) {
            tb.argsRaw += event.args_delta;
          }
        }
        if (event.args_delta) {
          post({
            type: "toolBlock",
            phase: "update",
            toolCallId: event.tool_call_id,
            toolName: event.tool_call_name || "tool",
            argsDelta: event.args_delta,
            step: event.step,
            ...childCtx,
          });
        }
        break;
      case "tool_call_completed":
        if (event.tool_call_id) {
          const content = event.content || "";
          const existing = this.turnToolBlocks.get(event.tool_call_id);
          this.turnToolBlocks.set(event.tool_call_id, {
            id: event.tool_call_id,
            name: event.tool_call_name || existing?.name || "tool",
            argsRaw: existing?.argsRaw || "",
            status: toolStatusFromResult(content),
            result: content,
            diagnostics: event.diagnostics,
          });
        }
        post({
          type: "toolBlock",
          phase: "complete",
          toolCallId: event.tool_call_id,
          toolName: event.tool_call_name || "tool",
          content: event.content || "",
          step: event.step,
          diagnostics: event.diagnostics,
          ...childCtx,
        });
        break;
      case "todos_updated": {
        const todos = parseTodosUpdated(event.content);
        if (todos.length > 0) {
          post({ type: "todosUpdate", todos });
        }
        break;
      }
      case "step_usage": {
        const usage = parseStepUsage(event.data);
        if (usage) {
          if (typeof usage.prompt_tokens === "number" && usage.prompt_tokens > 0) {
            this.turnPromptCtx = usage.prompt_tokens;
          }
          if (typeof usage.completion_tokens === "number" && usage.completion_tokens > 0) {
            this.turnTokensOut = usage.completion_tokens;
          }
          if (typeof usage.total_tokens === "number" && usage.total_tokens > 0) {
            this.turnTokensIn = usage.total_tokens;
          } else if (typeof usage.prompt_tokens === "number" && usage.prompt_tokens > 0) {
            this.turnTokensIn = usage.prompt_tokens;
          }
          post({ type: "stepUsage", usage });
        }
        break;
      }
      case "recoverable_error":
      case "error":
        if (event.content && !isBenignTurnError(event.content)) {
          post({ type: "error", message: event.content });
        }
        break;
      case "pending_ops": {
        let payload = parsePendingOps(event.data);
        if (!payload && event.content) {
          payload = parsePendingOps(parseJSONSafe(event.content));
        }
        if (payload && !payload.applied) {
          post({ type: "pendingOps", payload });
        } else if (payload?.applied) {
          post({ type: "pendingCleared" });
        }
        break;
      }
      default:
        break;
    }
  }

  private async handleSlashCommand(cmd: string, arg?: string): Promise<void> {
    const name = (cmd || "").trim().toLowerCase();
    switch (name) {
      case "/clear":
        this.clearSendQueue();
        this.resetTurnProjection();
        await this.session.startSession({ forceNew: true });
        await this.refreshHeaderAndHistory();
        break;
      case "/compact":
        await this.onWebviewMessage({ type: "compactSession", query: arg });
        break;
      case "/sessions":
        this.post({ type: "systemNote", text: "Open the session menu (↑ title bar) to switch chats." });
        break;
      case "/model":
        this.post({ type: "systemNote", text: "Use the model pill in the composer to change model." });
        break;
      case "/settings":
        await this.showSettings();
        break;
      case "/help":
        this.post({
          type: "systemNote",
          text: [
            "Slash commands:",
            "/clear — new chat",
            "/compact [hint] — compress LLM context",
            "/sessions — switch session (title menu)",
            "/model — change model (composer pill)",
            "/settings — Orchestra settings",
            "Rewind: hover a user message → ↩ Rewind",
            "@file — mention files in composer",
          ].join("\n"),
        });
        break;
      case "/rewind":
        this.post({
          type: "systemNote",
          text: "Hover a user message and click ↩ Rewind to truncate history to that checkpoint.",
        });
        break;
      default:
        this.post({ type: "systemNote", text: `Unknown command: ${name}. Try /help` });
        break;
    }
  }

  private async searchMentions(query: string): Promise<ChatFileRef[]> {
    const q = query.trim().toLowerCase();
    const exclude = "**/{node_modules,.git,.orchestra,dist,build,out,vendor}/**";
    type Scored = ChatFileRef & { score: number };
    const byPath = new Map<string, Scored>();

    const consider = (uri: vscode.Uri) => {
      if (uri.scheme !== "file") {
        return;
      }
      const fp = uri.fsPath;
      const name = path.basename(fp).toLowerCase();
      const rel = vscode.workspace.asRelativePath(uri).replace(/\\/g, "/").toLowerCase();
      let score = 0;
      if (!q) {
        score = 10;
      } else if (name === q) {
        score = 100;
      } else if (name.startsWith(q)) {
        score = 80;
      } else if (name.includes(q)) {
        score = 60;
      } else if (rel.includes(q.replace(/\\/g, "/"))) {
        score = 40;
      } else {
        return;
      }
      const prev = byPath.get(fp);
      if (!prev || prev.score < score) {
        byPath.set(fp, { ...this.mentionFileRef(uri), score });
      }
    };

    for (const doc of vscode.workspace.textDocuments) {
      consider(doc.uri);
    }

    const scanLimit = q ? 250 : 60;
    const uris = await vscode.workspace.findFiles("**/*", exclude, scanLimit);
    for (const u of uris) {
      consider(u);
    }

    if (q && byPath.size < 8) {
      const globPart = q.replace(/[\\[\]{}()?+*^$|]/g, "[$&]");
      const extra = await vscode.workspace.findFiles(`**/*${globPart}*`, exclude, 80);
      for (const u of extra) {
        consider(u);
      }
    }

    return [...byPath.values()]
      .sort((a, b) => b.score - a.score || a.name.localeCompare(b.name))
      .slice(0, 20)
      .map(({ score: _score, ...ref }) => ref);
  }

  /** Lightweight file ref for @-mention palette — no staging, no webview root churn. */
  private mentionFileRef(uri: vscode.Uri): ChatFileRef {
    const name = path.basename(uri.fsPath);
    const ext = path.extname(name).replace(/^\./, "").toLowerCase();
    const kind = fileKindFromExt(ext);
    const ref: ChatFileRef = {
      name,
      path: uri.fsPath,
      ext: ext || undefined,
      kind,
    };
    const webview = this.webviewTarget();
    if (kind === "image" && webview && uri.scheme === "file" && this.isUnderWorkspace(uri.fsPath)) {
      try {
        ref.previewUri = webview.asWebviewUri(uri).toString();
      } catch {
        /* preview optional */
      }
    }
    return ref;
  }

  private getLocalResourceRoots(): vscode.Uri[] {
    const roots = [
      vscode.Uri.joinPath(this.extensionUri, "media"),
      vscode.Uri.joinPath(this.extensionUri, "images"),
    ];
    for (const folder of vscode.workspace.workspaceFolders ?? []) {
      roots.push(folder.uri);
    }
    return roots;
  }

  private allLocalResourceRoots(): vscode.Uri[] {
    return [...this.getLocalResourceRoots(), ...this.extraResourceRoots];
  }

  private registerAttachmentPath(filePath: string): void {
    if (!filePath) {
      return;
    }
    // Workspace paths are already covered by getLocalResourceRoots() — updating
    // webview.options per file reloads the chat webview (breaks @-mention input).
    if (this.isUnderWorkspace(filePath)) {
      return;
    }
    const dirUri = vscode.Uri.file(path.dirname(filePath));
    const dirKey = dirUri.fsPath.replace(/\\/g, "/").toLowerCase();
    if (
      this.extraResourceRoots.some(
        (r) => r.fsPath.replace(/\\/g, "/").toLowerCase() === dirKey
      )
    ) {
      return;
    }
    this.extraResourceRoots.push(dirUri);
    this.scheduleWebviewRootsUpdate();
  }

  private scheduleWebviewRootsUpdate(): void {
    if (this.rootsUpdateTimer) {
      clearTimeout(this.rootsUpdateTimer);
    }
    this.rootsUpdateTimer = setTimeout(() => {
      this.rootsUpdateTimer = undefined;
      const webview = this.webviewTarget();
      if (webview) {
        webview.options = {
          ...webview.options,
          localResourceRoots: this.allLocalResourceRoots(),
        };
      }
    }, 300);
  }

  private workspaceRoot(): string | undefined {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  }

  private isUnderWorkspace(filePath: string): boolean {
    const root = this.workspaceRoot();
    if (!root) {
      return false;
    }
    const norm = (p: string) => path.normalize(p).replace(/\\/g, "/").toLowerCase();
    const r = norm(root).replace(/\/$/, "");
    const f = norm(filePath);
    return f === r || f.startsWith(r + "/");
  }

  private async ensureAttachmentInWorkspace(
    srcPath: string,
    preferredName?: string
  ): Promise<string> {
    if (this.isUnderWorkspace(srcPath)) {
      const stat = await fs.stat(srcPath);
      if (stat.size > MAX_ATTACHMENT_BYTES) {
        throw new Error(
          `Attachment exceeds ${MAX_ATTACHMENT_BYTES / (1024 * 1024)} MB limit`
        );
      }
      return srcPath;
    }
    const root = this.workspaceRoot();
    if (!root) {
      throw new Error("Open a workspace folder to attach external files");
    }
    const stat = await fs.stat(srcPath);
    if (stat.size > MAX_ATTACHMENT_BYTES) {
      throw new Error(
        `Attachment exceeds ${MAX_ATTACHMENT_BYTES / (1024 * 1024)} MB limit`
      );
    }
    const baseName = preferredName || path.basename(srcPath);
    const safeBase = baseName.replace(/[^\w.\-()+ ]+/g, "_").replace(/\s+/g, "-");
    const dir = path.join(root, ".orchestra", "attachments");
    await fs.mkdir(dir, { recursive: true });
    const dest = path.join(dir, `${Date.now()}-${safeBase}`);
    await fs.copyFile(srcPath, dest);
    return dest;
  }

  private async prepareAttachmentForSend(f: ChatFileRef): Promise<ChatFileRef> {
    if (!f.path) {
      return f;
    }
    const wsPath = await this.ensureAttachmentInWorkspace(f.path, f.name);
    return wsPath === f.path ? f : { ...f, path: wsPath };
  }

  private async enrichFileRef(f: ChatFileRef): Promise<ChatFileRef> {
    if (!f.path) {
      return f;
    }
    this.registerAttachmentPath(f.path);
    if (f.kind !== "image" || f.previewUri) {
      return f;
    }
    const uri = vscode.Uri.file(f.path);
    const ext =
      f.ext || path.extname(f.path).replace(/^\./, "").toLowerCase();
    const previewUri = await this.imagePreviewUri(uri, ext);
    return previewUri ? { ...f, previewUri } : f;
  }

  private async toFileRef(uri: vscode.Uri): Promise<ChatFileRef> {
    const wsPath = await this.ensureAttachmentInWorkspace(uri.fsPath);
    uri = vscode.Uri.file(wsPath);
    this.registerAttachmentPath(uri.fsPath);
    const name = path.basename(uri.fsPath);
    const ext = path.extname(name).replace(/^\./, "").toLowerCase();
    const kind = fileKindFromExt(ext);
    let previewUri: string | undefined;
    if (kind === "image") {
      previewUri = await this.imagePreviewUri(uri, ext);
    }
    return {
      name,
      path: uri.fsPath,
      ext: ext || undefined,
      kind,
      previewUri,
    };
  }

  private async imagePreviewUri(uri: vscode.Uri, ext: string): Promise<string | undefined> {
    const webview = this.webviewTarget();
    if (!webview || uri.scheme !== "file") {
      return undefined;
    }
    if (this.isUnderLocalResourceRoots(uri)) {
      return webview.asWebviewUri(uri).toString();
    }
    try {
      const stat = await fs.stat(uri.fsPath);
      if (stat.size > 15 * 1024 * 1024) {
        return webview.asWebviewUri(uri).toString();
      }
      const data = await fs.readFile(uri.fsPath);
      const mime = imageMimeFromExt(ext);
      return `data:${mime};base64,${Buffer.from(data).toString("base64")}`;
    } catch {
      try {
        return webview.asWebviewUri(uri).toString();
      } catch {
        return undefined;
      }
    }
  }

  private isUnderLocalResourceRoots(uri: vscode.Uri): boolean {
    if (uri.scheme !== "file") {
      return false;
    }
    const fp = uri.fsPath.replace(/\\/g, "/").toLowerCase();
    for (const root of this.allLocalResourceRoots()) {
      if (root.scheme !== "file") {
        continue;
      }
      const rp = root.fsPath.replace(/\\/g, "/").toLowerCase().replace(/\/$/, "");
      if (fp === rp || fp.startsWith(rp + "/")) {
        return true;
      }
    }
    return false;
  }

  /** Editor column for previews — never the Orchestra webview column. */
  private editorViewColumn(): vscode.ViewColumn {
    const chatCol = this.panel?.viewColumn;
    if (chatCol === vscode.ViewColumn.One) {
      return vscode.ViewColumn.Beside;
    }
    return vscode.ViewColumn.One;
  }

  /** Open a workspace file in the main editor area (not inside the chat webview). */
  private async openInWorkspaceEditor(
    uri: vscode.Uri,
    opts?: { preview?: boolean; focus?: boolean }
  ): Promise<void> {
    const preview = opts?.preview === true;
    const preserveFocus = opts?.focus !== true;
    const column = this.editorViewColumn();
    try {
      await vscode.commands.executeCommand("vscode.open", uri, {
        viewColumn: column,
        preserveFocus,
        preview,
      });
    } catch {
      try {
        await vscode.window.showTextDocument(uri, {
          viewColumn: column,
          preserveFocus,
          preview,
        });
      } catch {
        void vscode.window.showErrorMessage(`Orchestra: cannot open ${uri.fsPath}`);
      }
    }
  }

  private resolveWorkspaceFilePath(filePath: string): vscode.Uri | undefined {
    const trimmed = filePath.trim();
    if (!trimmed) {
      return undefined;
    }
    // On Windows path.isAbsolute("/foo") is true (drive-relative) and would map
    // to the current drive root — treat only fully-qualified paths as absolute.
    const fullyQualified =
      process.platform === "win32"
        ? /^[a-zA-Z]:[\\/]/.test(trimmed) || trimmed.startsWith("\\\\")
        : path.isAbsolute(trimmed);
    if (fullyQualified) {
      return vscode.Uri.file(trimmed);
    }
    const root = this.workspaceRoot();
    if (!root) {
      return undefined;
    }
    const rel = trimmed.replace(/^[\\/]+/, "");
    return vscode.Uri.file(path.join(root, rel.split("/").join(path.sep)));
  }

  /** Open the real workspace file with Cursor-style inline change highlights. */
  private async openFileWithChangeHighlights(
    filePath: string,
    before: string,
    after: string,
    focus = true
  ): Promise<void> {
    const uri = this.resolveWorkspaceFilePath(filePath);
    if (!uri) {
      void vscode.window.showErrorMessage("Orchestra: cannot resolve file path");
      return;
    }
    try {
      const doc = await vscode.workspace.openTextDocument(uri);
      const editor = await vscode.window.showTextDocument(doc, {
        viewColumn: this.editorViewColumn(),
        preserveFocus: !focus,
        preview: false,
      });
      this.pendingHighlights.apply(editor, before, after);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      void vscode.window.showErrorMessage(`Orchestra: cannot open ${filePath}: ${message}`);
    }
  }

  private async openSideBySideDiff(
    filePath: string,
    before: string,
    after: string,
    focus = false
  ): Promise<void> {
    const root = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    if (!root) {
      void vscode.window.showErrorMessage("Orchestra: no workspace folder for diff");
      return;
    }
    const diffDir = path.join(root, ".orchestra", "diff-preview");
    try {
      await fs.mkdir(diffDir, { recursive: true });
      const hash = crypto.createHash("sha256").update(filePath).digest("hex").slice(0, 12);
      const base = path.basename(filePath) || "file";
      const leftPath = path.join(diffDir, `${hash}.before.${base}`);
      const rightPath = path.join(diffDir, `${hash}.after.${base}`);
      await fs.writeFile(leftPath, before, "utf8");
      await fs.writeFile(rightPath, after, "utf8");
      const title = `${base} (Orchestra)`;
      await vscode.commands.executeCommand(
        "vscode.diff",
        vscode.Uri.file(leftPath),
        vscode.Uri.file(rightPath),
        title,
        { viewColumn: this.editorViewColumn(), preserveFocus: !focus }
      );
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      void vscode.window.showErrorMessage(`Orchestra diff: ${message}`);
    }
  }

  private webviewTarget(): vscode.Webview | undefined {
    return this.sidebar?.webview ?? this.panel?.webview;
  }

  private post(msg: HostToWebview): void {
    // Track pending-changes state so it can be re-delivered after a webview reload.
    if (msg.type === "pendingOps") {
      this.lastPendingOps = msg.payload;
    } else if (msg.type === "pendingCleared") {
      this.lastPendingOps = undefined;
      // Applied/discarded — inline highlights no longer describe reality.
      this.pendingHighlights.clearAll();
    }
    void this.webviewTarget()?.postMessage(msg);
  }

  private async showPermissionDialog(request: PermissionRequestPayload): Promise<void> {
    const isLSP =
      request.kind === "lsp.install" || request.tool === "lsp.install";
    const tool = request.tool || "tool";
    const detail = [request.description, request.reason].filter(Boolean).join("\n");
    let pick: string | undefined;
    if (isLSP) {
      pick = await vscode.window.showWarningMessage(
        "Orchestra: Install language server?",
        { modal: true, detail: detail || undefined },
        "Install",
        "Install always",
        "Skip"
      );
      this.session.resolvePermission({
        approved: pick === "Install" || pick === "Install always",
        always: pick === "Install always",
      });
      return;
    }
    pick = await vscode.window.showWarningMessage(
      `Orchestra permission: ${tool}`,
      { modal: true, detail: detail || undefined },
      "Allow",
      "Allow always",
      "Deny"
    );
    this.session.resolvePermission({
      approved: pick === "Allow" || pick === "Allow always",
      always: pick === "Allow always",
    });
  }

  private async showQuestionDialog(questions: QuestionItemPayload[]): Promise<void> {
    const answers: string[] = [];
    for (const q of questions) {
      if (q.options && q.options.length > 0) {
        const pick = await vscode.window.showQuickPick(q.options, {
          title: q.question,
          canPickMany: q.allow_multiple === true,
        });
        if (q.allow_multiple && Array.isArray(pick)) {
          answers.push(pick.join(", "));
        } else if (typeof pick === "string") {
          answers.push(pick);
        } else {
          answers.push("");
        }
      } else {
        const input = await vscode.window.showInputBox({
          title: q.question,
          prompt: q.question,
        });
        answers.push(input || "");
      }
    }
    this.session.resolveQuestion(answers);
  }

  private getHtml(webview: vscode.Webview): string {
    // Cache-bust so CSS/JS changes show up after compile without stale webview cache.
    const v = String(Date.now());
    const styleUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "media", "chat.css")
    ).with({ query: `v=${v}` });
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "media", "chat.bundle.js")
    ).with({ query: `v=${v}` });
    const logoUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "media", "logo.png")
    ).with({ query: `v=${v}` });
    const nonce = getNonce();
    const csp = [
      `default-src 'none'`,
      `style-src ${webview.cspSource}`,
      `script-src 'nonce-${nonce}'`,
      `img-src ${webview.cspSource} data:`,
    ].join("; ");

    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="${styleUri}" />
  <title>Orchestra</title>
</head>
<body>
  <div id="app" data-mode="agent">
    <header id="chrome-strip">
      <div id="top-accent" aria-hidden="true"></div>
      <div id="chrome-inner">
        <div id="chrome-brand" class="chrome-brand" title="Orchestra" aria-hidden="true">
          <img src="${logoUri}" alt="" width="22" height="22" class="chrome-logo" />
        </div>
        <div id="session-tabs" class="session-tabs" role="tablist" aria-label="Chat sessions"></div>
        <div class="chrome-actions">
          <button type="button" id="session-new-btn" class="chrome-action" title="New chat" aria-label="New chat">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/>
            </svg>
          </button>
          <button type="button" id="session-history-btn" class="chrome-action" title="All sessions" aria-label="All sessions">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="2"/>
              <path d="M12 7.5v4.5l3 2" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </button>
          <span id="chrome-hint" class="chrome-hint hidden" aria-live="polite"></span>
          <button type="button" id="settings-btn" class="chrome-action chrome-icon" title="Settings" aria-label="Settings">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M12 15.5a3.5 3.5 0 100-7 3.5 3.5 0 000 7z" stroke="currentColor" stroke-width="2"/>
              <path d="M19.4 13a7.8 7.8 0 000-2l2-1.2-2-3.5-2.3 1a7.9 7.9 0 00-1.7-1L15 3h-4l-.4 2.3a7.9 7.9 0 00-1.7 1l-2.3-1-2 3.5 2 1.2a7.8 7.8 0 000 2l-2 1.2 2 3.5 2.3-1a7.9 7.9 0 001.7 1L11 21h4l.4-2.3a7.9 7.9 0 001.7-1l2.3 1 2-3.5-2-1.2z" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>
            </svg>
          </button>
        </div>
      </div>
    </header>
    <div id="session-menu" class="menu top-menu" role="menu">
      <button type="button" class="menu-item menu-item-action" data-session-action="new">New chat</button>
      <div id="session-menu-list"></div>
    </div>
    <div id="subagents-bar" class="subagents-bar hidden">
      <div class="subagents-head">Subagents</div>
      <div id="subagents-tree" class="subagents-tree"></div>
    </div>
    <div id="workflow-bar" class="workflow-bar hidden">
      <div class="workflow-top">
        <span class="workflow-spin"></span>
        <span id="workflow-label" class="workflow-name"></span>
      </div>
      <div id="workflow-stages" class="workflow-stages"></div>
    </div>
    <div id="messages"></div>
    <div id="pending-bar" class="pending-bar hidden">
      <div class="pending-card">
        <div class="pending-head">
          <span id="pending-label" class="pending-label">0 pending changes</span>
          <div class="pending-head-actions">
            <button type="button" id="pending-apply-btn" class="pending-icon-btn pending-apply-btn" title="Apply changes" aria-label="Apply">✓</button>
            <button type="button" id="pending-reject-btn" class="pending-icon-btn pending-reject-btn" title="Discard changes" aria-label="Discard">✗</button>
          </div>
        </div>
        <div id="pending-review-list" class="pending-review-list"></div>
      </div>
    </div>
    <div id="diff-viewer" class="diff-viewer hidden" role="dialog" aria-modal="true">
      <div class="diff-viewer-card">
        <div class="diff-viewer-head">
          <span id="diff-viewer-title" class="diff-viewer-title"></span>
          <div class="diff-viewer-actions">
            <button type="button" id="diff-viewer-editor-btn" class="pill small">Open in editor</button>
            <button type="button" id="diff-viewer-close-btn" class="pill small">Close</button>
          </div>
        </div>
        <div class="diff-viewer-panes">
          <div class="diff-pane">
            <div class="diff-pane-label">Before</div>
            <div id="diff-pane-before" class="diff-pane-body"></div>
          </div>
          <div class="diff-pane">
            <div class="diff-pane-label">After</div>
            <div id="diff-pane-after" class="diff-pane-body"></div>
          </div>
        </div>
      </div>
    </div>
    <div id="image-preview" class="image-preview hidden" role="dialog" aria-modal="true">
      <div class="image-preview-card">
        <div class="image-preview-head">
          <span id="image-preview-title" class="image-preview-title"></span>
          <div class="image-preview-actions">
            <button type="button" id="image-preview-prev-btn" class="pill small" aria-label="Previous image">‹</button>
            <button type="button" id="image-preview-next-btn" class="pill small" aria-label="Next image">›</button>
            <span id="image-preview-counter" class="image-preview-counter"></span>
            <button type="button" id="image-preview-open-btn" class="pill small">Open file</button>
            <button type="button" id="image-preview-close-btn" class="pill small">Close</button>
          </div>
        </div>
        <div class="image-preview-body">
          <img id="image-preview-img" alt="" />
        </div>
      </div>
    </div>
    <div id="overlay" class="overlay hidden" role="dialog" aria-modal="true">
      <div class="overlay-card">
        <h2 id="overlay-title"></h2>
        <p id="overlay-body" class="overlay-body"></p>
        <div id="overlay-options" class="overlay-options"></div>
        <input id="overlay-input" type="text" class="overlay-input hidden" />
        <div id="overlay-actions" class="overlay-actions"></div>
      </div>
    </div>
    <div id="composer-wrap">
      <div id="todos-bar" class="todos-bar hidden">
        <button type="button" id="todos-chip" class="todos-chip" aria-expanded="false" aria-label="Task checklist">
          <span id="todos-chip-glyph" class="todos-chip-glyph" aria-hidden="true">□</span>
          <span id="todos-chip-summary" class="todos-chip-summary"></span>
          <span id="todos-chip-chev" class="todos-chip-chev" aria-hidden="true">▾</span>
        </button>
        <div id="todos-list" class="todos-list hidden" role="list"></div>
      </div>
      <div id="mode-menu" class="menu" role="menu"></div>
      <div id="effort-menu" class="menu effort" role="menu"></div>
      <div id="access-menu" class="menu access" role="menu"></div>
      <div id="model-menu" class="menu model-pop" role="menu">
        <div class="menu-section" id="model-menu-title">Models</div>
        <input id="model-menu-search" type="search" class="model-menu-search" placeholder="Search models…" autocomplete="off" />
        <div id="model-menu-list" class="model-list">
          <button type="button" class="menu-item" data-model-action="refresh">Refresh list</button>
        </div>
      </div>
      <div id="palette-menu" class="menu palette-menu hidden" role="listbox"></div>
      <div id="composer">
        <div id="chip-files" class="chip-files"></div>
        <div id="message-queue" class="message-queue hidden" aria-label="Queued messages"></div>
        <div id="composer-status" class="composer-status hidden" aria-live="polite" aria-busy="false">
          <span class="composer-status-spinner" aria-hidden="true"></span>
          <span id="composer-status-label">Working…</span>
        </div>
        <textarea id="input" rows="2" placeholder="Message, @ for files, / for commands…"></textarea>
        <div id="toolbar">
          <div id="toolbar-left">
            <button type="button" class="pill" id="mode-btn" aria-haspopup="menu">
              <span class="ico mode-icon mode-agent" id="mode-icon">∞</span>
              <span id="mode-label">Agent</span>
              <span class="chev">▾</span>
            </button>
            <button type="button" class="pill" id="effort-btn" data-effort="medium" aria-haspopup="menu" title="Medium">
              <span class="ico effort-icon effort-medium" id="effort-icon"><span class="effort-meter effort-medium"><i></i><i></i></span></span>
              <span id="effort-label">Medium</span>
              <span id="effort-fast-mark" class="effort-fast-mark" hidden aria-hidden="true">⚡</span>
              <span class="chev">▾</span>
            </button>
            <button type="button" class="pill" id="access-btn" data-access="ask" aria-haspopup="menu" title="Ask — shell с подтверждением; правки через Accept/Reject">
              <span class="ico access-icon access-ask" id="access-icon">◌</span>
              <span id="access-label">Ask</span>
              <span class="chev">▾</span>
            </button>
            <button type="button" class="pill" id="orch-config-btn" hidden title="Orchestra roles & tiers">
              <span class="ico">◎</span>
              <span>Orchestra</span>
            </button>
            <button type="button" class="pill" id="model-pill" aria-haspopup="menu" title="Model">
              <span id="model-label">Model</span>
              <span class="chev">▾</span>
            </button>
          </div>
          <div id="toolbar-right">
            <div id="status-footer" class="status-footer">
              <span id="status-lsp"></span>
            </div>
            <div id="context-wrap" class="context-wrap">
              <button type="button" id="context-btn" class="context-btn" aria-label="Context usage">
                <span class="context-ring-fill" id="context-ring-fill"></span>
              </button>
              <div id="context-popover" class="context-popover hidden" role="dialog" aria-label="Context usage">
                <div class="ctx-head">
                  <span class="ctx-title">Context</span>
                  <span id="ctx-pct" class="ctx-pct"></span>
                </div>
                <div id="ctx-summary" class="ctx-summary"></div>
                <div id="ctx-bar" class="ctx-bar"></div>
                <div id="ctx-rows" class="ctx-rows"></div>
                <p class="ctx-note">Estimate from last LLM step · full breakdown later</p>
              </div>
            </div>
            <button type="button" class="icon-btn" id="attach-btn" title="Attach files" aria-label="Attach files">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M9.5 3.5l-4.2 4.2a2.1 2.1 0 003 3L13 6a3.5 3.5 0 10-5-5L3.2 5.8a4.8 4.8 0 106.8 6.8L13 9.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/>
              </svg>
            </button>
            <button type="button" id="send" title="Send" aria-label="Send">
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M8 12.5V3.5M8 3.5L4 7.5M8 3.5l4 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}

function getNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}

function parseJSONSafe(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return undefined;
  }
}

function parsePendingOps(data: unknown): PendingOpsPayload | undefined {
  if (typeof data === "string") {
    data = parseJSONSafe(data);
  }
  if (!data || typeof data !== "object") {
    return undefined;
  }
  const d = data as Record<string, unknown>;
  const diffRaw = Array.isArray(d.diff) ? d.diff : [];
  const diff = diffRaw
    .map((item) => {
      if (!item || typeof item !== "object") {
        return undefined;
      }
      const x = item as Record<string, unknown>;
      const path = typeof x.path === "string" ? x.path : "";
      if (!path) {
        return undefined;
      }
      return {
        path,
        before: typeof x.before === "string" ? x.before : undefined,
        after: typeof x.after === "string" ? x.after : undefined,
      };
    })
    .filter((x): x is NonNullable<typeof x> => x !== undefined);
  const ops = Array.isArray(d.ops) ? d.ops : [];
  return {
    ops,
    diff,
    applied: d.applied === true,
  };
}

function parseTodosUpdated(content: string | undefined): TodoItemPayload[] {
  const raw = (content || "").trim();
  if (!raw) {
    return [];
  }
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) {
      return [];
    }
    const out: TodoItemPayload[] = [];
    for (const item of parsed) {
      if (!item || typeof item !== "object") {
        continue;
      }
      const t = item as Record<string, unknown>;
      const id = typeof t.id === "string" ? t.id : "";
      const text = typeof t.content === "string" ? t.content : "";
      const status = typeof t.status === "string" ? t.status : "pending";
      if (id && text) {
        out.push({ id, content: text, status });
      }
    }
    return out;
  } catch {
    return [];
  }
}

/** Hide @path refs and agent final JSON from chat bubbles. */
function uiDisplayText(raw: string): string {
  let t = sanitizeAssistantStream(stripFinalEnvelope(raw)).trim();
  if (!t) {
    return "";
  }
  if (/^(@\S+\s*)+$/.test(t)) {
    return "";
  }
  return t.replace(/\n\n(@\S+\s*)+$/, "").trim();
}

function attachmentRefsFromText(raw: string): ChatFileRef[] {
  const t = raw.trim();
  if (!t) {
    return [];
  }
  let block = "";
  if (/^(@\S+\s*)+$/.test(t)) {
    block = t;
  } else {
    const suffix = t.match(/\n\n((@\S+\s*)+)$/);
    block = suffix?.[1] ?? "";
  }
  if (!block) {
    return [];
  }
  const out: ChatFileRef[] = [];
  for (const m of block.matchAll(/@(\S+)/g)) {
    const fp = m[1];
    if (!fp) {
      continue;
    }
    const name = path.basename(fp);
    const ext = path.extname(name).replace(/^\./, "").toLowerCase();
    out.push({
      name,
      path: fp,
      ext: ext || undefined,
      kind: fileKindFromExt(ext),
    });
  }
  return out;
}

function fileKindFromExt(ext: string): ChatFileKind {
  const e = ext.toLowerCase();
  if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "avif"].includes(e)) {
    return "image";
  }
  if (
    [
      "ts",
      "tsx",
      "js",
      "jsx",
      "go",
      "py",
      "rs",
      "java",
      "c",
      "cpp",
      "h",
      "hpp",
      "cs",
      "json",
      "yaml",
      "yml",
      "md",
      "toml",
      "sql",
      "sh",
      "ps1",
      "vue",
      "svelte",
    ].includes(e)
  ) {
    return "code";
  }
  if (["txt", "log", "csv", "xml", "html", "css", "scss", "less"].includes(e)) {
    return "text";
  }
  return "binary";
}

function imageMimeFromExt(ext: string): string {
  switch (ext.toLowerCase()) {
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "gif":
      return "image/gif";
    case "webp":
      return "image/webp";
    case "bmp":
      return "image/bmp";
    case "ico":
      return "image/x-icon";
    case "avif":
      return "image/avif";
    case "svg":
      return "image/svg+xml";
    default:
      return "image/png";
  }
}

function parseStepUsage(data: unknown): StepUsagePayload | undefined {
  if (!data || typeof data !== "object") {
    return undefined;
  }
  const d = data as Record<string, unknown>;
  return {
    prompt_tokens: typeof d.prompt_tokens === "number" ? d.prompt_tokens : undefined,
    completion_tokens: typeof d.completion_tokens === "number" ? d.completion_tokens : undefined,
    total_tokens: typeof d.total_tokens === "number" ? d.total_tokens : undefined,
    cost_usd: typeof d.cost_usd === "number" ? d.cost_usd : undefined,
    source: typeof d.source === "string" ? d.source : undefined,
  };
}
