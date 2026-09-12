  // ---- the start screen, and startup -----------------------------------------
  //
  // Split out of 40-projects.js, which had grown to 2035 lines carrying four
  // jobs behind comment banners. The bundler concatenates these fragments
  // numerically into one IIFE, so everything 40 declares is in scope here and
  // the startup block still runs last.

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
    // Keyed, like the rail (reconcileByKey in 41-projects-rail.js): opening a
    // workspace repaints this list to mark that row busy, and rebuilding it
    // took the keyboard focus off the row the user had just pressed Enter on.
    if (known.length === 0) {
      reconcileByKey(list, [
        {
          key: "empty",
          render: (had) => {
            const empty = railNode(had, "div", "start-recent-empty");
            empty.textContent = "No workspaces yet — open a folder or clone a repository.";
            return empty;
          },
        },
      ]);
      return;
    }
    const entries = known.map((p) => ({
      key: "project:" + (p.id || p.path || ""),
      render: (had) => {
        const item = railNode(had, "button", "start-recent-item");
        item.type = "button";
        item.dataset.projectId = p.id || "";
        item.dataset.path = p.path || "";
        item.title = p.path || "";
        // A row outlives the repaint now, so both flags have to come off
        // again; they used to go away with the node that carried them.
        if (p.missing) {
          item.dataset.missing = "true";
        } else {
          delete item.dataset.missing;
        }
        if (busyProjectId && p.id === busyProjectId) {
          item.dataset.busy = "true";
        } else {
          delete item.dataset.busy;
        }

        let countText = "";
        if (p.missing) {
          countText = "folder is gone";
        } else if (typeof p.sessions === "number" && p.sessions >= 0) {
          countText = p.sessions === 1 ? "1 chat" : p.sessions + " chats";
        }
        const parts = [
          {
            key: "name",
            render: (h) => {
              const name = railNode(h, "span", "start-recent-name");
              // textContent: the name is a folder name off disk.
              name.textContent = p.name || p.path || "?";
              return name;
            },
          },
          {
            key: "path",
            render: (h) => {
              const path = railNode(h, "span", "start-recent-path");
              path.textContent = p.path || "";
              return path;
            },
          },
        ];
        if (countText) {
          parts.push({
            key: "count",
            render: (h) => {
              const count = railNode(h, "span", "start-recent-count");
              count.textContent = countText;
              return count;
            },
          });
        }
        reconcileByKey(item, parts);
        return item;
      },
    }));
    reconcileByKey(list, entries);
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

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
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
