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
    if (btn) btn.textContent = i18n(apiKeyVisible ? "set.prov.hide" : "set.prov.show");
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
        keyHint.textContent = i18n("set.prov.key.loaded");
      } else if (p.api_key_set) {
        keyHint.textContent = i18n("set.prov.key.saved");
      } else if (p.needs_key) {
        keyHint.textContent = i18n("set.prov.key.required");
      } else {
        keyHint.textContent = i18n("set.prov.key.none");
      }
    }
    const status = el("providerStatus");
    if (status) {
      if (p.active) {
        status.textContent = i18n("set.prov.status.active");
      } else if (p.ready) {
        status.textContent = i18n("set.prov.status.ready");
      } else if (p.needs_key && !p.api_key_set) {
        status.textContent = i18n("set.prov.status.need_key");
      } else if (p.custom && !p.api_base) {
        status.textContent = i18n("set.prov.status.need_base");
      } else {
        status.textContent = i18n("set.prov.status.none");
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
      empty.textContent = i18n("set.prov.loading");
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
      // The five buckets the core sorts providers into; the catalogue names
      // them again so the heading reads in the chosen language.
      head.textContent = i18n("set.prov.cat." + cat.toLowerCase());
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
      if (status) status.textContent = i18n("set.models.select_provider");
      return;
    }
    if (p.models_error) {
      if (status) status.textContent = i18n("set.models.failed", { detail: p.models_error });
    } else if (!p.ready) {
      if (status) status.textContent = i18n("set.models.configure_first");
    } else if (!p.models || !p.models.length) {
      if (status) status.textContent = i18n("set.models.none_returned");
    } else {
      if (status)
        status.textContent = i18n("set.models.count_from", {
          n: p.models.length,
          provider: p.name || p.key,
        });
    }
    const models = p.models || [];
    const q = (modelSearchFilter || "").trim().toLowerCase();
    const filtered = q ? models.filter((m) => (m.id || "").toLowerCase().includes(q)) : models;
    if (!filtered.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = q
        ? i18n("set.models.no_match", { q: modelSearchFilter })
        : i18n(p.ready ? "set.models.none_listed" : "set.models.not_ready");
      list.appendChild(empty);
      return;
    }
    if (status && filtered.length !== models.length) {
      status.textContent = i18n("set.models.filtered", {
        shown: filtered.length,
        total: models.length,
      });
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
      if (isActive)
        badge.textContent = ctx ? i18n("set.models.active_ctx", { ctx }) : i18n("set.models.active");
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
    // orchestraSavePayload() lives in 03-orchestra.js (same bundle scope):
    // the General tab contains the Orchestra routing section, so its Save
    // must persist the role/model picks too — otherwise pushState() would
    // re-render from disk and silently wipe the unsaved selections.
    const orch = orchestraSavePayload();
    vscode.postMessage({
      type: "saveGeneral",
      binaryPath: input("binaryPath")?.value || "",
      projectRoot: input("projectRoot")?.value || "",
      ...(orch || {}),
    });
  });

  el("saveModels")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "saveModels", ...modelsPayload() });
  });

  el("refreshModels")?.addEventListener("click", () => {
    showError("");
    const status = el("modelsStatus");
    if (status) status.textContent = i18n("set.models.refreshing");
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

