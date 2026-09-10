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
   * one kind coalesce into one row. Workflow stages, mode routes and steps sit
   * directly under the turn, in the order they first appeared — which for a recorded log is time order, and needs no timestamp so it holds for live rows too. Token columns come from step_usage only.
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
          steps: new Map(), // step number -> step node, for lookup
          depth1: [], // steps, stages and routes in the order they first appeared — which is time order
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
          kind: "step",
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
        t.depth1.push(s);
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
        let st = t.depth1.find((c) => c.kind === "stage" && c.key === stageKey);
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
          t.depth1.push(st);
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
          const wasFound = !!row;
          if (!row) {
            // Completed without a recorded start: still a row, with no
            // duration to claim.
            row = { kind: "tool", key: s.key + "/tool:" + (id || String(s.items.length)), id, label: str(d.tool_call_name) || "tool", startMs: undefined, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
            const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
            if (parent) parent.subrows.push(row);
            else s.items.push(row);
            if (id) s.tools.set(id, row);
          }
          row.output += str(d.content);
          row.outcome = "done";
          if (wasFound) touch(row, ms, live);
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
          t.depth1.push({ kind: "route", key: t.key + "/route:" + t.depth1.length, label: "mode " + str(r.from) + " → " + str(r.to), startMs: ms, endMs: ms, live, seq, output: str(r.reason) });
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
      for (const n of t.depth1) {
        if (n.kind !== "step") {
          push(t, n, 1, n.kind, { outcome: n.outcome, output: n.output || undefined });
          continue;
        }
        push(t, { key: n.key, label: "step " + n.n, startMs: n.startMs, endMs: n.endMs, live: n.live }, 1, "step", { step: n.n, tokensIn: n.tokensIn, tokensOut: n.tokensOut, outcome: n.outcome });
        for (const it of n.items) {
          if (it.kind === "tool") pushTool(t, n, it, 2);
          else push(t, it, 2, it.kind, { step: n.n, output: it.output || undefined, outcome: it.outcome });
        }
      }
    }
    return rows;
  }

  // ---- rendering ------------------------------------------------------------

  const trajectorySummary = document.getElementById("trajectory-summary");
  const trajectoryRowsEl = document.getElementById("trajectory-rows");
  const viewChatBtn = document.getElementById("view-chat-btn");
  const viewTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const appViewEl = document.getElementById("app");

  /** @type {Array<{seq?: number, time_ms?: number, type: string, data?: any}>} */
  let trajEvents = [];
  /** null until the host has answered once; then the log's own recorded flag. */
  let trajRecorded = null;
  /** Set when the host could not fetch the log; shown instead of a false "not recorded". */
  let trajError = "";
  let trajRenderQueued = false;

  const TRAJ_GLYPH = { turn: "◆", step: "▸", tool: "⚙", text: "¶", reasoning: "…", error: "!", stage: "▣", pending: "±", route: "↦", other: "·" };

  function currentView() {
    return appViewEl && appViewEl.dataset.view === "trajectory" ? "trajectory" : "chat";
  }

  /** @param {string} view */
  function setView(view) {
    const v = view === "trajectory" ? "trajectory" : "chat";
    if (appViewEl) appViewEl.dataset.view = v;
    if (viewChatBtn) viewChatBtn.setAttribute("aria-selected", v === "chat" ? "true" : "false");
    if (viewTrajectoryBtn) viewTrajectoryBtn.setAttribute("aria-selected", v === "trajectory" ? "true" : "false");
  }

  function bindViewSwitch() {
    if (viewChatBtn) viewChatBtn.addEventListener("click", () => setView("chat"));
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("click", () => setView("trajectory"));
    // Arrow keys move between the two segments, as a tablist does. The
    // listeners sit on each button rather than on a shared ancestor found via
    // closest(): the test harness's stub closest() returns null, which is what
    // left C1's rail keyboard path with no automated coverage.
    const onKey = (e) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      const next = currentView() === "chat" ? "trajectory" : "chat";
      setView(next);
      const btn = next === "chat" ? viewChatBtn : viewTrajectoryBtn;
      if (btn) btn.focus();
    };
    if (viewChatBtn) viewChatBtn.addEventListener("keydown", onKey);
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("keydown", onKey);
  }

  function scheduleTrajectoryRender() {
    if (trajRenderQueued) return;
    trajRenderQueued = true;
    requestAnimationFrame(() => {
      trajRenderQueued = false;
      renderTrajectory();
    });
  }

  /** Session switch or clear: forget everything and wait for the host. */
  function resetTrajectory() {
    trajEvents = [];
    trajRecorded = null;
    trajError = "";
    scheduleTrajectoryRender();
  }

  /** The host fetched session.trajectory: this is now the whole truth. */
  function replaceTrajectory(recorded, events, error) {
    trajRecorded = Boolean(recorded);
    trajEvents = Array.isArray(events) ? events.slice() : [];
    trajError = typeof error === "string" ? error : "";
    scheduleTrajectoryRender();
  }

  /** One live notification. A turn is being recorded now even if the session
   * predated the log, so the "not recorded" answer no longer applies. */
  function appendTrajectoryEvent(ev) {
    if (!ev || typeof ev.type !== "string") return;
    trajEvents.push({ type: ev.type, data: ev.data });
    trajRecorded = true;
    scheduleTrajectoryRender();
  }

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = "Trajectory unavailable: " + trajError;
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = "No trajectory was recorded for this session — it predates the log.";
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = trajRecorded === null ? "Loading trajectory…" : "Nothing has happened in this session yet.";
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    trajectorySummary.textContent = turns + " turn" + (turns === 1 ? "" : "s") + " · " + rows.length + " rows" + (liveCount ? " · " + liveCount + " live" : "");
    const frag = document.createDocumentFragment();
    for (const r of rows) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
  }

  /** @param {TrajRow} r */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.style.setProperty("--traj-depth", String(r.depth));

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const glyph = document.createElement("span");
    glyph.className = "traj-glyph";
    glyph.textContent = TRAJ_GLYPH[r.kind] || TRAJ_GLYPH.other;
    glyph.setAttribute("aria-hidden", "true");
    const label = document.createElement("span");
    label.className = "traj-label";
    label.textContent = r.label + (r.outcome && r.kind !== "tool" ? " · " + r.outcome : "");
    const dur = document.createElement("span");
    dur.className = "traj-dur";
    dur.textContent = r.durationMs === undefined ? "" : formatToolDuration(r.durationMs);
    const tok = document.createElement("span");
    tok.className = "traj-tok";
    // Blank unless a provider actually reported a number. Never an estimate.
    tok.textContent =
      (r.tokensIn === undefined ? "" : r.tokensIn + "↑") +
      (r.tokensOut === undefined ? "" : (r.tokensIn === undefined ? "" : " ") + r.tokensOut + "↓");
    el.append(off, glyph, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));

    if (r.input || r.output) {
      el.classList.add("traj-expandable");
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-expanded", "false");
      const detail = document.createElement("div");
      detail.className = "traj-detail hidden";
      const section = (head, text) => {
        const h = document.createElement("div");
        h.className = "traj-detail-head";
        h.textContent = head;
        const pre = document.createElement("pre");
        pre.className = "traj-pre";
        pre.textContent = text;
        detail.append(h, pre);
      };
      if (r.input) section("input", r.input);
      if (r.output) section("output", r.output);
      el.appendChild(detail);
      const toggle = () => {
        const hidden = detail.classList.toggle("hidden");
        el.setAttribute("aria-expanded", hidden ? "false" : "true");
      };
      el.addEventListener("click", toggle);
      el.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          toggle();
        }
      });
    }
    return el;
  }

  bindViewSwitch();
  setView("chat");
  scheduleTrajectoryRender();
