import * as assert from "assert";
import * as vscode from "vscode";
import { alignDiffLines, buildHighlightPlan } from "./changeHighlights";

function mockDoc(text: string): vscode.TextDocument {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  return {
    getText: () => text.replace(/\r\n/g, "\n"),
    lineCount: lines.length,
    lineAt: (line: number) => ({
      text: lines[line] ?? "",
      range: new vscode.Range(line, 0, line, (lines[line] ?? "").length),
    }),
  } as vscode.TextDocument;
}

function testAlignAppend() {
  const rows = alignDiffLines("a\nb", "a\nb\nc\nd");
  assert.strictEqual(rows.filter((r) => r.type === "add").length, 2);
}

function testHighlightAfterApplied() {
  const before = "one\ntwo";
  const after = "one\ntwo\nthree";
  const doc = mockDoc(after);
  const plan = buildHighlightPlan(doc, before, after);
  assert.strictEqual(plan.showsAfterContent, true);
  assert.ok(plan.addedRanges.length >= 1);
  assert.strictEqual(plan.addedRanges[0].start.line, 2);
}

function testHighlightPendingAppendGhost() {
  const before = "one\ntwo";
  const after = "one\ntwo\nthree";
  const doc = mockDoc(before);
  const plan = buildHighlightPlan(doc, before, after);
  assert.strictEqual(plan.showsAfterContent, false);
  assert.strictEqual(plan.ghostDecorations.length, 1);
  assert.match(String(plan.ghostDecorations[0].renderOptions?.after?.contentText), /three/);
}

testAlignAppend();
testHighlightAfterApplied();
testHighlightPendingAppendGhost();

console.log("changeHighlights.test.ts OK");
