  function runStatusLabel() {
    const hint = chromeHint?.textContent?.trim();
    if (hint && !chromeHint.classList.contains("hidden")) {
      return hint;
    }
    let base = busyStatusText || "Working…";
    if (busy && sendQueue.length > 0) {
      const n = sendQueue.length;
      base += ` · ${n} queued`;
    }
    return base;
  }

  function renderSendQueue() {
    if (!messageQueueEl) {
      return;
    }
    if (!sendQueue.length) {
      messageQueueEl.classList.add("hidden");
      messageQueueEl.replaceChildren();
      return;
    }
    messageQueueEl.classList.remove("hidden");
    messageQueueEl.replaceChildren();
    const head = document.createElement("div");
    head.className = "queue-head";
    head.textContent =
      sendQueue.length === 1 ? "1 message queued" : `${sendQueue.length} messages queued`;
    messageQueueEl.appendChild(head);
    sendQueue.forEach((item, idx) => {
      const row = document.createElement("div");
      row.className = "queue-item";
      row.dataset.queueId = item.id;
      const pos = document.createElement("span");
      pos.className = "queue-pos";
      pos.textContent = String(idx + 1);
      const text = document.createElement("span");
      text.className = "queue-text";
      const preview = (item.preview || "").trim();
      text.textContent = preview || (item.fileCount ? `${item.fileCount} attachment(s)` : "…");
      text.title = preview;
      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "queue-cancel";
      rm.setAttribute("aria-label", "Remove from queue");
      rm.textContent = "×";
      rm.addEventListener("click", () => {
        vscode.postMessage({ type: "cancelQueuedSend", id: item.id });
      });
      row.appendChild(pos);
      row.appendChild(text);
      row.appendChild(rm);
      messageQueueEl.appendChild(row);
    });
    updateBusyUi();
  }

  function beginTurn() {
    assistantBubble = null;
    resetTurnState();
    toolBlocks.clear();
    toolArgs.clear();
    execSteps.clear();
  }

  function setTypingIndicator(show, label) {
    if (!messagesEl) {
      return;
    }
    if (!show) {
      typingIndicatorEl?.remove();
      typingIndicatorEl = null;
      return;
    }
    if (!typingIndicatorEl) {
      typingIndicatorEl = document.createElement("div");
      typingIndicatorEl.className = "msg typing-indicator";
      typingIndicatorEl.setAttribute("aria-label", "Assistant is working");
      typingIndicatorEl.innerHTML =
        '<span class="typing-dots" aria-hidden="true">' +
        '<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>' +
        "</span>" +
        '<span class="typing-label"></span>';
      messagesEl.appendChild(typingIndicatorEl);
    }
    const lab = typingIndicatorEl.querySelector(".typing-label");
    if (lab) {
      lab.textContent = label || "Working…";
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function updateBusyUi() {
    const label = runStatusLabel();
    if (composerStatus) {
      composerStatus.classList.toggle("hidden", !busy);
      composerStatus.setAttribute("aria-busy", busy ? "true" : "false");
    }
    if (composerStatusLabel) {
      composerStatusLabel.textContent = label;
    }
    if (composerWrap) {
      composerWrap.classList.toggle("composer-busy", busy);
    }
    if (sendBtn) {
      sendBtn.classList.toggle("is-busy", busy);
      sendBtn.setAttribute("aria-busy", busy ? "true" : "false");
      sendBtn.title = busy ? "Stop" : "Send";
      sendBtn.setAttribute("aria-label", busy ? "Stop" : "Send");
    }
    setTypingIndicator(busy && !assistantBubble?.textContent?.trim(), label);
  }

  function setChromeHint(text, isError) {
    if (!chromeHint) {
      return;
    }
    const t = (text || "").trim();
    if (!t) {
      chromeHint.textContent = "";
      chromeHint.classList.add("hidden");
      chromeHint.classList.remove("error");
      if (busy) {
        busyStatusText = "Working…";
        updateBusyUi();
      }
      return;
    }
    chromeHint.textContent = t;
    chromeHint.classList.remove("hidden");
    chromeHint.classList.toggle("error", Boolean(isError));
    if (busy && !isError) {
      busyStatusText = t;
      updateBusyUi();
    }
  }

  function setBusy(next) {
    busy = next;
    if (contextBtn) {
      contextBtn.classList.toggle("busy", next);
    }
    if (next) {
      if (!busyStatusText) {
        busyStatusText = "Working…";
      }
    } else {
      busyStatusText = "Working…";
    }
    updateBusyUi();
    if (todos.length) {
      renderTodos();
    }
    if (!next) {
      if (!todosDoneFlashTimer) {
        if (chromeHint) {
          chromeHint.textContent = "";
          chromeHint.classList.add("hidden");
          chromeHint.classList.remove("error");
        }
      }
      if (workflowActiveName) {
        setWorkflow("", false);
      }
    }
  }

  function flashTodosDone() {
    setChromeHint("✓ Tasks done", false);
    clearTimeout(todosDoneFlashTimer);
    todosDoneFlashTimer = window.setTimeout(() => {
      todosDoneFlashTimer = 0;
      setChromeHint("", false);
    }, 2500);
  }

  function truncateTodoLabel(text, max) {
    const t = (text || "").trim();
    if (t.length <= max) {
      return t;
    }
    return t.slice(0, max - 1) + "…";
  }

  function closeMenus() {
    modeMenu?.classList.remove("open");
    effortMenu?.classList.remove("open");
    accessMenu?.classList.remove("open");
    sessionMenu?.classList.remove("open");
    modelMenu?.classList.remove("open");
    modeBtn?.classList.remove("open");
    effortBtn?.classList.remove("open");
    accessBtn?.classList.remove("open");
    sessionHistoryBtn?.classList.remove("open");
    modelPill?.classList.remove("open");
    hidePalette();
  }

  function hidePalette() {
    paletteMode = "";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu?.classList.add("hidden");
    if (paletteMenu) paletteMenu.innerHTML = "";
  }

  function detectPaletteQuery(text) {
    const t = text || "";
    const slash = t.match(/(?:^|\s)(\/[\w-]*)$/);
    if (slash) {
      return { mode: "slash", query: slash[1].slice(1).toLowerCase() };
    }
    const at = t.match(/(?:^|\s)@([^\s@]*)$/);
    if (at) {
      return { mode: "mention", query: at[1] };
    }
    return null;
  }

  function filterSlash(query) {
    const q = (query || "").toLowerCase();
    return SLASH_CMDS.concat(SKILL_CMDS).filter((c) => c.cmd.slice(1).startsWith(q));
  }

  function renderPalette(mode, items) {
    if (!paletteMenu) return;
    paletteMode = mode;
    paletteItems = items;
    paletteIndex = 0;
    paletteMenu.innerHTML = "";
    if (!items.length) {
      if (mode === "mention") {
        const head = document.createElement("div");
        head.className = "menu-section palette-head";
        head.textContent = "Files";
        paletteMenu.appendChild(head);
        const empty = document.createElement("div");
        empty.className = "palette-empty";
        empty.textContent = "No files found";
        paletteMenu.appendChild(empty);
        paletteMenu.classList.remove("hidden");
        return;
      }
      hidePalette();
      return;
    }
    if (mode === "mention") {
      const head = document.createElement("div");
      head.className = "menu-section palette-head";
      head.textContent = "Files";
      paletteMenu.appendChild(head);
    }
    items.slice(0, 12).forEach((item, i) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item palette-item" + (i === 0 ? " selected" : "");
      if (mode === "slash") {
        btn.innerHTML = `<span class="palette-cmd">${item.cmd}</span><span class="palette-desc">${item.desc || ""}</span>`;
      } else {
        const kind = item.kind || "binary";
        const thumb =
          kind === "image" && item.previewUri
            ? `<img class="palette-thumb" src="${escapeAttr(item.previewUri)}" alt="" />`
            : `<span class="palette-file-icon">${fileExtLabel(item.ext || item.name)}</span>`;
        btn.innerHTML =
          `${thumb}<span class="palette-file-meta">` +
          `<span class="palette-cmd">${escapeAttr(item.name)}</span>` +
          `<span class="palette-desc">${escapeAttr(relPathDisplay(item.path || ""))}</span>` +
          `</span>`;
      }
      btn.addEventListener("mousedown", (e) => {
        e.preventDefault();
        applyPaletteItem(i);
      });
      paletteMenu.appendChild(btn);
    });
    paletteMenu.classList.remove("hidden");
  }

  function applyPaletteItem(index) {
    const item = paletteItems[index];
    if (!item || !inputEl) return;
    const text = inputEl.value;
    if (paletteMode === "slash") {
      const trimmed = text.replace(/(?:^|\s)(\/[\w-]*)$/, "").trimEnd();
      if (item.cmd === "/compact") {
        inputEl.value = trimmed;
        hidePalette();
        vscode.postMessage({ type: "slashCommand", cmd: "/compact" });
        return;
      }
      inputEl.value = trimmed;
      hidePalette();
      vscode.postMessage({ type: "slashCommand", cmd: item.cmd });
      return;
    }
    if (paletteMode === "mention") {
      const base = text.replace(/(?:^|\s)@([^\s@]*)$/, "").trimEnd();
      inputEl.value = base ? base + " " : "";
      addFileRef(item);
      hidePalette();
      autoGrow();
      inputEl.focus();
      return;
    }
  }

  function showMentionLoading() {
    if (!paletteMenu) {
      return;
    }
    paletteMode = "mention";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu.innerHTML =
      '<div class="menu-section palette-head">Files</div>' +
      '<div class="palette-loading">Searching…</div>';
    paletteMenu.classList.remove("hidden");
  }

  function onInputPalette() {
    if (!inputEl) return;
    const hit = detectPaletteQuery(inputEl.value);
    if (!hit) {
      hidePalette();
      return;
    }
    if (hit.mode === "slash") {
      renderPalette("slash", filterSlash(hit.query));
      return;
    }
    showMentionLoading();
    clearTimeout(mentionTimer);
    mentionTimer = window.setTimeout(() => {
      vscode.postMessage({ type: "mentionSearch", query: hit.query });
    }, hit.query ? 120 : 180);
  }

  function renderTodos() {
    if (!todosBar || !todosList) return;
    if (!todos.length) {
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      todosHadOpen = false;
      todosExpanded = false;
      return;
    }
    const normalized = todos.map((t) => ({
      ...t,
      status: normalizeTodoStatus(t.status),
    }));
    let done = 0;
    let cancelled = 0;
    for (const t of normalized) {
      if (t.status === "done") done++;
      if (t.status === "cancelled") cancelled++;
    }
    const open = normalized.filter((t) => t.status !== "done" && t.status !== "cancelled");
    const total = normalized.length - cancelled;

    if (!open.length) {
      if (done > 0 && todosHadOpen) {
        flashTodosDone();
      }
      todosHadOpen = false;
      todosExpanded = false;
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      return;
    }

    todosHadOpen = true;
    todosBar.classList.remove("hidden");

    const inProg = open.find((t) => t.status === "in_progress");
    const focus = inProg || open[0];
    const focusLabel = truncateTodoLabel(focus.content || focus.id || "Task", 52);
    const spinning = Boolean(inProg && busy);

    if (todosChipGlyph) {
      todosChipGlyph.textContent = inProg ? "◉" : "□";
      todosChipGlyph.classList.toggle("spinning", spinning);
    }
    if (todosChipSummary) {
      todosChipSummary.textContent = `${done}/${total} · ${focusLabel}`;
    }
    if (todosChipChev) {
      todosChipChev.textContent = todosExpanded ? "▴" : "▾";
    }
    if (todosChip) {
      todosChip.setAttribute("aria-expanded", todosExpanded ? "true" : "false");
    }

    if (!todosExpanded) {
      todosList.classList.add("hidden");
      todosList.innerHTML = "";
      return;
    }

    todosList.classList.remove("hidden");
    todosList.innerHTML = "";
    const sorted = [...open].sort((a, b) => {
      const rank = (s) => (s === "in_progress" ? 0 : 1);
      return rank(a.status) - rank(b.status);
    });
    sorted.slice(0, 12).forEach((t) => {
      const row = document.createElement("div");
      row.className = "todo-row status-" + t.status;
      row.setAttribute("role", "listitem");
      const glyph = document.createElement("span");
      glyph.className = "todo-glyph" + (t.status === "in_progress" && busy ? " spinning" : "");
      glyph.textContent = todoGlyph(t.status);
      const text = document.createElement("span");
      text.className = "todo-text";
      text.textContent = t.content || t.id;
      row.appendChild(glyph);
      row.appendChild(text);
      todosList.appendChild(row);
    });
    if (open.length > 12) {
      const more = document.createElement("div");
      more.className = "todo-more";
      more.textContent = `+${open.length - 12} more`;
      todosList.appendChild(more);
    }
  }

  function normalizeTodoStatus(status) {
    const s = (status || "pending").toLowerCase();
    if (s === "completed" || s === "complete") return "done";
    if (s === "inprogress" || s === "in-progress") return "in_progress";
    return s;
  }

  function todoGlyph(status) {
    const s = normalizeTodoStatus(status);
    if (s === "done") return "✓";
    if (s === "cancelled") return "×";
    if (s === "in_progress") return "◉";
    return "□";
  }
