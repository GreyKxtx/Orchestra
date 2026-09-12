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
