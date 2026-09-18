# The graphical clients' design system

Scope: the three surfaces that share one renderer — the VS Code chat webview,
the browser page, and the Tauri desktop shell over that page — plus the
settings panel, which is a second document beside each of them.

Not in scope: the TUI. It draws with Lip Gloss into a terminal cell grid and
has its own vocabulary; see `docs/architecture/tui-chrome.md`.

---

## 1. Icons

**One set, one grid, one weight: `ui/vscode/media/icons.js`.**

Every icon on every graphical surface is an `<svg class="oi">` built from that
file, drawn on a **24×24 box**, **stroked** (never filled) with `currentColor`
at **1.75**, round caps and joins.

The file sits beside `i18n.js`, outside `chat-src/` and `settings-src/`, for
the same reason: four bundles read it (chat webview, settings webview, web
page, settings iframe), so an icon that moves between panels keeps its name and
its drawing.

```js
orchIconMarkup("git", { size: "md" })   // -> "<svg class=\"oi\" …>"
orchIconEl("chevron-down", { size: "sm" })  // the same, as an element
```

### Why the rule is worth keeping

Before it, an icon was whichever Unicode glyph looked closest — **32 distinct
ones across 96 places**: `∞ ◎ ◌ ▣ ≡ ⌕ ◇ ⌁ ◫ □ ▾ → ← ✱ ✦ ◈ $ ◉ ⏳ ⋯`. A glyph
is drawn by whichever font on the machine carries it, so every one arrived at
a different optical weight, cap height and baseline; `⌁` and `◫` fall out of
the UI font on Windows entirely. Beside them sat hand-drawn SVGs at 14, 15,
16, 18 and 22px on two grids at four stroke weights (1.4, 1.8, 2, 2.2). Each
choice was defensible alone. Together they were what made one header row read
as a pile of unrelated marks instead of a row of controls.

### The sizes

Three, chosen by role. They are `--icon-*` in `chat.css` and restated in
`settings.css` (a separate document with its own `:root`).

| token | px | where |
|---|---|---|
| `--icon-sm` | 14 | inside a control, beside its own label — a pill's mode icon, a chevron |
| `--icon-md` | 16 | a standalone control; a tool row |
| `--icon-lg` | 18 | a chrome-strip button |

### What is *not* an icon

- **Typographic marks stay text**: the `−` and `+` of diff stats, the `↩` of a
  keyboard hint, the `→` of an "a → b" label. Those are read as characters in
  a sentence.
- **Brand marks are not icons**: the Orchestra logo (`.rail-mark`,
  `.start-mark`) and GitHub's own mark are filled, on their own grids. A
  button that says GitHub shows GitHub, not a drawing of a branch.
- **Two drawings that cannot be SVG elements** — the sidebar's chat-row bubble
  and the two disclosure markers — are CSS masks. They carry the same path data
  at the same 1.75 weight; see `.rail-session::before` in `rail.css` and
  `.trace-summary::after` in `chat.css`.
- **The settings panel's own drawings** (nav items, index stats) still live on
  16 and 20 grids. They are not on the 24 grid, but they are at the same
  *optical* weight: 1.75/24 is 0.0729 units of stroke per unit of box, so a 16
  grid takes 1.17 and a 20 grid takes 1.46. Change one, change the other — the
  note at the top of `settings-body.html` says so too.

---

## 2. The control scale

Four heights, three radii, in `--ctl-h-*` and `--r-*`. Pixels, deliberately:
`zoom` on `:root` scales them together, so the proportions survive every zoom
level and every `data-scale` choice.

| token | px | where |
|---|---|---|
| `--ctl-h-xs` | 22 | a chip inside another control |
| `--ctl-h-sm` | 26 | composer pills, view segments |
| `--ctl-h-md` | 28 | the composer's own icon buttons — context ring, paperclip, Send |
| `--ctl-h-lg` | 32 | chrome-strip buttons, session tabs, the sidebar's gear |

A new value is a decision to argue for, not a default: reach for the nearest
existing one first.

Two rules that came out of specific bugs, both worth keeping:

- **Selection never changes type weight.** The view switch used to bump the
  chosen segment to `font-weight: 600`, which re-measured the label, so the
  whole switch changed width on every switch and nudged the buttons beside it.
  Selection is ground and colour.
- **A rule that sets `display` must also state the `[hidden]` case.** The
  effort pill's fast mark and the tool body's "show all" are hidden by
  attribute; a bare `display: inline-flex` outranks that and pins them open.

---

## 3. A tool row

`05d-tools.js` and `05e-messages.js`. A row is: icon · label · path · duration
· chevron, and a body that folds.

- **The icon says which tool ran, at every stage of the row's life.** It used
  to be replaced by a check mark on completion, which left the finished rows —
  nearly everything on screen — with no mark of their own kind. Success needs
  no badge once the spinner stops; failure gets one (`.tool-failed`).
- **`toolKind` is the single answer** everything downstream hangs off. Its last
  four rules are prefixes (`git.`, `gh.`, `lsp.`, `mcp:`, `browser.`) so a
  newly registered tool in one of those families is recognised without an edit.
  A tool that falls through renders under its raw name, which for `git.log` or
  `mcp:context7:query-docs` says more than any invented word would.
- **The body shows the whole result.** It used to be cut at 8000 characters
  with an ellipsis and no way back to the rest. It now folds at 24 lines with
  a footer that unfolds the whole thing (capped at 5000 lines).
- **Structured results are read as structure.** Most results are JSON, and most
  of those are the core's `{"output": "…"}` envelope wrapping text that was
  never JSON — printed raw, the thing a reader wants is behind escaped
  newlines and quotes. `toolResultView` unwraps that single-field envelope and
  pretty-prints anything genuinely structured, coloured through `--json-*`. The
  unmodified original is one click away ("Raw"), and Copy always copies the
  original bytes, because what gets pasted into a shell has to be what the
  tool returned.

---

## 4. The sidebar

A brand row over two columns, web and desktop only
(`ui/web/src/41-projects-rail.js`, `ui/web/rail.css`).

**The brand row** across the top is the mark and the word "Orchestra", once.
It says whose window this is; nothing else in the sidebar repeats it.

**The strip** on the left is the list of workspaces: one 40px square per
project, carrying the project's initial, tinted by a hue hashed from its
**path** — so three folders all called `ws` are three different colours — with
the project's **name under it**, cut to the strip's width. State sits on the
square's corner as a badge whose shape differs per state (a pulsing dot for
working, a square for waiting-for-you); an open idle project is simply a tile
at full strength, a closed one is dimmed. The one on screen is marked by a bar
on the strip's edge and by its label going to full colour. The add-workspace
button and the gear close the strip.

**The pane** beside it is the chats of the project on screen, under one line
that says its name and chat count. The path is *not* printed there: it is the
tooltip on the name and on the tile. The first version of this layout printed
it under the name, and the owner's reading was that it was noise — the label
on the tile and the heading already say the same word.

This is the layout the multi-project spec described
(`docs/superpowers/specs/2026-09-09-multi-project-window-design.md`: "a
vertical rail, one icon per project, five states"). The first implementation
had drifted into one flat list where the open project was a small-caps
heading, the rest were folder rows with unlabelled numbers, and two "+"
buttons meant two different things — the owner's verdict on it was "unclear".
The point of the sidebar is seeing, without switching, which project is
working and which is waiting; a strip of tiles shows that in a glance, a list
of folder names does not.

Two seams the tests and the handlers hang on, so keep them: every tile is a
`.project-chip` with `data-project-id / data-status / data-active`, and every
click on the sidebar is delegated from the `<nav id="project-rail">` — the
tiles live in `#project-strip`, the chats in `#project-rail-list`.

Not done yet: folding the sidebar hides the strip along with the pane. The
useful fold is the Discord one — pane away, strip stays — because the strip is
exactly what you want while the chats are out of the way.

## 5. What guards this

Both bundle checks, so a regression fails the same run that produces it:

- `ui/vscode/scripts/icon-names.mjs` reads the set and the call sites.
- `check-webview.mjs` check 6 and `check-web.mjs` check 7 assert that **every
  icon name the code asks for exists** (an unknown name renders as *nothing* —
  `orchIconMarkup` returns `""`, with no error anywhere) and that **every
  `<svg>` in `panel.ts` and on the served page carries the set's shape**
  (`class="oi"`, the 24 viewBox, stroke 1.75).

Both were proved to fail: a mistyped icon name and a hand-drawn `<svg>` each
stopped the check with the file and line.

What they do **not** cover, and what still has to be looked at: the settings
panel's own 16/20-grid drawings (not scanned — they are exempt by design, so a
new one at the wrong weight passes), and anything only a rendered frame shows.
`docs/clients-coverage.md` §8 carries the standing note that the cheapest next
step is an open-the-page-screenshot-read-the-console run in CI.
