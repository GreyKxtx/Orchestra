  const vscode = acquireVsCodeApi();

  /** @type {string} */
  let workspaceRoot = ".";
  /** @type {any[]} */
  let agents = [];
  /** @type {string[]} */
  let agentAvailableTools = [];
  /** @type {any[]} */
  let mcpServers = [];
  /** @type {any[]} */
  let skills = [];
  /** @type {number} */
  let graphUIPort = 6061;
  /** @type {any[]} */
  let providers = [];
  /** @type {string} */
  let selectedProviderKey = "";
  /** @type {string} */
  let selectedModelId = "";
  /** @type {string} */
  let activeProviderKey = "";
  /** @type {string} */
  let activeModelId = "";
  /** @type {string} */
  let modelSearchFilter = "";
  /** @type {any} */
  let orchestraConfig = null;
  /** @type {string} */
  let orchSharedProvider = "";
  /** @type {string | null} */
  let orchModalRoleKey = null;
  /** @type {string[]} */
  let orchModalSelection = [];
  /** @type {string} */
  let orchModalSearch = "";
  /** Minimum model context window in tokens; zero disables filtering. */
  let orchModalMinContext = 0;
  /** @type {boolean} */
  let apiKeyVisible = false;
  /** @type {{ version?: number, entries?: any[] }} */
  let mcpCatalog = { version: 1, entries: [] };
  /** @type {"browse" | "installed"} */
  let mcpTab = "browse";
  /** @type {string[]} */
  let mcpDraftTools = [];
  /** @type {boolean} */
  let mcpConfigureOpen = false;
  /** @type {boolean} */
  let mcpIsNewCustom = false;
  /** @type {boolean} */
  let mcpToolsLoading = false;
  /** @type {string} */
  let mcpCatalogFilter = "";
  /** @type {string} */
  let mcpCatalogCategory = "All";
  /** @type {string} */
  let mcpCatalogNextCursor = "";
  /** @type {string} */
  let mcpCatalogSource = "local";
  /** @type {string} */
  let mcpCatalogError = "";
  /** @type {boolean} */
  let mcpCatalogBusy = false;
  /** @type {boolean} */
  let mcpCatalogPrefetching = false;
  /** @type {number | null} */
  let mcpSearchTimer = null;
  /** @type {number} */
  let mcpCatalogPage = 0;
  const MCP_PAGE_SIZE = 20;

  const CATEGORY_ORDER = ["Local", "Cloud", "Gateway", "Other", "Named"];
  const MCP_CAT_ORDER = ["All", "Installable", "Featured", "Remote"];

  const errorEl = document.getElementById("error");

  function showError(text) {
    if (!errorEl) return;
    if (!text) {
      errorEl.classList.add("hidden");
      errorEl.textContent = "";
      return;
    }
    errorEl.textContent = text;
    errorEl.classList.remove("hidden");
  }

  function el(id) {
    return document.getElementById(id);
  }

  /** @returns {HTMLInputElement | null} */
  function input(id) {
    return /** @type {HTMLInputElement | null} */ (el(id));
  }

  /** @returns {HTMLTextAreaElement | null} */
  function area(id) {
    return /** @type {HTMLTextAreaElement | null} */ (el(id));
  }

  function navigateToSection(section) {
    if (!section) return;
    document.querySelectorAll(".nav-item").forEach((b) => {
      b.classList.toggle("active", b.getAttribute("data-section") === section);
    });
    document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
    el("sec-" + section)?.classList.add("active");
  }

  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.addEventListener("click", () => {
      const section = btn.getAttribute("data-section");
      if (!section) return;
      document.querySelectorAll(".nav-item").forEach((b) => b.classList.remove("active"));
      document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
      btn.classList.add("active");
      el("sec-" + section)?.classList.add("active");
      btn.scrollIntoView({ inline: "nearest", block: "nearest", behavior: "smooth" });
    });
  });

  el("backChat")?.addEventListener("click", () => {
    vscode.postMessage({ type: "backToChat" });
  });

  if (typeof window !== "undefined" && window.__ORCH_MCP_CATALOG) {
    mcpCatalog = window.__ORCH_MCP_CATALOG;
  }

  // ---- language -----------------------------------------------------------
  //
  // This panel is its own document, so it reads the choice itself rather than
  // waiting for a message: the VS Code host stamps window.__ORCH_LANG into the
  // webview's head (from the `orchestra.language` setting), and on the web
  // ui/web/src/settings/frame.js stamps it from localStorage. Empty means
  // "follow the environment", which pickLang() reads off navigator.language.

  /** What the picker shows when no language is forced. */
  const UI_LANG_AUTO = "";

  function savedUiLangSetting() {
    const raw = typeof window !== "undefined" ? window.__ORCH_LANG : "";
    return typeof raw === "string" ? raw : "";
  }

  function renderUiLanguageSelect() {
    const sel = /** @type {HTMLSelectElement | null} */ (el("uiLanguage"));
    if (!sel) return;
    const current = savedUiLangSetting();
    sel.innerHTML = "";
    const auto = document.createElement("option");
    auto.value = UI_LANG_AUTO;
    auto.textContent = i18n("set.lang.auto");
    sel.appendChild(auto);
    for (const lang of UI_LANGUAGES) {
      const opt = document.createElement("option");
      opt.value = lang.id;
      // A language names itself in its own language — a reader looking for
      // "Русский" should not have to find it under "Russian".
      opt.textContent = lang.label;
      sel.appendChild(opt);
    }
    sel.value = current;
  }

  /** Re-read the language and repaint everything already on screen. */
  function applyUiLanguage() {
    setUiLang(savedUiLangSetting());
    applyStaticI18n();
    renderUiLanguageSelect();
  }

  el("uiLanguage")?.addEventListener("change", () => {
    const sel = /** @type {HTMLSelectElement | null} */ (el("uiLanguage"));
    const lang = sel ? sel.value : "";
    if (typeof window !== "undefined") {
      window.__ORCH_LANG = lang;
    }
    // The host persists it — a VS Code setting, or this browser's storage —
    // and tells the chat window, which is a different document.
    vscode.postMessage({ type: "setLanguage", lang });
    applyUiLanguage();
    repaintTranslatedPanels();
  });
