import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const chatJs = path.join(root, "media", "chat.js");
const outDir = path.join(root, "media", "chat-src");

fs.mkdirSync(outDir, { recursive: true });

const src = fs.readFileSync(chatJs, "utf8");
const lines = src.split(/\r?\n/);

let start = 0;
for (let i = 0; i < lines.length; i++) {
  if (lines[i].includes("(function ()") || lines[i].includes("(function()")) {
    start = i + 1;
    break;
  }
}
let end = lines.length - 1;
for (let i = lines.length - 1; i >= 0; i--) {
  if (lines[i].trim() === "})();") {
    end = i;
    break;
  }
}

const body = lines.slice(start, end);
const anchors = [
  { name: "01-dom-state.js", match: (l) => l.includes("const vscode = acquireVsCodeApi") },
  { name: "02-util.js", match: (l) => l.trim().startsWith("function escapeAttr") },
  { name: "03-markdown.js", match: (l) => l.trim().startsWith("function escapeHtml") },
  { name: "04-diff-tools.js", match: (l) => l.trim().startsWith("function isMutatingToolBlock") },
  { name: "05-turn-ui.js", match: (l) => l.trim().startsWith("function upsertSubagentTask") },
  { name: "06-composer.js", match: (l) => l.trim().startsWith("function initModeMenu") },
  {
    name: "07-events.js",
    match: (l) => l.includes('window.addEventListener("message"'),
  },
];

const idxs = [];
for (const a of anchors) {
  const i = body.findIndex(a.match);
  if (i < 0) {
    console.error("missing anchor for", a.name);
    process.exit(1);
  }
  idxs.push({ name: a.name, i });
}
idxs.sort((a, b) => a.i - b.i);

for (let k = 0; k < idxs.length; k++) {
  const from = idxs[k].i;
  const to = k + 1 < idxs.length ? idxs[k + 1].i : body.length;
  const chunk = body.slice(from, to).join("\n") + "\n";
  fs.writeFileSync(path.join(outDir, idxs[k].name), chunk);
  console.log(idxs[k].name, to - from, "lines");
}

fs.writeFileSync(path.join(outDir, "00-header.txt"), lines.slice(0, start).join("\n") + "\n");
fs.writeFileSync(path.join(outDir, "99-footer.txt"), lines.slice(end).join("\n") + "\n");
console.log("split ok, body lines:", body.length);
