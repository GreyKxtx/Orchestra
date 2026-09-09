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
  let socket = null;

  class FakeWebSocket {
    static OPEN = 1;
    constructor(url) {
      this.url = url;
      this.readyState = 1;
      this.listeners = {};
      socket = this;
    }
    addEventListener(type, fn) {
      (this.listeners[type] ||= []).push(fn);
    }
    send(data) {
      sent.push(JSON.parse(data));
    }
    emit(type, ev) {
      for (const fn of this.listeners[type] || []) fn(ev);
    }
  }

  const handlers = [];
  const store = new Map();
  const el = () => ({
    addEventListener() {},
    removeEventListener() {},
    classList: { add() {}, remove() {}, contains: () => false, toggle() {} },
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
      getElementById: () => el(),
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

  vm.createContext(sandbox);
  vm.runInContext(src, sandbox, { filename: "web.bundle.js" });

  return {
    sent,
    inbound,
    open: () => socket.emit("open", {}),
    deliver: (obj) => socket.emit("message", { data: JSON.stringify(obj) }),
    post: (msg) => sandbox.window.postMessage(msg),
    close: () => socket.emit("close", {}),
    get socketURL() {
      return socket ? socket.url : "";
    },
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
