  // Inbound: agent/event and exec/output_chunk -> renderer messages.
  //
  // The parent transcript is accumulated here rather than appended by the
  // renderer, because the core streams tokens and the renderer redraws the
  // whole assistant bubble (deltaSync). Child-scoped events belong to a
  // subagent's own trace; they must not be folded into the parent's text —
  // see ui/vscode/src/chat/panel.ts:1589-1604 for the same rule.

  /** @type {Map<string, string>} */
  const turnTextByProject = new Map();
  /** @type {Map<string, Map<string, any>>} */
  const liveToolBlocksByProject = new Map();
  // The rest of the turn, kept for the same reason: the core records the
  // person's message itself but never the answer, so whatever is going to be
  // in the session file afterwards has to be accumulated here and written
  // back when the turn ends (saveAssistantTurn in 10-adapter-session.js).
  /** @type {Map<string, string>} */
  const turnReasoningByProject = new Map();
  /** @type {Map<string, any[]>} */
  const turnToolsByProject = new Map();
  /** @type {Map<string, any>} */
  const turnUsageByProject = new Map();

  // Named blocksForProject, not toolBlocks: ui/vscode/media/chat-src/01-dom-state.js
  // already declares a top-level `const toolBlocks = new Map()`, and the whole
  // bundle is one IIFE — a same-named top-level function here would be a
  // duplicate declaration and fail to parse. That file may not change, so this
  // one avoids the name instead.
  /** @param {string} projectId */
  function blocksForProject(projectId) {
    let m = liveToolBlocksByProject.get(projectId);
    if (!m) {
      m = new Map();
      liveToolBlocksByProject.set(projectId, m);
    }
    return m;
  }

  /**
   * Every notification from every connection lands here first. A project the
   * renderer is not showing contributes its state to the rail and nothing to
   * the transcript: folding a background project's text into the visible
   * bubble is the bug this routing exists to prevent.
   * @param {string} projectId @param {any} msg
   */
  function noteProjectEvent(projectId, msg) {
    const st = projectState(projectId);
    const before = st.status;

    if (msg.method === "agent/event") {
      const ev = msg.params || {};
      switch (ev.type) {
        case "tool_call_start":
        case "message_delta":
        case "reasoning_delta":
          if (st.status === "idle") st.status = "working";
          break;
        case "done":
        case "error":
          // A turn can end (or error out) while a permission/question prompt
          // is still outstanding — e.g. the agent errored before the tool
          // that raised it ever got an answer. Left alone, pendingAsk keeps a
          // JSON-RPC id nobody is waiting on, the rail shows a permanent
          // "asking" badge, and switching in re-raises a prompt whose reply
          // goes nowhere. Clear it here, and if that ask is the one currently
          // on screen, take the overlay down with it — the displayed-ask
          // state and the overlay must always come down together (see
          // setDisplayedAsk / clearDisplayedAsk in 30-adapter-asks.js).
          st.status = "idle";
          if (st.pendingAsk) {
            st.pendingAsk = null;
            if (isDisplayedAskFor(projectId)) {
              clearDisplayedAsk();
              const overlay = document.getElementById("overlay");
              if (overlay) {
                overlay.classList.add("hidden");
              }
            }
          }
          break;
        default:
          break;
      }
    }

    if (projectId === currentProjectId) {
      handleNotification(projectId, msg);
    }
    return st.status !== before;
  }

  /** @param {string} projectId @param {any} msg */
  function handleNotification(projectId, msg) {
    // The trajectory sees the raw notification, before the chat's lossy
    // translation below: one live row per event, reconciled against the log
    // when the turn ends. Only these four methods are recorded by the core's
    // tee, so only these four are forwarded — and only for the session
    // currently on screen. A session switch within this project (new/open
    // session) can leave an old turn still streaming, and its notifications
    // must not paint into a pane that now shows a different session. An
    // event with no session_id (workflow/stage_*, which is not
    // session-scoped at all) is forwarded unfiltered — there is nothing to
    // check it against.
    if (
      msg.method === "agent/event" ||
      msg.method === "exec/output_chunk" ||
      msg.method === "workflow/stage_start" ||
      msg.method === "workflow/stage_done"
    ) {
      const evSessionId = msg.params && msg.params.session_id;
      const onScreenSessionId = projectState(projectId).sessionId;
      if (!evSessionId || evSessionId === onScreenSessionId) {
        toRenderer({ type: "trajectoryEvent", event: { type: msg.method, data: msg.params || {} } });
      }
    }
    if (msg.method === "exec/output_chunk") {
      toRenderer({ type: "execChunk", chunk: (msg.params && msg.params.chunk) || "" });
      return;
    }
    if (msg.method !== "agent/event") {
      return;
    }
    const ev = msg.params || {};
    const isChild = ev.scope === "child";
    const blocks = blocksForProject(projectId);

    switch (ev.type) {
      case "message_delta":
        if (ev.content && !isChild) {
          const acc = (turnTextByProject.get(projectId) || "") + ev.content;
          turnTextByProject.set(projectId, acc);
          toRenderer({ type: "deltaSync", content: acc });
        }
        break;

      case "reasoning_delta":
        if (ev.content && !isChild) {
          turnReasoningByProject.set(projectId, (turnReasoningByProject.get(projectId) || "") + ev.content);
          toRenderer({ type: "reasoningDelta", content: ev.content });
        }
        break;

      // A tool block is drawn in three phases, and the renderer (shared with
      // the editor's webview: chat-src/05e-messages.js) reads exactly the
      // fields the editor host posts — phase, toolCallId, toolName, argsDelta,
      // content. It does nothing at all with any other shape, which is why
      // these must stay in step with ui/vscode/src/chat/panel.ts.
      case "tool_call_start": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = {
          id: ev.tool_call_id,
          name: ev.tool_call_name || "tool",
          argsRaw: "",
          status: "running",
          result: "",
          startedAt: Date.now(),
        };
        blocks.set(ev.tool_call_id, block);
        toRenderer({
          type: "toolBlock",
          phase: "start",
          toolCallId: block.id,
          toolName: block.name,
          step: ev.step,
        });
        break;
      }

      case "tool_call_delta": {
        if (isChild || !ev.tool_call_id || !ev.args_delta) {
          break;
        }
        const block = blocks.get(ev.tool_call_id);
        if (block) {
          block.argsRaw += ev.args_delta;
        }
        toRenderer({
          type: "toolBlock",
          phase: "update",
          toolCallId: ev.tool_call_id,
          toolName: ev.tool_call_name || (block && block.name) || "tool",
          argsDelta: ev.args_delta,
          step: ev.step,
        });
        break;
      }

      case "tool_call_completed": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = blocks.get(ev.tool_call_id) || {
          id: ev.tool_call_id,
          name: ev.tool_call_name || "tool",
          argsRaw: "",
          startedAt: 0,
        };
        const content = ev.content || "";
        block.status = toolStatusOf(content);
        block.result = content;
        block.diagnostics = Array.isArray(ev.diagnostics) ? ev.diagnostics : undefined;
        // Only when this page saw the start; a tool whose start went to
        // another page has no honest duration to keep.
        block.durationMs = block.startedAt ? Date.now() - block.startedAt : 0;
        blocks.delete(ev.tool_call_id);
        turnToolsFor(projectId).push(block);
        toRenderer({
          type: "toolBlock",
          phase: "complete",
          toolCallId: block.id,
          toolName: block.name,
          content,
          diagnostics: block.diagnostics,
          step: ev.step,
        });
        break;
      }

      case "child_started":
        toRenderer({
          type: "childLifecycle",
          phase: "started",
          taskId: ev.task_id || "",
          parentToolCallId: ev.parent_tool_call_id,
          subagentType: ev.subagent_type,
          content: ev.content,
        });
        break;

      case "child_done":
        toRenderer({
          type: "childLifecycle",
          phase: "done",
          taskId: ev.task_id || "",
          parentToolCallId: ev.parent_tool_call_id,
          subagentType: ev.subagent_type,
          status: ev.status,
          error: ev.error,
        });
        break;

      case "step_usage":
      case "context_estimate": {
        // The context gauge under the composer. step_usage is what the server
        // counted for the step; context_estimate is the agent's own count of
        // what it is about to send, with the per-category breakdown the
        // popover draws — it comes first, and the measurement replaces it.
        // The renderer keeps a worker's (child) usage off the gauge and counts
        // only its cost, so the scope travels with the message; a worker's
        // estimate has neither use and stops here. So does a measurement of
        // nothing — a server that reports no usage must not empty the ring.
        const usage = stepUsageFrom(ev.data);
        if (!usage) {
          break;
        }
        if (ev.type === "context_estimate") {
          if (isChild) {
            break;
          }
          usage.source = "estimate";
        }
        if (!isChild && !(usage.prompt_tokens > 0)) {
          break;
        }
        // What the server counted goes onto the saved answer as well, so a
        // reopened session can show what the turn cost. An estimate does not:
        // only a measurement may be recorded as spend.
        if (!isChild && ev.type === "step_usage") {
          const acc = turnUsageByProject.get(projectId) || {};
          if (usage.prompt_tokens > 0) acc.promptCtx = usage.prompt_tokens;
          if (usage.completion_tokens > 0) acc.tokensOut = usage.completion_tokens;
          if (usage.total_tokens > 0) acc.tokensIn = usage.total_tokens;
          turnUsageByProject.set(projectId, acc);
        }
        toRenderer({ type: "stepUsage", usage, scope: ev.scope });
        break;
      }

      case "recoverable_error":
      case "error": {
        // Housekeeping about the context window travels on the error channel
        // because that is the channel the agent has, but none of it is a
        // failure. The editor's webview translates these in
        // streamSanitize.ts; the web UI never did, so a routine "the history
        // is about to be summarised" arrived in the transcript as the bare
        // word CONTEXT_PRESSURE under a red error heading.
        const note = compactionNotice(ev.content || "");
        if (note) {
          toRenderer({ type: "systemNote", text: note });
          break;
        }
        toRenderer({ type: "error", message: ev.content || "error" });
        break;
      }

      default:
        break;
    }
  }

  /**
   * The usage in a step_usage or context_estimate event, in the shape the
   * renderer's stepUsage handler reads — the same reading parseStepUsage in
   * ui/vscode/src/chat/panel.ts does for the editor. Fields that are not
   * numbers are left out rather than zeroed; a breakdown row without a key or
   * without tokens is dropped.
   * @param {any} data @returns {any|null}
   */
  function stepUsageFrom(data) {
    if (!data || typeof data !== "object") {
      return null;
    }
    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const usage = {
      prompt_tokens: num(data.prompt_tokens),
      completion_tokens: num(data.completion_tokens),
      total_tokens: num(data.total_tokens),
      cost_usd: num(data.cost_usd),
      source: typeof data.source === "string" ? data.source : undefined,
    };
    const breakdown = Array.isArray(data.breakdown)
      ? data.breakdown
          .filter((b) => b && typeof b === "object")
          .map((b) => ({
            key: typeof b.key === "string" ? b.key : "",
            label: typeof b.label === "string" ? b.label : "",
            tokens: num(b.tokens) || 0,
          }))
          .filter((b) => b.key !== "" && b.tokens > 0)
      : [];
    if (breakdown.length > 0) {
      usage.breakdown = breakdown;
    }
    return usage;
  }

  /**
   * The plain-language version of a context-housekeeping notice, or "" when
   * the message is a real error. Kept in step with
   * ui/vscode/src/chat/streamSanitize.ts, which does the same job for the
   * editor's webview.
   * @param {string} message
   */
  function compactionNotice(message) {
    const m = String(message || "").trim();
    if (m === "CONTEXT_PRESSURE") {
      return "The context is nearly full — the history will be summarised before the next step.";
    }
    if (m === "CONTEXT_COMPACTED") {
      return "History summarised; the turn carries on.";
    }
    if (/контекст переполнен/i.test(m)) {
      return "Summarising the chat — " + m;
    }
    return "";
  }

  /** The tools this project's turn has finished, in the order they ran. */
  function turnToolsFor(projectId) {
    let list = turnToolsByProject.get(projectId);
    if (!list) {
      list = [];
      turnToolsByProject.set(projectId, list);
    }
    return list;
  }

  /**
   * The persisted spelling of a finished tool's status, read off its result
   * the way the editor host's toolStatusFromResult does.
   * @param {string} content
   */
  function toolStatusOf(content) {
    const s = String(content || "");
    if (s.startsWith("error: ")) return "failed";
    if (s.startsWith("skipped: ")) return "skipped";
    return "completed";
  }

  /**
   * The turn's answer as a ui_messages row, or null when the model said
   * nothing at all. Mirrors buildAssistantProjection in
   * ui/vscode/src/chat/turnProjection.ts — the same session file is read back
   * by the editor, the TUI and this page, so the shape is not ours to invent.
   * @param {string} projectId
   */
  function assistantTurnProjection(projectId) {
    const text = (turnTextByProject.get(projectId) || "").trim();
    const reasoning = (turnReasoningByProject.get(projectId) || "").trim();
    const tools = (turnToolsByProject.get(projectId) || []).map((t) => {
      const b = { name: t.name || "tool", status: t.status || "completed" };
      if (t.id) b.id = t.id;
      if (t.argsRaw) b.args_raw = t.argsRaw;
      if (t.result) b.result = t.result;
      if (t.diagnostics && t.diagnostics.length) b.diagnostics = t.diagnostics;
      // Absent rather than zero: a persisted 0 renders as "0ms" beside tools
      // that really did take no measurable time.
      if (t.durationMs > 0) b.duration_ms = t.durationMs;
      return b;
    });
    if (!text && !reasoning && tools.length === 0) {
      return null;
    }
    const msg = { role: "assistant", text };
    if (reasoning) msg.reasoning = reasoning;
    if (tools.length) msg.tool_blocks = tools;
    const usage = turnUsageByProject.get(projectId) || {};
    if (usage.promptCtx > 0) msg.prompt_ctx = usage.promptCtx;
    if (usage.tokensIn > 0) msg.tokens_in = usage.tokensIn;
    if (usage.tokensOut > 0) msg.tokens_out = usage.tokensOut;
    return msg;
  }

  // A new turn starts with an empty transcript — for the project whose turn it
  // is, which is always the one the renderer is showing.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnTextByProject.set(currentProjectId, "");
      turnReasoningByProject.set(currentProjectId, "");
      turnToolsByProject.set(currentProjectId, []);
      turnUsageByProject.delete(currentProjectId);
      blocksForProject(currentProjectId).clear();
    }
  });
