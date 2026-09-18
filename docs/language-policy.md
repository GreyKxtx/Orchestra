# Language policy for LLM-facing strings

H5 in architecture audit asked: which language should the codebase speak?

## Decision

- **Code comments, commit messages, internal docs**: English (already the case).
- **LLM-facing agent prompts and error messages** (system prompts, denial hints, plan/build mode messages, dedup STOP messages, LSP error injections): **English**. Translated in commit `<this commit>`. Rationale: English is the lingua franca of code and chat-tuned LLMs are most reliable in English. Russian-only strings hurt non-Russian operators reading logs.
- **CLI user-facing strings** (cobra command Short/Long, --help, flag descriptions, "Loading models...", "Saved!"): **kept Russian for now**. These are USER UX, not LLM context. The audience is the operator at the terminal; if the operator base expands beyond Russian-speakers, switch then. Adding i18n machinery would over-engineer the current single-locale case.
- **Tool descriptions and tool output**: **English** (revised — see below).

## Going forward

When adding a new string that the LLM will see (system prompt, error hint, tool description, schema description that ends up in tool defs), write it in English. When adding a new CLI help text, follow the existing Russian convention until policy changes.

## Audit trail

Files touched in the H5 initial pass:
- `internal/agent/agent.go` — dup-call STOP, plan_exit refusal, plan-mode write denial, diagnostic-tracker escalation hint.
- `internal/agent/circuit_breaker.go` — RecordSuccessfulCall STOP hint.
- `internal/agent/format.go` — extractLSPErrors hint, formatValidatorErrorCompact, formatPolicyDeniedCompact.
- `internal/agent/error_format_test.go` — assertions updated to match new strings.

Deferred (separate work item):
- `internal/ckg/provider.go` query/symbol/package response strings.

## Revision: tool descriptions are English after all

The original decision deferred ~60 tool descriptions on the grounds that the
default model is multilingual and the gain was uncertain. Two things changed:

1. Every prompt file was translated to English, with a test that fails on any
   Cyrillic. That left a single turn changing language between the system
   prompt and the tool schema sitting next to it.
2. The schemas turned out to be the larger half of the wire — roughly 32 KB of
   tool definitions against a ~2 KB system prompt for a cloud family — so the
   tokenisation penalty on Cyrillic lands on every request of every run, not
   just on the prompt.

All 58 tool definitions and the tool output the model reads back (grep results,
the .go read redirect) are now English, pinned by
`TestToolDefinitions_AreEnglish` in `internal/tools`.

Still Russian by policy, unchanged: CLI help, and the interactive `question`
prompts in `internal/tools/session/question.go` — those are read by the
operator at the terminal, not by the model.

## Revision: the graphical clients are multilingual

The rules above were written before the chat webview, the browser UI and the
desktop shell existed, and they do not fit those surfaces. A terminal has one
operator; a chat window that ships as a VS Code extension does not, and the
result was a screen mixing both languages — «Working…» beside «Доступ», Keep /
Drop beside a Russian hint about Accept/Reject.

Graphical clients are therefore **translated, not assigned a language**:

- **The catalogue for every webview** is `ui/vscode/media/i18n.js`. It sits
  outside both `chat-src/` and `settings-src/` because four bundles include
  it — the chat renderer and the settings panel, each on both hosts — so one
  catalogue serves all of them and a string that moves between the chat window
  and the settings panel keeps its key. Every string the user reads goes
  through `i18n(key, vars)`. It is deliberately not called `t`: `t` is a local
  variable in a dozen functions across those fragments, and a shadowed lookup
  fails silently.
- **The catalogue for the extension host** is `ui/vscode/src/i18n.ts`, for the
  notices the host itself writes into the transcript. It shares the key space
  with the renderer, so a string moved between the two keeps its key.
- **English is the fallback.** A key missing from another language renders in
  English, so a half-finished translation degrades to a readable screen rather
  than to `notice.session_busy`.
- **Choosing the language.** The picker lives in the settings panel, under
  General, and writes through to whichever store the host keeps: VS Code's
  `orchestra.language` (`auto` | `en` | `ru`), where `auto` follows the
  editor's own display language, or `localStorage["orchestra.lang"]` in the
  browser and the desktop window, defaulting to the browser's language.
- **Telling the other documents.** A window is two or three documents, not
  one, so a change has to travel: in VS Code the panel writes the setting and
  the chat window picks it up through `onDidChangeConfiguration`; on the web
  the page stores it and posts `uiLang` to the renderer and `language` into
  the settings iframe. Each document then redraws what is already on screen —
  a language change that only affected the *next* menu was the bug worth
  avoiding here.
- **Markup** carries `data-i18n`, `data-i18n-html` (for the few strings with a
  `<code>` or `<em>` in them), `data-i18n-title`, `data-i18n-placeholder` or
  `data-i18n-aria-label` instead of literal text. `ui/vscode/scripts/check-webview.mjs`
  and `ui/web/scripts/check-web.mjs` both check every key in the markup against
  the catalogue: a mistyped key is not an error anywhere — `i18n()` returns the
  key, and the screen quietly reads `set.mcp.done` where it should say "Done".

**Adding a language** is one more object in `I18N_CATALOGUE`, one more entry in
`UI_LANGUAGES`, one more object in `ui/vscode/src/i18n.ts`, and one more value
in the `orchestra.language` enum. The picker builds itself from `UI_LANGUAGES`.

Unchanged by this revision: the CLI and the TUI stay Russian, and everything
the model reads stays English. Neither goes through these catalogues.

**What is deliberately not translated**: text that belongs to something else
and only passes through — model ids and provider names from a remote catalog,
a skill's own description from its file, role labels the core sends, and error
detail quoted from the core or a provider. Translating those would be lying
about what the machine actually said.
