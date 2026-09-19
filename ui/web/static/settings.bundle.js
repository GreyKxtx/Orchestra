/* AUTO-GENERATED — do not edit. Sources: ui/vscode/media/settings-src/* + ui/web/src/settings/frame.js  →  node ui/web/scripts/bundle-settings-web.mjs */
//@ts-check
/* Generated from media/settings-src — edit fragments there, then: npm run bundle:webview */
(function () {
  // Everything the user reads goes through i18n(). Two languages now, more later:
  // a language is one more object in CATALOGUE plus one more entry in
  // UI_LANGUAGES, and nothing else changes.
  //
  // This file is not part of chat-src or settings-src: both bundles include it,
  // on both hosts, so the chat window and the settings panel read one catalogue
  // and a string that moves between them keeps its key.
  //
  // Why a catalogue compiled into the bundle rather than files fetched at
  // runtime: the VS Code webview has no network of its own and the browser page
  // is served under a CSP with default-src 'none', so a fetch would be blocked
  // in one host and, in the other, would paint one frame in the wrong language
  // before the answer arrived.
  //
  // English is the fallback: a key missing from another language falls back to
  // it rather than showing the key, so a half-translated language degrades to
  // a readable screen instead of "composer.access.hint".

  const I18N_FALLBACK_LANG = "en";
  /** The languages offered, in menu order. */
  const UI_LANGUAGES = [
    { id: "en", label: "English" },
    { id: "ru", label: "Русский" },
  ];

  const I18N_CATALOGUE = {
    en: {
      "mode.group.core": "Core",
      "mode.group.more": "More",

      "access.section": "Access",
      "access.ask.hint": "Shell with confirmation; edits go through Accept/Reject",
      "access.auto.hint": "Shell, and file writes land on disk immediately (no Accept/Reject)",
      "access.note": "Ask: edits are staged for Accept/Reject. Auto: edits are written straight to disk.",
      "access.tools.section": "Tools",
      "access.browser.label": "Browser",
      "access.browser.hint":
        "The agent may open pages, click and type in a browser (Playwright). Not available under Fast.",
      "access.browser.on": "{hint} · browser on",

      "turn.working": "Working…",
      "turn.running_tools": "Running tools…",
      "turn.queued": " · {n} queued",
      "turn.tasks_done": "✓ Tasks done",

      "tool.body.lines": "{n} lines",
      "tool.body.copy": "Copy",
      "tool.body.copied": "Copied",
      "tool.body.copy_failed": "Copy failed",
      "tool.body.raw": "Raw",
      "tool.body.pretty": "Formatted",
      "tool.body.show_all": "Show all {n} lines",
      "tool.body.collapse": "Collapse",
      "tool.body.capped": "First {n} lines — the rest is too long to show",

      "diff.loading": "Loading diff preview…",
      "diff.more_lines": "… {n} more changed lines",
      "diff.open_file": "Open file (Shift+click: side-by-side diff)",
      "diff.keep": "Keep",
      "diff.drop": "Drop",
      "diff.keep_title": "Apply just this file (a)",
      "diff.drop_title": "Reject just this file (x)",

      "conn.connecting": "Connecting…",
      "conn.reconnecting": "Reconnecting…",
      "conn.reconnecting_n": "Reconnecting… ({n})",
      "conn.lost": "lost the connection to the core — reload the page to try again",
      "conn.reopen_failed": "Reconnected, but this chat could not be read back: {detail}",

      "notice.turn_interrupted":
        "The previous turn was interrupted (the process died). History is kept up to the last completed step.",
      "notice.background_turn_done": "A background turn finished — history has been refreshed.",
      "notice.background_turn_running":
        "A previous turn of this session is still finishing in a background process. History will refresh when it does.",
      "notice.bad_stream":
        "The model returned a malformed stream instead of calling edit/write. Try again, make the request more specific, or switch model in the composer.",
      "notice.ui_sync_failed": "ui_sync failed — the last answer may not be saved to history: {detail}",
      "notice.session_busy":
        "session is busy: the previous turn is still running. Press Stop to interrupt it.",
      "notice.memory_written": "Memory: note written to agent.md ({source})",
      "notice.memory_failed": "Memory: could not write — {detail}",
      "memory.source.model": "the model's own summary",
      "memory.source.digest": "the turn digest",
      "notice.context_nearly_full": "Context is nearly full — the chat history will be summarised",
      "notice.compaction_done": "Chat summarised: history compressed, work continues",
      "notice.compaction": "Chat summary — {detail}",

      // ---- the window's own chrome --------------------------------------
      "chrome.new_chat": "New chat",
      "chrome.all_sessions": "All sessions",
      "chrome.settings": "Settings",
      "chrome.sessions_aria": "Chat sessions",
      "chrome.view_aria": "View",
      "chrome.view_chat": "Chat",
      "chrome.view_trajectory": "Trajectory",
      "chrome.subagents": "Subagents",

      "traj.scale_aria": "Timeline scale",
      "traj.duration": "Duration",
      "traj.turns": "Turns",
      "traj.calls": "Calls",
      "traj.search": "Search",
      "traj.search_aria": "Filter trajectory rows",
      "traj.row_aria": "Selected row",
      "traj.close_details": "Close details",
      "traj.detail_aria": "Detail view",
      "traj.tab_summary": "Summary",
      "traj.tab_preview": "Preview",
      "traj.tab_raw": "Raw",

      "pending.apply_title": "Apply changes",
      "pending.apply_aria": "Apply",
      "pending.discard_title": "Discard changes",
      "pending.discard_aria": "Discard",

      "diff.open_in_editor": "Open in editor",
      "diff.close": "Close",
      "diff.before": "Before",
      "diff.after": "After",

      "image.prev": "Previous image",
      "image.next": "Next image",
      "image.open_file": "Open file",
      "image.close": "Close",

      "todos.aria": "Task checklist",
      "model.menu_title": "Models",
      "model.search": "Search models…",
      "model.refresh": "Refresh list",
      "model.title": "Model",
      "queue.aria": "Queued messages",
      "composer.placeholder": "Message, @ for files, / for commands…",
      "composer.attach": "Attach files",
      "browser.title": "Browser",
      "browser.pane_aria": "Browser panel",
      "browser.back": "Back",
      "browser.forward": "Forward",
      "browser.reload": "Reload",
      "browser.pick": "Pick an element on the page",
      "browser.url_hint": "Address or search",
      "browser.console": "Console",
      "browser.more": "More",
      "browser.shot": "Take screenshot",
      "browser.shot_area": "Capture area",
      "browser.shooting": "Taking the screenshot…",
      "browser.shot_taken": "Screenshot attached as {name}",
      "browser.shot_failed": "The page gave no screenshot",
      "browser.area_hint": "Drag a rectangle over the page. Escape cancels.",
      "browser.hard_reload": "Hard reload",
      "browser.copy_url": "Copy current URL",
      "browser.copied": "Address copied",
      "browser.open_outside": "Open in a browser",
      "browser.opened_outside": "Opened outside",
      "browser.zoom": "Zoom",
      "browser.zoom_reset": "Back to 100%",
      "browser.engine": "Search with",
      "browser.app": "Open links in",
      "browser.app_default": "The system's browser",
      "browser.clear_cookies": "Clear cookies",
      "browser.clear_cache": "Clear cache",
      "browser.clear_site": "Clear this site's data",
      "browser.cleared": "Cleared",
      "browser.eval_hint": "Run JavaScript on the page",
      "browser.picking": "Click an element in the browser panel. Escape cancels.",
      "browser.picked": "Picked {tag} from {url} — attached as {name}",
      "composer.send": "Send",
      "composer.orchestra_title": "Orchestra roles & tiers",
      "cost.aria": "Spend and balance",
      "cost.title": "Spend",
      "cost.note": "Provider-reported cost · OpenRouter usage accounting",
      "ctx.aria": "Context usage",
      "ctx.title": "Context",
      "ctx.note": "Estimate from last LLM step · conversation grows during the turn",
      "ctx.row.conversation": "Conversation",
      "ctx.row.prompt": "Prompt context",
      "ctx.row.completion": "Completion",
      "ctx.row.reserved": "Reserved for reply",

      "cmd.clear": "New chat",
      "cmd.compact": "Compress LLM context",
      "cmd.help": "Show commands",
      "cmd.model": "Change model",
      "cmd.rewind": "Checkpoint rewind help",
      "cmd.sessions": "Switch session",
      "cmd.settings": "Open settings",

      "tab.close": "Close session",
      "code.open_file": "Open file",
      "reason.brief": "Thought briefly",
      "reason.for": "Thought for {n}s",
      "diff.no_changes": "No line changes detected",
      "diff.unavailable": "Diff preview unavailable",

      "perm.install_lsp": "Install language server?",
      "perm.allow_tool": "Allow {tool}?",
      "perm.tool": "tool",
      "perm.install_extra": "Install the language server for this workspace, or skip.",
      "perm.skip": "Skip",
      "perm.install_once": "Install once",
      "perm.install_always": "Install always",
      "perm.deny": "Deny",
      "perm.allow_once": "Allow once",
      "perm.allow_always": "Allow always",
      "question.step": "Question {n}/{total}",
      "question.next": "Next",

      "queue.remove": "Remove from queue",
      "typing.aria": "Assistant is working",
      "palette.files": "Files",
      "palette.no_files": "No files found",
      "palette.no_matches": "No matches",
      "attach.remove": "Remove file",
      "paste.too_big": "Pasted image exceeds 20 MB limit",

      "msg.rewind_title": "Rewind to here",
      "msg.rewind": "↩ Rewind",
      "msg.branch_title": "Branch a new chat from here",
      "msg.branch": "⑂ Branch",
      "msg.show_older": "Show {n} older messages",

      "traj.shell_output": "shell output",
      "traj.unavailable": "Trajectory unavailable: {detail}",
      "traj.not_recorded": "No trajectory was recorded for this session — it predates the log.",
      "traj.loading": "Loading trajectory…",
      "traj.empty": "Nothing has happened in this session yet.",
      "traj.turns_one": "1 turn",
      "traj.turns_n": "{n} turns",
      "traj.rows_n": "{n} rows",
      "traj.live_n": "{n} live",
      "traj.matching_n": "{n} matching",
      "traj.refresh_failed": "could not refresh: {detail}",
      "traj.fact.kind": "kind",
      "traj.fact.label": "label",
      "traj.fact.outcome": "outcome",
      "traj.fact.offset": "offset",
      "traj.fact.duration": "duration",
      "traj.fact.tokens_in": "tokens in",
      "traj.fact.tokens_out": "tokens out",
      "traj.fact.live": "live",
      "traj.fact.yes": "yes",
      "traj.fact.event": "event",
      "traj.fact.seq": "seq",
      "traj.no_payload": "This row records that the event happened; it carries no payload.",
      "traj.no_preview": "Nothing to preview for this row.",
      "traj.open_full_diff": "Open full diff",
      "traj.too_large": "The file is too large to align inline ({n} lines) — open the full diff.",
      "traj.no_line_changed": "No line changed in this file.",
      "traj.recorded_event": "recorded event",
      "traj.live_row": "row (live — not yet read back from the log)",
      "traj.result": "result",

      "orch.tiers": "Orchestra tiers",
      "orch.loading_map": "Loading tier map…",
      "orch.tier_models": "Orchestra tier models",
      "orch.l5_not_set": "L5 not set",
      "orch.fallback_main": "— (main model fallback)",
      "orch.not_set": "not set — falls back to the main model",
      "orch.failover_n": "{id} (failover {n})",
      "orch.configure": "Configure tiers…",

      "effort.head": "Effort",
      "effort.options": "Options",
      "effort.low": "Low",
      "effort.medium": "Medium",
      "effort.high": "High",
      // The core names these roles; the catalogue names them again so the panel
      // can read in the reader's language. A role the core adds later falls
      // back to whatever label it sends.
      "orch.role.planner": "Orchestrator",
      "orch.role.lead": "Dept Leads",
      "orch.role.complex": "Worker · complex",
      "orch.role.focused": "Worker · focused",
      "orch.role.micro": "Worker · micro",
      "orch.role.embed": "Embeddings",
      "model.no_providers": "No providers — open Settings",
      "model.none": "No models",
      "model.not_configured": "Not configured",
      "model.no_match": "No models match “{q}”",
      "model.retry": "No models — retry",

      "cost.session_spend": "Session spend",
      "cost.balance": "Balance",
      "cost.balance_prefix": "balance {amount}",
      "cost.session": "session {amount}",
      "cost.current_turn": "current turn {amount}",
      "cost.last_turn": "last turn {amount}",

      "conn.error": "connection error",
      "session.none": "No saved sessions",
      "session.delete": "Delete chat",
      "turn.failed": "turn failed",
      "turn.writing": "Writing…",

      // ---- the sidebar and the start screen (the browser and the desktop) ---
      "rail.aria": "Projects and sessions",
      "rail.close_settings": "Close settings",
      "rail.width": "Sidebar width",
      "rail.show": "Show sidebar",
      "rail.search_chats": "Search chats",
      "rail.search_aria": "Search this workspace's chats",
      "rail.delete_chat": "Delete this chat",
      "rail.delete_confirm": "Delete?",
      "rail.delete_confirm_title": "Click again to delete this chat for good",
      "rail.add_workspace": "Add a workspace folder",
      "rail.new_session": "New session in this workspace",
      "rail.new_session_label": "New session",
      "rail.now": "now",
      "rail.workspaces_aria": "Workspaces",
      "rail.pick_project": "Pick a workspace on the left to see its chats.",
      "rail.no_sessions": "No sessions yet",
      "rail.no_match": "No chat mentions “{q}”",
      "rail.chats_one": "1 chat",
      "rail.chats_n": "{n} chats",
      "rail.waiting": " — waiting for you",
      "rail.close_project": "Close project",
      "rail.forget_project": "Remove from list",
      "rail.delete_no_workspace": "The workspace is not open, so its chats cannot be deleted.",
      "rail.delete_failed": "Could not delete the chat: {detail}",

      "start.lead": "Open a workspace to start working in it.",
      "start.open_folder": "Open folder…",
      "start.clone_github": "Clone from GitHub…",
      "start.clone_url_label": "Repository URL",
      "start.clone": "Clone",
      "start.cancel": "Cancel",
      "start.clone_hint":
        "You will be asked where to put it. Private repositories need git credentials already set up on this machine.",
      "start.recent": "Recent workspaces",
      "start.none": "No workspaces yet — open a folder or clone a repository.",
      "start.opening": "Opening {name}…",
      "start.could_not_open": "Could not open {path}.",
      "start.opened_not_listed": "Opened {path}, but it is not in the workspace list.",
      "start.opened_not_switched": "Opened {path}, but could not switch to it.",
      "start.enter_url": "Enter a repository URL.",
      "start.clone_where": "Clone into which folder? (absolute path)",
      "start.project_folder": "Project folder (absolute path)",
      "start.folder_missing": "The folder for {name} is not there any more. ",
      "start.remove_from_list": "Remove from the list",
      "start.opening_note":
        "The workspace is still opening — its settings will load as soon as it is ready.",
      "start.no_project": "no project is open",

      "web.no_workspace": "No workspace is open.",
      "web.opening": "The workspace is still opening…",
      "web.compacted": "Context compacted.",
      "web.switch_tabs": "Switch chats from the tabs in the title bar.",
      "web.use_model_pill": "Use the model pill in the composer to change model.",
      "web.search_usage": "/search text — find text across this workspace's chats.",
      "web.no_workflows": "No workflows in this workspace. They live in .orchestra/workflows.",
      "web.workflows_head": "Workflows:",
      "web.no_description": "(no description)",
      "web.stages_n": "{n} stage(s)",
      "web.running_workflow": "Running workflow “{name}”…",
      "web.changes_applied": "Changes applied.",
      "web.changes_discarded": "Changes discarded.",
      "web.file_applied": "{path} applied.",
      "web.file_discarded": "{path} discarded.",
      "web.configured_endpoint": "Configured endpoint",
      "web.configured_endpoint_base": "Configured endpoint · {base}",
      "web.slash_help": [
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
      ].join("\n"),

      "graph.title": "Graph",
      "graph.pane_aria": "Project graph",
      "graph.depth_less": "One level of nesting less",
      "graph.depth_more": "One level of nesting more",
      "graph.files": "Files",
      "graph.files_title": "Draw the files, not only the folders",
      "graph.links": "Links",
      "graph.links_title": "Draw the calls between files",
      "graph.fit": "Fit",
      "graph.fit_title": "Fit the whole graph in view",
      "graph.refresh": "Refresh",
      "graph.refresh_title": "Read the graph again",
      "graph.levels": "levels {n}/{max}",
      "graph.levels_title": "How many levels of nesting the rings go out to",
      "graph.files_on_title": "Draw folders only, with the calls between them summed up",
      "graph.files_off_title": "Draw every file, not only the folders",
      "graph.links_on_title": "Leave out the calls between files, keeping the nesting",
      "graph.links_off_title": "Draw the calls between files again",
      "graph.stats_links": "{folders} folders · {files} files · {n} links",
      "graph.stats_heaviest":
        "{folders} folders · {files} files · the {drawn} heaviest of {total} links",
      "graph.reading": "Reading the project graph…",
      "graph.read_failed": "Could not read the graph: {detail}",
      "graph.workspace_root": "(workspace root)",
      "graph.row.file": "file",
      "graph.row.workspace": "workspace",
      "graph.row.folder": "folder",
      "graph.row.folded_in": "folded in",
      "graph.row.links": "links",
      "graph.row.out_in": "{out} out · {in} in",
      "graph.symbols_n": "{n} symbols",
      "graph.files_n": "{n} files",
      "graph.files_deeper": "{n} files deeper",
      "graph.section.indexed": "What is indexed",
      "graph.section.file_types": "File types",
      "graph.section.most_connected": "Most connected",
      "graph.section.selection": "Selection",
      "graph.section.file": "File",
      "graph.section.folder": "Folder",
      "graph.section.wired_to": "Wired to",
      "graph.section.inside": "Inside this file",
      "graph.ro.files": "files",
      "graph.ro.folders": "folders",
      "graph.ro.symbols": "symbols",
      "graph.ro.functions": "functions",
      "graph.ro.types": "types",
      "graph.ro.tests": "tests",
      "graph.ro.packages": "packages",
      "graph.ro.relations": "relations",
      "graph.ro.file_links": "file links",
      "graph.ro.embeddings": "embeddings",
      "graph.ro.nesting": "nesting",
      "graph.ro.levels_n": "{n} levels",
      "graph.ro.missing": " (+{n} missing)",
      "graph.ro.type": "type",
      "graph.ro.folded_away": "folded away",
      "graph.ro.links_out": "links out",
      "graph.ro.links_in": "links in",
      "graph.ro.lines": "lines",
      "graph.select_hint": "Click a node to see what is inside it and what it is wired to.",
      "graph.reading_short": "Reading…",
      "graph.no_symbols": "No symbols indexed in this file.",
      "graph.not_indexed": "This file is not in the index.",

      // ---- the settings panel -------------------------------------------
      "set.error": "error",
      "set.nav.general": "General",
      "set.nav.providers": "Providers",
      "set.nav.index": "Index & Graph",
      "set.nav.agent": "Agent",
      "set.nav.tools": "Tools & MCP",
      "set.nav.appearance": "Appearance",
      "set.back_aria": "Back to chat",
      "set.back": "Chat",
      "set.workspace": "Workspace",
      "set.save": "Save",
      "set.apply": "Apply",
      "set.close": "Close",
      "set.reload_all": "Reload all",
      "set.refresh_models": "Refresh models",

      "set.general.sub": "Workspace and Orchestra core connection",
      "set.general.binary": "Binary path",
      "set.general.binary_ph": "auto-detect orchestra.exe",
      "set.general.root": "Project root",
      "set.general.root_ph": "workspace folder",
      "set.general.restart_hint": "Restart core after changing binary or project root.",

      "set.lang.title": "Interface language",
      "set.lang.hint": "English and Russian for now. Anything not translated yet stays English.",
      "set.lang.auto": "Automatic",

      "set.orch.title": "Orchestra routing",
      "set.orch.sub":
        "Orchestrator (L5), department leads (L4), worker tiers, and the embedding model for semantic search. Pick models from the same provider — hover the <em>i</em> icon for what each role does.",
      "set.orch.shared_provider": "Shared provider",
      "set.orch.shared_hint":
        "Pick one gateway (OpenRouter) and assign different models per role. Primary model = first selected.",
      "set.orch.verify_summary": "Verification & retries",
      "set.orch.verify_det": "Deterministic worker verify (LSP + go build)",
      "set.orch.verify_llm": "LLM verifier after deterministic pass",
      "set.orch.max_retries": "Max worker retries",
      "set.orch.max_verify_retries": "Max verify retries",
      "set.orch.default_tier": "Default tier",
      "set.orch.modal_title": "Pick models",
      "set.orch.ctx_filter_aria": "Minimum context window",
      "set.orch.ctx_any": "Any context",
      "set.orch.pick_models": "Pick models…",
      "set.orch.slot.primary": "Primary",
      "set.orch.slot.fallback2": "Fallback 2",
      "set.orch.slot.fallback3": "Fallback 3",
      "set.orch.slot.embed": "Embedding model",
      "set.orch.slot.n": "Slot {n}",
      "set.orch.tier_title": "Orchestra tier {tier} (see orchestra-routing §1)",
      "set.orch.pick_embed": "Pick an embedding model",
      "set.orch.pick_up_to_3": "Pick up to 3 models (failover order)",
      "set.orch.hint_embed":
        "Pick one embedding model (text-embedding-…, nomic, bge). Chat models fail on /v1/embeddings.",
      "set.orch.hint_max": "Maximum 3 models — click a selected row to remove",
      "set.orch.hint_select": "Select up to {max} models in failover order (primary first)",
      "set.orch.modal_title_role": "Models · {role}",
      "set.orch.no_models": "Configure provider & refresh models first",
      "set.orch.filter_empty": "No models match the selected name and context window.",

      "set.role.planner.title": "L5 · Orchestrator",
      "set.role.planner.desc":
        "Reads the PRD, plans epics, splits work into WorkOrders and coordinates every department. Never edits code itself. Use your strongest reasoning model — it drives the whole run.",
      "set.role.planner.example": "e.g. Claude Sonnet / Opus, GPT-5, DeepSeek-R1",
      "set.role.lead.title": "L4 · Department Leads",
      "set.role.lead.desc":
        "Product & Documentation leads: write PRD.md, user stories, L1 conventions and decompose work for workers. Needs solid reasoning, but cheaper than L5.",
      "set.role.lead.example":
        "e.g. Claude Sonnet, GPT-5 mini, Qwen3-235B · empty = uses the Orchestrator model",
      "set.role.complex.title": "L3 · Worker (complex)",
      "set.role.complex.desc":
        "Big multi-file WorkOrders: new features, cross-module refactors, tricky bug fixes. Strongest of the worker tiers.",
      "set.role.complex.example": "e.g. Qwen3-Coder-32B, DeepSeek-V3, Claude Haiku",
      "set.role.focused.title": "L3 · Worker (focused)",
      "set.role.focused.desc":
        "Default tier: standard single-scope tasks — one function / file / test per WorkOrder. Most of the work runs here.",
      "set.role.focused.example": "e.g. Qwen2.5-Coder-14B/32B, Codestral",
      "set.role.micro.title": "L1 · Worker (micro)",
      "set.role.micro.desc":
        "Mechanical micro-edits: renames, comments, config tweaks, tiny fixes. Pick the cheapest / fastest model — quality demands are minimal.",
      "set.role.micro.example": "e.g. Qwen2.5-Coder-7B, Llama-3.1-8B, local LM Studio model",
      "set.role.embed.title": "Embeddings",
      "set.role.embed.desc":
        "Vector model for semantic_search and Index → Run embed. Must support POST /v1/embeddings — a chat model will fail. Uses the same provider credentials as Orchestra (OpenRouter, LM Studio, …).",
      "set.role.embed.example": "e.g. openai/text-embedding-3-small, nomic-embed-text, bge-m3",

      "set.prov.sub": "LLM provider and model — saved to <code>.orchestra.yml</code>",
      "set.prov.provider": "Provider",
      "set.prov.api_base": "API base",
      "set.prov.api_key": "API key",
      "set.prov.key_ph": "paste API key",
      "set.prov.show": "Show",
      "set.prov.hide": "Hide",
      "set.prov.model": "Model",
      "set.prov.adv": "Advanced generation",
      "set.prov.prompt_family": "Prompt family",
      "set.prov.temperature": "Temperature",
      "set.prov.max_tokens": "Max tokens",
      "set.prov.timeout": "Timeout (s)",
      "set.prov.multimodal": "Vision / multimodal (image attachments)",
      "set.prov.loading": "Loading providers…",
      "set.prov.main_global": "Main (global llm)",
      "set.prov.status.active": "Active provider",
      "set.prov.status.ready": "Configured — models loaded when available",
      "set.prov.status.need_key": "Enter API key and save to enable",
      "set.prov.status.need_base": "Enter API base URL for custom provider",
      "set.prov.status.none": "Not configured",
      "set.prov.key.loaded": "Saved key loaded — edit and Save to update",
      "set.prov.key.saved": "Key saved — click Show to view",
      "set.prov.key.required": "API key required",
      "set.prov.key.none": "No API key needed",
      "set.badge.error": "error",
      "set.badge.active": "active",
      "set.badge.models_n": "{n} models",
      "set.badge.ready": "ready",
      "set.badge.needs_key": "needs key",
      "set.badge.needs_url": "needs URL",

      "set.models.select_provider": "Select a provider",
      "set.models.failed": "Failed to load models: {detail}",
      "set.models.configure_first": "Configure credentials and save, then refresh",
      "set.models.none_returned": "No models returned — try Refresh",
      "set.models.count_from": "{n} models from {provider}",
      "set.models.no_match": "No models match “{q}”",
      "set.models.none_listed": "No models listed",
      "set.models.not_ready": "Provider not ready",
      "set.models.filtered": "{shown} / {total} models (filtered)",
      "set.models.refreshing": "Refreshing models…",
      "set.models.loading": "Loading models…",
      "set.models.active": "active",
      "set.models.active_ctx": "active · {ctx}",

      "set.index.sub":
        "CKG structural graph + semantic embeddings for <code>explore</code> / <code>semantic_search</code>",
      "set.index.stat.files": "indexed files",
      "set.index.stat.symbols": "symbols",
      "set.index.stat.links": "links",
      "set.index.stat.embeddings": "embeddings",
      "set.index.stat.functions": "functions",
      "set.index.stat.types": "types",
      "set.index.stat.tests": "tests",
      "set.index.stat.packages": "packages",
      "set.index.rebuild": "Rebuild graph",
      "set.index.run_embed": "Run embed",
      "set.index.open_graph": "Open graph viewer",
      "set.index.scope": "Scope & limits",
      "set.index.exclude": "Exclude dirs (one per line)",
      "set.index.ctx_limit": "Context limit (KB)",
      "set.index.max_files": "Max files",
      "set.index.optional": "optional",
      "set.index.semantic": "Semantic search options",
      "set.index.batch": "Batch size",
      "set.index.auto_explore": "Auto-explore top semantic hits",
      "set.index.save": "Save index settings",
      "set.index.rebuilding": "Rebuilding graph…",
      "set.index.embedding": "Running embed (may take a while)…",
      "set.index.no_ckg": "CKG store not available — start core first.",
      "set.index.need_embed": "{n} symbols need embedding — press “Run embed” · {path}",
      "set.index.graph_ready": "Graph ready · {path}",
      "set.index.no_embed_model":
        "No embedding model selected — pick one in General, then press Run embed.",
      "set.index.embed_model": "Embedding model: {model}",
      "set.index.embed_model_via": "Embedding model: {model} · via {provider}",
      "set.index.embed_result": "Embed: +{embedded} ({total} total, {remaining} remaining, {elapsed})",
      "set.index.graph_result": "Graph: {files} files, {nodes} nodes, {edges} edges",
      "set.index.files_n": "{n} files",

      "set.agent.system_prompt": "System prompt",
      "set.agent.override": "Project override",
      "set.agent.override_ph": "Leave empty to use the built-in / shared prompt…",
      "set.agent.clear": "Clear override",
      "set.agent.save_prompt": "Save prompt",
      "set.agent.custom": "Custom agents",
      "set.agent.name": "Name",
      "set.agent.tools": "Tools",
      "set.agent.tools_hint": "Toggle tools for this agent. All on = inherit full build toolset.",
      "set.agent.new": "New",
      "set.agent.delete": "Delete",
      "set.agent.save": "Save agent",
      "set.agent.none": "No custom agents yet",
      "set.agent.tools_n": "{n} tools",
      "set.agent.tools_all": "all tools",
      "set.agent.need_one_tool":
        "Enable at least one tool, or turn all on to inherit the full set.",
      "set.agent.catalog_na": "Tool catalog unavailable — start core and reload.",
      "set.agent.tools_full": "{n} tools · inherit full set",
      "set.agent.tools_on": "{on} / {total} tools enabled",
      "set.agent.toggle_all": "Toggle all {cat}",

      "set.mcp.sub":
        "Browse the official MCP Registry (plus featured locals) — install into <code>.orchestra.yml</code>. Installed servers use on/off toggles; open one to configure tools.",
      "set.mcp.cat.all": "All",
      "set.mcp.cat.installable": "Installable",
      "set.mcp.cat.featured": "Featured",
      "set.mcp.cat.remote": "Remote",
      "set.prov.cat.local": "Local",
      "set.prov.cat.cloud": "Cloud",
      "set.prov.cat.gateway": "Gateway",
      "set.prov.cat.other": "Other",
      "set.prov.cat.named": "Named",
      "set.mcp.tab_browse": "Browse",
      "set.mcp.tab_installed": "Installed",
      "set.mcp.search_ph": "Search registry (filesystem, github, slack…)",
      "set.mcp.loading_catalog": "Loading catalog…",
      "set.mcp.prev": "Prev",
      "set.mcp.next": "Next",
      "set.mcp.add_custom": "+ Custom server",
      "set.skills.title": "Skills",
      "set.skills.sub":
        "File-based agent bundles discovered in <code>~/.orchestra/skills</code> and <code>.orchestra/skills</code>. Read-only here — add one by dropping a folder in either place.",
      "set.skills.none": "No skills discovered — try orchestra skills install",
      "set.skills.badge": "skill",
      "set.hooks.summary": "Hooks, Git, Browser",
      "set.hooks.hint":
        "Configure <code>hooks</code>, <code>exec</code>, <code>web</code>, and <code>browser</code> in <code>.orchestra.yml</code>. UI editors coming later.",
      "set.mcp.configure": "Configure",
      "set.mcp.source": "Source",
      "set.mcp.source_hint": "Command and environment for this MCP server.",
      "set.mcp.server": "Server",
      "set.mcp.enable_title": "Enable server",
      "set.mcp.name": "Name",
      "set.mcp.command": "Command",
      "set.mcp.env": "Env (KEY=VAL per line)",
      "set.mcp.tools": "Tools",
      "set.mcp.tools_hint": "Enable or disable individual tools.",
      "set.mcp.remove": "Remove",
      "set.mcp.reload": "Reload",
      "set.mcp.done": "Done",
      "set.mcp.configure_named": "Configure {name}",
      "set.mcp.configure_custom": "Configure custom server",
      "set.mcp.new_server": "New server",
      "set.mcp.custom": "Custom",
      "set.mcp.enter_command": "Enter command below",
      "set.mcp.enter_command_done": "Enter a custom command, then Done.",
      "set.mcp.off": "Off",
      "set.mcp.error": "Error",
      "set.mcp.stopped": "Stopped",
      "set.mcp.installed": "Installed",
      "set.mcp.tool_one": "1 tool",
      "set.mcp.tool_n": "{n} tools",
      "set.mcp.loading_tools": "Loading tools…",
      "set.mcp.turn_on": "Turn the server on to load tools.",
      "set.mcp.no_tools": "No tools discovered yet — use Reload after save.",
      "set.mcp.reloading": "Reloading tools…",
      "set.mcp.registry_loading": "Loading MCP Registry…",
      "set.mcp.src_registry": "Official MCP Registry",
      "set.mcp.src_mixed": "Featured + Official MCP Registry",
      "set.mcp.src_local": "Local featured catalog",
      "set.mcp.loaded_n": " · {n} loaded",
      "set.mcp.loading_more": " · loading more…",
      "set.mcp.search_note": " · search “{q}”",
      "set.mcp.empty_loading": "Loading…",
      "set.mcp.empty_search": "No MCP servers match this search",
      "set.mcp.empty_catalog": "No MCP servers in catalog",
      "set.mcp.kind_remote": "remote",
      "set.mcp.kind_featured": "featured",
      "set.mcp.remote_only": "remote only",
      "set.mcp.remote_only_title": "Orchestra currently installs stdio MCP servers",
      "set.mcp.install": "Install",
      "set.mcp.install_env": "Install…",
      "set.mcp.docs": "Docs",
      "set.mcp.remote_not_supported":
        "This registry entry is remote-only — stdio install not available yet.",
      "set.mcp.fill_env": "Fill required env for {name}, then Done.",
      "set.mcp.installing": "Installing {name}…",
      "set.mcp.none_installed": "No MCP servers installed — browse the catalog to add some",
      "set.mcp.remove_server": "Remove server",
      "set.mcp.remove_named": "Remove {name}",
      "set.mcp.enable": "Enable",
      "set.mcp.disable": "Disable",
      "set.mcp.test_ok_one": "OK ({elapsed}): 1 tool",
      "set.mcp.test_ok_n": "OK ({elapsed}): {n} tools",
      "set.mcp.test_failed": "Failed: {detail}",
      "set.mcp.unknown": "unknown",

      "set.appear.sub": "How this page looks. Stored in this browser only.",
      "set.appear.theme_aria": "Theme",
      "set.appear.system": "Follow the system",
      "set.appear.system_hint": "Whatever your OS is set to",
      "set.appear.light": "Light",
      "set.appear.light_hint": "Always the light palette",
      "set.appear.dark": "Dark",
      "set.appear.dark_hint": "Always the dark palette",
      "set.appear.scale": "Interface scale",
      "set.appear.scale_sub":
        "How large everything is drawn. The automatic setting reads the window's width, which cannot know your monitor's physical size — set it yourself if the guess is wrong for your screen.",
      "set.appear.auto": "Automatic",
    },
    ru: {
      "mode.group.core": "Основные",
      "mode.group.more": "Дополнительные",

      "access.section": "Доступ",
      "access.ask.hint": "Shell с подтверждением; правки через Accept/Reject",
      "access.auto.hint": "Shell и запись файлов сразу на диск (без Accept/Reject)",
      "access.note": "Ask: правки в staging + Accept/Reject. Auto: правки пишутся на диск сразу.",
      "access.tools.section": "Инструменты",
      "access.browser.label": "Браузер",
      "access.browser.hint":
        "Агент может открывать страницы, нажимать и вводить текст в браузере (Playwright). Не действует при Fast.",
      "access.browser.on": "{hint} · браузер включён",

      "turn.working": "Работаю…",
      "turn.running_tools": "Выполняю инструменты…",
      "turn.queued": " · {n} в очереди",
      "turn.tasks_done": "✓ Задачи выполнены",

      "tool.body.lines": "строк: {n}",
      "tool.body.copy": "Копировать",
      "tool.body.copied": "Скопировано",
      "tool.body.copy_failed": "Не скопировалось",
      "tool.body.raw": "Как есть",
      "tool.body.pretty": "Форматированно",
      "tool.body.show_all": "Показать все {n} строк",
      "tool.body.collapse": "Свернуть",
      "tool.body.capped": "Первые {n} строк — остальное слишком длинное",

      "diff.loading": "Готовлю показ изменений…",
      "diff.more_lines": "… ещё {n} изменённых строк",
      "diff.open_file": "Открыть файл (Shift+клик: дифф в две колонки)",
      "diff.keep": "Принять",
      "diff.drop": "Отклонить",
      "diff.keep_title": "Применить только этот файл (a)",
      "diff.drop_title": "Отклонить только этот файл (x)",

      "conn.connecting": "Подключаюсь…",
      "conn.reconnecting": "Переподключаюсь…",
      "conn.reconnecting_n": "Переподключаюсь… ({n})",
      "conn.lost": "связь с ядром потеряна — перезагрузите страницу, чтобы попробовать снова",
      "conn.reopen_failed": "Переподключился, но этот чат не удалось прочитать: {detail}",

      "notice.turn_interrupted":
        "Предыдущий ход был прерван (процесс завершился аварийно). История сохранена до последнего выполненного шага.",
      "notice.background_turn_done": "Фоновый ход завершён — история обновлена.",
      "notice.background_turn_running":
        "Предыдущий ход этой сессии ещё завершается в фоновом процессе. История обновится автоматически, когда он закончит.",
      "notice.bad_stream":
        "Модель вернула некорректный поток вместо вызова edit/write. Попробуйте ещё раз, уточните запрос или смените модель в composer.",
      "notice.ui_sync_failed":
        "ui_sync failed — последний ответ может не сохраниться в истории: {detail}",
      "notice.session_busy":
        "session is busy: предыдущий ход ещё выполняется. Нажмите Stop, чтобы прервать его.",
      "notice.memory_written": "Память: заметка записана в agent.md ({source})",
      "notice.memory_failed": "Память: запись не удалась — {detail}",
      "memory.source.model": "сводка модели",
      "memory.source.digest": "из дайджеста хода",
      "notice.context_nearly_full": "Контекст почти заполнен — история чата будет суммаризирована",
      "notice.compaction_done": "Суммаризация чата: история сжата, работа продолжается",
      "notice.compaction": "Суммаризация чата — {detail}",

      // ---- the window's own chrome --------------------------------------
      "chrome.new_chat": "Новый чат",
      "chrome.all_sessions": "Все чаты",
      "chrome.settings": "Настройки",
      "chrome.sessions_aria": "Чаты",
      "chrome.view_aria": "Вид",
      "chrome.view_chat": "Чат",
      "chrome.view_trajectory": "Траектория",
      "chrome.subagents": "Подагенты",

      "traj.scale_aria": "Шкала времени",
      "traj.duration": "Длительность",
      "traj.turns": "Ходы",
      "traj.calls": "Вызовы",
      "traj.search": "Поиск",
      "traj.search_aria": "Фильтр строк траектории",
      "traj.row_aria": "Выбранная строка",
      "traj.close_details": "Закрыть подробности",
      "traj.detail_aria": "Подробности",
      "traj.tab_summary": "Сводка",
      "traj.tab_preview": "Просмотр",
      "traj.tab_raw": "Как есть",

      "pending.apply_title": "Применить изменения",
      "pending.apply_aria": "Применить",
      "pending.discard_title": "Отклонить изменения",
      "pending.discard_aria": "Отклонить",

      "diff.open_in_editor": "Открыть в редакторе",
      "diff.close": "Закрыть",
      "diff.before": "Было",
      "diff.after": "Стало",

      "image.prev": "Предыдущее изображение",
      "image.next": "Следующее изображение",
      "image.open_file": "Открыть файл",
      "image.close": "Закрыть",

      "todos.aria": "Список задач",
      "model.menu_title": "Модели",
      "model.search": "Поиск моделей…",
      "model.refresh": "Обновить список",
      "model.title": "Модель",
      "queue.aria": "Сообщения в очереди",
      "composer.placeholder": "Сообщение, @ — файлы, / — команды…",
      "composer.attach": "Прикрепить файлы",
      "browser.title": "Браузер",
      "browser.pane_aria": "Панель браузера",
      "browser.back": "Назад",
      "browser.forward": "Вперёд",
      "browser.reload": "Обновить",
      "browser.pick": "Выбрать элемент на странице",
      "browser.url_hint": "Адрес или поиск",
      "browser.console": "Консоль",
      "browser.more": "Ещё",
      "browser.shot": "Снимок страницы",
      "browser.shot_area": "Снимок области",
      "browser.shooting": "Снимаю…",
      "browser.shot_taken": "Снимок вложен как {name}",
      "browser.shot_failed": "Страница не отдала снимок",
      "browser.area_hint": "Выделите прямоугольник на странице. Escape отменяет.",
      "browser.hard_reload": "Перезагрузить без кеша",
      "browser.copy_url": "Скопировать адрес",
      "browser.copied": "Адрес скопирован",
      "browser.open_outside": "Открыть в браузере",
      "browser.opened_outside": "Открыто во внешнем браузере",
      "browser.zoom": "Масштаб",
      "browser.zoom_reset": "Вернуть 100%",
      "browser.engine": "Искать через",
      "browser.app": "Открывать ссылки в",
      "browser.app_default": "Браузер системы",
      "browser.clear_cookies": "Очистить cookie",
      "browser.clear_cache": "Очистить кеш",
      "browser.clear_site": "Очистить данные сайта",
      "browser.cleared": "Очищено",
      "browser.eval_hint": "Выполнить JavaScript на странице",
      "browser.picking": "Кликните элемент в панели браузера. Escape отменяет.",
      "browser.picked": "Взят {tag} со страницы {url} — вложен как {name}",
      "composer.send": "Отправить",
      "composer.orchestra_title": "Роли и уровни Orchestra",
      "cost.aria": "Расходы и баланс",
      "cost.title": "Расходы",
      "cost.note": "Стоимость по данным провайдера · учёт расхода OpenRouter",
      "ctx.aria": "Заполнение контекста",
      "ctx.title": "Контекст",
      "ctx.note": "Оценка по последнему шагу модели · за ход диалог растёт",
      "ctx.row.conversation": "Диалог",
      "ctx.row.prompt": "Контекст промпта",
      "ctx.row.completion": "Ответ",
      "ctx.row.reserved": "Зарезервировано под ответ",

      "cmd.clear": "Новый чат",
      "cmd.compact": "Сжать контекст модели",
      "cmd.help": "Показать команды",
      "cmd.model": "Сменить модель",
      "cmd.rewind": "Справка по откату к контрольной точке",
      "cmd.sessions": "Переключить чат",
      "cmd.settings": "Открыть настройки",

      "tab.close": "Закрыть чат",
      "code.open_file": "Открыть файл",
      "reason.brief": "Думал недолго",
      "reason.for": "Думал {n} с",
      "diff.no_changes": "Изменений в строках нет",
      "diff.unavailable": "Показать изменения не удалось",

      "perm.install_lsp": "Установить языковой сервер?",
      "perm.allow_tool": "Разрешить {tool}?",
      "perm.tool": "инструмент",
      "perm.install_extra": "Установить языковой сервер для этого проекта или пропустить.",
      "perm.skip": "Пропустить",
      "perm.install_once": "Установить один раз",
      "perm.install_always": "Устанавливать всегда",
      "perm.deny": "Запретить",
      "perm.allow_once": "Разрешить один раз",
      "perm.allow_always": "Разрешать всегда",
      "question.step": "Вопрос {n}/{total}",
      "question.next": "Далее",

      "queue.remove": "Убрать из очереди",
      "typing.aria": "Ассистент работает",
      "palette.files": "Файлы",
      "palette.no_files": "Файлы не найдены",
      "palette.no_matches": "Ничего не найдено",
      "attach.remove": "Убрать файл",
      "paste.too_big": "Вставленное изображение больше 20 МБ",

      "msg.rewind_title": "Откатиться сюда",
      "msg.rewind": "↩ Откат",
      "msg.branch_title": "Ответвить новый чат отсюда",
      "msg.branch": "⑂ Ветка",
      "msg.show_older": "Показать ещё {n} сообщений",

      "traj.shell_output": "вывод shell",
      "traj.unavailable": "Траектория недоступна: {detail}",
      "traj.not_recorded": "Для этого чата траектория не записывалась — он старше журнала.",
      "traj.loading": "Загружаю траекторию…",
      "traj.empty": "В этом чате пока ничего не происходило.",
      "traj.turns_one": "1 ход",
      "traj.turns_n": "ходов: {n}",
      "traj.rows_n": "строк: {n}",
      "traj.live_n": "в работе: {n}",
      "traj.matching_n": "совпадений: {n}",
      "traj.refresh_failed": "не удалось обновить: {detail}",
      "traj.fact.kind": "вид",
      "traj.fact.label": "название",
      "traj.fact.outcome": "итог",
      "traj.fact.offset": "смещение",
      "traj.fact.duration": "длительность",
      "traj.fact.tokens_in": "токенов на вход",
      "traj.fact.tokens_out": "токенов на выход",
      "traj.fact.live": "в работе",
      "traj.fact.yes": "да",
      "traj.fact.event": "событие",
      "traj.fact.seq": "номер",
      "traj.no_payload": "Эта строка фиксирует, что событие было; полезной нагрузки в ней нет.",
      "traj.no_preview": "Для этой строки нечего показать.",
      "traj.open_full_diff": "Открыть полный дифф",
      "traj.too_large": "Файл слишком велик, чтобы показать построчно ({n} строк) — откройте полный дифф.",
      "traj.no_line_changed": "В этом файле не изменилась ни одна строка.",
      "traj.recorded_event": "записанное событие",
      "traj.live_row": "строка (в работе — из журнала ещё не перечитана)",
      "traj.result": "результат",

      "orch.tiers": "Уровни Orchestra",
      "orch.loading_map": "Загружаю карту уровней…",
      "orch.tier_models": "Модели по уровням Orchestra",
      "orch.l5_not_set": "L5 не задан",
      "orch.fallback_main": "— (запасная: основная модель)",
      "orch.not_set": "не задано — берётся основная модель",
      "orch.failover_n": "{id} (подмена {n})",
      "orch.configure": "Настроить уровни…",

      "effort.head": "Усилие",
      "effort.options": "Параметры",
      "effort.low": "Низкое",
      "effort.medium": "Среднее",
      "effort.high": "Высокое",
      "orch.role.planner": "Оркестратор",
      "orch.role.lead": "Руководители направлений",
      "orch.role.complex": "Исполнитель · сложные",
      "orch.role.focused": "Исполнитель · обычные",
      "orch.role.micro": "Исполнитель · мелочь",
      "orch.role.embed": "Эмбеддинги",
      "model.no_providers": "Провайдеров нет — откройте настройки",
      "model.none": "Моделей нет",
      "model.not_configured": "Не настроено",
      "model.no_match": "Нет моделей по запросу «{q}»",
      "model.retry": "Моделей нет — попробовать снова",

      "cost.session_spend": "Расходы за чат",
      "cost.balance": "Баланс",
      "cost.balance_prefix": "баланс {amount}",
      "cost.session": "за чат {amount}",
      "cost.current_turn": "текущий ход {amount}",
      "cost.last_turn": "прошлый ход {amount}",

      "conn.error": "ошибка связи",
      "session.none": "Сохранённых чатов нет",
      "session.delete": "Удалить чат",
      "turn.failed": "ход не удался",
      "turn.writing": "Пишу…",

      // ---- the sidebar and the start screen (the browser and the desktop) ---
      "rail.aria": "Проекты и чаты",
      "rail.close_settings": "Закрыть настройки",
      "rail.width": "Ширина панели",
      "rail.show": "Показать панель",
      "rail.search_chats": "Поиск по чатам",
      "rail.search_aria": "Поиск по чатам этого проекта",
      "rail.delete_chat": "Удалить этот чат",
      "rail.delete_confirm": "Удалить?",
      "rail.delete_confirm_title": "Нажмите ещё раз, чтобы удалить чат навсегда",
      "rail.add_workspace": "Добавить папку проекта",
      "rail.new_session": "Новый чат в этом проекте",
      "rail.new_session_label": "Новый чат",
      "rail.now": "сейчас",
      "rail.workspaces_aria": "Проекты",
      "rail.pick_project": "Выберите проект слева — здесь будут его чаты.",
      "rail.no_sessions": "Чатов пока нет",
      "rail.no_match": "Ни в одном чате нет «{q}»",
      "rail.chats_one": "1 чат",
      "rail.chats_n": "чатов: {n}",
      "rail.waiting": " — ждёт вас",
      "rail.close_project": "Закрыть проект",
      "rail.forget_project": "Убрать из списка",
      "rail.delete_no_workspace": "Проект не открыт, поэтому удалять его чаты нельзя.",
      "rail.delete_failed": "Не удалось удалить чат: {detail}",

      "start.lead": "Откройте проект, чтобы начать в нём работать.",
      "start.open_folder": "Открыть папку…",
      "start.clone_github": "Клонировать с GitHub…",
      "start.clone_url_label": "Адрес репозитория",
      "start.clone": "Клонировать",
      "start.cancel": "Отмена",
      "start.clone_hint":
        "Спросим, куда его положить. Для приватных репозиториев на этой машине уже должны быть настроены учётные данные git.",
      "start.recent": "Недавние проекты",
      "start.none": "Проектов пока нет — откройте папку или клонируйте репозиторий.",
      "start.opening": "Открываю {name}…",
      "start.could_not_open": "Не удалось открыть {path}.",
      "start.opened_not_listed": "{path} открыт, но его нет в списке проектов.",
      "start.opened_not_switched": "{path} открыт, но переключиться на него не удалось.",
      "start.enter_url": "Введите адрес репозитория.",
      "start.clone_where": "В какую папку клонировать? (абсолютный путь)",
      "start.project_folder": "Папка проекта (абсолютный путь)",
      "start.folder_missing": "Папки проекта {name} больше нет. ",
      "start.remove_from_list": "Убрать из списка",
      "start.opening_note":
        "Проект ещё открывается — настройки загрузятся, как только он будет готов.",
      "start.no_project": "проект не открыт",

      "web.no_workspace": "Проект не открыт.",
      "web.opening": "Проект ещё открывается…",
      "web.compacted": "Контекст сжат.",
      "web.switch_tabs": "Переключайте чаты вкладками в заголовке окна.",
      "web.use_model_pill": "Смените модель кнопкой модели в composer.",
      "web.search_usage": "/search текст — искать текст по чатам этого проекта.",
      "web.no_workflows": "В этом проекте нет workflow. Они лежат в .orchestra/workflows.",
      "web.workflows_head": "Workflow:",
      "web.no_description": "(без описания)",
      "web.stages_n": "этапов: {n}",
      "web.running_workflow": "Запускаю workflow «{name}»…",
      "web.changes_applied": "Изменения применены.",
      "web.changes_discarded": "Изменения отклонены.",
      "web.file_applied": "{path} — применён.",
      "web.file_discarded": "{path} — отклонён.",
      "web.configured_endpoint": "Настроенный адрес",
      "web.configured_endpoint_base": "Настроенный адрес · {base}",
      "web.slash_help": [
        "Команды со слешем:",
        "/clear — новый чат",
        "/compact [подсказка] — сжать контекст модели",
        "/search текст — искать текст по сохранённым чатам",
        "/sessions — открыть список чатов",
        "/model — открыть меню моделей",
        "/workflows — показать workflow этого проекта",
        "/workflow имя [аргументы] — запустить один",
        "/settings — настройки Orchestra",
        "/<команда> аргументы — запустить свою команду этого проекта",
        "Откат: наведите на своё сообщение → ↩ Откат",
        "Ветка: наведите на своё сообщение → ⑂ Ветка",
        "Удалить чат: наведите на него в боковой панели → ×",
        "@файл — упомянуть файлы в composer",
      ].join("\n"),

      "graph.title": "Граф",
      "graph.pane_aria": "Граф проекта",
      "graph.depth_less": "На уровень вложенности меньше",
      "graph.depth_more": "На уровень вложенности больше",
      "graph.files": "Файлы",
      "graph.files_title": "Рисовать файлы, а не только папки",
      "graph.links": "Связи",
      "graph.links_title": "Рисовать вызовы между файлами",
      "graph.fit": "Вместить",
      "graph.fit_title": "Вместить весь граф в окно",
      "graph.refresh": "Обновить",
      "graph.refresh_title": "Перечитать граф",
      "graph.levels": "уровней {n}/{max}",
      "graph.levels_title": "На сколько уровней вложенности расходятся кольца",
      "graph.files_on_title": "Рисовать только папки, суммируя вызовы между ними",
      "graph.files_off_title": "Рисовать каждый файл, а не только папки",
      "graph.links_on_title": "Убрать вызовы между файлами, оставив вложенность",
      "graph.links_off_title": "Снова рисовать вызовы между файлами",
      "graph.stats_links": "папок {folders} · файлов {files} · связей {n}",
      "graph.stats_heaviest":
        "папок {folders} · файлов {files} · {drawn} самых тяжёлых связей из {total}",
      "graph.reading": "Читаю граф проекта…",
      "graph.read_failed": "Не удалось прочитать граф: {detail}",
      "graph.workspace_root": "(корень проекта)",
      "graph.row.file": "файл",
      "graph.row.workspace": "проект",
      "graph.row.folder": "папка",
      "graph.row.folded_in": "свёрнуто",
      "graph.row.links": "связи",
      "graph.row.out_in": "{out} исх. · {in} вх.",
      "graph.symbols_n": "символов: {n}",
      "graph.files_n": "файлов: {n}",
      "graph.files_deeper": "файлов глубже: {n}",
      "graph.section.indexed": "Что проиндексировано",
      "graph.section.file_types": "Типы файлов",
      "graph.section.most_connected": "Больше всего связей",
      "graph.section.selection": "Выбор",
      "graph.section.file": "Файл",
      "graph.section.folder": "Папка",
      "graph.section.wired_to": "С чем связан",
      "graph.section.inside": "Что внутри файла",
      "graph.ro.files": "файлы",
      "graph.ro.folders": "папки",
      "graph.ro.symbols": "символы",
      "graph.ro.functions": "функции",
      "graph.ro.types": "типы",
      "graph.ro.tests": "тесты",
      "graph.ro.packages": "пакеты",
      "graph.ro.relations": "отношения",
      "graph.ro.file_links": "связи файлов",
      "graph.ro.embeddings": "эмбеддинги",
      "graph.ro.nesting": "вложенность",
      "graph.ro.levels_n": "уровней: {n}",
      "graph.ro.missing": " (+{n} не хватает)",
      "graph.ro.type": "тип",
      "graph.ro.folded_away": "свёрнуто",
      "graph.ro.links_out": "связей наружу",
      "graph.ro.links_in": "связей внутрь",
      "graph.ro.lines": "строк",
      "graph.select_hint": "Нажмите на узел, чтобы увидеть, что внутри и с чем он связан.",
      "graph.reading_short": "Читаю…",
      "graph.no_symbols": "В этом файле нет проиндексированных символов.",
      "graph.not_indexed": "Этого файла нет в индексе.",

      // ---- the settings panel -------------------------------------------
      "set.error": "ошибка",
      "set.nav.general": "Общие",
      "set.nav.providers": "Провайдеры",
      "set.nav.index": "Индекс и граф",
      "set.nav.agent": "Агент",
      "set.nav.tools": "Инструменты и MCP",
      "set.nav.appearance": "Оформление",
      "set.back_aria": "Назад в чат",
      "set.back": "Чат",
      "set.workspace": "Проект",
      "set.save": "Сохранить",
      "set.apply": "Применить",
      "set.close": "Закрыть",
      "set.reload_all": "Перечитать всё",
      "set.refresh_models": "Обновить модели",

      "set.general.sub": "Проект и подключение к ядру Orchestra",
      "set.general.binary": "Путь к программе",
      "set.general.binary_ph": "определить orchestra.exe автоматически",
      "set.general.root": "Корень проекта",
      "set.general.root_ph": "папка проекта",
      "set.general.restart_hint": "После смены программы или корня проекта перезапустите ядро.",

      "set.lang.title": "Язык интерфейса",
      "set.lang.hint":
        "Пока английский и русский. Что ещё не переведено — остаётся на английском.",
      "set.lang.auto": "Автоматически",

      "set.orch.title": "Маршрутизация Orchestra",
      "set.orch.sub":
        "Оркестратор (L5), руководители направлений (L4), уровни исполнителей и модель эмбеддингов для семантического поиска. Берите модели одного провайдера — наведите на значок <em>i</em>, чтобы узнать, что делает каждая роль.",
      "set.orch.shared_provider": "Общий провайдер",
      "set.orch.shared_hint":
        "Выберите один шлюз (OpenRouter) и назначьте разные модели по ролям. Основная модель — выбранная первой.",
      "set.orch.verify_summary": "Проверка и повторы",
      "set.orch.verify_det": "Детерминированная проверка исполнителя (LSP + go build)",
      "set.orch.verify_llm": "Проверка моделью после детерминированной",
      "set.orch.max_retries": "Максимум повторов исполнителя",
      "set.orch.max_verify_retries": "Максимум повторов проверки",
      "set.orch.default_tier": "Уровень по умолчанию",
      "set.orch.modal_title": "Выбор моделей",
      "set.orch.ctx_filter_aria": "Минимальный контекст",
      "set.orch.ctx_any": "Любой контекст",
      "set.orch.pick_models": "Выберите модели…",
      "set.orch.slot.primary": "Основная",
      "set.orch.slot.fallback2": "Запасная 2",
      "set.orch.slot.fallback3": "Запасная 3",
      "set.orch.slot.embed": "Модель эмбеддингов",
      "set.orch.slot.n": "Слот {n}",
      "set.orch.tier_title": "Уровень Orchestra {tier} (см. orchestra-routing §1)",
      "set.orch.pick_embed": "Выберите модель эмбеддингов",
      "set.orch.pick_up_to_3": "До 3 моделей в порядке подмены",
      "set.orch.hint_embed":
        "Выберите одну модель эмбеддингов (text-embedding-…, nomic, bge). Чат-модели на /v1/embeddings не работают.",
      "set.orch.hint_max": "Максимум 3 модели — нажмите на выбранную, чтобы убрать",
      "set.orch.hint_select": "Выберите до {max} моделей в порядке подмены (основная первой)",
      "set.orch.modal_title_role": "Модели · {role}",
      "set.orch.no_models": "Сначала настройте провайдера и обновите модели",
      "set.orch.filter_empty": "Нет моделей под выбранное имя и размер контекста.",

      "set.role.planner.title": "L5 · Оркестратор",
      "set.role.planner.desc":
        "Читает PRD, планирует эпики, делит работу на WorkOrder'ы и координирует все направления. Сам код не правит. Ставьте сюда самую сильную в рассуждениях модель — она ведёт весь прогон.",
      "set.role.planner.example": "например Claude Sonnet / Opus, GPT-5, DeepSeek-R1",
      "set.role.lead.title": "L4 · Руководители направлений",
      "set.role.lead.desc":
        "Руководители продукта и документации: пишут PRD.md, пользовательские истории, соглашения L1 и раскладывают работу для исполнителей. Нужны крепкие рассуждения, но дешевле L5.",
      "set.role.lead.example":
        "например Claude Sonnet, GPT-5 mini, Qwen3-235B · пусто — берётся модель оркестратора",
      "set.role.complex.title": "L3 · Исполнитель (сложные)",
      "set.role.complex.desc":
        "Большие WorkOrder'ы на несколько файлов: новые возможности, рефакторинг между модулями, хитрые баги. Самый сильный из уровней исполнителей.",
      "set.role.complex.example": "например Qwen3-Coder-32B, DeepSeek-V3, Claude Haiku",
      "set.role.focused.title": "L3 · Исполнитель (обычные)",
      "set.role.focused.desc":
        "Уровень по умолчанию: обычные задачи в одной области — одна функция / файл / тест на WorkOrder. Здесь идёт большая часть работы.",
      "set.role.focused.example": "например Qwen2.5-Coder-14B/32B, Codestral",
      "set.role.micro.title": "L1 · Исполнитель (мелочь)",
      "set.role.micro.desc":
        "Механические микроправки: переименования, комментарии, мелочи в конфигах. Берите самую дешёвую и быструю модель — требования к качеству минимальны.",
      "set.role.micro.example": "например Qwen2.5-Coder-7B, Llama-3.1-8B, локальная модель LM Studio",
      "set.role.embed.title": "Эмбеддинги",
      "set.role.embed.desc":
        "Векторная модель для semantic_search и «Посчитать эмбеддинги» в разделе «Индекс». Должна поддерживать POST /v1/embeddings — чат-модель не подойдёт. Использует те же доступы провайдера, что и Orchestra (OpenRouter, LM Studio, …).",
      "set.role.embed.example": "например openai/text-embedding-3-small, nomic-embed-text, bge-m3",

      "set.prov.sub": "Провайдер LLM и модель — сохраняется в <code>.orchestra.yml</code>",
      "set.prov.provider": "Провайдер",
      "set.prov.api_base": "Адрес API",
      "set.prov.api_key": "Ключ API",
      "set.prov.key_ph": "вставьте ключ API",
      "set.prov.show": "Показать",
      "set.prov.hide": "Скрыть",
      "set.prov.model": "Модель",
      "set.prov.adv": "Параметры генерации",
      "set.prov.prompt_family": "Семейство промптов",
      "set.prov.temperature": "Температура",
      "set.prov.max_tokens": "Максимум токенов",
      "set.prov.timeout": "Таймаут (с)",
      "set.prov.multimodal": "Зрение / мультимодальность (вложенные картинки)",
      "set.prov.loading": "Загружаю провайдеров…",
      "set.prov.main_global": "Основной (глобальный llm)",
      "set.prov.status.active": "Активный провайдер",
      "set.prov.status.ready": "Настроен — модели подгрузятся, когда будут доступны",
      "set.prov.status.need_key": "Введите ключ API и сохраните, чтобы включить",
      "set.prov.status.need_base": "Укажите адрес API для своего провайдера",
      "set.prov.status.none": "Не настроен",
      "set.prov.key.loaded": "Сохранённый ключ загружен — измените и сохраните, чтобы обновить",
      "set.prov.key.saved": "Ключ сохранён — нажмите «Показать», чтобы увидеть",
      "set.prov.key.required": "Нужен ключ API",
      "set.prov.key.none": "Ключ API не нужен",
      "set.badge.error": "ошибка",
      "set.badge.active": "активен",
      "set.badge.models_n": "моделей: {n}",
      "set.badge.ready": "готов",
      "set.badge.needs_key": "нужен ключ",
      "set.badge.needs_url": "нужен адрес",

      "set.models.select_provider": "Выберите провайдера",
      "set.models.failed": "Не удалось загрузить модели: {detail}",
      "set.models.configure_first": "Заполните доступы, сохраните и обновите",
      "set.models.none_returned": "Модели не вернулись — нажмите «Обновить»",
      "set.models.count_from": "{n} моделей у {provider}",
      "set.models.no_match": "Нет моделей по запросу «{q}»",
      "set.models.none_listed": "Список моделей пуст",
      "set.models.not_ready": "Провайдер не готов",
      "set.models.filtered": "{shown} / {total} моделей (с фильтром)",
      "set.models.refreshing": "Обновляю модели…",
      "set.models.loading": "Загружаю модели…",
      "set.models.active": "активна",
      "set.models.active_ctx": "активна · {ctx}",

      "set.index.sub":
        "Структурный граф CKG и семантические эмбеддинги для <code>explore</code> / <code>semantic_search</code>",
      "set.index.stat.files": "файлов в индексе",
      "set.index.stat.symbols": "символов",
      "set.index.stat.links": "связей",
      "set.index.stat.embeddings": "эмбеддингов",
      "set.index.stat.functions": "функций",
      "set.index.stat.types": "типов",
      "set.index.stat.tests": "тестов",
      "set.index.stat.packages": "пакетов",
      "set.index.rebuild": "Перестроить граф",
      "set.index.run_embed": "Посчитать эмбеддинги",
      "set.index.open_graph": "Открыть просмотр графа",
      "set.index.scope": "Область и пределы",
      "set.index.exclude": "Исключить папки (по одной в строке)",
      "set.index.ctx_limit": "Предел контекста (КБ)",
      "set.index.max_files": "Максимум файлов",
      "set.index.optional": "необязательно",
      "set.index.semantic": "Параметры семантического поиска",
      "set.index.batch": "Размер пачки",
      "set.index.auto_explore": "Автоматически разбирать лучшие семантические попадания",
      "set.index.save": "Сохранить настройки индекса",
      "set.index.rebuilding": "Перестраиваю граф…",
      "set.index.embedding": "Считаю эмбеддинги (может занять время)…",
      "set.index.no_ckg": "Хранилище CKG недоступно — сначала запустите ядро.",
      "set.index.need_embed": "{n} символов без эмбеддингов — нажмите «Посчитать эмбеддинги» · {path}",
      "set.index.graph_ready": "Граф готов · {path}",
      "set.index.no_embed_model":
        "Модель эмбеддингов не выбрана — выберите её в разделе «Общие» и нажмите «Посчитать эмбеддинги».",
      "set.index.embed_model": "Модель эмбеддингов: {model}",
      "set.index.embed_model_via": "Модель эмбеддингов: {model} · через {provider}",
      "set.index.embed_result":
        "Эмбеддинги: +{embedded} (всего {total}, осталось {remaining}, {elapsed})",
      "set.index.graph_result": "Граф: файлов {files}, узлов {nodes}, связей {edges}",
      "set.index.files_n": "файлов: {n}",

      "set.agent.system_prompt": "Системный промпт",
      "set.agent.override": "Переопределение для проекта",
      "set.agent.override_ph": "Пусто — используется встроенный или общий промпт…",
      "set.agent.clear": "Убрать переопределение",
      "set.agent.save_prompt": "Сохранить промпт",
      "set.agent.custom": "Свои агенты",
      "set.agent.name": "Имя",
      "set.agent.tools": "Инструменты",
      "set.agent.tools_hint":
        "Включайте инструменты для этого агента. Все включены — наследуется полный набор режима build.",
      "set.agent.new": "Новый",
      "set.agent.delete": "Удалить",
      "set.agent.save": "Сохранить агента",
      "set.agent.none": "Своих агентов пока нет",
      "set.agent.tools_n": "инструментов: {n}",
      "set.agent.tools_all": "все инструменты",
      "set.agent.need_one_tool":
        "Включите хотя бы один инструмент — или включите все, чтобы наследовать полный набор.",
      "set.agent.catalog_na": "Каталог инструментов недоступен — запустите ядро и перечитайте.",
      "set.agent.tools_full": "инструментов: {n} · наследуется полный набор",
      "set.agent.tools_on": "включено {on} из {total}",
      "set.agent.toggle_all": "Переключить все: {cat}",

      "set.mcp.sub":
        "Смотрите официальный реестр MCP (и избранные локальные) — установка пишется в <code>.orchestra.yml</code>. У установленных серверов есть выключатель; откройте сервер, чтобы настроить инструменты.",
      "set.mcp.cat.all": "Все",
      "set.mcp.cat.installable": "Устанавливаемые",
      "set.mcp.cat.featured": "Избранные",
      "set.mcp.cat.remote": "Удалённые",
      "set.prov.cat.local": "Локальные",
      "set.prov.cat.cloud": "Облачные",
      "set.prov.cat.gateway": "Шлюзы",
      "set.prov.cat.other": "Прочие",
      "set.prov.cat.named": "Именованные",
      "set.mcp.tab_browse": "Каталог",
      "set.mcp.tab_installed": "Установленные",
      "set.mcp.search_ph": "Поиск по реестру (filesystem, github, slack…)",
      "set.mcp.loading_catalog": "Загружаю каталог…",
      "set.mcp.prev": "Назад",
      "set.mcp.next": "Вперёд",
      "set.mcp.add_custom": "+ Свой сервер",
      "set.skills.title": "Навыки",
      "set.skills.sub":
        "Файловые наборы для агента, найденные в <code>~/.orchestra/skills</code> и <code>.orchestra/skills</code>. Здесь только чтение — чтобы добавить, положите папку в любое из этих мест.",
      "set.skills.none": "Навыков не найдено — попробуйте orchestra skills install",
      "set.skills.badge": "навык",
      "set.hooks.summary": "Хуки, Git, браузер",
      "set.hooks.hint":
        "Настраивайте <code>hooks</code>, <code>exec</code>, <code>web</code> и <code>browser</code> в <code>.orchestra.yml</code>. Редакторы в интерфейсе будут позже.",
      "set.mcp.configure": "Настройка",
      "set.mcp.source": "Источник",
      "set.mcp.source_hint": "Команда и переменные окружения для этого сервера MCP.",
      "set.mcp.server": "Сервер",
      "set.mcp.enable_title": "Включить сервер",
      "set.mcp.name": "Имя",
      "set.mcp.command": "Команда",
      "set.mcp.env": "Переменные (KEY=VAL построчно)",
      "set.mcp.tools": "Инструменты",
      "set.mcp.tools_hint": "Включайте и выключайте отдельные инструменты.",
      "set.mcp.remove": "Удалить",
      "set.mcp.reload": "Перечитать",
      "set.mcp.done": "Готово",
      "set.mcp.configure_named": "Настройка {name}",
      "set.mcp.configure_custom": "Настройка своего сервера",
      "set.mcp.new_server": "Новый сервер",
      "set.mcp.custom": "Свой",
      "set.mcp.enter_command": "Введите команду ниже",
      "set.mcp.enter_command_done": "Введите свою команду и нажмите «Готово».",
      "set.mcp.off": "Выкл.",
      "set.mcp.error": "Ошибка",
      "set.mcp.stopped": "Остановлен",
      "set.mcp.installed": "Установлен",
      "set.mcp.tool_one": "1 инструмент",
      "set.mcp.tool_n": "инструментов: {n}",
      "set.mcp.loading_tools": "Загружаю инструменты…",
      "set.mcp.turn_on": "Включите сервер, чтобы загрузить инструменты.",
      "set.mcp.no_tools": "Инструменты пока не найдены — нажмите «Перечитать» после сохранения.",
      "set.mcp.reloading": "Перечитываю инструменты…",
      "set.mcp.registry_loading": "Загружаю реестр MCP…",
      "set.mcp.src_registry": "Официальный реестр MCP",
      "set.mcp.src_mixed": "Избранное + официальный реестр MCP",
      "set.mcp.src_local": "Локальный каталог избранного",
      "set.mcp.loaded_n": " · загружено {n}",
      "set.mcp.loading_more": " · загружаю ещё…",
      "set.mcp.search_note": " · поиск «{q}»",
      "set.mcp.empty_loading": "Загружаю…",
      "set.mcp.empty_search": "По этому запросу серверов MCP нет",
      "set.mcp.empty_catalog": "В каталоге нет серверов MCP",
      "set.mcp.kind_remote": "удалённый",
      "set.mcp.kind_featured": "избранный",
      "set.mcp.remote_only": "только удалённый",
      "set.mcp.remote_only_title": "Orchestra пока ставит только stdio-серверы MCP",
      "set.mcp.install": "Установить",
      "set.mcp.install_env": "Установить…",
      "set.mcp.docs": "Документация",
      "set.mcp.remote_not_supported":
        "Эта запись реестра только удалённая — установки stdio пока нет.",
      "set.mcp.fill_env": "Заполните обязательные переменные для {name} и нажмите «Готово».",
      "set.mcp.installing": "Устанавливаю {name}…",
      "set.mcp.none_installed": "Серверов MCP не установлено — добавьте их из каталога",
      "set.mcp.remove_server": "Удалить сервер",
      "set.mcp.remove_named": "Удалить {name}",
      "set.mcp.enable": "Включить",
      "set.mcp.disable": "Выключить",
      "set.mcp.test_ok_one": "OK ({elapsed}): 1 инструмент",
      "set.mcp.test_ok_n": "OK ({elapsed}): инструментов {n}",
      "set.mcp.test_failed": "Ошибка: {detail}",
      "set.mcp.unknown": "неизвестно",

      "set.appear.sub": "Как выглядит эта страница. Хранится только в этом браузере.",
      "set.appear.theme_aria": "Тема",
      "set.appear.system": "Как в системе",
      "set.appear.system_hint": "То, что выбрано в операционной системе",
      "set.appear.light": "Светлая",
      "set.appear.light_hint": "Всегда светлая палитра",
      "set.appear.dark": "Тёмная",
      "set.appear.dark_hint": "Всегда тёмная палитра",
      "set.appear.scale": "Масштаб интерфейса",
      "set.appear.scale_sub":
        "Насколько крупно всё нарисовано. Автоматический режим смотрит на ширину окна и не знает физический размер монитора — если он угадал неверно, задайте масштаб сами.",
      "set.appear.auto": "Автоматически",
    },
  };

  let uiLang = I18N_FALLBACK_LANG;

  /** "ru-RU" → "ru"; anything we do not have → "". */
  function normaliseLang(raw) {
    const s = String(raw || "")
      .toLowerCase()
      .replace("_", "-");
    for (const { id } of UI_LANGUAGES) {
      if (s === id || s.startsWith(id + "-")) {
        return id;
      }
    }
    return "";
  }

  /** The language to use: what was asked for, else the environment's, else English. */
  function pickLang(preferred) {
    const asked = normaliseLang(preferred);
    if (asked) {
      return asked;
    }
    const nav = typeof navigator !== "undefined" ? navigator.language : "";
    return normaliseLang(nav) || I18N_FALLBACK_LANG;
  }

  /** @returns {boolean} whether the language actually changed. */
  function setUiLang(preferred) {
    const next = pickLang(preferred);
    if (next === uiLang) {
      return false;
    }
    uiLang = next;
    return true;
  }

  function currentUiLang() {
    return uiLang;
  }

  /**
   * One string. `vars` fills {placeholders}; an unknown key returns itself,
   * which is visible in a screenshot and greppable in the catalogue.
   * @param {string} key @param {Record<string, any>=} vars
   */
  function i18n(key, vars) {
    const table = I18N_CATALOGUE[uiLang];
    const fallback = I18N_CATALOGUE[I18N_FALLBACK_LANG];
    let s = table && Object.prototype.hasOwnProperty.call(table, key) ? table[key] : undefined;
    if (s === undefined) {
      s = fallback && Object.prototype.hasOwnProperty.call(fallback, key) ? fallback[key] : key;
    }
    if (vars) {
      for (const name of Object.keys(vars)) {
        s = s.split("{" + name + "}").join(String(vars[name]));
      }
    }
    return s;
  }

  /**
   * Translate markup that was written in HTML rather than built in JS:
   * data-i18n sets the text, data-i18n-html sets markup (for the handful of
   * strings that carry a <code> or <em> — the catalogue is compiled in, never
   * user input), and data-i18n-title / -placeholder / -aria-label set that
   * attribute. Safe to call again after a language change.
   * @param {any=} root
   */
  function applyStaticI18n(root) {
    const scope = root || (typeof document !== "undefined" ? document : null);
    if (!scope || typeof scope.querySelectorAll !== "function") {
      return;
    }
    const pairs = [
      ["data-i18n", null],
      ["data-i18n-html", "innerHTML"],
      ["data-i18n-title", "title"],
      ["data-i18n-placeholder", "placeholder"],
      ["data-i18n-aria-label", "aria-label"],
    ];
    for (const [attr, target] of pairs) {
      const found = scope.querySelectorAll("[" + attr + "]") || [];
      for (const el of found) {
        const key = el.getAttribute(attr);
        if (!key) continue;
        if (target === null) {
          el.textContent = i18n(key);
        } else if (target === "innerHTML") {
          el.innerHTML = i18n(key);
        } else {
          el.setAttribute(target, i18n(key));
        }
      }
    }
  }
  /* ------------------------------------------------------------------ *
   * Icons — one set for every graphical surface.
   *
   * It sits beside i18n.js, outside chat-src/ and settings-src/, for the
   * same reason: four bundles (chat webview, settings webview, web page,
   * settings iframe) and one source, so an icon that moves between panels
   * keeps its name and its drawing.
   *
   * WHY THIS FILE EXISTS. Before it, an icon was whichever Unicode glyph
   * looked closest — 32 distinct ones across 96 places: ∞ ◎ ◌ ▣ ≡ ⌕ ◇ ⌁
   * ◫ □ ▾ → ← ✱ ✦ ◈ $ ▣ ◉ ⏳ ⋯. A glyph is drawn by whichever font on the
   * machine happens to carry it, so each arrived at a different optical
   * weight, a different cap height and a different baseline; ⌁ and ◫ fall
   * out of the UI font entirely on Windows. Side by side in one toolbar
   * they read as a pile of unrelated marks rather than one control strip,
   * which is exactly the complaint this file answers.
   *
   * THE GRID. Every path is drawn on a 24×24 box, stroked (never filled)
   * with currentColor at 1.75 units, round caps and joins. That is the one
   * rule to keep: a new icon drawn at another weight is visible instantly
   * next to its neighbours, which is the whole point of having a grid.
   *
   * SIZES. Three, and only three, chosen by role — see --icon-* in
   * chat.css. 14px sits inside a pill's text, 16px is a standalone control,
   * 18px is a chrome-strip button. Anything else is a new size nobody asked
   * for.
   *
   * WHAT IS NOT AN ICON. Typographic marks stay text: the − and + of diff
   * stats, the ↩ of a keyboard hint, the → of an "a → b" label. Those are
   * read as characters in a sentence, not as marks on a button.
   * ------------------------------------------------------------------ */

  /**
   * Path data, keyed by name. The value is the inner markup of the <svg>:
   * whatever `orchIconMarkup` should wrap. Keep entries alphabetical inside
   * their group so a duplicate is easy to spot.
   * @type {Record<string, string>}
   */
  const ORCH_ICON_PATHS = {
    /* --- tools: what a step did -------------------------------------- */
    // read: a page with its corner turned.
    read: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
    // list: a folder, because `ls` is asking a directory what it holds.
    list: '<path d="M3 8a2 2 0 0 1 2-2h3.4l2 2H19a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    write: '<path d="M4 20h4L19.2 8.8a2.1 2.1 0 0 0-3-3L5 17z"/><path d="M14.5 6.5l3 3"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="M20.5 20.5l-4.2-4.2"/>',
    // glob: an asterisk — the wildcard itself, which is what a glob is.
    glob: '<path d="M12 5v14"/><path d="M6.2 8.5l11.6 7"/><path d="M17.8 8.5l-11.6 7"/>',
    // symbols: braces, the universal mark for "the shape of the code".
    symbols:
      '<path d="M9 4c-2 0-2 3-2 4s0 4-2 4c2 0 2 3 2 4s0 4 2 4"/><path d="M15 4c2 0 2 3 2 4s0 4 2 4c-2 0-2 3-2 4s0 4-2 4"/>',
    exec: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7.5 9.5l3 2.5-3 2.5"/><path d="M13 15h3.5"/>',
    task: '<path d="M12 3l9 4.8-9 4.8-9-4.8z"/><path d="M3 12.4l9 4.8 9-4.8"/>',
    git: '<circle cx="6.5" cy="5.5" r="2.2"/><circle cx="6.5" cy="18.5" r="2.2"/><circle cx="17.5" cy="7.5" r="2.2"/><path d="M6.5 7.7v8.6"/><path d="M17.5 9.7v.8a4 4 0 0 1-4 4H9.5"/>',
    web: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a13.5 13.5 0 0 1 0 18a13.5 13.5 0 0 1 0-18z"/>',
    mcp: '<path d="M9 3v5"/><path d="M15 3v5"/><path d="M6 8h12v2.5a6 6 0 0 1-12 0z"/><path d="M12 16.5V21"/>',
    lsp: '<circle cx="12" cy="12" r="8.5"/><path d="M12 1.5v3.5"/><path d="M12 19v3.5"/><path d="M1.5 12H5"/><path d="M19 12h3.5"/><circle cx="12" cy="12" r="2.5"/>',
    todo: '<path d="M3.5 6.5l1.8 1.8 3-3"/><path d="M3.5 16l1.8 1.8 3-3"/><path d="M12 7h8.5"/><path d="M12 16.5h8.5"/>',
    question: '<path d="M4 5.5h16v10.5H9.5L4 20.5z"/><path d="M12 12.5v-.4c0-1.1 1.6-1.3 1.6-2.6A1.6 1.6 0 0 0 10.5 9"/>',
    memory: '<path d="M6.5 3.5h11v17l-5.5-3.8-5.5 3.8z"/>',
    skill: '<path d="M11 3l1.7 4.3L17 9l-4.3 1.7L11 15l-1.7-4.3L5 9l4.3-1.7z"/><path d="M18 15l.8 2.2L21 18l-2.2.8L18 21l-.8-2.2L15 18l2.2-.8z"/>',
    trash: '<path d="M4 6.5h16"/><path d="M9.5 6.5V4.5h5v2"/><path d="M6.5 6.5l.9 13h9.2l.9-13"/>',
    diff: '<path d="M4 8h11"/><path d="M12 5l3 3-3 3"/><path d="M20 16H9"/><path d="M12 13l-3 3 3 3"/>',
    explore: '<circle cx="12" cy="12" r="9"/><path d="M15.8 8.2l-2 5.6-5.6 2 2-5.6z"/>',
    // The fallback. A dot inside a ring reads as "a step happened" without
    // claiming to say which kind — better than a wrong icon.
    tool: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.6"/>',

    /* --- status ------------------------------------------------------ */
    // A checklist's three states, drawn as one shape so the column of them
    // lines up: the box is the same box whatever is inside it.
    box: '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/>',
    "box-check": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><path d="M8.5 12l2.5 2.5 4.5-5"/>',
    "box-cross": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><path d="M9 9l6 6"/><path d="M15 9l-6 6"/>',
    "box-active": '<rect x="4.5" y="4.5" width="15" height="15" rx="3.5"/><circle cx="12" cy="12" r="3.2" fill="currentColor" stroke="none"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    cross: '<path d="M6.5 6.5l11 11"/><path d="M17.5 6.5l-11 11"/>',
    close: '<path d="M6.5 6.5l11 11"/><path d="M17.5 6.5l-11 11"/>',
    running: '<circle cx="12" cy="12" r="8.5"/><path d="M12 6.5V12l3.5 2"/>',
    waiting: '<circle cx="12" cy="12" r="8.5"/><path d="M8 12h8"/>',

    /* --- modes: the composer's left-hand pills ----------------------- */
    "mode-agent":
      '<path d="M7 8.5a3.5 3.5 0 1 0 0 7c3.5 0 6-7 10-7a3.5 3.5 0 1 1 0 7c-4 0-6.5-7-10-7z"/>',
    "mode-orchestra": '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4"/>',
    "mode-build": '<rect x="4.5" y="4.5" width="15" height="15" rx="2.5"/><path d="M9 12h6"/>',
    "mode-plan": '<path d="M5 7h14"/><path d="M5 12h14"/><path d="M5 17h9"/>',
    "mode-explore": '<circle cx="11" cy="11" r="7"/><path d="M20.5 20.5l-4.2-4.2"/>',
    "mode-ask": '<path d="M12 3.5l8.5 8.5L12 20.5 3.5 12z"/>',
    "mode-debug": '<path d="M13.5 3L5.5 13.5H11L10.5 21l8-10.5H13z"/>',
    "mode-architecture": '<rect x="4" y="5" width="16" height="14" rx="2"/><path d="M12 5v14"/>',

    /* --- access ------------------------------------------------------ */
    // Ask: a ring left open, so the state reads as "waits for you".
    "access-ask": '<circle cx="12" cy="12" r="8.5" stroke-dasharray="3 3"/>',
    "access-auto": '<path d="M8 5.5l11 6.5-11 6.5z"/>',
    "access-browser": '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5a12.5 12.5 0 0 1 0 17a12.5 12.5 0 0 1 0-17z"/>',

    /* --- chrome and composer controls -------------------------------- */
    chat: '<path d="M20.5 11.5a8.5 8.5 0 0 1-8.5 8.5H3.5l2.6-2.6a8.5 8.5 0 1 1 14.4-5.9z"/><path d="M8.5 10.5h7"/><path d="M8.5 14h4.5"/>',
    trajectory: '<path d="M4 19V5"/><path d="M4 19h16"/><path d="M8.5 16v-4"/><path d="M12.5 16V8"/><path d="M16.5 16v-2.5"/>',
    graph: '<circle cx="12" cy="5.5" r="2.5"/><circle cx="5.5" cy="18" r="2.5"/><circle cx="18.5" cy="18" r="2.5"/><path d="M10.2 7.3L7.3 15.8"/><path d="M13.8 7.3l2.9 8.5"/>',
    gear: '<circle cx="12" cy="12" r="3.2"/><path d="M19.4 13a7.8 7.8 0 0 0 0-2l2-1.2-2-3.5-2.3 1a7.9 7.9 0 0 0-1.7-1L15 3h-4l-.4 2.3a7.9 7.9 0 0 0-1.7 1l-2.3-1-2 3.5 2 1.2a7.8 7.8 0 0 0 0 2l-2 1.2 2 3.5 2.3-1a7.9 7.9 0 0 0 1.7 1L11 21h4l.4-2.3a7.9 7.9 0 0 0 1.7-1l2.3 1 2-3.5z"/>',
    plus: '<path d="M12 5.5v13"/><path d="M5.5 12h13"/>',
    history: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7v5.2l3.4 2"/>',
    attach: '<path d="M14.5 6.5l-6.4 6.4a2.6 2.6 0 0 0 3.7 3.7l6.7-6.7a4.3 4.3 0 0 0-6.1-6.1l-6.7 6.7a6 6 0 0 0 8.5 8.5l4.3-4.3"/>',
    send: '<path d="M12 19.5V5"/><path d="M6 11l6-6 6 6"/>',
    stop: '<rect x="6.5" y="6.5" width="11" height="11" rx="2"/>',
    bolt: '<path d="M13.5 3L5.5 13.5H11L10.5 21l8-10.5H13z"/>',
    "arrow-left": '<path d="M19 12H5"/><path d="M11 6l-6 6 6 6"/>',
    "arrow-right": '<path d="M5 12h14"/><path d="M13 6l6 6-6 6"/>',
    reload: '<path d="M20 12a8 8 0 1 1-2.5-5.8"/><path d="M20 4v5h-5"/>',
    pick: '<rect x="4" y="4" width="10" height="10" rx="1.5"/><path d="M12 12l7.5 2.8-3.3 1.4-1.4 3.3z"/>',
    terminal: '<path d="M5.5 7.5l4 4.5-4 4.5"/><path d="M12.5 16.5h6"/>',
    dots: '<path d="M5.5 12h.01"/><path d="M12 12h.01"/><path d="M18.5 12h.01"/>',
    "chevron-down": '<path d="M6.5 9.5l5.5 5.5 5.5-5.5"/>',
    "chevron-up": '<path d="M6.5 14.5L12 9l5.5 5.5"/>',
    "chevron-right": '<path d="M9.5 6.5l5.5 5.5-5.5 5.5"/>',
    folder: '<path d="M3 8a2 2 0 0 1 2-2h3.4l2 2H19a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    copy: '<rect x="8.5" y="8.5" width="11" height="11" rx="2"/><path d="M15.5 8.5v-2a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2"/>',
    expand: '<path d="M8.5 4.5H4.5v4"/><path d="M15.5 19.5h4v-4"/><path d="M19.5 8.5v-4h-4"/><path d="M4.5 15.5v4h4"/>',
    collapse: '<path d="M4.5 8.5h4v-4"/><path d="M19.5 15.5h-4v4"/><path d="M15.5 4.5v4h4"/><path d="M8.5 19.5v-4h-4"/>',
  };

  /**
   * The rendered size of an icon, by role. Three values, deliberately:
   * inside a pill's own text, on a standalone control, on a chrome button.
   */
  const ORCH_ICON_SIZES = { sm: 14, md: 16, lg: 18 };

  /**
   * Markup for one icon.
   * @param {string} name a key of ORCH_ICON_PATHS
   * @param {{ size?: number | "sm" | "md" | "lg"; cls?: string }} [opts]
   * @returns {string} an <svg> element, or "" when the name is unknown
   */
  function orchIconMarkup(name, opts) {
    const body = ORCH_ICON_PATHS[name];
    if (!body) return "";
    const o = opts || {};
    const raw = o.size == null ? "md" : o.size;
    const px = typeof raw === "number" ? raw : ORCH_ICON_SIZES[raw] || ORCH_ICON_SIZES.md;
    const cls = o.cls ? ` ${o.cls}` : "";
    return (
      `<svg class="oi${cls}" width="${px}" height="${px}" viewBox="0 0 24 24" fill="none" ` +
      `stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" ` +
      `aria-hidden="true">${body}</svg>`
    );
  }

  /**
   * The same icon as a detached element, for the call sites that build DOM
   * rather than strings. Parsing our own constant markup is safe — the
   * paths above are the only thing that ever reaches innerHTML here.
   * @param {string} name @param {{ size?: number | "sm" | "md" | "lg"; cls?: string }} [opts]
   * @returns {SVGElement | null}
   */
  function orchIconEl(name, opts) {
    const markup = orchIconMarkup(name, opts);
    if (!markup) return null;
    const holder = document.createElement("div");
    holder.innerHTML = markup;
    return /** @type {SVGElement | null} */ (holder.firstElementChild);
  }

  /** True when an icon by that name exists — for call sites that fall back. */
  function orchHasIcon(name) {
    return Object.prototype.hasOwnProperty.call(ORCH_ICON_PATHS, name);
  }
  // The settings panel's host, on the web.
  //
  // In VS Code the panel is its own webview and reaches the extension through
  // acquireVsCodeApi(). Here the very same panel is an iframe inside the chat
  // page and the other end is ui/web/src/50-settings.js in the parent document
  // — the message protocol is identical, only the pipe differs, so every
  // fragment under ui/vscode/media/settings-src is used unchanged.
  //
  // An iframe rather than an inlined panel because settings.css declares its
  // own :root palette under the same token names chat.css uses: inlined, it
  // would repaint the whole app. A separate document also keeps its 2400 lines
  // of script out of the chat bundle's one shared scope.
  //
  // bundle-settings-web.mjs strips 01-core.js's own `const vscode = ...` line,
  // so this binding stands in for it.

  /** The parent is same-origin; naming it beats posting to "*". */
  const PARENT_ORIGIN = window.location.origin;

  const vscode = {
    /** @param {any} msg */
    postMessage(msg) {
      try {
        window.parent.postMessage(msg, PARENT_ORIGIN);
      } catch (e) {
        // A detached frame is not worth a broken page.
      }
    },
    /** The panel never reads it back; VS Code's own is a per-webview cache. */
    getState() {
      return undefined;
    },
    setState() {
      return undefined;
    },
  };

  // getHtml injects these in the webview; here they are files beside this one.
  // The catalogue arrives with the state message instead — 05-state.js prefers
  // msg.mcpCatalog and only falls back to the global.
  if (!window.__ORCH_ICON_BASE) {
    window.__ORCH_ICON_BASE = "provider-icons/";
  }
  if (!window.__ORCH_ICON_V) {
    window.__ORCH_ICON_V = "web";
  }

  // ---- language -----------------------------------------------------------
  //
  // The panel reads window.__ORCH_LANG at boot (settings-src/01-core.js). In
  // VS Code the extension stamps it into the webview's head; here the frame is
  // same-origin with the page that owns the stored choice, so it reads it
  // directly rather than waiting for a message and painting English first.

  function savedFrameLang() {
    try {
      return (window.localStorage && window.localStorage.getItem("orchestra.lang")) || "";
    } catch (e) {
      return "";
    }
  }

  if (typeof window.__ORCH_LANG !== "string") {
    window.__ORCH_LANG = savedFrameLang();
  }

  // ---- Appearance, which exists only on this host -------------------------
  //
  // VS Code panels follow the editor's theme, so the shared markup has no such
  // section; bundle-settings-web.mjs adds one to this page. Navigation between
  // tabs is generic over [data-section] in 01-core.js and needs nothing here —
  // only the three choices do.

  /** @returns {string} "system" | "light" | "dark" */
  function savedFrameTheme() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.theme") : "";
      return v === "light" || v === "dark" ? v : "system";
    } catch (e) {
      return "system";
    }
  }

  /** @param {string} choice */
  function syncFrameTheme(choice) {
    const root = document.documentElement;
    if (choice === "light" || choice === "dark") {
      root.setAttribute("data-theme", choice);
    } else {
      root.removeAttribute("data-theme");
    }
    document.querySelectorAll("[data-theme-choice]").forEach((el) => {
      el.setAttribute("aria-checked", el.getAttribute("data-theme-choice") === choice ? "true" : "false");
    });
  }

  document.addEventListener("click", (ev) => {
    const item = ev.target && ev.target.closest ? ev.target.closest("[data-theme-choice]") : null;
    if (!item) {
      return;
    }
    const choice = item.getAttribute("data-theme-choice") || "system";
    syncFrameTheme(choice);
    // The parent owns the stored value and the chat page's own stamp; it
    // echoes the change back so both documents always agree.
    vscode.postMessage({ type: "setTheme", theme: choice });
  });

  // ---- interface scale ---------------------------------------------------
  //
  // The parent's zoom already scales this frame with everything else, so there
  // is nothing to stamp here — only the current choice to show.

  const SCALES = ["100", "110", "125", "150", "175", "200"];

  /** @returns {string} "auto" or a percentage */
  function savedFrameScale() {
    try {
      const v = window.localStorage ? window.localStorage.getItem("orchestra.scale") : "";
      return SCALES.indexOf(v || "") >= 0 ? String(v) : "auto";
    } catch (e) {
      return "auto";
    }
  }

  /** @param {string} choice */
  function syncFrameScale(choice) {
    document.querySelectorAll("[data-scale-choice]").forEach((el) => {
      el.setAttribute(
        "aria-checked",
        el.getAttribute("data-scale-choice") === choice ? "true" : "false"
      );
    });
  }

  document.addEventListener("click", (ev) => {
    const item = ev.target && ev.target.closest ? ev.target.closest("[data-scale-choice]") : null;
    if (!item) {
      return;
    }
    const choice = item.getAttribute("data-scale-choice") || "auto";
    syncFrameScale(choice);
    vscode.postMessage({ type: "setScale", scale: choice });
  });

  window.addEventListener("message", (event) => {
    const msg = event && event.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    if (msg.type === "theme") {
      syncFrameTheme(msg.theme === "light" || msg.theme === "dark" ? msg.theme : "system");
    }
    if (msg.type === "scale") {
      syncFrameScale(SCALES.indexOf(String(msg.scale)) >= 0 ? String(msg.scale) : "auto");
    }
    if (msg.type === "language") {
      // The parent stores the choice and echoes it, so a change made in the
      // chat window reaches an already-open panel.
      window.__ORCH_LANG = typeof msg.lang === "string" ? msg.lang : "";
      applyUiLanguage();
      repaintTranslatedPanels();
    }
    if (msg.type === "workspace") {
      // Every setting on these screens belongs to one workspace's own
      // .orchestra.yml, so the panel names the workspace it is editing.
      const el = document.getElementById("navWorkspaceName");
      if (el) {
        el.textContent = String(msg.name || "—");
        el.title = String(msg.path || "");
      }
    }
  });

  syncFrameTheme(savedFrameTheme());
  syncFrameScale(savedFrameScale());
  /* vscode is supplied by ui/web/src/settings-frame.js */

  /** @type {string} */
  let workspaceRoot = ".";
  /** @type {any[]} */
  let agents = [];
  /** @type {string[]} */
  let agentAvailableTools = [];
  /** @type {any[]} */
  let mcpServers = [];
  /** @type {any[]} */
  let skills = [];
  /** @type {number} */
  let graphUIPort = 6061;
  /** @type {any[]} */
  let providers = [];
  /** @type {string} */
  let selectedProviderKey = "";
  /** @type {string} */
  let selectedModelId = "";
  /** @type {string} */
  let activeProviderKey = "";
  /** @type {string} */
  let activeModelId = "";
  /** @type {string} */
  let modelSearchFilter = "";
  /** @type {any} */
  let orchestraConfig = null;
  /** @type {string} */
  let orchSharedProvider = "";
  /** @type {string | null} */
  let orchModalRoleKey = null;
  /** @type {string[]} */
  let orchModalSelection = [];
  /** @type {string} */
  let orchModalSearch = "";
  /** Minimum model context window in tokens; zero disables filtering. */
  let orchModalMinContext = 0;
  /** @type {boolean} */
  let apiKeyVisible = false;
  /** @type {{ version?: number, entries?: any[] }} */
  let mcpCatalog = { version: 1, entries: [] };
  /** @type {"browse" | "installed"} */
  let mcpTab = "browse";
  /** @type {string[]} */
  let mcpDraftTools = [];
  /** @type {boolean} */
  let mcpConfigureOpen = false;
  /** @type {boolean} */
  let mcpIsNewCustom = false;
  /** @type {boolean} */
  let mcpToolsLoading = false;
  /** @type {string} */
  let mcpCatalogFilter = "";
  /** @type {string} */
  let mcpCatalogCategory = "All";
  /** @type {string} */
  let mcpCatalogNextCursor = "";
  /** @type {string} */
  let mcpCatalogSource = "local";
  /** @type {string} */
  let mcpCatalogError = "";
  /** @type {boolean} */
  let mcpCatalogBusy = false;
  /** @type {boolean} */
  let mcpCatalogPrefetching = false;
  /** @type {number | null} */
  let mcpSearchTimer = null;
  /** @type {number} */
  let mcpCatalogPage = 0;
  const MCP_PAGE_SIZE = 20;

  const CATEGORY_ORDER = ["Local", "Cloud", "Gateway", "Other", "Named"];
  const MCP_CAT_ORDER = ["All", "Installable", "Featured", "Remote"];

  const errorEl = document.getElementById("error");

  function showError(text) {
    if (!errorEl) return;
    if (!text) {
      errorEl.classList.add("hidden");
      errorEl.textContent = "";
      return;
    }
    errorEl.textContent = text;
    errorEl.classList.remove("hidden");
  }

  function el(id) {
    return document.getElementById(id);
  }

  /** @returns {HTMLInputElement | null} */
  function input(id) {
    return /** @type {HTMLInputElement | null} */ (el(id));
  }

  /** @returns {HTMLTextAreaElement | null} */
  function area(id) {
    return /** @type {HTMLTextAreaElement | null} */ (el(id));
  }

  function navigateToSection(section) {
    if (!section) return;
    document.querySelectorAll(".nav-item").forEach((b) => {
      b.classList.toggle("active", b.getAttribute("data-section") === section);
    });
    document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
    el("sec-" + section)?.classList.add("active");
  }

  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.addEventListener("click", () => {
      const section = btn.getAttribute("data-section");
      if (!section) return;
      document.querySelectorAll(".nav-item").forEach((b) => b.classList.remove("active"));
      document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
      btn.classList.add("active");
      el("sec-" + section)?.classList.add("active");
      btn.scrollIntoView({ inline: "nearest", block: "nearest", behavior: "smooth" });
    });
  });

  el("backChat")?.addEventListener("click", () => {
    vscode.postMessage({ type: "backToChat" });
  });

  if (typeof window !== "undefined" && window.__ORCH_MCP_CATALOG) {
    mcpCatalog = window.__ORCH_MCP_CATALOG;
  }

  // ---- language -----------------------------------------------------------
  //
  // This panel is its own document, so it reads the choice itself rather than
  // waiting for a message: the VS Code host stamps window.__ORCH_LANG into the
  // webview's head (from the `orchestra.language` setting), and on the web
  // ui/web/src/settings/frame.js stamps it from localStorage. Empty means
  // "follow the environment", which pickLang() reads off navigator.language.

  /** What the picker shows when no language is forced. */
  const UI_LANG_AUTO = "";

  function savedUiLangSetting() {
    const raw = typeof window !== "undefined" ? window.__ORCH_LANG : "";
    return typeof raw === "string" ? raw : "";
  }

  function renderUiLanguageSelect() {
    const sel = /** @type {HTMLSelectElement | null} */ (el("uiLanguage"));
    if (!sel) return;
    const current = savedUiLangSetting();
    sel.innerHTML = "";
    const auto = document.createElement("option");
    auto.value = UI_LANG_AUTO;
    auto.textContent = i18n("set.lang.auto");
    sel.appendChild(auto);
    for (const lang of UI_LANGUAGES) {
      const opt = document.createElement("option");
      opt.value = lang.id;
      // A language names itself in its own language — a reader looking for
      // "Русский" should not have to find it under "Russian".
      opt.textContent = lang.label;
      sel.appendChild(opt);
    }
    sel.value = current;
  }

  /** Re-read the language and repaint everything already on screen. */
  function applyUiLanguage() {
    setUiLang(savedUiLangSetting());
    applyStaticI18n();
    renderUiLanguageSelect();
  }

  el("uiLanguage")?.addEventListener("change", () => {
    const sel = /** @type {HTMLSelectElement | null} */ (el("uiLanguage"));
    const lang = sel ? sel.value : "";
    if (typeof window !== "undefined") {
      window.__ORCH_LANG = lang;
    }
    // The host persists it — a VS Code setting, or this browser's storage —
    // and tells the chat window, which is a different document.
    vscode.postMessage({ type: "setLanguage", lang });
    applyUiLanguage();
    repaintTranslatedPanels();
  });
  const MAX_ORCH_MODELS = 3;

  const ORCH_SLOT_LABEL_KEYS = ["set.orch.slot.primary", "set.orch.slot.fallback2", "set.orch.slot.fallback3"];

  /** @type {HTMLElement | null} */
  let openProvDropdown = null;

  function closeProvDropdowns() {
    if (openProvDropdown) {
      openProvDropdown.classList.remove("open");
      openProvDropdown = null;
    }
  }

  document.addEventListener("click", closeProvDropdowns);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeProvDropdowns();
  });

  /** @param {number | undefined} n */
  function formatContextTokens(n) {
    if (!n || n <= 0) return "";
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return (m >= 10 ? Math.round(m) : Math.round(m * 10) / 10) + "M ctx";
    }
    if (n >= 1000) {
      const k = n / 1000;
      return (k >= 100 ? Math.round(k) : Math.round(k * 10) / 10) + "k ctx";
    }
    return n + " ctx";
  }

  /** @param {any} p */
  function providerBadgeMeta(p) {
    if (!p) return { text: "—", className: "disabled" };
    if (p.models_error)
      return { text: i18n("set.badge.error"), className: "error", title: p.models_error };
    if (p.active) return { text: i18n("set.badge.active"), className: "running" };
    if (p.ready && p.model_count > 0) {
      return { text: i18n("set.badge.models_n", { n: p.model_count }), className: "ok" };
    }
    if (p.ready) return { text: i18n("set.badge.ready"), className: "ready" };
    if (p.needs_key && !p.api_key_set)
      return { text: i18n("set.badge.needs_key"), className: "disabled" };
    if (p.custom && !p.api_base)
      return { text: i18n("set.badge.needs_url"), className: "disabled" };
    return { text: "—", className: "disabled" };
  }

  /** @param {string} key @param {string} [extraClass] */
  function makeProviderIconEl(key, extraClass) {
    const span = document.createElement("span");
    span.className = "prov-icon" + (extraClass ? " " + extraClass : "");
    span.innerHTML = providerLogoHtml(key);
    span.title = key || "global";
    span.setAttribute("aria-hidden", "true");
    return span;
  }

  /** @param {HTMLElement} iconEl @param {string} key */
  function setProviderIconEl(iconEl, key) {
    iconEl.innerHTML = providerLogoHtml(key);
    iconEl.title = key || "global";
  }

  /** @param {any} p */
  function appendProviderBadge(parent, p) {
    const meta = providerBadgeMeta(p);
    const badge = document.createElement("span");
    badge.className = "badge " + meta.className;
    badge.textContent = meta.text;
    if (meta.title) badge.title = meta.title;
    parent.appendChild(badge);
  }

  /**
   * Custom provider picker with logos (replaces native select in orchestra).
   * @param {string} value
   * @param {(key: string) => void} onChange
   * @param {{ key: string, label: string }[]} options
   */
  function buildProviderDropdown(value, onChange, options) {
    const root = document.createElement("div");
    root.className = "prov-dropdown";

    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "prov-dropdown-btn";

    const btnIcon = makeProviderIconEl(value, "prov-dropdown-icon");
    const btnLabel = document.createElement("span");
    btnLabel.className = "prov-dropdown-label";
    const btnChevron = document.createElement("span");
    btnChevron.className = "prov-dropdown-chevron";
    btnChevron.innerHTML = orchIconMarkup("chevron-down", { size: "sm" });

    btn.appendChild(btnIcon);
    btn.appendChild(btnLabel);
    btn.appendChild(btnChevron);

    const menu = document.createElement("div");
    menu.className = "prov-dropdown-menu";
    menu.setAttribute("role", "listbox");

    /** @param {string} key */
    function labelFor(key) {
      const hit = options.find((o) => o.key === key);
      return hit ? hit.label : key || i18n("set.prov.main_global");
    }

    /** @param {string} key */
    function syncButton(key) {
      setProviderIconEl(btnIcon, key);
      btnLabel.textContent = labelFor(key);
      menu.querySelectorAll(".prov-dropdown-item").forEach((item) => {
        item.classList.toggle("selected", item.getAttribute("data-key") === key);
      });
    }

    options.forEach((o) => {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "prov-dropdown-item" + (o.key === value ? " selected" : "");
      item.setAttribute("data-key", o.key);
      item.setAttribute("role", "option");
      item.appendChild(makeProviderIconEl(o.key, "prov-dropdown-icon"));
      const lab = document.createElement("span");
      lab.className = "prov-dropdown-label";
      lab.textContent = o.label;
      item.appendChild(lab);
      item.addEventListener("click", (e) => {
        e.stopPropagation();
        syncButton(o.key);
        onChange(o.key);
        closeProvDropdowns();
      });
      menu.appendChild(item);
    });

    syncButton(value || "");

    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const wasOpen = root.classList.contains("open");
      closeProvDropdowns();
      if (!wasOpen) {
        root.classList.add("open");
        openProvDropdown = root;
      }
    });

    root.appendChild(btn);
    root.appendChild(menu);
    return root;
  }
  /** Catalog keys with bundled SVG logos under media/provider-icons/. */
  const PROVIDER_ICON_FILES = new Set([
    "",
    "lmstudio",
    "ollama",
    "vllm",
    "openrouter",
    "openai",
    "anthropic",
    "google",
    "mistral",
    "deepseek",
    "xai",
    "moonshot",
    "groq",
    "together",
    "fireworks",
    "cerebras",
    "custom",
  ]);

  /** @param {string} key */
  function providerIconFile(key) {
    const k = (key || "").toLowerCase();
    if (k === "") return "default";
    if (PROVIDER_ICON_FILES.has(k)) return k;
    return "";
  }

  /** @param {string} key */
  function providerLogoHtml(key) {
    const k = (key || "").toLowerCase();
    const file = providerIconFile(k);
    const base =
      typeof window !== "undefined" && window.__ORCH_ICON_BASE
        ? String(window.__ORCH_ICON_BASE)
        : "";
    if (base && file) {
      const ver =
        typeof window !== "undefined" && window.__ORCH_ICON_V
          ? String(window.__ORCH_ICON_V)
          : "";
      const src = base.replace(/\/?$/, "/") + file + ".svg" + (ver ? "?v=" + encodeURIComponent(ver) : "");
      return `<img class="prov-logo-img" src="${src}" alt="" width="22" height="22" loading="lazy" decoding="async" />`;
    }
    if (k) {
      const letter = k.slice(0, 1).toUpperCase();
      return `<svg viewBox="0 0 24 24" aria-hidden="true"><rect width="24" height="24" rx="6" fill="rgba(255,255,255,0.08)"/><text x="12" y="16" text-anchor="middle" fill="#b9bac2" font-size="11" font-family="Segoe UI,sans-serif" font-weight="600">${letter}</text></svg>`;
    }
    return `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke="rgba(255,255,255,0.35)" stroke-width="1.5" fill="none"/><path d="M8 12h8M12 8v8" stroke="#b9bac2" stroke-width="1.5" stroke-linecap="round"/></svg>`;
  }
  function modelsPayload() {
    return {
      provider: selectedProviderKey || input("provider")?.value || "",
      apiBase: input("apiBase")?.value || "",
      apiKey: input("apiKey")?.value || "",
      model: selectedModelId || input("model")?.value || "",
      promptFamily: input("promptFamily")?.value || "",
      temperature: input("temperature") ? Number(input("temperature").value) : undefined,
      maxTokens: input("maxTokens") ? Number(input("maxTokens").value) : undefined,
      timeoutS: input("timeoutS") ? Number(input("timeoutS").value) : undefined,
      multimodal: input("multimodal")?.checked,
    };
  }

  /** @param {string} key */
  function findProvider(key) {
    return providers.find((p) => p.key === key);
  }

  function syncHiddenFields() {
    if (input("provider")) input("provider").value = selectedProviderKey;
    if (input("model")) input("model").value = selectedModelId;
  }

  function applyApiKeyVisibility() {
    const field = input("apiKey");
    const btn = el("toggleApiKey");
    if (!field) return;
    field.type = apiKeyVisible ? "text" : "password";
    if (btn) btn.textContent = i18n(apiKeyVisible ? "set.prov.hide" : "set.prov.show");
  }

  /** @param {any} p @param {{ force?: boolean }} [opts] */
  function populateProviderCreds(p, opts) {
    if (!p) return;
    const force = Boolean(opts?.force);
    if (input("apiBase")) {
      const base = p.api_base || "";
      if (force || p.custom || !input("apiBase").value) {
        input("apiBase").value = base;
      }
    }
    if (input("apiKey")) {
      const key = typeof p.api_key === "string" ? p.api_key : "";
      if (force || key) {
        input("apiKey").value = key;
      }
    }
    applyApiKeyVisibility();
  }

  function updateCredBlock() {
    const cred = el("providerCreds");
    const p = findProvider(selectedProviderKey);
    if (!cred || !p) {
      cred?.classList.add("hidden");
      return;
    }
    const show = p.custom || p.needs_key || p.named || p.category === "Local";
    cred.classList.toggle("hidden", !show);
    if (input("apiBase") && (!input("apiBase").value || p.custom)) {
      input("apiBase").value = p.api_base || "";
    }
    populateProviderCreds(p);
    const keyHint = el("keyHint");
    if (keyHint) {
      if (p.api_key_set && p.api_key) {
        keyHint.textContent = i18n("set.prov.key.loaded");
      } else if (p.api_key_set) {
        keyHint.textContent = i18n("set.prov.key.saved");
      } else if (p.needs_key) {
        keyHint.textContent = i18n("set.prov.key.required");
      } else {
        keyHint.textContent = i18n("set.prov.key.none");
      }
    }
    const status = el("providerStatus");
    if (status) {
      if (p.active) {
        status.textContent = i18n("set.prov.status.active");
      } else if (p.ready) {
        status.textContent = i18n("set.prov.status.ready");
      } else if (p.needs_key && !p.api_key_set) {
        status.textContent = i18n("set.prov.status.need_key");
      } else if (p.custom && !p.api_base) {
        status.textContent = i18n("set.prov.status.need_base");
      } else {
        status.textContent = i18n("set.prov.status.none");
      }
    }
  }

  function renderProviders() {
    const list = el("providerList");
    if (!list) return;
    list.innerHTML = "";
    if (!providers.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = i18n("set.prov.loading");
      list.appendChild(empty);
      return;
    }

    /** @type {Record<string, any[]>} */
    const groups = {};
    providers.forEach((p) => {
      const cat = p.category || "Other";
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(p);
    });

    CATEGORY_ORDER.forEach((cat) => {
      const items = groups[cat];
      if (!items || !items.length) return;
      const head = document.createElement("div");
      head.className = "pick-group-label";
      // The five buckets the core sorts providers into; the catalogue names
      // them again so the heading reads in the chosen language.
      head.textContent = i18n("set.prov.cat." + cat.toLowerCase());
      list.appendChild(head);
      items.forEach((p) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "list-item pick-item" + (p.key === selectedProviderKey ? " selected" : "");
        btn.appendChild(makeProviderIconEl(p.key));
        const title = document.createElement("span");
        title.className = "pick-title";
        title.textContent = p.name || p.key;
        btn.appendChild(title);
        appendProviderBadge(btn, p);
        btn.addEventListener("click", () => {
          selectedProviderKey = p.key;
          apiKeyVisible = false;
          populateProviderCreds(p, { force: true });
          if (p.active && p.current_model) {
            selectedModelId = p.current_model;
          } else if (p.models && p.models.length) {
            selectedModelId = p.models[0].id || "";
          }
          syncHiddenFields();
          renderProviders();
          renderModels();
          updateCredBlock();
        });
        list.appendChild(btn);
      });
    });
    updateCredBlock();
  }

  function renderModels() {
    const list = el("modelList");
    const status = el("modelsStatus");
    const p = findProvider(selectedProviderKey);
    if (!list) return;
    list.innerHTML = "";
    if (!p) {
      if (status) status.textContent = i18n("set.models.select_provider");
      return;
    }
    if (p.models_error) {
      if (status) status.textContent = i18n("set.models.failed", { detail: p.models_error });
    } else if (!p.ready) {
      if (status) status.textContent = i18n("set.models.configure_first");
    } else if (!p.models || !p.models.length) {
      if (status) status.textContent = i18n("set.models.none_returned");
    } else {
      if (status)
        status.textContent = i18n("set.models.count_from", {
          n: p.models.length,
          provider: p.name || p.key,
        });
    }
    const models = p.models || [];
    const q = (modelSearchFilter || "").trim().toLowerCase();
    const filtered = q ? models.filter((m) => (m.id || "").toLowerCase().includes(q)) : models;
    if (!filtered.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = q
        ? i18n("set.models.no_match", { q: modelSearchFilter })
        : i18n(p.ready ? "set.models.none_listed" : "set.models.not_ready");
      list.appendChild(empty);
      return;
    }
    if (status && filtered.length !== models.length) {
      status.textContent = i18n("set.models.filtered", {
        shown: filtered.length,
        total: models.length,
      });
    }
    filtered.forEach((m) => {
      const id = m.id || "";
      const btn = document.createElement("button");
      btn.type = "button";
      const isActive = id === activeModelId && p.key === activeProviderKey;
      const isSelected = id === selectedModelId;
      btn.className = "list-item pick-item" + (isSelected ? " selected" : "");
      const title = document.createElement("span");
      title.className = "pick-title mono";
      title.textContent = id;
      btn.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge" + (isActive ? " running" : " ok");
      const ctx = formatContextTokens(m.context_tokens);
      if (isActive)
        badge.textContent = ctx ? i18n("set.models.active_ctx", { ctx }) : i18n("set.models.active");
      else if (ctx) badge.textContent = ctx;
      else badge.textContent = m.owned_by || "";
      btn.appendChild(badge);
      btn.addEventListener("click", () => {
        selectedModelId = id;
        syncHiddenFields();
        renderModels();
      });
      list.appendChild(btn);
    });
  }

  /** @param {any} catalog @param {boolean} [resetSelection] */
  function applyProviderCatalog(catalog, resetSelection) {
    if (!catalog) return;
    providers = catalog.providers || [];
    activeProviderKey = catalog.activeProvider || catalog.active_provider || "";
    activeModelId = catalog.activeModel || catalog.active_model || "";
    if (resetSelection || !selectedProviderKey) {
      selectedProviderKey = activeProviderKey || (providers[0] && providers[0].key) || "";
      selectedModelId = activeModelId || "";
    }
    const cur = findProvider(selectedProviderKey);
    if (cur && cur.active && cur.current_model) {
      selectedModelId = cur.current_model;
    } else if (resetSelection && activeModelId) {
      selectedModelId = activeModelId;
    }
    syncHiddenFields();
    renderProviders();
    renderModels();
    if (cur) populateProviderCreds(cur, { force: true });
  }

  el("saveGeneral")?.addEventListener("click", () => {
    showError("");
    // orchestraSavePayload() lives in 03-orchestra.js (same bundle scope):
    // the General tab contains the Orchestra routing section, so its Save
    // must persist the role/model picks too — otherwise pushState() would
    // re-render from disk and silently wipe the unsaved selections.
    const orch = orchestraSavePayload();
    vscode.postMessage({
      type: "saveGeneral",
      binaryPath: input("binaryPath")?.value || "",
      projectRoot: input("projectRoot")?.value || "",
      ...(orch || {}),
    });
  });

  el("saveModels")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "saveModels", ...modelsPayload() });
  });

  el("refreshModels")?.addEventListener("click", () => {
    showError("");
    const status = el("modelsStatus");
    if (status) status.textContent = i18n("set.models.refreshing");
    vscode.postMessage({
      type: "refreshModels",
      provider: selectedProviderKey || "",
      apiBase: input("apiBase")?.value || "",
      apiKey: input("apiKey")?.value || "",
    });
  });

  input("modelSearch")?.addEventListener("input", () => {
    modelSearchFilter = input("modelSearch")?.value || "";
    renderModels();
  });

  el("toggleApiKey")?.addEventListener("click", () => {
    apiKeyVisible = !apiKeyVisible;
    applyApiKeyVisibility();
  });
  function orchProviderOptions() {
    const ready = providers.filter((p) => p.ready || p.configured || p.active);
    const opts = [{ key: "", label: i18n("set.prov.main_global") }];
    ready.forEach((p) => opts.push({ key: p.key, label: p.name || p.key }));
    return opts;
  }

  /** @param {string} value @param {(key: string) => void} onChange */
  function buildOrchProviderSelect(value, onChange) {
    return buildProviderDropdown(value, onChange, orchProviderOptions());
  }

  /** @param {string[]} models */
  function renderOrchPickLabel(models) {
    if (!models.length) return "Pick models…";
    if (models.length === 1) return models[0];
    const first = models[0];
    const rest = models.length - 1;
    return `${first} +${rest}`;
  }

  /** @param {string[]} models @param {HTMLElement} host */
  function renderOrchPickChips(models, host) {
    host.innerHTML = "";
    if (!models.length) {
      host.textContent = i18n("set.orch.pick_models");
      host.classList.remove("has-models");
      return;
    }
    host.classList.add("has-models");
    models.forEach((id, i) => {
      const chip = document.createElement("span");
      chip.className = "orch-chip";
      const slot = document.createElement("span");
      slot.className = "orch-chip-slot";
      slot.textContent = String(i + 1);
      chip.appendChild(slot);
      const label = document.createElement("span");
      label.className = "orch-chip-label";
      label.textContent = id;
      label.title = id;
      chip.appendChild(label);
      host.appendChild(chip);
    });
  }

  /**
   * A role's name for the reader. The core sends one, in English; the
   * catalogue has the same six roles and answers in the chosen language. A
   * role the core adds later is not in the catalogue, so its own label stands.
   * @param {any} role
   */
  function orchRoleName(role) {
    const key = role && role.key;
    if (key && ORCH_ROLE_INFO_KEYS.indexOf(key) >= 0) {
      return i18n("orch.role." + key);
    }
    return (role && (role.label || role.key)) || "";
  }

  function orchRoleByKey(key) {
    const roles = (orchestraConfig && orchestraConfig.roles) || [];
    return roles.find((r) => r.key === key);
  }

  /** Canonical L-tier for a role: core-provided or legacy_map defaults. */
  /** @param {any} role */
  function orchRoleTier(role) {
    if (role && role.tier) return String(role.tier);
    const defaults = { planner: "L5", lead: "L4", complex: "L3", focused: "L3", micro: "L1", embed: "EMB" };
    return defaults[role && role.key] || "";
  }

  /**
   * Per-role hover help: what the role does + model examples (spec §1–2).
   * The text itself lives in media/i18n.js under set.role.<key>.*; this is the
   * set of roles that have help, in the order the catalogue carries them.
   */
  const ORCH_ROLE_INFO_KEYS = ["planner", "lead", "complex", "focused", "micro", "embed"];

  /** @param {string} key */
  function orchRoleInfo(key) {
    if (!key || ORCH_ROLE_INFO_KEYS.indexOf(key) < 0) return null;
    return {
      title: i18n("set.role." + key + ".title"),
      desc: i18n("set.role." + key + ".desc"),
      example: i18n("set.role." + key + ".example"),
    };
  }

  /** @param {any} role @returns {HTMLElement | null} */
  function buildOrchInfoIcon(role) {
    const info = orchRoleInfo(role && role.key);
    if (!info) return null;
    const wrap = document.createElement("span");
    wrap.className = "orch-info";
    wrap.tabIndex = 0;
    wrap.setAttribute("aria-label", `${info.title}: ${info.desc}`);
    wrap.textContent = "i";
    const tip = document.createElement("span");
    tip.className = "orch-tip";
    const t = document.createElement("strong");
    t.textContent = info.title;
    tip.appendChild(t);
    const d = document.createElement("span");
    d.className = "orch-tip-desc";
    d.textContent = info.desc;
    tip.appendChild(d);
    const ex = document.createElement("span");
    ex.className = "orch-tip-example";
    ex.textContent = info.example;
    tip.appendChild(ex);
    wrap.appendChild(tip);
    return wrap;
  }

  /** Effective provider key for a role (never unrelated fallbacks). */
  /** @param {any} role */
  function effectiveRoleProvider(role) {
    if (!role) return orchSharedProvider || orchestraConfig?.mainProvider || activeProviderKey || "";
    if (role.provider !== undefined && role.provider !== null) {
      const explicit = String(role.provider).trim();
      if (explicit === "") {
        return orchestraConfig?.mainProvider || activeProviderKey || "";
      }
      return explicit;
    }
    if (orchSharedProvider) return orchSharedProvider;
    return orchestraConfig?.mainProvider || activeProviderKey || "";
  }

  /** @param {string} provKey */
  function providerModelIds(provKey) {
    const key = (provKey || "").trim();
    const p = key ? findProvider(key) : findProvider(orchestraConfig?.mainProvider || activeProviderKey || "");
    if (!p || !Array.isArray(p.models)) return [];
    return p.models.map((m) => m.id || "").filter(Boolean);
  }

  /** Drop models that are not offered by the role's provider. */
  /** @param {any} role */
  function sanitizeRoleModels(role) {
    if (!role) return;
    const allowed = new Set(providerModelIds(effectiveRoleProvider(role)));
    let models =
      role.models && role.models.length ? role.models.slice() : role.model ? [role.model] : [];
    // Filter only when we actually know the provider's model list. An empty
    // list means the catalog probe hasn't loaded (or failed) — wiping the
    // persisted picks here would destroy saved settings on the next Save.
    if (allowed.size > 0) {
      const filtered = models.filter((id) => allowed.has(id));
      if (filtered.length > 0 || models.length === 0) {
        models = filtered;
      }
      // else: none matched — likely a stale/partial catalog; keep saved picks.
    }
    models = models.slice(0, maxModelsForRole(role));
    role.models = models;
    role.model = models[0] || "";
  }

  function sanitizeOrchestraRoles() {
    if (!orchestraConfig?.roles) return;
    orchestraConfig.roles.forEach((role) => sanitizeRoleModels(role));
  }

  /** @param {any} role @param {string} provKey */
  function setRoleProvider(role, provKey) {
    role.provider = provKey;
    role.models = [];
    role.model = "";
  }

  function maxModelsForRole(role) {
    return role && role.key === "embed" ? 1 : MAX_ORCH_MODELS;
  }

  /** @param {string} id */
  function isEmbeddingModelId(id) {
    const s = String(id || "").toLowerCase();
    return /embed|embedding|nomic|bge-|e5-|gte-|minilm|voyage/.test(s);
  }

  function roleModelsList(role) {
    if (role.models && role.models.length) return role.models.slice();
    if (role.model) return [role.model];
    return [];
  }

  function renderOrchestra() {
    const host = el("orchRoles");
    const sharedHost = el("orchSharedProviderWrap");
    if (!host || !orchestraConfig) return;

    if (sharedHost) {
      sharedHost.innerHTML = "";
      const sharedSel = buildOrchProviderSelect(orchSharedProvider, (key) => {
        orchSharedProvider = key;
        if (!orchestraConfig) return;
        orchestraConfig.roles.forEach((r) => {
          setRoleProvider(r, key);
        });
        renderOrchestra();
      });
      sharedHost.appendChild(sharedSel);
    }

    host.innerHTML = "";
    (orchestraConfig.roles || []).forEach((role) => {
      const row = document.createElement("div");
      row.className = "orch-row";
      const title = document.createElement("div");
      title.className = "orch-row-title";
      const tier = orchRoleTier(role);
      if (tier) {
        const badge = document.createElement("span");
        badge.className = "orch-tier orch-tier-" + tier.toLowerCase();
        badge.textContent = tier;
        badge.title = i18n("set.orch.tier_title", { tier });
        title.appendChild(badge);
      }
      const name = document.createElement("span");
      name.className = "orch-role-name";
      name.textContent = orchRoleName(role);
      title.appendChild(name);
      const infoIcon = buildOrchInfoIcon(role);
      if (infoIcon) title.appendChild(infoIcon);
      const provValue =
        role.provider !== undefined && role.provider !== null ? role.provider : orchSharedProvider || "";
      const provWrap = buildOrchProviderSelect(provValue, (key) => {
        setRoleProvider(role, key);
        renderOrchestra();
      });
      const models = roleModelsList(role);
      const pick = document.createElement("button");
      pick.type = "button";
      pick.className = "orch-pick" + (models.length ? " has-models" : "");
      pick.title = models.length
        ? models.map((m, i) => `${i + 1}. ${m}`).join("\n")
        : i18n(role.key === "embed" ? "set.orch.pick_embed" : "set.orch.pick_up_to_3");
      const pickInner = document.createElement("span");
      pickInner.className = "orch-pick-inner";
      renderOrchPickChips(models, pickInner);
      pick.appendChild(pickInner);
      pick.addEventListener("click", () => openOrchModal(role.key));
      row.appendChild(title);
      row.appendChild(provWrap);
      row.appendChild(pick);
      host.appendChild(row);
    });

    const tier = el("orchDefaultTier");
    if (tier) tier.value = orchestraConfig.defaultTier || "focused";
    const verify = /** @type {HTMLInputElement | null} */ (el("orchVerifyEnabled"));
    if (verify) verify.checked = orchestraConfig.workerVerifyEnabled !== false;
    const llmV = /** @type {HTMLInputElement | null} */ (el("orchLLMVerify"));
    if (llmV) llmV.checked = Boolean(orchestraConfig.workerLLMVerifyEnabled);
    const maxR = input("orchMaxRetries");
    if (maxR) maxR.value = String(orchestraConfig.maxWorkerRetries ?? 3);
    const maxVR = input("orchMaxVerifyRetries");
    if (maxVR) maxVR.value = String(orchestraConfig.maxWorkerVerifyRetries ?? 1);
  }

  function renderOrchModalSlots() {
    const slots = el("orchModalSlots");
    if (!slots) return;
    const role = orchModalRoleKey ? orchRoleByKey(orchModalRoleKey) : null;
    const max = maxModelsForRole(role);
    slots.innerHTML = "";
    for (let i = 0; i < max; i++) {
      const slot = document.createElement("div");
      slot.className = "orch-slot" + (orchModalSelection[i] ? " filled" : "");
      const label = document.createElement("span");
      label.className = "orch-slot-label";
      label.textContent =
        role && role.key === "embed"
          ? i18n("set.orch.slot.embed")
          : ORCH_SLOT_LABEL_KEYS[i]
            ? i18n(ORCH_SLOT_LABEL_KEYS[i])
            : i18n("set.orch.slot.n", { n: i + 1 });
      slot.appendChild(label);
      const val = document.createElement("span");
      val.className = "orch-slot-val";
      val.textContent = orchModalSelection[i] || "—";
      val.title = orchModalSelection[i] || "";
      slot.appendChild(val);
      slots.appendChild(slot);
    }
    const hint = el("orchModalHint");
    if (hint) {
      if (role && role.key === "embed") {
        hint.textContent = i18n("set.orch.hint_embed");
      } else {
        hint.textContent =
          orchModalSelection.length >= max
            ? i18n("set.orch.hint_max")
            : i18n("set.orch.hint_select", { max });
      }
    }
  }

  function toggleOrchModel(id) {
    const idx = orchModalSelection.indexOf(id);
    if (idx >= 0) {
      orchModalSelection.splice(idx, 1);
    } else if (orchModalSelection.length < maxModelsForRole(orchRoleByKey(orchModalRoleKey))) {
      orchModalSelection.push(id);
    }
    renderOrchModalSlots();
    renderOrchModalList();
  }

  function openOrchModal(roleKey) {
    const role = orchRoleByKey(roleKey);
    if (!role) return;
    sanitizeRoleModels(role);
    orchModalRoleKey = roleKey;
    const existing = roleModelsList(role);
    orchModalSelection = existing.slice(0, maxModelsForRole(role));
    orchModalSearch = "";
    orchModalMinContext = 0;
    const search = input("orchModelSearch");
    if (search) search.value = "";
    const contextFilter = /** @type {HTMLSelectElement | null} */ (el("orchContextFilter"));
    if (contextFilter) {
      contextFilter.value = "0";
      contextFilter.classList.toggle("hidden", roleKey === "embed");
    }
    const title = el("orchModalTitle");
    if (title) title.textContent = i18n("set.orch.modal_title_role", { role: orchRoleName(role) });
    renderOrchModalSlots();
    renderOrchModalList();
    el("orchModelModal")?.classList.remove("hidden");
  }

  function renderOrchModalList() {
    const list = el("orchModelPickList");
    if (!list) return;
    list.innerHTML = "";
    const role = orchModalRoleKey ? orchRoleByKey(orchModalRoleKey) : null;
    const provKey = effectiveRoleProvider(role);
    const p = findProvider(provKey);
    if (!p || !p.models || !p.models.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = p?.models_error || i18n("set.orch.no_models");
      list.appendChild(empty);
      return;
    }
    const q = (orchModalSearch || "").trim().toLowerCase();
    let models = p.models.filter((m) => {
      const matchesSearch = !q || (m.id || "").toLowerCase().includes(q);
      const contextTokens = Number(m.context_tokens) || 0;
      const matchesContext = orchModalMinContext <= 0 || contextTokens >= orchModalMinContext;
      return matchesSearch && matchesContext;
    });
    if (role && role.key === "embed") {
      const embeddingOnly = models.filter((m) => isEmbeddingModelId(m.id));
      if (embeddingOnly.length) models = embeddingOnly;
    }
    if (!models.length) {
      const empty = document.createElement("div");
      empty.className = "hint orch-filter-empty";
      empty.textContent = i18n("set.orch.filter_empty");
      list.appendChild(empty);
      return;
    }
    const atMax = orchModalSelection.length >= maxModelsForRole(role);
    models.forEach((m) => {
      const id = m.id || "";
      const selIdx = orchModalSelection.indexOf(id);
      const selected = selIdx >= 0;
      const row = document.createElement("button");
      row.type = "button";
      row.className = "orch-model-row" + (selected ? " selected" : "") + (!selected && atMax ? " dimmed" : "");
      if (selected) {
        const slot = document.createElement("span");
        slot.className = "orch-row-slot";
        slot.textContent = String(selIdx + 1);
        row.appendChild(slot);
      } else {
        const dot = document.createElement("span");
        dot.className = "orch-row-dot";
        row.appendChild(dot);
      }
      const name = document.createElement("span");
      name.className = "orch-row-name";
      name.textContent = id;
      row.appendChild(name);
      const ctx = formatContextTokens(m.context_tokens);
      if (ctx) {
        const meta = document.createElement("span");
        meta.className = "orch-row-meta";
        meta.textContent = ctx;
        row.appendChild(meta);
      }
      row.addEventListener("click", () => {
        if (!selected && atMax) return;
        toggleOrchModel(id);
      });
      list.appendChild(row);
    });
  }

  input("orchModelSearch")?.addEventListener("input", () => {
    orchModalSearch = input("orchModelSearch")?.value || "";
    renderOrchModalList();
  });

  el("orchContextFilter")?.addEventListener("change", () => {
    const filter = /** @type {HTMLSelectElement | null} */ (el("orchContextFilter"));
    orchModalMinContext = Number(filter?.value) || 0;
    renderOrchModalList();
  });

  el("orchModalClose")?.addEventListener("click", () => {
    el("orchModelModal")?.classList.add("hidden");
    orchModalRoleKey = null;
  });

  el("orchModalApply")?.addEventListener("click", () => {
    const role = orchModalRoleKey ? orchRoleByKey(orchModalRoleKey) : null;
    if (role) {
      role.models = orchModalSelection.slice(0, maxModelsForRole(role));
      role.model = role.models[0] || "";
    }
    el("orchModelModal")?.classList.add("hidden");
    orchModalRoleKey = null;
    renderOrchestra();
  });

  /**
   * Orchestra routing fields for a save message (roles + verification knobs).
   * Used by the General tab Save button so one click persists the whole tab.
   * @returns {Record<string, any> | null}
   */
  function orchestraSavePayload() {
    if (!orchestraConfig) return null;
    return {
      roles: orchestraConfig.roles,
      defaultTier: el("orchDefaultTier")?.value || "focused",
      maxWorkerRetries: input("orchMaxRetries") ? Number(input("orchMaxRetries").value) : undefined,
      workerVerifyEnabled: /** @type {HTMLInputElement | null} */ (el("orchVerifyEnabled"))?.checked,
      maxWorkerVerifyRetries: input("orchMaxVerifyRetries")
        ? Number(input("orchMaxVerifyRetries").value)
        : undefined,
      workerLLMVerifyEnabled: /** @type {HTMLInputElement | null} */ (el("orchLLMVerify"))?.checked,
    };
  }

  el("refreshOrchModels")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "refreshOrchModels" });
  });
  el("saveIndex")?.addEventListener("click", () => {
    showError("");
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    vscode.postMessage({
      type: "saveIndex",
      excludeDirs: area("excludeDirs")?.value || "",
      contextLimitKB: input("contextLimitKB") ? Number(input("contextLimitKB").value) : undefined,
      limitsMaxFiles: input("limitsMaxFiles") ? Number(input("limitsMaxFiles").value) : undefined,
      embedBatchSize: input("embedBatchSize") ? Number(input("embedBatchSize").value) : undefined,
      semanticAutoExplore: sem ? sem.checked : undefined,
    });
  });

  el("rebuildGraph")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = i18n("set.index.rebuilding");
    vscode.postMessage({ type: "rebuildGraph" });
  });

  el("runEmbed")?.addEventListener("click", () => {
    showError("");
    const out = el("indexActionOut");
    if (out) out.textContent = i18n("set.index.embedding");
    vscode.postMessage({ type: "runEmbed", rebuild: false });
  });

  el("openGraph")?.addEventListener("click", () => {
    vscode.postMessage({ type: "openGraphViewer", port: graphUIPort });
  });

  el("reload")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "reload" });
  });

  el("savePrompt")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({
      type: "savePrompt",
      content: area("systemPrompt")?.value || "",
      promptFamily: input("promptFamily")?.value || "",
    });
  });

  el("clearPrompt")?.addEventListener("click", () => {
    showError("");
    vscode.postMessage({ type: "clearPrompt" });
  });

  el("newAgent")?.addEventListener("click", () => {
    if (input("agentName")) input("agentName").value = "";
    if (area("agentPrompt")) area("agentPrompt").value = "";
    renderAgentToolsList(null);
  });

  el("saveAgent")?.addEventListener("click", () => {
    showError("");
    const tools = collectAgentTools();
    if (tools === null) {
      showError(i18n("set.agent.need_one_tool"));
      return;
    }
    vscode.postMessage({
      type: "upsertAgent",
      name: input("agentName")?.value || "",
      system_prompt: area("agentPrompt")?.value || "",
      tools,
    });
  });

  el("deleteAgent")?.addEventListener("click", () => {
    const name = input("agentName")?.value?.trim();
    if (!name) return;
    showError("");
    vscode.postMessage({ type: "deleteAgent", name });
  });

  /**
   * @returns {string[] | "" | null}
   * "" / empty array meaning inherit (all on);
   * string[] allowlist;
   * null = invalid (none enabled).
   */
  function collectAgentTools() {
    const host = el("agentToolsList");
    if (!host) return "";
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) return "";
    const on = [];
    let off = 0;
    boxes.forEach((box) => {
      const name = box.getAttribute("data-tool") || "";
      if (box.checked) {
        if (name) on.push(name);
      } else {
        off += 1;
      }
    });
    if (off === 0) return "";
    if (!on.length) return null;
    return on;
  }

  /** @param {string} name */
  function agentToolCategory(name) {
    if (
      name === "ls" ||
      name === "read" ||
      name === "glob" ||
      name === "write" ||
      name === "edit" ||
      name === "fs.delete" ||
      name === "fs.rename" ||
      name === "diff.preview" ||
      name === "ast_rename"
    ) {
      return "Filesystem";
    }
    if (
      name === "grep" ||
      name === "symbols" ||
      name === "explore" ||
      name === "semantic_search" ||
      name === "repo_map"
    ) {
      return "Search & nav";
    }
    if (name === "bash" || name.startsWith("bash.")) return "Exec";
    if (name === "webfetch" || name === "websearch") return "Web";
    if (name.startsWith("browser.")) return "Browser";
    if (name.startsWith("lsp.")) return "LSP";
    if (name.startsWith("git.")) return "Git";
    if (name.startsWith("gh.")) return "GitHub";
    if (
      name === "todowrite" ||
      name === "todoread" ||
      name.startsWith("memory_") ||
      name === "runtime_query" ||
      name === "question"
    ) {
      return "Session";
    }
    if (name.startsWith("task_") || name.startsWith("plan_")) return "Tasks & plan";
    return "Other";
  }

  const AGENT_TOOL_CAT_ORDER = [
    "Filesystem",
    "Search & nav",
    "Exec",
    "Web",
    "Browser",
    "LSP",
    "Git",
    "GitHub",
    "Session",
    "Tasks & plan",
    "Other",
  ];

  /** @param {HTMLElement} section */
  function syncAgentCategoryToggle(section) {
    const master = /** @type {HTMLInputElement | null} */ (
      section.querySelector('input[data-cat-toggle]')
    );
    if (!master) return;
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      section.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    let on = 0;
    boxes.forEach((b) => {
      if (b.checked) on += 1;
    });
    master.checked = boxes.length > 0 && on === boxes.length;
    master.indeterminate = on > 0 && on < boxes.length;
    const count = section.querySelector(".agent-tools-cat-count");
    if (count) count.textContent = `${on}/${boxes.length}`;
  }

  function updateAgentToolsHint() {
    const hint = el("agentToolsHint");
    const host = el("agentToolsList");
    if (!hint || !host) return;
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) {
      hint.textContent = i18n("set.agent.catalog_na");
      return;
    }
    let on = 0;
    boxes.forEach((b) => {
      if (b.checked) on += 1;
    });
    hint.textContent =
      on === boxes.length
        ? i18n("set.agent.tools_full", { n: boxes.length })
        : i18n("set.agent.tools_on", { on, total: boxes.length });
  }

  /** @param {any} a */
  function renderAgentToolsList(a) {
    const host = el("agentToolsList");
    const hint = el("agentToolsHint");
    if (!host) return;
    const openCats = new Set();
    host.querySelectorAll(".agent-tools-cat:not(.collapsed)").forEach((d) => {
      const id = d.getAttribute("data-cat");
      if (id) openCats.add(id);
    });
    host.innerHTML = "";
    const catalog = (agentAvailableTools.length
      ? agentAvailableTools
      : Array.isArray(a?.tools)
        ? a.tools
        : []
    )
      .map((x) => String(x || "").trim())
      .filter(Boolean);
    if (!catalog.length) {
      if (hint) hint.textContent = i18n("set.agent.catalog_na");
      return;
    }
    const selected = Array.isArray(a?.tools) ? a.tools.map((x) => String(x || "")) : null;
    const allOn = !selected || !selected.length;

    /** @type {Map<string, string[]>} */
    const byCat = new Map();
    catalog.forEach((name) => {
      const cat = agentToolCategory(name);
      if (!byCat.has(cat)) byCat.set(cat, []);
      byCat.get(cat)?.push(name);
    });

    AGENT_TOOL_CAT_ORDER.forEach((cat) => {
      const tools = byCat.get(cat);
      if (!tools || !tools.length) return;
      tools.sort();

      const section = document.createElement("div");
      section.className = "agent-tools-cat";
      section.setAttribute("data-cat", cat);
      const shouldOpen = openCats.has(cat) || (openCats.size === 0 && cat === "Filesystem");
      if (!shouldOpen) section.classList.add("collapsed");

      const head = document.createElement("div");
      head.className = "agent-tools-cat-head";

      const expand = document.createElement("button");
      expand.type = "button";
      expand.className = "agent-tools-cat-expand";
      expand.setAttribute("aria-expanded", shouldOpen ? "true" : "false");
      const chevron = document.createElement("span");
      chevron.className = "agent-tools-cat-chevron";
      chevron.setAttribute("aria-hidden", "true");
      chevron.innerHTML = orchIconMarkup(shouldOpen ? "chevron-down" : "chevron-right", { size: "sm" });
      expand.appendChild(chevron);
      const title = document.createElement("span");
      title.className = "agent-tools-cat-title";
      title.textContent = cat;
      expand.appendChild(title);
      const count = document.createElement("span");
      count.className = "agent-tools-cat-count";
      expand.appendChild(count);
      expand.addEventListener("click", () => {
        const willOpen = section.classList.contains("collapsed");
        if (willOpen) {
          host.querySelectorAll(".agent-tools-cat").forEach((other) => {
            if (other === section) return;
            other.classList.add("collapsed");
            const btn = other.querySelector(".agent-tools-cat-expand");
            const chev = other.querySelector(".agent-tools-cat-chevron");
            if (btn) btn.setAttribute("aria-expanded", "false");
            if (chev) chev.innerHTML = orchIconMarkup("chevron-right", { size: "sm" });
          });
        }
        const open = section.classList.toggle("collapsed") === false;
        expand.setAttribute("aria-expanded", open ? "true" : "false");
        chevron.innerHTML = orchIconMarkup(open ? "chevron-down" : "chevron-right", { size: "sm" });
      });
      head.appendChild(expand);

      const masterWrap = document.createElement("label");
      masterWrap.className = "mcp-switch";
      masterWrap.title = i18n("set.agent.toggle_all", { cat });
      const master = document.createElement("input");
      master.type = "checkbox";
      master.setAttribute("data-cat-toggle", cat);
      const masterUi = document.createElement("span");
      masterUi.className = "mcp-switch-ui";
      masterUi.setAttribute("aria-hidden", "true");
      masterWrap.appendChild(master);
      masterWrap.appendChild(masterUi);
      head.appendChild(masterWrap);
      section.appendChild(head);

      const body = document.createElement("div");
      body.className = "agent-tools-cat-body";
      tools.forEach((name) => {
        const row = document.createElement("div");
        row.className = "agent-tool-row";
        const labelText = document.createElement("span");
        labelText.className = "agent-tool-name";
        labelText.textContent = name;
        labelText.title = name;
        row.appendChild(labelText);
        const label = document.createElement("label");
        label.className = "mcp-switch";
        label.title = name;
        const box = document.createElement("input");
        box.type = "checkbox";
        box.setAttribute("data-tool", name);
        box.checked = allOn || (selected ? selected.includes(name) : false);
        box.addEventListener("change", () => {
          syncAgentCategoryToggle(section);
          updateAgentToolsHint();
        });
        const ui = document.createElement("span");
        ui.className = "mcp-switch-ui";
        ui.setAttribute("aria-hidden", "true");
        label.appendChild(box);
        label.appendChild(ui);
        row.appendChild(label);
        body.appendChild(row);
      });
      section.appendChild(body);

      master.addEventListener("change", () => {
        const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
          section.querySelectorAll('input[type="checkbox"][data-tool]')
        );
        boxes.forEach((b) => {
          b.checked = master.checked;
        });
        master.indeterminate = false;
        syncAgentCategoryToggle(section);
        updateAgentToolsHint();
      });

      host.appendChild(section);
      syncAgentCategoryToggle(section);
    });
    updateAgentToolsHint();
  }

  function fillAgentForm(a) {
    if (!a) return;
    if (input("agentName")) input("agentName").value = a.name || "";
    if (area("agentPrompt")) area("agentPrompt").value = a.system_prompt || "";
    renderAgentToolsList(a);
  }

  function renderAgents() {
    const list = el("agentsList");
    if (!list) return;
    list.innerHTML = "";
    if (!agents.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = i18n("set.agent.none");
      list.appendChild(empty);
    } else {
      agents.forEach((a) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "list-item";
        btn.textContent = a.name;
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent =
          Array.isArray(a.tools) && a.tools.length
            ? i18n("set.agent.tools_n", { n: a.tools.length })
            : i18n("set.agent.tools_all");
        btn.appendChild(badge);
        btn.addEventListener("click", () => fillAgentForm(a));
        list.appendChild(btn);
      });
    }
    const name = input("agentName")?.value?.trim();
    const cur = name ? agents.find((x) => x.name === name) : null;
    if (cur) {
      renderAgentToolsList(cur);
      return;
    }
    const host = el("agentToolsList");
    if (!host || !host.querySelector('input[data-tool]')) {
      renderAgentToolsList(null);
    }
  }

  function renderSkills() {
    const list = el("skillsList");
    if (!list) return;
    list.innerHTML = "";
    if (!skills.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = i18n("set.skills.none");
      list.appendChild(empty);
      return;
    }
    skills.forEach((s) => {
      const row = document.createElement("div");
      row.className = "list-item static";
      const title = document.createElement("span");
      title.textContent = s.name;
      row.appendChild(title);
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = s.origin || i18n("set.skills.badge");
      badge.title = s.description || "";
      row.appendChild(badge);
      list.appendChild(row);
    });
  }

  /** @param {number} v */
  function fmtStatNum(v) {
    const n = Number(v) || 0;
    if (n >= 100000) return `${Math.round(n / 1000)}k`;
    if (n >= 10000) return `${(n / 1000).toFixed(1)}k`;
    return String(n);
  }

  /** @param {string} language */
  function languageVisual(language) {
    const raw = String(language || "").trim();
    const key = raw.toLowerCase().replace(/[^a-z0-9+#]/g, "");
    if (key === "go" || key === "golang") return { key: "go", mark: "GO" };
    if (key === "tsx" || key === "jsx") return { key: "react", mark: "Re" };
    if (key === "typescript" || key === "ts") return { key: "typescript", mark: "TS" };
    if (key === "javascript" || key === "js") return { key: "javascript", mark: "JS" };
    if (key === "python" || key === "py") return { key: "python", mark: "Py" };
    if (key === "rust" || key === "rs") return { key: "rust", mark: "Rs" };
    if (key === "java") return { key: "java", mark: "Jv" };
    if (key === "c#" || key === "csharp" || key === "cs") return { key: "csharp", mark: "C#" };
    if (key === "c++" || key === "cpp" || key === "cplusplus") return { key: "cpp", mark: "C+" };
    if (key === "c") return { key: "c", mark: "C" };
    if (key === "html") return { key: "html", mark: "H5" };
    if (key === "css" || key === "scss" || key === "sass") return { key: "css", mark: "CSS" };
    if (key === "shell" || key === "bash" || key === "powershell") return { key: "shell", mark: ">_" };
    return { key: "other", mark: raw.slice(0, 2).toUpperCase() || "·" };
  }

  function renderIndexStats(index) {
    const g = (index && index.graph) || {};
    const set = (id, v) => {
      const n = el(id);
      if (n) {
        n.textContent = g.available ? fmtStatNum(v) : "—";
        n.title = g.available ? String(Number(v) || 0) : "";
      }
    };
    set("statFiles", g.files);
    set("statNodes", g.nodes);
    set("statEdges", g.edges);
    set("statEmb", g.embeddings);
    set("statFuncs", g.funcs);
    set("statTypes", g.types);
    set("statTests", g.tests);
    set("statPkgs", g.packages);

    const langsHost = el("indexLangs");
    if (langsHost) {
      langsHost.innerHTML = "";
      const langs = (g.available && g.langs && typeof g.langs === "object" && g.langs) || {};
      const entries = Object.entries(langs)
        .filter(([, n]) => Number(n) > 0)
        .sort((a, b) => Number(b[1]) - Number(a[1]));
      const total = entries.reduce((acc, [, n]) => acc + Number(n), 0);
      for (const [lang, n] of entries.slice(0, 6)) {
        const chip = document.createElement("span");
        chip.className = "lang-chip";
        const pct = total > 0 ? Math.round((Number(n) / total) * 100) : 0;
        const visual = languageVisual(lang);
        const logo = document.createElement("span");
        logo.className = `lang-logo lang-${visual.key}`;
        logo.textContent = visual.mark;
        logo.setAttribute("aria-hidden", "true");
        const label = document.createElement("span");
        label.textContent = `${lang} · ${pct}%`;
        chip.appendChild(logo);
        chip.appendChild(label);
        chip.title = i18n("set.index.files_n", { n });
        langsHost.appendChild(chip);
      }
      langsHost.classList.toggle("hidden", entries.length === 0);
    }

    const hint = el("indexStatusHint");
    if (hint) {
      if (!g.available) {
        hint.textContent = i18n("set.index.no_ckg");
      } else {
        const miss = g.missing_embeddings || 0;
        const dbPath = g.db_path || ".orchestra/ckg.db";
        hint.textContent =
          miss > 0
            ? i18n("set.index.need_embed", { n: miss, path: dbPath })
            : i18n("set.index.graph_ready", { path: dbPath });
      }
    }
    const embedHint = el("indexEmbedHint");
    if (embedHint) {
      const emb = (index && index.embed) || {};
      const model = String(emb.model || "").trim();
      const provider = String(emb.provider || "").trim();
      if (!model) {
        embedHint.textContent = i18n("set.index.no_embed_model");
      } else {
        embedHint.textContent = provider
          ? i18n("set.index.embed_model_via", { model, provider })
          : i18n("set.index.embed_model", { model });
      }
    }
  }

  /** @param {string} cmd */
  function resolveMcpCommand(cmd) {
    const root = workspaceRoot || ".";
    return String(cmd || "").replace(/\$\{workspaceRoot\}/g, root);
  }

  /** @param {any} entry */
  function catalogEntryToForm(entry) {
    const envLines = Array.isArray(entry.env) ? entry.env.slice() : [];
    return {
      name: entry.name || entry.id || "",
      command: resolveMcpCommand(entry.command || ""),
      env: envLines.join("\n"),
      allowed_tools: "",
      call_timeout_s: 0,
      disabled: false,
      envRequired: Boolean(entry.envRequired),
      title: entry.title || entry.name || "",
    };
  }

  /** @param {"browse" | "installed"} tab */
  function setMcpTab(tab) {
    mcpTab = tab === "installed" ? "installed" : "browse";
    document.querySelectorAll(".mcp-tab").forEach((btn) => {
      const on = btn.getAttribute("data-mcp-tab") === mcpTab;
      btn.classList.toggle("active", on);
      btn.setAttribute("aria-selected", on ? "true" : "false");
    });
    el("mcpBrowsePane")?.classList.toggle("hidden", mcpTab !== "browse");
    el("mcpInstalledPane")?.classList.toggle("hidden", mcpTab !== "installed");
  }

  /** @param {string} name */
  function findInstalledMcp(name) {
    const key = (name || "").toLowerCase();
    return mcpServers.find((s) => String(s.name || "").toLowerCase() === key);
  }

  function fillMcpForm(s) {
    if (!s) return;
    mcpIsNewCustom = false;
    if (input("mcpName")) input("mcpName").value = s.name || "";
    if (input("mcpCommand")) {
      input("mcpCommand").value = Array.isArray(s.command) ? s.command.join(" ") : s.command || "";
    }
    if (area("mcpEnv")) {
      if (typeof s.env === "string") {
        area("mcpEnv").value = s.env;
      } else {
        const env = s.env || {};
        area("mcpEnv").value = Object.keys(env)
          .map((k) => `${k}=${env[k]}`)
          .join("\n");
      }
    }
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (enabled) enabled.checked = !Boolean(s.disabled);
    const title = el("mcpCfgTitle");
    if (title)
      title.textContent = i18n("set.mcp.configure_named", { name: s.name || i18n("set.mcp.server") });
    const sub = el("mcpCfgSub");
    if (sub) {
      const n = Number(s.tool_count) || (Array.isArray(s.tools) ? s.tools.length : 0);
      sub.textContent = s.disabled
        ? i18n("set.mcp.off")
        : n > 0
          ? i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n })
          : s.status || i18n("set.mcp.installed");
    }
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = i18n("set.mcp.command");
    const srcMeta = el("mcpCfgSourceMeta");
    if (srcMeta) {
      srcMeta.textContent = Array.isArray(s.command) ? s.command.join(" ") : s.command || "—";
    }
    mcpDraftTools = Array.isArray(s.tools) && s.tools.length ? s.tools.slice() : [];
    renderMcpToolsList(s);
    updateMcpDeleteVisibility(s);
    openMcpConfigure(true);
    if (!mcpDraftTools.length && !s.disabled && (s.command || []).length) {
      requestMcpToolsProbe(false);
    }
  }

  function clearMcpForm() {
    mcpIsNewCustom = true;
    mcpDraftTools = [];
    if (input("mcpName")) input("mcpName").value = "";
    if (input("mcpCommand")) input("mcpCommand").value = "";
    if (area("mcpEnv")) area("mcpEnv").value = "";
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (enabled) enabled.checked = true;
    const title = el("mcpCfgTitle");
    if (title) title.textContent = i18n("set.mcp.configure_custom");
    const sub = el("mcpCfgSub");
    if (sub) sub.textContent = i18n("set.mcp.new_server");
    const srcLabel = el("mcpCfgSourceLabel");
    if (srcLabel) srcLabel.textContent = i18n("set.mcp.custom");
    const srcMeta = el("mcpCfgSourceMeta");
    if (srcMeta) srcMeta.textContent = i18n("set.mcp.enter_command");
    const out = el("mcpTestOut");
    if (out) out.textContent = "";
    renderMcpToolsList(null);
    updateMcpDeleteVisibility(null);
    openMcpConfigure(true);
  }

  /** @param {boolean} open */
  function openMcpConfigure(open) {
    mcpConfigureOpen = Boolean(open);
    el("mcpConfigure")?.classList.toggle("hidden", !mcpConfigureOpen);
  }

  /** @param {any} s */
  function updateMcpDeleteVisibility(s) {
    const btn = /** @type {HTMLButtonElement | null} */ (el("deleteMCP"));
    if (!btn) return;
    const name = String(s?.name || "").trim();
    const exists = Boolean(name && findInstalledMcp(name));
    btn.classList.toggle("hidden", !exists);
  }

  /** @param {any} s */
  function mcpStatusLabel(s) {
    if (!s) return "";
    if (s.disabled) return i18n("set.mcp.off");
    if (s.status === "error") return s.error || i18n("set.mcp.error");
    const n = Number(s.tool_count) || 0;
    if (s.status === "running" || n > 0) {
      return i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n });
    }
    if (s.status === "stopped") return i18n("set.mcp.stopped");
    return s.status || i18n("set.mcp.installed");
  }

  /**
   * Collect allowed_tools from toggles.
   * Empty string = all tools enabled.
   * @returns {string}
   */
  function collectAllowedTools() {
    const host = el("mcpToolsList");
    if (!host) return "";
    const boxes = /** @type {NodeListOf<HTMLInputElement>} */ (
      host.querySelectorAll('input[type="checkbox"][data-tool]')
    );
    if (!boxes.length) return "";
    const on = [];
    let off = 0;
    boxes.forEach((box) => {
      if (box.checked) on.push(box.getAttribute("data-tool") || "");
      else off += 1;
    });
    if (off === 0) return "";
    return on.filter(Boolean).join(", ");
  }

  /** @param {any} s */
  function renderMcpToolsList(s) {
    const host = el("mcpToolsList");
    const hint = el("mcpToolsHint");
    if (!host) return;
    host.innerHTML = "";
    const allowed = Array.isArray(s?.allowed_tools) ? s.allowed_tools : [];
    const allAllowed = !allowed.length;
    const tools = mcpDraftTools.length
      ? mcpDraftTools
      : Array.isArray(s?.tools)
        ? s.tools
        : allowed.slice();
    if (mcpToolsLoading) {
      if (hint) hint.textContent = i18n("set.mcp.loading_tools");
      return;
    }
    if (!tools.length) {
      if (hint) {
        hint.textContent = i18n(s?.disabled ? "set.mcp.turn_on" : "set.mcp.no_tools");
      }
      return;
    }
    if (hint)
      hint.textContent = i18n(tools.length === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", {
        n: tools.length,
      });
    tools.forEach((name) => {
      const row = document.createElement("div");
      row.className = "mcp-tool-row";
      const code = document.createElement("code");
      code.textContent = name;
      row.appendChild(code);
      const label = document.createElement("label");
      label.className = "mcp-switch";
      label.title = name;
      const box = document.createElement("input");
      box.type = "checkbox";
      box.setAttribute("data-tool", name);
      box.checked = allAllowed || allowed.includes(name);
      const ui = document.createElement("span");
      ui.className = "mcp-switch-ui";
      ui.setAttribute("aria-hidden", "true");
      label.appendChild(box);
      label.appendChild(ui);
      row.appendChild(label);
      host.appendChild(row);
    });
  }

  /** @param {boolean} showStatus */
  function requestMcpToolsProbe(showStatus) {
    const payload = mcpFormPayload();
    if (!payload.name && !payload.command) return;
    mcpToolsLoading = true;
    renderMcpToolsList(findInstalledMcp(payload.name) || payload);
    if (showStatus) {
      const out = el("mcpTestOut");
      if (out) out.textContent = i18n("set.mcp.reloading");
    }
    vscode.postMessage({ type: "testMCP", ...payload });
  }

  function renderMcpCatalogCats() {
    const host = el("mcpCatalogCats");
    if (!host) return;
    if (mcpCatalogCategory === "Official") mcpCatalogCategory = "All";
    host.innerHTML = "";
    const all = mcpCatalog.entries || [];
    MCP_CAT_ORDER.forEach((cat) => {
      const count =
        cat === "All" ? all.length : all.filter((e) => entryMatchesCategoryFor(cat, e)).length;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "mcp-cat" + (mcpCatalogCategory === cat ? " active" : "");
      btn.textContent = `${i18n("set.mcp.cat." + cat.toLowerCase())} (${count})`;
      btn.addEventListener("click", () => {
        mcpCatalogCategory = cat;
        mcpCatalogPage = 0;
        renderMcpCatalog();
      });
      host.appendChild(btn);
    });
  }

  function updateMcpCatalogStatus() {
    const status = el("mcpCatalogStatus");
    if (!status) return;
    if (mcpCatalogBusy && !(mcpCatalog.entries || []).length) {
      status.textContent = i18n("set.mcp.registry_loading");
      return;
    }
    const src = i18n(
      mcpCatalogSource === "registry"
        ? "set.mcp.src_registry"
        : mcpCatalogSource === "mixed"
          ? "set.mcp.src_mixed"
          : "set.mcp.src_local"
    );
    let text = src;
    const n = (mcpCatalog.entries || []).length;
    if (n) text += i18n("set.mcp.loaded_n", { n });
    if (mcpCatalogPrefetching) text += i18n("set.mcp.loading_more");
    if (mcpCatalogFilter.trim()) text += i18n("set.mcp.search_note", { q: mcpCatalogFilter.trim() });
    if (mcpCatalogError) text += ` · ${mcpCatalogError}`;
    status.textContent = text;
  }

  /** @param {string} cat @param {any} entry */
  function entryMatchesCategoryFor(cat, entry) {
    if (cat === "All") return true;
    if (cat === "Installable") return entry.installable !== false && Boolean(entry.command);
    if (cat === "Featured") return entry.source === "local" || (entry.tags || []).includes("featured");
    if (cat === "Remote") return entry.installable === false || (entry.tags || []).includes("remote") || entry.category === "Remote";
    return (entry.category || "") === cat;
  }

  /** @param {any} entry */
  function entryMatchesCategory(entry) {
    return entryMatchesCategoryFor(mcpCatalogCategory, entry);
  }

  function filteredMcpEntries() {
    return (mcpCatalog.entries || []).filter((e) => entryMatchesCategory(e));
  }

  function updateMcpPager(totalFiltered) {
    const pages = Math.max(1, Math.ceil(totalFiltered / MCP_PAGE_SIZE) || 1);
    if (mcpCatalogPage > pages - 1) mcpCatalogPage = pages - 1;
    if (mcpCatalogPage < 0) mcpCatalogPage = 0;
    const label = el("mcpCatalogPageLabel");
    if (label) label.textContent = `${mcpCatalogPage + 1} / ${pages}`;
    const prev = /** @type {HTMLButtonElement | null} */ (el("mcpCatalogPrev"));
    const next = /** @type {HTMLButtonElement | null} */ (el("mcpCatalogNext"));
    if (prev) prev.disabled = mcpCatalogPage <= 0;
    if (next) next.disabled = mcpCatalogPage >= pages - 1 || totalFiltered === 0;
  }

  function renderMcpCatalog() {
    renderMcpCatalogCats();
    updateMcpCatalogStatus();
    const list = el("mcpCatalogList");
    if (!list) return;
    list.innerHTML = "";
    const filtered = filteredMcpEntries();
    updateMcpPager(filtered.length);
    const start = mcpCatalogPage * MCP_PAGE_SIZE;
    const entries = filtered.slice(start, start + MCP_PAGE_SIZE);
    if (!entries.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = mcpCatalogBusy
        ? i18n("set.mcp.empty_loading")
        : i18n(mcpCatalogFilter.trim() ? "set.mcp.empty_search" : "set.mcp.empty_catalog");
      list.appendChild(empty);
      return;
    }
    entries.forEach((entry) => {
      const installed = findInstalledMcp(entry.name || entry.id);
      const card = document.createElement("div");
      card.className =
        "mcp-card" + (installed ? " installed" : "") + (entry.installable === false ? " remote-only" : "");

      const head = document.createElement("div");
      head.className = "mcp-card-head";

      const titleWrap = document.createElement("div");
      titleWrap.className = "mcp-card-title-wrap";
      const titleRow = document.createElement("div");
      titleRow.className = "mcp-card-name-row";
      const title = document.createElement("strong");
      title.textContent = entry.title || entry.name || entry.id;
      titleRow.appendChild(title);
      if (entry.version) {
        const ver = document.createElement("span");
        ver.className = "mcp-card-version";
        ver.textContent = `v${entry.version}`;
        titleRow.appendChild(ver);
      }
      titleWrap.appendChild(titleRow);
      if (entry.name && entry.name !== (entry.title || "")) {
        const meta = document.createElement("div");
        meta.className = "mcp-card-meta";
        meta.textContent = entry.name;
        titleWrap.appendChild(meta);
      }
      head.appendChild(titleWrap);

      const kind = document.createElement("span");
      kind.className = "mcp-card-cat";
      kind.textContent =
        entry.installable === false
          ? i18n("set.mcp.kind_remote")
          : entry.source === "local"
            ? i18n("set.mcp.kind_featured")
            : "stdio";
      head.appendChild(kind);
      card.appendChild(head);

      const desc = document.createElement("p");
      desc.className = "mcp-card-desc";
      desc.textContent = entry.description || "";
      card.appendChild(desc);

      const actions = document.createElement("div");
      actions.className = "mcp-card-actions";
      if (installed) {
        const badge = document.createElement("span");
        badge.className = "badge " + (installed.status || "");
        badge.textContent = installed.disabled
          ? i18n("set.mcp.off")
          : Number(installed.tool_count) > 0
            ? i18n("set.mcp.tool_n", { n: installed.tool_count })
            : installed.status || i18n("set.mcp.installed");
        actions.appendChild(badge);
        const cfg = document.createElement("button");
        cfg.type = "button";
        cfg.className = "secondary";
        cfg.textContent = i18n("set.mcp.configure");
        cfg.addEventListener("click", () => {
          fillMcpForm(installed);
          setMcpTab("installed");
        });
        actions.appendChild(cfg);
      } else if (entry.installable === false || !entry.command) {
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = i18n("set.mcp.remote_only");
        badge.title = i18n("set.mcp.remote_only_title");
        actions.appendChild(badge);
      } else {
        const install = document.createElement("button");
        install.type = "button";
        install.className = "secondary";
        install.textContent = i18n(entry.envRequired ? "set.mcp.install_env" : "set.mcp.install");
        install.addEventListener("click", () => installCatalogEntry(entry));
        actions.appendChild(install);
      }
      if (entry.homepage) {
        const link = document.createElement("button");
        link.type = "button";
        link.className = "secondary";
        link.textContent = i18n("set.mcp.docs");
        link.addEventListener("click", () => {
          vscode.postMessage({ type: "openExternal", url: entry.homepage });
        });
        actions.appendChild(link);
      }
      card.appendChild(actions);
      list.appendChild(card);
    });
  }

  /** @param {any} entry */
  function installCatalogEntry(entry) {
    if (entry.installable === false || !entry.command) {
      const out = el("mcpTestOut");
      if (out) out.textContent = i18n("set.mcp.remote_not_supported");
      setMcpTab("installed");
      openMcpConfigure(true);
      return;
    }
    const form = catalogEntryToForm(entry);
    fillMcpForm(form);
    setMcpTab("installed");
    const out = el("mcpTestOut");
    if (form.envRequired) {
      if (out) {
        out.textContent = i18n("set.mcp.fill_env", { name: form.title || form.name });
      }
      area("mcpEnv")?.focus();
      return;
    }
    if (out) out.textContent = i18n("set.mcp.installing", { name: form.name });
    showError("");
    vscode.postMessage({
      type: "upsertMCP",
      name: form.name,
      command: form.command,
      env: form.env,
      allowed_tools: "",
      call_timeout_s: 0,
      disabled: false,
    });
  }

  function requestMcpRegistry() {
    mcpCatalogPage = 0;
    vscode.postMessage({
      type: "fetchMcpRegistry",
      search: mcpCatalogFilter,
    });
  }

  function renderMcp() {
    const list = el("mcpList");
    if (!list) return;
    list.innerHTML = "";
    if (!mcpServers.length) {
      const empty = document.createElement("div");
      empty.className = "hint";
      empty.textContent = i18n("set.mcp.none_installed");
      list.appendChild(empty);
    } else {
      const selectedName = (input("mcpName")?.value || "").trim().toLowerCase();
      mcpServers.forEach((s) => {
        const row = document.createElement("div");
        row.className =
          "mcp-installed-row" +
          (mcpConfigureOpen && selectedName && String(s.name || "").toLowerCase() === selectedName
            ? " active"
            : "");

        const main = document.createElement("button");
        main.type = "button";
        main.className = "mcp-installed-row-main";
        const title = document.createElement("strong");
        title.textContent = s.name;
        main.appendChild(title);
        const meta = document.createElement("span");
        meta.className =
          "mcp-installed-meta" +
          (s.disabled ? "" : s.status === "error" ? " err" : s.status === "running" ? " ok" : "");
        meta.textContent = mcpStatusLabel(s);
        if (s.error) meta.title = s.error;
        main.appendChild(meta);
        main.addEventListener("click", () => fillMcpForm(s));
        row.appendChild(main);

        const del = document.createElement("button");
        del.type = "button";
        del.className = "mcp-row-delete";
        del.title = i18n("set.mcp.remove_server");
        del.setAttribute("aria-label", i18n("set.mcp.remove_named", { name: s.name }));
        del.textContent = "×";
        del.addEventListener("click", (ev) => {
          ev.preventDefault();
          ev.stopPropagation();
          const name = String(s.name || "").trim();
          if (!name) return;
          showError("");
          openMcpConfigure(false);
          mcpIsNewCustom = false;
          vscode.postMessage({ type: "deleteMCP", name });
        });
        row.appendChild(del);

        const toggle = document.createElement("label");
        toggle.className = "mcp-switch";
        toggle.title = i18n(s.disabled ? "set.mcp.enable" : "set.mcp.disable");
        const box = document.createElement("input");
        box.type = "checkbox";
        box.checked = !Boolean(s.disabled);
        box.addEventListener("click", (ev) => ev.stopPropagation());
        box.addEventListener("change", () => {
          const name = String(s.name || "").trim();
          if (!name) return;
          showError("");
          vscode.postMessage({
            type: "setMCPDisabled",
            name,
            disabled: !box.checked,
          });
        });
        const ui = document.createElement("span");
        ui.className = "mcp-switch-ui";
        ui.setAttribute("aria-hidden", "true");
        toggle.appendChild(box);
        toggle.appendChild(ui);
        toggle.addEventListener("click", (ev) => ev.stopPropagation());
        row.appendChild(toggle);

        list.appendChild(row);
      });
    }
    if (mcpConfigureOpen) {
      const cur = findInstalledMcp(input("mcpName")?.value || "");
      if (cur) {
        const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
        if (enabled) enabled.checked = !Boolean(cur.disabled);
        if (Array.isArray(cur.tools) && cur.tools.length) {
          mcpDraftTools = cur.tools.slice();
        }
        const sub = el("mcpCfgSub");
        if (sub) sub.textContent = mcpStatusLabel(cur);
        updateMcpDeleteVisibility(cur);
        if (!mcpToolsLoading) renderMcpToolsList(cur);
      } else if (!mcpIsNewCustom) {
        openMcpConfigure(false);
      }
    }
    renderMcpCatalog();
  }

  function mcpFormPayload() {
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    const name = input("mcpName")?.value || "";
    const existing = findInstalledMcp(name);
    return {
      name,
      command: input("mcpCommand")?.value || "",
      env: area("mcpEnv")?.value || "",
      allowed_tools: collectAllowedTools(),
      call_timeout_s: Number(existing?.call_timeout_s) || 0,
      disabled: enabled ? !enabled.checked : false,
    };
  }

  document.querySelectorAll(".mcp-tab").forEach((btn) => {
    btn.addEventListener("click", () => {
      setMcpTab(/** @type {"browse" | "installed"} */ (btn.getAttribute("data-mcp-tab") || "browse"));
    });
  });

  input("mcpCatalogSearch")?.addEventListener("input", () => {
    mcpCatalogFilter = input("mcpCatalogSearch")?.value || "";
    if (mcpSearchTimer) clearTimeout(mcpSearchTimer);
    mcpSearchTimer = window.setTimeout(() => {
      requestMcpRegistry();
    }, 350);
  });

  el("mcpCatalogPrev")?.addEventListener("click", () => {
    if (mcpCatalogPage <= 0) return;
    mcpCatalogPage -= 1;
    renderMcpCatalog();
  });

  el("mcpCatalogNext")?.addEventListener("click", () => {
    const pages = Math.max(1, Math.ceil(filteredMcpEntries().length / MCP_PAGE_SIZE) || 1);
    if (mcpCatalogPage >= pages - 1) return;
    mcpCatalogPage += 1;
    renderMcpCatalog();
  });

  el("mcpAddCustom")?.addEventListener("click", () => {
    clearMcpForm();
    const out = el("mcpTestOut");
    if (out) out.textContent = i18n("set.mcp.enter_command_done");
    input("mcpName")?.focus();
  });

  el("mcpCfgClose")?.addEventListener("click", () => {
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    renderMcp();
  });

  el("mcpConfigure")?.addEventListener("click", (ev) => {
    if (ev.target === el("mcpConfigure")) {
      openMcpConfigure(false);
      mcpIsNewCustom = false;
      renderMcp();
    }
  });

  el("mcpEnabled")?.addEventListener("change", () => {
    const name = input("mcpName")?.value?.trim();
    const enabled = /** @type {HTMLInputElement | null} */ (el("mcpEnabled"));
    if (!name || mcpIsNewCustom || !enabled) return;
    showError("");
    vscode.postMessage({
      type: "setMCPDisabled",
      name,
      disabled: !enabled.checked,
    });
  });

  el("saveMCP")?.addEventListener("click", () => {
    showError("");
    const payload = mcpFormPayload();
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    vscode.postMessage({ type: "upsertMCP", ...payload });
  });

  el("deleteMCP")?.addEventListener("click", (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    const name = input("mcpName")?.value?.trim();
    if (!name) return;
    showError("");
    openMcpConfigure(false);
    mcpIsNewCustom = false;
    vscode.postMessage({ type: "deleteMCP", name });
  });

  el("reloadMCP")?.addEventListener("click", () => {
    showError("");
    requestMcpToolsProbe(true);
  });
  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "error") {
      showError(msg.message || i18n("set.error"));
      return;
    }
    if (msg.type === "modelPicked" && input("model") && msg.model) {
      input("model").value = msg.model;
      return;
    }
    if (msg.type === "mcpTestResult") {
      const r = msg.result || {};
      mcpToolsLoading = false;
      const out = el("mcpTestOut");
      if (out) {
        const found = (r.tools || []).length;
        out.textContent = r.ok
          ? i18n(found === 1 ? "set.mcp.test_ok_one" : "set.mcp.test_ok_n", {
              elapsed: r.elapsed,
              n: found,
            })
          : i18n("set.mcp.test_failed", { detail: r.error || i18n("set.mcp.unknown") });
      }
      if (r.ok && Array.isArray(r.tools)) {
        mcpDraftTools = r.tools.slice();
        const cur = findInstalledMcp(input("mcpName")?.value || "");
        renderMcpToolsList(
          cur || {
            allowed_tools: [],
          }
        );
        const sub = el("mcpCfgSub");
        if (sub && mcpConfigureOpen) {
          const n = mcpDraftTools.length;
          sub.textContent = i18n(n === 1 ? "set.mcp.tool_one" : "set.mcp.tool_n", { n });
        }
      } else {
        renderMcpToolsList(findInstalledMcp(input("mcpName")?.value || ""));
      }
      return;
    }
    if (msg.type === "mcpCatalogBusy") {
      mcpCatalogBusy = Boolean(msg.busy);
      if (msg.prefetching !== undefined) mcpCatalogPrefetching = Boolean(msg.prefetching);
      if (msg.error) mcpCatalogError = String(msg.error);
      updateMcpCatalogStatus();
      return;
    }
    if (msg.type === "mcpCatalog") {
      const catalog = msg.catalog || {};
      const incoming = Array.isArray(catalog.entries) ? catalog.entries : [];
      if (msg.append && !msg.replace) {
        const seen = new Set((mcpCatalog.entries || []).map((e) => String(e.id || e.name || "").toLowerCase()));
        const merged = (mcpCatalog.entries || []).slice();
        incoming.forEach((e) => {
          const key = String(e.id || e.name || "").toLowerCase();
          if (!key || seen.has(key)) return;
          seen.add(key);
          merged.push(e);
        });
        mcpCatalog = { ...catalog, entries: merged };
      } else {
        mcpCatalog = { ...catalog, entries: incoming };
      }
      mcpCatalogNextCursor = catalog.nextCursor || "";
      mcpCatalogSource = catalog.source || "registry";
      mcpCatalogError = catalog.error || "";
      mcpCatalogPrefetching = Boolean(catalog.prefetching);
      mcpCatalogBusy = Boolean(catalog.prefetching);
      renderMcpCatalog();
      return;
    }
    if (msg.type === "indexBusy") {
      const out = el("indexActionOut");
      if (out) out.textContent = msg.busy ? msg.message || i18n("turn.working") : "";
      return;
    }
    if (msg.type === "indexActionResult") {
      const out = el("indexActionOut");
      if (out) {
        if (msg.action === "embed" && msg.result) {
          const r = msg.result;
          out.textContent = i18n("set.index.embed_result", {
            embedded: r.embedded,
            total: r.total,
            remaining: r.remaining,
            elapsed: r.elapsed,
          });
        } else if (msg.action === "rebuild" && msg.graph) {
          const g = msg.graph;
          out.textContent = i18n("set.index.graph_result", {
            files: g.files,
            nodes: g.nodes,
            edges: g.edges,
          });
        }
      }
      return;
    }
    if (msg.type === "modelsBusy") {
      const status = el("modelsStatus");
      if (status)
        status.textContent = msg.busy ? msg.message || i18n("set.models.loading") : status.textContent;
      return;
    }
    if (msg.type === "providerCatalog") {
      applyProviderCatalog(msg.catalog, false);
      const cur = findProvider(selectedProviderKey);
      if (cur) populateProviderCreds(cur, { force: true });
      if (orchestraConfig) {
        sanitizeOrchestraRoles();
        renderOrchestra();
      }
      return;
    }
    if (msg.type === "state") return handleState(msg);
    return;
  });

  /** @param {any} msg */
  function handleState(msg) {
    workspaceRoot = msg.workspaceRoot || ".";
    const llm = msg.llm || {};
    const ext = msg.extension || {};
    const prompt = msg.prompt || {};
    const index = msg.index || {};

    if (input("binaryPath")) input("binaryPath").value = ext.binaryPath || "";
    if (input("projectRoot")) input("projectRoot").value = ext.projectRoot || "";

    applyProviderCatalog(msg.providerCatalog, true);
    const cur = findProvider(selectedProviderKey);
    if (cur) populateProviderCreds(cur, { force: true });
    if (input("promptFamily")) input("promptFamily").value = llm.promptFamily || prompt.promptFamily || "";
    if (input("temperature")) input("temperature").value = String(llm.temperature ?? 0);
    if (input("maxTokens")) input("maxTokens").value = String(llm.maxTokens ?? 0);
    if (input("timeoutS")) input("timeoutS").value = String(llm.timeoutS ?? 0);
    if (input("multimodal")) input("multimodal").checked = Boolean(llm.multimodal);

    if (area("excludeDirs")) {
      area("excludeDirs").value = Array.isArray(index.excludeDirs)
        ? index.excludeDirs.join("\n")
        : "";
    }
    if (input("contextLimitKB")) input("contextLimitKB").value = String(index.contextLimitKB ?? 0);
    if (input("limitsMaxFiles")) {
      input("limitsMaxFiles").value = index.limits?.max_files ? String(index.limits.max_files) : "";
    }
    const emb = index.embed || {};
    if (input("embedBatchSize")) input("embedBatchSize").value = emb.batch_size ? String(emb.batch_size) : "";
    const sem = /** @type {HTMLInputElement | null} */ (el("semanticAutoExplore"));
    if (sem) sem.checked = emb.semantic_auto_explore !== false;
    graphUIPort = index.graphUIPort || 6061;
    renderIndexStats(index);

    if (area("systemPrompt")) area("systemPrompt").value = prompt.content || "";
    agents = (msg.agents && msg.agents.agents) || [];
    agentAvailableTools =
      (msg.agents && Array.isArray(msg.agents.availableTools) && msg.agents.availableTools) ||
      (msg.agents && Array.isArray(msg.agents.available_tools) && msg.agents.available_tools) ||
      agentAvailableTools;
    mcpServers = (msg.mcp && msg.mcp.servers) || [];
    skills = msg.skills || [];
    if (msg.mcpCatalog && Array.isArray(msg.mcpCatalog.entries)) {
      mcpCatalog = msg.mcpCatalog;
      mcpCatalogNextCursor = msg.mcpCatalog.nextCursor || "";
      mcpCatalogSource = msg.mcpCatalog.source || "local";
      mcpCatalogError = msg.mcpCatalog.error || "";
    } else if (typeof window !== "undefined" && window.__ORCH_MCP_CATALOG) {
      mcpCatalog = window.__ORCH_MCP_CATALOG;
      mcpCatalogSource = mcpCatalog.source || "local";
    }
    renderAgents();
    renderMcp();
    renderSkills();
    if (msg.orchestra) {
      orchestraConfig = msg.orchestra;
      const roles = orchestraConfig.roles || [];
      const explicit = roles.map((r) => (r.provider || "").trim()).filter(Boolean);
      const uniq = [...new Set(explicit)];
      if (uniq.length === 1) {
        orchSharedProvider = uniq[0];
      } else {
        orchSharedProvider =
          roles.find((r) => r.provider)?.provider ||
          orchestraConfig.mainProvider ||
          selectedProviderKey ||
          "";
      }
      sanitizeOrchestraRoles();
      renderOrchestra();
    }
    if (msg.navigateSection) {
      navigateToSection(msg.navigateSection);
    }
    showError("");
  }

  /**
   * Redraw the parts of the panel that are built in JS rather than written in
   * the markup. applyStaticI18n() covers the markup; these lists were painted
   * with the previous language and would otherwise sit there until the next
   * state push.
   */
  function repaintTranslatedPanels() {
    renderProviders();
    renderModels();
    renderAgents();
    renderMcp();
    renderSkills();
    renderMcpCatalog();
    if (orchestraConfig) renderOrchestra();
  }

  // The panel is a document of its own and loads its script at the end of the
  // body, so the markup is here: translate it before the first paint rather
  // than waiting for the host's first state message.
  applyUiLanguage();

  vscode.postMessage({ type: "ready" });
})();
