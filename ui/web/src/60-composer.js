  // ---- the composer's own messages ---------------------------------------
  //
  // Everything the chat input asks for that is not a turn: the model pill, the
  // Orchestra role breakdown, slash commands, @-file mentions, rewind, the
  // pending-diff bar. 10-adapter-session.js routes here before it gives up on
  // a message, so anything handled below stops being "not available in the
  // web UI yet".
  //
  // All of it is core RPC — runtime.list_providers, runtime.set_model,
  // runtime.get_orchestra, session.compact, session.rewind,
  // session.apply_pending, skill.invoke, tool.call — which is why the VS Code
  // host and this one can answer the same renderer with the same shapes. The
  // handful that genuinely cannot work here (opening an editor, a native diff
  // view) still fall through to the note, and say which.

  /**
   * Route one renderer message. Returning false means "not mine", and the
   * caller says so to the user.
   * @param {any} msg @returns {boolean}
   */
  function handleComposerMessage(msg) {
    switch (msg.type) {
      case "listProviderModels":
        void pushProviderModels();
        return true;

      case "setModel":
        void applyModel(String(msg.model || ""), msg.provider);
        return true;

      case "listOrchestraRoles":
        void pushOrchestraRoles();
        return true;

      case "openSettings":
        showRailSettings(true, typeof msg.section === "string" ? msg.section : "general");
        return true;

      case "openOrchestraSettings":
        showRailSettings(true, "general");
        return true;

      case "slashCommand":
        void runSlashCommand(String(msg.cmd || ""), msg.arg);
        return true;

      case "mentionSearch":
        void searchMentions(String(msg.query || ""));
        return true;

      case "rewindToMessage":
        void rewindToMessage(msg.uiIndex);
        return true;

      case "applyPending":
        void settlePending(true);
        return true;

      case "discardPending":
        void settlePending(false);
        return true;

      case "deleteSession":
        void deleteSessionById(String(msg.sessionId || ""));
        return true;

      case "cancelQueuedSend":
        // The VS Code panel queues sends while a turn is in flight and this
        // host does not, so there is never a queued send to cancel. Taking the
        // message keeps a note about a thing that cannot happen off the
        // screen.
        return true;

      default:
        return false;
    }
  }

  /** The connection for the project on screen, or null. */
  function composerConn() {
    return currentProjectId ? connFor(currentProjectId) : null;
  }

  /**
   * Run one core method for the composer. Failures are reported in the chat,
   * because every one of these is something the user just clicked.
   * @param {string} method @param {any} [params] @param {string} [what]
   */
  async function composerRpc(method, params, what) {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return null;
    }
    try {
      return await conn.send(method, params || {});
    } catch (err) {
      toRenderer({
        type: "systemNote",
        text: `[error] ${what || method}: ${String((err && err.message) || err)}`,
      });
      return null;
    }
  }

  /* ---- the model pill --------------------------------------------------- */

  async function pushProviderModels() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "providerModels", providers: [], activeProvider: "", activeModel: "" });
      return;
    }
    const r = (await composerRpc("runtime.list_providers", { probe: true }, "list providers")) || {};
    const providers = Array.isArray(r.providers) ? r.providers : [];
    // The same filter the VS Code panel applies: a provider nobody has
    // configured and that offers no models is noise in a picker.
    const listed = providers
      .filter(
        (p) =>
          p.configured ||
          p.active ||
          (p.ready && (Number(p.model_count) > 0 || (Array.isArray(p.models) ? p.models.length : 0) > 0))
      )
      .map((p) => ({
        key: p.key,
        name: p.name,
        active: p.active,
        ready: p.ready,
        models: p.models,
        models_error: p.models_error,
        model_count: p.model_count,
      }));

    if (listed.length === 0) {
      const fallback = await currentConfigModels();
      if (fallback) {
        listed.push(fallback);
      }
    }

    toRenderer({
      type: "providerModels",
      activeProvider: String(r.active_provider || ""),
      activeModel: String(r.active_model || ""),
      providers: listed,
    });
  }

  /**
   * The models the configuration on disk can actually reach, as one entry.
   *
   * A project may set llm.api_base and llm.model and leave llm.provider empty
   * — it works, and the agent answers — but then nothing in the catalogue is
   * "configured", the core probes nothing, and the picker is left saying "no
   * providers" while the chat is talking to a model. runtime.list_models asks
   * the configured endpoint directly, which is the one question that has an
   * answer here.
   *
   * The entry carries no key, so picking a model from it sends setModel with
   * no provider — a plain change of llm.model, which is what the config is.
   * @returns {Promise<any|null>}
   */
  async function currentConfigModels() {
    const conn = composerConn();
    if (!conn) {
      return null;
    }
    try {
      const [models, llm] = await Promise.all([
        conn.send("runtime.list_models", {}),
        conn.send("runtime.get_llm", {}).catch(() => ({})),
      ]);
      const list = Array.isArray(models && models.models) ? models.models : [];
      if (list.length === 0) {
        return null;
      }
      const base = String((llm && llm.api_base) || "").replace(/^https?:\/\//, "");
      return {
        key: "",
        name: base ? "Configured endpoint · " + base : "Configured endpoint",
        active: true,
        ready: true,
        models: list,
        model_count: list.length,
      };
    } catch (err) {
      return null;
    }
  }

  /** @param {string} model @param {string} [provider] */
  async function applyModel(model, provider) {
    if (!model) {
      return;
    }
    const params = { model, persist: true };
    if (provider) {
      params.provider = provider;
    }
    const r = await composerRpc("runtime.set_model", params, "set model");
    if (!r) {
      return;
    }
    toRenderer({
      type: "systemNote",
      text: `Model: ${r.model || model}${r.persisted ? " (saved)" : ""}`,
    });
    // The composer's pill reads its label off the header message, so the
    // header has to carry the new model — without it the core had saved the
    // change and the pill still showed the old name.
    const st = projectState(currentProjectId);
    toRenderer({
      type: "header",
      sessionId: st.sessionId,
      model: String(r.model || model),
      provider: String(r.provider || provider || ""),
    });
    await pushProviderModels();
  }

  /* ---- the Orchestra pill ------------------------------------------------ */

  async function pushOrchestraRoles() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "orchestraRoles", roles: [], defaultTier: "", error: "No workspace is open." });
      return;
    }
    try {
      const r = (await conn.send("runtime.get_orchestra", {})) || {};
      toRenderer({
        type: "orchestraRoles",
        roles: Array.isArray(r.roles) ? r.roles : [],
        defaultTier: String(r.default_tier || ""),
      });
    } catch (err) {
      // The pill shows the reason in place of the breakdown, so this is not a
      // chat note as well.
      toRenderer({
        type: "orchestraRoles",
        roles: [],
        defaultTier: "",
        error: String((err && err.message) || err),
      });
    }
  }

  /* ---- slash commands ---------------------------------------------------- */

  const SLASH_HELP = [
    "Slash commands:",
    "/clear — new chat",
    "/compact [hint] — compress LLM context",
    "/sessions — switch session (the tabs above)",
    "/model — change model (composer pill)",
    "/settings — Orchestra settings",
    "/<skill-name> args — run a loaded skill",
    "Rewind: hover a user message → ↩ Rewind",
    "@file — mention files in composer",
  ].join("\n");

  /** @param {string} cmd @param {string} [arg] */
  async function runSlashCommand(cmd, arg) {
    const name = cmd.trim().toLowerCase();
    switch (name) {
      case "/clear":
        await startSession(undefined);
        return;
      case "/compact": {
        const r = await composerRpc(
          "session.compact",
          { session_id: projectState(currentProjectId).sessionId, query: arg || "" },
          "compact"
        );
        if (r) {
          toRenderer({ type: "systemNote", text: "Context compacted." });
        }
        return;
      }
      case "/sessions":
        toRenderer({ type: "systemNote", text: "Switch chats from the tabs in the title bar." });
        return;
      case "/model":
        toRenderer({ type: "systemNote", text: "Use the model pill in the composer to change model." });
        return;
      case "/settings":
        showRailSettings(true, "general");
        return;
      case "/help":
        toRenderer({ type: "systemNote", text: SLASH_HELP });
        return;
      case "/rewind":
        toRenderer({
          type: "systemNote",
          text: "Hover a user message and click ↩ Rewind to truncate history to that checkpoint.",
        });
        return;
      default:
        await runSkillCommand(name.replace(/^\//, ""), arg || "");
    }
  }

  /**
   * A slash command that is not built in is a skill name — the same fallback
   * the VS Code panel and the TUI make.
   * @param {string} name @param {string} args
   */
  async function runSkillCommand(name, args) {
    if (!name) {
      return;
    }
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    try {
      const r = (await conn.send("skill.invoke", { skill: name, task: args })) || {};
      toRenderer({
        type: "systemNote",
        text: `[skill:${r.skill || name}] ${r.steps || 0} step(s) · marker=${r.marker || "(no marker)"}\n---\n${r.output || ""}`,
      });
    } catch (err) {
      const message = String((err && err.message) || err);
      // A name that is not a skill is a typo, not a failure worth a stack.
      toRenderer({
        type: "systemNote",
        text: /not found|unknown skill/i.test(message)
          ? `Unknown command: /${name}. Try /help`
          : `[error] skill.invoke: ${message}`,
      });
    }
  }

  /* ---- @file mentions ---------------------------------------------------- */

  /**
   * Files for the @ palette. The VS Code panel asks the editor's own index;
   * here the core's glob tool walks the workspace, which is the same set of
   * files the agent can see — and it already honours the project's excludes.
   * @param {string} query
   */
  async function searchMentions(query) {
    const q = query.trim();
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "mentionResults", query, files: [] });
      return;
    }
    // A bare @ lists the first files it finds; a query matches anywhere in the
    // name, which is what someone typing three letters expects.
    const pattern = q ? "**/*" + q.replace(/\\/g, "/") + "*" : "**/*";
    let files = [];
    try {
      const r = (await conn.send("tool.call", {
        name: "glob",
        input: { pattern, limit: 200 },
      })) || {};
      const raw = Array.isArray(r.files) ? r.files : [];
      files = raw
        .map((f) => String((f && f.path) || ""))
        .filter(Boolean)
        .map((p) => {
          const norm = p.replace(/\\/g, "/");
          const name = norm.slice(norm.lastIndexOf("/") + 1);
          const dot = name.lastIndexOf(".");
          return { name, path: p, ext: dot > 0 ? name.slice(dot + 1).toLowerCase() : "" };
        })
        // Name matches first, then path matches, each already in glob's order.
        .sort((a, b) => {
          const an = q ? (a.name.toLowerCase().includes(q.toLowerCase()) ? 0 : 1) : 0;
          const bn = q ? (b.name.toLowerCase().includes(q.toLowerCase()) ? 0 : 1) : 0;
          return an - bn;
        })
        .slice(0, 40);
    } catch (err) {
      // An empty palette is the honest answer; the composer stays usable.
      files = [];
    }
    toRenderer({ type: "mentionResults", query, files });
  }

  /* ---- rewind and the pending bar ---------------------------------------- */

  /** @param {any} uiIndex */
  async function rewindToMessage(uiIndex) {
    if (typeof uiIndex !== "number" || uiIndex < 0) {
      return;
    }
    const st = projectState(currentProjectId);
    if (!st.sessionId) {
      return;
    }
    const r = await composerRpc(
      "session.rewind",
      { session_id: st.sessionId, ui_index: uiIndex },
      "rewind"
    );
    if (!r) {
      return;
    }
    // The transcript is now a prefix of what is on screen; repaint it from the
    // core rather than trying to trim the DOM to match.
    const view = await composerRpc("session.get", { session_id: st.sessionId }, "reload history");
    if (!view) {
      return;
    }
    toRenderer({ type: "clearMessages" });
    toRenderer({ type: "history", messages: view.ui_messages || [] });
    toRenderer({ type: "header", sessionId: st.sessionId });
  }

  /** @param {boolean} apply true applies the staged changes, false discards. */
  async function settlePending(apply) {
    const st = projectState(currentProjectId);
    const method = apply ? "session.apply_pending" : "session.discard_pending";
    const r = await composerRpc(
      method,
      { session_id: st.sessionId },
      apply ? "apply changes" : "discard changes"
    );
    if (!r) {
      return;
    }
    toRenderer({ type: "pending", files: [] });
    toRenderer({
      type: "systemNote",
      text: apply ? "Changes applied." : "Changes discarded.",
    });
  }

  /* ---- deleting a chat ---------------------------------------------------- */

  /**
   * Close a session for good. There is no confirm: the VS Code panel raises a
   * modal, and this host's only modal dialog is Tauri's, which the page is
   * deliberately not allowed to open — see openFromStart. The session list
   * repaints either way, so a mistaken click is visible immediately.
   * @param {string} sessionId
   */
  async function deleteSessionById(sessionId) {
    if (!sessionId) {
      return;
    }
    const projectId = currentProjectId;
    const st = projectState(projectId);
    const wasActive = st.sessionId === sessionId;
    const r = await composerRpc("session.close", { session_id: sessionId }, "delete chat");
    if (!r) {
      return;
    }
    if (wasActive) {
      await startSession(undefined);
    }
    await refreshSessionList(projectId);
    if (projectId === currentProjectId) {
      renderProjects();
    }
  }
