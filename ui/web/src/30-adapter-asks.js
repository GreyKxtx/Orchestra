  // Server-initiated requests: permission/request and question/ask.
  //
  // These are why the transport is a WebSocket. The renderer already has the
  // overlays (05b-overlays.js); this fragment is the wiring between them and
  // the JSON-RPC ids that must be answered — an unanswered id is a tool that
  // waits forever.

  // Which project's ask is on screen right now. The renderer's reply carries
  // no project and no request id, so this is the only thing that can tell us
  // who a click belongs to. Resolving against currentProjectId instead means a
  // switch mid-prompt answers the wrong project — see activateProject in
  // 10-adapter-session.js, which is the other half of that fix.
  let displayedAsk = null;

  /** @param {string} projectId @param {{kind: string, id: any}} ask */
  function setDisplayedAsk(projectId, ask) {
    displayedAsk = { projectId, kind: ask.kind, id: ask.id };
  }

  function clearDisplayedAsk() {
    displayedAsk = null;
  }

  /**
   * Whether the ask currently on screen belongs to projectId. 20-adapter-
   * events.js uses this to decide whether a turn ending elsewhere must also
   * take the overlay down with it, so a stale ask and a stale overlay never
   * separate.
   * @param {string} projectId
   */
  function isDisplayedAskFor(projectId) {
    return Boolean(displayedAsk && displayedAsk.projectId === projectId);
  }

  /**
   * The turn that asked has ended: nobody waits on its prompt any more. Left
   * alone, pendingAsk keeps a JSON-RPC id nobody is waiting on, the rail
   * shows a permanent "asking" badge, and the overlay takes an answer that
   * goes nowhere. The displayed-ask state and the overlay come down together.
   * @param {string} projectId
   */
  function dropTurnAsk(projectId) {
    const st = projectState(projectId);
    if (!st.pendingAsk) {
      return;
    }
    st.pendingAsk = null;
    if (isDisplayedAskFor(projectId)) {
      clearDisplayedAsk();
      const overlay = document.getElementById("overlay");
      if (overlay) {
        overlay.classList.add("hidden");
      }
    }
  }

  /**
   * @param {string} projectId @param {any} msg
   */
  function handleServerRequest(projectId, msg) {
    const st = projectState(projectId);
    switch (msg.method) {
      case "permission/request":
        st.pendingAsk = {
          kind: "permission",
          id: msg.id,
          rendererMessage: { type: "permissionRequest", request: msg.params || {} },
        };
        st.status = "asking";
        break;
      case "question/ask":
        st.pendingAsk = {
          kind: "question",
          id: msg.id,
          rendererMessage: {
            type: "questionAsk",
            questions: (msg.params && msg.params.questions) || [],
          },
        };
        st.status = "asking";
        break;
      case "browser/call": {
        // The turn's browser.* tools, acting on the browser view this window
        // has open. No overlay and no waiting on the person: the permission
        // was given when the turn was sent, and the panel answers or refuses.
        const conn = connFor(projectId);
        const p = msg.params || {};
        void browserPanelOp(String(p.op || ""), p.params || {})
          .then((result) => conn.reply(msg.id, result || {}))
          .catch((err) => conn.reply(msg.id, { error: String((err && err.message) || err) }));
        return;
      }
      default:
        // An unknown server request must still be answered, or the core waits.
        connFor(projectId).reply(msg.id, { error: "unsupported" });
        return;
    }
    // Only the project on screen may raise an overlay. A background project's
    // prompt waits in its record and is raised by activateProject.
    if (projectId === currentProjectId) {
      toRenderer(st.pendingAsk.rendererMessage);
      setDisplayedAsk(projectId, st.pendingAsk);
    }
    renderProjects();
  }

  // The overlays answer through the renderer's existing messages, which
  // dispatchToCore hands here: the reply carries an id that belongs to this
  // fragment. It used to be a window "message" listener, which only the test
  // seam ever reached — in the page the renderer calls host.postMessage
  // directly, so an answered question never went back and the turn waited.
  /** @param {any} p */
  function answerDisplayedAsk(p) {
    // The renderer's reply carries no project id and no request id, so it is
    // resolved against whichever ask is actually displayed on screen — never
    // against currentProjectId, which may already name a different project
    // by the time the click lands.
    if (!displayedAsk) {
      return; // stale click; nothing is on screen to answer
    }
    const st = projectState(displayedAsk.projectId);
    if (p.type === "permissionReply") {
      if (!st.pendingAsk || st.pendingAsk.kind !== "permission" || displayedAsk.kind !== "permission") {
        return; // stale click; answering some other id would be worse
      }
      connFor(displayedAsk.projectId).reply(displayedAsk.id, {
        approved: Boolean(p.approved),
        always: Boolean(p.always),
      });
    } else if (p.type === "questionReply") {
      if (!st.pendingAsk || st.pendingAsk.kind !== "question" || displayedAsk.kind !== "question") {
        return;
      }
      connFor(displayedAsk.projectId).reply(displayedAsk.id, {
        answers: Array.isArray(p.answers) ? p.answers : [],
      });
    } else {
      return;
    }
    st.pendingAsk = null;
    st.status = st.inFlightTurnId !== null ? "working" : "idle";
    clearDisplayedAsk();
    renderProjects();
  }
