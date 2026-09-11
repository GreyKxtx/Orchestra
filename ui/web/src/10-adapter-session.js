  // Outbound: the renderer's message protocol -> JSON-RPC.
  //
  // This is the browser's half of what ui/vscode/src/chat/panel.ts does for the
  // extension. Only v1 scope is wired: chat, cancellation, sessions. Message
  // types outside that scope are acknowledged and ignored rather than dropped
  // silently — a no-op the user can see beats a control that does nothing.

  /**
   * Per-project session state. The renderer shows one project at a time, so
   * exactly one of these is "current"; the others are what a switch restores.
   * @type {Map<string, {sessionId: string, inFlightTurnId: any, workspaceRoot: string, status: string, pendingAsk: any}>}
   */
  const perProject = new Map();
  let currentProjectId = "";

  /** @param {string} projectId */
  function projectState(projectId) {
    let st = perProject.get(projectId);
    if (!st) {
      st = { sessionId: "", inFlightTurnId: null, workspaceRoot: "", status: "idle", pendingAsk: null };
      perProject.set(projectId, st);
    }
    return st;
  }

  /** @param {string} projectId */
  function forgetProjectState(projectId) {
    perProject.delete(projectId);
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
      if (projectId !== currentProjectId || projectState(projectId).sessionId !== sessionId) {
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
      if (projectId === currentProjectId && projectState(projectId).sessionId === sessionId) {
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
        toRenderer({ type: "history", messages: view.ui_messages || [] });
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
        // Everything else belongs to a VS Code affordance this host does not
        // have (opening editors, applying pending diffs, the settings webview).
        // Say so once rather than swallowing the click.
        toRenderer({
          type: "systemNote",
          text: `"${msg.type}" is not available in the web UI yet.`,
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
    toRenderer({ type: "userEcho", text: msg.text || "" });
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
      session_id: st.sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
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
          toRenderer({ type: "history", messages: view.ui_messages || [] });
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
