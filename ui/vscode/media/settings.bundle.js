/* AUTO-GENERATED — do not edit. Sources: media/settings-src/*.js  →  npm run bundle:webview */
//@ts-check
/* Generated from media/settings-src — edit fragments there, then: npm run bundle:webview */
(function () {
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
  /** Catalog keys with bundled SVG logos under media/provider-icons/. */
  const PROVIDER_ICON_FILES = new Set([
    "",
    "lmstudio",
    "ollama",
    "vllm",
    "openrouter",
    "openai",
    "anthropic",
    "google",
    "mistral",
    "deepseek",
    "xai",
    "moonshot",
    "groq",
    "together",
    "fireworks",
    "cerebras",
    "custom",
  ]);

  /** @param {string} key */
  function providerIconFile(key) {
    const k = (key || "").toLowerCase();
    if (k === "") return "default";
    if (PROVIDER_ICON_FILES.has(k)) return k;
    return "";
  }

  /** @param {string} key */
  function providerLogoHtml(key) {
    const k = (key || "").toLowerCase();
    const file = providerIconFile(k);
    const base =
      typeof window !== "undefined" && window.__ORCH_ICON_BASE
        ? String(window.__ORCH_ICON_BASE)
        : "";
    if (base && file) {
      const ver =
        typeof window !== "undefined" && window.__ORCH_ICON_V
          ? String(window.__ORCH_ICON_V)
          : "";
      const src = base.replace(/\/?$/, "/") + file + ".svg" + (ver ? "?v=" + encodeURIComponent(ver) : "");
      return `<img class="prov-logo-img" src="${src}" alt="" width="22" height="22" loading="lazy" decoding="async" />`;
    }
    if (k) {
      const letter = k.slice(0, 1).toUpperCase();
      return `<svg viewBox="0 0 24 24" aria-hidden="true"><rect width="24" height="24" rx="5" fill="#333"/><text x="12" y="16" text-anchor="middle" fill="#ccc" font-size="11" font-family="Segoe UI,sans-serif" font-weight="600">${letter}</text></svg>`;
    }
    return `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke="#aaa" stroke-width="1.5" fill="none"/><path d="M8 12h8M12 8v8" stroke="#ccc" stroke-width="1.5" stroke-linecap="round"/></svg>`;
  }
  const MAX_ORCH_MODELS = 3;

  const ORCH_SLOT_LABELS = ["Primary", "Fallback 2", "Fallback 3"];

  /** @type {HTMLElement | null} */
  let openProvDropdown = null;

  function closeProvDropdowns() {
    if (openProvDropdown) {
      openProvDropdown.classList.remove("open");
      openProvDropdown = null;
    }
  }

  document.addEventListener("click", closeProvDropdowns);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeProvDropdowns();
  });

  /** @param {number | undefined} n */
  function formatContextTokens(n) {
    if (!n || n <= 0) return "";
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return (m >= 10 ? Math.round(m) : Math.round(m * 10) / 10) + "M ctx";
    }
    if (n >= 1000) {
      const k = n / 1000;
      return (k >= 100 ? Math.round(k) : Math.round(k * 10) / 10) + "k ctx";
    }
    return n + " ctx";
  }

  /** @param {any} p */
  function providerBadgeMeta(p) {
    if (!p) return { text: "—", className: "disabled" };
    if (p.models_error) return { text: "error", className: "error", title: p.models_error };
    if (p.active) return { text: "active", className: "running" };
    if (p.ready && p.model_count > 0) {
      return { text: `${p.model_count} models`, className: "ok" };
    }
    if (p.ready) return { text: "ready", className: "ready" };
    if (p.needs_key && !p.api_key_set) return { text: "needs key", className: "disabled" };
    if (p.custom && !p.api_base) return { text: "needs URL", className: "disabled" };
    return { text: "—", className: "disabled" };
  }

  /** @param {string} key @param {string} [extraClass] */
  function makeProviderIconEl(key, extraClass) {
    const span = document.createElement("span");
    span.className = "prov-icon" + (extraClass ? " " + extraClass : "");
    span.innerHTML = providerLogoHtml(key);
    span.title = key || "global";
    span.setAttribute("aria-hidden", "true");
    return span;
  }

  /** @param {HTMLElement} iconEl @param {string} key */
  function setProviderIconEl(iconEl, key) {
    iconEl.innerHTML = providerLogoHtml(key);
    iconEl.title = key || "global";
  }

  /** @param {any} p */
  function appendProviderBadge(parent, p) {
    const meta = providerBadgeMeta(p);
    const badge = document.createElement("span");
    badge.className = "badge " + meta.className;
    badge.textContent = meta.text;
    if (meta.title) badge.title = meta.title;
    parent.appendChild(badge);
  }

  /**
   * Custom provider picker with logos (replaces native select in orchestra).
   * @param {string} value
   * @param {(key: string) => void} onChange
   * @param {{ key: string, label: string }[]} options
   */
  function buildProviderDropdown(value, onChange, options) {
    const root = document.createElement("div");
    root.className = "prov-dropdown";

    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "prov-dropdown-btn";

    const btnIcon = makeProviderIconEl(value, "prov-dropdown-icon");
    const btnLabel = document.createElement("span");
    btnLabel.className = "prov-dropdown-label";
    const btnChevron = document.createElement("span");
    btnChevron.className = "prov-dropdown-chevron";
    btnChevron.textContent = "▾";

    btn.appendChild(btnIcon);
    btn.appendChild(btnLabel);
    btn.appendChild(btnChevron);

    const menu = document.createElement("div");
    menu.className = "prov-dropdown-menu";
    menu.setAttribute("role", "listbox");

    /** @param {string} key */
    function labelFor(key) {
      const hit = options.find((o) => o.key === key);
      return hit ? hit.label : key || "Main (global llm)";
    }

    /** @param {string} key */
    function syncButton(key) {
      setProviderIconEl(btnIcon, key);
      btnLabel.textContent = labelFor(key);
      menu.querySelectorAll(".prov-dropdown-item").forEach((item) => {
        item.classList.toggle("selected", item.getAttribute("data-key") === key);
      });
    }

    options.forEach((o) => {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "prov-dropdown-item" + (o.key === value ? " selected" : "");
      item.setAttribute("data-key", o.key);
      item.setAttribute("role", "option");
      item.appendChild(makeProviderIconEl(o.key, "prov-dropdown-icon"));
      const lab = document.createElement("span");
      lab.className = "prov-dropdown-label";
      lab.textContent = o.label;
      item.appendChild(lab);
      item.addEventListener("click", (e) => {
        e.stopPropagation();
        syncButton(o.key);
        onChange(o.key);
        closeProvDropdowns();
      });
      menu.appendChild(item);
    });

    syncButton(value || "");

    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const wasOpen = root.classList.contains("open");
      closeProvDropdowns();
      if (!wasOpen) {
        root.classList.add("open");
        openProvDropdown = root;
      }
    });

    root.appendChild(btn);
    root.appendChild(menu);
    return root;
  }
  function modelsPayload() {
    return {
      provider: selectedProviderKey || input("provider")?.value || "",
      apiBase: input("apiBase")?.value || "",
      apiKey: input("apiKey")?.value || "",
      model: selectedModelId || input("model")?.value || "",
      promptFamily: input("promptFamily")?.value || "",
      temperature: input("temperature") ? Number(input("temperature").value) : undefined,
      maxTokens: input("maxTokens") ? Number(input("maxTokens").value) : undefined,
      timeoutS: input("timeoutS") ? Number(input("timeoutS").value) : undefined,
      multimodal: input("multimodal")?.checked,
    };
  }

  /** @param {string} key */
  function findProvider(key) {
    return providers.find((p) => p.key === key);
  }

  function syncHiddenFields() {
    if (input("provider")) input("provider").value = selectedProviderKey;
    if (input("model")) input("model").value = selectedModelId;
  }

  function applyApiKeyVisibility() {
    const field = input("apiKey");
    const btn = el("toggleApiKey");
    if (!field) return;
    field.type = apiKeyVisible ? "text" : "password";
    if (btn) btn.textContent = apiKeyVisible ? "Hide" : "Show";
  }

  /** @param {any} p @param {{ force?: boolean }} [opts] */
  function populateProviderCreds(p, opts) {
    if (!p) return;
    const force = Boolean(opts?.force);
    if (input("apiBase")) {
      const base = p.api_base || "";
      if (force || p.custom || !input("apiBase").value) {
        input("apiBase").value = base;
      }
    }
    if (input("apiKey")) {
      const key = typeof p.api_key === "string" ? p.api_key : "";
      if (force || key) {
        input("apiKey").value = key;
      }
    }
    applyApiKeyVisibility();
  }

  function updateCredBlock() {
    const cred = el("providerCreds");
    const p = findProvider(selectedProviderKey);
    if (!cred || !p) {
      cred?.classList.add("hidden");
      return;
    }
    const show = p.custom || p.needs_key || p.named || p.category === "Local";
    cred.classList.toggle("hidden", !show);
    if (input("apiBase") && (!input("apiBase").value || p.custom)) {
      input("apiBase").value = p.api_base || "";
    }
    populateProviderCreds(p);
    const keyHint = el("keyHint");
    if (keyHint) {
      if (p.api_key_set && p.api_key) {
        keyHint.textContent = "Saved key loaded — edit and Save to update";
      } else if (p.api_key_set) {
        keyHint.textContent = "Key saved — click Show to view";
      } else if (p.needs_key) {
        keyHint.textContent = "API key required";
      } else {
        keyHint.textContent = "No API key needed";
      }
    }
    const status = el("providerStatus");
    if (status) {
      if (p.active) {
        status.textContent = "Active provider";
      } else if (p.ready) {
        status.textContent = "Configured — models loaded when available";
      } else if (p.needs_key && !p.api_key_set) {
        status.textContent = "Enter API key and save to enable";
      } else if (p.custom && !p.api_base) {
        status.textContent = "Enter API base URL for custom provider";
      } else {
        status.textContent = "Not configured";
      }
    }
  }

  function renderProviders() {
    const list = el("providerList");
    if (!list) return;
    list.innerHTML = "";
    if (!providers.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = "Loading providers…";
      list.appendChild(empty);
      return;
    }

    /** @type {Record<string, any[]>} */
    const groups = {};
    providers.forEach((p) => {
      const cat = p.category || "Other";
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(p);
    });

    CATEGORY_ORDER.forEach((cat) => {
      const items = groups[cat];
      if (!items || !items.length) return;
      const head = document.createElement("div");
      head.className = "pick-group-label";
      head.textContent = cat;
      list.appendChild(head);
      items.forEach((p) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "list-item pick-item" + (p.key === selectedProviderKey ? " selected" : "");
        btn.appendChild(makeProviderIconEl(p.key));
        const title = document.createElement("span");
        title.className = "pick-title";
        title.textContent = p.name || p.key;
        btn.appendChild(title);
        appendProviderBadge(btn, p);
        btn.addEventListener("click", () => {
          selectedProviderKey = p.key;
          apiKeyVisible = false;
          populateProviderCreds(p, { force: true });
          if (p.active && p.current_model) {
            selectedModelId = p.current_model;
          } else if (p.models && p.models.length) {
            selectedModelId = p.models[0].id || "";
          }
          syncHiddenFields();
          renderProviders();
          renderModels();
          updateCredBlock();
        });
        list.appendChild(btn);
      });
    });
    updateCredBlock();
  }

  function renderModels() {
    const list = el("modelList");
    const status = el("modelsStatus");
    const p = findProvider(selectedProviderKey);
    if (!list) return;
    list.innerHTML = "";
    if (!p) {
      if (status) status.textContent = "Select a provider";
      return;
    }
    if (p.models_error) {
      if (status) status.textContent = `Failed to load models: ${p.models_error}`;
    } else if (!p.ready) {
      if (status) status.textContent = "Configure credentials and save, then refresh";
    } else if (!p.models || !p.models.length) {
      if (status) status.textContent = "No models returned — try Refresh";
    } else {
      if (status) status.textContent = `${p.models.length} models from ${p.name || p.key}`;
    }
    const models = p.models || [];
    const q = (modelSearchFilter || "").trim().toLowerCase();
    const filtered = q ? models.filter((m) => (m.id || "").toLowerCase().includes(q)) : models;
    if (!filtered.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = q ? `No models match “${modelSearchFilter}”` : p.ready ? "No models listed" : "Provider not ready";
      list.appendChild(empty);
      return;
    }
    if (status && filtered.length !== models.length) {
      status.textContent = `${filtered.length} / ${models.length} models (filtered)`;
    }
    filtered.forEach((m) => {
      const id = m.id || "";
      const btn = document.createElement("button");
      btn.type = "button";
      const isActive = id === activeModelId && p.key === activeProviderKey;
      const isSelected = id === selectedModelId;
      btn.className = "list-item pick-item" + (isSelected ? " selected" : "");
      const title = document.createElement("span");
      title.className = "pick-title mono";
      title.textContent = id;
      btn.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge" + (isActive ? " running" : " ok");
      const ctx = formatContextTokens(m.context_tokens);
      if (isActive) badge.textContent = ctx ? `active · ${ctx}` : "active";
      else if (ctx) badge.textContent = ctx;
      else badge.textContent = m.owned_by || "";
      btn.appendChild(badge);
      btn.addEventListener("click", () => {
        selectedModelId = id;
        syncHiddenFields();
        renderModels();
      });
      list.appendChild(btn);
    });
  }

  /** @param {any} catalog @param {boolean} [resetSelection] */
  function applyProviderCatalog(catalog, resetSelection) {
    if (!catalog) return;
    providers = catalog.providers || [];
    activeProviderKey = catalog.activeProvider || catalog.active_provider || "";
    activeModelId = catalog.activeModel || catalog.active_model || "";
    if (resetSelection || !selectedProviderKey) {
      selectedProviderKey = activeProviderKey || (providers[0] && providers[0].key) || "";
      selectedModelId = activeModelId || "";
    }
    const cur = findProvider(selectedProviderKey);
    if (cur && cur.active && cur.current_model) {
      selectedModelId = cur.current_model;
    } else if (resetSelection && activeModelId) {
      selectedModelId = activeModelId;
    }
    syncHiddenFields();
    renderProviders();
    renderModels();
    if (cur) populateProviderCreds(cur, { force: true });
  }

  el("saveGeneral")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({
      type: "saveGeneral",
      binaryPath: input("binaryPath")?.value || "",
      projectRoot: input("projectRoot")?.value || "",
    });
  });

  el("saveModels")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "saveModels", ...modelsPayload() });
  });

  el("refreshModels")?.addEventListener("click", () => {
    showError("");
    const status = el("modelsStatus");
    if (status) status.textContent = "Refreshing models…";
    vscode.postMessage({
      type: "refreshModels",
      provider: selectedProviderKey || "",
      apiBase: input("apiBase")?.value || "",
      apiKey: input("apiKey")?.value || "",
    });
  });

  input("modelSearch")?.addEventListener("input", () => {
    modelSearchFilter = input("modelSearch")?.value || "";
    renderModels();
  });

  el("toggleApiKey")?.addEventListener("click", () => {
    apiKeyVisible = !apiKeyVisible;
    applyApiKeyVisibility();
  });
  function orchProviderOptions() {
    const ready = providers.filter((p) => p.ready || p.configured || p.active);
    const opts = [{ key: "", label: "Main (global llm)" }];
    ready.forEach((p) => opts.push({ key: p.key, label: p.name || p.key }));
    return opts;
  }

  /** @param {string} value @param {(key: string) => void} onChange */
  function buildOrchProviderSelect(value, onChange) {
    return buildProviderDropdown(value, onChange, orchProviderOptions());
  }

  /** @param {string[]} models */
  function renderOrchPickLabel(models) {
    if (!models.length) return "Pick models…";
    if (models.length === 1) return models[0];
    const first = models[0];
    const rest = models.length - 1;
    return `${first} +${rest}`;
  }

  /** @param {string[]} models @param {HTMLElement} host */
  function renderOrchPickChips(models, host) {
    host.innerHTML = "";
    if (!models.length) {
      host.textContent = "Pick models…";
      host.classList.remove("has-models");
      return;
    }
    host.classList.add("has-models");
    models.forEach((id, i) => {
      const chip = document.createElement("span");
      chip.className = "orch-chip";
      const slot = document.createElement("span");
      slot.className = "orch-chip-slot";
      slot.textContent = String(i + 1);
      chip.appendChild(slot);
      const label = document.createElement("span");
      label.className = "orch-chip-label";
      label.textContent = id;
      label.title = id;
      chip.appendChild(label);
      host.appendChild(chip);
    });
  }

  function orchRoleByKey(key) {
    const roles = (orchestraConfig && orchestraConfig.roles) || [];
    return roles.find((r) => r.key === key);
  }

  /** Canonical L-tier for a role: core-provided or legacy_map defaults. */
  /** @param {any} role */
  function orchRoleTier(role) {
    if (role && role.tier) return String(role.tier);
    const defaults = { planner: "L5", complex: "L3", focused: "L3", micro: "L1" };
    return defaults[role && role.key] || "";
  }

  /** Effective provider key for a role (never unrelated fallbacks). */
  /** @param {any} role */
  function effectiveRoleProvider(role) {
    if (!role) return orchSharedProvider || orchestraConfig?.mainProvider || activeProviderKey || "";
    if (role.provider !== undefined && role.provider !== null) {
      const explicit = String(role.provider).trim();
      if (explicit === "") {
        return orchestraConfig?.mainProvider || activeProviderKey || "";
      }
      return explicit;
    }
    if (orchSharedProvider) return orchSharedProvider;
    return orchestraConfig?.mainProvider || activeProviderKey || "";
  }

  /** @param {string} provKey */
  function providerModelIds(provKey) {
    const key = (provKey || "").trim();
    const p = key ? findProvider(key) : findProvider(orchestraConfig?.mainProvider || activeProviderKey || "");
    if (!p || !Array.isArray(p.models)) return [];
    return p.models.map((m) => m.id || "").filter(Boolean);
  }

  /** Drop models that are not offered by the role's provider. */
  /** @param {any} role */
  function sanitizeRoleModels(role) {
    if (!role) return;
    const allowed = new Set(providerModelIds(effectiveRoleProvider(role)));
    let models =
      role.models && role.models.length ? role.models.slice() : role.model ? [role.model] : [];
    if (allowed.size > 0) {
      models = models.filter((id) => allowed.has(id));
    } else if (models.length) {
      models = [];
    }
    models = models.slice(0, MAX_ORCH_MODELS);
    role.models = models;
    role.model = models[0] || "";
  }

  function sanitizeOrchestraRoles() {
    if (!orchestraConfig?.roles) return;
    orchestraConfig.roles.forEach((role) => sanitizeRoleModels(role));
  }

  /** @param {any} role @param {string} provKey */
  function setRoleProvider(role, provKey) {
    role.provider = provKey;
    role.models = [];
    role.model = "";
  }

  function roleModelsList(role) {
    if (role.models && role.models.length) return role.models.slice();
    if (role.model) return [role.model];
    return [];
  }

  function renderOrchestra() {
    const host = el("orchRoles");
    const sharedHost = el("orchSharedProviderWrap");
    if (!host || !orchestraConfig) return;

    if (sharedHost) {
      sharedHost.innerHTML = "";
      const sharedSel = buildOrchProviderSelect(orchSharedProvider, (key) => {
        orchSharedProvider = key;
        if (!orchestraConfig) return;
        orchestraConfig.roles.forEach((r) => {
          setRoleProvider(r, key);
        });
        renderOrchestra();
      });
      sharedHost.appendChild(sharedSel);
    }

    host.innerHTML = "";
    (orchestraConfig.roles || []).forEach((role) => {
      const row = document.createElement("div");
      row.className = "orch-row";
      const title = document.createElement("div");
      title.className = "orch-row-title";
      const tier = orchRoleTier(role);
      if (tier) {
        const badge = document.createElement("span");
        badge.className = "orch-tier orch-tier-" + tier.toLowerCase();
        badge.textContent = tier;
        badge.title = "Orchestra tier " + tier + " (см. orchestra-routing §1)";
        title.appendChild(badge);
      }
      const name = document.createElement("span");
      name.className = "orch-role-name";
      name.textContent = role.label || role.key;
      title.appendChild(name);
      const provValue =
        role.provider !== undefined && role.provider !== null ? role.provider : orchSharedProvider || "";
      const provWrap = buildOrchProviderSelect(provValue, (key) => {
        setRoleProvider(role, key);
        renderOrchestra();
      });
      const models = roleModelsList(role);
      const pick = document.createElement("button");
      pick.type = "button";
      pick.className = "orch-pick" + (models.length ? " has-models" : "");
      pick.title = models.length ? models.map((m, i) => `${i + 1}. ${m}`).join("\n") : "Pick up to 3 models (failover order)";
      const pickInner = document.createElement("span");
      pickInner.className = "orch-pick-inner";
      renderOrchPickChips(models, pickInner);
      pick.appendChild(pickInner);
      pick.addEventListener("click", () => openOrchModal(role.key));
      row.appendChild(title);
      row.appendChild(provWrap);
      row.appendChild(pick);
      host.appendChild(row);
    });

    const tier = el("orchDefaultTier");
    if (tier) tier.value = orchestraConfig.defaultTier || "focused";
    const verify = /** @type {HTMLInputElement | null} */ (el("orchVerifyEnabled"));
    if (verify) verify.checked = orchestraConfig.workerVerifyEnabled !== false;
    const llmV = /** @type {HTMLInputElement | null} */ (el("orchLLMVerify"));
    if (llmV) llmV.checked = Boolean(orchestraConfig.workerLLMVerifyEnabled);
    const maxR = input("orchMaxRetries");
    if (maxR) maxR.value = String(orchestraConfig.maxWorkerRetries ?? 3);
    const maxVR = input("orchMaxVerifyRetries");
    if (maxVR) maxVR.value = String(orchestraConfig.maxWorkerVerifyRetries ?? 1);
  }

  function renderOrchModalSlots() {
    const slots = el("orchModalSlots");
    if (!slots) return;
    slots.innerHTML = "";
    for (let i = 0; i < MAX_ORCH_MODELS; i++) {
      const slot = document.createElement("div");
      slot.className = "orch-slot" + (orchModalSelection[i] ? " filled" : "");
      const label = document.createElement("span");
      label.className = "orch-slot-label";
      label.textContent = ORCH_SLOT_LABELS[i] || `Slot ${i + 1}`;
      slot.appendChild(label);
      const val = document.createElement("span");
      val.className = "orch-slot-val";
      val.textContent = orchModalSelection[i] || "—";
      val.title = orchModalSelection[i] || "";
      slot.appendChild(val);
      slots.appendChild(slot);
    }
    const hint = el("orchModalHint");
    if (hint) {
      hint.textContent =
        orchModalSelection.length >= MAX_ORCH_MODELS
          ? "Maximum 3 models — click a selected row to remove"
          : `Select up to ${MAX_ORCH_MODELS} models in failover order (primary first)`;
    }
  }

  function toggleOrchModel(id) {
    const idx = orchModalSelection.indexOf(id);
    if (idx >= 0) {
      orchModalSelection.splice(idx, 1);
    } else if (orchModalSelection.length < MAX_ORCH_MODELS) {
      orchModalSelection.push(id);
    }
    renderOrchModalSlots();
    renderOrchModalList();
  }

  function openOrchModal(roleKey) {
    const role = orchRoleByKey(roleKey);
    if (!role) return;
    sanitizeRoleModels(role);
    orchModalRoleKey = roleKey;
    const existing = roleModelsList(role);
    orchModalSelection = existing.slice(0, MAX_ORCH_MODELS);
    orchModalSearch = "";
    const search = input("orchModelSearch");
    if (search) search.value = "";
    const title = el("orchModalTitle");
    if (title) title.textContent = `Models · ${role.label || roleKey}`;
    renderOrchModalSlots();
    renderOrchModalList();
    el("orchModelModal")?.classList.remove("hidden");
  }

  function renderOrchModalList() {
    const list = el("orchModelPickList");
    if (!list) return;
    list.innerHTML = "";
    const role = orchModalRoleKey ? orchRoleByKey(orchModalRoleKey) : null;
    const provKey = effectiveRoleProvider(role);
    const p = findProvider(provKey);
    if (!p || !p.models || !p.models.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = p?.models_error || "Configure provider & refresh models first";
      list.appendChild(empty);
      return;
    }
    const q = (orchModalSearch || "").trim().toLowerCase();
    const models = q ? p.models.filter((m) => (m.id || "").toLowerCase().includes(q)) : p.models;
    const atMax = orchModalSelection.length >= MAX_ORCH_MODELS;
    models.forEach((m) => {
      const id = m.id || "";
      const selIdx = orchModalSelection.indexOf(id);
      const selected = selIdx >= 0;
      const row = document.createElement("button");
      row.type = "button";
      row.className = "orch-model-row" + (selected ? " selected" : "") + (!selected && atMax ? " dimmed" : "");
      if (selected) {
        const slot = document.createElement("span");
        slot.className = "orch-row-slot";
        slot.textContent = String(selIdx + 1);
        row.appendChild(slot);
      } else {
        const dot = document.createElement("span");
        dot.className = "orch-row-dot";
        dot.textContent = "○";
        row.appendChild(dot);
      }
      const name = document.createElement("span");
      name.className = "orch-row-name";
      name.textContent = id;
      row.appendChild(name);
      const ctx = formatContextTokens(m.context_tokens);
      if (ctx) {
        const meta = document.createElement("span");
        meta.className = "orch-row-meta";
        meta.textContent = ctx;
        row.appendChild(meta);
      }
      row.addEventListener("click", () => {
        if (!selected && atMax) return;
        toggleOrchModel(id);
      });
      list.appendChild(row);
    });
  }

  input("orchModelSearch")?.addEventListener("input", () => {
    orchModalSearch = input("orchModelSearch")?.value || "";
    renderOrchModalList();
  });

  el("orchModalClose")?.addEventListener("click", () => {
    el("orchModelModal")?.classList.add("hidden");
    orchModalRoleKey = null;
  });

  el("orchModalApply")?.addEventListener("click", () => {
    const role = orchModalRoleKey ? orchRoleByKey(orchModalRoleKey) : null;
    if (role) {
      role.models = orchModalSelection.slice(0, MAX_ORCH_MODELS);
      role.model = role.models[0] || "";
    }
    el("orchModelModal")?.classList.add("hidden");
    orchModalRoleKey = null;
    renderOrchestra();
  });

  el("saveOrchestra")?.addEventListener("click", () => {
    showError("");
    if (!orchestraConfig) return;
    vscode.postMessage({
      type: "saveOrchestra",
      roles: orchestraConfig.roles,
      defaultTier: el("orchDefaultTier")?.value || "focused",
      maxWorkerRetries: input("orchMaxRetries") ? Number(input("orchMaxRetries").value) : undefined,
      workerVerifyEnabled: /** @type {HTMLInputElement | null} */ (el("orchVerifyEnabled"))?.checked,
      maxWorkerVerifyRetries: input("orchMaxVerifyRetries")
        ? Number(input("orchMaxVerifyRetries").value)
        : undefined,
      workerLLMVerifyEnabled: /** @type {HTMLInputElement | null} */ (el("orchLLMVerify"))?.checked,
    });
  });

  el("refreshOrchModels")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "refreshOrchModels" });
  });
  el("saveIndex")?.addEventListener("click", () => {
    showError("");
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    vscode.postMessage({
      type: "saveIndex",
      excludeDirs: area("excludeDirs")?.value || "",
      contextLimitKB: input("contextLimitKB") ? Number(input("contextLimitKB").value) : undefined,
      limitsMaxFiles: input("limitsMaxFiles") ? Number(input("limitsMaxFiles").value) : undefined,
      embedAPIBase: input("embedAPIBase")?.value || "",
      embedAPIKey: input("embedAPIKey")?.value || "",
      embedModel: input("embedModel")?.value || "",
      embedBatchSize: input("embedBatchSize") ? Number(input("embedBatchSize").value) : undefined,
      semanticAutoExplore: sem ? sem.checked : undefined,
    });
  });

  el("rebuildGraph")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = "Rebuilding graph…";
    vscode.postMessage({ type: "rebuildGraph" });
  });

  el("runEmbed")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = "Running embed (may take a while)…";
    vscode.postMessage({ type: "runEmbed", rebuild: false });
  });

  el("openGraph")?.addEventListener("click", () => {
    vscode.postMessage({ type: "openGraphViewer", port: graphUIPort });
  });

  el("reload")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "reload" });
  });

  el("savePrompt")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({
      type: "savePrompt",
      content: area("systemPrompt")?.value || "",
      promptFamily: input("promptFamily")?.value || "",
    });
  });

  el("clearPrompt")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "clearPrompt" });
  });

  el("newAgent")?.addEventListener("click", () => {
    if (input("agentName")) input("agentName").value = "";
    if (area("agentPrompt")) area("agentPrompt").value = "";
    renderAgentToolsList(null);
  });

  el("saveAgent")?.addEventListener("click", () => {
    showError("");
    const tools = collectAgentTools();
    if (tools === null) {
      showError("Enable at least one tool, or turn all on to inherit the full set.");
      return;
    }
    vscode.postMessage({
      type: "upsertAgent",
      name: input("agentName")?.value || "",
      system_prompt: area("agentPrompt")?.value || "",
      tools,
    });
  });

  el("deleteAgent")?.addEventListener("click", () => {
    const name = input("agentName")?.value?.trim();
    if (!name) return;
    showError("");
    vscode.postMessage({ type: "deleteAgent", name });
  });

  /**
   * @returns {string[] | "" | null}
   * "" / empty array meaning inherit (all on);
   * string[] allowlist;
   * null = invalid (none enabled).
   */
  function collectAgentTools() {
    const host = el("agentToolsList");
    if (!host) return "";
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) return "";
    const on = [];
    let off = 0;
    boxes.forEach((box) => {
      const name = box.getAttribute("data-tool") || "";
      if (box.checked) {
        if (name) on.push(name);
      } else {
        off += 1;
      }
    });
    if (off === 0) return "";
    if (!on.length) return null;
    return on;
  }

  /** @param {string} name */
  function agentToolCategory(name) {
    if (
      name === "ls" ||
      name === "read" ||
      name === "glob" ||
      name === "write" ||
      name === "edit" ||
      name === "fs.delete" ||
      name === "fs.rename" ||
      name === "diff.preview" ||
      name === "ast_rename"
    ) {
      return "Filesystem";
    }
    if (
      name === "grep" ||
      name === "symbols" ||
      name === "explore" ||
      name === "semantic_search" ||
      name === "repo_map"
    ) {
      return "Search & nav";
    }
    if (name === "bash" || name.startsWith("bash.")) return "Exec";
    if (name === "webfetch" || name === "websearch") return "Web";
    if (name.startsWith("browser.")) return "Browser";
    if (name.startsWith("lsp.")) return "LSP";
    if (name.startsWith("git.")) return "Git";
    if (name.startsWith("gh.")) return "GitHub";
    if (
      name === "todowrite" ||
      name === "todoread" ||
      name.startsWith("memory_") ||
      name === "runtime_query" ||
      name === "question"
    ) {
      return "Session";
    }
    if (name.startsWith("task_") || name.startsWith("plan_")) return "Tasks & plan";
    return "Other";
  }

  const AGENT_TOOL_CAT_ORDER = [
    "Filesystem",
    "Search & nav",
    "Exec",
    "Web",
    "Browser",
    "LSP",
    "Git",
    "GitHub",
    "Session",
    "Tasks & plan",
    "Other",
  ];

  /** @param {HTMLElement} section */
  function syncAgentCategoryToggle(section) {
    const master = /** @type {HTMLInputElement | null} */ (
      section.querySelector('input[data-cat-toggle]')
    );
    if (!master) return;
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      section.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    let on = 0;
    boxes.forEach((b) => {
      if (b.checked) on += 1;
    });
    master.checked = boxes.length > 0 && on === boxes.length;
    master.indeterminate = on > 0 && on < boxes.length;
    const count = section.querySelector(".agent-tools-cat-count");
    if (count) count.textContent = `${on}/${boxes.length}`;
  }

  function updateAgentToolsHint() {
    const hint = el("agentToolsHint");
    const host = el("agentToolsList");
    if (!hint || !host) return;
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) {
      hint.textContent = "Tool catalog unavailable — start core and reload.";
      return;
    }
    let on = 0;
    boxes.forEach((b) => {
      if (b.checked) on += 1;
    });
    hint.textContent =
      on === boxes.length
        ? `${boxes.length} tools · inherit full set`
        : `${on} / ${boxes.length} tools enabled`;
  }

  /** @param {any} a */
  function renderAgentToolsList(a) {
    const host = el("agentToolsList");
    const hint = el("agentToolsHint");
    if (!host) return;
    const openCats = new Set();
    host.querySelectorAll(".agent-tools-cat:not(.collapsed)").forEach((d) => {
      const id = d.getAttribute("data-cat");
      if (id) openCats.add(id);
    });
    host.innerHTML = "";
    const catalog = (agentAvailableTools.length
      ? agentAvailableTools
      : Array.isArray(a?.tools)
        ? a.tools
        : []
    )
      .map((x) => String(x || "").trim())
      .filter(Boolean);
    if (!catalog.length) {
      if (hint) hint.textContent = "Tool catalog unavailable — start core and reload.";
      return;
    }
    const selected = Array.isArray(a?.tools) ? a.tools.map((x) => String(x || "")) : null;
    const allOn = !selected || !selected.length;

    /** @type {Map<string, string[]>} */
    const byCat = new Map();
    catalog.forEach((name) => {
      const cat = agentToolCategory(name);
      if (!byCat.has(cat)) byCat.set(cat, []);
      byCat.get(cat)?.push(name);
    });

    AGENT_TOOL_CAT_ORDER.forEach((cat) => {
      const tools = byCat.get(cat);
      if (!tools || !tools.length) return;
      tools.sort();

      const section = document.createElement("div");
      section.className = "agent-tools-cat";
      section.setAttribute("data-cat", cat);
      const shouldOpen = openCats.has(cat) || (openCats.size === 0 && cat === "Filesystem");
      if (!shouldOpen) section.classList.add("collapsed");

      const head = document.createElement("div");
      head.className = "agent-tools-cat-head";

      const expand = document.createElement("button");
      expand.type = "button";
      expand.className = "agent-tools-cat-expand";
      expand.setAttribute("aria-expanded", shouldOpen ? "true" : "false");
      const chevron = document.createElement("span");
      chevron.className = "agent-tools-cat-chevron";
      chevron.setAttribute("aria-hidden", "true");
      chevron.textContent = shouldOpen ? "▾" : "▸";
      expand.appendChild(chevron);
      const title = document.createElement("span");
      title.className = "agent-tools-cat-title";
      title.textContent = cat;
      expand.appendChild(title);
      const count = document.createElement("span");
      count.className = "agent-tools-cat-count";
      expand.appendChild(count);
      expand.addEventListener("click", () => {
        const willOpen = section.classList.contains("collapsed");
        if (willOpen) {
          host.querySelectorAll(".agent-tools-cat").forEach((other) => {
            if (other === section) return;
            other.classList.add("collapsed");
            const btn = other.querySelector(".agent-tools-cat-expand");
            const chev = other.querySelector(".agent-tools-cat-chevron");
            if (btn) btn.setAttribute("aria-expanded", "false");
            if (chev) chev.textContent = "▸";
          });
        }
        const open = section.classList.toggle("collapsed") === false;
        expand.setAttribute("aria-expanded", open ? "true" : "false");
        chevron.textContent = open ? "▾" : "▸";
      });
      head.appendChild(expand);

      const masterWrap = document.createElement("label");
      masterWrap.className = "mcp-switch";
      masterWrap.title = `Toggle all ${cat}`;
      const master = document.createElement("input");
      master.type = "checkbox";
      master.setAttribute("data-cat-toggle", cat);
      const masterUi = document.createElement("span");
      masterUi.className = "mcp-switch-ui";
      masterUi.setAttribute("aria-hidden", "true");
      masterWrap.appendChild(master);
      masterWrap.appendChild(masterUi);
      head.appendChild(masterWrap);
      section.appendChild(head);

      const body = document.createElement("div");
      body.className = "agent-tools-cat-body";
      tools.forEach((name) => {
        const row = document.createElement("div");
        row.className = "agent-tool-row";
        const labelText = document.createElement("span");
        labelText.className = "agent-tool-name";
        labelText.textContent = name;
        labelText.title = name;
        row.appendChild(labelText);
        const label = document.createElement("label");
        label.className = "mcp-switch";
        label.title = name;
        const box = document.createElement("input");
        box.type = "checkbox";
        box.setAttribute("data-tool", name);
        box.checked = allOn || (selected ? selected.includes(name) : false);
        box.addEventListener("change", () => {
          syncAgentCategoryToggle(section);
          updateAgentToolsHint();
        });
        const ui = document.createElement("span");
        ui.className = "mcp-switch-ui";
        ui.setAttribute("aria-hidden", "true");
        label.appendChild(box);
        label.appendChild(ui);
        row.appendChild(label);
        body.appendChild(row);
      });
      section.appendChild(body);

      master.addEventListener("change", () => {
        const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
          section.querySelectorAll('input[type="checkbox"][data-tool]')
        );
        boxes.forEach((b) => {
          b.checked = master.checked;
        });
        master.indeterminate = false;
        syncAgentCategoryToggle(section);
        updateAgentToolsHint();
      });

      host.appendChild(section);
      syncAgentCategoryToggle(section);
    });
    updateAgentToolsHint();
  }

  function fillAgentForm(a) {
    if (!a) return;
    if (input("agentName")) input("agentName").value = a.name || "";
    if (area("agentPrompt")) area("agentPrompt").value = a.system_prompt || "";
    renderAgentToolsList(a);
  }

  function renderAgents() {
    const list = el("agentsList");
    if (!list) return;
    list.innerHTML = "";
    if (!agents.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = "No custom agents yet";
      list.appendChild(empty);
    } else {
      agents.forEach((a) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "list-item";
        btn.textContent = a.name;
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = Array.isArray(a.tools) && a.tools.length
          ? `${a.tools.length} tools`
          : "all tools";
        btn.appendChild(badge);
        btn.addEventListener("click", () => fillAgentForm(a));
        list.appendChild(btn);
      });
    }
    const name = input("agentName")?.value?.trim();
    const cur = name ? agents.find((x) => x.name === name) : null;
    if (cur) {
      renderAgentToolsList(cur);
      return;
    }
    const host = el("agentToolsList");
    if (!host || !host.querySelector('input[data-tool]')) {
      renderAgentToolsList(null);
    }
  }

  function renderSkills() {
    const list = el("skillsList");
    if (!list) return;
    list.innerHTML = "";
    if (!skills.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = "No skills discovered — try orchestra skills install";
      list.appendChild(empty);
      return;
    }
    skills.forEach((s) => {
      const row = document.createElement("div");
      row.className = "list-item static";
      const title = document.createElement("span");
      title.textContent = s.name;
      row.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = s.origin || "skill";
      badge.title = s.description || "";
      row.appendChild(badge);
      list.appendChild(row);
    });
  }

  function renderIndexStats(index) {
    const g = (index && index.graph) || {};
    const set = (id, v) => {
      const n = el(id);
      if (n) n.textContent = g.available ? String(v ?? 0) : "—";
    };
    set("statFiles", g.files);
    set("statNodes", g.nodes);
    set("statEdges", g.edges);
    set("statEmb", g.embeddings);
    const hint = el("indexStatusHint");
    if (hint) {
      if (!g.available) {
        hint.textContent = "CKG store not available — start core first.";
      } else {
        const miss = g.missing_embeddings || 0;
        hint.textContent =
          miss > 0
            ? `${miss} nodes need embedding · ${g.db_path || ".orchestra/ckg.db"}`
            : `Graph ready · ${g.db_path || ".orchestra/ckg.db"}`;
      }
    }
  }

  /** @param {string} cmd */
  function resolveMcpCommand(cmd) {
    const root = workspaceRoot || ".";
    return String(cmd || "").replace(/\$\{workspaceRoot\}/g, root);
  }

  /** @param {any} entry */
  function catalogEntryToForm(entry) {
    const envLines = Array.isArray(entry.env) ? entry.env.slice() : [];
    return {
      name: entry.name || entry.id || "",
      command: resolveMcpCommand(entry.command || ""),
      env: envLines.join("\n"),
      allowed_tools: "",
      call_timeout_s: 0,
      disabled: false,
      envRequired: Boolean(entry.envRequired),
      title: entry.title || entry.name || "",
    };
  }

  /** @param {"browse" | "installed"} tab */
  function setMcpTab(tab) {
    mcpTab = tab === "installed" ? "installed" : "browse";
    document.querySelectorAll(".mcp-tab").forEach((btn) => {
      const on = btn.getAttribute("data-mcp-tab") === mcpTab;
      btn.classList.toggle("active", on);
      btn.setAttribute("aria-selected", on ? "true" : "false");
    });
    el("mcpBrowsePane")?.classList.toggle("hidden", mcpTab !== "browse");
    el("mcpInstalledPane")?.classList.toggle("hidden", mcpTab !== "installed");
  }

  /** @param {string} name */
  function findInstalledMcp(name) {
    const key = (name || "").toLowerCase();
    return mcpServers.find((s) => String(s.name || "").toLowerCase() === key);
  }

  function fillMcpForm(s) {
    if (!s) return;
    mcpIsNewCustom = false;
    if (input("mcpName")) input("mcpName").value = s.name || "";
    if (input("mcpCommand")) {
      input("mcpCommand").value = Array.isArray(s.command) ? s.command.join(" ") : s.command || "";
    }
    if (area("mcpEnv")) {
      if (typeof s.env === "string") {
        area("mcpEnv").value = s.env;
      } else {
        const env = s.env || {};
        area("mcpEnv").value = Object.keys(env)
          .map((k) => `${k}=${env[k]}`)
          .join("\n");
      }
    }
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (enabled) enabled.checked = !Boolean(s.disabled);
    const title = el("mcpCfgTitle");
    if (title) title.textContent = `Configure ${s.name || "server"}`;
    const sub = el("mcpCfgSub");
    if (sub) {
      const n = Number(s.tool_count) || (Array.isArray(s.tools) ? s.tools.length : 0);
      sub.textContent = s.disabled
        ? "Off"
        : n > 0
          ? `${n} tool${n === 1 ? "" : "s"}`
          : s.status || "Installed";
    }
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = "Command";
    const srcMeta = el("mcpCfgSourceMeta");
    if (srcMeta) {
      srcMeta.textContent = Array.isArray(s.command) ? s.command.join(" ") : s.command || "—";
    }
    mcpDraftTools = Array.isArray(s.tools) && s.tools.length ? s.tools.slice() : [];
    renderMcpToolsList(s);
    updateMcpDeleteVisibility(s);
    openMcpConfigure(true);
    if (!mcpDraftTools.length && !s.disabled && (s.command || []).length) {
      requestMcpToolsProbe(false);
    }
  }

  function clearMcpForm() {
    mcpIsNewCustom = true;
    mcpDraftTools = [];
    if (input("mcpName")) input("mcpName").value = "";
    if (input("mcpCommand")) input("mcpCommand").value = "";
    if (area("mcpEnv")) area("mcpEnv").value = "";
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (enabled) enabled.checked = true;
    const title = el("mcpCfgTitle");
    if (title) title.textContent = "Configure custom server";
    const sub = el("mcpCfgSub");
    if (sub) sub.textContent = "New server";
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = "Custom";
    const srcMeta = el("mcpCfgSourceMeta");
    if (srcMeta) srcMeta.textContent = "Enter command below";
    const out = el("mcpTestOut");
    if (out) out.textContent = "";
    renderMcpToolsList(null);
    updateMcpDeleteVisibility(null);
    openMcpConfigure(true);
  }

  /** @param {boolean} open */
  function openMcpConfigure(open) {
    mcpConfigureOpen = Boolean(open);
    el("mcpConfigure")?.classList.toggle("hidden", !mcpConfigureOpen);
  }

  /** @param {any} s */
  function updateMcpDeleteVisibility(s) {
    const btn = /** @type {HTMLButtonElement | null} */ (el("deleteMCP"));
    if (!btn) return;
    const name = String(s?.name || "").trim();
    const exists = Boolean(name && findInstalledMcp(name));
    btn.classList.toggle("hidden", !exists);
  }

  /** @param {any} s */
  function mcpStatusLabel(s) {
    if (!s) return "";
    if (s.disabled) return "Off";
    if (s.status === "error") return s.error || "Error";
    const n = Number(s.tool_count) || 0;
    if (s.status === "running" || n > 0) {
      return n === 1 ? "1 tool" : `${n} tools`;
    }
    if (s.status === "stopped") return "Stopped";
    return s.status || "Installed";
  }

  /**
   * Collect allowed_tools from toggles.
   * Empty string = all tools enabled.
   * @returns {string}
   */
  function collectAllowedTools() {
    const host = el("mcpToolsList");
    if (!host) return "";
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) return "";
    const on = [];
    let off = 0;
    boxes.forEach((box) => {
      if (box.checked) on.push(box.getAttribute("data-tool") || "");
      else off += 1;
    });
    if (off === 0) return "";
    return on.filter(Boolean).join(", ");
  }

  /** @param {any} s */
  function renderMcpToolsList(s) {
    const host = el("mcpToolsList");
    const hint = el("mcpToolsHint");
    if (!host) return;
    host.innerHTML = "";
    const allowed = Array.isArray(s?.allowed_tools) ? s.allowed_tools : [];
    const allAllowed = !allowed.length;
    const tools = mcpDraftTools.length
      ? mcpDraftTools
      : Array.isArray(s?.tools)
        ? s.tools
        : allowed.slice();
    if (mcpToolsLoading) {
      if (hint) hint.textContent = "Loading tools…";
      return;
    }
    if (!tools.length) {
      if (hint) {
        hint.textContent = s?.disabled
          ? "Turn the server on to load tools."
          : "No tools discovered yet — use Reload after save.";
      }
      return;
    }
    if (hint) hint.textContent = `${tools.length} tool${tools.length === 1 ? "" : "s"}`;
    tools.forEach((name) => {
      const row = document.createElement("div");
      row.className = "mcp-tool-row";
      const code = document.createElement("code");
      code.textContent = name;
      row.appendChild(code);
      const label = document.createElement("label");
      label.className = "mcp-switch";
      label.title = name;
      const box = document.createElement("input");
      box.type = "checkbox";
      box.setAttribute("data-tool", name);
      box.checked = allAllowed || allowed.includes(name);
      const ui = document.createElement("span");
      ui.className = "mcp-switch-ui";
      ui.setAttribute("aria-hidden", "true");
      label.appendChild(box);
      label.appendChild(ui);
      row.appendChild(label);
      host.appendChild(row);
    });
  }

  /** @param {boolean} showStatus */
  function requestMcpToolsProbe(showStatus) {
    const payload = mcpFormPayload();
    if (!payload.name && !payload.command) return;
    mcpToolsLoading = true;
    renderMcpToolsList(findInstalledMcp(payload.name) || payload);
    if (showStatus) {
      const out = el("mcpTestOut");
      if (out) out.textContent = "Reloading tools…";
    }
    vscode.postMessage({ type: "testMCP", ...payload });
  }

  function renderMcpCatalogCats() {
    const host = el("mcpCatalogCats");
    if (!host) return;
    if (mcpCatalogCategory === "Official") mcpCatalogCategory = "All";
    host.innerHTML = "";
    const all = mcpCatalog.entries || [];
    MCP_CAT_ORDER.forEach((cat) => {
      const count =
        cat === "All" ? all.length : all.filter((e) => entryMatchesCategoryFor(cat, e)).length;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "mcp-cat" + (mcpCatalogCategory === cat ? " active" : "");
      btn.textContent = `${cat} (${count})`;
      btn.addEventListener("click", () => {
        mcpCatalogCategory = cat;
        mcpCatalogPage = 0;
        renderMcpCatalog();
      });
      host.appendChild(btn);
    });
  }

  function updateMcpCatalogStatus() {
    const status = el("mcpCatalogStatus");
    if (!status) return;
    if (mcpCatalogBusy && !(mcpCatalog.entries || []).length) {
      status.textContent = "Loading MCP Registry…";
      return;
    }
    const src =
      mcpCatalogSource === "registry"
        ? "Official MCP Registry"
        : mcpCatalogSource === "mixed"
          ? "Featured + Official MCP Registry"
          : "Local featured catalog";
    let text = src;
    const n = (mcpCatalog.entries || []).length;
    if (n) text += ` · ${n} loaded`;
    if (mcpCatalogPrefetching) text += " · loading more…";
    if (mcpCatalogFilter.trim()) text += ` · search “${mcpCatalogFilter.trim()}”`;
    if (mcpCatalogError) text += ` · ${mcpCatalogError}`;
    status.textContent = text;
  }

  /** @param {string} cat @param {any} entry */
  function entryMatchesCategoryFor(cat, entry) {
    if (cat === "All") return true;
    if (cat === "Installable") return entry.installable !== false && Boolean(entry.command);
    if (cat === "Featured") return entry.source === "local" || (entry.tags || []).includes("featured");
    if (cat === "Remote") return entry.installable === false || (entry.tags || []).includes("remote") || entry.category === "Remote";
    return (entry.category || "") === cat;
  }

  /** @param {any} entry */
  function entryMatchesCategory(entry) {
    return entryMatchesCategoryFor(mcpCatalogCategory, entry);
  }

  function filteredMcpEntries() {
    return (mcpCatalog.entries || []).filter((e) => entryMatchesCategory(e));
  }

  function updateMcpPager(totalFiltered) {
    const pages = Math.max(1, Math.ceil(totalFiltered / MCP_PAGE_SIZE) || 1);
    if (mcpCatalogPage > pages - 1) mcpCatalogPage = pages - 1;
    if (mcpCatalogPage < 0) mcpCatalogPage = 0;
    const label = el("mcpCatalogPageLabel");
    if (label) label.textContent = `${mcpCatalogPage + 1} / ${pages}`;
    const prev = /** @type {HTMLButtonElement | null} */ (el("mcpCatalogPrev"));
    const next = /** @type {HTMLButtonElement | null} */ (el("mcpCatalogNext"));
    if (prev) prev.disabled = mcpCatalogPage <= 0;
    if (next) next.disabled = mcpCatalogPage >= pages - 1 || totalFiltered === 0;
  }

  function renderMcpCatalog() {
    renderMcpCatalogCats();
    updateMcpCatalogStatus();
    const list = el("mcpCatalogList");
    if (!list) return;
    list.innerHTML = "";
    const filtered = filteredMcpEntries();
    updateMcpPager(filtered.length);
    const start = mcpCatalogPage * MCP_PAGE_SIZE;
    const entries = filtered.slice(start, start + MCP_PAGE_SIZE);
    if (!entries.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = mcpCatalogBusy
        ? "Loading…"
        : mcpCatalogFilter.trim()
          ? "No MCP servers match this search"
          : "No MCP servers in catalog";
      list.appendChild(empty);
      return;
    }
    entries.forEach((entry) => {
      const installed = findInstalledMcp(entry.name || entry.id);
      const card = document.createElement("div");
      card.className =
        "mcp-card" + (installed ? " installed" : "") + (entry.installable === false ? " remote-only" : "");

      const head = document.createElement("div");
      head.className = "mcp-card-head";

      const titleWrap = document.createElement("div");
      titleWrap.className = "mcp-card-title-wrap";
      const titleRow = document.createElement("div");
      titleRow.className = "mcp-card-name-row";
      const title = document.createElement("strong");
      title.textContent = entry.title || entry.name || entry.id;
      titleRow.appendChild(title);
      if (entry.version) {
        const ver = document.createElement("span");
        ver.className = "mcp-card-version";
        ver.textContent = `v${entry.version}`;
        titleRow.appendChild(ver);
      }
      titleWrap.appendChild(titleRow);
      if (entry.name && entry.name !== (entry.title || "")) {
        const meta = document.createElement("div");
        meta.className = "mcp-card-meta";
        meta.textContent = entry.name;
        titleWrap.appendChild(meta);
      }
      head.appendChild(titleWrap);

      const kind = document.createElement("span");
      kind.className = "mcp-card-cat";
      kind.textContent =
        entry.installable === false ? "remote" : entry.source === "local" ? "featured" : "stdio";
      head.appendChild(kind);
      card.appendChild(head);

      const desc = document.createElement("p");
      desc.className = "mcp-card-desc";
      desc.textContent = entry.description || "";
      card.appendChild(desc);

      const actions = document.createElement("div");
      actions.className = "mcp-card-actions";
      if (installed) {
        const badge = document.createElement("span");
        badge.className = "badge " + (installed.status || "");
        badge.textContent = installed.disabled
          ? "Off"
          : Number(installed.tool_count) > 0
            ? `${installed.tool_count} tools`
            : installed.status || "installed";
        actions.appendChild(badge);
        const cfg = document.createElement("button");
        cfg.type = "button";
        cfg.className = "secondary";
        cfg.textContent = "Configure";
        cfg.addEventListener("click", () => {
          fillMcpForm(installed);
          setMcpTab("installed");
        });
        actions.appendChild(cfg);
      } else if (entry.installable === false || !entry.command) {
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = "remote only";
        badge.title = "Orchestra currently installs stdio MCP servers";
        actions.appendChild(badge);
      } else {
        const install = document.createElement("button");
        install.type = "button";
        install.className = "secondary";
        install.textContent = entry.envRequired ? "Install…" : "Install";
        install.addEventListener("click", () => installCatalogEntry(entry));
        actions.appendChild(install);
      }
      if (entry.homepage) {
        const link = document.createElement("button");
        link.type = "button";
        link.className = "secondary";
        link.textContent = "Docs";
        link.addEventListener("click", () => {
          vscode.postMessage({ type: "openExternal", url: entry.homepage });
        });
        actions.appendChild(link);
      }
      card.appendChild(actions);
      list.appendChild(card);
    });
  }

  /** @param {any} entry */
  function installCatalogEntry(entry) {
    if (entry.installable === false || !entry.command) {
      const out = el("mcpTestOut");
      if (out) out.textContent = "This registry entry is remote-only — stdio install not available yet.";
      setMcpTab("installed");
      openMcpConfigure(true);
      return;
    }
    const form = catalogEntryToForm(entry);
    fillMcpForm(form);
    setMcpTab("installed");
    const out = el("mcpTestOut");
    if (form.envRequired) {
      if (out) {
        out.textContent = `Fill required env for ${form.title || form.name}, then Done.`;
      }
      area("mcpEnv")?.focus();
      return;
    }
    if (out) out.textContent = `Installing ${form.name}…`;
    showError("");
    vscode.postMessage({
      type: "upsertMCP",
      name: form.name,
      command: form.command,
      env: form.env,
      allowed_tools: "",
      call_timeout_s: 0,
      disabled: false,
    });
  }

  function requestMcpRegistry() {
    mcpCatalogPage = 0;
    vscode.postMessage({
      type: "fetchMcpRegistry",
      search: mcpCatalogFilter,
    });
  }

  function renderMcp() {
    const list = el("mcpList");
    if (!list) return;
    list.innerHTML = "";
    if (!mcpServers.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = "No MCP servers installed — browse the catalog to add some";
      list.appendChild(empty);
    } else {
      const selectedName = (input("mcpName")?.value || "").trim().toLowerCase();
      mcpServers.forEach((s) => {
        const row = document.createElement("div");
        row.className =
          "mcp-installed-row" +
          (mcpConfigureOpen && selectedName && String(s.name || "").toLowerCase() === selectedName
            ? " active"
            : "");

        const main = document.createElement("button");
        main.type = "button";
        main.className = "mcp-installed-row-main";
        const title = document.createElement("strong");
        title.textContent = s.name;
        main.appendChild(title);
        const meta = document.createElement("span");
        meta.className =
          "mcp-installed-meta" +
          (s.disabled ? "" : s.status === "error" ? " err" : s.status === "running" ? " ok" : "");
        meta.textContent = mcpStatusLabel(s);
        if (s.error) meta.title = s.error;
        main.appendChild(meta);
        main.addEventListener("click", () => fillMcpForm(s));
        row.appendChild(main);

        const del = document.createElement("button");
        del.type = "button";
        del.className = "mcp-row-delete";
        del.title = "Remove server";
        del.setAttribute("aria-label", `Remove ${s.name}`);
        del.textContent = "×";
        del.addEventListener("click", (ev) => {
          ev.preventDefault();
          ev.stopPropagation();
          const name = String(s.name || "").trim();
          if (!name) return;
          showError("");
          openMcpConfigure(false);
          mcpIsNewCustom = false;
          vscode.postMessage({ type: "deleteMCP", name });
        });
        row.appendChild(del);

        const toggle = document.createElement("label");
        toggle.className = "mcp-switch";
        toggle.title = s.disabled ? "Enable" : "Disable";
        const box = document.createElement("input");
        box.type = "checkbox";
        box.checked = !Boolean(s.disabled);
        box.addEventListener("click", (ev) => ev.stopPropagation());
        box.addEventListener("change", () => {
          const name = String(s.name || "").trim();
          if (!name) return;
          showError("");
          vscode.postMessage({
            type: "setMCPDisabled",
            name,
            disabled: !box.checked,
          });
        });
        const ui = document.createElement("span");
        ui.className = "mcp-switch-ui";
        ui.setAttribute("aria-hidden", "true");
        toggle.appendChild(box);
        toggle.appendChild(ui);
        toggle.addEventListener("click", (ev) => ev.stopPropagation());
        row.appendChild(toggle);

        list.appendChild(row);
      });
    }
    if (mcpConfigureOpen) {
      const cur = findInstalledMcp(input("mcpName")?.value || "");
      if (cur) {
        const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
        if (enabled) enabled.checked = !Boolean(cur.disabled);
        if (Array.isArray(cur.tools) && cur.tools.length) {
          mcpDraftTools = cur.tools.slice();
        }
        const sub = el("mcpCfgSub");
        if (sub) sub.textContent = mcpStatusLabel(cur);
        updateMcpDeleteVisibility(cur);
        if (!mcpToolsLoading) renderMcpToolsList(cur);
      } else if (!mcpIsNewCustom) {
        openMcpConfigure(false);
      }
    }
    renderMcpCatalog();
  }

  function mcpFormPayload() {
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    const name = input("mcpName")?.value || "";
    const existing = findInstalledMcp(name);
    return {
      name,
      command: input("mcpCommand")?.value || "",
      env: area("mcpEnv")?.value || "",
      allowed_tools: collectAllowedTools(),
      call_timeout_s: Number(existing?.call_timeout_s) || 0,
      disabled: enabled ? !enabled.checked : false,
    };
  }

  document.querySelectorAll(".mcp-tab").forEach((btn) => {
    btn.addEventListener("click", () => {
      setMcpTab(/** @type {"browse" | "installed"} */ (btn.getAttribute("data-mcp-tab") || "browse"));
    });
  });

  input("mcpCatalogSearch")?.addEventListener("input", () => {
    mcpCatalogFilter = input("mcpCatalogSearch")?.value || "";
    if (mcpSearchTimer) clearTimeout(mcpSearchTimer);
    mcpSearchTimer = window.setTimeout(() => {
      requestMcpRegistry();
    }, 350);
  });

  el("mcpCatalogPrev")?.addEventListener("click", () => {
    if (mcpCatalogPage <= 0) return;
    mcpCatalogPage -= 1;
    renderMcpCatalog();
  });

  el("mcpCatalogNext")?.addEventListener("click", () => {
    const pages = Math.max(1, Math.ceil(filteredMcpEntries().length / MCP_PAGE_SIZE) || 1);
    if (mcpCatalogPage >= pages - 1) return;
    mcpCatalogPage += 1;
    renderMcpCatalog();
  });

  el("mcpAddCustom")?.addEventListener("click", () => {
    clearMcpForm();
    const out = el("mcpTestOut");
    if (out) out.textContent = "Enter a custom command, then Done.";
    input("mcpName")?.focus();
  });

  el("mcpCfgClose")?.addEventListener("click", () => {
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    renderMcp();
  });

  el("mcpConfigure")?.addEventListener("click", (ev) => {
    if (ev.target === el("mcpConfigure")) {
      openMcpConfigure(false);
      mcpIsNewCustom = false;
      renderMcp();
    }
  });

  el("mcpEnabled")?.addEventListener("change", () => {
    const name = input("mcpName")?.value?.trim();
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (!name || mcpIsNewCustom || !enabled) return;
    showError("");
    vscode.postMessage({
      type: "setMCPDisabled",
      name,
      disabled: !enabled.checked,
    });
  });

  el("saveMCP")?.addEventListener("click", () => {
    showError("");
    const payload = mcpFormPayload();
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    vscode.postMessage({ type: "upsertMCP", ...payload });
  });

  el("deleteMCP")?.addEventListener("click", (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    const name = input("mcpName")?.value?.trim();
    if (!name) return;
    showError("");
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    vscode.postMessage({ type: "deleteMCP", name });
  });

  el("reloadMCP")?.addEventListener("click", () => {
    showError("");
    requestMcpToolsProbe(true);
  });
  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "error") {
      showError(msg.message || "error");
      return;
    }
    if (msg.type === "modelPicked" && input("model") && msg.model) {
      input("model").value = msg.model;
      return;
    }
    if (msg.type === "mcpTestResult") {
      const r = msg.result || {};
      mcpToolsLoading = false;
      const out = el("mcpTestOut");
      if (out) {
        out.textContent = r.ok
          ? `OK (${r.elapsed}): ${(r.tools || []).length} tool${(r.tools || []).length === 1 ? "" : "s"}`
          : `Failed: ${r.error || "unknown"}`;
      }
      if (r.ok && Array.isArray(r.tools)) {
        mcpDraftTools = r.tools.slice();
        const cur = findInstalledMcp(input("mcpName")?.value || "");
        renderMcpToolsList(
          cur || {
            allowed_tools: [],
          }
        );
        const sub = el("mcpCfgSub");
        if (sub && mcpConfigureOpen) {
          const n = mcpDraftTools.length;
          sub.textContent = n === 1 ? "1 tool" : `${n} tools`;
        }
      } else {
        renderMcpToolsList(findInstalledMcp(input("mcpName")?.value || ""));
      }
      return;
    }
    if (msg.type === "mcpCatalogBusy") {
      mcpCatalogBusy = Boolean(msg.busy);
      if (msg.prefetching !== undefined) mcpCatalogPrefetching = Boolean(msg.prefetching);
      if (msg.error) mcpCatalogError = String(msg.error);
      updateMcpCatalogStatus();
      return;
    }
    if (msg.type === "mcpCatalog") {
      const catalog = msg.catalog || {};
      const incoming = Array.isArray(catalog.entries) ? catalog.entries : [];
      if (msg.append && !msg.replace) {
        const seen = new Set((mcpCatalog.entries || []).map((e) => String(e.id || e.name || "").toLowerCase()));
        const merged = (mcpCatalog.entries || []).slice();
        incoming.forEach((e) => {
          const key = String(e.id || e.name || "").toLowerCase();
          if (!key || seen.has(key)) return;
          seen.add(key);
          merged.push(e);
        });
        mcpCatalog = { ...catalog, entries: merged };
      } else {
        mcpCatalog = { ...catalog, entries: incoming };
      }
      mcpCatalogNextCursor = catalog.nextCursor || "";
      mcpCatalogSource = catalog.source || "registry";
      mcpCatalogError = catalog.error || "";
      mcpCatalogPrefetching = Boolean(catalog.prefetching);
      mcpCatalogBusy = Boolean(catalog.prefetching);
      renderMcpCatalog();
      return;
    }
    if (msg.type === "indexBusy") {
      const out = el("indexActionOut");
      if (out) out.textContent = msg.busy ? msg.message || "Working…" : "";
      return;
    }
    if (msg.type === "indexActionResult") {
      const out = el("indexActionOut");
      if (out) {
        if (msg.action === "embed" && msg.result) {
          const r = msg.result;
          out.textContent = `Embed: +${r.embedded} (${r.total} total, ${r.remaining} remaining, ${r.elapsed})`;
        } else if (msg.action === "rebuild" && msg.graph) {
          const g = msg.graph;
          out.textContent = `Graph: ${g.files} files, ${g.nodes} nodes, ${g.edges} edges`;
        }
      }
      return;
    }
    if (msg.type === "modelsBusy") {
      const status = el("modelsStatus");
      if (status) status.textContent = msg.busy ? msg.message || "Loading models…" : status.textContent;
      return;
    }
    if (msg.type === "providerCatalog") {
      applyProviderCatalog(msg.catalog, false);
      const cur = findProvider(selectedProviderKey);
      if (cur) populateProviderCreds(cur, { force: true });
      if (orchestraConfig) {
        sanitizeOrchestraRoles();
        renderOrchestra();
      }
      return;
    }
    if (msg.type === "state") return handleState(msg);
    return;
  });

  /** @param {any} msg */
  function handleState(msg) {
    workspaceRoot = msg.workspaceRoot || ".";
    const llm = msg.llm || {};
    const ext = msg.extension || {};
    const prompt = msg.prompt || {};
    const index = msg.index || {};

    if (input("binaryPath")) input("binaryPath").value = ext.binaryPath || "";
    if (input("projectRoot")) input("projectRoot").value = ext.projectRoot || "";

    applyProviderCatalog(msg.providerCatalog, true);
    const cur = findProvider(selectedProviderKey);
    if (cur) populateProviderCreds(cur, { force: true });
    if (input("promptFamily")) input("promptFamily").value = llm.promptFamily || prompt.promptFamily || "";
    if (input("temperature")) input("temperature").value = String(llm.temperature ?? 0);
    if (input("maxTokens")) input("maxTokens").value = String(llm.maxTokens ?? 0);
    if (input("timeoutS")) input("timeoutS").value = String(llm.timeoutS ?? 0);
    if (input("multimodal")) input("multimodal").checked = Boolean(llm.multimodal);

    if (area("excludeDirs")) {
      area("excludeDirs").value = Array.isArray(index.excludeDirs)
        ? index.excludeDirs.join("\n")
        : "";
    }
    if (input("contextLimitKB")) input("contextLimitKB").value = String(index.contextLimitKB ?? 0);
    if (input("limitsMaxFiles")) {
      input("limitsMaxFiles").value = index.limits?.max_files ? String(index.limits.max_files) : "";
    }
    const emb = index.embed || {};
    if (input("embedAPIBase")) input("embedAPIBase").value = emb.api_base || "";
    if (input("embedAPIKey")) input("embedAPIKey").value = "";
    if (input("embedModel")) input("embedModel").value = emb.model || "";
    if (input("embedBatchSize")) input("embedBatchSize").value = emb.batch_size ? String(emb.batch_size) : "";
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    if (sem) sem.checked = emb.semantic_auto_explore !== false;
    graphUIPort = index.graphUIPort || 6061;
    renderIndexStats(index);

    if (area("systemPrompt")) area("systemPrompt").value = prompt.content || "";
    const pathEl = el("promptPath");
    if (pathEl) {
      pathEl.textContent = prompt.hasOverride
        ? `Override active · ${prompt.path || ""}`
        : `No override · ${prompt.path || ".orchestra/system.txt"}`;
    }
    agents = (msg.agents && msg.agents.agents) || [];
    agentAvailableTools =
      (msg.agents && Array.isArray(msg.agents.availableTools) && msg.agents.availableTools) ||
      (msg.agents && Array.isArray(msg.agents.available_tools) && msg.agents.available_tools) ||
      agentAvailableTools;
    mcpServers = (msg.mcp && msg.mcp.servers) || [];
    skills = msg.skills || [];
    if (msg.mcpCatalog && Array.isArray(msg.mcpCatalog.entries)) {
      mcpCatalog = msg.mcpCatalog;
      mcpCatalogNextCursor = msg.mcpCatalog.nextCursor || "";
      mcpCatalogSource = msg.mcpCatalog.source || "local";
      mcpCatalogError = msg.mcpCatalog.error || "";
    } else if (typeof window !== "undefined" && window.__ORCH_MCP_CATALOG) {
      mcpCatalog = window.__ORCH_MCP_CATALOG;
      mcpCatalogSource = mcpCatalog.source || "local";
    }
    renderAgents();
    renderMcp();
    renderSkills();
    if (msg.orchestra) {
      orchestraConfig = msg.orchestra;
      const roles = orchestraConfig.roles || [];
      const explicit = roles.map((r) => (r.provider || "").trim()).filter(Boolean);
      const uniq = [...new Set(explicit)];
      if (uniq.length === 1) {
        orchSharedProvider = uniq[0];
      } else {
        orchSharedProvider =
          roles.find((r) => r.provider)?.provider ||
          orchestraConfig.mainProvider ||
          selectedProviderKey ||
          "";
      }
      sanitizeOrchestraRoles();
      renderOrchestra();
    }
    if (msg.navigateSection) {
      navigateToSection(msg.navigateSection);
    }
    showError("");
  }

  vscode.postMessage({ type: "ready" });
})();
