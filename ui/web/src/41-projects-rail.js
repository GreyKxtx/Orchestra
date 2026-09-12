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
