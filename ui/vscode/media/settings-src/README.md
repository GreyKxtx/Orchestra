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

Neither is the icon set: `media/icons.js` is bundled beside the catalogue and
shared the same way. Every icon is `orchIconMarkup(name)` — never a Unicode
glyph, which is drawn by whichever font on the machine carries it and so
arrives at a different weight on every surface. The grid, the three sizes and
what is deliberately *not* an icon are in `docs/ui-design-system.md`; both
check scripts fail on a name the set does not know.

| File | Role |
|------|------|
| `01-core.js` | helpers, nav, the language picker |
| `02-models.js` | provider/model picker |
| `03-orchestra.js` | Orchestra roles / multi-select modal |
| `04-agents-mcp.js` | agents, MCP catalog browse/install, index actions |
| `05-state.js` | host messages + apply state |
