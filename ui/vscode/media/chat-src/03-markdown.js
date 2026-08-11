  function escapeHtml(text) {
    return String(text || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  /** @param {string} s */
  function markdownInline(s) {
    let x = escapeHtml(s);
    x = x.replace(/`([^`\n]+)`/g, '<code class="md-code">$1</code>');
    x = x.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
    x = x.replace(/(?<![*])\*([^*\n]+)\*(?![*])/g, "<em>$1</em>");
    x = x.replace(
      /\[([^\]]+)\]\(([^)\s]+)\)/g,
      '<a class="md-link" href="$2" target="_blank" rel="noopener noreferrer">$1</a>'
    );
    return x;
  }

  function pathLooksLikeFile(s) {
    const t = (s || "").trim();
    return /[./\\]/.test(t) || /\.[a-z0-9]{1,6}$/i.test(t);
  }

  /** @returns {{ lang?: string; path?: string; startLine?: number; endLine?: number }} */
  function parseCodeFenceMeta(openRest) {
    const raw = (openRest || "").trim();
    if (!raw) {
      return { lang: "plain" };
    }
    const gh = raw.match(/^(\d+):(\d+):(.+)$/);
    if (gh) {
      const fp = gh[3].trim();
      return { lang: langFromPath(fp), path: fp, startLine: +gh[1], endLine: +gh[2] };
    }
    const parts = raw.split(/\s+/);
    const first = parts[0] || "";
    const rest = parts.slice(1).join(" ").trim();

    /** @param {string} s */
    function parsePathLines(s) {
      const m1 = s.match(/^(.+?)\s+[Ll]ines?\s+(\d+)\s*[-–]\s*(\d+)$/);
      if (m1) {
        return { path: m1[1].trim(), startLine: +m1[2], endLine: +m1[3] };
      }
      const m2 = s.match(/^(.+?):(\d+)\s*[-–]\s*(\d+)$/);
      if (m2) {
        return { path: m2[1].trim(), startLine: +m2[2], endLine: +m2[3] };
      }
      const m3 = s.match(/^(.+?):(\d+)$/);
      if (m3) {
        return { path: m3[1].trim(), startLine: +m3[2], endLine: +m3[2] };
      }
      if (pathLooksLikeFile(s)) {
        return { path: s.trim() };
      }
      return null;
    }

    const combined = rest ? `${first} ${rest}` : first;
    const pl =
      parsePathLines(combined) ||
      parsePathLines(rest) ||
      (pathLooksLikeFile(first) && !rest ? parsePathLines(first) : null) ||
      (pathLooksLikeFile(rest) ? parsePathLines(rest) : null);

    if (pl?.path) {
      const lang =
        first && !pathLooksLikeFile(first) && first !== pl.path ? first : langFromPath(pl.path);
      return {
        lang,
        path: pl.path,
        startLine: pl.startLine,
        endLine: pl.endLine,
      };
    }
    if (first && !pathLooksLikeFile(first)) {
      return { lang: first };
    }
    return { lang: "plain" };
  }

  function codeRefTitle(filePath, startLine, endLine) {
    const base = basename(filePath);
    if (startLine && endLine && endLine !== startLine) {
      return `${base} Lines ${startLine}-${endLine}`;
    }
    if (startLine) {
      return `${base} Line ${startLine}`;
    }
    return base;
  }

  function buildCodeRefCardHtml(filePath, lang, startLine, endLine, code) {
    const title = codeRefTitle(filePath, startLine, endLine);
    return (
      `<div class="code-ref-card diff-preview-card"` +
      ` data-path="${escapeAttr(filePath)}" data-lang="${escapeAttr(lang || langFromPath(filePath))}"` +
      (startLine ? ` data-start-line="${startLine}"` : "") +
      (endLine ? ` data-end-line="${endLine}"` : "") +
      `>` +
      `<div class="diff-preview-head code-ref-head">` +
      diffExtBadgeHtml(filePath) +
      `<button type="button" class="diff-preview-name code-ref-title" title="Open file">${escapeAttr(title)}</button>` +
      `</div>` +
      `<pre class="code-ref-body md-pre"><code>${escapeHtml(code)}</code></pre>` +
      `</div>`
    );
  }

  function bindCodeRefCard(card) {
    if (!card || card.dataset.bound === "1") {
      return;
    }
    card.dataset.bound = "1";
    const path = card.dataset.path || "";
    card.querySelector(".code-ref-title")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      if (!path) {
        return;
      }
      if (/** @type {MouseEvent} */ (e).shiftKey) {
        openExternalFile(path, true);
      } else {
        openExternalFile(path, true);
      }
    });
  }

  async function enhanceCodeRefCards(root) {
    if (!root) {
      return;
    }
    const cards = root.querySelectorAll(".code-ref-card:not([data-enhanced])");
    for (const card of cards) {
      card.dataset.enhanced = "1";
      bindCodeRefCard(card);
      const path = card.dataset.path || "";
      const lang = card.dataset.lang || langFromPath(path);
      const pre = card.querySelector(".code-ref-body code");
      if (!pre) {
        continue;
      }
      const code = pre.textContent || "";
      const lines = code.split("\n");
      const startLine = Number(card.dataset.startLine) || 1;
      const htmlLines = await requestHighlight(lines, lang);
      const body = document.createElement("div");
      body.className = "code-ref-body diff-preview-body";
      lines.forEach((line, idx) => {
        const row = document.createElement("div");
        row.className = "code-ref-row";
        row.innerHTML =
          `<span class="code-ref-ln">${startLine + idx}</span>` +
          `<span class="code-ref-code">${htmlLines[idx] || escapeHtml(line) || "&nbsp;"}</span>`;
        body.appendChild(row);
      });
      pre.closest(".code-ref-body")?.replaceWith(body);
    }
  }

  /** @param {string} raw */
  function renderMarkdownToHtml(raw) {
    const text = String(raw || "");
    if (!text.trim()) {
      return "";
    }
    const lines = text.split("\n");
    const out = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      if (trimmed.startsWith("```")) {
        const meta = parseCodeFenceMeta(trimmed.slice(3).trim());
        i++;
        const codeLines = [];
        while (i < lines.length && !lines[i].trim().startsWith("```")) {
          codeLines.push(lines[i]);
          i++;
        }
        if (i < lines.length) {
          i++;
        }
        const code = codeLines.join("\n");
        if (meta.path) {
          out.push(
            buildCodeRefCardHtml(
              meta.path,
              meta.lang || langFromPath(meta.path),
              meta.startLine,
              meta.endLine,
              code
            )
          );
        } else {
          out.push(`<pre class="md-pre"><code>${escapeHtml(code)}</code></pre>`);
        }
        continue;
      }
      if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
        out.push('<hr class="md-hr">');
        i++;
        continue;
      }
      const h3 = line.match(/^###\s+(.+)$/);
      if (h3) {
        out.push(`<h3 class="md-h3">${markdownInline(h3[1])}</h3>`);
        i++;
        continue;
      }
      const h2 = line.match(/^##\s+(.+)$/);
      if (h2) {
        out.push(`<h2 class="md-h2">${markdownInline(h2[1])}</h2>`);
        i++;
        continue;
      }
      const h1 = line.match(/^#\s+(.+)$/);
      if (h1) {
        out.push(`<h1 class="md-h1">${markdownInline(h1[1])}</h1>`);
        i++;
        continue;
      }
      if (/^[-*+]\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^[-*+]\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^[-*+]\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ul class="md-ul">${items.join("")}</ul>`);
        continue;
      }
      if (/^\d+\.\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^\d+\.\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ol class="md-ol">${items.join("")}</ol>`);
        continue;
      }
      if (!trimmed) {
        i++;
        continue;
      }
      const para = [];
      while (
        i < lines.length &&
        lines[i].trim() &&
        !lines[i].trim().startsWith("```") &&
        !/^#{1,3}\s+/.test(lines[i]) &&
        !/^[-*+]\s+/.test(lines[i]) &&
        !/^\d+\.\s+/.test(lines[i]) &&
        !/^(-{3,}|\*{3,}|_{3,})$/.test(lines[i].trim())
      ) {
        para.push(lines[i]);
        i++;
      }
      out.push(`<p class="md-p">${markdownInline(para.join(" "))}</p>`);
    }
    return out.join("");
  }

  /** @param {HTMLElement} el @param {string} raw */
  function applyAssistantMarkdown(el, raw) {
    el.classList.add("turn-text", "md-body");
    el.innerHTML = renderMarkdownToHtml(raw);
    void enhanceCodeRefCards(el);
  }

  /** @type {number | null} */
  let mdRenderPending = null;

  /** @param {HTMLElement} el @param {string} raw */
  function scheduleAssistantMarkdown(el, raw) {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
    }
    mdRenderPending = requestAnimationFrame(() => {
      mdRenderPending = null;
      applyAssistantMarkdown(el, raw);
    });
  }

  function flushAssistantMarkdown() {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
      mdRenderPending = null;
    }
    if (assistantBubble) {
      applyAssistantMarkdown(assistantBubble, sanitizeAssistantStream(stripFinalEnvelope(streamRawText)));
    }
  }

  function highlightCode(line, lang) {
    let s = escapeHtml(line);
    if (lang === "plain" || !line.trim()) return s;
    const strRe = /(&quot;[^&]*?&quot;|'[^']*?'|`[^`]*?`)/g;
    const apply = (text, re, cls) => text.replace(re, (m) => `<span class="syn-${cls}">${m}</span>`);
    s = apply(s, strRe, "str");
    if (lang === "go") {
      s = apply(
        s,
        /\b(func|return|if|else|for|range|package|import|type|struct|interface|var|const|go|defer|switch|case|default|map|chan|select)\b/g,
        "kw"
      );
    } else if (lang === "ts" || lang === "tsx" || lang === "js" || lang === "jsx") {
      s = apply(
        s,
        /\b(const|let|var|function|return|if|else|for|while|import|export|from|class|interface|type|async|await|new|this)\b/g,
        "kw"
      );
    } else if (lang === "py") {
      s = apply(s, /\b(def|return|if|elif|else|for|while|import|from|class|with|as|pass|None|True|False)\b/g, "kw");
    } else if (lang === "rust") {
      s = apply(s, /\b(fn|let|mut|pub|use|struct|enum|impl|match|if|else|return|mod|crate)\b/g, "kw");
    }
    s = apply(s, /(\/\/.*$|#.*$)/g, "cm");
    return s;
  }

