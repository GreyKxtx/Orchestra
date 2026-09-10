  // Server-initiated requests: permission/request and question/ask.
  //
  // These are why the transport is a WebSocket. The renderer already has the
  // overlays (05b-overlays.js); this fragment is the wiring between them and
  // the JSON-RPC ids that must be answered — an unanswered id is a tool that
  // waits forever.

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
      default:
        // An unknown server request must still be answered, or the core waits.
        connFor(projectId).reply(msg.id, { error: "unsupported" });
        return;
    }
    // Only the project on screen may raise an overlay. A background project's
    // prompt waits in its record and is raised by activateProject.
    if (projectId === currentProjectId) {
      toRenderer(st.pendingAsk.rendererMessage);
    }
    renderProjects();
  }

  // The overlays answer through the renderer's existing messages. Intercept
  // them here rather than in dispatchToCore, because they carry an id that
  // belongs to this fragment.
  window.addEventListener("message", (ev) => {
    const msg = ev.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    if (msg.type === "__host_dispatch__" && msg.payload) {
      const p = msg.payload;
      const st = projectState(currentProjectId);
      if (p.type === "permissionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "permission") {
          return; // stale click; answering some other id would be worse
        }
        connFor(currentProjectId).reply(st.pendingAsk.id, {
          approved: Boolean(p.approved),
          always: Boolean(p.always),
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        renderProjects();
      } else if (p.type === "questionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "question") {
          return;
        }
        connFor(currentProjectId).reply(st.pendingAsk.id, {
          answers: Array.isArray(p.answers) ? p.answers : [],
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        renderProjects();
      }
    }
  });
