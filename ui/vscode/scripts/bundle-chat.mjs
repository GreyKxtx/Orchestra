import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const srcDir = path.join(root, "media", "chat-src");
const outFile = path.join(root, "media", "chat.bundle.js");

const order = [
  "00-header.txt",
  "01-dom-state.js",
  "02-util.js",
  "03-markdown.js",
  "04-diff-tools.js",
  "05a-subagents-turn.js",
  "05b-overlays.js",
  "05c-busy-palette.js",
  "05d-tools.js",
  "05e-messages.js",
  "06-composer.js",
  "07-events.js",
  "99-footer.txt",
];

for (const name of order) {
  const p = path.join(srcDir, name);
  if (!fs.existsSync(p)) {
    console.error("missing fragment:", name);
    process.exit(1);
  }
}

const banner =
  "/* AUTO-GENERATED — do not edit. Sources: media/chat-src/*.js  →  npm run bundle:webview */\n";
const parts = order.map((name) => fs.readFileSync(path.join(srcDir, name), "utf8").replace(/\s+$/, ""));
const out = banner + parts.join("\n") + "\n";
fs.writeFileSync(outFile, out);

// Remove legacy monolith if present (source of truth is chat-src/).
const legacy = path.join(root, "media", "chat.js");
if (fs.existsSync(legacy)) {
  fs.unlinkSync(legacy);
  console.log("removed legacy media/chat.js");
}

console.log("bundled", path.relative(root, outFile), "(" + out.split(/\r?\n/).length + " lines)");
console.log("edit sources under media/chat-src/ (not the bundle)");
