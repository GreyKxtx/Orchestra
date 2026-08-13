  function initModeMenu() {
    if (!modeMenu) {
      return;
    }
    modeMenu.innerHTML = "";
    const byId = new Map(MODES.map((m) => [m.id, m]));
    MODE_GROUPS.forEach((group) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = group.label;
      modeMenu.appendChild(head);
      group.ids.forEach((id) => {
        const m = byId.get(id);
        if (!m) {
          return;
        }
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item";
        btn.dataset.id = m.id;
        btn.title = m.mode;
        btn.innerHTML =
          `<span class="mi mode-icon mode-${escapeAttr(m.id)}">${escapeAttr(m.icon)}</span>${escapeAttr(m.label)}`;
        modeMenu.appendChild(btn);
      });
    });
  }

  function syncModeUi() {
    const m = currentMode();
    if (modeLabel) {
      modeLabel.textContent = m.label;
    }
    const icon = document.getElementById("mode-icon");
    if (icon) {
      icon.textContent = m.icon;
      icon.className = `ico mode-icon mode-${m.id}`;
    }
    if (modeBtn) {
      modeBtn.dataset.mode = modeId;
    }
    const app = document.getElementById("app");
    if (app) {
      app.dataset.mode = modeId;
    }
    modeMenu?.querySelectorAll(".menu-item").forEach((el) => {
      const id = el.getAttribute("data-id");
      el.classList.toggle("selected", id === modeId);
    });
    if (orchConfigBtn) {
      orchConfigBtn.hidden = modeId !== "orchestra";
    }
    // Orchestra routes work across tiers, so a single-model pill would lie
    // about which model actually runs. Swap it for the tier breakdown.
    if (modeId === "orchestra") {
      renderOrchestraPill();
      vscode.postMessage({ type: "listOrchestraRoles" });
    } else if (modelLabelEl) {
      setModelLabel(currentModel);
      if (modelPill) modelPill.title = "Model";
    }
  }

  /** Models actually configured for one orchestra role. @param {any} r */
  function orchRoleModels(r) {
    if (Array.isArray(r?.models) && r.models.length) return r.models.filter(Boolean);
    return r?.model ? [r.model] : [];
  }

  /** Footer pill in orchestra mode: "L5 <planner> +N" + full map in tooltip. */
  function renderOrchestraPill() {
    if (!modelLabelEl) return;
    const roles = orchestraRolesInfo?.roles || [];
    const planner = roles.find((r) => r.key === "planner");
    const plannerModels = planner ? orchRoleModels(planner) : [];
    const others = roles.filter((r) => r.key !== "planner" && orchRoleModels(r).length > 0);
    if (!roles.length) {
      modelLabelEl.textContent = "Orchestra tiers";
      modelLabelEl.title = "Loading tier map…";
      if (modelPill) modelPill.title = "Orchestra tier models";
      return;
    }
    const base = plannerModels.length
      ? `L5 ${shortModel(plannerModels[0])}`
      : "L5 not set";
    modelLabelEl.textContent = others.length ? `${base} +${others.length}` : base;
    const lines = roles.map((r) => {
      const models = orchRoleModels(r);
      const tier = r.tier ? `${r.tier} · ` : "";
      return `${tier}${r.label}: ${models.length ? models.join(", ") : "— (main model fallback)"}`;
    });
    modelLabelEl.title = lines.join("\n");
    if (modelPill) modelPill.title = "Orchestra tier models";
  }

  /** Read-only tier → models breakdown inside the model dropdown. */
  function renderOrchestraRolesMenu() {
    if (!modelMenuList) return;
    if (modelMenuTitle) modelMenuTitle.textContent = "Orchestra tiers";
    if (modelMenuSearch) modelMenuSearch.style.display = "none";
    modelMenuList.innerHTML = "";
    const roles = orchestraRolesInfo?.roles || [];
    if (!roles.length) {
      const hint = document.createElement("div");
      hint.className = "menu-hint";
      hint.textContent = "Loading tier map…";
      modelMenuList.appendChild(hint);
    }
    roles.forEach((r) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = r.tier ? `${r.label} · ${r.tier}` : r.label;
      modelMenuList.appendChild(head);
      const models = orchRoleModels(r);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = "not set — falls back to the main model";
        modelMenuList.appendChild(empty);
        return;
      }
      models.forEach((id, i) => {
        const row = document.createElement("div");
        row.className = "menu-hint orch-tier-model";
        row.textContent = i === 0 ? id : `${id} (failover ${i + 1})`;
        row.title = id;
        modelMenuList.appendChild(row);
      });
    });
    const cfg = document.createElement("button");
    cfg.type = "button";
    cfg.className = "menu-item";
    cfg.setAttribute("data-model-action", "configure-orchestra");
    cfg.textContent = "Configure tiers…";
    modelMenuList.appendChild(cfg);
  }

  function effortMeterHtml(id) {
    const bars = id === "low" ? 1 : id === "medium" ? 2 : 3;
    let inner = "";
    for (let b = 0; b < bars; b++) {
      inner += "<i></i>";
    }
    return `<span class="effort-meter effort-${escapeAttr(id)}">${inner}</span>`;
  }

  /** @param {HTMLElement} el @param {string} id @param {boolean} [pill] */
  function setEffortIconEl(el, id, pill) {
    el.className = pill ? `ico effort-icon effort-${id}` : `mi effort-icon effort-${id}`;
    el.innerHTML = effortMeterHtml(id);
  }

  function initEffortMenu() {
    if (!effortMenu) {
      return;
    }
    effortMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = "Effort";
    effortMenu.appendChild(head);
    EFFORTS.forEach((e) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.dataset.effort = e.id;
      btn.innerHTML = `<span class="mi effort-icon effort-${escapeAttr(e.id)}">${effortMeterHtml(e.id)}</span>${escapeAttr(e.label)}`;
      effortMenu.appendChild(btn);
    });
    const optHead = document.createElement("div");
    optHead.className = "menu-section";
    optHead.textContent = "Options";
    effortMenu.appendChild(optHead);
    const fastRow = document.createElement("div");
    fastRow.className = "menu-row menu-row-fast";
    fastRow.innerHTML =
      '<span class="menu-row-label"><span class="mi effort-icon effort-fast" aria-hidden="true">⚡</span>Fast</span>' +
      '<button type="button" id="fast-toggle" class="toggle" role="switch" aria-checked="false"></button>';
    effortMenu.appendChild(fastRow);
    fastToggleRef.el = /** @type {HTMLButtonElement | null} */ (document.getElementById("fast-toggle"));
  }

  function syncEffortUi() {
    const e = currentEffort();
    if (effortLabel) {
      effortLabel.textContent = e.label;
    }
    const icon = document.getElementById("effort-icon");
    if (icon) {
      setEffortIconEl(icon, e.id, true);
    }
    if (effortBtn) {
      effortBtn.dataset.effort = effortId;
      effortBtn.title = fastOn ? `${e.label} · Fast profile` : e.label;
    }
    const fastMark = document.getElementById("effort-fast-mark");
    if (fastMark) {
      fastMark.hidden = !fastOn;
    }
    effortMenu?.querySelectorAll("[data-effort]").forEach((el) => {
      const id = el.getAttribute("data-effort");
      el.classList.toggle("selected", id === effortId);
    });
    const toggle = fastToggleRef.el;
    if (toggle) {
      toggle.classList.toggle("on", fastOn);
      toggle.setAttribute("aria-checked", fastOn ? "true" : "false");
    }
    effortMenu?.querySelector(".menu-row-fast")?.classList.toggle("fast-on", fastOn);
  }

  function ensureAssistant() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!assistantBubble) {
      assistantBubble = document.createElement("div");
      assistantBubble.className = "turn-text";
      inner.appendChild(assistantBubble);
    }
    return assistantBubble;
  }

  function renderFiles() {
    if (!filesEl) {
      return;
    }
    filesEl.innerHTML = "";
    filesEl.classList.toggle("has-files", files.length > 0);
    files.forEach((f, idx) => {
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "file-attach" + (isImage ? " file-attach-image" : "");

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "file-attach-thumb";
        img.src = f.previewUri;
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "file-attach-meta";
      const name = document.createElement("span");
      name.className = "file-attach-name";
      name.textContent = f.name;
      name.title = f.path || f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "file-attach-remove";
      rm.setAttribute("aria-label", "Remove file");
      rm.textContent = "×";
      rm.addEventListener("click", (e) => {
        e.stopPropagation();
        files.splice(idx, 1);
        renderFiles();
      });
      card.appendChild(rm);
      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        if (e.target === rm || rm.contains(/** @type {Node} */ (e.target))) return;
        onAttachmentClick(f, isImage, e);
      });
      filesEl.appendChild(card);
    });
  }

  function send() {
    if (busy) {
      vscode.postMessage({ type: "cancelTurn" });
      return;
    }
    if (!inputEl) {
      return;
    }
    let text = inputEl.value.trim();
    if (!text && files.length === 0) {
      return;
    }
    if (text.startsWith("/") && !text.includes("\n")) {
      const cmd = text.split(/\s+/)[0].toLowerCase();
      const arg = text.slice(cmd.length).trim() || undefined;
      inputEl.value = "";
      hidePalette();
      autoGrow();
      vscode.postMessage({ type: "slashCommand", cmd, arg });
      return;
    }
    const payload = {
      type: "send",
      text,
      mode: currentMode().mode,
      profile: effectiveProfile(),
      apply: false,
      allowExec: accessId === "auto",
      files: files.map((f) => ({
        name: f.name,
        path: f.path,
        ext: f.ext,
        kind: f.kind,
        previewUri: f.previewUri,
      })),
    };
    inputEl.value = "";
    autoGrow();
    files = [];
    renderFiles();
    closeMenus();
    vscode.postMessage(payload);
  }

  function autoGrow() {
    if (!inputEl) {
      return;
    }
    inputEl.style.height = "auto";
    inputEl.style.height = Math.min(140, Math.max(44, inputEl.scrollHeight)) + "px";
  }

  function positionAccessMenu() {
    if (!accessMenu || !accessBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = accessBtn.getBoundingClientRect();
    accessMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    accessMenu.style.right = "auto";
  }

  function positionEffortMenu() {
    if (!effortMenu || !effortBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = effortBtn.getBoundingClientRect();
    effortMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    effortMenu.style.right = "auto";
  }

  function positionModelMenu() {
    if (!modelMenu || !modelPill || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = modelPill.getBoundingClientRect();
    modelMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    modelMenu.style.right = "auto";
  }

  function renderProviderModels(catalog) {
    if (!modelMenuList) return;
    providerModelsCatalog = catalog;
    modelMenuList.innerHTML = "";
    const providers = Array.isArray(catalog.providers) ? catalog.providers : [];
    const activeModel = catalog.activeModel || currentModel;
    const q = (modelMenuFilter || "").trim().toLowerCase();
    if (!providers.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = "No providers — open Settings";
      modelMenuList.appendChild(btn);
      return;
    }
    let shown = 0;
    providers.forEach((p) => {
      const models = Array.isArray(p.models) ? p.models : [];
      const filtered = q
        ? models.filter((m) => (m.id || "").toLowerCase().includes(q))
        : models;
      if (!filtered.length && models.length && q) {
        return;
      }
      const head = document.createElement("div");
      head.className = "menu-section";
      const count = filtered.length || models.length;
      head.textContent = `${p.name || p.key}${count ? ` (${count})` : ""}`;
      modelMenuList.appendChild(head);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = p.models_error || (p.ready ? "No models" : "Not configured");
        modelMenuList.appendChild(empty);
        return;
      }
      if (!filtered.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = "No matches";
        modelMenuList.appendChild(empty);
        return;
      }
      filtered.forEach((m) => {
        shown++;
        const id = m.id || "";
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item" + (id === activeModel && p.active ? " selected" : "");
        btn.setAttribute("data-model", id);
        btn.setAttribute("data-provider", p.key);
        btn.innerHTML = id;
        btn.title = id;
        modelMenuList.appendChild(btn);
      });
    });
    if (shown === 0 && q) {
      const empty = document.createElement("div");
      empty.className = "menu-hint";
      empty.textContent = `No models match “${modelMenuFilter}”`;
      modelMenuList.appendChild(empty);
    }
  }

  function renderModelList(models, current) {
    if (!modelMenuList) {
      return;
    }
    if (current) {
      setModelLabel(current);
    }
    modelMenuList.innerHTML = "";
    if (!models.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = "No models — retry";
      modelMenuList.appendChild(btn);
      return;
    }
    models.forEach((m) => {
      const id = typeof m === "string" ? m : m.id;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item" + (id === currentModel ? " selected" : "");
      btn.setAttribute("data-model", id);
      btn.innerHTML = id;
      btn.title = id;
      modelMenuList.appendChild(btn);
    });
  }

  contextBtn?.addEventListener("mouseenter", () => showContextPopover(true));
  contextBtn?.addEventListener("mouseleave", () => {
    window.setTimeout(() => {
      if (!contextPopover?.matches(":hover")) showContextPopover(false);
    }, 120);
  });
  contextPopover?.addEventListener("mouseenter", () => showContextPopover(true));
  contextPopover?.addEventListener("mouseleave", () => showContextPopover(false));
  contextBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    showContextPopover(!ctxPopoverOpen);
  });

  sendBtn?.addEventListener("click", send);
  inputEl?.addEventListener("keydown", (e) => {
    if (paletteMode && paletteItems.length && (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Tab")) {
      e.preventDefault();
      if (e.key === "ArrowDown") {
        paletteIndex = (paletteIndex + 1) % paletteItems.length;
      } else if (e.key === "ArrowUp") {
        paletteIndex = (paletteIndex - 1 + paletteItems.length) % paletteItems.length;
      }
      paletteMenu?.querySelectorAll(".palette-item").forEach((el, i) => {
        el.classList.toggle("selected", i === paletteIndex);
      });
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (paletteMode && paletteItems.length) {
        applyPaletteItem(paletteIndex);
        return;
      }
      send();
    }
    if (e.key === "Escape") {
      closeMenus();
    }
  });
  inputEl?.addEventListener("input", () => {
    autoGrow();
    onInputPalette();
  });

  modeBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modeMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      modeMenu?.classList.add("open");
      modeBtn.classList.add("open");
    }
  });

  accessBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !accessMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionAccessMenu();
      accessMenu?.classList.add("open");
      accessBtn.classList.add("open");
    }
  });

  accessMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const item = /** @type {HTMLElement | null} */ (e.target.closest("[data-access]"));
    if (!item || !accessMenu.contains(item)) {
      return;
    }
    const id = item.getAttribute("data-access");
    if (id) {
      accessId = id;
      syncAccessUi();
      closeMenus();
    }
  });

  effortBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !effortMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionEffortMenu();
      effortMenu?.classList.add("open");
      effortBtn.classList.add("open");
    }
  });

  modeMenu?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest(".menu-item");
    if (!item) {
      return;
    }
    const id = item.getAttribute("data-id");
    if (!id) {
      return;
    }
    modeId = id;
    syncModeUi();
    vscode.setState({ ...(vscode.getState() || {}), modeId });
    closeMenus();
  });

  effortMenu?.addEventListener("click", (e) => {
    const effortItem = /** @type {HTMLElement | null} */ (e.target.closest("[data-effort]"));
    if (effortItem && effortMenu.contains(effortItem)) {
      const id = effortItem.getAttribute("data-effort");
      if (id) {
        effortId = id;
        syncEffortUi();
        closeMenus();
      }
      return;
    }
    if (/** @type {HTMLElement} */ (e.target).closest("#fast-toggle")) {
      e.stopPropagation();
      fastOn = !fastOn;
      syncEffortUi();
    }
  });

  attachBtn?.addEventListener("click", () => {
    vscode.postMessage({ type: "attach" });
  });

  sessionNewBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    vscode.postMessage({ type: "newSession" });
  });

  sessionHistoryBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !sessionMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      sessionMenu?.classList.add("open");
      sessionHistoryBtn.classList.add("open");
      vscode.postMessage({ type: "listSessions" });
    }
  });

  sessionTabsEl?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const closeEl = t.closest("[data-close-session]");
    if (closeEl) {
      e.stopPropagation();
      const sid = closeEl.getAttribute("data-close-session");
      if (sid) {
        vscode.postMessage({ type: "closeSession", sessionId: sid });
      }
      return;
    }
    const tab = t.closest(".session-tab[data-session-id]");
    if (!tab) {
      return;
    }
    const sid = tab.getAttribute("data-session-id");
    if (sid && sid !== activeSessionId) {
      vscode.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  pendingApplyBtn?.addEventListener("click", () => applyPendingChanges());
  pendingRejectBtn?.addEventListener("click", () => discardPendingChanges());

  diffViewerCloseBtn?.addEventListener("click", hideDiffViewer);
  diffViewerEditorBtn?.addEventListener("click", () => {
    if (!diffViewerState.path) return;
    openDiffMessage(diffViewerState.path, diffViewerState.before, diffViewerState.after, false);
  });
  diffViewer?.addEventListener("click", (e) => {
    if (e.target === diffViewer) hideDiffViewer();
  });

  imagePreviewCloseBtn?.addEventListener("click", hideImagePreview);
  imagePreviewPrevBtn?.addEventListener("click", () => shiftImagePreview(-1));
  imagePreviewNextBtn?.addEventListener("click", () => shiftImagePreview(1));
  imagePreviewOpenBtn?.addEventListener("click", () => {
    const item = imagePreviewState.items[imagePreviewState.index];
    if (!item?.path) return;
    hideImagePreview();
    openExternalFile(item.path, true);
  });
  imagePreview?.addEventListener("click", (e) => {
    if (e.target === imagePreview) hideImagePreview();
  });

  composerWrap?.addEventListener("dragover", (e) => {
    e.preventDefault();
    composerWrap.classList.add("drag-over");
  });
  composerWrap?.addEventListener("dragleave", () => {
    composerWrap.classList.remove("drag-over");
  });
  composerWrap?.addEventListener("drop", (e) => {
    e.preventDefault();
    composerWrap.classList.remove("drag-over");
    void attachFilesFromDataTransfer(e.dataTransfer);
  });

  inputEl?.addEventListener("paste", (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    let handledImage = false;
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (!file) continue;
        if (file.size > MAX_ATTACH_BYTES) {
          appendMsg("system", "Pasted image exceeds 20 MB limit");
          continue;
        }
        handledImage = true;
        e.preventDefault();
        void (async () => {
          try {
            const dataBase64 = await readFileAsBase64(file);
            vscode.postMessage({
              type: "attachBytes",
              name: file.name || "paste.png",
              mime: item.type,
              dataBase64,
            });
          } catch {
            /* ignore */
          }
        })();
      }
    }
    if (handledImage) {
      e.preventDefault();
    }
  });

  modelPill?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modelMenu?.classList.contains("open");
    closeMenus();
    if (open && modeId === "orchestra") {
      renderOrchestraRolesMenu();
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      vscode.postMessage({ type: "listOrchestraRoles" });
      return;
    }
    if (open) {
      if (modelMenuTitle) modelMenuTitle.textContent = "Models";
      if (modelMenuSearch) modelMenuSearch.style.display = "";
      modelMenuFilter = "";
      if (modelMenuSearch) {
        modelMenuSearch.value = "";
      }
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      vscode.postMessage({ type: "listProviderModels" });
      setTimeout(() => modelMenuSearch?.focus(), 30);
    }
  });

  modelMenuSearch?.addEventListener("input", () => {
    modelMenuFilter = modelMenuSearch?.value || "";
    if (providerModelsCatalog) {
      renderProviderModels(providerModelsCatalog);
    }
  });

  modelMenuSearch?.addEventListener("click", (e) => e.stopPropagation());

  orchConfigBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    vscode.postMessage({ type: "openOrchestraSettings" });
  });

  settingsBtn?.addEventListener("click", () => {
    vscode.postMessage({ type: "openSettings" });
  });

  todosChip?.addEventListener("click", (e) => {
    e.stopPropagation();
    todosExpanded = !todosExpanded;
    renderTodos();
  });

  sessionMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    // Delete is nested inside the row — check it first so the click does not
    // also open the session. The menu stays open; the host pushes a fresh list.
    const delEl = t.closest("[data-delete-session]");
    if (delEl) {
      const delId = delEl.getAttribute("data-delete-session");
      if (delId) {
        vscode.postMessage({ type: "deleteSession", sessionId: delId });
      }
      return;
    }
    const item = t.closest("[data-session-action], [data-session-id]");
    if (!item) {
      return;
    }
    const action = item.getAttribute("data-session-action");
    if (action === "new") {
      closeMenus();
      vscode.postMessage({ type: "newSession" });
      return;
    }
    const sid = item.getAttribute("data-session-id");
    if (sid) {
      closeMenus();
      vscode.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  modelMenuList?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest("[data-model], [data-model-action]");
    if (!item) {
      return;
    }
    if (item.getAttribute("data-model-action") === "refresh") {
      vscode.postMessage({ type: "listProviderModels" });
      return;
    }
    if (item.getAttribute("data-model-action") === "configure-orchestra") {
      closeMenus();
      vscode.postMessage({ type: "openOrchestraSettings" });
      return;
    }
    const model = item.getAttribute("data-model");
    const provider = item.getAttribute("data-provider") || undefined;
    if (model) {
      closeMenus();
      vscode.postMessage({ type: "setModel", model, provider });
    }
  });

  paletteMenu?.addEventListener("click", (e) => e.stopPropagation());
  document.addEventListener("click", () => closeMenus());
  modeMenu?.addEventListener("click", (e) => e.stopPropagation());
  effortMenu?.addEventListener("click", (e) => e.stopPropagation());
  sessionMenu?.addEventListener("click", (e) => e.stopPropagation());
  modelMenu?.addEventListener("click", (e) => e.stopPropagation());

