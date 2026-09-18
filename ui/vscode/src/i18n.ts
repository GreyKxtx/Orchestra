// The extension host's half of the UI language.
//
// The webviews have their own catalogue (media/i18n.js, shared by the chat
// bundle and the settings bundle) because they run in another context with no
// access to this module; the two share a key space on purpose, so a string
// moved between host and webview keeps its key.
// This file holds only the keys the host itself produces — the notices it
// posts into the transcript and the messages it puts in dialogs.
//
// Adding a language is one more entry in CATALOGUE plus one more value in the
// `orchestra.language` setting's enum. English is the fallback: a key missing
// from another language renders in English rather than as the key itself.

export type UiLang = "en" | "ru";

export const UI_LANGUAGES: UiLang[] = ["en", "ru"];

const FALLBACK: UiLang = "en";

const CATALOGUE: Record<UiLang, Record<string, string>> = {
  en: {
    "notice.turn_interrupted":
      "The previous turn was interrupted (the process died). History is kept up to the last completed step.",
    "notice.background_turn_done": "A background turn finished — history has been refreshed.",
    "notice.background_turn_running":
      "A previous turn of this session is still finishing in a background process. History will refresh when it does.",
    "notice.bad_stream":
      "The model returned a malformed stream instead of calling edit/write. Try again, make the request more specific, or switch model in the composer.",
    "notice.ui_sync_failed":
      "ui_sync failed — the last answer may not be saved to history: {detail}",
    "notice.session_busy":
      "session is busy: the previous turn is still running. Press Stop to interrupt it.",
    "notice.memory_written": "Memory: note written to agent.md ({source})",
    "notice.memory_failed": "Memory: could not write — {detail}",
    "memory.source.model": "the model's own summary",
    "memory.source.digest": "the turn digest",
    "notice.context_nearly_full": "Context is nearly full — the chat history will be summarised",
    "notice.compaction_done": "Chat summarised: history compressed, work continues",
    "notice.compaction": "Chat summary — {detail}",
    "access.ask.hint": "Ask — shell with confirmation; edits go through Accept/Reject",
  },
  ru: {
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
    "access.ask.hint": "Ask — shell с подтверждением; правки через Accept/Reject",
  },
};

let current: UiLang = FALLBACK;

/**
 * Read the `orchestra.language` setting, fall back to the editor's display
 * language, apply it to this module and hand it back so the caller can tell
 * the webview the same thing. Imported lazily so this module stays testable
 * without the vscode API.
 */
export function applyUiLanguageFromSettings(): UiLang {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const vscode = require("vscode") as typeof import("vscode");
  const setting = vscode.workspace.getConfiguration("orchestra").get<string>("language");
  const lang = resolveLang(setting, vscode.env.language);
  setLang(lang);
  return lang;
}

/** "ru-RU" → "ru"; a language we do not carry → undefined. */
export function normaliseLang(raw: string | undefined): UiLang | undefined {
  const s = String(raw || "").toLowerCase().replace("_", "-");
  return UI_LANGUAGES.find((id) => s === id || s.startsWith(id + "-"));
}

/**
 * Resolve the language from the setting, falling back to the editor's own
 * display language and then to English. "auto" and an unknown value both mean
 * "follow the editor".
 */
export function resolveLang(setting: string | undefined, editorLang: string | undefined): UiLang {
  if (setting && setting !== "auto") {
    const chosen = normaliseLang(setting);
    if (chosen) {
      return chosen;
    }
  }
  return normaliseLang(editorLang) ?? FALLBACK;
}

export function setLang(lang: UiLang): void {
  current = lang;
}

export function getLang(): UiLang {
  return current;
}

/** One string; `vars` fills {placeholders}. An unknown key returns itself. */
export function t(key: string, vars?: Record<string, string | number>): string {
  const table = CATALOGUE[current] ?? CATALOGUE[FALLBACK];
  let s = Object.prototype.hasOwnProperty.call(table, key)
    ? table[key]
    : Object.prototype.hasOwnProperty.call(CATALOGUE[FALLBACK], key)
      ? CATALOGUE[FALLBACK][key]
      : key;
  if (vars) {
    for (const name of Object.keys(vars)) {
      s = s.split("{" + name + "}").join(String(vars[name]));
    }
  }
  return s;
}
