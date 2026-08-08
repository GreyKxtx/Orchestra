# TUI Chrome — UX spec

Короткий контракт размещения сигналов в Orchestra TUI. Референс Crush/OpenCode — только density/accents, **не** whitelist.

## Wireframe

```
[ chat scrollback ]
[ ▌ Tasks (sticky) ]              optional; open todos only; hides when all done
[ ▌ workflow:name ○…⋯…✓ ]         optional; only while workflow active
[ ▌ input textarea ]
[   mode · model · provider · shell · ask|allow ]
[ project · tokens% · LSP · cost ………… hints ]
```

## Ownership (один source of truth)

| Signal | Owner | Not elsewhere |
|--------|--------|---------------|
| Agent mode (`build`/`plan`/`explore`) | Input mode-line | — |
| Model + provider | Input mode-line | Status does not repeat model |
| Shell trust (`ask` / `allow`) | Input mode-line + `/shell` | Status only shows hints during modal |
| Prompt tokens `N/M (p%)` | Status bar | No mini-bar; no `ctx` label |
| LSP ●/◐/○ | Status bar | — |
| Cost `$` | Status bar (paid providers) | — |
| Project name | Status bar left | — |
| Todos | Sticky checklist above input | Not in status bar; Ctrl+T does not toggle |
| Workflow stages | WorkflowProgress above input | Same Progress* glyphs as Tasks |
| Discoverability | Contextual status hints only (modal/busy/todos) | No permanent `/ · @ · Tab · …` legend — see `/help` and welcome |

## Interaction

- **Ctrl+T** — expand tools → toggle diff (same cascade as bare `t`).
- **`t` (пустой input)** — тот же cascade: tools → diff.
- **`d` / Ctrl+D (пустой input)** — показать/скрыть inline diff.
- **Diff review (развёрнутый diff):** `↑↓` файл · `a` принять · `x` откат/исключить · `Enter` принять все / apply pending.
- **Ctrl+R** — свернуть/развернуть Thinking (CoT).
- Bare `d`/`t` не перехватывают ввод, если в composer есть текст.
- **`/shell`** — toggle ask ↔ allow for session (persisted in `.orchestra.yml` `ui.allow_exec`).
- **Enter while agent busy** — enqueue message; drains automatically when the turn completes.
- **Permission modal** when ask + tool needs shell:
  - `[y]` once
  - `[a]` allow for session
  - `[t]` always this tool for session
  - `[n]` / Esc deny
- **Language**: chrome RU (hints, slash descs, modals); code/tool ids stay EN.

## Visual language

- Left accent `▌` + `BackgroundSecondary` for input, palettes, tasks, workflow.
- Message panels use thick `┃` mode accent.
- Diff / modal colors from `theme` tokens (Success / Error / Primary / Warning / TextMuted).
- Progress glyphs: `○` pending · `⋯` running · `✓` done · `✗` fail · `↻` redo.

## Out of scope

- Crush command whitelist
- Restoring header/footer from old design
- Отдельный `/preview` toggle (удалён; см. §7 — единый apply=true pipeline)

**In scope (реализовано):** diff review `[a]/[d]/[x]` при развёрнутом diff (`app_diff_review.go`); LSP diagnostics на `edit`/`write` tool blocks.
