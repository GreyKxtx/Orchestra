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
        await refreshSessionList();
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
   * Make projectId the one the renderer shows. Repaints from the core rather
   * than from a buffer — session.get is what makes holding no background
   * scrollback affordable — and re-raises a prompt the project was waiting on.
   * @param {string} projectId
   */
  async function activateProject(projectId) {
    currentProjectId = projectId;
    const st = projectState(projectId);
    setActiveConn(connFor(projectId));

    toRenderer({ type: "clearMessages" });
    // Take down the project we are leaving. Nothing in the renderer hides the
    // overlay from the outside — hideOverlay is private to 05b-overlays.js and
    // clearMessages does not touch it — so an overlay raised for the previous
    // project would stay on screen over this project's transcript, and the
    // button's reply would then be read against this project's record, find no
    // pendingAsk, and be dropped. The asking project keeps its record, so
    // switching back re-raises the prompt intact.
    if (!st.pendingAsk) {
      const overlay = document.getElementById("overlay");
      if (overlay) {
        overlay.classList.add("hidden");
      }
    }

    if (st.sessionId) {
      try {
        const view = await wsSend("session.get", { session_id: st.sessionId });
        if (projectId !== currentProjectId) {
          return;
        }
        toRenderer({ type: "history", messages: view.ui_messages || [] });
      } catch (err) {
        if (projectId === currentProjectId) {
          toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
        }
      }
    }
    if (projectId !== currentProjectId) {
      return;
    }
    toRenderer({ type: "header", sessionId: st.sessionId });
    // The renderer's "turnInFlight" arm ignores the payload and always calls
    // setBusy(true) (ui/vscode/media/chat-src/07-events.js). Sending it with
    // inFlight:false would lock the composer into "Stop" on every switch to an
    // idle project. "turnComplete" is the only message that reaches
    // setBusy(false), so send whichever one is true.
    if (st.inFlightTurnId !== null) {
      toRenderer({ type: "turnInFlight", inFlight: true });
    } else {
      toRenderer({ type: "turnComplete" });
    }
    if (st.pendingAsk) {
      toRenderer(st.pendingAsk.rendererMessage);
    }
    await refreshSessionList();
    renderProjects();
  }

  async function refreshSessionList() {
    try {
      const res = await wsSend("session.list", {});
      toRenderer({ type: "sessionList", sessions: res.sessions || [] });
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
        void refreshSessionList();
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

    const turn = wsSendCancellable("session.message", {
      session_id: st.sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
    });
    st.inFlightTurnId = turn.id;
    try {
      await turn.done;
    } catch (err) {
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
        toRenderer({ type: "turnComplete" });
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
    try {
      const params = sessionId ? { session_id: sessionId } : {};
      const started = await wsSend("session.start", params);
      st.sessionId = started.session_id || "";
      if (projectId === currentProjectId) {
        toRenderer({ type: "clearMessages" });
      }
      if (started.restored) {
        const view = await wsSend("session.get", { session_id: st.sessionId });
        if (projectId === currentProjectId) {
          toRenderer({ type: "history", messages: view.ui_messages || [] });
        }
      }
      if (projectId === currentProjectId) {
        toRenderer({ type: "header", sessionId: st.sessionId });
      }
      await refreshSessionList();
    } catch (err) {
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    }
  }
