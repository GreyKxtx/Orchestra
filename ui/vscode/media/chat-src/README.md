# Chat webview sources (edit these)

`media/chat.bundle.js` is **generated** — do not edit it (same idea as `out/*.js` from TypeScript).

```bash
npm run bundle:webview   # rebuild chat.bundle.js + settings.bundle.js
npm run compile          # bundle + tsc
```

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
| `06-composer.js` | 650 | mode / model / send |
| `07-events.js` | 360 | host message handlers + boot |
