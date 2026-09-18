// Checks the webview bundles that tsc does not cover.
//
// tsconfig.json includes only src/**, so nothing in media/chat-src is
// typechecked or tested by `npm test` — the //@ts-check header helps an editor
// and no one else. Changes there used to be verified by hand, once, by whoever
// made them. This script is what makes them verifiable again:
//
//   1. every generated bundle parses (a concatenation bug is a blank webview,
//      and the bundler only joins text — it never parses what it joins)
//   2. the bundles are in sync with their sources, so a fragment edited without
//      re-running `npm run bundle:webview` is caught here rather than shipped
//   3. the pure helpers worth asserting on actually return what they claim
//
// Run: node scripts/check-webview.mjs   (no dependencies, no install needed)

import { execFileSync } from "child_process";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { catalogueKeys, markupKeys } from "./i18n-keys.mjs";
import { iconNames, iconCallSites, iconTableEntries, strayIconSvgs } from "./icon-names.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");

let failures = 0;
const fail = (msg) => {
  console.error("FAIL " + msg);
  failures++;
};

// 1. Bundles parse.
for (const rel of ["media/chat.bundle.js", "media/settings.bundle.js"]) {
  const p = path.join(root, rel);
  if (!fs.existsSync(p)) {
    fail(`${rel} is missing — run npm run bundle:webview`);
    continue;
  }
  try {
    execFileSync(process.execPath, ["--check", p], { stdio: "pipe" });
    console.log("ok   " + rel + " parses");
  } catch (e) {
    fail(`${rel} does not parse:\n${e.stderr?.toString() || e.message}`);
  }
}

// 2. Bundles match their sources.
const chatBundle = path.join(root, "media", "chat.bundle.js");
if (fs.existsSync(chatBundle)) {
  // Compare with line endings normalised: on Windows the file is checked out
  // CRLF and the bundler writes LF, which is not staleness — it made this
  // check fail on every clean tree.
  const eol = (s) => s.replace(/\r\n/g, "\n");
  const before = eol(fs.readFileSync(chatBundle, "utf8"));
  execFileSync(process.execPath, [path.join(__dirname, "bundle-chat.mjs")], { stdio: "pipe" });
  const after = eol(fs.readFileSync(chatBundle, "utf8"));
  if (before !== after) {
    fail("media/chat.bundle.js was stale — it has now been regenerated, commit it");
  } else {
    console.log("ok   media/chat.bundle.js is current");
  }
}

// The settings bundle is generated from settings-src/ the same way and was
// only parse-checked above, so an edit to a fragment that was never rebundled
// passed this script. It happened to be caught by ui/web/scripts/check-web.mjs,
// which regenerates the *web's* settings bundle from the same sources — but
// that is a side effect of another surface's check, not this one doing its job,
// and this is the script the VSIX publish workflow runs.
const settingsBundle = path.join(root, "media", "settings.bundle.js");
if (fs.existsSync(settingsBundle)) {
  const eol = (s) => s.replace(/\r\n/g, "\n");
  const before = eol(fs.readFileSync(settingsBundle, "utf8"));
  execFileSync(process.execPath, [path.join(__dirname, "bundle-settings.mjs")], { stdio: "pipe" });
  const after = eol(fs.readFileSync(settingsBundle, "utf8"));
  if (before !== after) {
    fail("media/settings.bundle.js was stale — it has now been regenerated, commit it");
  } else {
    console.log("ok   media/settings.bundle.js is current");
  }
}

// 3. Pure helpers.
//
// The fragments are one shared IIFE scope with no exports, so a helper is
// pulled out by source and evaluated on its own. That is only safe for
// self-contained functions — which is exactly the set worth asserting on.
function extractFunction(fragment, name) {
  const src = fs.readFileSync(path.join(root, "media", "chat-src", fragment), "utf8");
  const start = src.indexOf(`function ${name}(`);
  if (start < 0) {
    return null;
  }
  // Balance braces from the function's opening one.
  let depth = 0;
  for (let i = src.indexOf("{", start); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) {
        return eval("(" + src.slice(start, i + 1) + ")");
      }
    }
  }
  return null;
}

const formatToolDuration = extractFunction("05d-tools.js", "formatToolDuration");
if (typeof formatToolDuration !== "function") {
  fail("formatToolDuration could not be extracted from 05d-tools.js");
} else {
  const cases = [
    [0, "0ms"],
    [420, "420ms"],
    [999, "999ms"],
    [1000, "1.0s"],
    [1240, "1.2s"],
    [59900, "59.9s"],
    // Just under a minute must not render as "60.0s" beside "1m 00s".
    [59999, "1m 00s"],
    [60000, "1m 00s"],
    [91400, "1m 31s"],
    // Absent or nonsensical input renders nothing, so the span collapses
    // (.tool-dur:empty) instead of printing "NaN".
    [NaN, ""],
    [-5, ""],
  ];
  let bad = 0;
  for (const [ms, want] of cases) {
    const got = formatToolDuration(ms);
    if (got !== want) {
      fail(`formatToolDuration(${ms}) = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
      bad++;
    }
  }
  if (bad === 0) {
    console.log(`ok   formatToolDuration (${cases.length} cases)`);
  }
}

// 4. The host seam is intact.
//
// The renderer must reach its host through the `host` object only. A stray
// `vscode.` call is invisible in VS Code (where the shim wraps the real API)
// and a blank screen in the browser, so it is caught here instead.
{
  const fragDir = path.join(root, "media", "chat-src");
  const strays = [];
  for (const name of fs.readdirSync(fragDir).filter((n) => n.endsWith(".js"))) {
    const src = fs.readFileSync(path.join(fragDir, name), "utf8");
    src.split(/\r?\n/).forEach((line, i) => {
      if (/\bvscode\s*\.\s*(postMessage|getState|setState)\s*\(/.test(line)) {
        strays.push(`${name}:${i + 1}`);
      }
    });
  }
  if (strays.length > 0) {
    fail(`fragments still call the VS Code API directly (use host.*): ${strays.join(", ")}`);
  } else {
    console.log("ok   host seam: no direct vscode.* calls in fragments");
  }

  const domState = fs.readFileSync(path.join(fragDir, "01-dom-state.js"), "utf8");
  if (!/const\s+host\s*=\s*acquireVsCodeApi\(\)/.test(domState)) {
    fail("01-dom-state.js must bind `const host = acquireVsCodeApi()`");
  } else {
    console.log("ok   host seam: bound in 01-dom-state.js");
  }
}

// 5. Every data-i18n key in the markup exists in the catalogue.
//
// A key with a typo is not an error anywhere: i18n() returns the key itself,
// so the screen reads "set.mcp.done" where it should say "Done". That is
// invisible to every other check here — the bundle parses, the ids are all
// present — and only shows up to whoever opens the panel.
{
  const known = catalogueKeys(path.join(root, "media"));
  if (known.size === 0) {
    fail("no keys could be read out of media/i18n.js");
  }
  const files = [
    path.join(root, "media", "settings-body.html"),
    path.join(root, "src", "chat", "panel.ts"),
  ];
  let used = 0;
  const missing = [];
  for (const file of files) {
    for (const { key, line } of markupKeys(file)) {
      used++;
      if (!known.has(key)) {
        missing.push(`${path.relative(root, file)}:${line} ${key}`);
      }
    }
  }
  if (missing.length > 0) {
    fail(
      `${missing.length} data-i18n key(s) are not in media/i18n.js — each one renders as the ` +
        `key itself: ${missing.join(", ")}`
    );
  } else {
    console.log(`ok   ${used} data-i18n keys, all in the catalogue (${known.size} keys)`);
  }
}

// 6. Every icon the code asks for exists, and no icon is drawn by hand.
//
// orchIconMarkup returns "" for a name it does not know, so a typo is an
// empty slot on a toolbar with nothing logged anywhere. And an <svg> written
// by hand looks right on its own and only reads as wrong beside the set —
// which is the whole reason the set exists (see media/icons.js).
{
  const known = iconNames(path.join(root, "media"));
  if (known.size === 0) {
    fail("no icons could be read out of media/icons.js");
  }
  const srcFiles = fs
    .readdirSync(path.join(root, "media", "chat-src"))
    .filter((n) => n.endsWith(".js"))
    .map((n) => path.join(root, "media", "chat-src", n))
    .concat(
      fs
        .readdirSync(path.join(root, "media", "settings-src"))
        .filter((n) => n.endsWith(".js"))
        .map((n) => path.join(root, "media", "settings-src", n))
    );
  let used = 0;
  const unknown = [];
  for (const file of srcFiles) {
    for (const { name, line } of iconCallSites(file)) {
      used++;
      if (!known.has(name)) unknown.push(`${path.relative(root, file)}:${line} ${name}`);
    }
  }
  // The mode and access tables name their icons in a field, not in a call.
  const tableFile = path.join(root, "media", "chat-src", "01-dom-state.js");
  for (const { name, line } of iconTableEntries(tableFile)) {
    used++;
    if (!known.has(name)) unknown.push(`${path.relative(root, tableFile)}:${line} ${name}`);
  }
  if (unknown.length > 0) {
    fail(
      `${unknown.length} icon name(s) are not in media/icons.js — each one renders as ` +
        `nothing at all: ${unknown.join(", ")}`
    );
  } else {
    console.log(`ok   ${used} icon names, all in the set (${known.size} icons)`);
  }

  const stray = strayIconSvgs(path.join(root, "src", "chat", "panel.ts"));
  if (stray.length > 0) {
    fail(
      `${stray.length} hand-drawn <svg> in src/chat/panel.ts — use a media/icons.js ` +
        `drawing at stroke 1.75 on the 24 grid: ` +
        stray.map((x) => `panel.ts:${x.line}`).join(", ")
    );
  } else {
    console.log("ok   every <svg> in panel.ts is one of the set's");
  }
}

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
console.log("\nwebview checks passed");
