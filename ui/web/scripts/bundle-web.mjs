// Builds ui/web/static from the shared VS Code renderer fragments plus the web's
// own host and adapter fragments.
//
// The shared fragments are read from ui/vscode/media/chat-src — not copied.
// `vsce package` packages only ui/vscode/, so a fragment moved out of that
// directory would be missing from the .vsix; reading in place keeps one copy,
// which makes drift impossible rather than merely detectable.

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const repo = path.join(root, "..", "..");
const sharedDir = path.join(repo, "ui", "vscode", "media", "chat-src");
const webDir = path.join(root, "src");
const outDir = path.join(root, "static");

// Order mirrors ui/vscode/scripts/bundle-chat.mjs, with the web's host and
// adapter fragments wrapped around the shared renderer.
const order = [
  [sharedDir, "00-header.txt"],
  [webDir, "00-web-prelude.js"],
  [sharedDir, "01-dom-state.js"],
  [sharedDir, "02-util.js"],
  [sharedDir, "03-markdown.js"],
  [sharedDir, "04-diff-tools.js"],
  [sharedDir, "05a-subagents-turn.js"],
  [sharedDir, "05b-overlays.js"],
  [sharedDir, "05c-busy-palette.js"],
  [sharedDir, "05d-tools.js"],
  [sharedDir, "05e-messages.js"],
  [sharedDir, "05f-trajectory.js"],
  [sharedDir, "06-composer.js"],
  [sharedDir, "07-events.js"],
  [webDir, "10-adapter-session.js"],
  [webDir, "20-adapter-events.js"],
  [webDir, "30-adapter-asks.js"],
  [webDir, "40-projects.js"],
  [webDir, "50-settings.js"],
  [webDir, "60-composer.js"],
  [sharedDir, "99-footer.txt"],
];

for (const [dir, name] of order) {
  const p = path.join(dir, name);
  if (!fs.existsSync(p)) {
    console.error("missing fragment:", path.relative(repo, p));
    process.exit(1);
  }
}

fs.mkdirSync(outDir, { recursive: true });

const banner =
  "/* AUTO-GENERATED — do not edit. Sources: ui/vscode/media/chat-src/* + ui/web/src/*  →  node ui/web/scripts/bundle-web.mjs */\n";
const parts = order.map(([dir, name]) =>
  fs.readFileSync(path.join(dir, name), "utf8").replace(/\s+$/, "")
);
let out = banner + parts.join("\n") + "\n";

// 01-dom-state.js binds `host` to acquireVsCodeApi(); the web prelude has
// already defined its own `host` above it, so strip that one line.
{
  const stripped = out.replace(
    /^\s*const host = acquireVsCodeApi\(\);[ \t]*$/m,
    "  /* host is supplied by ui/web/src/00-web-prelude.js */"
  );
  if (stripped === out) {
    console.error("could not strip acquireVsCodeApi() binding — 01-dom-state.js changed shape");
    process.exit(1);
  }
  out = stripped;
}

fs.writeFileSync(path.join(outDir, "web.bundle.js"), out);

// Static assets: the page, the stylesheet (portable as-is — chat.css has zero
// --vscode-* references) and the logo.
fs.copyFileSync(path.join(root, "index.src.html"), path.join(outDir, "index.html"));
fs.copyFileSync(path.join(repo, "ui", "vscode", "media", "chat.css"), path.join(outDir, "chat.css"));
fs.copyFileSync(path.join(repo, "ui", "vscode", "media", "logo.png"), path.join(outDir, "logo.png"));
fs.copyFileSync(path.join(root, "rail.css"), path.join(outDir, "rail.css"));
// Built by ui/desktop/scripts/build-icons.mjs from the same mark the app icon
// uses. Without it the page asks for /favicon.ico and takes a 404 on every
// load — visible in the desktop window's console, which is where it was found.
fs.copyFileSync(path.join(root, "favicon.ico"), path.join(outDir, "favicon.ico"));

// chat.css resolves @font-face against its own URL, so the fonts directory has
// to sit beside the copied stylesheet.
{
  const fontsSrc = path.join(repo, "ui", "vscode", "media", "fonts");
  const fontsOut = path.join(outDir, "fonts");
  fs.mkdirSync(fontsOut, { recursive: true });
  for (const name of fs.readdirSync(fontsSrc)) {
    fs.copyFileSync(path.join(fontsSrc, name), path.join(fontsOut, name));
  }
}

console.log("bundled ui/web/static (" + out.split(/\r?\n/).length + " lines of JS)");
