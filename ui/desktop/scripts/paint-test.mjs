// Tests the painter the developer tools are given, without a browser.
//
// The painter is one IIFE that reads the tools' colour tokens and restates
// them in the app's palette. The reading needs a document; the deciding does
// not, and that is the part worth testing: what it makes of a colour, and
// which of ours each of theirs becomes.
//
// Run: node ui/desktop/scripts/paint-test.mjs

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const src = fs.readFileSync(path.join(__dirname, "..", "devtools-paint.js"), "utf8");

function load() {
  const sandbox = { setTimeout, clearTimeout };
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(src, sandbox);
  return sandbox.__orchestraPaint;
}

/** A value from the other realm, as a plain one of ours. */
const plain = (x) => JSON.parse(JSON.stringify(x));

// The dark palette as the app measures it (ui/vscode/media/chat.css).
const DARK = {
  bg: "#151517",
  surface: "#1f1f23",
  surface2: "#26262b",
  border: "#2b2b30",
  muted2: "#5c5c64",
  muted: "#86868d",
  fgDim: "#b3b3b8",
  fg: "#e7e7ea",
  accent: "#bb9af7",
};

test("a colour is read in every spelling the tools use", () => {
  const paint = load();
  assert.deepEqual(plain(paint.parse("#333")), { r: 51, g: 51, b: 51, a: 1 });
  assert.deepEqual(plain(paint.parse("#242424")), { r: 36, g: 36, b: 36, a: 1 });
  assert.deepEqual(plain(paint.parse("rgb(40 40 40 / 100%)")), { r: 40, g: 40, b: 40, a: 1 });
  assert.deepEqual(plain(paint.parse("rgb(31 31 31 / 16%)")), { r: 31, g: 31, b: 31, a: 0.16 });
  assert.deepEqual(plain(paint.parse("rgba(255, 255, 255, 0.04)")), { r: 255, g: 255, b: 255, a: 0.04 });
  assert.equal(paint.parse("#1a1a1a38").a, 0x38 / 255);
  assert.equal(paint.parse("color-mix(in srgb, red, blue)"), null, "what is not a colour is left alone");
  assert.equal(paint.parse("transparent"), null);
});

test("their greys become ours by depth, their blue becomes our accent, meaning is kept", () => {
  const paint = load();
  // Chromium's dark neutrals, as the tools define them (rgb 31 … 227), with
  // the tokens whose treatment matters most named as themselves.
  const originals = {};
  for (const v of [0, 31, 36, 40, 43, 51, 60, 94, 117, 143, 171, 199, 227, 255]) {
    originals[`--grey-${v}`] = paint.parse(`rgb(${v} ${v} ${v})`);
  }
  // The two the ladder hangs from: their panel ground and their text.
  originals["--sys-color-cdt-base-container"] = paint.parse("rgb(40 40 40)");
  originals["--sys-color-on-surface"] = paint.parse("rgb(227 227 227)");
  originals["--sys-color-state-hover-on-subtle"] = paint.parse("rgb(253 252 251 / 10%)");
  originals["--sys-color-primary"] = paint.parse("rgb(168 199 250)");
  originals["--sys-color-tonal-container"] = paint.parse("rgb(0 74 119)");
  originals["--sys-color-token-tag"] = paint.parse("rgb(124 172 248)");
  originals["--sys-color-error"] = paint.parse("rgb(242 184 181)");
  originals["--sys-color-green"] = paint.parse("rgb(109 213 140)");
  const out = Object.fromEntries(plain(paint.mapping(originals, DARK)));

  // Their panel ground is our ground and their text is our text, whatever
  // the numbers; what is darker than their ground is our ground too, what
  // is lighter than their text is our text, and what lies between climbs our
  // ladder in order, never skipping back.
  assert.equal(out["--sys-color-cdt-base-container"], "rgb(21 21 23)");
  assert.equal(out["--grey-40"], "rgb(21 21 23)", "the same grey lands in the same place");
  assert.equal(out["--grey-0"], "rgb(21 21 23)", "nothing is darker than the ground");
  assert.equal(out["--grey-31"], "rgb(21 21 23)");
  assert.equal(out["--sys-color-on-surface"], "rgb(231 231 234)");
  assert.equal(out["--grey-255"], "rgb(231 231 234)", "nothing is lighter than the text");
  assert.equal(out["--grey-60"], "rgb(43 43 48)", "an elevated panel is a step up the ladder");
  const light = (s) => Number(/\d+/.exec(s)[0]);
  const ladder = [0, 31, 36, 40, 43, 51, 60, 94, 117, 143, 171, 199, 227, 255].map((v) => light(out[`--grey-${v}`]));
  for (let i = 1; i < ladder.length; i++) {
    assert.ok(ladder[i] >= ladder[i - 1], `the order is kept: ${ladder.join(" ")}`);
  }
  // A wash keeps its transparency.
  assert.equal(out["--sys-color-state-hover-on-subtle"], "rgb(231 231 234 / 0.1)");

  // Blue that stands off the ground is the accent; blue near it is a tint.
  assert.equal(out["--sys-color-primary"], "rgb(187 154 247)");
  assert.equal(out["--sys-color-tonal-container"], "rgb(79 68 101)");

  // Syntax and the semantic hues are not ours to change.
  assert.equal(out["--sys-color-token-tag"], undefined);
  assert.equal(out["--sys-color-error"], undefined);
  assert.equal(out["--sys-color-green"], undefined);
});

test("a palette that cannot be read paints nothing", () => {
  const paint = load();
  assert.deepEqual(plain(paint.mapping({ "--x": paint.parse("#333") }, {})), []);
  assert.deepEqual(plain(paint.mapping({ "--x": paint.parse("#333") }, { bg: "nope", fg: "#fff" })), []);
});
