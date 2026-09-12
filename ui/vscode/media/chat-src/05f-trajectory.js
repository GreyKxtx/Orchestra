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

      // The turn's own boundary. It exists so a turn that emitted nothing at
      // all is still a turn, and so the turn's duration is the core's
      // measurement rather than the span of whatever events bracketed it. It
      // sets the turn's edges and adds no row of its own — turnFor/touch above
      // have already taken the timestamp.
      if (method === "turn/start" || method === "turn/end") {
        if (method === "turn/end") {
          const d2 = num(d.duration_ms);
          if (d2 !== undefined) t.durationMs = d2;
          if (t.outcome === "open") t.outcome = "done";
        }
        continue;
      }

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
      push(t, { key: t.key, label: "turn " + t.ordinal, startMs: t.startMs, endMs: t.endMs, live: t.live }, 0, "turn",
        // The core measured this turn where it ran. Prefer that over the span
        // between the first and last event we happened to receive; extra is
        // merged last, so it wins over the derived value.
        t.durationMs === undefined ? { outcome: t.outcome } : { outcome: t.outcome, durationMs: t.durationMs });
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
  const trajTimelineEl = document.getElementById("traj-timeline");
  const trajMetricEl = document.getElementById("traj-metric");
  const trajSearchEl = document.getElementById("traj-search");
  const trajPanelEl = document.getElementById("traj-panel");
  const trajPanelKindEl = document.getElementById("traj-panel-kind");
  const trajPanelLocEl = document.getElementById("traj-panel-loc");
  const trajPanelBodyEl = document.getElementById("traj-panel-body");
  const trajPanelTabsEl = document.getElementById("traj-panel-tabs");
  const trajPanelCloseBtn = document.getElementById("traj-panel-close");
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
  /** The row whose details the side panel is showing, by key. */
  let trajSelectedKey = "";
  /** Which of the panel's three tabs is open. */
  let trajTab = "summary";
  /** What the timeline measures: elapsed time, one block per turn, or per call. */
  let trajMetric = "duration";
  /** The row filter typed into the toolbar's search box. */
  let trajQuery = "";
  /** @type {TrajRow[]} The rows of the last render, for the panel to look up. */
  let trajRowsCache = [];

  const TRAJ_BADGE = { turn: "TURN", step: "STEP", tool: "TOOL", text: "TEXT", reasoning: "THINK", error: "ERROR", stage: "STAGE", pending: "DIFF", route: "ROUTE", other: "EVENT" };
  /** Inline diffs run an O(n·m) alignment, so a big file gets its stats and the full viewer instead. */
  const TRAJ_DIFF_LINE_BUDGET = 1200;

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
    trajSelectedKey = "";
    trajRowsCache = [];
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

  /** Empty the timeline and fold the panel away: there is nothing to point at. */
  function clearTrajChrome() {
    if (trajTimelineEl) trajTimelineEl.innerHTML = "";
    if (trajPanelEl) trajPanelEl.hidden = true;
  }

  /** @param {TrajRow} r @param {string} q */
  function trajRowMatches(r, q) {
    if (!q) return true;
    const haystack = r.kind + " " + r.label + " " + (r.outcome || "") + " " + (r.input || "") + " " + (r.output || "");
    return haystack.toLowerCase().indexOf(q) !== -1;
  }

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajRowsCache = rows;
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = "Trajectory unavailable: " + trajError;
      clearTrajChrome();
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = "No trajectory was recorded for this session — it predates the log.";
      clearTrajChrome();
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = trajRecorded === null ? "Loading trajectory…" : "Nothing has happened in this session yet.";
      clearTrajChrome();
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    const q = trajQuery.trim().toLowerCase();
    const shown = q ? rows.filter((r) => trajRowMatches(r, q)) : rows;
    trajectorySummary.textContent =
      turns + " turn" + (turns === 1 ? "" : "s") + " · " + rows.length + " rows" +
      (liveCount ? " · " + liveCount + " live" : "") +
      (q ? " · " + shown.length + " matching" : "");
    renderTrajTimeline(rows);
    const frag = document.createDocumentFragment();
    for (const r of shown) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
    renderTrajPanel();
  }

  /**
   * The timeline. "duration" lays turns, steps and tool calls on three tracks
   * against one elapsed-time scale, which is the only view where a gap means
   * idle time; "turns" and "calls" give every turn (or every call) the same
   * width, for reading a long session by structure rather than by clock.
   * @param {TrajRow[]} rows
   */
  function renderTrajTimeline(rows) {
    if (!trajTimelineEl) return;
    trajTimelineEl.innerHTML = "";
    const track = (label, items, span) => {
      const line = document.createElement("div");
      line.className = "traj-tl-track";
      const name = document.createElement("span");
      name.className = "traj-tl-name";
      name.textContent = label;
      const bar = document.createElement("div");
      bar.className = "traj-tl-bar";
      items.forEach((r, i) => {
        const block = document.createElement("button");
        block.type = "button";
        block.className = "traj-tl-block";
        block.dataset.kind = r.kind;
        block.dataset.key = r.key;
        if (r.key === trajSelectedKey) block.dataset.selected = "true";
        block.title = r.label + (r.durationMs === undefined ? "" : " · " + formatToolDuration(r.durationMs));
        block.setAttribute("aria-label", r.kind + " " + r.label);
        const box = span(r, i);
        block.style.setProperty("left", box.left + "%");
        block.style.setProperty("width", box.width + "%");
        bar.appendChild(block);
      });
      line.append(name, bar);
      trajTimelineEl.appendChild(line);
    };

    if (trajMetric === "turns" || trajMetric === "calls") {
      const wanted = trajMetric === "turns" ? "turn" : "tool";
      const items = rows.filter((r) => r.kind === wanted);
      const each = items.length ? 100 / items.length : 100;
      track(trajMetric === "turns" ? "Turns" : "Calls", items, (_r, i) => ({
        left: +(i * each).toFixed(3),
        width: +Math.max(each - 0.4, 0.6).toFixed(3),
      }));
      return;
    }

    // Elapsed time: offsets are relative to the row's own turn, so shift each
    // turn by where it starts to put every track on one session-wide scale.
    const turnStart = new Map();
    let base = Infinity;
    for (const r of rows) {
      if (r.startMs === undefined) continue;
      if (r.kind === "turn") turnStart.set(r.turnId, r.startMs);
      if (r.startMs < base) base = r.startMs;
    }
    if (!isFinite(base)) base = 0;
    const at = (r) => {
      if (r.startMs !== undefined) return r.startMs - base;
      const s = turnStart.get(r.turnId);
      return s === undefined ? 0 : s - base + (r.offsetMs || 0);
    };
    let total = 0;
    for (const r of rows) {
      const end = at(r) + (r.durationMs || 0);
      if (end > total) total = end;
    }
    if (total <= 0) total = 1;
    const span = (r) => {
      const left = Math.max(0, Math.min(100, (at(r) / total) * 100));
      const raw = ((r.durationMs || 0) / total) * 100;
      return { left: +left.toFixed(3), width: +Math.max(Math.min(raw, 100 - left), 0.6).toFixed(3) };
    };
    track("Turns", rows.filter((r) => r.kind === "turn"), span);
    track("Steps", rows.filter((r) => r.kind === "step"), span);
    track("Tools", rows.filter((r) => r.kind === "tool"), span);
  }

  /** @param {string} key */
  function selectTrajRow(key) {
    trajSelectedKey = trajSelectedKey === key ? "" : key;
    scheduleTrajectoryRender();
  }

  /** The recorded event a row was built from, when it has one. @param {TrajRow} r */
  function trajSourceEvent(r) {
    if (!r || r.seq === undefined) return null;
    for (const e of trajEvents) {
      if (e && e.seq === r.seq) return e;
    }
    return null;
  }

  function renderTrajPanel() {
    if (!trajPanelEl || !trajPanelBodyEl) return;
    const row = trajRowsCache.find((r) => r.key === trajSelectedKey);
    if (!row) {
      trajPanelEl.hidden = true;
      return;
    }
    trajPanelEl.hidden = false;
    if (trajPanelKindEl) {
      trajPanelKindEl.textContent = TRAJ_BADGE[row.kind] || TRAJ_BADGE.other;
      trajPanelKindEl.dataset.kind = row.kind;
    }
    if (trajPanelLocEl) {
      const bits = [];
      if (row.turnId) bits.push("turn " + row.turnId);
      if (row.step !== undefined) bits.push("step " + row.step);
      trajPanelLocEl.textContent = bits.join(" · ") || row.label;
    }
    if (trajPanelTabsEl && trajPanelTabsEl.querySelectorAll) {
      trajPanelTabsEl.querySelectorAll(".traj-panel-tab").forEach((el) => {
        el.setAttribute("aria-selected", el.getAttribute("data-tab") === trajTab ? "true" : "false");
      });
    }
    trajPanelBodyEl.innerHTML = "";
    const ev = trajSourceEvent(row);
    if (trajTab === "raw") {
      renderTrajRaw(row, ev);
    } else if (trajTab === "preview") {
      renderTrajPreview(row, ev);
    } else {
      renderTrajSummaryTab(row, ev);
    }
  }

  /** @param {string} head @param {string} text */
  function trajPanelPre(head, text) {
    const h = document.createElement("div");
    h.className = "traj-panel-section";
    h.textContent = head;
    const pre = document.createElement("pre");
    pre.className = "traj-pre";
    pre.textContent = text;
    trajPanelBodyEl.append(h, pre);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajSummaryTab(row, ev) {
    const dl = document.createElement("div");
    dl.className = "traj-facts";
    const fact = (k, v) => {
      if (v === "" || v === undefined || v === null) return;
      const key = document.createElement("span");
      key.className = "traj-fact-key";
      key.textContent = k;
      const val = document.createElement("span");
      val.className = "traj-fact-val";
      val.textContent = String(v);
      dl.append(key, val);
    };
    fact("kind", row.kind);
    fact("label", row.label);
    fact("outcome", row.outcome);
    fact("offset", row.offsetMs === undefined ? "" : "+" + formatToolDuration(row.offsetMs));
    fact("duration", row.durationMs === undefined ? "" : formatToolDuration(row.durationMs));
    fact("tokens in", row.tokensIn);
    fact("tokens out", row.tokensOut);
    fact("live", row.live ? "yes" : "");
    fact("event", ev && ev.type ? ev.type + (ev.data && ev.data.type ? " · " + ev.data.type : "") : "");
    fact("seq", row.seq);
    trajPanelBodyEl.appendChild(dl);
    const hasDiff = ev && ev.data && ev.data.data && Array.isArray(ev.data.data.diff) && ev.data.data.diff.length > 0;
    if (!row.input && !row.output && !hasDiff) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "This row records that the event happened; it carries no payload.";
      trajPanelBodyEl.appendChild(note);
    }
  }

  /**
   * Preview shows what the row actually holds: file diffs for a pending-ops
   * row (the only event that carries before/after content), otherwise the
   * tool's arguments and its result. Tool results are truncated by the core
   * at 256 bytes, so the pane says so rather than looking complete.
   * @param {TrajRow} row @param {any} ev
   */
  function renderTrajPreview(row, ev) {
    // The notification nests its own payload: params are {turn_id, step, type,
    // data:{...}}, so a pending_ops diff lives at data.data.diff.
    const payload = ev && ev.data && ev.data.data && typeof ev.data.data === "object" ? ev.data.data : null;
    const diffs = payload && Array.isArray(payload.diff) ? payload.diff : null;
    if (diffs && diffs.length) {
      for (const d of diffs) {
        renderTrajFileDiff(String(d.path || ""), String(d.before || ""), String(d.after || ""));
      }
      return;
    }
    if (row.input) {
      let text = row.input;
      try {
        text = JSON.stringify(JSON.parse(row.input), null, 2);
      } catch (e) {
        // Arguments stream in as fragments, so a mid-turn row holds partial
        // JSON. Showing it verbatim beats showing nothing.
      }
      trajPanelPre("arguments", text);
    }
    if (row.output) {
      trajPanelPre("result", row.output);
    }
    if (!row.input && !row.output) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "Nothing to preview for this row.";
      trajPanelBodyEl.appendChild(note);
    }
  }

  /** @param {string} path @param {string} before @param {string} after */
  function renderTrajFileDiff(path, before, after) {
    const stats = countDiffStats(before, after);
    const head = document.createElement("div");
    head.className = "traj-diff-head";
    const name = document.createElement("span");
    name.className = "traj-diff-path";
    name.textContent = path || "(unnamed file)";
    const count = document.createElement("span");
    count.className = "traj-diff-stats";
    count.textContent = "+" + stats.add + " −" + stats.del;
    const open = document.createElement("button");
    open.type = "button";
    open.className = "traj-diff-open";
    open.textContent = "Open full diff";
    open.addEventListener("click", () => showDiffViewer(path, before, after, ""));
    head.append(name, count, open);
    trajPanelBodyEl.appendChild(head);

    const lineCount = before.split("\n").length + after.split("\n").length;
    if (lineCount > TRAJ_DIFF_LINE_BUDGET) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "The file is too large to align inline (" + lineCount + " lines) — open the full diff.";
      trajPanelBodyEl.appendChild(note);
      return;
    }
    const block = document.createElement("div");
    block.className = "traj-diff";
    for (const line of alignDiffLines(before, after)) {
      if (line.type === "same") continue;
      const el = document.createElement("div");
      el.className = "traj-diff-line traj-diff-" + line.type;
      el.textContent = (line.type === "add" ? "+ " : "− ") + (line.type === "add" ? line.right : line.left);
      block.appendChild(el);
    }
    if (!block.childNodes || block.childNodes.length === 0) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = "No line changed in this file.";
      trajPanelBodyEl.appendChild(note);
      return;
    }
    trajPanelBodyEl.appendChild(block);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajRaw(row, ev) {
    if (ev) {
      trajPanelPre("recorded event", JSON.stringify(ev, null, 2));
      return;
    }
    // A live row has no envelope yet: it arrived as a forwarded notification
    // with no seq or time_ms, so the row itself is the whole truth.
    trajPanelPre("row (live — not yet read back from the log)", JSON.stringify(row, null, 2));
  }

  /**
   * One row. Every row opens the side panel — the payload is no longer folded
   * out in place, so a row's height never changes and the list stays scannable.
   * @param {TrajRow} r
   */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.dataset.kind = r.kind;
    if (r.key === trajSelectedKey) el.dataset.selected = "true";
    el.style.setProperty("--traj-depth", String(r.depth));
    el.tabIndex = 0;
    el.setAttribute("role", "button");

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const badge = document.createElement("span");
    badge.className = "traj-badge";
    badge.dataset.kind = r.kind;
    badge.textContent = TRAJ_BADGE[r.kind] || TRAJ_BADGE.other;
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
    el.append(off, badge, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));
    el.addEventListener("click", () => selectTrajRow(r.key));
    el.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectTrajRow(r.key);
      }
    });
    return el;
  }

  function bindTrajectoryChrome() {
    if (trajMetricEl && trajMetricEl.addEventListener) {
      trajMetricEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-metric]") : null;
        if (!btn) return;
        trajMetric = btn.getAttribute("data-metric") || "duration";
        if (trajMetricEl.querySelectorAll) {
          trajMetricEl.querySelectorAll("[data-metric]").forEach((el) => {
            el.setAttribute("aria-selected", el.getAttribute("data-metric") === trajMetric ? "true" : "false");
          });
        }
        scheduleTrajectoryRender();
      });
    }
    if (trajSearchEl && trajSearchEl.addEventListener) {
      trajSearchEl.addEventListener("input", () => {
        trajQuery = trajSearchEl.value || "";
        scheduleTrajectoryRender();
      });
    }
    if (trajTimelineEl && trajTimelineEl.addEventListener) {
      trajTimelineEl.addEventListener("click", (e) => {
        const block = e.target && e.target.closest ? e.target.closest(".traj-tl-block") : null;
        if (!block) return;
        selectTrajRow(block.getAttribute("data-key") || "");
      });
    }
    if (trajPanelTabsEl && trajPanelTabsEl.addEventListener) {
      trajPanelTabsEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-tab]") : null;
        if (!btn) return;
        trajTab = btn.getAttribute("data-tab") || "summary";
        renderTrajPanel();
      });
    }
    if (trajPanelCloseBtn && trajPanelCloseBtn.addEventListener) {
      trajPanelCloseBtn.addEventListener("click", () => {
        trajSelectedKey = "";
        scheduleTrajectoryRender();
      });
    }
  }

  bindTrajectoryChrome();
  bindViewSwitch();
  setView("chat");
  scheduleTrajectoryRender();
