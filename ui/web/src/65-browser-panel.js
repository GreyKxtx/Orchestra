  /* ---- the browser panel and its element picker --------------------------- */
  //
  // Desktop only: the panel is a second Tauri webview (ui/desktop/src-tauri/
  // src/browser.rs) with the picker injected into every page it opens. This
  // side is the chrome — an address, Go, and the eyedropper — plus the one
  // thing the feature exists for: turning a picked element into an attachment
  // on the next message.
  //
  // In a plain browser there is no panel, so the button removes itself rather
  // than offering something that cannot work.

  /** The Tauri bridge, or null when the page is not running in the shell. */
  function tauriBridge() {
    const t = window.__TAURI__;
    return t && t.core && typeof t.core.invoke === "function" ? t : null;
  }

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

  /** UTF-8 text as base64, which is what attachments.store takes. */
  function base64Utf8(text) {
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

  (function initBrowserPanel() {
    const openBtn = document.getElementById("browser-btn");
    const bar = document.getElementById("browser-bar");
    const urlInput = /** @type {HTMLInputElement|null} */ (document.getElementById("browser-url"));
    const goBtn = document.getElementById("browser-go");
    const pickBtn = document.getElementById("browser-pick");
    if (!openBtn || !bar || !urlInput || !goBtn || !pickBtn) {
      return;
    }
    const t = tauriBridge();
    if (!t) {
      openBtn.remove();
      bar.remove();
      return;
    }
    openBtn.hidden = false;

    let opened = false;

    function note(key, vars) {
      toRenderer({ type: "systemNote", text: i18n(key, vars) });
    }

    async function invoke(command, args) {
      try {
        await t.core.invoke(command, args || {});
        return true;
      } catch (err) {
        toRenderer({
          type: "systemNote",
          text: `[error] browser: ${String((err && err.message) || err)}`,
        });
        return false;
      }
    }

    async function openPanel() {
      const url = urlInput.value.trim();
      if (!url) {
        urlInput.focus();
        return;
      }
      opened = await invoke("browser_open", { url });
    }

    openBtn.addEventListener("click", () => {
      bar.hidden = !bar.hidden;
      if (!bar.hidden) urlInput.focus();
    });
    goBtn.addEventListener("click", () => void openPanel());
    urlInput.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        void openPanel();
      }
    });
    pickBtn.addEventListener("click", async () => {
      if (!opened) {
        await openPanel();
        if (!opened) return;
      }
      if (await invoke("browser_pick", { on: true })) {
        note("browser.picking");
      }
    });

    // The pick arrives as text from a page we do not control: it becomes an
    // attachment the user still has to send, and is never read as a command.
    if (t.event && typeof t.event.listen === "function") {
      void t.event.listen("orchestra://pick", async (ev) => {
        const raw = ev && ev.payload && ev.payload.payload;
        if (!raw) return;
        let parsed;
        try {
          parsed = JSON.parse(String(raw));
        } catch (err) {
          return;
        }
        if (!parsed || parsed.error) return;
        const name = pickFileName(parsed);
        await storeAttachmentBytes({
          name,
          mime: "text/markdown",
          dataBase64: base64Utf8(pickMarkdown(parsed)),
        });
        note("browser.picked", {
          tag: String(parsed.tag || "element"),
          url: String(parsed.url || ""),
          name,
        });
      });
    }
  })();
