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
          p.reject(new Error(msg.error.message || "rpc error"));
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

  function activeConn() {
    return active;
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
        setChromeHint(toolDisplayName(msg.toolName) + "…", false);
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
      setChromeHint(toolDisplayName(msg.toolName) + "…", false);
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

  const TRAJ_GLYPH = { turn: "◆", step: "▸", tool: "⚙", text: "¶", reasoning: "…", error: "!", stage: "▣", pending: "±", route: "↦", other: "·" };

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

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = "Trajectory unavailable: " + trajError;
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = "No trajectory was recorded for this session — it predates the log.";
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = trajRecorded === null ? "Loading trajectory…" : "Nothing has happened in this session yet.";
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    trajectorySummary.textContent = turns + " turn" + (turns === 1 ? "" : "s") + " · " + rows.length + " rows" + (liveCount ? " · " + liveCount + " live" : "");
    const frag = document.createDocumentFragment();
    for (const r of rows) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
  }

  /** @param {TrajRow} r */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.style.setProperty("--traj-depth", String(r.depth));

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const glyph = document.createElement("span");
    glyph.className = "traj-glyph";
    glyph.textContent = TRAJ_GLYPH[r.kind] || TRAJ_GLYPH.other;
    glyph.setAttribute("aria-hidden", "true");
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
    el.append(off, glyph, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));

    if (r.input || r.output) {
      el.classList.add("traj-expandable");
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-expanded", "false");
      const detail = document.createElement("div");
      detail.className = "traj-detail hidden";
      const section = (head, text) => {
        const h = document.createElement("div");
        h.className = "traj-detail-head";
        h.textContent = head;
        const pre = document.createElement("pre");
        pre.className = "traj-pre";
        pre.textContent = text;
        detail.append(h, pre);
      };
      if (r.input) section("input", r.input);
      if (r.output) section("output", r.output);
      el.appendChild(detail);
      const toggle = () => {
        const hidden = detail.classList.toggle("hidden");
        el.setAttribute("aria-expanded", hidden ? "false" : "true");
      };
      el.addEventListener("click", toggle);
      el.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          toggle();
        }
      });
    }
    return el;
  }

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
   * @type {Map<string, {sessionId: string, inFlightTurnId: any, workspaceRoot: string, status: string, pendingAsk: any}>}
   */
  const perProject = new Map();
  let currentProjectId = "";

  /** @param {string} projectId */
  function projectState(projectId) {
    let st = perProject.get(projectId);
    if (!st) {
      st = { sessionId: "", inFlightTurnId: null, workspaceRoot: "", status: "idle", pendingAsk: null };
      perProject.set(projectId, st);
    }
    return st;
  }

  /** @param {string} projectId */
  function forgetProjectState(projectId) {
    perProject.delete(projectId);
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
      if (projectId === currentProjectId) {
        toRenderer({
          type: "header",
          model: health.model || "",
          provider: health.provider || "",
          sessionId: st.sessionId,
        });
        toRenderer({ type: "ready" });
        await refreshSessionList(projectId);
      }
    } catch (err) {
      const message = String(err && err.message ? err.message : err);
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message });
      }
      st.status = "idle";
    }
    renderProjects();
  }

  /**
   * Read the session's log and hand it to the renderer — unless the user has
   * switched projects while the request was in flight, in which case the
   * answer belongs to a project that is no longer on screen. Same rule as the
   * session.get repaint above and refreshSessionList below.
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
      if (projectId !== currentProjectId) {
        return;
      }
      toRenderer({
        type: "trajectory",
        recorded: Boolean(res && res.recorded),
        events: res && Array.isArray(res.events) ? res.events : [],
      });
    } catch (err) {
      if (projectId === currentProjectId) {
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
        toRenderer({ type: "history", messages: view.ui_messages || [] });
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
        // Everything else belongs to a VS Code affordance this host does not
        // have (opening editors, applying pending diffs, the settings webview).
        // Say so once rather than swallowing the click.
        toRenderer({
          type: "systemNote",
          text: `"${msg.type}" is not available in the web UI yet.`,
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
    toRenderer({ type: "userEcho", text: msg.text || "" });
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
      session_id: st.sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
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
          toRenderer({ type: "history", messages: view.ui_messages || [] });
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
    // tee, so only these four are forwarded.
    if (
      msg.method === "agent/event" ||
      msg.method === "exec/output_chunk" ||
      msg.method === "workflow/stage_start" ||
      msg.method === "workflow/stage_done"
    ) {
      toRenderer({ type: "trajectoryEvent", event: { type: msg.method, data: msg.params || {} } });
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
          toRenderer({ type: "reasoningDelta", content: ev.content });
        }
        break;

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
        toRenderer({ type: "toolBlock", block: { ...block } });
        break;
      }

      case "tool_call_delta": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = blocks.get(ev.tool_call_id);
        if (block) {
          block.argsRaw += ev.args_delta || "";
          toRenderer({ type: "toolBlock", block: { ...block } });
        }
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
          startedAt: Date.now(),
        };
        block.status = "done";
        block.result = ev.content || "";
        block.durationMs = Date.now() - (block.startedAt || Date.now());
        blocks.delete(ev.tool_call_id);
        toRenderer({ type: "toolBlock", block: { ...block } });
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

      case "recoverable_error":
      case "error":
        toRenderer({ type: "error", message: ev.content || "error" });
        break;

      default:
        break;
    }
  }

  // A new turn starts with an empty transcript — for the project whose turn it
  // is, which is always the one the renderer is showing.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnTextByProject.set(currentProjectId, "");
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
      const opened = await openProject(entry.path, false);
      if (!opened) {
        return;
      }
    }
    ensureConn(projectId);
    await activateProject(projectId);
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
      };
    });
    toRenderer({ type: "projectList", projects: rows });

    const list = document.getElementById("project-rail-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    for (const row of rows) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "project-chip";
      chip.dataset.projectId = row.id;
      chip.dataset.state = row.state;
      chip.dataset.status = row.status;
      chip.dataset.active = row.active ? "true" : "false";
      chip.title = row.path + (row.status === "asking" ? " — waiting for you" : "");
      chip.setAttribute("aria-label", row.name + " (" + row.status + ")");
      chip.textContent = (row.name || "?").slice(0, 2);
      list.appendChild(chip);
    }
  }

  // ---- input -------------------------------------------------------------

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
    railMenu.style.left = x + "px";
    railMenu.style.top = y + "px";
    railMenu.hidden = false;
  }

  const railEl = document.getElementById("project-rail-list");
  if (railEl && railEl.addEventListener) {
    railEl.addEventListener("click", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      void switchProject(chip.dataset.projectId || "");
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

  const addBtn = document.getElementById("project-add-btn");
  if (addBtn && addBtn.addEventListener) {
    addBtn.addEventListener("click", () => {
      void addProject();
    });
  }

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
  async function addProject() {
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
    path = String(path || "").trim();
    if (!path) {
      return;
    }
    if (await openProject(path, false)) {
      return;
    }
    // The one recoverable refusal: the folder is not an Orchestra project yet.
    const agreed = window.confirm
      ? window.confirm(path + " is not an Orchestra project yet. Initialise it?")
      : false;
    if (agreed) {
      await openProject(path, true);
    }
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
    currentProjectId = first ? first.id : "";
    setActiveConn(ensureConn(currentProjectId));
    renderProjects();
  })();
})();
