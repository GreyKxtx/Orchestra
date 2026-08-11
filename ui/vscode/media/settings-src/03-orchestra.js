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
      title.textContent = role.label || role.key;
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
