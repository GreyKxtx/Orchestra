  /** Canonical Orchestra tier label (L1–L5) from a legacy band name. */
  function subagentTierLabel(tier) {
    const t = String(tier || "").trim();
    if (!t) return "";
    if (/^L[1-5]$/i.test(t)) return t.toUpperCase();
    const map = { planner: "L5", lead: "L4", complex: "L3", focused: "L3", micro: "L1", explore: "L2" };
    return map[t.toLowerCase()] || "";
  }

  /**
   * Worker goals are often raw WorkOrder JSON ('{ "intent": "...", ... }').
   * Extract the human field instead of showing the JSON blob.
   */
  function humanizeTaskLabel(text) {
    const t = String(text || "").trim();
    if (!t.startsWith("{")) return t;
    try {
      const o = JSON.parse(t);
      const s = o.intent || o.goal || o.title || o.description || o.task_id || "";
      if (s) return String(s).trim();
    } catch {
      // Partial/invalid JSON — best-effort regex extraction.
      const m = t.match(/"(?:intent|goal|title|description)"\s*:\s*"((?:[^"\\]|\\.)*)/);
      if (m && m[1]) return m[1].replace(/\\"/g, '"');
    }
    return t;
  }

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
    let idx = subagents.findIndex((s) => s.taskId === taskId);
    if (idx < 0 && prev.parentToolCallId) {
      // Upgrade the generic tool-call row (created on task/task_spawn start)
      // in place instead of appending a duplicate.
      idx = subagents.findIndex((s) => !s.taskId && s.id === prev.parentToolCallId);
    }
    const row = {
      id: prev.parentToolCallId || taskId,
      taskId,
      type: prev.type,
      label: prev.label,
      tier: prev.tier,
      model: prev.model,
      agent: prev.agent,
      waitingFor: prev.waitingFor,
      waitReason: prev.waitReason,
      status: prev.status,
      error: prev.error,
      goal: prev.goal,
      result: prev.result,
      parentTaskId: prev.parentTaskId,
      depth: prev.depth,
      startedAt: prev.startedAt,
      endedAt: prev.endedAt,
      lessonPromote: prev.lessonPromote,
      playbookPromote: prev.playbookPromote,
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
    const tierL = subagentTierLabel(node.tier);
    const tierHtml = tierL
      ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(node.model || "")}">${tierL}</span>`
      : "";
    panel.innerHTML =
      `<div class="subagent-panel-head">` +
      `<span class="subagent-type">${escapeAttr(node.type || "agent")}</span>` +
      tierHtml +
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
      const label = humanizeTaskLabel(msg.content || "");
      upsertSubagentTask(taskId, {
        parentToolCallId: msg.parentToolCallId,
        parentTaskId: msg.parentTaskId || "",
        depth: Number(msg.depth) || 0,
        type: msg.subagentType || "agent",
        tier: msg.tier || "",
        model: msg.model || "",
        agent: msg.agent || "",
        waitingFor: [],
        waitReason: "",
        label: label.length > 48 ? label.slice(0, 45) + "…" : label || taskId,
        goal: label,
        startedAt: msg.at || Date.now(),
        endedAt: 0,
        status: "running",
        toolCount: 0,
      });
      // A chat rebuilt from its log already has its blocks; no panel of its own.
      if (!msg.quiet) ensureSubagentToolsHost(taskId, msg.parentToolCallId);
    } else if (msg.phase === "queued") {
      // Held back until the tasks whose files it overlaps are through.
      const patch = {
        status: "waiting",
        waitingFor: Array.isArray(msg.waitingFor) ? msg.waitingFor.map(String) : [],
        waitReason: String(msg.reason || ""),
      };
      if (msg.parentToolCallId) patch.parentToolCallId = msg.parentToolCallId;
      upsertSubagentTask(taskId, patch);
    } else if (msg.phase === "done") {
      const st =
        msg.status === "done"
          ? "done"
          : msg.status === "error" || msg.status === "timeout"
            ? "error"
            : "done";
      const patch = { status: st, endedAt: msg.at || Date.now(), waitingFor: [], waitReason: "" };
      if (msg.result) patch.result = String(msg.result);
      if (st === "error" && msg.error) {
        patch.error = String(msg.error);
      }
      const lessonHint = String(msg.lessonPromoteSuggestion || "").trim();
      const playbookHint = String(msg.playbookPromoteSuggestion || "").trim();
      if (lessonHint) patch.lessonPromote = lessonHint;
      if (playbookHint) patch.playbookPromote = playbookHint;
      upsertSubagentTask(taskId, patch);
      const hints = [];
      if (lessonHint) hints.push("lesson_promote");
      if (playbookHint) hints.push("playbook_promote");
      if (hints.length && !msg.quiet) {
        appendMsg(
          "system",
          `Learning: worker finished with ${hints.join(" + ")} suggestion — Lead should review task_result / call promote tool.`
        );
      }
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
    turnToolFirstStart = 0;
    turnToolLastEnd = 0;
  }

  // noteTurnToolStart/End track the group's wall clock. The TUI's tool group
  // shows this in its footer (ui/tui/view/tool_group.go); without it the
  // webview could say a turn ran nine tools but never how long that cost.
  function noteTurnToolStart() {
    if (!turnToolFirstStart) {
      turnToolFirstStart = Date.now();
    }
  }

  function noteTurnToolEnd() {
    turnToolLastEnd = Date.now();
    updateToolTraceSummary();
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
      toolTraceSummary.textContent = i18n("turn.running_tools");
      const list = document.createElement("div");
      list.className = "tool-trace-list";
      toolTraceEl.appendChild(toolTraceSummary);
      toolTraceEl.appendChild(list);
      inner.appendChild(toolTraceEl);
      // Open while the turn works: the rows are what the person watches.
      toolTraceEl.open = true;
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

  // A finished group stays open when it is short enough to read at a glance;
  // a long one folds to its "N tool calls" line. Thoughts do not count.
  const TRACE_OPEN_MAX_CALLS = 12;

  function settleToolTrace() {
    if (!toolTraceEl) return;
    const calls = toolTraceEl.querySelectorAll(".tool-trace-list > .tool-block:not(.think-row)").length;
    toolTraceEl.open = calls <= TRACE_OPEN_MAX_CALLS;
  }

  function updateToolTraceSummary() {
    if (!toolTraceSummary) {
      return;
    }
    // "8 tool calls": the count is the one thing the folded group has to say;
    // the per-kind breakdown ("Explored 3 reads, 2 searches, 1 tool") is
    // what the rows below already show. A group of thoughts and no tools is
    // named by the thinking.
    const n = turnToolCount.read + turnToolCount.search + turnToolCount.write + turnToolCount.other;
    toolTraceSummary.textContent = n ? i18nCount("trace.calls", n) : i18n("reason.brief");
  }

  /**
   * A "Think · …" row in the turn's trace, between the tool calls it led to.
   * The model's reasoning used to be one fold above all the tools, so what
   * it thought before the fifth call sat four rows away from that call.
   * One line, ellipsised; a click shows the whole of it.
   * @param {string=} text a whole thought (restored history)
   * @returns {HTMLElement | null}
   */
  function makeThinkRow(text) {
    const list = ensureToolTraceList();
    if (!list) return null;
    const row = document.createElement("div");
    row.className = "tool-block done think-row";
    const head = document.createElement("button");
    head.type = "button";
    head.className = "tool-head";
    head.innerHTML =
      `<span class="tool-icon">${orchIconMarkup("think", { size: "sm" })}</span>` +
      `<span class="tool-label"><span class="tr-name"></span><span class="tr-detail"></span></span>`;
    const nameEl = head.querySelector(".tr-name");
    if (nameEl) nameEl.textContent = i18n("think.label");
    const body = document.createElement("pre");
    body.className = "trace-body think-body hidden";
    head.addEventListener("click", () => {
      body.classList.toggle("hidden");
      head.classList.toggle("open");
    });
    row.appendChild(head);
    row.appendChild(body);
    list.appendChild(row);
    if (text) {
      body.textContent = text;
      setThinkLine(row, false);
    }
    updateToolTraceSummary();
    return row;
  }

  /** The row's one line: the start of the thought, or its newest words while it streams. */
  function setThinkLine(row, live) {
    const body = row.querySelector(".think-body");
    const detail = row.querySelector(".tr-detail");
    if (!body || !detail) return;
    const flat = (body.textContent || "").replace(/\s+/g, " ").trim();
    detail.textContent = live && flat.length > 240 ? "…" + flat.slice(-240) : flat;
  }

  /** @returns {HTMLElement | null} the body of the thought being streamed. */
  function ensureReasoning() {
    if (!reasoningDetails) {
      reasoningDetails = makeThinkRow();
      reasoningBody = reasoningDetails ? reasoningDetails.querySelector(".think-body") : null;
      if (!reasoningStarted) reasoningStarted = Date.now();
    }
    return reasoningBody;
  }

  /** Streamed reasoning: the text goes into the open thought's row. */
  function appendReasoning(chunk) {
    const body = ensureReasoning();
    if (!body || !reasoningDetails) return;
    body.textContent = (body.textContent || "") + chunk;
    setThinkLine(reasoningDetails, true);
  }

  /** A tool call or the answer began: the thought before it is done. */
  function finalizeReasoningSummary() {
    if (!reasoningDetails || !reasoningBody) {
      reasoningDetails = null;
      reasoningBody = null;
      return;
    }
    if (!(reasoningBody.textContent || "").trim()) {
      reasoningDetails.remove();
    } else {
      setThinkLine(reasoningDetails, false);
    }
    reasoningDetails = null;
    reasoningBody = null;
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

      const actBtn = t.closest(".pending-item-act");
      if (actBtn instanceof HTMLElement) {
        if (!d.path) return;
        const paths = [String(d.path)];
        if (actBtn.getAttribute("data-act") === "keep") applyPendingChanges(paths);
        else discardPendingChanges(paths);
        return;
      }

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
    // A changed row remembers whether unchanged lines came before it, so two
    // hunks are drawn as two, not glued into one block.
    const changedRows = [];
    let gap = false;
    for (const r of allRows) {
      if (r.type === "same") {
        gap = changedRows.length > 0;
        continue;
      }
      changedRows.push({ ...r, afterGap: gap });
      gap = false;
    }
    const displayRows = changedRows.slice(0, maxLines);
    if (displayRows.length === 0) {
      const hint = document.createElement("div");
      hint.className = "diff-empty-hint";
      hint.textContent = i18n(
        (before || "") === (after || "") ? "diff.no_changes" : "diff.unavailable"
      );
      container.appendChild(hint);
      return;
    }
    const codeLines = displayRows.map((r) =>
      r.type === "del" ? r.left ?? "" : r.type === "add" ? r.right ?? "" : ""
    );
    const htmlLines = await requestHighlight(codeLines, lang);
    displayRows.forEach((r, idx) => {
      if (r.afterGap) {
        const sep = document.createElement("div");
        sep.className = "diff-u-sep";
        sep.textContent = "⋯";
        container.appendChild(sep);
      }
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
      more.textContent = i18n("diff.more_lines", { n: changedRows.length - maxLines });
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
      head.innerHTML = diffExtBadgeHtml(filePath) + diffNameHtml(filePath) + diffStatsHtml(stats);
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
        head.innerHTML = diffExtBadgeHtml(filePath) + diffNameHtml(filePath) + diffStatsHtml(stats);
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
    // A card is drawn again whenever pending data for its file arrives, and
    // the highlighting is async: draws that overlapped all appended into the
    // same body and each line showed up three times. Draw off-screen; only
    // the newest draw puts its rows in.
    const token = {};
    lines._diffDraw = token;
    const fresh = document.createElement("div");
    await renderUnifiedDiffLines(fresh, before, after, filePath, 40);
    if (lines._diffDraw !== token) return;
    lines.replaceChildren(...fresh.childNodes);
    applyDiffFold(block, diffWrap, lines, stats);
  }
