import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const srcDir = path.join(root, "media", "settings-src");
const mediaDir = path.join(root, "media");
const outFile = path.join(root, "media", "settings.bundle.js");

// This panel is its own document and its own scope, so it gets its own copy of
// media/i18n.js — the same catalogue the chat renderer reads, which is why a
// string moved between the two surfaces keeps its key.
const order = [
  [srcDir, "00-header.txt"],
  [mediaDir, "i18n.js"],
  [srcDir, "01-core.js"],
  [srcDir, "01c-provider-logos.js"],
  [srcDir, "01b-provider-ui.js"],
  [srcDir, "02-models.js"],
  [srcDir, "03-orchestra.js"],
  [srcDir, "04-agents-mcp.js"],
  [srcDir, "05-state.js"],
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
  "/* AUTO-GENERATED — do not edit. Sources: media/i18n.js + media/settings-src/*.js  →  npm run bundle:webview */\n";
const parts = order.map(([dir, name]) =>
  fs.readFileSync(path.join(dir, name), "utf8").replace(/\s+$/, "")
);
const out = banner + parts.join("\n") + "\n";
fs.writeFileSync(outFile, out);

const legacy = path.join(root, "media", "settings.js");
if (fs.existsSync(legacy)) {
  fs.unlinkSync(legacy);
  console.log("removed legacy media/settings.js");
}

console.log("bundled", path.relative(root, outFile), "(" + out.split(/\r?\n/).length + " lines)");
console.log("edit sources under media/settings-src/ (not the bundle)");
