import { strict as assert } from "node:assert";
import { describe, it } from "node:test";
import {
  diffFromToolArgs,
  effectiveContextLimit,
  estimatePromptTokensFromUI,
  joinAssistantStreamSegments,
  sumCompletionTokensFromUI,
  toolBlocksFromUIMessage,
  buildAssistantProjection,
} from "./turnProjection";

describe("joinAssistantStreamSegments", () => {
  it("joins committed segments and current stream", () => {
    assert.equal(
      joinAssistantStreamSegments(["step one"], "step two"),
      "step one\n\nstep two"
    );
  });

  it("returns only current when no segments", () => {
    assert.equal(joinAssistantStreamSegments([], "live"), "live");
  });
});

describe("diffFromToolArgs", () => {
  it("extracts write content", () => {
    const d = diffFromToolArgs("write", JSON.stringify({ path: "a.go", content: "package main\n" }));
    assert.deepEqual(d, { before: "", after: "package main\n" });
  });

  it("extracts edit search/replace", () => {
    const d = diffFromToolArgs(
      "edit",
      JSON.stringify({ path: "b.ts", search: "old", replace: "new" })
    );
    assert.deepEqual(d, { before: "old", after: "new" });
  });
});

describe("estimatePromptTokensFromUI", () => {
  it("includes fixed overhead for non-empty chat", () => {
    const est = estimatePromptTokensFromUI([
      { role: "user", text: "Привет" },
      { role: "assistant", text: "Здравствуй!" },
    ]);
    assert.ok(est > 8000, `expected overhead+content, got ${est}`);
  });

  it("returns 0 for empty history", () => {
    assert.equal(estimatePromptTokensFromUI([]), 0);
  });
});

describe("effectiveContextLimit", () => {
  it("prefers discovered context over config", () => {
    assert.equal(effectiveContextLimit(20000, 51200), 51200);
  });

  it("falls back to num_ctx", () => {
    assert.equal(effectiveContextLimit(20000, 0), 20000);
  });
});

describe("sumCompletionTokensFromUI", () => {
  it("sums assistant outputs", () => {
    const n = sumCompletionTokensFromUI([
      { role: "assistant", text: "hello world" },
    ]);
    assert.ok(n > 0);
  });
});

describe("tool durations survive the round trip", () => {
  // sessionfile.UIToolBlock carries duration_ms and the TUI fills it
  // (internal/uimodel/convert.go). The extension declared the field and then
  // dropped it on the way in and on the way out, so a session's tool timings
  // were invisible in the webview even though they were sitting on disk.
  it("carries duration_ms out of tool_blocks", () => {
    const blocks = toolBlocksFromUIMessage({
      role: "assistant",
      tool_blocks: [{ name: "read", status: "completed", duration_ms: 1240 }],
    });
    assert.equal(blocks.length, 1);
    assert.equal(blocks[0].duration_ms, 1240);
  });

  it("carries duration_ms out of segments", () => {
    const blocks = toolBlocksFromUIMessage({
      role: "assistant",
      segments: [
        { kind: "tools", tools: [{ name: "bash", status: "completed", duration_ms: 91400 }] },
      ],
    });
    assert.equal(blocks.length, 1);
    assert.equal(blocks[0].duration_ms, 91400);
  });

  it("writes duration_ms into the projection", () => {
    const tools = new Map([
      [
        "c1",
        {
          id: "c1",
          name: "read",
          argsRaw: "",
          status: "completed" as const,
          result: "ok",
          durationMs: 350,
        },
      ],
    ]);
    const proj = buildAssistantProjection({
      text: "done",
      reasoning: "",
      tools,
      promptCtx: 0,
      tokensIn: 0,
      tokensOut: 0,
    });
    assert.equal(proj?.tool_blocks?.[0].duration_ms, 350);
  });

  // A tool that was never timed must persist no duration at all, rather than a
  // zero that the webview would render as "0ms".
  it("omits duration_ms when the tool was not timed", () => {
    const tools = new Map([
      ["c1", { id: "c1", name: "read", argsRaw: "", status: "completed" as const, result: "ok" }],
    ]);
    const proj = buildAssistantProjection({
      text: "done",
      reasoning: "",
      tools,
      promptCtx: 0,
      tokensIn: 0,
      tokensOut: 0,
    });
    assert.equal(proj?.tool_blocks?.[0].duration_ms, undefined);
  });
});
