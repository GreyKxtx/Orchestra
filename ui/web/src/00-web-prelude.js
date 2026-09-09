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

  // ---- JSON-RPC over the socket ------------------------------------------

  let ws = null;
  let nextRpcId = 1;
  /** @type {Map<number, {resolve: Function, reject: Function}>} */
  const pendingCalls = new Map();
  /** @type {((msg: any) => void) | null} */
  let onServerRequest = null; // set by 30-adapter-asks.js
  /** @type {((msg: any) => void) | null} */
  let onNotification = null; // set by 20-adapter-events.js

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

  /** @param {string} method @param {any} params @returns {Promise<any>} */
  function wsSend(method, params) {
    return new Promise((resolve, reject) => {
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        reject(new Error("not connected"));
        return;
      }
      const id = nextRpcId++;
      pendingCalls.set(id, { resolve, reject });
      ws.send(JSON.stringify({ jsonrpc: "2.0", id, method, params: params || {} }));
    });
  }

  /**
   * Like wsSend, but hands back the request id so the caller can cancel it
   * later with $/cancelRequest. Reading nextRpcId here is safe: wsSend
   * allocates it synchronously, with no await in between.
   * @param {string} method @param {any} params
   * @returns {{id: number, done: Promise<any>}}
   */
  function wsSendCancellable(method, params) {
    const id = nextRpcId;
    return { id, done: wsSend(method, params) };
  }

  /** @param {string} method @param {any} params */
  function wsNotify(method, params) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ jsonrpc: "2.0", method, params: params || {} }));
    }
  }

  /** Reply to a server-initiated request. @param {any} id @param {any} result */
  function wsReply(id, result) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ jsonrpc: "2.0", id, result }));
    }
  }

  /** @param {string} [projectId] */
  function connect(projectId) {
    const id = projectId || new URLSearchParams(location.search).get("project") || "";
    ws = new WebSocket(socketURL(id));
    ws.addEventListener("open", () => {
      toRenderer({ type: "status", status: "ok" });
      onConnected();
    });
    ws.addEventListener("close", () => {
      // A dropped socket ends the session on the core side, so say so plainly
      // rather than reconnecting into what looks like the same conversation.
      toRenderer({
        type: "status",
        status: "error",
        detail: "disconnected — reload to start a new session",
      });
      for (const { reject } of pendingCalls.values()) {
        reject(new Error("disconnected"));
      }
      pendingCalls.clear();
    });
    ws.addEventListener("error", () => {
      toRenderer({ type: "status", status: "error", detail: "connection error" });
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
        if (onServerRequest) {
          onServerRequest(msg);
        }
        return;
      }
      if (msg.method && onNotification) {
        onNotification(msg);
      }
    });

    // Test seam: adapter-test.mjs drives the outbound path by posting a window
    // message, because `host` lives inside this IIFE and nothing outside can
    // reach it. Harmless in a real page — no renderer fragment posts this type.
    window.addEventListener("message", (ev) => {
      if (ev.data && ev.data.type === "__host_dispatch__") {
        host.postMessage(ev.data.payload);
      }
    });
  }
