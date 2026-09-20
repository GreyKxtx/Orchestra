  // ---- the Browser view -----------------------------------------------------
  //
  // A fourth segment beside Chat, Trajectory and Graph: a real browser, and an
  // eyedropper that drops what you click into the chat as an attachment — the
  // markup and the CSS that F12 would show for that element, so the model can
  // find the component in the repository and change it.
  //
  // The page itself draws only the chrome: a toolbar, an empty stage and a
  // console drawer. What fills the stage is a webview of its own
  // (ui/desktop/src-tauri/src/browser.rs), an OS view laid over this one: no
  // iframe, because the sites worth inspecting refuse to be framed. The
  // stage's rectangle is measured here and handed to Rust, which keeps the
  // webview over it.
  //
  // Everything the toolbar cannot do by itself goes through two commands:
  // `browser_eval` runs a line in the page, and `browser_cdp` calls the
  // devtools protocol WebView2 already speaks — which is where the screenshot,
  // the hard reload and the two Clear items come from.
  //
  // Desktop only. In a plain browser there is no panel, so the segment is
  // never built rather than offering something that cannot work.

  const browserApp = document.getElementById("app");
  const browserSwitchEl = document.getElementById("view-switch");
  const browserChatBtn = document.getElementById("view-chat-btn");
  const browserTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const browserTrajectoryPane = document.getElementById("trajectory");

  /** @type {any} */ let browserBtn = null;
  /** @type {any} */ let browserPane = null;
  /** @type {any} */ let browserStage = null;
  /** @type {any} */ let browserUrlEl = null;
  /** @type {any} */ let browserPickBtn = null;
  /** @type {any} */ let browserMenuBtn = null;
  /** @type {any} */ let browserMenuEl = null;
  /** @type {any} */ let browserLinksBtn = null;
  /** @type {any} */ let browserLinksEl = null;
  /** @type {any} */ let browserSuggestEl = null;
  /** @type {any} */ let browserDockEl = null;
  /** @type {any} */ let browserDockBtn = null;
  /** @type {any} */ let browserSplitEl = null;
  let browserDockOpen = false;
  let browserDockW = 0;
  let browserDragging = false;
  let browserPlaceQueued = false;
  /** @type {any[]} */ let browserSuggested = [];
  let browserSuggestAt = -1;
  /** @type {any} */ let browserStatusEl = null;
  /** @type {any} */ let browserZoomEl = null;
  /** @type {any} */ let browserEngineEl = null;
  /** @type {any} */ let browserAppEl = null;
  let browserOpened = false;
  let browserPickArmed = false;
  let browserZoom = 1;
  /** @type {any[]} */ let browserInstalled = [];

  /** The search engines the address bar falls back to when the text is not a URL. */
  const BROWSER_ENGINES = [
    { id: "google", name: "Google", search: "https://www.google.com/search?q=" },
    { id: "bing", name: "Bing", search: "https://www.bing.com/search?q=" },
    { id: "duckduckgo", name: "DuckDuckGo", search: "https://duckduckgo.com/?q=" },
    { id: "yandex", name: "Yandex", search: "https://yandex.ru/search/?text=" },
  ];

  /** Past this many saved links the oldest drops off. */
  const MAX_LINKS = 60;

  /** How wide the inspector sits beside the page until the user drags it. */
  const BROWSER_DOCK_W = 400;

  /** What each side keeps whatever the line is dragged to. */
  const BROWSER_DOCK_MIN = 260;
  const BROWSER_STAGE_MIN = 320;

  /** How much of where the user has been the bar remembers, and offers. */
  const MAX_HISTORY = 200;
  const MAX_SUGGEST = 8;

  /** Zoom steps, the ones a browser's menu offers. */
  const BROWSER_ZOOMS = [0.5, 0.67, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3];

  /* ---- what the user chose ------------------------------------------------ */
  //
  // Beside the theme, the scale and the language: choices about this page, kept
  // in this browser rather than in the project's config, because they say how
  // the user likes to work and not what the project is.

  /** @param {string} key @param {string} fallback */
  function browserPref(key, fallback) {
    try {
      return (window.localStorage && window.localStorage.getItem(key)) || fallback;
    } catch (e) {
      return fallback;
    }
  }

  /** @param {string} key @param {string} value */
  function setBrowserPref(key, value) {
    try {
      if (window.localStorage) window.localStorage.setItem(key, value);
    } catch (e) {
      // A page without storage still works; the choice just does not survive.
    }
  }

  function browserEngine() {
    const id = browserPref("orchestra.browser.engine", "google");
    return BROWSER_ENGINES.find((e) => e.id === id) || BROWSER_ENGINES[0];
  }

  /**
   * What the address bar means. A scheme, a host with a dot, localhost or an
   * address is somewhere to go; anything else is something to look up.
   * @param {string} raw @param {string} [search] the engine's query prefix
   * @returns {string}
   */
  function browserAddress(raw, search) {
    const s = String(raw == null ? "" : raw).trim();
    if (!s) return "";
    // A scheme only where an authority follows it: "localhost:5173" is a host
    // and a port, not a scheme.
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) return s;
    if (/^(localhost|\[[0-9a-f:]+\])(:\d+)?([/?#]|$)/i.test(s)) return `http://${s}`;
    if (/^\d{1,3}(\.\d{1,3}){3}(:\d+)?([/?#]|$)/.test(s)) return `http://${s}`;
    if (!/\s/.test(s) && /^[^\s/@]+\.[a-z]{2,}(:\d+)?([/?#]|$)/i.test(s)) return `https://${s}`;
    return (search || browserEngine().search) + encodeURIComponent(s);
  }

  /**
   * The saved links after one more: newest first, one row per address, and
   * bounded, because this lives in the browser's own storage.
   * @param {any[]} list @param {{url: string, title?: string}} entry
   */
  function browserLinksWith(list, entry) {
    const rows = Array.isArray(list) ? list.filter((r) => r && r.url) : [];
    const url = String((entry && entry.url) || "").trim();
    if (!url) return rows.slice(0, MAX_LINKS);
    const title = String((entry && entry.title) || "").trim() || url;
    return [{ url, title }].concat(rows.filter((r) => r.url !== url)).slice(0, MAX_LINKS);
  }

  /**
   * What the bar offers for what has been typed so far: the saved links
   * first, because they were kept on purpose, then where the user has been.
   * An empty query offers the most recent of each, which is what a bar that
   * has just been clicked into should show.
   * @param {string} typed @param {any[]} links @param {any[]} history
   * @param {number} [limit]
   */
  function browserSuggest(typed, links, history, limit) {
    const q = String(typed || "").trim().toLowerCase();
    const cap = limit || MAX_SUGGEST;
    const out = [];
    const seen = {};
    const take = (rows, kind) => {
      for (const row of Array.isArray(rows) ? rows : []) {
        if (out.length >= cap) return;
        const url = String((row && row.url) || "");
        if (!url || seen[url]) continue;
        const title = String((row && row.title) || "");
        if (q && url.toLowerCase().indexOf(q) < 0 && title.toLowerCase().indexOf(q) < 0) continue;
        seen[url] = true;
        out.push({ url, title: title || url, kind });
      }
    };
    take(links, "link");
    take(history, "history");
    return out;
  }

  // The three pieces of this view worth testing without a browser
  // (ui/web/scripts/adapter-test.mjs reads them off the global).
  globalThis.__orchBrowserAddress = browserAddress;
  globalThis.__orchBrowserLinks = browserLinksWith;
  globalThis.__orchBrowserSuggest = browserSuggest;
  globalThis.__orchBrowserDockFit = browserDockFit;
  // The agent's side of the panel, so it can be exercised in a real window
  // without a model in the loop.
  globalThis.__orchBrowserPanelOp = (op, params) => browserPanelOp(op, params);
  globalThis.__orchBrowserAgentSet = (may) => browserAgentSet(may);

  /**
   * The width the dock takes when asked for `want` out of `room`: its own
   * minimum, and never so much that the page has nothing left to show. A
   * window too narrow for both keeps the dock at its minimum and lets the
   * page have the rest, however little that is.
   *
   * @param {number} want @param {number} room
   */
  function browserDockFit(want, room) {
    const most = Math.max(BROWSER_DOCK_MIN, room - BROWSER_STAGE_MIN);
    return Math.round(Math.min(Math.max(want || BROWSER_DOCK_W, BROWSER_DOCK_MIN), most));
  }

  // The developer tools painted in this app's colours: the palette every other
  // view here is drawn from, measured off this page and handed to the painter
  // that runs inside the tools (ui/desktop/devtools-paint.js). It reads every
  // colour they define and restates it in these — a grey as our grey of the
  // same depth, the browser's blue as our accent. Colour only: their type,
  // their icons and the arrangement of their panels stay as they are.
  const BROWSER_PALETTE = [
    ["bg", "--bg"],
    ["surface", "--surface"],
    ["surface2", "--surface-2"],
    ["border", "--border"],
    ["muted2", "--muted-2"],
    ["muted", "--muted"],
    ["fgDim", "--fg-dim"],
    ["fg", "--fg"],
    ["accent", "--mode-accent"],
  ];

  /** This app's colours as this page has them right now, as JSON. */
  function browserDockPalette() {
    if (typeof window.getComputedStyle !== "function") return "{}";
    const computed = window.getComputedStyle(document.documentElement);
    const out = {};
    for (const [name, ours] of BROWSER_PALETTE) {
      const value = (computed.getPropertyValue(ours) || "").trim();
      if (value) out[name] = value;
    }
    return JSON.stringify(out);
  }

  /** Light or dark, as this window has it — the ground those colours expect. */
  function browserDockTheme() {
    const chosen = document.documentElement.getAttribute("data-theme");
    if (chosen === "light" || chosen === "dark") return chosen;
    const light = typeof window.matchMedia === "function"
      && window.matchMedia("(prefers-color-scheme: light)").matches;
    return light ? "light" : "dark";
  }

  /** What the page and the tools share, the line between them set aside. */
  function browserBodyRoom() {
    const body = browserDockEl && browserDockEl.parentNode;
    if (!body || !body.getBoundingClientRect) return 0;
    const line = browserSplitEl && browserSplitEl.offsetWidth ? browserSplitEl.offsetWidth : 0;
    return body.getBoundingClientRect().width - line;
  }

  /** Put the dock at `want` css pixels of `room`, and answer with what it took. */
  function setBrowserDockWidth(want, room) {
    browserDockW = browserDockFit(want, room);
    if (browserDockEl) browserDockEl.style.flexBasis = `${browserDockW}px`;
    return browserDockW;
  }

  /** @returns {any[]} */
  function browserHistory() {
    try {
      const rows = JSON.parse(browserPref("orchestra.browser.history", "[]"));
      return Array.isArray(rows) ? rows.filter((r) => r && r.url) : [];
    } catch (e) {
      return [];
    }
  }

  /** Where the panel has just been. Same shape as a saved link, no title. */
  function rememberBrowserUrl(url) {
    const address = String(url || "");
    if (!address || address.indexOf("about:") === 0) return;
    const rows = browserLinksWith(browserHistory(), { url: address }).slice(0, MAX_HISTORY);
    setBrowserPref("orchestra.browser.history", JSON.stringify(rows));
  }

  /** @returns {any[]} */
  function browserLinks() {
    try {
      const rows = JSON.parse(browserPref("orchestra.browser.links", "[]"));
      return Array.isArray(rows) ? rows.filter((r) => r && r.url) : [];
    } catch (e) {
      return [];
    }
  }

  /** @param {any[]} rows */
  function setBrowserLinks(rows) {
    setBrowserPref("orchestra.browser.links", JSON.stringify(rows));
    drawBrowserLinks();
  }

  /** The page open right now, as a link worth keeping. */
  async function saveBrowserLink() {
    const answer = await browserEval("[String(location.href), String(document.title || '')]");
    const url = Array.isArray(answer) ? String(answer[0] || "") : "";
    if (!url || url.startsWith("about:")) return;
    setBrowserLinks(browserLinksWith(browserLinks(), { url, title: String(answer[1] || "") }));
    browserStatus(i18n("browser.link_saved"));
  }

  /** @param {string} url */
  function forgetBrowserLink(url) {
    setBrowserLinks(browserLinks().filter((r) => r.url !== url));
  }

  /** The Tauri bridge, or null when the page is not running in the shell. */
  function browserBridge() {
    const t = window.__TAURI__;
    return t && t.core && typeof t.core.invoke === "function" ? t : null;
  }

  function browserViewActive() {
    return Boolean(browserApp && browserApp.dataset && browserApp.dataset.view === "browser");
  }

  /** A line in the toolbar — the transcript is hidden while this view is up. */
  function browserStatus(text) {
    if (!browserStatusEl) return;
    browserStatusEl.textContent = text;
    browserStatusEl.title = text;
    browserStatusEl.hidden = !text;
  }

  /** One command, with whatever went wrong said where the user is looking. */
  async function browserInvoke(command, args) {
    const t = browserBridge();
    if (!t) return false;
    try {
      await t.core.invoke(command, args || {});
      return true;
    } catch (err) {
      const text = `[error] browser: ${String((err && err.message) || err)}`;
      browserStatus(text);
      toRenderer({ type: "systemNote", text });
      return false;
    }
  }

  /**
   * One line of JavaScript in the page, and what it evaluated to. The shell
   * hands back the result JSON encoded; a page that never answers times out
   * there rather than here.
   * @param {string} js
   * @returns {Promise<any>} undefined when the panel could not answer
   */
  async function browserEval(js) {
    const t = browserBridge();
    if (!t) return undefined;
    try {
      const raw = await t.core.invoke("browser_eval", { js });
      return JSON.parse(String(raw));
    } catch (err) {
      const text = `[error] browser: ${String((err && err.message) || err)}`;
      browserStatus(text);
      return undefined;
    }
  }

  /**
   * One devtools-protocol call on the panel. The same protocol the browser's
   * own F12 speaks, which is where the screenshot and the Clear items come
   * from — WebView2 has no other API for them.
   * @param {string} method @param {any} [params]
   * @param {boolean} [quiet] a failure nobody asked about is not reported
   * @returns {Promise<any>} undefined when the call failed
   */
  async function browserCdp(method, params, quiet) {
    const t = browserBridge();
    if (!t) return undefined;
    try {
      const raw = await t.core.invoke("browser_cdp", {
        method,
        params: JSON.stringify(params || {}),
      });
      return raw ? JSON.parse(String(raw)) : {};
    } catch (err) {
      if (!quiet) {
        const text = `[error] browser: ${String((err && err.message) || err)}`;
        browserStatus(text);
        toRenderer({ type: "systemNote", text });
      }
      return undefined;
    }
  }

  const BROWSER_NOWHERE = { x: 0, y: 0, width: 0, height: 0 };

  /**
   * The stage's rectangle in physical pixels — where the webview has to sit.
   * Physical, not CSS: the display's scaling and the app's own zoom both
   * change what a CSS pixel is, and devicePixelRatio carries them both.
   */
  function browserRect() {
    if (!browserStage || !browserStage.getBoundingClientRect) {
      return BROWSER_NOWHERE;
    }
    const r = browserStage.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    return {
      x: Math.round(r.left * dpr),
      y: Math.round(r.top * dpr),
      width: Math.round(r.width * dpr),
      height: Math.round(r.height * dpr),
    };
  }

  /** The dock's rectangle, in the same physical pixels as the stage's. */
  function browserDockRect() {
    if (!browserDockEl || browserDockEl.hidden || !browserDockEl.getBoundingClientRect) {
      return BROWSER_NOWHERE;
    }
    const r = browserDockEl.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    return {
      x: Math.round(r.left * dpr),
      y: Math.round(r.top * dpr),
      width: Math.round(r.width * dpr),
      height: Math.round(r.height * dpr),
    };
  }

  /** The rectangles of every menu and list open over this view right now. */
  function browserPopupRects() {
    const out = [];
    const add = (el) => {
      if (el && !el.hidden && el.getBoundingClientRect) out.push(el.getBoundingClientRect());
    };
    add(browserSuggestEl);
    for (const p of browserPopups) add(p.menu);
    return out;
  }

  /** True when an open popup lies over `el`: that pane has to step aside. */
  function browserPopupsOver(el) {
    if (!el || !el.getBoundingClientRect) return false;
    const r = el.getBoundingClientRect();
    return browserPopupRects().some((p) =>
      p.width > 0 && p.height > 0
      && p.left < r.right && p.right > r.left && p.top < r.bottom && p.bottom > r.top);
  }

  /** Whether `el`, once shown, would lie over the page — measured unseen. */
  function browserWouldCover(el) {
    if (!el || !el.style) return false;
    const hidden = el.hidden;
    el.hidden = false;
    el.style.visibility = "hidden";
    const over = browserPopupsOver(browserStage);
    el.style.visibility = "";
    el.hidden = hidden;
    return over;
  }

  /** @type {Promise<void> | null} */ let browserCovering = null;
  let browserUncoverTimer = 0;

  /**
   * A picture of the page for the time a menu is open over it. The page is an
   * OS view over this one and nothing drawn here can appear on top of it, so
   * while a menu lies over it the view steps aside — and this is what stands
   * in its place: the page as it was a moment ago, not a hole. Taken before
   * the view goes, so there is nothing to see in between.
   */
  function browserCover() {
    if (!browserOpened || !browserStage || !browserStage.style) return Promise.resolve();
    if (browserUncoverTimer) {
      clearTimeout(browserUncoverTimer);
      browserUncoverTimer = 0;
    }
    if (browserStage.style.backgroundImage) return Promise.resolve();
    if (browserCovering) return browserCovering;
    browserCovering = browserCdp(
      "Page.captureScreenshot",
      { format: "jpeg", quality: 80, captureBeyondViewport: false },
      true
    ).then((answer) => {
      browserCovering = null;
      const data = answer && answer.data;
      if (data && browserStage && browserStage.style) {
        browserStage.style.backgroundImage = `url(data:image/jpeg;base64,${data})`;
      }
    });
    return browserCovering;
  }

  /** The page is back; the picture can go, once the view has had a frame. */
  function browserUncover() {
    if (!browserStage || !browserStage.style || !browserStage.style.backgroundImage) return;
    if (browserUncoverTimer) return;
    browserUncoverTimer = setTimeout(() => {
      browserUncoverTimer = 0;
      if (browserStage && !browserPopupsOver(browserStage)) {
        browserStage.style.backgroundImage = "";
      }
    }, 300);
  }

  /**
   * Keep the two webviews over their panes — the page over the stage, the
   * developer tools over the dock — or out of sight while the view is not up,
   * and while a popup lies over one of them: they are OS views over this
   * page, so nothing drawn here can appear on top of them. A menu takes only
   * the pane it covers away, for as long as it is open, and the page leaves
   * its picture behind (browserCover) so nothing seems to vanish.
   *
   * Measured on the next frame, never in the same breath as the change that
   * prompted it: a rectangle read before the layout has run is the old one,
   * which is how the page kept the width the dock had just given back.
   */
  function placeBrowser() {
    if (!browserOpened || browserPlaceQueued) return;
    browserPlaceQueued = true;
    const soon = typeof requestAnimationFrame === "function" ? requestAnimationFrame : setTimeout;
    soon(() => {
      browserPlaceQueued = false;
      // The line may have been dragged, or the window resized under it.
      if (browserDockOpen) setBrowserDockWidth(browserDockW, browserBodyRoom());
      const up = browserViewActive();
      const pageShowing = up && !browserPopupsOver(browserStage);
      const dockShowing = up && browserDockOpen && !browserPopupsOver(browserDockEl);
      void browserInvoke("browser_bounds", {
        rect: pageShowing ? browserRect() : BROWSER_NOWHERE,
      });
      if (pageShowing) browserUncover();
      void browserInvoke("browser_devtools", {
        on: dockShowing,
        rect: dockShowing ? browserDockRect() : BROWSER_NOWHERE,
        theme: browserDockTheme(),
        palette: browserDockPalette(),
      });
    });
  }

  async function openBrowserAt(url) {
    const address = browserAddress(url);
    if (!address) {
      if (browserUrlEl && browserUrlEl.focus) browserUrlEl.focus();
      return;
    }
    if (browserUrlEl) browserUrlEl.value = address;
    if (await browserInvoke("browser_open", { url: address, rect: browserRect() })) {
      browserOpened = true;
      browserStatus("");
      rememberBrowserUrl(address);
    }
  }

  /* ---- what the agent is allowed to see ---------------------------------- */
  //
  // The core asks over "browser/call" (docs/PROTOCOL.md) and this answers. It
  // is the person's own browser on the other end of that request, which is the
  // whole point — the page they are looking at, with their session — and also
  // the whole risk, so the answer to an op that is not permitted, or a view
  // that is not open, is a refusal rather than an exception.
  //
  // Every op's result is a plain object; `{ error }` is how a refusal reads on
  // the wire, because the client's reply channel carries results only.

  /** How many lines of a page the model is shown before the rest is counted. */
  const BROWSER_SNAP_LINES = 300;

  /** Ref → backendNodeId for the last snapshot: what a later click addresses. */
  /** @type {Map<string, number>} */ const browserSnapRefs = new Map();

  /**
   * True when this host has a browser view at all — which is what the turn's
   * `browser_panel` claims. Not the same as having a page: navigating is how
   * the first page arrives, so it must be reachable before there is one.
   */
  function browserPanelAvailable() {
    return Boolean(browserBridge());
  }

  /** True when there is a page loaded for the agent to look at or act in. */
  function browserPanelOpen() {
    return Boolean(browserOpened && browserBridge());
  }

  /** The page's own answer about itself — our script, never the model's. */
  async function browserPanelStatus() {
    const answer = await browserCdp("Runtime.evaluate", {
      expression: "JSON.stringify({url: location.href, title: document.title})",
      returnByValue: true,
    }, true);
    const value = answer && answer.result && answer.result.value;
    let said = {};
    try {
      said = value ? JSON.parse(String(value)) : {};
    } catch (e) {
      said = {};
    }
    return {
      open: true,
      url: String(said.url || (browserUrlEl && browserUrlEl.value) || ""),
      title: String(said.title || ""),
    };
  }

  /**
   * The page as the tools see it: its accessibility tree, flattened to lines a
   * model can read and address. A ref is this panel's own — it names a node of
   * this page in this snapshot, and means nothing to any other browser.
   */
  async function browserPanelSnapshot() {
    await browserCdp("Accessibility.enable", {}, true);
    const answer = await browserCdp("Accessibility.getFullAXTree", {}, true);
    const nodes = (answer && answer.nodes) || [];
    if (!nodes.length) return { error: "the page did not answer with a tree" };

    const byId = new Map();
    for (const node of nodes) byId.set(String(node.nodeId), node);
    browserSnapRefs.clear();

    const lines = [];
    let more = 0;
    let at = 0;
    // What the tree says twice is not worth a line: the box a run of text is
    // laid out in, and the text of a thing that already shows its own name.
    // A model paying by the token should be given the page, not its geometry.
    const SKIP = new Set(["InlineTextBox", "LineBreak", "RootWebArea", "generic", "none", ""]);
    const walk = (node, depth, said) => {
      if (!node) return;
      const role = (node.role && node.role.value) || "";
      const name = String((node.name && node.name.value) || "").replace(/\s+/g, " ").trim();
      let show = !node.ignored && !SKIP.has(role)
        && (name || role === "textbox" || role === "img" || role === "checkbox");
      if (show && role === "StaticText" && name && said.indexOf(name) >= 0) show = false;
      if (show) {
        if (lines.length < BROWSER_SNAP_LINES) {
          const ref = `a${++at}`;
          if (typeof node.backendDOMNodeId === "number") {
            browserSnapRefs.set(ref, node.backendDOMNodeId);
          }
          const pad = "  ".repeat(Math.min(depth, 8));
          const shown = name ? ` "${name.slice(0, 120)}"` : "";
          lines.push(`[${ref}] ${pad}${role}${shown}`);
        } else {
          more++;
        }
      }
      for (const kid of node.childIds || []) {
        walk(byId.get(String(kid)), show ? depth + 1 : depth, show && name ? name : said);
      }
    };
    walk(nodes[0], 0, "");
    if (more) lines.push(`… ${more} more node(s) not shown`);

    const status = await browserPanelStatus();
    const head = `page ${status.url}${status.title ? ` — "${status.title}"` : ""}`;
    return { snapshot: [head].concat(lines).join("\n") };
  }

  /** @param {any} params */
  async function browserPanelScreenshot(params) {
    const answer = await browserCdp("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: Boolean(params && params.full_page),
    }, true);
    const data = answer && answer.data;
    if (!data) return { error: "the page did not answer with an image" };
    return { image: String(data) };
  }

  /** What this turn's sender allowed. Read is implied by having a panel. */
  let browserAgentMay = { drive: false, eval: false };

  /**
   * Remember what the turn being sent was allowed to do, so this side can
   * refuse an op above it. The core checks the same thing; this is the half
   * that cannot be talked out of it, because it is the half that owns the
   * browser.
   * @param {{drive?: boolean, eval?: boolean}} may
   */
  function browserAgentSet(may) {
    browserAgentMay = { drive: Boolean(may && may.drive), eval: Boolean(may && may.eval) };
  }

  /** Say in the toolbar what is being done to the page, and by whom. */
  function browserAgentBusy(what) {
    if (browserPane && browserPane.dataset) {
      if (what) browserPane.dataset.agent = what;
      else delete browserPane.dataset.agent;
    }
    if (what) browserStatus(i18n(what === "driving" ? "browser.agent_driving" : "browser.agent_reading"));
    else browserStatus("");
  }

  /** The node a ref or a selector names, as a remote object. */
  async function browserTarget(params) {
    const ref = String((params && params.ref) || "");
    if (ref) {
      const backendNodeId = browserSnapRefs.get(ref);
      if (!backendNodeId) {
        throw new Error(`no element ${ref} on this page — take a snapshot first`);
      }
      const node = await browserCdp("DOM.resolveNode", { backendNodeId }, true);
      const objectId = node && node.object && node.object.objectId;
      if (!objectId) throw new Error(`${ref} is no longer on the page — take a snapshot again`);
      return objectId;
    }
    const selector = String((params && params.element) || "");
    if (!selector) throw new Error("ref or element is required");
    // The selector is the model's, the script around it is ours.
    const found = await browserCdp("Runtime.evaluate", {
      expression: `document.querySelector(${JSON.stringify(selector)})`,
    }, true);
    const objectId = found && found.result && found.result.objectId;
    if (!objectId) throw new Error(`nothing on the page matches ${selector}`);
    return objectId;
  }

  /** Run a function of ours on that node, and answer with what it returned. */
  async function browserOnNode(objectId, fn, args) {
    const answer = await browserCdp("Runtime.callFunctionOn", {
      objectId,
      functionDeclaration: fn,
      arguments: (args || []).map((value) => ({ value })),
      returnByValue: true,
      awaitPromise: true,
    }, true);
    if (answer && answer.exceptionDetails) {
      throw new Error(String(answer.exceptionDetails.text || "the page refused"));
    }
    return answer && answer.result ? answer.result.value : undefined;
  }

  /**
   * A real click where the element is, not element.click(): a page that
   * listens for a press, a hover or a focus gets what it is listening for.
   * An element with no box — hidden, or scrolled out of a clipped container —
   * falls back to the DOM call, which is better than refusing.
   */
  async function browserPanelClick(params) {
    const objectId = await browserTarget(params);
    await browserOnNode(objectId, "function () { this.scrollIntoView({block: 'center', inline: 'center'}); }");
    const box = await browserCdp("DOM.getBoxModel", { objectId }, true);
    const quad = box && box.model && box.model.content;
    if (!quad || quad.length < 8) {
      await browserOnNode(objectId, "function () { this.click(); }");
      return { result: "clicked (no box on the page, so through the DOM)" };
    }
    const x = (quad[0] + quad[2] + quad[4] + quad[6]) / 4;
    const y = (quad[1] + quad[3] + quad[5] + quad[7]) / 4;
    const at = { x, y, button: "left", clickCount: 1 };
    await browserCdp("Input.dispatchMouseEvent", { type: "mouseMoved", ...at }, true);
    await browserCdp("Input.dispatchMouseEvent", { type: "mousePressed", ...at }, true);
    await browserCdp("Input.dispatchMouseEvent", { type: "mouseReleased", ...at }, true);
    return { result: `clicked at ${Math.round(x)},${Math.round(y)}` };
  }

  /** Put text in a field: focus it, take what was there, then type. */
  async function browserPanelType(params) {
    const objectId = await browserTarget(params);
    await browserOnNode(objectId, "function () { this.scrollIntoView({block: 'center'}); this.focus(); " +
      "if (this.select) this.select(); }");
    const text = String((params && params.text) || "");
    await browserCdp("Input.insertText", { text }, true);
    // Frameworks listen for input, and insertText alone does not always reach
    // the ones that watch the property rather than the event.
    await browserOnNode(objectId, "function () { this.dispatchEvent(new Event('input', {bubbles: true})); " +
      "this.dispatchEvent(new Event('change', {bubbles: true})); }");
    return { result: `typed ${text.length} character(s)` };
  }

  /** @param {any} params */
  async function browserPanelFill(params) {
    const fields = (params && params.fields) || [];
    let filled = 0;
    for (const field of fields) {
      await browserPanelType({ ref: field.ref, element: field.element, text: String(field.value || "") });
      filled++;
    }
    return { filled };
  }

  /** @param {any} params */
  async function browserPanelSelect(params) {
    const objectId = await browserTarget(params);
    const value = String((params && params.value) || "");
    const done = await browserOnNode(objectId, "function (want) { " +
      "const option = Array.from(this.options || []).find((o) => o.value === want || o.text === want); " +
      "if (!option) return false; this.value = option.value; " +
      "this.dispatchEvent(new Event('input', {bubbles: true})); " +
      "this.dispatchEvent(new Event('change', {bubbles: true})); return true; }", [value]);
    if (!done) throw new Error(`no option ${value} in that list`);
    return { result: `chose ${value}` };
  }

  /**
   * Open a page — or, when it is the page already open, read it again past
   * the cache. That second case is the one that matters here: a file was
   * edited, the server rebuilt, and what a plain navigation would show is
   * what was already shown.
   *
   * Either way this waits for the page to finish loading before answering,
   * so a screenshot taken on the next line is of the page and not of the
   * white it was about to paint over.
   *
   * @param {any} params
   */
  async function browserPanelNavigate(params) {
    const url = String((params && params.url) || "");
    if (!url) throw new Error("url is required");
    // Bring the view up on the way: the agent driving a browser nobody can
    // see is the thing every switch here exists to prevent.
    showBrowserView();
    const before = browserOpened ? await browserPanelStatus() : { url: "" };
    const again = Boolean(before.url) && (before.url === url || browserAddress(url) === before.url);
    browserSnapRefs.clear();
    if (again) {
      await browserCdp("Page.reload", { ignoreCache: true }, true);
    } else {
      await openBrowserAt(url);
    }
    const loaded = await browserPanelSettled();
    const after = await browserPanelStatus();
    return {
      result: `${again ? "reloaded" : "opened"} ${after.url}${loaded ? "" : " (still loading)"}`,
    };
  }

  /** Wait for the page to say it has finished loading. Bounded: a page that
   *  streams for ever must not hold the turn. */
  async function browserPanelSettled() {
    const until = Date.now() + 15000;
    for (;;) {
      const answer = await browserCdp("Runtime.evaluate", {
        expression: "document.readyState === 'complete'",
        returnByValue: true,
      }, true);
      if (answer && answer.result && answer.result.value === true) return true;
      if (Date.now() > until) return false;
      await new Promise((r) => setTimeout(r, 200));
    }
  }

  /**
   * Watch for a condition the core described. The conditions are data; the
   * script that checks them is ours, which is why this needs no more
   * permission than looking does.
   * @param {any} params
   */
  async function browserPanelWait(params) {
    const cond = {
      url: String((params && params.url) || ""),
      selector: String((params && params.selector) || ""),
      text: String((params && params.text) || ""),
    };
    const limit = Number((params && params.timeout_ms) || 5000);
    const until = Date.now() + Math.max(500, Math.min(limit, 120000));
    const check = `(() => { const c = ${JSON.stringify(cond)};` +
      " if (c.url && !location.href.includes(c.url)) return false;" +
      " if (c.selector && !document.querySelector(c.selector)) return false;" +
      " if (c.text && !(document.body && document.body.innerText.includes(c.text))) return false;" +
      " return true; })()";
    const said = [
      cond.url ? `a URL containing "${cond.url}"` : "",
      cond.selector ? `an element matching "${cond.selector}"` : "",
      cond.text ? `the text "${cond.text}"` : "",
    ].filter(Boolean).join(" and ");
    for (;;) {
      const answer = await browserCdp("Runtime.evaluate", { expression: check, returnByValue: true }, true);
      if (answer && answer.result && answer.result.value === true) {
        return { found: true, result: `found ${said}` };
      }
      if (Date.now() > until) {
        return { found: false, result: `waited and ${said} did not happen` };
      }
      await new Promise((r) => setTimeout(r, 250));
    }
  }

  /** @param {any} params */
  async function browserPanelEval(params) {
    const expression = String((params && params.expression) || "");
    if (!expression) throw new Error("expression is required");
    const answer = await browserCdp("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    }, true);
    if (answer && answer.exceptionDetails) {
      throw new Error(String(answer.exceptionDetails.text || "the script threw"));
    }
    const value = answer && answer.result ? answer.result.value : undefined;
    return { result: value === undefined ? "undefined" : JSON.stringify(value) };
  }

  /** Which ops each switch buys. Everything unlisted only looks. */
  const BROWSER_DRIVE_OPS = new Set(["navigate", "click", "type", "fill", "select"]);

  /**
   * One op from the core.
   * @param {string} op @param {any} params
   */
  async function browserPanelOp(op, params) {
    // What the turn was granted comes first, before whether there is a panel:
    // a permission is a property of the turn and does not change under it,
    // while a view can be opened mid-turn — and answering "open the view"
    // to an op that would still be refused afterwards sends the model after
    // the wrong thing. The core checks the same; this is the half that owns
    // the browser, and so the half that cannot be talked out of it.
    if (op === "eval" && !browserAgentMay.eval) {
      return { error: "this turn may not run script in the browser panel" };
    }
    if (BROWSER_DRIVE_OPS.has(op) && !browserAgentMay.drive) {
      return { error: "this turn may look at the browser panel but not act in it" };
    }
    // Navigating is how a first page arrives, so it needs the view, not a
    // page. Everything else needs something already loaded to act on.
    if (op === "navigate" ? !browserPanelAvailable() : !browserPanelOpen()) {
      return {
        error: browserPanelAvailable()
          ? "the browser panel has no page yet — browser.navigate to one first"
          : "this host has no browser view",
      };
    }
    browserAgentBusy(BROWSER_DRIVE_OPS.has(op) || op === "eval" ? "driving" : "reading");
    try {
      switch (op) {
        case "status":
          return await browserPanelStatus();
        case "snapshot":
          return await browserPanelSnapshot();
        case "screenshot":
          return await browserPanelScreenshot(params);
        case "wait":
          return await browserPanelWait(params);
        case "navigate":
          return await browserPanelNavigate(params);
        case "click":
          return await browserPanelClick(params);
        case "type":
          return await browserPanelType(params);
        case "fill":
          return await browserPanelFill(params);
        case "select":
          return await browserPanelSelect(params);
        case "eval":
          return await browserPanelEval(params);
        default:
          return { error: `the browser panel has no op "${op}"` };
      }
    } catch (err) {
      return { error: String((err && err.message) || err) };
    } finally {
      browserAgentBusy("");
    }
  }

  /* ---- what a pick becomes ------------------------------------------------ */

  /**
   * One picked element as the markdown that goes into the conversation: what
   * F12 shows for that node, and nothing else. The markup and the rules are
   * already trimmed by the picker — this only frames them.
   * @param {any} p
   */
  function pickMarkdown(p) {
    const lines = [];
    lines.push(`# Picked <${String(p.tag || "element")}>`);
    lines.push("");
    if (p.url) lines.push(`- page: ${p.url}`);
    if (p.title) lines.push(`- title: ${p.title}`);
    if (p.selector) lines.push(`- selector: \`${p.selector}\``);
    if (p.box) lines.push(`- box: ${p.box.w}×${p.box.h} at ${p.box.x},${p.box.y}`);
    lines.push("", "## Markup", "", "```html", String(p.markup || ""), "```");
    const css = Array.isArray(p.css) ? p.css.filter(Boolean) : [];
    if (css.length) {
      lines.push("", "## CSS that applies", "", "```css", css.join("\n"), "```");
    }
    return lines.join("\n") + "\n";
  }

  /** One element into the composer, from the eyedropper or from the tree. */
  async function attachPick(picked) {
    const name = pickFileName(picked);
    await storeAttachmentBytes({
      name,
      mime: "text/markdown",
      dataBase64: browserBase64(pickMarkdown(picked)),
    });
    const said = i18n("browser.picked", {
      tag: String(picked.tag || "element"),
      url: String(picked.url || ""),
      name,
    });
    browserStatus(said);
    toRenderer({ type: "systemNote", text: said });
  }

  /** UTF-8 text as base64, which is what attachments.store takes. */
  function browserBase64(text) {
    const bytes = new TextEncoder().encode(text);
    let binary = "";
    for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
  }

  function pickFileName(p) {
    const tag = String(p && p.tag ? p.tag : "element").replace(/[^a-z0-9-]/gi, "");
    const stamp = new Date().toTimeString().slice(0, 8).replace(/:/g, "");
    return `picked-${tag || "element"}-${stamp}.md`;
  }

  /* ---- screenshots -------------------------------------------------------- */

  /**
   * The page, or a part of it, as a PNG in the composer. The devtools protocol
   * returns it already base64 encoded, which is what attachments.store takes,
   * so nothing is decoded on the way.
   * @param {{x: number, y: number, w: number, h: number} | null} [clip]
   */
  async function browserShot(clip) {
    if (!browserOpened) return;
    const params = { format: "png", captureBeyondViewport: false };
    if (clip) {
      params.clip = { x: clip.x, y: clip.y, width: clip.w, height: clip.h, scale: 1 };
    }
    browserStatus(i18n("browser.shooting"));
    const answer = await browserCdp("Page.captureScreenshot", params);
    const data = answer && answer.data;
    if (!data) {
      browserStatus(i18n("browser.shot_failed"));
      return;
    }
    const stamp = new Date().toTimeString().slice(0, 8).replace(/:/g, "");
    const name = `page-${stamp}.png`;
    await storeAttachmentBytes({ name, mime: "image/png", dataBase64: String(data) });
    const said = i18n("browser.shot_taken", { name });
    browserStatus(said);
    toRenderer({ type: "systemNote", text: said });
  }

  /** Drag a rectangle over the page, then shoot exactly that. */
  async function browserShotArea() {
    if (!browserOpened) return;
    if ((await browserEval("window.__orchPick && __orchPick.armArea()")) === undefined) return;
    browserStatus(i18n("browser.area_hint"));
    const started = Date.now();
    while (Date.now() - started < 60000) {
      await new Promise((done) => setTimeout(done, 250));
      const answer = await browserEval("window.__orchPick ? __orchPick.takeArea() : 'idle'");
      if (answer === undefined || answer === "idle") {
        browserStatus("");
        return;
      }
      if (answer && typeof answer === "object") {
        browserStatus("");
        await browserShot(answer);
        return;
      }
    }
    browserStatus("");
  }

  /* ---- the inspector ------------------------------------------------------ */
  //
  // The browser's own developer tools — elements with the live DOM, console,
  // network, sources — docked beside the page instead of opening in the
  // window WebView2 puts them in.
  //
  // They are a page like any other: the browser process the panel runs in
  // serves their frontend, so the shell shows it in a webview of ours over
  // the dock's rectangle (browser_devtools). That process listens for the
  // devtools protocol on a loopback port, which is why the panel has a
  // profile of its own — the port drives every page of its environment, and
  // the page holding this app's capabilities is not in it.

  /**
   * Drag the line between the page and the tools. Both panes follow the line
   * as it moves — they are views of the operating system laid over this one,
   * and the pointer is captured on the press, so the moves keep arriving here
   * while the cursor runs ahead over them. The drag ends on the release, and
   * on anything that would eat the release: a cancelled pointer, the window
   * losing focus.
   *
   * @param {any} handle
   */
  function browserDragSplit(handle) {
    let from = 0;
    let width = 0;
    const move = (e) => {
      if (!browserDragging) return;
      setBrowserDockWidth(width + (from - e.clientX), browserBodyRoom());
      placeBrowser();
    };
    const stop = () => {
      if (!browserDragging) return;
      browserDragging = false;
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
      window.removeEventListener("blur", stop);
      setBrowserPref("orchestra.browser.dock", String(browserDockW));
      placeBrowser();
    };
    handle.addEventListener("pointerdown", (e) => {
      if (browserDragging) return;
      browserDragging = true;
      from = e.clientX;
      width = browserDockW;
      if (handle.setPointerCapture && e.pointerId !== undefined) {
        try { handle.setPointerCapture(e.pointerId); } catch (err) { /* a mouse that is gone */ }
      }
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", stop);
      window.addEventListener("pointercancel", stop);
      window.addEventListener("blur", stop);
      if (e.preventDefault) e.preventDefault();
    });
  }

  function browserDockToggle() {
    browserDockOpen = !browserDockOpen;
    if (browserDockEl) browserDockEl.hidden = !browserDockOpen;
    if (browserSplitEl) browserSplitEl.hidden = !browserDockOpen;
    if (browserDockBtn) {
      browserDockBtn.setAttribute("aria-pressed", browserDockOpen ? "true" : "false");
    }
    placeBrowser();
  }

  /* ---- the menu ----------------------------------------------------------- */

  /** @param {string} labelKey @param {() => void} onClick */
  function browserMenuItem(labelKey, onClick) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "browser-menu-item";
    b.textContent = i18n(labelKey);
    b.setAttribute("data-i18n", labelKey);
    b.addEventListener("click", () => {
      browserClosePopups();
      onClick();
    });
    return b;
  }

  function browserMenuSeparator() {
    const hr = document.createElement("div");
    hr.className = "browser-menu-sep";
    return hr;
  }

  /** A labelled row for the two choices and the zoom. */
  function browserMenuRow(labelKey) {
    const row = document.createElement("div");
    row.className = "browser-menu-row";
    const label = document.createElement("span");
    label.className = "browser-menu-label";
    label.textContent = i18n(labelKey);
    label.setAttribute("data-i18n", labelKey);
    row.appendChild(label);
    return row;
  }

  function browserSetZoom(scale) {
    browserZoom = Math.min(3, Math.max(0.25, scale));
    if (browserZoomEl) browserZoomEl.textContent = `${Math.round(browserZoom * 100)}%`;
    void browserInvoke("browser_zoom", { scale: browserZoom });
  }

  /** @param {number} step -1 or 1 */
  function browserStepZoom(step) {
    const i = BROWSER_ZOOMS.findIndex((z) => Math.abs(z - browserZoom) < 0.001);
    const from = i < 0 ? BROWSER_ZOOMS.indexOf(1) : i;
    const next = BROWSER_ZOOMS[Math.min(BROWSER_ZOOMS.length - 1, Math.max(0, from + step))];
    browserSetZoom(next);
  }

  async function browserCopyUrl() {
    const url = await browserEval("String(location.href)");
    if (typeof url !== "string" || !url) return;
    try {
      await navigator.clipboard.writeText(url);
      browserStatus(i18n("browser.copied"));
    } catch (e) {
      browserStatus(url);
    }
  }

  /** The chosen browser, or the system's own when nothing is chosen. */
  async function browserOpenExternal() {
    const url = (await browserEval("String(location.href)")) || (browserUrlEl && browserUrlEl.value);
    const address = browserAddress(String(url || ""));
    if (!address) return;
    const exe = browserPref("orchestra.browser.exe", "");
    if (await browserInvoke("browser_external", { url: address, exe })) {
      browserStatus(i18n("browser.opened_outside"));
    }
  }

  /** Everything the page being viewed has stored, and nothing of ours. */
  async function browserClearSite() {
    const origin = await browserEval("String(location.origin)");
    if (typeof origin !== "string" || !origin || origin === "null") return;
    await browserCdp("Storage.clearDataForOrigin", { origin, storageTypes: "all" });
    browserStatus(i18n("browser.cleared"));
  }

  function buildBrowserMenu() {
    const menu = document.createElement("div");
    menu.className = "browser-menu";
    menu.setAttribute("role", "menu");
    menu.hidden = true;

    menu.append(
      browserMenuItem("browser.shot", () => void browserShot(null)),
      browserMenuItem("browser.shot_area", () => void browserShotArea()),
      browserMenuSeparator(),
      browserMenuItem("browser.hard_reload", () => {
        void browserCdp("Page.reload", { ignoreCache: true });
      }),
      browserMenuItem("browser.copy_url", () => void browserCopyUrl()),
      browserMenuItem("browser.open_outside", () => void browserOpenExternal()),
      browserMenuSeparator()
    );

    // Zoom, the row every browser's menu has.
    const zoom = browserMenuRow("browser.zoom");
    const minus = document.createElement("button");
    minus.type = "button";
    minus.className = "browser-menu-step";
    minus.textContent = "−";
    minus.addEventListener("click", () => browserStepZoom(-1));
    browserZoomEl = document.createElement("span");
    browserZoomEl.className = "browser-menu-value";
    browserZoomEl.textContent = "100%";
    const plus = document.createElement("button");
    plus.type = "button";
    plus.className = "browser-menu-step";
    plus.textContent = "+";
    plus.addEventListener("click", () => browserStepZoom(1));
    const reset = document.createElement("button");
    reset.type = "button";
    reset.className = "browser-menu-step";
    reset.textContent = "↺";
    reset.title = i18n("browser.zoom_reset");
    reset.setAttribute("data-i18n-title", "browser.zoom_reset");
    reset.addEventListener("click", () => browserSetZoom(1));
    zoom.append(minus, browserZoomEl, plus, reset);
    menu.appendChild(zoom);

    // Which engine the bar searches with.
    const engine = browserMenuRow("browser.engine");
    browserEngineEl = document.createElement("select");
    browserEngineEl.className = "browser-menu-select";
    for (const e of BROWSER_ENGINES) {
      const opt = document.createElement("option");
      opt.value = e.id;
      opt.textContent = e.name;
      browserEngineEl.appendChild(opt);
    }
    browserEngineEl.value = browserEngine().id;
    browserEngineEl.addEventListener("change", () => {
      setBrowserPref("orchestra.browser.engine", browserEngineEl.value);
    });
    engine.appendChild(browserEngineEl);
    menu.appendChild(engine);

    // Which browser "open outside" means. The panel itself is always the one
    // the system embeds — Windows lets an app embed WebView2 and nothing else
    // — so the choice is about where a page leaves for, not what draws it.
    const app = browserMenuRow("browser.app");
    browserAppEl = document.createElement("select");
    browserAppEl.className = "browser-menu-select";
    browserAppEl.addEventListener("change", () => {
      setBrowserPref("orchestra.browser.exe", browserAppEl.value);
    });
    app.appendChild(browserAppEl);
    menu.appendChild(app);
    fillBrowserApps();

    menu.append(
      browserMenuSeparator(),
      browserMenuItem("browser.clear_cookies", () => {
        void browserCdp("Network.clearBrowserCookies", {});
        browserStatus(i18n("browser.cleared"));
      }),
      browserMenuItem("browser.clear_cache", () => {
        void browserCdp("Network.clearBrowserCache", {});
        browserStatus(i18n("browser.cleared"));
      }),
      // The site's own storage, not the profile's: the panel shares a
      // WebView2 profile with this page, and clearing all of it would throw
      // away the app's own theme, language and these very choices.
      browserMenuItem("browser.clear_site", () => void browserClearSite())
    );
    return menu;
  }

  /** The browsers the machine has, asked for once. */
  async function fillBrowserApps() {
    const t = browserBridge();
    if (!t || !browserAppEl) return;
    try {
      browserInstalled = (await t.core.invoke("browser_installed", {})) || [];
    } catch (e) {
      browserInstalled = [];
    }
    while (browserAppEl.firstChild) browserAppEl.removeChild(browserAppEl.firstChild);
    const first = document.createElement("option");
    first.value = "";
    first.textContent = i18n("browser.app_default");
    first.setAttribute("data-i18n", "browser.app_default");
    browserAppEl.appendChild(first);
    for (const b of browserInstalled) {
      const opt = document.createElement("option");
      opt.value = String(b.exe || "");
      opt.textContent = String(b.name || b.exe || "");
      browserAppEl.appendChild(opt);
    }
    const chosen = browserPref("orchestra.browser.exe", "");
    browserAppEl.value = browserInstalled.some((b) => b.exe === chosen) ? chosen : "";
  }

  /* ---- the toolbar's popups ------------------------------------------- */

  /** @type {any[]} */ const browserPopups = [];

  /**
   * A popup under a toolbar button. One is open at a time, and a click
   * anywhere else closes it.
   * @param {any} wrap @param {any} btn @param {any} menu @param {() => void} [onOpen]
   */
  function browserPopup(wrap, btn, menu, onOpen) {
    btn.setAttribute("aria-haspopup", "menu");
    btn.setAttribute("aria-expanded", "false");
    browserPopups.push({ wrap, btn, menu, onOpen });
    btn.addEventListener("click", () => browserOpenPopup(menu, Boolean(menu.hidden)));
    document.addEventListener("click", (ev) => {
      if (menu.hidden) return;
      const target = ev && ev.target;
      if (wrap.contains && target && wrap.contains(target)) return;
      browserOpenPopup(menu, false);
    });
  }

  /** @param {any} menu the one to show, or null to close them all @param {boolean} on */
  async function browserOpenPopup(menu, on) {
    // Opening over the page: its picture is taken before it is covered.
    if (menu && on && browserWouldCover(menu)) await browserCover();
    for (const p of browserPopups) {
      const show = p.menu === menu && on;
      p.menu.hidden = !show;
      p.btn.setAttribute("aria-expanded", show ? "true" : "false");
      if (show && p.onOpen) p.onOpen();
    }
    placeBrowser();
  }

  function browserClosePopups() {
    browserOpenPopup(null, false);
    hideBrowserSuggest();
  }

  /* ---- what the bar offers ---------------------------------------------- */

  function hideBrowserSuggest() {
    browserSuggested = [];
    browserSuggestAt = -1;
    const was = browserSuggestEl && !browserSuggestEl.hidden;
    if (browserSuggestEl) browserSuggestEl.hidden = true;
    if (browserUrlEl && browserUrlEl.setAttribute) {
      browserUrlEl.setAttribute("aria-expanded", "false");
    }
    if (was) placeBrowser();
  }

  /** Which row is under the keyboard right now. */
  function markBrowserSuggest() {
    if (!browserSuggestEl || !browserSuggestEl.childNodes) return;
    const rows = browserSuggestEl.childNodes;
    for (let i = 0; i < rows.length; i++) {
      const on = i === browserSuggestAt;
      if (rows[i].setAttribute) rows[i].setAttribute("aria-selected", on ? "true" : "false");
    }
  }

  /** @param {{url: string, title: string}} row */
  function browserSuggestRow(row) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "browser-menu-item browser-suggest-row";
    b.setAttribute("role", "option");
    b.setAttribute("aria-selected", "false");
    const title = document.createElement("span");
    title.className = "browser-suggest-title";
    title.textContent = row.title;
    b.appendChild(title);
    if (row.url !== row.title) {
      const url = document.createElement("span");
      url.className = "browser-suggest-url";
      url.textContent = row.url;
      b.appendChild(url);
    }
    // mousedown, not click: the bar must not lose focus before we are told.
    b.addEventListener("mousedown", (ev) => {
      if (ev && ev.preventDefault) ev.preventDefault();
      hideBrowserSuggest();
      void openBrowserAt(row.url);
    });
    return b;
  }

  /** The list under the bar, rebuilt for what has been typed so far. */
  function drawBrowserSuggest() {
    if (!browserSuggestEl || !browserUrlEl) return;
    const typed = String(browserUrlEl.value || "");
    const rows = browserSuggest(typed, browserLinks(), browserHistory());
    // What the bar would do with the text itself, offered first when that is
    // a search rather than an address.
    const engine = browserEngine();
    const asSearch = typed.trim() && browserAddress(typed).indexOf(engine.search) === 0
      ? [{ url: browserAddress(typed), title: i18n("browser.suggest_search", { q: typed.trim(), engine: engine.name }) }]
      : [];
    browserSuggested = asSearch.concat(rows).slice(0, MAX_SUGGEST + 1);
    while (browserSuggestEl.firstChild) browserSuggestEl.removeChild(browserSuggestEl.firstChild);
    for (const row of browserSuggested) browserSuggestEl.appendChild(browserSuggestRow(row));
    browserSuggestAt = -1;
    const was = !browserSuggestEl.hidden;
    const show = browserSuggested.length > 0;
    const reveal = () => {
      browserSuggestEl.hidden = !show;
      if (browserUrlEl.setAttribute) {
        browserUrlEl.setAttribute("aria-expanded", show ? "true" : "false");
      }
      if (was !== show) placeBrowser();
    };
    // Opening over the page: its picture is taken before it is covered.
    if (show && !was && browserWouldCover(browserSuggestEl)) {
      void browserCover().then(() => {
        if (browserSuggested.length) reveal();
      });
      return;
    }
    reveal();
  }

  /** @param {number} step */
  function moveBrowserSuggest(step) {
    if (!browserSuggested.length) return;
    const last = browserSuggested.length - 1;
    browserSuggestAt =
      browserSuggestAt + step < -1 ? last : browserSuggestAt + step > last ? -1 : browserSuggestAt + step;
    markBrowserSuggest();
  }

  /* ---- the links menu --------------------------------------------------- */

  /** The saved links, drawn fresh: they change while the menu is closed. */
  function drawBrowserLinks() {
    if (!browserLinksEl) return;
    while (browserLinksEl.firstChild) browserLinksEl.removeChild(browserLinksEl.firstChild);
    browserLinksEl.appendChild(browserMenuItem("browser.link_save", () => void saveBrowserLink()));
    browserLinksEl.appendChild(browserMenuSeparator());
    const rows = browserLinks();
    if (!rows.length) {
      const empty = document.createElement("div");
      empty.className = "browser-menu-row browser-menu-label";
      empty.textContent = i18n("browser.links_empty");
      empty.setAttribute("data-i18n", "browser.links_empty");
      browserLinksEl.appendChild(empty);
      return;
    }
    for (const row of rows) {
      const line = document.createElement("div");
      line.className = "browser-link-row";
      const open = document.createElement("button");
      open.type = "button";
      open.className = "browser-menu-item browser-link";
      open.textContent = String(row.title || row.url);
      open.title = String(row.url);
      open.addEventListener("click", () => {
        browserClosePopups();
        void openBrowserAt(String(row.url));
      });
      const drop = document.createElement("button");
      drop.type = "button";
      drop.className = "browser-link-x";
      drop.textContent = "×";
      drop.title = i18n("browser.link_forget");
      drop.setAttribute("aria-label", i18n("browser.link_forget"));
      drop.setAttribute("data-i18n-title", "browser.link_forget");
      drop.setAttribute("data-i18n-aria-label", "browser.link_forget");
      drop.addEventListener("click", () => forgetBrowserLink(String(row.url)));
      line.append(open, drop);
      browserLinksEl.appendChild(line);
    }
  }

  /* ---- the view ----------------------------------------------------------- */

  /**
   * One icon button of the toolbar, in the composer's own button style.
   * @param {string} icon a name in media/icons.js
   * @param {string} titleKey @param {() => void} onClick
   */
  function browserToolButton(icon, titleKey, onClick) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "icon-btn browser-tool";
    // A fixed string, none of it from data.
    b.innerHTML = orchIconMarkup(icon, { size: "md" });
    b.title = i18n(titleKey);
    b.setAttribute("aria-label", i18n(titleKey));
    b.setAttribute("data-i18n-title", titleKey);
    b.setAttribute("data-i18n-aria-label", titleKey);
    b.addEventListener("click", onClick);
    return b;
  }

  function ensureBrowserView() {
    if (browserBtn || !browserApp || !browserSwitchEl || !browserSwitchEl.appendChild) {
      return;
    }
    browserBtn = document.createElement("button");
    browserBtn.type = "button";
    browserBtn.className = "view-segment";
    browserBtn.setAttribute("role", "tab");
    browserBtn.setAttribute("aria-selected", "false");
    // A fixed string, none of it from data.
    browserBtn.innerHTML = orchIconMarkup("access-browser", { size: "md" }) + escapeHtml(i18n("browser.title"));
    browserBtn.addEventListener("click", () => showBrowserView());
    browserSwitchEl.appendChild(browserBtn);

    browserPane = document.createElement("div");
    browserPane.className = "browser-pane";
    browserPane.setAttribute("role", "tabpanel");
    browserPane.setAttribute("aria-label", i18n("browser.pane_aria"));

    const toolbar = document.createElement("div");
    toolbar.className = "browser-toolbar";

    toolbar.append(
      browserToolButton("arrow-left", "browser.back", () => void browserInvoke("browser_navigate", { action: "back" })),
      browserToolButton("arrow-right", "browser.forward", () => void browserInvoke("browser_navigate", { action: "forward" })),
      browserToolButton("reload", "browser.reload", () => void browserInvoke("browser_navigate", { action: "reload" }))
    );

    // The pages worth coming back to, beside the button that reloads this one.
    const linksWrap = document.createElement("div");
    linksWrap.className = "browser-menu-wrap";
    browserLinksBtn = browserToolButton("bookmark", "browser.links", () => {});
    browserLinksEl = document.createElement("div");
    browserLinksEl.className = "browser-menu browser-menu-left";
    browserLinksEl.setAttribute("role", "menu");
    browserLinksEl.hidden = true;
    linksWrap.append(browserLinksBtn, browserLinksEl);
    toolbar.appendChild(linksWrap);
    browserPopup(linksWrap, browserLinksBtn, browserLinksEl, () => drawBrowserLinks());

    const urlWrap = document.createElement("div");
    urlWrap.className = "browser-url-wrap";
    browserUrlEl = document.createElement("input");
    browserUrlEl.type = "text";
    browserUrlEl.className = "browser-url";
    browserUrlEl.placeholder = i18n("browser.url_hint");
    browserUrlEl.setAttribute("data-i18n-placeholder", "browser.url_hint");
    browserUrlEl.setAttribute("role", "combobox");
    browserUrlEl.setAttribute("aria-expanded", "false");
    browserUrlEl.setAttribute("aria-autocomplete", "list");
    browserUrlEl.addEventListener("keydown", (ev) => {
      if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
        ev.preventDefault();
        if (browserSuggestEl && browserSuggestEl.hidden) drawBrowserSuggest();
        else moveBrowserSuggest(ev.key === "ArrowDown" ? 1 : -1);
        return;
      }
      if (ev.key === "Escape") {
        hideBrowserSuggest();
        return;
      }
      if (ev.key !== "Enter") return;
      ev.preventDefault();
      const chosen = browserSuggested[browserSuggestAt];
      hideBrowserSuggest();
      void openBrowserAt(chosen ? chosen.url : browserUrlEl.value);
    });
    browserUrlEl.addEventListener("input", () => drawBrowserSuggest());
    browserUrlEl.addEventListener("focus", () => drawBrowserSuggest());
    browserUrlEl.addEventListener("blur", () => setTimeout(() => hideBrowserSuggest(), 120));
    // A click anywhere else closes the list too — blur alone is not enough
    // when focus never arrived, and the list hides the page while it is up.
    document.addEventListener("click", (ev) => {
      if (!browserSuggestEl || browserSuggestEl.hidden) return;
      const target = ev && ev.target;
      if (urlWrap.contains && target && urlWrap.contains(target)) return;
      hideBrowserSuggest();
    });
    browserSuggestEl = document.createElement("div");
    browserSuggestEl.className = "browser-menu browser-suggest";
    browserSuggestEl.setAttribute("role", "listbox");
    browserSuggestEl.hidden = true;
    urlWrap.append(browserUrlEl, browserSuggestEl);
    toolbar.appendChild(urlWrap);

    browserStatusEl = document.createElement("span");
    browserStatusEl.className = "browser-status";
    browserStatusEl.hidden = true;
    toolbar.appendChild(browserStatusEl);

    browserPickBtn = browserToolButton("pick", "browser.pick", () => void toggleBrowserPick());
    toolbar.appendChild(browserPickBtn);
    browserDockBtn = browserToolButton("terminal", "browser.console", () => browserDockToggle());
    browserDockBtn.setAttribute("aria-pressed", "false");
    toolbar.appendChild(browserDockBtn);

    const menuWrap = document.createElement("div");
    menuWrap.className = "browser-menu-wrap";
    browserMenuBtn = browserToolButton("dots", "browser.more", () => {});
    browserMenuEl = buildBrowserMenu();
    menuWrap.append(browserMenuBtn, browserMenuEl);
    toolbar.appendChild(menuWrap);
    browserPopup(menuWrap, browserMenuBtn, browserMenuEl);

    browserStage = document.createElement("div");
    browserStage.className = "browser-stage";

    // The inspector sits beside the page, not over it: both are OS views of
    // their own, and the stage shrinks to make room.
    browserDockEl = document.createElement("div");
    browserDockEl.className = "browser-dock";
    browserDockEl.hidden = true;
    browserDockW = Number(browserPref("orchestra.browser.dock", "")) || BROWSER_DOCK_W;
    browserDockEl.style.flexBasis = `${browserDockW}px`;

    browserSplitEl = document.createElement("div");
    browserSplitEl.className = "browser-split";
    browserSplitEl.hidden = true;
    browserDragSplit(browserSplitEl);

    const body = document.createElement("div");
    body.className = "browser-body";
    body.append(browserStage, browserSplitEl, browserDockEl);

    browserPane.append(toolbar, body);
    if (browserTrajectoryPane && browserTrajectoryPane.parentNode === browserApp && browserApp.insertBefore) {
      browserApp.insertBefore(browserPane, browserTrajectoryPane.nextSibling);
    } else {
      browserApp.appendChild(browserPane);
    }

    if (typeof MutationObserver === "function" && browserApp.dataset) {
      new MutationObserver(() => syncBrowserSegment()).observe(browserApp, {
        attributes: true,
        attributeFilter: ["data-view"],
      });
    }
    if (typeof ResizeObserver === "function") {
      new ResizeObserver(() => placeBrowser()).observe(browserStage);
    }
    window.addEventListener("resize", () => placeBrowser());
    // The tools are painted in this app's colours, so they follow a change of
    // theme — the one on the root element, and the system's own where the
    // user has left that to the system.
    if (typeof MutationObserver === "function" && document.documentElement) {
      new MutationObserver(() => placeBrowser()).observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-theme"],
      });
    }
    if (typeof window.matchMedia === "function") {
      const scheme = window.matchMedia("(prefers-color-scheme: dark)");
      if (scheme && scheme.addEventListener) {
        scheme.addEventListener("change", () => placeBrowser());
      }
    }
  }

  function showBrowserView() {
    ensureBrowserView();
    if (!browserApp || !browserApp.dataset) return;
    browserApp.dataset.view = "browser";
    syncBrowserSegment();
  }

  /** One selected segment, whichever side stamped the view. */
  function syncBrowserSegment() {
    const active = browserViewActive();
    if (browserBtn && browserBtn.setAttribute) {
      browserBtn.setAttribute("aria-selected", active ? "true" : "false");
    }
    if (active) {
      for (const b of [browserChatBtn, browserTrajectoryBtn]) {
        if (b && b.setAttribute) b.setAttribute("aria-selected", "false");
      }
      placeBrowser();
      return;
    }
    // Left the view: the webview goes out of sight but stays loaded, so
    // coming back does not reload the page under it.
    browserClosePopups();
    if (browserOpened) {
      browserPickArmed = false;
      syncBrowserPickButton();
      void browserInvoke("browser_hide", {});
    }
  }

  function syncBrowserPickButton() {
    if (!browserPickBtn || !browserPickBtn.setAttribute) return;
    browserPickBtn.setAttribute("aria-pressed", browserPickArmed ? "true" : "false");
  }

  async function toggleBrowserPick() {
    if (!browserOpened) {
      await openBrowserAt(browserUrlEl && browserUrlEl.value);
      if (!browserOpened) return;
    }
    const next = !browserPickArmed;
    if (!(await browserInvoke("browser_pick", { on: next }))) return;
    browserPickArmed = next;
    syncBrowserPickButton();
    browserStatus(next ? i18n("browser.picking") : "");
  }

  (function initBrowserPanel() {
    const t = browserBridge();
    if (!t || !browserApp || !browserSwitchEl) {
      return;
    }
    ensureBrowserView();

    // The pick arrives as text from a page we do not control: it becomes an
    // attachment the user still has to send, and is never read as a command.
    if (t.event && typeof t.event.listen === "function") {
      void t.event.listen("orchestra://pick", async (ev) => {
        const raw = ev && ev.payload && ev.payload.payload;
        browserPickArmed = false;
        syncBrowserPickButton();
        if (!raw) return;
        let parsed;
        try {
          parsed = JSON.parse(String(raw));
        } catch (err) {
          return;
        }
        if (!parsed || parsed.error) return;
        await attachPick(parsed);
      });

      // The bar follows the page, unless the user is typing in it.
      void t.event.listen("orchestra://browser-url", (ev) => {
        const url = ev && ev.payload && ev.payload.url;
        if (!url || !browserUrlEl || document.activeElement === browserUrlEl) return;
        if (String(url).startsWith("about:")) return;
        browserUrlEl.value = String(url);
        rememberBrowserUrl(String(url));
      });
    }
  })();
