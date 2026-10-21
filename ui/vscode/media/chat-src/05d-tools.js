  /**
   * The family a tool belongs to. Everything downstream — the icon, the
   * heading, the CSS accent — hangs off this one answer, so a tool missing
   * here is a step that renders as a nameless dot.
   *
   * The families below cover `internal/tools/registry.go` as it stands; the
   * prefix rules at the end are what keep a newly registered `git.*` or
   * `mcp:*` tool recognised without another edit here.
   */
  function toolKind(name) {
    const n = (name || "").toLowerCase();
    if (["read", "fs.read"].includes(n)) return "read";
    if (["ls", "list", "fs.list"].includes(n)) return "list";
    if (["write", "edit", "fs.write", "file.write_atomic"].includes(n)) return "write";
    if (["grep", "search.text", "search"].includes(n)) return "search";
    if (["glob"].includes(n)) return "glob";
    if (["symbols", "code.symbols"].includes(n)) return "symbols";
    if (["bash", "exec.run", "exec"].includes(n)) return "exec";
    if (["task", "task_spawn", "task_wait", "task_cancel", "task_result"].includes(n)) return "task";
    if (["todowrite", "todoread"].includes(n)) return "todo";
    if (["explore"].includes(n)) return "explore";
    if (["diff.preview"].includes(n)) return "diff";
    if (["fs.delete"].includes(n)) return "trash";
    if (["fs.rename"].includes(n)) return "rename";
    if (["webfetch", "websearch"].includes(n)) return "web";
    if (["memory_write"].includes(n)) return "memory";
    if (["question"].includes(n)) return "question";
    if (["skill_invoke"].includes(n)) return "skill";
    if (["plan_exit", "plan_enter"].includes(n)) return "plan";
    if (["runtime_query"].includes(n)) return "runtime";
    // Families, so a tool added to one of them needs nothing here.
    if (n.startsWith("git.") || n.startsWith("gh.")) return "git";
    if (n.startsWith("lsp.")) return "lsp";
    if (n.startsWith("mcp:")) return "mcp";
    if (n.startsWith("browser.") || n.startsWith("browser_")) return "web";
    return "other";
  }

  /**
   * The icon NAME (see media/icons.js) for a tool — never a glyph. Every
   * family has one, the fallback included: a step with no icon is a step
   * the reader cannot tell apart from its neighbours at a glance, which is
   * the whole job of this column.
   */
  const TOOL_ICON_BY_KIND = {
    read: "read",
    list: "list",
    write: "write",
    search: "search",
    glob: "glob",
    symbols: "symbols",
    exec: "exec",
    task: "task",
    todo: "todo",
    explore: "explore",
    diff: "diff",
    trash: "trash",
    rename: "write",
    web: "web",
    memory: "memory",
    question: "question",
    skill: "skill",
    plan: "mode-plan",
    runtime: "lsp",
    git: "git",
    lsp: "lsp",
    mcp: "mcp",
    other: "tool",
  };

  function toolIcon(name) {
    return TOOL_ICON_BY_KIND[toolKind(name)] || "tool";
  }

  /** The icon as markup, at the size a tool row uses. */
  function toolIconMarkup(name) {
    return orchIconMarkup(toolIcon(name), { size: "md" });
  }

  function toolDisplayName(name) {
    switch (toolKind(name)) {
      case "read":
        return "Read";
      case "list":
        return "Ls";
      case "write":
        return name === "edit" ? "Edit" : "Write";
      case "search":
        return "Grep";
      case "glob":
        return "Glob";
      case "symbols":
        return "Symbols";
      case "exec":
        return "Shell";
      case "task":
        return "Task";
      default:
        // The raw name. `git.log`, `lsp.references` and `mcp:ctx7:query-docs`
        // say more about the step than any word this function could invent
        // for them, and they are the names the docs and the CLI use.
        return name || "Tool";
    }
  }

  /**
   * Whether a finished tool's result is a failure. The core reports tool
   * errors as the result text rather than out of band, so this is the only
   * signal a renderer has — keep it in one place so the head, the accent
   * and the subagent tree all agree on what failed.
   */
  function toolResultIsError(content) {
    const s = String(content || "").trimStart().toLowerCase();
    return s.startsWith("error") || s.startsWith('{"error"') || s.startsWith('{"ok":false');
  }

  function parseToolArgs(raw) {
    const out = {};
    const text = (raw || "").trim();
    if (!text) return out;
    try {
      return JSON.parse(text);
    } catch {
      const patched = text + '"'.repeat(text.split('"').length % 2 === 0 ? 0 : 1);
      let depth = 0;
      let inStr = false;
      let esc = false;
      for (const ch of patched) {
        if (esc) {
          esc = false;
          continue;
        }
        if (ch === "\\") {
          esc = true;
          continue;
        }
        if (ch === '"') inStr = !inStr;
        else if (!inStr) {
          if (ch === "{") depth++;
          if (ch === "}") depth--;
        }
      }
      let fix = patched;
      for (let i = 0; i < depth; i++) fix += "}";
      try {
        return JSON.parse(fix);
      } catch {
        return out;
      }
    }
  }

  function toolPathFromArgs(name, argsRaw, content) {
    const args = parseToolArgs(argsRaw);
    let path =
      args.path || args.filePath || args.file_path || args.target || "";
    if (!path && content) {
      try {
        const parsed = JSON.parse(content);
        path = parsed.path || parsed.filePath || parsed.file_path || "";
      } catch {
        /* ignore */
      }
    }
    return typeof path === "string" ? path.trim() : "";
  }

  /**
   * A row's words: the tool, then what it ran on — "Read · src/app.js",
   * "Glob · *", "Shell · List files in the working directory". The detail is
   * the path as the model gave it, the pattern, or a command's own
   * description (the command itself when it has none, and in the tooltip).
   * @returns {{ name: string; detail: string; title: string }}
   */
  function toolRowParts(name, argsRaw, content) {
    const disp = toolDisplayName(name);
    const kind = toolKind(name);
    // A search names what it looked for; the folder it looked in ("." more
    // often than not) goes to the tooltip.
    if (kind === "search" || kind === "glob") {
      const args = parseToolArgs(argsRaw);
      const pat = String(args.pattern || args.query || "");
      const where = String(args.path || "");
      if (pat) return { name: disp, detail: pat, title: where ? pat + "  in  " + where : pat };
    }
    const path = toolPathFromArgs(name, argsRaw, content);
    if (path) {
      return { name: disp, detail: path, title: path };
    }
    if (kind === "exec") {
      const args = parseToolArgs(argsRaw);
      const cmd = String(args.command || "");
      const desc = String(args.description || "");
      return { name: disp, detail: desc || cmd, title: cmd || desc };
    }
    if (content && content.length < 80) {
      return { name: disp, detail: content, title: "" };
    }
    return { name: disp, detail: "", title: "" };
  }

  // formatToolDuration renders a tool's wall time the way the TUI does
  // (ui/tui/view/tool_group.go): sub-second work in ms because "0.4s" hides
  // the difference between 350ms and 450ms, seconds with one decimal, and
  // anything past a minute in m/s because "91.4s" stops being readable.
  function formatToolDuration(ms) {
    if (!Number.isFinite(ms) || ms < 0) return "";
    if (ms < 1000) return Math.round(ms) + "ms";
    // 59950+ rounds to "60.0s", which reads like a bug next to "1m 00s".
    if (ms < 59950) return (ms / 1000).toFixed(1) + "s";
    // Round to whole seconds BEFORE splitting: rounding the remainder
    // separately turns 59999ms into "0m 60s".
    const totalSec = Math.round(ms / 1000);
    return Math.floor(totalSec / 60) + "m " + String(totalSec % 60).padStart(2, "0") + "s";
  }

  /* ---- a tool's result, made readable ------------------------------ *
   * Tool results arrive as one string. Most are JSON, and most of those
   * are the core's `{"output": "…"}` envelope wrapping text that was never
   * JSON to begin with — so printed raw, the thing a reader wants is
   * behind a layer of escaped newlines and quotes. These four functions
   * unwrap that, pretty-print what is genuinely structured, and keep the
   * unmodified original one click away.
   * ------------------------------------------------------------------ */

  /** Lines shown before the body asks to be expanded. */
  const TOOL_BODY_PREVIEW_LINES = 24;
  /** The ceiling even an expanded body will not go past, in lines. */
  const TOOL_BODY_MAX_LINES = 5000;

  /** The full, unmodified result text of a block, kept out of the DOM. */
  const toolFullContent = new WeakMap();

  /**
   * What to actually show for a result string.
   * @param {string} raw
   * @returns {{ text: string; kind: "json" | "text"; unwrapped: boolean }}
   */
  function toolResultView(raw) {
    const s = String(raw == null ? "" : raw);
    const trimmed = s.trim();
    if (!trimmed || (trimmed[0] !== "{" && trimmed[0] !== "[")) {
      return { text: s, kind: "text", unwrapped: false };
    }
    let parsed;
    try {
      parsed = JSON.parse(trimmed);
    } catch {
      return { text: s, kind: "text", unwrapped: false };
    }
    // The core's single-field envelope. Its payload is text — command
    // output, a file listing, a log — and reading it as text is the whole
    // point of unwrapping it.
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const keys = Object.keys(parsed);
      if (keys.length === 1 && typeof parsed[keys[0]] === "string" && ["output", "text", "content", "result", "stdout"].includes(keys[0])) {
        return { text: parsed[keys[0]], kind: "text", unwrapped: true };
      }
    }
    return { text: JSON.stringify(parsed, null, 2), kind: "json", unwrapped: false };
  }

  /**
   * Colour a pretty-printed JSON document. A tokeniser rather than a
   * parser: it runs over text this renderer produced with
   * JSON.stringify, so the grammar it has to survive is only ever that.
   * Everything is escaped before a span goes near it.
   */
  function highlightJson(text) {
    return escapeHtml(text).replace(
      /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
      (m, str, colon, lit, num) => {
        if (str) {
          return colon
            ? `<span class="jkey">${str}</span>${colon}`
            : `<span class="jstr">${str}</span>`;
        }
        if (lit) return `<span class="jlit">${lit}</span>`;
        if (num) return `<span class="jnum">${num}</span>`;
        return m;
      }
    );
  }

  /** "12 lines · 3.4 KB", the label that tells you what you are not seeing. */
  function toolBodyMeta(lineCount, byteLength) {
    const kb = byteLength / 1024;
    const size = kb >= 1 ? `${kb >= 10 ? Math.round(kb) : kb.toFixed(1)} KB` : `${byteLength} B`;
    return `${i18n("tool.body.lines", { n: lineCount })} · ${size}`;
  }

  /**
   * Paint a tool block's body from the result kept in toolFullContent,
   * honouring the block's own two switches: pretty vs raw, and folded vs
   * whole. Called on completion and again on every click that flips one.
   */
  function renderToolBody(block) {
    const body = block?.querySelector?.(".tool-body");
    if (!body) return;
    const pre = body.querySelector(".tool-body-pre");
    const metaEl = body.querySelector(".tool-body-meta");
    const moreBtn = body.querySelector(".tool-body-more");
    const fmtBtn = body.querySelector('[data-body-action="format"]');
    if (!pre) return;

    const full = toolFullContent.get(block) || "";
    const raw = block.dataset.bodyFormat === "raw";
    // Parsed once. A tool result can be a megabyte of JSON, and this runs
    // again on every fold, unfold and format flip.
    const formatted = toolResultView(full);
    const view = raw ? { text: full, kind: "text", unwrapped: false } : formatted;
    const lines = view.text.split("\n");
    const expanded = block.dataset.bodyExpanded === "1";
    const capped = lines.length > TOOL_BODY_MAX_LINES;
    const shown = expanded
      ? lines.slice(0, TOOL_BODY_MAX_LINES)
      : lines.slice(0, TOOL_BODY_PREVIEW_LINES);
    const text = shown.join("\n");

    if (view.kind === "json") {
      pre.innerHTML = highlightJson(text);
    } else {
      pre.textContent = text;
    }
    pre.classList.toggle("is-json", view.kind === "json");

    if (metaEl) metaEl.textContent = toolBodyMeta(lines.length, full.length);
    if (fmtBtn) {
      // Offered only when there is a second way to read the same bytes.
      fmtBtn.hidden = !(formatted.kind === "json" || formatted.unwrapped);
      fmtBtn.textContent = raw ? i18n("tool.body.pretty") : i18n("tool.body.raw");
    }
    if (moreBtn) {
      const hidden = lines.length - shown.length;
      if (hidden > 0) {
        moreBtn.hidden = false;
        moreBtn.textContent = i18n("tool.body.show_all", { n: lines.length });
      } else if (expanded && lines.length > TOOL_BODY_PREVIEW_LINES) {
        moreBtn.hidden = false;
        moreBtn.textContent = capped ? i18n("tool.body.capped", { n: TOOL_BODY_MAX_LINES }) : i18n("tool.body.collapse");
        moreBtn.disabled = capped;
      } else {
        moreBtn.hidden = true;
      }
    }
  }

  /** Hand a finished tool's result to the body and paint it. */
  function setToolBodyContent(block, content) {
    if (!block) return;
    toolFullContent.set(block, String(content == null ? "" : content));
    renderToolBody(block);
  }

  /**
   * Copy the whole result — the original bytes, not the prettified view,
   * because what gets pasted into a shell or an issue has to be what the
   * tool actually returned.
   */
  function copyToolBody(block, btn) {
    const full = toolFullContent.get(block) || "";
    const done = (ok) => {
      if (!btn) return;
      btn.textContent = i18n(ok ? "tool.body.copied" : "tool.body.copy_failed");
      setTimeout(() => {
        btn.textContent = i18n("tool.body.copy");
      }, 1400);
    };
    try {
      navigator.clipboard.writeText(full).then(
        () => done(true),
        () => done(false)
      );
    } catch {
      done(false);
    }
  }

  /** Append streamed exec output to what the body already holds. */
  function appendToolBodyContent(block, chunk) {
    if (!block) return;
    toolFullContent.set(block, (toolFullContent.get(block) || "") + String(chunk || ""));
    // Live output is watched, not skimmed: keep it whole as it arrives.
    block.dataset.bodyExpanded = "1";
    renderToolBody(block);
  }

  function updateToolHead(block, name, argsRaw, content, running) {
    const head = block.querySelector(".tool-head");
    if (!head) return;
    // Durations exist only for tools this session actually watched run.
    // Restored history has none — the session snapshot does not persist
    // them — and replaying a start/complete pair would time the replay, not
    // the tool, printing "0ms" beside every historical call.
    const durEl = head.querySelector(".tool-dur");
    if (durEl) {
      const ms = Number(block.dataset.durationMs);
      durEl.textContent = block.dataset.durationMs ? formatToolDuration(ms) : "";
    }
    const icon = head.querySelector(".tool-icon");
    const label = head.querySelector(".tool-label");
    const sub = head.querySelector(".tool-sub");
    const statsEl = head.querySelector(".tool-stats");
    const filePath = toolPathFromArgs(name, argsRaw, content);
    if (filePath) {
      block.dataset.filePath = filePath;
    }
    // The icon states WHICH tool ran and never stops doing so. It used to be
    // replaced by a check mark on completion, which left every finished step
    // — the overwhelming majority of what is on screen — with no mark of its
    // own kind at all. Success needs no badge once the spinner is gone;
    // failure does, and gets one.
    if (icon && icon.dataset.iconFor !== name) {
      icon.innerHTML = toolIconMarkup(name);
      icon.dataset.iconFor = name || "";
    }
    if (!running) {
      block.classList.toggle("tool-failed", toolResultIsError(content));
    }
    if (label) {
      const parts = toolRowParts(name, argsRaw, content);
      label.textContent = "";
      const nameEl = document.createElement("span");
      nameEl.className = "tr-name";
      nameEl.textContent = parts.name;
      label.appendChild(nameEl);
      if (parts.detail) {
        const detailEl = document.createElement("span");
        detailEl.className = "tr-detail" + (filePath && parts.detail === filePath ? " tr-path" : "");
        detailEl.textContent = parts.detail;
        label.appendChild(detailEl);
      }
      if (parts.title) head.title = parts.title;
    }
    // The detail carries the whole path now; a second copy at the right edge
    // said the same thing smaller.
    if (sub) sub.textContent = "";
    if (statsEl && filePath && toolKind(name) === "write") {
      const diff = findDiffForPath(filePath);
      if (diff) {
        statsEl.innerHTML = statsHtml(countDiffStats(diff.before, diff.after));
      }
    }
  }

  function insertFileChangeCard(path, content, parent) {
    const host = parent || messagesEl;
    if (!host || !path) return;
    if (parent && parent.classList.contains("tool-block")) {
      attachToolDiffShell(parent, path);
      return;
    }
    const diff = findDiffForPath(path);
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(parent || document.createElement("div"), path, diff.before || "", diff.after || "");
      return;
    }
    attachToolDiffShell(parent || document.createElement("div"), path);
  }

  /* ---- the Subagents tab --------------------------------------------------
   * The delegation drawn as a tree, top down: the Lead, under it the agents
   * it started, under each the ones that agent started (parent_task_id). A
   * node is a block with the role's icon and colour, its status, its time and
   * its task; a click opens a card beside it — what the role does, what the
   * agent is doing now or why it failed, its files, its result, who started
   * it and whom it started. The stage pans and zooms; it fits itself while
   * the user has not moved it. The tab's segment appears once the chat has a
   * subagent, with how many and a mark while any runs.
   */
  const SA_COL = 262;
  const SA_ROW = 170;
  const SA_PHASES = ["discovery", "documentation", "contract", "execution", "delivery"];

  /** The roles drawn in a colour of their own; any other is "agent". */
  const SA_ROLES = ["explore", "scout", "ask", "product", "documentation", "architecture", "worker", "verifier", "debug", "general"];
  const SA_ROLE_ICON = {
    lead: "role-lead",
    product: "role-product",
    documentation: "role-docs",
    architecture: "task",
    scout: "explore",
    explore: "search",
    ask: "question",
    worker: "role-worker",
    verifier: "role-verifier",
    debug: "role-debug",
    general: "tool",
    agent: "tool",
  };

  /** The phase the Lead last wrote to state.md, "" until it has. */
  let orchestraPhase = "";
  /** What each subagent's model calls cost, by task: tokens in/out, dollars, calls, last context. */
  const saUsage = new Map();
  /** The agency's messages this turn, oldest first. */
  const saMessages = [];
  /** The user's message the turn answers, for the Orchestrator's card. */
  let saTurnGoal = "";
  /** The Orchestrator's own turn: when it ran, its calls, what it is doing. */
  const saLead = { startedAt: 0, endedAt: 0, calls: 0, now: "" };
  /** True while a chat's log is replayed into the tab: one drawing at the end, not one per event. */
  let saBatch = false;
  const SA_LAYOUTS = ["rays", "tree", "constellation"];
  const saView = {
    x: 0,
    y: 0,
    k: 1,
    moved: false,
    fitted: "",
    selected: "",
    layout: saStoredLayout(),
    /** Positions of the last drawing, and the shape they were laid out for. */
    pos: new Map(),
    posShape: "",
    /** A replay of the turn: its clock, and the span it plays. */
    replay: null,
    stepsOpen: false,
    headBound: false,
    stage: null,
    world: null,
    edges: null,
    pop: null,
    nodes: new Map(),
    ticker: 0,
  };

  function saStoredLayout() {
    try {
      const v = localStorage.getItem("orchestra.subagents.layout");
      if (v && SA_LAYOUTS.includes(v)) return v;
    } catch {
      // No storage in a preview or a locked-down webview: the default it is.
    }
    return "tree";
  }

  /** @param {string} type */
  function subagentRole(type) {
    const t = String(type || "").toLowerCase();
    return SA_ROLES.includes(t) ? t : "agent";
  }

  /** "Product Lead", or the type as it came when the role is not a built-in. @param {string} type */
  function subagentRoleName(type) {
    const role = subagentRole(type);
    return role === "agent" && type && type !== "agent" ? String(type) : i18n("role." + role);
  }

  /** @param {string} s @param {number} n */
  function clipText(s, n) {
    const t = String(s || "");
    return t.length > n ? t.slice(0, n - 1) + "…" : t;
  }

  /**
   * A node's caption: the task, not the role the prompt opens with — two
   * "You are the Backend Lead for Augur…" nodes read the same.
   * @param {string} goal
   */
  function graphCaption(goal) {
    const g = String(goal || "").trim();
    const m = /^you are [^.\n]{0,160}[.\n]\s*(\S[\s\S]*)$/i.exec(g);
    return m ? m[1] : g;
  }

  /** The live record of a subagent: tool count, what it is doing, its files. */
  function saLive(sa) {
    return (sa.taskId && subagentByTask.get(sa.taskId)) || sa;
  }

  function saKey(sa) {
    return sa.taskId || sa.id;
  }

  /** "1:42", "28 s"; "" when the agent was never timed (a reopened chat). @param {number|null} ms */
  function saClock(ms) {
    if (ms == null || !Number.isFinite(ms)) return "";
    const s = Math.max(0, Math.round(ms / 1000));
    const m = Math.floor(s / 60);
    return m ? `${m}:${String(s % 60).padStart(2, "0")}` : i18n("subagents.seconds", { n: s });
  }

  function saElapsed(sa) {
    return sa.startedAt ? (sa.endedAt || (saView.replay ? saView.replay.clock : Date.now())) - sa.startedAt : null;
  }

  /**
   * The agents on the stage: all of them, or during a replay the ones its
   * clock has reached, as they were then.
   */
  function saShown() {
    const r = saView.replay;
    if (!r) return subagents;
    return subagents
      .filter((sa) => !sa.startedAt || sa.startedAt <= r.clock)
      .map((sa) =>
        !sa.startedAt || (sa.endedAt && sa.endedAt <= r.clock)
          ? sa
          : Object.assign({}, sa, { status: sa.status === "waiting" ? "waiting" : "running", endedAt: 0, result: "", error: "" })
      );
  }

  const saBusy = (s) => s.status === "running" || s.status === "waiting";

  /** The tree: who started whom, the Lead at the root under the key "lead". */
  function saTree(list) {
    const all = list || saShown();
    const byTask = new Map();
    all.forEach((sa) => {
      if (sa.taskId) byTask.set(sa.taskId, sa);
    });
    const kids = new Map([["lead", []]]);
    const parentOf = new Map();
    all.forEach((sa) => {
      const p = sa.parentTaskId && byTask.has(sa.parentTaskId) && sa.parentTaskId !== sa.taskId ? sa.parentTaskId : "lead";
      if (!kids.has(p)) kids.set(p, []);
      kids.get(p).push(sa);
      parentOf.set(saKey(sa), p);
    });
    return { all, kids, parentOf, byKey: new Map(all.map((sa) => [saKey(sa), sa])) };
  }

  /** Positions in the chosen layout, laid out again only when the tree changed shape. */
  function saLayout(tree) {
    const shape = saView.layout + "|" + [...tree.parentOf].map(([k, p]) => k + ">" + p).join(",");
    if (saView.posShape === shape && saView.pos.size) return saView.pos;
    const pos =
      saView.layout === "rays" ? saLayoutRays(tree) : saView.layout === "constellation" ? saLayoutConstellation(tree) : saLayoutTree(tree);
    saView.pos = pos;
    saView.posShape = shape;
    return pos;
  }

  /** How many leaves hang under a key: the width its subtree needs. */
  function saLeaves(tree, key, depth) {
    const k = tree.kids.get(key) || [];
    if (!k.length || depth > 16) return 1;
    return k.reduce((n, c) => n + saLeaves(tree, saKey(c), depth + 1), 0);
  }

  /**
   * Rays: each subtree gets a wedge of the circle as wide as its leaves and
   * every level is one ring further out, so a department keeps its direction.
   */
  function saLayoutRays(tree) {
    const pos = new Map([["lead", { x: 0, y: 0, angle: 0 }]]);
    const seen = new Set(["lead"]);
    const base = Math.max(330, (saLeaves(tree, "lead", 0) * 250) / (2 * Math.PI));
    const place = (key, a0, a1, depth) => {
      const k = (tree.kids.get(key) || []).filter((c) => !seen.has(saKey(c)));
      k.forEach((c) => seen.add(saKey(c)));
      const total = k.reduce((n, c) => n + saLeaves(tree, saKey(c), 0), 0) || 1;
      const r = base + (depth - 1) * 290;
      let a = a0;
      k.forEach((c) => {
        const span = ((a1 - a0) * saLeaves(tree, saKey(c), 0)) / total;
        const mid = a + span / 2;
        pos.set(saKey(c), { x: Math.cos(mid) * r, y: Math.sin(mid) * r * 0.82, angle: mid });
        place(saKey(c), a, a + span, depth + 1);
        a += span;
      });
    };
    place("lead", -Math.PI * 0.95, Math.PI * 1.05, 1);
    return pos;
  }

  /**
   * Constellation: the rays as a seed, then a small simulation — blocks push
   * each other apart, each is pulled toward its parent and out along its
   * department's direction, so clusters form where the work is.
   */
  function saLayoutConstellation(tree) {
    const seed = saLayoutRays(tree);
    const ids = [...seed.keys()];
    const p = new Map();
    ids.forEach((id, i) => {
      const sd = seed.get(id);
      p.set(id, { x: sd.x * 0.8 + Math.sin(i * 7.3) * 30, y: sd.y * 0.8 + Math.cos(i * 3.1) * 30 });
    });
    const dirOf = (id) => {
      let k = id;
      for (let i = 0; i < 16 && tree.parentOf.get(k) && tree.parentOf.get(k) !== "lead"; i++) k = tree.parentOf.get(k);
      return (seed.get(k) || { angle: 0 }).angle || 0;
    };
    const iters = ids.length > 60 ? 160 : 320;
    for (let it = 0; it < iters; it++) {
      const f = new Map(ids.map((id) => [id, { x: 0, y: 0 }]));
      for (let i = 0; i < ids.length; i++)
        for (let j = i + 1; j < ids.length; j++) {
          const A = p.get(ids[i]);
          const B = p.get(ids[j]);
          const fa = f.get(ids[i]);
          const fb = f.get(ids[j]);
          const dx = B.x - A.x;
          const dy = B.y - A.y;
          const ox = 264 - Math.abs(dx);
          const oy = 110 - Math.abs(dy);
          if (ox > 0 && oy > 0) {
            if (ox < oy * 2.2) {
              const m = (dx >= 0 ? 1 : -1) * ox * 0.5;
              fa.x -= m;
              fb.x += m;
            } else {
              const m = (dy >= 0 ? 1 : -1) * oy * 0.5;
              fa.y -= m;
              fb.y += m;
            }
          } else {
            const k = 90 / (dx * dx + dy * dy + 1);
            fa.x -= dx * k;
            fa.y -= dy * k;
            fb.x += dx * k;
            fb.y += dy * k;
          }
        }
      ids.forEach((id) => {
        const parent = tree.parentOf.get(id);
        if (!parent || !p.get(parent)) return;
        const pp = p.get(parent);
        const want = parent === "lead" ? 320 : 270;
        const dx = p.get(id).x - pp.x;
        const dy = p.get(id).y - pp.y;
        const d = Math.hypot(dx, dy) || 1;
        const pull = (d - want) * 0.06;
        const fi = f.get(id);
        fi.x -= (dx / d) * pull;
        fi.y -= (dy / d) * pull;
        const ang = dirOf(id);
        fi.x += Math.cos(ang) * 1.6;
        fi.y += Math.sin(ang) * 1.6;
      });
      ids.forEach((id) => {
        if (id === "lead") return;
        const fi = f.get(id);
        const q = p.get(id);
        q.x += Math.max(-24, Math.min(24, fi.x));
        q.y += Math.max(-24, Math.min(24, fi.y));
      });
    }
    p.set("lead", { x: 0, y: 0 });
    return p;
  }

  /** A curve from parent to child: down the tree, or bowed along a ray. */
  function saEdgePath(a, b) {
    if (saView.layout === "tree") {
      const my = (a.y + b.y) / 2;
      return `M${a.x} ${a.y + 30} C${a.x} ${my} ${b.x} ${my} ${b.x} ${b.y - 30}`;
    }
    const mx = (a.x + b.x) / 2;
    const my = (a.y + b.y) / 2;
    const ang = Math.atan2(b.y - a.y, b.x - a.x);
    const bend = 0.12 * Math.hypot(b.x - a.x, b.y - a.y) * (a.x === 0 && a.y === 0 ? 0.3 : 1);
    return `M${a.x} ${a.y} Q${mx + Math.cos(ang + Math.PI / 2) * bend} ${my + Math.sin(ang + Math.PI / 2) * bend} ${b.x} ${b.y}`;
  }

  /** Top-down positions: a leaf per column, a parent over its children. */
  function saLayoutTree(tree) {
    const pos = new Map();
    const seen = new Set();
    let col = 0;
    const place = (key, depth) => {
      seen.add(key);
      const children = (tree.kids.get(key) || []).filter((c) => !seen.has(saKey(c)));
      let x;
      if (!children.length) {
        x = col++ * SA_COL;
      } else {
        const xs = children.map((c) => place(saKey(c), depth + 1));
        x = (xs[0] + xs[xs.length - 1]) / 2;
      }
      pos.set(key, { x, y: depth * SA_ROW });
      return x;
    };
    place("lead", 0);
    const shift = pos.get("lead").x;
    pos.forEach((p) => (p.x -= shift));
    return pos;
  }

  function renderSubagents() {
    if (saBatch) return;
    syncSubagentsTab();
    if (!subagentsTree) return;
    if (subagentsHead) subagentsHead.innerHTML = saHeadHtml();
    if (!subagents.length) {
      if (saView.replay) cancelAnimationFrame(saView.replay.raf);
      saView.replay = null;
      saView.selected = "";
      saView.fitted = "";
      saView.moved = false;
      if (saView.stage) saView.stage.remove();
      saView.stage = null;
      saView.nodes.clear();
      return;
    }
    ensureSaStage();
    drawSubagentTree();
  }

  /** Counts, the phase line once the Lead has named a phase, the layout and the replay. */
  function saHeadHtml() {
    let html = `<span class="sa-counts">${escapeAttr(subagentsSummary(saShown()))}</span>`;
    const at = SA_PHASES.indexOf(orchestraPhase);
    if (at >= 0) {
      html +=
        `<span class="sa-phases" aria-label="${escapeAttr(i18n("subagents.phase"))}">` +
        SA_PHASES.map((p, i) => {
          const cls = i < at ? "done" : i === at ? "now" : "";
          return `<span class="sa-phase ${cls}">${i < at ? orchIconMarkup("check", { size: 12 }) : ""}${p}</span>`;
        }).join('<span class="sa-phase-sep"></span>') +
        `</span>`;
    }
    if (!subagents.length) return html;
    html +=
      `<span class="sa-head-gap"></span>` +
      `<span class="sa-seg" role="group" aria-label="${escapeAttr(i18n("subagents.layout"))}">` +
      SA_LAYOUTS.map(
        (l) => `<button type="button" data-layout="${l}" aria-pressed="${saView.layout === l ? "true" : "false"}">${escapeAttr(i18n("subagents.layout." + l))}</button>`
      ).join("") +
      `</span>`;
    if (subagents.some((sa) => sa.startedAt)) {
      const on = Boolean(saView.replay);
      html += `<button type="button" class="sa-replay${on ? " on" : ""}" data-act="replay">${orchIconMarkup(on ? "stop" : "play", { size: 14 })}${escapeAttr(i18n(on ? "subagents.replay_stop" : "subagents.replay"))}</button>`;
    }
    return html;
  }

  /** The head's buttons, bound once: its markup is drawn again on every change. */
  function bindSaHead() {
    if (saView.headBound || !subagentsHead) return;
    saView.headBound = true;
    subagentsHead.addEventListener("click", (e) => {
      const t = e.target && typeof e.target.closest === "function" ? e.target.closest("[data-layout], [data-act]") : null;
      if (!t) return;
      const layout = t.getAttribute("data-layout");
      if (layout && SA_LAYOUTS.includes(layout) && layout !== saView.layout) {
        saView.layout = layout;
        try {
          localStorage.setItem("orchestra.subagents.layout", layout);
        } catch {
          // Remembered for this page only.
        }
        saView.moved = false;
        saView.fitted = "";
        saView.animateFit = true;
        renderSubagents();
      } else if (t.getAttribute("data-act") === "replay") {
        if (saView.replay) saStopReplay();
        else saStartReplay();
      }
    });
  }

  /** Play the turn back: agents appear when they started and finish when they did, ~40x faster. */
  function saStartReplay() {
    const starts = subagents.map((sa) => sa.startedAt).filter(Boolean);
    if (!starts.length) return;
    const t0 = Math.min(...starts) - 400;
    const t1 = Math.max(...subagents.map((sa) => sa.endedAt || (saBusy(sa) ? Date.now() : sa.startedAt || 0)));
    const span = Math.max(1000, t1 - t0);
    const real = Math.min(16000, Math.max(5000, span / 40));
    const r = { clock: t0, t0, t1, raf: 0 };
    saView.replay = r;
    saView.selected = "";
    saView.moved = false;
    saView.fitted = "";
    let last = performance.now();
    const tick = (now) => {
      if (saView.replay !== r) return;
      r.clock = Math.min(t1, r.clock + ((now - last) * span) / real);
      last = now;
      if (r.clock >= t1) {
        saStopReplay();
        return;
      }
      renderSubagents();
      r.raf = requestAnimationFrame(tick);
    };
    renderSubagents();
    r.raf = requestAnimationFrame(tick);
  }

  function saStopReplay() {
    if (!saView.replay) return;
    cancelAnimationFrame(saView.replay.raf);
    saView.replay = null;
    saView.fitted = "";
    renderSubagents();
  }

  function ensureSaStage() {
    bindSaHead();
    if (saView.stage) return;
    const stage = document.createElement("div");
    stage.className = "sa-stage";
    const world = document.createElement("div");
    world.className = "sa-world";
    const edges = document.createElement("div");
    edges.className = "sa-edges";
    world.appendChild(edges);
    stage.appendChild(world);
    const pop = document.createElement("section");
    pop.className = "sa-pop";
    pop.setAttribute("role", "dialog");
    pop.setAttribute("aria-modal", "false");
    stage.appendChild(pop);
    const zoom = document.createElement("div");
    zoom.className = "sa-zoom";
    const zoomBtn = (icon, label, fn, text) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "sa-zoom-btn" + (text ? " wide" : "");
      b.setAttribute("aria-label", label);
      b.innerHTML = orchIconMarkup(icon, { size: 15 }) + (text ? `<span>${escapeAttr(text)}</span>` : "");
      b.addEventListener("click", (e) => {
        e.stopPropagation();
        fn();
      });
      zoom.appendChild(b);
    };
    zoomBtn("minus", i18n("subagents.zoom_out"), () => saZoom(1 / 1.2));
    zoomBtn("plus", i18n("subagents.zoom_in"), () => saZoom(1.2));
    zoomBtn("expand", i18n("subagents.fit"), () => saFit(true, true), i18n("subagents.fit"));
    stage.appendChild(zoom);
    subagentsTree.appendChild(stage);
    Object.assign(saView, { stage, world, edges, pop });

    let drag = null;
    stage.addEventListener("pointerdown", (e) => {
      if (e.target && typeof e.target.closest === "function" && e.target.closest(".sa-node, .sa-pop, .sa-zoom")) return;
      drag = { x: e.clientX, y: e.clientY, vx: saView.x, vy: saView.y, moved: false };
      stage.classList.add("panning");
      if (typeof stage.setPointerCapture === "function") stage.setPointerCapture(e.pointerId);
    });
    stage.addEventListener("pointermove", (e) => {
      if (!drag) return;
      saView.x = drag.vx + e.clientX - drag.x;
      saView.y = drag.vy + e.clientY - drag.y;
      if (Math.abs(e.clientX - drag.x) + Math.abs(e.clientY - drag.y) > 4) {
        drag.moved = true;
        saView.moved = true;
      }
      saApplyView();
    });
    const endDrag = () => {
      if (drag && !drag.moved && saView.selected) saSelect("");
      drag = null;
      stage.classList.remove("panning");
    };
    stage.addEventListener("pointerup", endDrag);
    stage.addEventListener("pointercancel", endDrag);
    stage.addEventListener(
      "wheel",
      (e) => {
        // The card's text scrolls as text; the wheel moves the tree, and
        // zooms it only with Ctrl (a trackpad pinch arrives as Ctrl+wheel).
        if (e.target && typeof e.target.closest === "function" && e.target.closest(".sa-pop")) return;
        e.preventDefault();
        if (e.ctrlKey || e.metaKey) {
          const r = stage.getBoundingClientRect();
          saZoom(Math.exp(-e.deltaY * 0.0025), e.clientX - r.left, e.clientY - r.top);
          return;
        }
        const line = e.deltaMode === 1 ? 16 : 1;
        saView.x -= e.deltaX * line;
        saView.y -= e.deltaY * line;
        saView.moved = true;
        saApplyView();
      },
      { passive: false }
    );
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && saView.selected) saSelect("");
    });
    // A running agent's clock ticks while the tab is open; nothing runs
    // while it is not, or once every agent has finished.
    if (!saView.ticker) {
      saView.ticker = setInterval(() => {
        const app = document.getElementById("app");
        if (app && app.dataset && app.dataset.view === "subagents" && subagents.some(saBusy)) renderSubagents();
      }, 1000);
      if (saView.ticker && typeof saView.ticker.unref === "function") saView.ticker.unref();
    }
  }

  function drawSubagentTree() {
    const tree = saTree();
    const pos = saLayout(tree);
    const rel = saView.selected ? saLineage(tree, saView.selected) : null;

    let paths = "";
    tree.parentOf.forEach((parent, key) => {
      const a = pos.get(parent);
      const b = pos.get(key);
      const sa = tree.byKey.get(key);
      if (!a || !b || !sa) return;
      const cls = ["sa-edge", "status-" + sa.status];
      if (rel) cls.push(rel.has(key) && rel.has(parent) ? "hot" : "dim");
      paths += `<path class="${cls.join(" ")}" d="${saEdgePath(a, b)}"/>`;
    });
    saView.edges.innerHTML = `<svg class="sa-edges-svg" aria-hidden="true">${paths}</svg>`;

    const live = new Set(["lead"]);
    saPaintNode("lead", pos.get("lead"), saLeadNode(tree), rel);
    tree.all.forEach((sa) => {
      const key = saKey(sa);
      live.add(key);
      saPaintNode(key, pos.get(key), saAgentNode(sa, tree), rel, pos.get(tree.parentOf.get(key)));
    });
    saView.nodes.forEach((el, key) => {
      if (!live.has(key)) {
        el.remove();
        saView.nodes.delete(key);
      }
    });
    if (saView.selected && !live.has(saView.selected)) saView.selected = "";

    // Fit when the tree changed shape and the user has not moved it, and
    // only once the pane is on screen (a hidden pane measures zero).
    const shape = saView.layout + ":" + tree.all.length;
    if (!saView.moved && saView.fitted !== shape && saFit(false, Boolean(saView.fitted) || saView.animateFit)) {
      saView.fitted = shape;
      saView.animateFit = false;
    }
    saApplyView();
    if (saView.selected) saPaintPop(tree);
    else saView.pop.classList.remove("open");
  }

  function saLeadNode(tree) {
    const busy = tree.all.some(saBusy) || Boolean(saLead.startedAt && !saLead.endedAt);
    return {
      role: "lead",
      status: busy ? "running" : "done",
      name: i18n("subagents.lead"),
      time: saLeadClock() || i18nCount("subagents.count", tree.all.length),
      caption: i18n("subagents.lead_desc"),
      label: i18n("subagents.lead"),
      kids: (tree.kids.get("lead") || []).length,
    };
  }

  function saAgentNode(sa, tree) {
    const usage = sa.taskId ? saUsage.get(sa.taskId) : null;
    const goal = String(sa.goal || sa.label || saKey(sa));
    const status = sa.status || "done";
    const clock = saClock(saElapsed(sa));
    return {
      role: subagentRole(sa.type),
      status,
      name: subagentRoleName(sa.type),
      time: status === "error" || status === "waiting" || !clock ? i18n("subagents.status." + status) : clock,
      caption: graphCaption(goal),
      label: `${subagentRoleName(sa.type)}: ${goal}. ${i18n("subagents.status." + status)}`,
      kids: sa.taskId ? (tree.kids.get(sa.taskId) || []).length : 0,
      dept: saAgentName(sa),
      tier: subagentTierLabel(sa.tier),
      model: saModelShort(sa.model),
      tokens: usage ? formatTok(usage.in + usage.out) : "",
    };
  }

  function saPaintNode(key, p, o, rel, from) {
    let el = saView.nodes.get(key);
    if (!el) {
      el = document.createElement("button");
      el.type = "button";
      el.className = "sa-node spawning";
      el.setAttribute("data-key", key);
      el.addEventListener("click", (e) => {
        e.stopPropagation();
        saSelect(saView.selected === key ? "" : key);
      });
      saView.world.appendChild(el);
      saView.nodes.set(key, el);
      // A new agent grows out of the one that started it.
      if (from && p) {
        el.style.left = from.x + "px";
        el.style.top = from.y + "px";
      }
      requestAnimationFrame(() =>
        requestAnimationFrame(() => {
          el.classList.remove("spawning");
          const at = saView.pos.get(key);
          if (at) {
            el.style.left = at.x + "px";
            el.style.top = at.y + "px";
          }
        })
      );
      if (from && p) return saPaintNodeBody(el, key, o, rel);
    }
    saPaintNodeBody(el, key, o, rel);
    if (p) {
      el.style.left = p.x + "px";
      el.style.top = p.y + "px";
    }
  }

  function saPaintNodeBody(el, key, o, rel) {
    el.className =
      `sa-node role-${o.role} status-${o.status}` +
      (key === "lead" ? " lead" : "") +
      (saView.selected === key ? " selected" : "") +
      (rel && !rel.has(key) ? " dim" : "") +
      (el.classList.contains("spawning") ? " spawning" : "");
    el.setAttribute("aria-label", o.label);
    el.title = o.caption;
    el.innerHTML =
      `<span class="sa-glyph">${orchIconMarkup(SA_ROLE_ICON[o.role] || "tool", { size: 17 })}<span class="sa-st"></span></span>` +
      `<span class="sa-name">${escapeAttr(o.name)}${o.dept ? `<span class="sa-dept"> · ${escapeAttr(o.dept)}</span>` : ""}</span>` +
      `<span class="sa-time">${escapeAttr(o.time)}</span>` +
      `<span class="sa-task">${escapeAttr(o.caption)}</span>` +
      (o.tier || o.model || o.tokens
        ? `<span class="sa-meta">` +
          (o.tier ? `<span class="sa-tier">${escapeAttr(o.tier)}</span>` : "") +
          (o.model ? `<span class="sa-model" title="${escapeAttr(o.model)}">${escapeAttr(o.model)}</span>` : "") +
          (o.tokens ? `<span class="sa-tok">${escapeAttr(i18n("subagents.tok.short", { n: o.tokens }))}</span>` : "") +
          `</span>`
        : "") +
      (o.kids ? `<span class="sa-kids">${o.kids}</span>` : "");
  }

  function saLineage(tree, key) {
    const set = new Set([key]);
    let k = key;
    while (tree.parentOf.has(k)) {
      k = tree.parentOf.get(k);
      set.add(k);
    }
    const down = (x) =>
      (tree.kids.get(x) || []).forEach((c) => {
        set.add(saKey(c));
        if (c.taskId) down(c.taskId);
      });
    down(key === "lead" ? "lead" : (tree.byKey.get(key) || {}).taskId || "");
    return set;
  }

  function saSelect(key) {
    if (key !== saView.selected) {
      saView.stepsOpen = false;
      saView.taskOpen = false;
    }
    saView.selected = key;
    drawSubagentTree();
    // Blocks and the view may still be easing into place; the card follows
    // where its block ends up.
    clearTimeout(saView.placeTimer);
    if (key) saView.placeTimer = setTimeout(() => saView.selected === key && saPlacePop(), 560);
  }

  function saApplyView() {
    if (!saView.world) return;
    saView.world.style.transform = `translate(${saView.x}px, ${saView.y}px) scale(${saView.k})`;
    if (saView.selected) saPlacePop();
  }

  /** Fit the whole tree in the pane; false while the pane is not on screen. */
  function saFit(byUser, animate) {
    const stage = saView.stage;
    if (!stage || typeof stage.getBoundingClientRect !== "function") return false;
    const r = stage.getBoundingClientRect();
    if (!r || !r.width || !r.height) return false;
    const pos = saLayout(saTree());
    const ps = [...pos.values()];
    if (!ps.length) return false;
    const xs = ps.map((p) => p.x);
    const ys = ps.map((p) => p.y);
    const x0 = Math.min(...xs) - 140;
    const x1 = Math.max(...xs) + 140;
    const y0 = Math.min(...ys) - 60;
    const y1 = Math.max(...ys) + 70;
    saView.k = Math.max(0.35, Math.min(1, Math.min(r.width / (x1 - x0), r.height / (y1 - y0))));
    saView.x = r.width / 2 - ((x0 + x1) / 2) * saView.k;
    saView.y = Math.max(24, r.height / 2 - ((y0 + y1) / 2) * saView.k);
    if (byUser) saView.moved = false;
    if (byUser || animate) saGlide();
    saApplyView();
    return true;
  }

  /** The next move of the view eases there instead of jumping. */
  function saGlide() {
    const w = saView.world;
    if (!w) return;
    w.classList.add("gliding");
    clearTimeout(saView.glideTimer);
    saView.glideTimer = setTimeout(() => {
      w.classList.remove("gliding");
      if (saView.selected) saPlacePop();
    }, 520);
  }

  /** Bring a node to the middle, leaving room for its card on the right. @param {string} key */
  function saCenterOn(key) {
    const p = saView.pos.get(key);
    const stage = saView.stage;
    if (!p || !stage) return;
    const r = stage.getBoundingClientRect();
    saView.x = r.width / 2 - Math.min(200, r.width / 4) - p.x * saView.k;
    saView.y = r.height / 2 - p.y * saView.k;
    saView.moved = true;
    saGlide();
    saApplyView();
  }

  function saZoom(f, cx, cy) {
    const stage = saView.stage;
    if (!stage) return;
    const r = stage.getBoundingClientRect();
    const px = cx == null ? r.width / 2 : cx;
    const py = cy == null ? r.height / 2 : cy;
    const k = Math.max(0.3, Math.min(2, saView.k * f));
    saView.x = px - ((px - saView.x) * k) / saView.k;
    saView.y = py - ((py - saView.y) * k) / saView.k;
    saView.k = k;
    saView.moved = true;
    saApplyView();
  }

  /** The card beside the selected node. */
  function saPaintPop(tree) {
    const key = saView.selected;
    const pop = saView.pop;
    const sec = (title, body) => `<div class="sa-sec"><h3>${escapeAttr(title)}</h3>${body}</div>`;
    let html = "";
    if (key === "lead") {
      const busy = tree.all.some(saBusy) || Boolean(saLead.startedAt && !saLead.endedAt);
      const lm = saLeadModel();
      const leadClock = saLeadClock();
      const goalText = saGoalText();
      const leadFacts = [
        lm.model ? [i18n("subagents.fact.model"), `<span class="sa-mono" title="${escapeAttr(lm.model)}">${escapeAttr(lm.model)}</span>`] : null,
        lm.tier ? [i18n("subagents.fact.tier"), escapeAttr(lm.tier)] : null,
        leadClock ? [i18n("subagents.fact.time"), escapeAttr(leadClock)] : null,
        saLead.calls ? [i18n("subagents.fact.calls"), String(saLead.calls)] : null,
        [i18n("subagents.fact.agents"), escapeAttr(subagentsSummary(tree.all) || "0")],
      ].filter(Boolean);
      html +=
        saPopHead("lead", i18n("subagents.lead"), i18n("subagents.lead_desc")) +
        `<div class="sa-pop-status"><span class="sa-pill ${busy ? "running" : "done"}"><i></i>${escapeAttr(i18n("subagents.status." + (busy ? "running" : "done")))}${busy && leadClock ? " · " + escapeAttr(leadClock) : ""}</span></div>` +
        (goalText ? sec(i18n("subagents.sec.goal"), `<p>${escapeAttr(clipText(goalText, 600))}</p>`) : "") +
        (busy && saLead.now ? sec(i18n("subagents.sec.now"), `<div class="sa-now"><span></span><p>${escapeAttr(saLead.now)}</p></div>`) : "") +
        sec(i18n("subagents.sec.facts"), `<dl class="sa-facts">${leadFacts.map(([k, v]) => `<dt>${escapeAttr(k)}</dt><dd>${v}</dd>`).join("")}</dl>`) +
        (orchestraPhase ? sec(i18n("subagents.phase"), `<p>${escapeAttr(orchestraPhase)}</p>`) : "") +
        saUsageSec(saUsageTotal(), i18n("subagents.sec.spend_all")) +
        saChildrenSec(tree.kids.get("lead") || []) +
        saMessagesSec(saMessages) +
        `<div class="sa-pop-actions"><button type="button" class="sa-btn" data-act="chat">${orchIconMarkup("chat", { size: 15 })}${escapeAttr(i18n("subagents.open_chat"))}</button></div>`;
    } else {
      const sa = tree.byKey.get(key);
      if (!sa) return;
      const live = saLive(sa);
      const role = subagentRole(sa.type);
      const status = sa.status || "done";
      const clock = saClock(saElapsed(sa));
      const goal = String(sa.goal || sa.label || key);
      const parentKey = tree.parentOf.get(key);
      const parent = parentKey && parentKey !== "lead" ? tree.byKey.get(parentKey) : null;
      const tierText = saTierText(sa.tier);
      const modelRows = [
        [i18n("subagents.fact.model"), sa.model ? `<span class="sa-mono" title="${escapeAttr(sa.model)}">${escapeAttr(sa.model)}</span>` : escapeAttr(i18n("subagents.model_unknown"))],
        [i18n("subagents.fact.tier"), escapeAttr(tierText)],
      ];
      const facts = [
        ...modelRows,
        saAgentName(sa) ? [i18n("subagents.fact.dept"), `<span class="sa-mono">${escapeAttr(saAgentName(sa))}</span>`] : null,
        clock ? [i18n("subagents.fact.time"), escapeAttr(clock)] : null,
        live.toolCount ? [i18n("subagents.fact.calls"), String(live.toolCount)] : null,
        [i18n("subagents.fact.parent"), escapeAttr(parent ? subagentRoleName(parent.type) : i18n("subagents.lead"))],
      ].filter(Boolean);
      const dl = (rows) => `<dl class="sa-facts">${rows.map(([k, v]) => `<dt>${escapeAttr(k)}</dt><dd>${v}</dd>`).join("")}</dl>`;
      const waitFor = Array.isArray(sa.waitingFor) ? sa.waitingFor : [];
      const waitSec =
        status === "waiting" && (waitFor.length || sa.waitReason)
          ? sec(
              i18n("subagents.sec.waiting"),
              (waitFor.length
                ? `<div class="sa-chips">${waitFor
                    .map((id) => {
                      const w = tree.byKey.get(id);
                      const r = w ? subagentRole(w.type) : "agent";
                      return `<button type="button" class="sa-chip role-${r} status-${escapeAttr((w && w.status) || "done")}" data-go="${escapeAttr(id)}">${orchIconMarkup(SA_ROLE_ICON[r] || "tool", { size: 13 })}${escapeAttr(w ? subagentRoleName(w.type) : id)}</button>`;
                    })
                    .join("")}</div>`
                : "") + (sa.waitReason ? `<p class="sa-note">${escapeAttr(sa.waitReason)}</p>` : "")
            )
          : "";
      const files = Array.isArray(live.files) ? live.files : [];
      html +=
        saPopHead(role, subagentRoleName(sa.type), i18n("role." + role + ".desc")) +
        `<div class="sa-pop-status"><span class="sa-pill ${escapeAttr(status)}"><i></i>${escapeAttr(i18n("subagents.status." + status))}${saBusy(sa) && clock ? " · " + escapeAttr(clock) : ""}</span></div>` +
        sec(
          i18n("subagents.sec.task"),
          goal.length > 420
            ? `<p class="sa-long${saView.taskOpen ? "" : " clamped"}">${escapeAttr(goal)}</p><button type="button" class="sa-more" data-act="more">${escapeAttr(i18n(saView.taskOpen ? "subagents.less" : "subagents.more"))}</button>`
            : `<p>${escapeAttr(goal)}</p>`
        ) +
        waitSec +
        (saBusy(sa) && live.now
          ? sec(i18n(status === "waiting" ? "subagents.sec.waits_on" : "subagents.sec.now"), `<div class="sa-now"><span></span><p>${escapeAttr(live.now)}</p></div>`)
          : "") +
        (status === "error" && sa.error ? sec(i18n("subagents.sec.error"), `<div class="sa-err">${escapeAttr(sa.error)}</div>`) : "") +
        (status === "done" && sa.result ? sec(i18n("subagents.sec.result"), `<p>${escapeAttr(sa.result)}</p>`) : "") +
        (files.length
          ? sec(
              i18n("subagents.sec.files"),
              `<ul class="sa-files">${files
                .slice(0, 12)
                .map(
                  ([op, f]) =>
                    `<li><span class="sa-op${op === "w" ? " w" : ""}">${escapeAttr(i18n(op === "w" ? "subagents.file.write" : "subagents.file.read"))}</span><span class="sa-mono" title="${escapeAttr(f)}">${escapeAttr(f)}</span></li>`
                )
                .join("")}</ul>`
            )
          : "") +
        saUsageSec(sa.taskId ? saUsage.get(sa.taskId) : null, i18n("subagents.sec.spend")) +
        saMessagesSec(saMessagesOf(sa)) +
        sec(i18n("subagents.sec.facts"), dl(facts)) +
        saChildrenSec(sa.taskId ? tree.kids.get(sa.taskId) || [] : []) +
        (saView.stepsOpen ? saStepsSec(live) : "") +
        (saChatTarget(sa) || saSteps(live).length
          ? `<div class="sa-pop-actions">` +
            (saChatTarget(sa) ? `<button type="button" class="sa-btn" data-act="chat">${orchIconMarkup("chat", { size: 15 })}${escapeAttr(i18n("subagents.open_chat"))}</button>` : "") +
            (saSteps(live).length
              ? `<button type="button" class="sa-btn${saView.stepsOpen ? " on" : ""}" data-act="steps" aria-pressed="${saView.stepsOpen ? "true" : "false"}">${orchIconMarkup("list", { size: 15 })}${escapeAttr(i18n("subagents.steps", { n: saSteps(live).length }))}</button>`
              : "") +
            `</div>`
          : "");
    }
    pop.innerHTML = html;
    pop.classList.add("open");
    const close = pop.querySelector && pop.querySelector(".sa-close");
    if (close) close.addEventListener("click", () => saSelect(""));
    if (pop.querySelectorAll) {
      pop.querySelectorAll("[data-go]").forEach((b) =>
        b.addEventListener("click", () => {
          const go = b.getAttribute("data-go");
          saSelect(go);
          saCenterOn(go);
        })
      );
      const more = pop.querySelector('[data-act="more"]');
      if (more) {
        more.addEventListener("click", () => {
          saView.taskOpen = !saView.taskOpen;
          drawSubagentTree();
        });
      }
      const steps = pop.querySelector('[data-act="steps"]');
      if (steps) {
        steps.addEventListener("click", () => {
          saView.stepsOpen = !saView.stepsOpen;
          drawSubagentTree();
        });
      }
      const chat = pop.querySelector('[data-act="chat"]');
      if (chat) {
        chat.addEventListener("click", () => {
          const sa = tree.byKey.get(key);
          const users = key === "lead" && messagesEl && messagesEl.querySelectorAll ? messagesEl.querySelectorAll(".msg.user") : null;
          const target = sa ? saChatTarget(sa) : users && users.length ? users[users.length - 1] : null;
          setView("chat");
          if (target && typeof target.scrollIntoView === "function") target.scrollIntoView({ block: "center", behavior: "smooth" });
        });
      }
    }
    saPlacePop();
  }

  /** The agent's tool calls in the order it made them. */
  function saSteps(live) {
    return live && Array.isArray(live.steps) ? live.steps : [];
  }

  function saStepsSec(live) {
    const steps = saSteps(live);
    if (!steps.length) return "";
    const shown = steps.slice(-40);
    return (
      `<div class="sa-sec"><h3>${escapeAttr(i18n("subagents.sec.steps", { n: steps.length }))}</h3><ol class="sa-steps" start="${steps.length - shown.length + 1}">` +
      shown
        .map((st) => `<li><span class="sa-step-name">${escapeAttr(st.name)}</span>${st.detail ? `<span class="sa-mono" title="${escapeAttr(st.detail)}">${escapeAttr(st.detail)}</span>` : ""}</li>`)
        .join("") +
      `</ol></div>`
    );
  }

  function saPopHead(role, name, desc) {
    return (
      `<div class="sa-pop-head role-${role}">` +
      `<span class="sa-glyph">${orchIconMarkup(SA_ROLE_ICON[role] || "tool", { size: 19 })}</span>` +
      `<div><div class="sa-pop-title">${escapeAttr(name)}</div><div class="sa-pop-desc">${escapeAttr(desc)}</div></div>` +
      `<button type="button" class="sa-close" aria-label="${escapeAttr(i18n("subagents.close"))}">${orchIconMarkup("close", { size: 15 })}</button>` +
      `</div>`
    );
  }

  function saChildrenSec(children) {
    if (!children.length) return "";
    return (
      `<div class="sa-sec"><h3>${escapeAttr(i18n("subagents.sec.started", { n: children.length }))}</h3><div class="sa-chips">` +
      children
        .map((c) => {
          const role = subagentRole(c.type);
          return `<button type="button" class="sa-chip role-${role} status-${escapeAttr(c.status || "done")}" data-go="${escapeAttr(saKey(c))}">${orchIconMarkup(SA_ROLE_ICON[role] || "tool", { size: 13 })}${escapeAttr(subagentRoleName(c.type))}</button>`;
        })
        .join("") +
      `</div></div>`
    );
  }

  /** The block in the chat a subagent's calls are under. */
  function saChatTarget(sa) {
    const live = saLive(sa);
    return (sa.parentToolCallId && toolBlocks.get(sa.parentToolCallId)) || (live && live.toolsEl) || null;
  }

  function saPlacePop() {
    const el = saView.nodes.get(saView.selected);
    const pop = saView.pop;
    const stage = saView.stage;
    if (!el || !pop || !stage || typeof el.getBoundingClientRect !== "function") return;
    const r = el.getBoundingClientRect();
    const s = stage.getBoundingClientRect();
    const w = pop.offsetWidth || 360;
    const h = pop.offsetHeight || 0;
    let left = r.right - s.left + 14;
    if (left + w > s.width - 12) left = r.left - s.left - w - 14;
    if (left < 12) left = Math.max(12, Math.min(s.width - w - 12, r.left - s.left));
    const top = Math.max(12, Math.min(s.height - h - 60, r.top - s.top - 8));
    pop.style.left = left + "px";
    pop.style.top = top + "px";
  }

  /**
   * What a subagent is doing, from its own tool calls: the last call is
   * "now", the files it read and wrote are its files. The editor host sends
   * the calls as child tool blocks, the web host as childActivity.
   * @param {string} taskId @param {string} toolName @param {string} argsRaw
   * @param {"start"|"complete"} phase @param {boolean} count
   */
  function noteChildActivity(taskId, toolName, argsRaw, phase, count) {
    const node = taskId ? subagentByTask.get(taskId) : null;
    if (!node) return;
    if (phase === "start" && count) node.toolCount = (node.toolCount || 0) + 1;
    const parts = toolRowParts(toolName, argsRaw || "", "");
    node.now = parts.detail ? `${parts.name} · ${parts.detail}` : parts.name;
    // One step per call: its start names the tool, its end the arguments
    // that streamed in between.
    const steps = Array.isArray(node.steps) ? node.steps : (node.steps = []);
    if (phase === "start" || !steps.length || steps[steps.length - 1].done) {
      steps.push({ name: parts.name, detail: parts.detail || "", done: phase === "complete" });
      if (steps.length > 300) steps.splice(0, steps.length - 300);
    } else {
      Object.assign(steps[steps.length - 1], { name: parts.name, detail: parts.detail || "", done: true });
    }
    const path = toolPathFromArgs(toolName, argsRaw || "", "");
    if (phase === "complete" && path) {
      const files = Array.isArray(node.files) ? node.files : (node.files = []);
      const op = toolKind(toolName) === "write" ? "w" : "r";
      const at = files.findIndex(([, f]) => f === path);
      if (at < 0) files.push([op, path]);
      else if (op === "w") files[at][0] = "w";
    }
    renderSubagents();
  }

  /** A subagent's finished model call. @param {string} taskId @param {any} u */
  function noteChildUsage(taskId, u) {
    const acc = saUsage.get(taskId) || { in: 0, out: 0, cost: 0, calls: 0, ctx: 0, window: 0 };
    const num = (v) => (typeof v === "number" && v > 0 ? v : 0);
    acc.in += num(u.prompt_tokens);
    acc.out += num(u.completion_tokens);
    acc.cost += num(u.cost_usd);
    acc.calls++;
    if (num(u.prompt_tokens)) acc.ctx = u.prompt_tokens;
    if (num(u.context_tokens)) acc.window = u.context_tokens;
    saUsage.set(taskId, acc);
    renderSubagents();
  }

  /** A message between agents of the agency. */
  function noteAgentMessage(msg) {
    saMessages.push({
      channel: String(msg.channel || ""),
      from: String(msg.from || ""),
      to: String(msg.to || ""),
      kind: String(msg.kind || ""),
      content: String(msg.content || ""),
      taskId: String(msg.taskId || ""),
    });
    if (saMessages.length > 200) saMessages.splice(0, saMessages.length - 200);
    renderSubagents();
  }

  /** "claude-sonnet-5-5" out of "anthropic/claude-sonnet-5-5". @param {string} model */
  function saModelShort(model) {
    const m = String(model || "").trim();
    return m.slice(m.lastIndexOf("/") + 1);
  }

  /** "L3 · Worker · focused"; "" for no tier. @param {string} tier */
  function saTierText(tier) {
    const label = subagentTierLabel(tier);
    if (!label) return i18n("subagents.tier.parent");
    const key = "orch.role." + String(tier || "").toLowerCase();
    const name = i18n(key);
    return name && name !== key ? `${label} · ${name}` : label;
  }

  /** What the Orchestrator runs on: the planner (L5) in orchestra mode, the chat's model otherwise. */
  function saLeadModel() {
    const roles = (orchestraRolesInfo && orchestraRolesInfo.roles) || [];
    const planner = roles.find((r) => r.key === "planner");
    const models = planner ? orchRoleModels(planner) : [];
    if (modeId === "orchestra" && models.length) return { model: models[0], tier: "L5" };
    return { model: currentModel || "", tier: modeId === "orchestra" ? "L5" : "" };
  }

  /** The Orchestrator's time on the turn: running, or what the last turn took. */
  function saLeadClock() {
    if (!saLead.startedAt) return "";
    return saClock((saLead.endedAt || (saView.replay ? saView.replay.clock : Date.now())) - saLead.startedAt);
  }

  /** The user's message the turn answers: the one heard last, or the last in the chat. */
  function saGoalText() {
    if (saTurnGoal) return saTurnGoal;
    if (!messagesEl || typeof messagesEl.querySelectorAll !== "function") return "";
    const users = messagesEl.querySelectorAll(".msg.user");
    const last = users && users.length ? users[users.length - 1] : null;
    return last ? String(last.textContent || "").trim() : "";
  }

  /** A call of the Orchestrator's own. */
  function noteLeadActivity(toolName, argsRaw, phase) {
    if (phase === "start") {
      saLead.calls++;
      return;
    }
    const parts = toolRowParts(toolName, argsRaw || "", "");
    saLead.now = parts.detail ? `${parts.name} · ${parts.detail}` : parts.name;
    renderSubagents();
  }

  function noteLeadTurn(phase) {
    if (phase === "start") Object.assign(saLead, { startedAt: Date.now(), endedAt: 0, calls: 0, now: "" });
    else if (saLead.startedAt) Object.assign(saLead, { endedAt: Date.now(), now: "" });
    renderSubagents();
  }

  /**
   * A reopened chat's tab, rebuilt from the session's log: each subagent's
   * model, tier and times, its calls and files, its spend and its mail — what
   * the live events carried and the chat itself does not keep.
   */
  function saRebuildFromLog(events) {
    if (!Array.isArray(events)) return;
    const agentEvents = events.filter((e) => e && e.type === "agent/event" && e.data && typeof e.data.type === "string");
    if (!agentEvents.some((e) => e.data.type === "child_started")) return;
    saUsage.clear();
    saMessages.length = 0;
    subagentByTask.forEach((n) => Object.assign(n, { steps: [], files: [], toolCount: 0, now: "" }));
    const args = new Map();
    const names = new Map();
    // Claude's deltas named no call before 2026-10: a log of then ties a
    // delta to its call by the task and the call's index in the step.
    const callAt = new Map();
    let lastTurn = "";
    agentEvents.forEach((e) => {
      if (e.data.turn_id && !e.data.task_id) lastTurn = e.data.turn_id;
    });
    const lead = { startedAt: 0, endedAt: 0, calls: 0, now: "" };
    saBatch = true;
    try {
      agentEvents.forEach((e) => {
        const d = e.data;
        const at = Number(e.time_ms) || 0;
        const child = d.scope === "child" && d.task_id;
        if (d.turn_id === lastTurn && !child) {
          if (!lead.startedAt) lead.startedAt = at;
          if (d.type === "done") lead.endedAt = at;
          if (d.type === "tool_call_start") lead.calls++;
        }
        switch (d.type) {
          case "child_started":
            handleChildLifecycle({
              phase: "started",
              quiet: true,
              at,
              taskId: d.task_id,
              parentToolCallId: d.parent_tool_call_id,
              parentTaskId: d.parent_task_id,
              depth: d.depth,
              subagentType: d.subagent_type,
              tier: d.tier,
              model: d.model,
              agent: d.agent,
              content: d.content,
            });
            break;
          case "child_queued":
            handleChildLifecycle({ phase: "queued", quiet: true, at, taskId: d.task_id, waitingFor: d.waiting_for, reason: d.reason });
            break;
          case "child_done":
            handleChildLifecycle({ phase: "done", quiet: true, at, taskId: d.task_id, status: d.status, error: d.error, result: d.content });
            break;
          case "step_usage":
            if (child && d.data && typeof d.data === "object") noteChildUsage(d.task_id, d.data);
            break;
          case "agent_message":
            noteAgentMessage({ channel: d.channel, from: d.from, to: d.to, kind: d.kind, content: d.content, taskId: d.task_id });
            break;
          case "tool_call_start":
            if (child && d.tool_call_id) {
              names.set(d.tool_call_id, d.tool_call_name || "");
              args.set(d.tool_call_id, "");
              callAt.set(d.task_id + ":" + (d.tool_call_index || 0), d.tool_call_id);
              noteChildActivity(d.task_id, d.tool_call_name || "", "", "start", true);
            }
            break;
          case "tool_call_delta": {
            const id = d.tool_call_id || (child ? callAt.get(d.task_id + ":" + (d.tool_call_index || 0)) : "");
            if (id && args.has(id)) args.set(id, args.get(id) + (d.args_delta || ""));
            break;
          }
          case "tool_call_completed":
            if (child && d.tool_call_id) {
              noteChildActivity(d.task_id, d.tool_call_name || names.get(d.tool_call_id) || "", args.get(d.tool_call_id) || "", "complete", false);
              args.delete(d.tool_call_id);
            }
            break;
          default:
            break;
        }
      });
    } finally {
      saBatch = false;
    }
    // A turn still running keeps the clock the live events started.
    if (!saLead.startedAt || saLead.endedAt) Object.assign(saLead, lead);
    // An agent the log saw start and never saw end belongs to a turn that is
    // over: it did not finish, it was cut off.
    if (lead.endedAt) {
      subagentByTask.forEach((n, id) => {
        if (n.status === "running" || n.status === "waiting") upsertSubagentTask(id, { status: "error", endedAt: lead.endedAt, error: n.error || i18n("subagents.cut_off") });
      });
    }
    renderSubagents();
  }

  /** The agency address a subagent runs under, when it says more than its role. */
  function saAgentName(sa) {
    const a = String(sa.agent || "").trim();
    return a && a.toLowerCase() !== String(sa.type || "").toLowerCase() ? a : "";
  }

  /** Who an address is, for a message line: "Architecture · backend", "Orchestrator". @param {string} addr */
  function saAddressName(addr) {
    const a = String(addr || "");
    if (!a || a === "lead" || a === "root" || a === "orchestrator") return i18n("subagents.lead");
    const sa = saAddressOwner(a);
    return sa ? `${subagentRoleName(sa.type)} · ${a}` : a;
  }

  /**
   * Who answers to an address: the shallowest agent under it. A department's
   * workers carry its address too, and its mail is the Lead's, not theirs.
   */
  function saAddressOwner(addr) {
    let best = null;
    subagents.forEach((s) => {
      if (s.agent === addr && (!best || (s.depth || 0) < (best.depth || 0))) best = s;
    });
    return best;
  }

  /** The messages an agent sent or got, oldest first. */
  function saMessagesOf(sa) {
    const a = sa.agent || "";
    const owns = a && saAddressOwner(a) && saKey(saAddressOwner(a)) === saKey(sa);
    return saMessages.filter((m) => (sa.taskId && m.taskId === sa.taskId) || (owns && (m.from === a || m.to === a)));
  }

  /** Every subagent's spend added up, for the Lead's card. */
  function saUsageTotal() {
    const t = { in: 0, out: 0, cost: 0, calls: 0, ctx: 0, window: 0 };
    saUsage.forEach((u) => {
      t.in += u.in;
      t.out += u.out;
      t.cost += u.cost;
      t.calls += u.calls;
    });
    return t;
  }

  function saUsageSec(u, title) {
    if (!u || !u.calls) return "";
    const rows = [
      [i18n("subagents.fact.tokens"), `${formatTok(u.in)} ${i18n("subagents.tok.in")} · ${formatTok(u.out)} ${i18n("subagents.tok.out")}`],
      u.cost > 0 ? [i18n("subagents.fact.cost"), formatUsd(u.cost)] : null,
      [i18n("subagents.fact.model_calls"), String(u.calls)],
      u.ctx ? [i18n("subagents.fact.context"), u.window ? `${formatTok(u.ctx)} / ${formatTok(u.window)}` : formatTok(u.ctx)] : null,
    ].filter(Boolean);
    return (
      `<div class="sa-sec"><h3>${escapeAttr(title)}</h3><dl class="sa-facts">` +
      rows.map(([k, v]) => `<dt>${escapeAttr(k)}</dt><dd>${escapeAttr(v)}</dd>`).join("") +
      `</dl></div>`
    );
  }

  function saMessagesSec(list) {
    if (!list.length) return "";
    const shown = list.slice(-8);
    return (
      `<div class="sa-sec"><h3>${escapeAttr(i18n("subagents.sec.messages", { n: list.length }))}</h3><ul class="sa-msgs">` +
      shown
        .map(
          (m) =>
            `<li><div class="sa-msg-head"><span>${escapeAttr(saAddressName(m.from))}</span><span class="sa-msg-arrow">→</span><span>${escapeAttr(saAddressName(m.to))}</span>` +
            (m.kind ? `<span class="sa-msg-kind">${escapeAttr(m.kind)}</span>` : "") +
            `</div><p>${escapeAttr(clipText(m.content, 280))}</p></li>`
        )
        .join("") +
      `</ul></div>`
    );
  }

  /** The phase the Lead set, from the state.md it wrote. @param {string} argsRaw */
  function noteOrchestraPhase(argsRaw) {
    const args = parseToolArgs(argsRaw || "");
    const m = /phase:\s*([a-z]+)/.exec(String(args.content || ""));
    if (m && SA_PHASES.includes(m[1]) && m[1] !== orchestraPhase) {
      orchestraPhase = m[1];
      renderSubagents();
    }
  }

  function syncSubagentsTab() {
    if (!viewSubagentsBtn) return;
    const n = subagents.length;
    const running = subagents.some(saBusy);
    viewSubagentsBtn.classList.toggle("hidden", n === 0);
    viewSubagentsBtn.classList.toggle("has-running", running);
    if (subagentsCountEl) subagentsCountEl.textContent = n ? String(n) : "";
    const app = document.getElementById("app");
    if (!n && app && app.dataset && app.dataset.view === "subagents") setView("chat");
  }

  /** "2 running · 5 done · 1 failed", the parts there are. */
  function subagentsSummary(list) {
    const count = { running: 0, waiting: 0, done: 0, error: 0 };
    (list || subagents).forEach((s) => {
      if (s.status === "error") count.error++;
      else if (s.status === "done") count.done++;
      else if (s.status === "waiting") count.waiting++;
      else count.running++;
    });
    const parts = [];
    if (count.running) parts.push(i18n("subagents.running", { n: count.running }));
    if (count.waiting) parts.push(i18n("subagents.waiting", { n: count.waiting }));
    if (count.done) parts.push(i18n("subagents.done", { n: count.done }));
    if (count.error) parts.push(i18n("subagents.failed", { n: count.error }));
    return parts.join(" · ");
  }

  function trackSubagent(msg, phase, argsRaw) {
    const name = msg.toolName || "";
    if (!["task", "task_spawn", "task_wait", "task_cancel"].includes(name)) {
      return;
    }
    const id = msg.toolCallId || `${name}-${msg.step ?? subagents.length}`;
    if (phase === "start") {
      subagents.push({
        id,
        type: "agent",
        label: toolDisplayName(name),
        status: name === "task_wait" ? "waiting" : "running",
      });
    } else if (phase === "update" || phase === "complete") {
      const sa = subagents.find((s) => s.id === id);
      if (!sa) return;
      const args = parseToolArgs(argsRaw || "");
      const st = args.subagent_type || args.subagentType || sa.type;
      if (st) sa.type = String(st);
      if (args.tier) sa.tier = String(args.tier);
      if (args.model) sa.model = String(args.model);
      let desc = humanizeTaskLabel(args.description || args.prompt || args.goal || "");
      if (!desc && Array.isArray(args.workorders) && args.workorders.length) {
        const first = args.workorders[0] || {};
        const wo = first.intent || first.title || first.goal || first.description || "";
        desc = wo
          ? args.workorders.length > 1
            ? `${wo} (+${args.workorders.length - 1})`
            : String(wo)
          : `${args.workorders.length} workorders`;
      }
      if (desc) {
        const d = String(desc).trim();
        sa.goal = d;
        sa.label = d.length > 48 ? d.slice(0, 45) + "…" : d;
      }
      if (phase === "complete" && !sa.taskId) {
        // Rows adopted by a child (taskId set) get their status from
        // childLifecycle events; a completed spawn call must not override it.
        const res = String(msg.content || "").trim();
        let errText = /^error/i.test(res) ? res : "";
        if (!errText && res.startsWith("{")) {
          try {
            const o = JSON.parse(res);
            if (o && typeof o.error === "string" && o.error) errText = o.error;
          } catch {
            // A result that is not JSON: its first word decides.
          }
        }
        sa.status = errText ? "error" : "done";
        if (errText) sa.error = errText;
      }
    }
    renderSubagents();
  }

  function renderWorkflowStages() {
    if (!workflowStagesEl) return;
    workflowStagesEl.innerHTML = "";
    for (const st of workflowStages.values()) {
      const pill = document.createElement("span");
      pill.className = "workflow-pill state-" + st.state;
      const glyph = st.state === "done" ? "✓" : st.state === "running" ? "⋯" : "○";
      pill.textContent = `${glyph} ${st.name || st.id}`;
      pill.title = st.id;
      workflowStagesEl.appendChild(pill);
    }
  }

  function setWorkflow(label, active) {
    if (!workflowBar || !workflowLabel) return;
    if (!active) {
      workflowBar.classList.add("hidden");
      workflowLabel.textContent = "";
      workflowActiveName = "";
      workflowStages.clear();
      renderWorkflowStages();
      return;
    }
    workflowBar.classList.remove("hidden");
    workflowLabel.textContent = label;
  }

  function formatTok(n) {
    const v = Math.max(0, Math.round(n || 0));
    if (v >= 1_000_000) return (v / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
    if (v >= 1000) return (v / 1000).toFixed(1).replace(/\.0$/, "") + "K";
    return String(v);
  }

  function resetContextUsage() {
    ctxState.prompt = 0;
    ctxState.completion = 0;
    ctxState.estimated = false;
    ctxState.breakdown = [];
    renderContextUi();
  }

  /** Category colors for the context breakdown (Cursor-like palette). */
  const CTX_COLORS = {
    system: "#8b8b8b", // grey
    tools: "#a78bfa", // purple
    rules: "#34d399", // green
    skills: "#fbbf24", // orange
    conversation: "#f472b6", // pink
    completion: "#60a5fa", // blue
    reserved: "#4a4a4a", // dark grey
  };

  /**
   * Rows for the context popover. Uses the agent's per-category breakdown when
   * available; the conversation row absorbs the difference between the real
   * prompt total and the fixed categories so the numbers always add up.
   */
  function contextRowsData() {
    const used = ctxState.prompt;
    const bd = Array.isArray(ctxState.breakdown) ? ctxState.breakdown : [];
    const rows = [];
    if (bd.length > 0) {
      let fixedSum = 0;
      let convRow = null;
      bd.forEach((c) => {
        if (c.key === "conversation") {
          convRow = { key: c.key, label: c.label || "Conversation", tokens: c.tokens };
        } else {
          fixedSum += c.tokens;
          rows.push({ key: c.key, label: c.label || c.key, tokens: c.tokens });
        }
      });
      let conv = convRow ? convRow.tokens : 0;
      if (used > 0) {
        conv = Math.max(conv, used - fixedSum);
      }
      rows.push({ key: "conversation", label: i18n("ctx.row.conversation"), tokens: Math.max(0, conv) });
    } else if (used > 0) {
      rows.push({ key: "conversation", label: i18n("ctx.row.prompt"), tokens: used });
    }
    if (ctxState.completion > 0) {
      rows.push({ key: "completion", label: i18n("ctx.row.completion"), tokens: ctxState.completion });
    }
    return rows;
  }

  function renderContextUi() {
    const used = ctxState.prompt;
    const limit = ctxState.limit || 128000;
    const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
    if (contextRingFill) {
      contextRingFill.style.setProperty("--ctx-pct", String(pct));
    }
    if (ctxPct) {
      ctxPct.textContent = used > 0 ? `${pct}% full` : "—";
    }
    if (ctxSummary) {
      const est = ctxState.estimated ? "~" : "";
      ctxSummary.textContent =
        used > 0 ? `${est}${formatTok(used)} / ${formatTok(limit)} tokens` : `Up to ${formatTok(limit)} tokens`;
    }
    const rows = contextRowsData();
    if (ctxBar) {
      // The bar spans the whole context window: colored segments for each
      // category, a dark segment for the reserved reply budget, the rest of
      // the track is free space.
      ctxBar.innerHTML = "";
      const total = Math.max(limit, 1);
      const segs = rows.slice();
      if (ctxState.maxResponse > 0) {
        segs.push({ key: "reserved", label: i18n("ctx.row.reserved"), tokens: ctxState.maxResponse });
      }
      segs.forEach((r) => {
        if (r.tokens <= 0) return;
        const seg = document.createElement("div");
        seg.className = "ctx-bar-seg";
        seg.style.width = `${Math.min(100, Math.max(0.8, (r.tokens / total) * 100))}%`;
        seg.style.background = CTX_COLORS[r.key] || "#888888";
        seg.title = `${r.label}: ${formatTok(r.tokens)}`;
        ctxBar.appendChild(seg);
      });
    }
    if (ctxRows) {
      ctxRows.innerHTML = "";
      const items = rows.slice();
      items.push({ key: "reserved", label: i18n("ctx.row.reserved"), tokens: ctxState.maxResponse });
      items.forEach((item) => {
        if (item.tokens <= 0 && item.key !== "reserved") return;
        const row = document.createElement("div");
        row.className = "ctx-row";
        row.innerHTML = `<span class="ctx-swatch"></span><span class="ctx-row-label">${item.label}</span><span class="ctx-row-val">${formatTok(item.tokens)}</span>`;
        // The colour goes on through the CSSOM, never a style attribute in the
        // markup: both hosts serve this page under a CSP without
        // 'unsafe-inline', which drops one and left every swatch unpainted.
        const swatch = row.querySelector(".ctx-swatch");
        if (swatch) {
          swatch.style.background = CTX_COLORS[item.key] || "#888888";
        }
        ctxRows.appendChild(row);
      });
    }
  }

  function showContextPopover(show) {
    ctxPopoverOpen = show;
    contextPopover?.classList.toggle("hidden", !show);
    contextBtn?.classList.toggle("open", show);
  }

  function updateStatusFooter() {
    /* tokens shown in context popover */
  }

  function setLspStatus(st) {
    if (!statusLsp) return;
    const label = (st || "").trim();
    statusLsp.textContent = label ? `LSP ${label}` : "";
    statusLsp.classList.toggle("active", label === "active");
  }

  function imageGalleryFromList(fileList, activePath) {
    if (!Array.isArray(fileList)) return [];
    return fileList
      .filter((f) => f && f.kind === "image")
      .map((f) => ({
        name: f.name || "Image",
        path: f.path,
        previewUri: f.previewUri,
      }));
  }

  function renderImagePreviewAt(index) {
    if (!imagePreview || !imagePreviewImg || !imagePreviewState.items.length) return;
    const total = imagePreviewState.items.length;
    const idx = ((index % total) + total) % total;
    imagePreviewState.index = idx;
    const item = imagePreviewState.items[idx];
    if (imagePreviewTitle) {
      imagePreviewTitle.textContent = item.name || "Image";
    }
    if (imagePreviewCounter) {
      imagePreviewCounter.textContent = total > 1 ? `${idx + 1} / ${total}` : "";
    }
    if (imagePreviewPrevBtn) {
      imagePreviewPrevBtn.classList.toggle("hidden", total <= 1);
    }
    if (imagePreviewNextBtn) {
      imagePreviewNextBtn.classList.toggle("hidden", total <= 1);
    }
    imagePreviewImg.src = item.previewUri || "";
    imagePreviewImg.alt = item.name || "Preview";
    imagePreview.classList.remove("hidden");
  }

  function showImagePreview(name, previewUri, filePath, galleryItems) {
    if (!imagePreview || !imagePreviewImg) return;
    const items = Array.isArray(galleryItems) && galleryItems.length
      ? galleryItems.filter((g) => g && g.previewUri)
      : [{ name: name || "Image", path: filePath || "", previewUri: previewUri || "" }];
    imagePreviewState.items = items;
    const startIdx = Math.max(
      0,
      items.findIndex((g) => g.path && filePath && g.path === filePath)
    );
    renderImagePreviewAt(startIdx >= 0 ? startIdx : 0);
  }

  function hideImagePreview() {
    imagePreview?.classList.add("hidden");
    if (imagePreviewImg) {
      imagePreviewImg.src = "";
    }
    imagePreviewState = { items: [], index: 0 };
  }

  function shiftImagePreview(delta) {
    if (!imagePreviewState.items.length) return;
    renderImagePreviewAt(imagePreviewState.index + delta);
  }

  async function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === "string" ? reader.result : "";
        const comma = result.indexOf(",");
        resolve(comma >= 0 ? result.slice(comma + 1) : result);
      };
      reader.onerror = () => reject(reader.error || new Error("read failed"));
      reader.readAsDataURL(file);
    });
  }

  async function attachFilesFromDataTransfer(dt) {
    if (!dt) return;
    const fileItems = [];
    if (dt.files && dt.files.length) {
      for (let i = 0; i < dt.files.length; i++) {
        fileItems.push(dt.files[i]);
      }
    }
    for (const file of fileItems) {
      if (!file) continue;
      if (file.size > MAX_ATTACH_BYTES) {
        appendMsg("system", `Skipped ${file.name || "file"}: exceeds 20 MB limit`);
        continue;
      }
      try {
        const dataBase64 = await readFileAsBase64(file);
        host.postMessage({
          type: "attachBytes",
          name: file.name || "attachment",
          mime: file.type || undefined,
          dataBase64,
        });
      } catch {
        /* ignore single file failure */
      }
    }
  }

  function onAttachmentClick(f, _isImage, e) {
    if (!f?.path) return;
    openExternalFile(f.path, Boolean(e?.shiftKey));
  }

  function renderMsgAttachments(parent, fileList) {
    if (!parent || !fileList || !fileList.length) return;
    const wrap = document.createElement("div");
    wrap.className = "msg-attachments";
    fileList.forEach((f) => {
      if (!f || typeof f.name !== "string") return;
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "msg-attach" + (isImage ? " msg-attach-image" : "");
      card.title = (f.path || f.name) + " · click: open in editor · Shift+click: focus";

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "msg-attach-thumb";
        img.src = f.previewUri || "";
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "msg-attach-meta";
      const name = document.createElement("span");
      name.className = "msg-attach-name";
      name.textContent = f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        onAttachmentClick(f, isImage, e);
      });
      wrap.appendChild(card);
    });
    parent.appendChild(wrap);
  }

  /** @param {string} text @param {{ reasoning?: string; toolBlocks?: any[] }} opts */
