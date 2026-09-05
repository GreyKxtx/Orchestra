  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    switch (msg.type) {
      case "status": {
        const st = msg.status || "";
        if (st === "error") {
          setChromeHint(msg.detail || "connection error", true);
        } else if (st === "connecting") {
          busyStatusText = msg.detail || "Connecting…";
          setChromeHint(msg.detail || "Connecting…", false);
        } else if (st === "running") {
          busyStatusText = "Working…";
          setChromeHint("Working…", false);
        } else {
          setChromeHint("", false);
        }
        setBusy(st === "running" || st === "connecting");
        break;
      }
      case "ready":
        setChromeHint("", false);
        setBusy(false);
        break;
      case "turnInFlight":
        // Webview was rebuilt (e.g. returning from Settings) while the agent
        // turn is still running — re-arm the busy UI before the replayed
        // projection arrives.
        setBusy(true);
        break;
      case "header":
        if (msg.sessionId && msg.sessionId !== activeSessionId) {
          // Session switch: drop the previous session's turn breakdown.
          lastTurnUsage = null;
          turnCostAccum = 0;
        }
        activeSessionId = msg.sessionId || activeSessionId;
        updateActiveTabTitle(msg.title || "New chat");
        setModelLabel(msg.model || "");
        if (modelLabelEl && msg.provider) {
          modelLabelEl.title = `${msg.provider} · ${msg.model || ""}`;
        }
        if (typeof msg.sessionCost === "number") {
          sessionCostUSD = msg.sessionCost;
          renderCostChip();
        }
        if (modeId === "orchestra") {
          // Keep the tier breakdown pill; header carries the single main model.
          renderOrchestraPill();
        }
        break;
      case "sessionList": {
        if (!sessionMenuList) {
          break;
        }
        sessionMenuList.innerHTML = "";
        const sessions = Array.isArray(msg.sessions) ? msg.sessions : [];
        if (sessions.length === 0) {
          const empty = document.createElement("div");
          empty.className = "menu-section";
          empty.textContent = "No saved sessions";
          sessionMenuList.appendChild(empty);
          break;
        }
        /** Timestamp for grouping: prefer updated_at, fall back to the id (YYYYMMDDTHHMMSS-xxxx). */
        const sessionDate = (s) => {
          const iso = s.updated_at || s.created_at || "";
          const d = iso ? new Date(iso) : null;
          if (d && !Number.isNaN(d.getTime())) {
            return d;
          }
          const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s.id || "");
          return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : null;
        };
        const dayKey = (d) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
        const now = new Date();
        const todayKey = dayKey(now);
        const yesterdayKey = dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1));
        const dayLabel = (d) => {
          const k = dayKey(d);
          if (k === todayKey) {
            return "Today";
          }
          if (k === yesterdayKey) {
            return "Yesterday";
          }
          return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
        };
        let lastGroup = "";
        sessions.slice(0, 100).forEach((s) => {
          const d = sessionDate(s);
          const group = d ? dayLabel(d) : "Older";
          if (group !== lastGroup) {
            lastGroup = group;
            const header = document.createElement("div");
            header.className = "menu-section";
            header.textContent = group;
            sessionMenuList.appendChild(header);
          }
          const row = document.createElement("div");
          row.className = "menu-item session-row";
          row.setAttribute("role", "button");
          row.setAttribute("tabindex", "0");
          row.setAttribute("data-session-id", s.id);
          row.title = [s.model, s.msg_count ? `${s.msg_count} messages` : ""].filter(Boolean).join(" · ");

          const label = document.createElement("span");
          label.className = "session-row-title";
          label.textContent = s.title || s.id;
          row.appendChild(label);

          if (d) {
            const time = document.createElement("span");
            time.className = "session-row-time";
            time.textContent = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
            row.appendChild(time);
          }

          const del = document.createElement("span");
          del.className = "session-row-del";
          del.setAttribute("data-delete-session", s.id);
          del.title = "Delete chat";
          del.textContent = "✕";
          row.appendChild(del);

          sessionMenuList.appendChild(row);
        });
        break;
      }
      case "sessionTabs": {
        renderSessionTabs(msg.tabs, msg.activeId);
        break;
      }
      case "models": {
        renderModelList(Array.isArray(msg.models) ? msg.models : [], msg.current || currentModel);
        break;
      }
      case "providerModels":
        renderProviderModels(msg);
        break;
      case "orchestraRoles": {
        orchestraRolesInfo = {
          roles: Array.isArray(msg.roles) ? msg.roles : [],
          defaultTier: msg.defaultTier || "",
        };
        if (modeId === "orchestra") {
          renderOrchestraPill();
          if (modelMenu?.classList.contains("open")) {
            renderOrchestraRolesMenu();
          }
        }
        break;
      }
      case "pendingOps": {
        const payload = msg.payload || {};
        pendingState = {
          ops: Array.isArray(payload.ops) ? payload.ops : [],
          diff: Array.isArray(payload.diff) ? payload.diff : [],
        };
        diffReviewCursor = 0;
        renderPendingBar();
        void syncToolDiffPreviews();
        break;
      }
      case "pendingCleared":
        pendingState = { ops: [], diff: [] };
        diffReviewCursor = 0;
        renderPendingBar();
        syncToolDiffStats();
        break;
      case "permissionRequest":
        showPermissionOverlay(msg.request || {});
        break;
      case "questionAsk":
        showQuestionOverlay(Array.isArray(msg.questions) ? msg.questions : []);
        break;
      case "clearMessages":
        sendQueue = [];
        renderSendQueue();
        resetContextUsage();
        if (messagesEl) {
          messagesEl.innerHTML = "";
        }
        assistantBubble = null;
        resetTurnState();
        toolBlocks.clear();
        toolArgs.clear();
        execSteps.clear();
        todos = [];
        todosExpanded = false;
        todosHadOpen = false;
        subagents = [];
        subagentByTask.clear();
        renderSubagents();
        renderTodos();
        setWorkflow("", false);
        break;
      case "history": {
        const list = Array.isArray(msg.messages) ? msg.messages : [];
        const renderOne = (m) => {
          const role = m.role === "user" ? "user" : m.role === "system" ? "error" : "assistant";
          const opts = {
            uiIndex: typeof m.uiIndex === "number" ? m.uiIndex : undefined,
            files: Array.isArray(m.files) ? m.files : undefined,
            reasoning: typeof m.reasoning === "string" ? m.reasoning : undefined,
            toolBlocks: Array.isArray(m.toolBlocks) ? m.toolBlocks : undefined,
          };
          appendMsg(role, m.text || "", opts);
        };
        // Long sessions: render only the tail eagerly. Hundreds of markdown
        // bubbles + diff cards freeze the webview for seconds; older messages
        // expand on demand.
        const HISTORY_RENDER_CAP = 60;
        if (list.length > HISTORY_RENDER_CAP) {
          const hidden = list.slice(0, list.length - HISTORY_RENDER_CAP);
          const shown = list.slice(list.length - HISTORY_RENDER_CAP);
          const moreBtn = document.createElement("button");
          moreBtn.type = "button";
          moreBtn.className = "history-more";
          moreBtn.textContent = `Show ${hidden.length} older messages`;
          moreBtn.addEventListener("click", () => {
            moreBtn.remove();
            const frag = document.createDocumentFragment();
            const anchor = messagesEl?.firstChild || null;
            // Temporarily redirect appendMsg output by rendering then moving:
            // simplest correct approach — render into the live container and
            // reinsert before the previously-first node in order.
            const beforeCount = messagesEl ? messagesEl.childNodes.length : 0;
            hidden.forEach(renderOne);
            if (messagesEl && anchor) {
              const added = [];
              for (let i = beforeCount; i < messagesEl.childNodes.length; i++) {
                added.push(messagesEl.childNodes[i]);
              }
              added.forEach((n) => frag.appendChild(n));
              messagesEl.insertBefore(frag, anchor);
            }
            void syncToolDiffPreviews();
          });
          messagesEl?.appendChild(moreBtn);
          shown.forEach(renderOne);
        } else {
          list.forEach(renderOne);
        }
        void syncToolDiffPreviews();
        break;
      }
      case "queueUpdate":
        sendQueue = Array.isArray(msg.items) ? msg.items : [];
        renderSendQueue();
        break;
      case "turnStart":
        beginTurn();
        break;
      case "userEcho":
        appendMsg("user", msg.text, {
          uiIndex: typeof msg.uiIndex === "number" ? msg.uiIndex : undefined,
          files: Array.isArray(msg.files) ? msg.files : undefined,
        });
        break;
      case "reasoningDelta": {
        const body = ensureReasoning();
        if (body && typeof msg.content === "string") {
          body.textContent = (body.textContent || "") + msg.content;
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "delta":
      case "deltaSync": {
        if (busy) {
          busyStatusText = "Writing…";
          updateBusyUi();
        }
        const bubble = ensureAssistant();
        if (bubble && typeof msg.content === "string") {
          if (msg.type === "deltaSync") {
            streamRawText = msg.content;
          } else {
            streamRawText += msg.content;
          }
          scheduleAssistantMarkdown(
            bubble,
            sanitizeAssistantStream(stripFinalEnvelope(streamRawText))
          );
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "discardAssistantBubble": {
        flushAssistantMarkdown();
        if (assistantBubble) {
          assistantBubble.remove();
          assistantBubble = null;
        }
        streamRawText = "";
        break;
      }
      case "attachmentPreview":
        if (msg.path) {
          openExternalFile(msg.path, false);
        } else {
          appendMsg("system", `Cannot open attachment: ${msg.name || "file"}`);
        }
        break;
      case "childLifecycle":
        handleChildLifecycle(msg);
        break;
      case "diffViewer":
        showDiffViewer(msg.path || "", msg.before || "", msg.after || "", msg.language || "");
        break;
      case "highlightResult": {
        const cb = highlightWaiters.get(msg.requestId || "");
        if (cb) {
          highlightWaiters.delete(msg.requestId || "");
          cb(Array.isArray(msg.lines) ? msg.lines : []);
        }
        break;
      }
      case "toolBlock":
        handleToolBlock(msg);
        break;
      case "execChunk":
        if (typeof msg.step === "number" && typeof msg.chunk === "string") {
          appendExecChunk(msg.step, msg.chunk);
        }
        break;
      case "todosUpdate":
        todos = Array.isArray(msg.todos) ? msg.todos : [];
        renderTodos();
        break;
      case "stepUsage": {
        const u = msg.usage || {};
        if (msg.scope !== "child") {
          // Context gauge tracks the main agent only; worker windows differ.
          ctxState.prompt = typeof u.prompt_tokens === "number" ? u.prompt_tokens : 0;
          ctxState.completion = typeof u.completion_tokens === "number" ? u.completion_tokens : 0;
          ctxState.estimated = u.source === "estimate";
          if (Array.isArray(u.breakdown) && u.breakdown.length > 0) {
            ctxState.breakdown = u.breakdown;
          }
          renderContextUi();
        }
        // Live spend: each finished LLM call (planner step, worker step)
        // reports its cost here — update the chip mid-turn instead of
        // waiting for the end-of-turn turnUsage summary.
        if (typeof u.cost_usd === "number" && u.cost_usd > 0) {
          turnCostAccum += u.cost_usd;
          renderCostChip();
        }
        break;
      }
      case "turnUsage": {
        // Server total is authoritative — drop the live per-step estimate.
        turnCostAccum = 0;
        lastTurnUsage = msg.usage || null;
        if (typeof msg.sessionCost === "number" && msg.sessionCost > 0) {
          sessionCostUSD = msg.sessionCost;
        } else if (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0) {
          sessionCostUSD += lastTurnUsage.cost_usd;
        }
        renderCostChip();
        appendTurnCostNote(lastTurnUsage);
        break;
      }
      case "credits": {
        creditsInfo = {
          supported: msg.supported === true,
          provider: msg.provider || "",
          balance: typeof msg.balance === "number" ? msg.balance : 0,
        };
        renderCostChip();
        break;
      }
      case "contextInfo": {
        const info = msg.info || {};
        if (typeof info.contextLimit === "number" && info.contextLimit > 0) {
          ctxState.limit = info.contextLimit;
        }
        if (typeof info.maxResponseTokens === "number" && info.maxResponseTokens > 0) {
          ctxState.maxResponse = info.maxResponseTokens;
        }
        renderContextUi();
        break;
      }
      case "workflowStage": {
        const st = msg.stage || {};
        const stageId = st.stage_id || st.stageId || "";
        if (msg.phase === "start") {
          if (st.name && st.name !== workflowActiveName) {
            workflowActiveName = st.name;
            workflowStages.clear();
          }
          setWorkflow(st.name || "workflow", true);
          if (stageId) {
            workflowStages.set(stageId, {
              id: stageId,
              name: stageId,
              state: "running",
              attempt: st.attempt || 0,
            });
            renderWorkflowStages();
          }
        } else if (msg.phase === "done" && stageId) {
          const slot = workflowStages.get(stageId) || {
            id: stageId,
            name: stageId,
            state: "pending",
            attempt: st.attempt || 0,
          };
          const action = (st.action || "").toLowerCase();
          if (action.startsWith("redo")) {
            slot.state = "redo";
          } else if (action === "fail") {
            slot.state = "fail";
          } else {
            slot.state = "done";
          }
          workflowStages.set(stageId, slot);
          renderWorkflowStages();
        }
        break;
      }
      case "healthStatus":
        setLspStatus(msg.lspStatus || "");
        break;
      case "systemNote":
        appendMsg("system", msg.text || "");
        break;
      case "mentionResults": {
        const hit = inputEl ? detectPaletteQuery(inputEl.value) : null;
        if (!hit || hit.mode !== "mention" || hit.query !== (msg.query ?? "")) {
          break;
        }
        renderPalette("mention", Array.isArray(msg.files) ? msg.files : []);
        break;
      }
      case "tool": {
        const toolName = msg.toolName || "tool";
        const line = msg.done
          ? `✓ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`
          : `→ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`;
        appendMsg("tool", line);
        assistantBubble = null;
        break;
      }
      case "error":
        appendMsg("error", msg.message || "error");
        break;
      case "turnComplete":
        flushAssistantMarkdown();
        finalizeReasoningSummary();
        if (toolTraceEl) {
          toolTraceEl.open = false;
        }
        if (!msg.queuedNext) {
          setBusy(false);
        }
        assistantBubble = null;
        resetTurnState();
        if (!msg.ok) {
          setChromeHint("turn failed", true);
        }
        break;
      case "filesPicked": {
        if (Array.isArray(msg.files)) {
          for (const f of msg.files) {
            addFileRef(f);
          }
        }
        break;
      }
      default:
        break;
    }
  });

  /**
   * Small grey transcript note with the turn cost. In orchestra mode (or any
   * multi-model turn) each model gets its own "model $cost · N×" segment so
   * it is clear what each tier position cost.
   */
  function appendTurnCostNote(usage) {
    if (!messagesEl || !usage) {
      return;
    }
    const cost = typeof usage.cost_usd === "number" ? usage.cost_usd : 0;
    if (cost <= 0) {
      return;
    }
    const entries = Array.isArray(usage.entries) ? usage.entries : [];
    const parts = ["turn " + formatUsd(cost)];
    // Prompt-cache hit, when the provider reports one (Anthropic via a
    // gateway or native). Local models never set this. Without it, a long
    // Anthropic-through-OpenRouter turn that re-billed its whole transcript
    // every step looked the same as one that cached it — this is what would
    // have shown the field run's $2.18 turn was paying full price.
    const cached = typeof usage.cached_prompt_tokens === "number" ? usage.cached_prompt_tokens : 0;
    const promptTotal = typeof usage.prompt_tokens === "number" ? usage.prompt_tokens : 0;
    if (cached > 0 && promptTotal > 0) {
      parts.push("cache " + Math.round((cached / promptTotal) * 100) + "%");
    }
    if (entries.length > 1 || (modeId === "orchestra" && entries.length > 0)) {
      entries.forEach((en) => {
        const calls = typeof en.calls === "number" && en.calls > 1 ? " ×" + en.calls : "";
        parts.push(shortModelName(en.model) + " " + formatUsd(en.cost_usd || 0) + calls);
      });
    }
    const el = document.createElement("div");
    el.className = "usage-note";
    el.textContent = parts.join("   ·   ");
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  document.addEventListener("keydown", (e) => {
    if (!diffReviewActive()) return;
    const n = pendingState.diff.length;
    if (!n) return;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      diffReviewCursor = Math.max(0, diffReviewCursor - 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      diffReviewCursor = Math.min(n - 1, diffReviewCursor + 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      applyPendingChanges();
    }
  });

  initModeMenu();
  initEffortMenu();
  initAccessMenu();
  syncModeUi();
  syncEffortUi();
  syncAccessUi();
  renderContextUi();
  autoGrow();
  vscode.postMessage({ type: "ready" });
