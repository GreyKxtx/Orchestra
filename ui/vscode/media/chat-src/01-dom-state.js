  const vscode = acquireVsCodeApi();

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

  const saved = vscode.getState() || {};
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

