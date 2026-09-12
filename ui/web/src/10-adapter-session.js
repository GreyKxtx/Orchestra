  // Outbound: the renderer's message protocol -> JSON-RPC.
  //
  // This is the browser's half of what ui/vscode/src/chat/panel.ts does for the
  // extension. Only v1 scope is wired: chat, cancellation, sessions. Message
  // types outside that scope are acknowledged and ignored rather than dropped
  // silently — a no-op the user can see beats a control that does nothing.

  /**
   * Per-project session state. The renderer shows one project at a time, so
   * exactly one of these is "current"; the others are what a switch restores.
   * @type {Map<string, {sessionId: string, inFlightTurnId: any, workspaceRoot: string, status: string, pendingAsk: any, llm: any}>}
   */
  const perProject = new Map();
  let currentProjectId = "";

  /** @param {string} projectId */
  function projectState(projectId) {
    let st = perProject.get(projectId);
    if (!st) {
      st = {
        sessionId: "",
        inFlightTurnId: null,
        workspaceRoot: "",
        status: "idle",
        pendingAsk: null,
        // The core's last answer about the model and its window; see pushLLMInfo.
        llm: null,
      };
      perProject.set(projectId, st);
    }
    return st;
  }

  /** @param {string} projectId */
  function forgetProjectState(projectId) {
    perProject.delete(projectId);
  }

  // ---- what the composer says about the model -----------------------------
  //
  // The model pill and the context gauge under the input read two renderer
  // messages: header (model, provider) and contextInfo (the window and the
  // reply budget) — ui/vscode/media/chat-src/07-events.js. The editor's host
  // sends both from the core's own answer (panel.ts refreshHeaderAndHistory).
  // This host used to send a header with no model on every switch and no
  // contextInfo at all, so the pill kept whichever project's model it had
  // seen last and the gauge measured against a 128K default that was nobody's
  // window.

  /**
   * The ceiling the gauge measures against. num_ctx is the window the request
   * asks the server for; context_tokens is the most the model can take, from
   * the catalogue or the server's own answer. A prompt has to fit under both,
   * so the smaller one is the real limit: a local model run with num_ctx 20000
   * overflows at 20000 however large its catalogue entry says it could be.
   * @param {any} numCtx @param {any} contextTokens
   */
  function contextLimitFor(numCtx, contextTokens) {
    const asked = Number(numCtx) > 0 ? Number(numCtx) : 0;
    const most = Number(contextTokens) > 0 ? Number(contextTokens) : 0;
    if (asked > 0 && most > 0) {
      return Math.min(asked, most);
    }
    return asked || most || 128000;
  }

  /**
   * Ask the core what the project is talking to, keep the answer with the
   * project, and tell the composer if that project is the one on screen. Kept
   * per project so a switch can repaint from the last answer at once
   * (postLLMInfo) while a fresh read is on its way.
   * @param {string} projectId
   */
  async function pushLLMInfo(projectId) {
    const conn = connFor(projectId);
    if (!conn) {
      return;
    }
    // Asked for alongside the model, not after it: the balance is a separate
    // call, and chaining it behind this await means a core that is slow to
    // answer about the model never reports a balance at all.
    void pushCredits(projectId);
    let llm;
    try {
      llm = (await conn.send("runtime.get_llm", {})) || {};
    } catch (err) {
      // The pill keeps its last label: there is nothing truer to put there.
      return;
    }
    const st = projectState(projectId);
    const maxTokens = Number(llm.max_tokens) || 0;
    st.llm = {
      model: String(llm.model || ""),
      provider: String(llm.provider || ""),
      contextLimit: contextLimitFor(llm.num_ctx, llm.context_tokens),
      maxResponseTokens: maxTokens > 0 ? maxTokens : 4096,
    };
    if (projectId === currentProjectId) {
      postLLMInfo(projectId);
    }
  }

  /**
   * The provider's account balance for the cost popover. The renderer already
   * draws the row from a "credits" message (07-events.js) — the editor host
   * sent one and the web host never did, so the popover here could only ever
   * show spend, never what is left.
   *
   * Best-effort by design: providers without a balance API (every local
   * server, plain OpenAI) answer supported=false, and the row is omitted.
   * @param {string} projectId
   */
  async function pushCredits(projectId) {
    const conn = connFor(projectId);
    if (!conn || !conn.isOpen()) {
      return;
    }
    try {
      const c = (await conn.send("runtime.credits", {})) || {};
      if (projectId !== currentProjectId) {
        return;
      }
      toRenderer({
        type: "credits",
        supported: !!c.supported,
        provider: String(c.provider || ""),
        balance: Number(c.balance) || 0,
      });
    } catch (err) {
      // No balance API, no key, or an endpoint that is down: the popover
      // simply keeps showing spend without a balance row.
    }
  }

  /**
   * The title the tab strip shows for a session, from the last session.list
   * (40-projects.js keeps it in sessionsByProject). A header message has to
   * carry it: the renderer renames the active tab from every header it gets
   * (07-events.js), and one without a title says "New chat" — over a
   * conversation that has a name.
   * @param {string} projectId @param {string} sessionId
   */
  function sessionTitleFor(projectId, sessionId) {
    const row = (sessionsByProject.get(projectId) || []).find((s) => s && s.id === sessionId);
    const title = row && typeof row.title === "string" ? row.title.trim() : "";
    return title || "New chat";
  }

  /** The composer's model and window, from the last answer. @param {string} projectId */
  function postLLMInfo(projectId) {
    const st = projectState(projectId);
    if (!st.llm) {
      return;
    }
    toRenderer({
      type: "header",
      sessionId: st.sessionId,
      title: sessionTitleFor(projectId, st.sessionId),
      model: st.llm.model,
      provider: st.llm.provider,
    });
    toRenderer({
      type: "contextInfo",
      info: {
        contextLimit: st.llm.contextLimit,
        maxResponseTokens: st.llm.maxResponseTokens,
        model: st.llm.model,
      },
    });
  }

  /**
   * A saved transcript in the shape the renderer draws it.
   *
   * The session file keeps a message the way Go writes it — tool_blocks,
   * args_raw, duration_ms, attachments — and the renderer (shared with the
   * editor's webview) reads toolBlocks, argsRaw, durationMs, files. Posting
   * the raw rows straight through, as this used to, restored the words and
   * silently dropped everything else. Mirrors the mapping panel.ts does for
   * the editor.
   * @param {any[]} uiMessages
   */
  function historyMessagesFrom(uiMessages) {
    const list = Array.isArray(uiMessages) ? uiMessages : [];
    const out = [];
    list.forEach((m, idx) => {
      if (!m || typeof m !== "object") {
        return;
      }
      const role = m.role === "user" ? "user" : m.role === "system" ? "system" : "assistant";
      const text = String(m.text || m.content || "");
      const reasoning = uiReasoningOf(m);
      const toolBlocks = uiToolBlocksOf(m);
      const files = (Array.isArray(m.attachments) ? m.attachments : [])
        .filter((a) => a && (a.path || a.name))
        .map((a) => ({
          name: String(a.name || String(a.path || "").split(/[\\/]/).pop() || "file"),
          path: String(a.path || ""),
          ext: a.ext ? String(a.ext) : undefined,
          kind: a.kind === "image" ? "image" : "file",
        }));
      if (!text && !reasoning && toolBlocks.length === 0 && files.length === 0) {
        return;
      }
      const row = { role, text };
      // The index into the core's own list: rewind aims at it, so it counts
      // every row, including any this loop leaves out.
      if (role === "user") row.uiIndex = idx;
      if (files.length) row.files = files;
      if (reasoning) row.reasoning = reasoning;
      if (toolBlocks.length) row.toolBlocks = toolBlocks;
      out.push(row);
    });
    return out;
  }

  /** A saved message's reasoning, whether written flat or as segments. */
  function uiReasoningOf(m) {
    const direct = String((m && m.reasoning) || "").trim();
    if (direct) {
      return direct;
    }
    const parts = [];
    for (const seg of (m && m.segments) || []) {
      if (seg && seg.kind === "reasoning" && seg.text) parts.push(seg.text);
    }
    return parts.join("").trim();
  }

  /** A saved message's tool blocks, flat or in segments, renderer-spelled. */
  function uiToolBlocksOf(m) {
    const raw = [];
    for (const t of (m && m.tool_blocks) || []) {
      if (t && t.name) raw.push(t);
    }
    if (raw.length === 0) {
      for (const seg of (m && m.segments) || []) {
        if (!seg || seg.kind !== "tools" || !Array.isArray(seg.tools)) continue;
        for (const t of seg.tools) {
          if (t && t.name) raw.push(t);
        }
      }
    }
    return raw.map((t) => ({
      id: t.id,
      name: t.name,
      argsRaw: t.args_raw || t.args_preview || "",
      status: t.status || "completed",
      result: t.result || "",
      diagnostics: Array.isArray(t.diagnostics) && t.diagnostics.length ? t.diagnostics : undefined,
      durationMs: typeof t.duration_ms === "number" && t.duration_ms > 0 ? t.duration_ms : undefined,
    }));
  }

  /**
   * The last measured prompt in a saved transcript. The core writes each
   * assistant message's prompt_ctx and tokens_out into the session file
   * (internal/sessionfile/uimessage.go); reopening a session paints the gauge
   * from them, as panel.ts does, instead of showing an empty ring over a
   * conversation that is plainly not empty. Messages without the fields leave
   * it empty, which is the truth: nothing was measured.
   * @param {any[]} uiMessages
   */
  function postRestoredUsage(uiMessages) {
    const msgs = Array.isArray(uiMessages) ? uiMessages : [];
    let prompt = 0;
    let completion = 0;
    for (const m of msgs) {
      if (!m || String(m.role || "").toLowerCase() !== "assistant") {
        continue;
      }
      if (Number(m.prompt_ctx) > 0) {
        prompt = Number(m.prompt_ctx);
      } else if (prompt === 0 && Number(m.tokens_in) > 0) {
        prompt = Number(m.tokens_in);
      }
      if (Number(m.tokens_out) > 0) {
        completion = Number(m.tokens_out);
      }
    }
    if (prompt <= 0) {
      return;
    }
    toRenderer({
      type: "stepUsage",
      usage: { prompt_tokens: prompt, completion_tokens: completion, source: "restored" },
    });
  }

  /**
   * Read a project's state without creating a record for it. The render path
   * walks every known project, and must not resurrect the records
   * forgetProjectState() deletes.
   * @param {string} projectId
   */
  function peekProjectState(projectId) {
    return perProject.get(projectId) || null;
  }

  /** The state the renderer is currently showing. */
  function current() {
    return projectState(currentProjectId);
  }

  /** @param {string} projectId */
  async function onConnected(projectId) {
    const st = projectState(projectId);
    const conn = connFor(projectId);
    try {
      // core.health is answerable before initialize — it and initialize are the
      // only two methods exempt from the gate (internal/core/rpc_handler.go:76)
      // — and it is where project_root and project_id come from.
      const health = await conn.send("core.health", {});
      st.workspaceRoot = health.workspace_root || "";
      await conn.send("initialize", {
        project_root: st.workspaceRoot,
        project_id: health.project_id || "",
        protocol_version: health.protocol_version,
        ops_version: health.ops_version,
        tools_version: health.tools_version,
      });
      const started = await conn.send("session.start", {});
      st.sessionId = started.session_id || "";
      // Not awaited: the pill and the gauge fill in from the core's answer
      // when it comes, and the transcript does not wait for them.
      void pushLLMInfo(projectId);
      // What "/" offers beyond the built-in commands: this workspace's own
      // skills and .claude/commands, which only the core can enumerate.
      void pushSkillCommands(projectId);
      // Settings opened while this workspace was still coming up had nothing
      // to read from and said so; now there is. Without this the panel kept
      // that note, an empty provider list and no tool catalogue until it was
      // closed and opened again.
      if (projectId === currentProjectId && settingsPanelOpen()) {
        void pushSettingsState();
      }
      if (projectId === currentProjectId) {
        toRenderer({
          type: "header",
          model: health.model || "",
          provider: health.provider || "",
          sessionId: st.sessionId,
        });
        toRenderer({ type: "ready" });
        // The first session of this connection: give the Trajectory pane its
        // (usually empty) log now, or it sits on "Loading trajectory…" until
        // the user switches, starts a session, or finishes a turn.
        void refreshTrajectory(projectId, conn, st.sessionId);
        await refreshSessionList(projectId);
      }
    } catch (err) {
      const message = String(err && err.message ? err.message : err);
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message });
      }
      st.status = "idle";
    }
    // Live, or failed trying: either way the project's frame stops loading.
    noteProjectLive(projectId);
    renderProjects();
  }

  /**
   * Read the session's log and hand it to the renderer — unless the user has
   * switched projects, or to another session of the same project, while the
   * request was in flight, in which case the answer belongs to a view that is
   * no longer on screen. Same rule as the session.get repaint above and
   * refreshSessionList below.
   *
   * A failed fetch is reported as such, not as "not recorded": those are
   * different answers and the view says which.
   * @param {string} projectId @param {any} conn @param {string} sessionId
   */
  async function refreshTrajectory(projectId, conn, sessionId) {
    if (!sessionId) {
      return;
    }
    try {
      const res = await conn.send("session.trajectory", { session_id: sessionId });
      // peekProjectState, not projectState: the latter lazily creates a
      // record, so a guard running after the project was forgotten would
      // resurrect an orphan entry that renderProjects then iterates past.
      if (projectId !== currentProjectId || (peekProjectState(projectId) || {}).sessionId !== sessionId) {
        // The user left this project, or moved to another session of it,
        // while the request was in flight: the answer is for a view that is
        // no longer on screen.
        return;
      }
      toRenderer({
        type: "trajectory",
        recorded: Boolean(res && res.recorded),
        events: res && Array.isArray(res.events) ? res.events : [],
      });
    } catch (err) {
      if (projectId === currentProjectId && (peekProjectState(projectId) || {}).sessionId === sessionId) {
        toRenderer({ type: "trajectory", recorded: true, events: [], error: String(err && err.message ? err.message : err) });
      }
    }
  }

  /**
   * Make projectId the one the renderer shows. Repaints from the core rather
   * than from a buffer — session.get is what makes holding no background
   * scrollback affordable — and re-raises a prompt the project was waiting on.
   * @param {string} projectId
   */
  async function activateProject(projectId) {
    currentProjectId = projectId;
    const st = projectState(projectId);
    const conn = connFor(projectId);
    setActiveConn(conn);
    // Paint the switch now, not after the round trips below. renderProjects is
    // also what puts the start screen away, and this function does not reach
    // its closing render until session.list has answered — so a slow core left
    // the start screen covering a project that was already open and selected.
    renderProjects();

    // The pill and the gauge are the composer's, and the composer is shared by
    // every project: paint this project's model and window over the previous
    // project's now, from the last answer, and read them again in case the
    // model was changed while this project was in the background.
    postLLMInfo(projectId);
    void pushLLMInfo(projectId);
    // Each workspace has its own commands; the palette must follow the switch.
    void pushSkillCommands(projectId);

    toRenderer({ type: "clearMessages" });
    // Take the outgoing project's overlay down unconditionally, then raise this
    // project's own ask, both before any await. Leaving it up while
    // currentProjectId already names this project means the buttons on screen
    // belong to one project and the reply is resolved against another — see
    // setDisplayedAsk below, which is the second half of that fix.
    const overlay = document.getElementById("overlay");
    if (overlay) {
      overlay.classList.add("hidden");
    }
    clearDisplayedAsk();
    if (st.pendingAsk) {
      toRenderer(st.pendingAsk.rendererMessage);
      setDisplayedAsk(projectId, st.pendingAsk);
    }

    if (st.sessionId) {
      try {
        const view = await conn.send("session.get", { session_id: st.sessionId });
        if (projectId !== currentProjectId) {
          return;
        }
        toRenderer({ type: "history", messages: historyMessagesFrom(view.ui_messages) });
        postRestoredUsage(view.ui_messages);
      } catch (err) {
        if (projectId === currentProjectId) {
          toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
        }
      }
      // Not awaited: the fetch must not hold up the header/turnComplete/
      // session.list below, which is what the renderer's own state (composer
      // busy/idle, session list) depends on. refreshTrajectory carries its
      // own stale guard, so a late answer still lands correctly (or is
      // dropped) once it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
    }
    if (projectId !== currentProjectId) {
      return;
    }
    toRenderer({ type: "header", sessionId: st.sessionId });
    // The renderer's "turnInFlight" arm ignores the payload and always calls
    // setBusy(true) (ui/vscode/media/chat-src/07-events.js). Sending it with
    // inFlight:false would lock the composer into "Stop" on every switch to an
    // idle project. "turnComplete" is the only message that reaches
    // setBusy(false), so send whichever one is true. turnComplete's contract is
    // `{ ok: boolean }` (ui/vscode/src/protocol/events.ts) and a missing `ok` is
    // read as failure (ui/vscode/media/chat-src/07-events.js) — arriving at an
    // idle project is not a failed turn, so say so explicitly.
    if (st.inFlightTurnId !== null) {
      toRenderer({ type: "turnInFlight", inFlight: true });
    } else {
      toRenderer({ type: "turnComplete", ok: true });
    }
    await refreshSessionList(projectId);
    renderProjects();
  }

  /**
   * Same rule as sendTurn and startSession: the caller can switch projects
   * while session.list is in flight, and the result must not paint over
   * whatever project is on screen by the time it comes back.
   * @param {string} projectId
   */
  async function refreshSessionList(projectId) {
    try {
      const res = await connFor(projectId).send("session.list", {});
      // The sidebar keeps a list per project, so a late answer is still this
      // project's truth even when the user has switched away — unlike the
      // renderer message below, which paints whatever is on screen.
      noteSessionList(projectId, res.sessions || []);
      if (projectId === currentProjectId) {
        toRenderer({ type: "sessionList", sessions: res.sessions || [] });
      }
    } catch (err) {
      // A missing session list is not fatal; the chat still works.
    }
  }

  /** @param {any} msg */
  function dispatchToCore(msg) {
    switch (msg.type) {
      case "ready":
        // The renderer announces itself; the socket's open handler already ran
        // the handshake, so there is nothing further to do.
        return;

      case "send":
        void sendTurn(msg);
        return;

      case "cancelTurn":
        if (current().inFlightTurnId !== null) {
          wsNotify("$/cancelRequest", { id: current().inFlightTurnId });
        }
        return;

      case "newSession":
        void startSession(undefined);
        return;

      case "openSession":
        void startSession(msg.sessionId);
        return;

      case "closeSession":
        // Web-only: closing a tab hides it from the strip. The session is not
        // deleted — the sidebar still lists it. See 40-projects.js.
        closeSessionTab(msg.sessionId || "");
        return;

      case "deleteSession":
        // Web-only: throws the chat away in the core. The sidebar's × asks
        // twice before it gets here.
        void deleteSession(msg.projectId || currentProjectId, msg.sessionId || "");
        return;

      case "listSessions":
        void refreshSessionList(currentProjectId);
        return;

      case "permissionReply":
      case "questionReply":
        // Answered in 30-adapter-asks.js, which owns the JSON-RPC ids.
        return;

      case "switchProject":
        void switchProject(msg.projectId || "");
        return;

      case "openProjectConnection":
        void ensureConn(msg.projectId || "");
        return;

      default:
        // The composer's own messages — the model pill, the Orchestra
        // breakdown, slash commands, @-mentions, rewind, the pending bar — are
        // answered in 60-composer.js, all of them straight core RPC.
        if (handleComposerMessage(msg)) {
          return;
        }
        // What is left really does belong to a VS Code affordance this host
        // does not have: opening an editor on a file or a diff, and asking the
        // editor to tokenise a code block. Say so rather than swallow the
        // click.
        toRenderer({
          type: "systemNote",
          text: `"${msg.type}" needs an editor to open things in, so it does nothing here.`,
        });
    }
  }

  /** @param {any} msg */
  async function sendTurn(msg) {
    // Capture the id, not just the record: the user can switch projects while
    // this turn is awaiting, and everything after the await must know which
    // project it belongs to.
    const projectId = currentProjectId;
    const st = projectState(projectId);
    if (!st.sessionId) {
      toRenderer({ type: "error", message: "no session — reload the page" });
      return;
    }
    // The session this turn belongs to: the person can open another one while
    // it runs, and the answer must be written back to the session that asked.
    const sessionId = st.sessionId;
    // The chips on the message: files attached through the paperclip, a drop
    // or a paste (60-composer.js). Only ones with a path can go to the core —
    // it reads them from the workspace — and the echo shows the same ones.
    const files = (Array.isArray(msg.files) ? msg.files : []).filter(
      (f) => f && typeof f.path === "string" && f.path
    );
    const attachments = files.map((f) => {
      const a = { path: f.path };
      if (typeof f.name === "string" && f.name) {
        a.name = f.name;
      }
      if (f.kind === "image" || f.kind === "file") {
        a.kind = f.kind;
      }
      return a;
    });
    toRenderer({ type: "userEcho", text: msg.text || "", files: files.length ? files : undefined });
    toRenderer({ type: "turnStart" });
    toRenderer({ type: "turnInFlight", inFlight: true });
    st.status = "working";
    renderProjects();

    // Route through this project's own connection, not whichever one is
    // active by the time this line runs — the caller can switch away while
    // earlier awaits in this function (there are none here, but see
    // activateProject/startSession) are outstanding. sendCancellable allocates
    // and sends synchronously, so the id handed back and the id in the wire
    // frame are provably the same value.
    const conn = connFor(projectId);
    const turn = conn.sendCancellable("session.message", {
      session_id: sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
      ...(attachments.length ? { attachments } : {}),
    });
    st.inFlightTurnId = turn.id;
    // turnComplete's contract is `{ ok: boolean }`, and the renderer treats a
    // missing `ok` as failure — so this must report the truth, not a constant.
    let failed = false;
    try {
      await turn.done;
    } catch (err) {
      failed = true;
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    } finally {
      // The record and the rail always tell the truth, whichever project is on
      // screen. Only the renderer is gated: a turn that ends in a background
      // project must not put its error bubble, or its turnComplete, into the
      // transcript the user is looking at.
      st.inFlightTurnId = null;
      st.status = "idle";
      renderProjects();
      // Write the answer into the session before anything else: the core
      // records the person's message when the turn starts and nothing else,
      // so an answer this page does not save is gone when the session is
      // reopened. Not gated on the visible project — a turn that finished in
      // the background is exactly as worth keeping.
      void saveAssistantTurn(projectId, conn, sessionId);
      if (projectId === currentProjectId) {
        toRenderer({ type: "turnInFlight", inFlight: false });
        toRenderer({ type: "turnComplete", ok: !failed });
        // The log is complete once session.message has returned — the core
        // closes the writer before it answers — so this replaces the live
        // rows with the recorded ones, which carry the core's own timings.
        void refreshTrajectory(projectId, conn, st.sessionId);
      }
    }
  }

  /**
   * Append this turn's answer to the session's ui_messages.
   *
   * The core's own projection holds only what it can know without a client:
   * the person's message, appended when the turn starts (session_rpc.go,
   * SessionMessage). The answer — text, reasoning, the tools that ran, what
   * the step cost — exists only as a stream of events, so every host writes
   * it back itself; the editor does it in coreSession.ts (syncUIProjection)
   * and this is the same contract for the page. Read-modify-write against
   * session.get rather than a blind push, because the core appended the user
   * message after this page last saw the list.
   *
   * @param {string} projectId @param {any} conn @param {string} sessionId
   */
  async function saveAssistantTurn(projectId, conn, sessionId) {
    if (!sessionId || !conn) {
      return;
    }
    const answerMsg = assistantTurnProjection(projectId);
    if (!answerMsg) {
      return; // nothing was said and nothing ran: there is nothing to keep
    }
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const view = (await conn.send("session.get", { session_id: sessionId })) || {};
        const ui = Array.isArray(view.ui_messages) ? view.ui_messages.slice() : [];
        const last = ui.length ? ui[ui.length - 1] : null;
        const lastRole = last ? String((last && last.role) || "").toLowerCase() : "";
        const lastText = last ? String((last && (last.text || last.content)) || "").trim() : "";
        if (lastRole === "assistant" && lastText === answerMsg.text) {
          // The same answer already there — a retry, or another host that got
          // in first. Update it in place rather than saying it twice.
          ui[ui.length - 1] = Object.assign({}, last, answerMsg);
        } else {
          ui.push(answerMsg);
        }
        // title and model are read back and sent again on purpose: the core
        // sets both from these params, so leaving them out renames the
        // session to nothing.
        await conn.send("session.ui_sync", {
          session_id: sessionId,
          title: typeof view.title === "string" ? view.title : "",
          model: typeof view.model === "string" ? view.model : "",
          ui_messages: ui,
        });
        return;
      } catch (err) {
        if (attempt === 0) {
          await new Promise((r) => setTimeout(r, 800));
          continue;
        }
        // Say so rather than lose the answer silently.
        if (projectId === currentProjectId) {
          toRenderer({
            type: "systemNote",
            text: "The answer could not be saved to this session: " + String((err && err.message) || err),
          });
        }
      }
    }
  }

  /** @param {string | undefined} sessionId */
  async function startSession(sessionId) {
    // Same rule as sendTurn: current() is only safe before the first await.
    // The user can switch projects while this is in flight, and everything
    // after an await must be checked against currentProjectId before it paints.
    const projectId = currentProjectId;
    const st = projectState(projectId);
    const conn = connFor(projectId);
    try {
      const params = sessionId ? { session_id: sessionId } : {};
      const started = await conn.send("session.start", params);
      st.sessionId = started.session_id || "";
      if (projectId === currentProjectId) {
        toRenderer({ type: "clearMessages" });
      }
      if (started.restored) {
        const view = await conn.send("session.get", { session_id: st.sessionId });
        if (projectId === currentProjectId) {
          toRenderer({ type: "history", messages: historyMessagesFrom(view.ui_messages) });
          postRestoredUsage(view.ui_messages);
        }
      }
      if (projectId === currentProjectId) {
        toRenderer({ type: "header", sessionId: st.sessionId });
      }
      // A new or reopened session cleared the view above; give it the new
      // session's log, or the "nothing yet" answer, rather than leaving it
      // on "Loading trajectory…" until the next turn ends. Not awaited, for
      // the same reason as activateProject: it must not hold up
      // refreshSessionList below, and refreshTrajectory carries its own
      // stale guard for whenever it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
      await refreshSessionList(projectId);
    } catch (err) {
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    }
  }
