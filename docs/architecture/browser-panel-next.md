# The browser panel: what else these pieces make possible

Written after the panel landed (`desktop`, 2026-09). It is not a plan and
nothing here is committed to: it is a list of what the machinery built for the
panel can now carry, what each thing would cost, and what it would risk — so
that when one of these is wanted, the decision starts from something better
than a blank page.

## What exists now

Six pieces, none of which existed before the panel:

| Piece | Where | What it buys |
|---|---|---|
| A child webview placed over a pane of the app's own page | `browser.rs` (`browser_open`, `browser_bounds`), `65-browser-panel.js` (`placeBrowser`) | Any web surface can be shown *inside* a view, sharing its layout, without a second window |
| A WebView2 profile of the panel's own | `browser.rs` (`panel_profile`, `panel_args`) | Cookies, cache and logins of what is browsed are kept away from the page that holds the app's capabilities |
| A devtools-protocol port scoped to that profile | `browser.rs` (`panel_port`) | Everything CDP can do — read, drive, record, intercept — against the browsed page, and nothing else |
| `browser_cdp`: one protocol call from the page | `browser.rs`, `65-browser-panel.js` (`browserCdp`) | The view asks for a screenshot, a zoom, a cache clear, in one line |
| A script injected into every page the panel opens | `ui/desktop/picker.js` | Something of ours runs in the site's own world — the element picker is one use of this, not the only one |
| A third-party web UI restyled from outside | `ui/desktop/devtools-paint.js` | Edge's DevTools wear this app's palette; the same reading-and-restating works on any embedded page whose colours are tokens |

And one path that matters more than its size: a picked element reaches the
chat as an attachment (`PICK_EVENT` → the composer). There is a route from
what the user is looking at to what the agent is told.

## Built since this was written

The first item below landed (ProtocolVersion 21), and with it the third:
`browser.*` act on the panel under three separate permissions — look, act,
run script — and navigating to the page already open reloads it past the
cache, which is the edit-and-look loop.

The route taken was not the one sketched below. An MCP server in the shell
would have been reachable whenever the shell was running; a server-initiated
request (`browser/call`) on the connection that asked for the turn is
reachable only while that turn is being served, which is the property worth
having. The sections are kept as written, wrong route and all: the reasoning
that led somewhere better is more use later than a tidied record.

## Worth doing first

**The agent sees the page the user is on.** *(done)* Today `browser.*` (10 tools,
`--allow-browser`) spawns `npx @playwright/mcp` — a separate browser, its own
profile, Node required, headless by default (`internal/browser/client.go`).
The panel is a browser the user is already looking at, already logged in,
already on the page in question. Exposing it to the agent would let "why is
this page blank" be answered from the page that is blank, rather than from a
fresh one that is not.

The route is already there: the core speaks MCP, and an MCP server may be a
loopback URL (`MCPServerConfig.URL`, Streamable HTTP). The shell can host one
that offers the panel — `page.dom`, `page.console`, `page.network`,
`page.screenshot` — and the core reaches it with no new protocol. The reverse
channel that would otherwise be needed (core → shell) is avoided entirely.

Cost: a small HTTP server in the shell, four tool definitions, a consent gate.
Risk: this is the one idea here that hands a model the keys to a logged-in
browser. It needs to sit behind the same kind of explicit consent as
`exec.run`, and to be off by default. Reading (`dom`, `console`, `network`)
and driving (`click`, `type`) deserve separate answers.

**Console and network errors into the chat in one click.** The panel already
taps the page's console (`picker.js`). Add the failed requests (`Network.*`
over `browser_cdp`) and a button that attaches both with the URL, and the most
common question a person asks about a page answers itself before it is typed.
Cost: small, all of it in the view. Risk: an attached console may carry tokens
that were logged; it should be shown before it is sent, not after.

**Edit → reload → look.** *(done)* Point the panel at the project's own dev server,
and after the agent writes a file: hard reload (already in the ⋯ menu),
screenshot (already `browserShot`), attach. A loop that closes without a hand
on the mouse, which is what UI work has been missing here. Cost: small —
wiring, not new machinery. Risk: none beyond what the panel already is.

## Worth doing when the need appears

**A picked element becomes a place in the source.** The picker already knows
the element's path. Source maps plus `Debugger.getScriptSource` can often turn
that into a file and a line, and frameworks that keep their own debug metadata
(React, Svelte) make it exact. "Move this button" stops being a hunt. Cost:
medium, and honestly uncertain — the mapping is good in development builds and
poor in production ones. Worth a spike before a plan.

**Request interception.** `Fetch.enable` lets a response be stubbed, delayed or
failed without touching the code. Developing against an endpoint that does not
exist yet, or reproducing a timeout, becomes a menu item. Cost: small-medium.
Risk: a browser that silently lies about the network is a confusing browser —
it needs a visible mark in the toolbar while it is on.

**Visual checks.** Two screenshots and a comparison make a regression test that
needs no framework. The panel takes the screenshots today; the comparison is a
few dozen lines of Go. Cost: small. Risk: the usual one — pixel diffs are
noisy across machines, so this is a local aid, not CI.

**The painter, generalised.** `devtools-paint.js` reads a page's colour tokens
and restates them in ours. Nothing in it is specific to DevTools. Any embedded
third-party web UI — a dashboard, a docs site, a rendered report — could stop
looking like a foreign object. Cost: small per surface. Risk: none; it degrades
to the surface's own colours.

## Bigger, and needing a design first

**A recorded flow becomes a test.** CDP sees every input and navigation
(`Input.*`, `Page.*`). Recording them and emitting a Playwright script is what
Chrome's Recorder does; the pieces are all here. The design question is not
recording but *naming* — a replayable script needs stable selectors, and the
picker's child-index paths are not that.

**A preview pane in Chat.** The placement machinery is not specific to the
Browser view: a second child webview could show what the agent just produced —
an HTML report, a rendered diagram, a built page — beside the transcript. The
design question is what it is allowed to load, and that is a capability
question, not a layout one.

**The shell tests itself.** Everything used to verify this panel — drive the
window over `ORCH_DEBUG_PORT`, read the page's own numbers, screenshot with
`PrintWindow` — lives in a scratchpad and dies with the session. As
`ui/desktop/scripts/` it would be a smoke suite: open the view, open the dock,
drag the line, assert the rectangles, compare a screenshot. Four of the defects
found by hand in this work (a stale bundle, a dark stage, a 403 handshake, a
tab that reappeared every launch) were each visible in one such run. Cost:
medium, and it pays back on the second regression. This is the one on the list
that makes the others cheaper.

## The rules that shape all of it

Learned the hard way; every idea above lives inside them.

- **A child webview is a view of the operating system over the page.** Nothing
  drawn in HTML can appear on top of it. A popup either avoids the rectangle,
  or the rectangle steps aside and leaves a picture behind
  (`browserCover`).
- **One WebView2 environment per user-data folder**, and a second one there
  with different `additional_browser_args` is refused. Every webview sharing a
  profile is built with the same arguments.
- **A debugging port drives every page of its environment.** That is why the
  panel has a profile of its own, and why the app's own page is not in it.
- **Rectangles cross in physical pixels** (CSS × `devicePixelRatio`) and are
  measured on the next frame, never in the same breath as the change that
  prompted them.
- **A page's own stylesheets are adopted ones**, which cascade after anything
  added from outside: restating a token needs `!important`.
- **A tool's settings are its page's storage**, seeded before it boots
  (`devtools_boot`) — which is how the welcome tab and the screencast are
  turned off without a click.

## Security posture

What the panel is today: a browser whose profile the app's own page cannot
reach, whose protocol port is bound to loopback and scoped to that profile,
and which runs no code of ours inside a site except the picker.

What each idea above would change is worth stating plainly, because it is the
same change in every case: *the agent gaining a channel into a browser the user
is logged into*. Reading a page the user chose to open is a small step; driving
it — clicking, typing, submitting — is not, and it should never be one flag
away from the other. The existing gates (`--allow-browser`, `exec.confirm`,
`Permissions.Rules`) are the right shape for this; nothing here needs a new
kind of consent, only a correct use of the kind that exists.
