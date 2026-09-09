# Multi-project registry (desktop app, part A)

Status: approved design, ready for implementation planning.
Plan reference: `docs/parity-plan-2026-09.md` §1.9 #3 (Desktop), part A of four.

## Problem

`orchestra web` serves exactly one project. It takes a workspace root, builds one
`core.Core`, and exposes one WebSocket (`internal/cli/web.go`). That is correct
for a browser tab pointed at a repository, and wrong for an application.

An application is expected to hold several projects at once, list every session
across them, and let the user move between them without restarting anything. The
VS Code extension never had to solve this: it takes `workspaceFolders[0]`
(`ui/vscode/src/coreSession.ts:1890`) and gets one core per window, because the
IDE supplies the project. A standalone app supplies its own.

This spec covers only the headless half — the registry, the process model and the
client-facing contract. The application shell (session sidebar, project picker,
settings screens) and the Tauri packaging are separate pieces, built on top of
this one.

## What the competitors settled on

Checked rather than assumed, September 2026:

- **Claude Code Desktop** (redesigned 2026-04-14): the sidebar lists every active
  and recent *session*; project is a filter and a grouping, not a mode. Sessions
  can be split across two panes. Each session in a git repository gets its own
  worktree.
- **Cursor 2.0**: up to eight agents in parallel, each on an isolated git
  worktree.
- **Windsurf** (Wave 13): parallel Cascade sessions.

Two conclusions. First, **the session is the primary object and the project is a
label on it** — so "switching projects" is not an application mode and needs no
teardown. Second, **nobody runs one engine across several workspaces**; they run
one unit of work per isolated copy. The rejected third option below is rejected
for the same reason they rejected it.

## Decisions

**One process, N cores, one core per project.** The server holds
`map[projectID]*core.Core`. Each core is an ordinary single-workspace core,
unchanged. This is *not* "one core spanning several workspaces" — that would mean
rewriting `Core`, where `workspaceRoot` is woven through the CKG, the LSP
manager, the MCP host and the sessions path.

Verified safe: `internal/core` declares **zero** package-level mutable variables
(neither `var x = …` nor `var ( … )` form), so independent instances share no
hidden state.

**One WebSocket per project.** `/ws?project=<id>`. Today's "one live connection"
rule becomes "one live connection *per project*", which is exactly the desired
behaviour and needs no protocol change. The rule exists because
`RPCHandler.SetRequester` binds a core's MCP host to one connection
(`internal/core/rpc_handler.go:46-52`); with a core per project that binding is
naturally per project.

**Project identity is `cache.ComputeProjectID(root)`** (`patch/cache/cache.go:104`),
the `sha256:…` value the core already uses in `initialize` and reports in
`core.health`. No second notion of identity is invented.

**Authentication is a cookie, with the token kept for tooling.** The server sets
`orchestra=<token>; HttpOnly; SameSite=Strict; Path=/` when it serves the page,
then redirects to `/` so the token leaves the address bar and the history. The
browser then authenticates `fetch` and `WebSocket` automatically, and the page
holds no credential in JavaScript. `Authorization: Bearer`, `X-Orchestra-Token`
and `?token=` continue to work for `curl`, scripts and tests.

Loopback is not a substitute for authentication: any page in the user's browser
can reach `127.0.0.1`, and this server can write files and run shell commands.
`SameSite=Strict` closes CSRF on `/api/*`; the WebSocket is additionally
same-origin only, which `coder/websocket`'s `Accept` enforces by default.

**No development-mode origin escape hatch.** A separate dev server on another
port would be a different origin, so the cookie would not be sent and the
handshake would fail on `Origin`. Rather than add a `--dev-origin` flag that
exists only in development and diverges from production, the development loop is
the production loop: bundle into `ui/web/static/` and serve through the Go
server. A file watcher makes that loop fast.

## Rejected

**One core holding several workspaces.** The largest change of the three, and no
competitor does it. `workspaceRoot` is a constructor argument threaded through
CKG storage, LSP provisioning, the MCP host and the session directory; making it
plural touches everything that currently works.

**A supervisor spawning one `orchestra core` subprocess per project.** This is
what the VS Code extension does, and it would work — but in-process cores need no
IPC, no process supervision, no orphan reaping and no second copy of the
transport. The extension spawns a subprocess because it is written in TypeScript
and cannot call `core.New`; the app server is Go and can.

**Worktree isolation (parity plan C6).** Not needed here. With one active turn per
project there is no second writer to isolate from. It becomes necessary only if
parallel turns *within* one project are wanted later, as in Claude Code.

## The contract

Every endpoint accepts the cookie, `Authorization: Bearer <token>`,
`X-Orchestra-Token: <token>` or `?token=<token>`.

```
GET /api/projects
→ 200 {"projects":[
    {"id":"sha256:cf1a…","path":"C:\\…\\Orchestra","name":"Orchestra",
     "state":"ready","error":"","opened_at":1788879969}
  ]}

POST /api/projects   {"path":"C:\\…\\myrepo"}
POST /api/projects   {"path":"C:\\…\\fresh","init":true}
→ 201 <project>
→ 409 {"error":"already_open","id":"sha256:…"}
→ 422 {"error":"not_initialized","path":"…"}
→ 404 {"error":"no_such_dir","path":"…"}
→ 500 {"error":"open_failed","path":"…","detail":"…"}

DELETE /api/projects/{id}
→ 204
→ 404 {"error":"project_not_open"}

GET /ws?project=<id>
→ JSON-RPC stream for that project (protocol v15, unchanged)
→ 404 project_not_open
→ 409 a connection for this project is already live

GET /health
→ server-level health, not a project's
```

`state` is `ready` or `error`; `error` carries the reason when it is `error`.
`name` is the base name of `path`. Per-project model and provider are not in this
payload — they come from that project's own `core.health` over its socket, which
already returns them.

**`/ws` without `project`** keeps working and means the project the server was
started in. This preserves everything already written and documented for the
single-project case.

## Persistence

Open projects are remembered across restarts in `~/.orchestra/projects.json`
(0600), holding paths only — no tokens. On startup each is reopened; one that
fails to open is listed with `state: "error"` rather than dropped silently, so
the user can see what happened to a project they had open. A registry that
forgets on restart is not a registry.

## `init` must leave cobra

`POST /api/projects {"init":true}` needs the logic behind `orchestra init`, which
today is `runInit(cmd *cobra.Command, args []string)` reading package-level flag
variables (`internal/cli/init.go:34`). A plain `func initProject(root string,
opts InitOptions) error` is extracted from it, and `runInit` becomes a thin
caller. Without this, the application cannot add a repository that has no
`.orchestra.yml` — which is most repositories the first time.

The extraction must not change `orchestra init`'s behaviour: its existing tests
pass unchanged afterwards, and if any needs editing that is a signal the
extraction changed behaviour and must be re-examined.

## Lifecycle and failure

- **Open** builds the core, starts the background CKG and LSP warmup the same way
  `runWeb` does today, and returns once the core is usable. Warmup continuing in
  the background is not an error state.
- **Close** closes the core (`c.Close()`), which releases its CKG database and
  language servers. A live WebSocket for that project is dropped, which the
  client sees as a normal disconnect.
- **A project that fails to open** does not affect the others and does not stop
  the server. This is the difference from today: `orchestra web` exits when
  `core.New` fails, which is right for one project and wrong for a registry.
- **Resource cost is real and not capped in v1**: each open project holds its own
  CKG SQLite database and its own language servers. No arbitrary limit is
  invented; closing a project actually frees them, and that is what the user
  controls.

## Seams

- **Registry** — `map[projectID]*core.Core` behind a small type with `Open`,
  `Close`, `Get`, `List`. The HTTP layer and the WebSocket layer both go through
  it and neither owns the map.
- **Transport** — unchanged. `/ws` gains a project lookup in front of the
  existing per-connection setup; everything below it is what shipped in v15.

## Testing

- **Two projects open at once**, each with its own WebSocket, each `initialize`
  reporting its own `project_id` — the test that proves cores do not bleed into
  each other.
- **The 409 is per project**: a second connection to project A is refused while a
  first connection to project B is live and unaffected.
- **Close frees the project**: after `DELETE`, `/ws?project=<id>` is 404 and
  opening the same path again succeeds.
- **Open failures are typed and non-fatal**: a directory with no `.orchestra.yml`
  gives 422 and the server keeps serving; the same path with `init:true`
  succeeds.
- **The cookie is what authenticates the browser**: a request carrying only the
  cookie is accepted; one carrying nothing is 401. Mutation-verified, since this
  is the check that stands between a stray web page and a shell.
- **Persistence round trip**: projects opened, server restarted, list restored;
  a path that has become invalid comes back as `state: "error"`, not missing.
- **`orchestra init`'s existing tests pass unchanged** after the extraction.

## Risks

- **Memory grows per open project.** Several cores mean several CKG databases and
  several language-server processes. Mitigated by close actually freeing them and
  by the count being the user's choice, not a background behaviour.
- **`/ws` gains a meaning it did not have**, one release after being documented as
  a stable transport. The default (`project` omitted) is preserved precisely so
  nothing already written becomes false.
- **The cookie redirect changes the first-load flow.** `orchestra web` currently
  prints a URL with a token in it and that URL is what people will have copied.
  It keeps working — the token is still accepted — but the address bar will no
  longer show it after the redirect.
