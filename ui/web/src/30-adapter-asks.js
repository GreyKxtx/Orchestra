  // Server-initiated requests: permission/request and question/ask.
  //
  // These are why the transport is a WebSocket. The renderer already has the
  // overlays (05b-overlays.js); this fragment is the wiring between them and
  // the JSON-RPC ids that must be answered — an unanswered id is a tool that
  // waits forever.

  /** @type {any} */ let pendingPermissionId = null;
  /** @type {any} */ let pendingQuestionId = null;

  /** @param {any} msg */
  function handleServerRequest(msg) {
    switch (msg.method) {
      case "permission/request":
        pendingPermissionId = msg.id;
        toRenderer({ type: "permissionRequest", request: msg.params || {} });
        return;
      case "question/ask":
        pendingQuestionId = msg.id;
        toRenderer({ type: "questionAsk", questions: (msg.params && msg.params.questions) || [] });
        return;
      default:
        // An unknown server request must still be answered, or the core waits.
        wsReply(msg.id, { error: "unsupported" });
    }
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
      if (p.type === "permissionReply") {
        if (pendingPermissionId === null) {
          return; // stale click; answering some other id would be worse
        }
        wsReply(pendingPermissionId, {
          approved: Boolean(p.approved),
          always: Boolean(p.always),
        });
        pendingPermissionId = null;
      } else if (p.type === "questionReply") {
        if (pendingQuestionId === null) {
          return;
        }
        wsReply(pendingQuestionId, { answers: Array.isArray(p.answers) ? p.answers : [] });
        pendingQuestionId = null;
      }
    }
  });
