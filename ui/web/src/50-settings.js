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

  /** @param {string} method @param {any} params @returns {Promise<any>} */
  function settingsRpc(method, params) {
    const conn = currentProjectId ? connFor(currentProjectId) : null;
    if (!conn) {
      return Promise.reject(new Error("no project is open"));
    }
    return conn.send(method, params || {});
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

  function settingsWorkspaceRoot() {
    const entry = known.find((p) => p.id === currentProjectId);
    return (entry && entry.path) || "";
  }

  async function pushSettingsState() {
    try {
      const [llm, prompt, agents, mcp, index, skills, providerCatalog, orchestra, catalogFile] =
        await Promise.all([
          getLLM(),
          getSystemPrompt(),
          listAgents(),
          listMCP(),
          getIndexStatus(),
          listSkills(),
          listProviders({ probe: true, includeSecrets: true }),
          getOrchestra().catch(() => null),
          ensureMcpCatalogFile(),
        ]);
      const ws = settingsWorkspaceRoot();
      const navigateSection = settingsPendingSection;
      settingsPendingSection = "";
      // Which workspace this is. Everything on these screens is written to
      // that workspace's own .orchestra.yml — provider, models, roles, index,
      // MCP servers, agents are per workspace, not shared — so the panel has
      // to say which one it is editing, or the separation is invisible.
      const openProjectEntry = known.find((p) => p.id === currentProjectId);
      postToSettings({
        type: "workspace",
        name: (openProjectEntry && openProjectEntry.name) || "",
        path: (openProjectEntry && openProjectEntry.path) || ws,
      });
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
    } catch (err) {
      postToSettings({ type: "error", message: String((err && err.message) || err) });
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
