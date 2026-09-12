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
   * The live search over saved chats. hits=null means "not searching", so the
   * sidebar draws the ordinary list; an empty array means "searched and found
   * nothing", which has to look different from it.
   * @type {{projectId: string, query: string, hits: Array<any>|null}}
   */
  const sessionSearch = { projectId: "", query: "", hits: null };

  /**
   * Full-text search across this workspace's saved chats (session.search,
   * protocol v14 — implemented in the core since then and never called by any
   * interface until now). Results replace the session list in place, so a hit
   * is opened by the same click that opens any chat.
   * @param {string} query
   */
  async function showSessionSearch(query) {
    const q = String(query || "").trim();
    if (!q) {
      sessionSearch.projectId = "";
      sessionSearch.query = "";
      sessionSearch.hits = null;
      renderProjects();
      return;
    }
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      return;
    }
    sessionSearch.projectId = projectId;
    sessionSearch.query = q;
    revealSessionList();
    let hits = [];
    try {
      const r = (await conn.send("session.search", { query: q, insensitive: true, limit: 50 })) || {};
      hits = Array.isArray(r.hits) ? r.hits : [];
    } catch (err) {
      hits = [];
    }
    // A slow search that lands after the user moved on must not repaint
    // someone else's sidebar.
    if (sessionSearch.query !== q || sessionSearch.projectId !== projectId) {
      return;
    }
    // One row per chat: a chat matching six times is one chat to open, and the
    // first hit carries the snippet worth showing.
    const seen = new Set();
    sessionSearch.hits = hits.filter((h) => {
      const id = String((h && h.session_id) || "");
      if (!id || seen.has(id)) {
        return false;
      }
      seen.add(id);
      return true;
    });
    renderProjects();
  }

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
        noteProjectLive(id);
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
        noteProjectLive(id);
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
      // Marked before the await, because this is the slow half: a closed
      // workspace has to start a core, which is a second or two of a sidebar
      // that looked like it had ignored the click.
      markRailOpening(projectId);
      // init: true, as on the start screen. A workspace in this list is one
      // the person put there; refusing it for want of a .orchestra.yml left
      // the click doing nothing but writing a note into the transcript of
      // whichever project they were still looking at.
      const opened = await openProject(entry.path, true);
      markRailOpening("");
      if (!opened) {
        return;
      }
    }
    ensureConn(projectId);
    await activateProject(projectId);
  }

  /**
   * Name the workspace in the chat's own header. The sidebar says which one is
   * open, but it is a column of names off to one side and can now be folded
   * away entirely — while the transcript, the tabs and the composer look the
   * same whichever project they belong to. Sending a message to the wrong
   * workspace is a real mistake and nothing on that half of the window stood
   * in its way.
   *
   * Built here rather than in the markup on purpose: everything inside #app is
   * byte-identical with the VS Code webview's, and the editor's panel always
   * has its one folder, so it has nothing to name. The element is created
   * beside the logo at runtime, which leaves that parity alone.
   */
  /**
   * Held rather than looked up: it carries no id, because check-web requires
   * every id the code reaches for to exist in index.html, and this element
   * deliberately does not — it is built at runtime precisely so the shared
   * markup stays untouched.
   * @type {any}
   */
  let chromeProjectEl = null;

  /** @type {any} */
  let skeletonEl = null;

  /**
   * Grey placeholder lines in the transcript while a workspace is coming up.
   * Built at runtime and appended to #messages, which the renderer clears
   * with its own clearMessages when the real transcript arrives — so the
   * placeholder is gone the moment there is something to show instead.
   * @param {boolean} on
   */
  function paintSkeleton(on) {
    const messages = document.getElementById("messages");
    if (!messages || !messages.appendChild) {
      return;
    }
    const attached = Boolean(skeletonEl && skeletonEl.parentNode === messages);
    if (!on) {
      if (attached && messages.removeChild) {
        messages.removeChild(skeletonEl);
      }
      return;
    }
    if (attached) {
      return;
    }
    // Only into an empty transcript: a project switched to while another was
    // on screen paints over that one's history, not under it.
    if (messages.childNodes && messages.childNodes.length > 0) {
      return;
    }
    skeletonEl = document.createElement("div");
    skeletonEl.className = "skel";
    skeletonEl.setAttribute("aria-hidden", "true");
    // Two exchanges' worth of lines: a short one on the right, a longer run
    // on the left. Widths vary so it reads as text, not as bars.
    const rows = [
      ["skel-row skel-user", "34%"],
      ["skel-row", "72%"],
      ["skel-row", "58%"],
      ["skel-row", "66%"],
      ["skel-row skel-user", "22%"],
      ["skel-row", "80%"],
      ["skel-row", "47%"],
    ];
    for (const [cls, width] of rows) {
      const row = document.createElement("div");
      row.className = cls;
      if (row.style && row.style.setProperty) {
        row.style.setProperty("--w", width);
      }
      skeletonEl.appendChild(row);
    }
    messages.appendChild(skeletonEl);
  }

  function paintChromeProject() {
    const brand = document.getElementById("chrome-brand");
    if (!brand || !brand.appendChild) {
      return;
    }
    if (!chromeProjectEl) {
      chromeProjectEl = document.createElement("span");
      chromeProjectEl.className = "chrome-project";
      brand.appendChild(chromeProjectEl);
    }
    const label = chromeProjectEl;
    // The one on screen; failing that, the one on its way, so the name is up
    // before the core is.
    const entry =
      known.find((p) => p.id === currentProjectId) ||
      known.find((p) => pendingOpenId && p.id === pendingOpenId) ||
      known.find((p) => pendingOpenPath && p.path === pendingOpenPath);
    // textContent: a folder name off disk.
    label.textContent = (entry && (entry.name || entry.path)) || "";
    label.title = (entry && entry.path) || "";
    label.hidden = !label.textContent;
    if (brand.removeAttribute && label.textContent) {
      // The brand block was decoration while it held only the logo; it names
      // the workspace now, so it stops being hidden from a screen reader.
      brand.removeAttribute("aria-hidden");
    }
  }

  /** The workspace whose core is starting, so its row can say so. */
  let railOpeningId = "";

  /** @param {string} projectId "" clears it. */
  function markRailOpening(projectId) {
    railOpeningId = projectId || "";
    renderProjects();
  }

  /**
   * Start a session in a workspace, opening its core first if it has none.
   * The add button sits on a workspace heading, and a workspace can be listed
   * with its core closed — after a reload every one of them is. Reaching
   * session.start through a connection that is not there printed
   * "Cannot read properties of null (reading 'send')" into the transcript,
   * which is the whole reason this exists rather than a bare startSession.
   * @param {string} projectId
   */
  async function newSessionIn(projectId) {
    const id = projectId || currentProjectId;
    if (!id) {
      return;
    }
    if (id !== currentProjectId) {
      await switchProject(id);
      if (id !== currentProjectId) {
        return;
      }
    }
    if (!connFor(id)) {
      // switchProject returns early for the project already on screen, so a
      // closed current workspace is opened here.
      const entry = known.find((p) => p.id === id);
      if (entry && entry.state === "closed") {
        markRailOpening(id);
        const opened = await openProject(entry.path, true);
        markRailOpening("");
        if (!opened) {
          return;
        }
      }
      ensureConn(id);
      await activateProject(id);
      if (!connFor(id)) {
        return;
      }
    }
    await startSession(undefined);
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
      const err = new Error(humanApiError(body, res.status));
      // @ts-ignore — the caller distinguishes not_initialized from the rest.
      err.code = body.error || "";
      // @ts-ignore
      err.path = body.path || "";
      throw err;
    }
    return body;
  }

  /**
   * Turn the project API's machine code into something a person can read.
   *
   * The server answers with `error` — a code like `already_open` — and for the
   * failures that have a cause it sends the cause in `detail`. The client used
   * the code as the message and never read `detail`, so opening a project that
   * was already open said "could not open C:\work: already_open", with the
   * real explanation sitting unused in the same response.
   *
   * `err.code` keeps the raw value: callers branch on it.
   * @param {any} body @param {number} status
   */
  function humanApiError(body, status) {
    const code = String((body && body.error) || "").trim();
    const detail = String((body && body.detail) || "").trim();
    switch (code) {
      case "already_open":
        return "that project is already open";
      case "no_such_dir":
        return "there is no folder at that path";
      case "project_not_open":
        return "that project is not open";
      case "not_initialized":
        return "the core is still starting — try again in a moment";
      case "clone_not_enabled":
        return "cloning is turned off in this build";
      case "open_failed":
        return detail || "the project could not be opened";
      case "forget_failed":
        return detail || "the project could not be removed from the list";
      case "clone_failed":
        return detail || "the clone did not finish";
      case "bad_request":
        return detail || "the request was rejected";
    }
    return detail || code || `request failed (${status})`;
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
      // Already open is not a failure: the caller asked for this project to be
      // available and it is. Without this the start screen read 409 as a
      // refusal, fell through to the "initialise it?" path, and asked about a
      // project that was open the whole time.
      // @ts-ignore
      if (err && err.code === "already_open") {
        await refreshProjects();
        return true;
      }
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
    // Same move as closeProject: forgetting the project you are looking at
    // otherwise leaves currentProjectId naming an entry that is no longer in
    // `known` and has no chip, so the window shows a project that is gone.
    if (projectId === currentProjectId) {
      const next = known.find((p) => p.state === "ready" && p.id !== projectId);
      if (next) {
        await switchProject(next.id);
      }
    }
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

  /* The plus, drawn once. A literal — no value from the core reaches it. */
  const PLUS_SVG =
    '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
    '<path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>';

  /**
   * The one affordance for adding: it sits on the heading of the group it
   * adds to — a session under the workspace it belongs to, a workspace under
   * the list of workspaces — so neither needs a bar of its own.
   * @param {string} action "new-session" | "add-project"
   * @param {string} label
   * @param {string} [projectId]
   * @returns {HTMLElement}
   */
  function railAddButton(action, label, projectId) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "rail-add";
    btn.dataset.railAction = action;
    if (projectId) {
      btn.dataset.projectId = projectId;
    }
    btn.title = label;
    btn.setAttribute("aria-label", label);
    btn.innerHTML = PLUS_SVG;
    return btn;
  }

  /**
   * The button that throws a chat away. It asks twice — the first click arms
   * it for a few seconds — because there is no undo: the core removes the
   * snapshot and the event log from disk.
   * @param {string} projectId @param {string} sessionId
   */
  function railDeleteButton(projectId, sessionId) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "rail-session-del";
    btn.dataset.projectId = projectId;
    btn.dataset.sessionId = sessionId;
    btn.title = "Delete this chat";
    btn.setAttribute("aria-label", "Delete this chat");
    btn.textContent = "×";
    let armed = 0;
    btn.addEventListener("click", (e) => {
      if (e && e.stopPropagation) e.stopPropagation();
      if (e && e.preventDefault) e.preventDefault();
      if (!armed) {
        armed = 1;
        btn.classList.add("armed");
        btn.textContent = "Delete?";
        btn.title = "Click again to delete this chat for good";
        setTimeout(() => {
          if (!armed) return;
          armed = 0;
          btn.classList.remove("armed");
          btn.textContent = "×";
          btn.title = "Delete this chat";
        }, 4000);
        return;
      }
      armed = 0;
      void deleteSession(projectId, sessionId);
    });
    return btn;
  }

  /**
   * The workspaces heading, which always carries the add button — including
   * when there is nothing under it yet, which is exactly when a new user
   * needs it.
   * @param {string} label @returns {HTMLElement}
   */
  function railSectionHeading(label) {
    const sec = document.createElement("div");
    sec.className = "rail-section";
    const text = document.createElement("span");
    text.className = "rail-section-label";
    text.textContent = label;
    sec.appendChild(text);
    sec.appendChild(railAddButton("add-project", "Add a workspace folder"));
    return sec;
  }

  /**
   * The box above a workspace's chats. It searches their text, not their
   * titles: the core reads the saved messages, so "that thing about the
   * circuit breaker" finds the chat even when its title says nothing.
   * @param {string} projectId @returns {HTMLElement}
   */
  function railSessionSearchBox(projectId) {
    const box = document.createElement("input");
    box.type = "search";
    box.className = "rail-session-search";
    box.placeholder = "Search chats";
    box.setAttribute("aria-label", "Search this workspace's chats");
    if (sessionSearch.projectId === projectId) {
      box.value = sessionSearch.query;
    }
    let debounce = 0;
    box.addEventListener("input", () => {
      const q = String(box.value || "");
      if (debounce) {
        clearTimeout(debounce);
      }
      // Each keystroke is a file walk across every saved session, so let the
      // typing settle first.
      debounce = setTimeout(() => {
        void showSessionSearch(q);
      }, 200);
    });
    box.addEventListener("keydown", (e) => {
      if (e && e.key === "Escape") {
        box.value = "";
        void showSessionSearch("");
      }
    });
    return box;
  }

  /**
   * The search results, drawn as ordinary chat rows so the rail's existing
   * click handler opens them, with the matching line underneath.
   * @param {HTMLElement} container @param {string} projectId @param {string} openSessionId
   */
  function renderSessionHits(container, projectId, openSessionId) {
    const hits = sessionSearch.hits || [];
    if (hits.length === 0) {
      const empty = document.createElement("div");
      empty.className = "rail-sessions-empty";
      empty.textContent = `No chat mentions “${sessionSearch.query}”`;
      container.appendChild(empty);
      return;
    }
    for (const h of hits) {
      const sessionId = String((h && h.session_id) || "");
      const item = document.createElement("button");
      item.type = "button";
      item.className = "rail-session";
      item.dataset.projectId = projectId;
      item.dataset.sessionId = sessionId;
      item.dataset.active = sessionId === openSessionId ? "true" : "false";
      const title = document.createElement("span");
      title.className = "rail-session-title";
      title.textContent = String((h && h.title) || "") || sessionId || "untitled";
      const age = document.createElement("span");
      age.className = "rail-session-time";
      age.textContent = sessionAge({ id: sessionId, updated_at: h && h.updated_at });
      item.appendChild(title);
      item.appendChild(age);
      const snippet = String((h && h.snippet) || "").trim();
      if (snippet) {
        const line = document.createElement("span");
        line.className = "rail-session-snippet";
        // textContent: a snippet is the user's or the model's own words.
        line.textContent = snippet;
        item.appendChild(line);
        item.title = snippet;
      }
      const rowEl = document.createElement("div");
      rowEl.className = "rail-session-row";
      rowEl.appendChild(item);
      rowEl.appendChild(railDeleteButton(projectId, sessionId));
      container.appendChild(rowEl);
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
        // -1 from the server means "could not read in time", which the rail
        // shows as no number rather than as none.
        sessions: typeof p.sessions === "number" ? p.sessions : -1,
      };
    });
    toRenderer({ type: "projectList", projects: rows });
    // Before the early return below: the editor's webview has neither a rail
    // nor a start screen, and syncStartScreen is a no-op there, but the web
    // host must not skip it just because the rail is missing.
    syncStartScreen();

    paintChromeProject();

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
    // With nothing open there is no "other" to be other than, so the heading
    // is simply the list's own, at the top where a heading belongs.
    if (!hasActive) {
      list.appendChild(railSectionHeading("Workspaces"));
      otherLabelDone = true;
    }
    for (const row of ordered) {
      if (!row.active && hasActive && !otherLabelDone) {
        list.appendChild(railSectionHeading("Other workspaces"));
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
      if (railOpeningId && row.id === railOpeningId) {
        chip.dataset.opening = "true";
      }
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

      // How many chats the workspace holds. The server counts them off disk,
      // which is the only way a workspace whose core is closed can have a
      // number at all; the open one prefers its live list, so starting a
      // session bumps the count straight away instead of at the next poll.
      const listedNow = sessionsByProject.get(row.id);
      const count = row.active && listedNow ? listedNow.length : row.sessions;
      if (typeof count === "number" && count >= 0) {
        const badge = document.createElement("span");
        badge.className = "project-count";
        badge.textContent = String(count);
        badge.title = count === 1 ? "1 chat" : count + " chats";
        chip.appendChild(badge);
      }

      const dot = document.createElement("span");
      dot.className = "project-dot";
      chip.appendChild(dot);

      // The header is a row, not just the chip: the open workspace carries
      // the button that starts a session in it. A <button> cannot nest inside
      // another, so the two are siblings.
      const head = document.createElement("div");
      head.className = "project-head";
      head.appendChild(chip);
      if (row.active) {
        head.appendChild(railAddButton("new-session", "New session in this workspace", row.id));
      }
      group.appendChild(head);

      const sessions = document.createElement("div");
      sessions.className = "project-sessions";
      const listed = sessionsByProject.get(row.id) || [];
      const openSessionId = (peekProjectState(row.id) || {}).sessionId || "";

      // Searching replaces the list rather than sitting beside it: the whole
      // point is to narrow a hundred chats to the three that mention a thing.
      const searching = row.active && sessionSearch.projectId === row.id && sessionSearch.hits !== null;
      if (row.active) {
        sessions.appendChild(railSessionSearchBox(row.id));
      }
      if (searching) {
        renderSessionHits(sessions, row.id, openSessionId);
        group.appendChild(sessions);
        list.appendChild(group);
        continue;
      }
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
        // A button cannot nest in a button, so the row is a pair: the chat
        // itself, and the one that throws it away.
        const rowEl = document.createElement("div");
        rowEl.className = "rail-session-row";
        rowEl.appendChild(item);
        rowEl.appendChild(railDeleteButton(row.id, s.id || ""));
        sessions.appendChild(rowEl);
      }
      group.appendChild(sessions);
      list.appendChild(group);
    }
    // The inline heading is only written when other workspaces follow it. When
    // none do — one workspace open, or none at all on a first run — the
    // heading still has to appear, because it carries the only way to add one.
    if (!otherLabelDone) {
      list.appendChild(railSectionHeading("Workspaces"));
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

  /**
   * Delete a chat for good: the core removes its snapshot and its event log
   * (session.close, which cancels a running turn first). Closing a tab only
   * hides it from the strip — this is the one that throws the chat away, so
   * the rail's button asks twice before calling it.
   *
   * The chat on screen can be deleted too; a workspace is never left without
   * one, so a fresh session takes its place.
   * @param {string} projectId @param {string} sessionId
   */
  async function deleteSession(projectId, sessionId) {
    if (!projectId || !sessionId) {
      return;
    }
    const conn = connFor(projectId);
    if (!conn || !conn.isOpen()) {
      toRenderer({ type: "error", message: "The workspace is not open, so its chats cannot be deleted." });
      return;
    }
    try {
      await conn.send("session.close", { session_id: sessionId });
    } catch (err) {
      toRenderer({
        type: "error",
        message: "Could not delete the chat: " + String((err && err.message) || err),
      });
      return;
    }
    // Off the strip and out of the cached list before anything is read back,
    // so the row goes away on the click rather than on the next poll.
    hiddenTabsFor(projectId).delete(sessionId);
    noteSessionList(
      projectId,
      (sessionsByProject.get(projectId) || []).filter((s) => s && s.id !== sessionId)
    );
    const st = projectState(projectId);
    if (st.sessionId === sessionId) {
      st.sessionId = "";
      if (projectId === currentProjectId) {
        const next = visibleTabs()[0];
        if (next) {
          await openSessionRow(projectId, next.id);
        } else {
          await startSession(undefined);
        }
      }
    }
    pushSessionTabs();
    void refreshSessionList(projectId);
  }

  /**
   * Put this workspace's chats in front of the user. On the web that is the
   * sidebar — the header's "all sessions" button is hidden here, because the
   * rail is where the list lives — so unfold the rail, open the workspace's
   * group and scroll to the chat on screen. Answers false where there is no
   * rail (the editor's webview), so the caller can fall back to the button.
   */
  function revealSessionList() {
    const list = document.getElementById("project-rail-list");
    if (!list || !currentProjectId) {
      return false;
    }
    if (railCollapsed()) {
      applyRail(0, false);
    }
    collapsedProjects.delete(currentProjectId);
    renderProjects();
    const active = list.querySelector ? list.querySelector('.rail-session[data-active="true"]') : null;
    if (active && active.scrollIntoView) {
      active.scrollIntoView({ block: "nearest" });
    }
    return true;
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

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
  // ---- the start screen --------------------------------------------------
  //
  // Shown when no workspace is open. The desktop shell starts the core with
  // --no-project so its window opens here rather than guessing which project
  // you meant; a plain `orchestra web` opens the folder it was run in and
  // never sees this, unless every project is closed from the sidebar.

  /**
   * The start screen is a view of state, not a place the code navigates to:
   * it is up exactly while no workspace is on screen. It used to be switched
   * by hand at each place that opened something, and the places that opened
   * something without going through the screen were missed — clicking a
   * workspace in the sidebar left the start screen covering the chat of a
   * project that was open, selected and listing its sessions. So it is
   * derived here instead, from the one project the window is showing, and
   * renderProjects calls it; every path that changes what is open already
   * ends there.
   */
  /** How long the start screen takes to fade; matches rail.css. */
  const FADE_MS = 280;

  /**
   * Run the arrival animation on one element once. The attribute is what the
   * keyframes hang off, and it has to come off again or the animation never
   * replays on the next switch.
   * @param {any} el
   */
  function enterAnimation(el) {
    if (!el || !el.dataset || !window.setTimeout) {
      return;
    }
    el.dataset.entering = "true";
    window.setTimeout(() => {
      el.dataset.entering = "false";
    }, 760);
  }

  function syncStartScreen() {
    const screen = document.getElementById("start-screen");
    const app = document.getElementById("app");
    if (!screen || !app) {
      return;
    }
    const ready =
      Boolean(currentProjectId) &&
      known.some((p) => p.id === currentProjectId && p.state === "ready");
    const open = ready || pendingOpen();
    const wasOpen = !app.hidden;
    app.hidden = !open;
    // Loading until the project is live, not merely open: the core can be up
    // with its socket still connecting, and the composer must not take a
    // message it has nowhere to send.
    const loading = open && (!ready || pendingOpen());
    if (app.dataset) {
      app.dataset.loading = loading ? "true" : "false";
    }
    paintSkeleton(loading);
    if (open && !wasOpen) {
      // Leaving the start screen: it fades out over the project rather than
      // being switched off under it, and the project rises into place. Both
      // halves are CSS; this only marks which is which and clears up after.
      enterAnimation(app);
      enterAnimation(document.getElementById("project-rail"));
      if (!screen.hidden && screen.dataset) {
        screen.dataset.leaving = "true";
        if (window.setTimeout) {
          window.setTimeout(() => {
            screen.hidden = true;
            screen.dataset.leaving = "false";
          }, FADE_MS);
        } else {
          screen.hidden = true;
        }
      }
    } else {
      if (screen.dataset) {
        screen.dataset.leaving = "false";
      }
      screen.hidden = open;
    }
    // The sidebar goes with the chat. It is a sibling of #app rather than a
    // child, so hiding the chat alone left a column of workspaces down the
    // edge of a screen whose whole subject is which workspace to open — the
    // same list twice, one of them unreadable as a launcher. With nothing
    // open the start screen takes the window.
    const rail = document.getElementById("project-rail");
    if (rail) {
      rail.hidden = !open;
    }
    const edge = document.getElementById("rail-resizer");
    if (edge) {
      edge.hidden = !open;
    }
    if (!open) {
      renderStartScreen();
    }
  }

  /**
   * Say that a workspace is opening, and stop the screen taking another
   * click while it is. "" ends it. The id, when given, is the row that was
   * clicked: it keeps its contrast and spins, so it is clear which workspace
   * is coming while the rest of the list steps back.
   * @param {string} message @param {string} [projectId]
   */
  function startBusy(message, projectId) {
    // Kept rather than only written to the row, because opening a workspace
    // refreshes the list and rebuilds every row: renderStartScreen reads this
    // back, so the spinner survives its own progress.
    busyProjectId = message ? projectId || "" : "";
    const card = document.querySelector ? document.querySelector(".start-card") : null;
    const line = document.getElementById("start-busy");
    const text = document.getElementById("start-busy-text");
    if (card) {
      card.dataset.busy = message ? "true" : "false";
    }
    if (text) {
      text.textContent = message || "";
    }
    if (line) {
      line.hidden = !message;
    }
    renderStartScreen();
  }

  /** @param {string} message "" clears it. */
  function startError(message) {
    const el = document.getElementById("start-error");
    if (!el) {
      return;
    }
    el.textContent = message || "";
    el.hidden = !message;
  }

  /** The workspace currently opening, if the start screen is waiting on one. */
  let busyProjectId = "";

  /**
   * The workspace the window has left the start screen for, but which is not
   * live yet — its core starting, its socket not open. Either the id (a row
   * on the start screen) or only the path (a folder just picked). While one
   * is set the chat is on screen in its loading state, and syncStartScreen
   * treats it as open: the start screen leaves at the click, not when the
   * core finally answers, and the wait is spent looking at the project's own
   * frame filling in rather than at a dimmed launcher.
   */
  let pendingOpenId = "";
  let pendingOpenPath = "";

  function pendingOpen() {
    return Boolean(pendingOpenId || pendingOpenPath);
  }

  /**
   * The project's socket is up and its session started — or failed, which
   * ends the wait just the same. Called from the connection callbacks in
   * 10-adapter-session.js.
   * @param {string} projectId
   */
  function noteProjectLive(projectId) {
    if (!pendingOpen() || projectId !== currentProjectId) {
      return;
    }
    pendingOpenId = "";
    pendingOpenPath = "";
    renderProjects();
  }

  function renderStartScreen() {
    const list = document.getElementById("start-recent-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    if (known.length === 0) {
      const empty = document.createElement("div");
      empty.className = "start-recent-empty";
      empty.textContent = "No workspaces yet — open a folder or clone a repository.";
      list.appendChild(empty);
      return;
    }
    for (const p of known) {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "start-recent-item";
      item.dataset.projectId = p.id || "";
      item.dataset.path = p.path || "";
      item.title = p.path || "";
      if (p.missing) {
        item.dataset.missing = "true";
      }
      if (busyProjectId && p.id === busyProjectId) {
        item.dataset.busy = "true";
      }

      const name = document.createElement("span");
      name.className = "start-recent-name";
      // textContent: the name is a folder name off disk.
      name.textContent = p.name || p.path || "?";
      item.appendChild(name);

      const path = document.createElement("span");
      path.className = "start-recent-path";
      path.textContent = p.path || "";
      item.appendChild(path);

      const count = document.createElement("span");
      count.className = "start-recent-count";
      if (p.missing) {
        count.textContent = "folder is gone";
      } else if (typeof p.sessions === "number" && p.sessions >= 0) {
        count.textContent = p.sessions === 1 ? "1 chat" : p.sessions + " chats";
      }
      if (count.textContent) {
        item.appendChild(count);
      }
      list.appendChild(item);
    }
  }

  /**
   * Open a workspace from the start screen and leave it. Anything that fails
   * stays on the screen with the reason, because there is nowhere else to go.
   * @param {string} path
   */
  async function openFromStart(path, projectId) {
    startError("");
    if (!path) {
      return;
    }
    const entry = known.find((p) => p.path === path);
    startBusy("Opening " + ((entry && entry.name) || path) + "…", projectId || "");
    pendingOpenId = projectId || "";
    pendingOpenPath = path;
    renderProjects();
    try {
      await openAndEnter(path);
    } finally {
      startBusy("");
      const now = known.find((p) => p.path === path);
      if (!now || currentProjectId !== now.id) {
        // It did not get there. The start screen is still the place, and
        // it has to be usable again — with the reason, which openAndEnter
        // has already written into it.
        pendingOpenId = "";
        pendingOpenPath = "";
        renderProjects();
      }
      // Otherwise the wait ends when the socket is live: noteProjectLive.
    }
  }

  /** @param {string} path */
  async function openAndEnter(path) {
    // init: true, and nothing is asked. Picking a folder to work in IS the
    // consent to set it up — `orchestra web --init` has always treated a
    // folder chosen in a dialog exactly this way — and the only question
    // there was to ask cannot be asked here: window.confirm inside the
    // desktop shell is Tauri's own, it needs a capability this page is
    // deliberately not granted, and it rejects. Worse, it rejects
    // ASYNCHRONOUSLY, so the promise it returns reads as true and the code
    // carried on as if the user had agreed.
    if (!(await openProject(path, true))) {
      startError("Could not open " + path + ".");
      return;
    }
    await enterProject(path);
  }

  /**
   * Leave the start screen for a workspace that is now open. Opening it is
   * only half the job — without the switch the window left the start screen
   * for a transcript belonging to no project at all.
   * @param {string} path
   */
  async function enterProject(path) {
    const entry =
      known.find((p) => p.path === path) || known.find((p) => p.state === "ready");
    if (!entry) {
      startError("Opened " + path + ", but it is not in the workspace list.");
      return;
    }
    await switchProject(entry.id);
    if (currentProjectId !== entry.id) {
      startError("Opened " + path + ", but could not switch to it.");
    }
  }

  /** Clone a repository, then open it. Both halves are one request. */
  async function cloneFromStart() {
    const input = /** @type {HTMLInputElement|null} */ (
      document.getElementById("start-clone-url")
    );
    const go = /** @type {HTMLButtonElement|null} */ (
      document.getElementById("start-clone-go")
    );
    const url = input ? String(input.value || "").trim() : "";
    if (!url) {
      startError("Enter a repository URL.");
      return;
    }
    startError("");
    // Where to put it. The shell's folder picker when there is one; typed
    // otherwise, because a browser cannot open a native chooser.
    let parent = "";
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      try {
        const picked = await t.dialog.open({ directory: true, multiple: false });
        parent = typeof picked === "string" ? picked : "";
      } catch (e) {
        parent = "";
      }
    } else if (window.prompt) {
      parent = window.prompt("Clone into which folder? (absolute path)") || "";
    }
    parent = String(parent || "").trim();
    if (!parent) {
      return;
    }
    if (go) {
      go.disabled = true;
      go.textContent = "Cloning…";
    }
    startBusy("Cloning " + url + "…", "");
    try {
      const created = await api("/api/projects/clone", {
        method: "POST",
        body: JSON.stringify({ url, parent }),
      });
      await refreshProjects();
      if (created && created.id) {
        await switchProject(created.id);
      }
    } catch (e) {
      startError(String((e && e.message) || e));
    } finally {
      startBusy("");
      if (go) {
        go.disabled = false;
        go.textContent = "Clone";
      }
    }
  }

  {
    const screen = document.getElementById("start-screen");
    if (screen && screen.addEventListener) {
      screen.addEventListener("click", (ev) => {
        const drop = ev.target && ev.target.closest ? ev.target.closest("[data-forget-id]") : null;
        if (drop) {
          startError("");
          void (async () => {
            await forgetProject(drop.dataset.forgetId || "");
          })();
          return;
        }
        const item = ev.target && ev.target.closest ? ev.target.closest(".start-recent-item") : null;
        if (!item) {
          return;
        }
        if (item.dataset.missing === "true") {
          // Opening it would 404 and say "could not open" with nothing to do
          // about it. Say what is actually wrong and offer the only repair.
          startError("");
          const el = document.getElementById("start-error");
          if (el) {
            el.textContent =
              "The folder for " + (item.dataset.path || "this workspace") + " is not there any more. ";
            const drop = document.createElement("button");
            drop.type = "button";
            drop.className = "start-action start-error-action";
            drop.dataset.forgetId = item.dataset.projectId || "";
            drop.textContent = "Remove from the list";
            el.appendChild(drop);
            el.hidden = false;
          }
          return;
        }
        void openFromStart(item.dataset.path || "", item.dataset.projectId || "");
      });
    }
    const openBtn = document.getElementById("start-open-btn");
    if (openBtn && openBtn.addEventListener) {
      openBtn.addEventListener("click", () => {
        void (async () => {
          startError("");
          const picked = await pickProjectFolder();
          if (!picked) {
            return;
          }
          await openFromStart(picked, "");
        })();
      });
    }
    const cloneBtn = document.getElementById("start-clone-btn");
    const cloneForm = document.getElementById("start-clone-form");
    if (cloneBtn && cloneBtn.addEventListener && cloneForm) {
      cloneBtn.addEventListener("click", () => {
        cloneForm.hidden = !cloneForm.hidden;
        startError("");
        const input = document.getElementById("start-clone-url");
        if (!cloneForm.hidden && input && input.focus) {
          input.focus();
        }
      });
    }
    const cloneGo = document.getElementById("start-clone-go");
    if (cloneGo && cloneGo.addEventListener) {
      cloneGo.addEventListener("click", () => void cloneFromStart());
    }
    const cloneCancel = document.getElementById("start-clone-cancel");
    if (cloneCancel && cloneCancel.addEventListener && cloneForm) {
      cloneCancel.addEventListener("click", () => {
        cloneForm.hidden = true;
        startError("");
      });
    }
    const cloneInput = document.getElementById("start-clone-url");
    if (cloneInput && cloneInput.addEventListener) {
      cloneInput.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter") {
          ev.preventDefault();
          void cloneFromStart();
        }
      });
    }
  }

  /**
   * Ask for a folder and return it, or "" if the person backed out. Separate
   * from opening it because the start screen has to name the folder in its
   * "opening…" line before the open starts, and the dialog is the only place
   * the name comes from.
   * @returns {Promise<string>}
   */
  async function pickProjectFolder() {
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
    return String(path || "").trim();
  }

  async function addProject() {
    const path = await pickProjectFolder();
    if (!path) {
      return "";
    }
    // Chosen in a dialog, so set it up if it needs it — same judgment
    // `orchestra web --init` makes about a folder the user picked, and the
    // confirm that used to stand here cannot run in the desktop shell. See
    // openFromStart.
    return (await openProject(path, true)) ? path : "";
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
    if (!first) {
      // Nothing open: --no-project, or every workspace closed from the rail.
      // The start screen takes the window until one is picked, and no socket
      // is opened for a project that does not exist.
      currentProjectId = "";
      renderProjects();
      return;
    }
    currentProjectId = first.id;
    setActiveConn(ensureConn(currentProjectId));
    renderProjects();
  })();
