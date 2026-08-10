import { strict as assert } from "node:assert";
import { describe, it } from "node:test";
import {
  diffFromToolArgs,
  effectiveContextLimit,
  estimatePromptTokensFromUI,
  joinAssistantStreamSegments,
  sumCompletionTokensFromUI,
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
