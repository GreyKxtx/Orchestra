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
    static OPEN = 1;
    constructor(url) {
      this.url = url;
      this.readyState = 1;
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
    addEventListener() {},
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
    click() {},
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
    location: { protocol: "http:", host: "127.0.0.1:9", search: opts.search ?? "" },
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
    inbound.push(msg);
    for (const fn of handlers.slice()) {
      try {
        fn({ data: msg });
      } catch (e) {
        // A renderer fragment tripping over the stub DOM must not mask what the
        // adapter did; the adapter's own listeners are registered first.
      }
    }
  };
  sandbox.globalThis = sandbox;

  const fetchCalls = [];
  let fetchResponder = () => ({ projects: [] });
  sandbox.fetch = async (url, init) => {
    fetchCalls.push({ url: String(url), init: init || {} });
    // A real fetch never resolves synchronously; this one must not either. A
    // caller loads the bundle — which fires 40-projects.js's startup fetch
    // immediately — and only then calls setFetchResponder(), still on the same
    // synchronous turn. Reading fetchResponder before yielding would freeze in
    // the default responder set above, no matter what the test configures.
    await Promise.resolve();
    const body = fetchResponder(String(url), init || {});
    return {
      ok: true,
      status: 200,
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
  assert.equal(blocks[0].block.status, "running");
  assert.equal(blocks[1].block.status, "done");
  assert.equal(blocks[1].block.result, "ok");
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
  assert.deepEqual(history.messages, [{ role: "user", text: "earlier" }]);
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
