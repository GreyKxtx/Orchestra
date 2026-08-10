/** Syntax highlight for diff previews. Shiki is optional (dev-only); VSIX ships without node_modules. */

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

export function languageFromPath(filePath: string): string {
  const ext = (filePath.split(".").pop() || "").toLowerCase();
  const map: Record<string, string> = {
    go: "go",
    ts: "typescript",
    tsx: "tsx",
    js: "javascript",
    jsx: "jsx",
    py: "python",
    rs: "rust",
    md: "markdown",
    json: "json",
    yml: "yaml",
    yaml: "yaml",
    css: "css",
    html: "html",
    htm: "html",
    sh: "bash",
    bash: "bash",
    zsh: "bash",
    ps1: "powershell",
    sql: "sql",
    toml: "toml",
    xml: "xml",
    cs: "csharp",
    java: "java",
    kt: "kotlin",
    swift: "swift",
    rb: "ruby",
    php: "php",
  };
  return map[ext] || "plaintext";
}

/** Render one line. Uses Shiki when available locally; otherwise escaped plain text. */
export async function highlightLine(line: string, _lang: string): Promise<string> {
  if (!line.trim()) {
    return escapeHtml(line);
  }
  try {
    // Dynamic import: works in F5 dev (node_modules present); VSIX falls back to plain text.
    const { createHighlighter } = await import("shiki");
    const h = await createHighlighter({
      themes: ["dark-plus"],
      langs: ["go", "typescript", "javascript", "python", "json", "markdown", "bash", "plaintext"],
    });
    let grammar = _lang;
    if (!h.getLoadedLanguages().includes(grammar)) {
      grammar = "plaintext";
    }
    const full = h.codeToHtml(line, { lang: grammar, theme: "dark-plus" });
    const inner = full.replace(/^[\s\S]*?<code[^>]*>/, "").replace(/<\/code>[\s\S]*$/, "");
    return inner || escapeHtml(line);
  } catch {
    return escapeHtml(line);
  }
}

export async function highlightLines(lines: string[], lang: string): Promise<string[]> {
  const out: string[] = [];
  for (const line of lines) {
    out.push(await highlightLine(line, lang));
  }
  return out;
}
