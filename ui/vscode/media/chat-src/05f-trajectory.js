  // ---- Trajectory view ------------------------------------------------------
  //
  // The tab is a pure function from a session's event log to a tree of rows,
  // drawn by a thin renderer below. The log is what session.trajectory returns
  // and what the core's tee records: `{ seq, time_ms, type, data }` where
  // `type` is the notification method and `data` its params. A live event
  // forwarded mid-turn has the same shape minus seq and time_ms.

  /**
   * @typedef {{
   *   kind: "turn"|"step"|"tool"|"text"|"reasoning"|"error"|"stage"|"pending"|"route"|"other",
   *   depth: number,
   *   key: string,
   *   label: string,
   *   turnId: string,
   *   step?: number,
   *   startMs?: number,
   *   offsetMs?: number,
   *   durationMs?: number,
   *   tokensIn?: number,
   *   tokensOut?: number,
   *   outcome?: string,
   *   input?: string,
   *   output?: string,
   *   live: boolean,
   *   seq?: number
   * }} TrajRow
   */

  /**
   * buildTrajectoryTree turns recorded (or live) events into flat rows with a
   * depth. Pure: no DOM, no fragment state, no clock. Everything it knows
   * about time comes from the events' own core-stamped `time_ms`; a live
   * event has none, and its row says so rather than borrowing the client's.
   *
   * Grouping: turn (by data.turn_id, in order of first appearance) > step (by
   * data.step) > items. Tool calls are keyed by tool_call_id; a child-scoped
   * tool call nests under its parent_tool_call_id. Consecutive text deltas of
   * one kind coalesce into one row. Workflow stages and mode routes sit
   * directly under the turn. Token columns come from step_usage only.
   *
   * Self-contained on purpose: trajectory-test.mjs evaluates it standalone.
   *
   * @param {Array<{seq?: number, time_ms?: number, type: string, data?: any}>|null|undefined} events
   * @returns {TrajRow[]}
   */
  function buildTrajectoryTree(events) {
    /** @type {TrajRow[]} */
    const rows = [];
    if (!Array.isArray(events)) return rows;

    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const str = (v) => (typeof v === "string" ? v : "");
    const obj = (v) => (v && typeof v === "object" ? v : {});

    // Turns in order of first appearance. Events without a turn_id (older
    // cores, one-shot agent.run) share one unnamed turn so nothing is dropped.
    const turns = new Map();
    let turnOrdinal = 0;
    const turnFor = (id) => {
      let t = turns.get(id);
      if (!t) {
        turnOrdinal++;
        t = {
          key: "turn:" + id,
          id,
          ordinal: turnOrdinal,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: "open",
          steps: new Map(),
          stepOrder: [],
          children: [], // stages and routes: rows directly under the turn
        };
        turns.set(id, t);
      }
      return t;
    };
    const stepFor = (t, n) => {
      const key = String(n);
      let s = t.steps.get(key);
      if (!s) {
        s = {
          key: t.key + "/step:" + key,
          n,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: undefined,
          tokensIn: undefined,
          tokensOut: undefined,
          items: [], // rows under the step, in order
          tools: new Map(), // tool_call_id -> tool node (parent and child scope alike)
          openTool: null, // last parent-scope tool started and not yet completed
          lastText: null, // for coalescing message_delta / reasoning_delta
        };
        t.steps.set(key, s);
        t.stepOrder.push(key);
      }
      return s;
    };
    // Widen a node's [startMs, endMs] to include ms, and mark it live if ms is unknown.
    const touch = (node, ms, live) => {
      if (live) node.live = true;
      if (ms === undefined) return;
      if (node.startMs === undefined || ms < node.startMs) node.startMs = ms;
      if (node.endMs === undefined || ms > node.endMs) node.endMs = ms;
    };

    for (const e of events) {
      if (!e || typeof e !== "object") continue;
      const method = str(e.type);
      const d = obj(e.data);
      const ms = num(e.time_ms);
      const live = ms === undefined;
      const seq = num(e.seq);
      const t = turnFor(str(d.turn_id));
      touch(t, ms, live);

      if (method === "workflow/stage_start" || method === "workflow/stage_done") {
        const stageKey = t.key + "/stage:" + str(d.stage_id) + "#" + (num(d.attempt) ?? 0);
        let st = t.children.find((c) => c.key === stageKey);
        if (!st) {
          st = {
            kind: "stage",
            key: stageKey,
            label: str(d.stage_id) || str(d.name) || "stage",
            startMs: undefined,
            endMs: undefined,
            live: false,
            outcome: undefined,
            seq,
          };
          t.children.push(st);
        }
        touch(st, ms, live);
        if (method === "workflow/stage_done") st.outcome = str(d.marker) || str(d.action) || "done";
        continue;
      }

      const s = stepFor(t, num(d.step) ?? 0);
      touch(s, ms, live);

      if (method === "exec/output_chunk") {
        const target = s.openTool;
        if (target) {
          target.output += str(d.chunk);
          touch(target, ms, live);
        } else {
          s.items.push({ kind: "other", key: s.key + "/exec:" + s.items.length, label: "shell output", startMs: ms, endMs: ms, live, output: str(d.chunk), seq });
        }
        s.lastText = null;
        continue;
      }

      if (method !== "agent/event") {
        // A notification this view has never heard of stays visible as a
        // plain row: the log outlives the code that reads it.
        s.items.push({ kind: "other", key: s.key + "/" + method + ":" + s.items.length, label: method || "event", startMs: ms, endMs: ms, live, seq });
        s.lastText = null;
        continue;
      }

      const kind = str(d.type);
      const isChild = d.scope === "child";
      const parentToolId = str(d.parent_tool_call_id);

      switch (kind) {
        case "message_delta":
        case "reasoning_delta": {
          if (isChild) break; // a subagent's text belongs to its own trace, not this step's
          const k = kind === "reasoning_delta" ? "reasoning" : "text";
          if (s.lastText && s.lastText.kind === k) {
            s.lastText.output += str(d.content);
            touch(s.lastText, ms, live);
          } else {
            const row = { kind: k, key: s.key + "/" + k + ":" + s.items.length, label: k === "reasoning" ? "reasoning" : "response", startMs: ms, endMs: ms, live, output: str(d.content), seq };
            s.items.push(row);
            s.lastText = row;
          }
          break;
        }
        case "tool_call_start": {
          const id = str(d.tool_call_id) || "idx" + (num(d.tool_call_index) ?? s.items.length);
          const row = { kind: "tool", key: s.key + "/tool:" + id, id, label: str(d.tool_call_name) || "tool", startMs: ms, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
          const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
          if (parent) parent.subrows.push(row);
          else s.items.push(row);
          s.tools.set(id, row);
          if (!isChild) s.openTool = row;
          s.lastText = null;
          break;
        }
        case "tool_call_delta": {
          const row = s.tools.get(str(d.tool_call_id));
          if (row) {
            row.input += str(d.args_delta);
            touch(row, ms, live);
          }
          break;
        }
        case "tool_call_completed": {
          const id = str(d.tool_call_id);
          let row = s.tools.get(id);
          if (!row) {
            // Completed without a recorded start: still a row, with no
            // duration to claim.
            row = { kind: "tool", key: s.key + "/tool:" + (id || String(s.items.length)), id, label: str(d.tool_call_name) || "tool", startMs: undefined, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
            s.items.push(row);
            if (id) s.tools.set(id, row);
          }
          row.output += str(d.content);
          row.outcome = "done";
          touch(row, ms, live);
          if (s.openTool === row) s.openTool = null;
          s.lastText = null;
          break;
        }
        case "step_usage": {
          if (isChild) break; // a worker's usage is its own; the step's columns are the main agent's
          const u = obj(d.data);
          if (num(u.prompt_tokens) !== undefined) s.tokensIn = num(u.prompt_tokens);
          if (num(u.completion_tokens) !== undefined) s.tokensOut = num(u.completion_tokens);
          break;
        }
        case "context_estimate":
        case "todos_updated":
        case "child_started":
        case "child_done":
          // Deliberately no row. The estimate is not a measurement and must
          // never reach a token column; todos and child lifecycle are drawn
          // elsewhere in the chat.
          break;
        case "step_done":
          s.outcome = str(d.content) || "done";
          if (s.outcome === "final") t.outcome = "final";
          s.lastText = null;
          break;
        case "done":
          if (t.outcome === "open") t.outcome = "done";
          break;
        case "recoverable_error":
        case "error": {
          s.items.push({ kind: "error", key: s.key + "/err:" + s.items.length, label: kind === "error" ? "error" : "retry", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          if (kind === "error") t.outcome = "error";
          s.lastText = null;
          break;
        }
        case "pending_ops": {
          const p = obj(d.data);
          const n = Array.isArray(p.ops) ? p.ops.length : 0;
          s.items.push({ kind: "pending", key: s.key + "/pending:" + s.items.length, label: n + " pending change" + (n === 1 ? "" : "s") + (p.applied ? " (applied)" : ""), startMs: ms, endMs: ms, live, seq });
          s.lastText = null;
          break;
        }
        case "mode_route": {
          const r = obj(d.data);
          t.children.push({ kind: "route", key: t.key + "/route:" + t.children.length, label: "mode " + str(r.from) + " → " + str(r.to), startMs: ms, endMs: ms, live, seq, output: str(r.reason) });
          break;
        }
        default:
          s.items.push({ kind: "other", key: s.key + "/" + (kind || "event") + ":" + s.items.length, label: kind || "event", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          s.lastText = null;
      }
    }

    // Flatten to rows. Offsets are relative to the row's own turn.
    const dur = (n) => (n.startMs !== undefined && n.endMs !== undefined ? n.endMs - n.startMs : undefined);
    const off = (t, n) => (t.startMs !== undefined && n.startMs !== undefined ? n.startMs - t.startMs : undefined);
    const push = (t, n, depth, kind, extra) => {
      rows.push(
        Object.assign(
          { kind, depth, key: n.key, label: n.label, turnId: t.id, startMs: n.startMs, offsetMs: off(t, n), durationMs: dur(n), live: Boolean(n.live), seq: n.seq },
          extra || {}
        )
      );
    };
    const pushTool = (t, s, node, depth) => {
      push(t, node, depth, "tool", { step: s.n, input: node.input || undefined, output: node.output || undefined, outcome: node.outcome });
      for (const sub of node.subrows) pushTool(t, s, sub, depth + 1);
    };

    for (const t of turns.values()) {
      push(t, { key: t.key, label: "turn " + t.ordinal, startMs: t.startMs, endMs: t.endMs, live: t.live }, 0, "turn", { outcome: t.outcome });
      for (const c of t.children) push(t, c, 1, c.kind, { outcome: c.outcome, output: c.output || undefined });
      for (const k of t.stepOrder) {
        const s = t.steps.get(k);
        push(t, { key: s.key, label: "step " + s.n, startMs: s.startMs, endMs: s.endMs, live: s.live }, 1, "step", { step: s.n, tokensIn: s.tokensIn, tokensOut: s.tokensOut, outcome: s.outcome });
        for (const it of s.items) {
          if (it.kind === "tool") pushTool(t, s, it, 2);
          else push(t, it, 2, it.kind, { step: s.n, output: it.output || undefined, outcome: it.outcome });
        }
      }
    }
    return rows;
  }
