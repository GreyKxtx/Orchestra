  function upsertSubagentTask(taskId, fields) {
    if (!taskId) return;
    const prev =
      subagentByTask.get(taskId) ||
      ({
        taskId,
        type: "agent",
        label: taskId,
        status: "running",
        toolCount: 0,
      });
    Object.assign(prev, fields);
    subagentByTask.set(taskId, prev);
    const idx = subagents.findIndex((s) => s.taskId === taskId);
    const row = {
      id: prev.parentToolCallId || taskId,
      taskId,
      type: prev.type,
      label: prev.label,
      status: prev.status,
      parentToolCallId: prev.parentToolCallId,
      toolsEl: prev.toolsEl,
      toolCount: prev.toolCount,
    };
    if (idx >= 0) subagents[idx] = row;
    else subagents.push(row);
    renderSubagents();
  }

  function ensureSubagentToolsHost(taskId, parentToolCallId) {
    const node = subagentByTask.get(taskId);
    if (!node || node.toolsEl) return node?.toolsEl;
    const parentBlock = parentToolCallId ? toolBlocks.get(parentToolCallId) : null;
    if (parentBlock) {
      let host = parentBlock.querySelector(".subagent-tools");
      if (!host) {
        host = document.createElement("div");
        host.className = "subagent-tools";
        host.dataset.taskId = taskId;
        parentBlock.appendChild(host);
      }
      node.toolsEl = host;
      return host;
    }
    if (!messagesEl) return undefined;
    const panel = document.createElement("div");
    panel.className = "subagent-panel";
    panel.dataset.taskId = taskId;
    panel.innerHTML =
      `<div class="subagent-panel-head">` +
      `<span class="subagent-type">${escapeAttr(node.type || "agent")}</span>` +
      `<span class="subagent-label">${escapeAttr(node.label || taskId)}</span>` +
      `</div>`;
    const host = document.createElement("div");
    host.className = "subagent-tools";
    panel.appendChild(host);
    messagesEl.appendChild(panel);
    node.toolsEl = host;
    return host;
  }

  function handleChildLifecycle(msg) {
    const taskId = msg.taskId || "";
    if (!taskId) return;
    if (msg.phase === "started") {
      const label = (msg.content || "").trim();
      upsertSubagentTask(taskId, {
        parentToolCallId: msg.parentToolCallId,
        type: msg.subagentType || "agent",
        label: label.length > 48 ? label.slice(0, 45) + "…" : label || taskId,
        status: "running",
        toolCount: 0,
      });
      ensureSubagentToolsHost(taskId, msg.parentToolCallId);
    } else if (msg.phase === "done") {
      const st =
        msg.status === "done"
          ? "done"
          : msg.status === "error" || msg.status === "timeout"
            ? "error"
            : "done";
      upsertSubagentTask(taskId, { status: st });
    }
  }

  function resetTurnState() {
    assistantTurn = null;
    assistantTurnInner = null;
    assistantBubble = null;
    streamRawText = "";
    toolTraceEl = null;
    toolTraceSummary = null;
    reasoningDetails = null;
    reasoningBody = null;
    reasoningStarted = 0;
    turnToolCount = { read: 0, search: 0, write: 0, other: 0 };
  }

  /** @returns {HTMLElement | null} */
  function ensureTurn() {
    if (assistantTurnInner) {
      return assistantTurnInner;
    }
    if (!messagesEl) {
      return null;
    }
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return assistantTurnInner;
  }

  function commitPreToolText() {
    flushAssistantMarkdown();
    if (!assistantBubble) {
      streamRawText = "";
      return;
    }
    const visible = sanitizeAssistantStream(stripFinalEnvelope(streamRawText)).trim();
    if (!visible) {
      assistantBubble.remove();
      assistantBubble = null;
      streamRawText = "";
      return;
    }
    assistantBubble.classList.add("turn-text-segment");
    assistantBubble = null;
    streamRawText = "";
  }

  /** @returns {HTMLElement | null} */
  function ensureToolTraceList() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!toolTraceEl) {
      toolTraceEl = document.createElement("details");
      toolTraceEl.className = "tool-trace trace-details";
      toolTraceSummary = document.createElement("summary");
      toolTraceSummary.className = "trace-summary";
      toolTraceSummary.textContent = "Running tools…";
      const list = document.createElement("div");
      list.className = "tool-trace-list";
      toolTraceEl.appendChild(toolTraceSummary);
      toolTraceEl.appendChild(list);
      inner.appendChild(toolTraceEl);
      toolTraceEl.open = false;
    }
    return toolTraceEl.querySelector(".tool-trace-list");
  }

  function bumpToolTraceCount(name) {
    const kind = toolKind(name);
    if (kind === "read" || kind === "list") {
      turnToolCount.read++;
    } else if (kind === "search" || kind === "glob") {
      turnToolCount.search++;
    } else if (kind === "write") {
      turnToolCount.write++;
    } else {
      turnToolCount.other++;
    }
    updateToolTraceSummary();
  }

  function updateToolTraceSummary() {
    if (!toolTraceSummary) {
      return;
    }
    const parts = [];
    if (turnToolCount.read) {
      const n = turnToolCount.read;
      parts.push(`${n} ${n === 1 ? "read" : "reads"}`);
    }
    if (turnToolCount.search) {
      const n = turnToolCount.search;
      parts.push(`${n} ${n === 1 ? "search" : "searches"}`);
    }
    if (turnToolCount.write) {
      const n = turnToolCount.write;
      parts.push(`${n} ${n === 1 ? "edit" : "edits"}`);
    }
    if (turnToolCount.other) {
      const n = turnToolCount.other;
      parts.push(`${n} tool${n === 1 ? "" : "s"}`);
    }
    toolTraceSummary.textContent = parts.length ? `Explored ${parts.join(", ")}` : "Tools";
  }

  /** @returns {HTMLElement | null} */
  function ensureReasoning() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!reasoningDetails) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = "Thought briefly";
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      inner.insertBefore(reasoningDetails, toolTraceEl || null);
      reasoningDetails.open = false;
      reasoningStarted = Date.now();
    }
    return reasoningBody;
  }

  function finalizeReasoningSummary() {
    if (!reasoningDetails || !reasoningBody) {
      return;
    }
    const text = (reasoningBody.textContent || "").trim();
    if (!text) {
      reasoningDetails.remove();
      reasoningDetails = null;
      reasoningBody = null;
      return;
    }
    const sum = reasoningDetails.querySelector(".trace-summary");
    if (sum) {
      const sec = reasoningStarted ? Math.round((Date.now() - reasoningStarted) / 1000) : 0;
      sum.textContent = sec >= 2 ? `Thought for ${sec}s` : "Thought briefly";
    }
    reasoningDetails.open = false;
  }

  function messagesHostFor(msg) {
    if (msg.scope === "child" && msg.taskId) {
      const node = subagentByTask.get(msg.taskId);
      if (node) {
        if (!node.toolsEl) {
          ensureSubagentToolsHost(msg.taskId, msg.parentToolCallId || node.parentToolCallId);
        }
        if (node.toolsEl) return node.toolsEl;
      }
    }
    // Write/edit diffs stay in the main turn stream — never inside collapsed tool trace.
    if (toolKind(msg.toolName) === "write") {
      return ensureTurn();
    }
    return ensureToolTraceList() || messagesEl;
  }

  function bindWriteToolHead(block, head) {
    head.classList.add("tool-head-write");
    const chev = head.querySelector(".tool-chev");
    if (chev) chev.remove();
    head.addEventListener("click", (e) => {
      if (e.target.closest?.(".diff-preview-name")) return;
      const fp = block.dataset.filePath || "";
      if (!fp) return;
      const d = findDiffForPath(fp);
      e.preventDefault();
      openDiffMessage(fp, d?.before || "", d?.after || "", /** @type {MouseEvent} */ (e).shiftKey);
    });
  }

  function tryShowWriteDiff(block, name, argsRaw, content) {
    if (!block || toolKind(name) !== "write") return;
    const filePath = toolPathFromArgs(name, argsRaw, content || "");
    if (!filePath) return;
    block.dataset.filePath = filePath;
    block.dataset.argsRaw = argsRaw || "";
    let diff = findDiffForPath(filePath);
    if (!diff) {
      diff = extractDiffFromTool(name, argsRaw, content || "");
    }
    if (diff) {
      rememberBlockDiff(block, diff.before || "", diff.after || "");
    }
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(block, filePath, diff.before || "", diff.after || "");
    } else {
      attachToolDiffShell(block, filePath);
    }
  }

  function basename(path) {
    if (!path) return "";
    const parts = path.replace(/\\/g, "/").split("/");
    return parts[parts.length - 1] || path;
  }

  function normalizePath(p) {
    return (p || "").replace(/\\/g, "/").toLowerCase();
  }

  function pathsMatch(a, b) {
    const na = normalizePath(a);
    const nb = normalizePath(b);
    if (!na || !nb) return false;
    if (na === nb) return true;
    if (na.endsWith("/" + nb) || nb.endsWith("/" + na)) return true;
    const ba = basename(na);
    const bb = basename(nb);
    return ba !== "" && ba === bb;
  }

  function bindPendingReviewListEvents() {
    if (!pendingReviewListEl || pendingReviewListEl.dataset.bound === "1") return;
    pendingReviewListEl.dataset.bound = "1";
    pendingReviewListEl.addEventListener("click", (e) => {
      const t = e.target;
      if (!(t instanceof HTMLElement)) return;
      const item = t.closest(".pending-review-item");
      if (!item) return;
      const idx = Number(item.getAttribute("data-idx"));
      if (!Number.isFinite(idx) || idx < 0 || idx >= pendingState.diff.length) return;
      const d = pendingState.diff[idx];
      if (!d) return;

      const nameBtn = t.closest(".diff-preview-name");
      if (nameBtn) {
        diffReviewCursor = idx;
        renderPendingReviewList();
        if (!d.path) return;
        if (/** @type {MouseEvent} */ (e).altKey) {
          openExternalFile(d.path, true);
          return;
        }
        openDiffMessage(d.path, d.before || "", d.after || "", /** @type {MouseEvent} */ (e).shiftKey);
      }
    });
  }

  function diffReviewActive() {
    return pendingState.diff.length > 0 && document.activeElement !== inputEl;
  }

  async function renderUnifiedDiffLines(container, before, after, filePath, maxLines = 32) {
    const lang = langFromPath(filePath || "");
    const allRows = alignDiffLines(before, after);
    const changedRows = allRows.filter((r) => r.type !== "same");
    const displayRows = changedRows.slice(0, maxLines);
    if (displayRows.length === 0) {
      const hint = document.createElement("div");
      hint.className = "diff-empty-hint";
      hint.textContent =
        (before || "") === (after || "")
          ? "No line changes detected"
          : "Diff preview unavailable";
      container.appendChild(hint);
      return;
    }
    const codeLines = displayRows.map((r) =>
      r.type === "del" ? r.left ?? "" : r.type === "add" ? r.right ?? "" : ""
    );
    const htmlLines = await requestHighlight(codeLines, lang);
    displayRows.forEach((r, idx) => {
      const row = document.createElement("div");
      row.className = "diff-u-row " + r.type;
      const ln = r.type === "del" ? r.leftNum : r.rightNum;
      const gutter = r.type === "del" ? "−" : "+";
      row.innerHTML =
        `<span class="diff-u-ln">${ln || ""}</span>` +
        `<span class="diff-u-gutter">${gutter}</span>` +
        `<span class="diff-u-code">${htmlLines[idx] || "&nbsp;"}</span>`;
      container.appendChild(row);
    });
    if (changedRows.length > maxLines) {
      const more = document.createElement("div");
      more.className = "diff-more";
      more.textContent = `… ${changedRows.length - maxLines} more changed lines`;
      container.appendChild(more);
    }
  }

  async function appendDiffLines(container, before, after, filePath, maxLines = 32) {
    const wrap = document.createElement("div");
    wrap.className = "diff-u-block";
    await renderUnifiedDiffLines(wrap, before, after, filePath, maxLines);
    container.appendChild(wrap);
  }

  async function attachInlineToolDiff(block, filePath, before, after) {
    if (!block || !filePath) return;
    rememberBlockDiff(block, before, after);
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    block.classList.add("write-card-only");
    const stats = countDiffStats(before, after);
    let diffWrap = block.querySelector(".tool-diff-body.diff-preview-card");
    if (!diffWrap) {
      const body = block.querySelector(".tool-body");
      diffWrap = document.createElement("div");
      diffWrap.className = "tool-body tool-diff-body diff-preview-card";
      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(filePath) +
        `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
        diffStatsHtml(stats);
      const lines = document.createElement("div");
      lines.className = "diff-preview-body";
      diffWrap.appendChild(head);
      diffWrap.appendChild(lines);
      if (body) {
        body.replaceWith(diffWrap);
      } else {
        block.appendChild(diffWrap);
      }
      head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
      });
    } else {
      const head = diffWrap.querySelector(".diff-preview-head");
      if (head) {
        head.innerHTML =
          diffExtBadgeHtml(filePath) +
          `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
          diffStatsHtml(stats);
        head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
        });
      }
    }
    const lines = diffWrap.querySelector(".diff-preview-body");
    if (!lines) return;
    lines.classList.remove("tool-diff-pending-body");
    lines.textContent = "";
    await renderUnifiedDiffLines(lines, before, after, filePath, 40);
  }
