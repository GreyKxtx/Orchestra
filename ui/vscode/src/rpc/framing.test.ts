import { strict as assert } from "node:assert";
import { test } from "node:test";
import { encodeMessage, FrameDecoder } from "./framing";

test("decodes a single frame", () => {
  const d = new FrameDecoder();
  const out = d.push(encodeMessage('{"a":1}'));
  assert.deepEqual(out, ['{"a":1}']);
});

test("decodes frames split across chunks", () => {
  const d = new FrameDecoder();
  const buf = encodeMessage('{"hello":"world"}');
  const first = d.push(buf.subarray(0, 10));
  assert.deepEqual(first, []);
  const rest = d.push(buf.subarray(10));
  assert.deepEqual(rest, ['{"hello":"world"}']);
});

test("resyncs after non-framed garbage before a valid frame", () => {
  const d = new FrameDecoder();
  const diags: string[] = [];
  d.onDiagnostic = (m) => diags.push(m);
  const garbage = Buffer.from("panic: something went wrong\r\n\r\n", "utf8");
  const out = d.push(Buffer.concat([garbage, encodeMessage('{"ok":true}')]));
  assert.deepEqual(out, ['{"ok":true}']);
  assert.ok(diags.length > 0, "expected a framing diagnostic");
});

test("decoder is not poisoned: next frames decode after garbage", () => {
  const d = new FrameDecoder();
  d.onDiagnostic = () => undefined;
  d.push(Buffer.from("plain stdout noise\r\n\r\nmore noise\r\n\r\n", "utf8"));
  const out = d.push(encodeMessage('{"n":2}'));
  assert.deepEqual(out, ['{"n":2}']);
});

test("rejects absurd Content-Length and resyncs", () => {
  const d = new FrameDecoder();
  const diags: string[] = [];
  d.onDiagnostic = (m) => diags.push(m);
  const evil = Buffer.from("Content-Length: 999999999999\r\n\r\n", "utf8");
  const out = d.push(Buffer.concat([evil, encodeMessage('{"ok":1}')]));
  assert.deepEqual(out, ['{"ok":1}']);
  assert.ok(diags.some((m) => m.includes("Content-Length")));
});

test("decodes multiple frames from one chunk", () => {
  const d = new FrameDecoder();
  const out = d.push(Buffer.concat([encodeMessage("{}"), encodeMessage('{"b":2}')]));
  assert.deepEqual(out, ["{}", '{"b":2}']);
});
