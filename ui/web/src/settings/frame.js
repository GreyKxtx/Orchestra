  // The settings panel's host, on the web.
  //
  // In VS Code the panel is its own webview and reaches the extension through
  // acquireVsCodeApi(). Here the very same panel is an iframe inside the chat
  // page and the other end is ui/web/src/50-settings.js in the parent document
  // — the message protocol is identical, only the pipe differs, so every
  // fragment under ui/vscode/media/settings-src is used unchanged.
  //
  // An iframe rather than an inlined panel because settings.css declares its
  // own :root palette under the same token names chat.css uses: inlined, it
  // would repaint the whole app. A separate document also keeps its 2400 lines
  // of script out of the chat bundle's one shared scope.
  //
  // bundle-settings-web.mjs strips 01-core.js's own `const vscode = ...` line,
  // so this binding stands in for it.

  /** The parent is same-origin; naming it beats posting to "*". */
  const PARENT_ORIGIN = window.location.origin;

  const vscode = {
    /** @param {any} msg */
    postMessage(msg) {
      try {
        window.parent.postMessage(msg, PARENT_ORIGIN);
      } catch (e) {
        // A detached frame is not worth a broken page.
      }
    },
    /** The panel never reads it back; VS Code's own is a per-webview cache. */
    getState() {
      return undefined;
    },
    setState() {
      return undefined;
    },
  };

  // getHtml injects these in the webview; here they are files beside this one.
  // The catalogue arrives with the state message instead — 05-state.js prefers
  // msg.mcpCatalog and only falls back to the global.
  if (!window.__ORCH_ICON_BASE) {
    window.__ORCH_ICON_BASE = "provider-icons/";
  }
  if (!window.__ORCH_ICON_V) {
    window.__ORCH_ICON_V = "web";
  }

  // ---- Appearance, which exists only on this host -------------------------
  //
  // VS Code panels follow the editor's theme, so the shared markup has no such
  // section; bundle-settings-web.mjs adds one to this page. Navigation between
  // tabs is generic over [data-section] in 01-core.js and needs nothing here —
  // only the three choices do.

  /** @returns {string} "system" | "light" | "dark" */
  function savedFrameTheme() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.theme") : "";
      return v === "light" || v === "dark" ? v : "system";
    } catch (e) {
      return "system";
    }
  }

  /** @param {string} choice */
  function syncFrameTheme(choice) {
    const root = document.documentElement;
    if (choice === "light" || choice === "dark") {
      root.setAttribute("data-theme", choice);
    } else {
      root.removeAttribute("data-theme");
    }
    document.querySelectorAll("[data-theme-choice]").forEach((el) => {
      el.setAttribute("aria-checked", el.getAttribute("data-theme-choice") === choice ? "true" : "false");
    });
  }

  document.addEventListener("click", (ev) => {
    const item = ev.target && ev.target.closest ? ev.target.closest("[data-theme-choice]") : null;
    if (!item) {
      return;
    }
    const choice = item.getAttribute("data-theme-choice") || "system";
    syncFrameTheme(choice);
    // The parent owns the stored value and the chat page's own stamp; it
    // echoes the change back so both documents always agree.
    vscode.postMessage({ type: "setTheme", theme: choice });
  });

  window.addEventListener("message", (event) => {
    const msg = event && event.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    if (msg.type === "theme") {
      syncFrameTheme(msg.theme === "light" || msg.theme === "dark" ? msg.theme : "system");
    }
  });

  syncFrameTheme(savedFrameTheme());
