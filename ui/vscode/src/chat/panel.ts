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

/** Matches core `attachments.MaxImageBytes` (20 MB). */
const MAX_ATTACHMENT_BYTES = 20 * 1024 * 1024;

export class ChatPanel implements vscode.Disposable {
  public static readonly viewType = "orchestra.chat";

  private panel: vscode.WebviewPanel | undefined;
  private view: "chat" | "settings" = "chat";
  private readonly session: CoreSession;
  private readonly extensionUri: vscode.Uri;
  private readonly settings: SettingsView;
  private readonly disposables: vscode.Disposable[] = [];
  /** Parent dirs of chat attachments outside workspace — added to webview localResourceRoots. */
  private readonly extraResourceRoots: vscode.Uri[] = [];
  private rootsUpdateTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(session: CoreSession, extensionUri: vscode.Uri) {
    this.session = session;
    this.extensionUri = extensionUri;
    this.settings = new SettingsView(session, extensionUri);
    this.settings.bindPost((msg) => {
      void this.panel?.webview.postMessage(msg);
    });

    const onAgent = (event: AgentEventParams): void => {
      if (this.view !== "chat") {
        return;
      }
      this.forwardAgentEvent(event);
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
      this.post({ type: "permissionRequest", request });
    };
    const onQuestion = (questions: QuestionItemPayload[]): void => {
      if (this.view !== "chat") {
        void this.showQuestionDialog(questions);
        return;
      }
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
  }

  dispose(): void {
    if (this.rootsUpdateTimer) {
      clearTimeout(this.rootsUpdateTimer);
      this.rootsUpdateTimer = undefined;
    }
    this.panel?.dispose();
    for (const d of this.disposables) {
      d.dispose();
    }
  }

  async show(): Promise<void> {
    await this.ensurePanel();
    await this.showChat();
  }

  async showSettings(): Promise<void> {
    await this.ensurePanel();
    this.view = "settings";
    if (this.panel) {
      this.panel.title = "Orchestra Settings";
      this.panel.webview.html = this.settings.getHtml(this.panel.webview);
      this.panel.reveal(vscode.ViewColumn.Beside);
    }
    // settings.js posts ready → pushState
  }

  private async showChat(): Promise<void> {
    this.view = "chat";
    if (!this.panel) {
      return;
    }
    this.panel.title = "Orchestra";
    this.panel.webview.html = this.getHtml(this.panel.webview);
    this.panel.reveal(vscode.ViewColumn.Beside);

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
        contextLimit: 128000,
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
    }> = [];
    for (let idx = 0; idx < view.uiMessages.length; idx++) {
      const m = view.uiMessages[idx] as {
        role?: string;
        text?: string;
        content?: string;
        attachments?: Array<{ path?: string; name?: string; kind?: string; ext?: string; mime?: string }>;
      };
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
      if (text.length > 0 || (files && files.length > 0)) {
        history.push({
          role,
          text,
          uiIndex: role === "user" ? idx : undefined,
          files: files?.length ? files : undefined,
        });
      }
    }
    if (history.length > 0) {
      this.post({ type: "history", messages: history });
    }
    await this.refreshSessionTabs();
  }

  private sortSessionsByRecent(sessions: SessionMeta[]): SessionMeta[] {
    return [...sessions].sort((a, b) => {
      const ta = a.updated_at || "";
      const tb = b.updated_at || "";
      return tb.localeCompare(ta);
    });
  }

  private async refreshSessionTabs(): Promise<void> {
    const activeId = this.session.getSessionId();
    if (!activeId) {
      return;
    }
    const sessions = this.sortSessionsByRecent(await this.session.listSessions());
    const tabs = sessions.slice(0, 16).map((s) => ({
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
          const sessions = await this.session.listSessions();
          this.post({
            type: "sessionList",
            sessions: sessions.map((s) => ({
              id: s.id,
              title: s.title || "New chat",
              model: s.model,
              msg_count: s.msg_count,
            })),
          });
          return;
        }
        case "newSession": {
          await this.session.startSession({ forceNew: true });
          await this.refreshHeaderAndHistory();
          return;
        }
        case "openSession": {
          await this.session.startSession({ sessionId: msg.sessionId });
          await this.refreshHeaderAndHistory();
          return;
        }
        case "closeSession": {
          const sid = msg.sessionId.trim();
          if (!sid) {
            return;
          }
          const active = this.session.getSessionId();
          await this.session.closeSession(sid);
          if (sid === active) {
            const remaining = this.sortSessionsByRecent(await this.session.listSessions());
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
              .filter((p) => p.configured || p.active)
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
          await this.showSettings();
          return;
        }
        case "send": {
          const userText = msg.text.trim();
          const hasFiles = Boolean(msg.files && msg.files.length > 0);
          if (!userText && !hasFiles) {
            return;
          }
          const prepared = hasFiles
            ? await Promise.all(msg.files!.map((f) => this.prepareAttachmentForSend(f)))
            : undefined;
          const echoFiles = prepared
            ? await Promise.all(prepared.map((f) => this.enrichFileRef(f)))
            : undefined;
          this.post({
            type: "userEcho",
            text: userText,
            files: echoFiles,
          });
          this.post({ type: "status", status: "running" });
          try {
            await this.session.maybeSetSessionTitle(
              userText ||
                msg.files?.map((f) => f.name).filter(Boolean).join(", ") ||
                "Attachment"
            );
            const apply = msg.apply === true;
            await this.session.sendMessage(userText, {
              apply,
              mode: msg.mode,
              profile: msg.profile,
              attachments: prepared,
            });
            if (apply) {
              this.post({ type: "pendingCleared" });
            }
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
            this.post({ type: "turnComplete", ok: true });
            this.post({ type: "status", status: "ready" });
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
            this.post({ type: "turnComplete", ok: false });
            this.post({ type: "status", status: "error", detail: message });
          }
          return;
        }
        case "applyPending": {
          try {
            const ops = Array.isArray(msg.ops) ? msg.ops : undefined;
            if (ops && ops.length > 0) {
              await this.session.applyOps(ops, true);
              void vscode.window.showInformationMessage("Orchestra: changes applied");
              this.post({ type: "pendingCleared" });
            } else {
              const res = await this.session.applyPending(true);
              if (res.applied) {
                void vscode.window.showInformationMessage("Orchestra: changes applied");
                this.post({ type: "pendingCleared" });
              } else {
                void vscode.window.showWarningMessage("Orchestra: no pending changes to apply");
              }
            }
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
          }
          return;
        }
        case "discardPending": {
          this.post({ type: "pendingCleared" });
          return;
        }
        case "permissionReply": {
          this.session.resolvePermission({
            approved: Boolean(msg.approved),
            always: Boolean(msg.always),
          });
          return;
        }
        case "questionReply": {
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
            const res = await this.session.rewindSession(msg.uiIndex);
            void vscode.window.showInformationMessage(
              `Orchestra: rewound to message (${res.uiMessages} UI msgs)`
            );
            await this.refreshHeaderAndHistory();
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.post({ type: "error", message });
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
          await this.openSideBySideDiff(
            fp,
            msg.before ?? "",
            msg.after ?? "",
            msg.focus === true
          );
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

  private forwardAgentEvent(event: AgentEventParams): void {
    const childCtx = {
      scope: event.scope,
      taskId: event.task_id,
      parentToolCallId: event.parent_tool_call_id,
      subagentType: event.subagent_type,
    };
    switch (event.type) {
      case "child_started":
        this.post({
          type: "childLifecycle",
          phase: "started",
          taskId: event.task_id || "",
          parentToolCallId: event.parent_tool_call_id,
          subagentType: event.subagent_type,
          content: event.content,
        });
        break;
      case "child_done":
        this.post({
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
          this.post({ type: "delta", content: event.content });
        }
        break;
      case "reasoning_delta":
        if (event.content && event.scope !== "child") {
          this.post({ type: "delta", content: event.content });
        }
        break;
      case "tool_call_start":
        this.post({
          type: "toolBlock",
          phase: "start",
          toolCallId: event.tool_call_id,
          toolName: event.tool_call_name || "tool",
          step: event.step,
          ...childCtx,
        });
        break;
      case "tool_call_delta":
        if (event.args_delta) {
          this.post({
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
        this.post({
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
          this.post({ type: "todosUpdate", todos });
        }
        break;
      }
      case "step_usage": {
        const usage = parseStepUsage(event.data);
        if (usage) {
          this.post({ type: "stepUsage", usage });
        }
        break;
      }
      case "recoverable_error":
      case "error":
        if (event.content) {
          this.post({ type: "error", message: event.content });
        }
        break;
      case "pending_ops": {
        const payload = parsePendingOps(event.data);
        if (payload && !payload.applied) {
          this.post({ type: "pendingOps", payload });
        } else if (payload?.applied) {
          this.post({ type: "pendingCleared" });
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

    if (!q) {
      const seen = new Set<string>();
      const out: ChatFileRef[] = [];
      for (const doc of vscode.workspace.textDocuments) {
        if (doc.uri.scheme !== "file") {
          continue;
        }
        const fp = doc.uri.fsPath;
        if (seen.has(fp)) {
          continue;
        }
        seen.add(fp);
        out.push(this.mentionFileRef(doc.uri));
        if (out.length >= 14) {
          break;
        }
      }
      if (out.length < 20) {
        const uris = await vscode.workspace.findFiles("**/*", exclude, 30);
        for (const u of uris) {
          if (seen.has(u.fsPath)) {
            continue;
          }
          seen.add(u.fsPath);
          out.push(this.mentionFileRef(u));
          if (out.length >= 20) {
            break;
          }
        }
      }
      return out;
    }

    const pattern = q.includes("/") || q.includes("\\") ? `**/*${q}*` : `**/*${q}*`;
    const uris = await vscode.workspace.findFiles(pattern, exclude, 25);
    const out: ChatFileRef[] = [];
    for (const u of uris) {
      out.push(this.mentionFileRef(u));
    }
    return out;
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
    const webview = this.panel?.webview;
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
    const roots = [vscode.Uri.joinPath(this.extensionUri, "media")];
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
      if (this.panel?.webview) {
        this.panel.webview.options = {
          ...this.panel.webview.options,
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
    const webview = this.panel?.webview;
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

  private post(msg: HostToWebview): void {
    void this.panel?.webview.postMessage(msg);
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
      vscode.Uri.joinPath(this.extensionUri, "media", "chat.js")
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
  <div id="app">
    <header id="chrome-strip">
      <div id="top-accent" aria-hidden="true"></div>
      <div id="chrome-inner">
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
      <div class="menu-section">Recent</div>
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
    <div id="todos-bar" class="todos-bar hidden">
      <div class="todos-head">
        <span id="todos-title" class="todos-title">Tasks</span>
        <span id="todos-progress" class="todos-progress"></span>
      </div>
      <div id="todos-list" class="todos-list"></div>
    </div>
    <div id="messages"></div>
    <div id="pending-bar" class="pending-bar hidden">
      <div class="pending-summary">
        <span id="pending-label">0 pending changes</span>
        <span id="pending-review-hint" class="pending-review-hint hidden">↑↓ · a accept · x reject · Enter apply</span>
        <div class="pending-actions">
          <button type="button" id="pending-diff-btn" class="pill small">Review</button>
          <button type="button" id="pending-discard-btn" class="pill small">Discard</button>
          <button type="button" id="pending-apply-btn" class="pill small primary">Apply</button>
        </div>
      </div>
      <div id="pending-files" class="pending-files"></div>
      <div id="pending-diff" class="pending-diff hidden"></div>
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
      <div id="mode-menu" class="menu" role="menu">
        <button type="button" class="menu-item selected" data-id="agent"><span class="mi">∞</span>Agent</button>
        <button type="button" class="menu-item" data-id="plan"><span class="mi">≡</span>Plan</button>
        <button type="button" class="menu-item" data-id="debug"><span class="mi">⌁</span>Debug</button>
        <button type="button" class="menu-item" data-id="multitask"><span class="mi">◎</span>Multitask</button>
        <button type="button" class="menu-item" data-id="ask"><span class="mi">◇</span>Ask</button>
      </div>
      <div id="effort-menu" class="menu effort" role="menu">
        <div class="menu-section">Effort</div>
        <button type="button" class="menu-item" data-effort="low">Low</button>
        <button type="button" class="menu-item selected" data-effort="medium">Medium</button>
        <button type="button" class="menu-item" data-effort="high">High</button>
        <div class="menu-section">Options</div>
        <div class="menu-row">Fast <button type="button" id="fast-toggle" class="toggle" role="switch" aria-checked="false"></button></div>
      </div>
      <div id="model-menu" class="menu model-pop" role="menu">
        <div class="menu-section">Models</div>
        <div id="model-menu-list" class="model-list">
          <button type="button" class="menu-item" data-model-action="refresh">Refresh list</button>
        </div>
      </div>
      <div id="palette-menu" class="menu palette-menu hidden" role="listbox"></div>
      <div id="composer">
        <div id="chip-files" class="chip-files"></div>
        <textarea id="input" rows="2" placeholder="Plan, @ for files, / for commands…"></textarea>
        <div id="toolbar">
          <div id="toolbar-left">
            <button type="button" class="pill" id="mode-btn" aria-haspopup="menu">
              <span class="ico" id="mode-icon">∞</span>
              <span id="mode-label">Agent</span>
              <span class="chev">▾</span>
            </button>
            <button type="button" class="pill" id="effort-btn" aria-haspopup="menu">
              <span id="effort-label">Medium</span>
              <span class="chev">▾</span>
            </button>
            <button type="button" class="pill" id="apply-toggle" role="switch" aria-checked="false" title="Apply edits to disk">
              <span id="apply-label">Dry-run</span>
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

function parsePendingOps(data: unknown): PendingOpsPayload | undefined {
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

/** Hide @path refs in chat bubbles — paths belong on attachment chips only. */
function uiDisplayText(raw: string): string {
  const t = raw.trim();
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
