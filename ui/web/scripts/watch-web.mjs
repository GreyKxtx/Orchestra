// Re-runs the bundler when a source fragment changes.
//
// The development loop is the production loop: build into ui/web/static and let
// the Go server serve it. A separate dev server on another port would be a
// different origin, so the cookie would not be sent and the WebSocket handshake
// would fail on Origin — which is why there is no dev-server mode.
//
// Run: node ui/web/scripts/watch-web.mjs   (no dependencies)

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const repo = path.join(root, "..", "..");

const watched = [
  path.join(repo, "ui", "vscode", "media", "chat-src"),
  path.join(repo, "ui", "vscode", "media"), // chat.css
  path.join(root, "src"),
  root, // index.src.html
];

const bundle = () => {
  try {
    execFileSync(process.execPath, [path.join(__dirname, "bundle-web.mjs")], { stdio: "inherit" });
  } catch (e) {
    console.error("bundle failed:", e.message);
  }
};

bundle();

let pending = null;
const schedule = () => {
  clearTimeout(pending);
  // Editors write in bursts; one rebuild per burst is enough.
  pending = setTimeout(bundle, 120);
};

for (const dir of watched) {
  if (!fs.existsSync(dir)) continue;
  fs.watch(dir, { persistent: true }, (_event, name) => {
    if (!name) return;
    if (/\.(js|txt|css|html)$/.test(name)) schedule();
  });
}

console.log("watching for changes — Ctrl+C to stop");
