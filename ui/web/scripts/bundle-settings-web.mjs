// Builds the settings panel as a standalone page for the web UI.
//
//   static/settings.html        the shell + the markup both hosts share
//   static/settings.bundle.js   web prelude + ui/vscode/media/settings-src/*
//   static/settings.css         copied from media/
//   static/provider-icons/      copied from media/
//   static/mcp-catalog.json     copied from media/ (the parent maps it)
//
// It is a separate document, loaded in an iframe by the chat page, because
// settings.css declares its own :root palette under the same token names
// chat.css uses. See ui/web/src/settings-frame.js for the rest of the why.
//
// Run: node ui/web/scripts/bundle-settings-web.mjs   (no dependencies)

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const repo = path.join(root, "..", "..");
const mediaDir = path.join(repo, "ui", "vscode", "media");
const srcDir = path.join(mediaDir, "settings-src");
const webDir = path.join(root, "src");
const outDir = path.join(root, "static");

/** The fragment order settings.bundle.js uses, with the web prelude first. */
const frameDir = path.join(webDir, "settings");
const order = [
  [srcDir, "00-header.txt"],
  [frameDir, "frame.js"],
  [srcDir, "01-core.js"],
  [srcDir, "01b-provider-ui.js"],
  [srcDir, "01c-provider-logos.js"],
  [srcDir, "02-models.js"],
  [srcDir, "03-orchestra.js"],
  [srcDir, "04-agents-mcp.js"],
  [srcDir, "05-state.js"],
  [srcDir, "99-footer.txt"],
];

for (const [dir, name] of order) {
  const p = path.join(dir, name);
  if (!fs.existsSync(p)) {
    console.error("missing fragment:", path.relative(repo, p));
    process.exit(1);
  }
}

fs.mkdirSync(outDir, { recursive: true });

// ---- the script ---------------------------------------------------------

const banner =
  "/* AUTO-GENERATED — do not edit. Sources: ui/vscode/media/settings-src/* + " +
  "ui/web/src/settings/frame.js  →  node ui/web/scripts/bundle-settings-web.mjs */\n";
let out =
  banner + order.map(([dir, name]) => fs.readFileSync(path.join(dir, name), "utf8").replace(/\s+$/, "")).join("\n") + "\n";

// 01-core.js binds `vscode` to acquireVsCodeApi(); the web prelude has already
// defined its own above it, so strip that one line. Fail loudly if it moves —
// two bindings in one scope is a SyntaxError, and silently shipping one that
// calls a function no browser has is worse.
{
  const stripped = out.replace(
    /^\s*const vscode = acquireVsCodeApi\(\);[ \t]*$/m,
    "  /* vscode is supplied by ui/web/src/settings-frame.js */"
  );
  if (stripped === out) {
    console.error("could not strip acquireVsCodeApi() binding — 01-core.js changed shape");
    process.exit(1);
  }
  out = stripped;
}
// Comments stripped first: the prelude explains itself by naming the function,
// and a mention is not a call. No browser defines it, so a surviving call would
// throw on load with the panel already on screen.
{
  const code = out.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  if (/acquireVsCodeApi\s*\(/.test(code)) {
    console.error("acquireVsCodeApi is still called in the settings bundle");
    process.exit(1);
  }
}

fs.writeFileSync(path.join(outDir, "settings.bundle.js"), out);

// ---- the page -----------------------------------------------------------

const bodyFile = path.join(mediaDir, "settings-body.html");
const body = fs
  .readFileSync(bodyFile, "utf8")
  .replace(/^<!--[\s\S]*?-->\s*/, "")
  .trimEnd();

// Web-only: the theme is the page's, not the editor's, so the panel grows an
// Appearance section the VS Code build has no use for. The nav click handler in
// 01-core.js is generic over [data-section], so the tab works with no new JS;
// only the three choices need a handler, and they get one here.
const appearanceNav =
  '          <button type="button" class="nav-item" data-section="appearance">' +
  '<span class="nav-ico"><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<circle cx="8" cy="8" r="5.5"/><path d="M8 2.5v11"/><path d="M8 2.5a5.5 5.5 0 010 11" fill="currentColor" ' +
  'stroke="none"/></svg></span>Appearance</button>\n';

const appearancePanel = `
      <section id="sec-appearance" class="panel">
        <h1>Appearance</h1>
        <p class="sub">How this page looks. Stored in this browser only.</p>
        <div class="theme-choices" role="radiogroup" aria-label="Theme">
          <button type="button" class="theme-choice" data-theme-choice="system" role="radio" aria-checked="true">
            <span class="theme-choice-name">Follow the system</span>
            <span class="theme-choice-hint">Whatever your OS is set to</span>
          </button>
          <button type="button" class="theme-choice" data-theme-choice="light" role="radio" aria-checked="false">
            <span class="theme-choice-name">Light</span>
            <span class="theme-choice-hint">Always the light palette</span>
          </button>
          <button type="button" class="theme-choice" data-theme-choice="dark" role="radio" aria-checked="false">
            <span class="theme-choice-name">Dark</span>
            <span class="theme-choice-hint">Always the dark palette</span>
          </button>
        </div>
      </section>
`;

const navAnchor = '          <button type="button" class="nav-item" data-section="tools">';
if (!body.includes(navAnchor)) {
  console.error("settings-body.html changed shape — the Tools nav item was not found");
  process.exit(1);
}
let pageBody = body.replace(navAnchor, appearanceNav + navAnchor);

const mainClose = "    </main>";
if (!pageBody.includes(mainClose)) {
  console.error("settings-body.html changed shape — the end of <main> was not found");
  process.exit(1);
}
pageBody = pageBody.replace(mainClose, appearancePanel + mainClose);

const page = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="settings.css" />
  <title>Orchestra Settings</title>
  <script>
    // Stamped before the first paint, as the chat page does, or the panel
    // flashes the other theme on every open. The parent keeps the value.
    (function () {
      try {
        var saved = localStorage.getItem("orchestra.theme");
        if (saved === "light" || saved === "dark") {
          document.documentElement.setAttribute("data-theme", saved);
        }
      } catch (e) {
        // Storage can throw outright in a locked-down browser.
      }
    })();
  </script>
</head>
<body>
  ${pageBody}
  <script src="settings.bundle.js"></script>
</body>
</html>
`;
fs.writeFileSync(path.join(outDir, "settings.html"), page);

// ---- the assets ---------------------------------------------------------

// The stylesheet, plus what only this host needs: Geist (served from the same
// static/ directory as the chat page), a light palette, and the Appearance
// tab's controls. None of it is written back to media/settings.css — the VS
// Code panel follows the editor's theme and has no Appearance tab.
const webOnlyCss = `
/* ─────────────────────────────────────────────────────────────────────────
   Appended by ui/web/scripts/bundle-settings-web.mjs. Do not edit here; edit
   the bundler. media/settings.css, which the VS Code panel loads, has none of
   this.
   ───────────────────────────────────────────────────────────────────────── */

@font-face {
  font-family: "Geist Variable";
  font-style: normal;
  font-display: swap;
  font-weight: 100 900;
  src: url("fonts/geist-latin-wght-normal.woff2") format("woff2-variations");
  unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA,
    U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193,
    U+2212, U+2215, U+FEFF, U+FFFD;
}

@font-face {
  font-family: "Geist Variable";
  font-style: normal;
  font-display: swap;
  font-weight: 100 900;
  src: url("fonts/geist-cyrillic-wght-normal.woff2") format("woff2-variations");
  unicode-range: U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116;
}

:root {
  font-family: "Geist Variable", -apple-system, BlinkMacSystemFont, "Segoe UI",
    system-ui, sans-serif;
}

/* The light palette, in the same three states the chat page uses: the system
   preference unless dark is pinned, and the pinned choice either way. */
@media (prefers-color-scheme: light) {
  :root:not([data-theme="dark"]) {
    color-scheme: light;
    --bg: #ffffff;
    --fg: #18181b;
    --muted: #71717a;
    --border: rgba(0, 0, 0, 0.10);
    --border-strong: rgba(0, 0, 0, 0.20);
    --input: rgba(0, 0, 0, 0.028);
    --surface: rgba(0, 0, 0, 0.022);
    --surface-2: rgba(0, 0, 0, 0.05);
    --well: rgba(0, 0, 0, 0.045);
    --hover: rgba(0, 0, 0, 0.05);
    --selected: rgba(0, 0, 0, 0.08);
    --menu: #ffffff;
    --accent: #7c3aed;
    --accent-dim: rgba(124, 58, 237, 0.10);
    --accent-border: rgba(124, 58, 237, 0.40);
    --accent-ring: rgba(124, 58, 237, 0.25);
    --focus: #7c3aed;
    --warn: #a16207;
    --err: #c2410c;
    --nav: rgba(255, 255, 255, 0.85);
    --ok: #15803d;
  }
  :root:not([data-theme="dark"]) * {
    scrollbar-color: rgba(0, 0, 0, 0.18) transparent;
  }
  :root:not([data-theme="dark"]) *::-webkit-scrollbar-thumb {
    background: rgba(0, 0, 0, 0.16);
  }
  :root:not([data-theme="dark"]) *::-webkit-scrollbar-thumb:hover {
    background: rgba(0, 0, 0, 0.28);
  }
}

:root[data-theme="light"] {
  color-scheme: light;
  --bg: #ffffff;
  --fg: #18181b;
  --muted: #71717a;
  --border: rgba(0, 0, 0, 0.10);
  --border-strong: rgba(0, 0, 0, 0.20);
  --input: rgba(0, 0, 0, 0.028);
  --surface: rgba(0, 0, 0, 0.022);
  --surface-2: rgba(0, 0, 0, 0.05);
  --well: rgba(0, 0, 0, 0.045);
  --hover: rgba(0, 0, 0, 0.05);
  --selected: rgba(0, 0, 0, 0.08);
  --menu: #ffffff;
  --accent: #7c3aed;
  --accent-dim: rgba(124, 58, 237, 0.10);
  --accent-border: rgba(124, 58, 237, 0.40);
  --accent-ring: rgba(124, 58, 237, 0.25);
  --focus: #7c3aed;
  --warn: #a16207;
  --err: #c2410c;
  --nav: rgba(255, 255, 255, 0.85);
  --ok: #15803d;
}

:root[data-theme="light"] * {
  scrollbar-color: rgba(0, 0, 0, 0.18) transparent;
}

:root[data-theme="light"] *::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.16);
}

:root[data-theme="light"] *::-webkit-scrollbar-thumb:hover {
  background: rgba(0, 0, 0, 0.28);
}

/* The panel lives in an iframe that is already inset by the dialog, so it
   must not paint a second ground behind itself. */
html,
body {
  background: transparent;
}

/* Binary path and project root are VS Code settings — where to find the
   executable, and which folder to open. \`orchestra web\` answered both before
   this page loaded, so 50-settings.js sends them empty and the rows are hidden
   rather than inviting edits that would go nowhere. The line about restarting
   the core goes with them. */
#sec-general > label:has(#binaryPath),
#sec-general > label:has(#projectRoot),
#sec-general > label:has(#projectRoot) + .hint {
  display: none;
}

/* ---- the Appearance tab ------------------------------------------------ */

.theme-choices {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-width: 420px;
}

.theme-choice {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  align-items: baseline;
  gap: 2px 8px;
  padding: 10px 12px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--fg);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

/* A check in the gutter, so the current choice reads without colour. */
.theme-choice::before {
  content: "";
  grid-row: 1;
  grid-column: 1;
  color: var(--accent);
}

.theme-choice:hover {
  background: var(--hover);
}

.theme-choice[aria-checked="true"] {
  border-color: var(--accent-border);
  background: var(--accent-dim);
}

.theme-choice[aria-checked="true"]::before {
  content: "✓";
}

.theme-choice:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: -2px;
}

.theme-choice-name {
  grid-row: 1;
  grid-column: 2;
  font-size: 13.5px;
  font-weight: 500;
}

.theme-choice-hint {
  grid-row: 2;
  grid-column: 2;
  font-size: 12px;
  color: var(--muted);
}
`;

fs.writeFileSync(
  path.join(outDir, "settings.css"),
  fs.readFileSync(path.join(mediaDir, "settings.css"), "utf8").replace(/\s+$/, "") + "\n" + webOnlyCss
);

const catalog = path.join(mediaDir, "mcp-catalog.json");
if (fs.existsSync(catalog)) {
  fs.copyFileSync(catalog, path.join(outDir, "mcp-catalog.json"));
}

const icons = path.join(mediaDir, "provider-icons");
if (fs.existsSync(icons)) {
  const dest = path.join(outDir, "provider-icons");
  fs.mkdirSync(dest, { recursive: true });
  for (const name of fs.readdirSync(icons)) {
    const from = path.join(icons, name);
    if (fs.statSync(from).isFile()) {
      fs.copyFileSync(from, path.join(dest, name));
    }
  }
}

console.log(
  "bundled static/settings.html + settings.bundle.js (" + out.split("\n").length + " lines of JS)"
);
