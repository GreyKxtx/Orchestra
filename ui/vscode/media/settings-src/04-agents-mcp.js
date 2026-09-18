  el("saveIndex")?.addEventListener("click", () => {
    showError("");
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    vscode.postMessage({
      type: "saveIndex",
      excludeDirs: area("excludeDirs")?.value || "",
      contextLimitKB: input("contextLimitKB") ? Number(input("contextLimitKB").value) : undefined,
      limitsMaxFiles: input("limitsMaxFiles") ? Number(input("limitsMaxFiles").value) : undefined,
      embedBatchSize: input("embedBatchSize") ? Number(input("embedBatchSize").value) : undefined,
      semanticAutoExplore: sem ? sem.checked : undefined,
    });
  });

  el("rebuildGraph")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = i18n("set.index.rebuilding");
    vscode.postMessage({ type: "rebuildGraph" });
  });

  el("runEmbed")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = i18n("set.index.embedding");
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
      showError(i18n("set.agent.need_one_tool"));
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
      hint.textContent = i18n("set.agent.catalog_na");
      return;
    }
    let on = 0;
    boxes.forEach((b) => {
      if (b.checked) on += 1;
    });
    hint.textContent =
      on === boxes.length
        ? i18n("set.agent.tools_full", { n: boxes.length })
        : i18n("set.agent.tools_on", { on, total: boxes.length });
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
      if (hint) hint.textContent = i18n("set.agent.catalog_na");
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
      chevron.innerHTML = orchIconMarkup(shouldOpen ? "chevron-down" : "chevron-right", { size: "sm" });
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
            if (chev) chev.innerHTML = orchIconMarkup("chevron-right", { size: "sm" });
          });
        }
        const open = section.classList.toggle("collapsed") === false;
        expand.setAttribute("aria-expanded", open ? "true" : "false");
        chevron.innerHTML = orchIconMarkup(open ? "chevron-down" : "chevron-right", { size: "sm" });
      });
      head.appendChild(expand);

      const masterWrap = document.createElement("label");
      masterWrap.className = "mcp-switch";
      masterWrap.title = i18n("set.agent.toggle_all", { cat });
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
      empty.textContent = i18n("set.agent.none");
      list.appendChild(empty);
    } else {
      agents.forEach((a) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "list-item";
        btn.textContent = a.name;
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent =
          Array.isArray(a.tools) && a.tools.length
            ? i18n("set.agent.tools_n", { n: a.tools.length })
            : i18n("set.agent.tools_all");
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
      empty.textContent = i18n("set.skills.none");
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
      badge.textContent = s.origin || i18n("set.skills.badge");
      badge.title = s.description || "";
      row.appendChild(badge);
      list.appendChild(row);
    });
  }

  /** @param {number} v */
  function fmtStatNum(v) {
    const n = Number(v) || 0;
    if (n >= 100000) return `${Math.round(n / 1000)}k`;
    if (n >= 10000) return `${(n / 1000).toFixed(1)}k`;
    return String(n);
  }

  /** @param {string} language */
  function languageVisual(language) {
    const raw = String(language || "").trim();
    const key = raw.toLowerCase().replace(/[^a-z0-9+#]/g, "");
    if (key === "go" || key === "golang") return { key: "go", mark: "GO" };
    if (key === "tsx" || key === "jsx") return { key: "react", mark: "Re" };
    if (key === "typescript" || key === "ts") return { key: "typescript", mark: "TS" };
    if (key === "javascript" || key === "js") return { key: "javascript", mark: "JS" };
    if (key === "python" || key === "py") return { key: "python", mark: "Py" };
    if (key === "rust" || key === "rs") return { key: "rust", mark: "Rs" };
    if (key === "java") return { key: "java", mark: "Jv" };
    if (key === "c#" || key === "csharp" || key === "cs") return { key: "csharp", mark: "C#" };
    if (key === "c++" || key === "cpp" || key === "cplusplus") return { key: "cpp", mark: "C+" };
    if (key === "c") return { key: "c", mark: "C" };
    if (key === "html") return { key: "html", mark: "H5" };
    if (key === "css" || key === "scss" || key === "sass") return { key: "css", mark: "CSS" };
    if (key === "shell" || key === "bash" || key === "powershell") return { key: "shell", mark: ">_" };
    return { key: "other", mark: raw.slice(0, 2).toUpperCase() || "·" };
  }

  function renderIndexStats(index) {
    const g = (index && index.graph) || {};
    const set = (id, v) => {
      const n = el(id);
      if (n) {
        n.textContent = g.available ? fmtStatNum(v) : "—";
        n.title = g.available ? String(Number(v) || 0) : "";
      }
    };
    set("statFiles", g.files);
    set("statNodes", g.nodes);
    set("statEdges", g.edges);
    set("statEmb", g.embeddings);
    set("statFuncs", g.funcs);
    set("statTypes", g.types);
    set("statTests", g.tests);
    set("statPkgs", g.packages);

    const langsHost = el("indexLangs");
    if (langsHost) {
      langsHost.innerHTML = "";
      const langs = (g.available && g.langs && typeof g.langs === "object" && g.langs) || {};
      const entries = Object.entries(langs)
        .filter(([, n]) => Number(n) > 0)
        .sort((a, b) => Number(b[1]) - Number(a[1]));
      const total = entries.reduce((acc, [, n]) => acc + Number(n), 0);
      for (const [lang, n] of entries.slice(0, 6)) {
        const chip = document.createElement("span");
        chip.className = "lang-chip";
        const pct = total > 0 ? Math.round((Number(n) / total) * 100) : 0;
        const visual = languageVisual(lang);
        const logo = document.createElement("span");
        logo.className = `lang-logo lang-${visual.key}`;
        logo.textContent = visual.mark;
        logo.setAttribute("aria-hidden", "true");
        const label = document.createElement("span");
        label.textContent = `${lang} · ${pct}%`;
        chip.appendChild(logo);
        chip.appendChild(label);
        chip.title = i18n("set.index.files_n", { n });
        langsHost.appendChild(chip);
      }
      langsHost.classList.toggle("hidden", entries.length === 0);
    }

    const hint = el("indexStatusHint");
    if (hint) {
      if (!g.available) {
        hint.textContent = i18n("set.index.no_ckg");
      } else {
        const miss = g.missing_embeddings || 0;
        const dbPath = g.db_path || ".orchestra/ckg.db";
        hint.textContent =
          miss > 0
            ? i18n("set.index.need_embed", { n: miss, path: dbPath })
            : i18n("set.index.graph_ready", { path: dbPath });
      }
    }
    const embedHint = el("indexEmbedHint");
    if (embedHint) {
      const emb = (index && index.embed) || {};
      const model = String(emb.model || "").trim();
      const provider = String(emb.provider || "").trim();
      if (!model) {
        embedHint.textContent = i18n("set.index.no_embed_model");
      } else {
        embedHint.textContent = provider
          ? i18n("set.index.embed_model_via", { model, provider })
          : i18n("set.index.embed_model", { model });
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
    if (title)
      title.textContent = i18n("set.mcp.configure_named", { name: s.name || i18n("set.mcp.server") });
    const sub = el("mcpCfgSub");
    if (sub) {
      const n = Number(s.tool_count) || (Array.isArray(s.tools) ? s.tools.length : 0);
      sub.textContent = s.disabled
        ? i18n("set.mcp.off")
        : n > 0
          ? i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n })
          : s.status || i18n("set.mcp.installed");
    }
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = i18n("set.mcp.command");
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
    if (title) title.textContent = i18n("set.mcp.configure_custom");
    const sub = el("mcpCfgSub");
    if (sub) sub.textContent = i18n("set.mcp.new_server");
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = i18n("set.mcp.custom");
    const srcMeta = el("mcpCfgSourceMeta");
    if (srcMeta) srcMeta.textContent = i18n("set.mcp.enter_command");
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
    if (s.disabled) return i18n("set.mcp.off");
    if (s.status === "error") return s.error || i18n("set.mcp.error");
    const n = Number(s.tool_count) || 0;
    if (s.status === "running" || n > 0) {
      return i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n });
    }
    if (s.status === "stopped") return i18n("set.mcp.stopped");
    return s.status || i18n("set.mcp.installed");
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
      if (hint) hint.textContent = i18n("set.mcp.loading_tools");
      return;
    }
    if (!tools.length) {
      if (hint) {
        hint.textContent = i18n(s?.disabled ? "set.mcp.turn_on" : "set.mcp.no_tools");
      }
      return;
    }
    if (hint)
      hint.textContent = i18n(tools.length === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", {
        n: tools.length,
      });
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
      if (out) out.textContent = i18n("set.mcp.reloading");
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
      btn.textContent = `${i18n("set.mcp.cat." + cat.toLowerCase())} (${count})`;
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
      status.textContent = i18n("set.mcp.registry_loading");
      return;
    }
    const src = i18n(
      mcpCatalogSource === "registry"
        ? "set.mcp.src_registry"
        : mcpCatalogSource === "mixed"
          ? "set.mcp.src_mixed"
          : "set.mcp.src_local"
    );
    let text = src;
    const n = (mcpCatalog.entries || []).length;
    if (n) text += i18n("set.mcp.loaded_n", { n });
    if (mcpCatalogPrefetching) text += i18n("set.mcp.loading_more");
    if (mcpCatalogFilter.trim()) text += i18n("set.mcp.search_note", { q: mcpCatalogFilter.trim() });
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
        ? i18n("set.mcp.empty_loading")
        : i18n(mcpCatalogFilter.trim() ? "set.mcp.empty_search" : "set.mcp.empty_catalog");
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
        entry.installable === false
          ? i18n("set.mcp.kind_remote")
          : entry.source === "local"
            ? i18n("set.mcp.kind_featured")
            : "stdio";
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
          ? i18n("set.mcp.off")
          : Number(installed.tool_count) > 0
            ? i18n("set.mcp.tool_n", { n: installed.tool_count })
            : installed.status || i18n("set.mcp.installed");
        actions.appendChild(badge);
        const cfg = document.createElement("button");
        cfg.type = "button";
        cfg.className = "secondary";
        cfg.textContent = i18n("set.mcp.configure");
        cfg.addEventListener("click", () => {
          fillMcpForm(installed);
          setMcpTab("installed");
        });
        actions.appendChild(cfg);
      } else if (entry.installable === false || !entry.command) {
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = i18n("set.mcp.remote_only");
        badge.title = i18n("set.mcp.remote_only_title");
        actions.appendChild(badge);
      } else {
        const install = document.createElement("button");
        install.type = "button";
        install.className = "secondary";
        install.textContent = i18n(entry.envRequired ? "set.mcp.install_env" : "set.mcp.install");
        install.addEventListener("click", () => installCatalogEntry(entry));
        actions.appendChild(install);
      }
      if (entry.homepage) {
        const link = document.createElement("button");
        link.type = "button";
        link.className = "secondary";
        link.textContent = i18n("set.mcp.docs");
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
      if (out) out.textContent = i18n("set.mcp.remote_not_supported");
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
        out.textContent = i18n("set.mcp.fill_env", { name: form.title || form.name });
      }
      area("mcpEnv")?.focus();
      return;
    }
    if (out) out.textContent = i18n("set.mcp.installing", { name: form.name });
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
      empty.textContent = i18n("set.mcp.none_installed");
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
        del.title = i18n("set.mcp.remove_server");
        del.setAttribute("aria-label", i18n("set.mcp.remove_named", { name: s.name }));
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
        toggle.title = i18n(s.disabled ? "set.mcp.enable" : "set.mcp.disable");
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
    if (out) out.textContent = i18n("set.mcp.enter_command_done");
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

