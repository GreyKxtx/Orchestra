# Settings webview sources (edit these)

`media/settings.bundle.js` is **generated** — do not edit it.

```bash
npm run bundle:webview
```

The language catalogue is **not** here: `media/i18n.js` is bundled ahead of
these fragments and shared with the chat renderer, so one catalogue serves both
webviews on both hosts. Every string the user reads goes through `i18n(key)`,
and static markup in `media/settings-body.html` carries `data-i18n*` instead of
literal text.

| File | Role |
|------|------|
| `01-core.js` | helpers, nav, the language picker |
| `02-models.js` | provider/model picker |
| `03-orchestra.js` | Orchestra roles / multi-select modal |
| `04-agents-mcp.js` | agents, MCP catalog browse/install, index actions |
| `05-state.js` | host messages + apply state |
