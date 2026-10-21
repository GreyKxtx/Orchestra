  function isMutatingToolBlock(block) {
    return block?.classList?.contains("kind-write") === true;
  }

  function diffPathMatches(candidate, norm) {
    const p = (candidate || "").replace(/\\/g, "/");
    return p === norm || p.endsWith("/" + norm) || norm.endsWith("/" + p) || basename(p) === basename(norm);
  }

  function findDiffForPath(filePath) {
    if (!filePath) return null;
    const norm = filePath.replace(/\\/g, "/");
    if (pendingState.diff.length) {
      const hit = pendingState.diff.find((d) => diffPathMatches(d.path, norm));
      if (hit) {
        return hit;
      }
    }
    // Changes already written to disk. The core computes the same before/after
    // it computes for a dry run, so a turn that applies as it goes shows the
    // same diff as one that waits for approval. Without this the block falls
    // back to what it can rebuild from the call's arguments — for `write` that
    // is before="" and a rewritten file renders as entirely new lines.
    if (appliedDiffs.length) {
      const hit = appliedDiffs.find((d) => diffPathMatches(d.path, norm));
      if (hit) {
        return hit;
      }
    }
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const p = fp.replace(/\\/g, "/");
      if (
        p !== norm &&
        !p.endsWith("/" + norm) &&
        !norm.endsWith("/" + p) &&
        basename(p) !== basename(norm)
      ) {
        continue;
      }
      if (block.dataset.diffBefore !== undefined || block.dataset.diffAfter !== undefined) {
        return {
          path: fp,
          before: block.dataset.diffBefore || "",
          after: block.dataset.diffAfter || "",
        };
      }
    }
    return null;
  }

  function extractDiffFromTool(name, argsRaw, result) {
    const path = toolPathFromArgs(name, argsRaw, result || "");
    if (!path) {
      return null;
    }
    const args = parseToolArgs(argsRaw);
    const n = (name || "").toLowerCase();
    if (n === "write" || n === "fs.write" || n === "file.write_atomic") {
      const content = typeof args.content === "string" ? args.content : "";
      return { path, before: "", after: content };
    }
    if (n === "edit" || n === "fs.edit") {
      const search = typeof args.search === "string" ? args.search : "";
      const replace = typeof args.replace === "string" ? args.replace : "";
      if (search || replace) {
        return { path, before: search, after: replace };
      }
    }
    return null;
  }

  function rememberBlockDiff(block, before, after) {
    if (!block) return;
    block.dataset.diffBefore = before || "";
    block.dataset.diffAfter = after || "";
  }

  function syncToolDiffStats() {
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff) continue;
      const stats = countDiffStats(diff.before, diff.after);
      const el = block.querySelector(".tool-stats");
      if (el) el.innerHTML = statsHtml(stats);
    }
  }

  /** Upgrade write/edit tool blocks to Cursor-style inline diff when pending data arrives. */
  async function syncToolDiffPreviews() {
    syncToolDiffStats();
    const jobs = [];
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff || (!diff.before && !diff.after)) continue;
      block.querySelector(".file-change-card")?.remove();
      jobs.push(attachInlineToolDiff(block, fp, diff.before || "", diff.after || ""));
    }
    if (jobs.length) {
      await Promise.all(jobs);
    }
  }

  function attachToolDiffShell(block, filePath) {
    if (!block || !filePath) return;
    if (block.querySelector(".tool-diff-body.diff-preview-card")) return;
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    const body = block.querySelector(".tool-body");
    const diffWrap = document.createElement("div");
    diffWrap.className = "tool-body tool-diff-body diff-preview-card";
    const head = document.createElement("div");
    head.className = "diff-preview-head";
    head.innerHTML =
      diffExtBadgeHtml(filePath) +
      diffNameHtml(filePath) +
      `<span class="tool-diff-pending">…</span>`;
    const lines = document.createElement("div");
    lines.className = "diff-preview-body tool-diff-pending-body";
    lines.textContent = i18n("diff.loading");
    diffWrap.appendChild(head);
    diffWrap.appendChild(lines);
    head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      const d = findDiffForPath(filePath) || extractDiffFromTool("write", block.dataset.argsRaw || "", "");
      openDiffMessage(
        filePath,
        d?.before || block.dataset.diffBefore || "",
        d?.after || block.dataset.diffAfter || "",
        /** @type {MouseEvent} */ (e).shiftKey
      );
    });
    if (body) {
      body.replaceWith(diffWrap);
    } else {
      block.appendChild(diffWrap);
    }
    block.classList.add("write-card-only");
  }

  function renderPendingBar() {
    const n = pendingState.diff.length || pendingState.ops.length;
    if (!pendingBar) return;
    if (!n) {
      pendingBar.classList.add("hidden");
      return;
    }
    pendingBar.classList.remove("hidden");
    const fileCount = pendingState.diff.length;
    let add = 0;
    let del = 0;
    for (const d of pendingState.diff) {
      const s = countDiffStats(d.before, d.after);
      add += s.add || 0;
      del += s.del || 0;
    }
    if (pendingLabel) {
      pendingLabel.innerHTML =
        escapeAttr(fileCount ? i18n("pending.files_changed", { n: fileCount }) : i18n("pending.changes", { n })) +
        diffStatsHtml({ add, del });
    }
    ensurePendingToggle();
    pendingBar.classList.toggle("collapsed", !pendingExpanded);
    if (pendingExpanded) {
      renderPendingReviewList();
    }
  }

  /** The bar's show/hide button beside Apply and Discard. */
  function ensurePendingToggle() {
    if (!pendingLabel) return;
    const btn = document.getElementById("pending-toggle-btn");
    if (!btn) return;
    if (!btn.dataset.bound) {
      btn.dataset.bound = "1";
      btn.addEventListener("click", () => {
        pendingExpanded = !pendingExpanded;
        renderPendingBar();
      });
      pendingLabel.addEventListener("click", () => btn.click());
    }
    // The set's icons, not ▾ ✓ ✗: font glyphs come out at a different size
    // and weight in every font and sat crooked in their boxes.
    btn.innerHTML = pendingExpanded
      ? orchIconMarkup("chevron-up", { size: "sm" })
      : orchIconMarkup("chevron-down", { size: "sm" });
    const applyBtn = document.getElementById("pending-apply-btn");
    if (applyBtn && !applyBtn.querySelector("svg")) applyBtn.innerHTML = orchIconMarkup("check", { size: "sm" });
    const rejectBtn = document.getElementById("pending-reject-btn");
    if (rejectBtn && !rejectBtn.querySelector("svg")) rejectBtn.innerHTML = orchIconMarkup("close", { size: "sm" });
    btn.title = i18n(pendingExpanded ? "pending.collapse" : "pending.expand");
    btn.setAttribute("aria-label", btn.title);
    btn.setAttribute("aria-expanded", String(pendingExpanded));
  }

  // Access "Auto" is the person's word that the agent writes and runs without
  // asking. A turn keeps the flags it was sent with, so switching to Auto in
  // the middle of one changed nothing: the bar kept asking to accept every
  // edit. Under Auto the bar settles itself.
  function maybeAutoApplyPending() {
    if (accessId !== "auto" || !(pendingState.ops.length || pendingState.diff.length)) return;
    // auto: no "changes applied" line per edit — under Auto that is every edit.
    host.postMessage({ type: "applyPending", auto: true });
  }

  /** @param {any} request A command's consent under Auto is already given. */
  function autoApprovesPermission(request) {
    const kind = request && request.kind;
    return accessId === "auto" && (!kind || kind === "exec");
  }

  function diffExtBadgeHtml(filePath) {
    const ext = fileExtLabel(filePath || "");
    const lang = langFromPath(filePath || "");
    return `<span class="diff-ext-badge lang-${escapeAttr(lang)}">${escapeAttr(ext)}</span>`;
  }

  /** The file's name, then its folder in grey: a card names the file at a
   *  glance and still tells two index.html apart. */
  function diffNameHtml(filePath) {
    const p = String(filePath || "").replace(/\\/g, "/");
    const name = basename(p) || "file";
    const dir = p.length > name.length ? p.slice(0, p.length - name.length).replace(/\/+$/, "") : "";
    return (
      `<button type="button" class="diff-preview-name" title="${escapeAttr((p ? p + " — " : "") + i18n("diff.open_file"))}">${escapeAttr(name)}</button>` +
      (dir ? `<span class="diff-preview-dir">${escapeAttr(dir)}</span>` : "")
    );
  }

  // A card in the chat shows the first few changed lines and folds the rest:
  // a new 200-line file was a wall of green between two sentences.
  const DIFF_FOLD_ROWS = 6;

  function applyDiffFold(block, card, lines, stats) {
    card.querySelector(".diff-fold-btn")?.remove();
    const rows = lines.querySelectorAll(".diff-u-row").length;
    const total = Math.max(rows, (stats?.add || 0) + (stats?.del || 0));
    // Folding away one or two lines costs a click and saves nothing.
    const foldable = rows > DIFF_FOLD_ROWS + 2;
    const open = block.dataset.diffOpen === "1";
    card.classList.toggle("folded", foldable && !open);
    if (!foldable) return;
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "diff-fold-btn";
    btn.textContent = open ? i18n("diff.fold") : i18n("diff.unfold", { n: total - DIFF_FOLD_ROWS });
    btn.setAttribute("aria-expanded", String(open));
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      block.dataset.diffOpen = open ? "0" : "1";
      applyDiffFold(block, card, lines, stats);
    });
    card.appendChild(btn);
    if (!lines.dataset.foldBound) {
      lines.dataset.foldBound = "1";
      lines.addEventListener("click", () => {
        if (card.classList.contains("folded")) card.querySelector(".diff-fold-btn")?.click();
      });
    }
  }

  function diffStatsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  async function renderPendingReviewList() {
    if (!pendingReviewListEl) return;
    bindPendingReviewListEvents();
    pendingReviewListEl.innerHTML = "";
    if (!pendingState.diff.length) {
      return;
    }
    // Two overlapping draws (pending data twice in quick succession) each
    // appended the whole list; a draw that a newer one replaced stops.
    const draw = {};
    pendingReviewListEl._draw = draw;
    for (let idx = 0; idx < pendingState.diff.length; idx++) {
      if (pendingReviewListEl._draw !== draw) return;
      const d = pendingState.diff[idx];
      const stats = countDiffStats(d.before, d.after);
      const item = document.createElement("div");
      item.className = "pending-review-item diff-preview-card";
      item.setAttribute("data-idx", String(idx));
      if (idx === diffReviewCursor) item.classList.add("selected");

      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(d.path || "") +
        diffNameHtml(d.path || "") +
        diffStatsHtml(stats) +
        // Per-file decisions, the same two the a/x keys make. Without them the
        // only discoverable choice is all-or-nothing on the bar below.
        `<span class="pending-item-acts">` +
        `<button type="button" class="pending-item-act pending-item-keep" data-act="keep" title="${escapeAttr(i18n("diff.keep_title"))}">${escapeAttr(i18n("diff.keep"))}</button>` +
        `<button type="button" class="pending-item-act pending-item-drop" data-act="drop" title="${escapeAttr(i18n("diff.drop_title"))}">${escapeAttr(i18n("diff.drop"))}</button>` +
        `</span>`;

      const body = document.createElement("div");
      body.className = "diff-preview-body";

      item.appendChild(head);
      item.appendChild(body);
      pendingReviewListEl.appendChild(item);
      await renderUnifiedDiffLines(body, d.before || "", d.after || "", d.path || "", 14);
      if (idx === diffReviewCursor) {
        item.scrollIntoView({ block: "nearest", behavior: "smooth" });
      }
    }
  }

  // Both take an optional file list. Without one the host applies or rejects
  // the whole turn (the bar's two buttons); with one it settles just those
  // files and the core hands back what is still pending, so a reviewer can
  // keep the good edits of a turn and throw away the bad one.
  function applyPendingChanges(paths) {
    host.postMessage(
      paths && paths.length ? { type: "applyPending", paths } : { type: "applyPending" }
    );
  }

  function discardPendingChanges(paths) {
    host.postMessage(
      paths && paths.length ? { type: "discardPending", paths } : { type: "discardPending" }
    );
  }

  /** Apply or reject the file the review cursor is on. */
  function settleSelectedPendingFile(apply) {
    const d = pendingState.diff[diffReviewCursor];
    const path = d && d.path ? String(d.path) : "";
    if (!path) return;
    // The cursor stays put: the list shrinks under it, so the next file slides
    // into the selected slot and a reviewer can hold the key down.
    if (apply) applyPendingChanges([path]);
    else discardPendingChanges([path]);
  }

  function countDiffStats(before, after) {
    const bLines = (before || "").split("\n");
    const aLines = (after || "").split("\n");
    if (!before && after) {
      return { add: aLines.length, del: 0 };
    }
    if (before && !after) {
      return { add: 0, del: bLines.length };
    }
    const freq = new Map();
    for (const line of bLines) {
      freq.set(line, (freq.get(line) || 0) + 1);
    }
    let add = 0;
    for (const line of aLines) {
      const c = freq.get(line) || 0;
      if (c > 0) {
        freq.set(line, c - 1);
      } else {
        add++;
      }
    }
    let del = 0;
    for (const c of freq.values()) {
      del += c;
    }
    return { add, del };
  }

  function toolBlockKey(msg) {
    const id = msg.toolCallId || `${msg.toolName}-${msg.step ?? toolBlocks.size}`;
    if (msg.scope === "child" && msg.taskId) {
      return `${msg.taskId}:${id}`;
    }
    return id;
  }

  function requestHighlight(lines, lang) {
    const requestId = `hl-${++highlightSeq}`;
    return new Promise((resolve) => {
      highlightWaiters.set(requestId, resolve);
      host.postMessage({
        type: "highlightCode",
        requestId,
        language: lang || "plaintext",
        lines: Array.isArray(lines) ? lines : [],
      });
      setTimeout(() => {
        if (highlightWaiters.has(requestId)) {
          highlightWaiters.delete(requestId);
          resolve(lines.map((l) => escapeHtml(String(l || ""))));
        }
      }, 8000);
    });
  }

  /** Myers-style line alignment for side-by-side diff panes. */
  function alignDiffLines(before, after) {
    // An empty side has no lines, not one empty line: a new file is all "+",
    // without a "−" for a blank line that never was.
    const a = before ? before.split("\n") : [];
    const b = after ? after.split("\n") : [];
    const n = a.length;
    const m = b.length;
    const dp = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    const rows = [];
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && a[i] === b[j]) {
        rows.push({ type: "same", left: a[i], right: b[j], leftNum: i + 1, rightNum: j + 1 });
        i++;
        j++;
      } else if (i < n && (j >= m || dp[i + 1][j] >= dp[i][j + 1])) {
        // On a tie the old line goes first: a replaced line reads − then +.
        rows.push({ type: "del", left: a[i], leftNum: i + 1 });
        i++;
      } else if (j < m) {
        rows.push({ type: "add", right: b[j], rightNum: j + 1 });
        j++;
      }
    }
    return rows;
  }

  async function renderDiffPanes(before, after, lang) {
    if (!diffPaneBefore || !diffPaneAfter) return;
    const rows = alignDiffLines(before, after);
    const leftLines = rows.map((r) => (r.type === "add" ? "" : r.left ?? ""));
    const rightLines = rows.map((r) => (r.type === "del" ? "" : r.right ?? ""));
    const [leftHtml, rightHtml] = await Promise.all([
      requestHighlight(leftLines, lang),
      requestHighlight(rightLines, lang),
    ]);
    diffPaneBefore.innerHTML = "";
    diffPaneAfter.innerHTML = "";
    rows.forEach((r, idx) => {
      const lRow = document.createElement("div");
      lRow.className = "diff-sbs-row" + (r.type === "del" ? " del" : r.type === "same" ? " same" : " empty");
      lRow.innerHTML =
        `<span class="diff-ln">${r.leftNum || ""}</span>` +
        `<span class="diff-code">${leftHtml[idx] || "&nbsp;"}</span>`;
      diffPaneBefore.appendChild(lRow);
      const rRow = document.createElement("div");
      rRow.className = "diff-sbs-row" + (r.type === "add" ? " add" : r.type === "same" ? " same" : " empty");
      rRow.innerHTML =
        `<span class="diff-ln">${r.rightNum || ""}</span>` +
        `<span class="diff-code">${rightHtml[idx] || "&nbsp;"}</span>`;
      diffPaneAfter.appendChild(rRow);
    });
  }

  function showDiffViewer(path, before, after, language) {
    if (!diffViewer) return;
    diffViewerState = { path: path || "", before: before || "", after: after || "" };
    if (diffViewerTitle) {
      diffViewerTitle.textContent = basename(path) || path || "Diff";
      diffViewerTitle.title = path || "";
    }
    diffViewer.classList.remove("hidden");
    void renderDiffPanes(before, after, language || langFromPath(path));
  }

  function hideDiffViewer() {
    diffViewer?.classList.add("hidden");
  }

  function openExternalFile(filePath, focus) {
    if (!filePath) return;
    host.postMessage({ type: "openFile", path: filePath, focus: Boolean(focus) });
  }

  function openDiffMessage(path, before, after, sideBySide) {
    host.postMessage({
      type: "openDiff",
      path,
      before: before || "",
      after: after || "",
      focus: !sideBySide,
      sideBySide: Boolean(sideBySide),
    });
  }

