import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const srcDir = path.join(root, "media", "chat-src");
const big = path.join(srcDir, "05-turn-ui.js");
const body = fs.readFileSync(big, "utf8").split(/\r?\n/);

const anchors = [
  { name: "05a-subagents-turn.js", match: (l) => l.trim().startsWith("function upsertSubagentTask") },
  { name: "05b-overlays.js", match: (l) => l.trim().startsWith("function hideOverlay") },
  { name: "05c-busy-palette.js", match: (l) => l.trim().startsWith("function runStatusLabel") },
  { name: "05d-tools.js", match: (l) => l.trim().startsWith("function toolKind") },
  { name: "05e-messages.js", match: (l) => l.trim().startsWith("function appendHistoryAssistantTurn") },
];

const idxs = [];
for (const a of anchors) {
  const i = body.findIndex(a.match);
  if (i < 0) {
    console.error("missing", a.name);
    process.exit(1);
  }
  idxs.push({ name: a.name, i });
}

for (let k = 0; k < idxs.length; k++) {
  const from = idxs[k].i;
  const to = k + 1 < idxs.length ? idxs[k + 1].i : body.length;
  const chunk = body.slice(from, to).join("\n").replace(/\s+$/, "") + "\n";
  fs.writeFileSync(path.join(srcDir, idxs[k].name), chunk);
  console.log(idxs[k].name, to - from, "lines");
}

fs.unlinkSync(big);
console.log("removed 05-turn-ui.js");
