import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const srcDir = path.join(root, "media", "chat-src");
const mediaDir = path.join(root, "media");
const outFile = path.join(root, "media", "chat.bundle.js");

// media/i18n.js and media/icons.js sit outside chat-src because the settings
// panel is a second bundle with a second scope and the same catalogue and the
// same icon set — one file each, four bundles.
const order = [
  [srcDir, "00-header.txt"],
  [mediaDir, "i18n.js"],
  [mediaDir, "icons.js"],
  [srcDir, "01-dom-state.js"],
  [srcDir, "02-util.js"],
  [srcDir, "03-markdown.js"],
  [srcDir, "04-diff-tools.js"],
  [srcDir, "05a-subagents-turn.js"],
  [srcDir, "05b-overlays.js"],
  [srcDir, "05c-busy-palette.js"],
  [srcDir, "05d-tools.js"],
  [srcDir, "05e-messages.js"],
  [srcDir, "05f-trajectory.js"],
  [srcDir, "06-composer.js"],
  [srcDir, "07-events.js"],
  [srcDir, "99-footer.txt"],
];

for (const [dir, name] of order) {
  const p = path.join(dir, name);
  if (!fs.existsSync(p)) {
    console.error("missing fragment:", path.relative(root, p));
    process.exit(1);
  }
}

const banner =
  "/* AUTO-GENERATED — do not edit. Sources: media/i18n.js + media/icons.js + media/chat-src/*.js  →  npm run bundle:webview */\n";
const parts = order.map(([dir, name]) =>
  fs.readFileSync(path.join(dir, name), "utf8").replace(/\s+$/, "")
);
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
