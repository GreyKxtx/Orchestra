  /* ------------------------------------------------------------------ *
   * Icons — one set for every graphical surface.
   *
   * It sits beside i18n.js, outside chat-src/ and settings-src/, for the
   * same reason: four bundles (chat webview, settings webview, web page,
   * settings iframe) and one source, so an icon that moves between panels
   * keeps its name and its drawing.
   *
   * WHY THIS FILE EXISTS. Before it, an icon was whichever Unicode glyph
   * looked closest — 32 distinct ones across 96 places: ∞ ◎ ◌ ▣ ≡ ⌕ ◇ ⌁
   * ◫ □ ▾ → ← ✱ ✦ ◈ $ ▣ ◉ ⏳ ⋯. A glyph is drawn by whichever font on the
   * machine happens to carry it, so each arrived at a different optical
   * weight, a different cap height and a different baseline; ⌁ and ◫ fall
   * out of the UI font entirely on Windows. Side by side in one toolbar
   * they read as a pile of unrelated marks rather than one control strip,
   * which is exactly the complaint this file answers.
   *
   * THE GRID. Every path is drawn on a 24×24 box, stroked (never filled)
   * with currentColor at 1.75 units, round caps and joins. That is the one
   * rule to keep: a new icon drawn at another weight is visible instantly
   * next to its neighbours, which is the whole point of having a grid.
   *
   * SIZES. Three, and only three, chosen by role — see --icon-* in
   * chat.css. 14px sits inside a pill's text, 16px is a standalone control,
   * 18px is a chrome-strip button. Anything else is a new size nobody asked
   * for.
   *
   * WHAT IS NOT AN ICON. Typographic marks stay text: the − and + of diff
   * stats, the ↩ of a keyboard hint, the → of an "a → b" label. Those are
   * read as characters in a sentence, not as marks on a button.
   * ------------------------------------------------------------------ */

  /**
   * Path data, keyed by name. The value is the inner markup of the <svg>:
   * whatever `orchIconMarkup` should wrap. Keep entries alphabetical inside
   * their group so a duplicate is easy to spot.
   * @type {Record<string, string>}
   */
  const ORCH_ICON_PATHS = {
    /* --- tools: what a step did -------------------------------------- */
    // read: a page with its corner turned.
    read: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
    // list: a folder, because `ls` is asking a directory what it holds.
    list: '<path d="M3 8a2 2 0 0 1 2-2h3.4l2 2H19a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    write: '<path d="M4 20h4L19.2 8.8a2.1 2.1 0 0 0-3-3L5 17z"/><path d="M14.5 6.5l3 3"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="M20.5 20.5l-4.2-4.2"/>',
    // glob: an asterisk — the wildcard itself, which is what a glob is.
    glob: '<path d="M12 5v14"/><path d="M6.2 8.5l11.6 7"/><path d="M17.8 8.5l-11.6 7"/>',
    // symbols: braces, the universal mark for "the shape of the code".
    symbols:
      '<path d="M9 4c-2 0-2 3-2 4s0 4-2 4c2 0 2 3 2 4s0 4 2 4"/><path d="M15 4c2 0 2 3 2 4s0 4 2 4c-2 0-2 3-2 4s0 4-2 4"/>',
    exec: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7.5 9.5l3 2.5-3 2.5"/><path d="M13 15h3.5"/>',
    task: '<path d="M12 3l9 4.8-9 4.8-9-4.8z"/><path d="M3 12.4l9 4.8 9-4.8"/>',
    git: '<circle cx="6.5" cy="5.5" r="2.2"/><circle cx="6.5" cy="18.5" r="2.2"/><circle cx="17.5" cy="7.5" r="2.2"/><path d="M6.5 7.7v8.6"/><path d="M17.5 9.7v.8a4 4 0 0 1-4 4H9.5"/>',
    web: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a13.5 13.5 0 0 1 0 18a13.5 13.5 0 0 1 0-18z"/>',
    mcp: '<path d="M9 3v5"/><path d="M15 3v5"/><path d="M6 8h12v2.5a6 6 0 0 1-12 0z"/><path d="M12 16.5V21"/>',
    lsp: '<circle cx="12" cy="12" r="8.5"/><path d="M12 1.5v3.5"/><path d="M12 19v3.5"/><path d="M1.5 12H5"/><path d="M19 12h3.5"/><circle cx="12" cy="12" r="2.5"/>',
    todo: '<path d="M3.5 6.5l1.8 1.8 3-3"/><path d="M3.5 16l1.8 1.8 3-3"/><path d="M12 7h8.5"/><path d="M12 16.5h8.5"/>',
    question: '<path d="M4 5.5h16v10.5H9.5L4 20.5z"/><path d="M12 12.5v-.4c0-1.1 1.6-1.3 1.6-2.6A1.6 1.6 0 0 0 10.5 9"/>',
    memory: '<path d="M6.5 3.5h11v17l-5.5-3.8-5.5 3.8z"/>',
    skill: '<path d="M11 3l1.7 4.3L17 9l-4.3 1.7L11 15l-1.7-4.3L5 9l4.3-1.7z"/><path d="M18 15l.8 2.2L21 18l-2.2.8L18 21l-.8-2.2L15 18l2.2-.8z"/>',
    trash: '<path d="M4 6.5h16"/><path d="M9.5 6.5V4.5h5v2"/><path d="M6.5 6.5l.9 13h9.2l.9-13"/>',
    diff: '<path d="M4 8h11"/><path d="M12 5l3 3-3 3"/><path d="M20 16H9"/><path d="M12 13l-3 3 3 3"/>',
    explore: '<circle cx="12" cy="12" r="9"/><path d="M15.8 8.2l-2 5.6-5.6 2 2-5.6z"/>',
    // The fallback. A dot inside a ring reads as "a step happened" without
    // claiming to say which kind — better than a wrong icon.
    tool: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.6"/>',

    /* --- status ------------------------------------------------------ */
    // A checklist's three states, drawn as one shape so the column of them
    // lines up: the box is the same box whatever is inside it.
    box: '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/>',
    "box-check": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><path d="M8.5 12l2.5 2.5 4.5-5"/>',
    "box-cross": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><path d="M9 9l6 6"/><path d="M15 9l-6 6"/>',
    "box-active": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><circle cx="12" cy="12" r="3.2" fill="currentColor" stroke="none"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    cross: '<path d="M6.5 6.5l11 11"/><path d="M17.5 6.5l-11 11"/>',
    close: '<path d="M6.5 6.5l11 11"/><path d="M17.5 6.5l-11 11"/>',
    running: '<circle cx="12" cy="12" r="8.5"/><path d="M12 6.5V12l3.5 2"/>',
    waiting: '<circle cx="12" cy="12" r="8.5"/><path d="M8 12h8"/>',

    /* --- modes: the composer's left-hand pills ----------------------- */
    "mode-agent":
      '<path d="M7 8.5a3.5 3.5 0 1 0 0 7c3.5 0 6-7 10-7a3.5 3.5 0 1 1 0 7c-4 0-6.5-7-10-7z"/>',
    "mode-orchestra": '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4"/>',
    "mode-build": '<rect x="4.5" y="4.5" width="15" height="15" rx="2.5"/><path d="M9 12h6"/>',
    "mode-plan": '<path d="M5 7h14"/><path d="M5 12h14"/><path d="M5 17h9"/>',
    "mode-explore": '<circle cx="11" cy="11" r="7"/><path d="M20.5 20.5l-4.2-4.2"/>',
    "mode-ask": '<path d="M12 3.5l8.5 8.5L12 20.5 3.5 12z"/>',
    "mode-debug": '<path d="M13.5 3L5.5 13.5H11L10.5 21l8-10.5H13z"/>',
    "mode-architecture": '<rect x="4" y="5" width="16" height="14" rx="2"/><path d="M12 5v14"/>',

    /* --- access ------------------------------------------------------ */
    // Ask: a ring left open, so the state reads as "waits for you".
    "access-ask": '<circle cx="12" cy="12" r="8.5" stroke-dasharray="3 3"/>',
    "access-auto": '<path d="M8 5.5l11 6.5-11 6.5z"/>',
    "access-browser": '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5a12.5 12.5 0 0 1 0 17a12.5 12.5 0 0 1 0-17z"/>',

    /* --- chrome and composer controls -------------------------------- */
    chat: '<path d="M20.5 11.5a8.5 8.5 0 0 1-8.5 8.5H3.5l2.6-2.6a8.5 8.5 0 1 1 14.4-5.9z"/><path d="M8.5 10.5h7"/><path d="M8.5 14h4.5"/>',
    trajectory: '<path d="M4 19V5"/><path d="M4 19h16"/><path d="M8.5 16v-4"/><path d="M12.5 16V8"/><path d="M16.5 16v-2.5"/>',
    graph: '<circle cx="12" cy="5.5" r="2.5"/><circle cx="5.5" cy="18" r="2.5"/><circle cx="18.5" cy="18" r="2.5"/><path d="M10.2 7.3L7.3 15.8"/><path d="M13.8 7.3l2.9 8.5"/>',
    gear: '<circle cx="12" cy="12" r="3.2"/><path d="M19.4 13a7.8 7.8 0 0 0 0-2l2-1.2-2-3.5-2.3 1a7.9 7.9 0 0 0-1.7-1L15 3h-4l-.4 2.3a7.9 7.9 0 0 0-1.7 1l-2.3-1-2 3.5 2 1.2a7.8 7.8 0 0 0 0 2l-2 1.2 2 3.5 2.3-1a7.9 7.9 0 0 0 1.7 1L11 21h4l.4-2.3a7.9 7.9 0 0 0 1.7-1l2.3 1 2-3.5z"/>',
    plus: '<path d="M12 5.5v13"/><path d="M5.5 12h13"/>',
    history: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7v5.2l3.4 2"/>',
    attach: '<path d="M14.5 6.5l-6.4 6.4a2.6 2.6 0 0 0 3.7 3.7l6.7-6.7a4.3 4.3 0 0 0-6.1-6.1l-6.7 6.7a6 6 0 0 0 8.5 8.5l4.3-4.3"/>',
    send: '<path d="M12 19.5V5"/><path d="M6 11l6-6 6 6"/>',
    stop: '<rect x="6.5" y="6.5" width="11" height="11" rx="2"/>',
    bolt: '<path d="M13.5 3L5.5 13.5H11L10.5 21l8-10.5H13z"/>',
    "arrow-left": '<path d="M19 12H5"/><path d="M11 6l-6 6 6 6"/>',
    "arrow-right": '<path d="M5 12h14"/><path d="M13 6l6 6-6 6"/>',
    reload: '<path d="M20 12a8 8 0 1 1-2.5-5.8"/><path d="M20 4v5h-5"/>',
    pick: '<rect x="4" y="4" width="10" height="10" rx="1.5"/><path d="M12 12l7.5 2.8-3.3 1.4-1.4 3.3z"/>',
    bookmark: '<path d="M6.5 4.5h11v15l-5.5-4-5.5 4z"/>',
    terminal: '<path d="M5.5 7.5l4 4.5-4 4.5"/><path d="M12.5 16.5h6"/>',
    dots: '<path d="M5.5 12h.01"/><path d="M12 12h.01"/><path d="M18.5 12h.01"/>',
    "chevron-down": '<path d="M6.5 9.5l5.5 5.5 5.5-5.5"/>',
    "chevron-up": '<path d="M6.5 14.5L12 9l5.5 5.5"/>',
    "chevron-right": '<path d="M9.5 6.5l5.5 5.5-5.5 5.5"/>',
    folder: '<path d="M3 8a2 2 0 0 1 2-2h3.4l2 2H19a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    copy: '<rect x="8.5" y="8.5" width="11" height="11" rx="2"/><path d="M15.5 8.5v-2a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2"/>',
    expand: '<path d="M8.5 4.5H4.5v4"/><path d="M15.5 19.5h4v-4"/><path d="M19.5 8.5v-4h-4"/><path d="M4.5 15.5v4h4"/>',
    collapse: '<path d="M4.5 8.5h4v-4"/><path d="M19.5 15.5h-4v4"/><path d="M15.5 4.5v4h4"/><path d="M8.5 19.5v-4h-4"/>',
  };

  /**
   * The rendered size of an icon, by role. Three values, deliberately:
   * inside a pill's own text, on a standalone control, on a chrome button.
   */
  const ORCH_ICON_SIZES = { sm: 14, md: 16, lg: 18 };

  /**
   * Markup for one icon.
   * @param {string} name a key of ORCH_ICON_PATHS
   * @param {{ size?: number | "sm" | "md" | "lg"; cls?: string }} [opts]
   * @returns {string} an <svg> element, or "" when the name is unknown
   */
  function orchIconMarkup(name, opts) {
    const body = ORCH_ICON_PATHS[name];
    if (!body) return "";
    const o = opts || {};
    const raw = o.size == null ? "md" : o.size;
    const px = typeof raw === "number" ? raw : ORCH_ICON_SIZES[raw] || ORCH_ICON_SIZES.md;
    const cls = o.cls ? ` ${o.cls}` : "";
    return (
      `<svg class="oi${cls}" width="${px}" height="${px}" viewBox="0 0 24 24" fill="none" ` +
      `stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" ` +
      `aria-hidden="true">${body}</svg>`
    );
  }

  /**
   * The same icon as a detached element, for the call sites that build DOM
   * rather than strings. Parsing our own constant markup is safe — the
   * paths above are the only thing that ever reaches innerHTML here.
   * @param {string} name @param {{ size?: number | "sm" | "md" | "lg"; cls?: string }} [opts]
   * @returns {SVGElement | null}
   */
  function orchIconEl(name, opts) {
    const markup = orchIconMarkup(name, opts);
    if (!markup) return null;
    const holder = document.createElement("div");
    holder.innerHTML = markup;
    return /** @type {SVGElement | null} */ (holder.firstElementChild);
  }

  /** True when an icon by that name exists — for call sites that fall back. */
  function orchHasIcon(name) {
    return Object.prototype.hasOwnProperty.call(ORCH_ICON_PATHS, name);
  }
