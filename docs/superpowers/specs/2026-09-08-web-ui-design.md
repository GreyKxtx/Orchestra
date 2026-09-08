# Web-UI over a WebSocket transport (§1.9 #4)

Status: approved design, ready for implementation planning.
Plan reference: `docs/parity-plan-2026-09.md` §1.9 #4.

## Problem

Orchestra has two client surfaces, both speaking JSON-RPC over stdio: the
Bubbletea TUI (`ui/tui`) and the VS Code extension (`ui/vscode`). There is no
way to drive Orchestra from a browser.

`core --http` exists but cannot carry a real client. Its surface is exactly
two endpoints — `/health` (GET) and `/rpc` (POST, request/response) —
`protocol/jsonrpc/http.go:49` and `:56`, 170 lines in total. There is no push
channel: no SSE, no WebSocket. Meanwhile every useful interaction depends on
server→client traffic:

- `agent/event` streaming notifications (`docs/PROTOCOL.md:748`, event types
  at `:767`)
- `permission/request` and `question/ask`, both server-initiated requests
  (`docs/PROTOCOL.md:815`)

A browser client on today's `--http` could send commands and would never
receive a stream, and could never be asked for permission. Any tool needing
approval would hang or fail closed. So this item is not "build a page"; it is
"give the core a bidirectional transport, then put a client on it".

`core --http` is additionally loopback-only (`protocol/jsonrpc/http.go:35-37`
rejects any bind address other than 127.0.0.1) and labelled debug-only, "not
part of the stable contract and may change" (`internal/cli/core.go:34-36`,
with a stderr NOTE at `:83`).

## What already exists, and makes this cheap

Three findings from reading the code changed the size of this task:

1. **The JSON-RPC server is transport-agnostic already.**
   `jsonrpc.NewServer(h Handler, in io.Reader, out io.Writer)`
   (`protocol/jsonrpc/server.go:67`) with `Notify` (`:239`) and `Request`
   (`:261`) already implemented. Adapt a WebSocket to a Reader/Writer pair and
   the entire bidirectional machinery — `agent/event`, `permission/request`,
   `question/ask` — works unchanged.

2. **No new RPC methods are needed.** The handler dispatches 48 methods
   (`internal/core/rpc_handler.go`), including everything this UI wants:
   `session.start`, `session.message`, `session.list`, `session.get`,
   `session.history`, `session.search`, `session.fork`, `session.cancel`,
   `session.close`, `agent.run`.

3. **The chat renderer is already portable.** `ui/vscode/media/chat-src/` is
   4737 lines of vanilla JS — markdown, tool blocks, reasoning traces, inline
   diffs, composer. Its entire coupling to VS Code is **three API calls**:
   `vscode.postMessage` (35 uses), `vscode.getState` (3), `vscode.setState`
   (2). Nothing else. A host object supplying those three methods is roughly
   fifteen lines.

The renderer is built by concatenating fragments (`ui/vscode/scripts/bundle-chat.mjs`),
and `ui/vscode/scripts/check-webview.mjs` already guards that the bundle
parses and has not drifted from its sources.

## Non-goals

- **Not network-reachable in v1.** Binding stays on 127.0.0.1. The design
  keeps authentication a seam so a network mode is a later flag rather than a
  rewrite, but shipping TLS, real auth and multi-user isolation is out of
  scope — that is the accounts feature, deliberately deferred.
- **No new RPC methods and no changes to `core --http`.** The existing `/rpc`
  debug endpoint stays exactly as it is, debug-only.
- **Not TUI parity.** v1 is chat with streaming, permissions, questions, and
  sessions. Model switching, memory views, the todo panel, the command
  palette, rewind and fork controls are later.
- **No plugin architecture.** See "Seams" below: this design records the
  seams it actually creates and commits to revisiting them once the second
  consumer exists. It does not invent a plugin system in advance.
- **No desktop shell.** Decided separately: Tauri is the intended shell
  *later*, over this same frontend. Its only requirement on v1 is that the
  frontend avoid anything available solely in Chromium, since Tauri uses the
  OS webview (WebView2 / WKWebView / WebKitGTK).

## Transport

A new `/ws` endpoint alongside the existing `/health` and `/rpc`. The
WebSocket connection is adapted to an `io.Reader`/`io.Writer` pair, and each
connection gets its own `jsonrpc.Server` over the shared `core`.

Dependency: `github.com/coder/websocket` in the `protocol` module. This is the
one real cost — `protocol/go.mod` currently declares a single direct
dependency (`gojsonschema`), and the module is deliberately thin. The
alternative, SSE for server→client plus POST for client→server, needs no
dependency and can reuse `jsonrpc.Server` through the same Reader/Writer trick.
It was rejected on **disconnect semantics**: with a WebSocket, a closed tab
drops the connection and pending `permission/request` calls fail closed for
free, matching stdio exactly (process dies, session ends). With SSE the stream
drop does not reach `Serve(ctx)` as EOF, so session lifetime, reconnection and
the fate of in-flight permission requests all become hand-written code — and
that is precisely where "the approval hung forever" bugs live.

**Session is the connection.** One WebSocket, one `jsonrpc.Server`, one client
session. Disconnect ends it.

Unlike `/rpc`, `/ws` is a **supported** transport: it gets a section in
`docs/PROTOCOL.md` and moves the protocol version to **v15** (currently 14,
`protocol/version.go:25`).

## The `orchestra web` command

`orchestra web` starts the core, serves the static frontend, exposes `/ws`,
and opens a browser. Authentication reuses the existing bearer token and the
discovery file `.orchestra/core.http.json` (0600) that `--http` already writes
(`internal/cli/core.go:115-146`), including its PID-liveness stale-file
cleanup (`:179-229`).

The token is passed to the page by the server that serves it, so the user
never copies it by hand.

Flags mirror the existing ones: `--port` (0 = auto), `--token` (auto-generated
if empty), `--no-open` to skip launching the browser.

## Frontend

New `ui/web/`. The renderer fragments that are host-independent — markdown,
diff tools, tool blocks, messages, reasoning traces (`03-markdown.js`,
`04-diff-tools.js`, `05a-subagents-turn.js`, `05d-tools.js`,
`05e-messages.js`) — move to a shared directory that **both** bundles consume.
Host-specific fragments (`06-composer.js`, `05b-overlays.js`,
`05c-busy-palette.js`) stay per host.

The seam between them is a `host` object with the three methods the renderer
actually uses:

```js
host.postMessage(msg)   // VS Code: acquireVsCodeApi().postMessage
                        // Web: send over the WebSocket
host.getState()         // VS Code: acquireVsCodeApi().getState
host.setState(s)        // Web: sessionStorage
```

Bundling reuses the existing concatenation approach; `check-webview.mjs` is
extended to check both bundles.

### Composer and UI decisions

Borrowed from a teardown of DeepSeek's chat composer (read as a third-party
analysis — chat.deepseek.com is an authenticated SPA and was not inspected
directly):

- **Mode chips live in the composer, not in a menu.** Orchestra's real modes —
  prompt family (build / plan / architecture), orchestra mode, reasoning
  effort — are currently reachable only through the command palette. The ones
  changed often belong where the user is typing.
- **Tooltips name the outcome, not the mechanism.** "Think before answering,
  for problems that need reasoning" rather than `reasoning.effort: high`.
- **Active state is visible before sending, not discovered after.** Which
  model and which mode are armed shows on the composer.
- **An attachment is a removable chip inside the composer, with its parsing
  state visible.** Orchestra already carries `attachments[]` on
  `session.message` (protocol v13).
- **A collapsible reasoning block above the reply** — already implemented
  (`.reasoning-trace` in `05a-subagents-turn.js`); listed here as
  confirmation, not work.

Explicitly **not** borrowed: their Instant/Expert tiering, which is product
packaging. The same teardown flags a real defect there — capabilities
disappear silently between tiers with no explanation — which is a thing to
avoid, not copy.

## Scope of v1

- Chat: send a message, watch it stream.
- `permission/request` and `question/ask` rendered as modals; without these
  the agent hangs on the first tool needing approval, so they are not
  optional.
- Sessions: list, open, resume, and search (`session.list` / `session.get` /
  `session.history` / `session.search`, all already present). Without this the
  page is useless after the first reload — and it is the closest thing in this
  work to the owner's stated goal of managing sessions from one place.
- Everything rendered inside a message — tool blocks, diffs, reasoning —
  arrives for free with the shared renderer.

## Error handling

- **Connection lost:** the page shows a disconnected state and offers
  reconnect. A reconnect starts a *new* session; the previous one is
  recoverable from the session list, not silently resumed, because silently
  resuming would misrepresent what the core actually kept.
- **Permission request in flight when the tab closes:** the connection drops,
  the pending request fails closed. This is the behaviour Orchestra already
  has for a dying stdio client and it needs no new code.
- **Core not running / token mismatch:** `orchestra web` owns the core it
  starts, so the failure surfaces as a startup error in the terminal, not as a
  blank page.

## Seams

This design creates one seam and makes one existing seam explicit:

- **Transport** (`stdio` | `ws`) — the `Handler` already sits behind
  `io.Reader`/`io.Writer`, so this seam exists in the code and is simply being
  used for the second time. It is named here so the third transport does not
  get special-cased.
- **Host** (VS Code | browser) — the three-method object above.

**Commitment:** once the web host is real, this section is rewritten against
what actually diverged. The three methods are what is visible from one
consumer; a second consumer is the first honest evidence of where the seams
belong. Designing a general plugin system before that evidence exists means
splitting where things do not vary and fusing where they do.

A separate plan item covers the wider modularity question raised by DeepSeek
Harness — a catalogue of Orchestra's real seams, and a test for the
"model-visible means logged" invariant (anything reaching a model request must
be reconstructable from the session event stream). That work is deliberately
*not* part of this spec.

## Testing

- **Real WebSocket round trip against a real core:** connect, `session.start`,
  receive `agent/event` stream, answer a `permission/request`, and assert the
  answer reaches the tool.
- **Disconnect mid-permission:** drop the connection while a
  `permission/request` is outstanding; the request must fail closed, not hang.
  This is the test that justifies choosing WebSocket over SSE, so it is
  mutation-verified: make the drop non-fatal and watch this test fail.
- **Two concurrent connections** get independent sessions over one core.
- **Auth:** a connection without the bearer token is rejected at the
  handshake, before any JSON-RPC frame is read.
- **Frontend:** `check-webview.mjs` extended to parse both bundles and to
  verify the shared fragments are byte-identical between them (that is what
  stops the two hosts drifting).

## Risks

- **Touching shipped VS Code code.** Extracting shared fragments moves files
  the working webview depends on, and `media/` has no test harness beyond
  `check-webview.mjs`. Mitigation: the extraction is a file move plus the host
  shim, with the byte-identity check above as the guard. The alternative —
  copying the fragments into `ui/web/` — guarantees divergence within months.
- **A dependency in `protocol/`.** One module that has deliberately stayed
  near-empty gains `coder/websocket`. Accepted for the disconnect semantics;
  the reasoning is recorded above so a future reader can reverse it knowingly.
- **`/ws` becomes a supported contract.** Once documented and versioned it
  cannot be changed as freely as `/rpc`. That is the point, but it is a real
  obligation.
