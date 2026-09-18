// Shared by check-webview.mjs and check-web.mjs: the icon set, the names the
// code asks it for, and the shape a hand-written <svg> has to have.
//
// Two silent failure modes are what this file exists for. A mistyped icon name
// renders as nothing at all — orchIconMarkup returns "" for an unknown key, so
// a typo is an empty box on a toolbar, with no error anywhere. And an icon
// drawn by hand at some other size or stroke weight looks fine on its own and
// only reads as wrong beside its neighbours, which is precisely the thing the
// set was introduced to end.
import fs from "fs";
import path from "path";

/** Every name media/icons.js can draw. */
export function iconNames(mediaDir) {
  const src = fs.readFileSync(path.join(mediaDir, "icons.js"), "utf8");
  const start = src.indexOf("const ORCH_ICON_PATHS = {");
  if (start < 0) throw new Error("icons.js: ORCH_ICON_PATHS not found");
  const end = src.indexOf("\n  };", start);
  if (end < 0) throw new Error("icons.js: ORCH_ICON_PATHS is not closed");
  const body = src.slice(start, end);
  const out = new Set();
  const re = /^\s*(?:"([^"]+)"|([A-Za-z][\w-]*))\s*:\s*'/gm;
  let m;
  while ((m = re.exec(body))) out.add(m[1] || m[2]);
  return out;
}

/**
 * Every icon name a file asks for, as { name, line }. Only literal calls are
 * seen — a name held in a variable (the mode and access tables) is checked by
 * the table scan below instead.
 */
export function iconCallSites(file) {
  const lines = fs.readFileSync(file, "utf8").split(/\r?\n/);
  const out = [];
  lines.forEach((text, i) => {
    const re = /\borchIcon(?:Markup|El)\(\s*"([^"]+)"/g;
    let m;
    while ((m = re.exec(text))) out.push({ name: m[1], line: i + 1 });
  });
  return out;
}

/**
 * The `icon: "…"` fields of the mode and access tables, which reach
 * orchIconMarkup through a variable and so are invisible to the scan above.
 */
export function iconTableEntries(file) {
  const lines = fs.readFileSync(file, "utf8").split(/\r?\n/);
  const out = [];
  lines.forEach((text, i) => {
    const m = /^\s*(?:\{[^}]*?)?\bicon:\s*"([^"]+)"/.exec(text);
    if (m) out.push({ name: m[1], line: i + 1 });
  });
  return out;
}

/**
 * Hand-written <svg> elements in a static page or host file that do NOT match
 * the set's own shape. Brand marks are exempt: they are logos, not icons, and
 * are drawn filled on their own grid.
 */
export function strayIconSvgs(file) {
  const src = fs.readFileSync(file, "utf8");
  const lines = src.split(/\r?\n/);
  const out = [];
  lines.forEach((text, i) => {
    const m = /<svg\b([^>]*)>/.exec(text);
    if (!m) return;
    const attrs = m[1];
    if (/class="(?:rail-mark|start-mark)"/.test(attrs)) return; // the logo
    if (/viewBox="0 0 16 16"/.test(attrs) && /fill="currentColor"/.test(attrs)) return; // GitHub's mark
    const ok =
      /class="oi(?:\s|")/.test(attrs) &&
      /viewBox="0 0 24 24"/.test(attrs) &&
      /stroke-width="1\.75"/.test(attrs);
    if (!ok) out.push({ line: i + 1, text: text.trim().slice(0, 100) });
  });
  return out;
}
