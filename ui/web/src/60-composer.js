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

      case "forkFromMessage":
        void forkFromMessage(msg.uiIndex);
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

      case "attach":
        void pickAttachments();
        return true;

      case "attachBytes":
        void storeAttachmentBytes(msg);
        return true;

      default:
        return false;
    }
  }

  /* ---- attachments -------------------------------------------------------- */
  //
  // The paperclip posts "attach"; dropping or pasting a file posts
  // "attachBytes" with its contents. The editor's host answers the first
  // with the editor's file dialog and the second by writing the bytes into
  // the workspace itself (ui/vscode/src/chat/panel.ts). This host has neither
  // a dialog of its own nor a filesystem: the desktop shell lends it a native
  // picker (Tauri's dialog plugin), a browser has <input type=file>, and the
  // bytes go to the core — attachments.store keeps them under
  // .orchestra/attachments, inside the workspace, where a turn can read them.

  const IMAGE_ATTACHMENT_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "avif", "svg"]);
  /** An image this small travels as a data: URL so the chip can show it. */
  const ATTACHMENT_PREVIEW_MAX_BYTES = 2 * 1024 * 1024;

  /** @param {string} ext */
  function attachmentKind(ext) {
    return IMAGE_ATTACHMENT_EXTS.has(String(ext || "").toLowerCase()) ? "image" : "file";
  }

  /** The renderer's file chip for a path on disk. @param {string} p */
  function attachmentRefFromPath(p) {
    const norm = String(p || "").replace(/\\/g, "/");
    const name = norm.slice(norm.lastIndexOf("/") + 1) || norm;
    const dot = name.lastIndexOf(".");
    const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
    return { name, path: p, ext: ext || undefined, kind: attachmentKind(ext) };
  }

  /**
   * Whether p lies inside root. Paths from the desktop's picker are absolute
   * and spelled as the OS spells them; both sides are folded to forward
   * slashes and, when a drive letter says this is Windows, to one case.
   * @param {string} root @param {string} p
   */
  function pathInsideWorkspace(root, p) {
    let r = String(root || "").replace(/\\/g, "/").replace(/\/+$/, "");
    let q = String(p || "").replace(/\\/g, "/");
    if (!r || !q) {
      return false;
    }
    if (/^[a-z]:/i.test(r)) {
      r = r.toLowerCase();
      q = q.toLowerCase();
    }
    return q === r || q.startsWith(r + "/");
  }

  function workspaceRootNow() {
    return currentProjectId ? projectState(currentProjectId).workspaceRoot || "" : "";
  }

  async function pickAttachments() {
    if (!composerConn()) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    const t = window.__TAURI__;
    if (t && t.dialog && t.dialog.open) {
      let picked;
      try {
        picked = await t.dialog.open({
          multiple: true,
          directory: false,
          title: "Attach files",
          defaultPath: workspaceRootNow() || undefined,
        });
      } catch (err) {
        toRenderer({ type: "systemNote", text: `[error] attach: ${String((err && err.message) || err)}` });
        return;
      }
      const paths = (Array.isArray(picked) ? picked : picked ? [picked] : [])
        .map((p) => String(p || ""))
        .filter(Boolean);
      if (!paths.length) {
        return; // cancelled
      }
      // A file inside the workspace is attached where it is — the agent then
      // reads and edits the real file, not a copy. One outside it cannot be
      // read from here: the shell grants this page a picker, not the disk.
      const root = workspaceRootNow();
      const inside = paths.filter((p) => pathInsideWorkspace(root, p));
      const outside = paths.filter((p) => !pathInsideWorkspace(root, p));
      if (inside.length) {
        toRenderer({ type: "filesPicked", files: inside.map(attachmentRefFromPath) });
      }
      if (outside.length) {
        const names = outside.map((p) => attachmentRefFromPath(p).name).join(", ");
        toRenderer({
          type: "systemNote",
          text:
            `Not attached — outside the workspace: ${names}. ` +
            "Drag the file into the chat or paste it instead; a copy is kept in .orchestra/attachments.",
        });
      }
      return;
    }
    // A browser: the page's own file input, made for this click and removed
    // after it. Its files come as bytes, which is the attachBytes path.
    if (!document.body || !document.body.appendChild) {
      return;
    }
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = true;
    input.hidden = true;
    input.addEventListener("change", () => {
      const files = Array.from(input.files || []);
      if (input.remove) {
        input.remove();
      }
      for (const file of files) {
        void storeAttachmentFile(file);
      }
    });
    document.body.appendChild(input);
    input.click();
  }

  /** @param {File} file */
  async function storeAttachmentFile(file) {
    if (!file) {
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      toRenderer({ type: "systemNote", text: `Skipped ${file.name || "file"}: exceeds 20 MB limit` });
      return;
    }
    let dataBase64 = "";
    try {
      dataBase64 = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => {
          const s = String(reader.result || "");
          resolve(s.slice(s.indexOf(",") + 1));
        };
        reader.onerror = () => reject(reader.error || new Error("read failed"));
        reader.readAsDataURL(file);
      });
    } catch (err) {
      toRenderer({ type: "systemNote", text: `[error] attach ${file.name || "file"}: ${String((err && err.message) || err)}` });
      return;
    }
    await storeAttachmentBytes({ name: file.name || "attachment", mime: file.type || undefined, dataBase64 });
  }

  /**
   * Bytes to the core, an attachment back. The chip gets a data: preview for
   * a small image — the bytes are right here, and this host serves no files.
   * @param {{name?: string, mime?: string, dataBase64?: string}} msg
   */
  async function storeAttachmentBytes(msg) {
    const dataBase64 = String((msg && msg.dataBase64) || "");
    if (!dataBase64) {
      return;
    }
    const name = String((msg && msg.name) || "attachment");
    const mime = msg && msg.mime ? String(msg.mime) : "";
    const params = { name, data_base64: dataBase64 };
    if (mime) {
      params.mime = mime;
    }
    const r = await composerRpc("attachments.store", params, "attach");
    if (!r || !r.path) {
      return; // composerRpc has already said what went wrong
    }
    const ref = {
      name: String(r.name || name),
      path: String(r.path),
      ext: r.ext ? String(r.ext) : undefined,
      kind: r.kind === "image" ? "image" : "file",
    };
    if (ref.kind === "image" && mime.startsWith("image/") && dataBase64.length <= (ATTACHMENT_PREVIEW_MAX_BYTES * 4) / 3) {
      ref.previewUri = `data:${mime};base64,${dataBase64}`;
    }
    toRenderer({ type: "filesPicked", files: [ref] });
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
    // The pill reads its label off the header message and the gauge its
    // ceiling off contextInfo. Both come from the core's own answer, which
    // now names the new model and the window that goes with it — without
    // this the core had saved the change and the pill still showed the old
    // name over the old window.
    await pushLLMInfo(currentProjectId);
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
    "/search text — find text across saved chats",
    "/sessions — open the list of chats",
    "/model — open the model menu",
    "/workflows — list this workspace's workflows",
    "/workflow name [args] — run one",
    "/settings — Orchestra settings",
    "/<command> args — run one of this workspace's own commands",
    "Rewind: hover a user message → ↩ Rewind",
    "Branch: hover a user message → ⑂ Branch",
    "Delete a chat: hover it in the sidebar → ×",
    "@file — mention files in composer",
  ].join("\n");

  /** What "/" already means, so a workspace command of the same name is not
   * offered twice — the same rule the editor's skillSlashNames applies. */
  const BUILTIN_SLASH_NAMES = [
    "clear",
    "compact",
    "help",
    "model",
    "rewind",
    "search",
    "sessions",
    "settings",
    "workflow",
    "workflows",
  ];

  /**
   * This workspace's own commands — skills under .orchestra/skills and
   * ~/.orchestra/skills, plus .claude/commands — into the "/" palette. Only
   * the core can enumerate them, and without this the palette offered the
   * seven built-ins and nothing else, so a project's own command could only
   * be run by typing its whole name and hoping.
   * @param {string} projectId
   */
  async function pushSkillCommands(projectId) {
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      return;
    }
    try {
      const r = (await conn.send("skill.list", {})) || {};
      if (projectId !== currentProjectId) {
        return;
      }
      const skills = (Array.isArray(r.skills) ? r.skills : [])
        .map((s) => ({
          name: String((s && s.name) || "").trim(),
          description: String((s && s.description) || ""),
        }))
        .filter((s) => s.name && BUILTIN_SLASH_NAMES.indexOf(s.name.toLowerCase()) === -1);
      toRenderer({ type: "skillsList", skills });
    } catch (err) {
      // A workspace with no commands — or a core that cannot list them —
      // simply leaves the palette with its built-ins.
    }
  }

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
      // These two used to answer with a sentence about where the control is,
      // which reads as a command that does nothing. They open it instead.
      case "/sessions": {
        // The sidebar is where the web keeps the list; the header's button
        // for it is hidden here, so fall back to it only if there is no rail.
        if (revealSessionList()) {
          return;
        }
        const btn = document.getElementById("session-history-btn");
        if (btn && btn.click) {
          btn.click();
          return;
        }
        toRenderer({ type: "systemNote", text: "Switch chats from the tabs in the title bar." });
        return;
      }
      case "/model": {
        const pill = document.getElementById("model-pill");
        if (pill && pill.click) {
          pill.click();
          return;
        }
        toRenderer({ type: "systemNote", text: "Use the model pill in the composer to change model." });
        return;
      }
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
      case "/search":
        await searchSessions(arg || "");
        return;
      case "/workflows":
        await listWorkflows();
        return;
      case "/workflow":
        await runWorkflow(arg || "");
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
      // name + arguments: what internal/core/skill.go reads, and what the
      // editor host sends. Any other spelling arrives as an empty name and
      // the command fails before it starts.
      const r = (await conn.send("skill.invoke", { name, arguments: args })) || {};
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

  /* ---- searching saved chats --------------------------------------------- */

  /**
   * Full-text search across this workspace's saved sessions. The core has
   * shipped session.search since protocol v14 and no interface ever called
   * it, so a conversation you remembered having could only be found by
   * opening chats one at a time.
   *
   * Results land in the sidebar, where the chats already live and a row is
   * already clickable — a list of titles in a chat note would name them
   * without being able to open them.
   * @param {string} query
   */
  async function searchSessions(query) {
    const q = String(query || "").trim();
    if (!q) {
      showSessionSearch("");
      toRenderer({ type: "systemNote", text: "/search text — find text across this workspace's chats." });
      return;
    }
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    await showSessionSearch(q);
  }

  /* ---- workflows --------------------------------------------------------- */

  /** The workspace's multi-stage workflows, which until now ran only from the CLI. */
  async function listWorkflows() {
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    let rows = [];
    try {
      const r = (await conn.send("workflow.list", {})) || {};
      rows = Array.isArray(r.workflows) ? r.workflows : [];
    } catch (err) {
      toRenderer({ type: "systemNote", text: `[error] workflow.list: ${String((err && err.message) || err)}` });
      return;
    }
    if (rows.length === 0) {
      toRenderer({
        type: "systemNote",
        text: "No workflows in this workspace. They live in .orchestra/workflows.",
      });
      return;
    }
    const lines = rows.map((w) => {
      const name = String((w && w.name) || "").trim();
      const desc = String((w && w.description) || "").trim();
      const stages = Array.isArray(w && w.stages) ? w.stages.length : 0;
      return `/workflow ${name} — ${desc || "(no description)"} · ${stages} stage(s)`;
    });
    toRenderer({ type: "systemNote", text: ["Workflows:", ...lines].join("\n") });
  }

  /**
   * Runs one workflow. Everything after the name is its argument, so
   * "/workflow review the auth package" runs "review" with "the auth package".
   * @param {string} arg
   */
  async function runWorkflow(arg) {
    const raw = String(arg || "").trim();
    if (!raw) {
      await listWorkflows();
      return;
    }
    const space = raw.search(/\s/);
    const name = space === -1 ? raw : raw.slice(0, space);
    const args = space === -1 ? "" : raw.slice(space + 1).trim();
    const conn = composerConn();
    if (!conn) {
      toRenderer({ type: "systemNote", text: "No workspace is open." });
      return;
    }
    toRenderer({ type: "systemNote", text: `Running workflow "${name}"…` });
    try {
      // Workflows run stage by stage against the model, so this waits for as
      // long as the run takes; the socket call carries no deadline of its own.
      const r = (await conn.send("workflow.run", { name, arguments: args })) || {};
      const stages = Array.isArray(r.stages) ? r.stages : [];
      const took = Number(r.duration_ms) || 0;
      const head = r.failure_reason
        ? `[workflow:${r.name || name}] stopped at ${r.final_stage || "?"} — ${r.failure_reason}`
        : `[workflow:${r.name || name}] done in ${Math.round(took / 1000)}s`;
      const body = stages.map((s) => `  ${s.stage_id}${s.attempt > 1 ? ` (attempt ${s.attempt})` : ""} → ${s.action}${s.marker ? ` · ${s.marker}` : ""}`);
      toRenderer({ type: "systemNote", text: [head, ...body].join("\n") });
    } catch (err) {
      const message = String((err && err.message) || err);
      toRenderer({
        type: "systemNote",
        text: /not found|unknown workflow/i.test(message)
          ? `Unknown workflow: ${name}. Try /workflows`
          : `[error] workflow.run: ${message}`,
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
    // ui_message_index is what internal/core reads (SessionRewindParams), and
    // what the editor host and the TUI send. Any other spelling decodes as 0,
    // which silently rewinds the whole chat to its first message.
    const r = await composerRpc(
      "session.rewind",
      { session_id: st.sessionId, ui_message_index: uiIndex },
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

  /**
   * Branch a new chat from a checkpoint, leaving this one intact. The core has
   * had session.fork since protocol v14 and only the TUI ever called it, so in
   * the app the only way to revisit a decision was rewind — which throws the
   * rest of the conversation away.
   * @param {number} uiIndex
   */
  async function forkFromMessage(uiIndex) {
    // Index 0 is refused by the core: the branch is everything BEFORE the
    // point, so it would be an empty chat.
    if (typeof uiIndex !== "number" || uiIndex < 1) {
      return;
    }
    const projectId = currentProjectId;
    const st = projectState(projectId);
    if (!st.sessionId) {
      return;
    }
    // ui_message_index, exclusive, pointing at a user message — the same
    // index rewind takes (SessionForkParams).
    const r = await composerRpc(
      "session.fork",
      { session_id: st.sessionId, ui_message_index: uiIndex },
      "branch"
    );
    const branchId = String((r && r.session_id) || "");
    if (!branchId) {
      return;
    }
    await openSessionRow(projectId, branchId);
    toRenderer({
      type: "systemNote",
      text: "Branched: this chat has everything up to that message, and the original is untouched. Ask it differently here.",
    });
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
