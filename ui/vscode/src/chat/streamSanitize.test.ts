import { strict as assert } from "node:assert";
import { describe, it } from "node:test";
import { stripFinalEnvelope, sanitizeAssistantStream, shouldSuppressStreamChunk, looksLikeCorruptedStream, isBenignTurnError } from "./streamSanitize";

describe("stripFinalEnvelope", () => {
  it("removes balanced patches envelope", () => {
    const in_ = 'done {"type":"final","final":{"patches":[{"x":1}]}} ok';
    const got = stripFinalEnvelope(in_);
    assert.ok(!got.includes("patches"));
    assert.ok(got.includes("done"));
    assert.ok(got.includes("ok"));
  });

  it("hides unbalanced patches tail while streaming", () => {
    const in_ = 'answer\n{"type":"final","final":{"patches":[';
    assert.equal(stripFinalEnvelope(in_), "answer");
  });

  it("removes loose empty patches object", () => {
    const in_ = "Привет!\n\nЧто нужно?\n\n{\"patches\":[]}";
    const got = stripFinalEnvelope(in_);
    assert.ok(!got.includes("patches"));
    assert.ok(got.includes("Привет"));
  });
});

describe("sanitizeAssistantStream", () => {
  it("strips embedding-like numeric tail", () => {
    const garbage =
      '"Serving user request: add comment to App.jsx"1.2002003004005006007008009001001100120013001400150016001700180019002000';
    const got = sanitizeAssistantStream(garbage);
    assert.ok(!got.includes("1.200200"));
    assert.ok(got.includes("Serving user request") || got === "");
  });

  it("suppresses pure numeric chunks", () => {
    assert.equal(shouldSuppressStreamChunk("00000000000000000000"), true);
    assert.equal(shouldSuppressStreamChunk("hello"), false);
  });

  it("detects corrupted accumulated stream", () => {
    const s = "x".repeat(50) + "0".repeat(60);
    assert.equal(looksLikeCorruptedStream(s), true);
  });
});

describe("isBenignTurnError", () => {
  it("treats intentional cancel as benign", () => {
    assert.equal(isBenignTurnError("SSE read error: context canceled"), true);
    assert.equal(isBenignTurnError("request cancelled"), true);
    assert.equal(isBenignTurnError("connection reset"), false);
  });
});
