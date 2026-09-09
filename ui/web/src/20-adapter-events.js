  // Inbound: agent/event and exec/output_chunk -> renderer messages.
  //
  // The parent transcript is accumulated here rather than appended by the
  // renderer, because the core streams tokens and the renderer redraws the
  // whole assistant bubble (deltaSync). Child-scoped events belong to a
  // subagent's own trace; they must not be folded into the parent's text —
  // see ui/vscode/src/chat/panel.ts:1589-1604 for the same rule.

  let turnText = "";
  /** @type {Map<string, any>} */
  const liveToolBlocks = new Map();

  /** @param {any} msg */
  function handleNotification(msg) {
    if (msg.method === "exec/output_chunk") {
      toRenderer({ type: "execChunk", chunk: (msg.params && msg.params.chunk) || "" });
      return;
    }
    if (msg.method !== "agent/event") {
      return;
    }
    const ev = msg.params || {};
    const isChild = ev.scope === "child";

    switch (ev.type) {
      case "message_delta":
        if (ev.content && !isChild) {
          turnText += ev.content;
          toRenderer({ type: "deltaSync", content: turnText });
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
        liveToolBlocks.set(ev.tool_call_id, block);
        toRenderer({ type: "toolBlock", block: { ...block } });
        break;
      }

      case "tool_call_delta": {
        if (isChild || !ev.tool_call_id) {
          break;
        }
        const block = liveToolBlocks.get(ev.tool_call_id);
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
        const block = liveToolBlocks.get(ev.tool_call_id) || {
          id: ev.tool_call_id,
          name: ev.tool_call_name || "tool",
          argsRaw: "",
          startedAt: Date.now(),
        };
        block.status = "done";
        block.result = ev.content || "";
        block.durationMs = Date.now() - (block.startedAt || Date.now());
        liveToolBlocks.delete(ev.tool_call_id);
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

  // A new turn starts with an empty transcript.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnText = "";
      liveToolBlocks.clear();
    }
  });
