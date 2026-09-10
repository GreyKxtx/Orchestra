# The Desktop Window — Design

**Status:** approved design, part C of four (A — multi-project registry, done;
B — desktop shell, done; D — packaging, installers, updater).

**Two deliverables, two plans.** C1 is the multi-project window: the project
rail, several live connections, lazy opening. C2 is the trajectory view: a
timeline of what the agent actually did, modelled on DeepSeek Harness. They
share this document because they share a window, but they are sequenced and
planned separately — C1 alone is already eight to ten tasks, and C2 carries a
protocol question C1 does not.

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
not change by a single byte in C1** — an acceptance condition, checked by
comparing the built bundle before and after, not an aspiration. C2 is the
deliberate exception and says so where it is described; the condition stated
here binds the multi-project work only.

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

*C1. The trajectory view has its own section below.*

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

## The trajectory view (C2)

The owner asked for the visual logic of the Trajectory tab in DeepSeek
Harness. The first thing to record is what that tab actually is, because it is
easy to mistake for a widget.

### What DeepSeek Harness does

**A session there is not an array of chat messages.** It is an append-only log
of typed events, and the model's message history is *derived* from that log
rather than stored beside it — `deriveMessages()` projects the log into the
messages the model sees, and replay is simply re-derivation from the same
events. That is the whole trick; the tab is a consequence of it.

The event vocabulary covers execution boundaries (`turn/start`, `turn/end`,
`step/start`, `step/end`), the message-producing "surface" events
(`user/message`, `system/message`, `assistant/message`, `tool/call`,
`tool/result`), request configuration (`request/header`, `request/context`),
and lifecycle markers (`assistant/attempt` for failed attempts,
`session/end-seed`). Every event carries a monotonic contiguous `seq`, a `time`
in epoch milliseconds, and a typed `data` payload. Surface events additionally
carry `surfaceOp` — `append`, or `replace` over a `seq` range — and
`sourceEventSeqs`, the earlier events this one derives from. That last field is
where "which component produced this, and why" comes from.

Resume, fork and replay are not three features. They are three ways of seeding
a new log from an old one's prefix, which is why a fork is exact rather than
approximate.

**The tab renders that log as a timeline:** turn, then step, then tool call,
then nested sub-tool call, each row carrying its token usage and its duration.
Clicking a row opens that request's input, output and timing. Alongside the
timeline the UI exposes token usage, context pressure and a context breakdown.

Descriptions of colour-coded lanes (tool calls in one colour, context
injections in another, thinking in its own lane) appear in secondary write-ups
rather than the project's own documentation, so they are taken here as a hint
about grouping, not as a specification to copy.

### What Orchestra has today

Two projections, stored separately. `.orchestra/sessions/<id>.json` (schema v4)
holds `history` — the LLM's memory — and `ui_messages` — the interface's
projection — side by side. Neither is derived from the other, and neither is
derived from a log, because there is no log.

Live, during a turn, the picture is much better. `agent/event` notifications
already carry `step`, `turn_id`, `tool_call_id`, `tool_call_name`,
`tool_call_index` and `args_delta`, with types for `message_delta`,
`tool_call_start`, `tool_call_delta`, `tool_call_completed`, `step_done`,
`pending_ops`, `recoverable_error`, `done` and `error`; `exec/output_chunk`
streams shell output; `workflow/stage_start` and `workflow/stage_done` mark
workflow stages. That is genuinely enough to draw a trajectory **while a turn
runs**.

What is missing is everything after it ends. Per-step timing is not recorded.
Per-step token usage is not recorded. The events themselves are not persisted —
when the turn finishes, only the two projections remain. And `session.fork`
copies those two projections up to a `ui_message_index` rather than seeding a
log, so a fork is a copy whose fidelity depends on the copy being right.

### What we build, and in what order

An earlier draft of this section proposed a client-only first version that
recorded whatever events the page happened to see, with no protocol change. The
owner chose the larger scope instead: **the log is persisted by the core, and
it is built first.** This section records that decision and what follows from
it.

The reason the client-only version was rejected is worth keeping, because it is
the same reason the log has to come first. A timeline drawn from what one page
happened to witness cannot show a session from last week, cannot show the part
of a turn that ran while the page was closed, and — the symptom that actually
bit — cannot show the reasoning that streamed while the user was looking at a
different project. C1 already carries a background project's events over its
own socket and deliberately discards them, because the transcript repaints from
the core rather than from a client buffer. A client-side timeline would have had
to reintroduce exactly that buffer, and then be thrown away when the core
gained a log. So: the core gains the log, and the view is built once against
the real thing.

**Step one: the core records the log.** An append-only, per-session event log,
carrying a monotonic contiguous sequence number, an epoch-millisecond
timestamp, a type, a typed payload, and a `source` field. Step boundaries are
recorded where the work happens. Durations are measured at the source rather
than at the observer. Per-step token usage comes from the provider's response.
This is a session schema bump **and** a `ProtocolVersion` bump, because both
the on-disk format and the wire contract change.

**Step two: the view reads it.** The tab is a pure function from a list of
events to a tree of rows. Because the log is authoritative and durable, the tab
works for any session at any time, and switching projects mid-turn replays the
log rather than showing a gap.

**What is deliberately NOT in this scope, and why.** In DeepSeek Harness the
model's message history is *derived* from the log — `deriveMessages()` — and
nothing is stored beside it. That is the better end state, and this spec still
points at it. But Orchestra stores two projections today, `history` and
`ui_messages`, and `session.fork`, `session.rewind` and `session.search` are
existing, tested features that read them. Making the log authoritative for
those as well is a rewrite of the session subsystem, and bundling it with the
trajectory would put a working feature at risk to gain nothing the trajectory
needs.

So the log is added as a third structure, authoritative for the trajectory and
for nothing else yet. The projections keep working exactly as they do now.
Turning them into derivations of the log is named here as the block that
follows, and the log's shape is designed so that it can be — which is what the
next section is about.

### One event shape, defined once

The log has one shape, defined once, and it is designed for the two things
that come after it rather than only for the trajectory that reads it first.

For **plugin attribution**: every event carries a `source` field naming what
produced it. Nothing in this scope populates it with anything interesting, and
that is the point — see below.

For **derived projections**: surface-producing events carry enough to be
projected into messages later without a migration. That means recording the
events that *produce* a message — a user message, an assistant message, a tool
call, a tool result — as first-class events with their own sequence numbers,
not merely as side effects of a step. A log that records only timing and step
boundaries would draw a fine timeline and would have to be redesigned the day
anyone tried to derive `history` from it.

This is the one place where paying for a later block is worth it now: the field
names and the event vocabulary are almost free to get right today and expensive
to change once sessions on disk carry them.

### Where this meets extensibility

DeepSeek's source attribution is only meaningful because everything there is a
plugin: models, tools, skills, sessions, sandboxes, storage, the agent loop and
the UI are all mounted through a kernel, so "which plugin emitted this" is a
real question with a real answer. Orchestra has no plugin layer, so today there
is nothing to attribute.

The owner has flagged extensibility — modules, plugins, user-assembled tools —
as a later architectural conversation, and that sequencing is right: a plugin
system designed before the window's logic exists would be designed against
guesses. What this spec does about it is cheap and load-bearing: the trajectory
event carries `source` from its first version, so that introducing a plugin
layer later fills a field in rather than migrating a format.

### Where it lives, and a rule that must not be confused

The rail is desktop-only, and C1 is bound by the acceptance condition above:
the VS Code extension's bundle must not change.

The trajectory is different. A VS Code user benefits from it identically —
nothing about it is multi-project — so it belongs in the **shared** renderer
and **will** change the extension's bundle, deliberately and as a benefit.

These two rules contradict each other if quoted loosely, so state them
together: the no-change condition binds C1 only. C2 changes the extension on
purpose, and its own acceptance condition is the opposite one — the trajectory
must work in the VS Code webview too.

### Layout

A segmented control in the existing header switches the message area between
**Chat** and **Trajectory** for the active session. It is a view of one
session, so it sits where a session's content sits, and the rail keeps
selecting the project.

Rows nest by indentation: turn, step, tool call, sub-tool call. Each row shows
a time offset from the turn's start, a glyph for its kind, its name, and its
duration; token columns appear when a number is actually known. Clicking a row
expands its input and output in place.

Kind is carried by glyph and indentation first and colour second, so the view
survives both themes and a reader who cannot separate the hues.

## Changes to the core

C1's changes are small and confined to the part A surface, which nothing
outside this repository consumes. C2's are not small: they change the on-disk
session format and the wire contract. Both are listed here.

**C1.**

- `GET /api/projects` returns remembered-but-closed projects alongside open
  ones, each with a state field. Today it returns only what is open, so the
  rail cannot draw a list.
- Startup no longer restores every remembered project. The server opens the
  workspace it was given; the rest open on demand.

Per-project state — working, asking, quiet — is derived by the interface from
the event stream on each project's socket. The core gains no status endpoint
and no polling.

**C2.**

- An append-only event log per session, persisted alongside the existing
  projections. Session schema **v4 → v5**.
- `ProtocolVersion` **15 → 16**: a method to read a session's log, and the
  event shape on the wire. `initialize` hard-fails on a version mismatch, so
  every client in this repository — the VS Code extension included — moves in
  lockstep with this bump. That is affordable because they all ship from here,
  and it is stated so that nobody discovers it during implementation.
- Sessions written under v4 have no log and must keep opening. Their trajectory
  says the log was not recorded, in words, in place. **Nothing synthesises a
  log from `ui_messages`** — a fabricated timeline is the same error as an
  estimated token count, and this spec refuses both.

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

**C1.** *(as built: the adapter suite ended at 29 tests, not 13.)*
- The web adapter's test suite (`ui/web/scripts/adapter-test.mjs`) covers the
  multi-project layer: routing events to the right project, switching,
  repainting a session from the core, and the state each icon derives from an
  event stream.
- A lesson from C1 worth carrying into C2's tests: several defects survived
  nine reviews because tests asserted that a renderer message was *posted*
  without inspecting its payload. When a new path sends an existing renderer
  message, the test asserts the fields, and the payload is diffed against
  `ui/vscode/src/protocol/events.ts`.
- "The VS Code bundle did not change" is checked by building it before and
  after and comparing.
- By hand, recorded in the plan's task report: two projects open, an agent
  working in the background one, a notification arriving, and the click landing
  in the right project.

**C2 — the log.**
- Round-trip: a recorded turn is written, read back, and yields the same
  events in the same order with contiguous sequence numbers. A gap or a
  repeat in the sequence is a failure, not a warning.
- Append-only is enforced, not merely intended: a test attempts to rewrite an
  earlier event and asserts it is refused.
- Crash safety: a log truncated mid-write (a partial trailing record) must
  still load every complete event before it. The session must open.
- A session written under schema v4 opens, reports that no log was recorded,
  and **no test anywhere accepts a synthesised log** — a fixture asserts the
  trajectory is empty-with-a-reason rather than reconstructed.
- Timing is recorded at the source. A test asserts a step's duration comes
  from the core's own clock and not from when a client observed it.
- Token counts are present when the provider reported them and absent when it
  did not. Absent means absent: a test asserts no zero is written in place of
  an unknown.
- `session.fork`, `session.rewind` and `session.search` keep passing their
  existing tests unchanged. The log is additive, and that is what proves it.

**C2 — the view.**
- The timeline is a pure function from a list of events to a tree of rows, so
  it is tested as one: fixtures of event sequences in, an expected row tree
  out. Interleaved tool calls, a retry after `recoverable_error`, a turn cut
  short by cancellation, and a workflow's stages are each a fixture.
- Switching into a project mid-turn replays its log and shows the reasoning
  that streamed while it was in the background. This is the symptom that
  motivated the scope, so it gets an explicit test rather than being implied
  by the others.
- An event with an unknown `type` must render as a plain row rather than
  breaking the view — the log is meant to outlive the code that reads it.
- Rows with no known token count must render blank; a test asserts no number
  is invented.
- The trajectory works in the VS Code webview, which is C2's acceptance
  condition and the inverse of C1's.

## Non-goals

No file browser, no custom window frame, no reordering or grouping or colouring
projects, no search across all projects at once. Each is its own conversation.

For C2 specifically: **no plugin attribution** — the `source` field exists and
stays unpopulated until there is a plugin layer to name; and **no derived
projections** — `history` and `ui_messages` keep being stored as they are
today, and the log is authoritative for the trajectory only. Both are named in
the trajectory section, with the reasoning.

Also out: no synthesised log for sessions written before v5, and no estimated
token counts anywhere.

## Risks

- **Remote IPC on a random port** — the largest in C1, addressed above.
- **Size.** C1 is eight to ten tasks: a Go contract change, the web layer's
  move to several connections, the rail, notifications. Notifications are
  sequenced last so that if the work runs long they can be deferred without
  leaving anything half-built. C2 is planned separately for the same reason.
- **The renderer boundary can erode.** The pressure to "just add one field" to
  a shared fragment will be constant during C1. The bundle comparison is what
  makes that pressure visible instead of silent.
- **A session written before v5 has no log,** and an empty timeline reads as
  data loss rather than as a session recorded before the feature existed. The
  view says which it is, in words, in place, rather than showing an empty
  frame. This is the same risk the earlier client-only draft carried, and it
  does not disappear with a persisted log — it just applies to a smaller set of
  sessions that shrinks over time.
- **The protocol bump is lockstep.** `initialize` hard-fails on a mismatched
  `protocol_version`, so bumping to 16 breaks any client pinned to 15 until it
  is updated. Every client ships from this repository, so the cost is
  coordination inside one commit rather than a compatibility window — but it
  must be one commit, not a sequence.
- **The on-disk format is the expensive thing to get wrong.** Code that reads
  the log can be rewritten; sessions already written under a bad shape cannot.
  This is why the event vocabulary is designed for derived projections now,
  while it costs nothing, rather than when someone needs them.

## Sources

The trajectory section is derived from DeepSeek Harness's own documentation and
from hands-on write-ups; the data model is quoted from the first, the visual
description sanity-checked against the second.

- `deepseek-ai/deepseek-harness`, `docs/subsystems/session.md` — the session
  event log, event types, `deriveMessages()`, and seeding for resume/fork/replay.
- <https://deepseek.com/harness/en/> — the append-only session log as a stated
  rule, the Trajectory view's four operations, and the plugin architecture.
- <https://betterstack.com/community/guides/ai/deepseek-harness-plugin/> and
  <https://deepakness.com/blog/deepseek-harness/> — what the tab shows per row
  and what clicking one reveals.
