  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "error") {
      showError(msg.message || i18n("set.error"));
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
        const found = (r.tools || []).length;
        out.textContent = r.ok
          ? i18n(found === 1 ? "set.mcp.test_ok_one" : "set.mcp.test_ok_n", {
              elapsed: r.elapsed,
              n: found,
            })
          : i18n("set.mcp.test_failed", { detail: r.error || i18n("set.mcp.unknown") });
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
          sub.textContent = i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n });
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
      if (out) out.textContent = msg.busy ? msg.message || i18n("turn.working") : "";
      return;
    }
    if (msg.type === "indexActionResult") {
      const out = el("indexActionOut");
      if (out) {
        if (msg.action === "embed" && msg.result) {
          const r = msg.result;
          out.textContent = i18n("set.index.embed_result", {
            embedded: r.embedded,
            total: r.total,
            remaining: r.remaining,
            elapsed: r.elapsed,
          });
        } else if (msg.action === "rebuild" && msg.graph) {
          const g = msg.graph;
          out.textContent = i18n("set.index.graph_result", {
            files: g.files,
            nodes: g.nodes,
            edges: g.edges,
          });
        }
      }
      return;
    }
    if (msg.type === "modelsBusy") {
      const status = el("modelsStatus");
      if (status)
        status.textContent = msg.busy ? msg.message || i18n("set.models.loading") : status.textContent;
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
    if (input("embedBatchSize")) input("embedBatchSize").value = emb.batch_size ? String(emb.batch_size) : "";
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    if (sem) sem.checked = emb.semantic_auto_explore !== false;
    graphUIPort = index.graphUIPort || 6061;
    renderIndexStats(index);

    if (area("systemPrompt")) area("systemPrompt").value = prompt.content || "";
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

  /**
   * Redraw the parts of the panel that are built in JS rather than written in
   * the markup. applyStaticI18n() covers the markup; these lists were painted
   * with the previous language and would otherwise sit there until the next
   * state push.
   */
  function repaintTranslatedPanels() {
    renderProviders();
    renderModels();
    renderAgents();
    renderMcp();
    renderSkills();
    renderMcpCatalog();
    if (orchestraConfig) renderOrchestra();
  }

  // The panel is a document of its own and loads its script at the end of the
  // body, so the markup is here: translate it before the first paint rather
  // than waiting for the host's first state message.
  applyUiLanguage();

  vscode.postMessage({ type: "ready" });
