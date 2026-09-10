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
    // tee, so only these four are forwarded.
    if (
      msg.method === "agent/event" ||
      msg.method === "exec/output_chunk" ||
      msg.method === "workflow/stage_start" ||
      msg.method === "workflow/stage_done"
    ) {
      toRenderer({ type: "trajectoryEvent", event: { type: msg.method, data: msg.params || {} } });
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
          toRenderer({ type: "reasoningDelta", content: ev.content });
        }
        break;

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
        toRenderer({ type: "toolBlock", block: { ...block } });
        break;
      }

      case "tool_call_delta": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = blocks.get(ev.tool_call_id);
        if (block) {
          block.argsRaw += ev.args_delta || "";
          toRenderer({ type: "toolBlock", block: { ...block } });
        }
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
          startedAt: Date.now(),
        };
        block.status = "done";
        block.result = ev.content || "";
        block.durationMs = Date.now() - (block.startedAt || Date.now());
        blocks.delete(ev.tool_call_id);
        toRenderer({ type: "toolBlock", block: { ...block } });
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

      case "recoverable_error":
      case "error":
        toRenderer({ type: "error", message: ev.content || "error" });
        break;

      default:
        break;
    }
  }

  // A new turn starts with an empty transcript — for the project whose turn it
  // is, which is always the one the renderer is showing.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnTextByProject.set(currentProjectId, "");
      blocksForProject(currentProjectId).clear();
    }
  });
