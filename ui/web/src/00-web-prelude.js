  // The web host. Supplies the three methods the shared renderer fragments
  // reach for (see 01-dom-state.js's `host`), backed by a WebSocket instead of
  // the VS Code API. Everything the renderer knows about its host is here and
  // in the adapter fragments that follow.

  const STATE_KEY = "orchestra.web.state";

  const host = {
    /** @param {any} msg */
    postMessage(msg) {
      // dispatchToCore is defined in 10-adapter-session.js. Calls that arrive
      // before it exists are a bug, not a race: the renderer only posts in
      // response to user input or an inbound message, both of which come after
      // the whole bundle has evaluated.
      dispatchToCore(msg);
    },
    getState() {
      try {
        return JSON.parse(sessionStorage.getItem(STATE_KEY) || "{}");
      } catch (e) {
        return {};
      }
    },
    /** @param {any} state */
    setState(state) {
      try {
        sessionStorage.setItem(STATE_KEY, JSON.stringify(state));
      } catch (e) {
        // Private mode, or storage disabled. State is a convenience.
      }
    },
  };

  /**
   * Deliver an inbound message to the renderer. 07-events.js listens on
   * window's "message" event, so this is the same door VS Code posts through.
   * @param {any} msg
   */
  function toRenderer(msg) {
    window.postMessage(msg, "*");
  }

  // ---- JSON-RPC over one socket per project -------------------------------
  //
  // A project's core is reached through its own socket (part A's guard is per
  // project, so several are allowed). The four helpers below keep the
  // signatures the adapter fragments already use and route to whichever
  // project is active, so "which socket" is a question only this file and
  // 40-projects.js answer.

  /**
   * The socket for a project. No token: the page was served with an HttpOnly
   * cookie, which the browser attaches to the handshake by itself.
   * @param {string} projectId
   */
  function socketURL(projectId) {
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const base = `${scheme}//${location.host}/ws`;
    return projectId ? `${base}?project=${encodeURIComponent(projectId)}` : base;
  }

  /** @type {any} */
  let active = null;

  /**
   * @param {string} projectId "" for the project-less socket
   * @param {{onOpen?: Function, onClose?: Function, onError?: Function, onNotification?: Function, onServerRequest?: Function}} handlers
   */
  function createConn(projectId, handlers) {
    const h = handlers || {};
    let nextRpcId = 1;
    /** @type {Map<number, {resolve: Function, reject: Function}>} */
    const pendingCalls = new Map();
    const ws = new WebSocket(socketURL(projectId));

    const conn = {
      projectId,
      isOpen: () => ws.readyState === WebSocket.OPEN,
      /** @param {string} method @param {any} params @returns {Promise<any>} */
      send(method, params) {
        return new Promise((resolve, reject) => {
          if (ws.readyState !== WebSocket.OPEN) {
            reject(new Error("not connected"));
            return;
          }
          const id = nextRpcId++;
          pendingCalls.set(id, { resolve, reject });
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, method, params: params || {} }));
        });
      },
      /**
       * Like send, but hands back the request id so the caller can cancel it
       * later with $/cancelRequest. Reading nextRpcId here is safe: send
       * allocates it synchronously, with no await in between.
       * @param {string} method @param {any} params
       * @returns {{id: number, done: Promise<any>}}
       */
      sendCancellable(method, params) {
        const id = nextRpcId;
        return { id, done: conn.send(method, params) };
      },
      /** @param {string} method @param {any} params */
      notify(method, params) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", method, params: params || {} }));
        }
      },
      /** Reply to a server-initiated request. @param {any} id @param {any} result */
      reply(id, result) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, result }));
        }
      },
      close() {
        try {
          ws.close();
        } catch (e) {
          // Already closing. Nothing to do.
        }
      },
    };

    ws.addEventListener("open", () => {
      if (h.onOpen) h.onOpen(projectId);
    });
    ws.addEventListener("close", () => {
      for (const { reject } of pendingCalls.values()) {
        reject(new Error("disconnected"));
      }
      pendingCalls.clear();
      if (h.onClose) h.onClose(projectId);
    });
    ws.addEventListener("error", () => {
      if (h.onError) h.onError(projectId);
    });
    ws.addEventListener("message", (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (msg.id !== undefined && msg.method === undefined) {
        const p = pendingCalls.get(msg.id);
        if (!p) {
          return;
        }
        pendingCalls.delete(msg.id);
        if (msg.error) {
          p.reject(new Error(msg.error.message || "rpc error"));
        } else {
          p.resolve(msg.result);
        }
        return;
      }
      if (msg.id !== undefined && msg.method) {
        if (h.onServerRequest) h.onServerRequest(projectId, msg);
        return;
      }
      if (msg.method && h.onNotification) {
        h.onNotification(projectId, msg);
      }
    });

    return conn;
  }

  /** @param {any} conn */
  function setActiveConn(conn) {
    active = conn;
  }

  function activeConn() {
    return active;
  }

  // The four helpers the adapter fragments call. Same signatures as before;
  // the destination is now "whichever project is active".

  /** @param {string} method @param {any} params @returns {Promise<any>} */
  function wsSend(method, params) {
    if (!active) {
      return Promise.reject(new Error("not connected"));
    }
    return active.send(method, params);
  }

  /** @param {string} method @param {any} params @returns {{id: number, done: Promise<any>}} */
  function wsSendCancellable(method, params) {
    if (!active) {
      return { id: -1, done: Promise.reject(new Error("not connected")) };
    }
    return active.sendCancellable(method, params);
  }

  /** @param {string} method @param {any} params */
  function wsNotify(method, params) {
    if (active) active.notify(method, params);
  }

  /** @param {any} id @param {any} result */
  function wsReply(id, result) {
    if (active) active.reply(id, result);
  }

  // Test seam: adapter-test.mjs drives the outbound path by posting a window
  // message, because `host` lives inside this IIFE and nothing outside can
  // reach it. Harmless in a real page — no renderer fragment posts this type.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "__host_dispatch__") {
      host.postMessage(ev.data.payload);
    }
  });
