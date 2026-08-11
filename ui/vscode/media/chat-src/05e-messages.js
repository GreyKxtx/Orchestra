  function appendHistoryAssistantTurn(text, opts) {
    if (!messagesEl) {
      return;
    }
    resetTurnState();
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "assistant-turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);

    const reasoning = (opts?.reasoning || "").trim();
    if (reasoning) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = "Thought briefly";
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningBody.textContent = reasoning;
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      assistantTurnInner.appendChild(reasoningDetails);
      reasoningDetails.open = false;
    }

    const tools = Array.isArray(opts?.toolBlocks) ? opts.toolBlocks : [];
    for (const tb of tools) {
      const id = tb.id || `${tb.name}-${toolBlocks.size}`;
      handleToolBlock({ phase: "start", toolCallId: id, toolName: tb.name || "tool" });
      if (tb.argsRaw) {
        handleToolBlock({
          phase: "update",
          toolCallId: id,
          toolName: tb.name || "tool",
          argsDelta: tb.argsRaw,
        });
      }
      handleToolBlock({
        phase: "complete",
        toolCallId: id,
        toolName: tb.name || "tool",
        content: tb.result || "",
        diagnostics: tb.diagnostics,
      });
      if (toolKind(tb.name) === "write" && (tb.diffBefore !== undefined || tb.diffAfter !== undefined)) {
        const block = toolBlocks.get(id);
        const fp = toolPathFromArgs(tb.name || "", tb.argsRaw || "", tb.result || "");
        if (block && fp) {
          rememberBlockDiff(block, tb.diffBefore || "", tb.diffAfter || "");
          void attachInlineToolDiff(block, fp, tb.diffBefore || "", tb.diffAfter || "");
        }
      }
    }

    if (text) {
      assistantBubble = document.createElement("div");
      applyAssistantMarkdown(assistantBubble, text);
      assistantTurnInner.appendChild(assistantBubble);
    }

    if (toolTraceEl) {
      toolTraceEl.open = false;
    }
    resetTurnState();
    void syncToolDiffPreviews();
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  /** @param {string} role @param {string} text @param {{ uiIndex?: number; files?: any[]; reasoning?: string; toolBlocks?: any[] }} [opts] */
  function appendMsg(role, text, opts) {
    if (!messagesEl) {
      return null;
    }
    const el = document.createElement("div");
    el.className = `msg ${role}`;
    if (role === "user") {
      if (typeof opts?.uiIndex === "number") {
        el.dataset.uiIndex = String(opts.uiIndex);
      }
      if (opts?.files?.length) {
        renderMsgAttachments(el, opts.files);
      }
      const wrap = document.createElement("div");
      wrap.className = "user-wrap";
      const body = document.createElement("div");
      body.className = "user-text";
      body.textContent = text;
      wrap.appendChild(body);
      if (typeof opts?.uiIndex === "number") {
        const rewind = document.createElement("button");
        rewind.type = "button";
        rewind.className = "rewind-btn";
        rewind.title = "Rewind to here";
        rewind.textContent = "↩ Rewind";
        rewind.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          const idx = typeof opts.uiIndex === "number" ? opts.uiIndex : Number(el.dataset.uiIndex);
          if (!Number.isFinite(idx) || idx < 0) {
            return;
          }
          rewind.disabled = true;
          vscode.postMessage({ type: "rewindToMessage", uiIndex: idx });
        });
        wrap.appendChild(rewind);
      }
      if (text || wrap.querySelector(".rewind-btn")) {
        el.appendChild(wrap);
      }
    } else if (role === "system") {
      el.className = "msg system";
      el.textContent = text;
    } else if (role === "assistant" && (opts?.toolBlocks?.length || opts?.reasoning)) {
      appendHistoryAssistantTurn(text, opts);
      return null;
    } else if (role === "assistant") {
      applyAssistantMarkdown(el, text);
    } else {
      el.textContent = text;
    }
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  /** @param {{ phase: string; toolCallId?: string; toolName: string; content?: string; argsDelta?: string; step?: number; diagnostics?: any[] }} msg */
  function handleToolBlock(msg) {
    if (!messagesEl) return;
    const id = toolBlockKey(msg);
    const kind = toolKind(msg.toolName);
    const host = messagesHostFor(msg);

    if (msg.phase === "start") {
      if (msg.scope !== "child") {
        commitPreToolText();
        bumpToolTraceCount(msg.toolName);
      } else {
        assistantBubble = null;
      }
      toolArgs.set(id, "");
      const block = document.createElement("div");
      block.className = `tool-block running kind-${kind}` + (msg.scope === "child" ? " child-tool" : "");
      block.dataset.toolId = id;
      if (msg.taskId) block.dataset.taskId = msg.taskId;
      if (typeof msg.step === "number") {
        block.dataset.step = String(msg.step);
        if (msg.scope !== "child") {
          execSteps.set(msg.step, block);
        }
      }
      if (kind === "write" && msg.scope !== "child") {
        block.classList.add("write-card-only");
        (host || messagesEl).appendChild(block);
        toolBlocks.set(id, block);
        if (msg.scope === "child" && msg.taskId) {
          const node = subagentByTask.get(msg.taskId);
          if (node) {
            node.toolCount = (node.toolCount || 0) + 1;
            renderSubagents();
          }
        } else {
          trackSubagent(msg, "start", "");
        }
        setChromeHint(toolDisplayName(msg.toolName) + "…", false);
        if (host) host.scrollTop = host.scrollHeight;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        return;
      }
      const head = document.createElement("button");
      head.type = "button";
      head.className = "tool-head";
      head.innerHTML =
        `<span class="tool-icon">${toolIcon(msg.toolName)}</span>` +
        `<span class="tool-label">${escapeAttr(toolDisplayName(msg.toolName))}</span>` +
        `<span class="tool-sub"></span>` +
        `<span class="tool-stats"></span>` +
        `<span class="tool-spinner"></span>` +
        (kind === "write" ? "" : `<span class="tool-chev">▾</span>`);
      let body = null;
      if (kind !== "write") {
        body = document.createElement("pre");
        body.className = "tool-body hidden";
        head.addEventListener("click", (e) => {
          const stats = e.target.closest?.(".tool-stats");
          const fp = block.dataset.filePath || "";
          if (stats && fp && stats.textContent.trim()) {
            e.preventDefault();
            e.stopPropagation();
            const d = findDiffForPath(fp);
            openDiffMessage(fp, d?.before || "", d?.after || "", e.shiftKey);
            return;
          }
          body.classList.toggle("hidden");
          head.classList.toggle("open");
        });
      } else {
        bindWriteToolHead(block, head);
      }
      block.appendChild(head);
      if (body) block.appendChild(body);
      (host || messagesEl).appendChild(block);
      toolBlocks.set(id, block);
      if (msg.scope === "child" && msg.taskId) {
        const node = subagentByTask.get(msg.taskId);
        if (node) {
          node.toolCount = (node.toolCount || 0) + 1;
          renderSubagents();
        }
      } else {
        trackSubagent(msg, "start", "");
      }
      setChromeHint(toolDisplayName(msg.toolName) + "…", false);
      if (host) host.scrollTop = host.scrollHeight;
      messagesEl.scrollTop = messagesEl.scrollHeight;
      return;
    }

    const block = toolBlocks.get(id);
    if (!block) return;

    if (msg.phase === "update" && msg.argsDelta) {
      const prev = toolArgs.get(id) || "";
      const next = prev + msg.argsDelta;
      toolArgs.set(id, next);
      updateToolHead(block, msg.toolName, next, "", true);
      if (toolKind(msg.toolName) === "write") {
        tryShowWriteDiff(block, msg.toolName, next, "");
      }
      if (msg.scope !== "child") {
        trackSubagent(msg, "update", next);
      }
      syncToolDiffStats();
      return;
    }

    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    const argsRaw = toolArgs.get(id) || "";

    if (msg.phase === "complete") {
      block.classList.remove("running");
      block.classList.add("done", `kind-${kind}`);
      const spinner = block.querySelector(".tool-spinner");
      if (spinner) spinner.remove();
      updateToolHead(block, msg.toolName, argsRaw, msg.content || "", false);
      if (head && kind !== "write") head.classList.remove("open");

      if (body && msg.content && kind !== "write") {
        body.textContent = msg.content.length > 8000 ? msg.content.slice(0, 8000) + "\n…" : msg.content;
        body.classList.add("hidden");
      }

      if (msg.diagnostics && msg.diagnostics.length) {
        const diag = document.createElement("div");
        diag.className = "tool-diags";
        msg.diagnostics.slice(0, 6).forEach((d) => {
          const line = document.createElement("div");
          line.className = "tool-diag " + (d.severity || "");
          line.textContent = `L${d.start_line}: ${d.message}`;
          diag.appendChild(line);
        });
        block.appendChild(diag);
      }

      if (kind === "write") {
        tryShowWriteDiff(block, msg.toolName, argsRaw, msg.content || "");
      }

      if (msg.scope !== "child") {
        trackSubagent(msg, "complete", argsRaw);
      }
      syncToolDiffStats();
      toolArgs.delete(id);
      assistantBubble = null;
      if (busy) {
        setChromeHint("", false);
      }
    }
    if (host) host.scrollTop = host.scrollHeight;
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function appendExecChunk(step, chunk) {
    const block = execSteps.get(step);
    if (!block) return;
    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    if (!body) return;
    body.classList.remove("hidden");
    if (head) head.classList.add("open");
    body.textContent = (body.textContent || "") + chunk;
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
  }
