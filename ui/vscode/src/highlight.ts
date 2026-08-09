import { createHighlighter, type Highlighter } from "shiki";

let highlighterPromise: Promise<Highlighter> | undefined;

const COMMON_LANGS = [
  "go",
  "typescript",
  "tsx",
  "javascript",
  "jsx",
  "python",
  "rust",
  "json",
  "yaml",
  "markdown",
  "css",
  "html",
  "bash",
  "shell",
  "sql",
  "toml",
  "xml",
  "csharp",
  "java",
  "kotlin",
  "swift",
  "ruby",
  "php",
  "plaintext",
] as const;

async function getHighlighter(): Promise<Highlighter> {
  if (!highlighterPromise) {
    highlighterPromise = createHighlighter({
      themes: ["dark-plus"],
      langs: [...COMMON_LANGS],
    });
  }
  return highlighterPromise;
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

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

/** Render one line with TextMate tokens (Shiki). Returns inner HTML only. */
export async function highlightLine(line: string, lang: string): Promise<string> {
  if (!line.trim()) {
    return escapeHtml(line);
  }
  const h = await getHighlighter();
  let grammar = lang;
  if (!h.getLoadedLanguages().includes(grammar as (typeof COMMON_LANGS)[number])) {
    grammar = "plaintext";
  }
  try {
    const full = h.codeToHtml(line, {
      lang: grammar,
      theme: "dark-plus",
    });
    const inner = full.replace(/^[\s\S]*?<code[^>]*>/, "").replace(/<\/code>[\s\S]*$/, "");
    return inner || escapeHtml(line);
  } catch {
    return escapeHtml(line);
  }
}

export async function highlightLines(
  lines: string[],
  lang: string
): Promise<string[]> {
  const out: string[] = [];
  for (const line of lines) {
    out.push(await highlightLine(line, lang));
  }
  return out;
}
