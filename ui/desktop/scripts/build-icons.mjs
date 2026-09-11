// Renders ui/desktop/icon.svg to every raster the app needs.
//
//   src-tauri/icons/32x32.png, 128x128.png, 128x128@2x.png, icon.png, icon.ico
//   ui/web/static/favicon.ico   (the page the window loads; without one the
//                                webview asks for /favicon.ico and gets a 404)
//
// There is no image library in this repository and none is being added for six
// PNGs. Headless Edge — already required to look at the UI at all — rasterises
// the SVG, and the ICO container is assembled here: since Vista an .ico entry
// may be a whole PNG file, so packing is a 6-byte header, a 16-byte directory
// entry per size, and the PNG bytes.
//
// Run: node ui/desktop/scripts/build-icons.mjs   (no dependencies)

import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const desktop = path.join(__dirname, "..");
const repo = path.join(desktop, "..", "..");
const svgPath = path.join(desktop, "icon.svg");
const iconsDir = path.join(desktop, "src-tauri", "icons");
const webStatic = path.join(repo, "ui", "web", "static");

const EDGE_CANDIDATES = [
  "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
  "C:/Program Files/Microsoft/Edge/Application/msedge.exe",
  "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
];

/** PNG sizes to render once and reuse for both the files and the .ico. */
const SIZES = [16, 32, 48, 64, 128, 256, 512];
/** What goes inside icon.ico / favicon.ico, smallest first. */
const ICO_SIZES = [16, 32, 48, 64, 256];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function findBrowser() {
  for (const p of EDGE_CANDIDATES) {
    if (fs.existsSync(p)) return p;
  }
  console.error("no Edge or Chrome found; cannot rasterise the SVG");
  process.exit(1);
}

/**
 * Render the SVG at each size. One browser, one page, one CDP session: the
 * page holds an <img> whose width/height are set per capture, and the shot is
 * clipped to exactly that box with the page background left transparent.
 * @param {string} svg @param {number[]} sizes
 * @returns {Promise<Map<number, Buffer>>}
 */
async function render(svg, sizes) {
  const browser = findBrowser();
  const profile = path.join(os.tmpdir(), "orchestra-icon-profile");
  const port = 9412;
  const dataUri = "data:image/svg+xml;base64," + Buffer.from(svg, "utf8").toString("base64");
  const html =
    "<!doctype html><html><head><style>html,body{margin:0;padding:0;background:transparent}" +
    "img{display:block}</style></head><body>" +
    `<img id="m" src="${dataUri}"></body></html>`;
  const pageUri = "data:text/html;base64," + Buffer.from(html, "utf8").toString("base64");

  const proc = spawn(
    browser,
    [
      "--headless=new",
      "--disable-gpu",
      "--no-sandbox",
      "--hide-scrollbars",
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-sync",
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${profile}`,
      "--window-size=1200,1200",
      pageUri,
    ],
    { stdio: "ignore" }
  );

  try {
    let target = null;
    for (let i = 0; i < 30 && !target; i++) {
      await sleep(400);
      try {
        const list = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
        target = list.find((t) => t.type === "page") || null;
      } catch {
        /* not up yet */
      }
    }
    if (!target) throw new Error("the headless browser never opened a page");

    const ws = new WebSocket(target.webSocketDebuggerUrl);
    let id = 0;
    const pending = new Map();
    ws.addEventListener("message", (ev) => {
      const m = JSON.parse(ev.data.toString());
      if (m.id && pending.has(m.id)) {
        pending.get(m.id)(m);
        pending.delete(m.id);
      }
    });
    await new Promise((r) => ws.addEventListener("open", r));
    const send = (method, params) =>
      new Promise((res) => {
        const myId = ++id;
        pending.set(myId, res);
        ws.send(JSON.stringify({ id: myId, method, params }));
      });

    await send("Page.enable", {});
    await send("Runtime.enable", {});
    await send("Emulation.setDefaultBackgroundColorOverride", {
      color: { r: 0, g: 0, b: 0, a: 0 },
    });
    // The <img> has to have decoded before the first capture, or the shot is
    // an empty box of the right size.
    await send("Runtime.evaluate", {
      expression: "document.getElementById('m').decode()",
      awaitPromise: true,
    });

    const out = new Map();
    for (const size of sizes) {
      await send("Runtime.evaluate", {
        expression: `(() => { const m = document.getElementById('m');
          m.style.width = '${size}px'; m.style.height = '${size}px'; })()`,
      });
      await sleep(60);
      const shot = await send("Page.captureScreenshot", {
        format: "png",
        clip: { x: 0, y: 0, width: size, height: size, scale: 1 },
        captureBeyondViewport: true,
      });
      const data = shot.result?.data;
      if (!data) throw new Error(`no screenshot at ${size}px`);
      out.set(size, Buffer.from(data, "base64"));
    }
    ws.close();
    return out;
  } finally {
    proc.kill();
  }
}

/**
 * Pack PNGs into an .ico. Entries are PNG-in-ICO, which every Windows since
 * Vista reads, and which keeps this to arithmetic.
 * @param {Array<{size: number, png: Buffer}>} entries
 */
function buildIco(entries) {
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0); // reserved
  header.writeUInt16LE(1, 2); // 1 = icon
  header.writeUInt16LE(entries.length, 4);

  const dir = Buffer.alloc(16 * entries.length);
  let offset = header.length + dir.length;
  entries.forEach((e, i) => {
    const at = i * 16;
    // 256 is written as 0: the field is one byte and 256 does not fit.
    dir.writeUInt8(e.size >= 256 ? 0 : e.size, at + 0);
    dir.writeUInt8(e.size >= 256 ? 0 : e.size, at + 1);
    dir.writeUInt8(0, at + 2); // palette
    dir.writeUInt8(0, at + 3); // reserved
    dir.writeUInt16LE(1, at + 4); // colour planes
    dir.writeUInt16LE(32, at + 6); // bits per pixel
    dir.writeUInt32LE(e.png.length, at + 8);
    dir.writeUInt32LE(offset, at + 12);
    offset += e.png.length;
  });

  return Buffer.concat([header, dir, ...entries.map((e) => e.png)]);
}

const svg = fs.readFileSync(svgPath, "utf8");
const pngs = await render(svg, SIZES);

fs.mkdirSync(iconsDir, { recursive: true });
const written = [];
const write = (file, buf) => {
  fs.writeFileSync(file, buf);
  written.push(path.relative(repo, file));
};

write(path.join(iconsDir, "32x32.png"), pngs.get(32));
write(path.join(iconsDir, "128x128.png"), pngs.get(128));
write(path.join(iconsDir, "128x128@2x.png"), pngs.get(256));
write(path.join(iconsDir, "icon.png"), pngs.get(512));

const ico = buildIco(ICO_SIZES.map((size) => ({ size, png: pngs.get(size) })));
write(path.join(iconsDir, "icon.ico"), ico);
write(path.join(webStatic, "favicon.ico"), ico);
// bundle-web.mjs copies static assets from here; keep the source beside them.
write(path.join(repo, "ui", "web", "favicon.ico"), ico);

console.log("icons written:\n  " + written.join("\n  "));
