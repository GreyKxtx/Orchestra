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

test("garbage in does not throw: non-array, null events, events without data", () => {
  assert.deepEqual(buildTrajectoryTree(null), []);
  assert.deepEqual(buildTrajectoryTree(undefined), []);
  const rows = buildTrajectoryTree([null, {}, { type: "agent/event" }, ev("agent/event", null, 1, 1)]);
  assert.ok(Array.isArray(rows));
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
