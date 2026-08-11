  function isMutatingToolBlock(block) {
    return block?.classList?.contains("kind-write") === true;
  }

  function findDiffForPath(filePath) {
    if (!filePath) return null;
    const norm = filePath.replace(/\\/g, "/");
    if (pendingState.diff.length) {
      const hit = pendingState.diff.find((d) => {
        const p = (d.path || "").replace(/\\/g, "/");
        return p === norm || p.endsWith("/" + norm) || norm.endsWith("/" + p) || basename(p) === basename(norm);
      });
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
      `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
      `<span class="tool-diff-pending">…</span>`;
    const lines = document.createElement("div");
    lines.className = "diff-preview-body tool-diff-pending-body";
    lines.textContent = "Loading diff preview…";
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
    if (pendingLabel) {
      pendingLabel.textContent = fileCount
        ? `${fileCount} file${fileCount === 1 ? "" : "s"} changed`
        : `${n} pending change${n === 1 ? "" : "s"}`;
    }
    renderPendingReviewList();
  }

  function diffExtBadgeHtml(filePath) {
    const ext = fileExtLabel(filePath || "");
    const lang = langFromPath(filePath || "");
    return `<span class="diff-ext-badge lang-${escapeAttr(lang)}">${escapeAttr(ext)}</span>`;
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
    for (let idx = 0; idx < pendingState.diff.length; idx++) {
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
        `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(d.path || "file"))}</button>` +
        diffStatsHtml(stats);

      const body = document.createElement("div");
      body.className = "diff-preview-body";

      item.appendChild(head);
      item.appendChild(body);
      pendingReviewListEl.appendChild(item);
      await renderUnifiedDiffLines(body, d.before || "", d.after || "", d.path || "", 28);
      if (idx === diffReviewCursor) {
        item.scrollIntoView({ block: "nearest", behavior: "smooth" });
      }
    }
  }

  function applyPendingChanges() {
    vscode.postMessage({ type: "applyPending" });
  }

  function discardPendingChanges() {
    vscode.postMessage({ type: "discardPending" });
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
      vscode.postMessage({
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
    const a = (before || "").split("\n");
    const b = (after || "").split("\n");
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
      } else if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) {
        rows.push({ type: "add", right: b[j], rightNum: j + 1 });
        j++;
      } else if (i < n) {
        rows.push({ type: "del", left: a[i], leftNum: i + 1 });
        i++;
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
    vscode.postMessage({ type: "openFile", path: filePath, focus: Boolean(focus) });
  }

  function openDiffMessage(path, before, after, sideBySide) {
    vscode.postMessage({
      type: "openDiff",
      path,
      before: before || "",
      after: after || "",
      focus: !sideBySide,
      sideBySide: Boolean(sideBySide),
    });
  }

