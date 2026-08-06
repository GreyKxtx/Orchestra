# TUI Chrome — UX spec

Короткий контракт размещения сигналов в Orchestra TUI. Референс Crush/OpenCode — только density/accents, **не** whitelist.

## Wireframe

```
[ chat scrollback ]
[ ▌ Tasks N/M · Ctrl+T ]          optional; expand → numbered rows + status glyphs
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
| Todos | Task panel above input | Not in status bar |
| Workflow stages | WorkflowProgress above input | Same Progress* glyphs as Tasks |
| Discoverability | Contextual status hints only (modal/busy/todos) | No permanent `/ · @ · Tab · …` legend — see `/help` and welcome |

## Interaction

- **Ctrl+T** — cascade: expand tools → toggle diff → open Tasks (if any). Open Tasks panel → close first.
- **`t` (пустой input)** — Tasks если есть todos; иначе тот же cascade.
- **`d` / Ctrl+D (пустой input)** — показать/скрыть inline diff.
- **Ctrl+R** — свернуть/развернуть Thinking (CoT).
- Bare `d`/`t` не перехватывают ввод, если в composer есть текст.
- **`/shell`** — toggle ask ↔ allow for session (persisted in `.orchestra.yml` `ui.allow_exec`).
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
- Pending-ops `[a]/[d]/[x]` action bar in TUI
