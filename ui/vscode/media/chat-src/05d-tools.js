  function toolKind(name) {
    const n = (name || "").toLowerCase();
    if (["read", "fs.read"].includes(n)) return "read";
    if (["ls", "list", "fs.list"].includes(n)) return "list";
    if (["write", "edit", "fs.write", "file.write_atomic"].includes(n)) return "write";
    if (["grep", "search.text", "search"].includes(n)) return "search";
    if (["glob"].includes(n)) return "glob";
    if (["symbols", "code.symbols"].includes(n)) return "symbols";
    if (["bash", "exec.run", "exec"].includes(n)) return "exec";
    if (["todowrite", "task", "task_spawn"].includes(n)) return "task";
    return "other";
  }

  function toolIcon(name) {
    switch (toolKind(name)) {
      case "read":
        return "→";
      case "list":
        return "≡";
      case "write":
        return "←";
      case "search":
        return "✱";
      case "glob":
        return "✦";
      case "symbols":
        return "◈";
      case "exec":
        return "$";
      case "task":
        return "▣";
      default:
        return "•";
    }
  }

  function toolDisplayName(name) {
    switch (toolKind(name)) {
      case "read":
        return "Read";
      case "list":
        return "Ls";
      case "write":
        return name === "edit" ? "Edit" : "Write";
      case "search":
        return "Grep";
      case "glob":
        return "Glob";
      case "symbols":
        return "Symbols";
      case "exec":
        return "Shell";
      case "task":
        return "Task";
      default:
        return name || "Tool";
    }
  }

  function parseToolArgs(raw) {
    const out = {};
    const text = (raw || "").trim();
    if (!text) return out;
    try {
      return JSON.parse(text);
    } catch {
      const patched = text + '"'.repeat(text.split('"').length % 2 === 0 ? 0 : 1);
      let depth = 0;
      let inStr = false;
      let esc = false;
      for (const ch of patched) {
        if (esc) {
          esc = false;
          continue;
        }
        if (ch === "\\") {
          esc = true;
          continue;
        }
        if (ch === '"') inStr = !inStr;
        else if (!inStr) {
          if (ch === "{") depth++;
          if (ch === "}") depth--;
        }
      }
      let fix = patched;
      for (let i = 0; i < depth; i++) fix += "}";
      try {
        return JSON.parse(fix);
      } catch {
        return out;
      }
    }
  }

  function toolPathFromArgs(name, argsRaw, content) {
    const args = parseToolArgs(argsRaw);
    let path =
      args.path || args.filePath || args.file_path || args.target || "";
    if (!path && content) {
      try {
        const parsed = JSON.parse(content);
        path = parsed.path || parsed.filePath || parsed.file_path || "";
      } catch {
        /* ignore */
      }
    }
    return typeof path === "string" ? path.trim() : "";
  }

  function toolPreviewLine(name, argsRaw, content) {
    const disp = toolDisplayName(name);
    const path = toolPathFromArgs(name, argsRaw, content);
    if (path) {
      return `${disp} ${basename(path)}`;
    }
    if (toolKind(name) === "search" || toolKind(name) === "glob") {
      const args = parseToolArgs(argsRaw);
      const pat = args.pattern || args.query || "";
      return pat ? `${disp} "${pat}"` : disp;
    }
    if (toolKind(name) === "exec") {
      const args = parseToolArgs(argsRaw);
      const cmd = args.command || args.description || "";
      if (cmd) {
        return cmd.length > 56 ? cmd.slice(0, 53) + "…" : cmd;
      }
    }
    if (content && content.length < 80) {
      return `${disp} · ${content}`;
    }
    return disp;
  }

  function updateToolHead(block, name, argsRaw, content, running) {
    const head = block.querySelector(".tool-head");
    if (!head) return;
    const icon = head.querySelector(".tool-icon");
    const label = head.querySelector(".tool-label");
    const sub = head.querySelector(".tool-sub");
    const statsEl = head.querySelector(".tool-stats");
    const filePath = toolPathFromArgs(name, argsRaw, content);
    if (filePath) {
      block.dataset.filePath = filePath;
    }
    if (icon) icon.textContent = running ? toolIcon(name) : "✓";
    if (label) label.textContent = toolPreviewLine(name, argsRaw, content);
    if (sub) {
      sub.textContent = filePath && filePath.includes("/") ? filePath : "";
    }
    if (statsEl && filePath && toolKind(name) === "write") {
      const diff = findDiffForPath(filePath);
      if (diff) {
        statsEl.innerHTML = statsHtml(countDiffStats(diff.before, diff.after));
      }
    }
  }

  function insertFileChangeCard(path, content, parent) {
    const host = parent || messagesEl;
    if (!host || !path) return;
    if (parent && parent.classList.contains("tool-block")) {
      attachToolDiffShell(parent, path);
      return;
    }
    const diff = findDiffForPath(path);
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(parent || document.createElement("div"), path, diff.before || "", diff.after || "");
      return;
    }
    attachToolDiffShell(parent || document.createElement("div"), path);
  }

  function renderSubagents() {
    if (!subagentsBar || !subagentsTree) return;
    const active = subagents.filter((s) => s.status === "running" || s.status === "waiting");
    if (!subagents.length) {
      subagentsBar.classList.add("hidden");
      subagentsTree.innerHTML = "";
      return;
    }
    subagentsBar.classList.remove("hidden");
    subagentsTree.innerHTML = "";
    subagents.slice(-12).forEach((sa) => {
      const row = document.createElement("div");
      row.className = "subagent-row status-" + sa.status;
      const icon = sa.status === "done" ? "✓" : sa.status === "error" ? "✗" : sa.status === "waiting" ? "⏳" : "◉";
      const toolHint = sa.toolCount ? ` · ${sa.toolCount} tools` : "";
      const tierL = subagentTierLabel(sa.tier);
      const tierHtml = tierL
        ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(sa.model || "")}">${tierL}</span>`
        : "";
      row.innerHTML =
        `<span class="subagent-icon">${icon}</span>` +
        `<span class="subagent-type">${escapeAttr(sa.type || "agent")}</span>` +
        tierHtml +
        `<span class="subagent-label">${escapeAttr(sa.label || sa.taskId || sa.id)}${toolHint}</span>`;
      if (sa.taskId) {
        row.dataset.taskId = sa.taskId;
        row.title = sa.model ? `${sa.taskId} · ${sa.model}` : sa.taskId;
      }
      subagentsTree.appendChild(row);
      const node = sa.taskId ? subagentByTask.get(sa.taskId) : null;
      if (node?.toolsEl && node.toolCount > 0) {
        const nest = document.createElement("div");
        nest.className = "subagent-nested-hint";
        nest.textContent = `${node.toolCount} tool step${node.toolCount === 1 ? "" : "s"} (in chat)`;
        subagentsTree.appendChild(nest);
      }
    });
    if (subagents.length > 12) {
      const more = document.createElement("div");
      more.className = "subagent-more";
      more.textContent = `+${subagents.length - 12} more`;
      subagentsTree.appendChild(more);
    }
  }

  function trackSubagent(msg, phase, argsRaw) {
    const name = msg.toolName || "";
    if (!["task", "task_spawn", "task_wait", "task_cancel"].includes(name)) {
      return;
    }
    const id = msg.toolCallId || `${name}-${msg.step ?? subagents.length}`;
    if (phase === "start") {
      subagents.push({
        id,
        type: "agent",
        label: toolDisplayName(name),
        status: name === "task_wait" ? "waiting" : "running",
      });
    } else if (phase === "update" || phase === "complete") {
      const sa = subagents.find((s) => s.id === id);
      if (!sa) return;
      const args = parseToolArgs(argsRaw || "");
      const st = args.subagent_type || args.subagentType || sa.type;
      const desc = args.description || args.prompt || args.goal || "";
      if (st) sa.type = String(st);
      if (desc) {
        const d = String(desc).trim();
        sa.label = d.length > 48 ? d.slice(0, 45) + "…" : d;
      }
      if (phase === "complete") {
        const err = (msg.content || "").toLowerCase().startsWith("error");
        sa.status = err ? "error" : "done";
      }
    }
    renderSubagents();
  }

  function renderWorkflowStages() {
    if (!workflowStagesEl) return;
    workflowStagesEl.innerHTML = "";
    for (const st of workflowStages.values()) {
      const pill = document.createElement("span");
      pill.className = "workflow-pill state-" + st.state;
      const glyph = st.state === "done" ? "✓" : st.state === "running" ? "⋯" : "○";
      pill.textContent = `${glyph} ${st.name || st.id}`;
      pill.title = st.id;
      workflowStagesEl.appendChild(pill);
    }
  }

  function setWorkflow(label, active) {
    if (!workflowBar || !workflowLabel) return;
    if (!active) {
      workflowBar.classList.add("hidden");
      workflowLabel.textContent = "";
      workflowActiveName = "";
      workflowStages.clear();
      renderWorkflowStages();
      return;
    }
    workflowBar.classList.remove("hidden");
    workflowLabel.textContent = label;
  }

  function formatTok(n) {
    const v = Math.max(0, Math.round(n || 0));
    if (v >= 1_000_000) return (v / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
    if (v >= 1000) return (v / 1000).toFixed(1).replace(/\.0$/, "") + "K";
    return String(v);
  }

  function resetContextUsage() {
    ctxState.prompt = 0;
    ctxState.completion = 0;
    ctxState.estimated = false;
    renderContextUi();
  }

  function renderContextUi() {
    const used = ctxState.prompt;
    const limit = ctxState.limit || 128000;
    const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
    if (contextRingFill) {
      contextRingFill.style.setProperty("--ctx-pct", String(pct));
    }
    if (ctxPct) {
      ctxPct.textContent = used > 0 ? `${pct}% full` : "—";
    }
    if (ctxSummary) {
      const est = ctxState.estimated ? "~" : "";
      ctxSummary.textContent =
        used > 0 ? `${est}${formatTok(used)} / ${formatTok(limit)} tokens` : `Up to ${formatTok(limit)} tokens`;
    }
    if (ctxBar) {
      ctxBar.innerHTML = "";
      const rows = [
        { label: "Prompt", tokens: ctxState.prompt, color: "#888888" },
        { label: "Completion", tokens: ctxState.completion, color: "#a78bfa" },
        { label: "Reply budget", tokens: ctxState.maxResponse, color: "#555555" },
      ];
      const total = Math.max(used + ctxState.maxResponse, 1);
      rows.forEach((r) => {
        if (r.tokens <= 0 && r.label !== "Reply budget") return;
        const seg = document.createElement("div");
        seg.className = "ctx-bar-seg";
        seg.style.width = `${Math.max(2, (r.tokens / total) * 100)}%`;
        seg.style.background = r.color;
        ctxBar.appendChild(seg);
      });
    }
    if (ctxRows) {
      ctxRows.innerHTML = "";
      const items = [
        { label: "Prompt context", tokens: ctxState.prompt, color: "#888888" },
        { label: "Completion", tokens: ctxState.completion, color: "#a78bfa" },
        { label: "Reserved for reply", tokens: ctxState.maxResponse, color: "#555555" },
      ];
      items.forEach((item) => {
        const row = document.createElement("div");
        row.className = "ctx-row";
        row.innerHTML = `<span class="ctx-swatch" style="background:${item.color}"></span><span class="ctx-row-label">${item.label}</span><span class="ctx-row-val">${formatTok(item.tokens)}</span>`;
        ctxRows.appendChild(row);
      });
    }
  }

  function showContextPopover(show) {
    ctxPopoverOpen = show;
    contextPopover?.classList.toggle("hidden", !show);
    contextBtn?.classList.toggle("open", show);
  }

  function updateStatusFooter() {
    /* tokens shown in context popover */
  }

  function setLspStatus(st) {
    if (!statusLsp) return;
    const label = (st || "").trim();
    statusLsp.textContent = label ? `LSP ${label}` : "";
    statusLsp.classList.toggle("active", label === "active");
  }

  function imageGalleryFromList(fileList, activePath) {
    if (!Array.isArray(fileList)) return [];
    return fileList
      .filter((f) => f && f.kind === "image")
      .map((f) => ({
        name: f.name || "Image",
        path: f.path,
        previewUri: f.previewUri,
      }));
  }

  function renderImagePreviewAt(index) {
    if (!imagePreview || !imagePreviewImg || !imagePreviewState.items.length) return;
    const total = imagePreviewState.items.length;
    const idx = ((index % total) + total) % total;
    imagePreviewState.index = idx;
    const item = imagePreviewState.items[idx];
    if (imagePreviewTitle) {
      imagePreviewTitle.textContent = item.name || "Image";
    }
    if (imagePreviewCounter) {
      imagePreviewCounter.textContent = total > 1 ? `${idx + 1} / ${total}` : "";
    }
    if (imagePreviewPrevBtn) {
      imagePreviewPrevBtn.classList.toggle("hidden", total <= 1);
    }
    if (imagePreviewNextBtn) {
      imagePreviewNextBtn.classList.toggle("hidden", total <= 1);
    }
    imagePreviewImg.src = item.previewUri || "";
    imagePreviewImg.alt = item.name || "Preview";
    imagePreview.classList.remove("hidden");
  }

  function showImagePreview(name, previewUri, filePath, galleryItems) {
    if (!imagePreview || !imagePreviewImg) return;
    const items = Array.isArray(galleryItems) && galleryItems.length
      ? galleryItems.filter((g) => g && g.previewUri)
      : [{ name: name || "Image", path: filePath || "", previewUri: previewUri || "" }];
    imagePreviewState.items = items;
    const startIdx = Math.max(
      0,
      items.findIndex((g) => g.path && filePath && g.path === filePath)
    );
    renderImagePreviewAt(startIdx >= 0 ? startIdx : 0);
  }

  function hideImagePreview() {
    imagePreview?.classList.add("hidden");
    if (imagePreviewImg) {
      imagePreviewImg.src = "";
    }
    imagePreviewState = { items: [], index: 0 };
  }

  function shiftImagePreview(delta) {
    if (!imagePreviewState.items.length) return;
    renderImagePreviewAt(imagePreviewState.index + delta);
  }

  async function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === "string" ? reader.result : "";
        const comma = result.indexOf(",");
        resolve(comma >= 0 ? result.slice(comma + 1) : result);
      };
      reader.onerror = () => reject(reader.error || new Error("read failed"));
      reader.readAsDataURL(file);
    });
  }

  async function attachFilesFromDataTransfer(dt) {
    if (!dt) return;
    const fileItems = [];
    if (dt.files && dt.files.length) {
      for (let i = 0; i < dt.files.length; i++) {
        fileItems.push(dt.files[i]);
      }
    }
    for (const file of fileItems) {
      if (!file) continue;
      if (file.size > MAX_ATTACH_BYTES) {
        appendMsg("system", `Skipped ${file.name || "file"}: exceeds 20 MB limit`);
        continue;
      }
      try {
        const dataBase64 = await readFileAsBase64(file);
        vscode.postMessage({
          type: "attachBytes",
          name: file.name || "attachment",
          mime: file.type || undefined,
          dataBase64,
        });
      } catch {
        /* ignore single file failure */
      }
    }
  }

  function onAttachmentClick(f, _isImage, e) {
    if (!f?.path) return;
    openExternalFile(f.path, Boolean(e?.shiftKey));
  }

  function renderMsgAttachments(parent, fileList) {
    if (!parent || !fileList || !fileList.length) return;
    const wrap = document.createElement("div");
    wrap.className = "msg-attachments";
    fileList.forEach((f) => {
      if (!f || typeof f.name !== "string") return;
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "msg-attach" + (isImage ? " msg-attach-image" : "");
      card.title = (f.path || f.name) + " · click: open in editor · Shift+click: focus";

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "msg-attach-thumb";
        img.src = f.previewUri || "";
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "msg-attach-meta";
      const name = document.createElement("span");
      name.className = "msg-attach-name";
      name.textContent = f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        onAttachmentClick(f, isImage, e);
      });
      wrap.appendChild(card);
    });
    parent.appendChild(wrap);
  }

  /** @param {string} text @param {{ reasoning?: string; toolBlocks?: any[] }} opts */
