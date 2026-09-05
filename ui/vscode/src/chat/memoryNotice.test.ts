import { strict as assert } from "node:assert";
import { describe, it } from "node:test";
import { memoryNoticeText } from "./memoryNotice";

// Mirrors ui/tui/app_rpc.go noticeTurnMemory: the field run's memory
// failures went silently to stderr for nine days, so a failed write must be
// loud (with its reason); a written note gets one short line naming its
// source; a skip stays silent — most turns change nothing, and saying so
// every time would be the grep-noise problem again, in a different UI.

describe("memoryNoticeText", () => {
  it("returns null when there is no memory status", () => {
    assert.equal(memoryNoticeText(undefined), null);
    assert.equal(memoryNoticeText(null), null);
  });

  it("stays silent on a skip", () => {
    assert.equal(memoryNoticeText({ outcome: "skipped", detail: "turn changed no files" }), null);
  });

  it("names the source on a written note", () => {
    // Same wording as ui/tui/app_rpc.go noticeTurnMemory — one concept,
    // presented the same way in both clients.
    const modelText = memoryNoticeText({ outcome: "written", source: "model" });
    assert.ok(modelText && /сводка модели/.test(modelText));

    const digestText = memoryNoticeText({ outcome: "written", source: "digest" });
    assert.ok(digestText && /дайджеста/.test(digestText));
    assert.notEqual(modelText, digestText);
  });

  it("surfaces the reason on a failure", () => {
    const text = memoryNoticeText({ outcome: "failed", detail: "write agent.md: permission denied" });
    assert.ok(text && text.includes("permission denied"));
  });
});
