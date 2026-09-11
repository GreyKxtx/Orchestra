  // The projects module: the rail, one connection per open project, and the
  // switch between them.
  //
  // Division of labour. This fragment owns "which projects exist and which one
  // is on screen"; 10/20/30 own "what one project's session is doing". The
  // renderer is never told about more than one project's content — see
  // activateProject — which is what keeps ui/vscode/media/chat-src unchanged.

  /** @type {Map<string, any>} */
  const conns = new Map();
  /** @type {Array<any>} */
  let known = [];
  /** Each project's sessions, as last listed. @type {Map<string, Array<any>>} */
  const sessionsByProject = new Map();
  /** Projects the user folded away by hand. @type {Set<string>} */
  const collapsedProjects = new Set();

  /**
   * Called by refreshSessionList for every project, on screen or not, so the
   * sidebar can show a project's sessions without re-asking the core.
   * @param {string} projectId @param {Array<any>} sessions
   */
  function noteSessionList(projectId, sessions) {
    sessionsByProject.set(projectId, Array.isArray(sessions) ? sessions : []);
    renderProjects();
  }

  /**
   * When a session was last touched, in ms, or NaN. Sessions written by an
   * older core can lack updated_at, so fall back to the timestamp in the id
   * (YYYYMMDDTHHMMSS-xxxx) exactly as the session menu does.
   * @param {any} s
   */
  function sessionStamp(s) {
    const ms = Date.parse(s.updated_at || s.created_at || "");
    if (isFinite(ms)) {
      return ms;
    }
    const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s.id || "");
    return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]).getTime() : NaN;
  }

  /** Most recently touched first, for both the sidebar and the tab strip. */
  function byRecent(a, b) {
    const x = sessionStamp(a);
    const y = sessionStamp(b);
    return (isFinite(y) ? y : 0) - (isFinite(x) ? x : 0);
  }

  /** "4m" / "3h" / "5d" for a session row. @param {any} s */
  function sessionAge(s) {
    const ms = sessionStamp(s);
    if (!isFinite(ms)) {
      return "";
    }
    const secs = Math.max(0, Math.round((Date.now() - ms) / 1000));
    if (secs < 60) {
      return secs + "s";
    }
    const mins = Math.round(secs / 60);
    if (mins < 60) {
      return mins + "m";
    }
    const hours = Math.round(mins / 60);
    if (hours < 24) {
      return hours + "h";
    }
    return Math.round(hours / 24) + "d";
  }

  /** The Conn for an open project, or null. @param {string} projectId */
  function connFor(projectId) {
    return conns.get(projectId) || null;
  }

  /**
   * Create the project's connection if it has none. The handshake runs from
   * onOpen, so a caller only awaits the socket, not the session.
   * @param {string} projectId
   */
  function ensureConn(projectId) {
    const existing = conns.get(projectId);
    if (existing) {
      return existing;
    }
    const conn = createConn(projectId, {
      onOpen: (id) => {
        if (id === currentProjectId) {
          toRenderer({ type: "status", status: "ok" });
        }
        void onConnected(id);
      },
      onClose: (id) => {
        conns.delete(id);
        forgetProjectState(id);
        if (id === currentProjectId) {
          // A dropped socket ends the session on the core side, so say so
          // plainly rather than reconnecting into what looks like the same
          // conversation.
          toRenderer({
            type: "status",
            status: "error",
            detail: "disconnected — reload to start a new session",
          });
        }
        renderProjects();
      },
      onError: (id) => {
        if (id === currentProjectId) {
          toRenderer({ type: "status", status: "error", detail: "connection error" });
        }
      },
      onNotification: (id, msg) => {
        if (noteProjectEvent(id, msg)) {
          renderProjects();
        }
      },
      onServerRequest: (id, msg) => {
        handleServerRequest(id, msg);
        if (projectState(id).status === "asking") {
          notifyAsking(id);
        }
      },
    });
    conns.set(projectId, conn);
    return conn;
  }

  /**
   * Show a project. A closed one is opened first; its entry in the list has
   * the path, which is what POST /api/projects takes.
   * @param {string} projectId
   */
  async function switchProject(projectId) {
    if (!projectId || projectId === currentProjectId) {
      return;
    }
    const entry = known.find((p) => p.id === projectId);
    if (entry && entry.state === "closed") {
      const opened = await openProject(entry.path, false);
      if (!opened) {
        return;
      }
    }
    ensureConn(projectId);
    await activateProject(projectId);
  }

  // ---- the HTTP half -----------------------------------------------------

  /** @param {string} path @param {any} init */
  async function api(path, init) {
    const res = await fetch(path, {
      ...(init || {}),
      headers: { "Content-Type": "application/json", ...((init && init.headers) || {}) },
    });
    if (res.status === 204) {
      return {};
    }
    let body = {};
    try {
      body = await res.json();
    } catch (e) {
      body = {};
    }
    if (!res.ok) {
      const err = new Error(body.error || `request failed (${res.status})`);
      // @ts-ignore — the caller distinguishes not_initialized from the rest.
      err.code = body.error || "";
      // @ts-ignore
      err.path = body.path || "";
      throw err;
    }
    return body;
  }

  async function refreshProjects() {
    try {
      const body = await api("/api/projects", { method: "GET" });
      known = Array.isArray(body.projects) ? body.projects : [];
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not list projects: " + String(err && err.message ? err.message : err),
      });
    }
    renderProjects();
  }

  /**
   * Open a folder. `init` asks the server to write .orchestra.yml first; the
   * caller only sets it after the user agreed to that.
   * @param {string} path @param {boolean} init
   * @returns {Promise<boolean>} whether the project is now open
   */
  async function openProject(path, init) {
    try {
      await api("/api/projects", {
        method: "POST",
        body: JSON.stringify({ path, init: Boolean(init) }),
      });
      await refreshProjects();
      return true;
    } catch (err) {
      // @ts-ignore
      if (err && err.code === "not_initialized" && !init) {
        toRenderer({
          type: "systemNote",
          text: `${path} is not an Orchestra project yet. Use "Add project" again and confirm initialising it.`,
        });
        return false;
      }
      toRenderer({
        type: "systemNote",
        text: "could not open " + path + ": " + String(err && err.message ? err.message : err),
      });
      return false;
    }
  }

  /** Close a project. It stays in the list. @param {string} projectId */
  async function closeProject(projectId) {
    const conn = conns.get(projectId);
    if (conn) {
      conn.close();
      conns.delete(projectId);
    }
    forgetProjectState(projectId);
    try {
      await api("/api/projects/" + encodeURIComponent(projectId), { method: "DELETE" });
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not close the project: " + String(err && err.message ? err.message : err),
      });
    }
    await refreshProjects();
    if (projectId === currentProjectId) {
      const next = known.find((p) => p.state === "ready" && p.id !== projectId);
      if (next) {
        await switchProject(next.id);
      }
    }
  }

  /** Close a project and drop it from the list. @param {string} projectId */
  async function forgetProject(projectId) {
    const conn = conns.get(projectId);
    if (conn) {
      conn.close();
      conns.delete(projectId);
    }
    forgetProjectState(projectId);
    try {
      await api("/api/projects/" + encodeURIComponent(projectId) + "?forget=1", {
        method: "DELETE",
      });
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: "could not remove the project: " + String(err && err.message ? err.message : err),
      });
    }
    await refreshProjects();
  }

  // ---- the rail ----------------------------------------------------------

  /**
   * Tell the person a project they are not looking at needs an answer.
   *
   * Only the desktop shell can raise a Windows notification, and only because
   * capabilities/core-page.json grants this page exactly that call; in a plain
   * browser there is nothing to call and the rail's badge is the whole signal.
   * The active project never notifies — its prompt is already on screen.
   * @param {string} projectId
   */
  function notifyAsking(projectId) {
    if (projectId === currentProjectId) {
      return;
    }
    const entry = known.find((p) => p.id === projectId);
    const name = (entry && (entry.name || entry.path)) || projectId;
    const t = window.__TAURI__;
    if (!t || !t.notification || !t.notification.sendNotification) {
      return;
    }
    try {
      t.notification.sendNotification({
        title: "Orchestra",
        body: name + " is waiting for your answer",
      });
    } catch (e) {
      // A notification that cannot be raised is not worth an error in the chat.
    }
  }

  /**
   * Repaint the rail and tell the renderer what the list looks like. The
   * renderer message exists for the tests and for any future consumer; the
   * rail's own DOM is written here because it is web-only.
   */
  function renderProjects() {
    const rows = known.map((p) => {
      const st = peekProjectState(p.id);
      return {
        id: p.id,
        name: p.name || p.path,
        path: p.path,
        state: p.state,
        status: p.state === "closed" || !st ? "closed" : st.status,
        active: p.id === currentProjectId,
      };
    });
    toRenderer({ type: "projectList", projects: rows });

    const list = document.getElementById("project-rail-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    // The open project heads the list, its sessions under it; everything else
    // follows under one label. The renderer message above keeps the original
    // order — its consumers and tests were written against it.
    const ordered = rows.slice().sort((a, b) => (b.active ? 1 : 0) - (a.active ? 1 : 0));
    const hasActive = rows.some((r) => r.active);
    let otherLabelDone = false;
    for (const row of ordered) {
      if (!row.active && hasActive && !otherLabelDone) {
        const sec = document.createElement("div");
        sec.className = "rail-section";
        sec.textContent = "Other workspaces";
        list.appendChild(sec);
        otherLabelDone = true;
      }
      // Only the project on screen has a session list — the core is asked for
      // one per connection — so every other group stays folded, and its header
      // switches to it rather than expanding an empty list.
      const collapsed = row.active ? collapsedProjects.has(row.id) : true;

      const group = document.createElement("div");
      group.className = "project-group";
      group.dataset.projectId = row.id;
      group.dataset.active = row.active ? "true" : "false";
      group.dataset.collapsed = collapsed ? "true" : "false";

      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "project-chip";
      chip.dataset.projectId = row.id;
      chip.dataset.state = row.state;
      chip.dataset.status = row.status;
      chip.dataset.active = row.active ? "true" : "false";
      chip.title = row.path + (row.status === "asking" ? " — waiting for you" : "");
      chip.setAttribute("aria-label", row.name + " (" + row.status + ")");
      chip.setAttribute("aria-expanded", collapsed ? "false" : "true");

      // Drawn in CSS: an inline SVG here would need createElementNS, which the
      // adapter tests' document stub does not have, and the glyph is decoration.
      const chevron = document.createElement("span");
      chevron.className = "project-chevron";
      chevron.setAttribute("aria-hidden", "true");
      chip.appendChild(chevron);

      const name = document.createElement("span");
      name.className = "project-name";
      // textContent, not innerHTML: the name is a folder name off disk.
      name.textContent = row.name || "?";
      chip.appendChild(name);

      const dot = document.createElement("span");
      dot.className = "project-dot";
      chip.appendChild(dot);
      group.appendChild(chip);

      const sessions = document.createElement("div");
      sessions.className = "project-sessions";
      const listed = sessionsByProject.get(row.id) || [];
      const openSessionId = (peekProjectState(row.id) || {}).sessionId || "";
      // session.list only returns sessions that have been written to disk, and
      // a session is written by its first message — so the session the user is
      // looking at is missing from the list until they say something. Showing
      // it anyway is the difference between "where am I" and an empty sidebar.
      const openIsListed = listed.some((s) => s.id === openSessionId);
      if (row.active && openSessionId && !openIsListed) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "rail-session";
        item.dataset.projectId = row.id;
        item.dataset.sessionId = openSessionId;
        item.dataset.active = "true";
        const title = document.createElement("span");
        title.className = "rail-session-title";
        title.textContent = "New session";
        item.title = openSessionId;
        const age = document.createElement("span");
        age.className = "rail-session-time";
        age.textContent = "now";
        item.appendChild(title);
        item.appendChild(age);
        sessions.appendChild(item);
      }
      if (listed.length === 0 && !(row.active && openSessionId)) {
        const empty = document.createElement("div");
        empty.className = "rail-sessions-empty";
        empty.textContent = "No sessions yet";
        sessions.appendChild(empty);
      }
      for (const s of listed.slice(0, 100)) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "rail-session";
        item.dataset.projectId = row.id;
        item.dataset.sessionId = s.id || "";
        item.dataset.active = row.active && s.id === openSessionId ? "true" : "false";
        const title = document.createElement("span");
        title.className = "rail-session-title";
        // textContent: titles are the user's own first message.
        title.textContent = s.title || s.id || "untitled";
        item.title = s.title || s.id || "";
        const age = document.createElement("span");
        age.className = "rail-session-time";
        age.textContent = sessionAge(s);
        item.appendChild(title);
        item.appendChild(age);
        sessions.appendChild(item);
      }
      group.appendChild(sessions);
      list.appendChild(group);
    }
    pushSessionTabs();
  }

  // ---- the header's tab strip ---------------------------------------------
  //
  // The strip itself is chat-src's (renderSessionTabs in 02-util.js), painted
  // from the "sessionTabs" message and clicked back through "openSession" /
  // "closeSession" — exactly what the VS Code panel drives. All the web was
  // ever missing is a sender, so this is it; nothing inside #app changes.
  //
  // Hence "push", not "render": every fragment shares one function scope, so
  // naming this renderSessionTabs redefined the renderer's own and routed
  // 07-events.js straight back here, posting the message again forever.
  // check-web.mjs fails on a redeclaration now.

  /** Sessions closed out of the strip, per project. @type {Map<string, Set<string>>} */
  const hiddenTabs = new Map();

  /** @param {string} projectId */
  function hiddenTabsFor(projectId) {
    let set = hiddenTabs.get(projectId);
    if (!set) {
      set = new Set();
      hiddenTabs.set(projectId, set);
    }
    return set;
  }

  /** The tabs for the project on screen, most recent first. */
  function visibleTabs() {
    if (!currentProjectId) {
      return [];
    }
    const hidden = hiddenTabsFor(currentProjectId);
    return (sessionsByProject.get(currentProjectId) || [])
      .filter((s) => s.id && !hidden.has(s.id))
      .slice()
      .sort(byRecent);
  }

  function pushSessionTabs() {
    const openSessionId = currentProjectId
      ? (peekProjectState(currentProjectId) || {}).sessionId || ""
      : "";
    const tabs = visibleTabs()
      .slice(0, 16)
      .map((s) => ({
        id: s.id,
        title: (s.title || "New chat").trim() || "New chat",
        model: s.model,
        msg_count: s.msg_count,
      }));
    // session.list only returns sessions already written to disk, and a
    // session is written by its first message — so the one being looked at
    // has no row until the user says something. Lead with it anyway.
    const shown = openSessionId && !hiddenTabsFor(currentProjectId || "").has(openSessionId);
    if (shown && !tabs.some((t) => t.id === openSessionId)) {
      tabs.unshift({ id: openSessionId, title: "New chat" });
    }
    toRenderer({ type: "sessionTabs", activeId: openSessionId, tabs });
  }

  /**
   * Closing a tab hides it from the strip: the session stays on disk and in
   * the sidebar, which is the list of everything. Closing the one on screen
   * moves to the next tab, or starts a fresh session when none is left —
   * what a tab strip is expected to do.
   * @param {string} sessionId
   */
  function closeSessionTab(sessionId) {
    if (!sessionId || !currentProjectId) {
      return;
    }
    const openSessionId = (peekProjectState(currentProjectId) || {}).sessionId || "";
    hiddenTabsFor(currentProjectId).add(sessionId);
    if (sessionId !== openSessionId) {
      pushSessionTabs();
      return;
    }
    const next = visibleTabs()[0];
    if (next) {
      void openSessionRow(currentProjectId, next.id);
    } else {
      void startSession(undefined);
    }
    pushSessionTabs();
  }

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

  const addBtn = document.getElementById("project-add-btn");
  if (addBtn && addBtn.addEventListener) {
    addBtn.addEventListener("click", () => {
      void addProject();
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
    syncThemeMenu(choice);
  }

  const railSettingsBtn = document.getElementById("rail-settings-btn");
  const railSettingsModal = document.getElementById("rail-settings-modal");
  const railSettingsCloseBtn = document.getElementById("rail-settings-close");

  /** @param {string} choice */
  function syncThemeMenu(choice) {
    if (!railSettingsModal || !railSettingsModal.querySelectorAll) return;
    railSettingsModal.querySelectorAll("[data-theme-choice]").forEach((el) => {
      el.setAttribute("aria-checked", el.getAttribute("data-theme-choice") === choice ? "true" : "false");
    });
  }

  /** What the dialog can say about where you are, from what the page knows. */
  function renderSettingsFacts() {
    const entry = known.find((p) => p.id === currentProjectId);
    const nameEl = document.getElementById("rail-fact-project");
    const pathEl = document.getElementById("rail-fact-path");
    // textContent: both are strings off disk.
    if (nameEl) nameEl.textContent = (entry && (entry.name || entry.path)) || "—";
    if (pathEl) pathEl.textContent = (entry && entry.path) || "—";
  }

  /** @param {boolean} open */
  function showRailSettings(open) {
    if (railSettingsModal) railSettingsModal.hidden = !open;
    if (railSettingsBtn && railSettingsBtn.setAttribute) {
      railSettingsBtn.setAttribute("aria-expanded", open ? "true" : "false");
    }
    if (open) {
      renderSettingsFacts();
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
    railSettingsModal.addEventListener("click", (ev) => {
      // A click that lands on the scrim rather than the card dismisses.
      if (ev.target === railSettingsModal) {
        showRailSettings(false);
        return;
      }
      const item = ev.target && ev.target.closest ? ev.target.closest("[data-theme-choice]") : null;
      if (!item) return;
      // The dialog stays open on a pick: the page repaints behind it, which
      // is the point of choosing a theme from one.
      applyTheme(item.getAttribute("data-theme-choice") || "system");
    });
  }

  if (document.addEventListener) {
    document.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape" && railSettingsModal && !railSettingsModal.hidden) {
        showRailSettings(false);
      }
    });
  }

  syncThemeMenu(savedTheme());

  const newSessionBtn = document.getElementById("rail-new-session-btn");
  if (newSessionBtn && newSessionBtn.addEventListener) {
    newSessionBtn.addEventListener("click", () => {
      void startSession(undefined);
    });
  }

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

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
  async function addProject() {
    let path = "";
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      try {
        const picked = await t.dialog.open({ directory: true, multiple: false });
        path = typeof picked === "string" ? picked : "";
      } catch (e) {
        path = "";
      }
    } else if (window.prompt) {
      path = window.prompt("Project folder (absolute path)") || "";
    }
    path = String(path || "").trim();
    if (!path) {
      return;
    }
    if (await openProject(path, false)) {
      return;
    }
    // The one recoverable refusal: the folder is not an Orchestra project yet.
    const agreed = window.confirm
      ? window.confirm(path + " is not an Orchestra project yet. Initialise it?")
      : false;
    if (agreed) {
      await openProject(path, true);
    }
  }

  // ---- startup -----------------------------------------------------------
  //
  // The startupId branch below exists for a URL that carries a project id,
  // but nothing ships one today: the desktop shell's URL is
  // `{base}/?token={token}` (ui/desktop/src-tauri/src/main.rs), with no
  // `?project=`, so the desktop app always falls through to the
  // resolve-from-API branch. A plain `orchestra web` passes none either, so
  // the id must be resolved from the API before any socket opens — see the
  // id-before-socket note inside the IIFE.

  (async () => {
    const startupId = new URLSearchParams(location.search).get("project") || "";
    if (startupId) {
      currentProjectId = startupId;
      setActiveConn(ensureConn(startupId));
      await refreshProjects();
      return;
    }
    // No id in the URL — a plain `orchestra web`. Ask the API which project the
    // core is already serving BEFORE opening a socket, so that the connection,
    // the per-project record and the rail's active chip all share one key.
    //
    // Adopting the id afterwards does not work: `conns` would stay keyed ""
    // while `currentProjectId` named the project, and since the two are
    // separate socket slots on the server, every notification would fail the
    // `projectId === currentProjectId` gate, `sendTurn` would read an empty
    // session forever, and clicking the chip could not repair it because
    // `switchProject` returns early on the id it already holds.
    await refreshProjects();
    const first = known.find((p) => p.state === "ready");
    currentProjectId = first ? first.id : "";
    setActiveConn(ensureConn(currentProjectId));
    renderProjects();
  })();
