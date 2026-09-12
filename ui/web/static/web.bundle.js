/* AUTO-GENERATED — do not edit. Sources: ui/vscode/media/chat-src/* + ui/web/src/*  →  node ui/web/scripts/bundle-web.mjs */
//@ts-check
/* Generated from media/chat-src — edit fragments there, then: npm run bundle:webview */
(function () {
  // The web host. Supplies the three methods the shared renderer fragments
  // reach for (see 01-dom-state.js's `host`), backed by a WebSocket instead of
  // the VS Code API. Everything the renderer knows about its host is here and
  // in the adapter fragments that follow.

  const STATE_KEY = "orchestra.web.state";

  const host = {
    /** @param {any} msg */
    postMessage(msg) {
      // dispatchToCore is defined in 10-adapter-session.js. Calls that arrive
      // before it exists are a bug, not a race: the renderer only posts in
      // response to user input or an inbound message, both of which come after
      // the whole bundle has evaluated.
      dispatchToCore(msg);
    },
    getState() {
      try {
        return JSON.parse(sessionStorage.getItem(STATE_KEY) || "{}");
      } catch (e) {
        return {};
      }
    },
    /** @param {any} state */
    setState(state) {
      try {
        sessionStorage.setItem(STATE_KEY, JSON.stringify(state));
      } catch (e) {
        // Private mode, or storage disabled. State is a convenience.
      }
    },
  };

  /**
   * Deliver an inbound message to the renderer. 07-events.js listens on
   * window's "message" event, so this is the same door VS Code posts through.
   * @param {any} msg
   */
  function toRenderer(msg) {
    window.postMessage(msg, "*");
  }

  // ---- JSON-RPC over one socket per project -------------------------------
  //
  // A project's core is reached through its own socket (part A's guard is per
  // project, so several are allowed). The four helpers below keep the
  // signatures the adapter fragments already use and route to whichever
  // project is active, so "which socket" is a question only this file and
  // 40-projects.js answer.

  /**
   * The socket for a project. No token: the page was served with an HttpOnly
   * cookie, which the browser attaches to the handshake by itself.
   * @param {string} projectId
   */
  function socketURL(projectId) {
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const base = `${scheme}//${location.host}/ws`;
    return projectId ? `${base}?project=${encodeURIComponent(projectId)}` : base;
  }

  /** @type {any} */
  let active = null;

  /**
   * @param {string} projectId "" for the project-less socket
   * @param {{onOpen?: Function, onClose?: Function, onError?: Function, onNotification?: Function, onServerRequest?: Function}} handlers
   */
  function createConn(projectId, handlers) {
    const h = handlers || {};
    let nextRpcId = 1;
    /** @type {Map<number, {resolve: Function, reject: Function}>} */
    const pendingCalls = new Map();
    const ws = new WebSocket(socketURL(projectId));

    const conn = {
      projectId,
      isOpen: () => ws.readyState === WebSocket.OPEN,
      /** @param {string} method @param {any} params @returns {Promise<any>} */
      send(method, params) {
        return new Promise((resolve, reject) => {
          if (ws.readyState !== WebSocket.OPEN) {
            reject(new Error("not connected"));
            return;
          }
          const id = nextRpcId++;
          pendingCalls.set(id, { resolve, reject });
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, method, params: params || {} }));
        });
      },
      /**
       * Like send, but hands back the request id so the caller can cancel it
       * later with $/cancelRequest. Reading nextRpcId here is safe: send
       * allocates it synchronously, with no await in between.
       * @param {string} method @param {any} params
       * @returns {{id: number, done: Promise<any>}}
       */
      sendCancellable(method, params) {
        const id = nextRpcId;
        return { id, done: conn.send(method, params) };
      },
      /** @param {string} method @param {any} params */
      notify(method, params) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", method, params: params || {} }));
        }
      },
      /** Reply to a server-initiated request. @param {any} id @param {any} result */
      reply(id, result) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, result }));
        }
      },
      close() {
        try {
          ws.close();
        } catch (e) {
          // Already closing. Nothing to do.
        }
      },
    };

    ws.addEventListener("open", () => {
      if (h.onOpen) h.onOpen(projectId);
    });
    ws.addEventListener("close", () => {
      for (const { reject } of pendingCalls.values()) {
        reject(new Error("disconnected"));
      }
      pendingCalls.clear();
      if (h.onClose) h.onClose(projectId);
    });
    ws.addEventListener("error", () => {
      if (h.onError) h.onError(projectId);
    });
    ws.addEventListener("message", (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (msg.id !== undefined && msg.method === undefined) {
        const p = pendingCalls.get(msg.id);
        if (!p) {
          return;
        }
        pendingCalls.delete(msg.id);
        if (msg.error) {
          // -32603 carries the literal string "Internal error" as its message
          // and the thing that actually went wrong in data.error
          // (protocol/jsonrpc/server.go). Rejecting with the message alone
          // put "Internal error" in the transcript and threw the only useful
          // half away, leaving nothing to act on and nothing to report.
          const detail =
            msg.error.data && typeof msg.error.data.error === "string"
              ? msg.error.data.error
              : "";
          const text = detail || msg.error.message || "rpc error";
          const err = new Error(text);
          // @ts-ignore — kept for callers that branch on the code.
          err.rpcCode = msg.error.code;
          p.reject(err);
        } else {
          p.resolve(msg.result);
        }
        return;
      }
      if (msg.id !== undefined && msg.method) {
        if (h.onServerRequest) h.onServerRequest(projectId, msg);
        return;
      }
      if (msg.method && h.onNotification) {
        h.onNotification(projectId, msg);
      }
    });

    return conn;
  }

  /** @param {any} conn */
  function setActiveConn(conn) {
    active = conn;
  }

  // Only one of the original four helpers survives: wsNotify, for
  // dispatchToCore's "cancelTurn" ($/cancelRequest is a fire-and-forget
  // notification, not a call awaited across a switch, so routing it to
  // whichever project is active at the synchronous moment cancelTurn runs is
  // correct — it is always the project on screen). wsSend, wsSendCancellable
  // and wsReply routed to "whichever project is active by the time the call
  // happens", which is wrong for anything that awaits: every other caller now
  // takes connFor(projectId) itself and calls send()/sendCancellable()/reply()
  // on that project's own connection directly (10-adapter-session.js,
  // 30-adapter-asks.js).

  /** @param {string} method @param {any} params */
  function wsNotify(method, params) {
    if (active) active.notify(method, params);
  }

  // Test seam: adapter-test.mjs drives the outbound path by posting a window
  // message, because `host` lives inside this IIFE and nothing outside can
  // reach it. Harmless in a real page — no renderer fragment posts this type.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "__host_dispatch__") {
      host.postMessage(ev.data.payload);
    }
  });
  /* host is supplied by ui/web/src/00-web-prelude.js */

  /** @typedef {{ id: string; label: string; icon: string; mode: string }} ModeOpt */
  /** @typedef {{ id: string; label: string; profile: string }} EffortOpt */

  /** @type {ModeOpt[]} — must match TUI `agentModes` / docs/modes.md top-level modes */
  const MODES = [
    { id: "build", label: "Build", icon: "▣", mode: "build" },
    { id: "plan", label: "Plan", icon: "≡", mode: "plan" },
    { id: "explore", label: "Explore", icon: "⌕", mode: "explore" },
    { id: "ask", label: "Ask", icon: "◇", mode: "ask" },
    { id: "debug", label: "Debug", icon: "⌁", mode: "debug" },
    { id: "architecture", label: "Architecture", icon: "◫", mode: "architecture" },
    { id: "agent", label: "Agent", icon: "∞", mode: "agent" },
    { id: "orchestra", label: "Orchestra", icon: "◎", mode: "orchestra" },
  ];

  /** @type {{ label: string; ids: string[] }[]} */
  const MODE_GROUPS = [
    { label: "Основные", ids: ["agent", "orchestra", "build", "plan"] },
    { label: "Дополнительные", ids: ["explore", "ask", "debug", "architecture"] },
  ];

  /** @typedef {{ id: string; label: string; hint: string; icon: string }} AccessOpt */

  /** @type {AccessOpt[]} */
  const ACCESS_MODES = [
    {
      id: "ask",
      label: "Ask",
      hint: "Shell с подтверждением; правки через Accept/Reject",
      icon: "◌",
    },
    {
      id: "auto",
      label: "Auto",
      hint: "Shell и запись файлов сразу на диск (без Accept/Reject)",
      icon: "▶",
    },
  ];

  /** @typedef {{ id: string; label: string; profile: string }} EffortOpt */

  /** @type {EffortOpt[]} */
  const EFFORTS = [
    { id: "low", label: "Low", profile: "fast" },
    { id: "medium", label: "Medium", profile: "" },
    { id: "high", label: "High", profile: "precision" },
  ];

  const chromeHint = document.getElementById("chrome-hint");
  const messagesEl = document.getElementById("messages");
  const inputEl = /** @type {HTMLTextAreaElement | null} */ (document.getElementById("input"));
  const sendBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("send"));
  const modeBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("mode-btn"));
  const accessBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("access-btn"));
  const accessMenu = document.getElementById("access-menu");
  const accessLabel = document.getElementById("access-label");
  const effortBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("effort-btn"));
  const modeMenu = document.getElementById("mode-menu");
  const effortMenu = document.getElementById("effort-menu");
  const modeLabel = document.getElementById("mode-label");
  const effortLabel = document.getElementById("effort-label");
  const costWrap = document.getElementById("cost-wrap");
  const costBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("cost-btn"));
  const costLabelEl = document.getElementById("cost-label");
  const costPopover = document.getElementById("cost-popover");
  const costBalanceEl = document.getElementById("cost-balance");
  const costSummaryEl = document.getElementById("cost-summary");
  const costRowsEl = document.getElementById("cost-rows");
  const contextBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("context-btn"));
  const contextRingFill = document.getElementById("context-ring-fill");
  const contextPopover = document.getElementById("context-popover");
  const ctxPct = document.getElementById("ctx-pct");
  const ctxSummary = document.getElementById("ctx-summary");
  const ctxBar = document.getElementById("ctx-bar");
  const ctxRows = document.getElementById("ctx-rows");
  const attachBtn = document.getElementById("attach-btn");
  const filesEl = document.getElementById("chip-files");
  const fastToggleRef = /** @type {{ el: HTMLButtonElement | null }} */ ({ el: null });
  const composerWrap = document.getElementById("composer-wrap");
  const composerStatus = document.getElementById("composer-status");
  const composerStatusLabel = document.getElementById("composer-status-label");
  const messageQueueEl = document.getElementById("message-queue");
  const sessionTabsEl = document.getElementById("session-tabs");
  const sessionNewBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-new-btn"));
  const sessionHistoryBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-history-btn"));
  const settingsBtn = document.getElementById("settings-btn");
  const sessionMenu = document.getElementById("session-menu");
  const sessionMenuList = document.getElementById("session-menu-list");
  const modelLabelEl = document.getElementById("model-label");
  const modelPill = /** @type {HTMLButtonElement | null} */ (document.getElementById("model-pill"));
  const modelMenu = document.getElementById("model-menu");
  const modelMenuTitle = document.getElementById("model-menu-title");
  const modelMenuList = document.getElementById("model-menu-list");
  const modelMenuSearch = /** @type {HTMLInputElement | null} */ (document.getElementById("model-menu-search"));
  const orchConfigBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("orch-config-btn"));
  const pendingBar = document.getElementById("pending-bar");
  const pendingLabel = document.getElementById("pending-label");
  const pendingReviewListEl = document.getElementById("pending-review-list");
  const pendingApplyBtn = document.getElementById("pending-apply-btn");
  const pendingRejectBtn = document.getElementById("pending-reject-btn");
  const overlay = document.getElementById("overlay");
  const overlayTitle = document.getElementById("overlay-title");
  const overlayBody = document.getElementById("overlay-body");
  const overlayOptions = document.getElementById("overlay-options");
  const overlayInput = /** @type {HTMLInputElement | null} */ (document.getElementById("overlay-input"));
  const overlayActions = document.getElementById("overlay-actions");
  const paletteMenu = document.getElementById("palette-menu");
  const todosBar = document.getElementById("todos-bar");
  const todosList = document.getElementById("todos-list");
  const todosChip = document.getElementById("todos-chip");
  const todosChipGlyph = document.getElementById("todos-chip-glyph");
  const todosChipSummary = document.getElementById("todos-chip-summary");
  const todosChipChev = document.getElementById("todos-chip-chev");
  const workflowBar = document.getElementById("workflow-bar");
  const workflowLabel = document.getElementById("workflow-label");
  const workflowStagesEl = document.getElementById("workflow-stages");
  const subagentsBar = document.getElementById("subagents-bar");
  const subagentsTree = document.getElementById("subagents-tree");
  const diffViewer = document.getElementById("diff-viewer");
  const diffViewerTitle = document.getElementById("diff-viewer-title");
  const diffPaneBefore = document.getElementById("diff-pane-before");
  const diffPaneAfter = document.getElementById("diff-pane-after");
  const diffViewerCloseBtn = document.getElementById("diff-viewer-close-btn");
  const diffViewerEditorBtn = document.getElementById("diff-viewer-editor-btn");
  const imagePreview = document.getElementById("image-preview");
  const imagePreviewTitle = document.getElementById("image-preview-title");
  const imagePreviewImg = /** @type {HTMLImageElement | null} */ (document.getElementById("image-preview-img"));
  const imagePreviewCloseBtn = document.getElementById("image-preview-close-btn");
  const imagePreviewOpenBtn = document.getElementById("image-preview-open-btn");
  const imagePreviewPrevBtn = document.getElementById("image-preview-prev-btn");
  const imagePreviewNextBtn = document.getElementById("image-preview-next-btn");
  const imagePreviewCounter = document.getElementById("image-preview-counter");
  const statusLsp = document.getElementById("status-lsp");

  /** @type {{ cmd: string; desc: string }[]} */
  const SLASH_CMDS = [
    { cmd: "/clear", desc: "New chat" },
    { cmd: "/compact", desc: "Compress LLM context" },
    { cmd: "/help", desc: "Show commands" },
    { cmd: "/model", desc: "Change model" },
    { cmd: "/rewind", desc: "Checkpoint rewind help" },
    { cmd: "/sessions", desc: "Switch session" },
    { cmd: "/settings", desc: "Open settings" },
  ];

  /** Loaded skills, each usable as its own "/<name>" command. Replaced
   * wholesale on every "skillsList" message — it is its own array (not
   * appended to SLASH_CMDS) so a refresh can't accumulate stale entries.
   * @type {{ cmd: string; desc: string }[]} */
  let SKILL_CMDS = [];

  /** @type {Map<string, HTMLElement>} */
  const toolBlocks = new Map();
  /** @type {Map<string, string>} */
  const toolArgs = new Map();
  /** @type {Map<number, HTMLElement>} step → tool body pre */
  const execSteps = new Map();
  /** @type {{ id: string; content: string; status: string }[]} */
  let todos = [];
  let todosExpanded = false;
  let todosHadOpen = false;
  let todosDoneFlashTimer = 0;
  let paletteMode = "";
  let paletteIndex = 0;
  /** @type {any[]} */
  let paletteItems = [];
  let mentionTimer = 0;
  /** @type {{ prompt: number; completion: number; limit: number; maxResponse: number; estimated: boolean; breakdown: { key: string; label: string; tokens: number }[] }} */
  let ctxState = { prompt: 0, completion: 0, limit: 128000, maxResponse: 4096, estimated: false, breakdown: [] };
  let ctxPopoverOpen = false;

  /** @type {{ ops: any[]; diff: { path?: string; before?: string; after?: string; reviewStatus?: string }[] }} */
  let pendingState = { ops: [], diff: [] };
  let diffReviewCursor = 0;
  /** @type {{ id: string; type: string; label: string; status: string; taskId?: string; parentToolCallId?: string; toolsEl?: HTMLElement; toolCount?: number }[]} */
  let subagents = [];
  /** @type {Map<string, { taskId: string; parentToolCallId?: string; type: string; label: string; status: string; toolsEl?: HTMLElement; rowEl?: HTMLElement; toolCount: number }>} */
  const subagentByTask = new Map();
  /** @type {Map<string, (lines: string[]) => void>} */
  const highlightWaiters = new Map();
  let highlightSeq = 0;
  let diffViewerState = { path: "", before: "", after: "" };
  /** @type {{ items: { name: string; path?: string; previewUri?: string }[]; index: number }} */
  let imagePreviewState = { items: [], index: 0 };

  /** 20 MB — must match core `attachments.MaxImageBytes`. */
  const MAX_ATTACH_BYTES = 20 * 1024 * 1024;
  /** @type {Map<string, { id: string; name: string; state: string; attempt: number }>} */
  const workflowStages = new Map();
  let workflowActiveName = "";
  /** @type {{ questions: any[]; index: number; answers: string[]; mode: string }} */
  let questionState = { questions: [], index: 0, answers: [], mode: "" };

  const saved = host.getState() || {};
  let accessId =
    typeof saved.accessId === "string" && ACCESS_MODES.some((m) => m.id === saved.accessId)
      ? saved.accessId
      : "ask";
  let assistantBubble = null;
  /** @type {HTMLElement | null} */
  let assistantTurn = null;
  /** @type {HTMLElement | null} */
  let assistantTurnInner = null;
  /** @type {HTMLDetailsElement | null} */
  let toolTraceEl = null;
  /** @type {HTMLElement | null} */
  let toolTraceSummary = null;
  /** @type {HTMLDetailsElement | null} */
  let reasoningDetails = null;
  /** @type {HTMLElement | null} */
  let reasoningBody = null;
  let reasoningStarted = 0;
  /** @type {{ read: number; search: number; write: number; other: number }} */
  let turnToolCount = { read: 0, search: 0, write: 0, other: 0 };
  // Wall time of the turn's tool work: first tool start to last tool finish.
  // Zero means no live tool ran in this turn — restored history included,
  // which is timed by nobody and must not report a duration.
  let turnToolFirstStart = 0;
  let turnToolLastEnd = 0;
  let busy = false;
  let busyStatusText = "Working…";
  /** @type {Array<{ id: string; preview: string; fileCount?: number }>} */
  let sendQueue = [];
  /** @type {HTMLElement | null} */
  let typingIndicatorEl = null;
  /** Raw streamed assistant text before final-envelope stripping. */
  let streamRawText = "";
  let modeId = typeof saved.modeId === "string" && MODES.some((m) => m.id === saved.modeId) ? saved.modeId : "agent";
  let providerModelsCatalog = null;
  let modelMenuFilter = "";
  /** @type {{ roles: any[]; defaultTier: string } | null} Orchestra tier map for the footer pill. */
  let orchestraRolesInfo = null;
  let effortId = "medium";
  let fastOn = false;
  let currentModel = "";
  /** Accumulated session spend in USD (provider-reported). */
  let sessionCostUSD = 0;
  /**
   * Live cost of the in-flight turn, summed from per-step step_usage events
   * (each LLM call reports its cost when its stream finishes). Replaced by
   * the authoritative server total on turnUsage, then reset to 0.
   */
  let turnCostAccum = 0;
  /** @type {{ cost_usd?: number; total_tokens?: number; entries?: any[] } | null} Last turn usage summary. */
  let lastTurnUsage = null;
  /** @type {{ supported: boolean; provider?: string; balance?: number } | null} Provider balance (OpenRouter). */
  let creditsInfo = null;
  let activeSessionId = "";
  /** @type {{ name: string; path?: string; ext?: string; kind?: string; previewUri?: string }[]} */
  let files = [];
  function escapeAttr(value) {
    return String(value || "")
      .replace(/&/g, "&amp;")
      .replace(/"/g, "&quot;")
      .replace(/</g, "&lt;");
  }

  function fileExtLabel(nameOrExt) {
    const raw = (nameOrExt || "").trim();
    if (!raw) {
      return "FILE";
    }
    if (raw.includes(".")) {
      const ext = raw.split(".").pop() || "";
      return ext.slice(0, 4).toUpperCase() || "FILE";
    }
    return raw.slice(0, 4).toUpperCase();
  }

  function relPathDisplay(fullPath) {
    if (!fullPath) {
      return "";
    }
    const parts = fullPath.replace(/\\/g, "/").split("/");
    if (parts.length <= 2) {
      return fullPath;
    }
    return parts.slice(-3).join("/");
  }

  function addFileRef(f) {
    if (!f || typeof f.name !== "string") {
      return;
    }
    const key = f.path || f.name;
    if (files.some((x) => (x.path || x.name) === key)) {
      return;
    }
    files.push({
      name: f.name,
      path: typeof f.path === "string" ? f.path : undefined,
      ext: typeof f.ext === "string" ? f.ext : undefined,
      kind: typeof f.kind === "string" ? f.kind : undefined,
      previewUri: typeof f.previewUri === "string" ? f.previewUri : undefined,
    });
    renderFiles();
  }

  function truncateTabTitle(title) {
    const t = (title || "New chat").trim() || "New chat";
    return t.length > 22 ? t.slice(0, 20) + "…" : t;
  }

  /** @param {{ id: string; title?: string; model?: string }[]} tabs */
  function renderSessionTabs(tabs, activeId) {
    if (!sessionTabsEl) {
      return;
    }
    activeSessionId = activeId || "";
    sessionTabsEl.innerHTML = "";
    const list = Array.isArray(tabs) ? tabs : [];
    if (list.length === 0) {
      const empty = document.createElement("div");
      empty.className = "session-tabs-empty";
      empty.textContent = "New chat";
      sessionTabsEl.appendChild(empty);
      return;
    }
    list.forEach((s) => {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = "session-tab" + (s.id === activeId ? " active" : "");
      tab.setAttribute("role", "tab");
      tab.setAttribute("aria-selected", s.id === activeId ? "true" : "false");
      tab.setAttribute("data-session-id", s.id);
      tab.title = s.title || s.id;

      const icon = document.createElement("span");
      icon.className = "session-tab-icon";
      icon.innerHTML =
        '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
        '<path d="M7 9.5h10M7 13.5h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>' +
        '<path d="M5 5.5h14a2 2 0 012 2v8.5a2 2 0 01-2 2H9.5L6 21v-3H5a2 2 0 01-2-2V7.5a2 2 0 012-2z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>' +
        "</svg>";

      const label = document.createElement("span");
      label.className = "session-tab-label";
      label.textContent = truncateTabTitle(s.title);

      const close = document.createElement("span");
      close.className = "session-tab-close";
      close.setAttribute("data-close-session", s.id);
      close.setAttribute("role", "button");
      close.setAttribute("aria-label", "Close session");
      close.textContent = "×";

      tab.appendChild(icon);
      tab.appendChild(label);
      tab.appendChild(close);
      sessionTabsEl.appendChild(tab);
    });

    const activeTab = sessionTabsEl.querySelector(".session-tab.active");
    if (activeTab && typeof activeTab.scrollIntoView === "function") {
      activeTab.scrollIntoView({ block: "nearest", inline: "nearest" });
    }
  }

  function updateActiveTabTitle(title) {
    if (!sessionTabsEl || !activeSessionId) {
      return;
    }
    const tab = sessionTabsEl.querySelector(`[data-session-id="${activeSessionId}"]`);
    if (!tab) {
      return;
    }
    const label = tab.querySelector(".session-tab-label");
    if (label) {
      label.textContent = truncateTabTitle(title);
    }
    tab.title = title || activeSessionId;
  }

  /** Short label for long provider/model ids (Cursor-style). */
  function shortModel(id) {
    if (!id) {
      return "Model";
    }
    const parts = String(id).split(/[/\\]/);
    const last = parts[parts.length - 1] || id;
    return last.length > 28 ? last.slice(0, 25) + "…" : last;
  }

  function setModelLabel(id) {
    currentModel = id || currentModel;
    if (modelLabelEl) {
      modelLabelEl.textContent = shortModel(currentModel);
      modelLabelEl.title = currentModel || "";
    }
  }

  function currentMode() {
    return MODES.find((m) => m.id === modeId) || MODES[0];
  }

  function currentEffort() {
    return EFFORTS.find((e) => e.id === effortId) || EFFORTS[1];
  }

  function effectiveProfile() {
    if (fastOn) {
      return "fast";
    }
    return currentEffort().profile;
  }

  function currentAccess() {
    return ACCESS_MODES.find((m) => m.id === accessId) || ACCESS_MODES[0];
  }

  function initAccessMenu() {
    if (!accessMenu) {
      return;
    }
    accessMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = "Доступ";
    accessMenu.appendChild(head);
    ACCESS_MODES.forEach((m) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item access-item";
      btn.dataset.access = m.id;
      btn.title = m.hint;
      btn.innerHTML =
        `<span class="mi access-icon access-${escapeAttr(m.id)}">${escapeAttr(m.icon)}</span>` +
        `<span class="access-item-text"><span class="access-item-label">${escapeAttr(m.label)}</span>` +
        `<span class="access-item-hint">${escapeAttr(m.hint)}</span></span>`;
      accessMenu.appendChild(btn);
    });
    const note = document.createElement("div");
    note.className = "menu-hint access-menu-note";
    note.textContent =
      "Ask: правки в staging + Accept/Reject. Auto: правки пишутся на диск сразу.";
    accessMenu.appendChild(note);
  }

  function syncAccessUi() {
    const m = currentAccess();
    if (accessLabel) {
      accessLabel.textContent = m.label;
    }
    const icon = document.getElementById("access-icon");
    if (icon) {
      icon.textContent = m.icon;
      icon.className = `ico access-icon access-${m.id}`;
    }
    if (accessBtn) {
      accessBtn.dataset.access = accessId;
      accessBtn.title = m.hint;
    }
    accessMenu?.querySelectorAll("[data-access]").forEach((el) => {
      const id = el.getAttribute("data-access");
      el.classList.toggle("selected", id === accessId);
    });
    host.setState({ ...(host.getState() || {}), accessId });
  }

  function statsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  function langFromPath(filePath) {
    const ext = (filePath || "").split(".").pop()?.toLowerCase() || "";
    const map = {
      go: "go",
      ts: "ts",
      tsx: "tsx",
      js: "js",
      jsx: "jsx",
      py: "py",
      rs: "rust",
      md: "md",
      json: "json",
      yml: "yaml",
      yaml: "yaml",
      css: "css",
      html: "html",
      sh: "bash",
    };
    return map[ext] || "plain";
  }
  function escapeHtml(text) {
    // Quotes must be escaped too: escaped text is interpolated into
    // attribute values (e.g. link href) — an unescaped `"` would break out
    // of the attribute (XSS defense-in-depth on top of the CSP).
    return String(text || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  /** Only allow benign navigation schemes in model-authored links. */
  function safeLinkHref(url) {
    return /^(https?:|mailto:)/i.test(url) ? url : "";
  }

  /** @param {string} s */
  function markdownInline(s) {
    let x = escapeHtml(s);
    x = x.replace(/`([^`\n]+)`/g, '<code class="md-code">$1</code>');
    x = x.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
    x = x.replace(/(?<![*])\*([^*\n]+)\*(?![*])/g, "<em>$1</em>");
    x = x.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label, url) => {
      const href = safeLinkHref(url);
      if (!href) {
        // javascript:, command:, data: etc. — render as plain text.
        return `${label} (${url})`;
      }
      return `<a class="md-link" href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    });
    return x;
  }

  function pathLooksLikeFile(s) {
    const t = (s || "").trim();
    return /[./\\]/.test(t) || /\.[a-z0-9]{1,6}$/i.test(t);
  }

  /** @returns {{ lang?: string; path?: string; startLine?: number; endLine?: number }} */
  function parseCodeFenceMeta(openRest) {
    const raw = (openRest || "").trim();
    if (!raw) {
      return { lang: "plain" };
    }
    const gh = raw.match(/^(\d+):(\d+):(.+)$/);
    if (gh) {
      const fp = gh[3].trim();
      return { lang: langFromPath(fp), path: fp, startLine: +gh[1], endLine: +gh[2] };
    }
    const parts = raw.split(/\s+/);
    const first = parts[0] || "";
    const rest = parts.slice(1).join(" ").trim();

    /** @param {string} s */
    function parsePathLines(s) {
      const m1 = s.match(/^(.+?)\s+[Ll]ines?\s+(\d+)\s*[-–]\s*(\d+)$/);
      if (m1) {
        return { path: m1[1].trim(), startLine: +m1[2], endLine: +m1[3] };
      }
      const m2 = s.match(/^(.+?):(\d+)\s*[-–]\s*(\d+)$/);
      if (m2) {
        return { path: m2[1].trim(), startLine: +m2[2], endLine: +m2[3] };
      }
      const m3 = s.match(/^(.+?):(\d+)$/);
      if (m3) {
        return { path: m3[1].trim(), startLine: +m3[2], endLine: +m3[2] };
      }
      if (pathLooksLikeFile(s)) {
        return { path: s.trim() };
      }
      return null;
    }

    const combined = rest ? `${first} ${rest}` : first;
    const pl =
      parsePathLines(combined) ||
      parsePathLines(rest) ||
      (pathLooksLikeFile(first) && !rest ? parsePathLines(first) : null) ||
      (pathLooksLikeFile(rest) ? parsePathLines(rest) : null);

    if (pl?.path) {
      const lang =
        first && !pathLooksLikeFile(first) && first !== pl.path ? first : langFromPath(pl.path);
      return {
        lang,
        path: pl.path,
        startLine: pl.startLine,
        endLine: pl.endLine,
      };
    }
    if (first && !pathLooksLikeFile(first)) {
      return { lang: first };
    }
    return { lang: "plain" };
  }

  function codeRefTitle(filePath, startLine, endLine) {
    const base = basename(filePath);
    if (startLine && endLine && endLine !== startLine) {
      return `${base} Lines ${startLine}-${endLine}`;
    }
    if (startLine) {
      return `${base} Line ${startLine}`;
    }
    return base;
  }

  function buildCodeRefCardHtml(filePath, lang, startLine, endLine, code) {
    const title = codeRefTitle(filePath, startLine, endLine);
    return (
      `<div class="code-ref-card diff-preview-card"` +
      ` data-path="${escapeAttr(filePath)}" data-lang="${escapeAttr(lang || langFromPath(filePath))}"` +
      (startLine ? ` data-start-line="${startLine}"` : "") +
      (endLine ? ` data-end-line="${endLine}"` : "") +
      `>` +
      `<div class="diff-preview-head code-ref-head">` +
      diffExtBadgeHtml(filePath) +
      `<button type="button" class="diff-preview-name code-ref-title" title="Open file">${escapeAttr(title)}</button>` +
      `</div>` +
      `<pre class="code-ref-body md-pre"><code>${escapeHtml(code)}</code></pre>` +
      `</div>`
    );
  }

  function bindCodeRefCard(card) {
    if (!card || card.dataset.bound === "1") {
      return;
    }
    card.dataset.bound = "1";
    const path = card.dataset.path || "";
    card.querySelector(".code-ref-title")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      if (!path) {
        return;
      }
      if (/** @type {MouseEvent} */ (e).shiftKey) {
        openExternalFile(path, true);
      } else {
        openExternalFile(path, true);
      }
    });
  }

  async function enhanceCodeRefCards(root) {
    if (!root) {
      return;
    }
    const cards = root.querySelectorAll(".code-ref-card:not([data-enhanced])");
    for (const card of cards) {
      card.dataset.enhanced = "1";
      bindCodeRefCard(card);
      const path = card.dataset.path || "";
      const lang = card.dataset.lang || langFromPath(path);
      const pre = card.querySelector(".code-ref-body code");
      if (!pre) {
        continue;
      }
      const code = pre.textContent || "";
      const lines = code.split("\n");
      const startLine = Number(card.dataset.startLine) || 1;
      const htmlLines = await requestHighlight(lines, lang);
      const body = document.createElement("div");
      body.className = "code-ref-body diff-preview-body";
      lines.forEach((line, idx) => {
        const row = document.createElement("div");
        row.className = "code-ref-row";
        row.innerHTML =
          `<span class="code-ref-ln">${startLine + idx}</span>` +
          `<span class="code-ref-code">${htmlLines[idx] || escapeHtml(line) || "&nbsp;"}</span>`;
        body.appendChild(row);
      });
      pre.closest(".code-ref-body")?.replaceWith(body);
    }
  }

  /** @param {string} raw */
  function renderMarkdownToHtml(raw) {
    const text = String(raw || "");
    if (!text.trim()) {
      return "";
    }
    const lines = text.split("\n");
    const out = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      if (trimmed.startsWith("```")) {
        const meta = parseCodeFenceMeta(trimmed.slice(3).trim());
        i++;
        const codeLines = [];
        while (i < lines.length && !lines[i].trim().startsWith("```")) {
          codeLines.push(lines[i]);
          i++;
        }
        if (i < lines.length) {
          i++;
        }
        const code = codeLines.join("\n");
        if (meta.path) {
          out.push(
            buildCodeRefCardHtml(
              meta.path,
              meta.lang || langFromPath(meta.path),
              meta.startLine,
              meta.endLine,
              code
            )
          );
        } else {
          out.push(`<pre class="md-pre"><code>${escapeHtml(code)}</code></pre>`);
        }
        continue;
      }
      if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
        out.push('<hr class="md-hr">');
        i++;
        continue;
      }
      const h3 = line.match(/^###\s+(.+)$/);
      if (h3) {
        out.push(`<h3 class="md-h3">${markdownInline(h3[1])}</h3>`);
        i++;
        continue;
      }
      const h2 = line.match(/^##\s+(.+)$/);
      if (h2) {
        out.push(`<h2 class="md-h2">${markdownInline(h2[1])}</h2>`);
        i++;
        continue;
      }
      const h1 = line.match(/^#\s+(.+)$/);
      if (h1) {
        out.push(`<h1 class="md-h1">${markdownInline(h1[1])}</h1>`);
        i++;
        continue;
      }
      if (/^[-*+]\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^[-*+]\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^[-*+]\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ul class="md-ul">${items.join("")}</ul>`);
        continue;
      }
      if (/^\d+\.\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^\d+\.\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ol class="md-ol">${items.join("")}</ol>`);
        continue;
      }
      if (!trimmed) {
        i++;
        continue;
      }
      const para = [];
      while (
        i < lines.length &&
        lines[i].trim() &&
        !lines[i].trim().startsWith("```") &&
        !/^#{1,3}\s+/.test(lines[i]) &&
        !/^[-*+]\s+/.test(lines[i]) &&
        !/^\d+\.\s+/.test(lines[i]) &&
        !/^(-{3,}|\*{3,}|_{3,})$/.test(lines[i].trim())
      ) {
        para.push(lines[i]);
        i++;
      }
      out.push(`<p class="md-p">${markdownInline(para.join(" "))}</p>`);
    }
    return out.join("");
  }

  /** @param {HTMLElement} el @param {string} raw */
  function applyAssistantMarkdown(el, raw) {
    el.classList.add("turn-text", "md-body");
    el.innerHTML = renderMarkdownToHtml(raw);
    void enhanceCodeRefCards(el);
  }

  /** @type {number | null} */
  let mdRenderPending = null;

  /** @param {HTMLElement} el @param {string} raw */
  function scheduleAssistantMarkdown(el, raw) {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
    }
    mdRenderPending = requestAnimationFrame(() => {
      mdRenderPending = null;
      applyAssistantMarkdown(el, raw);
    });
  }

  function flushAssistantMarkdown() {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
      mdRenderPending = null;
    }
    if (assistantBubble) {
      applyAssistantMarkdown(assistantBubble, sanitizeAssistantStream(stripFinalEnvelope(streamRawText)));
    }
  }

  function highlightCode(line, lang) {
    let s = escapeHtml(line);
    if (lang === "plain" || !line.trim()) return s;
    const strRe = /(&quot;[^&]*?&quot;|'[^']*?'|`[^`]*?`)/g;
    const apply = (text, re, cls) => text.replace(re, (m) => `<span class="syn-${cls}">${m}</span>`);
    s = apply(s, strRe, "str");
    if (lang === "go") {
      s = apply(
        s,
        /\b(func|return|if|else|for|range|package|import|type|struct|interface|var|const|go|defer|switch|case|default|map|chan|select)\b/g,
        "kw"
      );
    } else if (lang === "ts" || lang === "tsx" || lang === "js" || lang === "jsx") {
      s = apply(
        s,
        /\b(const|let|var|function|return|if|else|for|while|import|export|from|class|interface|type|async|await|new|this)\b/g,
        "kw"
      );
    } else if (lang === "py") {
      s = apply(s, /\b(def|return|if|elif|else|for|while|import|from|class|with|as|pass|None|True|False)\b/g, "kw");
    } else if (lang === "rust") {
      s = apply(s, /\b(fn|let|mut|pub|use|struct|enum|impl|match|if|else|return|mod|crate)\b/g, "kw");
    }
    s = apply(s, /(\/\/.*$|#.*$)/g, "cm");
    return s;
  }
  function isMutatingToolBlock(block) {
    return block?.classList?.contains("kind-write") === true;
  }

  function findDiffForPath(filePath) {
    if (!filePath) return null;
    const norm = filePath.replace(/\\/g, "/");
    if (pendingState.diff.length) {
      const hit = pendingState.diff.find((d) => {
        const p = (d.path || "").replace(/\\/g, "/");
        return p === norm || p.endsWith("/" + norm) || norm.endsWith("/" + p) || basename(p) === basename(norm);
      });
      if (hit) {
        return hit;
      }
    }
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const p = fp.replace(/\\/g, "/");
      if (
        p !== norm &&
        !p.endsWith("/" + norm) &&
        !norm.endsWith("/" + p) &&
        basename(p) !== basename(norm)
      ) {
        continue;
      }
      if (block.dataset.diffBefore !== undefined || block.dataset.diffAfter !== undefined) {
        return {
          path: fp,
          before: block.dataset.diffBefore || "",
          after: block.dataset.diffAfter || "",
        };
      }
    }
    return null;
  }

  function extractDiffFromTool(name, argsRaw, result) {
    const path = toolPathFromArgs(name, argsRaw, result || "");
    if (!path) {
      return null;
    }
    const args = parseToolArgs(argsRaw);
    const n = (name || "").toLowerCase();
    if (n === "write" || n === "fs.write" || n === "file.write_atomic") {
      const content = typeof args.content === "string" ? args.content : "";
      return { path, before: "", after: content };
    }
    if (n === "edit" || n === "fs.edit") {
      const search = typeof args.search === "string" ? args.search : "";
      const replace = typeof args.replace === "string" ? args.replace : "";
      if (search || replace) {
        return { path, before: search, after: replace };
      }
    }
    return null;
  }

  function rememberBlockDiff(block, before, after) {
    if (!block) return;
    block.dataset.diffBefore = before || "";
    block.dataset.diffAfter = after || "";
  }

  function syncToolDiffStats() {
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff) continue;
      const stats = countDiffStats(diff.before, diff.after);
      const el = block.querySelector(".tool-stats");
      if (el) el.innerHTML = statsHtml(stats);
    }
  }

  /** Upgrade write/edit tool blocks to Cursor-style inline diff when pending data arrives. */
  async function syncToolDiffPreviews() {
    syncToolDiffStats();
    const jobs = [];
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff || (!diff.before && !diff.after)) continue;
      block.querySelector(".file-change-card")?.remove();
      jobs.push(attachInlineToolDiff(block, fp, diff.before || "", diff.after || ""));
    }
    if (jobs.length) {
      await Promise.all(jobs);
    }
  }

  function attachToolDiffShell(block, filePath) {
    if (!block || !filePath) return;
    if (block.querySelector(".tool-diff-body.diff-preview-card")) return;
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    const body = block.querySelector(".tool-body");
    const diffWrap = document.createElement("div");
    diffWrap.className = "tool-body tool-diff-body diff-preview-card";
    const head = document.createElement("div");
    head.className = "diff-preview-head";
    head.innerHTML =
      diffExtBadgeHtml(filePath) +
      `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
      `<span class="tool-diff-pending">…</span>`;
    const lines = document.createElement("div");
    lines.className = "diff-preview-body tool-diff-pending-body";
    lines.textContent = "Loading diff preview…";
    diffWrap.appendChild(head);
    diffWrap.appendChild(lines);
    head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      const d = findDiffForPath(filePath) || extractDiffFromTool("write", block.dataset.argsRaw || "", "");
      openDiffMessage(
        filePath,
        d?.before || block.dataset.diffBefore || "",
        d?.after || block.dataset.diffAfter || "",
        /** @type {MouseEvent} */ (e).shiftKey
      );
    });
    if (body) {
      body.replaceWith(diffWrap);
    } else {
      block.appendChild(diffWrap);
    }
    block.classList.add("write-card-only");
  }

  function renderPendingBar() {
    const n = pendingState.diff.length || pendingState.ops.length;
    if (!pendingBar) return;
    if (!n) {
      pendingBar.classList.add("hidden");
      return;
    }
    pendingBar.classList.remove("hidden");
    const fileCount = pendingState.diff.length;
    if (pendingLabel) {
      pendingLabel.textContent = fileCount
        ? `${fileCount} file${fileCount === 1 ? "" : "s"} changed`
        : `${n} pending change${n === 1 ? "" : "s"}`;
    }
    renderPendingReviewList();
  }

  function diffExtBadgeHtml(filePath) {
    const ext = fileExtLabel(filePath || "");
    const lang = langFromPath(filePath || "");
    return `<span class="diff-ext-badge lang-${escapeAttr(lang)}">${escapeAttr(ext)}</span>`;
  }

  function diffStatsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  async function renderPendingReviewList() {
    if (!pendingReviewListEl) return;
    bindPendingReviewListEvents();
    pendingReviewListEl.innerHTML = "";
    if (!pendingState.diff.length) {
      return;
    }
    for (let idx = 0; idx < pendingState.diff.length; idx++) {
      const d = pendingState.diff[idx];
      const stats = countDiffStats(d.before, d.after);
      const item = document.createElement("div");
      item.className = "pending-review-item diff-preview-card";
      item.setAttribute("data-idx", String(idx));
      if (idx === diffReviewCursor) item.classList.add("selected");

      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(d.path || "") +
        `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(d.path || "file"))}</button>` +
        diffStatsHtml(stats);

      const body = document.createElement("div");
      body.className = "diff-preview-body";

      item.appendChild(head);
      item.appendChild(body);
      pendingReviewListEl.appendChild(item);
      await renderUnifiedDiffLines(body, d.before || "", d.after || "", d.path || "", 28);
      if (idx === diffReviewCursor) {
        item.scrollIntoView({ block: "nearest", behavior: "smooth" });
      }
    }
  }

  function applyPendingChanges() {
    host.postMessage({ type: "applyPending" });
  }

  function discardPendingChanges() {
    host.postMessage({ type: "discardPending" });
  }

  function countDiffStats(before, after) {
    const bLines = (before || "").split("\n");
    const aLines = (after || "").split("\n");
    if (!before && after) {
      return { add: aLines.length, del: 0 };
    }
    if (before && !after) {
      return { add: 0, del: bLines.length };
    }
    const freq = new Map();
    for (const line of bLines) {
      freq.set(line, (freq.get(line) || 0) + 1);
    }
    let add = 0;
    for (const line of aLines) {
      const c = freq.get(line) || 0;
      if (c > 0) {
        freq.set(line, c - 1);
      } else {
        add++;
      }
    }
    let del = 0;
    for (const c of freq.values()) {
      del += c;
    }
    return { add, del };
  }

  function toolBlockKey(msg) {
    const id = msg.toolCallId || `${msg.toolName}-${msg.step ?? toolBlocks.size}`;
    if (msg.scope === "child" && msg.taskId) {
      return `${msg.taskId}:${id}`;
    }
    return id;
  }

  function requestHighlight(lines, lang) {
    const requestId = `hl-${++highlightSeq}`;
    return new Promise((resolve) => {
      highlightWaiters.set(requestId, resolve);
      host.postMessage({
        type: "highlightCode",
        requestId,
        language: lang || "plaintext",
        lines: Array.isArray(lines) ? lines : [],
      });
      setTimeout(() => {
        if (highlightWaiters.has(requestId)) {
          highlightWaiters.delete(requestId);
          resolve(lines.map((l) => escapeHtml(String(l || ""))));
        }
      }, 8000);
    });
  }

  /** Myers-style line alignment for side-by-side diff panes. */
  function alignDiffLines(before, after) {
    const a = (before || "").split("\n");
    const b = (after || "").split("\n");
    const n = a.length;
    const m = b.length;
    const dp = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    const rows = [];
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && a[i] === b[j]) {
        rows.push({ type: "same", left: a[i], right: b[j], leftNum: i + 1, rightNum: j + 1 });
        i++;
        j++;
      } else if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) {
        rows.push({ type: "add", right: b[j], rightNum: j + 1 });
        j++;
      } else if (i < n) {
        rows.push({ type: "del", left: a[i], leftNum: i + 1 });
        i++;
      }
    }
    return rows;
  }

  async function renderDiffPanes(before, after, lang) {
    if (!diffPaneBefore || !diffPaneAfter) return;
    const rows = alignDiffLines(before, after);
    const leftLines = rows.map((r) => (r.type === "add" ? "" : r.left ?? ""));
    const rightLines = rows.map((r) => (r.type === "del" ? "" : r.right ?? ""));
    const [leftHtml, rightHtml] = await Promise.all([
      requestHighlight(leftLines, lang),
      requestHighlight(rightLines, lang),
    ]);
    diffPaneBefore.innerHTML = "";
    diffPaneAfter.innerHTML = "";
    rows.forEach((r, idx) => {
      const lRow = document.createElement("div");
      lRow.className = "diff-sbs-row" + (r.type === "del" ? " del" : r.type === "same" ? " same" : " empty");
      lRow.innerHTML =
        `<span class="diff-ln">${r.leftNum || ""}</span>` +
        `<span class="diff-code">${leftHtml[idx] || "&nbsp;"}</span>`;
      diffPaneBefore.appendChild(lRow);
      const rRow = document.createElement("div");
      rRow.className = "diff-sbs-row" + (r.type === "add" ? " add" : r.type === "same" ? " same" : " empty");
      rRow.innerHTML =
        `<span class="diff-ln">${r.rightNum || ""}</span>` +
        `<span class="diff-code">${rightHtml[idx] || "&nbsp;"}</span>`;
      diffPaneAfter.appendChild(rRow);
    });
  }

  function showDiffViewer(path, before, after, language) {
    if (!diffViewer) return;
    diffViewerState = { path: path || "", before: before || "", after: after || "" };
    if (diffViewerTitle) {
      diffViewerTitle.textContent = basename(path) || path || "Diff";
      diffViewerTitle.title = path || "";
    }
    diffViewer.classList.remove("hidden");
    void renderDiffPanes(before, after, language || langFromPath(path));
  }

  function hideDiffViewer() {
    diffViewer?.classList.add("hidden");
  }

  function openExternalFile(filePath, focus) {
    if (!filePath) return;
    host.postMessage({ type: "openFile", path: filePath, focus: Boolean(focus) });
  }

  function openDiffMessage(path, before, after, sideBySide) {
    host.postMessage({
      type: "openDiff",
      path,
      before: before || "",
      after: after || "",
      focus: !sideBySide,
      sideBySide: Boolean(sideBySide),
    });
  }
  /** Canonical Orchestra tier label (L1–L5) from a legacy band name. */
  function subagentTierLabel(tier) {
    const t = String(tier || "").trim();
    if (!t) return "";
    if (/^L[1-5]$/i.test(t)) return t.toUpperCase();
    const map = { planner: "L5", lead: "L4", complex: "L3", focused: "L3", micro: "L1", explore: "L2" };
    return map[t.toLowerCase()] || "";
  }

  /**
   * Worker goals are often raw WorkOrder JSON ('{ "intent": "...", ... }').
   * Extract the human field instead of showing the JSON blob.
   */
  function humanizeTaskLabel(text) {
    const t = String(text || "").trim();
    if (!t.startsWith("{")) return t;
    try {
      const o = JSON.parse(t);
      const s = o.intent || o.goal || o.title || o.description || o.task_id || "";
      if (s) return String(s).trim();
    } catch {
      // Partial/invalid JSON — best-effort regex extraction.
      const m = t.match(/"(?:intent|goal|title|description)"\s*:\s*"((?:[^"\\]|\\.)*)/);
      if (m && m[1]) return m[1].replace(/\\"/g, '"');
    }
    return t;
  }

  function upsertSubagentTask(taskId, fields) {
    if (!taskId) return;
    const prev =
      subagentByTask.get(taskId) ||
      ({
        taskId,
        type: "agent",
        label: taskId,
        status: "running",
        toolCount: 0,
      });
    Object.assign(prev, fields);
    subagentByTask.set(taskId, prev);
    let idx = subagents.findIndex((s) => s.taskId === taskId);
    if (idx < 0 && prev.parentToolCallId) {
      // Upgrade the generic tool-call row (created on task/task_spawn start)
      // in place instead of appending a duplicate.
      idx = subagents.findIndex((s) => !s.taskId && s.id === prev.parentToolCallId);
    }
    const row = {
      id: prev.parentToolCallId || taskId,
      taskId,
      type: prev.type,
      label: prev.label,
      tier: prev.tier,
      model: prev.model,
      status: prev.status,
      error: prev.error,
      parentToolCallId: prev.parentToolCallId,
      toolsEl: prev.toolsEl,
      toolCount: prev.toolCount,
    };
    if (idx >= 0) subagents[idx] = row;
    else subagents.push(row);
    renderSubagents();
  }

  function ensureSubagentToolsHost(taskId, parentToolCallId) {
    const node = subagentByTask.get(taskId);
    if (!node || node.toolsEl) return node?.toolsEl;
    const parentBlock = parentToolCallId ? toolBlocks.get(parentToolCallId) : null;
    if (parentBlock) {
      let host = parentBlock.querySelector(".subagent-tools");
      if (!host) {
        host = document.createElement("div");
        host.className = "subagent-tools";
        host.dataset.taskId = taskId;
        parentBlock.appendChild(host);
      }
      node.toolsEl = host;
      return host;
    }
    if (!messagesEl) return undefined;
    const panel = document.createElement("div");
    panel.className = "subagent-panel";
    panel.dataset.taskId = taskId;
    const tierL = subagentTierLabel(node.tier);
    const tierHtml = tierL
      ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(node.model || "")}">${tierL}</span>`
      : "";
    panel.innerHTML =
      `<div class="subagent-panel-head">` +
      `<span class="subagent-type">${escapeAttr(node.type || "agent")}</span>` +
      tierHtml +
      `<span class="subagent-label">${escapeAttr(node.label || taskId)}</span>` +
      `</div>`;
    const host = document.createElement("div");
    host.className = "subagent-tools";
    panel.appendChild(host);
    messagesEl.appendChild(panel);
    node.toolsEl = host;
    return host;
  }

  function handleChildLifecycle(msg) {
    const taskId = msg.taskId || "";
    if (!taskId) return;
    if (msg.phase === "started") {
      const label = humanizeTaskLabel(msg.content || "");
      upsertSubagentTask(taskId, {
        parentToolCallId: msg.parentToolCallId,
        type: msg.subagentType || "agent",
        tier: msg.tier || "",
        model: msg.model || "",
        label: label.length > 48 ? label.slice(0, 45) + "…" : label || taskId,
        status: "running",
        toolCount: 0,
      });
      ensureSubagentToolsHost(taskId, msg.parentToolCallId);
    } else if (msg.phase === "done") {
      const st =
        msg.status === "done"
          ? "done"
          : msg.status === "error" || msg.status === "timeout"
            ? "error"
            : "done";
      const patch = { status: st };
      if (st === "error" && msg.error) {
        patch.error = String(msg.error);
      }
      const lessonHint = String(msg.lessonPromoteSuggestion || "").trim();
      const playbookHint = String(msg.playbookPromoteSuggestion || "").trim();
      if (lessonHint) patch.lessonPromote = lessonHint;
      if (playbookHint) patch.playbookPromote = playbookHint;
      upsertSubagentTask(taskId, patch);
      const hints = [];
      if (lessonHint) hints.push("lesson_promote");
      if (playbookHint) hints.push("playbook_promote");
      if (hints.length) {
        appendMsg(
          "system",
          `Learning: worker finished with ${hints.join(" + ")} suggestion — Lead should review task_result / call promote tool.`
        );
      }
    }
  }

  function resetTurnState() {
    assistantTurn = null;
    assistantTurnInner = null;
    assistantBubble = null;
    streamRawText = "";
    toolTraceEl = null;
    toolTraceSummary = null;
    reasoningDetails = null;
    reasoningBody = null;
    reasoningStarted = 0;
    turnToolCount = { read: 0, search: 0, write: 0, other: 0 };
    turnToolFirstStart = 0;
    turnToolLastEnd = 0;
  }

  // noteTurnToolStart/End track the group's wall clock. The TUI's tool group
  // shows this in its footer (ui/tui/view/tool_group.go); without it the
  // webview could say a turn ran nine tools but never how long that cost.
  function noteTurnToolStart() {
    if (!turnToolFirstStart) {
      turnToolFirstStart = Date.now();
    }
  }

  function noteTurnToolEnd() {
    turnToolLastEnd = Date.now();
    updateToolTraceSummary();
  }

  /** @returns {HTMLElement | null} */
  function ensureTurn() {
    if (assistantTurnInner) {
      return assistantTurnInner;
    }
    if (!messagesEl) {
      return null;
    }
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return assistantTurnInner;
  }

  function commitPreToolText() {
    flushAssistantMarkdown();
    if (!assistantBubble) {
      streamRawText = "";
      return;
    }
    const visible = sanitizeAssistantStream(stripFinalEnvelope(streamRawText)).trim();
    if (!visible) {
      assistantBubble.remove();
      assistantBubble = null;
      streamRawText = "";
      return;
    }
    assistantBubble.classList.add("turn-text-segment");
    assistantBubble = null;
    streamRawText = "";
  }

  /** @returns {HTMLElement | null} */
  function ensureToolTraceList() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!toolTraceEl) {
      toolTraceEl = document.createElement("details");
      toolTraceEl.className = "tool-trace trace-details";
      toolTraceSummary = document.createElement("summary");
      toolTraceSummary.className = "trace-summary";
      toolTraceSummary.textContent = "Running tools…";
      const list = document.createElement("div");
      list.className = "tool-trace-list";
      toolTraceEl.appendChild(toolTraceSummary);
      toolTraceEl.appendChild(list);
      inner.appendChild(toolTraceEl);
      toolTraceEl.open = false;
    }
    return toolTraceEl.querySelector(".tool-trace-list");
  }

  function bumpToolTraceCount(name) {
    const kind = toolKind(name);
    if (kind === "read" || kind === "list") {
      turnToolCount.read++;
    } else if (kind === "search" || kind === "glob") {
      turnToolCount.search++;
    } else if (kind === "write") {
      turnToolCount.write++;
    } else {
      turnToolCount.other++;
    }
    updateToolTraceSummary();
  }

  function updateToolTraceSummary() {
    if (!toolTraceSummary) {
      return;
    }
    const parts = [];
    if (turnToolCount.read) {
      const n = turnToolCount.read;
      parts.push(`${n} ${n === 1 ? "read" : "reads"}`);
    }
    if (turnToolCount.search) {
      const n = turnToolCount.search;
      parts.push(`${n} ${n === 1 ? "search" : "searches"}`);
    }
    if (turnToolCount.write) {
      const n = turnToolCount.write;
      parts.push(`${n} ${n === 1 ? "edit" : "edits"}`);
    }
    if (turnToolCount.other) {
      const n = turnToolCount.other;
      parts.push(`${n} tool${n === 1 ? "" : "s"}`);
    }
    let text = parts.length ? `Explored ${parts.join(", ")}` : "Tools";
    // Only when a live tool both started and finished: a group still running,
    // or one replayed from history, has no honest number to show.
    if (turnToolFirstStart && turnToolLastEnd > turnToolFirstStart) {
      text += ` · ${formatToolDuration(turnToolLastEnd - turnToolFirstStart)}`;
    }
    toolTraceSummary.textContent = text;
  }

  /** @returns {HTMLElement | null} */
  function ensureReasoning() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!reasoningDetails) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = "Thought briefly";
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      inner.insertBefore(reasoningDetails, toolTraceEl || null);
      reasoningDetails.open = false;
      reasoningStarted = Date.now();
    }
    return reasoningBody;
  }

  function finalizeReasoningSummary() {
    if (!reasoningDetails || !reasoningBody) {
      return;
    }
    const text = (reasoningBody.textContent || "").trim();
    if (!text) {
      reasoningDetails.remove();
      reasoningDetails = null;
      reasoningBody = null;
      return;
    }
    const sum = reasoningDetails.querySelector(".trace-summary");
    if (sum) {
      const sec = reasoningStarted ? Math.round((Date.now() - reasoningStarted) / 1000) : 0;
      sum.textContent = sec >= 2 ? `Thought for ${sec}s` : "Thought briefly";
    }
    reasoningDetails.open = false;
  }

  function messagesHostFor(msg) {
    if (msg.scope === "child" && msg.taskId) {
      const node = subagentByTask.get(msg.taskId);
      if (node) {
        if (!node.toolsEl) {
          ensureSubagentToolsHost(msg.taskId, msg.parentToolCallId || node.parentToolCallId);
        }
        if (node.toolsEl) return node.toolsEl;
      }
    }
    // Write/edit diffs stay in the main turn stream — never inside collapsed tool trace.
    if (toolKind(msg.toolName) === "write") {
      return ensureTurn();
    }
    return ensureToolTraceList() || messagesEl;
  }

  function bindWriteToolHead(block, head) {
    head.classList.add("tool-head-write");
    const chev = head.querySelector(".tool-chev");
    if (chev) chev.remove();
    head.addEventListener("click", (e) => {
      if (e.target.closest?.(".diff-preview-name")) return;
      const fp = block.dataset.filePath || "";
      if (!fp) return;
      const d = findDiffForPath(fp);
      e.preventDefault();
      openDiffMessage(fp, d?.before || "", d?.after || "", /** @type {MouseEvent} */ (e).shiftKey);
    });
  }

  function tryShowWriteDiff(block, name, argsRaw, content) {
    if (!block || toolKind(name) !== "write") return;
    const filePath = toolPathFromArgs(name, argsRaw, content || "");
    if (!filePath) return;
    block.dataset.filePath = filePath;
    block.dataset.argsRaw = argsRaw || "";
    let diff = findDiffForPath(filePath);
    if (!diff) {
      diff = extractDiffFromTool(name, argsRaw, content || "");
    }
    if (diff) {
      rememberBlockDiff(block, diff.before || "", diff.after || "");
    }
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(block, filePath, diff.before || "", diff.after || "");
    } else {
      attachToolDiffShell(block, filePath);
    }
  }

  function basename(path) {
    if (!path) return "";
    const parts = path.replace(/\\/g, "/").split("/");
    return parts[parts.length - 1] || path;
  }

  function normalizePath(p) {
    return (p || "").replace(/\\/g, "/").toLowerCase();
  }

  function pathsMatch(a, b) {
    const na = normalizePath(a);
    const nb = normalizePath(b);
    if (!na || !nb) return false;
    if (na === nb) return true;
    if (na.endsWith("/" + nb) || nb.endsWith("/" + na)) return true;
    const ba = basename(na);
    const bb = basename(nb);
    return ba !== "" && ba === bb;
  }

  function bindPendingReviewListEvents() {
    if (!pendingReviewListEl || pendingReviewListEl.dataset.bound === "1") return;
    pendingReviewListEl.dataset.bound = "1";
    pendingReviewListEl.addEventListener("click", (e) => {
      const t = e.target;
      if (!(t instanceof HTMLElement)) return;
      const item = t.closest(".pending-review-item");
      if (!item) return;
      const idx = Number(item.getAttribute("data-idx"));
      if (!Number.isFinite(idx) || idx < 0 || idx >= pendingState.diff.length) return;
      const d = pendingState.diff[idx];
      if (!d) return;

      const nameBtn = t.closest(".diff-preview-name");
      if (nameBtn) {
        diffReviewCursor = idx;
        renderPendingReviewList();
        if (!d.path) return;
        if (/** @type {MouseEvent} */ (e).altKey) {
          openExternalFile(d.path, true);
          return;
        }
        openDiffMessage(d.path, d.before || "", d.after || "", /** @type {MouseEvent} */ (e).shiftKey);
      }
    });
  }

  function diffReviewActive() {
    return pendingState.diff.length > 0 && document.activeElement !== inputEl;
  }

  async function renderUnifiedDiffLines(container, before, after, filePath, maxLines = 32) {
    const lang = langFromPath(filePath || "");
    const allRows = alignDiffLines(before, after);
    const changedRows = allRows.filter((r) => r.type !== "same");
    const displayRows = changedRows.slice(0, maxLines);
    if (displayRows.length === 0) {
      const hint = document.createElement("div");
      hint.className = "diff-empty-hint";
      hint.textContent =
        (before || "") === (after || "")
          ? "No line changes detected"
          : "Diff preview unavailable";
      container.appendChild(hint);
      return;
    }
    const codeLines = displayRows.map((r) =>
      r.type === "del" ? r.left ?? "" : r.type === "add" ? r.right ?? "" : ""
    );
    const htmlLines = await requestHighlight(codeLines, lang);
    displayRows.forEach((r, idx) => {
      const row = document.createElement("div");
      row.className = "diff-u-row " + r.type;
      const ln = r.type === "del" ? r.leftNum : r.rightNum;
      const gutter = r.type === "del" ? "−" : "+";
      row.innerHTML =
        `<span class="diff-u-ln">${ln || ""}</span>` +
        `<span class="diff-u-gutter">${gutter}</span>` +
        `<span class="diff-u-code">${htmlLines[idx] || "&nbsp;"}</span>`;
      container.appendChild(row);
    });
    if (changedRows.length > maxLines) {
      const more = document.createElement("div");
      more.className = "diff-more";
      more.textContent = `… ${changedRows.length - maxLines} more changed lines`;
      container.appendChild(more);
    }
  }

  async function appendDiffLines(container, before, after, filePath, maxLines = 32) {
    const wrap = document.createElement("div");
    wrap.className = "diff-u-block";
    await renderUnifiedDiffLines(wrap, before, after, filePath, maxLines);
    container.appendChild(wrap);
  }

  async function attachInlineToolDiff(block, filePath, before, after) {
    if (!block || !filePath) return;
    rememberBlockDiff(block, before, after);
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    block.classList.add("write-card-only");
    const stats = countDiffStats(before, after);
    let diffWrap = block.querySelector(".tool-diff-body.diff-preview-card");
    if (!diffWrap) {
      const body = block.querySelector(".tool-body");
      diffWrap = document.createElement("div");
      diffWrap.className = "tool-body tool-diff-body diff-preview-card";
      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(filePath) +
        `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
        diffStatsHtml(stats);
      const lines = document.createElement("div");
      lines.className = "diff-preview-body";
      diffWrap.appendChild(head);
      diffWrap.appendChild(lines);
      if (body) {
        body.replaceWith(diffWrap);
      } else {
        block.appendChild(diffWrap);
      }
      head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
      });
    } else {
      const head = diffWrap.querySelector(".diff-preview-head");
      if (head) {
        head.innerHTML =
          diffExtBadgeHtml(filePath) +
          `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
          diffStatsHtml(stats);
        head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
        });
      }
    }
    const lines = diffWrap.querySelector(".diff-preview-body");
    if (!lines) return;
    lines.classList.remove("tool-diff-pending-body");
    lines.textContent = "";
    await renderUnifiedDiffLines(lines, before, after, filePath, 40);
  }
  function hideOverlay() {
    overlay?.classList.add("hidden");
    if (overlayOptions) overlayOptions.innerHTML = "";
    if (overlayActions) overlayActions.innerHTML = "";
    overlayInput?.classList.add("hidden");
  }

  /** @param {any} request */
  function showPermissionOverlay(request) {
    if (!overlay || !overlayTitle || !overlayBody || !overlayActions) return;
    const isLSP = request.kind === "lsp.install" || request.tool === "lsp.install";
    overlayTitle.textContent = isLSP
      ? "Install language server?"
      : `Allow ${request.tool || "tool"}?`;
    const extra = isLSP ? "Install the language server for this workspace, or skip." : "";
    overlayBody.textContent = [request.description, request.reason, extra]
      .filter(Boolean)
      .join("\n\n");
    overlayActions.innerHTML = "";
    const buttons = isLSP
      ? [
          { label: "Skip", approved: false },
          { label: "Install once", approved: true },
          { label: "Install always", approved: true, always: true },
        ]
      : [
          { label: "Deny", approved: false },
          { label: "Allow once", approved: true },
          { label: "Allow always", approved: true, always: true },
        ];
    buttons.forEach((btn) => {
      const el = document.createElement("button");
      el.type = "button";
      el.className = "pill" + (btn.approved ? " primary" : "");
      el.textContent = btn.label;
      el.addEventListener("click", () => {
        hideOverlay();
        host.postMessage({
          type: "permissionReply",
          approved: btn.approved,
          always: Boolean(btn.always),
        });
      });
      overlayActions.appendChild(el);
    });
    overlay.classList.remove("hidden");
  }

  /** @param {any[]} questions */
  function showQuestionOverlay(questions) {
    if (!questions.length) {
      host.postMessage({ type: "questionReply", answers: [] });
      return;
    }
    questionState = { questions, index: 0, answers: [], mode: "question" };
    renderQuestionStep();
  }

  function renderQuestionStep() {
    const q = questionState.questions[questionState.index];
    if (!q || !overlay || !overlayTitle || !overlayBody || !overlayOptions || !overlayActions) {
      host.postMessage({ type: "questionReply", answers: questionState.answers });
      hideOverlay();
      return;
    }
    overlayTitle.textContent = `Question ${questionState.index + 1}/${questionState.questions.length}`;
    overlayBody.textContent = q.question || "";
    overlayOptions.innerHTML = "";
    overlayActions.innerHTML = "";
    if (q.options && q.options.length) {
      q.options.forEach((opt) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "pill";
        btn.textContent = opt;
        btn.addEventListener("click", () => {
          questionState.answers.push(opt);
          questionState.index += 1;
          renderQuestionStep();
        });
        overlayOptions.appendChild(btn);
      });
    } else {
      overlayInput?.classList.remove("hidden");
      if (overlayInput) overlayInput.value = "";
      const next = document.createElement("button");
      next.type = "button";
      next.className = "pill primary";
      next.textContent = "Next";
      next.addEventListener("click", () => {
        questionState.answers.push(overlayInput?.value || "");
        questionState.index += 1;
        overlayInput?.classList.add("hidden");
        renderQuestionStep();
      });
      overlayActions.appendChild(next);
    }
    overlay.classList.remove("hidden");
  }

  function matchJSONObject(s, start) {
    let depth = 0;
    let inStr = false;
    let esc = false;
    for (let j = start; j < s.length; j++) {
      const c = s[j];
      if (esc) {
        esc = false;
        continue;
      }
      if (c === "\\") {
        esc = true;
        continue;
      }
      if (c === '"') {
        inStr = !inStr;
        continue;
      }
      if (inStr) {
        continue;
      }
      if (c === "{") {
        depth++;
      } else if (c === "}") {
        depth--;
        if (depth === 0) {
          return j;
        }
      }
    }
    return -1;
  }

  /** @param {string} text */
  function sanitizeAssistantStream(text) {
    let t = String(text || "").trim();
    if (!t) return "";
    if (t.startsWith('"') && !t.endsWith('"') && t.length < 400) {
      t = t.replace(/^"+/, "").trim();
    }
    function digitLikeRatio(s) {
      if (!s.length) return 0;
      let n = 0;
      for (const c of s) {
        if (/[\d.eE+\-]/.test(c)) n++;
      }
      return n / s.length;
    }
    function looksCorrupted(s) {
      const x = s.trim();
      if (x.length < 40) return false;
      if (/0{48,}/.test(x)) return true;
      if (x.length >= 120 && digitLikeRatio(x) > 0.75) return true;
      if (/Serving user request/i.test(x) && digitLikeRatio(x.slice(30)) > 0.6) return true;
      return false;
    }
    const numericRun = t.match(/([\d.eE+\-]{80,}|0{32,})/);
    if (numericRun && numericRun.index > 0) {
      t = t.slice(0, numericRun.index).trimEnd().replace(/^"+|"+$/g, "").trim();
    }
    if (looksCorrupted(t)) {
      const prefix = (t.split(/[\d.eE]{20,}/)[0] || "").trim().replace(/^"+|"+$/g, "").trim();
      if (prefix.length > 0 && prefix.length < 240 && !looksCorrupted(prefix)) return prefix;
      return "";
    }
    return t;
  }

  function stripFinalEnvelope(text) {
    let out = text;
    for (;;) {
      const i = out.indexOf("{");
      if (i < 0) {
        return out;
      }
      const end = matchJSONObject(out, i);
      if (end < 0) {
        const tail = out.slice(i);
        if (
          tail.includes('"patches"') ||
          (tail.includes('"type"') && tail.includes('"final"'))
        ) {
          return out.slice(0, i).trimEnd();
        }
        return out;
      }
      const blob = out.slice(i, end + 1);
      if (blob.includes('"patches"')) {
        out = (out.slice(0, i) + out.slice(end + 1)).trim();
        continue;
      }
      return out.slice(0, end + 1) + stripFinalEnvelope(out.slice(end + 1));
    }
  }
  function runStatusLabel() {
    const hint = chromeHint?.textContent?.trim();
    if (hint && !chromeHint.classList.contains("hidden")) {
      return hint;
    }
    let base = busyStatusText || "Working…";
    if (busy && sendQueue.length > 0) {
      const n = sendQueue.length;
      base += ` · ${n} queued`;
    }
    return base;
  }

  function renderSendQueue() {
    if (!messageQueueEl) {
      return;
    }
    if (!sendQueue.length) {
      messageQueueEl.classList.add("hidden");
      messageQueueEl.replaceChildren();
      return;
    }
    messageQueueEl.classList.remove("hidden");
    messageQueueEl.replaceChildren();
    const head = document.createElement("div");
    head.className = "queue-head";
    head.textContent =
      sendQueue.length === 1 ? "1 message queued" : `${sendQueue.length} messages queued`;
    messageQueueEl.appendChild(head);
    sendQueue.forEach((item, idx) => {
      const row = document.createElement("div");
      row.className = "queue-item";
      row.dataset.queueId = item.id;
      const pos = document.createElement("span");
      pos.className = "queue-pos";
      pos.textContent = String(idx + 1);
      const text = document.createElement("span");
      text.className = "queue-text";
      const preview = (item.preview || "").trim();
      text.textContent = preview || (item.fileCount ? `${item.fileCount} attachment(s)` : "…");
      text.title = preview;
      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "queue-cancel";
      rm.setAttribute("aria-label", "Remove from queue");
      rm.textContent = "×";
      rm.addEventListener("click", () => {
        host.postMessage({ type: "cancelQueuedSend", id: item.id });
      });
      row.appendChild(pos);
      row.appendChild(text);
      row.appendChild(rm);
      messageQueueEl.appendChild(row);
    });
    updateBusyUi();
  }

  function beginTurn() {
    assistantBubble = null;
    resetTurnState();
    toolBlocks.clear();
    toolArgs.clear();
    execSteps.clear();
  }

  function setTypingIndicator(show, label) {
    if (!messagesEl) {
      return;
    }
    if (!show) {
      typingIndicatorEl?.remove();
      typingIndicatorEl = null;
      return;
    }
    if (!typingIndicatorEl) {
      typingIndicatorEl = document.createElement("div");
      typingIndicatorEl.className = "msg typing-indicator";
      typingIndicatorEl.setAttribute("aria-label", "Assistant is working");
      typingIndicatorEl.innerHTML =
        '<span class="typing-dots" aria-hidden="true">' +
        '<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>' +
        "</span>" +
        '<span class="typing-label"></span>';
      messagesEl.appendChild(typingIndicatorEl);
    }
    const lab = typingIndicatorEl.querySelector(".typing-label");
    if (lab) {
      lab.textContent = label || "Working…";
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function updateBusyUi() {
    const label = runStatusLabel();
    if (composerStatus) {
      composerStatus.classList.toggle("hidden", !busy);
      composerStatus.setAttribute("aria-busy", busy ? "true" : "false");
    }
    if (composerStatusLabel) {
      composerStatusLabel.textContent = label;
    }
    if (composerWrap) {
      composerWrap.classList.toggle("composer-busy", busy);
    }
    if (sendBtn) {
      sendBtn.classList.toggle("is-busy", busy);
      sendBtn.setAttribute("aria-busy", busy ? "true" : "false");
      sendBtn.title = busy ? "Stop" : "Send";
      sendBtn.setAttribute("aria-label", busy ? "Stop" : "Send");
    }
    setTypingIndicator(busy && !assistantBubble?.textContent?.trim(), label);
  }

  function setChromeHint(text, isError) {
    if (!chromeHint) {
      return;
    }
    const t = (text || "").trim();
    if (!t) {
      chromeHint.textContent = "";
      chromeHint.classList.add("hidden");
      chromeHint.classList.remove("error");
      if (busy) {
        busyStatusText = "Working…";
        updateBusyUi();
      }
      return;
    }
    chromeHint.textContent = t;
    chromeHint.classList.remove("hidden");
    chromeHint.classList.toggle("error", Boolean(isError));
    if (busy && !isError) {
      busyStatusText = t;
      updateBusyUi();
    }
  }

  function setBusy(next) {
    busy = next;
    if (contextBtn) {
      contextBtn.classList.toggle("busy", next);
    }
    if (next) {
      if (!busyStatusText) {
        busyStatusText = "Working…";
      }
    } else {
      busyStatusText = "Working…";
    }
    updateBusyUi();
    if (todos.length) {
      renderTodos();
    }
    if (!next) {
      if (!todosDoneFlashTimer) {
        if (chromeHint) {
          chromeHint.textContent = "";
          chromeHint.classList.add("hidden");
          chromeHint.classList.remove("error");
        }
      }
      if (workflowActiveName) {
        setWorkflow("", false);
      }
    }
  }

  function flashTodosDone() {
    setChromeHint("✓ Tasks done", false);
    clearTimeout(todosDoneFlashTimer);
    todosDoneFlashTimer = window.setTimeout(() => {
      todosDoneFlashTimer = 0;
      setChromeHint("", false);
    }, 2500);
  }

  function truncateTodoLabel(text, max) {
    const t = (text || "").trim();
    if (t.length <= max) {
      return t;
    }
    return t.slice(0, max - 1) + "…";
  }

  function closeMenus() {
    modeMenu?.classList.remove("open");
    effortMenu?.classList.remove("open");
    accessMenu?.classList.remove("open");
    sessionMenu?.classList.remove("open");
    modelMenu?.classList.remove("open");
    modeBtn?.classList.remove("open");
    effortBtn?.classList.remove("open");
    accessBtn?.classList.remove("open");
    sessionHistoryBtn?.classList.remove("open");
    modelPill?.classList.remove("open");
    hidePalette();
  }

  function hidePalette() {
    paletteMode = "";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu?.classList.add("hidden");
    if (paletteMenu) paletteMenu.innerHTML = "";
  }

  function detectPaletteQuery(text) {
    const t = text || "";
    const slash = t.match(/(?:^|\s)(\/[\w-]*)$/);
    if (slash) {
      return { mode: "slash", query: slash[1].slice(1).toLowerCase() };
    }
    const at = t.match(/(?:^|\s)@([^\s@]*)$/);
    if (at) {
      return { mode: "mention", query: at[1] };
    }
    return null;
  }

  function filterSlash(query) {
    const q = (query || "").toLowerCase();
    return SLASH_CMDS.concat(SKILL_CMDS).filter((c) => c.cmd.slice(1).startsWith(q));
  }

  function renderPalette(mode, items) {
    if (!paletteMenu) return;
    paletteMode = mode;
    paletteItems = items;
    paletteIndex = 0;
    paletteMenu.innerHTML = "";
    if (!items.length) {
      if (mode === "mention") {
        const head = document.createElement("div");
        head.className = "menu-section palette-head";
        head.textContent = "Files";
        paletteMenu.appendChild(head);
        const empty = document.createElement("div");
        empty.className = "palette-empty";
        empty.textContent = "No files found";
        paletteMenu.appendChild(empty);
        paletteMenu.classList.remove("hidden");
        return;
      }
      hidePalette();
      return;
    }
    if (mode === "mention") {
      const head = document.createElement("div");
      head.className = "menu-section palette-head";
      head.textContent = "Files";
      paletteMenu.appendChild(head);
    }
    items.slice(0, 12).forEach((item, i) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item palette-item" + (i === 0 ? " selected" : "");
      if (mode === "slash") {
        btn.innerHTML = `<span class="palette-cmd">${item.cmd}</span><span class="palette-desc">${item.desc || ""}</span>`;
      } else {
        const kind = item.kind || "binary";
        const thumb =
          kind === "image" && item.previewUri
            ? `<img class="palette-thumb" src="${escapeAttr(item.previewUri)}" alt="" />`
            : `<span class="palette-file-icon">${fileExtLabel(item.ext || item.name)}</span>`;
        btn.innerHTML =
          `${thumb}<span class="palette-file-meta">` +
          `<span class="palette-cmd">${escapeAttr(item.name)}</span>` +
          `<span class="palette-desc">${escapeAttr(relPathDisplay(item.path || ""))}</span>` +
          `</span>`;
      }
      btn.addEventListener("mousedown", (e) => {
        e.preventDefault();
        applyPaletteItem(i);
      });
      paletteMenu.appendChild(btn);
    });
    paletteMenu.classList.remove("hidden");
  }

  function applyPaletteItem(index) {
    const item = paletteItems[index];
    if (!item || !inputEl) return;
    const text = inputEl.value;
    if (paletteMode === "slash") {
      const trimmed = text.replace(/(?:^|\s)(\/[\w-]*)$/, "").trimEnd();
      if (item.cmd === "/compact") {
        inputEl.value = trimmed;
        hidePalette();
        host.postMessage({ type: "slashCommand", cmd: "/compact" });
        return;
      }
      inputEl.value = trimmed;
      hidePalette();
      host.postMessage({ type: "slashCommand", cmd: item.cmd });
      return;
    }
    if (paletteMode === "mention") {
      const base = text.replace(/(?:^|\s)@([^\s@]*)$/, "").trimEnd();
      inputEl.value = base ? base + " " : "";
      addFileRef(item);
      hidePalette();
      autoGrow();
      inputEl.focus();
      return;
    }
  }

  function showMentionLoading() {
    if (!paletteMenu) {
      return;
    }
    paletteMode = "mention";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu.innerHTML =
      '<div class="menu-section palette-head">Files</div>' +
      '<div class="palette-loading">Searching…</div>';
    paletteMenu.classList.remove("hidden");
  }

  function onInputPalette() {
    if (!inputEl) return;
    const hit = detectPaletteQuery(inputEl.value);
    if (!hit) {
      hidePalette();
      return;
    }
    if (hit.mode === "slash") {
      renderPalette("slash", filterSlash(hit.query));
      return;
    }
    showMentionLoading();
    clearTimeout(mentionTimer);
    mentionTimer = window.setTimeout(() => {
      host.postMessage({ type: "mentionSearch", query: hit.query });
    }, hit.query ? 120 : 180);
  }

  function renderTodos() {
    if (!todosBar || !todosList) return;
    if (!todos.length) {
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      todosHadOpen = false;
      todosExpanded = false;
      return;
    }
    const normalized = todos.map((t) => ({
      ...t,
      status: normalizeTodoStatus(t.status),
    }));
    let done = 0;
    let cancelled = 0;
    for (const t of normalized) {
      if (t.status === "done") done++;
      if (t.status === "cancelled") cancelled++;
    }
    const open = normalized.filter((t) => t.status !== "done" && t.status !== "cancelled");
    const total = normalized.length - cancelled;

    if (!open.length) {
      if (done > 0 && todosHadOpen) {
        flashTodosDone();
      }
      todosHadOpen = false;
      todosExpanded = false;
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      return;
    }

    todosHadOpen = true;
    todosBar.classList.remove("hidden");

    const inProg = open.find((t) => t.status === "in_progress");
    const focus = inProg || open[0];
    const focusLabel = truncateTodoLabel(focus.content || focus.id || "Task", 52);
    const spinning = Boolean(inProg && busy);

    if (todosChipGlyph) {
      todosChipGlyph.textContent = inProg ? "◉" : "□";
      todosChipGlyph.classList.toggle("spinning", spinning);
    }
    if (todosChipSummary) {
      todosChipSummary.textContent = `${done}/${total} · ${focusLabel}`;
    }
    if (todosChipChev) {
      todosChipChev.textContent = todosExpanded ? "▴" : "▾";
    }
    if (todosChip) {
      todosChip.setAttribute("aria-expanded", todosExpanded ? "true" : "false");
    }

    if (!todosExpanded) {
      todosList.classList.add("hidden");
      todosList.innerHTML = "";
      return;
    }

    todosList.classList.remove("hidden");
    todosList.innerHTML = "";
    const sorted = [...open].sort((a, b) => {
      const rank = (s) => (s === "in_progress" ? 0 : 1);
      return rank(a.status) - rank(b.status);
    });
    sorted.slice(0, 12).forEach((t) => {
      const row = document.createElement("div");
      row.className = "todo-row status-" + t.status;
      row.setAttribute("role", "listitem");
      const glyph = document.createElement("span");
      glyph.className = "todo-glyph" + (t.status === "in_progress" && busy ? " spinning" : "");
      glyph.textContent = todoGlyph(t.status);
      const text = document.createElement("span");
      text.className = "todo-text";
      text.textContent = t.content || t.id;
      row.appendChild(glyph);
      row.appendChild(text);
      todosList.appendChild(row);
    });
    if (open.length > 12) {
      const more = document.createElement("div");
      more.className = "todo-more";
      more.textContent = `+${open.length - 12} more`;
      todosList.appendChild(more);
    }
  }

  function normalizeTodoStatus(status) {
    const s = (status || "pending").toLowerCase();
    if (s === "completed" || s === "complete") return "done";
    if (s === "inprogress" || s === "in-progress") return "in_progress";
    return s;
  }

  function todoGlyph(status) {
    const s = normalizeTodoStatus(status);
    if (s === "done") return "✓";
    if (s === "cancelled") return "×";
    if (s === "in_progress") return "◉";
    return "□";
  }
  function toolKind(name) {
    const n = (name || "").toLowerCase();
    if (["read", "fs.read"].includes(n)) return "read";
    if (["ls", "list", "fs.list"].includes(n)) return "list";
    if (["write", "edit", "fs.write", "file.write_atomic"].includes(n)) return "write";
    if (["grep", "search.text", "search"].includes(n)) return "search";
    if (["glob"].includes(n)) return "glob";
    if (["symbols", "code.symbols"].includes(n)) return "symbols";
    if (["bash", "exec.run", "exec"].includes(n)) return "exec";
    if (["todowrite", "task", "task_spawn"].includes(n)) return "task";
    return "other";
  }

  function toolIcon(name) {
    switch (toolKind(name)) {
      case "read":
        return "→";
      case "list":
        return "≡";
      case "write":
        return "←";
      case "search":
        return "✱";
      case "glob":
        return "✦";
      case "symbols":
        return "◈";
      case "exec":
        return "$";
      case "task":
        return "▣";
      default:
        return "•";
    }
  }

  function toolDisplayName(name) {
    switch (toolKind(name)) {
      case "read":
        return "Read";
      case "list":
        return "Ls";
      case "write":
        return name === "edit" ? "Edit" : "Write";
      case "search":
        return "Grep";
      case "glob":
        return "Glob";
      case "symbols":
        return "Symbols";
      case "exec":
        return "Shell";
      case "task":
        return "Task";
      default:
        return name || "Tool";
    }
  }

  function parseToolArgs(raw) {
    const out = {};
    const text = (raw || "").trim();
    if (!text) return out;
    try {
      return JSON.parse(text);
    } catch {
      const patched = text + '"'.repeat(text.split('"').length % 2 === 0 ? 0 : 1);
      let depth = 0;
      let inStr = false;
      let esc = false;
      for (const ch of patched) {
        if (esc) {
          esc = false;
          continue;
        }
        if (ch === "\\") {
          esc = true;
          continue;
        }
        if (ch === '"') inStr = !inStr;
        else if (!inStr) {
          if (ch === "{") depth++;
          if (ch === "}") depth--;
        }
      }
      let fix = patched;
      for (let i = 0; i < depth; i++) fix += "}";
      try {
        return JSON.parse(fix);
      } catch {
        return out;
      }
    }
  }

  function toolPathFromArgs(name, argsRaw, content) {
    const args = parseToolArgs(argsRaw);
    let path =
      args.path || args.filePath || args.file_path || args.target || "";
    if (!path && content) {
      try {
        const parsed = JSON.parse(content);
        path = parsed.path || parsed.filePath || parsed.file_path || "";
      } catch {
        /* ignore */
      }
    }
    return typeof path === "string" ? path.trim() : "";
  }

  function toolPreviewLine(name, argsRaw, content) {
    const disp = toolDisplayName(name);
    const path = toolPathFromArgs(name, argsRaw, content);
    if (path) {
      return `${disp} ${basename(path)}`;
    }
    if (toolKind(name) === "search" || toolKind(name) === "glob") {
      const args = parseToolArgs(argsRaw);
      const pat = args.pattern || args.query || "";
      return pat ? `${disp} "${pat}"` : disp;
    }
    if (toolKind(name) === "exec") {
      const args = parseToolArgs(argsRaw);
      const cmd = args.command || args.description || "";
      if (cmd) {
        return cmd.length > 56 ? cmd.slice(0, 53) + "…" : cmd;
      }
    }
    if (content && content.length < 80) {
      return `${disp} · ${content}`;
    }
    return disp;
  }

  // formatToolDuration renders a tool's wall time the way the TUI does
  // (ui/tui/view/tool_group.go): sub-second work in ms because "0.4s" hides
  // the difference between 350ms and 450ms, seconds with one decimal, and
  // anything past a minute in m/s because "91.4s" stops being readable.
  function formatToolDuration(ms) {
    if (!Number.isFinite(ms) || ms < 0) return "";
    if (ms < 1000) return Math.round(ms) + "ms";
    // 59950+ rounds to "60.0s", which reads like a bug next to "1m 00s".
    if (ms < 59950) return (ms / 1000).toFixed(1) + "s";
    // Round to whole seconds BEFORE splitting: rounding the remainder
    // separately turns 59999ms into "0m 60s".
    const totalSec = Math.round(ms / 1000);
    return Math.floor(totalSec / 60) + "m " + String(totalSec % 60).padStart(2, "0") + "s";
  }

  function updateToolHead(block, name, argsRaw, content, running) {
    const head = block.querySelector(".tool-head");
    if (!head) return;
    // Durations exist only for tools this session actually watched run.
    // Restored history has none — the session snapshot does not persist
    // them — and replaying a start/complete pair would time the replay, not
    // the tool, printing "0ms" beside every historical call.
    const durEl = head.querySelector(".tool-dur");
    if (durEl) {
      const ms = Number(block.dataset.durationMs);
      durEl.textContent = block.dataset.durationMs ? formatToolDuration(ms) : "";
    }
    const icon = head.querySelector(".tool-icon");
    const label = head.querySelector(".tool-label");
    const sub = head.querySelector(".tool-sub");
    const statsEl = head.querySelector(".tool-stats");
    const filePath = toolPathFromArgs(name, argsRaw, content);
    if (filePath) {
      block.dataset.filePath = filePath;
    }
    if (icon) icon.textContent = running ? toolIcon(name) : "✓";
    if (label) label.textContent = toolPreviewLine(name, argsRaw, content);
    if (sub) {
      sub.textContent = filePath && filePath.includes("/") ? filePath : "";
    }
    if (statsEl && filePath && toolKind(name) === "write") {
      const diff = findDiffForPath(filePath);
      if (diff) {
        statsEl.innerHTML = statsHtml(countDiffStats(diff.before, diff.after));
      }
    }
  }

  function insertFileChangeCard(path, content, parent) {
    const host = parent || messagesEl;
    if (!host || !path) return;
    if (parent && parent.classList.contains("tool-block")) {
      attachToolDiffShell(parent, path);
      return;
    }
    const diff = findDiffForPath(path);
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(parent || document.createElement("div"), path, diff.before || "", diff.after || "");
      return;
    }
    attachToolDiffShell(parent || document.createElement("div"), path);
  }

  function renderSubagents() {
    if (!subagentsBar || !subagentsTree) return;
    const active = subagents.filter((s) => s.status === "running" || s.status === "waiting");
    if (!subagents.length) {
      subagentsBar.classList.add("hidden");
      subagentsTree.innerHTML = "";
      return;
    }
    subagentsBar.classList.remove("hidden");
    subagentsTree.innerHTML = "";
    subagents.slice(-12).forEach((sa) => {
      const row = document.createElement("div");
      row.className = "subagent-row status-" + sa.status;
      const icon = sa.status === "done" ? "✓" : sa.status === "error" ? "✗" : sa.status === "waiting" ? "⏳" : "◉";
      const toolHint = sa.toolCount ? ` · ${sa.toolCount} tools` : "";
      const tierL = subagentTierLabel(sa.tier);
      const tierHtml = tierL
        ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(sa.model || "")}">${tierL}</span>`
        : "";
      row.innerHTML =
        `<span class="subagent-icon">${icon}</span>` +
        `<span class="subagent-type">${escapeAttr(sa.type || "agent")}</span>` +
        tierHtml +
        `<span class="subagent-label">${escapeAttr(sa.label || sa.taskId || sa.id)}${toolHint}</span>`;
      if (sa.taskId) {
        row.dataset.taskId = sa.taskId;
        row.title = sa.model ? `${sa.taskId} · ${sa.model}` : sa.taskId;
      }
      if (sa.status === "error" && sa.error) {
        row.title = row.title ? `${row.title}\n${sa.error}` : sa.error;
      }
      const promoteBits = [];
      if (sa.lessonPromote) promoteBits.push("lesson↑");
      if (sa.playbookPromote) promoteBits.push("playbook↑");
      if (promoteBits.length) {
        row.innerHTML +=
          `<span class="subagent-promote-badge" title="${escapeAttr(
            (sa.lessonPromote || sa.playbookPromote || "").slice(0, 240)
          )}">${promoteBits.join(" · ")}</span>`;
      }
      subagentsTree.appendChild(row);
      if (sa.status === "error" && sa.error) {
        const errHint = document.createElement("div");
        errHint.className = "subagent-nested-hint subagent-error-hint";
        const short = String(sa.error);
        errHint.textContent = short.length > 120 ? short.slice(0, 117) + "…" : short;
        errHint.title = String(sa.error);
        subagentsTree.appendChild(errHint);
      }
      const node = sa.taskId ? subagentByTask.get(sa.taskId) : null;
      if (node?.toolsEl && node.toolCount > 0) {
        const nest = document.createElement("div");
        nest.className = "subagent-nested-hint";
        nest.textContent = `${node.toolCount} tool step${node.toolCount === 1 ? "" : "s"} (in chat)`;
        subagentsTree.appendChild(nest);
      }
    });
    if (subagents.length > 12) {
      const more = document.createElement("div");
      more.className = "subagent-more";
      more.textContent = `+${subagents.length - 12} more`;
      subagentsTree.appendChild(more);
    }
  }

  function trackSubagent(msg, phase, argsRaw) {
    const name = msg.toolName || "";
    if (!["task", "task_spawn", "task_wait", "task_cancel"].includes(name)) {
      return;
    }
    const id = msg.toolCallId || `${name}-${msg.step ?? subagents.length}`;
    if (phase === "start") {
      subagents.push({
        id,
        type: "agent",
        label: toolDisplayName(name),
        status: name === "task_wait" ? "waiting" : "running",
      });
    } else if (phase === "update" || phase === "complete") {
      const sa = subagents.find((s) => s.id === id);
      if (!sa) return;
      const args = parseToolArgs(argsRaw || "");
      const st = args.subagent_type || args.subagentType || sa.type;
      if (st) sa.type = String(st);
      if (args.tier) sa.tier = String(args.tier);
      if (args.model) sa.model = String(args.model);
      let desc = humanizeTaskLabel(args.description || args.prompt || args.goal || "");
      if (!desc && Array.isArray(args.workorders) && args.workorders.length) {
        const first = args.workorders[0] || {};
        const wo = first.intent || first.title || first.goal || first.description || "";
        desc = wo
          ? args.workorders.length > 1
            ? `${wo} (+${args.workorders.length - 1})`
            : String(wo)
          : `${args.workorders.length} workorders`;
      }
      if (desc) {
        const d = String(desc).trim();
        sa.label = d.length > 48 ? d.slice(0, 45) + "…" : d;
      }
      if (phase === "complete" && !sa.taskId) {
        // Rows adopted by a child (taskId set) get their status from
        // childLifecycle events; a completed spawn call must not override it.
        const err = (msg.content || "").toLowerCase().startsWith("error");
        sa.status = err ? "error" : "done";
      }
    }
    renderSubagents();
  }

  function renderWorkflowStages() {
    if (!workflowStagesEl) return;
    workflowStagesEl.innerHTML = "";
    for (const st of workflowStages.values()) {
      const pill = document.createElement("span");
      pill.className = "workflow-pill state-" + st.state;
      const glyph = st.state === "done" ? "✓" : st.state === "running" ? "⋯" : "○";
      pill.textContent = `${glyph} ${st.name || st.id}`;
      pill.title = st.id;
      workflowStagesEl.appendChild(pill);
    }
  }

  function setWorkflow(label, active) {
    if (!workflowBar || !workflowLabel) return;
    if (!active) {
      workflowBar.classList.add("hidden");
      workflowLabel.textContent = "";
      workflowActiveName = "";
      workflowStages.clear();
      renderWorkflowStages();
      return;
    }
    workflowBar.classList.remove("hidden");
    workflowLabel.textContent = label;
  }

  function formatTok(n) {
    const v = Math.max(0, Math.round(n || 0));
    if (v >= 1_000_000) return (v / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
    if (v >= 1000) return (v / 1000).toFixed(1).replace(/\.0$/, "") + "K";
    return String(v);
  }

  function resetContextUsage() {
    ctxState.prompt = 0;
    ctxState.completion = 0;
    ctxState.estimated = false;
    ctxState.breakdown = [];
    renderContextUi();
  }

  /** Category colors for the context breakdown (Cursor-like palette). */
  const CTX_COLORS = {
    system: "#8b8b8b", // grey
    tools: "#a78bfa", // purple
    rules: "#34d399", // green
    skills: "#fbbf24", // orange
    conversation: "#f472b6", // pink
    completion: "#60a5fa", // blue
    reserved: "#4a4a4a", // dark grey
  };

  /**
   * Rows for the context popover. Uses the agent's per-category breakdown when
   * available; the conversation row absorbs the difference between the real
   * prompt total and the fixed categories so the numbers always add up.
   */
  function contextRowsData() {
    const used = ctxState.prompt;
    const bd = Array.isArray(ctxState.breakdown) ? ctxState.breakdown : [];
    const rows = [];
    if (bd.length > 0) {
      let fixedSum = 0;
      let convRow = null;
      bd.forEach((c) => {
        if (c.key === "conversation") {
          convRow = { key: c.key, label: c.label || "Conversation", tokens: c.tokens };
        } else {
          fixedSum += c.tokens;
          rows.push({ key: c.key, label: c.label || c.key, tokens: c.tokens });
        }
      });
      let conv = convRow ? convRow.tokens : 0;
      if (used > 0) {
        conv = Math.max(conv, used - fixedSum);
      }
      rows.push({ key: "conversation", label: "Conversation", tokens: Math.max(0, conv) });
    } else if (used > 0) {
      rows.push({ key: "conversation", label: "Prompt context", tokens: used });
    }
    if (ctxState.completion > 0) {
      rows.push({ key: "completion", label: "Completion", tokens: ctxState.completion });
    }
    return rows;
  }

  function renderContextUi() {
    const used = ctxState.prompt;
    const limit = ctxState.limit || 128000;
    const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
    if (contextRingFill) {
      contextRingFill.style.setProperty("--ctx-pct", String(pct));
    }
    if (ctxPct) {
      ctxPct.textContent = used > 0 ? `${pct}% full` : "—";
    }
    if (ctxSummary) {
      const est = ctxState.estimated ? "~" : "";
      ctxSummary.textContent =
        used > 0 ? `${est}${formatTok(used)} / ${formatTok(limit)} tokens` : `Up to ${formatTok(limit)} tokens`;
    }
    const rows = contextRowsData();
    if (ctxBar) {
      // The bar spans the whole context window: colored segments for each
      // category, a dark segment for the reserved reply budget, the rest of
      // the track is free space.
      ctxBar.innerHTML = "";
      const total = Math.max(limit, 1);
      const segs = rows.slice();
      if (ctxState.maxResponse > 0) {
        segs.push({ key: "reserved", label: "Reserved for reply", tokens: ctxState.maxResponse });
      }
      segs.forEach((r) => {
        if (r.tokens <= 0) return;
        const seg = document.createElement("div");
        seg.className = "ctx-bar-seg";
        seg.style.width = `${Math.min(100, Math.max(0.8, (r.tokens / total) * 100))}%`;
        seg.style.background = CTX_COLORS[r.key] || "#888888";
        seg.title = `${r.label}: ${formatTok(r.tokens)}`;
        ctxBar.appendChild(seg);
      });
    }
    if (ctxRows) {
      ctxRows.innerHTML = "";
      const items = rows.slice();
      items.push({ key: "reserved", label: "Reserved for reply", tokens: ctxState.maxResponse });
      items.forEach((item) => {
        if (item.tokens <= 0 && item.key !== "reserved") return;
        const row = document.createElement("div");
        row.className = "ctx-row";
        row.innerHTML = `<span class="ctx-swatch" style="background:${CTX_COLORS[item.key] || "#888888"}"></span><span class="ctx-row-label">${item.label}</span><span class="ctx-row-val">${formatTok(item.tokens)}</span>`;
        ctxRows.appendChild(row);
      });
    }
  }

  function showContextPopover(show) {
    ctxPopoverOpen = show;
    contextPopover?.classList.toggle("hidden", !show);
    contextBtn?.classList.toggle("open", show);
  }

  function updateStatusFooter() {
    /* tokens shown in context popover */
  }

  function setLspStatus(st) {
    if (!statusLsp) return;
    const label = (st || "").trim();
    statusLsp.textContent = label ? `LSP ${label}` : "";
    statusLsp.classList.toggle("active", label === "active");
  }

  function imageGalleryFromList(fileList, activePath) {
    if (!Array.isArray(fileList)) return [];
    return fileList
      .filter((f) => f && f.kind === "image")
      .map((f) => ({
        name: f.name || "Image",
        path: f.path,
        previewUri: f.previewUri,
      }));
  }

  function renderImagePreviewAt(index) {
    if (!imagePreview || !imagePreviewImg || !imagePreviewState.items.length) return;
    const total = imagePreviewState.items.length;
    const idx = ((index % total) + total) % total;
    imagePreviewState.index = idx;
    const item = imagePreviewState.items[idx];
    if (imagePreviewTitle) {
      imagePreviewTitle.textContent = item.name || "Image";
    }
    if (imagePreviewCounter) {
      imagePreviewCounter.textContent = total > 1 ? `${idx + 1} / ${total}` : "";
    }
    if (imagePreviewPrevBtn) {
      imagePreviewPrevBtn.classList.toggle("hidden", total <= 1);
    }
    if (imagePreviewNextBtn) {
      imagePreviewNextBtn.classList.toggle("hidden", total <= 1);
    }
    imagePreviewImg.src = item.previewUri || "";
    imagePreviewImg.alt = item.name || "Preview";
    imagePreview.classList.remove("hidden");
  }

  function showImagePreview(name, previewUri, filePath, galleryItems) {
    if (!imagePreview || !imagePreviewImg) return;
    const items = Array.isArray(galleryItems) && galleryItems.length
      ? galleryItems.filter((g) => g && g.previewUri)
      : [{ name: name || "Image", path: filePath || "", previewUri: previewUri || "" }];
    imagePreviewState.items = items;
    const startIdx = Math.max(
      0,
      items.findIndex((g) => g.path && filePath && g.path === filePath)
    );
    renderImagePreviewAt(startIdx >= 0 ? startIdx : 0);
  }

  function hideImagePreview() {
    imagePreview?.classList.add("hidden");
    if (imagePreviewImg) {
      imagePreviewImg.src = "";
    }
    imagePreviewState = { items: [], index: 0 };
  }

  function shiftImagePreview(delta) {
    if (!imagePreviewState.items.length) return;
    renderImagePreviewAt(imagePreviewState.index + delta);
  }

  async function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === "string" ? reader.result : "";
        const comma = result.indexOf(",");
        resolve(comma >= 0 ? result.slice(comma + 1) : result);
      };
      reader.onerror = () => reject(reader.error || new Error("read failed"));
      reader.readAsDataURL(file);
    });
  }

  async function attachFilesFromDataTransfer(dt) {
    if (!dt) return;
    const fileItems = [];
    if (dt.files && dt.files.length) {
      for (let i = 0; i < dt.files.length; i++) {
        fileItems.push(dt.files[i]);
      }
    }
    for (const file of fileItems) {
      if (!file) continue;
      if (file.size > MAX_ATTACH_BYTES) {
        appendMsg("system", `Skipped ${file.name || "file"}: exceeds 20 MB limit`);
        continue;
      }
      try {
        const dataBase64 = await readFileAsBase64(file);
        host.postMessage({
          type: "attachBytes",
          name: file.name || "attachment",
          mime: file.type || undefined,
          dataBase64,
        });
      } catch {
        /* ignore single file failure */
      }
    }
  }

  function onAttachmentClick(f, _isImage, e) {
    if (!f?.path) return;
    openExternalFile(f.path, Boolean(e?.shiftKey));
  }

  function renderMsgAttachments(parent, fileList) {
    if (!parent || !fileList || !fileList.length) return;
    const wrap = document.createElement("div");
    wrap.className = "msg-attachments";
    fileList.forEach((f) => {
      if (!f || typeof f.name !== "string") return;
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "msg-attach" + (isImage ? " msg-attach-image" : "");
      card.title = (f.path || f.name) + " · click: open in editor · Shift+click: focus";

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "msg-attach-thumb";
        img.src = f.previewUri || "";
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "msg-attach-meta";
      const name = document.createElement("span");
      name.className = "msg-attach-name";
      name.textContent = f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        onAttachmentClick(f, isImage, e);
      });
      wrap.appendChild(card);
    });
    parent.appendChild(wrap);
  }

  /** @param {string} text @param {{ reasoning?: string; toolBlocks?: any[] }} opts */
  function appendHistoryAssistantTurn(text, opts) {
    if (!messagesEl) {
      return;
    }
    resetTurnState();
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "assistant-turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);

    const reasoning = (opts?.reasoning || "").trim();
    if (reasoning) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = "Thought briefly";
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningBody.textContent = reasoning;
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      assistantTurnInner.appendChild(reasoningDetails);
      reasoningDetails.open = false;
    }

    const tools = Array.isArray(opts?.toolBlocks) ? opts.toolBlocks : [];
    for (const tb of tools) {
      const id = tb.id || `${tb.name}-${toolBlocks.size}`;
      handleToolBlock({ phase: "start", toolCallId: id, toolName: tb.name || "tool", restored: true });
      if (tb.argsRaw) {
        handleToolBlock({
          phase: "update",
          toolCallId: id,
          toolName: tb.name || "tool",
          argsDelta: tb.argsRaw,
          restored: true,
        });
      }
      handleToolBlock({
        phase: "complete",
        toolCallId: id,
        toolName: tb.name || "tool",
        content: tb.result || "",
        diagnostics: tb.diagnostics,
        durationMs: tb.durationMs,
        restored: true,
      });
      if (toolKind(tb.name) === "write" && (tb.diffBefore !== undefined || tb.diffAfter !== undefined)) {
        const block = toolBlocks.get(id);
        const fp = toolPathFromArgs(tb.name || "", tb.argsRaw || "", tb.result || "");
        if (block && fp) {
          rememberBlockDiff(block, tb.diffBefore || "", tb.diffAfter || "");
          void attachInlineToolDiff(block, fp, tb.diffBefore || "", tb.diffAfter || "");
        }
      }
    }

    if (text) {
      assistantBubble = document.createElement("div");
      applyAssistantMarkdown(assistantBubble, text);
      assistantTurnInner.appendChild(assistantBubble);
    }

    if (toolTraceEl) {
      toolTraceEl.open = false;
    }
    resetTurnState();
    void syncToolDiffPreviews();
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  /** @param {string} role @param {string} text @param {{ uiIndex?: number; files?: any[]; reasoning?: string; toolBlocks?: any[] }} [opts] */
  function appendMsg(role, text, opts) {
    if (!messagesEl) {
      return null;
    }
    const el = document.createElement("div");
    el.className = `msg ${role}`;
    if (role === "user") {
      if (typeof opts?.uiIndex === "number") {
        el.dataset.uiIndex = String(opts.uiIndex);
      }
      if (opts?.files?.length) {
        renderMsgAttachments(el, opts.files);
      }
      const wrap = document.createElement("div");
      wrap.className = "user-wrap";
      const body = document.createElement("div");
      body.className = "user-text";
      body.textContent = text;
      wrap.appendChild(body);
      if (typeof opts?.uiIndex === "number") {
        const rewind = document.createElement("button");
        rewind.type = "button";
        rewind.className = "rewind-btn";
        rewind.title = "Rewind to here";
        rewind.textContent = "↩ Rewind";
        rewind.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          const idx = typeof opts.uiIndex === "number" ? opts.uiIndex : Number(el.dataset.uiIndex);
          if (!Number.isFinite(idx) || idx < 0) {
            return;
          }
          rewind.disabled = true;
          host.postMessage({ type: "rewindToMessage", uiIndex: idx });
        });
        wrap.appendChild(rewind);

        // Rewind truncates this chat; branching keeps it and continues the
        // conversation in a copy, which is what you want when the question is
        // "what if I had asked differently" rather than "undo that".
        //
        // Not on the first message: the branch is everything BEFORE the point
        // (sessionfile.ForkSnapshot), so branching there would produce an
        // empty chat — the core rejects it, and a button that can only fail is
        // worse than no button. Starting over from nothing is /clear.
        if (opts.uiIndex > 0) {
          const fork = document.createElement("button");
          fork.type = "button";
          fork.className = "fork-btn";
          fork.title = "Branch a new chat from here";
          fork.textContent = "⑂ Branch";
          fork.addEventListener("click", (e) => {
            e.preventDefault();
            e.stopPropagation();
            const idx = typeof opts.uiIndex === "number" ? opts.uiIndex : Number(el.dataset.uiIndex);
            if (!Number.isFinite(idx) || idx < 1) {
              return;
            }
            fork.disabled = true;
            host.postMessage({ type: "forkFromMessage", uiIndex: idx });
          });
          wrap.appendChild(fork);
        }
      }
      if (text || wrap.querySelector(".rewind-btn")) {
        el.appendChild(wrap);
      }
    } else if (role === "system") {
      el.className = "msg system";
      el.textContent = text;
    } else if (role === "assistant" && (opts?.toolBlocks?.length || opts?.reasoning)) {
      appendHistoryAssistantTurn(text, opts);
      return null;
    } else if (role === "assistant") {
      applyAssistantMarkdown(el, text);
    } else {
      el.textContent = text;
    }
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  /** @param {{ phase: string; toolCallId?: string; toolName: string; content?: string; argsDelta?: string; step?: number; diagnostics?: any[]; restored?: boolean; durationMs?: number }} msg */
  function handleToolBlock(msg) {
    if (!messagesEl) return;
    const id = toolBlockKey(msg);
    const kind = toolKind(msg.toolName);
    const host = messagesHostFor(msg);

    if (msg.phase === "start") {
      if (msg.scope !== "child") {
        commitPreToolText();
        bumpToolTraceCount(msg.toolName);
      } else {
        assistantBubble = null;
      }
      toolArgs.set(id, "");
      const block = document.createElement("div");
      block.className = `tool-block running kind-${kind}` + (msg.scope === "child" ? " child-tool" : "");
      block.dataset.toolId = id;
      // Restored history replays start/complete back to back, so timing it
      // would measure the replay. Only live tools carry a start stamp.
      if (!msg.restored) {
        block.dataset.startedAt = String(Date.now());
        noteTurnToolStart();
      }
      if (msg.taskId) block.dataset.taskId = msg.taskId;
      if (typeof msg.step === "number") {
        block.dataset.step = String(msg.step);
        if (msg.scope !== "child") {
          execSteps.set(msg.step, block);
        }
      }
      if (kind === "write" && msg.scope !== "child") {
        block.classList.add("write-card-only");
        (host || messagesEl).appendChild(block);
        toolBlocks.set(id, block);
        if (msg.scope === "child" && msg.taskId) {
          const node = subagentByTask.get(msg.taskId);
          if (node) {
            node.toolCount = (node.toolCount || 0) + 1;
            renderSubagents();
          }
        } else {
          trackSubagent(msg, "start", "");
        }
        // A restored turn is a replay, not work in progress: narrating it
        // leaves "Ls…" in the chrome strip of a session that is sitting idle.
        if (!msg.restored) {
          setChromeHint(toolDisplayName(msg.toolName) + "…", false);
        }
        if (host) host.scrollTop = host.scrollHeight;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        return;
      }
      const head = document.createElement("button");
      head.type = "button";
      head.className = "tool-head";
      head.innerHTML =
        `<span class="tool-icon">${toolIcon(msg.toolName)}</span>` +
        `<span class="tool-label">${escapeAttr(toolDisplayName(msg.toolName))}</span>` +
        `<span class="tool-sub"></span>` +
        `<span class="tool-dur"></span>` +
        `<span class="tool-stats"></span>` +
        `<span class="tool-spinner"></span>` +
        (kind === "write" ? "" : `<span class="tool-chev">▾</span>`);
      let body = null;
      if (kind !== "write") {
        body = document.createElement("pre");
        body.className = "tool-body hidden";
        head.addEventListener("click", (e) => {
          const stats = e.target.closest?.(".tool-stats");
          const fp = block.dataset.filePath || "";
          if (stats && fp && stats.textContent.trim()) {
            e.preventDefault();
            e.stopPropagation();
            const d = findDiffForPath(fp);
            openDiffMessage(fp, d?.before || "", d?.after || "", e.shiftKey);
            return;
          }
          body.classList.toggle("hidden");
          head.classList.toggle("open");
        });
      } else {
        bindWriteToolHead(block, head);
      }
      block.appendChild(head);
      if (body) block.appendChild(body);
      (host || messagesEl).appendChild(block);
      toolBlocks.set(id, block);
      if (msg.scope === "child" && msg.taskId) {
        const node = subagentByTask.get(msg.taskId);
        if (node) {
          node.toolCount = (node.toolCount || 0) + 1;
          renderSubagents();
        }
      } else {
        trackSubagent(msg, "start", "");
      }
      // A restored turn is a replay, not work in progress: narrating it
        // leaves "Ls…" in the chrome strip of a session that is sitting idle.
        if (!msg.restored) {
          setChromeHint(toolDisplayName(msg.toolName) + "…", false);
        }
      if (host) host.scrollTop = host.scrollHeight;
      messagesEl.scrollTop = messagesEl.scrollHeight;
      return;
    }

    const block = toolBlocks.get(id);
    if (!block) return;

    if (msg.phase === "update" && msg.argsDelta) {
      const prev = toolArgs.get(id) || "";
      const next = prev + msg.argsDelta;
      toolArgs.set(id, next);
      updateToolHead(block, msg.toolName, next, "", true);
      if (toolKind(msg.toolName) === "write") {
        tryShowWriteDiff(block, msg.toolName, next, "");
      }
      if (msg.scope !== "child") {
        trackSubagent(msg, "update", next);
      }
      syncToolDiffStats();
      return;
    }

    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    const argsRaw = toolArgs.get(id) || "";

    if (msg.phase === "complete") {
      block.classList.remove("running");
      block.classList.add("done", `kind-${kind}`);
      const spinner = block.querySelector(".tool-spinner");
      if (spinner) spinner.remove();
      if (block.dataset.startedAt) {
        block.dataset.durationMs = String(Date.now() - Number(block.dataset.startedAt));
        noteTurnToolEnd();
      } else if (typeof msg.durationMs === "number" && msg.durationMs > 0) {
        // Restored from the session snapshot: the tool was timed when it ran,
        // by whichever surface ran it (sessionfile.UIToolBlock.duration_ms).
        block.dataset.durationMs = String(msg.durationMs);
      }
      updateToolHead(block, msg.toolName, argsRaw, msg.content || "", false);
      if (head && kind !== "write") head.classList.remove("open");

      if (body && msg.content && kind !== "write") {
        body.textContent = msg.content.length > 8000 ? msg.content.slice(0, 8000) + "\n…" : msg.content;
        body.classList.add("hidden");
      }

      if (msg.diagnostics && msg.diagnostics.length) {
        const diag = document.createElement("div");
        diag.className = "tool-diags";
        msg.diagnostics.slice(0, 6).forEach((d) => {
          const line = document.createElement("div");
          line.className = "tool-diag " + (d.severity || "");
          line.textContent = `L${d.start_line}: ${d.message}`;
          diag.appendChild(line);
        });
        block.appendChild(diag);
      }

      if (kind === "write") {
        tryShowWriteDiff(block, msg.toolName, argsRaw, msg.content || "");
      }

      if (msg.scope !== "child") {
        trackSubagent(msg, "complete", argsRaw);
      }
      syncToolDiffStats();
      toolArgs.delete(id);
      assistantBubble = null;
      if (busy) {
        setChromeHint("", false);
      }
    }
    if (host) host.scrollTop = host.scrollHeight;
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function appendExecChunk(step, chunk) {
    const block = execSteps.get(step);
    if (!block) return;
    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    if (!body) return;
    body.classList.remove("hidden");
    if (head) head.classList.add("open");
    body.textContent = (body.textContent || "") + chunk;
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
  }
  // ---- Trajectory view ------------------------------------------------------
  //
  // The tab is a pure function from a session's event log to a tree of rows,
  // drawn by a thin renderer below. The log is what session.trajectory returns
  // and what the core's tee records: `{ seq, time_ms, type, data }` where
  // `type` is the notification method and `data` its params. A live event
  // forwarded mid-turn has the same shape minus seq and time_ms.

  /**
   * @typedef {{
   *   kind: "turn"|"step"|"tool"|"text"|"reasoning"|"error"|"stage"|"pending"|"route"|"other",
   *   depth: number,
   *   key: string,
   *   label: string,
   *   turnId: string,
   *   step?: number,
   *   startMs?: number,
   *   offsetMs?: number,
   *   durationMs?: number,
   *   tokensIn?: number,
   *   tokensOut?: number,
   *   outcome?: string,
   *   input?: string,
   *   output?: string,
   *   live: boolean,
   *   seq?: number
   * }} TrajRow
   */

  /**
   * buildTrajectoryTree turns recorded (or live) events into flat rows with a
   * depth. Pure: no DOM, no fragment state, no clock. Everything it knows
   * about time comes from the events' own core-stamped `time_ms`; a live
   * event has none, and its row says so rather than borrowing the client's.
   *
   * Grouping: turn (by data.turn_id, in order of first appearance) > step (by
   * data.step) > items. Tool calls are keyed by tool_call_id; a child-scoped
   * tool call nests under its parent_tool_call_id. Consecutive text deltas of
   * one kind coalesce into one row. Workflow stages, mode routes and steps sit
   * directly under the turn, in the order they first appeared — which for a recorded log is time order, and needs no timestamp so it holds for live rows too. Token columns come from step_usage only.
   *
   * Self-contained on purpose: trajectory-test.mjs evaluates it standalone.
   *
   * @param {Array<{seq?: number, time_ms?: number, type: string, data?: any}>|null|undefined} events
   * @returns {TrajRow[]}
   */
  function buildTrajectoryTree(events) {
    /** @type {TrajRow[]} */
    const rows = [];
    if (!Array.isArray(events)) return rows;

    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const str = (v) => (typeof v === "string" ? v : "");
    const obj = (v) => (v && typeof v === "object" ? v : {});

    // Turns in order of first appearance. Events without a turn_id (older
    // cores, one-shot agent.run) share one unnamed turn so nothing is dropped.
    const turns = new Map();
    let turnOrdinal = 0;
    const turnFor = (id) => {
      let t = turns.get(id);
      if (!t) {
        turnOrdinal++;
        t = {
          key: "turn:" + id,
          id,
          ordinal: turnOrdinal,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: "open",
          steps: new Map(), // step number -> step node, for lookup
          depth1: [], // steps, stages and routes in the order they first appeared — which is time order
        };
        turns.set(id, t);
      }
      return t;
    };
    const stepFor = (t, n) => {
      const key = String(n);
      let s = t.steps.get(key);
      if (!s) {
        s = {
          kind: "step",
          key: t.key + "/step:" + key,
          n,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: undefined,
          tokensIn: undefined,
          tokensOut: undefined,
          items: [], // rows under the step, in order
          tools: new Map(), // tool_call_id -> tool node (parent and child scope alike)
          openTool: null, // last parent-scope tool started and not yet completed
          lastText: null, // for coalescing message_delta / reasoning_delta
        };
        t.steps.set(key, s);
        t.depth1.push(s);
      }
      return s;
    };
    // Widen a node's [startMs, endMs] to include ms, and mark it live if ms is unknown.
    const touch = (node, ms, live) => {
      if (live) node.live = true;
      if (ms === undefined) return;
      if (node.startMs === undefined || ms < node.startMs) node.startMs = ms;
      if (node.endMs === undefined || ms > node.endMs) node.endMs = ms;
    };

    for (const e of events) {
      if (!e || typeof e !== "object") continue;
      const method = str(e.type);
      const d = obj(e.data);
      const ms = num(e.time_ms);
      const live = ms === undefined;
      const seq = num(e.seq);
      const t = turnFor(str(d.turn_id));
      touch(t, ms, live);

      if (method === "workflow/stage_start" || method === "workflow/stage_done") {
        const stageKey = t.key + "/stage:" + str(d.stage_id) + "#" + (num(d.attempt) ?? 0);
        let st = t.depth1.find((c) => c.kind === "stage" && c.key === stageKey);
        if (!st) {
          st = {
            kind: "stage",
            key: stageKey,
            label: str(d.stage_id) || str(d.name) || "stage",
            startMs: undefined,
            endMs: undefined,
            live: false,
            outcome: undefined,
            seq,
          };
          t.depth1.push(st);
        }
        touch(st, ms, live);
        if (method === "workflow/stage_done") st.outcome = str(d.marker) || str(d.action) || "done";
        continue;
      }

      const s = stepFor(t, num(d.step) ?? 0);
      touch(s, ms, live);

      if (method === "exec/output_chunk") {
        const target = s.openTool;
        if (target) {
          target.output += str(d.chunk);
          touch(target, ms, live);
        } else {
          s.items.push({ kind: "other", key: s.key + "/exec:" + s.items.length, label: "shell output", startMs: ms, endMs: ms, live, output: str(d.chunk), seq });
        }
        s.lastText = null;
        continue;
      }

      if (method !== "agent/event") {
        // A notification this view has never heard of stays visible as a
        // plain row: the log outlives the code that reads it.
        s.items.push({ kind: "other", key: s.key + "/" + method + ":" + s.items.length, label: method || "event", startMs: ms, endMs: ms, live, seq });
        s.lastText = null;
        continue;
      }

      const kind = str(d.type);
      const isChild = d.scope === "child";
      const parentToolId = str(d.parent_tool_call_id);

      switch (kind) {
        case "message_delta":
        case "reasoning_delta": {
          if (isChild) break; // a subagent's text belongs to its own trace, not this step's
          const k = kind === "reasoning_delta" ? "reasoning" : "text";
          if (s.lastText && s.lastText.kind === k) {
            s.lastText.output += str(d.content);
            touch(s.lastText, ms, live);
          } else {
            const row = { kind: k, key: s.key + "/" + k + ":" + s.items.length, label: k === "reasoning" ? "reasoning" : "response", startMs: ms, endMs: ms, live, output: str(d.content), seq };
            s.items.push(row);
            s.lastText = row;
          }
          break;
        }
        case "tool_call_start": {
          const id = str(d.tool_call_id) || "idx" + (num(d.tool_call_index) ?? s.items.length);
          const row = { kind: "tool", key: s.key + "/tool:" + id, id, label: str(d.tool_call_name) || "tool", startMs: ms, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
          const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
          if (parent) parent.subrows.push(row);
          else s.items.push(row);
          s.tools.set(id, row);
          if (!isChild) s.openTool = row;
          s.lastText = null;
          break;
        }
        case "tool_call_delta": {
          const row = s.tools.get(str(d.tool_call_id));
          if (row) {
            row.input += str(d.args_delta);
            touch(row, ms, live);
          }
          break;
        }
        case "tool_call_completed": {
          const id = str(d.tool_call_id);
          let row = s.tools.get(id);
          const wasFound = !!row;
          if (!row) {
            // Completed without a recorded start: still a row, with no
            // duration to claim.
            row = { kind: "tool", key: s.key + "/tool:" + (id || String(s.items.length)), id, label: str(d.tool_call_name) || "tool", startMs: undefined, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
            const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
            if (parent) parent.subrows.push(row);
            else s.items.push(row);
            if (id) s.tools.set(id, row);
          }
          row.output += str(d.content);
          row.outcome = "done";
          if (wasFound) touch(row, ms, live);
          if (s.openTool === row) s.openTool = null;
          s.lastText = null;
          break;
        }
        case "step_usage": {
          if (isChild) break; // a worker's usage is its own; the step's columns are the main agent's
          const u = obj(d.data);
          if (num(u.prompt_tokens) !== undefined) s.tokensIn = num(u.prompt_tokens);
          if (num(u.completion_tokens) !== undefined) s.tokensOut = num(u.completion_tokens);
          break;
        }
        case "context_estimate":
        case "todos_updated":
        case "child_started":
        case "child_done":
          // Deliberately no row. The estimate is not a measurement and must
          // never reach a token column; todos and child lifecycle are drawn
          // elsewhere in the chat.
          break;
        case "step_done":
          s.outcome = str(d.content) || "done";
          if (s.outcome === "final") t.outcome = "final";
          s.lastText = null;
          break;
        case "done":
          if (t.outcome === "open") t.outcome = "done";
          break;
        case "recoverable_error":
        case "error": {
          s.items.push({ kind: "error", key: s.key + "/err:" + s.items.length, label: kind === "error" ? "error" : "retry", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          if (kind === "error") t.outcome = "error";
          s.lastText = null;
          break;
        }
        case "pending_ops": {
          const p = obj(d.data);
          const n = Array.isArray(p.ops) ? p.ops.length : 0;
          s.items.push({ kind: "pending", key: s.key + "/pending:" + s.items.length, label: n + " pending change" + (n === 1 ? "" : "s") + (p.applied ? " (applied)" : ""), startMs: ms, endMs: ms, live, seq });
          s.lastText = null;
          break;
        }
        case "mode_route": {
          const r = obj(d.data);
          t.depth1.push({ kind: "route", key: t.key + "/route:" + t.depth1.length, label: "mode " + str(r.from) + " → " + str(r.to), startMs: ms, endMs: ms, live, seq, output: str(r.reason) });
          break;
        }
        default:
          s.items.push({ kind: "other", key: s.key + "/" + (kind || "event") + ":" + s.items.length, label: kind || "event", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          s.lastText = null;
      }
    }

    // Flatten to rows. Offsets are relative to the row's own turn.
    const dur = (n) => (n.startMs !== undefined && n.endMs !== undefined ? n.endMs - n.startMs : undefined);
    const off = (t, n) => (t.startMs !== undefined && n.startMs !== undefined ? n.startMs - t.startMs : undefined);
    const push = (t, n, depth, kind, extra) => {
      rows.push(
        Object.assign(
          { kind, depth, key: n.key, label: n.label, turnId: t.id, startMs: n.startMs, offsetMs: off(t, n), durationMs: dur(n), live: Boolean(n.live), seq: n.seq },
          extra || {}
        )
      );
    };
    const pushTool = (t, s, node, depth) => {
      push(t, node, depth, "tool", { step: s.n, input: node.input || undefined, output: node.output || undefined, outcome: node.outcome });
      for (const sub of node.subrows) pushTool(t, s, sub, depth + 1);
    };

    for (const t of turns.values()) {
      push(t, { key: t.key, label: "turn " + t.ordinal, startMs: t.startMs, endMs: t.endMs, live: t.live }, 0, "turn", { outcome: t.outcome });
      for (const n of t.depth1) {
        if (n.kind !== "step") {
          push(t, n, 1, n.kind, { outcome: n.outcome, output: n.output || undefined });
          continue;
        }
        push(t, { key: n.key, label: "step " + n.n, startMs: n.startMs, endMs: n.endMs, live: n.live }, 1, "step", { step: n.n, tokensIn: n.tokensIn, tokensOut: n.tokensOut, outcome: n.outcome });
        for (const it of n.items) {
          if (it.kind === "tool") pushTool(t, n, it, 2);
          else push(t, it, 2, it.kind, { step: n.n, output: it.output || undefined, outcome: it.outcome });
        }
      }
    }
    return rows;
  }

  // ---- rendering ------------------------------------------------------------

  const trajectorySummary = document.getElementById("trajectory-summary");
  const trajectoryRowsEl = document.getElementById("trajectory-rows");
  const trajTimelineEl = document.getElementById("traj-timeline");
  const trajMetricEl = document.getElementById("traj-metric");
  const trajSearchEl = document.getElementById("traj-search");
  const trajPanelEl = document.getElementById("traj-panel");
  const trajPanelKindEl = document.getElementById("traj-panel-kind");
  const trajPanelLocEl = document.getElementById("traj-panel-loc");
  const trajPanelBodyEl = document.getElementById("traj-panel-body");
  const trajPanelTabsEl = document.getElementById("traj-panel-tabs");
  const trajPanelCloseBtn = document.getElementById("traj-panel-close");
  const viewChatBtn = document.getElementById("view-chat-btn");
  const viewTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const appViewEl = document.getElementById("app");

  /** @type {Array<{seq?: number, time_ms?: number, type: string, data?: any}>} */
  let trajEvents = [];
  /** null until the host has answered once; then the log's own recorded flag. */
  let trajRecorded = null;
  /** Set when the host could not fetch the log; shown instead of a false "not recorded". */
  let trajError = "";
  let trajRenderQueued = false;
  /** The row whose details the side panel is showing, by key. */
  let trajSelectedKey = "";
  /** Which of the panel's three tabs is open. */
  let trajTab = "summary";
  /** What the timeline measures: elapsed time, one block per turn, or per call. */
  let trajMetric = "duration";
  /** The row filter typed into the toolbar's search box. */
  let trajQuery = "";
  /** @type {TrajRow[]} The rows of the last render, for the panel to look up. */
  let trajRowsCache = [];

  const TRAJ_BADGE = { turn: "TURN", step: "STEP", tool: "TOOL", text: "TEXT", reasoning: "THINK", error: "ERROR", stage: "STAGE", pending: "DIFF", route: "ROUTE", other: "EVENT" };
  /** Inline diffs run an O(n·m) alignment, so a big file gets its stats and the full viewer instead. */
  const TRAJ_DIFF_LINE_BUDGET = 1200;

  function currentView() {
    return appViewEl && appViewEl.dataset.view === "trajectory" ? "trajectory" : "chat";
  }

  /** @param {string} view */
  function setView(view) {
    const v = view === "trajectory" ? "trajectory" : "chat";
    if (appViewEl) appViewEl.dataset.view = v;
    if (viewChatBtn) viewChatBtn.setAttribute("aria-selected", v === "chat" ? "true" : "false");
    if (viewTrajectoryBtn) viewTrajectoryBtn.setAttribute("aria-selected", v === "trajectory" ? "true" : "false");
  }

  function bindViewSwitch() {
    if (viewChatBtn) viewChatBtn.addEventListener("click", () => setView("chat"));
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("click", () => setView("trajectory"));
    // Arrow keys move between the two segments, as a tablist does. The
    // listeners sit on each button rather than on a shared ancestor found via
    // closest(): the test harness's stub closest() returns null, which is what
    // left C1's rail keyboard path with no automated coverage.
    const onKey = (e) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      const next = currentView() === "chat" ? "trajectory" : "chat";
      setView(next);
      const btn = next === "chat" ? viewChatBtn : viewTrajectoryBtn;
      if (btn) btn.focus();
    };
    if (viewChatBtn) viewChatBtn.addEventListener("keydown", onKey);
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("keydown", onKey);
  }

  function scheduleTrajectoryRender() {
    if (trajRenderQueued) return;
    trajRenderQueued = true;
    requestAnimationFrame(() => {
      trajRenderQueued = false;
      renderTrajectory();
    });
  }

  /** Session switch or clear: forget everything and wait for the host. */
  function resetTrajectory() {
    trajEvents = [];
    trajRecorded = null;
    trajError = "";
    trajSelectedKey = "";
    trajRowsCache = [];
    scheduleTrajectoryRender();
  }

  /** The host fetched session.trajectory: this is now the whole truth. */
  function replaceTrajectory(recorded, events, error) {
    trajRecorded = Boolean(recorded);
    trajEvents = Array.isArray(events) ? events.slice() : [];
    trajError = typeof error === "string" ? error : "";
    scheduleTrajectoryRender();
  }

  /** One live notification. A turn is being recorded now even if the session
   * predated the log, so the "not recorded" answer no longer applies. */
  function appendTrajectoryEvent(ev) {
    if (!ev || typeof ev.type !== "string") return;
    trajEvents.push({ type: ev.type, data: ev.data });
    trajRecorded = true;
    scheduleTrajectoryRender();
  }

  /** Empty the timeline and fold the panel away: there is nothing to point at. */
  function clearTrajChrome() {
    if (trajTimelineEl) trajTimelineEl.innerHTML = "";
    if (trajPanelEl) trajPanelEl.hidden = true;
  }

  /** @param {TrajRow} r @param {string} q */
  function trajRowMatches(r, q) {
    if (!q) return true;
    const haystack = r.kind + " " + r.label + " " + (r.outcome || "") + " " + (r.input || "") + " " + (r.output || "");
    return haystack.toLowerCase().indexOf(q) !== -1;
  }

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajRowsCache = rows;
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = "Trajectory unavailable: " + trajError;
      clearTrajChrome();
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = "No trajectory was recorded for this session — it predates the log.";
      clearTrajChrome();
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = trajRecorded === null ? "Loading trajectory…" : "Nothing has happened in this session yet.";
      clearTrajChrome();
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    const q = trajQuery.trim().toLowerCase();
    const shown = q ? rows.filter((r) => trajRowMatches(r, q)) : rows;
    trajectorySummary.textContent =
      turns + " turn" + (turns === 1 ? "" : "s") + " · " + rows.length + " rows" +
      (liveCount ? " · " + liveCount + " live" : "") +
      (q ? " · " + shown.length + " matching" : "");
    renderTrajTimeline(rows);
    const frag = document.createDocumentFragment();
    for (const r of shown) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
    renderTrajPanel();
  }

  /**
   * The timeline. "duration" lays turns, steps and tool calls on three tracks
   * against one elapsed-time scale, which is the only view where a gap means
   * idle time; "turns" and "calls" give every turn (or every call) the same
   * width, for reading a long session by structure rather than by clock.
   * @param {TrajRow[]} rows
   */
  function renderTrajTimeline(rows) {
    if (!trajTimelineEl) return;
    trajTimelineEl.innerHTML = "";
    const track = (label, items, span) => {
      const line = document.createElement("div");
      line.className = "traj-tl-track";
      const name = document.createElement("span");
      name.className = "traj-tl-name";
      name.textContent = label;
      const bar = document.createElement("div");
      bar.className = "traj-tl-bar";
      items.forEach((r, i) => {
        const block = document.createElement("button");
        block.type = "button";
        block.className = "traj-tl-block";
        block.dataset.kind = r.kind;
        block.dataset.key = r.key;
        if (r.key === trajSelectedKey) block.dataset.selected = "true";
        block.title = r.label + (r.durationMs === undefined ? "" : " · " + formatToolDuration(r.durationMs));
        block.setAttribute("aria-label", r.kind + " " + r.label);
        const box = span(r, i);
        block.style.setProperty("left", box.left + "%");
        block.style.setProperty("width", box.width + "%");
        bar.appendChild(block);
      });
      line.append(name, bar);
      trajTimelineEl.appendChild(line);
    };

    if (trajMetric === "turns" || trajMetric === "calls") {
      const wanted = trajMetric === "turns" ? "turn" : "tool";
      const items = rows.filter((r) => r.kind === wanted);
      const each = items.length ? 100 / items.length : 100;
      track(trajMetric === "turns" ? "Turns" : "Calls", items, (_r, i) => ({
        left: +(i * each).toFixed(3),
        width: +Math.max(each - 0.4, 0.6).toFixed(3),
      }));
      return;
    }

    // Elapsed time: offsets are relative to the row's own turn, so shift each
    // turn by where it starts to put every track on one session-wide scale.
    const turnStart = new Map();
    let base = Infinity;
    for (const r of rows) {
      if (r.startMs === undefined) continue;
      if (r.kind === "turn") turnStart.set(r.turnId, r.startMs);
      if (r.startMs < base) base = r.startMs;
    }
    if (!isFinite(base)) base = 0;
    const at = (r) => {
      if (r.startMs !== undefined) return r.startMs - base;
      const s = turnStart.get(r.turnId);
      return s === undefined ? 0 : s - base + (r.offsetMs || 0);
    };
    let total = 0;
    for (const r of rows) {
      const end = at(r) + (r.durationMs || 0);
      if (end > total) total = end;
    }
    if (total <= 0) total = 1;
    const span = (r) => {
      const left = Math.max(0, Math.min(100, (at(r) / total) * 100));
      const raw = ((r.durationMs || 0) / total) * 100;
      return { left: +left.toFixed(3), width: +Math.max(Math.min(raw, 100 - left), 0.6).toFixed(3) };
    };
    track("Turns", rows.filter((r) => r.kind === "turn"), span);
    track("Steps", rows.filter((r) => r.kind === "step"), span);
    track("Tools", rows.filter((r) => r.kind === "tool"), span);
  }

  /** @param {string} key */
  function selectTrajRow(key) {
    trajSelectedKey = trajSelectedKey === key ? "" : key;
    scheduleTrajectoryRender();
  }

  /** The recorded event a row was built from, when it has one. @param {TrajRow} r */
  function trajSourceEvent(r) {
    if (!r || r.seq === undefined) return null;
    for (const e of trajEvents) {
      if (e && e.seq === r.seq) return e;
    }
    return null;
  }

  function renderTrajPanel() {
    if (!trajPanelEl || !trajPanelBodyEl) return;
    const row = trajRowsCache.find((r) => r.key === trajSelectedKey);
    if (!row) {
      trajPanelEl.hidden = true;
      return;
    }
    trajPanelEl.hidden = false;
    if (trajPanelKindEl) {
      trajPanelKindEl.textContent = TRAJ_BADGE[row.kind] || TRAJ_BADGE.other;
      trajPanelKindEl.dataset.kind = row.kind;
    }
    if (trajPanelLocEl) {
      const bits = [];
      if (row.turnId) bits.push("turn " + row.turnId);
      if (row.step !== undefined) bits.push("step " + row.step);
      trajPanelLocEl.textContent = bits.join(" · ") || row.label;
    }
    if (trajPanelTabsEl && trajPanelTabsEl.querySelectorAll) {
      trajPanelTabsEl.querySelectorAll(".traj-panel-tab").forEach((el) => {
        el.setAttribute("aria-selected", el.getAttribute("data-tab") === trajTab ? "true" : "false");
      });
    }
    trajPanelBodyEl.innerHTML = "";
    const ev = trajSourceEvent(row);
    if (trajTab === "raw") {
      renderTrajRaw(row, ev);
    } else if (trajTab === "preview") {
      renderTrajPreview(row, ev);
    } else {
      renderTrajSummaryTab(row, ev);
    }
  }

  /** @param {string} head @param {string} text */
  function trajPanelPre(head, text) {
    const h = document.createElement("div");
    h.className = "traj-panel-section";
    h.textContent = head;
    const pre = document.createElement("pre");
    pre.className = "traj-pre";
    pre.textContent = text;
    trajPanelBodyEl.append(h, pre);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajSummaryTab(row, ev) {
    const dl = document.createElement("div");
    dl.className = "traj-facts";
    const fact = (k, v) => {
      if (v === "" || v === undefined || v === null) return;
      const key = document.createElement("span");
      key.className = "traj-fact-key";
      key.textContent = k;
      const val = document.createElement("span");
      val.className = "traj-fact-val";
      val.textContent = String(v);
      dl.append(key, val);
    };
    fact("kind", row.kind);
    fact("label", row.label);
    fact("outcome", row.outcome);
    fact("offset", row.offsetMs === undefined ? "" : "+" + formatToolDuration(row.offsetMs));
    fact("duration", row.durationMs === undefined ? "" : formatToolDuration(row.durationMs));
    fact("tokens in", row.tokensIn);
    fact("tokens out", row.tokensOut);
    fact("live", row.live ? "yes" : "");
    fact("event", ev && ev.type ? ev.type + (ev.data && ev.data.type ? " · " + ev.data.type : "") : "");
    fact("seq", row.seq);
    trajPanelBodyEl.appendChild(dl);
    const hasDiff = ev && ev.data && ev.data.data && Array.isArray(ev.data.data.diff) && ev.data.data.diff.length > 0;
    if (!row.input && !row.output && !hasDiff) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "This row records that the event happened; it carries no payload.";
      trajPanelBodyEl.appendChild(note);
    }
  }

  /**
   * Preview shows what the row actually holds: file diffs for a pending-ops
   * row (the only event that carries before/after content), otherwise the
   * tool's arguments and its result. Tool results are truncated by the core
   * at 256 bytes, so the pane says so rather than looking complete.
   * @param {TrajRow} row @param {any} ev
   */
  function renderTrajPreview(row, ev) {
    // The notification nests its own payload: params are {turn_id, step, type,
    // data:{...}}, so a pending_ops diff lives at data.data.diff.
    const payload = ev && ev.data && ev.data.data && typeof ev.data.data === "object" ? ev.data.data : null;
    const diffs = payload && Array.isArray(payload.diff) ? payload.diff : null;
    if (diffs && diffs.length) {
      for (const d of diffs) {
        renderTrajFileDiff(String(d.path || ""), String(d.before || ""), String(d.after || ""));
      }
      return;
    }
    if (row.input) {
      let text = row.input;
      try {
        text = JSON.stringify(JSON.parse(row.input), null, 2);
      } catch (e) {
        // Arguments stream in as fragments, so a mid-turn row holds partial
        // JSON. Showing it verbatim beats showing nothing.
      }
      trajPanelPre("arguments", text);
    }
    if (row.output) {
      trajPanelPre("result", row.output);
    }
    if (!row.input && !row.output) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "Nothing to preview for this row.";
      trajPanelBodyEl.appendChild(note);
    }
  }

  /** @param {string} path @param {string} before @param {string} after */
  function renderTrajFileDiff(path, before, after) {
    const stats = countDiffStats(before, after);
    const head = document.createElement("div");
    head.className = "traj-diff-head";
    const name = document.createElement("span");
    name.className = "traj-diff-path";
    name.textContent = path || "(unnamed file)";
    const count = document.createElement("span");
    count.className = "traj-diff-stats";
    count.textContent = "+" + stats.add + " −" + stats.del;
    const open = document.createElement("button");
    open.type = "button";
    open.className = "traj-diff-open";
    open.textContent = "Open full diff";
    open.addEventListener("click", () => showDiffViewer(path, before, after, ""));
    head.append(name, count, open);
    trajPanelBodyEl.appendChild(head);

    const lineCount = before.split("\n").length + after.split("\n").length;
    if (lineCount > TRAJ_DIFF_LINE_BUDGET) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "The file is too large to align inline (" + lineCount + " lines) — open the full diff.";
      trajPanelBodyEl.appendChild(note);
      return;
    }
    const block = document.createElement("div");
    block.className = "traj-diff";
    for (const line of alignDiffLines(before, after)) {
      if (line.type === "same") continue;
      const el = document.createElement("div");
      el.className = "traj-diff-line traj-diff-" + line.type;
      el.textContent = (line.type === "add" ? "+ " : "− ") + (line.type === "add" ? line.right : line.left);
      block.appendChild(el);
    }
    if (!block.childNodes || block.childNodes.length === 0) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "No line changed in this file.";
      trajPanelBodyEl.appendChild(note);
      return;
    }
    trajPanelBodyEl.appendChild(block);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajRaw(row, ev) {
    if (ev) {
      trajPanelPre("recorded event", JSON.stringify(ev, null, 2));
      return;
    }
    // A live row has no envelope yet: it arrived as a forwarded notification
    // with no seq or time_ms, so the row itself is the whole truth.
    trajPanelPre("row (live — not yet read back from the log)", JSON.stringify(row, null, 2));
  }

  /**
   * One row. Every row opens the side panel — the payload is no longer folded
   * out in place, so a row's height never changes and the list stays scannable.
   * @param {TrajRow} r
   */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.dataset.kind = r.kind;
    if (r.key === trajSelectedKey) el.dataset.selected = "true";
    el.style.setProperty("--traj-depth", String(r.depth));
    el.tabIndex = 0;
    el.setAttribute("role", "button");

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const badge = document.createElement("span");
    badge.className = "traj-badge";
    badge.dataset.kind = r.kind;
    badge.textContent = TRAJ_BADGE[r.kind] || TRAJ_BADGE.other;
    const label = document.createElement("span");
    label.className = "traj-label";
    label.textContent = r.label + (r.outcome && r.kind !== "tool" ? " · " + r.outcome : "");
    const dur = document.createElement("span");
    dur.className = "traj-dur";
    dur.textContent = r.durationMs === undefined ? "" : formatToolDuration(r.durationMs);
    const tok = document.createElement("span");
    tok.className = "traj-tok";
    // Blank unless a provider actually reported a number. Never an estimate.
    tok.textContent =
      (r.tokensIn === undefined ? "" : r.tokensIn + "↑") +
      (r.tokensOut === undefined ? "" : (r.tokensIn === undefined ? "" : " ") + r.tokensOut + "↓");
    el.append(off, badge, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));
    el.addEventListener("click", () => selectTrajRow(r.key));
    el.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectTrajRow(r.key);
      }
    });
    return el;
  }

  function bindTrajectoryChrome() {
    if (trajMetricEl && trajMetricEl.addEventListener) {
      trajMetricEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-metric]") : null;
        if (!btn) return;
        trajMetric = btn.getAttribute("data-metric") || "duration";
        if (trajMetricEl.querySelectorAll) {
          trajMetricEl.querySelectorAll("[data-metric]").forEach((el) => {
            el.setAttribute("aria-selected", el.getAttribute("data-metric") === trajMetric ? "true" : "false");
          });
        }
        scheduleTrajectoryRender();
      });
    }
    if (trajSearchEl && trajSearchEl.addEventListener) {
      trajSearchEl.addEventListener("input", () => {
        trajQuery = trajSearchEl.value || "";
        scheduleTrajectoryRender();
      });
    }
    if (trajTimelineEl && trajTimelineEl.addEventListener) {
      trajTimelineEl.addEventListener("click", (e) => {
        const block = e.target && e.target.closest ? e.target.closest(".traj-tl-block") : null;
        if (!block) return;
        selectTrajRow(block.getAttribute("data-key") || "");
      });
    }
    if (trajPanelTabsEl && trajPanelTabsEl.addEventListener) {
      trajPanelTabsEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-tab]") : null;
        if (!btn) return;
        trajTab = btn.getAttribute("data-tab") || "summary";
        renderTrajPanel();
      });
    }
    if (trajPanelCloseBtn && trajPanelCloseBtn.addEventListener) {
      trajPanelCloseBtn.addEventListener("click", () => {
        trajSelectedKey = "";
        scheduleTrajectoryRender();
      });
    }
  }

  bindTrajectoryChrome();
  bindViewSwitch();
  setView("chat");
  scheduleTrajectoryRender();
  function initModeMenu() {
    if (!modeMenu) {
      return;
    }
    modeMenu.innerHTML = "";
    const byId = new Map(MODES.map((m) => [m.id, m]));
    MODE_GROUPS.forEach((group) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = group.label;
      modeMenu.appendChild(head);
      group.ids.forEach((id) => {
        const m = byId.get(id);
        if (!m) {
          return;
        }
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item";
        btn.dataset.id = m.id;
        btn.title = m.mode;
        btn.innerHTML =
          `<span class="mi mode-icon mode-${escapeAttr(m.id)}">${escapeAttr(m.icon)}</span>${escapeAttr(m.label)}`;
        modeMenu.appendChild(btn);
      });
    });
  }

  function syncModeUi() {
    const m = currentMode();
    if (modeLabel) {
      modeLabel.textContent = m.label;
    }
    const icon = document.getElementById("mode-icon");
    if (icon) {
      icon.textContent = m.icon;
      icon.className = `ico mode-icon mode-${m.id}`;
    }
    if (modeBtn) {
      modeBtn.dataset.mode = modeId;
    }
    const app = document.getElementById("app");
    if (app) {
      app.dataset.mode = modeId;
    }
    modeMenu?.querySelectorAll(".menu-item").forEach((el) => {
      const id = el.getAttribute("data-id");
      el.classList.toggle("selected", id === modeId);
    });
    if (orchConfigBtn) {
      orchConfigBtn.hidden = modeId !== "orchestra";
    }
    // Orchestra routes work across tiers, so a single-model pill would lie
    // about which model actually runs. Swap it for the tier breakdown.
    if (modeId === "orchestra") {
      renderOrchestraPill();
      host.postMessage({ type: "listOrchestraRoles" });
    } else if (modelLabelEl) {
      setModelLabel(currentModel);
      if (modelPill) modelPill.title = "Model";
    }
  }

  /** Models actually configured for one orchestra role. @param {any} r */
  function orchRoleModels(r) {
    if (Array.isArray(r?.models) && r.models.length) return r.models.filter(Boolean);
    return r?.model ? [r.model] : [];
  }

  /** Footer pill in orchestra mode: "L5 <planner> +N" + full map in tooltip. */
  function renderOrchestraPill() {
    if (!modelLabelEl) return;
    const roles = orchestraRolesInfo?.roles || [];
    const planner = roles.find((r) => r.key === "planner");
    const plannerModels = planner ? orchRoleModels(planner) : [];
    const others = roles.filter((r) => r.key !== "planner" && orchRoleModels(r).length > 0);
    if (!roles.length) {
      modelLabelEl.textContent = "Orchestra tiers";
      modelLabelEl.title = "Loading tier map…";
      if (modelPill) modelPill.title = "Orchestra tier models";
      return;
    }
    const base = plannerModels.length
      ? `L5 ${shortModel(plannerModels[0])}`
      : "L5 not set";
    modelLabelEl.textContent = others.length ? `${base} +${others.length}` : base;
    const lines = roles.map((r) => {
      const models = orchRoleModels(r);
      const tier = r.tier ? `${r.tier} · ` : "";
      return `${tier}${r.label}: ${models.length ? models.join(", ") : "— (main model fallback)"}`;
    });
    modelLabelEl.title = lines.join("\n");
    if (modelPill) modelPill.title = "Orchestra tier models";
  }

  /** Read-only tier → models breakdown inside the model dropdown. */
  function renderOrchestraRolesMenu() {
    if (!modelMenuList) return;
    if (modelMenuTitle) modelMenuTitle.textContent = "Orchestra tiers";
    if (modelMenuSearch) modelMenuSearch.style.display = "none";
    modelMenuList.innerHTML = "";
    const roles = orchestraRolesInfo?.roles || [];
    if (!roles.length) {
      const hint = document.createElement("div");
      hint.className = "menu-hint";
      hint.textContent = "Loading tier map…";
      modelMenuList.appendChild(hint);
    }
    roles.forEach((r) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = r.tier ? `${r.label} · ${r.tier}` : r.label;
      modelMenuList.appendChild(head);
      const models = orchRoleModels(r);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = "not set — falls back to the main model";
        modelMenuList.appendChild(empty);
        return;
      }
      models.forEach((id, i) => {
        const row = document.createElement("div");
        row.className = "menu-hint orch-tier-model";
        row.textContent = i === 0 ? id : `${id} (failover ${i + 1})`;
        row.title = id;
        modelMenuList.appendChild(row);
      });
    });
    const cfg = document.createElement("button");
    cfg.type = "button";
    cfg.className = "menu-item";
    cfg.setAttribute("data-model-action", "configure-orchestra");
    cfg.textContent = "Configure tiers…";
    modelMenuList.appendChild(cfg);
  }

  function effortMeterHtml(id) {
    const bars = id === "low" ? 1 : id === "medium" ? 2 : 3;
    let inner = "";
    for (let b = 0; b < bars; b++) {
      inner += "<i></i>";
    }
    return `<span class="effort-meter effort-${escapeAttr(id)}">${inner}</span>`;
  }

  /** @param {HTMLElement} el @param {string} id @param {boolean} [pill] */
  function setEffortIconEl(el, id, pill) {
    el.className = pill ? `ico effort-icon effort-${id}` : `mi effort-icon effort-${id}`;
    el.innerHTML = effortMeterHtml(id);
  }

  function initEffortMenu() {
    if (!effortMenu) {
      return;
    }
    effortMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = "Effort";
    effortMenu.appendChild(head);
    EFFORTS.forEach((e) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.dataset.effort = e.id;
      btn.innerHTML = `<span class="mi effort-icon effort-${escapeAttr(e.id)}">${effortMeterHtml(e.id)}</span>${escapeAttr(e.label)}`;
      effortMenu.appendChild(btn);
    });
    const optHead = document.createElement("div");
    optHead.className = "menu-section";
    optHead.textContent = "Options";
    effortMenu.appendChild(optHead);
    const fastRow = document.createElement("div");
    fastRow.className = "menu-row menu-row-fast";
    fastRow.innerHTML =
      '<span class="menu-row-label"><span class="mi effort-icon effort-fast" aria-hidden="true">⚡</span>Fast</span>' +
      '<button type="button" id="fast-toggle" class="toggle" role="switch" aria-checked="false"></button>';
    effortMenu.appendChild(fastRow);
    fastToggleRef.el = /** @type {HTMLButtonElement | null} */ (document.getElementById("fast-toggle"));
  }

  function syncEffortUi() {
    const e = currentEffort();
    if (effortLabel) {
      effortLabel.textContent = e.label;
    }
    const icon = document.getElementById("effort-icon");
    if (icon) {
      setEffortIconEl(icon, e.id, true);
    }
    if (effortBtn) {
      effortBtn.dataset.effort = effortId;
      effortBtn.title = fastOn ? `${e.label} · Fast profile` : e.label;
    }
    const fastMark = document.getElementById("effort-fast-mark");
    if (fastMark) {
      fastMark.hidden = !fastOn;
    }
    effortMenu?.querySelectorAll("[data-effort]").forEach((el) => {
      const id = el.getAttribute("data-effort");
      el.classList.toggle("selected", id === effortId);
    });
    const toggle = fastToggleRef.el;
    if (toggle) {
      toggle.classList.toggle("on", fastOn);
      toggle.setAttribute("aria-checked", fastOn ? "true" : "false");
    }
    effortMenu?.querySelector(".menu-row-fast")?.classList.toggle("fast-on", fastOn);
  }

  function ensureAssistant() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!assistantBubble) {
      assistantBubble = document.createElement("div");
      assistantBubble.className = "turn-text";
      inner.appendChild(assistantBubble);
    }
    return assistantBubble;
  }

  function renderFiles() {
    if (!filesEl) {
      return;
    }
    filesEl.innerHTML = "";
    filesEl.classList.toggle("has-files", files.length > 0);
    files.forEach((f, idx) => {
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "file-attach" + (isImage ? " file-attach-image" : "");

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "file-attach-thumb";
        img.src = f.previewUri;
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "file-attach-meta";
      const name = document.createElement("span");
      name.className = "file-attach-name";
      name.textContent = f.name;
      name.title = f.path || f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "file-attach-remove";
      rm.setAttribute("aria-label", "Remove file");
      rm.textContent = "×";
      rm.addEventListener("click", (e) => {
        e.stopPropagation();
        files.splice(idx, 1);
        renderFiles();
      });
      card.appendChild(rm);
      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        if (e.target === rm || rm.contains(/** @type {Node} */ (e.target))) return;
        onAttachmentClick(f, isImage, e);
      });
      filesEl.appendChild(card);
    });
  }

  function send() {
    if (busy) {
      host.postMessage({ type: "cancelTurn" });
      return;
    }
    if (!inputEl) {
      return;
    }
    let text = inputEl.value.trim();
    if (!text && files.length === 0) {
      return;
    }
    if (text.startsWith("/") && !text.includes("\n")) {
      const cmd = text.split(/\s+/)[0].toLowerCase();
      const arg = text.slice(cmd.length).trim() || undefined;
      inputEl.value = "";
      hidePalette();
      autoGrow();
      host.postMessage({ type: "slashCommand", cmd, arg });
      return;
    }
    const payload = {
      type: "send",
      text,
      mode: currentMode().mode,
      profile: effectiveProfile(),
      apply: false,
      allowExec: accessId === "auto",
      files: files.map((f) => ({
        name: f.name,
        path: f.path,
        ext: f.ext,
        kind: f.kind,
        previewUri: f.previewUri,
      })),
    };
    inputEl.value = "";
    autoGrow();
    files = [];
    renderFiles();
    closeMenus();
    host.postMessage(payload);
  }

  function autoGrow() {
    if (!inputEl) {
      return;
    }
    inputEl.style.height = "auto";
    inputEl.style.height = Math.min(140, Math.max(44, inputEl.scrollHeight)) + "px";
  }

  function positionAccessMenu() {
    if (!accessMenu || !accessBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = accessBtn.getBoundingClientRect();
    accessMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    accessMenu.style.right = "auto";
  }

  function positionEffortMenu() {
    if (!effortMenu || !effortBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = effortBtn.getBoundingClientRect();
    effortMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    effortMenu.style.right = "auto";
  }

  function positionModelMenu() {
    if (!modelMenu || !modelPill || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = modelPill.getBoundingClientRect();
    modelMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    modelMenu.style.right = "auto";
  }

  function renderProviderModels(catalog) {
    if (!modelMenuList) return;
    providerModelsCatalog = catalog;
    modelMenuList.innerHTML = "";
    const providers = Array.isArray(catalog.providers) ? catalog.providers : [];
    const activeModel = catalog.activeModel || currentModel;
    const q = (modelMenuFilter || "").trim().toLowerCase();
    if (!providers.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = "No providers — open Settings";
      modelMenuList.appendChild(btn);
      return;
    }
    let shown = 0;
    providers.forEach((p) => {
      const models = Array.isArray(p.models) ? p.models : [];
      const filtered = q
        ? models.filter((m) => (m.id || "").toLowerCase().includes(q))
        : models;
      if (!filtered.length && models.length && q) {
        return;
      }
      const head = document.createElement("div");
      head.className = "menu-section";
      const count = filtered.length || models.length;
      head.textContent = `${p.name || p.key}${count ? ` (${count})` : ""}`;
      modelMenuList.appendChild(head);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = p.models_error || (p.ready ? "No models" : "Not configured");
        modelMenuList.appendChild(empty);
        return;
      }
      if (!filtered.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = "No matches";
        modelMenuList.appendChild(empty);
        return;
      }
      filtered.forEach((m) => {
        shown++;
        const id = m.id || "";
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item" + (id === activeModel && p.active ? " selected" : "");
        btn.setAttribute("data-model", id);
        btn.setAttribute("data-provider", p.key);
        // textContent, not innerHTML: model ids come from a remote catalog.
        btn.textContent = id;
        btn.title = id;
        modelMenuList.appendChild(btn);
      });
    });
    if (shown === 0 && q) {
      const empty = document.createElement("div");
      empty.className = "menu-hint";
      empty.textContent = `No models match “${modelMenuFilter}”`;
      modelMenuList.appendChild(empty);
    }
  }

  function renderModelList(models, current) {
    if (!modelMenuList) {
      return;
    }
    if (current) {
      setModelLabel(current);
    }
    modelMenuList.innerHTML = "";
    if (!models.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = "No models — retry";
      modelMenuList.appendChild(btn);
      return;
    }
    models.forEach((m) => {
      const id = typeof m === "string" ? m : m.id;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item" + (id === currentModel ? " selected" : "");
      btn.setAttribute("data-model", id);
      // textContent, not innerHTML: model ids come from a remote catalog.
      btn.textContent = id;
      btn.title = id;
      modelMenuList.appendChild(btn);
    });
  }

  contextBtn?.addEventListener("mouseenter", () => showContextPopover(true));
  contextBtn?.addEventListener("mouseleave", () => {
    window.setTimeout(() => {
      if (!contextPopover?.matches(":hover")) showContextPopover(false);
    }, 120);
  });
  contextPopover?.addEventListener("mouseenter", () => showContextPopover(true));
  contextPopover?.addEventListener("mouseleave", () => showContextPopover(false));
  contextBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    showContextPopover(!ctxPopoverOpen);
  });

  // ---- Spend / balance chip (provider-reported cost, OpenRouter balance) ----

  /** Format a USD amount compactly: $1.24, $0.031, <$0.001. */
  function formatUsd(v) {
    if (!(typeof v === "number") || !isFinite(v) || v <= 0) {
      return "$0.00";
    }
    if (v < 0.001) {
      return "<$0.001";
    }
    if (v < 0.1) {
      return "$" + v.toFixed(3);
    }
    return "$" + v.toFixed(2);
  }

  function shortModelName(id) {
    const parts = String(id || "").split("/");
    return parts[parts.length - 1] || String(id || "");
  }

  function renderCostChip() {
    if (!costWrap) {
      return;
    }
    const liveTotal = sessionCostUSD + turnCostAccum;
    const hasSpend = liveTotal > 0 || (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0);
    const hasBalance = !!(creditsInfo && creditsInfo.supported);
    if (!hasSpend && !hasBalance) {
      costWrap.classList.add("hidden");
      return;
    }
    costWrap.classList.remove("hidden");
    if (costLabelEl) {
      costLabelEl.textContent = hasSpend ? formatUsd(liveTotal) : formatUsd(creditsInfo?.balance || 0);
      costLabelEl.title = hasSpend ? "Session spend" : "Balance";
    }
    if (costBalanceEl) {
      costBalanceEl.textContent = hasBalance ? "balance " + formatUsd(creditsInfo?.balance || 0) : "";
    }
    if (costSummaryEl) {
      const bits = [];
      bits.push("session " + formatUsd(liveTotal));
      if (turnCostAccum > 0) {
        bits.push("current turn " + formatUsd(turnCostAccum));
      } else if (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0) {
        bits.push("last turn " + formatUsd(lastTurnUsage.cost_usd));
      }
      costSummaryEl.textContent = bits.join(" · ");
    }
    if (costRowsEl) {
      costRowsEl.innerHTML = "";
      const entries = (lastTurnUsage && Array.isArray(lastTurnUsage.entries)) ? lastTurnUsage.entries : [];
      entries.forEach((en) => {
        const row = document.createElement("div");
        row.className = "cost-row";
        const name = document.createElement("span");
        name.className = "cost-row-model";
        name.textContent = shortModelName(en.model);
        name.title = (en.provider ? en.provider + " / " : "") + (en.model || "");
        const meta = document.createElement("span");
        meta.className = "cost-row-meta";
        const calls = typeof en.calls === "number" && en.calls > 0 ? en.calls + "× · " : "";
        const toks = typeof en.total_tokens === "number" && en.total_tokens > 0
          ? Math.round(en.total_tokens / 1000) + "k tok · "
          : "";
        meta.textContent = calls + toks + formatUsd(en.cost_usd || 0);
        row.appendChild(name);
        row.appendChild(meta);
        costRowsEl.appendChild(row);
      });
    }
  }

  let costPopoverOpen = false;
  function showCostPopover(show) {
    costPopoverOpen = show;
    costPopover?.classList.toggle("hidden", !show);
    costBtn?.classList.toggle("open", show);
  }
  costBtn?.addEventListener("mouseenter", () => showCostPopover(true));
  costBtn?.addEventListener("mouseleave", () => {
    window.setTimeout(() => {
      if (!costPopover?.matches(":hover")) showCostPopover(false);
    }, 120);
  });
  costPopover?.addEventListener("mouseenter", () => showCostPopover(true));
  costPopover?.addEventListener("mouseleave", () => showCostPopover(false));
  costBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    showCostPopover(!costPopoverOpen);
  });

  sendBtn?.addEventListener("click", send);
  inputEl?.addEventListener("keydown", (e) => {
    if (paletteMode && paletteItems.length && (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Tab")) {
      e.preventDefault();
      if (e.key === "ArrowDown") {
        paletteIndex = (paletteIndex + 1) % paletteItems.length;
      } else if (e.key === "ArrowUp") {
        paletteIndex = (paletteIndex - 1 + paletteItems.length) % paletteItems.length;
      }
      paletteMenu?.querySelectorAll(".palette-item").forEach((el, i) => {
        el.classList.toggle("selected", i === paletteIndex);
      });
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (paletteMode && paletteItems.length) {
        applyPaletteItem(paletteIndex);
        return;
      }
      send();
    }
    if (e.key === "Escape") {
      closeMenus();
    }
  });
  inputEl?.addEventListener("input", () => {
    autoGrow();
    onInputPalette();
  });

  modeBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modeMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      modeMenu?.classList.add("open");
      modeBtn.classList.add("open");
    }
  });

  accessBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !accessMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionAccessMenu();
      accessMenu?.classList.add("open");
      accessBtn.classList.add("open");
    }
  });

  accessMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const item = /** @type {HTMLElement | null} */ (e.target.closest("[data-access]"));
    if (!item || !accessMenu.contains(item)) {
      return;
    }
    const id = item.getAttribute("data-access");
    if (id) {
      accessId = id;
      syncAccessUi();
      closeMenus();
    }
  });

  effortBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !effortMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionEffortMenu();
      effortMenu?.classList.add("open");
      effortBtn.classList.add("open");
    }
  });

  modeMenu?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest(".menu-item");
    if (!item) {
      return;
    }
    const id = item.getAttribute("data-id");
    if (!id) {
      return;
    }
    modeId = id;
    syncModeUi();
    host.setState({ ...(host.getState() || {}), modeId });
    closeMenus();
  });

  effortMenu?.addEventListener("click", (e) => {
    const effortItem = /** @type {HTMLElement | null} */ (e.target.closest("[data-effort]"));
    if (effortItem && effortMenu.contains(effortItem)) {
      const id = effortItem.getAttribute("data-effort");
      if (id) {
        effortId = id;
        syncEffortUi();
        closeMenus();
      }
      return;
    }
    if (/** @type {HTMLElement} */ (e.target).closest("#fast-toggle")) {
      e.stopPropagation();
      fastOn = !fastOn;
      syncEffortUi();
    }
  });

  attachBtn?.addEventListener("click", () => {
    host.postMessage({ type: "attach" });
  });

  sessionNewBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    host.postMessage({ type: "newSession" });
  });

  sessionHistoryBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !sessionMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      sessionMenu?.classList.add("open");
      sessionHistoryBtn.classList.add("open");
      host.postMessage({ type: "listSessions" });
    }
  });

  sessionTabsEl?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const closeEl = t.closest("[data-close-session]");
    if (closeEl) {
      e.stopPropagation();
      const sid = closeEl.getAttribute("data-close-session");
      if (sid) {
        host.postMessage({ type: "closeSession", sessionId: sid });
      }
      return;
    }
    const tab = t.closest(".session-tab[data-session-id]");
    if (!tab) {
      return;
    }
    const sid = tab.getAttribute("data-session-id");
    if (sid && sid !== activeSessionId) {
      host.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  pendingApplyBtn?.addEventListener("click", () => applyPendingChanges());
  pendingRejectBtn?.addEventListener("click", () => discardPendingChanges());

  diffViewerCloseBtn?.addEventListener("click", hideDiffViewer);
  diffViewerEditorBtn?.addEventListener("click", () => {
    if (!diffViewerState.path) return;
    openDiffMessage(diffViewerState.path, diffViewerState.before, diffViewerState.after, false);
  });
  diffViewer?.addEventListener("click", (e) => {
    if (e.target === diffViewer) hideDiffViewer();
  });

  imagePreviewCloseBtn?.addEventListener("click", hideImagePreview);
  imagePreviewPrevBtn?.addEventListener("click", () => shiftImagePreview(-1));
  imagePreviewNextBtn?.addEventListener("click", () => shiftImagePreview(1));
  imagePreviewOpenBtn?.addEventListener("click", () => {
    const item = imagePreviewState.items[imagePreviewState.index];
    if (!item?.path) return;
    hideImagePreview();
    openExternalFile(item.path, true);
  });
  imagePreview?.addEventListener("click", (e) => {
    if (e.target === imagePreview) hideImagePreview();
  });

  composerWrap?.addEventListener("dragover", (e) => {
    e.preventDefault();
    composerWrap.classList.add("drag-over");
  });
  composerWrap?.addEventListener("dragleave", () => {
    composerWrap.classList.remove("drag-over");
  });
  composerWrap?.addEventListener("drop", (e) => {
    e.preventDefault();
    composerWrap.classList.remove("drag-over");
    void attachFilesFromDataTransfer(e.dataTransfer);
  });

  inputEl?.addEventListener("paste", (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    let handledImage = false;
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (!file) continue;
        if (file.size > MAX_ATTACH_BYTES) {
          appendMsg("system", "Pasted image exceeds 20 MB limit");
          continue;
        }
        handledImage = true;
        e.preventDefault();
        void (async () => {
          try {
            const dataBase64 = await readFileAsBase64(file);
            host.postMessage({
              type: "attachBytes",
              name: file.name || "paste.png",
              mime: item.type,
              dataBase64,
            });
          } catch {
            /* ignore */
          }
        })();
      }
    }
    if (handledImage) {
      e.preventDefault();
    }
  });

  modelPill?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modelMenu?.classList.contains("open");
    closeMenus();
    if (open && modeId === "orchestra") {
      renderOrchestraRolesMenu();
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      host.postMessage({ type: "listOrchestraRoles" });
      return;
    }
    if (open) {
      if (modelMenuTitle) modelMenuTitle.textContent = "Models";
      if (modelMenuSearch) modelMenuSearch.style.display = "";
      modelMenuFilter = "";
      if (modelMenuSearch) {
        modelMenuSearch.value = "";
      }
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      host.postMessage({ type: "listProviderModels" });
      setTimeout(() => modelMenuSearch?.focus(), 30);
    }
  });

  modelMenuSearch?.addEventListener("input", () => {
    modelMenuFilter = modelMenuSearch?.value || "";
    if (providerModelsCatalog) {
      renderProviderModels(providerModelsCatalog);
    }
  });

  modelMenuSearch?.addEventListener("click", (e) => e.stopPropagation());

  orchConfigBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    host.postMessage({ type: "openOrchestraSettings" });
  });

  settingsBtn?.addEventListener("click", () => {
    host.postMessage({ type: "openSettings" });
  });

  todosChip?.addEventListener("click", (e) => {
    e.stopPropagation();
    todosExpanded = !todosExpanded;
    renderTodos();
  });

  sessionMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    // Delete is nested inside the row — check it first so the click does not
    // also open the session. The menu stays open; the host pushes a fresh list.
    const delEl = t.closest("[data-delete-session]");
    if (delEl) {
      const delId = delEl.getAttribute("data-delete-session");
      if (delId) {
        host.postMessage({ type: "deleteSession", sessionId: delId });
      }
      return;
    }
    const item = t.closest("[data-session-action], [data-session-id]");
    if (!item) {
      return;
    }
    const action = item.getAttribute("data-session-action");
    if (action === "new") {
      closeMenus();
      host.postMessage({ type: "newSession" });
      return;
    }
    const sid = item.getAttribute("data-session-id");
    if (sid) {
      closeMenus();
      host.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  modelMenuList?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest("[data-model], [data-model-action]");
    if (!item) {
      return;
    }
    if (item.getAttribute("data-model-action") === "refresh") {
      host.postMessage({ type: "listProviderModels" });
      return;
    }
    if (item.getAttribute("data-model-action") === "configure-orchestra") {
      closeMenus();
      host.postMessage({ type: "openOrchestraSettings" });
      return;
    }
    const model = item.getAttribute("data-model");
    const provider = item.getAttribute("data-provider") || undefined;
    if (model) {
      closeMenus();
      host.postMessage({ type: "setModel", model, provider });
    }
  });

  paletteMenu?.addEventListener("click", (e) => e.stopPropagation());
  document.addEventListener("click", () => closeMenus());
  modeMenu?.addEventListener("click", (e) => e.stopPropagation());
  effortMenu?.addEventListener("click", (e) => e.stopPropagation());
  sessionMenu?.addEventListener("click", (e) => e.stopPropagation());
  modelMenu?.addEventListener("click", (e) => e.stopPropagation());
  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    switch (msg.type) {
      case "status": {
        const st = msg.status || "";
        if (st === "error") {
          setChromeHint(msg.detail || "connection error", true);
        } else if (st === "connecting") {
          busyStatusText = msg.detail || "Connecting…";
          setChromeHint(msg.detail || "Connecting…", false);
        } else if (st === "running") {
          busyStatusText = "Working…";
          setChromeHint("Working…", false);
        } else {
          setChromeHint("", false);
        }
        setBusy(st === "running" || st === "connecting");
        break;
      }
      case "ready":
        setChromeHint("", false);
        setBusy(false);
        break;
      case "turnInFlight":
        // Webview was rebuilt (e.g. returning from Settings) while the agent
        // turn is still running — re-arm the busy UI before the replayed
        // projection arrives.
        setBusy(true);
        break;
      case "header":
        if (msg.sessionId && msg.sessionId !== activeSessionId) {
          // Session switch: drop the previous session's turn breakdown.
          lastTurnUsage = null;
          turnCostAccum = 0;
        }
        activeSessionId = msg.sessionId || activeSessionId;
        updateActiveTabTitle(msg.title || "New chat");
        setModelLabel(msg.model || "");
        if (modelLabelEl && msg.provider) {
          modelLabelEl.title = `${msg.provider} · ${msg.model || ""}`;
        }
        if (typeof msg.sessionCost === "number") {
          sessionCostUSD = msg.sessionCost;
          renderCostChip();
        }
        if (modeId === "orchestra") {
          // Keep the tier breakdown pill; header carries the single main model.
          renderOrchestraPill();
        }
        break;
      case "sessionList": {
        if (!sessionMenuList) {
          break;
        }
        sessionMenuList.innerHTML = "";
        const sessions = Array.isArray(msg.sessions) ? msg.sessions : [];
        if (sessions.length === 0) {
          const empty = document.createElement("div");
          empty.className = "menu-section";
          empty.textContent = "No saved sessions";
          sessionMenuList.appendChild(empty);
          break;
        }
        /** Timestamp for grouping: prefer updated_at, fall back to the id (YYYYMMDDTHHMMSS-xxxx). */
        const sessionDate = (s) => {
          const iso = s.updated_at || s.created_at || "";
          const d = iso ? new Date(iso) : null;
          if (d && !Number.isNaN(d.getTime())) {
            return d;
          }
          const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s.id || "");
          return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : null;
        };
        const dayKey = (d) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
        const now = new Date();
        const todayKey = dayKey(now);
        const yesterdayKey = dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1));
        const dayLabel = (d) => {
          const k = dayKey(d);
          if (k === todayKey) {
            return "Today";
          }
          if (k === yesterdayKey) {
            return "Yesterday";
          }
          return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
        };
        let lastGroup = "";
        sessions.slice(0, 100).forEach((s) => {
          const d = sessionDate(s);
          const group = d ? dayLabel(d) : "Older";
          if (group !== lastGroup) {
            lastGroup = group;
            const header = document.createElement("div");
            header.className = "menu-section";
            header.textContent = group;
            sessionMenuList.appendChild(header);
          }
          const row = document.createElement("div");
          row.className = "menu-item session-row";
          row.setAttribute("role", "button");
          row.setAttribute("tabindex", "0");
          row.setAttribute("data-session-id", s.id);
          row.title = [s.model, s.msg_count ? `${s.msg_count} messages` : ""].filter(Boolean).join(" · ");

          const label = document.createElement("span");
          label.className = "session-row-title";
          label.textContent = s.title || s.id;
          row.appendChild(label);

          if (d) {
            const time = document.createElement("span");
            time.className = "session-row-time";
            time.textContent = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
            row.appendChild(time);
          }

          const del = document.createElement("span");
          del.className = "session-row-del";
          del.setAttribute("data-delete-session", s.id);
          del.title = "Delete chat";
          del.textContent = "✕";
          row.appendChild(del);

          sessionMenuList.appendChild(row);
        });
        break;
      }
      case "sessionTabs": {
        renderSessionTabs(msg.tabs, msg.activeId);
        break;
      }
      case "models": {
        renderModelList(Array.isArray(msg.models) ? msg.models : [], msg.current || currentModel);
        break;
      }
      case "providerModels":
        renderProviderModels(msg);
        break;
      case "orchestraRoles": {
        orchestraRolesInfo = {
          roles: Array.isArray(msg.roles) ? msg.roles : [],
          defaultTier: msg.defaultTier || "",
        };
        if (modeId === "orchestra") {
          renderOrchestraPill();
          if (modelMenu?.classList.contains("open")) {
            renderOrchestraRolesMenu();
          }
        }
        break;
      }
      case "pendingOps": {
        const payload = msg.payload || {};
        pendingState = {
          ops: Array.isArray(payload.ops) ? payload.ops : [],
          diff: Array.isArray(payload.diff) ? payload.diff : [],
        };
        diffReviewCursor = 0;
        renderPendingBar();
        void syncToolDiffPreviews();
        break;
      }
      case "pendingCleared":
        pendingState = { ops: [], diff: [] };
        diffReviewCursor = 0;
        renderPendingBar();
        syncToolDiffStats();
        break;
      case "permissionRequest":
        showPermissionOverlay(msg.request || {});
        break;
      case "questionAsk":
        showQuestionOverlay(Array.isArray(msg.questions) ? msg.questions : []);
        break;
      case "clearMessages":
        resetTrajectory();
        sendQueue = [];
        renderSendQueue();
        resetContextUsage();
        if (messagesEl) {
          messagesEl.innerHTML = "";
        }
        assistantBubble = null;
        resetTurnState();
        toolBlocks.clear();
        toolArgs.clear();
        execSteps.clear();
        todos = [];
        todosExpanded = false;
        todosHadOpen = false;
        subagents = [];
        subagentByTask.clear();
        renderSubagents();
        renderTodos();
        setWorkflow("", false);
        break;
      case "trajectory":
        replaceTrajectory(msg.recorded, msg.events, msg.error);
        break;
      case "trajectoryEvent":
        appendTrajectoryEvent(msg.event);
        break;
      case "history": {
        const list = Array.isArray(msg.messages) ? msg.messages : [];
        const renderOne = (m) => {
          const role = m.role === "user" ? "user" : m.role === "system" ? "error" : "assistant";
          const opts = {
            uiIndex: typeof m.uiIndex === "number" ? m.uiIndex : undefined,
            files: Array.isArray(m.files) ? m.files : undefined,
            reasoning: typeof m.reasoning === "string" ? m.reasoning : undefined,
            toolBlocks: Array.isArray(m.toolBlocks) ? m.toolBlocks : undefined,
          };
          appendMsg(role, m.text || "", opts);
        };
        // Long sessions: render only the tail eagerly. Hundreds of markdown
        // bubbles + diff cards freeze the webview for seconds; older messages
        // expand on demand.
        const HISTORY_RENDER_CAP = 60;
        if (list.length > HISTORY_RENDER_CAP) {
          const hidden = list.slice(0, list.length - HISTORY_RENDER_CAP);
          const shown = list.slice(list.length - HISTORY_RENDER_CAP);
          const moreBtn = document.createElement("button");
          moreBtn.type = "button";
          moreBtn.className = "history-more";
          moreBtn.textContent = `Show ${hidden.length} older messages`;
          moreBtn.addEventListener("click", () => {
            moreBtn.remove();
            const frag = document.createDocumentFragment();
            const anchor = messagesEl?.firstChild || null;
            // Temporarily redirect appendMsg output by rendering then moving:
            // simplest correct approach — render into the live container and
            // reinsert before the previously-first node in order.
            const beforeCount = messagesEl ? messagesEl.childNodes.length : 0;
            hidden.forEach(renderOne);
            if (messagesEl && anchor) {
              const added = [];
              for (let i = beforeCount; i < messagesEl.childNodes.length; i++) {
                added.push(messagesEl.childNodes[i]);
              }
              added.forEach((n) => frag.appendChild(n));
              messagesEl.insertBefore(frag, anchor);
            }
            void syncToolDiffPreviews();
          });
          messagesEl?.appendChild(moreBtn);
          shown.forEach(renderOne);
        } else {
          list.forEach(renderOne);
        }
        void syncToolDiffPreviews();
        break;
      }
      case "queueUpdate":
        sendQueue = Array.isArray(msg.items) ? msg.items : [];
        renderSendQueue();
        break;
      case "turnStart":
        beginTurn();
        break;
      case "userEcho":
        appendMsg("user", msg.text, {
          uiIndex: typeof msg.uiIndex === "number" ? msg.uiIndex : undefined,
          files: Array.isArray(msg.files) ? msg.files : undefined,
        });
        break;
      case "reasoningDelta": {
        const body = ensureReasoning();
        if (body && typeof msg.content === "string") {
          body.textContent = (body.textContent || "") + msg.content;
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "delta":
      case "deltaSync": {
        if (busy) {
          busyStatusText = "Writing…";
          updateBusyUi();
        }
        const bubble = ensureAssistant();
        if (bubble && typeof msg.content === "string") {
          if (msg.type === "deltaSync") {
            streamRawText = msg.content;
          } else {
            streamRawText += msg.content;
          }
          scheduleAssistantMarkdown(
            bubble,
            sanitizeAssistantStream(stripFinalEnvelope(streamRawText))
          );
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "discardAssistantBubble": {
        flushAssistantMarkdown();
        if (assistantBubble) {
          assistantBubble.remove();
          assistantBubble = null;
        }
        streamRawText = "";
        break;
      }
      case "attachmentPreview":
        if (msg.path) {
          openExternalFile(msg.path, false);
        } else {
          appendMsg("system", `Cannot open attachment: ${msg.name || "file"}`);
        }
        break;
      case "childLifecycle":
        handleChildLifecycle(msg);
        break;
      case "diffViewer":
        showDiffViewer(msg.path || "", msg.before || "", msg.after || "", msg.language || "");
        break;
      case "highlightResult": {
        const cb = highlightWaiters.get(msg.requestId || "");
        if (cb) {
          highlightWaiters.delete(msg.requestId || "");
          cb(Array.isArray(msg.lines) ? msg.lines : []);
        }
        break;
      }
      case "toolBlock":
        handleToolBlock(msg);
        break;
      case "execChunk":
        if (typeof msg.step === "number" && typeof msg.chunk === "string") {
          appendExecChunk(msg.step, msg.chunk);
        }
        break;
      case "todosUpdate":
        todos = Array.isArray(msg.todos) ? msg.todos : [];
        renderTodos();
        break;
      case "stepUsage": {
        const u = msg.usage || {};
        if (msg.scope !== "child") {
          // Context gauge tracks the main agent only; worker windows differ.
          ctxState.prompt = typeof u.prompt_tokens === "number" ? u.prompt_tokens : 0;
          ctxState.completion = typeof u.completion_tokens === "number" ? u.completion_tokens : 0;
          ctxState.estimated = u.source === "estimate";
          if (Array.isArray(u.breakdown) && u.breakdown.length > 0) {
            ctxState.breakdown = u.breakdown;
          }
          renderContextUi();
        }
        // Live spend: each finished LLM call (planner step, worker step)
        // reports its cost here — update the chip mid-turn instead of
        // waiting for the end-of-turn turnUsage summary.
        if (typeof u.cost_usd === "number" && u.cost_usd > 0) {
          turnCostAccum += u.cost_usd;
          renderCostChip();
        }
        break;
      }
      case "turnUsage": {
        // Server total is authoritative — drop the live per-step estimate.
        turnCostAccum = 0;
        lastTurnUsage = msg.usage || null;
        if (typeof msg.sessionCost === "number" && msg.sessionCost > 0) {
          sessionCostUSD = msg.sessionCost;
        } else if (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0) {
          sessionCostUSD += lastTurnUsage.cost_usd;
        }
        renderCostChip();
        appendTurnCostNote(lastTurnUsage);
        break;
      }
      case "credits": {
        creditsInfo = {
          supported: msg.supported === true,
          provider: msg.provider || "",
          balance: typeof msg.balance === "number" ? msg.balance : 0,
        };
        renderCostChip();
        break;
      }
      case "contextInfo": {
        const info = msg.info || {};
        if (typeof info.contextLimit === "number" && info.contextLimit > 0) {
          ctxState.limit = info.contextLimit;
        }
        if (typeof info.maxResponseTokens === "number" && info.maxResponseTokens > 0) {
          ctxState.maxResponse = info.maxResponseTokens;
        }
        renderContextUi();
        break;
      }
      case "workflowStage": {
        const st = msg.stage || {};
        const stageId = st.stage_id || st.stageId || "";
        if (msg.phase === "start") {
          if (st.name && st.name !== workflowActiveName) {
            workflowActiveName = st.name;
            workflowStages.clear();
          }
          setWorkflow(st.name || "workflow", true);
          if (stageId) {
            workflowStages.set(stageId, {
              id: stageId,
              name: stageId,
              state: "running",
              attempt: st.attempt || 0,
            });
            renderWorkflowStages();
          }
        } else if (msg.phase === "done" && stageId) {
          const slot = workflowStages.get(stageId) || {
            id: stageId,
            name: stageId,
            state: "pending",
            attempt: st.attempt || 0,
          };
          const action = (st.action || "").toLowerCase();
          if (action.startsWith("redo")) {
            slot.state = "redo";
          } else if (action === "fail") {
            slot.state = "fail";
          } else {
            slot.state = "done";
          }
          workflowStages.set(stageId, slot);
          renderWorkflowStages();
        }
        break;
      }
      case "healthStatus":
        setLspStatus(msg.lspStatus || "");
        break;
      case "systemNote":
        appendMsg("system", msg.text || "");
        break;
      case "skillsList":
        SKILL_CMDS = (Array.isArray(msg.skills) ? msg.skills : []).map((s) => ({
          cmd: "/" + (s.name || ""),
          desc: s.description || "",
        }));
        break;
      case "mentionResults": {
        const hit = inputEl ? detectPaletteQuery(inputEl.value) : null;
        if (!hit || hit.mode !== "mention" || hit.query !== (msg.query ?? "")) {
          break;
        }
        renderPalette("mention", Array.isArray(msg.files) ? msg.files : []);
        break;
      }
      case "tool": {
        const toolName = msg.toolName || "tool";
        const line = msg.done
          ? `✓ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`
          : `→ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`;
        appendMsg("tool", line);
        assistantBubble = null;
        break;
      }
      case "error":
        appendMsg("error", msg.message || "error");
        break;
      case "turnComplete":
        flushAssistantMarkdown();
        finalizeReasoningSummary();
        if (toolTraceEl) {
          toolTraceEl.open = false;
        }
        if (!msg.queuedNext) {
          setBusy(false);
        }
        assistantBubble = null;
        resetTurnState();
        if (!msg.ok) {
          setChromeHint("turn failed", true);
        }
        break;
      case "filesPicked": {
        if (Array.isArray(msg.files)) {
          for (const f of msg.files) {
            addFileRef(f);
          }
        }
        break;
      }
      default:
        break;
    }
  });

  /**
   * Small grey transcript note with the turn cost. In orchestra mode (or any
   * multi-model turn) each model gets its own "model $cost · N×" segment so
   * it is clear what each tier position cost.
   */
  function appendTurnCostNote(usage) {
    if (!messagesEl || !usage) {
      return;
    }
    const cost = typeof usage.cost_usd === "number" ? usage.cost_usd : 0;
    if (cost <= 0) {
      return;
    }
    const entries = Array.isArray(usage.entries) ? usage.entries : [];
    const parts = ["turn " + formatUsd(cost)];
    // Prompt-cache hit, when the provider reports one (Anthropic via a
    // gateway or native). Local models never set this. Without it, a long
    // Anthropic-through-OpenRouter turn that re-billed its whole transcript
    // every step looked the same as one that cached it — this is what would
    // have shown the field run's $2.18 turn was paying full price.
    const cached = typeof usage.cached_prompt_tokens === "number" ? usage.cached_prompt_tokens : 0;
    const promptTotal = typeof usage.prompt_tokens === "number" ? usage.prompt_tokens : 0;
    if (cached > 0 && promptTotal > 0) {
      parts.push("cache " + Math.round((cached / promptTotal) * 100) + "%");
    }
    if (entries.length > 1 || (modeId === "orchestra" && entries.length > 0)) {
      entries.forEach((en) => {
        const calls = typeof en.calls === "number" && en.calls > 1 ? " ×" + en.calls : "";
        parts.push(shortModelName(en.model) + " " + formatUsd(en.cost_usd || 0) + calls);
      });
    }
    const el = document.createElement("div");
    el.className = "usage-note";
    el.textContent = parts.join("   ·   ");
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  document.addEventListener("keydown", (e) => {
    if (!diffReviewActive()) return;
    const n = pendingState.diff.length;
    if (!n) return;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      diffReviewCursor = Math.max(0, diffReviewCursor - 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      diffReviewCursor = Math.min(n - 1, diffReviewCursor + 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      applyPendingChanges();
    }
  });

  initModeMenu();
  initEffortMenu();
  initAccessMenu();
  syncModeUi();
  syncEffortUi();
  syncAccessUi();
  renderContextUi();
  autoGrow();
  host.postMessage({ type: "ready" });
  // Outbound: the renderer's message protocol -> JSON-RPC.
  //
  // This is the browser's half of what ui/vscode/src/chat/panel.ts does for the
  // extension. Only v1 scope is wired: chat, cancellation, sessions. Message
  // types outside that scope are acknowledged and ignored rather than dropped
  // silently — a no-op the user can see beats a control that does nothing.

  /**
   * Per-project session state. The renderer shows one project at a time, so
   * exactly one of these is "current"; the others are what a switch restores.
   * @type {Map<string, {sessionId: string, inFlightTurnId: any, workspaceRoot: string, status: string, pendingAsk: any, llm: any}>}
   */
  const perProject = new Map();
  let currentProjectId = "";

  /** @param {string} projectId */
  function projectState(projectId) {
    let st = perProject.get(projectId);
    if (!st) {
      st = {
        sessionId: "",
        inFlightTurnId: null,
        workspaceRoot: "",
        status: "idle",
        pendingAsk: null,
        // The core's last answer about the model and its window; see pushLLMInfo.
        llm: null,
      };
      perProject.set(projectId, st);
    }
    return st;
  }

  /** @param {string} projectId */
  function forgetProjectState(projectId) {
    perProject.delete(projectId);
  }

  // ---- what the composer says about the model -----------------------------
  //
  // The model pill and the context gauge under the input read two renderer
  // messages: header (model, provider) and contextInfo (the window and the
  // reply budget) — ui/vscode/media/chat-src/07-events.js. The editor's host
  // sends both from the core's own answer (panel.ts refreshHeaderAndHistory).
  // This host used to send a header with no model on every switch and no
  // contextInfo at all, so the pill kept whichever project's model it had
  // seen last and the gauge measured against a 128K default that was nobody's
  // window.

  /**
   * The ceiling the gauge measures against. num_ctx is the window the request
   * asks the server for; context_tokens is the most the model can take, from
   * the catalogue or the server's own answer. A prompt has to fit under both,
   * so the smaller one is the real limit: a local model run with num_ctx 20000
   * overflows at 20000 however large its catalogue entry says it could be.
   * @param {any} numCtx @param {any} contextTokens
   */
  function contextLimitFor(numCtx, contextTokens) {
    const asked = Number(numCtx) > 0 ? Number(numCtx) : 0;
    const most = Number(contextTokens) > 0 ? Number(contextTokens) : 0;
    if (asked > 0 && most > 0) {
      return Math.min(asked, most);
    }
    return asked || most || 128000;
  }

  /**
   * Ask the core what the project is talking to, keep the answer with the
   * project, and tell the composer if that project is the one on screen. Kept
   * per project so a switch can repaint from the last answer at once
   * (postLLMInfo) while a fresh read is on its way.
   * @param {string} projectId
   */
  async function pushLLMInfo(projectId) {
    const conn = connFor(projectId);
    if (!conn) {
      return;
    }
    // Asked for alongside the model, not after it: the balance is a separate
    // call, and chaining it behind this await means a core that is slow to
    // answer about the model never reports a balance at all.
    void pushCredits(projectId);
    let llm;
    try {
      llm = (await conn.send("runtime.get_llm", {})) || {};
    } catch (err) {
      // The pill keeps its last label: there is nothing truer to put there.
      return;
    }
    const st = projectState(projectId);
    const maxTokens = Number(llm.max_tokens) || 0;
    st.llm = {
      model: String(llm.model || ""),
      provider: String(llm.provider || ""),
      contextLimit: contextLimitFor(llm.num_ctx, llm.context_tokens),
      maxResponseTokens: maxTokens > 0 ? maxTokens : 4096,
    };
    if (projectId === currentProjectId) {
      postLLMInfo(projectId);
    }
  }

  /**
   * The provider's account balance for the cost popover. The renderer already
   * draws the row from a "credits" message (07-events.js) — the editor host
   * sent one and the web host never did, so the popover here could only ever
   * show spend, never what is left.
   *
   * Best-effort by design: providers without a balance API (every local
   * server, plain OpenAI) answer supported=false, and the row is omitted.
   * @param {string} projectId
   */
  async function pushCredits(projectId) {
    const conn = connFor(projectId);
    if (!conn || !conn.isOpen()) {
      return;
    }
    try {
      const c = (await conn.send("runtime.credits", {})) || {};
      if (projectId !== currentProjectId) {
        return;
      }
      toRenderer({
        type: "credits",
        supported: !!c.supported,
        provider: String(c.provider || ""),
        balance: Number(c.balance) || 0,
      });
    } catch (err) {
      // No balance API, no key, or an endpoint that is down: the popover
      // simply keeps showing spend without a balance row.
    }
  }

  /**
   * The title the tab strip shows for a session, from the last session.list
   * (40-projects.js keeps it in sessionsByProject). A header message has to
   * carry it: the renderer renames the active tab from every header it gets
   * (07-events.js), and one without a title says "New chat" — over a
   * conversation that has a name.
   * @param {string} projectId @param {string} sessionId
   */
  function sessionTitleFor(projectId, sessionId) {
    const row = (sessionsByProject.get(projectId) || []).find((s) => s && s.id === sessionId);
    const title = row && typeof row.title === "string" ? row.title.trim() : "";
    return title || "New chat";
  }

  /** The composer's model and window, from the last answer. @param {string} projectId */
  function postLLMInfo(projectId) {
    const st = projectState(projectId);
    if (!st.llm) {
      return;
    }
    toRenderer({
      type: "header",
      sessionId: st.sessionId,
      title: sessionTitleFor(projectId, st.sessionId),
      model: st.llm.model,
      provider: st.llm.provider,
    });
    toRenderer({
      type: "contextInfo",
      info: {
        contextLimit: st.llm.contextLimit,
        maxResponseTokens: st.llm.maxResponseTokens,
        model: st.llm.model,
      },
    });
  }

  /**
   * A saved transcript in the shape the renderer draws it.
   *
   * The session file keeps a message the way Go writes it — tool_blocks,
   * args_raw, duration_ms, attachments — and the renderer (shared with the
   * editor's webview) reads toolBlocks, argsRaw, durationMs, files. Posting
   * the raw rows straight through, as this used to, restored the words and
   * silently dropped everything else. Mirrors the mapping panel.ts does for
   * the editor.
   * @param {any[]} uiMessages
   */
  function historyMessagesFrom(uiMessages) {
    const list = Array.isArray(uiMessages) ? uiMessages : [];
    const out = [];
    list.forEach((m, idx) => {
      if (!m || typeof m !== "object") {
        return;
      }
      const role = m.role === "user" ? "user" : m.role === "system" ? "system" : "assistant";
      const text = String(m.text || m.content || "");
      const reasoning = uiReasoningOf(m);
      const toolBlocks = uiToolBlocksOf(m);
      const files = (Array.isArray(m.attachments) ? m.attachments : [])
        .filter((a) => a && (a.path || a.name))
        .map((a) => ({
          name: String(a.name || String(a.path || "").split(/[\\/]/).pop() || "file"),
          path: String(a.path || ""),
          ext: a.ext ? String(a.ext) : undefined,
          kind: a.kind === "image" ? "image" : "file",
        }));
      if (!text && !reasoning && toolBlocks.length === 0 && files.length === 0) {
        return;
      }
      const row = { role, text };
      // The index into the core's own list: rewind aims at it, so it counts
      // every row, including any this loop leaves out.
      if (role === "user") row.uiIndex = idx;
      if (files.length) row.files = files;
      if (reasoning) row.reasoning = reasoning;
      if (toolBlocks.length) row.toolBlocks = toolBlocks;
      out.push(row);
    });
    return out;
  }

  /** A saved message's reasoning, whether written flat or as segments. */
  function uiReasoningOf(m) {
    const direct = String((m && m.reasoning) || "").trim();
    if (direct) {
      return direct;
    }
    const parts = [];
    for (const seg of (m && m.segments) || []) {
      if (seg && seg.kind === "reasoning" && seg.text) parts.push(seg.text);
    }
    return parts.join("").trim();
  }

  /** A saved message's tool blocks, flat or in segments, renderer-spelled. */
  function uiToolBlocksOf(m) {
    const raw = [];
    for (const t of (m && m.tool_blocks) || []) {
      if (t && t.name) raw.push(t);
    }
    if (raw.length === 0) {
      for (const seg of (m && m.segments) || []) {
        if (!seg || seg.kind !== "tools" || !Array.isArray(seg.tools)) continue;
        for (const t of seg.tools) {
          if (t && t.name) raw.push(t);
        }
      }
    }
    return raw.map((t) => ({
      id: t.id,
      name: t.name,
      argsRaw: t.args_raw || t.args_preview || "",
      status: t.status || "completed",
      result: t.result || "",
      diagnostics: Array.isArray(t.diagnostics) && t.diagnostics.length ? t.diagnostics : undefined,
      durationMs: typeof t.duration_ms === "number" && t.duration_ms > 0 ? t.duration_ms : undefined,
    }));
  }

  /**
   * The last measured prompt in a saved transcript. The core writes each
   * assistant message's prompt_ctx and tokens_out into the session file
   * (internal/sessionfile/uimessage.go); reopening a session paints the gauge
   * from them, as panel.ts does, instead of showing an empty ring over a
   * conversation that is plainly not empty. Messages without the fields leave
   * it empty, which is the truth: nothing was measured.
   * @param {any[]} uiMessages
   */
  function postRestoredUsage(uiMessages) {
    const msgs = Array.isArray(uiMessages) ? uiMessages : [];
    let prompt = 0;
    let completion = 0;
    for (const m of msgs) {
      if (!m || String(m.role || "").toLowerCase() !== "assistant") {
        continue;
      }
      if (Number(m.prompt_ctx) > 0) {
        prompt = Number(m.prompt_ctx);
      } else if (prompt === 0 && Number(m.tokens_in) > 0) {
        prompt = Number(m.tokens_in);
      }
      if (Number(m.tokens_out) > 0) {
        completion = Number(m.tokens_out);
      }
    }
    if (prompt <= 0) {
      return;
    }
    toRenderer({
      type: "stepUsage",
      usage: { prompt_tokens: prompt, completion_tokens: completion, source: "restored" },
    });
  }

  /**
   * Read a project's state without creating a record for it. The render path
   * walks every known project, and must not resurrect the records
   * forgetProjectState() deletes.
   * @param {string} projectId
   */
  function peekProjectState(projectId) {
    return perProject.get(projectId) || null;
  }

  /** The state the renderer is currently showing. */
  function current() {
    return projectState(currentProjectId);
  }

  /** @param {string} projectId */
  async function onConnected(projectId) {
    const st = projectState(projectId);
    const conn = connFor(projectId);
    try {
      // core.health is answerable before initialize — it and initialize are the
      // only two methods exempt from the gate (internal/core/rpc_handler.go:76)
      // — and it is where project_root and project_id come from.
      const health = await conn.send("core.health", {});
      st.workspaceRoot = health.workspace_root || "";
      await conn.send("initialize", {
        project_root: st.workspaceRoot,
        project_id: health.project_id || "",
        protocol_version: health.protocol_version,
        ops_version: health.ops_version,
        tools_version: health.tools_version,
      });
      const started = await conn.send("session.start", {});
      st.sessionId = started.session_id || "";
      // Not awaited: the pill and the gauge fill in from the core's answer
      // when it comes, and the transcript does not wait for them.
      void pushLLMInfo(projectId);
      // What "/" offers beyond the built-in commands: this workspace's own
      // skills and .claude/commands, which only the core can enumerate.
      void pushSkillCommands(projectId);
      // Settings opened while this workspace was still coming up had nothing
      // to read from and said so; now there is. Without this the panel kept
      // that note, an empty provider list and no tool catalogue until it was
      // closed and opened again.
      if (projectId === currentProjectId && settingsPanelOpen()) {
        void pushSettingsState();
      }
      if (projectId === currentProjectId) {
        toRenderer({
          type: "header",
          model: health.model || "",
          provider: health.provider || "",
          sessionId: st.sessionId,
        });
        toRenderer({ type: "ready" });
        // The first session of this connection: give the Trajectory pane its
        // (usually empty) log now, or it sits on "Loading trajectory…" until
        // the user switches, starts a session, or finishes a turn.
        void refreshTrajectory(projectId, conn, st.sessionId);
        await refreshSessionList(projectId);
      }
    } catch (err) {
      const message = String(err && err.message ? err.message : err);
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message });
      }
      st.status = "idle";
    }
    // Live, or failed trying: either way the project's frame stops loading.
    noteProjectLive(projectId);
    renderProjects();
  }

  /**
   * Read the session's log and hand it to the renderer — unless the user has
   * switched projects, or to another session of the same project, while the
   * request was in flight, in which case the answer belongs to a view that is
   * no longer on screen. Same rule as the session.get repaint above and
   * refreshSessionList below.
   *
   * A failed fetch is reported as such, not as "not recorded": those are
   * different answers and the view says which.
   * @param {string} projectId @param {any} conn @param {string} sessionId
   */
  async function refreshTrajectory(projectId, conn, sessionId) {
    if (!sessionId) {
      return;
    }
    try {
      const res = await conn.send("session.trajectory", { session_id: sessionId });
      if (projectId !== currentProjectId || projectState(projectId).sessionId !== sessionId) {
        // The user left this project, or moved to another session of it,
        // while the request was in flight: the answer is for a view that is
        // no longer on screen.
        return;
      }
      toRenderer({
        type: "trajectory",
        recorded: Boolean(res && res.recorded),
        events: res && Array.isArray(res.events) ? res.events : [],
      });
    } catch (err) {
      if (projectId === currentProjectId && projectState(projectId).sessionId === sessionId) {
        toRenderer({ type: "trajectory", recorded: true, events: [], error: String(err && err.message ? err.message : err) });
      }
    }
  }

  /**
   * Make projectId the one the renderer shows. Repaints from the core rather
   * than from a buffer — session.get is what makes holding no background
   * scrollback affordable — and re-raises a prompt the project was waiting on.
   * @param {string} projectId
   */
  async function activateProject(projectId) {
    currentProjectId = projectId;
    const st = projectState(projectId);
    const conn = connFor(projectId);
    setActiveConn(conn);
    // Paint the switch now, not after the round trips below. renderProjects is
    // also what puts the start screen away, and this function does not reach
    // its closing render until session.list has answered — so a slow core left
    // the start screen covering a project that was already open and selected.
    renderProjects();

    // The pill and the gauge are the composer's, and the composer is shared by
    // every project: paint this project's model and window over the previous
    // project's now, from the last answer, and read them again in case the
    // model was changed while this project was in the background.
    postLLMInfo(projectId);
    void pushLLMInfo(projectId);
    // Each workspace has its own commands; the palette must follow the switch.
    void pushSkillCommands(projectId);

    toRenderer({ type: "clearMessages" });
    // Take the outgoing project's overlay down unconditionally, then raise this
    // project's own ask, both before any await. Leaving it up while
    // currentProjectId already names this project means the buttons on screen
    // belong to one project and the reply is resolved against another — see
    // setDisplayedAsk below, which is the second half of that fix.
    const overlay = document.getElementById("overlay");
    if (overlay) {
      overlay.classList.add("hidden");
    }
    clearDisplayedAsk();
    if (st.pendingAsk) {
      toRenderer(st.pendingAsk.rendererMessage);
      setDisplayedAsk(projectId, st.pendingAsk);
    }

    if (st.sessionId) {
      try {
        const view = await conn.send("session.get", { session_id: st.sessionId });
        if (projectId !== currentProjectId) {
          return;
        }
        toRenderer({ type: "history", messages: historyMessagesFrom(view.ui_messages) });
        postRestoredUsage(view.ui_messages);
      } catch (err) {
        if (projectId === currentProjectId) {
          toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
        }
      }
      // Not awaited: the fetch must not hold up the header/turnComplete/
      // session.list below, which is what the renderer's own state (composer
      // busy/idle, session list) depends on. refreshTrajectory carries its
      // own stale guard, so a late answer still lands correctly (or is
      // dropped) once it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
    }
    if (projectId !== currentProjectId) {
      return;
    }
    toRenderer({ type: "header", sessionId: st.sessionId });
    // The renderer's "turnInFlight" arm ignores the payload and always calls
    // setBusy(true) (ui/vscode/media/chat-src/07-events.js). Sending it with
    // inFlight:false would lock the composer into "Stop" on every switch to an
    // idle project. "turnComplete" is the only message that reaches
    // setBusy(false), so send whichever one is true. turnComplete's contract is
    // `{ ok: boolean }` (ui/vscode/src/protocol/events.ts) and a missing `ok` is
    // read as failure (ui/vscode/media/chat-src/07-events.js) — arriving at an
    // idle project is not a failed turn, so say so explicitly.
    if (st.inFlightTurnId !== null) {
      toRenderer({ type: "turnInFlight", inFlight: true });
    } else {
      toRenderer({ type: "turnComplete", ok: true });
    }
    await refreshSessionList(projectId);
    renderProjects();
  }

  /**
   * Same rule as sendTurn and startSession: the caller can switch projects
   * while session.list is in flight, and the result must not paint over
   * whatever project is on screen by the time it comes back.
   * @param {string} projectId
   */
  async function refreshSessionList(projectId) {
    try {
      const res = await connFor(projectId).send("session.list", {});
      // The sidebar keeps a list per project, so a late answer is still this
      // project's truth even when the user has switched away — unlike the
      // renderer message below, which paints whatever is on screen.
      noteSessionList(projectId, res.sessions || []);
      if (projectId === currentProjectId) {
        toRenderer({ type: "sessionList", sessions: res.sessions || [] });
      }
    } catch (err) {
      // A missing session list is not fatal; the chat still works.
    }
  }

  /** @param {any} msg */
  function dispatchToCore(msg) {
    switch (msg.type) {
      case "ready":
        // The renderer announces itself; the socket's open handler already ran
        // the handshake, so there is nothing further to do.
        return;

      case "send":
        void sendTurn(msg);
        return;

      case "cancelTurn":
        if (current().inFlightTurnId !== null) {
          wsNotify("$/cancelRequest", { id: current().inFlightTurnId });
        }
        return;

      case "newSession":
        void startSession(undefined);
        return;

      case "openSession":
        void startSession(msg.sessionId);
        return;

      case "closeSession":
        // Web-only: closing a tab hides it from the strip. The session is not
        // deleted — the sidebar still lists it. See 40-projects.js.
        closeSessionTab(msg.sessionId || "");
        return;

      case "deleteSession":
        // Web-only: throws the chat away in the core. The sidebar's × asks
        // twice before it gets here.
        void deleteSession(msg.projectId || currentProjectId, msg.sessionId || "");
        return;

      case "listSessions":
        void refreshSessionList(currentProjectId);
        return;

      case "permissionReply":
      case "questionReply":
        // Answered in 30-adapter-asks.js, which owns the JSON-RPC ids.
        return;

      case "switchProject":
        void switchProject(msg.projectId || "");
        return;

      case "openProjectConnection":
        void ensureConn(msg.projectId || "");
        return;

      default:
        // The composer's own messages — the model pill, the Orchestra
        // breakdown, slash commands, @-mentions, rewind, the pending bar — are
        // answered in 60-composer.js, all of them straight core RPC.
        if (handleComposerMessage(msg)) {
          return;
        }
        // What is left really does belong to a VS Code affordance this host
        // does not have: opening an editor on a file or a diff, and asking the
        // editor to tokenise a code block. Say so rather than swallow the
        // click.
        toRenderer({
          type: "systemNote",
          text: `"${msg.type}" needs an editor to open things in, so it does nothing here.`,
        });
    }
  }

  /** @param {any} msg */
  async function sendTurn(msg) {
    // Capture the id, not just the record: the user can switch projects while
    // this turn is awaiting, and everything after the await must know which
    // project it belongs to.
    const projectId = currentProjectId;
    const st = projectState(projectId);
    if (!st.sessionId) {
      toRenderer({ type: "error", message: "no session — reload the page" });
      return;
    }
    // The session this turn belongs to: the person can open another one while
    // it runs, and the answer must be written back to the session that asked.
    const sessionId = st.sessionId;
    // The chips on the message: files attached through the paperclip, a drop
    // or a paste (60-composer.js). Only ones with a path can go to the core —
    // it reads them from the workspace — and the echo shows the same ones.
    const files = (Array.isArray(msg.files) ? msg.files : []).filter(
      (f) => f && typeof f.path === "string" && f.path
    );
    const attachments = files.map((f) => {
      const a = { path: f.path };
      if (typeof f.name === "string" && f.name) {
        a.name = f.name;
      }
      if (f.kind === "image" || f.kind === "file") {
        a.kind = f.kind;
      }
      return a;
    });
    toRenderer({ type: "userEcho", text: msg.text || "", files: files.length ? files : undefined });
    toRenderer({ type: "turnStart" });
    toRenderer({ type: "turnInFlight", inFlight: true });
    st.status = "working";
    renderProjects();

    // Route through this project's own connection, not whichever one is
    // active by the time this line runs — the caller can switch away while
    // earlier awaits in this function (there are none here, but see
    // activateProject/startSession) are outstanding. sendCancellable allocates
    // and sends synchronously, so the id handed back and the id in the wire
    // frame are provably the same value.
    const conn = connFor(projectId);
    const turn = conn.sendCancellable("session.message", {
      session_id: sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
      ...(attachments.length ? { attachments } : {}),
    });
    st.inFlightTurnId = turn.id;
    // turnComplete's contract is `{ ok: boolean }`, and the renderer treats a
    // missing `ok` as failure — so this must report the truth, not a constant.
    let failed = false;
    try {
      await turn.done;
    } catch (err) {
      failed = true;
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    } finally {
      // The record and the rail always tell the truth, whichever project is on
      // screen. Only the renderer is gated: a turn that ends in a background
      // project must not put its error bubble, or its turnComplete, into the
      // transcript the user is looking at.
      st.inFlightTurnId = null;
      st.status = "idle";
      renderProjects();
      // Write the answer into the session before anything else: the core
      // records the person's message when the turn starts and nothing else,
      // so an answer this page does not save is gone when the session is
      // reopened. Not gated on the visible project — a turn that finished in
      // the background is exactly as worth keeping.
      void saveAssistantTurn(projectId, conn, sessionId);
      if (projectId === currentProjectId) {
        toRenderer({ type: "turnInFlight", inFlight: false });
        toRenderer({ type: "turnComplete", ok: !failed });
        // The log is complete once session.message has returned — the core
        // closes the writer before it answers — so this replaces the live
        // rows with the recorded ones, which carry the core's own timings.
        void refreshTrajectory(projectId, conn, st.sessionId);
      }
    }
  }

  /**
   * Append this turn's answer to the session's ui_messages.
   *
   * The core's own projection holds only what it can know without a client:
   * the person's message, appended when the turn starts (session_rpc.go,
   * SessionMessage). The answer — text, reasoning, the tools that ran, what
   * the step cost — exists only as a stream of events, so every host writes
   * it back itself; the editor does it in coreSession.ts (syncUIProjection)
   * and this is the same contract for the page. Read-modify-write against
   * session.get rather than a blind push, because the core appended the user
   * message after this page last saw the list.
   *
   * @param {string} projectId @param {any} conn @param {string} sessionId
   */
  async function saveAssistantTurn(projectId, conn, sessionId) {
    if (!sessionId || !conn) {
      return;
    }
    const answerMsg = assistantTurnProjection(projectId);
    if (!answerMsg) {
      return; // nothing was said and nothing ran: there is nothing to keep
    }
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const view = (await conn.send("session.get", { session_id: sessionId })) || {};
        const ui = Array.isArray(view.ui_messages) ? view.ui_messages.slice() : [];
        const last = ui.length ? ui[ui.length - 1] : null;
        const lastRole = last ? String((last && last.role) || "").toLowerCase() : "";
        const lastText = last ? String((last && (last.text || last.content)) || "").trim() : "";
        if (lastRole === "assistant" && lastText === answerMsg.text) {
          // The same answer already there — a retry, or another host that got
          // in first. Update it in place rather than saying it twice.
          ui[ui.length - 1] = Object.assign({}, last, answerMsg);
        } else {
          ui.push(answerMsg);
        }
        // title and model are read back and sent again on purpose: the core
        // sets both from these params, so leaving them out renames the
        // session to nothing.
        await conn.send("session.ui_sync", {
          session_id: sessionId,
          title: typeof view.title === "string" ? view.title : "",
          model: typeof view.model === "string" ? view.model : "",
          ui_messages: ui,
        });
        return;
      } catch (err) {
        if (attempt === 0) {
          await new Promise((r) => setTimeout(r, 800));
          continue;
        }
        // Say so rather than lose the answer silently.
        if (projectId === currentProjectId) {
          toRenderer({
            type: "systemNote",
            text: "The answer could not be saved to this session: " + String((err && err.message) || err),
          });
        }
      }
    }
  }

  /** @param {string | undefined} sessionId */
  async function startSession(sessionId) {
    // Same rule as sendTurn: current() is only safe before the first await.
    // The user can switch projects while this is in flight, and everything
    // after an await must be checked against currentProjectId before it paints.
    const projectId = currentProjectId;
    const st = projectState(projectId);
    const conn = connFor(projectId);
    try {
      const params = sessionId ? { session_id: sessionId } : {};
      const started = await conn.send("session.start", params);
      st.sessionId = started.session_id || "";
      if (projectId === currentProjectId) {
        toRenderer({ type: "clearMessages" });
      }
      if (started.restored) {
        const view = await conn.send("session.get", { session_id: st.sessionId });
        if (projectId === currentProjectId) {
          toRenderer({ type: "history", messages: historyMessagesFrom(view.ui_messages) });
          postRestoredUsage(view.ui_messages);
        }
      }
      if (projectId === currentProjectId) {
        toRenderer({ type: "header", sessionId: st.sessionId });
      }
      // A new or reopened session cleared the view above; give it the new
      // session's log, or the "nothing yet" answer, rather than leaving it
      // on "Loading trajectory…" until the next turn ends. Not awaited, for
      // the same reason as activateProject: it must not hold up
      // refreshSessionList below, and refreshTrajectory carries its own
      // stale guard for whenever it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
      await refreshSessionList(projectId);
    } catch (err) {
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    }
  }
  // Inbound: agent/event and exec/output_chunk -> renderer messages.
  //
  // The parent transcript is accumulated here rather than appended by the
  // renderer, because the core streams tokens and the renderer redraws the
  // whole assistant bubble (deltaSync). Child-scoped events belong to a
  // subagent's own trace; they must not be folded into the parent's text —
  // see ui/vscode/src/chat/panel.ts:1589-1604 for the same rule.

  /** @type {Map<string, string>} */
  const turnTextByProject = new Map();
  /** @type {Map<string, Map<string, any>>} */
  const liveToolBlocksByProject = new Map();
  // The rest of the turn, kept for the same reason: the core records the
  // person's message itself but never the answer, so whatever is going to be
  // in the session file afterwards has to be accumulated here and written
  // back when the turn ends (saveAssistantTurn in 10-adapter-session.js).
  /** @type {Map<string, string>} */
  const turnReasoningByProject = new Map();
  /** @type {Map<string, any[]>} */
  const turnToolsByProject = new Map();
  /** @type {Map<string, any>} */
  const turnUsageByProject = new Map();

  // Named blocksForProject, not toolBlocks: ui/vscode/media/chat-src/01-dom-state.js
  // already declares a top-level `const toolBlocks = new Map()`, and the whole
  // bundle is one IIFE — a same-named top-level function here would be a
  // duplicate declaration and fail to parse. That file may not change, so this
  // one avoids the name instead.
  /** @param {string} projectId */
  function blocksForProject(projectId) {
    let m = liveToolBlocksByProject.get(projectId);
    if (!m) {
      m = new Map();
      liveToolBlocksByProject.set(projectId, m);
    }
    return m;
  }

  /**
   * Every notification from every connection lands here first. A project the
   * renderer is not showing contributes its state to the rail and nothing to
   * the transcript: folding a background project's text into the visible
   * bubble is the bug this routing exists to prevent.
   * @param {string} projectId @param {any} msg
   */
  function noteProjectEvent(projectId, msg) {
    const st = projectState(projectId);
    const before = st.status;

    if (msg.method === "agent/event") {
      const ev = msg.params || {};
      switch (ev.type) {
        case "tool_call_start":
        case "message_delta":
        case "reasoning_delta":
          if (st.status === "idle") st.status = "working";
          break;
        case "done":
        case "error":
          // A turn can end (or error out) while a permission/question prompt
          // is still outstanding — e.g. the agent errored before the tool
          // that raised it ever got an answer. Left alone, pendingAsk keeps a
          // JSON-RPC id nobody is waiting on, the rail shows a permanent
          // "asking" badge, and switching in re-raises a prompt whose reply
          // goes nowhere. Clear it here, and if that ask is the one currently
          // on screen, take the overlay down with it — the displayed-ask
          // state and the overlay must always come down together (see
          // setDisplayedAsk / clearDisplayedAsk in 30-adapter-asks.js).
          st.status = "idle";
          if (st.pendingAsk) {
            st.pendingAsk = null;
            if (isDisplayedAskFor(projectId)) {
              clearDisplayedAsk();
              const overlay = document.getElementById("overlay");
              if (overlay) {
                overlay.classList.add("hidden");
              }
            }
          }
          break;
        default:
          break;
      }
    }

    if (projectId === currentProjectId) {
      handleNotification(projectId, msg);
    }
    return st.status !== before;
  }

  /** @param {string} projectId @param {any} msg */
  function handleNotification(projectId, msg) {
    // The trajectory sees the raw notification, before the chat's lossy
    // translation below: one live row per event, reconciled against the log
    // when the turn ends. Only these four methods are recorded by the core's
    // tee, so only these four are forwarded — and only for the session
    // currently on screen. A session switch within this project (new/open
    // session) can leave an old turn still streaming, and its notifications
    // must not paint into a pane that now shows a different session. An
    // event with no session_id (workflow/stage_*, which is not
    // session-scoped at all) is forwarded unfiltered — there is nothing to
    // check it against.
    if (
      msg.method === "agent/event" ||
      msg.method === "exec/output_chunk" ||
      msg.method === "workflow/stage_start" ||
      msg.method === "workflow/stage_done"
    ) {
      const evSessionId = msg.params && msg.params.session_id;
      const onScreenSessionId = projectState(projectId).sessionId;
      if (!evSessionId || evSessionId === onScreenSessionId) {
        toRenderer({ type: "trajectoryEvent", event: { type: msg.method, data: msg.params || {} } });
      }
    }
    if (msg.method === "exec/output_chunk") {
      toRenderer({ type: "execChunk", chunk: (msg.params && msg.params.chunk) || "" });
      return;
    }
    if (msg.method !== "agent/event") {
      return;
    }
    const ev = msg.params || {};
    const isChild = ev.scope === "child";
    const blocks = blocksForProject(projectId);

    switch (ev.type) {
      case "message_delta":
        if (ev.content && !isChild) {
          const acc = (turnTextByProject.get(projectId) || "") + ev.content;
          turnTextByProject.set(projectId, acc);
          toRenderer({ type: "deltaSync", content: acc });
        }
        break;

      case "reasoning_delta":
        if (ev.content && !isChild) {
          turnReasoningByProject.set(projectId, (turnReasoningByProject.get(projectId) || "") + ev.content);
          toRenderer({ type: "reasoningDelta", content: ev.content });
        }
        break;

      // A tool block is drawn in three phases, and the renderer (shared with
      // the editor's webview: chat-src/05e-messages.js) reads exactly the
      // fields the editor host posts — phase, toolCallId, toolName, argsDelta,
      // content. It does nothing at all with any other shape, which is why
      // these must stay in step with ui/vscode/src/chat/panel.ts.
      case "tool_call_start": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = {
          id: ev.tool_call_id,
          name: ev.tool_call_name || "tool",
          argsRaw: "",
          status: "running",
          result: "",
          startedAt: Date.now(),
        };
        blocks.set(ev.tool_call_id, block);
        toRenderer({
          type: "toolBlock",
          phase: "start",
          toolCallId: block.id,
          toolName: block.name,
          step: ev.step,
        });
        break;
      }

      case "tool_call_delta": {
        if (isChild || !ev.tool_call_id || !ev.args_delta) {
          break;
        }
        const block = blocks.get(ev.tool_call_id);
        if (block) {
          block.argsRaw += ev.args_delta;
        }
        toRenderer({
          type: "toolBlock",
          phase: "update",
          toolCallId: ev.tool_call_id,
          toolName: ev.tool_call_name || (block && block.name) || "tool",
          argsDelta: ev.args_delta,
          step: ev.step,
        });
        break;
      }

      case "tool_call_completed": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = blocks.get(ev.tool_call_id) || {
          id: ev.tool_call_id,
          name: ev.tool_call_name || "tool",
          argsRaw: "",
          startedAt: 0,
        };
        const content = ev.content || "";
        block.status = toolStatusOf(content);
        block.result = content;
        block.diagnostics = Array.isArray(ev.diagnostics) ? ev.diagnostics : undefined;
        // Only when this page saw the start; a tool whose start went to
        // another page has no honest duration to keep.
        block.durationMs = block.startedAt ? Date.now() - block.startedAt : 0;
        blocks.delete(ev.tool_call_id);
        turnToolsFor(projectId).push(block);
        toRenderer({
          type: "toolBlock",
          phase: "complete",
          toolCallId: block.id,
          toolName: block.name,
          content,
          diagnostics: block.diagnostics,
          step: ev.step,
        });
        break;
      }

      case "child_started":
        toRenderer({
          type: "childLifecycle",
          phase: "started",
          taskId: ev.task_id || "",
          parentToolCallId: ev.parent_tool_call_id,
          subagentType: ev.subagent_type,
          content: ev.content,
        });
        break;

      case "child_done":
        toRenderer({
          type: "childLifecycle",
          phase: "done",
          taskId: ev.task_id || "",
          parentToolCallId: ev.parent_tool_call_id,
          subagentType: ev.subagent_type,
          status: ev.status,
          error: ev.error,
        });
        break;

      case "step_usage":
      case "context_estimate": {
        // The context gauge under the composer. step_usage is what the server
        // counted for the step; context_estimate is the agent's own count of
        // what it is about to send, with the per-category breakdown the
        // popover draws — it comes first, and the measurement replaces it.
        // The renderer keeps a worker's (child) usage off the gauge and counts
        // only its cost, so the scope travels with the message; a worker's
        // estimate has neither use and stops here. So does a measurement of
        // nothing — a server that reports no usage must not empty the ring.
        const usage = stepUsageFrom(ev.data);
        if (!usage) {
          break;
        }
        if (ev.type === "context_estimate") {
          if (isChild) {
            break;
          }
          usage.source = "estimate";
        }
        if (!isChild && !(usage.prompt_tokens > 0)) {
          break;
        }
        // What the server counted goes onto the saved answer as well, so a
        // reopened session can show what the turn cost. An estimate does not:
        // only a measurement may be recorded as spend.
        if (!isChild && ev.type === "step_usage") {
          const acc = turnUsageByProject.get(projectId) || {};
          if (usage.prompt_tokens > 0) acc.promptCtx = usage.prompt_tokens;
          if (usage.completion_tokens > 0) acc.tokensOut = usage.completion_tokens;
          if (usage.total_tokens > 0) acc.tokensIn = usage.total_tokens;
          turnUsageByProject.set(projectId, acc);
        }
        toRenderer({ type: "stepUsage", usage, scope: ev.scope });
        break;
      }

      case "recoverable_error":
      case "error": {
        // Housekeeping about the context window travels on the error channel
        // because that is the channel the agent has, but none of it is a
        // failure. The editor's webview translates these in
        // streamSanitize.ts; the web UI never did, so a routine "the history
        // is about to be summarised" arrived in the transcript as the bare
        // word CONTEXT_PRESSURE under a red error heading.
        const note = compactionNotice(ev.content || "");
        if (note) {
          toRenderer({ type: "systemNote", text: note });
          break;
        }
        toRenderer({ type: "error", message: ev.content || "error" });
        break;
      }

      default:
        break;
    }
  }

  /**
   * The usage in a step_usage or context_estimate event, in the shape the
   * renderer's stepUsage handler reads — the same reading parseStepUsage in
   * ui/vscode/src/chat/panel.ts does for the editor. Fields that are not
   * numbers are left out rather than zeroed; a breakdown row without a key or
   * without tokens is dropped.
   * @param {any} data @returns {any|null}
   */
  function stepUsageFrom(data) {
    if (!data || typeof data !== "object") {
      return null;
    }
    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const usage = {
      prompt_tokens: num(data.prompt_tokens),
      completion_tokens: num(data.completion_tokens),
      total_tokens: num(data.total_tokens),
      cost_usd: num(data.cost_usd),
      source: typeof data.source === "string" ? data.source : undefined,
    };
    const breakdown = Array.isArray(data.breakdown)
      ? data.breakdown
          .filter((b) => b && typeof b === "object")
          .map((b) => ({
            key: typeof b.key === "string" ? b.key : "",
            label: typeof b.label === "string" ? b.label : "",
            tokens: num(b.tokens) || 0,
          }))
          .filter((b) => b.key !== "" && b.tokens > 0)
      : [];
    if (breakdown.length > 0) {
      usage.breakdown = breakdown;
    }
    return usage;
  }

  /**
   * The plain-language version of a context-housekeeping notice, or "" when
   * the message is a real error. Kept in step with
   * ui/vscode/src/chat/streamSanitize.ts, which does the same job for the
   * editor's webview.
   * @param {string} message
   */
  function compactionNotice(message) {
    const m = String(message || "").trim();
    if (m === "CONTEXT_PRESSURE") {
      return "The context is nearly full — the history will be summarised before the next step.";
    }
    if (m === "CONTEXT_COMPACTED") {
      return "History summarised; the turn carries on.";
    }
    if (/контекст переполнен/i.test(m)) {
      return "Summarising the chat — " + m;
    }
    return "";
  }

  /** The tools this project's turn has finished, in the order they ran. */
  function turnToolsFor(projectId) {
    let list = turnToolsByProject.get(projectId);
    if (!list) {
      list = [];
      turnToolsByProject.set(projectId, list);
    }
    return list;
  }

  /**
   * The persisted spelling of a finished tool's status, read off its result
   * the way the editor host's toolStatusFromResult does.
   * @param {string} content
   */
  function toolStatusOf(content) {
    const s = String(content || "");
    if (s.startsWith("error: ")) return "failed";
    if (s.startsWith("skipped: ")) return "skipped";
    return "completed";
  }

  /**
   * The turn's answer as a ui_messages row, or null when the model said
   * nothing at all. Mirrors buildAssistantProjection in
   * ui/vscode/src/chat/turnProjection.ts — the same session file is read back
   * by the editor, the TUI and this page, so the shape is not ours to invent.
   * @param {string} projectId
   */
  function assistantTurnProjection(projectId) {
    const text = (turnTextByProject.get(projectId) || "").trim();
    const reasoning = (turnReasoningByProject.get(projectId) || "").trim();
    const tools = (turnToolsByProject.get(projectId) || []).map((t) => {
      const b = { name: t.name || "tool", status: t.status || "completed" };
      if (t.id) b.id = t.id;
      if (t.argsRaw) b.args_raw = t.argsRaw;
      if (t.result) b.result = t.result;
      if (t.diagnostics && t.diagnostics.length) b.diagnostics = t.diagnostics;
      // Absent rather than zero: a persisted 0 renders as "0ms" beside tools
      // that really did take no measurable time.
      if (t.durationMs > 0) b.duration_ms = t.durationMs;
      return b;
    });
    if (!text && !reasoning && tools.length === 0) {
      return null;
    }
    const msg = { role: "assistant", text };
    if (reasoning) msg.reasoning = reasoning;
    if (tools.length) msg.tool_blocks = tools;
    const usage = turnUsageByProject.get(projectId) || {};
    if (usage.promptCtx > 0) msg.prompt_ctx = usage.promptCtx;
    if (usage.tokensIn > 0) msg.tokens_in = usage.tokensIn;
    if (usage.tokensOut > 0) msg.tokens_out = usage.tokensOut;
    return msg;
  }

  // A new turn starts with an empty transcript — for the project whose turn it
  // is, which is always the one the renderer is showing.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnTextByProject.set(currentProjectId, "");
      turnReasoningByProject.set(currentProjectId, "");
      turnToolsByProject.set(currentProjectId, []);
      turnUsageByProject.delete(currentProjectId);
      blocksForProject(currentProjectId).clear();
    }
  });
  // Server-initiated requests: permission/request and question/ask.
  //
  // These are why the transport is a WebSocket. The renderer already has the
  // overlays (05b-overlays.js); this fragment is the wiring between them and
  // the JSON-RPC ids that must be answered — an unanswered id is a tool that
  // waits forever.

  // Which project's ask is on screen right now. The renderer's reply carries
  // no project and no request id, so this is the only thing that can tell us
  // who a click belongs to. Resolving against currentProjectId instead means a
  // switch mid-prompt answers the wrong project — see activateProject in
  // 10-adapter-session.js, which is the other half of that fix.
  let displayedAsk = null;

  /** @param {string} projectId @param {{kind: string, id: any}} ask */
  function setDisplayedAsk(projectId, ask) {
    displayedAsk = { projectId, kind: ask.kind, id: ask.id };
  }

  function clearDisplayedAsk() {
    displayedAsk = null;
  }

  /**
   * Whether the ask currently on screen belongs to projectId. 20-adapter-
   * events.js uses this to decide whether a turn ending elsewhere must also
   * take the overlay down with it, so a stale ask and a stale overlay never
   * separate.
   * @param {string} projectId
   */
  function isDisplayedAskFor(projectId) {
    return Boolean(displayedAsk && displayedAsk.projectId === projectId);
  }

  /**
   * @param {string} projectId @param {any} msg
   */
  function handleServerRequest(projectId, msg) {
    const st = projectState(projectId);
    switch (msg.method) {
      case "permission/request":
        st.pendingAsk = {
          kind: "permission",
          id: msg.id,
          rendererMessage: { type: "permissionRequest", request: msg.params || {} },
        };
        st.status = "asking";
        break;
      case "question/ask":
        st.pendingAsk = {
          kind: "question",
          id: msg.id,
          rendererMessage: {
            type: "questionAsk",
            questions: (msg.params && msg.params.questions) || [],
          },
        };
        st.status = "asking";
        break;
      default:
        // An unknown server request must still be answered, or the core waits.
        connFor(projectId).reply(msg.id, { error: "unsupported" });
        return;
    }
    // Only the project on screen may raise an overlay. A background project's
    // prompt waits in its record and is raised by activateProject.
    if (projectId === currentProjectId) {
      toRenderer(st.pendingAsk.rendererMessage);
      setDisplayedAsk(projectId, st.pendingAsk);
    }
    renderProjects();
  }

  // The overlays answer through the renderer's existing messages. Intercept
  // them here rather than in dispatchToCore, because they carry an id that
  // belongs to this fragment.
  window.addEventListener("message", (ev) => {
    const msg = ev.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    if (msg.type === "__host_dispatch__" && msg.payload) {
      const p = msg.payload;
      // The renderer's reply carries no project id and no request id, so it is
      // resolved against whichever ask is actually displayed on screen — never
      // against currentProjectId, which may already name a different project
      // by the time the click lands.
      if (!displayedAsk) {
        return; // stale click; nothing is on screen to answer
      }
      const st = projectState(displayedAsk.projectId);
      if (p.type === "permissionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "permission" || displayedAsk.kind !== "permission") {
          return; // stale click; answering some other id would be worse
        }
        connFor(displayedAsk.projectId).reply(displayedAsk.id, {
          approved: Boolean(p.approved),
          always: Boolean(p.always),
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        clearDisplayedAsk();
        renderProjects();
      } else if (p.type === "questionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "question" || displayedAsk.kind !== "question") {
          return;
        }
        connFor(displayedAsk.projectId).reply(displayedAsk.id, {
          answers: Array.isArray(p.answers) ? p.answers : [],
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        clearDisplayedAsk();
        renderProjects();
      }
    }
  });
  // The projects module: the rail, one connection per open project, and the
  // switch between them.
  //
  // Division of labour. This fragment owns "which projects exist and which one
  // is on screen"; 10/20/30 own "what one project's session is doing". The
  // renderer is never told about more than one project's content — see
  // activateProject — which is what keeps ui/vscode/media/chat-src unchanged.

  /** @type {Map<string, any>} */
  const conns = new Map();
  /** @type {Array<any>} */
  let known = [];
  /** Each project's sessions, as last listed. @type {Map<string, Array<any>>} */
  const sessionsByProject = new Map();
  /** Projects the user folded away by hand. @type {Set<string>} */
  const collapsedProjects = new Set();
  /**
   * The live search over saved chats. hits=null means "not searching", so the
   * sidebar draws the ordinary list; an empty array means "searched and found
   * nothing", which has to look different from it.
   * @type {{projectId: string, query: string, hits: Array<any>|null}}
   */
  const sessionSearch = { projectId: "", query: "", hits: null };

  /**
   * Full-text search across this workspace's saved chats (session.search,
   * protocol v14 — implemented in the core since then and never called by any
   * interface until now). Results replace the session list in place, so a hit
   * is opened by the same click that opens any chat.
   * @param {string} query
   */
  async function showSessionSearch(query) {
    const q = String(query || "").trim();
    if (!q) {
      sessionSearch.projectId = "";
      sessionSearch.query = "";
      sessionSearch.hits = null;
      renderProjects();
      return;
    }
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      return;
    }
    sessionSearch.projectId = projectId;
    sessionSearch.query = q;
    revealSessionList();
    let hits = [];
    try {
      const r = (await conn.send("session.search", { query: q, insensitive: true, limit: 50 })) || {};
      hits = Array.isArray(r.hits) ? r.hits : [];
    } catch (err) {
      hits = [];
    }
    // A slow search that lands after the user moved on must not repaint
    // someone else's sidebar.
    if (sessionSearch.query !== q || sessionSearch.projectId !== projectId) {
      return;
    }
    // One row per chat: a chat matching six times is one chat to open, and the
    // first hit carries the snippet worth showing.
    const seen = new Set();
    sessionSearch.hits = hits.filter((h) => {
      const id = String((h && h.session_id) || "");
      if (!id || seen.has(id)) {
        return false;
      }
      seen.add(id);
      return true;
    });
    renderProjects();
  }

  /**
   * Called by refreshSessionList for every project, on screen or not, so the
   * sidebar can show a project's sessions without re-asking the core.
   * @param {string} projectId @param {Array<any>} sessions
   */
  function noteSessionList(projectId, sessions) {
    sessionsByProject.set(projectId, Array.isArray(sessions) ? sessions : []);
    renderProjects();
  }

  /**
   * When a session was last touched, in ms, or NaN. Sessions written by an
   * older core can lack updated_at, so fall back to the timestamp in the id
   * (YYYYMMDDTHHMMSS-xxxx) exactly as the session menu does.
   * @param {any} s
   */
  function sessionStamp(s) {
    const ms = Date.parse(s.updated_at || s.created_at || "");
    if (isFinite(ms)) {
      return ms;
    }
    const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s.id || "");
    return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]).getTime() : NaN;
  }

  /** Most recently touched first, for both the sidebar and the tab strip. */
  function byRecent(a, b) {
    const x = sessionStamp(a);
    const y = sessionStamp(b);
    return (isFinite(y) ? y : 0) - (isFinite(x) ? x : 0);
  }

  /** "4m" / "3h" / "5d" for a session row. @param {any} s */
  function sessionAge(s) {
    const ms = sessionStamp(s);
    if (!isFinite(ms)) {
      return "";
    }
    const secs = Math.max(0, Math.round((Date.now() - ms) / 1000));
    if (secs < 60) {
      return secs + "s";
    }
    const mins = Math.round(secs / 60);
    if (mins < 60) {
      return mins + "m";
    }
    const hours = Math.round(mins / 60);
    if (hours < 24) {
      return hours + "h";
    }
    return Math.round(hours / 24) + "d";
  }

  /** The Conn for an open project, or null. @param {string} projectId */
  function connFor(projectId) {
    return conns.get(projectId) || null;
  }

  /**
   * Create the project's connection if it has none. The handshake runs from
   * onOpen, so a caller only awaits the socket, not the session.
   * @param {string} projectId
   */
  function ensureConn(projectId) {
    const existing = conns.get(projectId);
    if (existing) {
      return existing;
    }
    const conn = createConn(projectId, {
      onOpen: (id) => {
        if (id === currentProjectId) {
          toRenderer({ type: "status", status: "ok" });
        }
        void onConnected(id);
      },
      onClose: (id) => {
        conns.delete(id);
        forgetProjectState(id);
        noteProjectLive(id);
        if (id === currentProjectId) {
          // A dropped socket ends the session on the core side, so say so
          // plainly rather than reconnecting into what looks like the same
          // conversation.
          toRenderer({
            type: "status",
            status: "error",
            detail: "disconnected — reload to start a new session",
          });
        }
        renderProjects();
      },
      onError: (id) => {
        if (id === currentProjectId) {
          toRenderer({ type: "status", status: "error", detail: "connection error" });
        }
        noteProjectLive(id);
      },
      onNotification: (id, msg) => {
        if (noteProjectEvent(id, msg)) {
          renderProjects();
        }
      },
      onServerRequest: (id, msg) => {
        handleServerRequest(id, msg);
        if (projectState(id).status === "asking") {
          notifyAsking(id);
        }
      },
    });
    conns.set(projectId, conn);
    return conn;
  }

  /**
   * Show a project. A closed one is opened first; its entry in the list has
   * the path, which is what POST /api/projects takes.
   * @param {string} projectId
   */
  async function switchProject(projectId) {
    if (!projectId || projectId === currentProjectId) {
      return;
    }
    const entry = known.find((p) => p.id === projectId);
    if (entry && entry.state === "closed") {
      // Marked before the await, because this is the slow half: a closed
      // workspace has to start a core, which is a second or two of a sidebar
      // that looked like it had ignored the click.
      markRailOpening(projectId);
      // init: true, as on the start screen. A workspace in this list is one
      // the person put there; refusing it for want of a .orchestra.yml left
      // the click doing nothing but writing a note into the transcript of
      // whichever project they were still looking at.
      const opened = await openProject(entry.path, true);
      markRailOpening("");
      if (!opened) {
        return;
      }
    }
    ensureConn(projectId);
    await activateProject(projectId);
  }

  /**
   * Name the workspace in the chat's own header. The sidebar says which one is
   * open, but it is a column of names off to one side and can now be folded
   * away entirely — while the transcript, the tabs and the composer look the
   * same whichever project they belong to. Sending a message to the wrong
   * workspace is a real mistake and nothing on that half of the window stood
   * in its way.
   *
   * Built here rather than in the markup on purpose: everything inside #app is
   * byte-identical with the VS Code webview's, and the editor's panel always
   * has its one folder, so it has nothing to name. The element is created
   * beside the logo at runtime, which leaves that parity alone.
   */
  /**
   * Held rather than looked up: it carries no id, because check-web requires
   * every id the code reaches for to exist in index.html, and this element
   * deliberately does not — it is built at runtime precisely so the shared
   * markup stays untouched.
   * @type {any}
   */
  let chromeProjectEl = null;

  /** @type {any} */
  let skeletonEl = null;

  /**
   * Grey placeholder lines in the transcript while a workspace is coming up.
   * Built at runtime and appended to #messages, which the renderer clears
   * with its own clearMessages when the real transcript arrives — so the
   * placeholder is gone the moment there is something to show instead.
   * @param {boolean} on
   */
  function paintSkeleton(on) {
    const messages = document.getElementById("messages");
    if (!messages || !messages.appendChild) {
      return;
    }
    const attached = Boolean(skeletonEl && skeletonEl.parentNode === messages);
    if (!on) {
      if (attached && messages.removeChild) {
        messages.removeChild(skeletonEl);
      }
      return;
    }
    if (attached) {
      return;
    }
    // Only into an empty transcript: a project switched to while another was
    // on screen paints over that one's history, not under it.
    if (messages.childNodes && messages.childNodes.length > 0) {
      return;
    }
    skeletonEl = document.createElement("div");
    skeletonEl.className = "skel";
    skeletonEl.setAttribute("aria-hidden", "true");
    // Two exchanges' worth of lines: a short one on the right, a longer run
    // on the left. Widths vary so it reads as text, not as bars.
    const rows = [
      ["skel-row skel-user", "34%"],
      ["skel-row", "72%"],
      ["skel-row", "58%"],
      ["skel-row", "66%"],
      ["skel-row skel-user", "22%"],
      ["skel-row", "80%"],
      ["skel-row", "47%"],
    ];
    for (const [cls, width] of rows) {
      const row = document.createElement("div");
      row.className = cls;
      if (row.style && row.style.setProperty) {
        row.style.setProperty("--w", width);
      }
      skeletonEl.appendChild(row);
    }
    messages.appendChild(skeletonEl);
  }

  function paintChromeProject() {
    const brand = document.getElementById("chrome-brand");
    if (!brand || !brand.appendChild) {
      return;
    }
    if (!chromeProjectEl) {
      chromeProjectEl = document.createElement("span");
      chromeProjectEl.className = "chrome-project";
      brand.appendChild(chromeProjectEl);
    }
    const label = chromeProjectEl;
    // The one on screen; failing that, the one on its way, so the name is up
    // before the core is.
    const entry =
      known.find((p) => p.id === currentProjectId) ||
      known.find((p) => pendingOpenId && p.id === pendingOpenId) ||
      known.find((p) => pendingOpenPath && p.path === pendingOpenPath);
    // textContent: a folder name off disk.
    label.textContent = (entry && (entry.name || entry.path)) || "";
    label.title = (entry && entry.path) || "";
    label.hidden = !label.textContent;
    if (brand.removeAttribute && label.textContent) {
      // The brand block was decoration while it held only the logo; it names
      // the workspace now, so it stops being hidden from a screen reader.
      brand.removeAttribute("aria-hidden");
    }
  }

  /** The workspace whose core is starting, so its row can say so. */
  let railOpeningId = "";

  /** @param {string} projectId "" clears it. */
  function markRailOpening(projectId) {
    railOpeningId = projectId || "";
    renderProjects();
  }

  /**
   * Start a session in a workspace, opening its core first if it has none.
   * The add button sits on a workspace heading, and a workspace can be listed
   * with its core closed — after a reload every one of them is. Reaching
   * session.start through a connection that is not there printed
   * "Cannot read properties of null (reading 'send')" into the transcript,
   * which is the whole reason this exists rather than a bare startSession.
   * @param {string} projectId
   */
  async function newSessionIn(projectId) {
    const id = projectId || currentProjectId;
    if (!id) {
      return;
    }
    if (id !== currentProjectId) {
      await switchProject(id);
      if (id !== currentProjectId) {
        return;
      }
    }
    if (!connFor(id)) {
      // switchProject returns early for the project already on screen, so a
      // closed current workspace is opened here.
      const entry = known.find((p) => p.id === id);
      if (entry && entry.state === "closed") {
        markRailOpening(id);
        const opened = await openProject(entry.path, true);
        markRailOpening("");
        if (!opened) {
          return;
        }
      }
      ensureConn(id);
      await activateProject(id);
      if (!connFor(id)) {
        return;
      }
    }
    await startSession(undefined);
  }

  // ---- the HTTP half -----------------------------------------------------

  /** @param {string} path @param {any} init */
  async function api(path, init) {
    const res = await fetch(path, {
      ...(init || {}),
      headers: { "Content-Type": "application/json", ...((init && init.headers) || {}) },
    });
    if (res.status === 204) {
      return {};
    }
    let body = {};
    try {
      body = await res.json();
    } catch (e) {
      body = {};
    }
    if (!res.ok) {
      const err = new Error(body.error || `request failed (${res.status})`);
      // @ts-ignore — the caller distinguishes not_initialized from the rest.
      err.code = body.error || "";
      // @ts-ignore
      err.path = body.path || "";
      throw err;
    }
    return body;
  }

  async function refreshProjects() {
    try {
      const body = await api("/api/projects", { method: "GET" });
      known = Array.isArray(body.projects) ? body.projects : [];
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not list projects: " + String(err && err.message ? err.message : err),
      });
    }
    renderProjects();
  }

  /**
   * Open a folder. `init` asks the server to write .orchestra.yml first; the
   * caller only sets it after the user agreed to that.
   * @param {string} path @param {boolean} init
   * @returns {Promise<boolean>} whether the project is now open
   */
  async function openProject(path, init) {
    try {
      await api("/api/projects", {
        method: "POST",
        body: JSON.stringify({ path, init: Boolean(init) }),
      });
      await refreshProjects();
      return true;
    } catch (err) {
      // Already open is not a failure: the caller asked for this project to be
      // available and it is. Without this the start screen read 409 as a
      // refusal, fell through to the "initialise it?" path, and asked about a
      // project that was open the whole time.
      // @ts-ignore
      if (err && err.code === "already_open") {
        await refreshProjects();
        return true;
      }
      // @ts-ignore
      if (err && err.code === "not_initialized" && !init) {
        toRenderer({
          type: "systemNote",
          text: `${path} is not an Orchestra project yet. Use "Add project" again and confirm initialising it.`,
        });
        return false;
      }
      toRenderer({
        type: "systemNote",
        text: "could not open " + path + ": " + String(err && err.message ? err.message : err),
      });
      return false;
    }
  }

  /** Close a project. It stays in the list. @param {string} projectId */
  async function closeProject(projectId) {
    const conn = conns.get(projectId);
    if (conn) {
      conn.close();
      conns.delete(projectId);
    }
    forgetProjectState(projectId);
    try {
      await api("/api/projects/" + encodeURIComponent(projectId), { method: "DELETE" });
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not close the project: " + String(err && err.message ? err.message : err),
      });
    }
    await refreshProjects();
    if (projectId === currentProjectId) {
      const next = known.find((p) => p.state === "ready" && p.id !== projectId);
      if (next) {
        await switchProject(next.id);
      }
    }
  }

  /** Close a project and drop it from the list. @param {string} projectId */
  async function forgetProject(projectId) {
    const conn = conns.get(projectId);
    if (conn) {
      conn.close();
      conns.delete(projectId);
    }
    forgetProjectState(projectId);
    try {
      await api("/api/projects/" + encodeURIComponent(projectId) + "?forget=1", {
        method: "DELETE",
      });
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not remove the project: " + String(err && err.message ? err.message : err),
      });
    }
    await refreshProjects();
  }

  // ---- the rail ----------------------------------------------------------

  /**
   * Tell the person a project they are not looking at needs an answer.
   *
   * Only the desktop shell can raise a Windows notification, and only because
   * capabilities/core-page.json grants this page exactly that call; in a plain
   * browser there is nothing to call and the rail's badge is the whole signal.
   * The active project never notifies — its prompt is already on screen.
   * @param {string} projectId
   */
  function notifyAsking(projectId) {
    if (projectId === currentProjectId) {
      return;
    }
    const entry = known.find((p) => p.id === projectId);
    const name = (entry && (entry.name || entry.path)) || projectId;
    const t = window.__TAURI__;
    if (!t || !t.notification || !t.notification.sendNotification) {
      return;
    }
    try {
      t.notification.sendNotification({
        title: "Orchestra",
        body: name + " is waiting for your answer",
      });
    } catch (e) {
      // A notification that cannot be raised is not worth an error in the chat.
    }
  }

  /* The plus, drawn once. A literal — no value from the core reaches it. */
  const PLUS_SVG =
    '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
    '<path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>';

  /**
   * The one affordance for adding: it sits on the heading of the group it
   * adds to — a session under the workspace it belongs to, a workspace under
   * the list of workspaces — so neither needs a bar of its own.
   * @param {string} action "new-session" | "add-project"
   * @param {string} label
   * @param {string} [projectId]
   * @returns {HTMLElement}
   */
  function railAddButton(action, label, projectId) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "rail-add";
    btn.dataset.railAction = action;
    if (projectId) {
      btn.dataset.projectId = projectId;
    }
    btn.title = label;
    btn.setAttribute("aria-label", label);
    btn.innerHTML = PLUS_SVG;
    return btn;
  }

  /**
   * The button that throws a chat away. It asks twice — the first click arms
   * it for a few seconds — because there is no undo: the core removes the
   * snapshot and the event log from disk.
   * @param {string} projectId @param {string} sessionId
   */
  function railDeleteButton(projectId, sessionId) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "rail-session-del";
    btn.dataset.projectId = projectId;
    btn.dataset.sessionId = sessionId;
    btn.title = "Delete this chat";
    btn.setAttribute("aria-label", "Delete this chat");
    btn.textContent = "×";
    let armed = 0;
    btn.addEventListener("click", (e) => {
      if (e && e.stopPropagation) e.stopPropagation();
      if (e && e.preventDefault) e.preventDefault();
      if (!armed) {
        armed = 1;
        btn.classList.add("armed");
        btn.textContent = "Delete?";
        btn.title = "Click again to delete this chat for good";
        setTimeout(() => {
          if (!armed) return;
          armed = 0;
          btn.classList.remove("armed");
          btn.textContent = "×";
          btn.title = "Delete this chat";
        }, 4000);
        return;
      }
      armed = 0;
      void deleteSession(projectId, sessionId);
    });
    return btn;
  }

  /**
   * The workspaces heading, which always carries the add button — including
   * when there is nothing under it yet, which is exactly when a new user
   * needs it.
   * @param {string} label @returns {HTMLElement}
   */
  function railSectionHeading(label) {
    const sec = document.createElement("div");
    sec.className = "rail-section";
    const text = document.createElement("span");
    text.className = "rail-section-label";
    text.textContent = label;
    sec.appendChild(text);
    sec.appendChild(railAddButton("add-project", "Add a workspace folder"));
    return sec;
  }

  /**
   * The box above a workspace's chats. It searches their text, not their
   * titles: the core reads the saved messages, so "that thing about the
   * circuit breaker" finds the chat even when its title says nothing.
   * @param {string} projectId @returns {HTMLElement}
   */
  function railSessionSearchBox(projectId) {
    const box = document.createElement("input");
    box.type = "search";
    box.className = "rail-session-search";
    box.placeholder = "Search chats";
    box.setAttribute("aria-label", "Search this workspace's chats");
    if (sessionSearch.projectId === projectId) {
      box.value = sessionSearch.query;
    }
    let debounce = 0;
    box.addEventListener("input", () => {
      const q = String(box.value || "");
      if (debounce) {
        clearTimeout(debounce);
      }
      // Each keystroke is a file walk across every saved session, so let the
      // typing settle first.
      debounce = setTimeout(() => {
        void showSessionSearch(q);
      }, 200);
    });
    box.addEventListener("keydown", (e) => {
      if (e && e.key === "Escape") {
        box.value = "";
        void showSessionSearch("");
      }
    });
    return box;
  }

  /**
   * The search results, drawn as ordinary chat rows so the rail's existing
   * click handler opens them, with the matching line underneath.
   * @param {HTMLElement} container @param {string} projectId @param {string} openSessionId
   */
  function renderSessionHits(container, projectId, openSessionId) {
    const hits = sessionSearch.hits || [];
    if (hits.length === 0) {
      const empty = document.createElement("div");
      empty.className = "rail-sessions-empty";
      empty.textContent = `No chat mentions “${sessionSearch.query}”`;
      container.appendChild(empty);
      return;
    }
    for (const h of hits) {
      const sessionId = String((h && h.session_id) || "");
      const item = document.createElement("button");
      item.type = "button";
      item.className = "rail-session";
      item.dataset.projectId = projectId;
      item.dataset.sessionId = sessionId;
      item.dataset.active = sessionId === openSessionId ? "true" : "false";
      const title = document.createElement("span");
      title.className = "rail-session-title";
      title.textContent = String((h && h.title) || "") || sessionId || "untitled";
      const age = document.createElement("span");
      age.className = "rail-session-time";
      age.textContent = sessionAge({ id: sessionId, updated_at: h && h.updated_at });
      item.appendChild(title);
      item.appendChild(age);
      const snippet = String((h && h.snippet) || "").trim();
      if (snippet) {
        const line = document.createElement("span");
        line.className = "rail-session-snippet";
        // textContent: a snippet is the user's or the model's own words.
        line.textContent = snippet;
        item.appendChild(line);
        item.title = snippet;
      }
      const rowEl = document.createElement("div");
      rowEl.className = "rail-session-row";
      rowEl.appendChild(item);
      rowEl.appendChild(railDeleteButton(projectId, sessionId));
      container.appendChild(rowEl);
    }
  }

  /**
   * Repaint the rail and tell the renderer what the list looks like. The
   * renderer message exists for the tests and for any future consumer; the
   * rail's own DOM is written here because it is web-only.
   */
  function renderProjects() {
    const rows = known.map((p) => {
      const st = peekProjectState(p.id);
      return {
        id: p.id,
        name: p.name || p.path,
        path: p.path,
        state: p.state,
        status: p.state === "closed" || !st ? "closed" : st.status,
        active: p.id === currentProjectId,
        // -1 from the server means "could not read in time", which the rail
        // shows as no number rather than as none.
        sessions: typeof p.sessions === "number" ? p.sessions : -1,
      };
    });
    toRenderer({ type: "projectList", projects: rows });
    // Before the early return below: the editor's webview has neither a rail
    // nor a start screen, and syncStartScreen is a no-op there, but the web
    // host must not skip it just because the rail is missing.
    syncStartScreen();

    paintChromeProject();

    const list = document.getElementById("project-rail-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    // The open project heads the list, its sessions under it; everything else
    // follows under one label. The renderer message above keeps the original
    // order — its consumers and tests were written against it.
    const ordered = rows.slice().sort((a, b) => (b.active ? 1 : 0) - (a.active ? 1 : 0));
    const hasActive = rows.some((r) => r.active);
    let otherLabelDone = false;
    // With nothing open there is no "other" to be other than, so the heading
    // is simply the list's own, at the top where a heading belongs.
    if (!hasActive) {
      list.appendChild(railSectionHeading("Workspaces"));
      otherLabelDone = true;
    }
    for (const row of ordered) {
      if (!row.active && hasActive && !otherLabelDone) {
        list.appendChild(railSectionHeading("Other workspaces"));
        otherLabelDone = true;
      }
      // Only the project on screen has a session list — the core is asked for
      // one per connection — so every other group stays folded, and its header
      // switches to it rather than expanding an empty list.
      const collapsed = row.active ? collapsedProjects.has(row.id) : true;

      const group = document.createElement("div");
      group.className = "project-group";
      group.dataset.projectId = row.id;
      group.dataset.active = row.active ? "true" : "false";
      group.dataset.collapsed = collapsed ? "true" : "false";

      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "project-chip";
      chip.dataset.projectId = row.id;
      chip.dataset.state = row.state;
      chip.dataset.status = row.status;
      chip.dataset.active = row.active ? "true" : "false";
      chip.title = row.path + (row.status === "asking" ? " — waiting for you" : "");
      chip.setAttribute("aria-label", row.name + " (" + row.status + ")");
      if (railOpeningId && row.id === railOpeningId) {
        chip.dataset.opening = "true";
      }
      chip.setAttribute("aria-expanded", collapsed ? "false" : "true");

      // Drawn in CSS: an inline SVG here would need createElementNS, which the
      // adapter tests' document stub does not have, and the glyph is decoration.
      const chevron = document.createElement("span");
      chevron.className = "project-chevron";
      chevron.setAttribute("aria-hidden", "true");
      chip.appendChild(chevron);

      const name = document.createElement("span");
      name.className = "project-name";
      // textContent, not innerHTML: the name is a folder name off disk.
      name.textContent = row.name || "?";
      chip.appendChild(name);

      // How many chats the workspace holds. The server counts them off disk,
      // which is the only way a workspace whose core is closed can have a
      // number at all; the open one prefers its live list, so starting a
      // session bumps the count straight away instead of at the next poll.
      const listedNow = sessionsByProject.get(row.id);
      const count = row.active && listedNow ? listedNow.length : row.sessions;
      if (typeof count === "number" && count >= 0) {
        const badge = document.createElement("span");
        badge.className = "project-count";
        badge.textContent = String(count);
        badge.title = count === 1 ? "1 chat" : count + " chats";
        chip.appendChild(badge);
      }

      const dot = document.createElement("span");
      dot.className = "project-dot";
      chip.appendChild(dot);

      // The header is a row, not just the chip: the open workspace carries
      // the button that starts a session in it. A <button> cannot nest inside
      // another, so the two are siblings.
      const head = document.createElement("div");
      head.className = "project-head";
      head.appendChild(chip);
      if (row.active) {
        head.appendChild(railAddButton("new-session", "New session in this workspace", row.id));
      }
      group.appendChild(head);

      const sessions = document.createElement("div");
      sessions.className = "project-sessions";
      const listed = sessionsByProject.get(row.id) || [];
      const openSessionId = (peekProjectState(row.id) || {}).sessionId || "";

      // Searching replaces the list rather than sitting beside it: the whole
      // point is to narrow a hundred chats to the three that mention a thing.
      const searching = row.active && sessionSearch.projectId === row.id && sessionSearch.hits !== null;
      if (row.active) {
        sessions.appendChild(railSessionSearchBox(row.id));
      }
      if (searching) {
        renderSessionHits(sessions, row.id, openSessionId);
        group.appendChild(sessions);
        list.appendChild(group);
        continue;
      }
      // session.list only returns sessions that have been written to disk, and
      // a session is written by its first message — so the session the user is
      // looking at is missing from the list until they say something. Showing
      // it anyway is the difference between "where am I" and an empty sidebar.
      const openIsListed = listed.some((s) => s.id === openSessionId);
      if (row.active && openSessionId && !openIsListed) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "rail-session";
        item.dataset.projectId = row.id;
        item.dataset.sessionId = openSessionId;
        item.dataset.active = "true";
        const title = document.createElement("span");
        title.className = "rail-session-title";
        title.textContent = "New session";
        item.title = openSessionId;
        const age = document.createElement("span");
        age.className = "rail-session-time";
        age.textContent = "now";
        item.appendChild(title);
        item.appendChild(age);
        sessions.appendChild(item);
      }
      if (listed.length === 0 && !(row.active && openSessionId)) {
        const empty = document.createElement("div");
        empty.className = "rail-sessions-empty";
        empty.textContent = "No sessions yet";
        sessions.appendChild(empty);
      }
      for (const s of listed.slice(0, 100)) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "rail-session";
        item.dataset.projectId = row.id;
        item.dataset.sessionId = s.id || "";
        item.dataset.active = row.active && s.id === openSessionId ? "true" : "false";
        const title = document.createElement("span");
        title.className = "rail-session-title";
        // textContent: titles are the user's own first message.
        title.textContent = s.title || s.id || "untitled";
        item.title = s.title || s.id || "";
        const age = document.createElement("span");
        age.className = "rail-session-time";
        age.textContent = sessionAge(s);
        item.appendChild(title);
        item.appendChild(age);
        // A button cannot nest in a button, so the row is a pair: the chat
        // itself, and the one that throws it away.
        const rowEl = document.createElement("div");
        rowEl.className = "rail-session-row";
        rowEl.appendChild(item);
        rowEl.appendChild(railDeleteButton(row.id, s.id || ""));
        sessions.appendChild(rowEl);
      }
      group.appendChild(sessions);
      list.appendChild(group);
    }
    // The inline heading is only written when other workspaces follow it. When
    // none do — one workspace open, or none at all on a first run — the
    // heading still has to appear, because it carries the only way to add one.
    if (!otherLabelDone) {
      list.appendChild(railSectionHeading("Workspaces"));
    }
    pushSessionTabs();
  }

  // ---- the header's tab strip ---------------------------------------------
  //
  // The strip itself is chat-src's (renderSessionTabs in 02-util.js), painted
  // from the "sessionTabs" message and clicked back through "openSession" /
  // "closeSession" — exactly what the VS Code panel drives. All the web was
  // ever missing is a sender, so this is it; nothing inside #app changes.
  //
  // Hence "push", not "render": every fragment shares one function scope, so
  // naming this renderSessionTabs redefined the renderer's own and routed
  // 07-events.js straight back here, posting the message again forever.
  // check-web.mjs fails on a redeclaration now.

  /** Sessions closed out of the strip, per project. @type {Map<string, Set<string>>} */
  const hiddenTabs = new Map();

  /** @param {string} projectId */
  function hiddenTabsFor(projectId) {
    let set = hiddenTabs.get(projectId);
    if (!set) {
      set = new Set();
      hiddenTabs.set(projectId, set);
    }
    return set;
  }

  /** The tabs for the project on screen, most recent first. */
  function visibleTabs() {
    if (!currentProjectId) {
      return [];
    }
    const hidden = hiddenTabsFor(currentProjectId);
    return (sessionsByProject.get(currentProjectId) || [])
      .filter((s) => s.id && !hidden.has(s.id))
      .slice()
      .sort(byRecent);
  }

  function pushSessionTabs() {
    const openSessionId = currentProjectId
      ? (peekProjectState(currentProjectId) || {}).sessionId || ""
      : "";
    const tabs = visibleTabs()
      .slice(0, 16)
      .map((s) => ({
        id: s.id,
        title: (s.title || "New chat").trim() || "New chat",
        model: s.model,
        msg_count: s.msg_count,
      }));
    // session.list only returns sessions already written to disk, and a
    // session is written by its first message — so the one being looked at
    // has no row until the user says something. Lead with it anyway.
    const shown = openSessionId && !hiddenTabsFor(currentProjectId || "").has(openSessionId);
    if (shown && !tabs.some((t) => t.id === openSessionId)) {
      tabs.unshift({ id: openSessionId, title: "New chat" });
    }
    toRenderer({ type: "sessionTabs", activeId: openSessionId, tabs });
  }

  /**
   * Closing a tab hides it from the strip: the session stays on disk and in
   * the sidebar, which is the list of everything. Closing the one on screen
   * moves to the next tab, or starts a fresh session when none is left —
   * what a tab strip is expected to do.
   * @param {string} sessionId
   */
  function closeSessionTab(sessionId) {
    if (!sessionId || !currentProjectId) {
      return;
    }
    const openSessionId = (peekProjectState(currentProjectId) || {}).sessionId || "";
    hiddenTabsFor(currentProjectId).add(sessionId);
    if (sessionId !== openSessionId) {
      pushSessionTabs();
      return;
    }
    const next = visibleTabs()[0];
    if (next) {
      void openSessionRow(currentProjectId, next.id);
    } else {
      void startSession(undefined);
    }
    pushSessionTabs();
  }

  /**
   * Delete a chat for good: the core removes its snapshot and its event log
   * (session.close, which cancels a running turn first). Closing a tab only
   * hides it from the strip — this is the one that throws the chat away, so
   * the rail's button asks twice before calling it.
   *
   * The chat on screen can be deleted too; a workspace is never left without
   * one, so a fresh session takes its place.
   * @param {string} projectId @param {string} sessionId
   */
  async function deleteSession(projectId, sessionId) {
    if (!projectId || !sessionId) {
      return;
    }
    const conn = connFor(projectId);
    if (!conn || !conn.isOpen()) {
      toRenderer({ type: "error", message: "The workspace is not open, so its chats cannot be deleted." });
      return;
    }
    try {
      await conn.send("session.close", { session_id: sessionId });
    } catch (err) {
      toRenderer({
        type: "error",
        message: "Could not delete the chat: " + String((err && err.message) || err),
      });
      return;
    }
    // Off the strip and out of the cached list before anything is read back,
    // so the row goes away on the click rather than on the next poll.
    hiddenTabsFor(projectId).delete(sessionId);
    noteSessionList(
      projectId,
      (sessionsByProject.get(projectId) || []).filter((s) => s && s.id !== sessionId)
    );
    const st = projectState(projectId);
    if (st.sessionId === sessionId) {
      st.sessionId = "";
      if (projectId === currentProjectId) {
        const next = visibleTabs()[0];
        if (next) {
          await openSessionRow(projectId, next.id);
        } else {
          await startSession(undefined);
        }
      }
    }
    pushSessionTabs();
    void refreshSessionList(projectId);
  }

  /**
   * Put this workspace's chats in front of the user. On the web that is the
   * sidebar — the header's "all sessions" button is hidden here, because the
   * rail is where the list lives — so unfold the rail, open the workspace's
   * group and scroll to the chat on screen. Answers false where there is no
   * rail (the editor's webview), so the caller can fall back to the button.
   */
  function revealSessionList() {
    const list = document.getElementById("project-rail-list");
    if (!list || !currentProjectId) {
      return false;
    }
    if (railCollapsed()) {
      applyRail(0, false);
    }
    collapsedProjects.delete(currentProjectId);
    renderProjects();
    const active = list.querySelector ? list.querySelector('.rail-session[data-active="true"]') : null;
    if (active && active.scrollIntoView) {
      active.scrollIntoView({ block: "nearest" });
    }
    return true;
  }

  // ---- input -------------------------------------------------------------

  /**
   * The scale chat.css puts on :root above 2600px, or 1. Read back rather
   * than duplicated here so the breakpoints stay in one file.
   */
  function rootZoom() {
    try {
      const z = parseFloat(window.getComputedStyle(document.documentElement).zoom);
      return isFinite(z) && z > 0 ? z : 1;
    } catch (e) {
      // No getComputedStyle (the adapter tests' stub) means no zoom either.
      return 1;
    }
  }

  const railMenu = (() => {
    const el = document.createElement("div");
    el.className = "project-menu";
    el.hidden = true;
    if (document.body && document.body.appendChild) {
      document.body.appendChild(el);
    }
    return el;
  })();

  /** @param {string} projectId @param {number} x @param {number} y */
  function openRailMenu(projectId, x, y) {
    railMenu.innerHTML = "";
    const entry = known.find((p) => p.id === projectId);
    if (entry && entry.state !== "closed") {
      const close = document.createElement("button");
      close.type = "button";
      close.textContent = "Close project";
      close.addEventListener("click", () => {
        railMenu.hidden = true;
        void closeProject(projectId);
      });
      railMenu.appendChild(close);
    }
    const forget = document.createElement("button");
    forget.type = "button";
    forget.textContent = "Remove from list";
    forget.addEventListener("click", () => {
      railMenu.hidden = true;
      void forgetProject(projectId);
    });
    railMenu.appendChild(forget);
    // clientX/clientY are viewport pixels, but chat.css zooms :root on very
    // wide displays and this menu is positioned inside that zoom — so the
    // same number means a different place. Divide it back out.
    const z = rootZoom();
    railMenu.style.left = x / z + "px";
    railMenu.style.top = y / z + "px";
    railMenu.hidden = false;
  }

  const railEl = document.getElementById("project-rail-list");
  if (railEl && railEl.addEventListener) {
    railEl.addEventListener("click", (ev) => {
      // The two add buttons, which renderProjects writes onto group headings.
      // First, because they sit inside a heading that is itself clickable.
      const add = ev.target && ev.target.closest ? ev.target.closest("[data-rail-action]") : null;
      if (add) {
        if (add.dataset.railAction === "add-project") {
          void addProject();
        } else if (add.dataset.railAction === "new-session") {
          void newSessionIn(add.dataset.projectId || "");
        }
        return;
      }
      const row = ev.target && ev.target.closest ? ev.target.closest(".rail-session") : null;
      if (row) {
        void openSessionRow(row.dataset.projectId || "", row.dataset.sessionId || "");
        return;
      }
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      const projectId = chip.dataset.projectId || "";
      if (projectId === currentProjectId) {
        // Switching to the project already on screen does nothing, so the
        // header's job there is to fold its session list away.
        if (collapsedProjects.has(projectId)) {
          collapsedProjects.delete(projectId);
        } else {
          collapsedProjects.add(projectId);
        }
        renderProjects();
        return;
      }
      collapsedProjects.delete(projectId);
      void switchProject(projectId);
    });
    railEl.addEventListener("contextmenu", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      if (ev.preventDefault) ev.preventDefault();
      openRailMenu(chip.dataset.projectId || "", ev.clientX || 0, ev.clientY || 0);
    });
    // "Close project" and "Remove from list" were reachable only through
    // contextmenu, which a keyboard user cannot fire at all. Shift+F10 and the
    // ContextMenu key are the platform's own keyboard equivalent of a
    // right-click, so give the focused chip that path into the same menu
    // rather than building a second one.
    railEl.addEventListener("keydown", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      const isMenuKey = ev.key === "ContextMenu" || (ev.key === "F10" && ev.shiftKey);
      if (!isMenuKey) {
        return;
      }
      if (ev.preventDefault) ev.preventDefault();
      const rect = chip.getBoundingClientRect ? chip.getBoundingClientRect() : null;
      const x = rect ? rect.left : 0;
      const y = rect ? rect.bottom : 0;
      openRailMenu(chip.dataset.projectId || "", x, y);
    });
  }
  if (document.addEventListener) {
    document.addEventListener("click", (ev) => {
      if (!railMenu.hidden && ev.target !== railMenu) {
        railMenu.hidden = true;
      }
    });
  }

  // ---- appearance --------------------------------------------------------
  //
  // Web-only. The VS Code webview follows the editor's own theme, so nothing
  // here has a counterpart in chat-src.

  /** @returns {string} "system" | "light" | "dark" */
  function savedTheme() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.theme") : "";
      return v === "light" || v === "dark" ? v : "system";
    } catch (e) {
      // A browser with site data blocked throws on access, not on read.
      return "system";
    }
  }

  /** @param {string} choice */
  function applyTheme(choice) {
    const root = document.documentElement;
    if (!root) return;
    if (choice === "light" || choice === "dark") {
      if (root.setAttribute) root.setAttribute("data-theme", choice);
    } else if (root.removeAttribute) {
      root.removeAttribute("data-theme");
    }
    try {
      if (window.localStorage) {
        if (choice === "system") window.localStorage.removeItem("orchestra.theme");
        else window.localStorage.setItem("orchestra.theme", choice);
      }
    } catch (e) {
      // Not persisting is survivable; the page still honours the click.
    }
  }

  /** The scales Appearance offers, as percentages. @type {string[]} */
  const SCALE_CHOICES = ["100", "110", "125", "150", "175", "200"];

  /** @returns {string} "auto" or one of SCALE_CHOICES */
  function savedScale() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.scale") : "";
      return SCALE_CHOICES.indexOf(v || "") >= 0 ? String(v) : "auto";
    } catch (e) {
      return "auto";
    }
  }

  /**
   * How large the interface is drawn. chat.css picks a default from the
   * viewport, which is only ever a guess about a monitor's physical size;
   * this is the answer for when the guess is wrong.
   * @param {string} choice
   */
  function applyScale(choice) {
    const root = document.documentElement;
    if (!root) return;
    if (SCALE_CHOICES.indexOf(choice) >= 0) {
      if (root.setAttribute) root.setAttribute("data-scale", choice);
    } else if (root.removeAttribute) {
      root.removeAttribute("data-scale");
    }
    try {
      if (window.localStorage) {
        if (SCALE_CHOICES.indexOf(choice) >= 0) {
          window.localStorage.setItem("orchestra.scale", choice);
        } else {
          window.localStorage.removeItem("orchestra.scale");
        }
      }
    } catch (e) {
      // Not persisting is survivable; the page still honours the click.
    }
  }

  // ---- the sidebar's width ------------------------------------------------
  //
  // Dragging its edge sets the width; clicking the edge folds the sidebar away
  // and back. Both are remembered, because a width you have to set again on
  // every launch is not a width you have set.

  /** Narrower than this and a workspace name has nowhere to go. */
  const RAIL_MIN = 200;
  /** Wider than this and the sidebar is competing with the transcript. */
  const RAIL_MAX = 520;
  /** Dragged below this, the sidebar folds rather than becoming unusable. */
  const RAIL_FOLD_AT = 150;

  const railColumn = document.getElementById("project-rail");
  const railResizer = document.getElementById("rail-resizer");

  /** @param {number} px */
  function clampRailWidth(px) {
    return Math.max(RAIL_MIN, Math.min(RAIL_MAX, Math.round(px)));
  }

  /** @returns {{width: number, collapsed: boolean}} */
  function savedRail() {
    let width = 0;
    let collapsed = false;
    try {
      if (window.localStorage) {
        width = parseInt(window.localStorage.getItem("orchestra.railWidth") || "", 10);
        collapsed = window.localStorage.getItem("orchestra.railCollapsed") === "1";
      }
    } catch (e) {
      // A locked-down browser just gets the default.
    }
    return { width: isFinite(width) && width > 0 ? clampRailWidth(width) : 0, collapsed };
  }

  /**
   * @param {number} width 0 keeps whatever width is set — pass one only when
   *   changing it, so folding and unfolding never lose the dragged size.
   * @param {boolean} collapsed
   */
  function applyRail(width, collapsed) {
    const root = document.documentElement;
    if (width && root && root.style && root.style.setProperty) {
      root.style.setProperty("--rail-w", width + "px");
    }
    if (railColumn) {
      railColumn.dataset.collapsed = collapsed ? "true" : "false";
    }
    if (railResizer) {
      railResizer.dataset.collapsed = collapsed ? "true" : "false";
      if (railResizer.setAttribute) {
        railResizer.setAttribute("aria-label", collapsed ? "Show sidebar" : "Sidebar width");
      }
    }
    try {
      if (window.localStorage) {
        if (width) {
          window.localStorage.setItem("orchestra.railWidth", String(width));
        }
        window.localStorage.setItem("orchestra.railCollapsed", collapsed ? "1" : "0");
      }
    } catch (e) {
      // Same: the click still takes effect for this session.
    }
  }

  function railCollapsed() {
    return Boolean(railColumn && railColumn.dataset && railColumn.dataset.collapsed === "true");
  }

  /** The width the sidebar has right now, dragged or default. */
  function railWidthNow() {
    if (railColumn && railColumn.getBoundingClientRect) {
      const w = railColumn.getBoundingClientRect().width;
      if (w > 1) {
        return clampRailWidth(w);
      }
    }
    return savedRail().width || 296;
  }

  {
    const start = savedRail();
    applyRail(start.width, start.collapsed);
  }

  if (railResizer && railResizer.addEventListener && railColumn) {
    let dragging = false;
    let moved = false;
    let originLeft = 0;
    let startX = 0;

    const endDrag = () => {
      if (!dragging) {
        return;
      }
      dragging = false;
      railResizer.dataset.dragging = "false";
      if (document.body && document.body.dataset) {
        document.body.dataset.railDrag = "false";
      }
      // A press that never moved is a click, and a click folds. Doing this on
      // pointerup rather than on "click" keeps the two gestures from both
      // firing off one press.
      if (!moved) {
        applyRail(0, !railCollapsed());
      }
    };

    railResizer.addEventListener("pointerdown", (ev) => {
      if (ev.button !== undefined && ev.button !== 0) {
        return;
      }
      dragging = true;
      moved = false;
      const box = railColumn.getBoundingClientRect ? railColumn.getBoundingClientRect() : null;
      originLeft = box ? box.left : 0;
      startX = ev.clientX;
      railResizer.dataset.dragging = "true";
      if (document.body && document.body.dataset) {
        document.body.dataset.railDrag = "true";
      }
      if (railResizer.setPointerCapture && ev.pointerId !== undefined) {
        // Capture, or the drag dies the moment the pointer crosses into the
        // transcript — which is where every useful drag goes.
        try {
          railResizer.setPointerCapture(ev.pointerId);
        } catch (e) {
          // Not fatal: the listeners below still fire while the button is down.
        }
      }
      if (ev.preventDefault) {
        ev.preventDefault();
      }
    });

    railResizer.addEventListener("pointermove", (ev) => {
      if (!dragging) {
        return;
      }
      const want = ev.clientX - originLeft;
      // Measured against where the press landed, not against the current
      // width: folded, the pointer starts a whole sidebar away from the width
      // it would set, and any jitter would then read as a drag and swallow
      // the click that was meant to unfold it.
      if (Math.abs(ev.clientX - startX) > 3) {
        moved = true;
      }
      if (!moved) {
        return;
      }
      if (want < RAIL_FOLD_AT) {
        // Dragged shut. The width is left alone, so letting go and clicking
        // the edge again brings back the size that was there before.
        applyRail(0, true);
        return;
      }
      applyRail(clampRailWidth(want), false);
    });

    railResizer.addEventListener("pointerup", endDrag);
    railResizer.addEventListener("pointercancel", endDrag);

    // The same two gestures from the keyboard, since the handle is focusable.
    railResizer.addEventListener("keydown", (ev) => {
      const key = ev.key;
      if (key === "Enter" || key === " " || key === "Spacebar") {
        applyRail(0, !railCollapsed());
      } else if (key === "ArrowLeft") {
        const next = railWidthNow() - 24;
        applyRail(next < RAIL_FOLD_AT ? 0 : clampRailWidth(next), next < RAIL_FOLD_AT);
      } else if (key === "ArrowRight") {
        applyRail(clampRailWidth(railWidthNow() + 24), false);
      } else {
        return;
      }
      if (ev.preventDefault) {
        ev.preventDefault();
      }
    });
  }

  const railSettingsBtn = document.getElementById("rail-settings-btn");
  const railSettingsModal = document.getElementById("rail-settings-modal");
  const railSettingsCloseBtn = document.getElementById("rail-settings-close");

  /** @param {boolean} open */
  function showRailSettings(open, section) {
    if (railSettingsModal) railSettingsModal.hidden = !open;
    if (railSettingsBtn && railSettingsBtn.setAttribute) {
      railSettingsBtn.setAttribute("aria-expanded", open ? "true" : "false");
    }
    if (open) {
      // 50-settings.js owns what is inside: it loads the panel on first open
      // and answers it from there. The section is what the composer's own
      // "settings" affordances ask to land on.
      openSettingsPanel(section || "general");
    } else if (railSettingsBtn && railSettingsBtn.focus) {
      // Sending focus back to the opener is the whole reason a dialog is
      // navigable by keyboard at all.
      railSettingsBtn.focus();
    }
  }

  if (railSettingsBtn && railSettingsBtn.addEventListener) {
    railSettingsBtn.addEventListener("click", () => {
      showRailSettings(Boolean(railSettingsModal && railSettingsModal.hidden));
    });
  }

  if (railSettingsCloseBtn && railSettingsCloseBtn.addEventListener) {
    railSettingsCloseBtn.addEventListener("click", () => showRailSettings(false));
  }

  if (railSettingsModal && railSettingsModal.addEventListener) {
    // A click that lands on the scrim rather than the card dismisses. Clicks
    // inside the panel never reach here — it is a separate document.
    railSettingsModal.addEventListener("click", (ev) => {
      if (ev.target === railSettingsModal) {
        showRailSettings(false);
      }
    });
  }

  if (document.addEventListener) {
    document.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape" && railSettingsModal && !railSettingsModal.hidden) {
        showRailSettings(false);
      }
    });
  }

  // The pre-paint script in the page already stamped the root from storage;
  // this only makes the in-memory choice and the DOM agree on first load.
  applyTheme(savedTheme());
  applyScale(savedScale());

  /**
   * Open one session from the sidebar. A row of another project is reachable
   * whenever that project has been on screen before, so switch first and only
   * then ask for the session.
   * @param {string} projectId @param {string} sessionId
   */
  async function openSessionRow(projectId, sessionId) {
    if (!projectId || !sessionId) {
      return;
    }
    if (projectId !== currentProjectId) {
      await switchProject(projectId);
      if (projectId !== currentProjectId) {
        return;
      }
    }
    if (projectState(projectId).sessionId === sessionId) {
      return;
    }
    await startSession(sessionId);
  }

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
  // ---- the start screen --------------------------------------------------
  //
  // Shown when no workspace is open. The desktop shell starts the core with
  // --no-project so its window opens here rather than guessing which project
  // you meant; a plain `orchestra web` opens the folder it was run in and
  // never sees this, unless every project is closed from the sidebar.

  /**
   * The start screen is a view of state, not a place the code navigates to:
   * it is up exactly while no workspace is on screen. It used to be switched
   * by hand at each place that opened something, and the places that opened
   * something without going through the screen were missed — clicking a
   * workspace in the sidebar left the start screen covering the chat of a
   * project that was open, selected and listing its sessions. So it is
   * derived here instead, from the one project the window is showing, and
   * renderProjects calls it; every path that changes what is open already
   * ends there.
   */
  /** How long the start screen takes to fade; matches rail.css. */
  const FADE_MS = 280;

  /**
   * Run the arrival animation on one element once. The attribute is what the
   * keyframes hang off, and it has to come off again or the animation never
   * replays on the next switch.
   * @param {any} el
   */
  function enterAnimation(el) {
    if (!el || !el.dataset || !window.setTimeout) {
      return;
    }
    el.dataset.entering = "true";
    window.setTimeout(() => {
      el.dataset.entering = "false";
    }, 760);
  }

  function syncStartScreen() {
    const screen = document.getElementById("start-screen");
    const app = document.getElementById("app");
    if (!screen || !app) {
      return;
    }
    const ready =
      Boolean(currentProjectId) &&
      known.some((p) => p.id === currentProjectId && p.state === "ready");
    const open = ready || pendingOpen();
    const wasOpen = !app.hidden;
    app.hidden = !open;
    // Loading until the project is live, not merely open: the core can be up
    // with its socket still connecting, and the composer must not take a
    // message it has nowhere to send.
    const loading = open && (!ready || pendingOpen());
    if (app.dataset) {
      app.dataset.loading = loading ? "true" : "false";
    }
    paintSkeleton(loading);
    if (open && !wasOpen) {
      // Leaving the start screen: it fades out over the project rather than
      // being switched off under it, and the project rises into place. Both
      // halves are CSS; this only marks which is which and clears up after.
      enterAnimation(app);
      enterAnimation(document.getElementById("project-rail"));
      if (!screen.hidden && screen.dataset) {
        screen.dataset.leaving = "true";
        if (window.setTimeout) {
          window.setTimeout(() => {
            screen.hidden = true;
            screen.dataset.leaving = "false";
          }, FADE_MS);
        } else {
          screen.hidden = true;
        }
      }
    } else {
      if (screen.dataset) {
        screen.dataset.leaving = "false";
      }
      screen.hidden = open;
    }
    // The sidebar goes with the chat. It is a sibling of #app rather than a
    // child, so hiding the chat alone left a column of workspaces down the
    // edge of a screen whose whole subject is which workspace to open — the
    // same list twice, one of them unreadable as a launcher. With nothing
    // open the start screen takes the window.
    const rail = document.getElementById("project-rail");
    if (rail) {
      rail.hidden = !open;
    }
    const edge = document.getElementById("rail-resizer");
    if (edge) {
      edge.hidden = !open;
    }
    if (!open) {
      renderStartScreen();
    }
  }

  /**
   * Say that a workspace is opening, and stop the screen taking another
   * click while it is. "" ends it. The id, when given, is the row that was
   * clicked: it keeps its contrast and spins, so it is clear which workspace
   * is coming while the rest of the list steps back.
   * @param {string} message @param {string} [projectId]
   */
  function startBusy(message, projectId) {
    // Kept rather than only written to the row, because opening a workspace
    // refreshes the list and rebuilds every row: renderStartScreen reads this
    // back, so the spinner survives its own progress.
    busyProjectId = message ? projectId || "" : "";
    const card = document.querySelector ? document.querySelector(".start-card") : null;
    const line = document.getElementById("start-busy");
    const text = document.getElementById("start-busy-text");
    if (card) {
      card.dataset.busy = message ? "true" : "false";
    }
    if (text) {
      text.textContent = message || "";
    }
    if (line) {
      line.hidden = !message;
    }
    renderStartScreen();
  }

  /** @param {string} message "" clears it. */
  function startError(message) {
    const el = document.getElementById("start-error");
    if (!el) {
      return;
    }
    el.textContent = message || "";
    el.hidden = !message;
  }

  /** The workspace currently opening, if the start screen is waiting on one. */
  let busyProjectId = "";

  /**
   * The workspace the window has left the start screen for, but which is not
   * live yet — its core starting, its socket not open. Either the id (a row
   * on the start screen) or only the path (a folder just picked). While one
   * is set the chat is on screen in its loading state, and syncStartScreen
   * treats it as open: the start screen leaves at the click, not when the
   * core finally answers, and the wait is spent looking at the project's own
   * frame filling in rather than at a dimmed launcher.
   */
  let pendingOpenId = "";
  let pendingOpenPath = "";

  function pendingOpen() {
    return Boolean(pendingOpenId || pendingOpenPath);
  }

  /**
   * The project's socket is up and its session started — or failed, which
   * ends the wait just the same. Called from the connection callbacks in
   * 10-adapter-session.js.
   * @param {string} projectId
   */
  function noteProjectLive(projectId) {
    if (!pendingOpen() || projectId !== currentProjectId) {
      return;
    }
    pendingOpenId = "";
    pendingOpenPath = "";
    renderProjects();
  }

  function renderStartScreen() {
    const list = document.getElementById("start-recent-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    if (known.length === 0) {
      const empty = document.createElement("div");
      empty.className = "start-recent-empty";
      empty.textContent = "No workspaces yet — open a folder or clone a repository.";
      list.appendChild(empty);
      return;
    }
    for (const p of known) {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "start-recent-item";
      item.dataset.projectId = p.id || "";
      item.dataset.path = p.path || "";
      item.title = p.path || "";
      if (p.missing) {
        item.dataset.missing = "true";
      }
      if (busyProjectId && p.id === busyProjectId) {
        item.dataset.busy = "true";
      }

      const name = document.createElement("span");
      name.className = "start-recent-name";
      // textContent: the name is a folder name off disk.
      name.textContent = p.name || p.path || "?";
      item.appendChild(name);

      const path = document.createElement("span");
      path.className = "start-recent-path";
      path.textContent = p.path || "";
      item.appendChild(path);

      const count = document.createElement("span");
      count.className = "start-recent-count";
      if (p.missing) {
        count.textContent = "folder is gone";
      } else if (typeof p.sessions === "number" && p.sessions >= 0) {
        count.textContent = p.sessions === 1 ? "1 chat" : p.sessions + " chats";
      }
      if (count.textContent) {
        item.appendChild(count);
      }
      list.appendChild(item);
    }
  }

  /**
   * Open a workspace from the start screen and leave it. Anything that fails
   * stays on the screen with the reason, because there is nowhere else to go.
   * @param {string} path
   */
  async function openFromStart(path, projectId) {
    startError("");
    if (!path) {
      return;
    }
    const entry = known.find((p) => p.path === path);
    startBusy("Opening " + ((entry && entry.name) || path) + "…", projectId || "");
    pendingOpenId = projectId || "";
    pendingOpenPath = path;
    renderProjects();
    try {
      await openAndEnter(path);
    } finally {
      startBusy("");
      const now = known.find((p) => p.path === path);
      if (!now || currentProjectId !== now.id) {
        // It did not get there. The start screen is still the place, and
        // it has to be usable again — with the reason, which openAndEnter
        // has already written into it.
        pendingOpenId = "";
        pendingOpenPath = "";
        renderProjects();
      }
      // Otherwise the wait ends when the socket is live: noteProjectLive.
    }
  }

  /** @param {string} path */
  async function openAndEnter(path) {
    // init: true, and nothing is asked. Picking a folder to work in IS the
    // consent to set it up — `orchestra web --init` has always treated a
    // folder chosen in a dialog exactly this way — and the only question
    // there was to ask cannot be asked here: window.confirm inside the
    // desktop shell is Tauri's own, it needs a capability this page is
    // deliberately not granted, and it rejects. Worse, it rejects
    // ASYNCHRONOUSLY, so the promise it returns reads as true and the code
    // carried on as if the user had agreed.
    if (!(await openProject(path, true))) {
      startError("Could not open " + path + ".");
      return;
    }
    await enterProject(path);
  }

  /**
   * Leave the start screen for a workspace that is now open. Opening it is
   * only half the job — without the switch the window left the start screen
   * for a transcript belonging to no project at all.
   * @param {string} path
   */
  async function enterProject(path) {
    const entry =
      known.find((p) => p.path === path) || known.find((p) => p.state === "ready");
    if (!entry) {
      startError("Opened " + path + ", but it is not in the workspace list.");
      return;
    }
    await switchProject(entry.id);
    if (currentProjectId !== entry.id) {
      startError("Opened " + path + ", but could not switch to it.");
    }
  }

  /** Clone a repository, then open it. Both halves are one request. */
  async function cloneFromStart() {
    const input = /** @type {HTMLInputElement|null} */ (
      document.getElementById("start-clone-url")
    );
    const go = /** @type {HTMLButtonElement|null} */ (
      document.getElementById("start-clone-go")
    );
    const url = input ? String(input.value || "").trim() : "";
    if (!url) {
      startError("Enter a repository URL.");
      return;
    }
    startError("");
    // Where to put it. The shell's folder picker when there is one; typed
    // otherwise, because a browser cannot open a native chooser.
    let parent = "";
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      try {
        const picked = await t.dialog.open({ directory: true, multiple: false });
        parent = typeof picked === "string" ? picked : "";
      } catch (e) {
        parent = "";
      }
    } else if (window.prompt) {
      parent = window.prompt("Clone into which folder? (absolute path)") || "";
    }
    parent = String(parent || "").trim();
    if (!parent) {
      return;
    }
    if (go) {
      go.disabled = true;
      go.textContent = "Cloning…";
    }
    startBusy("Cloning " + url + "…", "");
    try {
      const created = await api("/api/projects/clone", {
        method: "POST",
        body: JSON.stringify({ url, parent }),
      });
      await refreshProjects();
      if (created && created.id) {
        await switchProject(created.id);
      }
    } catch (e) {
      startError(String((e && e.message) || e));
    } finally {
      startBusy("");
      if (go) {
        go.disabled = false;
        go.textContent = "Clone";
      }
    }
  }

  {
    const screen = document.getElementById("start-screen");
    if (screen && screen.addEventListener) {
      screen.addEventListener("click", (ev) => {
        const drop = ev.target && ev.target.closest ? ev.target.closest("[data-forget-id]") : null;
        if (drop) {
          startError("");
          void (async () => {
            await forgetProject(drop.dataset.forgetId || "");
          })();
          return;
        }
        const item = ev.target && ev.target.closest ? ev.target.closest(".start-recent-item") : null;
        if (!item) {
          return;
        }
        if (item.dataset.missing === "true") {
          // Opening it would 404 and say "could not open" with nothing to do
          // about it. Say what is actually wrong and offer the only repair.
          startError("");
          const el = document.getElementById("start-error");
          if (el) {
            el.textContent =
              "The folder for " + (item.dataset.path || "this workspace") + " is not there any more. ";
            const drop = document.createElement("button");
            drop.type = "button";
            drop.className = "start-action start-error-action";
            drop.dataset.forgetId = item.dataset.projectId || "";
            drop.textContent = "Remove from the list";
            el.appendChild(drop);
            el.hidden = false;
          }
          return;
        }
        void openFromStart(item.dataset.path || "", item.dataset.projectId || "");
      });
    }
    const openBtn = document.getElementById("start-open-btn");
    if (openBtn && openBtn.addEventListener) {
      openBtn.addEventListener("click", () => {
        void (async () => {
          startError("");
          const picked = await pickProjectFolder();
          if (!picked) {
            return;
          }
          await openFromStart(picked, "");
        })();
      });
    }
    const cloneBtn = document.getElementById("start-clone-btn");
    const cloneForm = document.getElementById("start-clone-form");
    if (cloneBtn && cloneBtn.addEventListener && cloneForm) {
      cloneBtn.addEventListener("click", () => {
        cloneForm.hidden = !cloneForm.hidden;
        startError("");
        const input = document.getElementById("start-clone-url");
        if (!cloneForm.hidden && input && input.focus) {
          input.focus();
        }
      });
    }
    const cloneGo = document.getElementById("start-clone-go");
    if (cloneGo && cloneGo.addEventListener) {
      cloneGo.addEventListener("click", () => void cloneFromStart());
    }
    const cloneCancel = document.getElementById("start-clone-cancel");
    if (cloneCancel && cloneCancel.addEventListener && cloneForm) {
      cloneCancel.addEventListener("click", () => {
        cloneForm.hidden = true;
        startError("");
      });
    }
    const cloneInput = document.getElementById("start-clone-url");
    if (cloneInput && cloneInput.addEventListener) {
      cloneInput.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter") {
          ev.preventDefault();
          void cloneFromStart();
        }
      });
    }
  }

  /**
   * Ask for a folder and return it, or "" if the person backed out. Separate
   * from opening it because the start screen has to name the folder in its
   * "opening…" line before the open starts, and the dialog is the only place
   * the name comes from.
   * @returns {Promise<string>}
   */
  async function pickProjectFolder() {
    let path = "";
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      try {
        const picked = await t.dialog.open({ directory: true, multiple: false });
        path = typeof picked === "string" ? picked : "";
      } catch (e) {
        path = "";
      }
    } else if (window.prompt) {
      path = window.prompt("Project folder (absolute path)") || "";
    }
    return String(path || "").trim();
  }

  async function addProject() {
    const path = await pickProjectFolder();
    if (!path) {
      return "";
    }
    // Chosen in a dialog, so set it up if it needs it — same judgment
    // `orchestra web --init` makes about a folder the user picked, and the
    // confirm that used to stand here cannot run in the desktop shell. See
    // openFromStart.
    return (await openProject(path, true)) ? path : "";
  }

  // ---- startup -----------------------------------------------------------
  //
  // The startupId branch below exists for a URL that carries a project id,
  // but nothing ships one today: the desktop shell's URL is
  // `{base}/?token={token}` (ui/desktop/src-tauri/src/main.rs), with no
  // `?project=`, so the desktop app always falls through to the
  // resolve-from-API branch. A plain `orchestra web` passes none either, so
  // the id must be resolved from the API before any socket opens — see the
  // id-before-socket note inside the IIFE.

  (async () => {
    const startupId = new URLSearchParams(location.search).get("project") || "";
    if (startupId) {
      currentProjectId = startupId;
      setActiveConn(ensureConn(startupId));
      await refreshProjects();
      return;
    }
    // No id in the URL — a plain `orchestra web`. Ask the API which project the
    // core is already serving BEFORE opening a socket, so that the connection,
    // the per-project record and the rail's active chip all share one key.
    //
    // Adopting the id afterwards does not work: `conns` would stay keyed ""
    // while `currentProjectId` named the project, and since the two are
    // separate socket slots on the server, every notification would fail the
    // `projectId === currentProjectId` gate, `sendTurn` would read an empty
    // session forever, and clicking the chip could not repair it because
    // `switchProject` returns early on the id it already holds.
    await refreshProjects();
    const first = known.find((p) => p.state === "ready");
    if (!first) {
      // Nothing open: --no-project, or every workspace closed from the rail.
      // The start screen takes the window until one is picked, and no socket
      // is opened for a project that does not exist.
      currentProjectId = "";
      renderProjects();
      return;
    }
    currentProjectId = first.id;
    setActiveConn(ensureConn(currentProjectId));
    renderProjects();
  })();
  // The settings panel's host half, on the web.
  //
  // ui/vscode/src/chat/settings.ts is the same thing for the extension: it
  // answers the panel's messages by calling the core and posting state back.
  // Every call it makes is plain JSON-RPC on the core — runtime.*, index.*,
  // agents.*, mcp.*, skill.list — and `orchestra web` mounts the very same
  // core.NewRPCHandler (internal/cli/web.go), so all of it answers here too
  // over the project's socket. What does not carry over is the VS Code API:
  // toasts become a line in the panel's own error strip, openExternal happens
  // inside the frame, and the two extension-only fields (binary path, project
  // root) have no meaning when the server already chose the workspace.
  //
  // The field mappings below mirror ui/vscode/src/coreSession.ts exactly,
  // because the panel renders from their camelCase shape.

  const settingsFrame = document.getElementById("settings-frame");

  /** @param {any} msg */
  function postToSettings(msg) {
    if (!settingsFrame || !settingsFrame.contentWindow) {
      return;
    }
    try {
      settingsFrame.contentWindow.postMessage(msg, window.location.origin);
    } catch (e) {
      // The frame can be gone mid-flight; nothing here is worth throwing over.
    }
  }

  /**
   * What the panel says while there is nothing to read from yet. The gear is
   * reachable the moment a workspace is chosen, before its core is up; the
   * settings follow once it is — onConnected pushes them (see
   * settingsPanelOpen) — and the panel says so instead of "no project".
   */
  const SETTINGS_OPENING_NOTE = "The workspace is still opening — its settings will load as soon as it is ready.";

  /** @param {string} method @param {any} params @returns {Promise<any>} */
  function settingsRpc(method, params) {
    const conn = currentProjectId ? connFor(currentProjectId) : null;
    if (!conn || !conn.isOpen()) {
      return Promise.reject(new Error(pendingOpen() || currentProjectId ? SETTINGS_OPENING_NOTE : "no project is open"));
    }
    return conn.send(method, params || {});
  }

  /** Whether the settings dialog is on screen. */
  function settingsPanelOpen() {
    return Boolean(railSettingsModal) && railSettingsModal.hidden === false;
  }

  const num = (v) => (typeof v === "number" ? v : 0);
  const posInt = (v) => {
    const n = Number(v);
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined;
  };
  const numOrUndef = (v) => {
    const n = Number(v);
    return Number.isFinite(n) ? n : undefined;
  };

  // ---- the reads, shaped as coreSession.ts shapes them --------------------

  async function getLLM() {
    const r = (await settingsRpc("runtime.get_llm", {})) || {};
    return {
      provider: r.provider || "",
      apiBase: r.api_base || "",
      model: r.model || "",
      apiKeySet: Boolean(r.api_key_set),
      apiKeyHint: r.api_key_hint || "",
      temperature: num(r.temperature),
      maxTokens: num(r.max_tokens),
      timeoutS: num(r.timeout_s),
      promptFamily: r.prompt_family || "",
      multimodal: Boolean(r.multimodal),
      numCtx: num(r.num_ctx),
      contextTokens: num(r.context_tokens),
    };
  }

  async function getSystemPrompt() {
    const r = (await settingsRpc("runtime.get_system_prompt", {})) || {};
    return {
      content: r.content || "",
      hasOverride: Boolean(r.has_override),
      promptFamily: r.prompt_family || "",
      path: r.path || "",
    };
  }

  async function listAgents() {
    const r = (await settingsRpc("agents.list", {})) || {};
    return {
      agents: Array.isArray(r.agents) ? r.agents : [],
      builtInModes: Array.isArray(r.built_in_modes) ? r.built_in_modes : [],
      availableTools: Array.isArray(r.available_tools) ? r.available_tools : [],
    };
  }

  async function listMCP() {
    const r = (await settingsRpc("mcp.list", {})) || {};
    return { servers: Array.isArray(r.servers) ? r.servers : [] };
  }

  async function listSkills() {
    const r = (await settingsRpc("skill.list", {})) || {};
    const skills = Array.isArray(r.skills) ? r.skills : [];
    return skills.map((s) => ({
      name: s.name || "",
      description: s.description || "",
      origin: s.origin,
    }));
  }

  /** @param {{probe?: boolean, probeKey?: string, includeSecrets?: boolean}} [options] */
  async function listProviders(options) {
    const params = {};
    if (options && options.probe) params.probe = true;
    if (options && options.probeKey && options.probeKey.trim()) params.probe_key = options.probeKey.trim();
    if (options && options.includeSecrets) params.include_secrets = true;
    const r = (await settingsRpc("runtime.list_providers", params)) || {};
    return {
      providers: Array.isArray(r.providers) ? r.providers : [],
      activeProvider: typeof r.active_provider === "string" ? r.active_provider : "",
      activeModel: typeof r.active_model === "string" ? r.active_model : "",
    };
  }

  async function getIndexStatus() {
    const r = (await settingsRpc("index.status", {})) || {};
    const graph = r.graph || {};
    const embed = r.embed || {};
    const limits = r.limits || {};
    return {
      projectRoot: String(r.project_root || ""),
      excludeDirs: Array.isArray(r.exclude_dirs) ? r.exclude_dirs : [],
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
          embed.semantic_auto_explore === undefined ? undefined : Boolean(embed.semantic_auto_explore),
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
        langs: graph.langs && typeof graph.langs === "object" ? graph.langs : {},
      },
      graphUIPort: Number(r.graph_ui_port) || 6061,
    };
  }

  async function getOrchestra() {
    const r = (await settingsRpc("runtime.get_orchestra", {})) || {};
    const rolesRaw = Array.isArray(r.roles) ? r.roles : [];
    const roles = rolesRaw.map((o) => ({
      key: String(o.key || ""),
      label: String(o.label || ""),
      tier: String(o.tier || ""),
      provider: String(o.provider || ""),
      model: String(o.model || ""),
      models: (Array.isArray(o.models) ? o.models : []).filter((m) => typeof m === "string"),
    }));
    const namedRaw = r.named && typeof r.named === "object" ? r.named : {};
    const named = {};
    for (const k of Object.keys(namedRaw)) {
      const o = namedRaw[k] || {};
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

  // ---- the bundled MCP catalogue ------------------------------------------
  //
  // mapLocalCatalog from ui/vscode/src/chat/mcpRegistry.ts. The file itself is
  // copied to static/ by the settings bundler; the ${workspaceRoot} it splices
  // into commands is only known here, at push time.

  /** @type {any} */
  let mcpCatalogFile = null;

  function deriveLocalTags(e) {
    const tags = ["featured"];
    if (e.category) tags.push(String(e.category).toLowerCase());
    if (e.envRequired) tags.push("needs-key");
    tags.push("stdio");
    return [...new Set(tags.map((t) => t.trim()).filter(Boolean))];
  }

  /** @param {any} local @param {string} workspaceRoot */
  function mapLocalCatalog(local, workspaceRoot) {
    const root = workspaceRoot || ".";
    const raw = local && Array.isArray(local.entries) ? local.entries : [];
    const out = [];
    for (const e of raw) {
      if (!e || typeof e !== "object") continue;
      const id = String(e.id || e.name || "").trim();
      if (!id) continue;
      const command = String(e.command || "").replace(/\$\{workspaceRoot\}/g, root);
      out.push({
        id,
        name: String(e.name || id),
        title: String(e.title || e.name || id),
        description: String(e.description || ""),
        category: String(e.category || "Local"),
        command,
        env: Array.isArray(e.env) ? e.env.map((x) => String(x)) : [],
        envRequired: Boolean(e.envRequired),
        homepage: e.homepage ? String(e.homepage) : undefined,
        icon: e.icon ? String(e.icon) : undefined,
        tags: Array.isArray(e.tags) ? e.tags.map((x) => String(x)) : deriveLocalTags(e),
        version: e.version ? String(e.version) : undefined,
        installable: e.installable === false ? false : Boolean(command),
        source: "local",
      });
    }
    return out;
  }

  async function ensureMcpCatalogFile() {
    if (mcpCatalogFile) {
      return mcpCatalogFile;
    }
    try {
      const res = await fetch("mcp-catalog.json", { method: "GET" });
      mcpCatalogFile = res.ok ? await res.json() : { version: 1, entries: [] };
    } catch (e) {
      mcpCatalogFile = { version: 1, entries: [] };
    }
    return mcpCatalogFile;
  }

  // ---- state ---------------------------------------------------------------

  /** The section to open on the next push; the panel navigates to it once. */
  let settingsPendingSection = "general";

  /**
   * Counts pushes. An answer that arrives for an earlier push is dropped when
   * a later one has started since: the frame is loaded once and repainted on
   * every open, so by the time a slow read comes back the panel can be
   * showing another project, and the answer is about this one.
   */
  let settingsPushSeq = 0;

  function settingsProjectEntry() {
    return known.find((p) => p.id === currentProjectId) || null;
  }

  function settingsWorkspaceRoot() {
    const entry = settingsProjectEntry();
    return (entry && entry.path) || "";
  }

  /**
   * Which workspace this is. Everything on these screens is written to that
   * workspace's own .orchestra.yml — provider, models, roles, index, MCP
   * servers, agents are per workspace, not shared — so the panel has to say
   * which one it is editing, or the separation is invisible.
   *
   * Sent before any read, not after all of them: the name is in the project
   * list already. It used to follow the state, and the frame keeps whatever
   * it showed last until told otherwise — so for as long as a slow read held
   * the state back, the panel kept the previous project's name over this
   * project's settings, and looked like it had not noticed the switch.
   */
  function postSettingsWorkspace() {
    const entry = settingsProjectEntry();
    postToSettings({
      type: "workspace",
      name: (entry && entry.name) || "",
      path: (entry && entry.path) || "",
    });
  }

  async function pushSettingsState() {
    const seq = ++settingsPushSeq;
    const projectId = currentProjectId;
    postSettingsWorkspace();
    try {
      const [llm, prompt, agents, mcp, index, skills, providerCatalog, orchestra, catalogFile] =
        await Promise.all([
          getLLM(),
          getSystemPrompt(),
          listAgents(),
          listMCP(),
          getIndexStatus(),
          listSkills(),
          // Without probe: the catalogue as the config has it, answered at
          // once. The models come in a second message, from
          // probeSettingsProviders below. Probing asks every configured
          // provider's server for its models, and one on a host that is down
          // — a VPN that is not up — holds the answer for as long as the HTTP
          // timeouts allow; the whole panel used to wait on it, showing
          // nothing new for half a minute.
          listProviders({ includeSecrets: true }),
          getOrchestra().catch(() => null),
          ensureMcpCatalogFile(),
        ]);
      if (seq !== settingsPushSeq || projectId !== currentProjectId) {
        return; // the panel has moved on to another project
      }
      const ws = settingsWorkspaceRoot();
      const navigateSection = settingsPendingSection;
      settingsPendingSection = "";
      postToSettings({
        type: "state",
        llm,
        prompt,
        agents,
        mcp,
        index,
        skills,
        providerCatalog,
        orchestra,
        ...(navigateSection ? { navigateSection } : {}),
        // Where the binary lives and which folder is the root are VS Code
        // settings; on the web the server answered both before the page loaded.
        // Sent empty so the panel's two inputs stay blank — they are hidden.
        extension: { binaryPath: "", projectRoot: "" },
        workspaceRoot: ws,
        mcpCatalog: {
          version: 1,
          entries: mapLocalCatalog(catalogFile, ws),
          source: "local",
        },
      });
      void probeSettingsProviders(seq, projectId);
    } catch (err) {
      if (seq !== settingsPushSeq || projectId !== currentProjectId) {
        return;
      }
      postToSettings({ type: "error", message: String((err && err.message) || err) });
    }
  }

  /**
   * The second half of a push: the catalogue with each provider's models,
   * which means asking their servers. It arrives in the same providerCatalog
   * message the Refresh button's answer does, so the panel takes it the same
   * way — the selection kept, the model list filled in. Until then the models
   * pane says it is loading rather than that nothing came back.
   * @param {number} seq @param {string} projectId
   */
  async function probeSettingsProviders(seq, projectId) {
    const current = () => seq === settingsPushSeq && projectId === currentProjectId;
    postToSettings({ type: "modelsBusy", busy: true, message: "Loading models…" });
    try {
      const catalog = await listProviders({ probe: true, includeSecrets: true });
      if (current()) {
        postToSettings({ type: "providerCatalog", catalog });
      }
    } catch (err) {
      if (current()) {
        settingsNote("Could not load models: " + String((err && err.message) || err));
      }
    } finally {
      if (current()) {
        postToSettings({ type: "modelsBusy", busy: false });
      }
    }
  }

  /** The panel has no toast of its own; its error strip doubles as one. */
  function settingsNote(message) {
    postToSettings({ type: "error", message });
    setTimeout(() => postToSettings({ type: "error", message: "" }), 4000);
  }

  /** @param {any} msg */
  async function saveOrchestraFrom(msg) {
    const roles = (Array.isArray(msg.roles) ? msg.roles : []).map((r) => ({
      key: r.key,
      label: r.label,
      provider: r.provider,
      model: r.model,
      models: r.models && r.models.length ? r.models : undefined,
    }));
    const params = { roles, persist: true };
    if (msg.defaultTier) params.default_tier = String(msg.defaultTier);
    if (msg.maxWorkerRetries !== undefined) params.max_worker_retries = posInt(msg.maxWorkerRetries);
    if (msg.workerVerifyEnabled !== undefined) params.worker_verify_enabled = Boolean(msg.workerVerifyEnabled);
    if (msg.maxWorkerVerifyRetries !== undefined) {
      params.max_worker_verify_retries = posInt(msg.maxWorkerVerifyRetries);
    }
    if (msg.workerLLMVerifyEnabled !== undefined) {
      params.worker_llm_verify_enabled = Boolean(msg.workerLLMVerifyEnabled);
    }
    await settingsRpc("runtime.configure_orchestra", params);
  }

  // ---- the panel's messages ------------------------------------------------

  const SETTINGS_TYPES = new Set([
    "ready",
    "reload",
    "saveGeneral",
    "saveModels",
    "saveIndex",
    "savePrompt",
    "clearPrompt",
    "upsertAgent",
    "deleteAgent",
    "upsertMCP",
    "deleteMCP",
    "setMCPDisabled",
    "testMCP",
    "rebuildGraph",
    "runEmbed",
    "openGraphViewer",
    "refreshModels",
    "saveOrchestra",
    "refreshOrchModels",
    "fetchMcpRegistry",
    "openExternal",
    "backToChat",
    "setTheme",
    "setScale",
  ]);

  /** @param {any} msg */
  async function handleSettingsMessage(msg) {
    const t = msg.type;
    switch (t) {
      case "backToChat":
        showRailSettings(false);
        return;

      case "setTheme":
        // The frame stamped itself already; store it and stamp this document.
        applyTheme(msg.theme === "light" || msg.theme === "dark" ? msg.theme : "system");
        return;

      case "setScale":
        // Only this document is stamped: the dialog is inside it, so the
        // frame is scaled by the same zoom without knowing about it.
        applyScale(String(msg.scale || "auto"));
        return;

      case "ready":
      case "reload":
        await pushSettingsState();
        return;

      case "openExternal":
        // Only the frame can open a window without the parent guessing at
        // pop-up rules, and it is the one that asked.
        postToSettings({ type: "openExternalFromFrame", url: String(msg.url || "") });
        return;

      case "openGraphViewer":
        settingsNote(
          "The graph viewer runs as its own local server — start it with `orchestra ckg-ui`. " +
            "The web UI cannot launch processes."
        );
        return;

      case "fetchMcpRegistry": {
        // The remote registry is fetched by the extension host, which is not
        // subject to a page's cross-origin rules. Say so rather than spin.
        const file = await ensureMcpCatalogFile();
        postToSettings({
          type: "mcpCatalog",
          catalog: {
            version: 1,
            entries: mapLocalCatalog(file, settingsWorkspaceRoot()),
            source: "local",
            error: "Browsing the remote registry is not available in the web UI yet — this is the bundled catalogue.",
          },
        });
        postToSettings({ type: "mcpCatalogBusy", busy: false, prefetching: false });
        return;
      }

      case "saveGeneral":
        // Binary path and project root are extension settings with no web
        // counterpart; the tab's other half, Orchestra routing, still saves.
        if (Array.isArray(msg.roles)) {
          await saveOrchestraFrom(msg);
          settingsNote("Orchestra settings saved");
        }
        await pushSettingsState();
        return;

      case "refreshModels": {
        const probeKey = String(msg.provider || "").trim();
        const apiBase = String(msg.apiBase || "").trim();
        const apiKey = String(msg.apiKey || "").trim();
        if (probeKey && (apiBase || apiKey)) {
          const params = { persist: Boolean(apiKey), provider: probeKey };
          if (apiBase) params.api_base = apiBase;
          if (apiKey) params.api_key = apiKey;
          await settingsRpc("runtime.configure_llm", params);
        }
        postToSettings({ type: "modelsBusy", busy: true });
        const catalog = await listProviders(
          probeKey ? { probeKey, includeSecrets: true } : { probe: true, includeSecrets: true }
        );
        postToSettings({ type: "providerCatalog", catalog, probeKey: probeKey || undefined });
        postToSettings({ type: "modelsBusy", busy: false });
        return;
      }

      case "saveModels": {
        const apiKey = String(msg.apiKey || "").trim();
        const model = String(msg.model || "").trim();
        const params = { persist: true };
        const provider = String(msg.provider || "").trim();
        const apiBase = String(msg.apiBase || "").trim();
        if (provider) params.provider = provider;
        if (apiBase) params.api_base = apiBase;
        if (apiKey) params.api_key = apiKey;
        if (model) params.model = model;
        if (numOrUndef(msg.temperature) !== undefined) params.temperature = numOrUndef(msg.temperature);
        if (posInt(msg.maxTokens) !== undefined) params.max_tokens = posInt(msg.maxTokens);
        if (posInt(msg.timeoutS) !== undefined) params.timeout_s = posInt(msg.timeoutS);
        if (msg.promptFamily !== undefined) params.prompt_family = String(msg.promptFamily);
        if (msg.multimodal !== undefined) params.multimodal = Boolean(msg.multimodal);
        await settingsRpc("runtime.configure_llm", params);
        settingsNote(
          apiKey && !model
            ? "Provider credentials saved — pick a model and save again to activate"
            : "Model settings saved"
        );
        // The chat's own pill and context gauge show the model too; a change
        // made here has to reach them, or they name the model that was.
        void pushLLMInfo(currentProjectId);
        await pushSettingsState();
        return;
      }

      case "refreshOrchModels": {
        postToSettings({ type: "modelsBusy", busy: true, message: "Loading models…" });
        const catalog = await listProviders({ probe: true });
        postToSettings({ type: "providerCatalog", catalog });
        postToSettings({ type: "modelsBusy", busy: false });
        return;
      }

      case "saveOrchestra":
        if (!Array.isArray(msg.roles)) {
          throw new Error("roles required");
        }
        await saveOrchestraFrom(msg);
        settingsNote("Orchestra settings saved");
        await pushSettingsState();
        return;

      case "saveIndex": {
        const excludeDirs = String(msg.excludeDirs || "")
          .split(/\r?\n/)
          .map((x) => x.trim())
          .filter(Boolean);
        const params = { persist: true, exclude_dirs: excludeDirs };
        if (posInt(msg.contextLimitKB) !== undefined) params.context_limit_kb = posInt(msg.contextLimitKB);
        if (posInt(msg.limitsMaxFiles) !== undefined) params.limits_max_files = posInt(msg.limitsMaxFiles);
        if (posInt(msg.embedBatchSize) !== undefined) params.embed_batch_size = posInt(msg.embedBatchSize);
        if (msg.semanticAutoExplore !== undefined) {
          params.semantic_auto_explore = Boolean(msg.semanticAutoExplore);
        }
        await settingsRpc("index.configure", params);
        settingsNote("Index settings saved");
        await pushSettingsState();
        return;
      }

      case "rebuildGraph": {
        postToSettings({ type: "indexBusy", busy: true, message: "Rebuilding graph…" });
        const r = (await settingsRpc("index.rebuild", {})) || {};
        const g = r.graph || {};
        postToSettings({
          type: "indexActionResult",
          action: "rebuild",
          graph: {
            files: Number(g.files) || 0,
            nodes: Number(g.nodes) || 0,
            edges: Number(g.edges) || 0,
            embeddings: Number(g.embeddings) || 0,
            missing_embeddings: Number(g.missing_embeddings) || 0,
          },
        });
        postToSettings({ type: "indexBusy", busy: false });
        await pushSettingsState();
        return;
      }

      case "runEmbed": {
        postToSettings({ type: "indexBusy", busy: true, message: "Embedding…" });
        const params = {};
        if (msg.rebuild) params.rebuild = true;
        if (posInt(msg.limit) !== undefined) params.limit = posInt(msg.limit);
        const r = (await settingsRpc("index.embed", params)) || {};
        postToSettings({
          type: "indexActionResult",
          action: "embed",
          result: {
            model: r.model || "",
            embedded: Number(r.embedded) || 0,
            total: Number(r.total) || 0,
            remaining: Number(r.remaining) || 0,
            elapsed: r.elapsed || "",
          },
        });
        postToSettings({ type: "indexBusy", busy: false });
        await pushSettingsState();
        return;
      }

      case "savePrompt": {
        const params = { persist: true };
        if (msg.content !== undefined) params.content = String(msg.content);
        if (msg.promptFamily !== undefined) params.prompt_family = String(msg.promptFamily);
        await settingsRpc("runtime.set_system_prompt", params);
        settingsNote("System prompt saved");
        await pushSettingsState();
        return;
      }

      case "clearPrompt":
        await settingsRpc("runtime.set_system_prompt", { persist: true, clear: true });
        settingsNote("System prompt cleared");
        await pushSettingsState();
        return;

      case "upsertAgent":
        await settingsRpc("agents.upsert", { agent: msg.agent, persist: true });
        settingsNote("Agent saved");
        await pushSettingsState();
        return;

      case "deleteAgent":
        await settingsRpc("agents.delete", { name: String(msg.name || ""), persist: true });
        settingsNote("Agent deleted");
        await pushSettingsState();
        return;

      case "upsertMCP": {
        const r = (await settingsRpc("mcp.upsert", { server: msg.server, persist: true })) || {};
        const warnings = Array.isArray(r.warnings) ? r.warnings : [];
        settingsNote(warnings.length ? "Saved with warnings: " + warnings.join("; ") : "MCP server saved");
        await pushSettingsState();
        return;
      }

      case "deleteMCP":
        await settingsRpc("mcp.delete", { name: String(msg.name || ""), persist: true });
        settingsNote("MCP server removed");
        await pushSettingsState();
        return;

      case "setMCPDisabled":
        await settingsRpc("mcp.set_disabled", {
          name: String(msg.name || ""),
          disabled: Boolean(msg.disabled),
          persist: true,
        });
        await pushSettingsState();
        return;

      case "testMCP": {
        const params = {};
        if (msg.name) params.name = String(msg.name);
        if (msg.server) params.server = msg.server;
        const r = (await settingsRpc("mcp.test", params)) || {};
        postToSettings({
          type: "mcpTestResult",
          result: {
            ok: Boolean(r.ok),
            name: r.name || "",
            tools: Array.isArray(r.tools) ? r.tools : [],
            error: r.error || "",
            elapsed: r.elapsed || "",
          },
        });
        return;
      }

      default:
        return;
    }
  }

  if (window.addEventListener) {
    window.addEventListener("message", (event) => {
      // Only the settings frame, and only same-origin: everything else on this
      // channel belongs to the renderer, which 00-web-prelude.js drives.
      if (event.origin !== window.location.origin) {
        return;
      }
      if (!settingsFrame || event.source !== settingsFrame.contentWindow) {
        return;
      }
      const msg = event.data;
      if (!msg || typeof msg !== "object" || !SETTINGS_TYPES.has(msg.type)) {
        return;
      }
      void handleSettingsMessage(msg).catch((err) => {
        postToSettings({ type: "error", message: String((err && err.message) || err) });
      });
    });
  }

  /**
   * Called when the dialog opens. The frame is loaded lazily — its 2400 lines
   * of script and a round of RPC are not worth paying for on every page load —
   * and announces itself with "ready", which pushes the state.
   * @param {string} section
   */
  function openSettingsPanel(section) {
    settingsPendingSection = section || "general";
    if (!settingsFrame) {
      return;
    }
    if (!settingsFrame.getAttribute("src")) {
      settingsFrame.setAttribute("src", "settings.html");
      return;
    }
    // Already loaded: ask it to repaint from a fresh read.
    void pushSettingsState();
  }
  // ---- the composer's own messages ---------------------------------------
  //
  // Everything the chat input asks for that is not a turn: the model pill, the
  // Orchestra role breakdown, slash commands, @-file mentions, rewind, the
  // pending-diff bar. 10-adapter-session.js routes here before it gives up on
  // a message, so anything handled below stops being "not available in the
  // web UI yet".
  //
  // All of it is core RPC — runtime.list_providers, runtime.set_model,
  // runtime.get_orchestra, session.compact, session.rewind,
  // session.apply_pending, skill.invoke, tool.call — which is why the VS Code
  // host and this one can answer the same renderer with the same shapes. The
  // handful that genuinely cannot work here (opening an editor, a native diff
  // view) still fall through to the note, and say which.

  /**
   * Route one renderer message. Returning false means "not mine", and the
   * caller says so to the user.
   * @param {any} msg @returns {boolean}
   */
  function handleComposerMessage(msg) {
    switch (msg.type) {
      case "listProviderModels":
        void pushProviderModels();
        return true;

      case "setModel":
        void applyModel(String(msg.model || ""), msg.provider);
        return true;

      case "listOrchestraRoles":
        void pushOrchestraRoles();
        return true;

      case "openSettings":
        showRailSettings(true, typeof msg.section === "string" ? msg.section : "general");
        return true;

      case "openOrchestraSettings":
        showRailSettings(true, "general");
        return true;

      case "slashCommand":
        void runSlashCommand(String(msg.cmd || ""), msg.arg);
        return true;

      case "mentionSearch":
        void searchMentions(String(msg.query || ""));
        return true;

      case "rewindToMessage":
        void rewindToMessage(msg.uiIndex);
        return true;

      case "forkFromMessage":
        void forkFromMessage(msg.uiIndex);
        return true;

      case "applyPending":
        void settlePending(true);
        return true;

      case "discardPending":
        void settlePending(false);
        return true;

      case "deleteSession":
        void deleteSessionById(String(msg.sessionId || ""));
        return true;

      case "cancelQueuedSend":
        // The VS Code panel queues sends while a turn is in flight and this
        // host does not, so there is never a queued send to cancel. Taking the
        // message keeps a note about a thing that cannot happen off the
        // screen.
        return true;

      case "attach":
        void pickAttachments();
        return true;

      case "attachBytes":
        void storeAttachmentBytes(msg);
        return true;

      default:
        return false;
    }
  }

  /* ---- attachments -------------------------------------------------------- */
  //
  // The paperclip posts "attach"; dropping or pasting a file posts
  // "attachBytes" with its contents. The editor's host answers the first
  // with the editor's file dialog and the second by writing the bytes into
  // the workspace itself (ui/vscode/src/chat/panel.ts). This host has neither
  // a dialog of its own nor a filesystem: the desktop shell lends it a native
  // picker (Tauri's dialog plugin), a browser has <input type=file>, and the
  // bytes go to the core — attachments.store keeps them under
  // .orchestra/attachments, inside the workspace, where a turn can read them.

  const IMAGE_ATTACHMENT_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "avif", "svg"]);
  /** An image this small travels as a data: URL so the chip can show it. */
  const ATTACHMENT_PREVIEW_MAX_BYTES = 2 * 1024 * 1024;

  /** @param {string} ext */
  function attachmentKind(ext) {
    return IMAGE_ATTACHMENT_EXTS.has(String(ext || "").toLowerCase()) ? "image" : "file";
  }

  /** The renderer's file chip for a path on disk. @param {string} p */
  function attachmentRefFromPath(p) {
    const norm = String(p || "").replace(/\\/g, "/");
    const name = norm.slice(norm.lastIndexOf("/") + 1) || norm;
    const dot = name.lastIndexOf(".");
    const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
    return { name, path: p, ext: ext || undefined, kind: attachmentKind(ext) };
  }

  /**
   * Whether p lies inside root. Paths from the desktop's picker are absolute
   * and spelled as the OS spells them; both sides are folded to forward
   * slashes and, when a drive letter says this is Windows, to one case.
   * @param {string} root @param {string} p
   */
  function pathInsideWorkspace(root, p) {
    let r = String(root || "").replace(/\\/g, "/").replace(/\/+$/, "");
    let q = String(p || "").replace(/\\/g, "/");
    if (!r || !q) {
      return false;
    }
    if (/^[a-z]:/i.test(r)) {
      r = r.toLowerCase();
      q = q.toLowerCase();
    }
    return q === r || q.startsWith(r + "/");
  }

  function workspaceRootNow() {
    return currentProjectId ? projectState(currentProjectId).workspaceRoot || "" : "";
  }

  async function pickAttachments() {
    if (!composerConn()) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      let picked;
      try {
        picked = await t.dialog.open({
          multiple: true,
          directory: false,
          title: "Attach files",
          defaultPath: workspaceRootNow() || undefined,
        });
      } catch (err) {
        toRenderer({ type: "systemNote", text: `[error] attach: ${String((err && err.message) || err)}` });
        return;
      }
      const paths = (Array.isArray(picked) ? picked : picked ? [picked] : [])
        .map((p) => String(p || ""))
        .filter(Boolean);
      if (!paths.length) {
        return; // cancelled
      }
      // A file inside the workspace is attached where it is — the agent then
      // reads and edits the real file, not a copy. One outside it cannot be
      // read from here: the shell grants this page a picker, not the disk.
      const root = workspaceRootNow();
      const inside = paths.filter((p) => pathInsideWorkspace(root, p));
      const outside = paths.filter((p) => !pathInsideWorkspace(root, p));
      if (inside.length) {
        toRenderer({ type: "filesPicked", files: inside.map(attachmentRefFromPath) });
      }
      if (outside.length) {
        const names = outside.map((p) => attachmentRefFromPath(p).name).join(", ");
        toRenderer({
          type: "systemNote",
          text:
            `Not attached — outside the workspace: ${names}. ` +
            "Drag the file into the chat or paste it instead; a copy is kept in .orchestra/attachments.",
        });
      }
      return;
    }
    // A browser: the page's own file input, made for this click and removed
    // after it. Its files come as bytes, which is the attachBytes path.
    if (!document.body || !document.body.appendChild) {
      return;
    }
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = true;
    input.hidden = true;
    input.addEventListener("change", () => {
      const files = Array.from(input.files || []);
      if (input.remove) {
        input.remove();
      }
      for (const file of files) {
        void storeAttachmentFile(file);
      }
    });
    document.body.appendChild(input);
    input.click();
  }

  /** @param {File} file */
  async function storeAttachmentFile(file) {
    if (!file) {
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      toRenderer({ type: "systemNote", text: `Skipped ${file.name || "file"}: exceeds 20 MB limit` });
      return;
    }
    let dataBase64 = "";
    try {
      dataBase64 = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => {
          const s = String(reader.result || "");
          resolve(s.slice(s.indexOf(",") + 1));
        };
        reader.onerror = () => reject(reader.error || new Error("read failed"));
        reader.readAsDataURL(file);
      });
    } catch (err) {
      toRenderer({ type: "systemNote", text: `[error] attach ${file.name || "file"}: ${String((err && err.message) || err)}` });
      return;
    }
    await storeAttachmentBytes({ name: file.name || "attachment", mime: file.type || undefined, dataBase64 });
  }

  /**
   * Bytes to the core, an attachment back. The chip gets a data: preview for
   * a small image — the bytes are right here, and this host serves no files.
   * @param {{name?: string, mime?: string, dataBase64?: string}} msg
   */
  async function storeAttachmentBytes(msg) {
    const dataBase64 = String((msg && msg.dataBase64) || "");
    if (!dataBase64) {
      return;
    }
    const name = String((msg && msg.name) || "attachment");
    const mime = msg && msg.mime ? String(msg.mime) : "";
    const params = { name, data_base64: dataBase64 };
    if (mime) {
      params.mime = mime;
    }
    const r = await composerRpc("attachments.store", params, "attach");
    if (!r || !r.path) {
      return; // composerRpc has already said what went wrong
    }
    const ref = {
      name: String(r.name || name),
      path: String(r.path),
      ext: r.ext ? String(r.ext) : undefined,
      kind: r.kind === "image" ? "image" : "file",
    };
    if (ref.kind === "image" && mime.startsWith("image/") && dataBase64.length <= (ATTACHMENT_PREVIEW_MAX_BYTES * 4) / 3) {
      ref.previewUri = `data:${mime};base64,${dataBase64}`;
    }
    toRenderer({ type: "filesPicked", files: [ref] });
  }

  /** The connection for the project on screen, or null. */
  function composerConn() {
    return currentProjectId ? connFor(currentProjectId) : null;
  }

  /**
   * Run one core method for the composer. Failures are reported in the chat,
   * because every one of these is something the user just clicked.
   * @param {string} method @param {any} [params] @param {string} [what]
   */
  async function composerRpc(method, params, what) {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return null;
    }
    try {
      return await conn.send(method, params || {});
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: `[error] ${what || method}: ${String((err && err.message) || err)}`,
      });
      return null;
    }
  }

  /* ---- the model pill --------------------------------------------------- */

  async function pushProviderModels() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "providerModels", providers: [], activeProvider: "", activeModel: "" });
      return;
    }
    const r = (await composerRpc("runtime.list_providers", { probe: true }, "list providers")) || {};
    const providers = Array.isArray(r.providers) ? r.providers : [];
    // The same filter the VS Code panel applies: a provider nobody has
    // configured and that offers no models is noise in a picker.
    const listed = providers
      .filter(
        (p) =>
          p.configured ||
          p.active ||
          (p.ready && (Number(p.model_count) > 0 || (Array.isArray(p.models) ? p.models.length : 0) > 0))
      )
      .map((p) => ({
        key: p.key,
        name: p.name,
        active: p.active,
        ready: p.ready,
        models: p.models,
        models_error: p.models_error,
        model_count: p.model_count,
      }));

    if (listed.length === 0) {
      const fallback = await currentConfigModels();
      if (fallback) {
        listed.push(fallback);
      }
    }

    toRenderer({
      type: "providerModels",
      activeProvider: String(r.active_provider || ""),
      activeModel: String(r.active_model || ""),
      providers: listed,
    });
  }

  /**
   * The models the configuration on disk can actually reach, as one entry.
   *
   * A project may set llm.api_base and llm.model and leave llm.provider empty
   * — it works, and the agent answers — but then nothing in the catalogue is
   * "configured", the core probes nothing, and the picker is left saying "no
   * providers" while the chat is talking to a model. runtime.list_models asks
   * the configured endpoint directly, which is the one question that has an
   * answer here.
   *
   * The entry carries no key, so picking a model from it sends setModel with
   * no provider — a plain change of llm.model, which is what the config is.
   * @returns {Promise<any|null>}
   */
  async function currentConfigModels() {
    const conn = composerConn();
    if (!conn) {
      return null;
    }
    try {
      const [models, llm] = await Promise.all([
        conn.send("runtime.list_models", {}),
        conn.send("runtime.get_llm", {}).catch(() => ({})),
      ]);
      const list = Array.isArray(models && models.models) ? models.models : [];
      if (list.length === 0) {
        return null;
      }
      const base = String((llm && llm.api_base) || "").replace(/^https?:\/\//, "");
      return {
        key: "",
        name: base ? "Configured endpoint · " + base : "Configured endpoint",
        active: true,
        ready: true,
        models: list,
        model_count: list.length,
      };
    } catch (err) {
      return null;
    }
  }

  /** @param {string} model @param {string} [provider] */
  async function applyModel(model, provider) {
    if (!model) {
      return;
    }
    const params = { model, persist: true };
    if (provider) {
      params.provider = provider;
    }
    const r = await composerRpc("runtime.set_model", params, "set model");
    if (!r) {
      return;
    }
    toRenderer({
      type: "systemNote",
      text: `Model: ${r.model || model}${r.persisted ? " (saved)" : ""}`,
    });
    // The pill reads its label off the header message and the gauge its
    // ceiling off contextInfo. Both come from the core's own answer, which
    // now names the new model and the window that goes with it — without
    // this the core had saved the change and the pill still showed the old
    // name over the old window.
    await pushLLMInfo(currentProjectId);
    await pushProviderModels();
  }

  /* ---- the Orchestra pill ------------------------------------------------ */

  async function pushOrchestraRoles() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "orchestraRoles", roles: [], defaultTier: "", error: "No workspace is open." });
      return;
    }
    try {
      const r = (await conn.send("runtime.get_orchestra", {})) || {};
      toRenderer({
        type: "orchestraRoles",
        roles: Array.isArray(r.roles) ? r.roles : [],
        defaultTier: String(r.default_tier || ""),
      });
    } catch (err) {
      // The pill shows the reason in place of the breakdown, so this is not a
      // chat note as well.
      toRenderer({
        type: "orchestraRoles",
        roles: [],
        defaultTier: "",
        error: String((err && err.message) || err),
      });
    }
  }

  /* ---- slash commands ---------------------------------------------------- */

  const SLASH_HELP = [
    "Slash commands:",
    "/clear — new chat",
    "/compact [hint] — compress LLM context",
    "/search text — find text across saved chats",
    "/sessions — open the list of chats",
    "/model — open the model menu",
    "/workflows — list this workspace's workflows",
    "/workflow name [args] — run one",
    "/settings — Orchestra settings",
    "/<command> args — run one of this workspace's own commands",
    "Rewind: hover a user message → ↩ Rewind",
    "Branch: hover a user message → ⑂ Branch",
    "Delete a chat: hover it in the sidebar → ×",
    "@file — mention files in composer",
  ].join("\n");

  /** What "/" already means, so a workspace command of the same name is not
   * offered twice — the same rule the editor's skillSlashNames applies. */
  const BUILTIN_SLASH_NAMES = [
    "clear",
    "compact",
    "help",
    "model",
    "rewind",
    "search",
    "sessions",
    "settings",
    "workflow",
    "workflows",
  ];

  /**
   * This workspace's own commands — skills under .orchestra/skills and
   * ~/.orchestra/skills, plus .claude/commands — into the "/" palette. Only
   * the core can enumerate them, and without this the palette offered the
   * seven built-ins and nothing else, so a project's own command could only
   * be run by typing its whole name and hoping.
   * @param {string} projectId
   */
  async function pushSkillCommands(projectId) {
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      return;
    }
    try {
      const r = (await conn.send("skill.list", {})) || {};
      if (projectId !== currentProjectId) {
        return;
      }
      const skills = (Array.isArray(r.skills) ? r.skills : [])
        .map((s) => ({
          name: String((s && s.name) || "").trim(),
          description: String((s && s.description) || ""),
        }))
        .filter((s) => s.name && BUILTIN_SLASH_NAMES.indexOf(s.name.toLowerCase()) === -1);
      toRenderer({ type: "skillsList", skills });
    } catch (err) {
      // A workspace with no commands — or a core that cannot list them —
      // simply leaves the palette with its built-ins.
    }
  }

  /** @param {string} cmd @param {string} [arg] */
  async function runSlashCommand(cmd, arg) {
    const name = cmd.trim().toLowerCase();
    switch (name) {
      case "/clear":
        await startSession(undefined);
        return;
      case "/compact": {
        const r = await composerRpc(
          "session.compact",
          { session_id: projectState(currentProjectId).sessionId, query: arg || "" },
          "compact"
        );
        if (r) {
          toRenderer({ type: "systemNote", text: "Context compacted." });
        }
        return;
      }
      // These two used to answer with a sentence about where the control is,
      // which reads as a command that does nothing. They open it instead.
      case "/sessions": {
        // The sidebar is where the web keeps the list; the header's button
        // for it is hidden here, so fall back to it only if there is no rail.
        if (revealSessionList()) {
          return;
        }
        const btn = document.getElementById("session-history-btn");
        if (btn && btn.click) {
          btn.click();
          return;
        }
        toRenderer({ type: "systemNote", text: "Switch chats from the tabs in the title bar." });
        return;
      }
      case "/model": {
        const pill = document.getElementById("model-pill");
        if (pill && pill.click) {
          pill.click();
          return;
        }
        toRenderer({ type: "systemNote", text: "Use the model pill in the composer to change model." });
        return;
      }
      case "/settings":
        showRailSettings(true, "general");
        return;
      case "/help":
        toRenderer({ type: "systemNote", text: SLASH_HELP });
        return;
      case "/rewind":
        toRenderer({
          type: "systemNote",
          text: "Hover a user message and click ↩ Rewind to truncate history to that checkpoint.",
        });
        return;
      case "/search":
        await searchSessions(arg || "");
        return;
      case "/workflows":
        await listWorkflows();
        return;
      case "/workflow":
        await runWorkflow(arg || "");
        return;
      default:
        await runSkillCommand(name.replace(/^\//, ""), arg || "");
    }
  }

  /**
   * A slash command that is not built in is a skill name — the same fallback
   * the VS Code panel and the TUI make.
   * @param {string} name @param {string} args
   */
  async function runSkillCommand(name, args) {
    if (!name) {
      return;
    }
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    try {
      // name + arguments: what internal/core/skill.go reads, and what the
      // editor host sends. Any other spelling arrives as an empty name and
      // the command fails before it starts.
      const r = (await conn.send("skill.invoke", { name, arguments: args })) || {};
      toRenderer({
        type: "systemNote",
        text: `[skill:${r.skill || name}] ${r.steps || 0} step(s) · marker=${r.marker || "(no marker)"}\n---\n${r.output || ""}`,
      });
    } catch (err) {
      const message = String((err && err.message) || err);
      // A name that is not a skill is a typo, not a failure worth a stack.
      toRenderer({
        type: "systemNote",
        text: /not found|unknown skill/i.test(message)
          ? `Unknown command: /${name}. Try /help`
          : `[error] skill.invoke: ${message}`,
      });
    }
  }

  /* ---- searching saved chats --------------------------------------------- */

  /**
   * Full-text search across this workspace's saved sessions. The core has
   * shipped session.search since protocol v14 and no interface ever called
   * it, so a conversation you remembered having could only be found by
   * opening chats one at a time.
   *
   * Results land in the sidebar, where the chats already live and a row is
   * already clickable — a list of titles in a chat note would name them
   * without being able to open them.
   * @param {string} query
   */
  async function searchSessions(query) {
    const q = String(query || "").trim();
    if (!q) {
      showSessionSearch("");
      toRenderer({ type: "systemNote", text: "/search text — find text across this workspace's chats." });
      return;
    }
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    await showSessionSearch(q);
  }

  /* ---- workflows --------------------------------------------------------- */

  /** The workspace's multi-stage workflows, which until now ran only from the CLI. */
  async function listWorkflows() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    let rows = [];
    try {
      const r = (await conn.send("workflow.list", {})) || {};
      rows = Array.isArray(r.workflows) ? r.workflows : [];
    } catch (err) {
      toRenderer({ type: "systemNote", text: `[error] workflow.list: ${String((err && err.message) || err)}` });
      return;
    }
    if (rows.length === 0) {
      toRenderer({
        type: "systemNote",
        text: "No workflows in this workspace. They live in .orchestra/workflows.",
      });
      return;
    }
    const lines = rows.map((w) => {
      const name = String((w && w.name) || "").trim();
      const desc = String((w && w.description) || "").trim();
      const stages = Array.isArray(w && w.stages) ? w.stages.length : 0;
      return `/workflow ${name} — ${desc || "(no description)"} · ${stages} stage(s)`;
    });
    toRenderer({ type: "systemNote", text: ["Workflows:", ...lines].join("\n") });
  }

  /**
   * Runs one workflow. Everything after the name is its argument, so
   * "/workflow review the auth package" runs "review" with "the auth package".
   * @param {string} arg
   */
  async function runWorkflow(arg) {
    const raw = String(arg || "").trim();
    if (!raw) {
      await listWorkflows();
      return;
    }
    const space = raw.search(/\s/);
    const name = space === -1 ? raw : raw.slice(0, space);
    const args = space === -1 ? "" : raw.slice(space + 1).trim();
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    toRenderer({ type: "systemNote", text: `Running workflow "${name}"…` });
    try {
      // Workflows run stage by stage against the model, so this waits for as
      // long as the run takes; the socket call carries no deadline of its own.
      const r = (await conn.send("workflow.run", { name, arguments: args })) || {};
      const stages = Array.isArray(r.stages) ? r.stages : [];
      const took = Number(r.duration_ms) || 0;
      const head = r.failure_reason
        ? `[workflow:${r.name || name}] stopped at ${r.final_stage || "?"} — ${r.failure_reason}`
        : `[workflow:${r.name || name}] done in ${Math.round(took / 1000)}s`;
      const body = stages.map((s) => `  ${s.stage_id}${s.attempt > 1 ? ` (attempt ${s.attempt})` : ""} → ${s.action}${s.marker ? ` · ${s.marker}` : ""}`);
      toRenderer({ type: "systemNote", text: [head, ...body].join("\n") });
    } catch (err) {
      const message = String((err && err.message) || err);
      toRenderer({
        type: "systemNote",
        text: /not found|unknown workflow/i.test(message)
          ? `Unknown workflow: ${name}. Try /workflows`
          : `[error] workflow.run: ${message}`,
      });
    }
  }

  /* ---- @file mentions ---------------------------------------------------- */

  /**
   * Files for the @ palette. The VS Code panel asks the editor's own index;
   * here the core's glob tool walks the workspace, which is the same set of
   * files the agent can see — and it already honours the project's excludes.
   * @param {string} query
   */
  async function searchMentions(query) {
    const q = query.trim();
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "mentionResults", query, files: [] });
      return;
    }
    // A bare @ lists the first files it finds; a query matches anywhere in the
    // name, which is what someone typing three letters expects.
    const pattern = q ? "**/*" + q.replace(/\\/g, "/") + "*" : "**/*";
    let files = [];
    try {
      const r = (await conn.send("tool.call", {
        name: "glob",
        input: { pattern, limit: 200 },
      })) || {};
      const raw = Array.isArray(r.files) ? r.files : [];
      files = raw
        .map((f) => String((f && f.path) || ""))
        .filter(Boolean)
        .map((p) => {
          const norm = p.replace(/\\/g, "/");
          const name = norm.slice(norm.lastIndexOf("/") + 1);
          const dot = name.lastIndexOf(".");
          return { name, path: p, ext: dot > 0 ? name.slice(dot + 1).toLowerCase() : "" };
        })
        // Name matches first, then path matches, each already in glob's order.
        .sort((a, b) => {
          const an = q ? (a.name.toLowerCase().includes(q.toLowerCase()) ? 0 : 1) : 0;
          const bn = q ? (b.name.toLowerCase().includes(q.toLowerCase()) ? 0 : 1) : 0;
          return an - bn;
        })
        .slice(0, 40);
    } catch (err) {
      // An empty palette is the honest answer; the composer stays usable.
      files = [];
    }
    toRenderer({ type: "mentionResults", query, files });
  }

  /* ---- rewind and the pending bar ---------------------------------------- */

  /** @param {any} uiIndex */
  async function rewindToMessage(uiIndex) {
    if (typeof uiIndex !== "number" || uiIndex < 0) {
      return;
    }
    const st = projectState(currentProjectId);
    if (!st.sessionId) {
      return;
    }
    // ui_message_index is what internal/core reads (SessionRewindParams), and
    // what the editor host and the TUI send. Any other spelling decodes as 0,
    // which silently rewinds the whole chat to its first message.
    const r = await composerRpc(
      "session.rewind",
      { session_id: st.sessionId, ui_message_index: uiIndex },
      "rewind"
    );
    if (!r) {
      return;
    }
    // The transcript is now a prefix of what is on screen; repaint it from the
    // core rather than trying to trim the DOM to match.
    const view = await composerRpc("session.get", { session_id: st.sessionId }, "reload history");
    if (!view) {
      return;
    }
    toRenderer({ type: "clearMessages" });
    toRenderer({ type: "history", messages: view.ui_messages || [] });
    toRenderer({ type: "header", sessionId: st.sessionId });
  }

  /**
   * Branch a new chat from a checkpoint, leaving this one intact. The core has
   * had session.fork since protocol v14 and only the TUI ever called it, so in
   * the app the only way to revisit a decision was rewind — which throws the
   * rest of the conversation away.
   * @param {number} uiIndex
   */
  async function forkFromMessage(uiIndex) {
    // Index 0 is refused by the core: the branch is everything BEFORE the
    // point, so it would be an empty chat.
    if (typeof uiIndex !== "number" || uiIndex < 1) {
      return;
    }
    const projectId = currentProjectId;
    const st = projectState(projectId);
    if (!st.sessionId) {
      return;
    }
    // ui_message_index, exclusive, pointing at a user message — the same
    // index rewind takes (SessionForkParams).
    const r = await composerRpc(
      "session.fork",
      { session_id: st.sessionId, ui_message_index: uiIndex },
      "branch"
    );
    const branchId = String((r && r.session_id) || "");
    if (!branchId) {
      return;
    }
    await openSessionRow(projectId, branchId);
    toRenderer({
      type: "systemNote",
      text: "Branched: this chat has everything up to that message, and the original is untouched. Ask it differently here.",
    });
  }

  /** @param {boolean} apply true applies the staged changes, false discards. */
  async function settlePending(apply) {
    const st = projectState(currentProjectId);
    const method = apply ? "session.apply_pending" : "session.discard_pending";
    const r = await composerRpc(
      method,
      { session_id: st.sessionId },
      apply ? "apply changes" : "discard changes"
    );
    if (!r) {
      return;
    }
    toRenderer({ type: "pending", files: [] });
    toRenderer({
      type: "systemNote",
      text: apply ? "Changes applied." : "Changes discarded.",
    });
  }

  /* ---- deleting a chat ---------------------------------------------------- */

  /**
   * Close a session for good. There is no confirm: the VS Code panel raises a
   * modal, and this host's only modal dialog is Tauri's, which the page is
   * deliberately not allowed to open — see openFromStart. The session list
   * repaints either way, so a mistaken click is visible immediately.
   * @param {string} sessionId
   */
  async function deleteSessionById(sessionId) {
    if (!sessionId) {
      return;
    }
    const projectId = currentProjectId;
    const st = projectState(projectId);
    const wasActive = st.sessionId === sessionId;
    const r = await composerRpc("session.close", { session_id: sessionId }, "delete chat");
    if (!r) {
      return;
    }
    if (wasActive) {
      await startSession(undefined);
    }
    await refreshSessionList(projectId);
    if (projectId === currentProjectId) {
      renderProjects();
    }
  }
  // ---- the Graph view -------------------------------------------------------
  //
  // A third segment beside Chat and Trajectory: the shape of the project, read
  // from the code knowledge graph the core keeps (.orchestra/ckg.db, over
  // index.graph). The workspace sits in the middle with the files that answer
  // to no folder; every ring outwards is one more level of nesting, and a
  // curve across the rings says symbols in one file call or use symbols in the
  // other — its width how many. Beside the picture a readout of what the index
  // holds (files, folders, symbols, tests, languages) and, for whatever is
  // selected, its neighbours and — for a file — its functions with the first
  // lines of each, read back with index.outline.
  //
  // Built at runtime, like the project label in the header: everything inside
  // #app is byte-identical with the VS Code webview's markup, and the editor
  // has its own graph viewer. The shared switch (05f-trajectory.js) knows two
  // views and stamps data-view on #app; this segment stamps "graph" the same
  // way and follows the attribute back, so either side's click leaves exactly
  // one segment selected.

  const GRAPH_LEVEL = "file";
  /** Past this many drawn nodes the picture folds a level of nesting away. */
  const GRAPH_VISIBLE_CAP = 2600;
  /** Only the heaviest relations are drawn; the rest are in the readout. */
  const GRAPH_LINK_CAP = 600;
  /** Rings never closer than this, nor further apart. */
  const GRAPH_RING_MIN = 110;
  const GRAPH_RING_MAX = 460;

  const graphApp = document.getElementById("app");
  const graphSwitchEl = document.getElementById("view-switch");
  const graphTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const graphChatBtn = document.getElementById("view-chat-btn");
  const graphTrajectoryPane = document.getElementById("trajectory");

  /** @type {any} */ let graphBtn = null;
  /** @type {any} */ let graphPane = null;
  /** @type {any} */ let graphStage = null;
  /** @type {any} */ let graphCanvas = null;
  /** @type {any} */ let graphStatsEl = null;
  /** @type {any} */ let graphHintEl = null;
  /** @type {any} */ let graphCardEl = null;
  /** @type {any} */ let graphSideEl = null;
  /** @type {any} */ let graphFilesBtn = null;
  /** @type {any} */ let graphLinksBtn = null;
  /** @type {any} */ let graphDepthOutEl = null;

  /** @type {{projectId: string, available: boolean, nodes: any[], links: any[], stats: any} | null} */
  let graphData = null;
  /** @type {any} */ let graphTree = null;
  /** @type {any} */ let graphLayout = null;
  let graphLoading = false;
  let graphShowFiles = true;
  let graphShowLinks = true;
  let graphDepth = 3;
  let graphDrawQueued = false;
  /** @type {any} */ let graphHover = null;
  /** @type {string} */ let graphSelectedId = "";
  /** @type {any} */ let graphDrag = null;
  /** @type {any} */ let graphOutline = null;
  let graphOutlineSeq = 0;
  let graphOpenSymbol = -1;

  function graphViewActive() {
    return Boolean(graphApp && graphApp.dataset && graphApp.dataset.view === "graph");
  }

  const GRAPH_ICON =
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
    '<circle cx="6" cy="18" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<circle cx="12" cy="6" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<circle cx="18" cy="18" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<path d="M7.4 16 10.6 8.2M13.4 8.2l3.2 7.8M8.4 18h7.2" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>' +
    "</svg>";

  /** One colour per language, so a ring of files says what it is made of. */
  const GRAPH_LANG_COLORS = {
    go: "#7fd1e8",
    ts: "#6aa6f5",
    tsx: "#6aa6f5",
    js: "#e6c368",
    jsx: "#e6c368",
    mjs: "#e6c368",
    cjs: "#e6c368",
    py: "#66c288",
    rs: "#e09660",
    java: "#e08484",
    kt: "#c08ae8",
    rb: "#e07a7a",
    php: "#9a8ae0",
    c: "#8fb6d8",
    h: "#8fb6d8",
    cc: "#8fb6d8",
    cpp: "#8fb6d8",
    hpp: "#8fb6d8",
    cs: "#79c6a8",
    css: "#6ad0b0",
    scss: "#6ad0b0",
    html: "#e0906a",
    md: "#9a9aa4",
    json: "#b294e0",
    yml: "#b294e0",
    yaml: "#b294e0",
    toml: "#b294e0",
    sql: "#d0a05a",
    sh: "#86bf86",
  };

  /** @param {string} id */
  function graphExtOf(id) {
    const base = id.slice(id.lastIndexOf("/") + 1);
    const dot = base.lastIndexOf(".");
    return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
  }

  /** @param {string} id */
  function graphColorFor(id) {
    return GRAPH_LANG_COLORS[graphExtOf(id)] || "";
  }

  function ensureGraphView() {
    if (graphBtn || !graphApp || !graphSwitchEl || !graphSwitchEl.appendChild) {
      return;
    }
    graphBtn = document.createElement("button");
    graphBtn.type = "button";
    graphBtn.className = "view-segment";
    graphBtn.setAttribute("role", "tab");
    graphBtn.setAttribute("aria-selected", "false");
    // A fixed string, none of it from data.
    graphBtn.innerHTML = GRAPH_ICON + "Graph";
    graphBtn.addEventListener("click", () => showGraphView());
    graphBtn.addEventListener("keydown", (e) => {
      if (e.key === "ArrowLeft" && graphTrajectoryBtn && graphTrajectoryBtn.click) {
        e.preventDefault();
        graphTrajectoryBtn.click();
        if (graphTrajectoryBtn.focus) graphTrajectoryBtn.focus();
      }
    });
    if (graphTrajectoryBtn && graphTrajectoryBtn.parentNode === graphSwitchEl && graphSwitchEl.insertBefore) {
      graphSwitchEl.insertBefore(graphBtn, graphTrajectoryBtn.nextSibling);
    } else {
      graphSwitchEl.appendChild(graphBtn);
    }

    graphPane = document.createElement("div");
    graphPane.className = "graph-pane";
    graphPane.setAttribute("role", "tabpanel");
    graphPane.setAttribute("aria-label", "Project graph");

    const toolbar = document.createElement("div");
    toolbar.className = "graph-toolbar";
    graphStatsEl = document.createElement("span");
    graphStatsEl.className = "graph-stats";

    const depth = document.createElement("span");
    depth.className = "graph-depth";
    const less = graphToolButton("−", "One level of nesting less", () => stepGraphDepth(-1));
    graphDepthOutEl = document.createElement("span");
    graphDepthOutEl.className = "graph-depth-value";
    const more = graphToolButton("+", "One level of nesting more", () => stepGraphDepth(1));
    depth.append(less, graphDepthOutEl, more);

    graphFilesBtn = graphToolButton("Files", "Draw the files, not only the folders", () => {
      graphShowFiles = !graphShowFiles;
      layoutGraph(true);
      renderGraphSide();
      scheduleGraphDraw();
    });
    graphLinksBtn = graphToolButton("Links", "Draw the calls between files", () => {
      graphShowLinks = !graphShowLinks;
      syncGraphControls();
      scheduleGraphDraw();
    });
    const fit = graphToolButton("Fit", "Fit the whole graph in view", () => {
      if (graphLayout) {
        graphLayout.fitPending = true;
        scheduleGraphDraw();
      }
    });
    const refresh = graphToolButton("Refresh", "Read the graph again", () => void loadGraph(true));
    toolbar.append(graphStatsEl, depth, graphFilesBtn, graphLinksBtn, fit, refresh);

    const body = document.createElement("div");
    body.className = "graph-body";
    graphStage = document.createElement("div");
    graphStage.className = "graph-stage";
    graphCanvas = document.createElement("canvas");
    graphCanvas.className = "graph-canvas";
    graphHintEl = document.createElement("div");
    graphHintEl.className = "graph-hint";
    graphHintEl.hidden = true;
    graphCardEl = document.createElement("div");
    graphCardEl.className = "graph-card";
    graphCardEl.hidden = true;
    graphStage.append(graphCanvas, graphHintEl, graphCardEl);
    graphSideEl = document.createElement("aside");
    graphSideEl.className = "graph-side";
    body.append(graphStage, graphSideEl);
    graphPane.append(toolbar, body);

    if (graphTrajectoryPane && graphTrajectoryPane.parentNode === graphApp && graphApp.insertBefore) {
      graphApp.insertBefore(graphPane, graphTrajectoryPane.nextSibling);
    } else {
      graphApp.appendChild(graphPane);
    }
    bindGraphCanvas();
    syncGraphControls();
    renderGraphSide();

    if (typeof MutationObserver === "function" && graphApp.dataset) {
      new MutationObserver(() => syncGraphSegment()).observe(graphApp, {
        attributes: true,
        attributeFilter: ["data-view"],
      });
    }
    if (typeof ResizeObserver === "function") {
      new ResizeObserver(() => scheduleGraphDraw()).observe(graphPane);
    }
  }

  /** @param {string} label @param {string} title @param {() => void} onClick */
  function graphToolButton(label, title, onClick) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "graph-tool";
    b.textContent = label;
    b.title = title;
    b.addEventListener("click", onClick);
    return b;
  }

  function showGraphView() {
    ensureGraphView();
    if (!graphApp || !graphApp.dataset) {
      return;
    }
    graphApp.dataset.view = "graph";
    syncGraphSegment();
    void loadGraph(false);
  }

  /** One selected segment, whichever side stamped the view. */
  function syncGraphSegment() {
    const active = graphViewActive();
    if (graphBtn && graphBtn.setAttribute) {
      graphBtn.setAttribute("aria-selected", active ? "true" : "false");
    }
    if (active) {
      for (const b of [graphChatBtn, graphTrajectoryBtn]) {
        if (b && b.setAttribute) b.setAttribute("aria-selected", "false");
      }
      void loadGraph(false);
      scheduleGraphDraw();
    }
  }

  /** @param {number} by */
  function stepGraphDepth(by) {
    if (!graphTree) return;
    const next = Math.max(1, Math.min(graphTree.maxDepth, graphDepth + by));
    if (next === graphDepth) return;
    graphDepth = next;
    layoutGraph(true);
    renderGraphSide();
    scheduleGraphDraw();
  }

  function syncGraphControls() {
    if (graphDepthOutEl) {
      const max = graphTree ? graphTree.maxDepth : graphDepth;
      graphDepthOutEl.textContent = "levels " + graphDepth + "/" + max;
      graphDepthOutEl.title = "How many levels of nesting the rings go out to";
    }
    if (graphFilesBtn) {
      graphFilesBtn.classList.toggle("on", graphShowFiles);
      graphFilesBtn.title = graphShowFiles
        ? "Draw folders only, with the calls between them summed up"
        : "Draw every file, not only the folders";
    }
    if (graphLinksBtn) {
      graphLinksBtn.classList.toggle("on", graphShowLinks);
      graphLinksBtn.title = graphShowLinks
        ? "Leave out the calls between files, keeping the nesting"
        : "Draw the calls between files again";
    }
    if (graphStatsEl && graphLayout) {
      const folders = graphLayout.nodes.filter((n) => n.group === "folder").length;
      const files = graphLayout.nodes.filter((n) => n.group === "file").length;
      const drawn = graphLayout.relationsDrawn;
      const total = graphLayout.relationsTotal;
      graphStatsEl.textContent =
        folders + " folders · " + files + " files · " +
        (drawn < total ? "the " + drawn + " heaviest of " + total + " links" : drawn + " links");
    }
  }

  /** @param {string} text */
  function setGraphHint(text) {
    if (!graphHintEl) return;
    graphHintEl.textContent = text;
    graphHintEl.hidden = !text;
  }

  /** @param {boolean} force */
  async function loadGraph(force) {
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      graphData = null;
      graphTree = null;
      graphLayout = null;
      setGraphHint(pendingOpen() ? "The workspace is still opening…" : "No workspace is open.");
      if (graphStatsEl) graphStatsEl.textContent = "";
      renderGraphSide();
      return;
    }
    if (!force && graphData && graphData.projectId === projectId) {
      return;
    }
    if (graphLoading) {
      return;
    }
    graphLoading = true;
    setGraphHint("Reading the project graph…");
    try {
      const r = (await conn.send("index.graph", { level: GRAPH_LEVEL })) || {};
      if (projectId !== currentProjectId) {
        return; // the user has moved on; the next show reads that project's
      }
      graphData = {
        projectId,
        available: Boolean(r.available),
        nodes: Array.isArray(r.nodes) ? r.nodes : [],
        links: Array.isArray(r.links) ? r.links : [],
        stats: r.stats && typeof r.stats === "object" ? r.stats : {},
      };
      graphSelectedId = "";
      graphOutline = null;
      graphOpenSymbol = -1;
      buildGraphTree();
      layoutGraph(true);
      if (!graphData.available || graphData.nodes.length === 0) {
        setGraphHint("Nothing is indexed yet. Settings → Index & Graph → Rebuild graph, then Refresh here.");
      } else {
        setGraphHint("");
      }
      renderGraphSide();
      scheduleGraphDraw();
    } catch (err) {
      if (projectId === currentProjectId) {
        setGraphHint("Could not read the graph: " + String((err && err.message) || err));
      }
    } finally {
      graphLoading = false;
    }
  }

  // A project switch or a new session clears the transcript; the graph is
  // the project's, so it follows the same signal rather than activateProject.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "clearMessages" && graphViewActive()) {
      void loadGraph(false);
    }
  });

  /* ---- the tree ------------------------------------------------------------- */

  /** @param {any} n */
  function graphSymbolCount(n) {
    const m = n && n.meta ? n.meta : {};
    for (const k of Object.keys(m)) {
      if (/символ|symbol/i.test(k) && typeof m[k] === "number") return m[k];
    }
    return 0;
  }

  /** @param {string} id */
  function graphParentOf(id) {
    const i = id.lastIndexOf("/");
    return i > 0 ? id.slice(0, i) : "";
  }

  function graphProjectName() {
    const entry = typeof known !== "undefined" ? known.find((p) => p.id === currentProjectId) : null;
    return (entry && (entry.name || entry.path)) || "workspace";
  }

  /**
   * Folders and files as one tree, the workspace at its root. Every folder in
   * a file's path exists even when the graph named only some of them, so a
   * ring is a level of nesting and nothing hangs off nowhere.
   */
  function buildGraphTree() {
    const root = {
      id: "",
      name: graphProjectName(),
      group: "root",
      depth: 0,
      parent: null,
      children: [],
      symbols: 0,
      files: 0,
      subFiles: 0,
      subSymbols: 0,
      leaves: 0,
      meta: {},
    };
    const byId = new Map([["", root]]);

    const folder = (id) => {
      const have = byId.get(id);
      if (have) return have;
      const parentId = graphParentOf(id);
      const parent = folder(parentId);
      const node = {
        id,
        name: id.slice(parentId ? parentId.length + 1 : 0) || id,
        group: "folder",
        depth: parent.depth + 1,
        parent,
        children: [],
        symbols: 0,
        files: 0,
        subFiles: 0,
        subSymbols: 0,
        leaves: 0,
        meta: {},
      };
      byId.set(id, node);
      parent.children.push(node);
      return node;
    };

    const nodes = graphData ? graphData.nodes : [];
    for (const raw of nodes) {
      if (raw.group === "folder" && raw.id) {
        const f = folder(raw.id);
        f.meta = raw.meta || {};
        if (raw.name) f.name = raw.name;
      }
    }
    for (const raw of nodes) {
      if (raw.group !== "file" || !raw.id) continue;
      const parent = folder(graphParentOf(raw.id));
      const node = {
        id: raw.id,
        name: raw.name || raw.id.slice(raw.id.lastIndexOf("/") + 1),
        group: "file",
        depth: parent.depth + 1,
        parent,
        children: [],
        symbols: graphSymbolCount(raw),
        files: 0,
        subFiles: 1,
        subSymbols: 0,
        leaves: 1,
        meta: raw.meta || {},
      };
      byId.set(node.id, node);
      parent.children.push(node);
    }

    // Roll the counts up and note how deep the tree runs.
    let maxDepth = 1;
    const roll = (n) => {
      let files = n.group === "file" ? 1 : 0;
      let symbols = n.symbols;
      for (const c of n.children) {
        roll(c);
        files += c.subFiles;
        symbols += c.subSymbols;
      }
      n.subFiles = files;
      n.subSymbols = symbols;
      if (n.depth > maxDepth) maxDepth = n.depth;
      // Folders first, then files; each by name, so the picture is the same
      // every time it is drawn.
      n.children.sort((a, b) => {
        if (a.group !== b.group) return a.group === "folder" ? -1 : 1;
        return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
      });
    };
    roll(root);

    graphTree = { root, byId, maxDepth };
    graphDepth = autoGraphDepth();
  }

  /** @param {number} depth */
  function countGraphVisible(depth) {
    let n = 0;
    const walk = (node) => {
      if (node.depth > depth) return;
      if (node.group === "file" && !graphShowFiles) return;
      n++;
      for (const c of node.children) walk(c);
    };
    walk(graphTree.root);
    return n;
  }

  /** The most nesting that still draws a picture rather than a cloud. */
  function autoGraphDepth() {
    if (!graphTree) return 3;
    let best = 1;
    for (let d = 1; d <= graphTree.maxDepth; d++) {
      if (countGraphVisible(d) > GRAPH_VISIBLE_CAP) break;
      best = d;
    }
    return Math.max(1, Math.min(graphTree.maxDepth, best));
  }

  /* ---- the layout ----------------------------------------------------------- */

  /**
   * A radial tree: the workspace at the centre, one ring per level of nesting,
   * every subtree its own wedge. Deterministic — no simulation to settle, so a
   * thousand files draw as fast as ten and land in the same place twice.
   * @param {boolean} refit
   */
  function layoutGraph(refit) {
    if (!graphTree) {
      graphLayout = null;
      syncGraphControls();
      return;
    }
    if (graphDepth > graphTree.maxDepth) graphDepth = graphTree.maxDepth;

    const visible = [];
    const byId = new Map();
    const pick = (node) => {
      if (node.group === "file" && !graphShowFiles) return null;
      const shown = {
        id: node.id,
        name: node.name,
        group: node.group,
        depth: node.depth,
        meta: node.meta,
        symbols: node.group === "file" ? node.symbols : node.subSymbols,
        files: node.subFiles,
        folded: 0,
        children: [],
        leaves: 1,
        angle: 0,
        radius: 0,
        x: 0,
        y: 0,
        r: 4,
        outW: 0,
        inW: 0,
        neighbours: new Map(),
      };
      if (node.depth < graphDepth) {
        for (const c of node.children) {
          const kid = pick(c);
          if (kid) shown.children.push(kid);
        }
      }
      if (!shown.children.length && node.children.length) {
        // The subtree stops here: say how much of it is folded away.
        shown.folded = node.subFiles - (node.group === "file" ? 1 : 0);
      }
      shown.leaves = shown.children.length
        ? shown.children.reduce((a, c) => a + c.leaves, 0)
        : 1;
      visible.push(shown);
      byId.set(shown.id, shown);
      return shown;
    };
    const root = pick(graphTree.root);

    // Angles by leaf count, so a wide subtree gets a wide wedge; radius by
    // depth, spread so the outermost ring has room for its leaves.
    const leaves = Math.max(1, root.leaves);
    const depthSpan = Math.max(1, graphDepth);
    const ringGap = Math.max(
      GRAPH_RING_MIN,
      Math.min(GRAPH_RING_MAX, (leaves * 17) / (2 * Math.PI * depthSpan))
    );
    const place = (node, a0, a1) => {
      node.angle = (a0 + a1) / 2;
      node.span = a1 - a0;
      node.radius = node.depth * ringGap;
      node.x = Math.cos(node.angle) * node.radius;
      node.y = Math.sin(node.angle) * node.radius;
      node.r =
        node.group === "file"
          ? Math.min(12, 3.4 + Math.sqrt(node.symbols) * 0.7)
          : node.group === "root"
            ? 16
            : Math.min(18, 5.5 + Math.sqrt(node.files + 1) * 1.7);
      let a = a0;
      for (const c of node.children) {
        const span = ((a1 - a0) * c.leaves) / Math.max(1, node.leaves);
        place(c, a, a + span);
        a += span;
      }
    };
    // A hair short of a full turn: the first and last wedge stay apart.
    place(root, -Math.PI / 2, -Math.PI / 2 + Math.PI * 2 * 0.997);

    // Links: containment from the tree, relations from the graph, both folded
    // onto whichever ancestor is actually drawn.
    const links = [];
    const index = new Map(visible.map((n, i) => [n.id, i]));
    for (const n of visible) {
      for (const c of n.children) {
        links.push({ a: index.get(n.id), b: index.get(c.id), rel: "in_folder", w: 1 });
      }
    }
    const visibleAncestor = (id) => {
      let node = graphTree.byId.get(id);
      while (node && !byId.has(node.id)) node = node.parent;
      return node ? node.id : null;
    };
    const weights = new Map();
    for (const l of graphData ? graphData.links : []) {
      if (l.relation === "in_folder") continue;
      const a = visibleAncestor(l.source);
      const b = visibleAncestor(l.target);
      if (a === null || b === null || a === b) continue;
      const key = a + "\u0000" + b;
      weights.set(key, (weights.get(key) || 0) + (Number(l.weight) || 1));
    }
    const relations = [];
    for (const [key, w] of weights) {
      const [a, b] = key.split("\u0000");
      const ia = index.get(a);
      const ib = index.get(b);
      if (ia === undefined || ib === undefined) continue;
      relations.push({ a: ia, b: ib, rel: "calls", w });
      const na = visible[ia];
      const nb = visible[ib];
      na.outW += w;
      nb.inW += w;
      na.neighbours.set(b, (na.neighbours.get(b) || 0) + w);
      nb.neighbours.set(a, (nb.neighbours.get(a) || 0) + w);
    }
    // Every relation counts towards a node's own numbers and its list of
    // neighbours; only the heaviest are drawn, or the picture is a haze.
    relations.sort((x, y) => y.w - x.w);
    for (const e of relations.slice(0, GRAPH_LINK_CAP)) links.push(e);
    const relationsDrawn = Math.min(relations.length, GRAPH_LINK_CAP);
    const relationsTotal = relations.length;

    const keep = graphLayout && !refit ? graphLayout : null;
    graphLayout = {
      nodes: visible,
      links,
      byId,
      index,
      relationsDrawn,
      relationsTotal,
      ringGap,
      maxRadius: depthSpan * ringGap,
      scale: keep ? keep.scale : 1,
      tx: keep ? keep.tx : 0,
      ty: keep ? keep.ty : 0,
      fitPending: !keep,
    };
    graphHover = null;
    syncGraphControls();
  }

  function graphCssVar(name, fallback) {
    try {
      const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
      return v || fallback;
    } catch (e) {
      return fallback;
    }
  }

  function fitGraphToView(width, height) {
    const s = graphLayout;
    if (!s || !s.nodes.length) return;
    let reach = 1;
    for (const n of s.nodes) reach = Math.max(reach, n.radius + n.r);
    const span = reach * 2 + 60;
    s.scale = Math.max(0.04, Math.min(2.5, Math.min((width - 32) / span, (height - 32) / span)));
    s.tx = width / 2;
    s.ty = height / 2;
  }

  function scheduleGraphDraw() {
    if (graphDrawQueued) return;
    graphDrawQueued = true;
    requestAnimationFrame(() => {
      graphDrawQueued = false;
      drawGraph();
    });
  }

  function drawGraph() {
    if (!graphViewActive() || !graphCanvas || !graphCanvas.getContext) return;
    const rect = graphCanvas.getBoundingClientRect();
    const width = Math.max(1, Math.floor(rect.width));
    const height = Math.max(1, Math.floor(rect.height));
    const dpr = window.devicePixelRatio || 1;
    if (graphCanvas.width !== Math.floor(width * dpr) || graphCanvas.height !== Math.floor(height * dpr)) {
      graphCanvas.width = Math.floor(width * dpr);
      graphCanvas.height = Math.floor(height * dpr);
    }
    const ctx = graphCanvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    const s = graphLayout;
    if (!s) return;
    if (s.fitPending) {
      fitGraphToView(width, height);
      s.fitPending = false;
    }

    const fg = graphCssVar("--fg", "#e6e6ea");
    const muted = graphCssVar("--muted", "#86868d");
    const accent = graphCssVar("--accent", "#8b8cff");
    const border = graphCssVar("--border", "#2b2b30");
    const surface = graphCssVar("--surface", "#1f1f23");
    const focus = graphSelectedId ? s.byId.get(graphSelectedId) : null;
    const hot = graphHover || focus;

    ctx.save();
    ctx.translate(s.tx, s.ty);
    ctx.scale(s.scale, s.scale);
    const inv = 1 / s.scale;

    // The rings themselves: one per level of nesting that has anything on it.
    let deepest = 0;
    for (const n of s.nodes) deepest = Math.max(deepest, n.depth);
    ctx.strokeStyle = border;
    ctx.globalAlpha = 0.55;
    for (let d = 1; d <= deepest; d++) {
      ctx.beginPath();
      ctx.arc(0, 0, d * s.ringGap, 0, Math.PI * 2);
      ctx.lineWidth = inv;
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    // Containment, drawn as the tree it is: out along the parent's ring to
    // the child's angle, then outwards to the child.
    ctx.lineCap = "round";
    for (const e of s.links) {
      if (e.rel !== "in_folder") continue;
      const a = s.nodes[e.a];
      const b = s.nodes[e.b];
      const lit = hot === a || hot === b;
      ctx.strokeStyle = lit ? muted : border;
      ctx.globalAlpha = lit ? 1 : 0.8;
      ctx.lineWidth = (lit ? 1.6 : 1) * inv;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.quadraticCurveTo(Math.cos(b.angle) * a.radius, Math.sin(b.angle) * a.radius, b.x, b.y);
      ctx.stroke();
    }

    // Relations, bowed towards the middle so a bundle of them reads as one
    // stream rather than a net over the whole picture.
    for (const e of s.links) {
      if (e.rel === "in_folder") continue;
      const a = s.nodes[e.a];
      const b = s.nodes[e.b];
      const lit = hot === a || hot === b;
      if (!graphShowLinks && !lit) continue;
      ctx.strokeStyle = lit ? fg : accent;
      ctx.globalAlpha = lit ? 0.95 : hot ? 0.05 : 0.13;
      ctx.lineWidth = Math.min(4, 0.7 + Math.log(e.w + 1) * 0.6) * inv;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.quadraticCurveTo(((a.x + b.x) / 2) * 0.35, ((a.y + b.y) / 2) * 0.35, b.x, b.y);
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    for (const a of s.nodes) {
      const lit = hot === a;
      const near = hot && hot.neighbours && hot.neighbours.has(a.id);
      ctx.beginPath();
      ctx.arc(a.x, a.y, a.r, 0, Math.PI * 2);
      if (a.group === "file") {
        ctx.fillStyle = lit ? fg : graphColorFor(a.id) || accent;
        ctx.globalAlpha = hot && !lit && !near ? 0.55 : 1;
        ctx.fill();
      } else {
        ctx.fillStyle = surface;
        ctx.fill();
        ctx.lineWidth = (lit ? 2.4 : 1.5) * inv;
        ctx.strokeStyle = lit ? fg : a.group === "root" ? accent : muted;
        ctx.stroke();
      }
      ctx.globalAlpha = 1;
      if (graphSelectedId && a.id === graphSelectedId) {
        ctx.beginPath();
        ctx.arc(a.x, a.y, a.r + 5 * inv, 0, Math.PI * 2);
        ctx.strokeStyle = accent;
        ctx.lineWidth = 2 * inv;
        ctx.stroke();
      }
    }

    // Labels at screen size, turned to sit along their ring: folders whenever
    // there are few enough to read, files when the view is close enough or the
    // node is the one being looked at. The name is the point of the picture.
    ctx.textBaseline = "middle";
    for (const a of s.nodes) {
      const lit = hot === a;
      // A name is drawn when its own slice of the ring is wide enough on
      // screen to hold one, which is what keeps a thousand files from
      // writing over each other; the one being looked at always is.
      const room = a.span * Math.max(a.radius, s.ringGap) * s.scale;
      const named = hot && hot.neighbours && hot.neighbours.size <= 40 && hot.neighbours.has(a.id);
      const show = a.group === "root" || lit || named || room > (a.group === "folder" ? 12 : 13);
      if (!show) continue;
      const size = (a.group === "root" ? 14 : a.group === "folder" ? 12 : 11) * inv;
      ctx.font = size + "px ui-sans-serif, system-ui, sans-serif";
      ctx.fillStyle = lit || a.group === "root" ? fg : muted;
      ctx.globalAlpha = lit ? 1 : 0.9;
      if (a.group === "root") {
        ctx.textAlign = "center";
        ctx.fillText(a.name, 0, -a.r - 10 * inv);
        ctx.textAlign = "left";
        continue;
      }
      // Along the ray, reading outwards; flipped on the left half so no name
      // is upside down.
      const flip = Math.cos(a.angle) < 0;
      ctx.save();
      ctx.translate(a.x, a.y);
      ctx.rotate(a.angle + (flip ? Math.PI : 0));
      ctx.textAlign = flip ? "right" : "left";
      ctx.fillText(a.name, (flip ? -1 : 1) * (a.r + 5 * inv), 0);
      ctx.restore();
    }
    ctx.globalAlpha = 1;
    ctx.textAlign = "left";
    ctx.restore();
  }

  /* ---- the pointer ---------------------------------------------------------- */

  /** World coordinates of a pointer event on the canvas. */
  function graphWorldPoint(ev) {
    const rect = graphCanvas.getBoundingClientRect();
    const sx = ev.clientX - rect.left;
    const sy = ev.clientY - rect.top;
    const s = graphLayout;
    return { sx, sy, x: (sx - s.tx) / s.scale, y: (sy - s.ty) / s.scale };
  }

  function graphNodeAt(x, y) {
    const s = graphLayout;
    if (!s) return null;
    let best = null;
    let bestD = Infinity;
    const slack = 5 / s.scale;
    for (const a of s.nodes) {
      const dx = a.x - x;
      const dy = a.y - y;
      const d = Math.sqrt(dx * dx + dy * dy);
      if (d <= a.r + slack && d < bestD) {
        best = a;
        bestD = d;
      }
    }
    return best;
  }

  function showGraphCard(node, sx, sy) {
    if (!graphCardEl) return;
    if (!node) {
      graphCardEl.hidden = true;
      return;
    }
    // textContent throughout: names and paths come off the disk.
    graphCardEl.innerHTML = "";
    const title = document.createElement("div");
    title.className = "graph-card-title";
    title.textContent = node.name;
    const path = document.createElement("div");
    path.className = "graph-card-path";
    path.textContent = node.id || "(workspace root)";
    graphCardEl.append(title, path);
    const rows = [];
    if (node.group === "file") {
      rows.push(["file", node.symbols ? node.symbols + " symbols" : ""]);
    } else {
      rows.push([node.group === "root" ? "workspace" : "folder", node.files + " files"]);
      if (node.folded) rows.push(["folded in", node.folded + " files deeper"]);
    }
    if (node.outW || node.inW) rows.push(["links", node.outW + " out · " + node.inW + " in"]);
    for (const [k, v] of rows) {
      if (!v) continue;
      const row = document.createElement("div");
      row.className = "graph-card-row";
      const key = document.createElement("span");
      key.textContent = k;
      const val = document.createElement("span");
      val.textContent = v;
      row.append(key, val);
      graphCardEl.appendChild(row);
    }
    graphCardEl.hidden = false;
    const stage = graphStage.getBoundingClientRect();
    const canvasRect = graphCanvas.getBoundingClientRect();
    let left = canvasRect.left - stage.left + sx + 14;
    let top = canvasRect.top - stage.top + sy + 14;
    const cw = graphCardEl.offsetWidth || 220;
    const ch = graphCardEl.offsetHeight || 90;
    if (left + cw > stage.width - 8) left = Math.max(8, left - cw - 28);
    if (top + ch > stage.height - 8) top = Math.max(8, top - ch - 28);
    graphCardEl.style.left = left + "px";
    graphCardEl.style.top = top + "px";
  }

  function bindGraphCanvas() {
    if (!graphCanvas || !graphCanvas.addEventListener) return;
    graphCanvas.addEventListener("pointerdown", (ev) => {
      if (!graphLayout) return;
      const p = graphWorldPoint(ev);
      graphDrag = { lastX: p.sx, lastY: p.sy, moved: 0, node: graphNodeAt(p.x, p.y) };
      if (graphCanvas.setPointerCapture) graphCanvas.setPointerCapture(ev.pointerId);
    });
    graphCanvas.addEventListener("pointermove", (ev) => {
      if (!graphLayout) return;
      const p = graphWorldPoint(ev);
      if (graphDrag) {
        const dx = p.sx - graphDrag.lastX;
        const dy = p.sy - graphDrag.lastY;
        graphDrag.lastX = p.sx;
        graphDrag.lastY = p.sy;
        graphDrag.moved += Math.abs(dx) + Math.abs(dy);
        if (graphDrag.moved > 3) {
          graphLayout.tx += dx;
          graphLayout.ty += dy;
          graphCanvas.classList.add("dragging");
          showGraphCard(null);
          scheduleGraphDraw();
        }
        return;
      }
      const node = graphNodeAt(p.x, p.y);
      if (node !== graphHover) {
        graphHover = node;
        scheduleGraphDraw();
      }
      showGraphCard(node, p.sx, p.sy);
      graphCanvas.classList.toggle("over-node", Boolean(node));
    });
    const release = (ev) => {
      const drag = graphDrag;
      graphDrag = null;
      graphCanvas.classList.remove("dragging");
      if (drag && drag.moved <= 3 && ev.type === "pointerup") {
        selectGraphNode(drag.node ? drag.node.id : "");
      }
    };
    graphCanvas.addEventListener("pointerup", release);
    graphCanvas.addEventListener("pointercancel", release);
    graphCanvas.addEventListener("pointerleave", () => {
      graphHover = null;
      showGraphCard(null);
      scheduleGraphDraw();
    });
    graphCanvas.addEventListener(
      "wheel",
      (ev) => {
        if (!graphLayout) return;
        ev.preventDefault();
        const p = graphWorldPoint(ev);
        const factor = Math.exp(-ev.deltaY * 0.0012);
        const next = Math.max(0.03, Math.min(8, graphLayout.scale * factor));
        // Zoom about the pointer: the world point under it stays put.
        graphLayout.tx = p.sx - p.x * next;
        graphLayout.ty = p.sy - p.y * next;
        graphLayout.scale = next;
        graphLayout.fitPending = false;
        scheduleGraphDraw();
      },
      { passive: false }
    );
    graphCanvas.addEventListener("dblclick", () => {
      if (graphLayout) {
        graphLayout.fitPending = true;
        scheduleGraphDraw();
      }
    });
  }

  /* ---- the readout ---------------------------------------------------------- */

  /** @param {string} id */
  function selectGraphNode(id) {
    const node = graphLayout ? graphLayout.byId.get(id) : null;
    graphSelectedId = node ? id : "";
    graphOpenSymbol = -1;
    graphOutline = null;
    renderGraphSide();
    scheduleGraphDraw();
    if (node && node.group === "file") {
      void loadGraphOutline(node.id);
    }
  }

  /** @param {string} path */
  async function loadGraphOutline(path) {
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) return;
    const seq = ++graphOutlineSeq;
    graphOutline = { path, loading: true, error: "", result: null };
    renderGraphSide();
    try {
      const r = await conn.send("index.outline", { path });
      if (seq !== graphOutlineSeq || projectId !== currentProjectId) return;
      graphOutline = { path, loading: false, error: "", result: r || {} };
    } catch (err) {
      if (seq !== graphOutlineSeq) return;
      graphOutline = { path, loading: false, error: String((err && err.message) || err), result: null };
    }
    renderGraphSide();
  }

  /** @param {any} parent @param {string} cls @param {string} text */
  function graphEl(parent, cls, text) {
    const el = document.createElement("div");
    el.className = cls;
    if (text !== undefined) el.textContent = text;
    if (parent) parent.appendChild(el);
    return el;
  }

  /** A label, a leader, a value — the shape a console gives a count. */
  function graphReadout(parent, label, value, extraClass) {
    const row = document.createElement("div");
    row.className = "graph-ro" + (extraClass ? " " + extraClass : "");
    const k = document.createElement("span");
    k.className = "graph-ro-k";
    k.textContent = label;
    const dots = document.createElement("span");
    dots.className = "graph-ro-dots";
    const v = document.createElement("span");
    v.className = "graph-ro-v";
    v.textContent = String(value);
    row.append(k, dots, v);
    parent.appendChild(row);
    return row;
  }

  function graphSection(parent, title) {
    const block = document.createElement("section");
    block.className = "graph-block";
    graphEl(block, "graph-block-title", title);
    parent.appendChild(block);
    return block;
  }

  function renderGraphSide() {
    if (!graphSideEl) return;
    graphSideEl.innerHTML = "";
    const stats = (graphData && graphData.stats) || {};
    const nodes = (graphData && graphData.nodes) || [];

    const head = graphSection(graphSideEl, "Workspace");
    graphEl(head, "graph-head-name", graphProjectName());
    const entry = typeof known !== "undefined" ? known.find((p) => p.id === currentProjectId) : null;
    if (entry && entry.path) graphEl(head, "graph-head-path", entry.path);
    graphReadout(head, "index", graphData ? (graphData.available ? "ready" : "empty") : "—",
      graphData && graphData.available ? "ok" : "");

    // Whatever is selected goes straight under the workspace: it is what the
    // person just clicked, and the counters are not going anywhere.
    if (graphSelectedId) renderGraphSelection(graphSideEl);

    if (graphData) {
      const folders = nodes.filter((n) => n.group === "folder").length;
      const files = nodes.filter((n) => n.group === "file").length;
      const relations = (graphData.links || []).filter((l) => l.relation !== "in_folder").length;
      const index = graphSection(graphSideEl, "What is indexed");
      graphReadout(index, "files", stats.files || files);
      graphReadout(index, "folders", folders);
      graphReadout(index, "symbols", stats.nodes || 0);
      graphReadout(index, "functions", stats.funcs || 0);
      graphReadout(index, "types", stats.types || 0);
      graphReadout(index, "tests", stats.tests || 0);
      graphReadout(index, "packages", stats.packages || 0);
      graphReadout(index, "relations", stats.edges || 0);
      graphReadout(index, "file links", relations);
      if (stats.embeddings) {
        graphReadout(index, "embeddings", stats.embeddings + (stats.missing_embeddings ? " (+" + stats.missing_embeddings + " missing)" : ""));
      }
      graphReadout(index, "nesting", graphTree ? graphTree.maxDepth + " levels" : "—");

      // What the files are made of: the graph's own languages when it has
      // them, the extensions of the file nodes otherwise.
      const kinds = new Map();
      const langs = stats.langs && typeof stats.langs === "object" ? stats.langs : null;
      if (langs) {
        for (const k of Object.keys(langs)) kinds.set(k, langs[k]);
      } else {
        for (const n of nodes) {
          if (n.group !== "file") continue;
          const ext = graphExtOf(n.id) || "other";
          kinds.set(ext, (kinds.get(ext) || 0) + 1);
        }
      }
      const sorted = [...kinds.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10);
      if (sorted.length) {
        const block = graphSection(graphSideEl, "File types");
        for (const [name, count] of sorted) {
          const row = graphReadout(block, name, count, "graph-ro-lang");
          const dot = document.createElement("i");
          dot.className = "graph-swatch";
          dot.style.background = GRAPH_LANG_COLORS[String(name).toLowerCase()] || "var(--accent)";
          row.insertBefore(dot, row.firstChild);
        }
      }

      // The files everything else leans on: the picture's centre of gravity.
      if (graphLayout) {
        const hubs = graphLayout.nodes
          .filter((n) => n.group === "file" && n.inW + n.outW > 0)
          .sort((a, b) => b.inW + b.outW - (a.inW + a.outW))
          .slice(0, 6);
        if (hubs.length) {
          const block = graphSection(graphSideEl, "Most connected");
          for (const h of hubs) graphNeighbourRow(block, h.id, h.inW + h.outW);
        }
      }
    }

    if (!graphSelectedId) renderGraphSelection(graphSideEl);
  }

  /** @param {any} parent @param {string} id @param {number} weight */
  function graphNeighbourRow(parent, id, weight) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "graph-link-row";
    const name = document.createElement("span");
    name.className = "graph-link-name";
    name.textContent = id.slice(id.lastIndexOf("/") + 1);
    const path = document.createElement("span");
    path.className = "graph-link-path";
    path.textContent = id;
    const w = document.createElement("span");
    w.className = "graph-link-weight";
    w.textContent = String(weight);
    // Name and weight share the first row; the path runs under both.
    row.append(name, w, path);
    row.title = id;
    row.addEventListener("click", () => selectGraphNode(id));
    parent.appendChild(row);
    return row;
  }

  const GRAPH_SYMBOL_LABELS = {
    func: "fn",
    method: "fn",
    struct: "type",
    interface: "iface",
    type: "type",
    test: "test",
    const: "const",
    var: "var",
  };

  /** @param {any} parent */
  function renderGraphSelection(parent) {
    const node = graphLayout && graphSelectedId ? graphLayout.byId.get(graphSelectedId) : null;
    if (!node) {
      const empty = graphSection(parent, "Selection");
      graphEl(empty, "graph-empty", "Click a node to see what is inside it and what it is wired to.");
      return;
    }
    const block = graphSection(parent, node.group === "file" ? "File" : "Folder");
    graphEl(block, "graph-head-name", node.name);
    graphEl(block, "graph-head-path", node.id || "(workspace root)");
    if (node.group === "file") {
      graphReadout(block, "symbols", node.symbols);
      const ext = graphExtOf(node.id);
      if (ext) graphReadout(block, "type", ext);
    } else {
      graphReadout(block, "files", node.files);
      graphReadout(block, "symbols", node.symbols);
      if (node.folded) graphReadout(block, "folded away", node.folded + " files");
    }
    graphReadout(block, "links out", node.outW);
    graphReadout(block, "links in", node.inW);

    // For a file the functions come first — that is what the person opened it
    // for; its neighbours follow.
    if (node.group === "file") renderGraphFunctions(parent, node);
    const neighbours = [...node.neighbours.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10);
    if (neighbours.length) {
      const nb = graphSection(parent, "Wired to");
      for (const [id, w] of neighbours) graphNeighbourRow(nb, id, w);
    }
  }

  /** @param {any} parent @param {any} node */
  function renderGraphFunctions(parent, node) {
    const fns = graphSection(parent, "Inside this file");
    if (!graphOutline || graphOutline.path !== node.id) {
      graphEl(fns, "graph-empty", "Reading…");
      return;
    }
    if (graphOutline.loading) {
      graphEl(fns, "graph-empty", "Reading…");
      return;
    }
    if (graphOutline.error) {
      graphEl(fns, "graph-empty", graphOutline.error);
      return;
    }
    const res = graphOutline.result || {};
    const symbols = Array.isArray(res.symbols) ? res.symbols : [];
    if (res.lines) {
      graphReadout(fns, "lines", res.lines);
    }
    if (!symbols.length) {
      graphEl(fns, "graph-empty", res.available ? "No symbols indexed in this file." : "This file is not in the index.");
      return;
    }
    symbols.forEach((sym, i) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "graph-sym" + (graphOpenSymbol === i ? " open" : "");
      const kind = document.createElement("span");
      kind.className = "graph-sym-kind";
      kind.textContent = GRAPH_SYMBOL_LABELS[String(sym.kind || "").toLowerCase()] || String(sym.kind || "sym");
      const name = document.createElement("span");
      name.className = "graph-sym-name";
      name.textContent = sym.name || "(unnamed)";
      const where = document.createElement("span");
      where.className = "graph-sym-lines";
      where.textContent = sym.line_start ? sym.line_start + "–" + sym.line_end : "";
      row.append(kind, name, where);
      row.title = (sym.fqn || sym.name || "") + " · " + (sym.calls_out || 0) + " out · " + (sym.calls_in || 0) + " in";
      row.addEventListener("click", () => {
        graphOpenSymbol = graphOpenSymbol === i ? -1 : i;
        renderGraphSide();
      });
      fns.appendChild(row);
      if (graphOpenSymbol === i) {
        const pre = document.createElement("pre");
        pre.className = "graph-code";
        // textContent: this is source off the disk, never markup.
        pre.textContent = sym.preview || "(no source to show)";
        fns.appendChild(pre);
        if (sym.truncated) {
          const shown = (sym.preview || "").split("\n").length;
          const whole = Number(sym.line_end) - Number(sym.line_start) + 1;
          graphEl(fns, "graph-code-note", "first " + shown + " of " + whole + " lines");
        }
      }
    });
  }

  ensureGraphView();
  // ---- the turn rail ----------------------------------------------------------
  //
  // One short bar per message of the person's own, down the right edge of
  // the transcript, in place of the scrollbar (rail.css hides that). Each bar
  // sits where its message is in the scroll, the one for the message in view
  // is lit, and a click scrolls to it: a long conversation is navigated by
  // its questions rather than by dragging a thumb. Built at runtime, outside
  // the shared markup, like the graph view — the editor's webview keeps its
  // scrollbar.

  const railMessagesEl = document.getElementById("messages");
  const railAppEl = document.getElementById("app");
  /** @type {any} */
  let turnRailEl = null;
  /** @type {{el: any, tick: any}[]} */
  let turnRailItems = [];
  let turnRailQueued = false;

  function ensureTurnRail() {
    if (turnRailEl || !railAppEl || !railAppEl.appendChild) {
      return;
    }
    turnRailEl = document.createElement("div");
    turnRailEl.className = "turn-rail";
    turnRailEl.setAttribute("aria-hidden", "true");
    turnRailEl.hidden = true;
    railAppEl.appendChild(turnRailEl);
  }

  function scheduleTurnRail() {
    if (turnRailQueued) return;
    turnRailQueued = true;
    requestAnimationFrame(() => {
      turnRailQueued = false;
      rebuildTurnRail();
    });
  }

  /** The transcript's own text of a user message, for the bar's tooltip. */
  function turnRailLabel(el) {
    const body = el.querySelector ? el.querySelector(".user-text") : null;
    const text = String((body || el).textContent || "")
      .replace(/\s+/g, " ")
      .trim();
    return text.length > 90 ? text.slice(0, 87) + "…" : text;
  }

  function rebuildTurnRail() {
    ensureTurnRail();
    if (!turnRailEl || !railMessagesEl || !railMessagesEl.querySelectorAll) {
      return;
    }
    const users = Array.from(railMessagesEl.querySelectorAll(".msg.user"));
    const chatView = !railAppEl.dataset || !railAppEl.dataset.view || railAppEl.dataset.view === "chat";
    const show = users.length > 0 && chatView && !railMessagesEl.hidden;
    turnRailEl.hidden = !show;
    turnRailItems = [];
    if (!show) {
      return;
    }
    if (!railMessagesEl.getBoundingClientRect || !railAppEl.getBoundingClientRect) {
      return;
    }
    const appRect = railAppEl.getBoundingClientRect();
    const box = railMessagesEl.getBoundingClientRect();
    turnRailEl.style.top = Math.round(box.top - appRect.top + 8) + "px";
    turnRailEl.style.height = Math.max(0, Math.round(box.height - 16)) + "px";
    const total = Math.max(railMessagesEl.scrollHeight || box.height, 1);
    turnRailEl.innerHTML = "";
    for (const el of users) {
      const r = el.getBoundingClientRect();
      const offset = r.top - box.top + (railMessagesEl.scrollTop || 0);
      const tick = document.createElement("button");
      tick.type = "button";
      tick.className = "turn-tick";
      const label = turnRailLabel(el);
      tick.title = label;
      tick.setAttribute("aria-label", label || "message");
      tick.style.top = Math.min(100, Math.max(0, (offset / total) * 100)).toFixed(2) + "%";
      tick.addEventListener("click", () => {
        const at = el.getBoundingClientRect().top - railMessagesEl.getBoundingClientRect().top + (railMessagesEl.scrollTop || 0);
        if (railMessagesEl.scrollTo) {
          railMessagesEl.scrollTo({ top: Math.max(0, at - 12), behavior: "smooth" });
        } else {
          railMessagesEl.scrollTop = Math.max(0, at - 12);
        }
      });
      turnRailEl.appendChild(tick);
      turnRailItems.push({ el, tick });
    }
    updateTurnRailCurrent();
  }

  /** Light the bar of the last question that has scrolled into the upper part of the view. */
  function updateTurnRailCurrent() {
    if (!turnRailItems.length || !railMessagesEl.getBoundingClientRect) return;
    const box = railMessagesEl.getBoundingClientRect();
    const line = box.top + box.height * 0.4;
    let current = null;
    for (const item of turnRailItems) {
      if (item.el.getBoundingClientRect().top <= line) current = item;
    }
    if (!current) current = turnRailItems[0];
    for (const item of turnRailItems) {
      item.tick.classList.toggle("current", item === current);
    }
  }

  if (railMessagesEl && railMessagesEl.addEventListener) {
    railMessagesEl.addEventListener("scroll", () => updateTurnRailCurrent(), { passive: true });
  }
  if (railMessagesEl && typeof MutationObserver === "function") {
    new MutationObserver(() => scheduleTurnRail()).observe(railMessagesEl, { childList: true });
  }
  if (railMessagesEl && typeof ResizeObserver === "function") {
    new ResizeObserver(() => scheduleTurnRail()).observe(railMessagesEl);
  }
  if (railAppEl && typeof MutationObserver === "function") {
    new MutationObserver(() => scheduleTurnRail()).observe(railAppEl, {
      attributes: true,
      attributeFilter: ["data-view"],
    });
  }
  // The transcript changes wholesale on these; the observer above sees the
  // children, this sees the moment.
  window.addEventListener("message", (ev) => {
    const t = ev.data && ev.data.type;
    if (t === "clearMessages" || t === "history" || t === "userEcho") {
      scheduleTurnRail();
    }
  });
})();
