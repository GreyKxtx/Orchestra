  // ---- the turn rail ----------------------------------------------------------
  //
  // One short bar per message of the person's own, down the right edge of
  // the transcript, in place of the scrollbar (rail.css hides that). Each bar
  // sits where its message is in the scroll, the one for the message in view
  // is lit, and a click scrolls to it: a long conversation is navigated by
  // its questions rather than by dragging a thumb. Built at runtime, outside
  // the shared markup, like the graph view — the editor's webview keeps its
  // scrollbar.

  const railMessagesEl = document.getElementById("messages");
  const railAppEl = document.getElementById("app");
  /** @type {any} */
  let turnRailEl = null;
  /** @type {{el: any, tick: any}[]} */
  let turnRailItems = [];
  let turnRailQueued = false;

  function ensureTurnRail() {
    if (turnRailEl || !railAppEl || !railAppEl.appendChild) {
      return;
    }
    turnRailEl = document.createElement("div");
    turnRailEl.className = "turn-rail";
    turnRailEl.setAttribute("aria-hidden", "true");
    turnRailEl.hidden = true;
    railAppEl.appendChild(turnRailEl);
  }

  function scheduleTurnRail() {
    if (turnRailQueued) return;
    turnRailQueued = true;
    requestAnimationFrame(() => {
      turnRailQueued = false;
      rebuildTurnRail();
    });
  }

  /** The transcript's own text of a user message, for the bar's tooltip. */
  function turnRailLabel(el) {
    const body = el.querySelector ? el.querySelector(".user-text") : null;
    const text = String((body || el).textContent || "")
      .replace(/\s+/g, " ")
      .trim();
    return text.length > 90 ? text.slice(0, 87) + "…" : text;
  }

  function rebuildTurnRail() {
    ensureTurnRail();
    if (!turnRailEl || !railMessagesEl || !railMessagesEl.querySelectorAll) {
      return;
    }
    const users = Array.from(railMessagesEl.querySelectorAll(".msg.user"));
    const chatView = !railAppEl.dataset || !railAppEl.dataset.view || railAppEl.dataset.view === "chat";
    const show = users.length > 0 && chatView && !railMessagesEl.hidden;
    turnRailEl.hidden = !show;
    turnRailItems = [];
    if (!show) {
      return;
    }
    if (!railMessagesEl.getBoundingClientRect || !railAppEl.getBoundingClientRect) {
      return;
    }
    const appRect = railAppEl.getBoundingClientRect();
    const box = railMessagesEl.getBoundingClientRect();
    turnRailEl.style.top = Math.round(box.top - appRect.top + 8) + "px";
    turnRailEl.style.height = Math.max(0, Math.round(box.height - 16)) + "px";
    const total = Math.max(railMessagesEl.scrollHeight || box.height, 1);
    turnRailEl.innerHTML = "";
    for (const el of users) {
      const r = el.getBoundingClientRect();
      const offset = r.top - box.top + (railMessagesEl.scrollTop || 0);
      const tick = document.createElement("button");
      tick.type = "button";
      tick.className = "turn-tick";
      const label = turnRailLabel(el);
      tick.title = label;
      tick.setAttribute("aria-label", label || "message");
      tick.style.top = Math.min(100, Math.max(0, (offset / total) * 100)).toFixed(2) + "%";
      tick.addEventListener("click", () => {
        const at = el.getBoundingClientRect().top - railMessagesEl.getBoundingClientRect().top + (railMessagesEl.scrollTop || 0);
        if (railMessagesEl.scrollTo) {
          railMessagesEl.scrollTo({ top: Math.max(0, at - 12), behavior: "smooth" });
        } else {
          railMessagesEl.scrollTop = Math.max(0, at - 12);
        }
      });
      turnRailEl.appendChild(tick);
      turnRailItems.push({ el, tick });
    }
    updateTurnRailCurrent();
  }

  /** Light the bar of the last question that has scrolled into the upper part of the view. */
  function updateTurnRailCurrent() {
    if (!turnRailItems.length || !railMessagesEl.getBoundingClientRect) return;
    const box = railMessagesEl.getBoundingClientRect();
    const line = box.top + box.height * 0.4;
    let current = null;
    for (const item of turnRailItems) {
      if (item.el.getBoundingClientRect().top <= line) current = item;
    }
    if (!current) current = turnRailItems[0];
    for (const item of turnRailItems) {
      item.tick.classList.toggle("current", item === current);
    }
  }

  if (railMessagesEl && railMessagesEl.addEventListener) {
    railMessagesEl.addEventListener("scroll", () => updateTurnRailCurrent(), { passive: true });
  }
  if (railMessagesEl && typeof MutationObserver === "function") {
    new MutationObserver(() => scheduleTurnRail()).observe(railMessagesEl, { childList: true });
  }
  if (railMessagesEl && typeof ResizeObserver === "function") {
    new ResizeObserver(() => scheduleTurnRail()).observe(railMessagesEl);
  }
  if (railAppEl && typeof MutationObserver === "function") {
    new MutationObserver(() => scheduleTurnRail()).observe(railAppEl, {
      attributes: true,
      attributeFilter: ["data-view"],
    });
  }
  // The transcript changes wholesale on these; the observer above sees the
  // children, this sees the moment.
  window.addEventListener("message", (ev) => {
    const t = ev.data && ev.data.type;
    if (t === "clearMessages" || t === "history" || t === "userEcho") {
      scheduleTurnRail();
    }
  });
