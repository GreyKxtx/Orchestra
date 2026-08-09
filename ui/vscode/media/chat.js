//@ts-check
(function () {
  // @ts-ignore
  const vscode = acquireVsCodeApi();

  /** @typedef {{ id: string; label: string; icon: string; mode: string }} ModeOpt */
  /** @typedef {{ id: string; label: string; profile: string }} EffortOpt */

  /** @type {ModeOpt[]} */
  const MODES = [
    { id: "agent", label: "Agent", icon: "∞", mode: "build" },
    { id: "plan", label: "Plan", icon: "≡", mode: "plan" },
    { id: "debug", label: "Debug", icon: "⌁", mode: "explore" },
    { id: "multitask", label: "Multitask", icon: "◎", mode: "build" },
    { id: "ask", label: "Ask", icon: "◇", mode: "explore" },
  ];

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
  const effortBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("effort-btn"));
  const modeMenu = document.getElementById("mode-menu");
  const effortMenu = document.getElementById("effort-menu");
  const modeLabel = document.getElementById("mode-label");
  const effortLabel = document.getElementById("effort-label");
  const contextBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("context-btn"));
  const contextRingFill = document.getElementById("context-ring-fill");
  const contextPopover = document.getElementById("context-popover");
  const ctxPct = document.getElementById("ctx-pct");
  const ctxSummary = document.getElementById("ctx-summary");
  const ctxBar = document.getElementById("ctx-bar");
  const ctxRows = document.getElementById("ctx-rows");
  const attachBtn = document.getElementById("attach-btn");
  const filesEl = document.getElementById("chip-files");
  const fastToggle = /** @type {HTMLButtonElement | null} */ (document.getElementById("fast-toggle"));
  const composerWrap = document.getElementById("composer-wrap");
  const sessionTabsEl = document.getElementById("session-tabs");
  const sessionNewBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-new-btn"));
  const sessionHistoryBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-history-btn"));
  const settingsBtn = document.getElementById("settings-btn");
  const sessionMenu = document.getElementById("session-menu");
  const sessionMenuList = document.getElementById("session-menu-list");
  const modelLabelEl = document.getElementById("model-label");
  const modelPill = /** @type {HTMLButtonElement | null} */ (document.getElementById("model-pill"));
  const modelMenu = document.getElementById("model-menu");
  const modelMenuList = document.getElementById("model-menu-list");
  const applyToggle = /** @type {HTMLButtonElement | null} */ (document.getElementById("apply-toggle"));
  const applyLabel = document.getElementById("apply-label");
  const pendingBar = document.getElementById("pending-bar");
  const pendingLabel = document.getElementById("pending-label");
  const pendingReviewHint = document.getElementById("pending-review-hint");
  const pendingFilesEl = document.getElementById("pending-files");
  const pendingDiff = document.getElementById("pending-diff");
  const pendingApplyBtn = document.getElementById("pending-apply-btn");
  const pendingDiscardBtn = document.getElementById("pending-discard-btn");
  const pendingDiffBtn = document.getElementById("pending-diff-btn");
  const overlay = document.getElementById("overlay");
  const overlayTitle = document.getElementById("overlay-title");
  const overlayBody = document.getElementById("overlay-body");
  const overlayOptions = document.getElementById("overlay-options");
  const overlayInput = /** @type {HTMLInputElement | null} */ (document.getElementById("overlay-input"));
  const overlayActions = document.getElementById("overlay-actions");
  const paletteMenu = document.getElementById("palette-menu");
  const todosBar = document.getElementById("todos-bar");
  const todosList = document.getElementById("todos-list");
  const todosProgress = document.getElementById("todos-progress");
  const todosTitle = document.getElementById("todos-title");
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

  /** @type {Map<string, HTMLElement>} */
  const toolBlocks = new Map();
  /** @type {Map<string, string>} */
  const toolArgs = new Map();
  /** @type {Map<number, HTMLElement>} step → tool body pre */
  const execSteps = new Map();
  /** @type {{ id: string; content: string; status: string }[]} */
  let todos = [];
  let paletteMode = "";
  let paletteIndex = 0;
  /** @type {any[]} */
  let paletteItems = [];
  let mentionTimer = 0;
  /** @type {{ prompt: number; completion: number; limit: number; maxResponse: number; estimated: boolean }} */
  let ctxState = { prompt: 0, completion: 0, limit: 128000, maxResponse: 4096, estimated: false };
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

  const saved = vscode.getState() || {};
  let applyOn = saved.applyOn === true;
  let assistantBubble = null;
  let busy = false;
  let modeId = "agent";
  let effortId = "medium";
  let fastOn = false;
  let currentModel = "";
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

  function syncApplyUi() {
    if (applyToggle) {
      applyToggle.classList.toggle("on", applyOn);
      applyToggle.setAttribute("aria-checked", applyOn ? "true" : "false");
    }
    if (applyLabel) {
      applyLabel.textContent = applyOn ? "Apply" : "Dry-run";
    }
    vscode.setState({ ...(vscode.getState() || {}), applyOn });
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
    return String(text || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
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

  function findDiffForPath(filePath) {
    if (!filePath || !pendingState.diff.length) return null;
    const norm = filePath.replace(/\\/g, "/");
    return (
      pendingState.diff.find((d) => {
        const p = (d.path || "").replace(/\\/g, "/");
        return p === norm || p.endsWith("/" + norm) || norm.endsWith("/" + p) || basename(p) === basename(norm);
      }) || null
    );
  }

  function syncToolDiffStats() {
    for (const block of toolBlocks.values()) {
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff) continue;
      const stats = countDiffStats(diff.before, diff.after);
      const el = block.querySelector(".tool-stats");
      if (el) el.innerHTML = statsHtml(stats);
    }
  }

  function renderPendingBar() {
    const n = pendingState.diff.length || pendingState.ops.length;
    if (!pendingBar) return;
    if (!n || applyOn) {
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
    renderPendingFiles();
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
      vscode.postMessage({
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
    vscode.postMessage({ type: "openFile", path: filePath, focus: Boolean(focus) });
  }

  function openDiffMessage(path, before, after, focus) {
    vscode.postMessage({
      type: "openDiff",
      path,
      before: before || "",
      after: after || "",
      focus: Boolean(focus),
    });
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
    const idx = subagents.findIndex((s) => s.taskId === taskId);
    const row = {
      id: prev.parentToolCallId || taskId,
      taskId,
      type: prev.type,
      label: prev.label,
      status: prev.status,
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
    panel.innerHTML =
      `<div class="subagent-panel-head">` +
      `<span class="subagent-type">${escapeAttr(node.type || "agent")}</span>` +
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
      const label = (msg.content || "").trim();
      upsertSubagentTask(taskId, {
        parentToolCallId: msg.parentToolCallId,
        type: msg.subagentType || "agent",
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
      upsertSubagentTask(taskId, { status: st });
    }
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
    return messagesEl;
  }

  function basename(path) {
    if (!path) return "";
    const parts = path.replace(/\\/g, "/").split("/");
    return parts[parts.length - 1] || path;
  }

  function normalizePath(p) {
    return (p || "").replace(/\\/g, "/").toLowerCase();
  }

  function ensureDiffReviewFields() {
    pendingState.diff = pendingState.diff.map((d) => ({
      ...d,
      reviewStatus: d.reviewStatus || "",
    }));
  }

  function acceptedDiffPaths() {
    const set = new Set();
    for (const d of pendingState.diff) {
      if (d.reviewStatus !== "rejected" && d.path) {
        set.add(normalizePath(d.path));
      }
    }
    return set;
  }

  function filterOpsByPaths(ops, paths) {
    if (!Array.isArray(ops) || paths.size === 0) return [];
    return ops.filter((op) => {
      const p = op && typeof op.path === "string" ? normalizePath(op.path) : "";
      return p && paths.has(p);
    });
  }

  function diffReviewActive() {
    return pendingState.diff.length > 0 && !applyOn && document.activeElement !== inputEl;
  }

  function syncDiffReviewHint() {
    if (!pendingReviewHint) return;
    const show = pendingState.diff.length > 0 && !applyOn;
    pendingReviewHint.classList.toggle("hidden", !show);
  }

  function renderPendingFiles() {
    if (!pendingFilesEl) return;
    pendingFilesEl.innerHTML = "";
    if (!pendingState.diff.length) {
      pendingFilesEl.classList.remove("has-files");
      return;
    }
    pendingFilesEl.classList.add("has-files");
    pendingState.diff.forEach((d, idx) => {
      const stats = countDiffStats(d.before, d.after);
      const chip = document.createElement("button");
      chip.type = "button";
      let cls = "file-change-chip";
      if (idx === diffReviewCursor) cls += " selected";
      if (d.reviewStatus === "accepted") cls += " accepted";
      if (d.reviewStatus === "rejected") cls += " rejected";
      chip.className = cls;
      const statusMark =
        d.reviewStatus === "accepted" ? " ✓" : d.reviewStatus === "rejected" ? " ✗" : "";
      chip.innerHTML =
        `<span class="fcc-name">${escapeAttr(basename(d.path))}${statusMark}</span>` +
        `<span class="fcc-stats">` +
        (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
        (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
        `</span>`;
      chip.title =
        (d.path || "") +
        " — click: diff in editor · Shift+click: focus · Alt+click: open file";
      chip.addEventListener("click", (e) => {
        diffReviewCursor = idx;
        renderPendingFiles();
        if (!d.path) return;
        if (e.altKey) {
          openExternalFile(d.path, true);
          return;
        }
        openDiffMessage(d.path, d.before || "", d.after || "", e.shiftKey);
      });
      pendingFilesEl.appendChild(chip);
    });
    syncDiffReviewHint();
  }

  function renderPendingDiff() {
    if (!pendingDiff) return;
    pendingDiff.innerHTML = "";
    if (!pendingState.diff.length) {
      pendingDiff.classList.add("hidden");
      return;
    }
    pendingState.diff.forEach((d) => {
      const block = document.createElement("div");
      block.className = "diff-file";
      const head = document.createElement("button");
      head.type = "button";
      head.className = "diff-path";
      const stats = countDiffStats(d.before, d.after);
      head.innerHTML =
        `<span class="diff-path-name">${escapeAttr(d.path || "file")}</span>` +
        `<span class="fcc-stats">` +
        (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
        (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
        `</span>`;
      head.title = "Open diff in VS Code editor · Shift+click: focus";
      head.addEventListener("click", (e) => {
        if (!d.path) return;
        openDiffMessage(d.path, d.before || "", d.after || "", e.shiftKey);
      });
      block.appendChild(head);
      if (d.before || d.after) {
        void appendDiffLines(block, d.before, d.after, d.path);
      }
      pendingDiff.appendChild(block);
    });
  }

  async function appendDiffLines(container, before, after, filePath, maxLines = 32) {
    const wrap = document.createElement("div");
    wrap.className = "diff-lines";
    const lang = langFromPath(filePath || "");
    const bLines = (before || "").split("\n");
    const aLines = (after || "").split("\n");
    let shown = 0;
    const delLines = [];
    const addLines = [];
    for (const line of bLines) {
      if (shown >= maxLines) break;
      delLines.push(line);
      shown++;
    }
    for (const line of aLines) {
      if (shown >= maxLines) break;
      addLines.push(line);
      shown++;
    }
    const [delHtml, addHtml] = await Promise.all([
      requestHighlight(delLines, lang),
      requestHighlight(addLines, lang),
    ]);
    delHtml.forEach((html) => {
      const row = document.createElement("div");
      row.className = "diff-line del";
      row.innerHTML = "− " + html;
      wrap.appendChild(row);
    });
    addHtml.forEach((html) => {
      const row = document.createElement("div");
      row.className = "diff-line add";
      row.innerHTML = "+ " + html;
      wrap.appendChild(row);
    });
    const total = bLines.length + aLines.length;
    if (total > maxLines) {
      const more = document.createElement("div");
      more.className = "diff-more";
      more.textContent = `… ${total - maxLines} more lines`;
      wrap.appendChild(more);
    }
    container.appendChild(wrap);
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
        vscode.postMessage({
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
      vscode.postMessage({ type: "questionReply", answers: [] });
      return;
    }
    questionState = { questions, index: 0, answers: [], mode: "question" };
    renderQuestionStep();
  }

  function renderQuestionStep() {
    const q = questionState.questions[questionState.index];
    if (!q || !overlay || !overlayTitle || !overlayBody || !overlayOptions || !overlayActions) {
      vscode.postMessage({ type: "questionReply", answers: questionState.answers });
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

  function setChromeHint(text, isError) {
    if (!chromeHint) {
      return;
    }
    const t = (text || "").trim();
    if (!t) {
      chromeHint.textContent = "";
      chromeHint.classList.add("hidden");
      chromeHint.classList.remove("error");
      return;
    }
    chromeHint.textContent = t;
    chromeHint.classList.remove("hidden");
    chromeHint.classList.toggle("error", Boolean(isError));
  }

  function setBusy(next) {
    busy = next;
    if (sendBtn) {
      sendBtn.disabled = next;
    }
    if (inputEl) {
      inputEl.disabled = next;
    }
    if (contextBtn) {
      contextBtn.classList.toggle("busy", next);
    }
    if (todos.length) {
      renderTodos();
    }
    if (!next) {
      setChromeHint("", false);
      if (workflowActiveName) {
        setWorkflow("", false);
      }
    }
  }

  function closeMenus() {
    modeMenu?.classList.remove("open");
    effortMenu?.classList.remove("open");
    sessionMenu?.classList.remove("open");
    modelMenu?.classList.remove("open");
    modeBtn?.classList.remove("open");
    effortBtn?.classList.remove("open");
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
    return SLASH_CMDS.filter((c) => c.cmd.slice(1).startsWith(q));
  }

  function renderPalette(mode, items) {
    if (!paletteMenu) return;
    paletteMode = mode;
    paletteItems = items;
    paletteIndex = 0;
    paletteMenu.innerHTML = "";
    if (!items.length) {
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
        vscode.postMessage({ type: "slashCommand", cmd: "/compact" });
        return;
      }
      inputEl.value = trimmed;
      hidePalette();
      vscode.postMessage({ type: "slashCommand", cmd: item.cmd });
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
      vscode.postMessage({ type: "mentionSearch", query: hit.query });
    }, hit.query ? 120 : 180);
  }

  function renderTodos() {
    if (!todosBar || !todosList) return;
    if (!todos.length) {
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      if (todosProgress) todosProgress.textContent = "";
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
      if (done > 0) {
        todosBar.classList.remove("hidden");
        if (todosTitle) todosTitle.textContent = "Tasks";
        if (todosProgress) todosProgress.textContent = `${done}/${total} done`;
        todosList.innerHTML = "";
        const row = document.createElement("div");
        row.className = "todo-row status-done";
        row.innerHTML = '<span class="todo-glyph">✓</span><span class="todo-text">All tasks completed</span>';
        todosList.appendChild(row);
      } else {
        todosBar.classList.add("hidden");
      }
      return;
    }
    todosBar.classList.remove("hidden");
    if (todosTitle) todosTitle.textContent = busy ? "Working…" : "Tasks";
    if (todosProgress) todosProgress.textContent = `${done}/${total} done`;
    todosList.innerHTML = "";
    const sorted = [...open].sort((a, b) => {
      const rank = (s) => (s === "in_progress" ? 0 : 1);
      return rank(a.status) - rank(b.status);
    });
    sorted.slice(0, 10).forEach((t) => {
      const row = document.createElement("div");
      row.className = "todo-row status-" + t.status;
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
    if (open.length > 10) {
      const more = document.createElement("div");
      more.className = "todo-more";
      more.textContent = `+${open.length - 10} more`;
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

  function updateToolHead(block, name, argsRaw, content, running) {
    const head = block.querySelector(".tool-head");
    if (!head) return;
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
    if (statsEl && filePath) {
      const diff = findDiffForPath(filePath);
      if (diff) {
        statsEl.innerHTML = statsHtml(countDiffStats(diff.before, diff.after));
      }
    }
  }

  function insertFileChangeCard(path, content) {
    if (!messagesEl || !path) return;
    const diff = findDiffForPath(path);
    const stats = diff ? countDiffStats(diff.before, diff.after) : null;
    const card = document.createElement("button");
    card.type = "button";
    card.className = "file-change-card";
    const ext = path.split(".").pop() || "file";
    card.innerHTML =
      `<span class="fcc-ext">${escapeAttr(ext.slice(0, 4).toUpperCase())}</span>` +
      `<span class="fcc-body">` +
      `<span class="fcc-name">${escapeAttr(basename(path))}</span>` +
      `<span class="fcc-path">${escapeAttr(path)}</span>` +
      `</span>` +
      statsHtml(stats) +
      `<span class="fcc-action">Diff</span>`;
    card.title = "Open diff in VS Code · Shift+click: focus · Alt+click: open file";
    card.addEventListener("click", (e) => {
      if (e.altKey) {
        openExternalFile(path, true);
        return;
      }
      const d = findDiffForPath(path);
      openDiffMessage(path, d?.before || "", d?.after || "", e.shiftKey);
    });
    messagesEl.appendChild(card);
    messagesEl.scrollTop = messagesEl.scrollHeight;
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
      row.innerHTML =
        `<span class="subagent-icon">${icon}</span>` +
        `<span class="subagent-type">${escapeAttr(sa.type || "agent")}</span>` +
        `<span class="subagent-label">${escapeAttr(sa.label || sa.taskId || sa.id)}${toolHint}</span>`;
      if (sa.taskId) {
        row.dataset.taskId = sa.taskId;
        row.title = sa.taskId;
      }
      subagentsTree.appendChild(row);
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
      const desc = args.description || args.prompt || args.goal || "";
      if (st) sa.type = String(st);
      if (desc) {
        const d = String(desc).trim();
        sa.label = d.length > 48 ? d.slice(0, 45) + "…" : d;
      }
      if (phase === "complete") {
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
    if (ctxBar) {
      ctxBar.innerHTML = "";
      const rows = [
        { label: "Prompt", tokens: ctxState.prompt, color: "#888888" },
        { label: "Completion", tokens: ctxState.completion, color: "#a78bfa" },
        { label: "Reply budget", tokens: ctxState.maxResponse, color: "#555555" },
      ];
      const total = Math.max(used + ctxState.maxResponse, 1);
      rows.forEach((r) => {
        if (r.tokens <= 0 && r.label !== "Reply budget") return;
        const seg = document.createElement("div");
        seg.className = "ctx-bar-seg";
        seg.style.width = `${Math.max(2, (r.tokens / total) * 100)}%`;
        seg.style.background = r.color;
        ctxBar.appendChild(seg);
      });
    }
    if (ctxRows) {
      ctxRows.innerHTML = "";
      const items = [
        { label: "Prompt context", tokens: ctxState.prompt, color: "#888888" },
        { label: "Completion", tokens: ctxState.completion, color: "#a78bfa" },
        { label: "Reserved for reply", tokens: ctxState.maxResponse, color: "#555555" },
      ];
      items.forEach((item) => {
        const row = document.createElement("div");
        row.className = "ctx-row";
        row.innerHTML = `<span class="ctx-swatch" style="background:${item.color}"></span><span class="ctx-row-label">${item.label}</span><span class="ctx-row-val">${formatTok(item.tokens)}</span>`;
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
        vscode.postMessage({
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

  /** @param {string} role @param {string} text @param {{ uiIndex?: number; files?: any[] }} [opts] */
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
        rewind.addEventListener("click", () => {
          vscode.postMessage({ type: "rewindToMessage", uiIndex: opts.uiIndex });
        });
        wrap.appendChild(rewind);
      }
      if (text) {
        el.appendChild(wrap);
      }
    } else if (role === "system") {
      el.className = "msg system";
      el.textContent = text;
    } else {
      el.textContent = text;
    }
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  /** @param {{ phase: string; toolCallId?: string; toolName: string; content?: string; argsDelta?: string; step?: number; diagnostics?: any[] }} msg */
  function handleToolBlock(msg) {
    if (!messagesEl) return;
    const id = toolBlockKey(msg);
    const kind = toolKind(msg.toolName);
    const host = messagesHostFor(msg);

    if (msg.phase === "start") {
      if (msg.scope !== "child") {
        assistantBubble = null;
      }
      toolArgs.set(id, "");
      const block = document.createElement("div");
      block.className = `tool-block running kind-${kind}` + (msg.scope === "child" ? " child-tool" : "");
      block.dataset.toolId = id;
      if (msg.taskId) block.dataset.taskId = msg.taskId;
      if (typeof msg.step === "number") {
        block.dataset.step = String(msg.step);
        if (msg.scope !== "child") {
          execSteps.set(msg.step, block);
        }
      }
      const head = document.createElement("button");
      head.type = "button";
      head.className = "tool-head";
      head.innerHTML =
        `<span class="tool-icon">${toolIcon(msg.toolName)}</span>` +
        `<span class="tool-label">${escapeAttr(toolDisplayName(msg.toolName))}</span>` +
        `<span class="tool-sub"></span>` +
        `<span class="tool-stats"></span>` +
        `<span class="tool-spinner"></span>` +
        `<span class="tool-chev">▾</span>`;
      const body = document.createElement("pre");
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
      block.appendChild(head);
      block.appendChild(body);
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
      updateToolHead(block, msg.toolName, argsRaw, msg.content || "", false);
      if (head) head.classList.add("open");

      if (body && msg.content) {
        body.textContent = msg.content.length > 8000 ? msg.content.slice(0, 8000) + "\n…" : msg.content;
        body.classList.remove("hidden");
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
        const filePath = toolPathFromArgs(msg.toolName, argsRaw, msg.content || "");
        if (filePath) {
          block.dataset.filePath = filePath;
          insertFileChangeCard(filePath, msg.content || "");
        }
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

  function syncModeUi() {
    const m = currentMode();
    if (modeLabel) {
      modeLabel.textContent = m.label;
    }
    const icon = document.getElementById("mode-icon");
    if (icon) {
      icon.textContent = m.icon;
    }
    modeMenu?.querySelectorAll(".menu-item").forEach((el) => {
      const id = el.getAttribute("data-id");
      el.classList.toggle("selected", id === modeId);
    });
  }

  function syncEffortUi() {
    const e = currentEffort();
    if (effortLabel) {
      effortLabel.textContent = e.label;
    }
    effortMenu?.querySelectorAll("[data-effort]").forEach((el) => {
      const id = el.getAttribute("data-effort");
      el.classList.toggle("selected", id === effortId);
    });
    if (fastToggle) {
      fastToggle.classList.toggle("on", fastOn);
      fastToggle.setAttribute("aria-checked", fastOn ? "true" : "false");
    }
  }

  function ensureAssistant() {
    if (!assistantBubble) {
      assistantBubble = appendMsg("assistant", "");
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
    if (!inputEl || busy) {
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
      vscode.postMessage({ type: "slashCommand", cmd, arg });
      return;
    }
    const payload = {
      type: "send",
      text,
      mode: currentMode().mode,
      profile: effectiveProfile(),
      apply: applyOn,
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
    assistantBubble = null;
    toolBlocks.clear();
    toolArgs.clear();
    execSteps.clear();
    ctxState.prompt = 0;
    ctxState.completion = 0;
    renderContextUi();
    closeMenus();
    vscode.postMessage(payload);
  }

  function autoGrow() {
    if (!inputEl) {
      return;
    }
    inputEl.style.height = "auto";
    inputEl.style.height = Math.min(140, Math.max(44, inputEl.scrollHeight)) + "px";
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
    modelMenuList.innerHTML = "";
    const providers = Array.isArray(catalog.providers) ? catalog.providers : [];
    const activeModel = catalog.activeModel || currentModel;
    if (!providers.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = "No providers — open Settings";
      modelMenuList.appendChild(btn);
      return;
    }
    providers.forEach((p) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = p.name || p.key;
      modelMenuList.appendChild(head);
      const models = Array.isArray(p.models) ? p.models : [];
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = p.models_error || (p.ready ? "No models" : "Not configured");
        modelMenuList.appendChild(empty);
        return;
      }
      models.slice(0, 40).forEach((m) => {
        const id = m.id || "";
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item" + (id === activeModel && p.active ? " selected" : "");
        btn.setAttribute("data-model", id);
        btn.setAttribute("data-provider", p.key);
        btn.innerHTML = id;
        btn.title = id;
        modelMenuList.appendChild(btn);
      });
    });
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
      btn.innerHTML = id;
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
    closeMenus();
  });

  effortMenu?.querySelectorAll("[data-effort]").forEach((el) => {
    el.addEventListener("click", () => {
      const id = el.getAttribute("data-effort");
      if (!id) {
        return;
      }
      effortId = id;
      syncEffortUi();
      closeMenus();
    });
  });

  fastToggle?.addEventListener("click", (e) => {
    e.stopPropagation();
    fastOn = !fastOn;
    syncEffortUi();
  });

  attachBtn?.addEventListener("click", () => {
    vscode.postMessage({ type: "attach" });
  });

  sessionNewBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    vscode.postMessage({ type: "newSession" });
  });

  sessionHistoryBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !sessionMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      sessionMenu?.classList.add("open");
      sessionHistoryBtn.classList.add("open");
      vscode.postMessage({ type: "listSessions" });
    }
  });

  sessionTabsEl?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const closeEl = t.closest("[data-close-session]");
    if (closeEl) {
      e.stopPropagation();
      const sid = closeEl.getAttribute("data-close-session");
      if (sid) {
        vscode.postMessage({ type: "closeSession", sessionId: sid });
      }
      return;
    }
    const tab = t.closest(".session-tab[data-session-id]");
    if (!tab) {
      return;
    }
    const sid = tab.getAttribute("data-session-id");
    if (sid && sid !== activeSessionId) {
      vscode.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  applyToggle?.addEventListener("click", (e) => {
    e.stopPropagation();
    applyOn = !applyOn;
    syncApplyUi();
    renderPendingBar();
  });

  pendingApplyBtn?.addEventListener("click", () => {
    const paths = acceptedDiffPaths();
    const filtered =
      pendingState.diff.length > 0 ? filterOpsByPaths(pendingState.ops, paths) : pendingState.ops;
    if (pendingState.diff.length && filtered.length === 0) {
      appendMsg("system", "No accepted files to apply");
      return;
    }
    vscode.postMessage({
      type: "applyPending",
      ops: filtered.length ? filtered : undefined,
    });
  });
  pendingDiscardBtn?.addEventListener("click", () => {
    pendingState = { ops: [], diff: [] };
    renderPendingBar();
    renderPendingDiff();
    vscode.postMessage({ type: "discardPending" });
  });
  pendingDiffBtn?.addEventListener("click", () => {
    if (!pendingDiff) return;
    const willShow = pendingDiff.classList.contains("hidden");
    if (willShow) {
      renderPendingDiff();
      pendingDiff.classList.remove("hidden");
    } else {
      pendingDiff.classList.add("hidden");
    }
  });

  diffViewerCloseBtn?.addEventListener("click", hideDiffViewer);
  diffViewerEditorBtn?.addEventListener("click", () => {
    if (!diffViewerState.path) return;
    openDiffMessage(diffViewerState.path, diffViewerState.before, diffViewerState.after, true);
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
            vscode.postMessage({
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
    if (open) {
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      vscode.postMessage({ type: "listProviderModels" });
    }
  });

  settingsBtn?.addEventListener("click", () => {
    vscode.postMessage({ type: "openSettings" });
  });

  sessionMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest("[data-session-action], [data-session-id]");
    if (!item) {
      return;
    }
    const action = item.getAttribute("data-session-action");
    if (action === "new") {
      closeMenus();
      vscode.postMessage({ type: "newSession" });
      return;
    }
    const sid = item.getAttribute("data-session-id");
    if (sid) {
      closeMenus();
      vscode.postMessage({ type: "openSession", sessionId: sid });
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
      vscode.postMessage({ type: "listProviderModels" });
      return;
    }
    const model = item.getAttribute("data-model");
    const provider = item.getAttribute("data-provider") || undefined;
    if (model) {
      closeMenus();
      vscode.postMessage({ type: "setModel", model, provider });
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
          setChromeHint(msg.detail || "connecting…", false);
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
      case "header":
        activeSessionId = msg.sessionId || activeSessionId;
        updateActiveTabTitle(msg.title || "New chat");
        setModelLabel(msg.model || "");
        if (modelLabelEl && msg.provider) {
          modelLabelEl.title = `${msg.provider} · ${msg.model || ""}`;
        }
        setBusy(false);
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
        sessions.slice(0, 20).forEach((s) => {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.className = "menu-item";
          btn.setAttribute("data-session-id", s.id);
          btn.textContent = s.title || s.id;
          if (s.model) {
            btn.title = s.model;
          }
          sessionMenuList.appendChild(btn);
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
      case "pendingOps": {
        const payload = msg.payload || {};
        pendingState = {
          ops: Array.isArray(payload.ops) ? payload.ops : [],
          diff: Array.isArray(payload.diff) ? payload.diff : [],
        };
        ensureDiffReviewFields();
        diffReviewCursor = 0;
        renderPendingBar();
        syncToolDiffStats();
        break;
      }
      case "pendingCleared":
        pendingState = { ops: [], diff: [] };
        diffReviewCursor = 0;
        renderPendingBar();
        syncToolDiffStats();
        if (pendingDiff) pendingDiff.classList.add("hidden");
        break;
      case "permissionRequest":
        showPermissionOverlay(msg.request || {});
        break;
      case "questionAsk":
        showQuestionOverlay(Array.isArray(msg.questions) ? msg.questions : []);
        break;
      case "clearMessages":
        if (messagesEl) {
          messagesEl.innerHTML = "";
        }
        assistantBubble = null;
        toolBlocks.clear();
        toolArgs.clear();
        execSteps.clear();
        todos = [];
        subagents = [];
        subagentByTask.clear();
        renderSubagents();
        renderTodos();
        setWorkflow("", false);
        break;
      case "history": {
        const list = Array.isArray(msg.messages) ? msg.messages : [];
        list.forEach((m) => {
          const role = m.role === "user" ? "user" : m.role === "system" ? "error" : "assistant";
          const opts = {
            uiIndex: typeof m.uiIndex === "number" ? m.uiIndex : undefined,
            files: Array.isArray(m.files) ? m.files : undefined,
          };
          appendMsg(role, m.text || "", opts);
        });
        break;
      }
      case "userEcho":
        appendMsg("user", msg.text, {
          uiIndex: typeof msg.uiIndex === "number" ? msg.uiIndex : undefined,
          files: Array.isArray(msg.files) ? msg.files : undefined,
        });
        break;
      case "delta": {
        const bubble = ensureAssistant();
        if (bubble && typeof msg.content === "string") {
          bubble.textContent = (bubble.textContent || "") + msg.content;
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
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
        const prompt = typeof u.prompt_tokens === "number" ? u.prompt_tokens : 0;
        const completion = typeof u.completion_tokens === "number" ? u.completion_tokens : 0;
        if (prompt > 0 || completion > 0) {
          ctxState.prompt = prompt;
          ctxState.completion = completion;
          ctxState.estimated = u.source === "estimate";
          renderContextUi();
        }
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
      case "mentionResults":
        renderPalette("mention", Array.isArray(msg.files) ? msg.files : []);
        break;
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
        setBusy(false);
        break;
      case "turnComplete":
        setBusy(false);
        assistantBubble = null;
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

  document.addEventListener("keydown", (e) => {
    if (!diffReviewActive()) return;
    const n = pendingState.diff.length;
    if (!n) return;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      diffReviewCursor = Math.max(0, diffReviewCursor - 1);
      renderPendingFiles();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      diffReviewCursor = Math.min(n - 1, diffReviewCursor + 1);
      renderPendingFiles();
      return;
    }
    if (e.key === "a" || e.key === "A") {
      e.preventDefault();
      pendingState.diff[diffReviewCursor].reviewStatus = "accepted";
      renderPendingFiles();
      return;
    }
    if (e.key === "x" || e.key === "X") {
      e.preventDefault();
      pendingState.diff[diffReviewCursor].reviewStatus = "rejected";
      renderPendingFiles();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      pendingApplyBtn?.click();
    }
  });

  syncModeUi();
  syncEffortUi();
  syncApplyUi();
  renderContextUi();
  autoGrow();
  vscode.postMessage({ type: "ready" });
})();
