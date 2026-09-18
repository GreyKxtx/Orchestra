// Reading the UI catalogue and the keys the markup asks it for.
//
// Shared by ui/vscode/scripts/check-webview.mjs and ui/web/scripts/check-web.mjs
// because both check the same thing on their own surface: a data-i18n key that
// is not in media/i18n.js is not an error anywhere — i18n() returns the key, so
// the screen reads "set.mcp.done" where it should say "Done". Nothing else
// catches that: the bundle still parses and every id is still present.

import fs from "fs";
import path from "path";

/**
 * The keys English carries. English is the fallback language, so its table is
 * the one that has to be complete — a key missing only from another language
 * degrades to readable English, which is by design.
 * @param {string} mediaDir path to ui/vscode/media
 * @returns {Set<string>}
 */
export function catalogueKeys(mediaDir) {
  const src = fs.readFileSync(path.join(mediaDir, "i18n.js"), "utf8");
  const start = src.indexOf("{", src.indexOf("const I18N_CATALOGUE"));
  if (start < 0) {
    return new Set();
  }
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    if (src[i] === "{") {
      depth++;
    } else if (src[i] === "}") {
      depth--;
      if (depth === 0) {
        // The literal holds string values only; there is nothing to execute.
        const table = new Function("return " + src.slice(start, i + 1))();
        return new Set(Object.keys(table.en || {}));
      }
    }
  }
  return new Set();
}

/**
 * Every data-i18n / -html / -title / -placeholder / -aria-label key in one
 * file, with the line it sits on.
 * @param {string} file
 * @returns {{ key: string; line: number }[]}
 */
export function markupKeys(file) {
  const src = fs.readFileSync(file, "utf8");
  /** @type {{ key: string; line: number }[]} */
  const found = [];
  src.split(/\r?\n/).forEach((line, i) => {
    for (const m of line.matchAll(/\bdata-i18n(?:-[a-z-]+)?="([^"]+)"/g)) {
      found.push({ key: m[1], line: i + 1 });
    }
  });
  return found;
}
