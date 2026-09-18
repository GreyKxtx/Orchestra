  // ---- the rail and the header's tab strip -----------------------------------
  //
  // Split out of 40-projects.js, which had grown to 2035 lines carrying four
  // jobs behind comment banners. The bundler concatenates these fragments
  // numerically into one IIFE, so everything 40 declares is in scope here and
  // the startup block still runs last.

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

  /* The plus, from the one icon set — see media/icons.js. It used to be
     hand-drawn here at 13px and stroke 2.4, beside 15px/2 and 18px/2.2
     copies elsewhere on the same screen. */
  const PLUS_SVG = orchIconMarkup("plus", { size: "sm" });

  /**
   * The button that starts a session, on the pane's heading. (The one that
   * adds a workspace is static markup at the foot of the strip; both reach
   * the same delegated handler through data-rail-action.)
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
    btn.title = i18n("rail.delete_chat");
    btn.setAttribute("aria-label", i18n("rail.delete_chat"));
    btn.textContent = "×";
    let armed = 0;
    btn.addEventListener("click", (e) => {
      if (e && e.stopPropagation) e.stopPropagation();
      if (e && e.preventDefault) e.preventDefault();
      if (!armed) {
        armed = 1;
        btn.classList.add("armed");
        btn.textContent = i18n("rail.delete_confirm");
        btn.title = i18n("rail.delete_confirm_title");
        setTimeout(() => {
          if (!armed) return;
          armed = 0;
          btn.classList.remove("armed");
          btn.textContent = "×";
          btn.title = i18n("rail.delete_chat");
        }, 4000);
        return;
      }
      armed = 0;
      void deleteSession(projectId, sessionId);
    });
    return btn;
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
    box.placeholder = i18n("rail.search_chats");
    box.setAttribute("aria-label", i18n("rail.search_aria"));
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

  // ---- repainting without throwing the rail away ---------------------------

  /**
   * Put `entries` under `parent`, in order, reusing the nodes already there.
   *
   * The rail repaints on every status flip, and a running turn flips status
   * constantly, so emptying the list first — which is what it used to do —
   * cost the user real things. The keyboard focus came off whatever chip they
   * were on. The chat-search box emptied mid-word: its value only reaches
   * sessionSearch after a 200 ms debounce, so a repaint inside that window
   * threw the keystrokes away. And the delete button's "click again to
   * confirm" lives in a closure, so an armed one silently disarmed.
   *
   * Matching by key keeps every node that is still wanted, along with its
   * focus, its caret, its listeners and whatever those listeners closed over.
   * Each entry renders from the node it had last time, or from nothing on the
   * first paint; a key no entry claims is removed.
   *
   * @param {HTMLElement} parent
   * @param {{key: string, render: (existing: any) => any}[]} entries
   */
  function reconcileByKey(parent, entries) {
    const spare = new Map();
    for (const node of Array.prototype.slice.call(parent.children)) {
      const key = node.dataset && node.dataset.railKey;
      if (key) {
        spare.set(key, node);
      } else {
        // Written by something other than this function, so there is no key
        // to match it by and no way to know what it was for.
        parent.removeChild(node);
      }
    }
    let cursor = parent.firstChild;
    for (const entry of entries) {
      const node = entry.render(spare.get(entry.key) || null);
      spare.delete(entry.key);
      node.dataset.railKey = entry.key;
      if (node === cursor) {
        cursor = node.nextSibling;
        continue;
      }
      // Either new, or reused from further down the list: insertBefore moves
      // a node that already has a parent, which is what makes reordering work.
      parent.insertBefore(node, cursor);
    }
    for (const node of spare.values()) {
      if (node.parentNode === parent) {
        parent.removeChild(node);
      }
    }
  }

  /**
   * Reuse, or make one. Class names never change between repaints of the same
   * key, so they are written once, at birth.
   * @param {any} existing @param {string} tag @param {string} cls
   */
  function railNode(existing, tag, cls) {
    if (existing) {
      return existing;
    }
    const node = document.createElement(tag);
    node.className = cls;
    return node;
  }

  /**
   * One chat row: the button that opens it and the one that throws it away.
   * `snippet`, present only on a search result, is the matching line.
   * @typedef {{id: string, label: string, age: string, tooltip: string, snippet?: string}} RailSessionInfo
   * @param {string} projectId @param {RailSessionInfo} info @param {boolean} active @param {any} existing
   */
  function railSessionRow(projectId, info, active, existing) {
    const rowEl = railNode(existing, "div", "rail-session-row");
    // A button cannot nest in a button, so the row is a pair.
    reconcileByKey(rowEl, [
      { key: "open", render: (had) => railSessionButton(projectId, info, active, had) },
      { key: "del", render: (had) => had || railDeleteButton(projectId, info.id) },
    ]);
    return rowEl;
  }

  /** @param {string} projectId @param {RailSessionInfo} info @param {boolean} active @param {any} existing */
  function railSessionButton(projectId, info, active, existing) {
    const item = railNode(existing, "button", "rail-session");
    item.type = "button";
    item.dataset.projectId = projectId;
    item.dataset.sessionId = info.id;
    item.dataset.active = active ? "true" : "false";
    item.title = info.tooltip;
    const parts = [
      {
        key: "title",
        render: (had) => {
          const t = railNode(had, "span", "rail-session-title");
          // textContent: titles are the user's own first message.
          t.textContent = info.label;
          return t;
        },
      },
      {
        key: "time",
        render: (had) => {
          const a = railNode(had, "span", "rail-session-time");
          a.textContent = info.age;
          return a;
        },
      },
    ];
    if (info.snippet) {
      parts.push({
        key: "snippet",
        render: (had) => {
          const line = railNode(had, "span", "rail-session-snippet");
          // textContent: a snippet is the user's or the model's own words.
          line.textContent = info.snippet;
          return line;
        },
      });
    }
    reconcileByKey(item, parts);
    return item;
  }

  /**
   * A stable hue per workspace, from its PATH. Three folders all called `ws`
   * are three different projects, and the name alone cannot tell them apart;
   * the path can, and a colour derived from it does so at a glance.
   * @param {string} path @returns {number} 0..359
   */
  function projectHue(path) {
    let h = 0;
    const str = String(path || "");
    for (let i = 0; i < str.length; i++) {
      h = (h * 31 + str.charCodeAt(i)) >>> 0;
    }
    return h % 360;
  }

  /** The one character on a tile: the first letter or digit of the name. */
  function projectGlyph(name) {
    const m = String(name || "").trim().match(/[\p{L}\p{N}]/u);
    return m ? m[0].toUpperCase() : "?";
  }

  /**
   * One workspace on the strip: a tile carrying its initial, tinted by its
   * path, with the state badge on its corner and the active marker on its
   * edge. It keeps the .project-chip class and its data attributes: every
   * listener in 42-projects-chrome.js and every test is bound to those.
   * @param {any} row @param {any} existing
   */
  function railProjectChip(row, existing) {
    const chip = railNode(existing, "button", "project-chip");
    chip.type = "button";
    chip.setAttribute("role", "tab");
    chip.dataset.projectId = row.id;
    chip.dataset.state = row.state;
    chip.dataset.status = row.status;
    chip.dataset.active = row.active ? "true" : "false";
    chip.setAttribute("aria-selected", row.active ? "true" : "false");
    // The tile shows one letter, so the tooltip carries what it cannot: the
    // name, the path, and whether the project is waiting for an answer.
    chip.title =
      (row.name || "?") + "\n" + (row.path || "") + (row.status === "asking" ? i18n("rail.waiting") : "");
    chip.setAttribute("aria-label", row.name + " (" + row.status + ")");
    if (railOpeningId && row.id === railOpeningId) {
      chip.dataset.opening = "true";
    } else {
      // A chip outlives the repaint now, so the flag has to be taken off
      // again; it used to go away with the node that carried it.
      delete chip.dataset.opening;
    }
    // Through the CSSOM, never a style attribute in markup: both hosts serve
    // this page under a CSP without 'unsafe-inline'. The stub document the
    // adapter tests run in has no setProperty, hence the guard.
    if (chip.style && chip.style.setProperty) {
      chip.style.setProperty("--project-hue", String(projectHue(row.path)));
    }
    reconcileByKey(chip, [
      {
        key: "glyph",
        render: (had) => {
          const g = railNode(had, "span", "project-glyph");
          g.setAttribute("aria-hidden", "true");
          // textContent: the name is a folder name off disk.
          g.textContent = projectGlyph(row.name);
          return g;
        },
      },
      { key: "dot", render: (had) => railNode(had, "span", "project-dot") },
    ]);
    return chip;
  }

  /** The chats under one workspace — or its search results, which replace them. */
  function railProjectSessions(row, existing) {
    const sessions = railNode(existing, "div", "project-sessions");
    const listed = sessionsByProject.get(row.id) || [];
    const openSessionId = (peekProjectState(row.id) || {}).sessionId || "";
    const entries = [];
    if (row.active) {
      entries.push({
        key: "search",
        render: (had) => {
          if (!had) {
            return railSessionSearchBox(row.id);
          }
          // Keep the box in step with the state — /search in the composer
          // sets the query from outside — but never while the user is in it:
          // their keystrokes only reach sessionSearch after the debounce, and
          // writing the older value back over them is the exact data loss
          // this reconciliation exists to stop.
          const want = sessionSearch.projectId === row.id ? sessionSearch.query : "";
          if (document.activeElement !== had && had.value !== want) {
            had.value = want;
          }
          return had;
        },
      });
    }

    // Searching replaces the list rather than sitting beside it: the whole
    // point is to narrow a hundred chats to the three that mention a thing.
    const searching = row.active && sessionSearch.projectId === row.id && sessionSearch.hits !== null;
    if (searching) {
      const hits = sessionSearch.hits || [];
      if (hits.length === 0) {
        entries.push({
          key: "no-hits",
          render: (had) => {
            const empty = railNode(had, "div", "rail-sessions-empty");
            empty.textContent = i18n("rail.no_match", { q: sessionSearch.query });
            return empty;
          },
        });
      }
      // Drawn as ordinary chat rows, so the rail's existing click handler
      // opens them, with the matching line underneath.
      for (const h of hits) {
        const id = String((h && h.session_id) || "");
        const snippet = String((h && h.snippet) || "").trim();
        const info = {
          id,
          label: String((h && h.title) || "") || id || "untitled",
          age: sessionAge({ id, updated_at: h && h.updated_at }),
          tooltip: snippet,
          snippet,
        };
        entries.push({
          key: "hit:" + id,
          render: (had) => railSessionRow(row.id, info, id === openSessionId, had),
        });
      }
      reconcileByKey(sessions, entries);
      return sessions;
    }

    // session.list only returns sessions that have been written to disk, and
    // a session is written by its first message — so the session the user is
    // looking at is missing from the list until they say something. Showing
    // it anyway is the difference between "where am I" and an empty sidebar.
    const openIsListed = listed.some((s) => s.id === openSessionId);
    if (row.active && openSessionId && !openIsListed) {
      const info = {
        id: openSessionId,
        label: i18n("rail.new_session_label"),
        age: i18n("rail.now"),
        tooltip: openSessionId,
      };
      entries.push({ key: "new", render: (had) => railSessionButton(row.id, info, true, had) });
    }
    if (listed.length === 0 && !(row.active && openSessionId)) {
      entries.push({
        key: "empty",
        render: (had) => {
          const empty = railNode(had, "div", "rail-sessions-empty");
          empty.textContent = i18n("rail.no_sessions");
          return empty;
        },
      });
    }
    for (const s of listed.slice(0, 100)) {
      const info = {
        id: s.id || "",
        label: s.title || s.id || "untitled",
        age: sessionAge(s),
        tooltip: s.title || s.id || "",
      };
      const active = Boolean(row.active && s.id === openSessionId);
      entries.push({
        key: "row:" + info.id,
        render: (had) => railSessionRow(row.id, info, active, had),
      });
    }
    reconcileByKey(sessions, entries);
    return sessions;
  }

  /**
   * The pane's heading: which workspace these chats belong to, in full — the
   * name, the path, how many there are — and the button that starts one. The
   * tile on the strip can only show a letter; this is where the rest goes.
   * @param {any} row @param {any} existing
   */
  function railPaneHead(row, existing) {
    const head = railNode(existing, "div", "rail-pane-head");
    // The open workspace prefers its live list, so starting a session bumps
    // the count straight away instead of at the next poll.
    const listedNow = sessionsByProject.get(row.id);
    const count = listedNow ? listedNow.length : row.sessions;
    const titleParts = [
      {
        key: "name",
        render: (h) => {
          const t = railNode(h, "span", "rail-pane-title");
          // textContent: a folder name off disk.
          t.textContent = row.name || "?";
          return t;
        },
      },
    ];
    if (typeof count === "number" && count >= 0) {
      titleParts.push({
        key: "count",
        render: (h) => {
          const c = railNode(h, "span", "rail-pane-count");
          c.textContent = i18n(count === 1 ? "rail.chats_one" : "rail.chats_n", { n: count });
          return c;
        },
      });
    }
    reconcileByKey(head, [
      {
        key: "titles",
        render: (had) => {
          const box = railNode(had, "div", "rail-pane-titles");
          reconcileByKey(box, [
            {
              key: "title-row",
              render: (h) => {
                const r = railNode(h, "div", "rail-pane-title-row");
                reconcileByKey(r, titleParts);
                return r;
              },
            },
            {
              key: "path",
              render: (h) => {
                // The box runs right-to-left so the ellipsis eats the START
                // of a long path; the text inside is isolated back to LTR so
                // the path itself still reads the right way round.
                const pth = railNode(h, "div", "rail-pane-path");
                pth.title = row.path || "";
                reconcileByKey(pth, [
                  {
                    key: "text",
                    render: (hh) => {
                      const t = railNode(hh, "span", "rail-pane-path-text");
                      t.textContent = row.path || "";
                      return t;
                    },
                  },
                ]);
                return pth;
              },
            },
          ]);
          return box;
        },
      },
      {
        key: "add",
        render: (had) => had || railAddButton("new-session", i18n("rail.new_session"), row.id),
      },
    ]);
    return head;
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

    // The strip: one tile per workspace, in the registry's own order.
    const strip = document.getElementById("project-strip");
    if (strip) {
      reconcileByKey(
        strip,
        rows.map((row) => ({ key: "chip:" + row.id, render: (had) => railProjectChip(row, had) }))
      );
    }

    // The pane: the open workspace's chats under a heading that names it.
    // Only the project on screen has a session list — the core is asked for
    // one per connection — which is why there is one pane and not a tree.
    const list = document.getElementById("project-rail-list");
    if (!list) {
      return;
    }
    const activeRow = rows.find((r) => r.active) || null;
    const entries = [];
    if (activeRow) {
      entries.push({ key: "head", render: (had) => railPaneHead(activeRow, had) });
      entries.push({ key: "sessions", render: (had) => railProjectSessions(activeRow, had) });
    } else {
      entries.push({
        key: "none",
        render: (had) => {
          const empty = railNode(had, "div", "rail-pane-none");
          empty.textContent = i18n("rail.pick_project");
          return empty;
        },
      });
    }
    reconcileByKey(list, entries);
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
        title: (s.title || i18n("chrome.new_chat")).trim() || i18n("chrome.new_chat"),
        model: s.model,
        msg_count: s.msg_count,
      }));
    // session.list only returns sessions already written to disk, and a
    // session is written by its first message — so the one being looked at
    // has no row until the user says something. Lead with it anyway.
    const shown = openSessionId && !hiddenTabsFor(currentProjectId || "").has(openSessionId);
    if (shown && !tabs.some((t) => t.id === openSessionId)) {
      tabs.unshift({ id: openSessionId, title: i18n("chrome.new_chat") });
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
      toRenderer({ type: "error", message: i18n("rail.delete_no_workspace") });
      return;
    }
    try {
      await conn.send("session.close", { session_id: sessionId });
    } catch (err) {
      toRenderer({
        type: "error",
        message: i18n("rail.delete_failed", { detail: String((err && err.message) || err) }),
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
   * rail is where the list lives — so unfold the rail and scroll to the chat
   * on screen. Answers false where there is no
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
    renderProjects();
    const active = list.querySelector ? list.querySelector('.rail-session[data-active="true"]') : null;
    if (active && active.scrollIntoView) {
      active.scrollIntoView({ block: "nearest" });
    }
    return true;
  }
