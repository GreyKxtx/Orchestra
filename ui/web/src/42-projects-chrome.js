  // ---- input, appearance and the sidebar's width -----------------------------
  //
  // Split out of 40-projects.js, which had grown to 2035 lines carrying four
  // jobs behind comment banners. The bundler concatenates these fragments
  // numerically into one IIFE, so everything 40 declares is in scope here and
  // the startup block still runs last.

  // ---- input -------------------------------------------------------------

  /**
   * The scale chat.css puts on :root above 2600px, or 1. Read back rather
   * than duplicated here so the breakpoints stay in one file.
   */
  function rootZoom() {
    try {
      const z = parseFloat(window.getComputedStyle(document.documentElement).zoom);
      return isFinite(z) && z > 0 ? z : 1;
    } catch (e) {
      // No getComputedStyle (the adapter tests' stub) means no zoom either.
      return 1;
    }
  }

  const railMenu = (() => {
    const el = document.createElement("div");
    el.className = "project-menu";
    el.hidden = true;
    if (document.body && document.body.appendChild) {
      document.body.appendChild(el);
    }
    return el;
  })();

  /** @param {string} projectId @param {number} x @param {number} y */
  function openRailMenu(projectId, x, y) {
    railMenu.innerHTML = "";
    const entry = known.find((p) => p.id === projectId);
    if (entry && entry.state !== "closed") {
      const close = document.createElement("button");
      close.type = "button";
      close.textContent = "Close project";
      close.addEventListener("click", () => {
        railMenu.hidden = true;
        void closeProject(projectId);
      });
      railMenu.appendChild(close);
    }
    const forget = document.createElement("button");
    forget.type = "button";
    forget.textContent = "Remove from list";
    forget.addEventListener("click", () => {
      railMenu.hidden = true;
      void forgetProject(projectId);
    });
    railMenu.appendChild(forget);
    // clientX/clientY are viewport pixels, but chat.css zooms :root on very
    // wide displays and this menu is positioned inside that zoom — so the
    // same number means a different place. Divide it back out.
    const z = rootZoom();
    railMenu.style.left = x / z + "px";
    railMenu.style.top = y / z + "px";
    railMenu.hidden = false;
  }

  const railEl = document.getElementById("project-rail-list");
  if (railEl && railEl.addEventListener) {
    railEl.addEventListener("click", (ev) => {
      // The two add buttons, which renderProjects writes onto group headings.
      // First, because they sit inside a heading that is itself clickable.
      const add = ev.target && ev.target.closest ? ev.target.closest("[data-rail-action]") : null;
      if (add) {
        if (add.dataset.railAction === "add-project") {
          void addProject();
        } else if (add.dataset.railAction === "new-session") {
          void newSessionIn(add.dataset.projectId || "");
        }
        return;
      }
      const row = ev.target && ev.target.closest ? ev.target.closest(".rail-session") : null;
      if (row) {
        void openSessionRow(row.dataset.projectId || "", row.dataset.sessionId || "");
        return;
      }
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      const projectId = chip.dataset.projectId || "";
      if (projectId === currentProjectId) {
        // Switching to the project already on screen does nothing, so the
        // header's job there is to fold its session list away.
        if (collapsedProjects.has(projectId)) {
          collapsedProjects.delete(projectId);
        } else {
          collapsedProjects.add(projectId);
        }
        renderProjects();
        return;
      }
      collapsedProjects.delete(projectId);
      void switchProject(projectId);
    });
    railEl.addEventListener("contextmenu", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      if (ev.preventDefault) ev.preventDefault();
      openRailMenu(chip.dataset.projectId || "", ev.clientX || 0, ev.clientY || 0);
    });
    // "Close project" and "Remove from list" were reachable only through
    // contextmenu, which a keyboard user cannot fire at all. Shift+F10 and the
    // ContextMenu key are the platform's own keyboard equivalent of a
    // right-click, so give the focused chip that path into the same menu
    // rather than building a second one.
    railEl.addEventListener("keydown", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      const isMenuKey = ev.key === "ContextMenu" || (ev.key === "F10" && ev.shiftKey);
      if (!isMenuKey) {
        return;
      }
      if (ev.preventDefault) ev.preventDefault();
      const rect = chip.getBoundingClientRect ? chip.getBoundingClientRect() : null;
      const x = rect ? rect.left : 0;
      const y = rect ? rect.bottom : 0;
      openRailMenu(chip.dataset.projectId || "", x, y);
    });
  }
  if (document.addEventListener) {
    document.addEventListener("click", (ev) => {
      if (!railMenu.hidden && ev.target !== railMenu) {
        railMenu.hidden = true;
      }
    });
  }

  // ---- appearance --------------------------------------------------------
  //
  // Web-only. The VS Code webview follows the editor's own theme, so nothing
  // here has a counterpart in chat-src.

  /** @returns {string} "system" | "light" | "dark" */
  function savedTheme() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.theme") : "";
      return v === "light" || v === "dark" ? v : "system";
    } catch (e) {
      // A browser with site data blocked throws on access, not on read.
      return "system";
    }
  }

  /** @param {string} choice */
  function applyTheme(choice) {
    const root = document.documentElement;
    if (!root) return;
    if (choice === "light" || choice === "dark") {
      if (root.setAttribute) root.setAttribute("data-theme", choice);
    } else if (root.removeAttribute) {
      root.removeAttribute("data-theme");
    }
    try {
      if (window.localStorage) {
        if (choice === "system") window.localStorage.removeItem("orchestra.theme");
        else window.localStorage.setItem("orchestra.theme", choice);
      }
    } catch (e) {
      // Not persisting is survivable; the page still honours the click.
    }
  }

  /** The scales Appearance offers, as percentages. @type {string[]} */
  const SCALE_CHOICES = ["100", "110", "125", "150", "175", "200"];

  /** @returns {string} "auto" or one of SCALE_CHOICES */
  function savedScale() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.scale") : "";
      return SCALE_CHOICES.indexOf(v || "") >= 0 ? String(v) : "auto";
    } catch (e) {
      return "auto";
    }
  }

  /**
   * How large the interface is drawn. chat.css picks a default from the
   * viewport, which is only ever a guess about a monitor's physical size;
   * this is the answer for when the guess is wrong.
   * @param {string} choice
   */
  function applyScale(choice) {
    const root = document.documentElement;
    if (!root) return;
    if (SCALE_CHOICES.indexOf(choice) >= 0) {
      if (root.setAttribute) root.setAttribute("data-scale", choice);
    } else if (root.removeAttribute) {
      root.removeAttribute("data-scale");
    }
    try {
      if (window.localStorage) {
        if (SCALE_CHOICES.indexOf(choice) >= 0) {
          window.localStorage.setItem("orchestra.scale", choice);
        } else {
          window.localStorage.removeItem("orchestra.scale");
        }
      }
    } catch (e) {
      // Not persisting is survivable; the page still honours the click.
    }
  }

  // ---- the sidebar's width ------------------------------------------------
  //
  // Dragging its edge sets the width; clicking the edge folds the sidebar away
  // and back. Both are remembered, because a width you have to set again on
  // every launch is not a width you have set.

  /** Narrower than this and a workspace name has nowhere to go. */
  const RAIL_MIN = 200;
  /** Wider than this and the sidebar is competing with the transcript. */
  const RAIL_MAX = 520;
  /** Dragged below this, the sidebar folds rather than becoming unusable. */
  const RAIL_FOLD_AT = 150;

  const railColumn = document.getElementById("project-rail");
  const railResizer = document.getElementById("rail-resizer");

  /** @param {number} px */
  function clampRailWidth(px) {
    return Math.max(RAIL_MIN, Math.min(RAIL_MAX, Math.round(px)));
  }

  /** @returns {{width: number, collapsed: boolean}} */
  function savedRail() {
    let width = 0;
    let collapsed = false;
    try {
      if (window.localStorage) {
        width = parseInt(window.localStorage.getItem("orchestra.railWidth") || "", 10);
        collapsed = window.localStorage.getItem("orchestra.railCollapsed") === "1";
      }
    } catch (e) {
      // A locked-down browser just gets the default.
    }
    return { width: isFinite(width) && width > 0 ? clampRailWidth(width) : 0, collapsed };
  }

  /**
   * @param {number} width 0 keeps whatever width is set — pass one only when
   *   changing it, so folding and unfolding never lose the dragged size.
   * @param {boolean} collapsed
   */
  function applyRail(width, collapsed) {
    const root = document.documentElement;
    if (width && root && root.style && root.style.setProperty) {
      root.style.setProperty("--rail-w", width + "px");
    }
    if (railColumn) {
      railColumn.dataset.collapsed = collapsed ? "true" : "false";
    }
    if (railResizer) {
      railResizer.dataset.collapsed = collapsed ? "true" : "false";
      if (railResizer.setAttribute) {
        railResizer.setAttribute("aria-label", collapsed ? "Show sidebar" : "Sidebar width");
      }
    }
    try {
      if (window.localStorage) {
        if (width) {
          window.localStorage.setItem("orchestra.railWidth", String(width));
        }
        window.localStorage.setItem("orchestra.railCollapsed", collapsed ? "1" : "0");
      }
    } catch (e) {
      // Same: the click still takes effect for this session.
    }
  }

  function railCollapsed() {
    return Boolean(railColumn && railColumn.dataset && railColumn.dataset.collapsed === "true");
  }

  /** The width the sidebar has right now, dragged or default. */
  function railWidthNow() {
    if (railColumn && railColumn.getBoundingClientRect) {
      const w = railColumn.getBoundingClientRect().width;
      if (w > 1) {
        return clampRailWidth(w);
      }
    }
    return savedRail().width || 296;
  }

  {
    const start = savedRail();
    applyRail(start.width, start.collapsed);
  }

  if (railResizer && railResizer.addEventListener && railColumn) {
    let dragging = false;
    let moved = false;
    let originLeft = 0;
    let startX = 0;

    const endDrag = () => {
      if (!dragging) {
        return;
      }
      dragging = false;
      railResizer.dataset.dragging = "false";
      if (document.body && document.body.dataset) {
        document.body.dataset.railDrag = "false";
      }
      // A press that never moved is a click, and a click folds. Doing this on
      // pointerup rather than on "click" keeps the two gestures from both
      // firing off one press.
      if (!moved) {
        applyRail(0, !railCollapsed());
      }
    };

    railResizer.addEventListener("pointerdown", (ev) => {
      if (ev.button !== undefined && ev.button !== 0) {
        return;
      }
      dragging = true;
      moved = false;
      const box = railColumn.getBoundingClientRect ? railColumn.getBoundingClientRect() : null;
      originLeft = box ? box.left : 0;
      startX = ev.clientX;
      railResizer.dataset.dragging = "true";
      if (document.body && document.body.dataset) {
        document.body.dataset.railDrag = "true";
      }
      if (railResizer.setPointerCapture && ev.pointerId !== undefined) {
        // Capture, or the drag dies the moment the pointer crosses into the
        // transcript — which is where every useful drag goes.
        try {
          railResizer.setPointerCapture(ev.pointerId);
        } catch (e) {
          // Not fatal: the listeners below still fire while the button is down.
        }
      }
      if (ev.preventDefault) {
        ev.preventDefault();
      }
    });

    railResizer.addEventListener("pointermove", (ev) => {
      if (!dragging) {
        return;
      }
      const want = ev.clientX - originLeft;
      // Measured against where the press landed, not against the current
      // width: folded, the pointer starts a whole sidebar away from the width
      // it would set, and any jitter would then read as a drag and swallow
      // the click that was meant to unfold it.
      if (Math.abs(ev.clientX - startX) > 3) {
        moved = true;
      }
      if (!moved) {
        return;
      }
      if (want < RAIL_FOLD_AT) {
        // Dragged shut. The width is left alone, so letting go and clicking
        // the edge again brings back the size that was there before.
        applyRail(0, true);
        return;
      }
      applyRail(clampRailWidth(want), false);
    });

    railResizer.addEventListener("pointerup", endDrag);
    railResizer.addEventListener("pointercancel", endDrag);

    // The same two gestures from the keyboard, since the handle is focusable.
    railResizer.addEventListener("keydown", (ev) => {
      const key = ev.key;
      if (key === "Enter" || key === " " || key === "Spacebar") {
        applyRail(0, !railCollapsed());
      } else if (key === "ArrowLeft") {
        const next = railWidthNow() - 24;
        applyRail(next < RAIL_FOLD_AT ? 0 : clampRailWidth(next), next < RAIL_FOLD_AT);
      } else if (key === "ArrowRight") {
        applyRail(clampRailWidth(railWidthNow() + 24), false);
      } else {
        return;
      }
      if (ev.preventDefault) {
        ev.preventDefault();
      }
    });
  }

  const railSettingsBtn = document.getElementById("rail-settings-btn");
  const railSettingsModal = document.getElementById("rail-settings-modal");
  const railSettingsCloseBtn = document.getElementById("rail-settings-close");

  /** @param {boolean} open */
  function showRailSettings(open, section) {
    if (railSettingsModal) railSettingsModal.hidden = !open;
    if (railSettingsBtn && railSettingsBtn.setAttribute) {
      railSettingsBtn.setAttribute("aria-expanded", open ? "true" : "false");
    }
    if (open) {
      // 50-settings.js owns what is inside: it loads the panel on first open
      // and answers it from there. The section is what the composer's own
      // "settings" affordances ask to land on.
      openSettingsPanel(section || "general");
    } else if (railSettingsBtn && railSettingsBtn.focus) {
      // Sending focus back to the opener is the whole reason a dialog is
      // navigable by keyboard at all.
      railSettingsBtn.focus();
    }
  }

  if (railSettingsBtn && railSettingsBtn.addEventListener) {
    railSettingsBtn.addEventListener("click", () => {
      showRailSettings(Boolean(railSettingsModal && railSettingsModal.hidden));
    });
  }

  if (railSettingsCloseBtn && railSettingsCloseBtn.addEventListener) {
    railSettingsCloseBtn.addEventListener("click", () => showRailSettings(false));
  }

  if (railSettingsModal && railSettingsModal.addEventListener) {
    // A click that lands on the scrim rather than the card dismisses. Clicks
    // inside the panel never reach here — it is a separate document.
    railSettingsModal.addEventListener("click", (ev) => {
      if (ev.target === railSettingsModal) {
        showRailSettings(false);
      }
    });
  }

  if (document.addEventListener) {
    document.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape" && railSettingsModal && !railSettingsModal.hidden) {
        showRailSettings(false);
      }
    });
  }

  // The pre-paint script in the page already stamped the root from storage;
  // this only makes the in-memory choice and the DOM agree on first load.
  applyTheme(savedTheme());
  applyScale(savedScale());

  /**
   * Open one session from the sidebar. A row of another project is reachable
   * whenever that project has been on screen before, so switch first and only
   * then ask for the session.
   * @param {string} projectId @param {string} sessionId
   */
  async function openSessionRow(projectId, sessionId) {
    if (!projectId || !sessionId) {
      return;
    }
    if (projectId !== currentProjectId) {
      await switchProject(projectId);
      if (projectId !== currentProjectId) {
        return;
      }
    }
    if (projectState(projectId).sessionId === sessionId) {
      return;
    }
    await startSession(sessionId);
  }
