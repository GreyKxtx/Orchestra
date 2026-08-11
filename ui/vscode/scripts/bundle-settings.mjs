import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const srcDir = path.join(root, "media", "settings-src");
const outFile = path.join(root, "media", "settings.bundle.js");

const order = [
  "00-header.txt",
  "01-core.js",
  "01c-provider-logos.js",
  "01b-provider-ui.js",
  "02-models.js",
  "03-orchestra.js",
  "04-agents-mcp.js",
  "05-state.js",
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
  "/* AUTO-GENERATED — do not edit. Sources: media/settings-src/*.js  →  npm run bundle:webview */\n";
const parts = order.map((name) => fs.readFileSync(path.join(srcDir, name), "utf8").replace(/\s+$/, ""));
const out = banner + parts.join("\n") + "\n";
fs.writeFileSync(outFile, out);

const legacy = path.join(root, "media", "settings.js");
if (fs.existsSync(legacy)) {
  fs.unlinkSync(legacy);
  console.log("removed legacy media/settings.js");
}

console.log("bundled", path.relative(root, outFile), "(" + out.split(/\r?\n/).length + " lines)");
console.log("edit sources under media/settings-src/ (not the bundle)");
