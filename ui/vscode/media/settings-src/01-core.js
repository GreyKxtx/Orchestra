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
