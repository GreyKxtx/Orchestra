# The Browser Panel and its Element Picker — Design

**Status:** draft for review, 2026-09-19. Owner asked for it in so many words:
a real browser inside the app, any site, with an eyedropper that drops the
element into the chat, "как в курсоре". The point is that the model can then
change the code of what was picked.

## What it is

A browser the user drives, plus one gesture. Arm the picker, move the mouse,
the element under it lights up, click it, and its structure lands in the chat
as an attachment on the next message. On an arbitrary site that structure is
exactly what F12 shows: the element's markup and the CSS that actually applies
to it. In the user's own project that same markup is enough for the model to
find the component in the repository and edit it.

## Shape

Four pieces, three of them small.

**1. The panel (Rust, `ui/desktop/src-tauri`).** A second webview showing an
arbitrary URL. `WebviewWindowBuilder` already does this — the main window is
built that way — and `initialization_script` puts our picker into every
document the panel loads, before the page's own scripts run. Verified against
tauri 2.11.5, the version in `Cargo.toml`: `initialization_script`,
`initialization_script_for_all_frames`, `on_navigation` and
`eval_with_callback` are all there.

The panel's own chrome (URL bar, back, forward, reload, the Pick toggle) lives
in Orchestra's UI, not in the page. See Risks for where the chrome sits.

**2. The picker (JavaScript, injected).** Draws a one-element outline and a
label under the cursor, captures on click, cancels on Escape. It talks to
nobody: it leaves the payload in a module-scoped variable and the Rust side
takes it. No network, no Tauri IPC, no token ever enters a third-party page.

**3. The channel (Rust).** Arming is `webview.eval("__orchPick.arm()")`.
Collecting is `eval_with_callback("__orchPick.take()")`, which hands Rust the
JSON the page returned; while the picker is armed Rust asks every 200 ms.
Polling instead of a push because a push would need an IPC bridge in the page,
which is the one thing this design refuses. `eval_with_callback` swallows
exceptions on Windows, so the page side returns a string and never throws.

**4. The landing (existing).** The payload becomes a file and goes through the
`attachments.store` RPC that the composer already uses for dropped files: the
core writes it under `.orchestra/attachments`, the UI shows a chip, and the
next message carries it. No new tool for the model, no protocol change —
`internal/attachments` already turns a stored file into something a turn reads.

## The payload

One markdown file, named after the element, holding:

- the page URL and a CSS selector that finds the element again;
- the element's markup, trimmed: three levels of children, author attributes
  only, text nodes cut at 200 characters;
- the CSS rules that actually match the element, with their selectors and
  origin — not `getComputedStyle`, which is 340 properties of noise per node;
- the element's box: size and position.

No screenshot in v1. The model already has `browser.screenshot` through
Playwright when it wants a picture, and the owner asked for structure.

Trimming is not a nicety. A single screen-sized block is routinely 100 KB of
markup, and this file is read by the model on every turn it stays attached.

## Safety

The injected script shares a world with the page's own scripts — WebView2 has
no isolated world — so a hostile page can read and call anything we put there.
Therefore the page gets no token, no IPC bridge and no core address. The worst
a hostile page can do is fake the content of a pick, and the user sees the chip
before sending. `on_navigation` stays open for the panel (it must reach any
site), unlike the main window, which is pinned to the core's origin.

## Risks

**The chrome's home is the one open question.** A split view inside the main
window — chat left, browser right, one window, our own URL bar — needs
`Window::add_child`, which tauri 2.11.5 gates behind its `unstable` feature.
That is what the owner asked for and it is worth trying, but it is an unstable
API on the platform we ship. So: build pieces 2, 3 and 4 first, which are
identical either way, then attempt the split view; if `unstable` misbehaves on
WebView2, the fallback is a separate browser window driven from the same UI,
and nothing else in the design moves.

**The second browser.** The model's `browser.*` tools drive a Playwright
browser, which is not this panel. In v1 they are unrelated: the user picks in
the panel, the model reads the payload. Connecting them later means pointing
the tools at the panel over CDP.

## Out of scope for v1

Tabs. Console and network in the payload. Several picks in one message.
Source-file resolution through the React fiber. Vue and Svelte. The same
feature in the VS Code extension and the web UI.

## Verification

- The picker's trimming and selector generation are pure functions of a DOM:
  test them in the web test harness against a fixture page, including the
  100 KB block and a node with no stable class.
- The Rust side gets a test that a panel built with the injection script
  reports a pick, and that a page that returns rubbish is ignored rather than
  attached.
- One manual pass at the end: open a real third-party site, pick a button,
  confirm what lands in the chat is what F12 shows for that button.
