// Tests the browser adapter without a browser.
//
// The bundle is one IIFE over a handful of globals (window, document,
// WebSocket, sessionStorage, location). Providing those is enough to evaluate
// it and drive the translation, which is the part worth testing: what JSON-RPC
// does the renderer's message produce, and what renderer message does a
// JSON-RPC frame produce.
//
// Run: node ui/web/scripts/adapter-test.mjs

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");

/**
 * Evaluate the bundle in a sandbox with a scriptable socket. Returns handles
 * for driving it: `sent` are the frames the page wrote, `deliver` pushes a
 * frame in, `post` posts a renderer message, `inbound` are the messages the
 * renderer received.
 */
export function loadBundle(opts = {}) {
  const src = fs.readFileSync(path.join(root, "static", "web.bundle.js"), "utf8");

  const sent = [];
  const inbound = [];
  const sockets = [];
  let socket = null;

  class FakeWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;
    constructor(url) {
      this.url = url;
      // As in a browser: connecting until "open" fires. A send before that
      // is refused ("not connected"), which is what a settings panel opened
      // while the core is still starting must see.
      this.readyState = 0;
      this.listeners = {};
      socket = this;
      sockets.push(this);
    }
    addEventListener(type, fn) {
      (this.listeners[type] ||= []).push(fn);
    }
    send(data) {
      sent.push({ url: this.url, ...JSON.parse(data) });
    }
    emit(type, ev) {
      if (type === "open") this.readyState = 1;
      if (type === "close") this.readyState = 3;
      for (const fn of this.listeners[type] || []) fn(ev);
    }
  }

  const handlers = [];
  const store = new Map();
  const elementsById = new Map();
  // A real classList, not a no-op stub: I1's fix reads back whether "hidden" is
  // present, and a stub that silently drops add()/remove() would make that
  // undetectable no matter what the production code does.
  const makeClassList = () => {
    const classes = new Set();
    return {
      add: (...names) => names.forEach((n) => classes.add(n)),
      remove: (...names) => names.forEach((n) => classes.delete(n)),
      toggle: (name, force) => {
        const next = force === undefined ? !classes.has(name) : Boolean(force);
        if (next) classes.add(name);
        else classes.delete(name);
        return next;
      },
      contains: (name) => classes.has(name),
    };
  };
  const el = () => ({
    _listeners: {},
    addEventListener(type, fn) {
      (this._listeners[type] ||= []).push(fn);
    },
    click() {
      // A real click event carries both, and listeners in the shared renderer
      // call stopPropagation as a matter of course.
      const ev = { preventDefault() {}, stopPropagation() {}, stopImmediatePropagation() {}, target: this };
      for (const fn of this._listeners.click || []) fn(ev);
    },
    append() {},
    keydown(key) {
      for (const fn of this._listeners.keydown || []) fn({ key, preventDefault() {} });
    },
    removeEventListener() {},
    classList: makeClassList(),
    appendChild() {},
    removeChild() {},
    insertBefore() {},
    setAttribute() {},
    removeAttribute() {},
    getAttribute: () => null,
    closest: () => null,
    focus() {},
    blur() {},
    remove() {},
    style: { setProperty() {}, removeProperty() {}, getPropertyValue: () => "" },
    dataset: {},
    children: [],
    childNodes: [],
    textContent: "",
    innerHTML: "",
    innerText: "",
    value: "",
    scrollTop: 0,
    scrollHeight: 0,
    clientHeight: 0,
    offsetHeight: 0,
    querySelector: () => null,
    querySelectorAll: () => [],
    getBoundingClientRect: () => ({ top: 0, left: 0, width: 0, height: 0, bottom: 0, right: 0 }),
  });

  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    queueMicrotask,
    requestAnimationFrame: (fn) => setTimeout(fn, 0),
    cancelAnimationFrame: (h) => clearTimeout(h),
    WebSocket: FakeWebSocket,
    location: {
      protocol: "http:",
      host: "127.0.0.1:9",
      origin: "http://127.0.0.1:9",
      search: opts.search ?? "",
    },
    sessionStorage: {
      getItem: (k) => (store.has(k) ? store.get(k) : null),
      setItem: (k, v) => store.set(k, String(v)),
      removeItem: (k) => store.delete(k),
    },
    document: {
      // A real browser returns the same node for the same id every time —
      // e.g. 01-dom-state.js and activateProject both look up "#overlay" and
      // must land on one shared element, not two independent stubs. Cache by
      // id instead of minting a fresh stub per call.
      getElementById: (id) => {
        let e = elementsById.get(id);
        if (!e) {
          e = el();
          elementsById.set(id, e);
        }
        return e;
      },
      createElement: () => el(),
      createTextNode: () => el(),
      createDocumentFragment: () => el(),
      addEventListener() {},
      removeEventListener() {},
      body: el(),
      documentElement: el(),
      querySelector: () => null,
      querySelectorAll: () => [],
    },
    navigator: { clipboard: { writeText: async () => {} }, userAgent: "node" },
    URLSearchParams,
    crypto: { randomUUID: () => "test-uuid" },
    Date,
    Math,
    JSON,
    Promise,
    Map,
    Set,
    Error,
    Array,
    Object,
    String,
    Number,
    Boolean,
    RegExp,
  };
  sandbox.window = sandbox;
  sandbox.self = sandbox;
  sandbox.window.addEventListener = (type, fn) => {
    if (type === "message") handlers.push(fn);
  };
  sandbox.window.removeEventListener = () => {};
  sandbox.window.postMessage = (msg) => {
    // A real postMessage structurally clones its payload — the listener never
    // sees the sender's own object. The vm sandbox otherwise hands back the
    // exact object the bundle (running in its own vm realm) built, which
    // carries that realm's Object.prototype and fails a strict deepEqual
    // against a plain object built in this module — a mismatch a real browser
    // could never produce. Every payload here is plain JSON-safe data, so a
    // JSON round trip is a faithful enough clone.
    const cloned = JSON.parse(JSON.stringify(msg));
    inbound.push(cloned);
    for (const fn of handlers.slice()) {
      try {
        fn({ data: cloned });
      } catch (e) {
        // A renderer fragment tripping over the stub DOM must not mask what the
        // adapter did; the adapter's own listeners are registered first.
      }
    }
  };
  sandbox.globalThis = sandbox;

  const fetchCalls = [];
  // One open project, which is what every `orchestra web` that was given a
  // folder reports. An EMPTY list is no longer the same thing: it means
  // --no-project, and the page answers it with the start screen and opens no
  // socket at all — so a test that wants the adapter connected must say a
  // project is there. Tests that want the start screen respond with [].
  let fetchResponder = () => ({
    projects: [{ id: "p-default", path: "/w", name: "w", state: "ready", sessions: 0 }],
  });
  sandbox.fetch = async (url, init) => {
    fetchCalls.push({ url: String(url), init: init || {} });
    // A real fetch never resolves synchronously; this one must not either. A
    // caller loads the bundle — which fires 40-projects.js's startup fetch
    // immediately — and only then calls setFetchResponder(), still on the same
    // synchronous turn. Reading fetchResponder before yielding would freeze in
    // the default responder set above, no matter what the test configures.
    await Promise.resolve();
    const raw = fetchResponder(String(url), init || {});
    // A responder that wants a failure says so with __httpStatus; everything
    // else is a 200. Without this the harness could only ever produce success,
    // so the page's handling of a rejected request went untested.
    let status = 200;
    let body = raw;
    if (raw && typeof raw.__httpStatus === "number") {
      status = raw.__httpStatus;
      body = { ...raw };
      delete body.__httpStatus;
    }
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    };
  };

  vm.createContext(sandbox);
  vm.runInContext(src, sandbox, { filename: "web.bundle.js" });

  return {
    sent,
    inbound,
    fetchCalls,
    setFetchResponder: (fn) => {
      fetchResponder = fn;
    },
    setTauri: (api) => {
      sandbox.__TAURI__ = api;
    },
    socketFor: (projectId) =>
      sockets.find((s) => s.url.includes(`project=${encodeURIComponent(projectId)}`)) || null,
    open: () => socket.emit("open", {}),
    openFor: (projectId) => {
      const s = sockets.find((x) => x.url.includes(`project=${encodeURIComponent(projectId)}`));
      assert.ok(s, `no socket for project ${projectId}; urls: ${sockets.map((x) => x.url).join(", ")}`);
      s.emit("open", {});
    },
    deliver: (obj) => socket.emit("message", { data: JSON.stringify(obj) }),
    deliverTo: (projectId, obj) => {
      const s = sockets.find((x) => x.url.includes(`project=${encodeURIComponent(projectId)}`));
      assert.ok(s, `no socket for project ${projectId}; urls: ${sockets.map((x) => x.url).join(", ")}`);
      s.emit("message", { data: JSON.stringify(obj) });
    },
    post: (msg) => sandbox.window.postMessage(msg),
    /**
     * A message from an iframe: same-origin, with the frame's contentWindow as
     * its source. 50-settings.js accepts nothing else on the window's message
     * channel — every other message there belongs to the renderer.
     */
    postFromFrame: (frameId, msg) => {
      const frame = elementsById.get(frameId);
      assert.ok(frame && frame.contentWindow, `frame ${frameId} has no contentWindow`);
      const cloned = JSON.parse(JSON.stringify(msg));
      for (const fn of handlers.slice()) {
        try {
          fn({ data: cloned, source: frame.contentWindow, origin: sandbox.location.origin });
        } catch (e) {
          // As in postMessage above: a renderer fragment tripping over the
          // stub DOM must not mask what the adapter did.
        }
      }
    },
    close: () => socket.emit("close", {}),
    get socketURL() {
      return socket ? socket.url : "";
    },
    /** The stub element for an id, if the bundle has looked it up. */
    elementById: (id) => elementsById.get(id) || null,
  };
}

/** Answer the next request for `method` with `result`, so a chain can proceed. */
function answer(b, method, result) {
  const req = b.sent.find((m) => m.method === method && m.id !== undefined);
  assert.ok(req, `no ${method} was sent; sent: ${b.sent.map((m) => m.method).join(", ")}`);
  b.deliver({ jsonrpc: "2.0", id: req.id, result });
  return req;
}

/** The renderer posts through `host`, which lives inside the IIFE; this is the
 * test seam 00-web-prelude.js installs for reaching it. */
function dispatch(b, msg) {
  b.post({ type: "__host_dispatch__", payload: msg });
}

const tick = () => new Promise((r) => setTimeout(r, 0));

/**
 * Drive the handshake to a started session. core.health comes first because it
 * is one of the only two methods the core answers before initialize
 * (internal/core/rpc_handler.go:76), and it is where project_root comes from.
 */
async function handshake(b) {
  // The id-less startup socket only opens after 40-projects.js's startup IIFE
  // resolves the project id via GET /api/projects (the C1 fix) — a real
  // microtask hop now, not a synchronous side effect of loadBundle().
  await tick();
  b.open();
  answer(b, "core.health", {
    workspace_root: "/w",
    project_id: "p",
    protocol_version: 15,
    ops_version: 1,
    tools_version: 14,
  });
  await tick();
  answer(b, "initialize", {});
  await tick();
  answer(b, "session.start", { session_id: "s-1", restored: false });
  await tick();
  return b;
}

test("connecting handshakes and starts a session", async () => {
  const b = loadBundle();
  // Same reason as handshake()'s: the id-less startup socket only opens after
  // the API round trip the C1 fix added.
  await tick();
  b.open();

  answer(b, "core.health", {
    workspace_root: "/w",
    project_id: "p",
    protocol_version: 15,
    ops_version: 1,
    tools_version: 14,
  });
  await tick();

  const init = answer(b, "initialize", {});
  assert.equal(init.params.project_root, "/w", "project_root must come from core.health");
  assert.equal(init.params.project_id, "p");

  await tick();
  assert.ok(answer(b, "session.start", { session_id: "s-1", restored: false }));
});

test("a composer send becomes session.message", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;

  // What 06-composer.js:305-325 posts.
  dispatch(b, {
    type: "send",
    text: "hello",
    mode: "build",
    profile: "",
    apply: false,
    allowExec: true,
    files: [],
  });

  const msg = b.sent.find((m) => m.method === "session.message");
  assert.ok(msg, `no session.message; sent: ${b.sent.map((m) => m.method).join(", ")}`);
  assert.equal(msg.params.session_id, "s-1");
  assert.equal(msg.params.content, "hello");
  assert.equal(msg.params.allow_exec, true);
  assert.equal(msg.params.apply, true, "the web host applies to disk: it has no Accept/Reject editor UI");
});

test("cancelTurn sends $/cancelRequest for the in-flight turn", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;

  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", allowExec: false, files: [] });
  const turn = b.sent.find((m) => m.method === "session.message");
  assert.ok(turn, "no session.message went out");
  b.sent.length = 0;

  dispatch(b, { type: "cancelTurn" });
  const cancel = b.sent.find((m) => m.method === "$/cancelRequest");
  assert.ok(cancel, "cancelTurn must cancel the in-flight request, or Stop does nothing");
  assert.equal(cancel.params.id, turn.id);
});

/** Bring a bundle to a started session with the message logs cleared. */
async function ready(b) {
  await handshake(b);
  b.inbound.length = 0;
  b.sent.length = 0;
  return b;
}

test("message_delta accumulates and reaches the renderer as text", async () => {
  const b = await ready(loadBundle());
  b.deliver({ jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", content: "Hel" } });
  b.deliver({ jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", content: "lo" } });

  const texts = b.inbound
    .filter((m) => m.type === "delta" || m.type === "deltaSync")
    .map((m) => m.content);
  assert.ok(texts.length > 0, "no delta reached the renderer");
  assert.equal(texts[texts.length - 1], "Hello", "deltas must accumulate, not replace");
});

test("child-scoped deltas do not leak into the main transcript", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "subagent chatter", scope: "child" },
  });
  const leaked = b.inbound.filter((m) => m.type === "delta" || m.type === "deltaSync");
  assert.equal(leaked.length, 0, "a child's tokens must not appear in the parent transcript");
});

test("a tool call becomes a running block, then a completed one", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_start", tool_call_id: "t1", tool_call_name: "bash" },
  });
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_completed", tool_call_id: "t1", tool_call_name: "bash", content: "ok" },
  });

  const blocks = b.inbound.filter((m) => m.type === "toolBlock");
  assert.equal(blocks.length, 2, `expected start+complete, got ${blocks.length}`);
  // The renderer draws a block from its phase, not from a status field.
  assert.equal(blocks[0].phase, "start");
  assert.equal(blocks[0].toolName, "bash");
  assert.equal(blocks[1].phase, "complete");
  assert.equal(blocks[1].content, "ok");
});

test("exec output is streamed into the tool block", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    method: "exec/output_chunk",
    params: { chunk: "line one\n" },
  });
  const chunks = b.inbound.filter((m) => m.type === "execChunk");
  assert.equal(chunks.length, 1);
  assert.equal(chunks[0].chunk, "line one\n");
});

test("permission/request opens the overlay and the reply goes back with the same id", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    id: "srv-1",
    method: "permission/request",
    params: { tool: "bash", description: "go test ./...", reason: "to verify the fix" },
  });

  const shown = b.inbound.find((m) => m.type === "permissionRequest");
  assert.ok(shown, "the permission overlay was never shown");
  assert.equal(shown.request.tool, "bash");

  // The overlay replies through the renderer's existing message
  // (05b-overlays.js:37-42).
  dispatch(b, { type: "permissionReply", approved: true, always: false });

  const reply = b.sent.find((m) => m.id === "srv-1");
  assert.ok(reply, "no reply was sent for srv-1 — the tool would hang");
  assert.equal(reply.result.approved, true);
});

test("question/ask round trips answers", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    id: "srv-2",
    method: "question/ask",
    params: { questions: [{ question: "Which approach?", options: ["A", "B"], allow_multiple: false }] },
  });

  const shown = b.inbound.find((m) => m.type === "questionAsk");
  assert.ok(shown, "the question overlay was never shown");

  dispatch(b, { type: "questionReply", answers: ["A"] });

  const reply = b.sent.find((m) => m.id === "srv-2");
  assert.ok(reply, "no reply was sent for srv-2 — the question tool would hang");
  assert.deepEqual(reply.result.answers, ["A"]);
});

test("a permission reply with no outstanding request is dropped, not misrouted", async () => {
  const b = await ready(loadBundle());
  dispatch(b, { type: "permissionReply", approved: true, always: false });
  const strays = b.sent.filter((m) => m.result !== undefined);
  assert.equal(strays.length, 0, "a stale reply must not be sent against some other id");
});

test("an unknown server request is still answered, or the core waits forever", async () => {
  const b = await ready(loadBundle());
  b.deliver({ jsonrpc: "2.0", id: "srv-9", method: "some/futureRequest", params: {} });
  const reply = b.sent.find((m) => m.id === "srv-9");
  assert.ok(reply, "an unhandled server request left the core hanging");
});

test("the socket URL carries no token — the cookie authenticates", async () => {
  const b = loadBundle();
  // The id-less startup socket only opens once the API round trip the C1 fix
  // added resolves.
  await tick();
  assert.ok(b.socketURL, "the bundle did not expose the socket URL it dialled");
  assert.ok(
    !/token=/.test(b.socketURL),
    `socket URL still threads a token (${b.socketURL}); the cookie is the credential now`
  );
});

test("a project id reaches the socket URL", async () => {
  const b = loadBundle({ search: "?project=sha256%3Aabc" });
  assert.match(
    b.socketURL,
    /[?&]project=sha256%3Aabc/,
    `socket URL did not carry the project (${b.socketURL})`
  );
});

/** Drive one project's socket through the handshake to a started session. */
async function handshakeFor(b, projectId) {
  b.openFor(projectId);
  answerOn(b, projectId, "core.health", {
    workspace_root: "/" + projectId,
    project_id: projectId,
    protocol_version: 15,
    ops_version: 1,
    tools_version: 14,
  });
  await tick();
  answerOn(b, projectId, "initialize", {});
  await tick();
  answerOn(b, projectId, "session.start", { session_id: "s-" + projectId, restored: false });
  await tick();
}

/** Open a project's socket without making it active. */
async function openBackground(b, projectId) {
  dispatch(b, { type: "openProjectConnection", projectId });
  await tick();
  await handshakeFor(b, projectId);
}

/**
 * Answer a pending request on one project's socket.
 *
 * The answered-ids set hangs off the bundle handle, not off the module. A
 * module-level set would be shared by every test in the file, and the keys
 * collide across tests: the socket url is fixed (`127.0.0.1:9`), project ids
 * repeat (`A`, `B`), and each bundle's rpc ids restart at 1 — so the second
 * test's first request would look already answered and this helper would fail
 * with "no unanswered core.health".
 */
function answerOn(b, projectId, method, result) {
  if (!b.__answered) {
    b.__answered = new Set();
  }
  const req = b.sent.find(
    (m) =>
      m.method === method &&
      m.id !== undefined &&
      String(m.url).includes(`project=${encodeURIComponent(projectId)}`) &&
      !b.__answered.has(m.url + ":" + m.id)
  );
  assert.ok(
    req,
    `no unanswered ${method} on project ${projectId}; sent: ${b.sent
      .map((m) => m.method + "@" + m.url)
      .join(", ")}`
  );
  b.__answered.add(req.url + ":" + req.id);
  b.deliverTo(projectId, { jsonrpc: "2.0", id: req.id, result });
  return req;
}

test("a background project's streamed text does not enter the active transcript", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();

  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // B streams assistant text. A is active, so nothing may reach the renderer's
  // transcript.
  const before = b.inbound.filter((m) => m.type === "deltaSync").length;
  b.deliverTo("B", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "hello from B", step: 1 },
  });
  await tick();
  const after = b.inbound.filter((m) => m.type === "deltaSync").length;
  assert.equal(after, before, "a background project's text was folded into the active transcript");
});

test("a background project that starts a tool call reports itself as working", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_start", tool_call_id: "t1", tool_call_name: "read", step: 1 },
  });
  await tick();

  const railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.ok(railed, "no projectList message was posted after a background event");
  const bEntry = railed.projects.find((p) => p.id === "B");
  assert.equal(bEntry.status, "working");
});

test("a permission request in a background project marks it asking and survives a switch", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 77,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  let railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.equal(railed.projects.find((p) => p.id === "B").status, "asking");

  // No overlay while B is in the background.
  assert.equal(
    b.inbound.filter((m) => m.type === "permissionRequest").length,
    0,
    "a background project raised an overlay over the active project"
  );

  // Switching to B raises it, and answering replies on B's socket with B's id.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  assert.equal(
    b.inbound.filter((m) => m.type === "permissionRequest").length,
    1,
    "switching to a project that is asking did not raise its prompt"
  );

  dispatch(b, { type: "permissionReply", approved: true, always: false });
  await tick();
  const reply = b.sent.filter((m) => m.id === 77 && m.result !== undefined).pop();
  assert.ok(reply, "the permission reply never went out");
  assert.match(reply.url, /project=B/, "the reply went to the wrong project's socket");
});

test("switching repaints from the core rather than a buffer", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();

  const get = b.sent.filter((m) => m.method === "session.get").pop();
  assert.ok(get, "switching did not ask the core for the session");
  assert.match(get.url, /project=B/);
  answerOn(b, "B", "session.get", { ui_messages: [{ role: "user", text: "earlier" }] });
  await tick();

  const history = b.inbound.filter((m) => m.type === "history").pop();
  // uiIndex is the row's place in the core's own list, which rewind aims at.
  assert.deepEqual(history.messages, [{ role: "user", text: "earlier", uiIndex: 0 }]);
});

// ---- fix round 1 regression tests --------------------------------------

test("orchestra web with no ?project= resolves the id from the API before opening the socket", async () => {
  const b = loadBundle({ search: "" });
  b.setFetchResponder(() => ({
    projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }],
  }));
  await tick();

  // C1: adopting the id after opening a "" socket leaves the connection, the
  // per-project record and the rail keyed differently — the id must be
  // resolved before the socket opens, so the very first socket already
  // carries it.
  assert.match(
    b.socketURL,
    /[?&]project=A\b/,
    `the startup socket must be keyed by the resolved project id, not opened blind (${b.socketURL})`
  );

  await handshakeFor(b, "A");
  b.inbound.length = 0;
  b.deliverTo("A", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "hello", step: 1 },
  });
  await tick();
  assert.ok(
    b.inbound.some((m) => m.type === "deltaSync"),
    "a notification for the resolved project never reached the renderer — currentProjectId and the socket's key must match"
  );

  const railed = b.inbound.filter((m) => m.type === "projectList").pop();
  const entry = railed && railed.projects.find((p) => p.id === "A");
  assert.ok(entry, "the resolved project never appeared in the rail");
  assert.equal(entry.active, true, "the rail did not mark the resolved project active");
});

test("switching to an idle project posts turnComplete, never turnInFlight", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // B has a session and nothing in flight.
  b.inbound.length = 0;
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  // C2: the renderer's turnInFlight arm always calls setBusy(true) regardless
  // of the payload, so sending it with inFlight:false locks the composer into
  // "Stop" forever. turnComplete is the only message that reaches setBusy(false).
  assert.equal(
    b.inbound.filter((m) => m.type === "turnInFlight").length,
    0,
    "switching to an idle project must not post turnInFlight, or the composer locks into Stop"
  );
  assert.ok(
    b.inbound.some((m) => m.type === "turnComplete"),
    "switching to an idle project never told the composer the turn was done"
  );
});

test("a turn that ends in a background project never repaints the transcript on screen", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // A starts a turn, then the user switches to B before it settles.
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", allowExec: false, files: [] });
  await tick();
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  // Scenario 1: A's turn fails after the switch.
  b.inbound.length = 0;
  const failReq = b.sent
    .filter((m) => m.method === "session.message" && String(m.url).includes("project=A"))
    .pop();
  assert.ok(failReq, "no session.message was ever sent for A");
  b.deliverTo("A", { jsonrpc: "2.0", id: failReq.id, error: { message: "boom" } });
  await tick();

  assert.equal(
    b.inbound.filter((m) => m.type === "error").length,
    0,
    "a background turn's failure leaked an error bubble into the visible transcript"
  );
  assert.equal(
    b.inbound.filter((m) => m.type === "turnComplete").length,
    0,
    "a background turn's failure leaked turnComplete into the visible transcript"
  );
  assert.equal(
    b.inbound.filter((m) => m.type === "turnInFlight").length,
    0,
    "a background turn's failure leaked turnInFlight into the visible transcript"
  );

  let railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.ok(railed, "no projectList was posted after the background turn settled");
  assert.equal(
    railed.projects.find((p) => p.id === "A").status,
    "idle",
    "A's rail status must return to idle even though nothing painted"
  );

  // Scenario 2: back to A briefly to start a second turn, then away again —
  // this one succeeds after the switch.
  dispatch(b, { type: "switchProject", projectId: "A" });
  await tick();
  answerOn(b, "A", "session.get", { ui_messages: [] });
  await tick();

  dispatch(b, { type: "send", text: "again", mode: "build", profile: "", allowExec: false, files: [] });
  await tick();
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  b.inbound.length = 0;
  const okReq = b.sent
    .filter((m) => m.method === "session.message" && String(m.url).includes("project=A"))
    .pop();
  assert.ok(okReq, "no second session.message was ever sent for A");
  b.deliverTo("A", { jsonrpc: "2.0", id: okReq.id, result: {} });
  await tick();

  assert.equal(
    b.inbound.filter((m) => m.type === "error").length,
    0,
    "a background turn's success leaked an error bubble into the visible transcript"
  );
  assert.equal(
    b.inbound.filter((m) => m.type === "turnComplete").length,
    0,
    "a background turn's success leaked turnComplete into the visible transcript"
  );
  assert.equal(
    b.inbound.filter((m) => m.type === "turnInFlight").length,
    0,
    "a background turn's success leaked turnInFlight into the visible transcript"
  );

  railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.ok(railed, "no projectList was posted after the second background turn settled");
  assert.equal(railed.projects.find((p) => p.id === "A").status, "idle");
});

test("switching away from a raised overlay hides it, and switching back re-raises it", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  const overlay = b.elementById("overlay");
  assert.ok(overlay, "the bundle never looked up #overlay");
  // The real page starts with class="overlay hidden"; the stub DOM does not
  // parse that markup, so prime the same starting state by hand.
  overlay.classList.add("hidden");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 55,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  // Switch to B: its permission prompt raises the overlay.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  assert.equal(
    overlay.classList.contains("hidden"),
    false,
    "switching to a project that is asking must raise its overlay"
  );

  // I1: switch back to A. Nothing outside 05b-overlays.js can hide the
  // overlay from here, so without the fix B's prompt stays on screen over A.
  dispatch(b, { type: "switchProject", projectId: "A" });
  await tick();
  answerOn(b, "A", "session.get", { ui_messages: [] });
  await tick();
  assert.equal(
    overlay.classList.contains("hidden"),
    true,
    "switching away from an asking project left its overlay stranded on screen"
  );

  // Switch back to B: the same prompt re-raises, proving the record survived.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  assert.equal(
    overlay.classList.contains("hidden"),
    false,
    "switching back to an asking project did not re-raise its prompt"
  );
});

test("per-project text accumulators do not cross-contaminate on switch", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("A", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "Alpha", step: 1 },
  });
  await tick();

  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  b.inbound.length = 0;
  b.deliverTo("B", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "Beta", step: 1 },
  });
  await tick();
  const bDelta = b.inbound.filter((m) => m.type === "deltaSync").pop();
  assert.ok(bDelta, "B's own text never reached the renderer while B was active");
  assert.equal(bDelta.content, "Beta", "B's accumulator must start clean, not carry A's text");

  dispatch(b, { type: "switchProject", projectId: "A" });
  await tick();
  answerOn(b, "A", "session.get", { ui_messages: [] });
  await tick();

  b.inbound.length = 0;
  b.deliverTo("A", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: " Gamma", step: 1 },
  });
  await tick();
  const aDelta = b.inbound.filter((m) => m.type === "deltaSync").pop();
  assert.ok(aDelta, "A's own text never reached the renderer after switching back");
  assert.equal(
    aDelta.content,
    "Alpha Gamma",
    "A's accumulator must still hold only A's text, not B's — a single shared accumulator would fail this"
  );
});

// ---- Task 9: notifications, and the refreshSessionList guard -----------

test("a background project that starts asking raises one notification", async () => {
  const b = loadBundle({ search: "?project=A" });
  const notified = [];
  b.setTauri({
    notification: {
      sendNotification: (opts) => {
        notified.push(opts);
      },
    },
  });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 5,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  assert.equal(notified.length, 1, "a background project's prompt raised no notification");
  assert.match(notified[0].body || "", /b/i, "the notification does not name the project");

  // The active project's own prompt must NOT notify — it is already on screen.
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: 6,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();
  assert.equal(notified.length, 1, "the active project's prompt raised a notification");
});

test("a session.list that resolves after switching away does not paint the abandoned project", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // Start switching to B. Its session.get answers, which is what lets
  // activateProject go on to ask for its session.list — but that request is
  // left unanswered here, so it is still in flight when the user leaves B.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  const listReq = b.sent.find(
    (m) => m.method === "session.list" && String(m.url).includes("project=B")
  );
  assert.ok(listReq, "activateProject never asked B for its session list");

  // Back to A before B's session.list comes back.
  dispatch(b, { type: "switchProject", projectId: "A" });
  await tick();
  answerOn(b, "A", "session.get", { ui_messages: [] });
  await tick();

  b.inbound.length = 0;
  // B's session.list answers late, once the user is already back on A.
  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: listReq.id,
    result: { sessions: [{ session_id: "from-B" }] },
  });
  await tick();

  const stray = b.inbound.find(
    (m) => m.type === "sessionList" && (m.sessions || []).some((s) => s.session_id === "from-B")
  );
  assert.equal(stray, undefined, "B's stale session list painted after switching back to A");
});

// ---- final fix round regression tests ----------------------------------

test("switching between two asking projects raises the incoming prompt before the round trip, and a reply resolves against it, not the outgoing one", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // A is asking (on screen) and B is also asking (in the background).
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: 10,
    method: "permission/request",
    params: { tool: "bash", command: "rm a" },
  });
  await tick();
  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 20,
    method: "permission/request",
    params: { tool: "bash", command: "rm b" },
  });
  await tick();
  assert.equal(
    b.inbound.filter((m) => m.type === "permissionRequest").length,
    1,
    "only A's prompt should be on screen so far"
  );

  // Switch to B. Its session.get is left unanswered on purpose, so the switch
  // is suspended mid-await — exactly the window the critical fix closes.
  b.inbound.length = 0;
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();

  // 1a: B's own prompt must already be on screen before session.get answers —
  // not A's stale one, and not nothing.
  const shown = b.inbound.filter((m) => m.type === "permissionRequest");
  assert.equal(
    shown.length,
    1,
    "the incoming project's prompt must be raised before the round trip, not after it"
  );
  assert.equal(shown[0].request.command, "rm b", "the overlay must show B's prompt, not A's stale one");

  // 1b: a reply right now — before session.get has resolved — must answer B
  // (the ask actually on screen), never A.
  dispatch(b, { type: "permissionReply", approved: true, always: true });
  await tick();

  const replyToB = b.sent.filter((m) => m.id === 20 && m.result !== undefined).pop();
  assert.ok(replyToB, "the reply never reached B, whose prompt was the one on screen");
  assert.match(replyToB.url, /project=B/, "the reply went out on the wrong project's socket");

  const replyToA = b.sent.find((m) => m.id === 10 && m.result !== undefined);
  assert.equal(
    replyToA,
    undefined,
    "A's request must not have been answered by a click meant for B — that would write an always-allow rule into the wrong project"
  );

  // Finish the switch so nothing is left dangling.
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
});

test("switching to an idle project sends turnComplete with ok: true, not read as a failed turn", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.inbound.length = 0;
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  const complete = b.inbound.filter((m) => m.type === "turnComplete").pop();
  assert.ok(complete, "switching to an idle project never sent turnComplete");
  assert.equal(
    complete.ok,
    true,
    "turnComplete without ok:true reads as a failed turn in the renderer's contract (ui/vscode/media/chat-src/07-events.js), which is wrong for an idle switch"
  );
});

test("a turn's turnComplete reports ok:false only when the turn actually failed", async () => {
  const b = await ready(loadBundle());

  // Failure case.
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", allowExec: false, files: [] });
  const failReq = b.sent.find((m) => m.method === "session.message");
  assert.ok(failReq, "no session.message went out");
  b.inbound.length = 0;
  b.deliver({ jsonrpc: "2.0", id: failReq.id, error: { message: "boom" } });
  await tick();
  let complete = b.inbound.filter((m) => m.type === "turnComplete").pop();
  assert.ok(complete, "no turnComplete after a failed turn");
  assert.equal(
    complete.ok,
    false,
    "a turn that threw must report ok:false, or the renderer never shows 'turn failed'"
  );

  // Success case, right after, to prove ok isn't just hardcoded either way.
  b.inbound.length = 0;
  dispatch(b, { type: "send", text: "again", mode: "build", profile: "", allowExec: false, files: [] });
  const okReq = b.sent.filter((m) => m.method === "session.message").pop();
  assert.ok(okReq, "no second session.message went out");
  b.deliver({ jsonrpc: "2.0", id: okReq.id, result: {} });
  await tick();
  complete = b.inbound.filter((m) => m.type === "turnComplete").pop();
  assert.ok(complete, "no turnComplete after a successful turn");
  assert.equal(complete.ok, true, "a turn that succeeded must not be reported as ok:false");
});

test("startSession's post-await RPCs stay on the project's own connection after an intervening switch", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.sent.length = 0;
  // A opens a different, restored session.
  dispatch(b, { type: "openSession", sessionId: "s-old" });
  await tick();

  const startReq = b.sent.find(
    (m) => m.method === "session.start" && String(m.url).includes("project=A")
  );
  assert.ok(startReq, "openSession never asked A to start the given session");

  // Switch to B while A's session.start is still in flight. This changes
  // which connection is "active" before A's startSession runs its next RPC —
  // exactly the gap the fix closes.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  // Answer A's session.start as restored, which drives startSession into its
  // second RPC (session.get) and then refreshSessionList's session.list. Both
  // must still target A, not whichever project became active meanwhile.
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: startReq.id,
    result: { session_id: "s-old", restored: true },
  });
  await tick();

  const getReq = b.sent.find(
    (m) => m.method === "session.get" && String(m.url).includes("project=A")
  );
  assert.ok(
    getReq,
    "startSession's session.get for the restored session must go out on A's own connection, not whichever project became active meanwhile"
  );

  b.deliverTo("A", { jsonrpc: "2.0", id: getReq.id, result: { ui_messages: [] } });
  await tick();

  const listReq = b.sent.find(
    (m) => m.method === "session.list" && String(m.url).includes("project=A")
  );
  assert.ok(
    listReq,
    "refreshSessionList after openSession must go out on A's own connection, not B's, even though B is active by then"
  );
});

test("an outstanding ask does not stick forever when its turn ends", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  const overlay = b.elementById("overlay");
  overlay.classList.add("hidden");

  b.inbound.length = 0;
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: 42,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  let railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.equal(railed.projects.find((p) => p.id === "A").status, "asking");
  assert.equal(overlay.classList.contains("hidden"), false, "A's own permission overlay was never raised");

  // The turn ends (e.g. the agent errored) while the prompt is still
  // unanswered — nobody ever replies to id 42.
  b.deliverTo("A", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "error", content: "boom" },
  });
  await tick();

  railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.equal(
    railed.projects.find((p) => p.id === "A").status,
    "idle",
    "the rail must not show a permanent 'asking' badge once the turn that asked has ended"
  );
  assert.equal(
    overlay.classList.contains("hidden"),
    true,
    "the overlay must come down with the stale ask — a cleared pendingAsk with the overlay still up is the leak this guards against"
  );

  // A late click on the now-stale overlay must be dropped, not answered.
  b.sent.length = 0;
  dispatch(b, { type: "permissionReply", approved: true, always: false });
  await tick();
  const stray = b.sent.find((m) => m.id === 42 && m.result !== undefined);
  assert.equal(
    stray,
    undefined,
    "a reply after the asking turn ended must be dropped, not answered against a dead request"
  );
});

// ---- C2b: the Trajectory view (renderer state, through the shared fragment) --

test("trajectory recorded:false says the session predates the log, in words, in place", async () => {
  const b = loadBundle({ search: "?project=A" });
  await tick();
  b.post({ type: "trajectory", recorded: false, events: [] });
  await tick();
  const summary = b.elementById("trajectory-summary");
  assert.ok(summary, "the fragment never looked up #trajectory-summary");
  assert.match(summary.textContent, /predates the log/);
});

test("trajectory replaces, trajectoryEvent appends, and the summary counts real rows", async () => {
  const b = loadBundle({ search: "?project=A" });
  await tick();
  b.post({
    type: "trajectory",
    recorded: true,
    events: [
      { seq: 1, time_ms: 1000, type: "agent/event", data: { type: "tool_call_start", step: 1, turn_id: "t1", tool_call_id: "c1", tool_call_name: "read" } },
      { seq: 2, time_ms: 1200, type: "agent/event", data: { type: "tool_call_completed", step: 1, turn_id: "t1", tool_call_id: "c1", content: "ok" } },
    ],
  });
  await tick();
  const summary = b.elementById("trajectory-summary");
  assert.match(summary.textContent, /1 turn · 3 rows/);

  b.post({ type: "trajectoryEvent", event: { type: "agent/event", data: { type: "tool_call_start", step: 2, turn_id: "t1", tool_call_id: "c2", tool_call_name: "edit" } } });
  await tick();
  // A second step and its tool: two more rows. Three rows are live — the new
  // step, the new tool, and the turn they landed in, which is ongoing again.
  assert.match(summary.textContent, /1 turn · 5 rows · 3 live/);

  b.post({ type: "trajectory", recorded: true, events: [] });
  await tick();
  assert.match(summary.textContent, /Nothing has happened/);

  b.post({ type: "trajectory", recorded: true, events: [], error: "boom" });
  await tick();
  assert.match(summary.textContent, /unavailable.*boom/);
});

test("the segmented control switches #app[data-view], by click and by arrow key", async () => {
  const b = loadBundle({ search: "?project=A" });
  await tick();
  const app = b.elementById("app");
  const chatBtn = b.elementById("view-chat-btn");
  const trajBtn = b.elementById("view-trajectory-btn");
  assert.ok(app && chatBtn && trajBtn, "the fragment must look up #app and both segment buttons");
  assert.equal(app.dataset.view, "chat");
  trajBtn.click();
  assert.equal(app.dataset.view, "trajectory");
  chatBtn.click();
  assert.equal(app.dataset.view, "chat");
  chatBtn.keydown("ArrowRight");
  assert.equal(app.dataset.view, "trajectory");
  trajBtn.keydown("ArrowLeft");
  assert.equal(app.dataset.view, "chat");
});

test("the header's tab strip is the open project's session list, and closing a tab hides it", async () => {
  const b = loadBundle({ search: "?project=A" });
  await tick();
  await handshakeFor(b, "A");
  answerOn(b, "A", "session.list", {
    sessions: [
      { id: "older", title: "Older", updated_at: "2026-01-01T00:00:00Z" },
      { id: "s-A", title: "On screen", updated_at: "2026-01-02T00:00:00Z" },
    ],
  });
  await tick();

  const tabs = b.inbound.filter((m) => m.type === "sessionTabs").pop();
  assert.ok(tabs, "the adapter never drove the header's tab strip");
  assert.equal(tabs.activeId, "s-A", "the open session is not the active tab");
  assert.deepEqual(
    tabs.tabs.map((t) => t.id),
    ["s-A", "older"],
    "tabs are not the session list, most recent first"
  );

  // Closing a tab that is not the one on screen only hides it: no session
  // switch, and the sidebar still knows about it.
  b.inbound.length = 0;
  dispatch(b, { type: "closeSession", sessionId: "older" });
  await tick();
  const after = b.inbound.filter((m) => m.type === "sessionTabs").pop();
  assert.ok(after, "closing a tab did not repaint the strip");
  assert.deepEqual(after.tabs.map((t) => t.id), ["s-A"]);
  assert.equal(after.activeId, "s-A", "closing another tab moved the session on screen");
});

test("clearMessages also clears the trajectory", async () => {
  const b = loadBundle({ search: "?project=A" });
  await tick();
  b.post({ type: "trajectory", recorded: true, events: [{ seq: 1, time_ms: 1, type: "agent/event", data: { type: "step_done", step: 1, turn_id: "t1", content: "final" } }] });
  await tick();
  assert.match(b.elementById("trajectory-summary").textContent, /1 turn/);
  b.post({ type: "clearMessages" });
  await tick();
  assert.match(b.elementById("trajectory-summary").textContent, /Loading trajectory/);
});

// ---- C2b: the web host feeds the Trajectory view ----------------------------

test("the first session of a freshly connected project fetches its trajectory, so the pane is not left loading", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }],
  }));
  await tick();
  await handshakeFor(b, "A");

  const req = b.sent.filter((m) => m.method === "session.trajectory").pop();
  assert.ok(req, "connecting did not ask the core for the first session's trajectory");
  assert.equal(req.params.session_id, "s-A", "the fetch must name the session onConnected just started");
  answerOn(b, "A", "session.trajectory", { recorded: true, events: [] });
  await tick();

  const msg = b.inbound.filter((m) => m.type === "trajectory").pop();
  assert.ok(msg, "no trajectory message reached the renderer");
  assert.equal(msg.recorded, true);
  assert.deepEqual(msg.events, []);
  assert.equal(msg.error, undefined);
});

test("a trajectory answer for a session the project has since left is dropped, even on the same project", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }],
  }));
  await tick();
  await handshakeFor(b, "A"); // leaves s-A's fetch in flight

  dispatch(b, { type: "newSession" });
  await tick();
  answerOn(b, "A", "session.start", { session_id: "s-A2", restored: false });
  await tick();

  const reqs = b.sent.filter((m) => m.method === "session.trajectory");
  const old = reqs.find((m) => m.params.session_id === "s-A");
  const fresh = reqs.find((m) => m.params.session_id === "s-A2");
  assert.ok(old && fresh, "both sessions' fetches must be in flight");

  // The newer answer lands first, the stale one last — an order the core is
  // free to produce, since it handles each request in its own goroutine.
  const freshEvents = [{ seq: 1, time_ms: 5, type: "agent/event", data: { type: "step_done", step: 1, turn_id: "t2", content: "" } }];
  b.deliverTo("A", { jsonrpc: "2.0", id: fresh.id, result: { recorded: true, events: freshEvents } });
  await tick();
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: old.id,
    result: { recorded: true, events: [{ seq: 9, time_ms: 1, type: "agent/event", data: { type: "step_done", step: 1, turn_id: "t1", content: "stale" } }] },
  });
  await tick();

  const painted = b.inbound.filter((m) => m.type === "trajectory");
  assert.ok(painted.length >= 1, "the fresh answer must have been painted");
  assert.deepEqual(painted[painted.length - 1].events, freshEvents, "the stale s-A answer must not repaint over s-A2");
});

test("switching to a project fetches its trajectory on its own connection and posts the fields intact", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();

  const req = b.sent.filter((m) => m.method === "session.trajectory").pop();
  assert.ok(req, "switching did not ask the core for the trajectory");
  assert.match(req.url, /project=B/);
  const events = [{ seq: 1, time_ms: 5, type: "agent/event", data: { type: "step_done", step: 1, turn_id: "t1", content: "final" } }];
  answerOn(b, "B", "session.trajectory", { recorded: true, events });
  await tick();

  const msg = b.inbound.filter((m) => m.type === "trajectory").pop();
  assert.ok(msg, "no trajectory message reached the renderer");
  assert.equal(msg.recorded, true);
  assert.deepEqual(msg.events, events);
  assert.equal(msg.error, undefined);
});

test("a live notification for the on-screen project is forwarded as trajectoryEvent with the method and params; a background project's is not", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");
  b.inbound.length = 0;

  const params = { type: "tool_call_start", step: 1, turn_id: "t1", tool_call_id: "c1", tool_call_name: "read", session_id: "s-A" };
  b.deliverTo("A", { jsonrpc: "2.0", method: "agent/event", params });
  b.deliverTo("A", { jsonrpc: "2.0", method: "exec/output_chunk", params: { step: 1, chunk: "x", turn_id: "t1" } });
  b.deliverTo("B", { jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", step: 1, content: "bg", turn_id: "t9" } });
  await tick();

  const fwd = b.inbound.filter((m) => m.type === "trajectoryEvent");
  assert.equal(fwd.length, 2, "exactly the two on-screen notifications, nothing from B");
  assert.deepEqual(fwd[0].event, { type: "agent/event", data: params });
  assert.equal(fwd[1].event.type, "exec/output_chunk");
  assert.equal(fwd[1].event.data.chunk, "x");
});

test("a live notification for a session the project has since left is not forwarded, even on the same project", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({ projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }] }));
  await tick();
  await handshakeFor(b, "A"); // session s-A
  dispatch(b, { type: "newSession" });
  await tick();
  answerOn(b, "A", "session.start", { session_id: "s-A2", restored: false });
  await tick();
  b.inbound.length = 0;

  // A live event for the OLD session, still streaming after the switch.
  b.deliverTo("A", { jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", step: 1, content: "stale", turn_id: "t1", session_id: "s-A" } });
  await tick();
  assert.equal(
    b.inbound.filter((m) => m.type === "trajectoryEvent").length,
    0,
    "a live event for the abandoned session must not reach the new session's pane"
  );

  // The NEW session's own live event still forwards normally.
  b.deliverTo("A", { jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", step: 1, content: "fresh", turn_id: "t2", session_id: "s-A2" } });
  await tick();
  const fwd = b.inbound.filter((m) => m.type === "trajectoryEvent");
  assert.equal(fwd.length, 1, "the new session's own live event must still be forwarded");
  assert.equal(fwd[0].event.data.content, "fresh");
});

test("when a turn ends on the on-screen project the trajectory is re-fetched, after turnComplete", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({ projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }] }));
  await tick();
  await handshakeFor(b, "A");
  b.sent.length = 0;

  // What 06-composer.js posts (see "a composer send becomes session.message").
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", apply: false, allowExec: true, files: [] });
  await tick();
  const turn = b.sent.find((m) => m.method === "session.message");
  assert.ok(turn, "no session.message was sent");
  b.deliverTo("A", { jsonrpc: "2.0", id: turn.id, result: {} });
  await tick();

  const done = b.inbound.findIndex((m) => m.type === "turnComplete");
  assert.ok(done >= 0, "turnComplete never posted");
  const req = b.sent.filter((m) => m.method === "session.trajectory").pop();
  assert.ok(req, "the turn ended and nobody re-read the log");
  assert.equal(req.params.session_id, "s-A", "handshakeFor starts session s-<projectId>");
  answerOn(b, "A", "session.trajectory", { recorded: true, events: [] });
  await tick();
  const after = b.inbound.slice(done + 1).find((m) => m.type === "trajectory");
  assert.ok(after, "the re-fetched trajectory must arrive after turnComplete, replacing the live rows");
});

test("switching into a project mid-turn replays its log: the reasoning that streamed in the background is fetched, not lost", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // B works in the background: two reasoning deltas that A's screen never saw.
  b.deliverTo("B", { jsonrpc: "2.0", method: "agent/event", params: { type: "reasoning_delta", step: 1, content: "thinking ", turn_id: "tb" } });
  b.deliverTo("B", { jsonrpc: "2.0", method: "agent/event", params: { type: "reasoning_delta", step: 1, content: "hard", turn_id: "tb" } });
  await tick();
  assert.equal(b.inbound.filter((m) => m.type === "trajectoryEvent").length, 0, "background events must not be forwarded live");

  b.inbound.length = 0;
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  // The core's log has what streamed while we were away.
  const recorded = [
    { seq: 1, time_ms: 100, type: "agent/event", data: { type: "reasoning_delta", step: 1, content: "thinking ", turn_id: "tb" } },
    { seq: 2, time_ms: 140, type: "agent/event", data: { type: "reasoning_delta", step: 1, content: "hard", turn_id: "tb" } },
  ];
  answerOn(b, "B", "session.trajectory", { recorded: true, events: recorded });
  await tick();
  await tick();

  const msg = b.inbound.filter((m) => m.type === "trajectory").pop();
  assert.ok(msg, "switching mid-turn must replay the log");
  assert.deepEqual(msg.events, recorded);
  // And the renderer drew it: one turn, one step, one coalesced reasoning row.
  assert.match(b.elementById("trajectory-summary").textContent, /1 turn · 3 rows/);
});

test("a session.trajectory that resolves after switching away does not paint the abandoned project", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  const pendingB = b.sent.filter((m) => m.method === "session.trajectory").pop();
  assert.ok(pendingB);

  // Switch back to A before B's trajectory arrives.
  dispatch(b, { type: "switchProject", projectId: "A" });
  await tick();
  b.inbound.length = 0;
  b.deliverTo("B", { jsonrpc: "2.0", id: pendingB.id, result: { recorded: true, events: [{ seq: 1, time_ms: 1, type: "agent/event", data: { type: "step_done", step: 1, turn_id: "tb", content: "final" } }] } });
  await tick();

  assert.equal(b.inbound.filter((m) => m.type === "trajectory").length, 0, "B's late trajectory must not be painted over A");
});

test("starting a new session in the on-screen project fetches that session's trajectory, after the view was cleared", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }],
  }));
  await tick();
  await handshakeFor(b, "A");
  const before = b.sent.filter((m) => m.method === "session.trajectory").length;

  dispatch(b, { type: "newSession" });
  await tick();
  answerOn(b, "A", "session.start", { session_id: "s-A2", restored: false });
  await tick();

  const reqs = b.sent.filter((m) => m.method === "session.trajectory");
  assert.equal(reqs.length, before + 1, "a new session did not ask the core for its trajectory");
  const newReq = reqs[reqs.length - 1];
  assert.equal(newReq.params.session_id, "s-A2", "the fetch must name the new session, not the old one");
  // Answered by id, not by answerOn: onConnected's own fetch for the original
  // s-A session (fix round 1) is still unanswered on this socket, and
  // answerOn would resolve that older request first.
  b.deliverTo("A", { jsonrpc: "2.0", id: newReq.id, result: { recorded: true, events: [] } });
  await tick();

  const types = b.inbound.map((m) => m.type);
  const cleared = types.lastIndexOf("clearMessages");
  const painted = types.lastIndexOf("trajectory");
  assert.ok(cleared >= 0 && painted > cleared, "the empty log must be painted after the clear, not before it");
  const msg = b.inbound[painted];
  assert.equal(msg.recorded, true);
  assert.deepEqual(msg.events, []);
  assert.equal(msg.error, undefined);
});

test("no open workspace means the start screen and no socket at all", async () => {
  // What --no-project looks like to the page: the API answers, and the answer
  // is empty. Opening a socket here would name a project that does not exist,
  // and the transcript behind it would be a chat with nothing.
  const b = loadBundle({ search: "" });
  b.setFetchResponder(() => ({ projects: [] }));
  await tick();
  await tick();

  assert.equal(b.socketURL, "", "no project is open, so no socket may be opened");

  const screen = b.elementById("start-screen");
  const app = b.elementById("app");
  assert.ok(screen, "the start screen element was never looked up");
  assert.equal(screen.hidden, false, "the start screen must be showing");
  assert.ok(app, "#app was never looked up");
  assert.equal(app.hidden, true, "the transcript must be hidden behind it");
  // The sidebar too: with nothing open it would list the same workspaces the
  // screen itself is offering, down the edge of the window, as a second and
  // worse copy of the choice being made.
  const rail = b.elementById("project-rail");
  assert.ok(rail, "the sidebar was never looked up");
  assert.equal(rail.hidden, true, "the sidebar must be hidden too");
});

test("opening a workspace from the sidebar takes the start screen down", async () => {
  // The start screen used to be switched off by hand, and only where the user
  // was expected to leave it — the two buttons on the screen itself. The
  // sidebar lists every remembered workspace and is on screen the whole time,
  // so clicking one there opened the project, selected it, listed its
  // sessions, and left the start screen sitting on top of the chat.
  let opened = false;
  const entry = (state) => ({
    id: "p-1",
    path: "/w1",
    name: "w1",
    state,
    sessions: 2,
  });

  const b = loadBundle({ search: "" });
  b.setFetchResponder(() => ({ projects: [entry(opened ? "ready" : "closed")] }));
  await tick();
  await tick();

  const screen = b.elementById("start-screen");
  const app = b.elementById("app");
  assert.equal(screen.hidden, false, "nothing is open yet, so the start screen shows");

  // What the rail's chip does with a workspace whose core is closed.
  b.setFetchResponder((url, init) => {
    if (url === "/api/projects" && init && init.method === "POST") {
      opened = true;
      return {};
    }
    return { projects: [entry(opened ? "ready" : "closed")] };
  });
  dispatch(b, { type: "switchProject", projectId: "p-1" });
  await tick();
  await tick();

  assert.ok(b.socketFor("p-1"), "the workspace's socket must be open");
  assert.equal(app.hidden, false, "the chat must be on screen at once");
  // The start screen fades out over the project rather than being switched
  // off under it, so it is still in the document for the length of the fade.
  await new Promise((r) => setTimeout(r, 400));
  assert.equal(screen.hidden, true, "the start screen must be gone");
  assert.equal(app.hidden, false, "the chat must be on screen");
  assert.equal(
    b.elementById("project-rail").hidden,
    false,
    "and the sidebar must be back with it",
  );
});

// ---- the settings panel -----------------------------------------------------
//
// 50-settings.js is the host half of the settings iframe: the frame posts
// "ready" or "reload", the host reads the core over the current project's
// socket and posts the state back. These tests stand in for the frame with a
// contentWindow that records what it was sent.

/** @returns {any[]} what the host posted into the frame */
function attachSettingsFrame(b) {
  const frame = b.elementById("settings-frame");
  assert.ok(frame, "50-settings.js never looked the settings frame up");
  const posted = [];
  frame.contentWindow = {
    postMessage(msg) {
      posted.push(JSON.parse(JSON.stringify(msg)));
    },
  };
  return posted;
}

/**
 * A model probe: runtime.list_providers asked to call every configured
 * provider's server for its models. The slow read — a provider on a host that
 * is down holds it for as long as the HTTP timeouts allow.
 */
const isModelProbe = (m) =>
  m.method === "runtime.list_providers" && m.id !== undefined && m.params && m.params.probe === true;

/** Minimal answers for the reads pushSettingsState makes. */
function settingsAnswer(method, model) {
  switch (method) {
    case "runtime.get_llm":
      return { provider: "lmstudio", api_base: "http://localhost:1234/v1", model };
    case "runtime.list_providers":
      return {
        providers: [{ key: "lmstudio", ready: true, active: true, current_model: model }],
        active_provider: "lmstudio",
        active_model: model,
      };
    default:
      return {};
  }
}

/** Answer every unanswered request on a project's socket except a model probe. */
function answerSettingsReads(b, projectId, model) {
  if (!b.__answered) {
    b.__answered = new Set();
  }
  const on = (m) => String(m.url).includes(`project=${encodeURIComponent(projectId)}`);
  for (const req of b.sent.filter((m) => m.id !== undefined && on(m) && !isModelProbe(m))) {
    if (b.__answered.has(req.url + ":" + req.id)) {
      continue;
    }
    b.__answered.add(req.url + ":" + req.id);
    b.deliverTo(projectId, { jsonrpc: "2.0", id: req.id, result: settingsAnswer(req.method, model) });
  }
}

function probedCatalog(model) {
  return {
    providers: [{ key: "lmstudio", ready: true, active: true, models: [{ id: model }], model_count: 1 }],
    active_provider: "lmstudio",
    active_model: model,
  };
}

test("the settings panel names the workspace at once and gets its state without waiting on the model probe", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [{ id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 }],
  }));
  await tick();
  await handshakeFor(b, "A");
  const posted = attachSettingsFrame(b);

  // The frame has loaded and asks for its state.
  b.postFromFrame("settings-frame", { type: "ready" });
  await tick();

  // Nothing has been answered, and the panel already knows whose settings it
  // is showing: the name is in the project list, no round trip needed. It
  // used to go out only after every read had answered — so while a slow read
  // held the state back, the panel kept naming the project it was opened on
  // last, over this project's settings.
  const ws = posted.find((m) => m.type === "workspace");
  assert.ok(
    ws,
    "the workspace must be named before any RPC answers; posted: " + posted.map((m) => m.type).join(", ")
  );
  assert.equal(ws.name, "a");
  assert.equal(ws.path, "/a");

  // Answer every read but the model probe. The whole panel used to wait for
  // it — half a minute for a provider on a host that is down, showing
  // nothing new the entire time.
  answerSettingsReads(b, "A", "m-a");
  await tick();
  await tick();
  const state = posted.find((m) => m.type === "state");
  assert.ok(
    state,
    "the state must arrive without the probe; posted: " + posted.map((m) => m.type).join(", ")
  );
  assert.equal(state.workspaceRoot, "/a");
  assert.equal(state.llm.model, "m-a");

  // The probe is still asked for, and its answer lands as its own message —
  // the one the Refresh button's answer already arrives in.
  const probe = b.sent.find(isModelProbe);
  assert.ok(probe, "the model probe must still be asked for");
  b.deliverTo("A", { jsonrpc: "2.0", id: probe.id, result: probedCatalog("m-a") });
  await tick();
  const catalog = posted.find((m) => m.type === "providerCatalog");
  assert.ok(catalog, "the probed catalogue must reach the panel");
  assert.equal(catalog.catalog.providers[0].models.length, 1);
});

test("a model probe answered after switching projects does not paint the other project's panel", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");
  const posted = attachSettingsFrame(b);

  // Settings opened on A: its reads answer, its probe hangs.
  b.postFromFrame("settings-frame", { type: "ready" });
  await tick();
  answerSettingsReads(b, "A", "m-a");
  await tick();
  await tick();
  assert.ok(posted.find((m) => m.type === "state" && m.llm.model === "m-a"), "A's state must show");
  const probeA = b.sent.find((m) => isModelProbe(m) && String(m.url).includes("project=A"));
  assert.ok(probeA, "A's probe must have gone out");

  // Over to B; settings opened again there.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  await tick();
  posted.length = 0;
  b.postFromFrame("settings-frame", { type: "reload" });
  await tick();
  const ws = posted.find((m) => m.type === "workspace");
  assert.ok(ws && ws.name === "b", "the panel must name B at once");
  answerSettingsReads(b, "B", "m-b");
  await tick();
  await tick();
  assert.ok(posted.find((m) => m.type === "state" && m.llm.model === "m-b"), "B's state must show");

  // A's probe comes back now. It is about A's providers; B's panel must not
  // take it.
  posted.length = 0;
  b.deliverTo("A", { jsonrpc: "2.0", id: probeA.id, result: probedCatalog("m-a") });
  await tick();
  assert.equal(
    posted.filter((m) => m.type === "providerCatalog").length,
    0,
    "A's late probe must be dropped"
  );

  // B's own probe lands.
  const probeB = b.sent.find((m) => isModelProbe(m) && String(m.url).includes("project=B"));
  assert.ok(probeB, "B's probe must have gone out");
  b.deliverTo("B", { jsonrpc: "2.0", id: probeB.id, result: probedCatalog("m-b") });
  await tick();
  const catalog = posted.find((m) => m.type === "providerCatalog");
  assert.ok(catalog, "B's probe must reach the panel");
  assert.equal(catalog.catalog.activeModel, "m-b");
});

// ---- the composer's model pill and context gauge ----------------------------
//
// Both read renderer messages the editor's host has always sent and this one
// did not: header carries the model, contextInfo the window and the reply
// budget, stepUsage the prompt size of the last step (07-events.js).

const twoProjects = () => ({
  projects: [
    { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
    { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
  ],
});

test("connecting tells the composer the real model and its context window", async () => {
  const b = await handshake(loadBundle());
  // The handshake answered core.health, initialize and session.start; the
  // read that fills the pill and the gauge is still pending.
  answer(b, "runtime.get_llm", {
    provider: "lmstudio",
    model: "qwen/qwen3.6-27b",
    num_ctx: 20000,
    context_tokens: 32768,
    max_tokens: 8192,
  });
  await tick();
  const header = b.inbound.filter((m) => m.type === "header" && m.model).pop();
  assert.ok(
    header,
    "the header must carry the model; headers: " + JSON.stringify(b.inbound.filter((m) => m.type === "header"))
  );
  assert.equal(header.model, "qwen/qwen3.6-27b");
  assert.equal(header.provider, "lmstudio");
  const info = b.inbound.filter((m) => m.type === "contextInfo").pop();
  assert.ok(info, "contextInfo must be sent");
  // num_ctx is the window the request asks for; the catalogue's 32K is more
  // than that, so the prompt overflows at 20000 — the gauge measures against
  // the smaller of the two.
  assert.equal(info.info.contextLimit, 20000);
  assert.equal(info.info.maxResponseTokens, 8192);
  assert.equal(info.info.model, "qwen/qwen3.6-27b");
});

test("switching projects repaints the composer with that project's model and window", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(twoProjects);
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");
  answerOn(b, "A", "runtime.get_llm", { model: "m-a", provider: "lmstudio", num_ctx: 8000 });
  answerOn(b, "B", "runtime.get_llm", {
    model: "m-b",
    provider: "openrouter",
    context_tokens: 64000,
    max_tokens: 2048,
  });
  await tick();
  // B answered while it was in the background: nothing on screen changes.
  assert.equal(
    b.inbound.filter((m) => m.type === "header" && m.model === "m-b").length,
    0,
    "a background project's model must not reach the composer"
  );

  b.inbound.length = 0;
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  // From the answer already in hand — before B's socket has said anything.
  const header = b.inbound.find((m) => m.type === "header" && m.model);
  assert.ok(header, "the switch must name B's model at once");
  assert.equal(header.model, "m-b");
  assert.equal(header.provider, "openrouter");
  const info = b.inbound.find((m) => m.type === "contextInfo");
  assert.ok(info, "the switch must carry B's window");
  assert.equal(info.info.contextLimit, 64000);
  assert.equal(info.info.maxResponseTokens, 2048);
  // And B is asked again, in case the model changed while it was in the
  // background.
  const reads = b.sent.filter(
    (m) => m.method === "runtime.get_llm" && String(m.url).includes("project=B")
  );
  assert.equal(reads.length, 2, "the switch must re-read B's model");
});

test("step usage and the agent's own estimate reach the context gauge; a worker's estimate does not", async () => {
  const b = await handshake(loadBundle());
  const event = (params) =>
    b.deliver({
      jsonrpc: "2.0",
      method: "agent/event",
      params: { session_id: "s-1", step: 1, ...params },
    });

  b.inbound.length = 0;
  event({
    type: "context_estimate",
    data: {
      prompt_tokens: 7000,
      source: "estimate",
      breakdown: [
        { key: "system", label: "System prompt", tokens: 900 },
        { key: "tools", label: "Tool definitions", tokens: 6000 },
        { key: "conversation", label: "Conversation", tokens: 100 },
        { key: "", label: "nameless", tokens: 5 },
      ],
    },
  });
  const est = b.inbound.find((m) => m.type === "stepUsage");
  assert.ok(est, "the estimate must reach the gauge");
  assert.equal(est.usage.prompt_tokens, 7000);
  assert.equal(est.usage.source, "estimate");
  assert.deepEqual(
    est.usage.breakdown.map((r) => r.key),
    ["system", "tools", "conversation"],
    "a breakdown row without a key is dropped"
  );
  assert.equal(est.scope, undefined);

  b.inbound.length = 0;
  event({
    type: "step_usage",
    data: { prompt_tokens: 7420, completion_tokens: 88, total_tokens: 7508, cost_usd: 0 },
  });
  const real = b.inbound.find((m) => m.type === "stepUsage");
  assert.ok(real, "the measurement must reach the gauge");
  assert.equal(real.usage.prompt_tokens, 7420);
  assert.equal(real.usage.completion_tokens, 88);
  assert.equal(real.usage.source, undefined);

  b.inbound.length = 0;
  event({ type: "context_estimate", scope: "child", data: { prompt_tokens: 3000, source: "estimate" } });
  assert.equal(
    b.inbound.filter((m) => m.type === "stepUsage").length,
    0,
    "a worker's estimate says nothing about the main window"
  );
  event({ type: "step_usage", data: { prompt_tokens: 0, completion_tokens: 0 } });
  assert.equal(
    b.inbound.filter((m) => m.type === "stepUsage").length,
    0,
    "a measurement of nothing must not empty the gauge"
  );

  event({
    type: "step_usage",
    scope: "child",
    data: { prompt_tokens: 3000, completion_tokens: 10, cost_usd: 0.01 },
  });
  const child = b.inbound.find((m) => m.type === "stepUsage");
  assert.ok(child, "a worker's measured usage still carries its cost");
  assert.equal(child.scope, "child");
  assert.equal(child.usage.cost_usd, 0.01);
});

test("reopening a session paints the gauge from the transcript's last measured prompt", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "openSession", sessionId: "s-old" });
  await tick();
  answer(b, "session.start", { session_id: "s-old", restored: true });
  await tick();
  answer(b, "session.get", {
    session_id: "s-old",
    ui_messages: [
      { role: "user", text: "hi" },
      { role: "assistant", text: "first", prompt_ctx: 3100, tokens_out: 40 },
      { role: "user", text: "more" },
      { role: "assistant", text: "second", prompt_ctx: 5120, tokens_out: 64 },
    ],
  });
  await tick();
  const history = b.inbound.findIndex((m) => m.type === "history");
  assert.ok(history >= 0, "the transcript must be painted");
  const usage = b.inbound.find((m, i) => i > history && m.type === "stepUsage");
  assert.ok(usage, "the last measured prompt must follow the transcript");
  assert.equal(usage.usage.prompt_tokens, 5120);
  assert.equal(usage.usage.completion_tokens, 64);
  assert.equal(usage.usage.source, "restored");
});

test("picking a model from the pill re-reads the model and its window from the core", async () => {
  const b = await handshake(loadBundle());
  answer(b, "runtime.get_llm", { model: "m-1", provider: "lmstudio", num_ctx: 8000 });
  await tick();
  b.inbound.length = 0;
  dispatch(b, { type: "setModel", model: "m-2", provider: "lmstudio" });
  await tick();
  answer(b, "runtime.set_model", { model: "m-2", provider: "lmstudio", persisted: true });
  await tick();
  const reads = b.sent.filter((m) => m.method === "runtime.get_llm");
  assert.equal(reads.length, 2, "the change must be read back from the core");
  b.deliver({
    jsonrpc: "2.0",
    id: reads[1].id,
    result: { model: "m-2", provider: "lmstudio", num_ctx: 32000, max_tokens: 4096 },
  });
  await tick();
  const header = b.inbound.filter((m) => m.type === "header" && m.model).pop();
  assert.ok(header, "the pill must be told the new model");
  assert.equal(header.model, "m-2");
  const info = b.inbound.filter((m) => m.type === "contextInfo").pop();
  assert.ok(info, "the gauge must be told the new window");
  assert.equal(info.info.contextLimit, 32000);
});

test("the composer's header keeps the session's title", async () => {
  const b = await handshake(loadBundle());
  // The strip knows the session by name...
  answer(b, "session.list", {
    sessions: [{ id: "s-1", title: "Renaming the gauge", updated_at: "2026-09-11T10:00:00Z" }],
  });
  await tick();
  b.inbound.length = 0;
  // ...and the header that brings the model must not take the name away:
  // the renderer renames the active tab from every header, and one without
  // a title says "New chat".
  answer(b, "runtime.get_llm", { model: "m-1", provider: "lmstudio", num_ctx: 8000 });
  await tick();
  const header = b.inbound.find((m) => m.type === "header" && m.model === "m-1");
  assert.ok(header, "the header must carry the model");
  assert.equal(header.title, "Renaming the gauge");
});

// ---- settings opened before the core is up ----------------------------------

test("settings opened while the workspace is still coming up fill in once it is live", async () => {
  let opened = false;
  const entry = (state) => ({ id: "p-1", path: "/w1", name: "w1", state, sessions: 0 });
  const b = loadBundle({ search: "" });
  b.setFetchResponder(() => ({ projects: [entry(opened ? "ready" : "closed")] }));
  await tick();
  await tick();
  b.setFetchResponder((url, init) => {
    if (url === "/api/projects" && init && init.method === "POST") {
      opened = true;
      return {};
    }
    return { projects: [entry(opened ? "ready" : "closed")] };
  });
  dispatch(b, { type: "switchProject", projectId: "p-1" });
  await tick();
  await tick();
  assert.ok(b.socketFor("p-1"), "the workspace's socket is being dialled");

  // The gear is reachable now, before the core has said a word. The markup
  // ships the dialog with the hidden attribute; the stub has to be told.
  b.elementById("rail-settings-modal").hidden = true;
  b.elementById("rail-settings-btn").click();
  const posted = attachSettingsFrame(b);
  b.postFromFrame("settings-frame", { type: "ready" });
  await tick();
  await tick();
  const note = posted.find((m) => m.type === "error");
  assert.ok(
    note && /still opening/.test(note.message),
    "the panel must say the workspace is opening; posted: " + JSON.stringify(posted)
  );
  assert.ok(posted.find((m) => m.type === "workspace" && m.name === "w1"), "and still name it");

  // Then the core comes up — and the panel is filled without being reopened.
  posted.length = 0;
  await handshakeFor(b, "p-1");
  await tick();
  assert.ok(
    posted.find((m) => m.type === "workspace" && m.name === "w1"),
    "the panel must be pushed again once the workspace is live; posted: " + posted.map((m) => m.type).join(", ")
  );
  answerSettingsReads(b, "p-1", "m-1");
  await tick();
  await tick();
  assert.ok(posted.find((m) => m.type === "state"), "and get its state");
});

// ---- attachments --------------------------------------------------------------

test("the paperclip on the desktop attaches workspace files by path and explains the rest", async () => {
  const b = await handshake(loadBundle());
  b.setTauri({ dialog: { open: async () => ["/w/src/a.go", "/elsewhere/notes.txt"] } });
  b.inbound.length = 0;
  dispatch(b, { type: "attach" });
  await tick();
  await tick();
  const picked = b.inbound.find((m) => m.type === "filesPicked");
  assert.ok(picked, "files inside the workspace must reach the composer");
  assert.deepEqual(picked.files, [{ name: "a.go", path: "/w/src/a.go", ext: "go", kind: "file" }]);
  const note = b.inbound.find((m) => m.type === "systemNote" && /outside the workspace/.test(m.text));
  assert.ok(note && note.text.includes("notes.txt"), "a file outside the workspace is named, not silently dropped");
});

test("dropped or pasted bytes are stored by the core and come back as a chip", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "attachBytes", name: "shot.png", mime: "image/png", dataBase64: "iVBORw0KGgo=" });
  await tick();
  const req = answer(b, "attachments.store", {
    name: "shot.png",
    path: "/w/.orchestra/attachments/1-shot.png",
    rel: ".orchestra/attachments/1-shot.png",
    ext: "png",
    kind: "image",
    size: 8,
  });
  assert.equal(req.params.data_base64, "iVBORw0KGgo=");
  assert.equal(req.params.mime, "image/png");
  assert.equal(req.params.name, "shot.png");
  await tick();
  const picked = b.inbound.find((m) => m.type === "filesPicked");
  assert.ok(picked, "the stored file must come back as a chip");
  assert.equal(picked.files[0].path, "/w/.orchestra/attachments/1-shot.png");
  assert.equal(picked.files[0].kind, "image");
  assert.match(picked.files[0].previewUri, /^data:image\/png;base64,/);
});

test("a send with chips carries them to session.message as attachments", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, {
    type: "send",
    text: "look at this",
    mode: "build",
    profile: "",
    apply: false,
    allowExec: false,
    files: [
      { name: "a.go", path: "/w/src/a.go", ext: "go", kind: "file" },
      { name: "no-path.txt" },
    ],
  });
  await tick();
  const req = b.sent.find((m) => m.method === "session.message");
  assert.ok(req, "the turn must be sent");
  assert.deepEqual(req.params.attachments, [{ path: "/w/src/a.go", name: "a.go", kind: "file" }]);
  const echo = b.inbound.find((m) => m.type === "userEcho");
  assert.ok(echo && echo.files && echo.files.length === 1, "the echo shows the chip that was sent");
});

// ---- persisting the assistant turn -------------------------------------------

/** Stream one full assistant turn into a bundle whose turn is in flight. */
function streamTurn(b) {
  const ev = (params) => b.deliver({ jsonrpc: "2.0", method: "agent/event", params });
  ev({ type: "reasoning_delta", content: "pondering" });
  ev({ type: "tool_call_start", tool_call_id: "t1", tool_call_name: "read" });
  ev({ type: "tool_call_delta", tool_call_id: "t1", args_delta: '{"path":"a.go"}' });
  ev({ type: "tool_call_completed", tool_call_id: "t1", tool_call_name: "read", content: "package a" });
  ev({ type: "message_delta", content: "Hel" });
  ev({ type: "message_delta", content: "lo" });
  ev({ type: "step_usage", data: { prompt_tokens: 120, completion_tokens: 7 } });
}

test("the answer the model streamed is written back into the session", async () => {
  const b = await ready(loadBundle());
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", apply: false, allowExec: false });
  await tick();
  streamTurn(b);
  answer(b, "session.message", { steps: 1 });
  await tick();
  await tick();
  answer(b, "session.get", {
    session_id: "s-1",
    title: "A chat",
    model: "m1",
    ui_messages: [{ role: "user", text: "hi" }],
  });
  await tick();
  await tick();

  const sync = b.sent.find((m) => m.method === "session.ui_sync");
  assert.ok(sync, "the answer must be saved; sent: " + b.sent.map((m) => m.method).join(", "));
  assert.equal(sync.params.session_id, "s-1");
  assert.equal(sync.params.title, "A chat", "the write-back must not wipe the session's title");
  const ui = sync.params.ui_messages;
  assert.equal(ui.length, 2, "the user's message stays, the answer is appended: " + JSON.stringify(ui));
  assert.equal(ui[0].role, "user");
  const last = ui[1];
  assert.equal(last.role, "assistant");
  assert.equal(last.text, "Hello");
  assert.equal(last.reasoning, "pondering");
  assert.equal(last.prompt_ctx, 120);
  assert.equal(last.tokens_out, 7);
  assert.ok(Array.isArray(last.tool_blocks) && last.tool_blocks.length === 1, "the tools ran too");
  assert.equal(last.tool_blocks[0].name, "read");
  assert.equal(last.tool_blocks[0].args_raw, '{"path":"a.go"}');
  assert.equal(last.tool_blocks[0].result, "package a");
  assert.equal(last.tool_blocks[0].status, "completed");
});

test("a second turn appends rather than replacing the first answer", async () => {
  const b = await ready(loadBundle());
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", apply: false, allowExec: false });
  await tick();
  b.deliver({ jsonrpc: "2.0", method: "agent/event", params: { type: "message_delta", content: "one" } });
  answer(b, "session.message", {});
  await tick();
  await tick();
  answer(b, "session.get", {
    session_id: "s-1",
    title: "A chat",
    ui_messages: [{ role: "user", text: "hi" }, { role: "assistant", text: "older" }],
  });
  await tick();
  await tick();
  const sync = b.sent.find((m) => m.method === "session.ui_sync");
  assert.ok(sync, "the answer must be saved");
  const ui = sync.params.ui_messages;
  assert.equal(ui.length, 3, "an earlier answer must survive: " + JSON.stringify(ui));
  assert.equal(ui[1].text, "older");
  assert.equal(ui[2].text, "one");
});

test("a turn that produced nothing is not written back", async () => {
  const b = await ready(loadBundle());
  dispatch(b, { type: "send", text: "hi", mode: "build", profile: "", apply: false, allowExec: false });
  await tick();
  answer(b, "session.message", {});
  await tick();
  await tick();
  assert.ok(
    !b.sent.find((m) => m.method === "session.ui_sync"),
    "nothing was said, so nothing is written: " + b.sent.map((m) => m.method).join(", ")
  );
});

test("restored history reaches the renderer with its reasoning, tools and files", async () => {
  const b = await handshake(loadBundle());
  b.inbound.length = 0;
  b.sent.length = 0;
  dispatch(b, { type: "openSession", sessionId: "s-2" });
  await tick();
  answer(b, "session.start", { session_id: "s-2", restored: true });
  await tick();
  answer(b, "session.get", {
    session_id: "s-2",
    ui_messages: [
      { role: "user", text: "look", attachments: [{ name: "a.go", path: "/w/a.go", ext: "go", kind: "file" }] },
      {
        role: "assistant",
        text: "done",
        reasoning: "hmm",
        tool_blocks: [
          { id: "t1", name: "read", args_raw: "{}", status: "completed", result: "ok", duration_ms: 12 },
        ],
      },
    ],
  });
  await tick();
  const hist = b.inbound.find((m) => m.type === "history");
  assert.ok(hist, "the restored session must reach the renderer");
  assert.equal(hist.messages.length, 2);
  assert.equal(hist.messages[0].role, "user");
  assert.equal(hist.messages[0].uiIndex, 0, "a user message carries its index, or rewind has nothing to aim at");
  assert.deepEqual(hist.messages[0].files, [{ name: "a.go", path: "/w/a.go", ext: "go", kind: "file" }]);
  assert.equal(hist.messages[1].role, "assistant");
  assert.equal(hist.messages[1].text, "done");
  assert.equal(hist.messages[1].reasoning, "hmm");
  assert.ok(Array.isArray(hist.messages[1].toolBlocks), "the renderer reads toolBlocks, not tool_blocks");
  assert.equal(hist.messages[1].toolBlocks[0].argsRaw, "{}");
  assert.equal(hist.messages[1].toolBlocks[0].durationMs, 12);
});

test("a tool call reaches the renderer in the phases it draws", async () => {
  const b = await ready(loadBundle());
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_start", tool_call_id: "t1", tool_call_name: "read", step: 2 },
  });
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_delta", tool_call_id: "t1", args_delta: '{"path":"a.go"}' },
  });
  b.deliver({
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_completed", tool_call_id: "t1", tool_call_name: "read", content: "package a" },
  });
  const blocks = b.inbound.filter((m) => m.type === "toolBlock");
  assert.deepEqual(
    blocks.map((m) => m.phase),
    ["start", "update", "complete"],
    "the renderer draws a tool block by phase: " + JSON.stringify(blocks)
  );
  assert.equal(blocks[0].toolCallId, "t1");
  assert.equal(blocks[0].toolName, "read");
  assert.equal(blocks[1].argsDelta, '{"path":"a.go"}');
  assert.equal(blocks[2].content, "package a");
});

// ---- deleting a chat ----------------------------------------------------------

test("deleting a chat removes it from the core, not just from the strip", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "deleteSession", sessionId: "s-old" });
  await tick();
  const close = b.sent.find((m) => m.method === "session.close");
  assert.ok(close, "the chat must be deleted in the core; sent: " + b.sent.map((m) => m.method).join(", "));
  assert.equal(close.params.session_id, "s-old");
  b.deliver({ jsonrpc: "2.0", id: close.id, result: null });
  await tick();
  await tick();
  assert.ok(b.sent.find((m) => m.method === "session.list"), "the sidebar must be read again after a delete");
});

test("deleting the chat you are in opens a fresh one", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  dispatch(b, { type: "deleteSession", sessionId: "s-1" });
  await tick();
  const close = b.sent.find((m) => m.method === "session.close");
  assert.ok(close, "the open chat must be deleted too");
  b.deliver({ jsonrpc: "2.0", id: close.id, result: null });
  await tick();
  await tick();
  assert.ok(
    b.sent.find((m) => m.method === "session.start"),
    "deleting the chat on screen must leave a new one open; sent: " + b.sent.map((m) => m.method).join(", ")
  );
});

test("a chat that could not be deleted says so and stays", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "deleteSession", sessionId: "s-old" });
  await tick();
  const close = b.sent.find((m) => m.method === "session.close");
  b.deliver({ jsonrpc: "2.0", id: close.id, error: { message: "file is locked" } });
  await tick();
  await tick();
  const note = b.inbound.find((m) => m.type === "error" || m.type === "systemNote");
  assert.ok(note && /locked/.test(note.message || note.text || ""), "a failed delete must be reported");
});

// ---- commands -----------------------------------------------------------------

test("the project's own commands reach the palette", async () => {
  const b = await handshake(loadBundle());
  const req = b.sent.find((m) => m.method === "skill.list");
  assert.ok(req, "the host must ask the core what commands this project has");
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: { skills: [{ name: "review", description: "Review the diff" }, { name: "clear", description: "shadows a built-in" }] },
  });
  await tick();
  const msg = b.inbound.find((m) => m.type === "skillsList");
  assert.ok(msg, "the palette is filled by a skillsList message");
  assert.deepEqual(
    msg.skills,
    [{ name: "review", description: "Review the diff" }],
    "a name that shadows a built-in command is left out, as in the editor"
  );
});

test("/model opens the model menu instead of explaining where it is", async () => {
  const b = await handshake(loadBundle());
  b.inbound.length = 0;
  const pill = b.elementById("model-pill");
  assert.ok(pill, "the composer's model pill is in the page");
  let clicked = 0;
  pill.addEventListener("click", () => clicked++);
  dispatch(b, { type: "slashCommand", cmd: "/model" });
  await tick();
  assert.equal(clicked, 1, "the command must open the menu itself");
});

test("/sessions opens the list of chats instead of explaining where it is", async () => {
  const b = await handshake(loadBundle());
  b.inbound.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/sessions" });
  await tick();
  // On the web the list lives in the sidebar: the command unfolds it and
  // repaints it, rather than answering with a sentence about the tab strip.
  assert.ok(
    b.inbound.find((m) => m.type === "projectList"),
    "the sidebar must be repainted: " + b.inbound.map((m) => m.type).join(", ")
  );
  assert.ok(
    !b.inbound.find((m) => m.type === "systemNote"),
    "the command acts; it does not explain where the control is"
  );
});

test("a workspace command is invoked with the names the core reads", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/review-diff", arg: "HEAD~1" });
  await tick();
  const req = b.sent.find((m) => m.method === "skill.invoke");
  assert.ok(req, "the command must reach the core; sent: " + b.sent.map((m) => m.method).join(", "));
  // internal/core/skill.go reads name + arguments. Sending anything else is
  // "skill name is empty" — every command failing for a spelling.
  assert.equal(req.params.name, "review-diff");
  assert.equal(req.params.arguments, "HEAD~1");
});

test("rewind aims at the message you clicked, not at the first one", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  dispatch(b, { type: "rewindToMessage", uiIndex: 4 });
  await tick();
  const req = b.sent.find((m) => m.method === "session.rewind");
  assert.ok(req, "rewind must reach the core; sent: " + b.sent.map((m) => m.method).join(", "));
  // SessionRewindParams reads ui_message_index. The web host sent ui_index,
  // which decodes to 0 — so every rewind, from any message, truncated the
  // whole chat back to its first message.
  assert.equal(req.params.ui_message_index, 4, "the index the core reads must carry the clicked message");
  assert.equal(req.params.ui_index, undefined, "the old spelling must be gone, not sent alongside");
});

test("branching forks the chat and opens the branch", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "forkFromMessage", uiIndex: 2 });
  await tick();
  const req = b.sent.find((m) => m.method === "session.fork");
  assert.ok(req, "branching must reach the core; sent: " + b.sent.map((m) => m.method).join(", "));
  assert.equal(req.params.session_id, "s-1");
  assert.equal(req.params.ui_message_index, 2, "fork takes the same index rewind does");
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: { session_id: "s-2", parent_id: "s-1", ui_messages: 3, history_messages: 6 },
  });
  await tick();
  // The branch is a different session, so the host has to open it — a fork
  // that leaves you looking at the parent has done nothing visible.
  const open = b.sent.find((m) => m.method === "session.start" && m.params && m.params.session_id === "s-2");
  assert.ok(open, "the new branch must be opened; sent: " + b.sent.map((m) => m.method).join(", "));
});

test("branching at the very first message is not attempted", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  dispatch(b, { type: "forkFromMessage", uiIndex: 0 });
  await tick();
  // sessionfile.ForkSnapshot refuses index 0 — the branch would hold nothing.
  // Asking anyway would answer a plausible click with a raw core error.
  assert.ok(
    !b.sent.find((m) => m.method === "session.fork"),
    "the host must not ask for a branch the core will refuse"
  );
});

test("/search asks the core to search saved chats", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/search", arg: "circuit breaker" });
  await tick();
  const req = b.sent.find((m) => m.method === "session.search");
  assert.ok(req, "session.search must be called; sent: " + b.sent.map((m) => m.method).join(", "));
  assert.equal(req.params.query, "circuit breaker");
  assert.equal(req.params.insensitive, true, "typing lowercase must still find a capitalised line");
});

test("a search that finds the same chat twice offers it once", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/search", arg: "breaker" });
  await tick();
  const req = b.sent.find((m) => m.method === "session.search");
  assert.ok(req);
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: {
      hits: [
        { session_id: "s-9", title: "Guards", index: 2, role: "user", snippet: "the breaker trips" },
        { session_id: "s-9", title: "Guards", index: 7, role: "assistant", snippet: "breaker again" },
        { session_id: "s-8", title: "Other", index: 1, role: "user", snippet: "breaker elsewhere" },
      ],
    },
  });
  await tick();
  // The sidebar lists chats to open, so six matches in one chat are one row.
  assert.ok(
    b.inbound.find((m) => m.type === "projectList"),
    "the sidebar must be repainted with the results"
  );
});

test("/workflows lists what the workspace has instead of staying CLI-only", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/workflows" });
  await tick();
  const req = b.sent.find((m) => m.method === "workflow.list");
  assert.ok(req, "workflow.list must be called; sent: " + b.sent.map((m) => m.method).join(", "));
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: { workflows: [{ name: "review", description: "Review the diff", stages: ["read", "judge"] }] },
  });
  await tick();
  const note = b.inbound.find((m) => m.type === "systemNote" && /review/.test(m.text || ""));
  assert.ok(note, "the workflow must be named back to the user");
  assert.match(note.text, /\/workflow review/, "the note must show how to run it");
});

test("/workflow runs one and reports where it ended", async () => {
  const b = await handshake(loadBundle());
  b.sent.length = 0;
  b.inbound.length = 0;
  dispatch(b, { type: "slashCommand", cmd: "/workflow", arg: "review the auth package" });
  await tick();
  const req = b.sent.find((m) => m.method === "workflow.run");
  assert.ok(req, "workflow.run must be called; sent: " + b.sent.map((m) => m.method).join(", "));
  assert.equal(req.params.name, "review", "the first word is the workflow");
  assert.equal(req.params.arguments, "the auth package", "the rest is its argument");
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: { name: "review", stages: [{ stage_id: "read", attempt: 1, action: "done" }], duration_ms: 4000 },
  });
  await tick();
  assert.ok(
    b.inbound.find((m) => m.type === "systemNote" && /workflow:review/.test(m.text || "")),
    "the run's outcome must come back to the chat"
  );
});

test("the provider's balance reaches the cost popover", async () => {
  const b = await handshake(loadBundle());
  const req = b.sent.find((m) => m.method === "runtime.credits");
  assert.ok(req, "the web host must ask for the balance as the editor host does");
  b.deliver({
    jsonrpc: "2.0",
    id: req.id,
    result: { provider: "openrouter", supported: true, balance: 12.5 },
  });
  await tick();
  const msg = b.inbound.find((m) => m.type === "credits");
  assert.ok(msg, "the renderer draws the balance row from a credits message");
  assert.equal(msg.supported, true);
  assert.equal(msg.balance, 12.5);
});

test("a rejected project request reads as a sentence, not as a machine code", async () => {
  // The server answers with `error` — a code — and puts the cause in `detail`.
  // The page used to show the code: "could not list projects: not_initialized".
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({ __httpStatus: 503, error: "not_initialized" }));
  await tick();

  const note = b.inbound.find((m) => m.type === "systemNote" && /could not list projects/.test(m.text || ""));
  assert.ok(note, `no systemNote for the failed listing; got: ${b.inbound.map((m) => m.type).join(", ")}`);
  assert.ok(
    /still starting/.test(note.text),
    `the note must explain the failure, got: ${note.text}`
  );
  assert.ok(
    !/not_initialized/.test(note.text),
    `the raw code must not reach the user, got: ${note.text}`
  );
});

test("a rejected request falls back to the server's detail when the code is unknown", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    __httpStatus: 500,
    error: "something_new",
    detail: "the disk is full",
  }));
  await tick();

  const note = b.inbound.find((m) => m.type === "systemNote" && /could not list projects/.test(m.text || ""));
  assert.ok(note, "no systemNote for the failed listing");
  assert.ok(/the disk is full/.test(note.text), `detail must be preferred, got: ${note.text}`);
});

test("a background project's connection does not fetch its trajectory", async () => {
  // The source gate is `projectId === currentProjectId`, and it reads
  // correctly — but every fixture that opens a background project only looks
  // at its traffic after switching into it, so a spurious fetch would have
  // gone unnoticed. This is the negative assertion that was missing.
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();

  await handshakeFor(b, "A");
  await openBackground(b, "B");
  await tick();

  const strays = b.sent.filter(
    (m) => m.method === "session.trajectory" && String(m.url).includes("project=B")
  );
  assert.equal(
    strays.length,
    0,
    `a background project must not fetch its trajectory; got ${strays.length}`
  );
});
