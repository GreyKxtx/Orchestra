# Trajectory View (C2b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Trajectory tab beside Chat that draws a session's recorded event log as a tree of rows — turn, step, tool call, sub-tool call — with offsets, durations and provider-reported tokens, live while a turn runs and complete for any session at any time, in the web UI and the VS Code webview alike.

**Architecture:** The view is one shared renderer fragment (`ui/vscode/media/chat-src/05f-trajectory.js`) that both bundles already compose from the same directory, so it is written once. Its heart is a pure function, `buildTrajectoryTree(events) → rows[]`, with no DOM, no fragment state and no clock, so it is tested on fixtures alone. Each host (the web adapter, the VS Code panel) feeds the renderer two messages: `trajectory` replaces the whole view from `session.trajectory`; `trajectoryEvent` appends one live notification. When a turn ends the host re-fetches the log, and the recorded rows — with core-stamped times — replace the live ones.

**Tech Stack:** Plain JS fragments in one IIFE (no build step beyond concatenation), TypeScript for the VS Code host, `node:test` for both test suites, existing `session.trajectory` JSON-RPC method (protocol 16, no protocol change here).

**Spec:** `docs/superpowers/specs/2026-09-09-multi-project-window-design.md` — sections "The trajectory view (C2)", "Layout", "Testing → C2 — the view", "Non-goals", "Risks". C2a (`docs/superpowers/plans/2026-09-10-trajectory-event-log.md`, merged) built the log this view reads; its follow-ups are in `docs/follow-ups/2026-09-10-trajectory-event-log.md`.

## Global Constraints

- **The view is a pure function from events to rows.** `buildTrajectoryTree` touches no DOM, reads no fragment state, and calls no clock. Everything it knows about time comes from the events' own core-stamped `time_ms`.
- **No estimated numbers.** Token columns are filled only from `step_usage` events (`data.prompt_tokens`, `data.completion_tokens`). `context_estimate` is excluded **by event type**, never by inspecting a `source` field, and produces no row. A test asserts an estimate leaves the token columns blank.
- **No client clock, anywhere.** A live event has no `time_ms`; its row shows a blank offset and a blank duration and is marked `live`. `Date.now()` does not appear in the fragment.
- **Nothing synthesises a log.** `recorded: false` renders the sentence "No trajectory was recorded for this session — it predates the log." in place. Nothing derives rows from `ui_messages` or the chat pane.
- **An unknown event `type`, or an unknown notification method, renders as a plain row** with the type as its label. The log outlives the code that reads it; the view must not break on a vocabulary it has never seen.
- **Kind is carried by glyph and indentation first, colour second.** The view must read in both themes and to a reader who cannot separate the hues.
- **One shared implementation.** The fragment lives in `ui/vscode/media/chat-src/` and is added to **both** bundle orders (`ui/vscode/scripts/bundle-chat.mjs` and `ui/web/scripts/bundle-web.mjs`). The VS Code extension's bundle **changes on purpose** — C2's acceptance condition is the inverse of C1's: the trajectory must work in the VS Code webview.
- **`trajectory` replaces; `trajectoryEvent` appends; turn end re-fetches.** This is the reconciliation rule. A live event that arrives before a `trajectory` response is replaced by it; one that arrives after is appended; the re-fetch at turn end makes the final state the recorded one regardless of interleaving.
- **Assert payload fields, not "a message was posted."** C1's lesson: several defects survived nine reviews because tests checked that a renderer message went out without reading its fields. Every new test that observes a renderer message asserts its fields.
- **Session snapshot schema stays at v4; `ProtocolVersion` stays at 16.** This plan adds no JSON-RPC method and no wire field. Only the renderer-internal message protocol (`ui/vscode/src/protocol/events.ts`) grows.
- `ui/vscode/out/` and `ui/vscode/node_modules` are untracked build output. Do not commit them. `npm run compile` in `ui/vscode` regenerates `out/` locally; that is fine and expected in this plan, because the extension is meant to change.
- Both bundles must be regenerated and committed with their sources: `npm run bundle:webview` (in `ui/vscode`) and `node ui/web/scripts/bundle-web.mjs` (repo root). `check:webview` and `check-web.mjs` fail on a stale bundle.
- CI runs, in this order and all must stay green: `npm run compile`, `npm run check:webview` (both in `ui/vscode`), `node ui/web/scripts/check-web.mjs`, `node ui/web/scripts/adapter-test.mjs`. There is no linter beyond these.
- `gofmt -l` and `git status` may show CRLF artifacts (`web.bundle.js` "modified" with an empty diff). Restore with `git checkout --`; never commit them.

---

## File structure

| File | Responsibility |
|---|---|
| `ui/vscode/media/chat-src/05f-trajectory.js` (create) | The whole view: `buildTrajectoryTree` (pure), `renderTrajectory` / `renderTrajRow` (DOM), the segmented control (`setView`, `bindViewSwitch`), and the three state mutators the dispatcher calls (`resetTrajectory`, `replaceTrajectory`, `appendTrajectoryEvent`). Looks up its own DOM ids rather than adding them to `01-dom-state.js`, so the feature is one file; `check-web.mjs` still enforces the ids exist in the page. |
| `ui/vscode/scripts/trajectory-test.mjs` (create) | `node:test` fixtures for `buildTrajectoryTree`, evaluated standalone the way `check-webview.mjs` evaluates `formatToolDuration`. |
| `ui/vscode/scripts/bundle-chat.mjs`, `ui/web/scripts/bundle-web.mjs` | Add `05f-trajectory.js` after `05e-messages.js` in both orders. |
| `ui/vscode/package.json` | `check:webview` also runs the trajectory tests, so CI runs them. |
| `ui/vscode/media/chat-src/07-events.js` | Two new cases (`trajectory`, `trajectoryEvent`); `clearMessages` also resets the trajectory. |
| `ui/vscode/media/chat.css` | The pane, the rows, the segmented control, and the `data-view` visibility rule. Shared: the web bundle copies this file verbatim. |
| `ui/vscode/media/chat-src/README.md` | One table row for the new fragment. |
| `ui/vscode/src/protocol/events.ts` | `TrajectoryEvent` type; two `HostToWebview` members. |
| `ui/web/index.src.html`, `ui/vscode/src/chat/panel.ts` (`getHtml`) | The segmented control after `#session-tabs`; the `#trajectory` pane after `#messages`; `data-view="chat"` on `#app`. Identical markup in both. |
| `ui/web/src/10-adapter-session.js` | `refreshTrajectory(projectId, conn, sessionId)`; called from `activateProject` after `session.get`, and from `sendTurn`'s `finally` for the on-screen project. Stale-response guard as `session.get` has. |
| `ui/web/src/20-adapter-events.js` | In `handleNotification` (already gated to the on-screen project), forward `agent/event`, `exec/output_chunk`, `workflow/stage_start`, `workflow/stage_done` as `trajectoryEvent` before the existing translation. |
| `ui/web/scripts/adapter-test.mjs` | Harness: stub elements record listeners and `click()` fires them (unblocks the segmented control and C1's parked keyboard item). Five new host tests plus two renderer-state tests. |
| `ui/vscode/src/coreSession.ts` | `sessionTrajectory(sessionId?)` wrapper mirroring `sessionGet`. |
| `ui/vscode/src/chat/panel.ts` | `refreshTrajectory(sessionId)`; called after the `history` post and in the turn's `finally` after `turnComplete`; `trajectoryEvent` forwarded from `forwardAgentEvent`, `onExecChunk`, `onWorkflow`. |

---

### Task 1: The pure function — events in, rows out

The view's whole logic, testable without a browser. Nothing in this task touches the DOM or either host.

**Files:**
- Create: `ui/vscode/media/chat-src/05f-trajectory.js` (this task writes only `buildTrajectoryTree` and its typedef; Task 2 appends the rendering)
- Create: `ui/vscode/scripts/trajectory-test.mjs`
- Modify: `ui/vscode/scripts/bundle-chat.mjs:11-23` (the `order` array)
- Modify: `ui/web/scripts/bundle-web.mjs:22-40` (the `order` array)
- Modify: `ui/vscode/package.json` (`check:webview` script)

**Interfaces:**
- Consumes: the shape `session.trajectory` returns and the tee records — `{ seq, time_ms, type, source, data }` where `type` is the notification method (`"agent/event"`, `"exec/output_chunk"`, `"workflow/stage_start"`, `"workflow/stage_done"`) and `data` its params. For `agent/event`, `data` carries `step`, `type` (the kind: `tool_call_start`, `step_usage`, …), `turn_id`, `content`, `data`, `tool_call_id`, `tool_call_name`, `args_delta`, `scope`, `parent_tool_call_id` — see `docs/PROTOCOL.md` "agent/event".
- Produces: `function buildTrajectoryTree(events) → TrajRow[]` and the `TrajRow` typedef below. Task 2 renders these rows; Tasks 3 and 4 never see them.

```js
/**
 * @typedef {{
 *   kind: "turn"|"step"|"tool"|"text"|"reasoning"|"error"|"stage"|"pending"|"route"|"other",
 *   depth: number,          // 0 turn, 1 step or stage or route, 2 items in a step, 3+ nested child tools
 *   key: string,            // stable, unique within one build; used as data-key
 *   label: string,
 *   turnId: string,
 *   step?: number,
 *   startMs?: number,       // first event's core time; undefined for live rows
 *   offsetMs?: number,      // startMs - the turn's startMs; undefined if either unknown
 *   durationMs?: number,    // last - first; undefined if either end unknown
 *   tokensIn?: number,      // ONLY from step_usage
 *   tokensOut?: number,     // ONLY from step_usage
 *   outcome?: string,       // step_done content, stage marker, tool "done", turn "open"|"done"|"final"|"error"
 *   input?: string,         // tool arguments, assembled from args_delta
 *   output?: string,        // tool result preview (+ shell output), or coalesced text
 *   live: boolean,          // true if any contributing event lacked time_ms
 *   seq?: number
 * }} TrajRow
 */
```

- [ ] **Step 1: Register the fragment in both bundle orders**

In `ui/vscode/scripts/bundle-chat.mjs`, after the line `"05e-messages.js",` add:

```js
  "05f-trajectory.js",
```

In `ui/web/scripts/bundle-web.mjs`, after the line `[sharedDir, "05e-messages.js"],` add:

```js
  [sharedDir, "05f-trajectory.js"],
```

Both bundlers read the fragment from the same directory; nothing is copied.

- [ ] **Step 2: Write the failing tests**

Create `ui/vscode/scripts/trajectory-test.mjs`:

```js
// Fixture tests for buildTrajectoryTree, the pure function behind the
// Trajectory view. The fragments are one IIFE with no exports, so the function
// is pulled out by source and evaluated on its own — the same technique
// check-webview.mjs uses for formatToolDuration, duplicated here (15 lines)
// rather than exported from a script that is not a module.
//
// Run: node --test scripts/trajectory-test.mjs   (from ui/vscode; CI runs it via check:webview)
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");

function extractFunction(fragment, name) {
  const src = fs.readFileSync(path.join(root, "media", "chat-src", fragment), "utf8");
  const start = src.indexOf(`function ${name}(`);
  if (start < 0) return null;
  let depth = 0;
  for (let i = src.indexOf("{", start); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return eval("(" + src.slice(start, i + 1) + ")");
    }
  }
  return null;
}

const buildTrajectoryTree = extractFunction("05f-trajectory.js", "buildTrajectoryTree");
assert.equal(typeof buildTrajectoryTree, "function", "buildTrajectoryTree must be a self-contained function in 05f-trajectory.js");

/** A recorded notification. time_ms/seq omitted => a live event. */
const ev = (type, data, time_ms, seq) => {
  const e = { type, data };
  if (time_ms !== undefined) e.time_ms = time_ms;
  if (seq !== undefined) e.seq = seq;
  return e;
};
/** An agent/event with kind and fields, in turn t1 unless overridden. */
const ae = (kind, fields, time_ms, seq) =>
  ev("agent/event", Object.assign({ type: kind, turn_id: "t1" }, fields), time_ms, seq);

const kinds = (rows) => rows.map((r) => r.kind);
const byKind = (rows, kind) => rows.filter((r) => r.kind === kind);

test("one turn, one step, one tool call: turn > step > tool with offsets, duration and tokens", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "read" }, 1000, 1),
    ae("tool_call_delta", { step: 1, tool_call_id: "c1", args_delta: '{"path":"a"}' }, 1010, 2),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", tool_call_name: "read", content: "ok" }, 1250, 3),
    ae("step_usage", { step: 1, data: { prompt_tokens: 120, completion_tokens: 30 } }, 1300, 4),
    ae("step_done", { step: 1, content: "tool_call" }, 1300, 5),
    ae("done", { step: 1 }, 1310, 6),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool"]);
  const [turn, step, tool] = rows;
  assert.equal(turn.depth, 0);
  assert.equal(turn.label, "turn 1");
  assert.equal(turn.offsetMs, 0);
  assert.equal(turn.durationMs, 310);
  assert.equal(turn.outcome, "done");
  assert.equal(turn.live, false);
  assert.equal(step.depth, 1);
  assert.equal(step.label, "step 1");
  assert.equal(step.step, 1);
  assert.equal(step.tokensIn, 120);
  assert.equal(step.tokensOut, 30);
  assert.equal(step.outcome, "tool_call");
  assert.equal(tool.depth, 2);
  assert.equal(tool.label, "read");
  assert.equal(tool.offsetMs, 0);
  assert.equal(tool.durationMs, 250);
  assert.equal(tool.input, '{"path":"a"}');
  assert.equal(tool.output, "ok");
  assert.equal(tool.outcome, "done");
  assert.equal(tool.seq, 1);
});

test("interleaved tool calls keep their own durations, keyed by tool_call_id", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "a" }, 0, 1),
    ae("tool_call_start", { step: 1, tool_call_id: "c2", tool_call_name: "b" }, 100, 2),
    ae("tool_call_completed", { step: 1, tool_call_id: "c2", content: "" }, 300, 3),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "" }, 500, 4),
  ]);
  const tools = byKind(rows, "tool");
  assert.deepEqual(tools.map((t) => t.label), ["a", "b"]);
  assert.equal(tools[0].durationMs, 500);
  assert.equal(tools[1].durationMs, 200);
  assert.equal(tools[1].offsetMs, 100);
});

test("a retry after recoverable_error is an error row in the first step, then a second step", () => {
  const rows = buildTrajectoryTree([
    ae("recoverable_error", { step: 1, content: "stale" }, 100, 1),
    ae("step_done", { step: 1, content: "final_retry" }, 110, 2),
    ae("tool_call_start", { step: 2, tool_call_id: "c1", tool_call_name: "edit" }, 200, 3),
    ae("tool_call_completed", { step: 2, tool_call_id: "c1", content: "" }, 250, 4),
    ae("step_done", { step: 2, content: "final" }, 260, 5),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "error", "step", "tool"]);
  assert.equal(rows[0].outcome, "final");
  assert.equal(rows[1].outcome, "final_retry");
  assert.equal(rows[2].label, "retry");
  assert.equal(rows[2].output, "stale");
  assert.equal(rows[2].depth, 2);
  assert.equal(rows[3].outcome, "final");
});

test("a turn cut short claims nothing: open outcome, no duration for the unfinished tool", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "bash" }, 0, 1),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool"]);
  assert.equal(rows[0].outcome, "open");
  assert.equal(rows[1].outcome, undefined);
  assert.equal(rows[2].durationMs, undefined);
  assert.equal(rows[2].outcome, undefined);
});

test("workflow stages are depth-1 rows under the turn, with attempt-scoped duration and marker", () => {
  const rows = buildTrajectoryTree([
    ev("workflow/stage_start", { name: "w", stage_id: "plan", attempt: 1, turn_id: "t1" }, 0, 1),
    ev("workflow/stage_done", { name: "w", stage_id: "plan", attempt: 1, marker: "ok", action: "next", output_kb: 2, turn_id: "t1" }, 400, 2),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "stage"]);
  assert.equal(rows[1].depth, 1);
  assert.equal(rows[1].label, "plan");
  assert.equal(rows[1].durationMs, 400);
  assert.equal(rows[1].outcome, "ok");
});

test("an unknown agent/event kind and an unknown method both render as plain rows, never break", () => {
  const rows = buildTrajectoryTree([
    ae("hologram", { step: 1, content: "x" }, 0, 1),
    ev("weather/update", { turn_id: "t1", step: 1, temp: 21 }, 5, 2),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "other", "other"]);
  assert.equal(rows[2].label, "hologram");
  assert.equal(rows[2].output, "x");
  assert.equal(rows[3].label, "weather/update");
});

test("a context_estimate produces no row and leaves the token columns blank", () => {
  const rows = buildTrajectoryTree([
    ae("context_estimate", { step: 1, data: { prompt_tokens: 8192, source: "estimate" } }, 0, 1),
    ae("step_done", { step: 1, content: "final" }, 10, 2),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step"]);
  assert.equal(rows[1].tokensIn, undefined);
  assert.equal(rows[1].tokensOut, undefined);
  assert.ok(!rows.some((r) => r.label === "context_estimate"), "the estimate must not appear as a row either");
});

test("child-scoped tool calls nest under their parent tool; child text does not join the step", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "task" }, 0, 1),
    ae("tool_call_start", { step: 1, tool_call_id: "c9", tool_call_name: "grep", scope: "child", parent_tool_call_id: "c1", task_id: "k1" }, 50, 2),
    ae("message_delta", { step: 1, content: "zzz", scope: "child", parent_tool_call_id: "c1", task_id: "k1" }, 60, 3),
    ae("tool_call_completed", { step: 1, tool_call_id: "c9", content: "", scope: "child", parent_tool_call_id: "c1", task_id: "k1" }, 80, 4),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "" }, 100, 5),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool", "tool"]);
  assert.equal(rows[2].label, "task");
  assert.equal(rows[2].depth, 2);
  assert.equal(rows[2].durationMs, 100);
  assert.equal(rows[3].label, "grep");
  assert.equal(rows[3].depth, 3);
  assert.equal(rows[3].durationMs, 30);
  assert.ok(!rows.some((r) => r.kind === "text"), "a subagent's text belongs to its own trace");
});

test("live events (no time_ms) build rows with blank times and live: true — never a client clock", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "read" }),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "ok" }),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool"]);
  for (const r of rows) {
    assert.equal(r.live, true, r.kind);
    assert.equal(r.startMs, undefined, r.kind);
    assert.equal(r.offsetMs, undefined, r.kind);
    assert.equal(r.durationMs, undefined, r.kind);
  }
  assert.equal(rows[2].output, "ok");
});

test("consecutive deltas of one kind coalesce into one row; a kind change starts a new row", () => {
  const rows = buildTrajectoryTree([
    ae("reasoning_delta", { step: 1, content: "a" }, 0, 1),
    ae("reasoning_delta", { step: 1, content: "b" }, 1, 2),
    ae("message_delta", { step: 1, content: "c" }, 2, 3),
    ae("message_delta", { step: 1, content: "d" }, 3, 4),
    ae("reasoning_delta", { step: 1, content: "e" }, 4, 5),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "reasoning", "text", "reasoning"]);
  assert.equal(rows[2].output, "ab");
  assert.equal(rows[3].output, "cd");
  assert.equal(rows[4].output, "e");
  assert.equal(rows[2].durationMs, 1);
});

test("shell output chunks attach to the open tool call's output, before its result preview", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "bash" }, 0, 1),
    ev("exec/output_chunk", { turn_id: "t1", step: 1, chunk: "ls\n" }, 10, 2),
    ev("exec/output_chunk", { turn_id: "t1", step: 1, chunk: "a.txt\n" }, 20, 3),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "exit 0" }, 30, 4),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool"]);
  assert.equal(rows[2].output, "ls\na.txt\nexit 0");
  assert.equal(rows[2].durationMs, 30);
});

test("two turns: each is its own depth-0 row, offsets are relative to each turn's own start", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "a" }, 1000, 1),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "" }, 1100, 2),
    ae("done", { step: 1 }, 1100, 3),
    ae("tool_call_start", { step: 1, tool_call_id: "c2", tool_call_name: "b", turn_id: "t2" }, 5000, 4),
    ae("tool_call_completed", { step: 1, tool_call_id: "c2", content: "", turn_id: "t2" }, 5400, 5),
    ae("done", { step: 1, turn_id: "t2" }, 5400, 6),
  ]);
  const turns = byKind(rows, "turn");
  assert.deepEqual(turns.map((t) => t.label), ["turn 1", "turn 2"]);
  assert.equal(turns[0].offsetMs, 0);
  assert.equal(turns[1].offsetMs, 0);
  const tools = byKind(rows, "tool");
  assert.equal(tools[1].offsetMs, 0);
  assert.equal(tools[1].durationMs, 400);
  assert.equal(tools[1].turnId, "t2");
});

test("pending_ops is a row that counts the ops", () => {
  const rows = buildTrajectoryTree([
    ae("pending_ops", { step: 1, data: { ops: [{}, {}], applied: false, diff: [] } }, 0, 1),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "pending"]);
  assert.equal(rows[2].label, "2 pending changes");
});

test("depth-1 rows keep the order they happened: a stage after a step is not hoisted above it", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "read" }, 0, 1),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "" }, 10, 2),
    ev("workflow/stage_start", { name: "w", stage_id: "verify", attempt: 1, turn_id: "t1" }, 1000, 3),
    ev("workflow/stage_done", { name: "w", stage_id: "verify", attempt: 1, marker: "ok", turn_id: "t1" }, 1400, 4),
    ae("tool_call_start", { step: 2, tool_call_id: "c2", tool_call_name: "edit" }, 2000, 5),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool", "stage", "step", "tool"]);
  // Offsets are monotone down the list, so nothing reads as going back in time.
  const offsets = rows.slice(1).map((r) => r.offsetMs);
  assert.deepEqual(offsets, [0, 0, 1000, 2000, 2000]);
});

test("a child tool completed without a recorded start still nests under its parent", () => {
  const rows = buildTrajectoryTree([
    ae("tool_call_start", { step: 1, tool_call_id: "c1", tool_call_name: "task" }, 0, 1),
    ae("tool_call_completed", { step: 1, tool_call_id: "c9", tool_call_name: "grep", content: "", scope: "child", parent_tool_call_id: "c1", task_id: "k1" }, 80, 2),
    ae("tool_call_completed", { step: 1, tool_call_id: "c1", content: "" }, 100, 3),
  ]);
  assert.deepEqual(kinds(rows), ["turn", "step", "tool", "tool"]);
  assert.equal(rows[3].label, "grep");
  assert.equal(rows[3].depth, 3);
  assert.equal(rows[3].durationMs, undefined, "no start was recorded, so no duration is claimed");
});

test("garbage in does not throw: non-array, null events, events without data", () => {
  assert.deepEqual(buildTrajectoryTree(null), []);
  assert.deepEqual(buildTrajectoryTree(undefined), []);
  const rows = buildTrajectoryTree([null, {}, { type: "agent/event" }, ev("agent/event", null, 1, 1)]);
  assert.ok(Array.isArray(rows));
});
```

- [ ] **Step 3: Run the tests and watch them fail**

From `ui/vscode`:

```bash
node --test scripts/trajectory-test.mjs
```

Expected: the file-level `assert.equal(typeof buildTrajectoryTree, "function", …)` fails — the fragment does not exist yet, so `fs.readFileSync` throws `ENOENT` for `05f-trajectory.js`. Either failure is the expected one.

- [ ] **Step 4: Write the pure function**

Create `ui/vscode/media/chat-src/05f-trajectory.js`. In this task it holds only the typedef and `buildTrajectoryTree`; Task 2 appends the rendering below it in the same file. The fragment is spliced into an IIFE, so it has no `import`/`export` and two-space indentation like its neighbours.

```js
  // ---- Trajectory view ------------------------------------------------------
  //
  // The tab is a pure function from a session's event log to a tree of rows,
  // drawn by a thin renderer below. The log is what session.trajectory returns
  // and what the core's tee records: `{ seq, time_ms, type, data }` where
  // `type` is the notification method and `data` its params. A live event
  // forwarded mid-turn has the same shape minus seq and time_ms.

  /**
   * @typedef {{
   *   kind: "turn"|"step"|"tool"|"text"|"reasoning"|"error"|"stage"|"pending"|"route"|"other",
   *   depth: number,
   *   key: string,
   *   label: string,
   *   turnId: string,
   *   step?: number,
   *   startMs?: number,
   *   offsetMs?: number,
   *   durationMs?: number,
   *   tokensIn?: number,
   *   tokensOut?: number,
   *   outcome?: string,
   *   input?: string,
   *   output?: string,
   *   live: boolean,
   *   seq?: number
   * }} TrajRow
   */

  /**
   * buildTrajectoryTree turns recorded (or live) events into flat rows with a
   * depth. Pure: no DOM, no fragment state, no clock. Everything it knows
   * about time comes from the events' own core-stamped `time_ms`; a live
   * event has none, and its row says so rather than borrowing the client's.
   *
   * Grouping: turn (by data.turn_id, in order of first appearance) > step (by
   * data.step) > items. Tool calls are keyed by tool_call_id; a child-scoped
   * tool call nests under its parent_tool_call_id. Consecutive text deltas of
   * one kind coalesce into one row. Workflow stages, mode routes and steps sit
   * directly under the turn in the order they first appeared — time order for a
   * recorded log, and a rule that needs no timestamp so it holds for live rows
   * too. Token columns come from step_usage only.
   *
   * Self-contained on purpose: trajectory-test.mjs evaluates it standalone.
   *
   * @param {Array<{seq?: number, time_ms?: number, type: string, data?: any}>|null|undefined} events
   * @returns {TrajRow[]}
   */
  function buildTrajectoryTree(events) {
    /** @type {TrajRow[]} */
    const rows = [];
    if (!Array.isArray(events)) return rows;

    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const str = (v) => (typeof v === "string" ? v : "");
    const obj = (v) => (v && typeof v === "object" ? v : {});

    // Turns in order of first appearance. Events without a turn_id (older
    // cores, one-shot agent.run) share one unnamed turn so nothing is dropped.
    const turns = new Map();
    let turnOrdinal = 0;
    const turnFor = (id) => {
      let t = turns.get(id);
      if (!t) {
        turnOrdinal++;
        t = {
          key: "turn:" + id,
          id,
          ordinal: turnOrdinal,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: "open",
          steps: new Map(), // step number -> step node, for lookup
          depth1: [], // steps, stages and routes in the order they first appeared — which is time order
        };
        turns.set(id, t);
      }
      return t;
    };
    const stepFor = (t, n) => {
      const key = String(n);
      let s = t.steps.get(key);
      if (!s) {
        s = {
          kind: "step",
          key: t.key + "/step:" + key,
          n,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: undefined,
          tokensIn: undefined,
          tokensOut: undefined,
          items: [], // rows under the step, in order
          tools: new Map(), // tool_call_id -> tool node (parent and child scope alike)
          openTool: null, // last parent-scope tool started and not yet completed
          lastText: null, // for coalescing message_delta / reasoning_delta
        };
        t.steps.set(key, s);
        t.depth1.push(s);
      }
      return s;
    };
    // Widen a node's [startMs, endMs] to include ms, and mark it live if ms is unknown.
    const touch = (node, ms, live) => {
      if (live) node.live = true;
      if (ms === undefined) return;
      if (node.startMs === undefined || ms < node.startMs) node.startMs = ms;
      if (node.endMs === undefined || ms > node.endMs) node.endMs = ms;
    };

    for (const e of events) {
      if (!e || typeof e !== "object") continue;
      const method = str(e.type);
      const d = obj(e.data);
      const ms = num(e.time_ms);
      const live = ms === undefined;
      const seq = num(e.seq);
      const t = turnFor(str(d.turn_id));
      touch(t, ms, live);

      if (method === "workflow/stage_start" || method === "workflow/stage_done") {
        const stageKey = t.key + "/stage:" + str(d.stage_id) + "#" + (num(d.attempt) ?? 0);
        let st = t.depth1.find((c) => c.kind === "stage" && c.key === stageKey);
        if (!st) {
          st = {
            kind: "stage",
            key: stageKey,
            label: str(d.stage_id) || str(d.name) || "stage",
            startMs: undefined,
            endMs: undefined,
            live: false,
            outcome: undefined,
            seq,
          };
          t.depth1.push(st);
        }
        touch(st, ms, live);
        if (method === "workflow/stage_done") st.outcome = str(d.marker) || str(d.action) || "done";
        continue;
      }

      const s = stepFor(t, num(d.step) ?? 0);
      touch(s, ms, live);

      if (method === "exec/output_chunk") {
        const target = s.openTool;
        if (target) {
          target.output += str(d.chunk);
          touch(target, ms, live);
        } else {
          s.items.push({ kind: "other", key: s.key + "/exec:" + s.items.length, label: "shell output", startMs: ms, endMs: ms, live, output: str(d.chunk), seq });
        }
        s.lastText = null;
        continue;
      }

      if (method !== "agent/event") {
        // A notification this view has never heard of stays visible as a
        // plain row: the log outlives the code that reads it.
        s.items.push({ kind: "other", key: s.key + "/" + method + ":" + s.items.length, label: method || "event", startMs: ms, endMs: ms, live, seq });
        s.lastText = null;
        continue;
      }

      const kind = str(d.type);
      const isChild = d.scope === "child";
      const parentToolId = str(d.parent_tool_call_id);

      switch (kind) {
        case "message_delta":
        case "reasoning_delta": {
          if (isChild) break; // a subagent's text belongs to its own trace, not this step's
          const k = kind === "reasoning_delta" ? "reasoning" : "text";
          if (s.lastText && s.lastText.kind === k) {
            s.lastText.output += str(d.content);
            touch(s.lastText, ms, live);
          } else {
            const row = { kind: k, key: s.key + "/" + k + ":" + s.items.length, label: k === "reasoning" ? "reasoning" : "response", startMs: ms, endMs: ms, live, output: str(d.content), seq };
            s.items.push(row);
            s.lastText = row;
          }
          break;
        }
        case "tool_call_start": {
          const id = str(d.tool_call_id) || "idx" + (num(d.tool_call_index) ?? s.items.length);
          const row = { kind: "tool", key: s.key + "/tool:" + id, id, label: str(d.tool_call_name) || "tool", startMs: ms, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
          const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
          if (parent) parent.subrows.push(row);
          else s.items.push(row);
          s.tools.set(id, row);
          if (!isChild) s.openTool = row;
          s.lastText = null;
          break;
        }
        case "tool_call_delta": {
          const row = s.tools.get(str(d.tool_call_id));
          if (row) {
            row.input += str(d.args_delta);
            touch(row, ms, live);
          }
          break;
        }
        case "tool_call_completed": {
          const id = str(d.tool_call_id);
          let row = s.tools.get(id);
          if (!row) {
            // Completed without a recorded start: still a row, with no
            // duration to claim.
            row = { kind: "tool", key: s.key + "/tool:" + (id || String(s.items.length)), id, label: str(d.tool_call_name) || "tool", startMs: undefined, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
            const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
            if (parent) parent.subrows.push(row);
            else s.items.push(row);
            if (id) s.tools.set(id, row);
          }
          row.output += str(d.content);
          row.outcome = "done";
          touch(row, ms, live);
          if (s.openTool === row) s.openTool = null;
          s.lastText = null;
          break;
        }
        case "step_usage": {
          if (isChild) break; // a worker's usage is its own; the step's columns are the main agent's
          const u = obj(d.data);
          if (num(u.prompt_tokens) !== undefined) s.tokensIn = num(u.prompt_tokens);
          if (num(u.completion_tokens) !== undefined) s.tokensOut = num(u.completion_tokens);
          break;
        }
        case "context_estimate":
        case "todos_updated":
        case "child_started":
        case "child_done":
          // Deliberately no row. The estimate is not a measurement and must
          // never reach a token column; todos and child lifecycle are drawn
          // elsewhere in the chat.
          break;
        case "step_done":
          s.outcome = str(d.content) || "done";
          if (s.outcome === "final") t.outcome = "final";
          s.lastText = null;
          break;
        case "done":
          if (t.outcome === "open") t.outcome = "done";
          break;
        case "recoverable_error":
        case "error": {
          s.items.push({ kind: "error", key: s.key + "/err:" + s.items.length, label: kind === "error" ? "error" : "retry", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          if (kind === "error") t.outcome = "error";
          s.lastText = null;
          break;
        }
        case "pending_ops": {
          const p = obj(d.data);
          const n = Array.isArray(p.ops) ? p.ops.length : 0;
          s.items.push({ kind: "pending", key: s.key + "/pending:" + s.items.length, label: n + " pending change" + (n === 1 ? "" : "s") + (p.applied ? " (applied)" : ""), startMs: ms, endMs: ms, live, seq });
          s.lastText = null;
          break;
        }
        case "mode_route": {
          const r = obj(d.data);
          t.depth1.push({ kind: "route", key: t.key + "/route:" + t.depth1.length, label: "mode " + str(r.from) + " → " + str(r.to), startMs: ms, endMs: ms, live, seq, output: str(r.reason) });
          break;
        }
        default:
          s.items.push({ kind: "other", key: s.key + "/" + (kind || "event") + ":" + s.items.length, label: kind || "event", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          s.lastText = null;
      }
    }

    // Flatten to rows. Offsets are relative to the row's own turn.
    const dur = (n) => (n.startMs !== undefined && n.endMs !== undefined ? n.endMs - n.startMs : undefined);
    const off = (t, n) => (t.startMs !== undefined && n.startMs !== undefined ? n.startMs - t.startMs : undefined);
    const push = (t, n, depth, kind, extra) => {
      rows.push(
        Object.assign(
          { kind, depth, key: n.key, label: n.label, turnId: t.id, startMs: n.startMs, offsetMs: off(t, n), durationMs: dur(n), live: Boolean(n.live), seq: n.seq },
          extra || {}
        )
      );
    };
    const pushTool = (t, s, node, depth) => {
      push(t, node, depth, "tool", { step: s.n, input: node.input || undefined, output: node.output || undefined, outcome: node.outcome });
      for (const sub of node.subrows) pushTool(t, s, sub, depth + 1);
    };

    for (const t of turns.values()) {
      push(t, { key: t.key, label: "turn " + t.ordinal, startMs: t.startMs, endMs: t.endMs, live: t.live }, 0, "turn", { outcome: t.outcome });
      for (const n of t.depth1) {
        if (n.kind !== "step") {
          push(t, n, 1, n.kind, { outcome: n.outcome, output: n.output || undefined });
          continue;
        }
        push(t, { key: n.key, label: "step " + n.n, startMs: n.startMs, endMs: n.endMs, live: n.live }, 1, "step", { step: n.n, tokensIn: n.tokensIn, tokensOut: n.tokensOut, outcome: n.outcome });
        for (const it of n.items) {
          if (it.kind === "tool") pushTool(t, n, it, 2);
          else push(t, it, 2, it.kind, { step: n.n, output: it.output || undefined, outcome: it.outcome });
        }
      }
    }
    return rows;
  }
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
node --test scripts/trajectory-test.mjs
```

Expected: 16 tests pass. If one fails, the fixture and the function disagree — read the failing assertion against the grouping rules in the doc comment and fix **the function** unless the fixture contradicts the spec; if you change a fixture, say why in your report.

- [ ] **Step 6: Wire the tests into CI, regenerate both bundles, verify they still parse**

In `ui/vscode/package.json`, change the `check:webview` script from

```json
"check:webview": "node scripts/check-webview.mjs",
```

to

```json
"check:webview": "node scripts/check-webview.mjs && node --test scripts/trajectory-test.mjs",
```

CI already runs `npm run check:webview`, so the fixtures run on every push without touching `.github/workflows/ci.yml`.

Then, from `ui/vscode`: `npm run bundle:webview && npm run check:webview`. From the repo root: `node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/check-web.mjs && node ui/web/scripts/adapter-test.mjs`.

Expected: all green. The bundles now contain the function; nothing calls it yet.

- [ ] **Step 7: Commit**

```bash
git add ui/vscode/media/chat-src/05f-trajectory.js ui/vscode/scripts/trajectory-test.mjs ui/vscode/scripts/bundle-chat.mjs ui/web/scripts/bundle-web.mjs ui/vscode/package.json ui/vscode/media/chat.bundle.js ui/web/static/web.bundle.js
git commit -m "feat(ui): buildTrajectoryTree — the trajectory view as a pure function

Events from session.trajectory (or forwarded live) in, depth-annotated rows
out: turn > step > tool call > nested child tool call, with offsets and
durations from the events' own core-stamped times and token columns from
step_usage only. No DOM, no state, no clock, so it is tested on fourteen
fixtures alone — interleaved tools, a retry, a turn cut short, workflow
stages, unknown types, an estimate that must leave the columns blank, child
scope, live events, coalesced deltas, shell output, two turns.

Registered in both bundle orders; nothing renders it yet."
```

---

### Task 2: The pane, the segmented control, and the renderer

Everything the user sees, in both pages. Still no host wiring: this task ends with a view that renders whatever a `trajectory` / `trajectoryEvent` message hands it, verified through the adapter harness and by eye.

**Files:**
- Modify: `ui/vscode/media/chat-src/05f-trajectory.js` (append the rendering below `buildTrajectoryTree`)
- Modify: `ui/vscode/media/chat-src/07-events.js` (two cases; `clearMessages`)
- Modify: `ui/vscode/media/chat.css` (append)
- Modify: `ui/vscode/media/chat-src/README.md` (one table row)
- Modify: `ui/vscode/src/protocol/events.ts` (`TrajectoryEvent`; two `HostToWebview` members)
- Modify: `ui/web/index.src.html` (markup)
- Modify: `ui/vscode/src/chat/panel.ts` (`getHtml`, the same markup)
- Modify: `ui/web/scripts/adapter-test.mjs` (harness: listeners; two tests)

**Interfaces:**
- Consumes: `buildTrajectoryTree(events) → TrajRow[]` (Task 1); `formatToolDuration(ms) → string` from `05d-tools.js:139` (already in the IIFE ahead of this fragment).
- Produces, for Tasks 3 and 4, the two renderer messages and the DOM ids:
  - `{ type: "trajectory"; recorded: boolean; events: TrajectoryEvent[]; error?: string }` — replaces the view. `error` set means the fetch failed; the view says so rather than claiming "not recorded".
  - `{ type: "trajectoryEvent"; event: { type: string; data: unknown } }` — appends one live notification (`type` is the method).
  - ids: `view-switch`, `view-chat-btn`, `view-trajectory-btn`, `trajectory`, `trajectory-summary`, `trajectory-rows`; `#app[data-view="chat"|"trajectory"]`.

- [ ] **Step 1: Declare the messages**

In `ui/vscode/src/protocol/events.ts`, after the `StepUsagePayload` interface, add:

```ts
/** One recorded (or live) notification, as session.trajectory returns it.
 * `type` is the JSON-RPC method ("agent/event", "exec/output_chunk", …) and
 * `data` its params. seq and time_ms are absent on a live event forwarded
 * mid-turn; the view renders blank times for those rather than a client clock. */
export interface TrajectoryEvent {
  seq?: number;
  time_ms?: number;
  type: string;
  source?: string;
  data?: unknown;
}
```

In the `HostToWebview` union, after `| { type: "history"; messages: ChatHistoryMessage[] }`, add:

```ts
  /** Replace the Trajectory view from the session's log. recorded:false means the
   * session predates the log — a different answer from an empty log. error set
   * means the fetch failed; the view says so instead of claiming either. */
  | { type: "trajectory"; recorded: boolean; events: TrajectoryEvent[]; error?: string }
  /** Append one live notification to the Trajectory view. Reconciled by the
   * next "trajectory" message, which the host sends when the turn ends. */
  | { type: "trajectoryEvent"; event: { type: string; data: unknown } }
```

- [ ] **Step 2: Improve the harness so the control can be driven, then write the failing tests**

In `ui/web/scripts/adapter-test.mjs`, the stub element factory `el()` has `addEventListener() {}` and `click() {}` as no-ops, which is why C1's follow-ups record the keyboard path as untestable. Replace those two lines inside `el()` with listener recording:

```js
    _listeners: {},
    addEventListener(type, fn) {
      (this._listeners[type] ||= []).push(fn);
    },
    click() {
      for (const fn of this._listeners.click || []) fn({ preventDefault() {} });
    },
    append() {},
    keydown(key) {
      for (const fn of this._listeners.keydown || []) fn({ key, preventDefault() {} });
    },
```

(`removeEventListener() {}` stays a no-op.) `append() {}` is new too: the renderer uses `el.append(...)`, the stub only had `appendChild`, and a missing method would throw inside the harness's swallowed `try` — the summary would still read right and a real error would hide. Keep everything else in `el()` as it is. This is additive: existing tests never relied on `click()` doing nothing.

Then append these tests at the end of the file:

```js
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
```

- [ ] **Step 3: Run them and watch them fail**

From the repo root: `node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/adapter-test.mjs`

Expected: the four new tests fail — `#trajectory-summary` was never looked up (`summary` is null), or `textContent` is `""`. The 29 existing tests still pass.

- [ ] **Step 4: Append the rendering to the fragment**

Append to `ui/vscode/media/chat-src/05f-trajectory.js`, below `buildTrajectoryTree`:

```js
  // ---- rendering ------------------------------------------------------------

  const trajectorySummary = document.getElementById("trajectory-summary");
  const trajectoryRowsEl = document.getElementById("trajectory-rows");
  const viewChatBtn = document.getElementById("view-chat-btn");
  const viewTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const appViewEl = document.getElementById("app");

  /** @type {Array<{seq?: number, time_ms?: number, type: string, data?: any}>} */
  let trajEvents = [];
  /** null until the host has answered once; then the log's own recorded flag. */
  let trajRecorded = null;
  /** Set when the host could not fetch the log; shown instead of a false "not recorded". */
  let trajError = "";
  let trajRenderQueued = false;

  const TRAJ_GLYPH = { turn: "◆", step: "▸", tool: "⚙", text: "¶", reasoning: "…", error: "!", stage: "▣", pending: "±", route: "↦", other: "·" };

  function currentView() {
    return appViewEl && appViewEl.dataset.view === "trajectory" ? "trajectory" : "chat";
  }

  /** @param {string} view */
  function setView(view) {
    const v = view === "trajectory" ? "trajectory" : "chat";
    if (appViewEl) appViewEl.dataset.view = v;
    if (viewChatBtn) viewChatBtn.setAttribute("aria-selected", v === "chat" ? "true" : "false");
    if (viewTrajectoryBtn) viewTrajectoryBtn.setAttribute("aria-selected", v === "trajectory" ? "true" : "false");
  }

  function bindViewSwitch() {
    if (viewChatBtn) viewChatBtn.addEventListener("click", () => setView("chat"));
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("click", () => setView("trajectory"));
    // Arrow keys move between the two segments, as a tablist does. The
    // listeners sit on each button rather than on a shared ancestor found via
    // closest(): the test harness's stub closest() returns null, which is what
    // left C1's rail keyboard path with no automated coverage.
    const onKey = (e) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      const next = currentView() === "chat" ? "trajectory" : "chat";
      setView(next);
      const btn = next === "chat" ? viewChatBtn : viewTrajectoryBtn;
      if (btn) btn.focus();
    };
    if (viewChatBtn) viewChatBtn.addEventListener("keydown", onKey);
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("keydown", onKey);
  }

  function scheduleTrajectoryRender() {
    if (trajRenderQueued) return;
    trajRenderQueued = true;
    requestAnimationFrame(() => {
      trajRenderQueued = false;
      renderTrajectory();
    });
  }

  /** Session switch or clear: forget everything and wait for the host. */
  function resetTrajectory() {
    trajEvents = [];
    trajRecorded = null;
    trajError = "";
    scheduleTrajectoryRender();
  }

  /** The host fetched session.trajectory: this is now the whole truth. */
  function replaceTrajectory(recorded, events, error) {
    trajRecorded = Boolean(recorded);
    trajEvents = Array.isArray(events) ? events.slice() : [];
    trajError = typeof error === "string" ? error : "";
    scheduleTrajectoryRender();
  }

  /** One live notification. A turn is being recorded now even if the session
   * predated the log, so the "not recorded" answer no longer applies. */
  function appendTrajectoryEvent(ev) {
    if (!ev || typeof ev.type !== "string") return;
    trajEvents.push({ type: ev.type, data: ev.data });
    trajRecorded = true;
    scheduleTrajectoryRender();
  }

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = "Trajectory unavailable: " + trajError;
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = "No trajectory was recorded for this session — it predates the log.";
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = trajRecorded === null ? "Loading trajectory…" : "Nothing has happened in this session yet.";
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    trajectorySummary.textContent = turns + " turn" + (turns === 1 ? "" : "s") + " · " + rows.length + " rows" + (liveCount ? " · " + liveCount + " live" : "");
    const frag = document.createDocumentFragment();
    for (const r of rows) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
  }

  /** @param {TrajRow} r */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.style.setProperty("--traj-depth", String(r.depth));

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const glyph = document.createElement("span");
    glyph.className = "traj-glyph";
    glyph.textContent = TRAJ_GLYPH[r.kind] || TRAJ_GLYPH.other;
    glyph.setAttribute("aria-hidden", "true");
    const label = document.createElement("span");
    label.className = "traj-label";
    label.textContent = r.label + (r.outcome && r.kind !== "tool" ? " · " + r.outcome : "");
    const dur = document.createElement("span");
    dur.className = "traj-dur";
    dur.textContent = r.durationMs === undefined ? "" : formatToolDuration(r.durationMs);
    const tok = document.createElement("span");
    tok.className = "traj-tok";
    // Blank unless a provider actually reported a number. Never an estimate.
    tok.textContent =
      (r.tokensIn === undefined ? "" : r.tokensIn + "↑") +
      (r.tokensOut === undefined ? "" : (r.tokensIn === undefined ? "" : " ") + r.tokensOut + "↓");
    el.append(off, glyph, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));

    if (r.input || r.output) {
      el.classList.add("traj-expandable");
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-expanded", "false");
      const detail = document.createElement("div");
      detail.className = "traj-detail hidden";
      const section = (head, text) => {
        const h = document.createElement("div");
        h.className = "traj-detail-head";
        h.textContent = head;
        const pre = document.createElement("pre");
        pre.className = "traj-pre";
        pre.textContent = text;
        detail.append(h, pre);
      };
      if (r.input) section("input", r.input);
      if (r.output) section("output", r.output);
      el.appendChild(detail);
      const toggle = () => {
        const hidden = detail.classList.toggle("hidden");
        el.setAttribute("aria-expanded", hidden ? "false" : "true");
      };
      el.addEventListener("click", toggle);
      el.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          toggle();
        }
      });
    }
    return el;
  }

  bindViewSwitch();
  setView("chat");
  scheduleTrajectoryRender();
```

`formatToolDuration` is defined in `05d-tools.js`, which both bundles place before this fragment; `01-dom-state.js` already looks elements up at top level, so the DOM is present when this runs.

- [ ] **Step 5: Dispatch the messages**

In `ui/vscode/media/chat-src/07-events.js`, inside the `switch (msg.type)`, add beside `case "history":`:

```js
      case "trajectory":
        replaceTrajectory(msg.recorded, msg.events, msg.error);
        break;
      case "trajectoryEvent":
        appendTrajectoryEvent(msg.event);
        break;
```

Find `case "clearMessages":` (line ~179) and add `resetTrajectory();` as the first statement of that case, before whatever it already does. A session switch clears the chat pane through this message on both hosts, and the trajectory must not outlive the session it belongs to.

- [ ] **Step 6: Markup, identical in both pages**

In `ui/web/index.src.html`:

1. Change `<div id="app" data-mode="agent">` to `<div id="app" data-mode="agent" data-view="chat">`.
2. Immediately after the line `<div id="session-tabs" class="session-tabs" role="tablist" aria-label="Chat sessions"></div>` insert:

```html
        <div id="view-switch" class="view-switch" role="tablist" aria-label="View">
          <button type="button" id="view-chat-btn" class="view-segment" role="tab" aria-selected="true" aria-controls="messages">Chat</button>
          <button type="button" id="view-trajectory-btn" class="view-segment" role="tab" aria-selected="false" aria-controls="trajectory">Trajectory</button>
        </div>
```

3. Immediately after `<div id="messages"></div>` insert:

```html
    <div id="trajectory" class="trajectory" role="tabpanel" aria-labelledby="view-trajectory-btn">
      <div id="trajectory-summary" class="traj-summary" aria-live="polite"></div>
      <div id="trajectory-rows" class="traj-rows"></div>
    </div>
```

In `ui/vscode/src/chat/panel.ts`, inside `getHtml()`'s template (the markup starting around line 2304), make the same three edits at the same three places: `data-view="chat"` on `#app` (line ~2304), the `#view-switch` block after `#session-tabs` (line ~2311), the `#trajectory` block after `<div id="messages"></div>` (line ~2349). The markup is byte-identical between the two pages; `check-web.mjs` will fail if an id the fragment looks up is missing from `index.html`.

- [ ] **Step 7: Styles**

Append to `ui/vscode/media/chat.css`:

```css
/* ---- Trajectory view (C2b) ---------------------------------------------- */

/* One pane is shown at a time; the segmented control sets data-view on #app. */
#app[data-view="trajectory"] #messages {
  display: none;
}
#app:not([data-view="trajectory"]) #trajectory {
  display: none;
}

.view-switch {
  display: inline-flex;
  align-items: stretch;
  flex: 0 0 auto;
  margin-left: 6px;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}
.view-segment {
  padding: 4px 10px;
  border: none;
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  transition: color 0.12s, background 0.12s;
}
.view-segment + .view-segment {
  border-left: 1px solid var(--border);
}
.view-segment:hover {
  color: var(--fg);
  background: var(--pill-hover);
}
.view-segment[aria-selected="true"] {
  color: var(--fg);
  background: var(--menu-selected);
}
.view-segment:focus-visible {
  outline: 1px solid var(--accent);
  outline-offset: -1px;
}

#trajectory {
  flex: 1 1 auto;
  overflow-y: auto;
  width: 100%;
  max-width: 720px;
  margin: 0 auto;
  padding: 10px 14px 8px;
  min-height: 0;
  font-size: 12px;
}
.traj-summary {
  color: var(--muted);
  padding: 2px 0 8px;
  font-variant-numeric: tabular-nums;
}
.traj-rows {
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.traj-row {
  display: grid;
  grid-template-columns: 5.5em 1.2em minmax(0, 1fr) 4.5em auto;
  align-items: baseline;
  column-gap: 8px;
  padding: 3px 6px 3px calc(6px + var(--traj-depth, 0) * 16px);
  border-radius: 4px;
  font-variant-numeric: tabular-nums;
}
/* Kind first by glyph and indent; colour is a secondary cue. */
.traj-off,
.traj-dur,
.traj-tok {
  color: var(--muted);
  white-space: nowrap;
}
.traj-dur,
.traj-tok {
  text-align: right;
}
.traj-glyph {
  color: var(--muted);
  text-align: center;
}
.traj-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.traj-turn .traj-label {
  font-weight: 600;
}
.traj-turn {
  margin-top: 6px;
  border-top: 1px solid var(--border);
  padding-top: 6px;
}
.traj-step .traj-label {
  font-weight: 500;
}
.traj-tool .traj-glyph {
  color: var(--tool-fg);
}
.traj-error .traj-glyph,
.traj-error .traj-label {
  color: var(--error-fg);
}
.traj-reasoning .traj-label,
.traj-text .traj-label {
  color: var(--muted);
  font-style: italic;
}
.traj-live .traj-label::after {
  content: " · live";
  color: var(--mode-accent);
  font-style: normal;
  font-weight: 400;
}
.traj-expandable {
  cursor: pointer;
}
.traj-expandable:hover {
  background: var(--menu-hover);
}
.traj-expandable:focus-visible {
  outline: 1px solid var(--accent);
  outline-offset: -1px;
}
.traj-detail {
  grid-column: 1 / -1;
  margin: 4px 0 2px;
}
.traj-detail-head {
  color: var(--muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-top: 4px;
}
.traj-pre {
  margin: 2px 0 0;
  padding: 6px 8px;
  max-height: 240px;
  overflow: auto;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11.5px;
  white-space: pre-wrap;
  word-break: break-word;
}
```

- [ ] **Step 8: Note the fragment in the README**

In `ui/vscode/media/chat-src/README.md`, add a table row after `05e-messages.js`:

```markdown
| `05f-trajectory.js` | 330 | Trajectory view: pure event→row builder, renderer, Chat/Trajectory switch |
```

- [ ] **Step 9: Rebuild, run every check, and look at it**

From `ui/vscode`: `npm run bundle:webview && npm run check:webview && npm run compile`.
From the repo root: `node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/check-web.mjs && node ui/web/scripts/adapter-test.mjs`.

Expected: all green; the adapter suite is now 33 tests.

**Then look at it, and put what you saw in the report.** Open the built web page with a headless browser and take three screenshots at 1200×800: the page as loaded (Chat selected, control visible in the header), after clicking Trajectory with no data (the "Loading trajectory…" line), and the Chat view again. On Windows:

```powershell
$edge = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
$page = (Resolve-Path ui\web\static\index.html).Path
& $edge --headless=new --disable-gpu --window-size=1200,800 --screenshot="$env:TEMP\traj-chat.png" "file:///$page"
```

Then read the PNG back with the Read tool and describe what is actually visible: is the control in the header beside the session tabs, are the two panes mutually exclusive, does the light-on-dark contrast hold. A screenshot the report does not describe was not looked at. If the page needs a running core to render at all, say so and fall back to describing the DOM via the harness.

- [ ] **Step 10: Commit**

```bash
git add ui/vscode/media/chat-src/05f-trajectory.js ui/vscode/media/chat-src/07-events.js ui/vscode/media/chat.css ui/vscode/media/chat-src/README.md ui/vscode/src/protocol/events.ts ui/web/index.src.html ui/vscode/src/chat/panel.ts ui/web/scripts/adapter-test.mjs ui/vscode/media/chat.bundle.js ui/web/static/web.bundle.js ui/web/static/index.html ui/web/static/chat.css
git commit -m "feat(ui): the Trajectory pane and the Chat/Trajectory switch, in both pages

A segmented control in the header switches #app[data-view] between the chat
pane and a trajectory pane that renders buildTrajectoryTree's rows: offset,
glyph, label, duration, provider-reported tokens; a click expands a row's
input and output in place. Kind is carried by glyph and indent first,
colour second. Empty states say which they are — predates the log, nothing
yet, or unavailable — in words, in place.

Two renderer messages: trajectory replaces from the log, trajectoryEvent
appends a live notification. Nothing sends them yet.

The adapter harness's stub elements now record listeners and fire them on
click()/keydown(), which is what lets the switch be tested and what C1
lacked for its rail keyboard path."
```

---

### Task 3: The web host — fetch on switch, append live, re-fetch on turn end

The mid-turn switch is the symptom that motivated C2. It gets its own test here.

**Files:**
- Modify: `ui/web/src/10-adapter-session.js` (`onConnected` ~line 46; `activateProject` ~line 91; `sendTurn`'s `finally` ~line 262; `startSession` ~line 277)
- Modify: `ui/web/src/20-adapter-events.js` (`handleNotification` ~line 83)
- Modify: `ui/web/scripts/adapter-test.mjs` (eight tests)

**Interfaces:**
- Consumes: renderer messages `trajectory` / `trajectoryEvent` (Task 2); JSON-RPC `session.trajectory {session_id} → {recorded, events[]}` (C2a); the adapter's `conn.send(method, params)`, `toRenderer(msg)`, `currentProjectId`, `projectState(id)`, `connFor(id)`.
- Produces: `async function refreshTrajectory(projectId, conn, sessionId)` in `10-adapter-session.js`, used by `onConnected`, `activateProject`, `sendTurn` and `startSession` — every path that gives the renderer a session.

- [ ] **Step 1: Write the failing tests**

Append to `ui/web/scripts/adapter-test.mjs`:

```js
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
  assert.equal(reqs[reqs.length - 1].params.session_id, "s-A2", "the fetch must name the new session, not the old one");
  answerOn(b, "A", "session.trajectory", { recorded: true, events: [] });
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
```

`handshakeFor`, `openBackground`, `answerOn`, `dispatch`, `tick` already exist in the file (used by the "switching repaints from the core" test at line ~636). `handshakeFor(b, id)` answers `session.start` with `session_id: "s-" + id` (line ~494), which is why the third test asserts `"s-A"`.

- [ ] **Step 2: Run them and watch them fail**

From the repo root: `node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/adapter-test.mjs`

Expected: the eight new tests fail on "connecting did not ask the core for the first session's trajectory" / "switching did not ask the core for the trajectory" / "exactly the two on-screen notifications" (0 found) / "the turn ended and nobody re-read the log" / "a new session did not ask the core for its trajectory". The others still pass.

- [ ] **Step 3: The fetch, with the stale guard**

In `ui/web/src/10-adapter-session.js`, add before `activateProject`:

```js
  /**
   * Read the session's log and hand it to the renderer — unless the user has
   * switched projects, or to another session of the same project, while the
   * request was in flight, in which case the answer belongs to a view that is
   * no longer on screen. The core answers each request in its own goroutine,
   * so two fetches for one project can land in either order.
   *
   * A failed fetch is reported as such, not as "not recorded": those are
   * different answers and the view says which.
   * @param {string} projectId @param {any} conn @param {string} sessionId
   */
  async function refreshTrajectory(projectId, conn, sessionId) {
    if (!sessionId) {
      return;
    }
    try {
      const res = await conn.send("session.trajectory", { session_id: sessionId });
      if (projectId !== currentProjectId || projectState(projectId).sessionId !== sessionId) {
        // The user left this project, or moved to another session of it,
        // while the request was in flight: the answer is for a view that is
        // no longer on screen.
        return;
      }
      toRenderer({
        type: "trajectory",
        recorded: Boolean(res && res.recorded),
        events: res && Array.isArray(res.events) ? res.events : [],
      });
    } catch (err) {
      if (projectId === currentProjectId && projectState(projectId).sessionId === sessionId) {
        toRenderer({ type: "trajectory", recorded: true, events: [], error: String(err && err.message ? err.message : err) });
      }
    }
  }
```

In `onConnected` — the path that starts the *first* session of a freshly connected project — inside its `if (projectId === currentProjectId) { … }` block, after `toRenderer({ type: "ready" });` and before `await refreshSessionList(projectId);`, add:

```js
        // The first session of this connection: give the Trajectory pane its
        // (usually empty) log now, or it sits on "Loading trajectory…" until
        // the user switches, starts a session, or finishes a turn.
        void refreshTrajectory(projectId, conn, st.sessionId);
```

Without this the pane is never populated on first load: `onConnected` posts `header` and `ready` and never passes through `clearMessages`, `startSession` or `activateProject`. The first build of this task shipped without it and a driven headless page showed "Loading trajectory…" indefinitely.

In `activateProject`, inside the `if (st.sessionId) { … }` block, after the `try { … session.get … } catch { … }` and still inside the `if`, add:

```js
      // Not awaited: the fetch must not hold up the header/turnComplete/
      // session.list below, which is what the renderer's own state (composer
      // busy/idle, session list) depends on. refreshTrajectory carries its
      // own stale guard, so a late answer still lands correctly (or is
      // dropped) once it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
```

(Fire-and-forget on purpose: awaiting here would make the `header` post and the session list wait on the log fetch, and would hang any caller whose core never answers `session.trajectory`. The stale guard inside `refreshTrajectory` is what makes that safe.)

In `sendTurn`'s `finally`, inside the existing `if (projectId === currentProjectId) { … }` after `toRenderer({ type: "turnComplete", ok: !failed });`, add:

```js
        // The log is complete once session.message has returned — the core
        // closes the writer before it answers — so this replaces the live rows
        // with the recorded ones, which carry the core's own timings.
        void refreshTrajectory(projectId, conn, st.sessionId);
```

`sendTurn` has no `conn` local today — it calls `connFor(projectId).sendCancellable(...)` inline. Hoist it: `const conn = connFor(projectId);` immediately before that call, and use `conn.sendCancellable(...)` there, so the `finally` refreshes on the same connection the turn went out on.

In `startSession` — the path behind the renderer's `newSession` and `openSession` messages — after the `header` post (`toRenderer({ type: "header", sessionId: st.sessionId });` inside its `if (projectId === currentProjectId)`) and before `await refreshSessionList(projectId);`, add:

```js
      // A new or reopened session cleared the view above; give it the new
      // session's log, or the "nothing yet" answer, rather than leaving it
      // on "Loading trajectory…" until the next turn ends. Not awaited, for
      // the same reason as activateProject: it must not hold up
      // refreshSessionList below, and refreshTrajectory carries its own
      // stale guard for whenever it resolves.
      void refreshTrajectory(projectId, conn, st.sessionId);
```

`refreshTrajectory` carries its own stale guard, so no extra `currentProjectId` check is needed around the call. Without this hook the pane would stay on "Loading trajectory…" after every session switch within a project: `clearMessages` resets the view and nothing repopulates it.

- [ ] **Step 4: Forward live notifications for the on-screen project**

In `ui/web/src/20-adapter-events.js`, at the top of `handleNotification(projectId, msg)` — which `noteProjectEvent` already calls only for `currentProjectId` — before the existing `if (msg.method === "exec/output_chunk")`:

```js
    // The trajectory sees the raw notification, before the chat's lossy
    // translation below: one live row per event, reconciled against the log
    // when the turn ends. Only these four methods are recorded by the core's
    // tee, so only these four are forwarded.
    if (
      msg.method === "agent/event" ||
      msg.method === "exec/output_chunk" ||
      msg.method === "workflow/stage_start" ||
      msg.method === "workflow/stage_done"
    ) {
      toRenderer({ type: "trajectoryEvent", event: { type: msg.method, data: msg.params || {} } });
    }
```

- [ ] **Step 5: Run everything**

From the repo root: `node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/check-web.mjs && node ui/web/scripts/adapter-test.mjs`

Expected: all green; 41 tests.

- [ ] **Step 6: Commit**

```bash
git add ui/web/src/10-adapter-session.js ui/web/src/20-adapter-events.js ui/web/scripts/adapter-test.mjs ui/web/static/web.bundle.js
git commit -m "feat(web): feed the Trajectory view — fetch on switch, forward live, re-fetch on turn end

activateProject reads session.trajectory after session.get and hands it to
the renderer, with the same stale-response guard the repaint has: an answer
that arrives after the user switched away belongs to a project no longer on
screen. handleNotification forwards the four recorded methods raw, before
the chat's lossy translation, for the on-screen project only. When a turn
ends the log is re-read, so the live rows give way to recorded ones with the
core's own timings.

Switching into a project mid-turn now replays what streamed while it was in
the background — the symptom that motivated C2 — and that has its own test.

startSession refreshes too: a new or reopened session cleared the view, and
the pane would otherwise sit on its loading line until the next turn ended.
So does onConnected, for the first session of a connection: without it the
pane was never populated on first load."
```

---

### Task 4: The VS Code host — the same three hooks, and the acceptance check

C2's acceptance condition is the inverse of C1's: **the trajectory must work in the VS Code webview.** This task wires the extension and verifies it by running it.

**Files:**
- Modify: `ui/vscode/src/coreSession.ts` (a `sessionTrajectory` wrapper beside `sessionGet`, ~line 375)
- Modify: `ui/vscode/src/chat/panel.ts` (subscriptions ~line 122-131; history load ~line 607; `forwardAgentEvent` ~line 1548; turn `finally` ~line 1383)

**Interfaces:**
- Consumes: `TrajectoryEvent` and the two `HostToWebview` members (Task 2); `this.client.request(method, params, timeoutMs)` on `CoreSession`; `this.post(msg)` on the panel.
- Produces: `CoreSession.sessionTrajectory(sessionId?: string): Promise<{ recorded: boolean; events: TrajectoryEvent[] }>`; `ChatPanel.refreshTrajectory(sessionId: string): Promise<void>` (private).

- [ ] **Step 1: The RPC wrapper**

In `ui/vscode/src/coreSession.ts`, add after the `sessionGet` method (the one that requests `"session.get"` at ~line 386 and returns `{ sessionId, … }`):

```ts
  /**
   * The session's append-only event log, as the core recorded it.
   * recorded:false means the session predates the log, which is not the same
   * as a log with no events — the view says which.
   */
  async sessionTrajectory(sessionId?: string): Promise<{ recorded: boolean; events: TrajectoryEvent[] }> {
    if (!this.client) {
      throw new Error("core client missing");
    }
    const id = (sessionId || this.sessionId || "").trim();
    if (!id) {
      throw new Error("session_id required");
    }
    const result = (await this.client.request("session.trajectory", { session_id: id }, 30_000)) as {
      recorded?: boolean;
      events?: TrajectoryEvent[];
    };
    return {
      recorded: Boolean(result.recorded),
      events: Array.isArray(result.events) ? result.events : [],
    };
  }
```

Import `TrajectoryEvent` from `./protocol/events` alongside the file's existing imports from that module. If `this.sessionId` is not the field `sessionGet` reads (check line ~382: `(sessionId || this.sessionId || "")`), use whatever that line uses.

- [ ] **Step 2: The panel — fetch, forward, re-fetch**

In `ui/vscode/src/chat/panel.ts`:

**a. A private method**, near `forwardAgentEvent`:

```ts
  /**
   * Replace the Trajectory view from the core's log. A failed fetch is
   * reported as unavailable, never as "not recorded" — those are different
   * answers, and the spec forbids the view from guessing.
   */
  private async refreshTrajectory(sessionId: string): Promise<void> {
    if (!sessionId) {
      return;
    }
    try {
      const res = await this.session.sessionTrajectory(sessionId);
      this.post({ type: "trajectory", recorded: res.recorded, events: res.events });
    } catch (err) {
      this.post({
        type: "trajectory",
        recorded: true,
        events: [],
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }
```

`this.session` is the panel's `CoreSession`; if the field has another name, use it.

**b. After the history is posted** — find `this.post({ type: "history", messages: history });` (~line 607, inside the `if (history.length > 0)`), and after the closing brace of that `if`, add:

```ts
    // Not awaited: the tab bar and step-usage posts below must not wait on the
    // log fetch. refreshTrajectory drops its answer if the session changed
    // meanwhile, so a late reply lands right or not at all.
    void this.refreshTrajectory(view.sessionId);
```

`view` is the `sessionGet` result already in scope there (it has `sessionId`, `uiMessages`, `costUSD`).

**c. Forward live events.** At the top of `forwardAgentEvent(event, render)`, immediately after the `post` closure is defined and before `const childCtx`, add:

```ts
    // The trajectory sees every event raw, including child scope, before the
    // chat's translation below. Reconciled from the log when the turn ends.
    post({ type: "trajectoryEvent", event: { type: "agent/event", data: event } });
```

In the constructor's subscriptions (~line 126-131), extend `onExecChunk` and `onWorkflow`:

```ts
    const onExecChunk = (payload: { step: number; chunk: string }): void => {
      this.post({ type: "trajectoryEvent", event: { type: "exec/output_chunk", data: payload } });
      this.post({ type: "execChunk", step: payload.step, chunk: payload.chunk });
    };
    const onWorkflow = (phase: "start" | "done", stage: WorkflowStagePayload): void => {
      this.post({
        type: "trajectoryEvent",
        event: { type: phase === "start" ? "workflow/stage_start" : "workflow/stage_done", data: stage },
      });
      this.post({ type: "workflowStage", phase, stage });
    };
```

**d. Re-fetch when the turn ends.** In the `finally` that posts `turnComplete` (~line 1381-1384), after `this.post({ type: "turnComplete", ok, queuedNext });`, add:

```ts
      // session.message has returned, so the core has closed the writer and
      // the log is complete: replace the live rows with the recorded ones.
      void this.refreshTrajectory(this.session.sessionId ?? "");
```

If `sessionId` on `CoreSession` is private, add a public getter `get currentSessionId(): string | undefined { return this.sessionId; }` to `CoreSession` and use it here; do not widen the field's visibility.

- [ ] **Step 3: Compile and run every check**

From `ui/vscode`: `npm run compile && npm run check:webview`.
From the repo root: `node ui/web/scripts/check-web.mjs && node ui/web/scripts/adapter-test.mjs`.

Expected: all green. `tsc` will name any field or method whose name the brief guessed wrong; fix the reference to the real name and say so in the report.

- [ ] **Step 4: The acceptance check — run it in VS Code, and write down what happened**

This is C2's acceptance condition and it cannot be delegated to a test. Press F5 in `ui/vscode` (Extension Development Host) against a workspace that has an `.orchestra.yml` and a session with a recorded log (any session that has had a turn since C2a merged; `ls .orchestra/sessions/*.events.jsonl`).

Record in your report, as observations not as intentions:
1. The header shows **Chat | Trajectory** beside the session tabs, Chat selected.
2. Clicking **Trajectory** shows rows for the recorded session: at least one `turn`, its `step`s, and a tool call with a duration. Say what the first three rows read.
3. Clicking a tool row expands its input and output in place; clicking again collapses it.
4. Send a message. While it runs, live rows appear with " · live" and blank durations. When it finishes, the rows are replaced and the durations are filled in.
5. Open a session that predates the log (`session.list` from before C2a merged, or delete a sidecar in a scratch workspace): the pane says "No trajectory was recorded for this session — it predates the log."
6. `Ctrl+Shift+P → Developer: Toggle Developer Tools` shows no error in the webview console.

If step 4's live rows never appear, the `render` gate in `forwardAgentEvent` or the message order is wrong — report it, do not work around it. If step 5 shows "Loading trajectory…" forever, the fetch failed silently — the `catch` in `refreshTrajectory` should have posted `error`; find out why it did not.

- [ ] **Step 5: Commit**

```bash
git add ui/vscode/src/coreSession.ts ui/vscode/src/chat/panel.ts
git commit -m "feat(vscode): feed the Trajectory view from the extension host

sessionTrajectory wraps the RPC beside sessionGet. The panel fetches the log
after posting history, forwards every agent/event, exec chunk and workflow
stage raw as trajectoryEvent before the chat's translation, and re-fetches
when the turn ends so the recorded rows replace the live ones. A failed
fetch is reported as unavailable, never as not recorded.

Verified in the Extension Development Host: the switch, the rows, the
expand, live rows giving way to recorded timings, and the predates-the-log
sentence. C2's acceptance condition is the inverse of C1's — the extension
changes on purpose, and the trajectory works in its webview."
```

---

### Task 5: The core says "nothing yet" for a session that has had no turn

Found by driving the built page against a real core: a session created seconds earlier rendered "No trajectory was recorded for this session — it predates the log." The sidecar is created by the first agent launch, so `trajectory.Read` reports `recorded:false` for every session that has not had a turn yet, and the view says the one thing that is untrue. A C2a semantics gap; this task closes it in the core, where the two cases can be told apart.

**Files:**
- Modify: `internal/core/trajectory_rpc.go` (`SessionTrajectory`, the `!recorded` branch and the doc comment)
- Modify: `internal/core/trajectory_rpc_test.go` (two tests)
- Modify: `docs/PROTOCOL.md` (the `recorded` bullet under `session.trajectory`)

**Interfaces:**
- Consumes: `trajectory.Read(root, id) (events, recorded, err)`; `c.sessions.GetOrLoad(root, id) (*coresession.Session, error)` — returns `"session not found"` for an id that is neither in memory nor on disk; `sess.History` (field) and `sess.UIMessages()` under `sess.Lock()`.
- Produces: unchanged wire shape `{recorded, events[]}`. `ProtocolVersion` stays 16 — only the answer for one state changes, and no client parsed it differently.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/trajectory_rpc_test.go`:

```go
func TestSessionTrajectory_FreshSessionWithNoTurnIsRecordedAndEmpty(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: started.SessionID})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if !res.Recorded {
		t.Error("Recorded = false, want true — a session with no turn yet has had nothing to record; it does not predate the log")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}

func TestSessionTrajectory_SessionWithHistoryAndNoLogPredatesTheLog(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	sess := c.sessions.CreateWithID("old-chat")
	sess.Lock()
	sess.History = append(sess.History, llm.Message{Role: llm.RoleUser, Content: "hello from before the log"})
	sess.Unlock()
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: "old-chat"})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if res.Recorded {
		t.Error("Recorded = true, want false — this session has history but no log, so it predates the log")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}
```

Add `"github.com/orchestra/orchestra/llm"` to the file's imports.

- [ ] **Step 2: Run them and watch the first fail**

Run: `go test ./internal/core -run 'TestSessionTrajectory' -v`

Expected: `TestSessionTrajectory_FreshSessionWithNoTurnIsRecordedAndEmpty` FAILS with "Recorded = false, want true". The other four (three existing, one new) pass — the "predates" test passes already because today every missing sidecar is `false`; it exists to pin that answer once the fresh-session case flips.

- [ ] **Step 3: Tell the two cases apart**

In `internal/core/trajectory_rpc.go`, replace the doc comment on `SessionTrajectory` and the body after `trajectory.Read`:

```go
// SessionTrajectory returns the append-only event log for a session.
//
// A missing sidecar is not one answer but two. A session this core knows — in
// memory or on disk — whose history and UI messages are both empty has had no
// turn, and the first turn is what creates the sidecar: nothing has happened, and
// nothing was missed, so that is Recorded true with no events. A session with
// history and no sidecar was recorded by a core that predates the log, and a
// session id that names nothing is treated the same way: Recorded false. Only a
// blank id is rejected, because that is a malformed request rather than a
// question about a session.
func (c *Core) SessionTrajectory(p SessionTrajectoryParams) (*SessionTrajectoryResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	id := strings.TrimSpace(p.SessionID)
	if id == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "session_id is empty", nil)
	}
	events, recorded, err := trajectory.Read(c.workspaceRoot, id)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": id})
	}
	if !recorded {
		// No sidecar yet. If the session exists and is empty, the log is
		// simply not born: the first agent launch creates it. Saying
		// "predates the log" here would be untrue of every new chat.
		if sess, lookErr := c.sessions.GetOrLoad(c.workspaceRoot, id); lookErr == nil && sess != nil {
			sess.Lock()
			empty := len(sess.History) == 0 && len(sess.UIMessages()) == 0
			sess.Unlock()
			if empty {
				recorded = true
			}
		}
	}
	// Never nil: `events` marshals to `null` when nil, and a client that reads
	// `events.length` would fault on it. An empty log is `[]`.
	if events == nil {
		events = []trajectory.Event{}
	}
	return &SessionTrajectoryResult{Recorded: recorded, Events: events}, nil
}
```

`GetOrLoad` is the right lookup, not `LoadOrCreate`: it must never create a session as a side effect of asking about one. Its "session not found" error is the unknown-id case and keeps `recorded` false.

- [ ] **Step 4: Run the package**

Run: `go test ./internal/core -run 'TestSessionTrajectory' -v` then `go vet ./internal/core && go test ./internal/core`

Expected: all five `TestSessionTrajectory_*` pass; the package is green.

- [ ] **Step 5: Say it in the protocol doc**

In `docs/PROTOCOL.md`, under `session.trajectory` → Response `result`, replace the `recorded` bullet with:

```markdown
- `recorded` (bool) — `false` означает, что для этой сессии лога нет и не будет: сессия либо создана до появления этой фичи (у неё есть история, но нет sidecar-файла), либо не существует. Сессия, у которой ещё не было ни одного хода, — это `recorded: true, events: []`: лог заводится первым запуском агента, и до этого «ничего не произошло» — правда, а «сессия старше лога» — нет. Клиент должен показать разные сообщения для `false` и для пустого `true`, а не одну пустую таблицу.
```

- [ ] **Step 6: Commit**

```bash
git add internal/core/trajectory_rpc.go internal/core/trajectory_rpc_test.go docs/PROTOCOL.md
git commit -m "fix(core): a session with no turn yet is recorded and empty, not older than the log

The sidecar is created by the first agent launch, so trajectory.Read said
recorded:false for every new chat and the view told the user the session
predated the log. When the sidecar is missing, session.trajectory now looks
the session up: known and empty means nothing has happened; known with
history means it predates the log; unknown stays false. Wire shape unchanged."
```

**Known limits.** A session whose only content is todos or a plan path (no history, no UI messages) also reads as "nothing yet" — correct, since no turn ran. A session started by a detached core and not yet snapshotted is unknown here and reads `false` until its snapshot lands; the pane says "predates" for the seconds between. Acceptable; noted for the follow-ups.

---

## Self-Review

**1. Spec coverage.**

- "A segmented control in the existing header switches the message area between Chat and Trajectory" — Task 2 (control, `data-view`, CSS exclusivity).
- "Rows nest by indentation: turn, step, tool call, sub-tool call" — Task 1 (`depth` 0–3; child tools nest under `parent_tool_call_id`).
- "Each row shows a time offset from the turn's start, a glyph for its kind, its name, and its duration; token columns appear when a number is actually known" — Task 1 computes `offsetMs`/`durationMs`/tokens from `step_usage` only; Task 2 renders blanks for unknowns. Fixture 7 asserts an estimate leaves the columns blank; fixture 9 asserts live rows have blank times.
- "Clicking a row expands its input and output in place" — Task 2 (`traj-expandable`, keyboard too).
- "Kind is carried by glyph and indentation first, colour second" — Task 2 CSS: glyph column and depth padding are structural; colour is applied only to glyph/label accents.
- "Switching projects mid-turn replays the log rather than showing a gap" — Task 3's fourth test, named for the symptom.
- "A session with no sidecar … says the log was not recorded, in words, in place" — Task 2 empty state; Task 3/4 pass `recorded` through untouched; a fetch failure is a third, distinct sentence so the view never claims "predates" when it means "could not read".
- "An event with an unknown type must render as a plain row" — fixture 6, both an unknown kind and an unknown method.
- "Rows with no known token count must render blank; no number is invented" — fixture 7 and the renderer's `traj-tok` rule.
- "The timeline is a pure function … tested as one: fixtures in, expected row tree out. Interleaved tool calls, a retry after recoverable_error, a turn cut short, a workflow's stages" — fixtures 2, 3, 4, 5 respectively.
- "The trajectory works in the VS Code webview" — Task 4 Step 4, recorded as observations.
- Non-goals honoured: no plugin attribution (the `source` field is carried but never read), no derived projections (the view reads only the log), no synthesised log, no estimates.

**2. Placeholder scan.** No TBD/TODO. Every code step has its code. Names the brief could not verify are flagged as such with the instruction to use the real one and report it (`this.session`, `conn` in `sendTurn`, `this.sessionId` visibility), which is the honest shape when a field's visibility cannot be read from a grep; each such note says exactly what to check.

**3. Type consistency.**

- `buildTrajectoryTree(events) → TrajRow[]` — defined Task 1, called Task 2 (`renderTrajectory`).
- `replaceTrajectory(recorded, events, error)` / `appendTrajectoryEvent(ev)` / `resetTrajectory()` — defined Task 2 Step 4, called Task 2 Step 5.
- Renderer message shapes `{type:"trajectory", recorded, events, error?}` and `{type:"trajectoryEvent", event:{type,data}}` — declared Task 2 Step 1 (`events.ts`), produced by Task 3 (`refreshTrajectory`, `handleNotification`) and Task 4 (`refreshTrajectory`, `forwardAgentEvent`, `onExecChunk`, `onWorkflow`), consumed by Task 2 Step 5 with the same field names.
- DOM ids `view-chat-btn`, `view-trajectory-btn`, `trajectory-summary`, `trajectory-rows`, `app` — created Task 2 Step 6 in both pages, looked up Task 2 Step 4, asserted Task 2 Step 2 and Task 3's fourth test.
- `sessionTrajectory` returns `{recorded, events}` — Task 4 Step 1 defines, Step 2a consumes.
- Summary text formats — `"N turn(s) · M rows[ · L live]"`, `"Loading trajectory…"`, `"Nothing has happened in this session yet."`, `"No trajectory was recorded for this session — it predates the log."`, `"Trajectory unavailable: …"` — produced in Task 2 Step 4, matched by regexps in Task 2 Step 2 and Task 3's fourth test. Fixture arithmetic: two events (start+completed) → turn, step, tool = 3 rows; appending a step-2 tool start adds a step row and a tool row = 5, both live = "2 live".

**Known limits, stated rather than hidden:**
- A turn that was cancelled and a turn that is still running look the same in a recorded log (`outcome: "open"`): C2a chose not to add a boundary event (follow-ups item 11). The view claims nothing it cannot know.
- Depth-1 rows (steps, stages, routes) come out in first-appearance order. For a recorded log that is time order; for a live tail it is arrival order. An earlier draft listed stages before all steps and the Task 1 review showed offsets going backwards down the page.
- The renderer rebuilds all rows on every live event (coalesced to one frame by `requestAnimationFrame`). The core already debounces deltas; if a very long session makes this visible, keyed reconciliation is the fix and it lives entirely inside `renderTrajectory`.
