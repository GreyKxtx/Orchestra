  function escapeAttr(value) {
    return String(value || "")
      .replace(/&/g, "&amp;")
      .replace(/"/g, "&quot;")
      .replace(/</g, "&lt;");
  }

  function fileExtLabel(nameOrExt) {
    const raw = (nameOrExt || "").trim();
    if (!raw) {
      return "FILE";
    }
    if (raw.includes(".")) {
      const ext = raw.split(".").pop() || "";
      return ext.slice(0, 4).toUpperCase() || "FILE";
    }
    return raw.slice(0, 4).toUpperCase();
  }

  function relPathDisplay(fullPath) {
    if (!fullPath) {
      return "";
    }
    const parts = fullPath.replace(/\\/g, "/").split("/");
    if (parts.length <= 2) {
      return fullPath;
    }
    return parts.slice(-3).join("/");
  }

  function addFileRef(f) {
    if (!f || typeof f.name !== "string") {
      return;
    }
    const key = f.path || f.name;
    if (files.some((x) => (x.path || x.name) === key)) {
      return;
    }
    files.push({
      name: f.name,
      path: typeof f.path === "string" ? f.path : undefined,
      ext: typeof f.ext === "string" ? f.ext : undefined,
      kind: typeof f.kind === "string" ? f.kind : undefined,
      previewUri: typeof f.previewUri === "string" ? f.previewUri : undefined,
    });
    renderFiles();
  }

  function truncateTabTitle(title) {
    const t = (title || i18n("chrome.new_chat")).trim() || i18n("chrome.new_chat");
    return t.length > 22 ? t.slice(0, 20) + "…" : t;
  }

  /** @param {{ id: string; title?: string; model?: string }[]} tabs */
  function renderSessionTabs(tabs, activeId) {
    if (!sessionTabsEl) {
      return;
    }
    activeSessionId = activeId || "";
    sessionTabsEl.innerHTML = "";
    const list = Array.isArray(tabs) ? tabs : [];
    if (list.length === 0) {
      const empty = document.createElement("div");
      empty.className = "session-tabs-empty";
      empty.textContent = i18n("chrome.new_chat");
      sessionTabsEl.appendChild(empty);
      return;
    }
    list.forEach((s) => {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = "session-tab" + (s.id === activeId ? " active" : "");
      tab.setAttribute("role", "tab");
      tab.setAttribute("aria-selected", s.id === activeId ? "true" : "false");
      tab.setAttribute("data-session-id", s.id);
      tab.title = s.title || s.id;

      const icon = document.createElement("span");
      icon.className = "session-tab-icon";
      icon.innerHTML =
        '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
        '<path d="M7 9.5h10M7 13.5h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>' +
        '<path d="M5 5.5h14a2 2 0 012 2v8.5a2 2 0 01-2 2H9.5L6 21v-3H5a2 2 0 01-2-2V7.5a2 2 0 012-2z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>' +
        "</svg>";

      const label = document.createElement("span");
      label.className = "session-tab-label";
      label.textContent = truncateTabTitle(s.title);

      const close = document.createElement("span");
      close.className = "session-tab-close";
      close.setAttribute("data-close-session", s.id);
      close.setAttribute("role", "button");
      close.setAttribute("aria-label", i18n("tab.close"));
      close.textContent = "×";

      tab.appendChild(icon);
      tab.appendChild(label);
      tab.appendChild(close);
      sessionTabsEl.appendChild(tab);
    });

    const activeTab = sessionTabsEl.querySelector(".session-tab.active");
    if (activeTab && typeof activeTab.scrollIntoView === "function") {
      activeTab.scrollIntoView({ block: "nearest", inline: "nearest" });
    }
  }

  function updateActiveTabTitle(title) {
    if (!sessionTabsEl || !activeSessionId) {
      return;
    }
    const tab = sessionTabsEl.querySelector(`[data-session-id="${activeSessionId}"]`);
    if (!tab) {
      return;
    }
    const label = tab.querySelector(".session-tab-label");
    if (label) {
      label.textContent = truncateTabTitle(title);
    }
    tab.title = title || activeSessionId;
  }

  /** Short label for long provider/model ids (Cursor-style). */
  function shortModel(id) {
    if (!id) {
      return "Model";
    }
    const parts = String(id).split(/[/\\]/);
    const last = parts[parts.length - 1] || id;
    return last.length > 28 ? last.slice(0, 25) + "…" : last;
  }

  function setModelLabel(id) {
    currentModel = id || currentModel;
    if (modelLabelEl) {
      modelLabelEl.textContent = shortModel(currentModel);
      modelLabelEl.title = currentModel || "";
    }
  }

  function currentMode() {
    return MODES.find((m) => m.id === modeId) || MODES[0];
  }

  function currentEffort() {
    return EFFORTS.find((e) => e.id === effortId) || EFFORTS[1];
  }

  function effectiveProfile() {
    if (fastOn) {
      return "fast";
    }
    return currentEffort().profile;
  }

  function currentAccess() {
    return ACCESS_MODES.find((m) => m.id === accessId) || ACCESS_MODES[0];
  }

  function initAccessMenu() {
    if (!accessMenu) {
      return;
    }
    accessMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = i18n("access.section");
    accessMenu.appendChild(head);
    ACCESS_MODES.forEach((m) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item access-item";
      btn.dataset.access = m.id;
      btn.title = i18n(m.hintKey);
      btn.innerHTML =
        `<span class="mi access-icon access-${escapeAttr(m.id)}">${escapeAttr(m.icon)}</span>` +
        `<span class="access-item-text"><span class="access-item-label">${escapeAttr(m.label)}</span>` +
        `<span class="access-item-hint">${escapeAttr(i18n(m.hintKey))}</span></span>`;
      accessMenu.appendChild(btn);
    });
    const note = document.createElement("div");
    note.className = "menu-hint access-menu-note";
    note.textContent =
      i18n("access.note");
    accessMenu.appendChild(note);
    const optHead = document.createElement("div");
    optHead.className = "menu-section";
    optHead.textContent = i18n("access.tools.section");
    accessMenu.appendChild(optHead);
    const browserRow = document.createElement("div");
    browserRow.className = "menu-row menu-row-browser";
    browserRow.title =
      i18n("access.browser.hint");
    browserRow.innerHTML =
      `<span class="menu-row-label"><span class="mi" aria-hidden="true">◎</span>${escapeAttr(i18n("access.browser.label"))}</span>` +
      `<button type="button" id="browser-toggle" class="toggle" role="switch" aria-checked="false" aria-label="${escapeAttr(i18n("access.browser.label"))}"></button>`;
    accessMenu.appendChild(browserRow);
  }

  function syncAccessUi() {
    const m = currentAccess();
    if (accessLabel) {
      accessLabel.textContent = m.label;
    }
    const icon = document.getElementById("access-icon");
    if (icon) {
      icon.textContent = m.icon;
      icon.className = `ico access-icon access-${m.id}`;
    }
    if (accessBtn) {
      accessBtn.dataset.access = accessId;
      accessBtn.title = i18n(m.hintKey);
    }
    accessMenu?.querySelectorAll("[data-access]").forEach((el) => {
      const id = el.getAttribute("data-access");
      el.classList.toggle("selected", id === accessId);
    });
    const browserToggle = document.getElementById("browser-toggle");
    if (browserToggle) {
      browserToggle.classList.toggle("on", browserOn);
      browserToggle.setAttribute("aria-checked", browserOn ? "true" : "false");
    }
    if (accessBtn) {
      accessBtn.title = browserOn
        ? i18n("access.browser.on", { hint: i18n(m.hintKey) })
        : i18n(m.hintKey);
    }
    host.setState({ ...(host.getState() || {}), accessId, browserOn });
  }

  function statsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  function langFromPath(filePath) {
    const ext = (filePath || "").split(".").pop()?.toLowerCase() || "";
    const map = {
      go: "go",
      ts: "ts",
      tsx: "tsx",
      js: "js",
      jsx: "jsx",
      py: "py",
      rs: "rust",
      md: "md",
      json: "json",
      yml: "yaml",
      yaml: "yaml",
      css: "css",
      html: "html",
      sh: "bash",
    };
    return map[ext] || "plain";
  }

