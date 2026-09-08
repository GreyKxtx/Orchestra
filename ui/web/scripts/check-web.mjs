// Checks the web bundle, mirroring ui/vscode/scripts/check-webview.mjs.
//
//   1. the bundle parses
//   2. the bundle is in sync with its sources
//   3. index.html carries every element id the fragments look up
//
// (3) is the anti-drift guard that matters here: the shared fragments are read
// from ui/vscode/media/chat-src, so JS drift is impossible by construction, but
// the page skeleton is the web's own — and a missing id disables a feature
// silently, with no error anywhere.
//
// Run: node ui/web/scripts/check-web.mjs   (no dependencies)

import { execFileSync } from "child_process";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const repo = path.join(root, "..", "..");

let failures = 0;
const fail = (msg) => {
  console.error("FAIL " + msg);
  failures++;
};

// 1. Bundle parses.
const bundle = path.join(root, "static", "web.bundle.js");
if (!fs.existsSync(bundle)) {
  fail("static/web.bundle.js is missing — run node ui/web/scripts/bundle-web.mjs");
} else {
  try {
    execFileSync(process.execPath, ["--check", bundle], { stdio: "pipe" });
    console.log("ok   static/web.bundle.js parses");
  } catch (e) {
    fail(`static/web.bundle.js does not parse:\n${e.stderr?.toString() || e.message}`);
  }

  // 2. Bundle matches its sources.
  // Compare with line endings normalised: on Windows the file is checked out
  // CRLF and the bundler writes LF, which is not staleness.
  const eol = (s) => s.replace(/\r\n/g, "\n");
  const before = eol(fs.readFileSync(bundle, "utf8"));
  execFileSync(process.execPath, [path.join(__dirname, "bundle-web.mjs")], { stdio: "pipe" });
  const after = eol(fs.readFileSync(bundle, "utf8"));
  if (before !== after) {
    fail("static/web.bundle.js was stale — it has now been regenerated, commit it");
  } else {
    console.log("ok   static/web.bundle.js is current");
  }
}

// 3. Every id the fragments look up exists in the page.
{
  const html = fs.readFileSync(path.join(root, "static", "index.html"), "utf8");
  const present = new Set();
  for (const m of html.matchAll(/\bid="([^"]+)"/g)) {
    present.add(m[1]);
  }

  const wanted = new Set();
  // Ids the fragments inject themselves (e.g. 06-composer.js writes
  // `<button id="fast-toggle">` and looks it up two lines later) are not the
  // page's job to provide, so they are excused rather than reported.
  const injected = new Set();
  const dirs = [
    path.join(repo, "ui", "vscode", "media", "chat-src"),
    path.join(root, "src"),
  ];
  for (const dir of dirs) {
    for (const name of fs.readdirSync(dir).filter((n) => n.endsWith(".js"))) {
      const src = fs.readFileSync(path.join(dir, name), "utf8");
      for (const m of src.matchAll(/getElementById\(\s*"([^"]+)"\s*\)/g)) {
        wanted.add(m[1]);
      }
      for (const m of src.matchAll(/\bid=\\?["']([^"'\\]+)\\?["']/g)) {
        injected.add(m[1]);
      }
    }
  }

  const missing = [...wanted]
    .filter((id) => !present.has(id) && !injected.has(id))
    .sort();
  if (missing.length > 0) {
    fail(
      `index.html is missing ${missing.length} element id(s) the renderer looks up ` +
        `(each one silently disables a feature): ${missing.join(", ")}`
    );
  } else {
    console.log(`ok   index.html carries all ${wanted.size} looked-up ids`);
  }
}

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
console.log("\nweb checks passed");
