// Security regression tests for the webview markdown renderer.
// The renderer lives in media/chat-src/03-markdown.js (vanilla JS bundled
// into chat.bundle.js); we evaluate the real source in a sandbox so these
// tests break if someone reintroduces an unescaped interpolation.
import { strict as assert } from "node:assert";
import { test } from "node:test";
import * as fs from "node:fs";
import * as path from "node:path";

type MarkdownExports = {
  escapeHtml: (s: string) => string;
  safeLinkHref: (s: string) => string;
  markdownInline: (s: string) => string;
  renderMarkdownToHtml: (s: string) => string;
};

function loadMarkdownModule(): MarkdownExports {
  const srcPath = path.join(__dirname, "..", "..", "media", "chat-src", "03-markdown.js");
  const src = fs.readFileSync(srcPath, "utf8");
  // Stub the cross-file globals the module references; none are exercised by
  // the pure string-rendering functions under test.
  const factory = new Function(
    "langFromPath",
    "escapeAttr",
    "diffExtBadgeHtml",
    "requestHighlight",
    "basename",
    "openExternalFile",
    "sanitizeAssistantStream",
    "stripFinalEnvelope",
    "document",
    "assistantBubble",
    "streamRawText",
    "requestAnimationFrame",
    "cancelAnimationFrame",
    src + "\nreturn { escapeHtml, safeLinkHref, markdownInline, renderMarkdownToHtml };"
  );
  const escapeAttr = (v: unknown) =>
    String(v ?? "").replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
  return factory(
    () => "plain", // langFromPath
    escapeAttr,
    () => "", // diffExtBadgeHtml
    async (lines: string[]) => lines, // requestHighlight
    (p: string) => p.split(/[\\/]/).pop() || p, // basename
    () => undefined, // openExternalFile
    (s: string) => s, // sanitizeAssistantStream
    (s: string) => s, // stripFinalEnvelope
    undefined, // document
    null, // assistantBubble
    "", // streamRawText
    () => 0, // requestAnimationFrame
    () => undefined // cancelAnimationFrame
  ) as MarkdownExports;
}

const md = loadMarkdownModule();

test("escapeHtml escapes quotes (attribute breakout defense)", () => {
  const out = md.escapeHtml(`<img src="x" onerror='alert(1)'>`);
  assert.ok(!out.includes('"'), `raw double quote survived: ${out}`);
  assert.ok(!out.includes("'"), `raw single quote survived: ${out}`);
  assert.ok(!out.includes("<"), `raw < survived: ${out}`);
});

test("markdownInline neutralizes javascript: links", () => {
  const out = md.markdownInline("[click](javascript:alert(1))");
  assert.ok(!out.includes("<a"), `anchor rendered for javascript: URL: ${out}`);
  assert.ok(out.includes("click"), "label text lost");
});

test("markdownInline neutralizes command: and data: links", () => {
  for (const scheme of ["command:workbench.action.terminal.new", "data:text/html,<b>x</b>", "vscode://x"]) {
    const out = md.markdownInline(`[go](${scheme})`);
    assert.ok(!out.includes("<a"), `anchor rendered for ${scheme}: ${out}`);
  }
});

test("markdownInline keeps normal https links", () => {
  const out = md.markdownInline("[docs](https://example.com/a?b=1)");
  assert.ok(out.includes('<a class="md-link" href="https://example.com/a?b=1"'), out);
});

test("markdownInline prevents href attribute breakout", () => {
  const out = md.markdownInline(`[x](https://a"onpointerover="alert(1))`);
  // The quote must arrive escaped — no raw `"` splitting the attribute.
  assert.ok(!/href="https:\/\/a"onpointerover/.test(out), `attribute breakout: ${out}`);
  assert.ok(!out.includes('"onpointerover="'), `attribute breakout: ${out}`);
});

test("renderMarkdownToHtml escapes script tags in code fences", () => {
  const out = md.renderMarkdownToHtml("```\n<script>alert(1)</script>\n```");
  assert.ok(!out.includes("<script>"), `script tag survived: ${out}`);
  assert.ok(out.includes("&lt;script&gt;"), `expected escaped script: ${out}`);
});

test("renderMarkdownToHtml escapes html in paragraphs and headings", () => {
  const out = md.renderMarkdownToHtml("# <img src=x onerror=alert(1)>\n\n<iframe></iframe>");
  assert.ok(!out.includes("<img"), out);
  assert.ok(!out.includes("<iframe"), out);
});
