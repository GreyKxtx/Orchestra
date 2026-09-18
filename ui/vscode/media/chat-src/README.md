# Chat webview sources (edit these)

`media/chat.bundle.js` is **generated** — do not edit it (same idea as `out/*.js` from TypeScript).

```bash
npm run bundle:webview   # rebuild chat.bundle.js + settings.bundle.js
npm run compile          # bundle + tsc
```

The language catalogue is **not** here: `media/i18n.js` is bundled ahead of
these fragments and shared with the settings panel, which is its own bundle and
its own scope. Every string the user reads goes through `i18n(key)`, and static
markup carries `data-i18n*` — in `../../src/chat/panel.ts` for the webview and
in `ui/web/index.src.html` for the browser, which hold the same markup.

| File | ~lines | Role |
|------|--------|------|
| `01-dom-state.js` | 200 | DOM refs + shared state |
| `02-util.js` | 220 | path / label helpers |
| `03-markdown.js` | 300 | markdown + code fences |
| `04-diff-tools.js` | 330 | pending diffs / viewer |
| `05a-subagents-turn.js` | 410 | subagents + turn shell |
| `05b-overlays.js` | 190 | permission / question overlays |
| `05c-busy-palette.js` | 420 | busy UI, slash/mention palette, todos |
| `05d-tools.js` | 480 | tool cards / workflow / context |
| `05e-messages.js` | 280 | append messages / tool blocks |
| `05f-trajectory.js` | 330 | Trajectory view: pure event→row builder, renderer, Chat/Trajectory switch |
| `06-composer.js` | 650 | mode / model / send |
| `07-events.js` | 360 | host message handlers + boot |
