// Tests the injected element picker without a browser.
//
// The picker is one IIFE over `globalThis.document` / `location`, so a small
// fake tree is enough to drive the parts worth testing: the selector it builds
// for an element, the markup it trims, the CSS rules it keeps, and the
// arm/click/take cycle Rust drives.
//
// Run: node ui/desktop/scripts/picker-test.mjs

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const src = fs.readFileSync(path.join(__dirname, "..", "picker.js"), "utf8");

/** A DOM node that answers the handful of things the picker asks of one. */
function el(tag, opts = {}) {
  const node = {
    nodeType: 1,
    tagName: tag.toUpperCase(),
    id: opts.id || "",
    attrs: { ...(opts.attrs || {}) },
    children: [],
    childNodes: [],
    parentElement: null,
    matchesList: opts.matches || [],
    rect: opts.rect || { left: 10, top: 20, width: 100, height: 40 },
  };
  if (opts.id) node.attrs.id = opts.id;
  if (opts.class) node.attrs.class = opts.class;
  node.attributes = Object.entries(node.attrs).map(([name, value]) => ({ name, value }));
  node.getAttribute = (name) => (name in node.attrs ? node.attrs[name] : null);
  node.setAttribute = (name, value) => {
    node.attrs[name] = value;
    node.attributes = Object.entries(node.attrs).map(([n, v]) => ({ name: n, value: v }));
  };
  node.matches = (sel) => node.matchesList.includes(sel);
  node.getBoundingClientRect = () => node.rect;
  node.appendChild = (child) => {
    node.children.push(child);
    node.childNodes.push(child);
    child.parentElement = node;
    return child;
  };
  node.removeChild = (child) => {
    node.children = node.children.filter((c) => c !== child);
    node.childNodes = node.childNodes.filter((c) => c !== child);
    child.parentElement = null;
  };
  node.style = { cssText: "" };
  for (const child of opts.children || []) node.appendChild(child);
  for (const t of opts.text ? [opts.text] : []) node.childNodes.push({ nodeType: 3, nodeValue: t });
  return node;
}

/** Load the picker over a fake document; returns the picker and the listeners. */
function load({ root, sheets = [], uniqueIds = [], title = "Page" } = {}) {
  const listeners = {};
  const document = {
    title,
    documentElement: root,
    body: root,
    styleSheets: sheets,
    createElement: (tag) => el(tag),
    addEventListener: (type, fn) => ((listeners[type] ||= []).push(fn)),
    removeEventListener: (type, fn) => {
      listeners[type] = (listeners[type] || []).filter((f) => f !== fn);
    },
    querySelectorAll: (sel) => (uniqueIds.includes(sel) ? [{}] : [{}, {}]),
  };
  const sandbox = {
    document,
    location: { href: "https://example.com/pricing" },
    Math,
    JSON,
    String,
    Object,
    Array,
  };
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(src, sandbox);
  return { pick: sandbox.__ORCH_PICK_ALIAS || sandbox.__orchPick, listeners, document };
}

function fire(listeners, type, ev) {
  for (const fn of listeners[type] || []) fn(ev);
}

test("the selector prefers a unique id and otherwise walks up", () => {
  const button = el("button", { class: "btn btn-primary extra" });
  const card = el("div", { class: "card", children: [button] });
  const root = el("html", { children: [card] });
  const { pick } = load({ root, uniqueIds: [] });

  // No id anywhere: a chain of tag plus at most two classes.
  assert.equal(pick._internals.selectorFor(button), "div.card > button.btn.btn-primary");

  const withId = el("button", { id: "buy" });
  el("div", { children: [withId] });
  const unique = load({ root, uniqueIds: ["#buy"] });
  assert.equal(unique.pick._internals.selectorFor(withId), "#buy");
});

test("nth-of-type appears only where the tag repeats", () => {
  const a = el("li", { class: "row" });
  const b = el("li", { class: "row" });
  const only = el("span", { class: "only" });
  el("ul", { class: "list", children: [a, b, only] });
  const { pick } = load({ root: el("html") });

  assert.match(pick._internals.selectorFor(b), /li\.row:nth-of-type\(2\)$/);
  assert.equal(pick._internals.selectorFor(only).endsWith("span.only"), true);
});

test("markup stops at the depth cap and says what it left out", () => {
  const deep = el("em", { text: "deep" });
  const l3 = el("span", { children: [deep] });
  const l2 = el("p", { children: [l3] });
  const l1 = el("div", { class: "card", children: [l2] });
  const { pick } = load({ root: el("html", { children: [l1] }) });

  const out = pick._internals.markup(l1, 1, "");
  assert.match(out, /<div class="card">/);
  assert.match(out, /<p>/);
  assert.match(out, /1 more element\(s\)/);
  assert.equal(out.includes("deep"), false);
});

test("long text and long attributes are cut, void elements self-close", () => {
  const img = el("img", { attrs: { src: "x".repeat(400), alt: "hero" } });
  const p = el("p", { text: "y".repeat(400) });
  const { pick } = load({ root: el("html") });

  const imgOut = pick._internals.markup(img, 3, "");
  assert.match(imgOut, /<img .* \/>$/);
  assert.equal(imgOut.includes("x".repeat(250)), false);

  const pOut = pick._internals.markup(p, 3, "");
  assert.equal(pOut.includes("y".repeat(250)), false);
  assert.match(pOut, /…<\/p>$/);
});

test("only matching rules are kept, and a cross-origin sheet is skipped", () => {
  const button = el("button", { class: "btn", attrs: { style: "color:red" }, matches: [".btn", "button"] });
  const good = { cssRules: [
    { selectorText: ".btn", cssText: ".btn { padding: 8px; }" },
    { selectorText: ".unrelated", cssText: ".unrelated { color: blue; }" },
    { selectorText: "button", cssText: "button { border: 0; }" },
  ] };
  const crossOrigin = { get cssRules() { throw new Error("SecurityError"); } };
  const { pick } = load({ root: el("html", { children: [button] }), sheets: [crossOrigin, good] });

  // Array.from: the picker builds its array inside the vm realm, so its
  // prototype is not the host's and deepStrictEqual would fail on that alone.
  const css = Array.from(pick._internals.appliedRules(button));
  assert.equal(css[0], "[inline] { color:red }");
  assert.deepEqual(css.slice(1), [".btn { padding: 8px; }", "button { border: 0; }"]);
});

test("arm, click, take: one payload, then nothing", () => {
  const button = el("button", { class: "btn", text: "Buy" });
  const root = el("html", { children: [el("div", { class: "card", children: [button] })] });
  const { pick, listeners } = load({ root });

  assert.equal(pick.isArmed(), false);
  assert.equal(pick.take(), "idle");

  pick.arm();
  assert.equal(pick.isArmed(), true);
  assert.equal(pick.take(), "");

  let prevented = false;
  fire(listeners, "click", {
    target: button,
    preventDefault: () => (prevented = true),
    stopPropagation: () => {},
  });
  assert.equal(prevented, true, "the page must not receive the click that picked");
  assert.equal(pick.isArmed(), false, "one pick disarms");

  const payload = JSON.parse(pick.take());
  assert.equal(payload.tag, "button");
  assert.equal(payload.url, "https://example.com/pricing");
  assert.match(payload.selector, /button\.btn$/);
  assert.match(payload.markup, /Buy<\/button>/);
  assert.deepEqual(payload.box, { x: 10, y: 20, w: 100, h: 40 });

  assert.equal(pick.take(), "idle", "a payload is handed over once");
});

test("Escape cancels without a payload, and the overlay is not pickable", () => {
  const button = el("button", { class: "btn" });
  const root = el("html", { children: [button] });
  const { pick, listeners } = load({ root });

  pick.arm();
  fire(listeners, "keydown", { key: "Escape" });
  assert.equal(pick.isArmed(), false);
  assert.equal(pick.take(), "idle");

  // A click on the picker's own overlay picks nothing.
  pick.arm();
  const overlay = el("div", { attrs: { "data-orch-pick": "box" } });
  fire(listeners, "click", { target: overlay, preventDefault: () => {}, stopPropagation: () => {} });
  assert.equal(pick.take(), "idle");
});
