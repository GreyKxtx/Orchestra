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
import { catalogueKeys, markupKeys } from "../../vscode/scripts/i18n-keys.mjs";
import { iconNames, iconCallSites, strayIconSvgs } from "../../vscode/scripts/icon-names.mjs";

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

// 4. No fragment redeclares another's top-level name.
//
// Every fragment is concatenated into ONE function scope, so two files
// declaring the same name is not a clash the engine reports — the later one
// silently replaces the earlier. That has bitten twice: a duplicate const
// threw at load (caught by check 1), and a duplicate `function
// renderSessionTabs` replaced the renderer's with the adapter's, which turned
// one message into an endless loop and painted nothing. Neither parsing nor
// the id check can see it.
//
// Top level inside the IIFE is exactly one indent, which every fragment
// follows; anything deeper is a real nested scope and is left alone. The
// bundle is what gets read, not the sources, because the bundler deliberately
// drops one line on the way in — 01-dom-state.js's `host = acquireVsCodeApi()`,
// which 00-web-prelude.js replaces — and that is not a clash. Sources are read
// only to say which file each surviving duplicate came from.
if (fs.existsSync(bundle)) {
  const topLevel = / {2}(?:async )?(?:function\*? |const |let |class )([A-Za-z_$][\w$]*)/;
  const counts = new Map();
  for (const line of fs.readFileSync(bundle, "utf8").split(/\r?\n/)) {
    const m = topLevel.exec(line);
    if (m && line.startsWith("  " + line.trim().slice(0, 1))) {
      counts.set(m[1], (counts.get(m[1]) || 0) + 1);
    }
  }
  const duplicated = [...counts.entries()].filter(([, n]) => n > 1).map(([id]) => id);

  /** @type {Map<string, string[]>} */
  const declaredIn = new Map();
  const dirs = [
    path.join(repo, "ui", "vscode", "media", "chat-src"),
    path.join(root, "src"),
  ];
  for (const dir of dirs) {
    for (const name of fs.readdirSync(dir).filter((n) => n.endsWith(".js"))) {
      const src = fs.readFileSync(path.join(dir, name), "utf8");
      for (const id of duplicated) {
        const re = new RegExp(`^ {2}(?:async )?(?:function\\*? |const |let |class )${id}\\b`, "m");
        if (re.test(src)) {
          declaredIn.set(id, (declaredIn.get(id) || []).concat(name));
        }
      }
    }
  }

  if (duplicated.length > 0) {
    const where = duplicated
      .map((id) => `${id} (${(declaredIn.get(id) || ["?"]).join(", ")})`)
      .sort();
    fail(
      `${duplicated.length} name(s) are declared at the top level of more than one ` +
        `fragment; they share one scope, so the last one silently wins: ${where.join("; ")}`
    );
  } else {
    console.log(`ok   ${counts.size} top-level names, each declared once`);
  }
}

// 5. The settings page, which is its own document and its own bundle.
{
  const settingsBundle = path.join(root, "static", "settings.bundle.js");
  if (!fs.existsSync(settingsBundle)) {
    fail("static/settings.bundle.js is missing — run node ui/web/scripts/bundle-settings-web.mjs");
  } else {
    try {
      execFileSync(process.execPath, ["--check", settingsBundle], { stdio: "pipe" });
    } catch (e) {
      fail(`static/settings.bundle.js does not parse:\n${e.stderr?.toString() || e.message}`);
    }
    const eol = (s) => s.replace(/\r\n/g, "\n");
    const page = path.join(root, "static", "settings.html");
    const theme = path.join(root, "static", "settings-theme.css");
    const generated = [settingsBundle, page, theme];
    const before = generated.map((f) => (fs.existsSync(f) ? eol(fs.readFileSync(f, "utf8")) : ""));
    execFileSync(process.execPath, [path.join(__dirname, "bundle-settings-web.mjs")], {
      stdio: "pipe",
    });
    const after = generated.map((f) => eol(fs.readFileSync(f, "utf8")));
    if (before.some((s, i) => s !== after[i])) {
      fail("the settings page was stale — it has now been regenerated, commit it");
    } else {
      console.log("ok   static/settings.html and its bundle parse and are current");
    }

    // Its markup comes from ui/vscode/media/settings-body.html, which the VS
    // Code panel reads too — so a missing id here means the shared file and the
    // fragments have drifted, in both hosts at once.
    const html = fs.readFileSync(page, "utf8");
    const present = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
    const wanted = new Set();
    const injected = new Set();
    const dir = path.join(repo, "ui", "vscode", "media", "settings-src");
    for (const name of fs.readdirSync(dir).filter((n) => n.endsWith(".js"))) {
      const s = fs.readFileSync(path.join(dir, name), "utf8");
      for (const m of s.matchAll(/getElementById\(\s*"([^"]+)"\s*\)/g)) wanted.add(m[1]);
      for (const m of s.matchAll(/\bel\(\s*"([^"]+)"\s*\)/g)) wanted.add(m[1]);
      for (const m of s.matchAll(/\b(?:input|area)\(\s*"([^"]+)"\s*\)/g)) wanted.add(m[1]);
      for (const m of s.matchAll(/\bid=\\?["']([^"'\\]+)\\?["']/g)) injected.add(m[1]);
    }
    const missing = [...wanted].filter((id) => !present.has(id) && !injected.has(id)).sort();
    if (missing.length > 0) {
      fail(
        `settings.html is missing ${missing.length} element id(s) the panel looks up: ${missing.join(", ")}`
      );
    } else {
      console.log(`ok   settings.html carries all ${wanted.size} looked-up ids`);
    }
  }
}

// 6. Every data-i18n key on the generated pages exists in the catalogue.
//
// Same reason as in ui/vscode/scripts/check-webview.mjs: a mistyped key is not
// an error, it is a screen that reads "start.clone" where it should say
// "Clone". Last, because it reads the pages the checks above have just
// regenerated — including the Appearance section bundle-settings-web.mjs
// injects, which exists in no source file.
{
  const known = catalogueKeys(path.join(repo, "ui", "vscode", "media"));
  if (known.size === 0) {
    fail("no keys could be read out of ui/vscode/media/i18n.js");
  }
  const pages = [
    path.join(root, "static", "index.html"),
    path.join(root, "static", "settings.html"),
  ].filter((f) => fs.existsSync(f));
  let used = 0;
  const missing = [];
  for (const file of pages) {
    for (const { key, line } of markupKeys(file)) {
      used++;
      if (!known.has(key)) {
        missing.push(`${path.relative(root, file)}:${line} ${key}`);
      }
    }
  }
  if (missing.length > 0) {
    fail(
      `${missing.length} data-i18n key(s) are not in ui/vscode/media/i18n.js — each one renders ` +
        `as the key itself: ${missing.join(", ")}`
    );
  } else {
    console.log(`ok   ${used} data-i18n keys on the pages, all in the catalogue`);
  }
}

// 7. The icon set, on this side of the fence.
//
// Same two silent failures as the webview check: a name the set does not know
// draws nothing, and an <svg> written by hand only reads as wrong beside its
// neighbours. The page checked here is the GENERATED one, so a drawing that
// slipped into index.src.html is caught where it is actually served.
{
  const mediaDir = path.join(repo, "ui", "vscode", "media");
  const known = iconNames(mediaDir);
  const srcFiles = fs
    .readdirSync(path.join(root, "src"))
    .filter((n) => n.endsWith(".js"))
    .map((n) => path.join(root, "src", n))
    .concat([path.join(root, "src", "settings", "frame.js")].filter((f) => fs.existsSync(f)));
  let used = 0;
  const unknown = [];
  for (const file of srcFiles) {
    for (const { name, line } of iconCallSites(file)) {
      used++;
      if (!known.has(name)) unknown.push(`${path.relative(root, file)}:${line} ${name}`);
    }
  }
  if (unknown.length > 0) {
    fail(
      `${unknown.length} icon name(s) are not in media/icons.js — each one renders as ` +
        `nothing at all: ${unknown.join(", ")}`
    );
  } else {
    console.log(`ok   ${used} icon names in the web sources, all in the set (${known.size} icons)`);
  }

  const page = path.join(root, "static", "index.html");
  const stray = strayIconSvgs(page);
  if (stray.length > 0) {
    fail(
      `${stray.length} hand-drawn <svg> in the served page — fix them in index.src.html ` +
        `and use a media/icons.js drawing at stroke 1.75 on the 24 grid: ` +
        stray.map((x) => `index.html:${x.line}`).join(", ")
    );
  } else {
    console.log("ok   every <svg> on the served page is one of the set's");
  }
}

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
console.log("\nweb checks passed");
