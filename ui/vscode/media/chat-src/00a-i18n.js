  // Everything the user reads goes through i18n(). Two languages now, more later:
  // a language is one more object in CATALOGUE plus one more entry in
  // UI_LANGUAGES, and nothing else changes.
  //
  // Why a catalogue compiled into the bundle rather than files fetched at
  // runtime: the VS Code webview has no network of its own and the browser page
  // is served under a CSP with default-src 'none', so a fetch would be blocked
  // in one host and, in the other, would paint one frame in the wrong language
  // before the answer arrived.
  //
  // English is the fallback: a key missing from another language falls back to
  // it rather than showing the key, so a half-translated language degrades to
  // a readable screen instead of "composer.access.hint".

  const I18N_FALLBACK_LANG = "en";
  /** The languages offered, in menu order. */
  const UI_LANGUAGES = [
    { id: "en", label: "English" },
    { id: "ru", label: "Русский" },
  ];

  const I18N_CATALOGUE = {
    en: {
      "mode.group.core": "Core",
      "mode.group.more": "More",

      "access.section": "Access",
      "access.ask.hint": "Shell with confirmation; edits go through Accept/Reject",
      "access.auto.hint": "Shell, and file writes land on disk immediately (no Accept/Reject)",
      "access.note": "Ask: edits are staged for Accept/Reject. Auto: edits are written straight to disk.",
      "access.tools.section": "Tools",
      "access.browser.label": "Browser",
      "access.browser.hint":
        "The agent may open pages, click and type in a browser (Playwright). Not available under Fast.",
      "access.browser.on": "{hint} · browser on",

      "turn.working": "Working…",
      "turn.running_tools": "Running tools…",
      "turn.queued": " · {n} queued",
      "turn.tasks_done": "✓ Tasks done",

      "diff.loading": "Loading diff preview…",
      "diff.more_lines": "… {n} more changed lines",
      "diff.open_file": "Open file (Shift+click: side-by-side diff)",
      "diff.keep": "Keep",
      "diff.drop": "Drop",
      "diff.keep_title": "Apply just this file (a)",
      "diff.drop_title": "Reject just this file (x)",

      "conn.connecting": "Connecting…",
      "conn.reconnecting": "Reconnecting…",
      "conn.reconnecting_n": "Reconnecting… ({n})",
      "conn.lost": "lost the connection to the core — reload the page to try again",
      "conn.reopen_failed": "Reconnected, but this chat could not be read back: {detail}",

      "notice.turn_interrupted":
        "The previous turn was interrupted (the process died). History is kept up to the last completed step.",
      "notice.background_turn_done": "A background turn finished — history has been refreshed.",
      "notice.background_turn_running":
        "A previous turn of this session is still finishing in a background process. History will refresh when it does.",
      "notice.bad_stream":
        "The model returned a malformed stream instead of calling edit/write. Try again, make the request more specific, or switch model in the composer.",
      "notice.ui_sync_failed": "ui_sync failed — the last answer may not be saved to history: {detail}",
      "notice.session_busy":
        "session is busy: the previous turn is still running. Press Stop to interrupt it.",
      "notice.memory_written": "Memory: note written to agent.md ({source})",
      "notice.memory_failed": "Memory: could not write — {detail}",
      "memory.source.model": "the model's own summary",
      "memory.source.digest": "the turn digest",
      "notice.context_nearly_full": "Context is nearly full — the chat history will be summarised",
      "notice.compaction_done": "Chat summarised: history compressed, work continues",
      "notice.compaction": "Chat summary — {detail}",
    },
    ru: {
      "mode.group.core": "Основные",
      "mode.group.more": "Дополнительные",

      "access.section": "Доступ",
      "access.ask.hint": "Shell с подтверждением; правки через Accept/Reject",
      "access.auto.hint": "Shell и запись файлов сразу на диск (без Accept/Reject)",
      "access.note": "Ask: правки в staging + Accept/Reject. Auto: правки пишутся на диск сразу.",
      "access.tools.section": "Инструменты",
      "access.browser.label": "Браузер",
      "access.browser.hint":
        "Агент может открывать страницы, нажимать и вводить текст в браузере (Playwright). Не действует при Fast.",
      "access.browser.on": "{hint} · браузер включён",

      "turn.working": "Работаю…",
      "turn.running_tools": "Выполняю инструменты…",
      "turn.queued": " · {n} в очереди",
      "turn.tasks_done": "✓ Задачи выполнены",

      "diff.loading": "Готовлю показ изменений…",
      "diff.more_lines": "… ещё {n} изменённых строк",
      "diff.open_file": "Открыть файл (Shift+клик: дифф в две колонки)",
      "diff.keep": "Принять",
      "diff.drop": "Отклонить",
      "diff.keep_title": "Применить только этот файл (a)",
      "diff.drop_title": "Отклонить только этот файл (x)",

      "conn.connecting": "Подключаюсь…",
      "conn.reconnecting": "Переподключаюсь…",
      "conn.reconnecting_n": "Переподключаюсь… ({n})",
      "conn.lost": "связь с ядром потеряна — перезагрузите страницу, чтобы попробовать снова",
      "conn.reopen_failed": "Переподключился, но этот чат не удалось прочитать: {detail}",

      "notice.turn_interrupted":
        "Предыдущий ход был прерван (процесс завершился аварийно). История сохранена до последнего выполненного шага.",
      "notice.background_turn_done": "Фоновый ход завершён — история обновлена.",
      "notice.background_turn_running":
        "Предыдущий ход этой сессии ещё завершается в фоновом процессе. История обновится автоматически, когда он закончит.",
      "notice.bad_stream":
        "Модель вернула некорректный поток вместо вызова edit/write. Попробуйте ещё раз, уточните запрос или смените модель в composer.",
      "notice.ui_sync_failed":
        "ui_sync failed — последний ответ может не сохраниться в истории: {detail}",
      "notice.session_busy":
        "session is busy: предыдущий ход ещё выполняется. Нажмите Stop, чтобы прервать его.",
      "notice.memory_written": "Память: заметка записана в agent.md ({source})",
      "notice.memory_failed": "Память: запись не удалась — {detail}",
      "memory.source.model": "сводка модели",
      "memory.source.digest": "из дайджеста хода",
      "notice.context_nearly_full": "Контекст почти заполнен — история чата будет суммаризирована",
      "notice.compaction_done": "Суммаризация чата: история сжата, работа продолжается",
      "notice.compaction": "Суммаризация чата — {detail}",
    },
  };

  let uiLang = I18N_FALLBACK_LANG;

  /** "ru-RU" → "ru"; anything we do not have → "". */
  function normaliseLang(raw) {
    const s = String(raw || "")
      .toLowerCase()
      .replace("_", "-");
    for (const { id } of UI_LANGUAGES) {
      if (s === id || s.startsWith(id + "-")) {
        return id;
      }
    }
    return "";
  }

  /** The language to use: what was asked for, else the environment's, else English. */
  function pickLang(preferred) {
    const asked = normaliseLang(preferred);
    if (asked) {
      return asked;
    }
    const nav = typeof navigator !== "undefined" ? navigator.language : "";
    return normaliseLang(nav) || I18N_FALLBACK_LANG;
  }

  /** @returns {boolean} whether the language actually changed. */
  function setUiLang(preferred) {
    const next = pickLang(preferred);
    if (next === uiLang) {
      return false;
    }
    uiLang = next;
    return true;
  }

  function currentUiLang() {
    return uiLang;
  }

  /**
   * One string. `vars` fills {placeholders}; an unknown key returns itself,
   * which is visible in a screenshot and greppable in the catalogue.
   * @param {string} key @param {Record<string, any>=} vars
   */
  function i18n(key, vars) {
    const table = I18N_CATALOGUE[uiLang];
    const fallback = I18N_CATALOGUE[I18N_FALLBACK_LANG];
    let s = table && Object.prototype.hasOwnProperty.call(table, key) ? table[key] : undefined;
    if (s === undefined) {
      s = fallback && Object.prototype.hasOwnProperty.call(fallback, key) ? fallback[key] : key;
    }
    if (vars) {
      for (const name of Object.keys(vars)) {
        s = s.split("{" + name + "}").join(String(vars[name]));
      }
    }
    return s;
  }

  /**
   * Translate markup that was written in HTML rather than built in JS:
   * data-i18n sets the text, data-i18n-title / -placeholder / -aria-label set
   * that attribute. Safe to call again after a language change.
   * @param {any=} root
   */
  function applyStaticI18n(root) {
    const scope = root || (typeof document !== "undefined" ? document : null);
    if (!scope || typeof scope.querySelectorAll !== "function") {
      return;
    }
    const pairs = [
      ["data-i18n", null],
      ["data-i18n-title", "title"],
      ["data-i18n-placeholder", "placeholder"],
      ["data-i18n-aria-label", "aria-label"],
    ];
    for (const [attr, target] of pairs) {
      const found = scope.querySelectorAll("[" + attr + "]") || [];
      for (const el of found) {
        const key = el.getAttribute(attr);
        if (!key) continue;
        if (target === null) {
          el.textContent = i18n(key);
        } else {
          el.setAttribute(target, i18n(key));
        }
      }
    }
  }
