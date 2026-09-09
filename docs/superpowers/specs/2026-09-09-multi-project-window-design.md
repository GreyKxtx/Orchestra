# The Multi-Project Window — Design

**Status:** approved design, part C of four (A — multi-project registry, done;
B — desktop shell, done; D — packaging, installers, updater). The owner is
still extending the visual requirements; the "What the window shows" section
below holds what is settled and will grow before the implementation plan is
written. Nothing outside that section depends on those additions.

**Depends on:**
- `docs/superpowers/specs/2026-09-09-project-registry-design.md` — the
  registry, `/api/projects`, cookie auth, the per-project WebSocket.
- `docs/superpowers/specs/2026-09-09-desktop-shell-design.md` — the Tauri
  shell that owns the core process and the window.

## Problem

Part A built an engine that holds several projects in one process, each with
its own `core.Core` and its own `/ws?project=<id>` socket. Part B built a
window that starts that engine and shows its page. Nothing in the interface
reaches the engine: `/api/projects` is not mentioned anywhere under `ui/web/`.
The app opens exactly the page a browser tab opened, minus the address bar,
against exactly one project.

So the reason to open the desktop app instead of the VS Code extension does
not exist yet. That reason — decided by the owner — is working with several
projects at once: an agent runs in one project while you read another, and you
can see which project needs you without switching into it.

## Decisions

**The point is visible state, not a switcher.** A menu that changes the
current project is not worth an application. What is worth it is knowing,
without switching, that one project is working, another is waiting for an
answer, and a third is quiet. Every decision below serves that.

**The shared renderer stays single-project.** `ui/vscode/media/chat-src/` —
about 4700 lines — is compiled into both the VS Code webview and the web page
by two bundlers. It renders one chat, and it will keep rendering one chat.
Multi-project logic lives only in the web-specific layer (`ui/web/src/`, about
719 lines today) and the web-specific markup (`ui/web/index.src.html`), neither
of which is packaged into the `.vsix`. **The VS Code extension's bundle must
not change by a single byte in this part** — an acceptance condition, checked
by comparing the built bundle before and after, not an aspiration.

**Every open project holds its own socket; background sockets only listen.**
Part A's connection guard is per project, so one socket per project is allowed.
A background project's socket exists to learn its state — working, asking,
quiet — and nothing else. Scrollback is not accumulated in memory for projects
you are not looking at.

**Switching repaints from the core, not from a buffer.** `session.get` and
`session.ui_sync` exist for exactly this. Switching to a project asks the core
for that project's session and repaints; the renderer is handed one project's
state at a time, which is what keeps it single-project.

**The rail lists remembered projects; opening is lazy.** A project stays
visible whether or not the core holds it. Clicking a closed one opens it;
clicking an open one switches to it. Closing a project (the core releases its
memory) and removing it from the list are different actions with different
weights: closing is undone by one click, removing means finding the folder
again.

**This retires the startup restore.** Part B's review left a defect open: the
server reopens every remembered project during startup, inside the shell's
15-second announce budget, so one unreachable path makes the app fail to start
— identically every launch, with no in-app recovery. Lazy opening dissolves it.
`~/.orchestra/projects.json` stops meaning "reopen all of these at startup" and
becomes "the user's list of projects". The server opens only the workspace the
shell passes. An unreachable folder becomes a grey icon in the rail instead of
a refusal to start.

## What the window shows

*This section is the one the owner is still extending. What follows is settled.*

**A vertical rail down the left edge**, one icon per project, roughly 50px
wide so it costs no vertical space. The existing header — session tabs, new
chat, history, settings — is unchanged and continues to describe the active
project.

**Five states per icon**, distinguishable without colour alone:
- active (the project you are looking at)
- an agent is working
- waiting for your answer (a permission prompt or a question)
- open and idle
- in the list but closed

**Actions:** click a closed project to open it; click an open one to switch;
an add-folder control at the foot of the rail. A context menu offers "close
project" and "remove from list" as separate items.

**Adding a folder with no `.orchestra.yml`** must offer to initialize it. The
registry's `POST /api/projects` refuses an uninitialized directory with
`ErrNotInitialized`, and `orchestra web --init` already does this work for the
startup workspace; the add-project flow needs the same step, driven from the
interface.

## Changes to the core

Small, and confined to the part A surface, which nothing outside this
repository consumes.

- `GET /api/projects` returns remembered-but-closed projects alongside open
  ones, each with a state field. Today it returns only what is open, so the
  rail cannot draw a list.
- Startup no longer restores every remembered project. The server opens the
  workspace it was given; the rest open on demand.

Per-project state — working, asking, quiet — is derived by the interface from
the event stream on each project's socket. The core gains no status endpoint
and no polling.

## Shell privileges

Windows notifications and the native folder picker both require the page to
call the shell. The page is served by the Go core at `http://127.0.0.1:<port>`
— an external URL as far as Tauri is concerned — so it has no access to the
shell by default.

The grant is narrow: one capability with `remote.urls` scoped to the core's
own loopback origin, carrying exactly two permissions — show a notification,
and open a folder dialog. Part A's cookie and `Origin` defences are untouched.
`app.withGlobalTauri` is enabled so the plain-JS page can call the API without
a bundler.

**This is the design's largest risk, and it is verified first.** The core's
port is chosen at runtime, so the capability's URL pattern must match a
wildcard port, and whether Tauri v2 injects its IPC into an external URL under
these conditions can only be settled on a live window. The first task of the
implementation plan proves or disproves it in isolation, before anything is
built on top.

If it cannot be made to work, the fallback is `request_user_attention` from
Rust (a taskbar highlight, no page involvement) plus a path text field instead
of a dialog — and full notifications return to discussion rather than quietly
disappearing.

## Testing

- The web adapter's test suite (`ui/web/scripts/adapter-test.mjs`, 13 tests
  today) covers the multi-project layer: routing events to the right project,
  switching, repainting a session from the core, and the state each icon
  derives from an event stream.
- "The VS Code bundle did not change" is checked by building it before and
  after and comparing.
- By hand, recorded in the plan's task report: two projects open, an agent
  working in the background one, a notification arriving, and the click landing
  in the right project.

## Non-goals

No file browser, no custom window frame, no reordering or grouping or colouring
projects, no search across all projects at once. Each is its own conversation.

## Risks

- **Remote IPC on a random port** — the largest, addressed above.
- **Size.** Eight to ten tasks: a Go contract change, the web layer's move to
  several connections, the rail, notifications. Notifications are sequenced
  last so that if the work runs long they can be deferred without leaving
  anything half-built.
- **The renderer boundary can erode.** The pressure to "just add one field" to
  a shared fragment will be constant. The bundle comparison is what makes that
  pressure visible instead of silent.
