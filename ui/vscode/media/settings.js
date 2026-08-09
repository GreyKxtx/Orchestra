//@ts-check
(function () {
  // @ts-ignore
  const vscode = acquireVsCodeApi();

  /** @type {string} */
  let workspaceRoot = ".";
  /** @type {any[]} */
  let agents = [];
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

  const CATEGORY_ORDER = ["Local", "Cloud", "Gateway", "Other", "Named"];

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
    const keyHint = el("keyHint");
    if (keyHint) {
      keyHint.textContent = p.api_key_set
        ? "Key saved — leave blank to keep"
        : p.needs_key
          ? "API key required"
          : "No API key needed";
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
        const title = document.createElement("span");
        title.textContent = p.name || p.key;
        btn.appendChild(title);
        const badge = document.createElement("span");
        badge.className = "badge";
        if (p.active) badge.classList.add("running");
        if (p.models_error) badge.classList.add("error");
        let badgeText = p.key;
        if (p.ready && p.model_count > 0) badgeText = `${p.model_count} models`;
        else if (p.ready) badgeText = "ready";
        else if (p.needs_key && !p.api_key_set) badgeText = "needs key";
        else badgeText = p.custom ? "custom" : "—";
        badge.textContent = badgeText;
        if (p.models_error) badge.title = p.models_error;
        btn.appendChild(badge);
        btn.addEventListener("click", () => {
          selectedProviderKey = p.key;
          if (input("apiBase")) input("apiBase").value = p.api_base || "";
          if (input("apiKey")) input("apiKey").value = "";
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
    if (!models.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = p.ready ? "No models listed" : "Provider not ready";
      list.appendChild(empty);
      return;
    }
    models.forEach((m) => {
      const id = m.id || "";
      const btn = document.createElement("button");
      btn.type = "button";
      const isActive = id === activeModelId && p.key === activeProviderKey;
      const isSelected = id === selectedModelId;
      btn.className = "list-item pick-item" + (isSelected ? " selected" : "");
      const title = document.createElement("span");
      title.textContent = id;
      btn.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge" + (isActive ? " running" : "");
      badge.textContent = isActive ? "active" : m.owned_by || "";
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
    if (input("agentTools")) input("agentTools").value = "";
    if (input("agentModel")) input("agentModel").value = "";
    if (input("agentProvider")) input("agentProvider").value = "";
  });

  el("saveAgent")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({
      type: "upsertAgent",
      name: input("agentName")?.value || "",
      system_prompt: area("agentPrompt")?.value || "",
      tools: input("agentTools")?.value || "",
      model: input("agentModel")?.value || "",
      agentProvider: input("agentProvider")?.value || "",
    });
  });

  el("deleteAgent")?.addEventListener("click", () => {
    const name = input("agentName")?.value?.trim();
    if (!name) return;
    showError("");
    vscode.postMessage({ type: "deleteAgent", name });
  });

  function fillAgentForm(a) {
    if (!a) return;
    if (input("agentName")) input("agentName").value = a.name || "";
    if (area("agentPrompt")) area("agentPrompt").value = a.system_prompt || "";
    if (input("agentTools")) input("agentTools").value = Array.isArray(a.tools) ? a.tools.join(", ") : "";
    if (input("agentModel")) input("agentModel").value = a.model || "";
    if (input("agentProvider")) input("agentProvider").value = a.provider || "";
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
      return;
    }
    agents.forEach((a) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "list-item";
      btn.textContent = a.name;
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = a.model || (a.tools ? `${a.tools.length} tools` : "inherit");
      btn.appendChild(badge);
      btn.addEventListener("click", () => fillAgentForm(a));
      list.appendChild(btn);
    });
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

  function mcpPreset(key) {
    const root = workspaceRoot || ".";
    if (key === "filesystem") {
      return {
        name: "filesystem",
        command: `npx -y @modelcontextprotocol/server-filesystem ${root}`,
        env: "",
      };
    }
    if (key === "github") {
      return {
        name: "github",
        command: "npx -y @modelcontextprotocol/server-github",
        env: "GITHUB_PERSONAL_ACCESS_TOKEN=",
      };
    }
    if (key === "memory") {
      return {
        name: "memory",
        command: "npx -y @modelcontextprotocol/server-memory",
        env: "",
      };
    }
    return { name: "", command: "", env: "" };
  }

  document.querySelectorAll(".mcp-preset").forEach((btn) => {
    btn.addEventListener("click", () => {
      const key = btn.getAttribute("data-preset") || "custom";
      const p = mcpPreset(key);
      if (input("mcpName")) input("mcpName").value = p.name;
      if (input("mcpCommand")) input("mcpCommand").value = p.command;
      if (area("mcpEnv")) area("mcpEnv").value = p.env;
      if (input("mcpAllowed")) input("mcpAllowed").value = "";
      if (input("mcpTimeout")) input("mcpTimeout").value = "0";
      const dis = /** @type {HTMLInputElement | null} */ (el("mcpDisabled"));
      if (dis) dis.checked = false;
    });
  });

  function fillMcpForm(s) {
    if (!s) return;
    if (input("mcpName")) input("mcpName").value = s.name || "";
    if (input("mcpCommand")) input("mcpCommand").value = Array.isArray(s.command) ? s.command.join(" ") : "";
    if (area("mcpEnv")) {
      const env = s.env || {};
      area("mcpEnv").value = Object.keys(env)
        .map((k) => `${k}=${env[k]}`)
        .join("\n");
    }
    if (input("mcpAllowed")) {
      input("mcpAllowed").value = Array.isArray(s.allowed_tools) ? s.allowed_tools.join(", ") : "";
    }
    if (input("mcpTimeout")) input("mcpTimeout").value = String(s.call_timeout_s || 0);
    const dis = /** @type {HTMLInputElement | null} */ (el("mcpDisabled"));
    if (dis) dis.checked = Boolean(s.disabled);
  }

  function renderMcp() {
    const list = el("mcpList");
    if (!list) return;
    list.innerHTML = "";
    if (!mcpServers.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = "No MCP servers configured";
      list.appendChild(empty);
      return;
    }
    mcpServers.forEach((s) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "list-item";
      const title = document.createElement("span");
      title.textContent = s.name;
      btn.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge " + (s.status || "");
      badge.textContent = s.status + (s.tool_count ? ` · ${s.tool_count}` : "");
      if (s.error) badge.title = s.error;
      btn.appendChild(badge);
      btn.addEventListener("click", () => fillMcpForm(s));
      list.appendChild(btn);
    });
  }

  function mcpFormPayload() {
    const dis = /** @type {HTMLInputElement | null} */ (el("mcpDisabled"));
    return {
      name: input("mcpName")?.value || "",
      command: input("mcpCommand")?.value || "",
      env: area("mcpEnv")?.value || "",
      allowed_tools: input("mcpAllowed")?.value || "",
      call_timeout_s: input("mcpTimeout") ? Number(input("mcpTimeout").value) : 0,
      disabled: Boolean(dis?.checked),
    };
  }

  el("saveMCP")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "upsertMCP", ...mcpFormPayload() });
  });

  el("deleteMCP")?.addEventListener("click", () => {
    const name = input("mcpName")?.value?.trim();
    if (!name) return;
    showError("");
    vscode.postMessage({ type: "deleteMCP", name });
  });

  el("testMCP")?.addEventListener("click", () => {
    showError("");
    const out = el("mcpTestOut");
    if (out) out.textContent = "Testing…";
    vscode.postMessage({ type: "testMCP", ...mcpFormPayload() });
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
      const out = el("mcpTestOut");
      if (out) {
        out.textContent = r.ok
          ? `OK (${r.elapsed}): ${(r.tools || []).slice(0, 12).join(", ")}${(r.tools || []).length > 12 ? "…" : ""}`
          : `Failed: ${r.error || "unknown"}`;
      }
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
    if (input("apiKey")) input("apiKey").value = "";
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
    mcpServers = (msg.mcp && msg.mcp.servers) || [];
    skills = msg.skills || [];
    renderAgents();
    renderMcp();
    renderSkills();
    showError("");
  }

  vscode.postMessage({ type: "ready" });
})();
