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
  const before = fs.readFileSync(chatBundle, "utf8");
  execFileSync(process.execPath, [path.join(__dirname, "bundle-chat.mjs")], { stdio: "pipe" });
  const after = fs.readFileSync(chatBundle, "utf8");
  if (before !== after) {
    fail("media/chat.bundle.js was stale — it has now been regenerated, commit it");
  } else {
    console.log("ok   media/chat.bundle.js is current");
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

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
console.log("\nwebview checks passed");
