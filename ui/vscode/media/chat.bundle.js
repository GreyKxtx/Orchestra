/* AUTO-GENERATED — do not edit. Sources: media/i18n.js + media/icons.js + media/chat-src/*.js  →  npm run bundle:webview */
//@ts-check
/* Generated from media/chat-src — edit fragments there, then: npm run bundle:webview */
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
        "The agent may look at the page: its elements and a screenshot. With the Browser view open that is your page, with your session; otherwise one it starts for itself. Not available under Fast.",
      "access.browser.on": "{hint} · browser on",
      "access.browser.drive.label": "…and act in it",
      "access.browser.drive.hint":
        "Click, type, fill forms and open other addresses in the Browser view. The view shows when the agent is doing it.",
      "access.browser.eval.label": "…and run script in it",
      "access.browser.eval.hint":
        "Run the agent's own JavaScript in the page, with your session. Stronger than everything else together — off unless you turn it on for this.",

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
      "browser.links": "Saved links",
      "browser.suggest_search": "{engine}: {q}",
      "browser.links_empty": "Nothing saved yet",
      "browser.link_save": "Save this page",
      "browser.link_saved": "Saved",
      "browser.link_forget": "Forget this link",
      "browser.agent_reading": "The agent is looking at this page",
      "browser.agent_driving": "The agent is acting on this page",
      "browser.console": "Developer tools",
      "browser.eval_hint": "Run JavaScript on the page",
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
      "cmd.trust": "Trust this workspace's settings",
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
      "web.trust_ignored":
        "This workspace is not trusted: its settings that act on this machine are not in effect — {ignored}. If the project is yours, /trust applies them (or `orchestra trust` in a terminal); /trust revoke forgets the trust.",
      "web.trusted": "Workspace trusted: its settings are in effect.",
      "web.trust_revoked": "Trust revoked: the workspace's machine-level settings are no longer in effect.",
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
        "/trust [revoke] — trust this workspace's settings (MCP servers, hooks, consent)",
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
        "Агент может смотреть на страницу: её элементы и снимок. Когда открыта вкладка «Браузер» — это ваша страница с вашей сессией, иначе он поднимет свою. Не действует при Fast.",
      "access.browser.on": "{hint} · браузер включён",
      "access.browser.drive.label": "…и действовать в ней",
      "access.browser.drive.hint":
        "Нажимать, вводить текст, заполнять формы и открывать другие адреса во вкладке «Браузер». Вкладка показывает, когда это делает агент.",
      "access.browser.eval.label": "…и выполнять скрипт",
      "access.browser.eval.hint":
        "Выполнять собственный JavaScript агента на странице, с вашей сессией. Сильнее всего остального вместе взятого — выключено, пока не включите отдельно.",

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
      "browser.links": "Сохранённые ссылки",
      "browser.suggest_search": "{engine}: {q}",
      "browser.links_empty": "Пока ничего не сохранено",
      "browser.link_save": "Сохранить эту страницу",
      "browser.link_saved": "Сохранено",
      "browser.link_forget": "Убрать ссылку",
      "browser.agent_reading": "Агент смотрит на эту страницу",
      "browser.agent_driving": "Агент действует на этой странице",
      "browser.console": "Инструменты разработчика",
      "browser.eval_hint": "Выполнить JavaScript на странице",
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
      "cmd.trust": "Доверить рабочей области её настройки",
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
      "web.trust_ignored":
        "Рабочая область не доверена: её настройки, влияющие на машину, не применяются — {ignored}. Если проект ваш, /trust применит их (или `orchestra trust` в терминале); /trust revoke забудет доверие.",
      "web.trusted": "Рабочая область доверена: её настройки применены.",
      "web.trust_revoked": "Доверие снято: настройки рабочей области, влияющие на машину, больше не применяются.",
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
        "/trust [revoke] — доверить рабочей области её настройки (MCP-серверы, hooks, согласия)",
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
    bookmark: '<path d="M6.5 4.5h11v15l-5.5-4-5.5 4z"/>',
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
  const host = acquireVsCodeApi();

  /** @typedef {{ id: string; label: string; icon: string; mode: string }} ModeOpt */
  /* `icon` is a NAME in media/icons.js, never a character. A Unicode glyph
     is drawn by whichever font on the machine carries it, so eight modes
     picked from eight blocks arrived at eight different weights — and ⌁
     and ◫ fall out of the UI font on Windows entirely. */
  /** @typedef {{ id: string; label: string; profile: string }} EffortOpt */

  /** @type {ModeOpt[]} — must match TUI `agentModes` / docs/modes.md top-level modes */
  const MODES = [
    { id: "build", label: "Build", icon: "mode-build", mode: "build" },
    { id: "plan", label: "Plan", icon: "mode-plan", mode: "plan" },
    { id: "explore", label: "Explore", icon: "mode-explore", mode: "explore" },
    { id: "ask", label: "Ask", icon: "mode-ask", mode: "ask" },
    { id: "debug", label: "Debug", icon: "mode-debug", mode: "debug" },
    { id: "architecture", label: "Architecture", icon: "mode-architecture", mode: "architecture" },
    { id: "agent", label: "Agent", icon: "mode-agent", mode: "agent" },
    { id: "orchestra", label: "Orchestra", icon: "mode-orchestra", mode: "orchestra" },
  ];

  /** @type {{ label: string; ids: string[] }[]} */
  const MODE_GROUPS = [
    { labelKey: "mode.group.core", ids: ["agent", "orchestra", "build", "plan"] },
    { labelKey: "mode.group.more", ids: ["explore", "ask", "debug", "architecture"] },
  ];

  /** @typedef {{ id: string; label: string; hint: string; icon: string }} AccessOpt */

  /** @type {AccessOpt[]} */
  const ACCESS_MODES = [
    {
      id: "ask",
      label: "Ask",
      hintKey: "access.ask.hint",
      icon: "access-ask",
    },
    {
      id: "auto",
      label: "Auto",
      hintKey: "access.auto.hint",
      icon: "access-auto",
    },
  ];

  /** @typedef {{ id: string; label: string; profile: string }} EffortOpt */

  /** @type {EffortOpt[]} */
  // Plain adjectives, so they translate. The mode names beside them (Agent,
  // Orchestra, Plan) and the access levels (Ask, Auto) do not: those are this
  // product's own vocabulary, the same words the CLI flags and the docs use.
  const EFFORTS = [
    { id: "low", labelKey: "effort.low", profile: "fast" },
    { id: "medium", labelKey: "effort.medium", profile: "" },
    { id: "high", labelKey: "effort.high", profile: "precision" },
  ];

  const chromeHint = document.getElementById("chrome-hint");
  const messagesEl = document.getElementById("messages");
  const inputEl = /** @type {HTMLTextAreaElement | null} */ (document.getElementById("input"));
  const sendBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("send"));
  const modeBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("mode-btn"));
  const accessBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("access-btn"));
  const accessMenu = document.getElementById("access-menu");
  const accessLabel = document.getElementById("access-label");
  const effortBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("effort-btn"));
  const modeMenu = document.getElementById("mode-menu");
  const effortMenu = document.getElementById("effort-menu");
  const modeLabel = document.getElementById("mode-label");
  const effortLabel = document.getElementById("effort-label");
  const costWrap = document.getElementById("cost-wrap");
  const costBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("cost-btn"));
  const costLabelEl = document.getElementById("cost-label");
  const costPopover = document.getElementById("cost-popover");
  const costBalanceEl = document.getElementById("cost-balance");
  const costSummaryEl = document.getElementById("cost-summary");
  const costRowsEl = document.getElementById("cost-rows");
  const contextBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("context-btn"));
  const contextRingFill = document.getElementById("context-ring-fill");
  const contextPopover = document.getElementById("context-popover");
  const ctxPct = document.getElementById("ctx-pct");
  const ctxSummary = document.getElementById("ctx-summary");
  const ctxBar = document.getElementById("ctx-bar");
  const ctxRows = document.getElementById("ctx-rows");
  const attachBtn = document.getElementById("attach-btn");
  const filesEl = document.getElementById("chip-files");
  const fastToggleRef = /** @type {{ el: HTMLButtonElement | null }} */ ({ el: null });
  const composerWrap = document.getElementById("composer-wrap");
  const composerStatus = document.getElementById("composer-status");
  const composerStatusLabel = document.getElementById("composer-status-label");
  const messageQueueEl = document.getElementById("message-queue");
  const sessionTabsEl = document.getElementById("session-tabs");
  const sessionNewBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-new-btn"));
  const sessionHistoryBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("session-history-btn"));
  const settingsBtn = document.getElementById("settings-btn");
  const sessionMenu = document.getElementById("session-menu");
  const sessionMenuList = document.getElementById("session-menu-list");
  const modelLabelEl = document.getElementById("model-label");
  const modelPill = /** @type {HTMLButtonElement | null} */ (document.getElementById("model-pill"));
  const modelMenu = document.getElementById("model-menu");
  const modelMenuTitle = document.getElementById("model-menu-title");
  const modelMenuList = document.getElementById("model-menu-list");
  const modelMenuSearch = /** @type {HTMLInputElement | null} */ (document.getElementById("model-menu-search"));
  const orchConfigBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById("orch-config-btn"));
  const pendingBar = document.getElementById("pending-bar");
  const pendingLabel = document.getElementById("pending-label");
  const pendingReviewListEl = document.getElementById("pending-review-list");
  const pendingApplyBtn = document.getElementById("pending-apply-btn");
  const pendingRejectBtn = document.getElementById("pending-reject-btn");
  const overlay = document.getElementById("overlay");
  const overlayTitle = document.getElementById("overlay-title");
  const overlayBody = document.getElementById("overlay-body");
  const overlayOptions = document.getElementById("overlay-options");
  const overlayInput = /** @type {HTMLInputElement | null} */ (document.getElementById("overlay-input"));
  const overlayActions = document.getElementById("overlay-actions");
  const paletteMenu = document.getElementById("palette-menu");
  const todosBar = document.getElementById("todos-bar");
  const todosList = document.getElementById("todos-list");
  const todosChip = document.getElementById("todos-chip");
  const todosChipGlyph = document.getElementById("todos-chip-glyph");
  const todosChipSummary = document.getElementById("todos-chip-summary");
  const todosChipChev = document.getElementById("todos-chip-chev");
  const workflowBar = document.getElementById("workflow-bar");
  const workflowLabel = document.getElementById("workflow-label");
  const workflowStagesEl = document.getElementById("workflow-stages");
  const subagentsBar = document.getElementById("subagents-bar");
  const subagentsTree = document.getElementById("subagents-tree");
  const diffViewer = document.getElementById("diff-viewer");
  const diffViewerTitle = document.getElementById("diff-viewer-title");
  const diffPaneBefore = document.getElementById("diff-pane-before");
  const diffPaneAfter = document.getElementById("diff-pane-after");
  const diffViewerCloseBtn = document.getElementById("diff-viewer-close-btn");
  const diffViewerEditorBtn = document.getElementById("diff-viewer-editor-btn");
  const imagePreview = document.getElementById("image-preview");
  const imagePreviewTitle = document.getElementById("image-preview-title");
  const imagePreviewImg = /** @type {HTMLImageElement | null} */ (document.getElementById("image-preview-img"));
  const imagePreviewCloseBtn = document.getElementById("image-preview-close-btn");
  const imagePreviewOpenBtn = document.getElementById("image-preview-open-btn");
  const imagePreviewPrevBtn = document.getElementById("image-preview-prev-btn");
  const imagePreviewNextBtn = document.getElementById("image-preview-next-btn");
  const imagePreviewCounter = document.getElementById("image-preview-counter");
  const statusLsp = document.getElementById("status-lsp");

  /**
   * The built-in slash commands. `descKey` rather than `desc` because the
   * palette is rebuilt on every keystroke and the language can change under
   * it — resolving the text at render time is what makes that work.
   * @type {{ cmd: string; descKey: string }[]}
   */
  const SLASH_CMDS = [
    { cmd: "/clear", descKey: "cmd.clear" },
    { cmd: "/compact", descKey: "cmd.compact" },
    { cmd: "/help", descKey: "cmd.help" },
    { cmd: "/model", descKey: "cmd.model" },
    { cmd: "/rewind", descKey: "cmd.rewind" },
    { cmd: "/sessions", descKey: "cmd.sessions" },
    { cmd: "/settings", descKey: "cmd.settings" },
    { cmd: "/trust", descKey: "cmd.trust" },
  ];

  /** Loaded skills, each usable as its own "/<name>" command. Replaced
   * wholesale on every "skillsList" message — it is its own array (not
   * appended to SLASH_CMDS) so a refresh can't accumulate stale entries.
   * @type {{ cmd: string; desc: string }[]} */
  let SKILL_CMDS = [];

  /** @type {Map<string, HTMLElement>} */
  const toolBlocks = new Map();
  /** @type {Map<string, string>} */
  const toolArgs = new Map();
  /** @type {Map<number, HTMLElement>} step → tool body pre */
  const execSteps = new Map();
  /** @type {{ id: string; content: string; status: string }[]} */
  let todos = [];
  let todosExpanded = false;
  let todosHadOpen = false;
  let todosDoneFlashTimer = 0;
  let paletteMode = "";
  let paletteIndex = 0;
  /** @type {any[]} */
  let paletteItems = [];
  let mentionTimer = 0;
  /** @type {{ prompt: number; completion: number; limit: number; maxResponse: number; estimated: boolean; breakdown: { key: string; label: string; tokens: number }[] }} */
  let ctxState = { prompt: 0, completion: 0, limit: 128000, maxResponse: 4096, estimated: false, breakdown: [] };
  let ctxPopoverOpen = false;

  /** @type {{ ops: any[]; diff: { path?: string; before?: string; after?: string; reviewStatus?: string }[] }} */
  let pendingState = { ops: [], diff: [] };
  // Diffs for changes the turn already wrote to disk. Kept apart from
  // pendingState on purpose: these need no decision from the user, so they must
  // never raise the apply bar — they only give the tool blocks a real diff to
  // draw instead of one rebuilt from the call's arguments.
  /** @type {{ path?: string; before?: string; after?: string }[]} */
  let appliedDiffs = [];
  let diffReviewCursor = 0;
  /** @type {{ id: string; type: string; label: string; status: string; taskId?: string; parentToolCallId?: string; toolsEl?: HTMLElement; toolCount?: number }[]} */
  let subagents = [];
  /** @type {Map<string, { taskId: string; parentToolCallId?: string; type: string; label: string; status: string; toolsEl?: HTMLElement; rowEl?: HTMLElement; toolCount: number }>} */
  const subagentByTask = new Map();
  /** @type {Map<string, (lines: string[]) => void>} */
  const highlightWaiters = new Map();
  let highlightSeq = 0;
  let diffViewerState = { path: "", before: "", after: "" };
  /** @type {{ items: { name: string; path?: string; previewUri?: string }[]; index: number }} */
  let imagePreviewState = { items: [], index: 0 };

  /** 20 MB — must match core `attachments.MaxImageBytes`. */
  const MAX_ATTACH_BYTES = 20 * 1024 * 1024;
  /** @type {Map<string, { id: string; name: string; state: string; attempt: number }>} */
  const workflowStages = new Map();
  let workflowActiveName = "";
  /** @type {{ questions: any[]; index: number; answers: string[]; mode: string }} */
  let questionState = { questions: [], index: 0, answers: [], mode: "" };

  const saved = host.getState() || {};
  let accessId =
    typeof saved.accessId === "string" && ACCESS_MODES.some((m) => m.id === saved.accessId)
      ? saved.accessId
      : "ask";
  // Browser tools for the turn (allow_browser). Off until the user turns it on.
  let browserOn = saved.browserOn === true;
  // Acting in the page, and running script in it: each asked for on its own.
  let browserDriveOn = saved.browserDrive === true;
  let browserEvalOn = saved.browserEval === true;
  let assistantBubble = null;
  /** @type {HTMLElement | null} */
  let assistantTurn = null;
  /** @type {HTMLElement | null} */
  let assistantTurnInner = null;
  /** @type {HTMLDetailsElement | null} */
  let toolTraceEl = null;
  /** @type {HTMLElement | null} */
  let toolTraceSummary = null;
  /** @type {HTMLDetailsElement | null} */
  let reasoningDetails = null;
  /** @type {HTMLElement | null} */
  let reasoningBody = null;
  let reasoningStarted = 0;
  /** @type {{ read: number; search: number; write: number; other: number }} */
  let turnToolCount = { read: 0, search: 0, write: 0, other: 0 };
  // Wall time of the turn's tool work: first tool start to last tool finish.
  // Zero means no live tool ran in this turn — restored history included,
  // which is timed by nobody and must not report a duration.
  let turnToolFirstStart = 0;
  let turnToolLastEnd = 0;
  let busy = false;
  let busyStatusText = i18n("turn.working");
  /** @type {Array<{ id: string; preview: string; fileCount?: number }>} */
  let sendQueue = [];
  /** @type {HTMLElement | null} */
  let typingIndicatorEl = null;
  /** Raw streamed assistant text before final-envelope stripping. */
  let streamRawText = "";
  let modeId = typeof saved.modeId === "string" && MODES.some((m) => m.id === saved.modeId) ? saved.modeId : "agent";
  let providerModelsCatalog = null;
  let modelMenuFilter = "";
  /** @type {{ roles: any[]; defaultTier: string } | null} Orchestra tier map for the footer pill. */
  let orchestraRolesInfo = null;
  let effortId = "medium";
  let fastOn = false;
  let currentModel = "";
  /** Accumulated session spend in USD (provider-reported). */
  let sessionCostUSD = 0;
  /**
   * Live cost of the in-flight turn, summed from per-step step_usage events
   * (each LLM call reports its cost when its stream finishes). Replaced by
   * the authoritative server total on turnUsage, then reset to 0.
   */
  let turnCostAccum = 0;
  /** @type {{ cost_usd?: number; total_tokens?: number; entries?: any[] } | null} Last turn usage summary. */
  let lastTurnUsage = null;
  /** @type {{ supported: boolean; provider?: string; balance?: number } | null} Provider balance (OpenRouter). */
  let creditsInfo = null;
  let activeSessionId = "";
  /** @type {{ name: string; path?: string; ext?: string; kind?: string; previewUri?: string }[]} */
  let files = [];
  function escapeAttr(value) {
    return String(value || "")
      .replace(/&/g, "&amp;")
      .replace(/"/g, "&quot;")
      .replace(/</g, "&lt;");
  }

  function fileExtLabel(nameOrExt) {
    const raw = (nameOrExt || "").trim();
    if (!raw) {
      return "FILE";
    }
    if (raw.includes(".")) {
      const ext = raw.split(".").pop() || "";
      return ext.slice(0, 4).toUpperCase() || "FILE";
    }
    return raw.slice(0, 4).toUpperCase();
  }

  function relPathDisplay(fullPath) {
    if (!fullPath) {
      return "";
    }
    const parts = fullPath.replace(/\\/g, "/").split("/");
    if (parts.length <= 2) {
      return fullPath;
    }
    return parts.slice(-3).join("/");
  }

  function addFileRef(f) {
    if (!f || typeof f.name !== "string") {
      return;
    }
    const key = f.path || f.name;
    if (files.some((x) => (x.path || x.name) === key)) {
      return;
    }
    files.push({
      name: f.name,
      path: typeof f.path === "string" ? f.path : undefined,
      ext: typeof f.ext === "string" ? f.ext : undefined,
      kind: typeof f.kind === "string" ? f.kind : undefined,
      previewUri: typeof f.previewUri === "string" ? f.previewUri : undefined,
    });
    renderFiles();
  }

  function truncateTabTitle(title) {
    const t = (title || i18n("chrome.new_chat")).trim() || i18n("chrome.new_chat");
    return t.length > 22 ? t.slice(0, 20) + "…" : t;
  }

  /** @param {{ id: string; title?: string; model?: string }[]} tabs */
  function renderSessionTabs(tabs, activeId) {
    if (!sessionTabsEl) {
      return;
    }
    activeSessionId = activeId || "";
    sessionTabsEl.innerHTML = "";
    const list = Array.isArray(tabs) ? tabs : [];
    if (list.length === 0) {
      const empty = document.createElement("div");
      empty.className = "session-tabs-empty";
      empty.textContent = i18n("chrome.new_chat");
      sessionTabsEl.appendChild(empty);
      return;
    }
    list.forEach((s) => {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = "session-tab" + (s.id === activeId ? " active" : "");
      tab.setAttribute("role", "tab");
      tab.setAttribute("aria-selected", s.id === activeId ? "true" : "false");
      tab.setAttribute("data-session-id", s.id);
      tab.title = s.title || s.id;

      const icon = document.createElement("span");
      icon.className = "session-tab-icon";
      icon.innerHTML =
        '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
        '<path d="M7 9.5h10M7 13.5h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>' +
        '<path d="M5 5.5h14a2 2 0 012 2v8.5a2 2 0 01-2 2H9.5L6 21v-3H5a2 2 0 01-2-2V7.5a2 2 0 012-2z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>' +
        "</svg>";

      const label = document.createElement("span");
      label.className = "session-tab-label";
      label.textContent = truncateTabTitle(s.title);

      const close = document.createElement("span");
      close.className = "session-tab-close";
      close.setAttribute("data-close-session", s.id);
      close.setAttribute("role", "button");
      close.setAttribute("aria-label", i18n("tab.close"));
      close.textContent = "×";

      tab.appendChild(icon);
      tab.appendChild(label);
      tab.appendChild(close);
      sessionTabsEl.appendChild(tab);
    });

    const activeTab = sessionTabsEl.querySelector(".session-tab.active");
    if (activeTab && typeof activeTab.scrollIntoView === "function") {
      activeTab.scrollIntoView({ block: "nearest", inline: "nearest" });
    }
  }

  function updateActiveTabTitle(title) {
    if (!sessionTabsEl || !activeSessionId) {
      return;
    }
    const tab = sessionTabsEl.querySelector(`[data-session-id="${activeSessionId}"]`);
    if (!tab) {
      return;
    }
    const label = tab.querySelector(".session-tab-label");
    if (label) {
      label.textContent = truncateTabTitle(title);
    }
    tab.title = title || activeSessionId;
  }

  /** Short label for long provider/model ids (Cursor-style). */
  function shortModel(id) {
    if (!id) {
      return "Model";
    }
    const parts = String(id).split(/[/\\]/);
    const last = parts[parts.length - 1] || id;
    return last.length > 28 ? last.slice(0, 25) + "…" : last;
  }

  function setModelLabel(id) {
    currentModel = id || currentModel;
    if (modelLabelEl) {
      modelLabelEl.textContent = shortModel(currentModel);
      modelLabelEl.title = currentModel || "";
    }
  }

  function currentMode() {
    return MODES.find((m) => m.id === modeId) || MODES[0];
  }

  function currentEffort() {
    return EFFORTS.find((e) => e.id === effortId) || EFFORTS[1];
  }

  function effectiveProfile() {
    if (fastOn) {
      return "fast";
    }
    return currentEffort().profile;
  }

  function currentAccess() {
    return ACCESS_MODES.find((m) => m.id === accessId) || ACCESS_MODES[0];
  }

  function initAccessMenu() {
    if (!accessMenu) {
      return;
    }
    accessMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = i18n("access.section");
    accessMenu.appendChild(head);
    ACCESS_MODES.forEach((m) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item access-item";
      btn.dataset.access = m.id;
      btn.title = i18n(m.hintKey);
      btn.innerHTML =
        `<span class="mi access-icon access-${escapeAttr(m.id)}">${orchIconMarkup(m.icon, { size: "sm" })}</span>` +
        `<span class="access-item-text"><span class="access-item-label">${escapeAttr(m.label)}</span>` +
        `<span class="access-item-hint">${escapeAttr(i18n(m.hintKey))}</span></span>`;
      accessMenu.appendChild(btn);
    });
    const note = document.createElement("div");
    note.className = "menu-hint access-menu-note";
    note.textContent =
      i18n("access.note");
    accessMenu.appendChild(note);
    const optHead = document.createElement("div");
    optHead.className = "menu-section";
    optHead.textContent = i18n("access.tools.section");
    accessMenu.appendChild(optHead);
    const browserRow = document.createElement("div");
    browserRow.className = "menu-row menu-row-browser";
    browserRow.title =
      i18n("access.browser.hint");
    browserRow.innerHTML =
      `<span class="menu-row-label"><span class="mi" aria-hidden="true">${orchIconMarkup("access-browser", { size: "sm" })}</span>${escapeAttr(i18n("access.browser.label"))}</span>` +
      `<button type="button" id="browser-toggle" class="toggle" role="switch" aria-checked="false" aria-label="${escapeAttr(i18n("access.browser.label"))}"></button>`;
    accessMenu.appendChild(browserRow);
    // Two more, each a step further into the person's own browser. A child
    // switched on switches its parents on: nobody means "act in the page but
    // do not look at it".
    for (const level of [
      { id: "browser-drive-toggle", key: "access.browser.drive" },
      { id: "browser-eval-toggle", key: "access.browser.eval" },
    ]) {
      const row = document.createElement("div");
      row.className = "menu-row menu-row-browser menu-row-browser-level";
      row.title = i18n(`${level.key}.hint`);
      row.innerHTML =
        `<span class="menu-row-label">${escapeAttr(i18n(`${level.key}.label`))}</span>` +
        `<button type="button" id="${level.id}" class="toggle" role="switch" aria-checked="false" ` +
        `aria-label="${escapeAttr(i18n(`${level.key}.label`))}"></button>`;
      accessMenu.appendChild(row);
    }
  }

  function syncAccessUi() {
    const m = currentAccess();
    if (accessLabel) {
      accessLabel.textContent = m.label;
    }
    const icon = document.getElementById("access-icon");
    if (icon) {
      icon.innerHTML = orchIconMarkup(m.icon, { size: "sm" });
      icon.className = `ico access-icon access-${m.id}`;
    }
    if (accessBtn) {
      accessBtn.dataset.access = accessId;
      accessBtn.title = i18n(m.hintKey);
    }
    accessMenu?.querySelectorAll("[data-access]").forEach((el) => {
      const id = el.getAttribute("data-access");
      el.classList.toggle("selected", id === accessId);
    });
    for (const [id, on] of [
      ["browser-toggle", browserOn],
      ["browser-drive-toggle", browserDriveOn],
      ["browser-eval-toggle", browserEvalOn],
    ]) {
      const toggle = document.getElementById(id);
      if (!toggle) continue;
      toggle.classList.toggle("on", on);
      toggle.setAttribute("aria-checked", on ? "true" : "false");
    }
    if (accessBtn) {
      accessBtn.title = browserOn
        ? i18n("access.browser.on", { hint: i18n(m.hintKey) })
        : i18n(m.hintKey);
    }
    host.setState({ ...(host.getState() || {}), accessId, browserOn, browserDrive: browserDriveOn, browserEval: browserEvalOn });
  }

  function statsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  function langFromPath(filePath) {
    const ext = (filePath || "").split(".").pop()?.toLowerCase() || "";
    const map = {
      go: "go",
      ts: "ts",
      tsx: "tsx",
      js: "js",
      jsx: "jsx",
      py: "py",
      rs: "rust",
      md: "md",
      json: "json",
      yml: "yaml",
      yaml: "yaml",
      css: "css",
      html: "html",
      sh: "bash",
    };
    return map[ext] || "plain";
  }
  function escapeHtml(text) {
    // Quotes must be escaped too: escaped text is interpolated into
    // attribute values (e.g. link href) — an unescaped `"` would break out
    // of the attribute (XSS defense-in-depth on top of the CSP).
    return String(text || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  /** Only allow benign navigation schemes in model-authored links. */
  function safeLinkHref(url) {
    return /^(https?:|mailto:)/i.test(url) ? url : "";
  }

  /** @param {string} s */
  function markdownInline(s) {
    let x = escapeHtml(s);
    x = x.replace(/`([^`\n]+)`/g, '<code class="md-code">$1</code>');
    x = x.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
    x = x.replace(/(?<![*])\*([^*\n]+)\*(?![*])/g, "<em>$1</em>");
    x = x.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label, url) => {
      const href = safeLinkHref(url);
      if (!href) {
        // javascript:, command:, data: etc. — render as plain text.
        return `${label} (${url})`;
      }
      return `<a class="md-link" href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    });
    return x;
  }

  function pathLooksLikeFile(s) {
    const t = (s || "").trim();
    return /[./\\]/.test(t) || /\.[a-z0-9]{1,6}$/i.test(t);
  }

  /** @returns {{ lang?: string; path?: string; startLine?: number; endLine?: number }} */
  function parseCodeFenceMeta(openRest) {
    const raw = (openRest || "").trim();
    if (!raw) {
      return { lang: "plain" };
    }
    const gh = raw.match(/^(\d+):(\d+):(.+)$/);
    if (gh) {
      const fp = gh[3].trim();
      return { lang: langFromPath(fp), path: fp, startLine: +gh[1], endLine: +gh[2] };
    }
    const parts = raw.split(/\s+/);
    const first = parts[0] || "";
    const rest = parts.slice(1).join(" ").trim();

    /** @param {string} s */
    function parsePathLines(s) {
      const m1 = s.match(/^(.+?)\s+[Ll]ines?\s+(\d+)\s*[-–]\s*(\d+)$/);
      if (m1) {
        return { path: m1[1].trim(), startLine: +m1[2], endLine: +m1[3] };
      }
      const m2 = s.match(/^(.+?):(\d+)\s*[-–]\s*(\d+)$/);
      if (m2) {
        return { path: m2[1].trim(), startLine: +m2[2], endLine: +m2[3] };
      }
      const m3 = s.match(/^(.+?):(\d+)$/);
      if (m3) {
        return { path: m3[1].trim(), startLine: +m3[2], endLine: +m3[2] };
      }
      if (pathLooksLikeFile(s)) {
        return { path: s.trim() };
      }
      return null;
    }

    const combined = rest ? `${first} ${rest}` : first;
    const pl =
      parsePathLines(combined) ||
      parsePathLines(rest) ||
      (pathLooksLikeFile(first) && !rest ? parsePathLines(first) : null) ||
      (pathLooksLikeFile(rest) ? parsePathLines(rest) : null);

    if (pl?.path) {
      const lang =
        first && !pathLooksLikeFile(first) && first !== pl.path ? first : langFromPath(pl.path);
      return {
        lang,
        path: pl.path,
        startLine: pl.startLine,
        endLine: pl.endLine,
      };
    }
    if (first && !pathLooksLikeFile(first)) {
      return { lang: first };
    }
    return { lang: "plain" };
  }

  function codeRefTitle(filePath, startLine, endLine) {
    const base = basename(filePath);
    if (startLine && endLine && endLine !== startLine) {
      return `${base} Lines ${startLine}-${endLine}`;
    }
    if (startLine) {
      return `${base} Line ${startLine}`;
    }
    return base;
  }

  function buildCodeRefCardHtml(filePath, lang, startLine, endLine, code) {
    const title = codeRefTitle(filePath, startLine, endLine);
    return (
      `<div class="code-ref-card diff-preview-card"` +
      ` data-path="${escapeAttr(filePath)}" data-lang="${escapeAttr(lang || langFromPath(filePath))}"` +
      (startLine ? ` data-start-line="${startLine}"` : "") +
      (endLine ? ` data-end-line="${endLine}"` : "") +
      `>` +
      `<div class="diff-preview-head code-ref-head">` +
      diffExtBadgeHtml(filePath) +
      `<button type="button" class="diff-preview-name code-ref-title" title="${escapeAttr(i18n("code.open_file"))}">${escapeAttr(title)}</button>` +
      `</div>` +
      `<pre class="code-ref-body md-pre"><code>${escapeHtml(code)}</code></pre>` +
      `</div>`
    );
  }

  function bindCodeRefCard(card) {
    if (!card || card.dataset.bound === "1") {
      return;
    }
    card.dataset.bound = "1";
    const path = card.dataset.path || "";
    card.querySelector(".code-ref-title")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      if (!path) {
        return;
      }
      if (/** @type {MouseEvent} */ (e).shiftKey) {
        openExternalFile(path, true);
      } else {
        openExternalFile(path, true);
      }
    });
  }

  async function enhanceCodeRefCards(root) {
    if (!root) {
      return;
    }
    const cards = root.querySelectorAll(".code-ref-card:not([data-enhanced])");
    for (const card of cards) {
      card.dataset.enhanced = "1";
      bindCodeRefCard(card);
      const path = card.dataset.path || "";
      const lang = card.dataset.lang || langFromPath(path);
      const pre = card.querySelector(".code-ref-body code");
      if (!pre) {
        continue;
      }
      const code = pre.textContent || "";
      const lines = code.split("\n");
      const startLine = Number(card.dataset.startLine) || 1;
      const htmlLines = await requestHighlight(lines, lang);
      const body = document.createElement("div");
      body.className = "code-ref-body diff-preview-body";
      lines.forEach((line, idx) => {
        const row = document.createElement("div");
        row.className = "code-ref-row";
        row.innerHTML =
          `<span class="code-ref-ln">${startLine + idx}</span>` +
          `<span class="code-ref-code">${htmlLines[idx] || escapeHtml(line) || "&nbsp;"}</span>`;
        body.appendChild(row);
      });
      pre.closest(".code-ref-body")?.replaceWith(body);
    }
  }

  /** @param {string} raw */
  function renderMarkdownToHtml(raw) {
    const text = String(raw || "");
    if (!text.trim()) {
      return "";
    }
    const lines = text.split("\n");
    const out = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      if (trimmed.startsWith("```")) {
        const meta = parseCodeFenceMeta(trimmed.slice(3).trim());
        i++;
        const codeLines = [];
        while (i < lines.length && !lines[i].trim().startsWith("```")) {
          codeLines.push(lines[i]);
          i++;
        }
        if (i < lines.length) {
          i++;
        }
        const code = codeLines.join("\n");
        if (meta.path) {
          out.push(
            buildCodeRefCardHtml(
              meta.path,
              meta.lang || langFromPath(meta.path),
              meta.startLine,
              meta.endLine,
              code
            )
          );
        } else {
          out.push(`<pre class="md-pre"><code>${escapeHtml(code)}</code></pre>`);
        }
        continue;
      }
      if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
        out.push('<hr class="md-hr">');
        i++;
        continue;
      }
      const h3 = line.match(/^###\s+(.+)$/);
      if (h3) {
        out.push(`<h3 class="md-h3">${markdownInline(h3[1])}</h3>`);
        i++;
        continue;
      }
      const h2 = line.match(/^##\s+(.+)$/);
      if (h2) {
        out.push(`<h2 class="md-h2">${markdownInline(h2[1])}</h2>`);
        i++;
        continue;
      }
      const h1 = line.match(/^#\s+(.+)$/);
      if (h1) {
        out.push(`<h1 class="md-h1">${markdownInline(h1[1])}</h1>`);
        i++;
        continue;
      }
      if (/^[-*+]\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^[-*+]\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^[-*+]\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ul class="md-ul">${items.join("")}</ul>`);
        continue;
      }
      if (/^\d+\.\s+/.test(line)) {
        const items = [];
        while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
          items.push(`<li>${markdownInline(lines[i].replace(/^\d+\.\s+/, ""))}</li>`);
          i++;
        }
        out.push(`<ol class="md-ol">${items.join("")}</ol>`);
        continue;
      }
      if (!trimmed) {
        i++;
        continue;
      }
      const para = [];
      while (
        i < lines.length &&
        lines[i].trim() &&
        !lines[i].trim().startsWith("```") &&
        !/^#{1,3}\s+/.test(lines[i]) &&
        !/^[-*+]\s+/.test(lines[i]) &&
        !/^\d+\.\s+/.test(lines[i]) &&
        !/^(-{3,}|\*{3,}|_{3,})$/.test(lines[i].trim())
      ) {
        para.push(lines[i]);
        i++;
      }
      out.push(`<p class="md-p">${markdownInline(para.join(" "))}</p>`);
    }
    return out.join("");
  }

  /** @param {HTMLElement} el @param {string} raw */
  function applyAssistantMarkdown(el, raw) {
    el.classList.add("turn-text", "md-body");
    el.innerHTML = renderMarkdownToHtml(raw);
    void enhanceCodeRefCards(el);
  }

  /** @type {number | null} */
  let mdRenderPending = null;

  /** @param {HTMLElement} el @param {string} raw */
  function scheduleAssistantMarkdown(el, raw) {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
    }
    mdRenderPending = requestAnimationFrame(() => {
      mdRenderPending = null;
      applyAssistantMarkdown(el, raw);
    });
  }

  function flushAssistantMarkdown() {
    if (mdRenderPending !== null) {
      cancelAnimationFrame(mdRenderPending);
      mdRenderPending = null;
    }
    if (assistantBubble) {
      applyAssistantMarkdown(assistantBubble, sanitizeAssistantStream(stripFinalEnvelope(streamRawText)));
    }
  }

  function highlightCode(line, lang) {
    let s = escapeHtml(line);
    if (lang === "plain" || !line.trim()) return s;
    const strRe = /(&quot;[^&]*?&quot;|'[^']*?'|`[^`]*?`)/g;
    const apply = (text, re, cls) => text.replace(re, (m) => `<span class="syn-${cls}">${m}</span>`);
    s = apply(s, strRe, "str");
    if (lang === "go") {
      s = apply(
        s,
        /\b(func|return|if|else|for|range|package|import|type|struct|interface|var|const|go|defer|switch|case|default|map|chan|select)\b/g,
        "kw"
      );
    } else if (lang === "ts" || lang === "tsx" || lang === "js" || lang === "jsx") {
      s = apply(
        s,
        /\b(const|let|var|function|return|if|else|for|while|import|export|from|class|interface|type|async|await|new|this)\b/g,
        "kw"
      );
    } else if (lang === "py") {
      s = apply(s, /\b(def|return|if|elif|else|for|while|import|from|class|with|as|pass|None|True|False)\b/g, "kw");
    } else if (lang === "rust") {
      s = apply(s, /\b(fn|let|mut|pub|use|struct|enum|impl|match|if|else|return|mod|crate)\b/g, "kw");
    }
    s = apply(s, /(\/\/.*$|#.*$)/g, "cm");
    return s;
  }
  function isMutatingToolBlock(block) {
    return block?.classList?.contains("kind-write") === true;
  }

  function diffPathMatches(candidate, norm) {
    const p = (candidate || "").replace(/\\/g, "/");
    return p === norm || p.endsWith("/" + norm) || norm.endsWith("/" + p) || basename(p) === basename(norm);
  }

  function findDiffForPath(filePath) {
    if (!filePath) return null;
    const norm = filePath.replace(/\\/g, "/");
    if (pendingState.diff.length) {
      const hit = pendingState.diff.find((d) => diffPathMatches(d.path, norm));
      if (hit) {
        return hit;
      }
    }
    // Changes already written to disk. The core computes the same before/after
    // it computes for a dry run, so a turn that applies as it goes shows the
    // same diff as one that waits for approval. Without this the block falls
    // back to what it can rebuild from the call's arguments — for `write` that
    // is before="" and a rewritten file renders as entirely new lines.
    if (appliedDiffs.length) {
      const hit = appliedDiffs.find((d) => diffPathMatches(d.path, norm));
      if (hit) {
        return hit;
      }
    }
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const p = fp.replace(/\\/g, "/");
      if (
        p !== norm &&
        !p.endsWith("/" + norm) &&
        !norm.endsWith("/" + p) &&
        basename(p) !== basename(norm)
      ) {
        continue;
      }
      if (block.dataset.diffBefore !== undefined || block.dataset.diffAfter !== undefined) {
        return {
          path: fp,
          before: block.dataset.diffBefore || "",
          after: block.dataset.diffAfter || "",
        };
      }
    }
    return null;
  }

  function extractDiffFromTool(name, argsRaw, result) {
    const path = toolPathFromArgs(name, argsRaw, result || "");
    if (!path) {
      return null;
    }
    const args = parseToolArgs(argsRaw);
    const n = (name || "").toLowerCase();
    if (n === "write" || n === "fs.write" || n === "file.write_atomic") {
      const content = typeof args.content === "string" ? args.content : "";
      return { path, before: "", after: content };
    }
    if (n === "edit" || n === "fs.edit") {
      const search = typeof args.search === "string" ? args.search : "";
      const replace = typeof args.replace === "string" ? args.replace : "";
      if (search || replace) {
        return { path, before: search, after: replace };
      }
    }
    return null;
  }

  function rememberBlockDiff(block, before, after) {
    if (!block) return;
    block.dataset.diffBefore = before || "";
    block.dataset.diffAfter = after || "";
  }

  function syncToolDiffStats() {
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff) continue;
      const stats = countDiffStats(diff.before, diff.after);
      const el = block.querySelector(".tool-stats");
      if (el) el.innerHTML = statsHtml(stats);
    }
  }

  /** Upgrade write/edit tool blocks to Cursor-style inline diff when pending data arrives. */
  async function syncToolDiffPreviews() {
    syncToolDiffStats();
    const jobs = [];
    for (const block of toolBlocks.values()) {
      if (!isMutatingToolBlock(block)) continue;
      const fp = block.dataset.filePath || "";
      if (!fp) continue;
      const diff = findDiffForPath(fp);
      if (!diff || (!diff.before && !diff.after)) continue;
      block.querySelector(".file-change-card")?.remove();
      jobs.push(attachInlineToolDiff(block, fp, diff.before || "", diff.after || ""));
    }
    if (jobs.length) {
      await Promise.all(jobs);
    }
  }

  function attachToolDiffShell(block, filePath) {
    if (!block || !filePath) return;
    if (block.querySelector(".tool-diff-body.diff-preview-card")) return;
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    const body = block.querySelector(".tool-body");
    const diffWrap = document.createElement("div");
    diffWrap.className = "tool-body tool-diff-body diff-preview-card";
    const head = document.createElement("div");
    head.className = "diff-preview-head";
    head.innerHTML =
      diffExtBadgeHtml(filePath) +
      `<button type="button" class="diff-preview-name" title="${escapeAttr(i18n("diff.open_file"))}">${escapeAttr(basename(filePath))}</button>` +
      `<span class="tool-diff-pending">…</span>`;
    const lines = document.createElement("div");
    lines.className = "diff-preview-body tool-diff-pending-body";
    lines.textContent = i18n("diff.loading");
    diffWrap.appendChild(head);
    diffWrap.appendChild(lines);
    head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      const d = findDiffForPath(filePath) || extractDiffFromTool("write", block.dataset.argsRaw || "", "");
      openDiffMessage(
        filePath,
        d?.before || block.dataset.diffBefore || "",
        d?.after || block.dataset.diffAfter || "",
        /** @type {MouseEvent} */ (e).shiftKey
      );
    });
    if (body) {
      body.replaceWith(diffWrap);
    } else {
      block.appendChild(diffWrap);
    }
    block.classList.add("write-card-only");
  }

  function renderPendingBar() {
    const n = pendingState.diff.length || pendingState.ops.length;
    if (!pendingBar) return;
    if (!n) {
      pendingBar.classList.add("hidden");
      return;
    }
    pendingBar.classList.remove("hidden");
    const fileCount = pendingState.diff.length;
    if (pendingLabel) {
      pendingLabel.textContent = fileCount
        ? `${fileCount} file${fileCount === 1 ? "" : "s"} changed`
        : `${n} pending change${n === 1 ? "" : "s"}`;
    }
    renderPendingReviewList();
  }

  function diffExtBadgeHtml(filePath) {
    const ext = fileExtLabel(filePath || "");
    const lang = langFromPath(filePath || "");
    return `<span class="diff-ext-badge lang-${escapeAttr(lang)}">${escapeAttr(ext)}</span>`;
  }

  function diffStatsHtml(stats) {
    if (!stats || (!stats.add && !stats.del)) return "";
    return (
      `<span class="fcc-stats">` +
      (stats.add ? `<span class="fcc-add">+${stats.add}</span>` : "") +
      (stats.del ? `<span class="fcc-del">−${stats.del}</span>` : "") +
      `</span>`
    );
  }

  async function renderPendingReviewList() {
    if (!pendingReviewListEl) return;
    bindPendingReviewListEvents();
    pendingReviewListEl.innerHTML = "";
    if (!pendingState.diff.length) {
      return;
    }
    for (let idx = 0; idx < pendingState.diff.length; idx++) {
      const d = pendingState.diff[idx];
      const stats = countDiffStats(d.before, d.after);
      const item = document.createElement("div");
      item.className = "pending-review-item diff-preview-card";
      item.setAttribute("data-idx", String(idx));
      if (idx === diffReviewCursor) item.classList.add("selected");

      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(d.path || "") +
        `<button type="button" class="diff-preview-name" title="${escapeAttr(i18n("diff.open_file"))}">${escapeAttr(basename(d.path || "file"))}</button>` +
        diffStatsHtml(stats) +
        // Per-file decisions, the same two the a/x keys make. Without them the
        // only discoverable choice is all-or-nothing on the bar below.
        `<span class="pending-item-acts">` +
        `<button type="button" class="pending-item-act pending-item-keep" data-act="keep" title="${escapeAttr(i18n("diff.keep_title"))}">${escapeAttr(i18n("diff.keep"))}</button>` +
        `<button type="button" class="pending-item-act pending-item-drop" data-act="drop" title="${escapeAttr(i18n("diff.drop_title"))}">${escapeAttr(i18n("diff.drop"))}</button>` +
        `</span>`;

      const body = document.createElement("div");
      body.className = "diff-preview-body";

      item.appendChild(head);
      item.appendChild(body);
      pendingReviewListEl.appendChild(item);
      await renderUnifiedDiffLines(body, d.before || "", d.after || "", d.path || "", 28);
      if (idx === diffReviewCursor) {
        item.scrollIntoView({ block: "nearest", behavior: "smooth" });
      }
    }
  }

  // Both take an optional file list. Without one the host applies or rejects
  // the whole turn (the bar's two buttons); with one it settles just those
  // files and the core hands back what is still pending, so a reviewer can
  // keep the good edits of a turn and throw away the bad one.
  function applyPendingChanges(paths) {
    host.postMessage(
      paths && paths.length ? { type: "applyPending", paths } : { type: "applyPending" }
    );
  }

  function discardPendingChanges(paths) {
    host.postMessage(
      paths && paths.length ? { type: "discardPending", paths } : { type: "discardPending" }
    );
  }

  /** Apply or reject the file the review cursor is on. */
  function settleSelectedPendingFile(apply) {
    const d = pendingState.diff[diffReviewCursor];
    const path = d && d.path ? String(d.path) : "";
    if (!path) return;
    // The cursor stays put: the list shrinks under it, so the next file slides
    // into the selected slot and a reviewer can hold the key down.
    if (apply) applyPendingChanges([path]);
    else discardPendingChanges([path]);
  }

  function countDiffStats(before, after) {
    const bLines = (before || "").split("\n");
    const aLines = (after || "").split("\n");
    if (!before && after) {
      return { add: aLines.length, del: 0 };
    }
    if (before && !after) {
      return { add: 0, del: bLines.length };
    }
    const freq = new Map();
    for (const line of bLines) {
      freq.set(line, (freq.get(line) || 0) + 1);
    }
    let add = 0;
    for (const line of aLines) {
      const c = freq.get(line) || 0;
      if (c > 0) {
        freq.set(line, c - 1);
      } else {
        add++;
      }
    }
    let del = 0;
    for (const c of freq.values()) {
      del += c;
    }
    return { add, del };
  }

  function toolBlockKey(msg) {
    const id = msg.toolCallId || `${msg.toolName}-${msg.step ?? toolBlocks.size}`;
    if (msg.scope === "child" && msg.taskId) {
      return `${msg.taskId}:${id}`;
    }
    return id;
  }

  function requestHighlight(lines, lang) {
    const requestId = `hl-${++highlightSeq}`;
    return new Promise((resolve) => {
      highlightWaiters.set(requestId, resolve);
      host.postMessage({
        type: "highlightCode",
        requestId,
        language: lang || "plaintext",
        lines: Array.isArray(lines) ? lines : [],
      });
      setTimeout(() => {
        if (highlightWaiters.has(requestId)) {
          highlightWaiters.delete(requestId);
          resolve(lines.map((l) => escapeHtml(String(l || ""))));
        }
      }, 8000);
    });
  }

  /** Myers-style line alignment for side-by-side diff panes. */
  function alignDiffLines(before, after) {
    const a = (before || "").split("\n");
    const b = (after || "").split("\n");
    const n = a.length;
    const m = b.length;
    const dp = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    const rows = [];
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && a[i] === b[j]) {
        rows.push({ type: "same", left: a[i], right: b[j], leftNum: i + 1, rightNum: j + 1 });
        i++;
        j++;
      } else if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) {
        rows.push({ type: "add", right: b[j], rightNum: j + 1 });
        j++;
      } else if (i < n) {
        rows.push({ type: "del", left: a[i], leftNum: i + 1 });
        i++;
      }
    }
    return rows;
  }

  async function renderDiffPanes(before, after, lang) {
    if (!diffPaneBefore || !diffPaneAfter) return;
    const rows = alignDiffLines(before, after);
    const leftLines = rows.map((r) => (r.type === "add" ? "" : r.left ?? ""));
    const rightLines = rows.map((r) => (r.type === "del" ? "" : r.right ?? ""));
    const [leftHtml, rightHtml] = await Promise.all([
      requestHighlight(leftLines, lang),
      requestHighlight(rightLines, lang),
    ]);
    diffPaneBefore.innerHTML = "";
    diffPaneAfter.innerHTML = "";
    rows.forEach((r, idx) => {
      const lRow = document.createElement("div");
      lRow.className = "diff-sbs-row" + (r.type === "del" ? " del" : r.type === "same" ? " same" : " empty");
      lRow.innerHTML =
        `<span class="diff-ln">${r.leftNum || ""}</span>` +
        `<span class="diff-code">${leftHtml[idx] || "&nbsp;"}</span>`;
      diffPaneBefore.appendChild(lRow);
      const rRow = document.createElement("div");
      rRow.className = "diff-sbs-row" + (r.type === "add" ? " add" : r.type === "same" ? " same" : " empty");
      rRow.innerHTML =
        `<span class="diff-ln">${r.rightNum || ""}</span>` +
        `<span class="diff-code">${rightHtml[idx] || "&nbsp;"}</span>`;
      diffPaneAfter.appendChild(rRow);
    });
  }

  function showDiffViewer(path, before, after, language) {
    if (!diffViewer) return;
    diffViewerState = { path: path || "", before: before || "", after: after || "" };
    if (diffViewerTitle) {
      diffViewerTitle.textContent = basename(path) || path || "Diff";
      diffViewerTitle.title = path || "";
    }
    diffViewer.classList.remove("hidden");
    void renderDiffPanes(before, after, language || langFromPath(path));
  }

  function hideDiffViewer() {
    diffViewer?.classList.add("hidden");
  }

  function openExternalFile(filePath, focus) {
    if (!filePath) return;
    host.postMessage({ type: "openFile", path: filePath, focus: Boolean(focus) });
  }

  function openDiffMessage(path, before, after, sideBySide) {
    host.postMessage({
      type: "openDiff",
      path,
      before: before || "",
      after: after || "",
      focus: !sideBySide,
      sideBySide: Boolean(sideBySide),
    });
  }
  /** Canonical Orchestra tier label (L1–L5) from a legacy band name. */
  function subagentTierLabel(tier) {
    const t = String(tier || "").trim();
    if (!t) return "";
    if (/^L[1-5]$/i.test(t)) return t.toUpperCase();
    const map = { planner: "L5", lead: "L4", complex: "L3", focused: "L3", micro: "L1", explore: "L2" };
    return map[t.toLowerCase()] || "";
  }

  /**
   * Worker goals are often raw WorkOrder JSON ('{ "intent": "...", ... }').
   * Extract the human field instead of showing the JSON blob.
   */
  function humanizeTaskLabel(text) {
    const t = String(text || "").trim();
    if (!t.startsWith("{")) return t;
    try {
      const o = JSON.parse(t);
      const s = o.intent || o.goal || o.title || o.description || o.task_id || "";
      if (s) return String(s).trim();
    } catch {
      // Partial/invalid JSON — best-effort regex extraction.
      const m = t.match(/"(?:intent|goal|title|description)"\s*:\s*"((?:[^"\\]|\\.)*)/);
      if (m && m[1]) return m[1].replace(/\\"/g, '"');
    }
    return t;
  }

  function upsertSubagentTask(taskId, fields) {
    if (!taskId) return;
    const prev =
      subagentByTask.get(taskId) ||
      ({
        taskId,
        type: "agent",
        label: taskId,
        status: "running",
        toolCount: 0,
      });
    Object.assign(prev, fields);
    subagentByTask.set(taskId, prev);
    let idx = subagents.findIndex((s) => s.taskId === taskId);
    if (idx < 0 && prev.parentToolCallId) {
      // Upgrade the generic tool-call row (created on task/task_spawn start)
      // in place instead of appending a duplicate.
      idx = subagents.findIndex((s) => !s.taskId && s.id === prev.parentToolCallId);
    }
    const row = {
      id: prev.parentToolCallId || taskId,
      taskId,
      type: prev.type,
      label: prev.label,
      tier: prev.tier,
      model: prev.model,
      status: prev.status,
      error: prev.error,
      parentToolCallId: prev.parentToolCallId,
      toolsEl: prev.toolsEl,
      toolCount: prev.toolCount,
    };
    if (idx >= 0) subagents[idx] = row;
    else subagents.push(row);
    renderSubagents();
  }

  function ensureSubagentToolsHost(taskId, parentToolCallId) {
    const node = subagentByTask.get(taskId);
    if (!node || node.toolsEl) return node?.toolsEl;
    const parentBlock = parentToolCallId ? toolBlocks.get(parentToolCallId) : null;
    if (parentBlock) {
      let host = parentBlock.querySelector(".subagent-tools");
      if (!host) {
        host = document.createElement("div");
        host.className = "subagent-tools";
        host.dataset.taskId = taskId;
        parentBlock.appendChild(host);
      }
      node.toolsEl = host;
      return host;
    }
    if (!messagesEl) return undefined;
    const panel = document.createElement("div");
    panel.className = "subagent-panel";
    panel.dataset.taskId = taskId;
    const tierL = subagentTierLabel(node.tier);
    const tierHtml = tierL
      ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(node.model || "")}">${tierL}</span>`
      : "";
    panel.innerHTML =
      `<div class="subagent-panel-head">` +
      `<span class="subagent-type">${escapeAttr(node.type || "agent")}</span>` +
      tierHtml +
      `<span class="subagent-label">${escapeAttr(node.label || taskId)}</span>` +
      `</div>`;
    const host = document.createElement("div");
    host.className = "subagent-tools";
    panel.appendChild(host);
    messagesEl.appendChild(panel);
    node.toolsEl = host;
    return host;
  }

  function handleChildLifecycle(msg) {
    const taskId = msg.taskId || "";
    if (!taskId) return;
    if (msg.phase === "started") {
      const label = humanizeTaskLabel(msg.content || "");
      upsertSubagentTask(taskId, {
        parentToolCallId: msg.parentToolCallId,
        type: msg.subagentType || "agent",
        tier: msg.tier || "",
        model: msg.model || "",
        label: label.length > 48 ? label.slice(0, 45) + "…" : label || taskId,
        status: "running",
        toolCount: 0,
      });
      ensureSubagentToolsHost(taskId, msg.parentToolCallId);
    } else if (msg.phase === "done") {
      const st =
        msg.status === "done"
          ? "done"
          : msg.status === "error" || msg.status === "timeout"
            ? "error"
            : "done";
      const patch = { status: st };
      if (st === "error" && msg.error) {
        patch.error = String(msg.error);
      }
      const lessonHint = String(msg.lessonPromoteSuggestion || "").trim();
      const playbookHint = String(msg.playbookPromoteSuggestion || "").trim();
      if (lessonHint) patch.lessonPromote = lessonHint;
      if (playbookHint) patch.playbookPromote = playbookHint;
      upsertSubagentTask(taskId, patch);
      const hints = [];
      if (lessonHint) hints.push("lesson_promote");
      if (playbookHint) hints.push("playbook_promote");
      if (hints.length) {
        appendMsg(
          "system",
          `Learning: worker finished with ${hints.join(" + ")} suggestion — Lead should review task_result / call promote tool.`
        );
      }
    }
  }

  function resetTurnState() {
    assistantTurn = null;
    assistantTurnInner = null;
    assistantBubble = null;
    streamRawText = "";
    toolTraceEl = null;
    toolTraceSummary = null;
    reasoningDetails = null;
    reasoningBody = null;
    reasoningStarted = 0;
    turnToolCount = { read: 0, search: 0, write: 0, other: 0 };
    turnToolFirstStart = 0;
    turnToolLastEnd = 0;
  }

  // noteTurnToolStart/End track the group's wall clock. The TUI's tool group
  // shows this in its footer (ui/tui/view/tool_group.go); without it the
  // webview could say a turn ran nine tools but never how long that cost.
  function noteTurnToolStart() {
    if (!turnToolFirstStart) {
      turnToolFirstStart = Date.now();
    }
  }

  function noteTurnToolEnd() {
    turnToolLastEnd = Date.now();
    updateToolTraceSummary();
  }

  /** @returns {HTMLElement | null} */
  function ensureTurn() {
    if (assistantTurnInner) {
      return assistantTurnInner;
    }
    if (!messagesEl) {
      return null;
    }
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return assistantTurnInner;
  }

  function commitPreToolText() {
    flushAssistantMarkdown();
    if (!assistantBubble) {
      streamRawText = "";
      return;
    }
    const visible = sanitizeAssistantStream(stripFinalEnvelope(streamRawText)).trim();
    if (!visible) {
      assistantBubble.remove();
      assistantBubble = null;
      streamRawText = "";
      return;
    }
    assistantBubble.classList.add("turn-text-segment");
    assistantBubble = null;
    streamRawText = "";
  }

  /** @returns {HTMLElement | null} */
  function ensureToolTraceList() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!toolTraceEl) {
      toolTraceEl = document.createElement("details");
      toolTraceEl.className = "tool-trace trace-details";
      toolTraceSummary = document.createElement("summary");
      toolTraceSummary.className = "trace-summary";
      toolTraceSummary.textContent = i18n("turn.running_tools");
      const list = document.createElement("div");
      list.className = "tool-trace-list";
      toolTraceEl.appendChild(toolTraceSummary);
      toolTraceEl.appendChild(list);
      inner.appendChild(toolTraceEl);
      toolTraceEl.open = false;
    }
    return toolTraceEl.querySelector(".tool-trace-list");
  }

  function bumpToolTraceCount(name) {
    const kind = toolKind(name);
    if (kind === "read" || kind === "list") {
      turnToolCount.read++;
    } else if (kind === "search" || kind === "glob") {
      turnToolCount.search++;
    } else if (kind === "write") {
      turnToolCount.write++;
    } else {
      turnToolCount.other++;
    }
    updateToolTraceSummary();
  }

  function updateToolTraceSummary() {
    if (!toolTraceSummary) {
      return;
    }
    const parts = [];
    if (turnToolCount.read) {
      const n = turnToolCount.read;
      parts.push(`${n} ${n === 1 ? "read" : "reads"}`);
    }
    if (turnToolCount.search) {
      const n = turnToolCount.search;
      parts.push(`${n} ${n === 1 ? "search" : "searches"}`);
    }
    if (turnToolCount.write) {
      const n = turnToolCount.write;
      parts.push(`${n} ${n === 1 ? "edit" : "edits"}`);
    }
    if (turnToolCount.other) {
      const n = turnToolCount.other;
      parts.push(`${n} tool${n === 1 ? "" : "s"}`);
    }
    let text = parts.length ? `Explored ${parts.join(", ")}` : "Tools";
    // Only when a live tool both started and finished: a group still running,
    // or one replayed from history, has no honest number to show.
    if (turnToolFirstStart && turnToolLastEnd > turnToolFirstStart) {
      text += ` · ${formatToolDuration(turnToolLastEnd - turnToolFirstStart)}`;
    }
    toolTraceSummary.textContent = text;
  }

  /** @returns {HTMLElement | null} */
  function ensureReasoning() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!reasoningDetails) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = i18n("reason.brief");
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      inner.insertBefore(reasoningDetails, toolTraceEl || null);
      reasoningDetails.open = false;
      reasoningStarted = Date.now();
    }
    return reasoningBody;
  }

  function finalizeReasoningSummary() {
    if (!reasoningDetails || !reasoningBody) {
      return;
    }
    const text = (reasoningBody.textContent || "").trim();
    if (!text) {
      reasoningDetails.remove();
      reasoningDetails = null;
      reasoningBody = null;
      return;
    }
    const sum = reasoningDetails.querySelector(".trace-summary");
    if (sum) {
      const sec = reasoningStarted ? Math.round((Date.now() - reasoningStarted) / 1000) : 0;
      sum.textContent = sec >= 2 ? i18n("reason.for", { n: sec }) : i18n("reason.brief");
    }
    reasoningDetails.open = false;
  }

  function messagesHostFor(msg) {
    if (msg.scope === "child" && msg.taskId) {
      const node = subagentByTask.get(msg.taskId);
      if (node) {
        if (!node.toolsEl) {
          ensureSubagentToolsHost(msg.taskId, msg.parentToolCallId || node.parentToolCallId);
        }
        if (node.toolsEl) return node.toolsEl;
      }
    }
    // Write/edit diffs stay in the main turn stream — never inside collapsed tool trace.
    if (toolKind(msg.toolName) === "write") {
      return ensureTurn();
    }
    return ensureToolTraceList() || messagesEl;
  }

  function bindWriteToolHead(block, head) {
    head.classList.add("tool-head-write");
    const chev = head.querySelector(".tool-chev");
    if (chev) chev.remove();
    head.addEventListener("click", (e) => {
      if (e.target.closest?.(".diff-preview-name")) return;
      const fp = block.dataset.filePath || "";
      if (!fp) return;
      const d = findDiffForPath(fp);
      e.preventDefault();
      openDiffMessage(fp, d?.before || "", d?.after || "", /** @type {MouseEvent} */ (e).shiftKey);
    });
  }

  function tryShowWriteDiff(block, name, argsRaw, content) {
    if (!block || toolKind(name) !== "write") return;
    const filePath = toolPathFromArgs(name, argsRaw, content || "");
    if (!filePath) return;
    block.dataset.filePath = filePath;
    block.dataset.argsRaw = argsRaw || "";
    let diff = findDiffForPath(filePath);
    if (!diff) {
      diff = extractDiffFromTool(name, argsRaw, content || "");
    }
    if (diff) {
      rememberBlockDiff(block, diff.before || "", diff.after || "");
    }
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(block, filePath, diff.before || "", diff.after || "");
    } else {
      attachToolDiffShell(block, filePath);
    }
  }

  function basename(path) {
    if (!path) return "";
    const parts = path.replace(/\\/g, "/").split("/");
    return parts[parts.length - 1] || path;
  }

  function normalizePath(p) {
    return (p || "").replace(/\\/g, "/").toLowerCase();
  }

  function pathsMatch(a, b) {
    const na = normalizePath(a);
    const nb = normalizePath(b);
    if (!na || !nb) return false;
    if (na === nb) return true;
    if (na.endsWith("/" + nb) || nb.endsWith("/" + na)) return true;
    const ba = basename(na);
    const bb = basename(nb);
    return ba !== "" && ba === bb;
  }

  function bindPendingReviewListEvents() {
    if (!pendingReviewListEl || pendingReviewListEl.dataset.bound === "1") return;
    pendingReviewListEl.dataset.bound = "1";
    pendingReviewListEl.addEventListener("click", (e) => {
      const t = e.target;
      if (!(t instanceof HTMLElement)) return;
      const item = t.closest(".pending-review-item");
      if (!item) return;
      const idx = Number(item.getAttribute("data-idx"));
      if (!Number.isFinite(idx) || idx < 0 || idx >= pendingState.diff.length) return;
      const d = pendingState.diff[idx];
      if (!d) return;

      const actBtn = t.closest(".pending-item-act");
      if (actBtn instanceof HTMLElement) {
        if (!d.path) return;
        const paths = [String(d.path)];
        if (actBtn.getAttribute("data-act") === "keep") applyPendingChanges(paths);
        else discardPendingChanges(paths);
        return;
      }

      const nameBtn = t.closest(".diff-preview-name");
      if (nameBtn) {
        diffReviewCursor = idx;
        renderPendingReviewList();
        if (!d.path) return;
        if (/** @type {MouseEvent} */ (e).altKey) {
          openExternalFile(d.path, true);
          return;
        }
        openDiffMessage(d.path, d.before || "", d.after || "", /** @type {MouseEvent} */ (e).shiftKey);
      }
    });
  }

  function diffReviewActive() {
    return pendingState.diff.length > 0 && document.activeElement !== inputEl;
  }

  async function renderUnifiedDiffLines(container, before, after, filePath, maxLines = 32) {
    const lang = langFromPath(filePath || "");
    const allRows = alignDiffLines(before, after);
    const changedRows = allRows.filter((r) => r.type !== "same");
    const displayRows = changedRows.slice(0, maxLines);
    if (displayRows.length === 0) {
      const hint = document.createElement("div");
      hint.className = "diff-empty-hint";
      hint.textContent = i18n(
        (before || "") === (after || "") ? "diff.no_changes" : "diff.unavailable"
      );
      container.appendChild(hint);
      return;
    }
    const codeLines = displayRows.map((r) =>
      r.type === "del" ? r.left ?? "" : r.type === "add" ? r.right ?? "" : ""
    );
    const htmlLines = await requestHighlight(codeLines, lang);
    displayRows.forEach((r, idx) => {
      const row = document.createElement("div");
      row.className = "diff-u-row " + r.type;
      const ln = r.type === "del" ? r.leftNum : r.rightNum;
      const gutter = r.type === "del" ? "−" : "+";
      row.innerHTML =
        `<span class="diff-u-ln">${ln || ""}</span>` +
        `<span class="diff-u-gutter">${gutter}</span>` +
        `<span class="diff-u-code">${htmlLines[idx] || "&nbsp;"}</span>`;
      container.appendChild(row);
    });
    if (changedRows.length > maxLines) {
      const more = document.createElement("div");
      more.className = "diff-more";
      more.textContent = i18n("diff.more_lines", { n: changedRows.length - maxLines });
      container.appendChild(more);
    }
  }

  async function appendDiffLines(container, before, after, filePath, maxLines = 32) {
    const wrap = document.createElement("div");
    wrap.className = "diff-u-block";
    await renderUnifiedDiffLines(wrap, before, after, filePath, maxLines);
    container.appendChild(wrap);
  }

  async function attachInlineToolDiff(block, filePath, before, after) {
    if (!block || !filePath) return;
    rememberBlockDiff(block, before, after);
    block.querySelector(".file-change-card")?.remove();
    block.querySelector(".tool-head")?.remove();
    block.classList.add("write-card-only");
    const stats = countDiffStats(before, after);
    let diffWrap = block.querySelector(".tool-diff-body.diff-preview-card");
    if (!diffWrap) {
      const body = block.querySelector(".tool-body");
      diffWrap = document.createElement("div");
      diffWrap.className = "tool-body tool-diff-body diff-preview-card";
      const head = document.createElement("div");
      head.className = "diff-preview-head";
      head.innerHTML =
        diffExtBadgeHtml(filePath) +
        `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
        diffStatsHtml(stats);
      const lines = document.createElement("div");
      lines.className = "diff-preview-body";
      diffWrap.appendChild(head);
      diffWrap.appendChild(lines);
      if (body) {
        body.replaceWith(diffWrap);
      } else {
        block.appendChild(diffWrap);
      }
      head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
      });
    } else {
      const head = diffWrap.querySelector(".diff-preview-head");
      if (head) {
        head.innerHTML =
          diffExtBadgeHtml(filePath) +
          `<button type="button" class="diff-preview-name" title="Open file (Shift+click: side-by-side diff)">${escapeAttr(basename(filePath))}</button>` +
          diffStatsHtml(stats);
        head.querySelector(".diff-preview-name")?.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          openDiffMessage(filePath, before, after, /** @type {MouseEvent} */ (e).shiftKey);
        });
      }
    }
    const lines = diffWrap.querySelector(".diff-preview-body");
    if (!lines) return;
    lines.classList.remove("tool-diff-pending-body");
    lines.textContent = "";
    await renderUnifiedDiffLines(lines, before, after, filePath, 40);
  }
  function hideOverlay() {
    overlay?.classList.add("hidden");
    if (overlayOptions) overlayOptions.innerHTML = "";
    if (overlayActions) overlayActions.innerHTML = "";
    overlayInput?.classList.add("hidden");
  }

  /** @param {any} request */
  function showPermissionOverlay(request) {
    if (!overlay || !overlayTitle || !overlayBody || !overlayActions) return;
    const isLSP = request.kind === "lsp.install" || request.tool === "lsp.install";
    overlayTitle.textContent = isLSP
      ? i18n("perm.install_lsp")
      : i18n("perm.allow_tool", { tool: request.tool || i18n("perm.tool") });
    const extra = isLSP ? i18n("perm.install_extra") : "";
    overlayBody.textContent = [request.description, request.reason, extra]
      .filter(Boolean)
      .join("\n\n");
    overlayActions.innerHTML = "";
    const buttons = isLSP
      ? [
          { label: i18n("perm.skip"), approved: false },
          { label: i18n("perm.install_once"), approved: true },
          { label: i18n("perm.install_always"), approved: true, always: true },
        ]
      : [
          { label: i18n("perm.deny"), approved: false },
          { label: i18n("perm.allow_once"), approved: true },
          { label: i18n("perm.allow_always"), approved: true, always: true },
        ];
    buttons.forEach((btn) => {
      const el = document.createElement("button");
      el.type = "button";
      el.className = "pill" + (btn.approved ? " primary" : "");
      el.textContent = btn.label;
      el.addEventListener("click", () => {
        hideOverlay();
        host.postMessage({
          type: "permissionReply",
          approved: btn.approved,
          always: Boolean(btn.always),
        });
      });
      overlayActions.appendChild(el);
    });
    overlay.classList.remove("hidden");
  }

  /** @param {any[]} questions */
  function showQuestionOverlay(questions) {
    if (!questions.length) {
      host.postMessage({ type: "questionReply", answers: [] });
      return;
    }
    questionState = { questions, index: 0, answers: [], mode: "question" };
    renderQuestionStep();
  }

  function renderQuestionStep() {
    const q = questionState.questions[questionState.index];
    if (!q || !overlay || !overlayTitle || !overlayBody || !overlayOptions || !overlayActions) {
      host.postMessage({ type: "questionReply", answers: questionState.answers });
      hideOverlay();
      return;
    }
    overlayTitle.textContent = i18n("question.step", {
      n: questionState.index + 1,
      total: questionState.questions.length,
    });
    overlayBody.textContent = q.question || "";
    overlayOptions.innerHTML = "";
    overlayActions.innerHTML = "";
    if (q.options && q.options.length) {
      q.options.forEach((opt) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "pill";
        btn.textContent = opt;
        btn.addEventListener("click", () => {
          questionState.answers.push(opt);
          questionState.index += 1;
          renderQuestionStep();
        });
        overlayOptions.appendChild(btn);
      });
    } else {
      overlayInput?.classList.remove("hidden");
      if (overlayInput) overlayInput.value = "";
      const next = document.createElement("button");
      next.type = "button";
      next.className = "pill primary";
      next.textContent = i18n("question.next");
      next.addEventListener("click", () => {
        questionState.answers.push(overlayInput?.value || "");
        questionState.index += 1;
        overlayInput?.classList.add("hidden");
        renderQuestionStep();
      });
      overlayActions.appendChild(next);
    }
    overlay.classList.remove("hidden");
  }

  function matchJSONObject(s, start) {
    let depth = 0;
    let inStr = false;
    let esc = false;
    for (let j = start; j < s.length; j++) {
      const c = s[j];
      if (esc) {
        esc = false;
        continue;
      }
      if (c === "\\") {
        esc = true;
        continue;
      }
      if (c === '"') {
        inStr = !inStr;
        continue;
      }
      if (inStr) {
        continue;
      }
      if (c === "{") {
        depth++;
      } else if (c === "}") {
        depth--;
        if (depth === 0) {
          return j;
        }
      }
    }
    return -1;
  }

  /** @param {string} text */
  function sanitizeAssistantStream(text) {
    let t = String(text || "").trim();
    if (!t) return "";
    if (t.startsWith('"') && !t.endsWith('"') && t.length < 400) {
      t = t.replace(/^"+/, "").trim();
    }
    function digitLikeRatio(s) {
      if (!s.length) return 0;
      let n = 0;
      for (const c of s) {
        if (/[\d.eE+\-]/.test(c)) n++;
      }
      return n / s.length;
    }
    function looksCorrupted(s) {
      const x = s.trim();
      if (x.length < 40) return false;
      if (/0{48,}/.test(x)) return true;
      if (x.length >= 120 && digitLikeRatio(x) > 0.75) return true;
      if (/Serving user request/i.test(x) && digitLikeRatio(x.slice(30)) > 0.6) return true;
      return false;
    }
    const numericRun = t.match(/([\d.eE+\-]{80,}|0{32,})/);
    if (numericRun && numericRun.index > 0) {
      t = t.slice(0, numericRun.index).trimEnd().replace(/^"+|"+$/g, "").trim();
    }
    if (looksCorrupted(t)) {
      const prefix = (t.split(/[\d.eE]{20,}/)[0] || "").trim().replace(/^"+|"+$/g, "").trim();
      if (prefix.length > 0 && prefix.length < 240 && !looksCorrupted(prefix)) return prefix;
      return "";
    }
    return t;
  }

  function stripFinalEnvelope(text) {
    let out = text;
    for (;;) {
      const i = out.indexOf("{");
      if (i < 0) {
        return out;
      }
      const end = matchJSONObject(out, i);
      if (end < 0) {
        const tail = out.slice(i);
        if (
          tail.includes('"patches"') ||
          (tail.includes('"type"') && tail.includes('"final"'))
        ) {
          return out.slice(0, i).trimEnd();
        }
        return out;
      }
      const blob = out.slice(i, end + 1);
      if (blob.includes('"patches"')) {
        out = (out.slice(0, i) + out.slice(end + 1)).trim();
        continue;
      }
      return out.slice(0, end + 1) + stripFinalEnvelope(out.slice(end + 1));
    }
  }
  function runStatusLabel() {
    const hint = chromeHint?.textContent?.trim();
    if (hint && !chromeHint.classList.contains("hidden")) {
      return hint;
    }
    let base = busyStatusText || i18n("turn.working");
    if (busy && sendQueue.length > 0) {
      const n = sendQueue.length;
      base += ` · ${n} queued`;
    }
    return base;
  }

  function renderSendQueue() {
    if (!messageQueueEl) {
      return;
    }
    if (!sendQueue.length) {
      messageQueueEl.classList.add("hidden");
      messageQueueEl.replaceChildren();
      return;
    }
    messageQueueEl.classList.remove("hidden");
    messageQueueEl.replaceChildren();
    const head = document.createElement("div");
    head.className = "queue-head";
    head.textContent =
      sendQueue.length === 1 ? "1 message queued" : `${sendQueue.length} messages queued`;
    messageQueueEl.appendChild(head);
    sendQueue.forEach((item, idx) => {
      const row = document.createElement("div");
      row.className = "queue-item";
      row.dataset.queueId = item.id;
      const pos = document.createElement("span");
      pos.className = "queue-pos";
      pos.textContent = String(idx + 1);
      const text = document.createElement("span");
      text.className = "queue-text";
      const preview = (item.preview || "").trim();
      text.textContent = preview || (item.fileCount ? `${item.fileCount} attachment(s)` : "…");
      text.title = preview;
      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "queue-cancel";
      rm.setAttribute("aria-label", i18n("queue.remove"));
      rm.textContent = "×";
      rm.addEventListener("click", () => {
        host.postMessage({ type: "cancelQueuedSend", id: item.id });
      });
      row.appendChild(pos);
      row.appendChild(text);
      row.appendChild(rm);
      messageQueueEl.appendChild(row);
    });
    updateBusyUi();
  }

  function beginTurn() {
    assistantBubble = null;
    resetTurnState();
    toolBlocks.clear();
    toolArgs.clear();
    execSteps.clear();
  }

  function setTypingIndicator(show, label) {
    if (!messagesEl) {
      return;
    }
    if (!show) {
      typingIndicatorEl?.remove();
      typingIndicatorEl = null;
      return;
    }
    if (!typingIndicatorEl) {
      typingIndicatorEl = document.createElement("div");
      typingIndicatorEl.className = "msg typing-indicator";
      typingIndicatorEl.setAttribute("aria-label", i18n("typing.aria"));
      typingIndicatorEl.innerHTML =
        '<span class="typing-dots" aria-hidden="true">' +
        '<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>' +
        "</span>" +
        '<span class="typing-label"></span>';
      messagesEl.appendChild(typingIndicatorEl);
    }
    const lab = typingIndicatorEl.querySelector(".typing-label");
    if (lab) {
      lab.textContent = label || i18n("turn.working");
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function updateBusyUi() {
    const label = runStatusLabel();
    if (composerStatus) {
      composerStatus.classList.toggle("hidden", !busy);
      composerStatus.setAttribute("aria-busy", busy ? "true" : "false");
    }
    if (composerStatusLabel) {
      composerStatusLabel.textContent = label;
    }
    if (composerWrap) {
      composerWrap.classList.toggle("composer-busy", busy);
    }
    if (sendBtn) {
      sendBtn.classList.toggle("is-busy", busy);
      sendBtn.setAttribute("aria-busy", busy ? "true" : "false");
      sendBtn.title = busy ? "Stop" : "Send";
      sendBtn.setAttribute("aria-label", busy ? "Stop" : "Send");
    }
    setTypingIndicator(busy && !assistantBubble?.textContent?.trim(), label);
  }

  function setChromeHint(text, isError) {
    if (!chromeHint) {
      return;
    }
    const t = (text || "").trim();
    if (!t) {
      chromeHint.textContent = "";
      chromeHint.classList.add("hidden");
      chromeHint.classList.remove("error");
      if (busy) {
        busyStatusText = i18n("turn.working");
        updateBusyUi();
      }
      return;
    }
    chromeHint.textContent = t;
    chromeHint.classList.remove("hidden");
    chromeHint.classList.toggle("error", Boolean(isError));
    if (busy && !isError) {
      busyStatusText = t;
      updateBusyUi();
    }
  }

  function setBusy(next) {
    busy = next;
    if (contextBtn) {
      contextBtn.classList.toggle("busy", next);
    }
    if (next) {
      if (!busyStatusText) {
        busyStatusText = i18n("turn.working");
      }
    } else {
      busyStatusText = i18n("turn.working");
    }
    updateBusyUi();
    if (todos.length) {
      renderTodos();
    }
    if (!next) {
      if (!todosDoneFlashTimer) {
        if (chromeHint) {
          chromeHint.textContent = "";
          chromeHint.classList.add("hidden");
          chromeHint.classList.remove("error");
        }
      }
      if (workflowActiveName) {
        setWorkflow("", false);
      }
    }
  }

  function flashTodosDone() {
    setChromeHint(i18n("turn.tasks_done"), false);
    clearTimeout(todosDoneFlashTimer);
    todosDoneFlashTimer = window.setTimeout(() => {
      todosDoneFlashTimer = 0;
      setChromeHint("", false);
    }, 2500);
  }

  function truncateTodoLabel(text, max) {
    const t = (text || "").trim();
    if (t.length <= max) {
      return t;
    }
    return t.slice(0, max - 1) + "…";
  }

  function closeMenus() {
    modeMenu?.classList.remove("open");
    effortMenu?.classList.remove("open");
    accessMenu?.classList.remove("open");
    sessionMenu?.classList.remove("open");
    modelMenu?.classList.remove("open");
    modeBtn?.classList.remove("open");
    effortBtn?.classList.remove("open");
    accessBtn?.classList.remove("open");
    sessionHistoryBtn?.classList.remove("open");
    modelPill?.classList.remove("open");
    hidePalette();
  }

  function hidePalette() {
    paletteMode = "";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu?.classList.add("hidden");
    if (paletteMenu) paletteMenu.innerHTML = "";
  }

  function detectPaletteQuery(text) {
    const t = text || "";
    const slash = t.match(/(?:^|\s)(\/[\w-]*)$/);
    if (slash) {
      return { mode: "slash", query: slash[1].slice(1).toLowerCase() };
    }
    const at = t.match(/(?:^|\s)@([^\s@]*)$/);
    if (at) {
      return { mode: "mention", query: at[1] };
    }
    return null;
  }

  function filterSlash(query) {
    const q = (query || "").toLowerCase();
    return SLASH_CMDS.concat(SKILL_CMDS).filter((c) => c.cmd.slice(1).startsWith(q));
  }

  function renderPalette(mode, items) {
    if (!paletteMenu) return;
    paletteMode = mode;
    paletteItems = items;
    paletteIndex = 0;
    paletteMenu.innerHTML = "";
    if (!items.length) {
      if (mode === "mention") {
        const head = document.createElement("div");
        head.className = "menu-section palette-head";
        head.textContent = i18n("palette.files");
        paletteMenu.appendChild(head);
        const empty = document.createElement("div");
        empty.className = "palette-empty";
        empty.textContent = i18n("palette.no_files");
        paletteMenu.appendChild(empty);
        paletteMenu.classList.remove("hidden");
        return;
      }
      hidePalette();
      return;
    }
    if (mode === "mention") {
      const head = document.createElement("div");
      head.className = "menu-section palette-head";
      head.textContent = i18n("palette.files");
      paletteMenu.appendChild(head);
    }
    items.slice(0, 12).forEach((item, i) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item palette-item" + (i === 0 ? " selected" : "");
      if (mode === "slash") {
        // Built-ins carry a catalogue key; skills carry their own description,
        // which is whatever the skill's own file says and is not translated.
        const desc = item.descKey ? i18n(item.descKey) : item.desc || "";
        btn.innerHTML = `<span class="palette-cmd">${item.cmd}</span><span class="palette-desc">${escapeHtml(desc)}</span>`;
      } else {
        const kind = item.kind || "binary";
        const thumb =
          kind === "image" && item.previewUri
            ? `<img class="palette-thumb" src="${escapeAttr(item.previewUri)}" alt="" />`
            : `<span class="palette-file-icon">${fileExtLabel(item.ext || item.name)}</span>`;
        btn.innerHTML =
          `${thumb}<span class="palette-file-meta">` +
          `<span class="palette-cmd">${escapeAttr(item.name)}</span>` +
          `<span class="palette-desc">${escapeAttr(relPathDisplay(item.path || ""))}</span>` +
          `</span>`;
      }
      btn.addEventListener("mousedown", (e) => {
        e.preventDefault();
        applyPaletteItem(i);
      });
      paletteMenu.appendChild(btn);
    });
    paletteMenu.classList.remove("hidden");
  }

  function applyPaletteItem(index) {
    const item = paletteItems[index];
    if (!item || !inputEl) return;
    const text = inputEl.value;
    if (paletteMode === "slash") {
      const trimmed = text.replace(/(?:^|\s)(\/[\w-]*)$/, "").trimEnd();
      if (item.cmd === "/compact") {
        inputEl.value = trimmed;
        hidePalette();
        host.postMessage({ type: "slashCommand", cmd: "/compact" });
        return;
      }
      inputEl.value = trimmed;
      hidePalette();
      host.postMessage({ type: "slashCommand", cmd: item.cmd });
      return;
    }
    if (paletteMode === "mention") {
      const base = text.replace(/(?:^|\s)@([^\s@]*)$/, "").trimEnd();
      inputEl.value = base ? base + " " : "";
      addFileRef(item);
      hidePalette();
      autoGrow();
      inputEl.focus();
      return;
    }
  }

  function showMentionLoading() {
    if (!paletteMenu) {
      return;
    }
    paletteMode = "mention";
    paletteItems = [];
    paletteIndex = 0;
    paletteMenu.innerHTML =
      '<div class="menu-section palette-head">Files</div>' +
      '<div class="palette-loading">Searching…</div>';
    paletteMenu.classList.remove("hidden");
  }

  function onInputPalette() {
    if (!inputEl) return;
    const hit = detectPaletteQuery(inputEl.value);
    if (!hit) {
      hidePalette();
      return;
    }
    if (hit.mode === "slash") {
      renderPalette("slash", filterSlash(hit.query));
      return;
    }
    showMentionLoading();
    clearTimeout(mentionTimer);
    mentionTimer = window.setTimeout(() => {
      host.postMessage({ type: "mentionSearch", query: hit.query });
    }, hit.query ? 120 : 180);
  }

  function renderTodos() {
    if (!todosBar || !todosList) return;
    if (!todos.length) {
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      todosHadOpen = false;
      todosExpanded = false;
      return;
    }
    const normalized = todos.map((t) => ({
      ...t,
      status: normalizeTodoStatus(t.status),
    }));
    let done = 0;
    let cancelled = 0;
    for (const t of normalized) {
      if (t.status === "done") done++;
      if (t.status === "cancelled") cancelled++;
    }
    const open = normalized.filter((t) => t.status !== "done" && t.status !== "cancelled");
    const total = normalized.length - cancelled;

    if (!open.length) {
      if (done > 0 && todosHadOpen) {
        flashTodosDone();
      }
      todosHadOpen = false;
      todosExpanded = false;
      todosBar.classList.add("hidden");
      todosList.innerHTML = "";
      todosList.classList.add("hidden");
      return;
    }

    todosHadOpen = true;
    todosBar.classList.remove("hidden");

    const inProg = open.find((t) => t.status === "in_progress");
    const focus = inProg || open[0];
    const focusLabel = truncateTodoLabel(focus.content || focus.id || "Task", 52);
    const spinning = Boolean(inProg && busy);

    if (todosChipGlyph) {
      todosChipGlyph.innerHTML = orchIconMarkup(inProg ? "box-active" : "box", { size: "sm" });
      todosChipGlyph.classList.toggle("spinning", spinning);
    }
    if (todosChipSummary) {
      todosChipSummary.textContent = `${done}/${total} · ${focusLabel}`;
    }
    if (todosChipChev) {
      todosChipChev.innerHTML = orchIconMarkup(todosExpanded ? "chevron-up" : "chevron-down", { size: "sm" });
    }
    if (todosChip) {
      todosChip.setAttribute("aria-expanded", todosExpanded ? "true" : "false");
    }

    if (!todosExpanded) {
      todosList.classList.add("hidden");
      todosList.innerHTML = "";
      return;
    }

    todosList.classList.remove("hidden");
    todosList.innerHTML = "";
    const sorted = [...open].sort((a, b) => {
      const rank = (s) => (s === "in_progress" ? 0 : 1);
      return rank(a.status) - rank(b.status);
    });
    sorted.slice(0, 12).forEach((t) => {
      const row = document.createElement("div");
      row.className = "todo-row status-" + t.status;
      row.setAttribute("role", "listitem");
      const glyph = document.createElement("span");
      glyph.className = "todo-glyph" + (t.status === "in_progress" && busy ? " spinning" : "");
      glyph.innerHTML = orchIconMarkup(todoGlyph(t.status), { size: "sm" });
      const text = document.createElement("span");
      text.className = "todo-text";
      text.textContent = t.content || t.id;
      row.appendChild(glyph);
      row.appendChild(text);
      todosList.appendChild(row);
    });
    if (open.length > 12) {
      const more = document.createElement("div");
      more.className = "todo-more";
      more.textContent = `+${open.length - 12} more`;
      todosList.appendChild(more);
    }
  }

  function normalizeTodoStatus(status) {
    const s = (status || "pending").toLowerCase();
    if (s === "completed" || s === "complete") return "done";
    if (s === "inprogress" || s === "in-progress") return "in_progress";
    return s;
  }

  /** The icon NAME for a checklist row's state — see media/icons.js. */
  function todoGlyph(status) {
    const s = normalizeTodoStatus(status);
    if (s === "done") return "box-check";
    if (s === "cancelled") return "box-cross";
    if (s === "in_progress") return "box-active";
    return "box";
  }
  /**
   * The family a tool belongs to. Everything downstream — the icon, the
   * heading, the CSS accent — hangs off this one answer, so a tool missing
   * here is a step that renders as a nameless dot.
   *
   * The families below cover `internal/tools/registry.go` as it stands; the
   * prefix rules at the end are what keep a newly registered `git.*` or
   * `mcp:*` tool recognised without another edit here.
   */
  function toolKind(name) {
    const n = (name || "").toLowerCase();
    if (["read", "fs.read"].includes(n)) return "read";
    if (["ls", "list", "fs.list"].includes(n)) return "list";
    if (["write", "edit", "fs.write", "file.write_atomic"].includes(n)) return "write";
    if (["grep", "search.text", "search"].includes(n)) return "search";
    if (["glob"].includes(n)) return "glob";
    if (["symbols", "code.symbols"].includes(n)) return "symbols";
    if (["bash", "exec.run", "exec"].includes(n)) return "exec";
    if (["task", "task_spawn", "task_wait", "task_cancel", "task_result"].includes(n)) return "task";
    if (["todowrite", "todoread"].includes(n)) return "todo";
    if (["explore"].includes(n)) return "explore";
    if (["diff.preview"].includes(n)) return "diff";
    if (["fs.delete"].includes(n)) return "trash";
    if (["fs.rename"].includes(n)) return "rename";
    if (["webfetch", "websearch"].includes(n)) return "web";
    if (["memory_write"].includes(n)) return "memory";
    if (["question"].includes(n)) return "question";
    if (["skill_invoke"].includes(n)) return "skill";
    if (["plan_exit", "plan_enter"].includes(n)) return "plan";
    if (["runtime_query"].includes(n)) return "runtime";
    // Families, so a tool added to one of them needs nothing here.
    if (n.startsWith("git.") || n.startsWith("gh.")) return "git";
    if (n.startsWith("lsp.")) return "lsp";
    if (n.startsWith("mcp:")) return "mcp";
    if (n.startsWith("browser.") || n.startsWith("browser_")) return "web";
    return "other";
  }

  /**
   * The icon NAME (see media/icons.js) for a tool — never a glyph. Every
   * family has one, the fallback included: a step with no icon is a step
   * the reader cannot tell apart from its neighbours at a glance, which is
   * the whole job of this column.
   */
  const TOOL_ICON_BY_KIND = {
    read: "read",
    list: "list",
    write: "write",
    search: "search",
    glob: "glob",
    symbols: "symbols",
    exec: "exec",
    task: "task",
    todo: "todo",
    explore: "explore",
    diff: "diff",
    trash: "trash",
    rename: "write",
    web: "web",
    memory: "memory",
    question: "question",
    skill: "skill",
    plan: "mode-plan",
    runtime: "lsp",
    git: "git",
    lsp: "lsp",
    mcp: "mcp",
    other: "tool",
  };

  function toolIcon(name) {
    return TOOL_ICON_BY_KIND[toolKind(name)] || "tool";
  }

  /** The icon as markup, at the size a tool row uses. */
  function toolIconMarkup(name) {
    return orchIconMarkup(toolIcon(name), { size: "md" });
  }

  function toolDisplayName(name) {
    switch (toolKind(name)) {
      case "read":
        return "Read";
      case "list":
        return "Ls";
      case "write":
        return name === "edit" ? "Edit" : "Write";
      case "search":
        return "Grep";
      case "glob":
        return "Glob";
      case "symbols":
        return "Symbols";
      case "exec":
        return "Shell";
      case "task":
        return "Task";
      default:
        // The raw name. `git.log`, `lsp.references` and `mcp:ctx7:query-docs`
        // say more about the step than any word this function could invent
        // for them, and they are the names the docs and the CLI use.
        return name || "Tool";
    }
  }

  /**
   * Whether a finished tool's result is a failure. The core reports tool
   * errors as the result text rather than out of band, so this is the only
   * signal a renderer has — keep it in one place so the head, the accent
   * and the subagent tree all agree on what failed.
   */
  function toolResultIsError(content) {
    const s = String(content || "").trimStart().toLowerCase();
    return s.startsWith("error") || s.startsWith('{"error"') || s.startsWith('{"ok":false');
  }

  function parseToolArgs(raw) {
    const out = {};
    const text = (raw || "").trim();
    if (!text) return out;
    try {
      return JSON.parse(text);
    } catch {
      const patched = text + '"'.repeat(text.split('"').length % 2 === 0 ? 0 : 1);
      let depth = 0;
      let inStr = false;
      let esc = false;
      for (const ch of patched) {
        if (esc) {
          esc = false;
          continue;
        }
        if (ch === "\\") {
          esc = true;
          continue;
        }
        if (ch === '"') inStr = !inStr;
        else if (!inStr) {
          if (ch === "{") depth++;
          if (ch === "}") depth--;
        }
      }
      let fix = patched;
      for (let i = 0; i < depth; i++) fix += "}";
      try {
        return JSON.parse(fix);
      } catch {
        return out;
      }
    }
  }

  function toolPathFromArgs(name, argsRaw, content) {
    const args = parseToolArgs(argsRaw);
    let path =
      args.path || args.filePath || args.file_path || args.target || "";
    if (!path && content) {
      try {
        const parsed = JSON.parse(content);
        path = parsed.path || parsed.filePath || parsed.file_path || "";
      } catch {
        /* ignore */
      }
    }
    return typeof path === "string" ? path.trim() : "";
  }

  function toolPreviewLine(name, argsRaw, content) {
    const disp = toolDisplayName(name);
    const path = toolPathFromArgs(name, argsRaw, content);
    if (path) {
      return `${disp} ${basename(path)}`;
    }
    if (toolKind(name) === "search" || toolKind(name) === "glob") {
      const args = parseToolArgs(argsRaw);
      const pat = args.pattern || args.query || "";
      return pat ? `${disp} "${pat}"` : disp;
    }
    if (toolKind(name) === "exec") {
      const args = parseToolArgs(argsRaw);
      const cmd = args.command || args.description || "";
      if (cmd) {
        return cmd.length > 56 ? cmd.slice(0, 53) + "…" : cmd;
      }
    }
    if (content && content.length < 80) {
      return `${disp} · ${content}`;
    }
    return disp;
  }

  // formatToolDuration renders a tool's wall time the way the TUI does
  // (ui/tui/view/tool_group.go): sub-second work in ms because "0.4s" hides
  // the difference between 350ms and 450ms, seconds with one decimal, and
  // anything past a minute in m/s because "91.4s" stops being readable.
  function formatToolDuration(ms) {
    if (!Number.isFinite(ms) || ms < 0) return "";
    if (ms < 1000) return Math.round(ms) + "ms";
    // 59950+ rounds to "60.0s", which reads like a bug next to "1m 00s".
    if (ms < 59950) return (ms / 1000).toFixed(1) + "s";
    // Round to whole seconds BEFORE splitting: rounding the remainder
    // separately turns 59999ms into "0m 60s".
    const totalSec = Math.round(ms / 1000);
    return Math.floor(totalSec / 60) + "m " + String(totalSec % 60).padStart(2, "0") + "s";
  }

  /* ---- a tool's result, made readable ------------------------------ *
   * Tool results arrive as one string. Most are JSON, and most of those
   * are the core's `{"output": "…"}` envelope wrapping text that was never
   * JSON to begin with — so printed raw, the thing a reader wants is
   * behind a layer of escaped newlines and quotes. These four functions
   * unwrap that, pretty-print what is genuinely structured, and keep the
   * unmodified original one click away.
   * ------------------------------------------------------------------ */

  /** Lines shown before the body asks to be expanded. */
  const TOOL_BODY_PREVIEW_LINES = 24;
  /** The ceiling even an expanded body will not go past, in lines. */
  const TOOL_BODY_MAX_LINES = 5000;

  /** The full, unmodified result text of a block, kept out of the DOM. */
  const toolFullContent = new WeakMap();

  /**
   * What to actually show for a result string.
   * @param {string} raw
   * @returns {{ text: string; kind: "json" | "text"; unwrapped: boolean }}
   */
  function toolResultView(raw) {
    const s = String(raw == null ? "" : raw);
    const trimmed = s.trim();
    if (!trimmed || (trimmed[0] !== "{" && trimmed[0] !== "[")) {
      return { text: s, kind: "text", unwrapped: false };
    }
    let parsed;
    try {
      parsed = JSON.parse(trimmed);
    } catch {
      return { text: s, kind: "text", unwrapped: false };
    }
    // The core's single-field envelope. Its payload is text — command
    // output, a file listing, a log — and reading it as text is the whole
    // point of unwrapping it.
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const keys = Object.keys(parsed);
      if (keys.length === 1 && typeof parsed[keys[0]] === "string" && ["output", "text", "content", "result", "stdout"].includes(keys[0])) {
        return { text: parsed[keys[0]], kind: "text", unwrapped: true };
      }
    }
    return { text: JSON.stringify(parsed, null, 2), kind: "json", unwrapped: false };
  }

  /**
   * Colour a pretty-printed JSON document. A tokeniser rather than a
   * parser: it runs over text this renderer produced with
   * JSON.stringify, so the grammar it has to survive is only ever that.
   * Everything is escaped before a span goes near it.
   */
  function highlightJson(text) {
    return escapeHtml(text).replace(
      /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
      (m, str, colon, lit, num) => {
        if (str) {
          return colon
            ? `<span class="jkey">${str}</span>${colon}`
            : `<span class="jstr">${str}</span>`;
        }
        if (lit) return `<span class="jlit">${lit}</span>`;
        if (num) return `<span class="jnum">${num}</span>`;
        return m;
      }
    );
  }

  /** "12 lines · 3.4 KB", the label that tells you what you are not seeing. */
  function toolBodyMeta(lineCount, byteLength) {
    const kb = byteLength / 1024;
    const size = kb >= 1 ? `${kb >= 10 ? Math.round(kb) : kb.toFixed(1)} KB` : `${byteLength} B`;
    return `${i18n("tool.body.lines", { n: lineCount })} · ${size}`;
  }

  /**
   * Paint a tool block's body from the result kept in toolFullContent,
   * honouring the block's own two switches: pretty vs raw, and folded vs
   * whole. Called on completion and again on every click that flips one.
   */
  function renderToolBody(block) {
    const body = block?.querySelector?.(".tool-body");
    if (!body) return;
    const pre = body.querySelector(".tool-body-pre");
    const metaEl = body.querySelector(".tool-body-meta");
    const moreBtn = body.querySelector(".tool-body-more");
    const fmtBtn = body.querySelector('[data-body-action="format"]');
    if (!pre) return;

    const full = toolFullContent.get(block) || "";
    const raw = block.dataset.bodyFormat === "raw";
    // Parsed once. A tool result can be a megabyte of JSON, and this runs
    // again on every fold, unfold and format flip.
    const formatted = toolResultView(full);
    const view = raw ? { text: full, kind: "text", unwrapped: false } : formatted;
    const lines = view.text.split("\n");
    const expanded = block.dataset.bodyExpanded === "1";
    const capped = lines.length > TOOL_BODY_MAX_LINES;
    const shown = expanded
      ? lines.slice(0, TOOL_BODY_MAX_LINES)
      : lines.slice(0, TOOL_BODY_PREVIEW_LINES);
    const text = shown.join("\n");

    if (view.kind === "json") {
      pre.innerHTML = highlightJson(text);
    } else {
      pre.textContent = text;
    }
    pre.classList.toggle("is-json", view.kind === "json");

    if (metaEl) metaEl.textContent = toolBodyMeta(lines.length, full.length);
    if (fmtBtn) {
      // Offered only when there is a second way to read the same bytes.
      fmtBtn.hidden = !(formatted.kind === "json" || formatted.unwrapped);
      fmtBtn.textContent = raw ? i18n("tool.body.pretty") : i18n("tool.body.raw");
    }
    if (moreBtn) {
      const hidden = lines.length - shown.length;
      if (hidden > 0) {
        moreBtn.hidden = false;
        moreBtn.textContent = i18n("tool.body.show_all", { n: lines.length });
      } else if (expanded && lines.length > TOOL_BODY_PREVIEW_LINES) {
        moreBtn.hidden = false;
        moreBtn.textContent = capped ? i18n("tool.body.capped", { n: TOOL_BODY_MAX_LINES }) : i18n("tool.body.collapse");
        moreBtn.disabled = capped;
      } else {
        moreBtn.hidden = true;
      }
    }
  }

  /** Hand a finished tool's result to the body and paint it. */
  function setToolBodyContent(block, content) {
    if (!block) return;
    toolFullContent.set(block, String(content == null ? "" : content));
    renderToolBody(block);
  }

  /**
   * Copy the whole result — the original bytes, not the prettified view,
   * because what gets pasted into a shell or an issue has to be what the
   * tool actually returned.
   */
  function copyToolBody(block, btn) {
    const full = toolFullContent.get(block) || "";
    const done = (ok) => {
      if (!btn) return;
      btn.textContent = i18n(ok ? "tool.body.copied" : "tool.body.copy_failed");
      setTimeout(() => {
        btn.textContent = i18n("tool.body.copy");
      }, 1400);
    };
    try {
      navigator.clipboard.writeText(full).then(
        () => done(true),
        () => done(false)
      );
    } catch {
      done(false);
    }
  }

  /** Append streamed exec output to what the body already holds. */
  function appendToolBodyContent(block, chunk) {
    if (!block) return;
    toolFullContent.set(block, (toolFullContent.get(block) || "") + String(chunk || ""));
    // Live output is watched, not skimmed: keep it whole as it arrives.
    block.dataset.bodyExpanded = "1";
    renderToolBody(block);
  }

  function updateToolHead(block, name, argsRaw, content, running) {
    const head = block.querySelector(".tool-head");
    if (!head) return;
    // Durations exist only for tools this session actually watched run.
    // Restored history has none — the session snapshot does not persist
    // them — and replaying a start/complete pair would time the replay, not
    // the tool, printing "0ms" beside every historical call.
    const durEl = head.querySelector(".tool-dur");
    if (durEl) {
      const ms = Number(block.dataset.durationMs);
      durEl.textContent = block.dataset.durationMs ? formatToolDuration(ms) : "";
    }
    const icon = head.querySelector(".tool-icon");
    const label = head.querySelector(".tool-label");
    const sub = head.querySelector(".tool-sub");
    const statsEl = head.querySelector(".tool-stats");
    const filePath = toolPathFromArgs(name, argsRaw, content);
    if (filePath) {
      block.dataset.filePath = filePath;
    }
    // The icon states WHICH tool ran and never stops doing so. It used to be
    // replaced by a check mark on completion, which left every finished step
    // — the overwhelming majority of what is on screen — with no mark of its
    // own kind at all. Success needs no badge once the spinner is gone;
    // failure does, and gets one.
    if (icon && icon.dataset.iconFor !== name) {
      icon.innerHTML = toolIconMarkup(name);
      icon.dataset.iconFor = name || "";
    }
    if (!running) {
      block.classList.toggle("tool-failed", toolResultIsError(content));
    }
    if (label) label.textContent = toolPreviewLine(name, argsRaw, content);
    if (sub) {
      sub.textContent = filePath && filePath.includes("/") ? filePath : "";
    }
    if (statsEl && filePath && toolKind(name) === "write") {
      const diff = findDiffForPath(filePath);
      if (diff) {
        statsEl.innerHTML = statsHtml(countDiffStats(diff.before, diff.after));
      }
    }
  }

  function insertFileChangeCard(path, content, parent) {
    const host = parent || messagesEl;
    if (!host || !path) return;
    if (parent && parent.classList.contains("tool-block")) {
      attachToolDiffShell(parent, path);
      return;
    }
    const diff = findDiffForPath(path);
    if (diff && (diff.before || diff.after)) {
      void attachInlineToolDiff(parent || document.createElement("div"), path, diff.before || "", diff.after || "");
      return;
    }
    attachToolDiffShell(parent || document.createElement("div"), path);
  }

  function renderSubagents() {
    if (!subagentsBar || !subagentsTree) return;
    const active = subagents.filter((s) => s.status === "running" || s.status === "waiting");
    if (!subagents.length) {
      subagentsBar.classList.add("hidden");
      subagentsTree.innerHTML = "";
      return;
    }
    subagentsBar.classList.remove("hidden");
    subagentsTree.innerHTML = "";
    subagents.slice(-12).forEach((sa) => {
      const row = document.createElement("div");
      row.className = "subagent-row status-" + sa.status;
      const icon = sa.status === "done" ? "✓" : sa.status === "error" ? "✗" : sa.status === "waiting" ? "⏳" : "◉";
      const toolHint = sa.toolCount ? ` · ${sa.toolCount} tools` : "";
      const tierL = subagentTierLabel(sa.tier);
      const tierHtml = tierL
        ? `<span class="subagent-tier subagent-tier-${tierL.toLowerCase()}" title="${escapeAttr(sa.model || "")}">${tierL}</span>`
        : "";
      row.innerHTML =
        `<span class="subagent-icon">${icon}</span>` +
        `<span class="subagent-type">${escapeAttr(sa.type || "agent")}</span>` +
        tierHtml +
        `<span class="subagent-label">${escapeAttr(sa.label || sa.taskId || sa.id)}${toolHint}</span>`;
      if (sa.taskId) {
        row.dataset.taskId = sa.taskId;
        row.title = sa.model ? `${sa.taskId} · ${sa.model}` : sa.taskId;
      }
      if (sa.status === "error" && sa.error) {
        row.title = row.title ? `${row.title}\n${sa.error}` : sa.error;
      }
      const promoteBits = [];
      if (sa.lessonPromote) promoteBits.push("lesson↑");
      if (sa.playbookPromote) promoteBits.push("playbook↑");
      if (promoteBits.length) {
        row.innerHTML +=
          `<span class="subagent-promote-badge" title="${escapeAttr(
            (sa.lessonPromote || sa.playbookPromote || "").slice(0, 240)
          )}">${promoteBits.join(" · ")}</span>`;
      }
      subagentsTree.appendChild(row);
      if (sa.status === "error" && sa.error) {
        const errHint = document.createElement("div");
        errHint.className = "subagent-nested-hint subagent-error-hint";
        const short = String(sa.error);
        errHint.textContent = short.length > 120 ? short.slice(0, 117) + "…" : short;
        errHint.title = String(sa.error);
        subagentsTree.appendChild(errHint);
      }
      const node = sa.taskId ? subagentByTask.get(sa.taskId) : null;
      if (node?.toolsEl && node.toolCount > 0) {
        const nest = document.createElement("div");
        nest.className = "subagent-nested-hint";
        nest.textContent = `${node.toolCount} tool step${node.toolCount === 1 ? "" : "s"} (in chat)`;
        subagentsTree.appendChild(nest);
      }
    });
    if (subagents.length > 12) {
      const more = document.createElement("div");
      more.className = "subagent-more";
      more.textContent = `+${subagents.length - 12} more`;
      subagentsTree.appendChild(more);
    }
  }

  function trackSubagent(msg, phase, argsRaw) {
    const name = msg.toolName || "";
    if (!["task", "task_spawn", "task_wait", "task_cancel"].includes(name)) {
      return;
    }
    const id = msg.toolCallId || `${name}-${msg.step ?? subagents.length}`;
    if (phase === "start") {
      subagents.push({
        id,
        type: "agent",
        label: toolDisplayName(name),
        status: name === "task_wait" ? "waiting" : "running",
      });
    } else if (phase === "update" || phase === "complete") {
      const sa = subagents.find((s) => s.id === id);
      if (!sa) return;
      const args = parseToolArgs(argsRaw || "");
      const st = args.subagent_type || args.subagentType || sa.type;
      if (st) sa.type = String(st);
      if (args.tier) sa.tier = String(args.tier);
      if (args.model) sa.model = String(args.model);
      let desc = humanizeTaskLabel(args.description || args.prompt || args.goal || "");
      if (!desc && Array.isArray(args.workorders) && args.workorders.length) {
        const first = args.workorders[0] || {};
        const wo = first.intent || first.title || first.goal || first.description || "";
        desc = wo
          ? args.workorders.length > 1
            ? `${wo} (+${args.workorders.length - 1})`
            : String(wo)
          : `${args.workorders.length} workorders`;
      }
      if (desc) {
        const d = String(desc).trim();
        sa.label = d.length > 48 ? d.slice(0, 45) + "…" : d;
      }
      if (phase === "complete" && !sa.taskId) {
        // Rows adopted by a child (taskId set) get their status from
        // childLifecycle events; a completed spawn call must not override it.
        const err = (msg.content || "").toLowerCase().startsWith("error");
        sa.status = err ? "error" : "done";
      }
    }
    renderSubagents();
  }

  function renderWorkflowStages() {
    if (!workflowStagesEl) return;
    workflowStagesEl.innerHTML = "";
    for (const st of workflowStages.values()) {
      const pill = document.createElement("span");
      pill.className = "workflow-pill state-" + st.state;
      const glyph = st.state === "done" ? "✓" : st.state === "running" ? "⋯" : "○";
      pill.textContent = `${glyph} ${st.name || st.id}`;
      pill.title = st.id;
      workflowStagesEl.appendChild(pill);
    }
  }

  function setWorkflow(label, active) {
    if (!workflowBar || !workflowLabel) return;
    if (!active) {
      workflowBar.classList.add("hidden");
      workflowLabel.textContent = "";
      workflowActiveName = "";
      workflowStages.clear();
      renderWorkflowStages();
      return;
    }
    workflowBar.classList.remove("hidden");
    workflowLabel.textContent = label;
  }

  function formatTok(n) {
    const v = Math.max(0, Math.round(n || 0));
    if (v >= 1_000_000) return (v / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
    if (v >= 1000) return (v / 1000).toFixed(1).replace(/\.0$/, "") + "K";
    return String(v);
  }

  function resetContextUsage() {
    ctxState.prompt = 0;
    ctxState.completion = 0;
    ctxState.estimated = false;
    ctxState.breakdown = [];
    renderContextUi();
  }

  /** Category colors for the context breakdown (Cursor-like palette). */
  const CTX_COLORS = {
    system: "#8b8b8b", // grey
    tools: "#a78bfa", // purple
    rules: "#34d399", // green
    skills: "#fbbf24", // orange
    conversation: "#f472b6", // pink
    completion: "#60a5fa", // blue
    reserved: "#4a4a4a", // dark grey
  };

  /**
   * Rows for the context popover. Uses the agent's per-category breakdown when
   * available; the conversation row absorbs the difference between the real
   * prompt total and the fixed categories so the numbers always add up.
   */
  function contextRowsData() {
    const used = ctxState.prompt;
    const bd = Array.isArray(ctxState.breakdown) ? ctxState.breakdown : [];
    const rows = [];
    if (bd.length > 0) {
      let fixedSum = 0;
      let convRow = null;
      bd.forEach((c) => {
        if (c.key === "conversation") {
          convRow = { key: c.key, label: c.label || "Conversation", tokens: c.tokens };
        } else {
          fixedSum += c.tokens;
          rows.push({ key: c.key, label: c.label || c.key, tokens: c.tokens });
        }
      });
      let conv = convRow ? convRow.tokens : 0;
      if (used > 0) {
        conv = Math.max(conv, used - fixedSum);
      }
      rows.push({ key: "conversation", label: i18n("ctx.row.conversation"), tokens: Math.max(0, conv) });
    } else if (used > 0) {
      rows.push({ key: "conversation", label: i18n("ctx.row.prompt"), tokens: used });
    }
    if (ctxState.completion > 0) {
      rows.push({ key: "completion", label: i18n("ctx.row.completion"), tokens: ctxState.completion });
    }
    return rows;
  }

  function renderContextUi() {
    const used = ctxState.prompt;
    const limit = ctxState.limit || 128000;
    const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
    if (contextRingFill) {
      contextRingFill.style.setProperty("--ctx-pct", String(pct));
    }
    if (ctxPct) {
      ctxPct.textContent = used > 0 ? `${pct}% full` : "—";
    }
    if (ctxSummary) {
      const est = ctxState.estimated ? "~" : "";
      ctxSummary.textContent =
        used > 0 ? `${est}${formatTok(used)} / ${formatTok(limit)} tokens` : `Up to ${formatTok(limit)} tokens`;
    }
    const rows = contextRowsData();
    if (ctxBar) {
      // The bar spans the whole context window: colored segments for each
      // category, a dark segment for the reserved reply budget, the rest of
      // the track is free space.
      ctxBar.innerHTML = "";
      const total = Math.max(limit, 1);
      const segs = rows.slice();
      if (ctxState.maxResponse > 0) {
        segs.push({ key: "reserved", label: i18n("ctx.row.reserved"), tokens: ctxState.maxResponse });
      }
      segs.forEach((r) => {
        if (r.tokens <= 0) return;
        const seg = document.createElement("div");
        seg.className = "ctx-bar-seg";
        seg.style.width = `${Math.min(100, Math.max(0.8, (r.tokens / total) * 100))}%`;
        seg.style.background = CTX_COLORS[r.key] || "#888888";
        seg.title = `${r.label}: ${formatTok(r.tokens)}`;
        ctxBar.appendChild(seg);
      });
    }
    if (ctxRows) {
      ctxRows.innerHTML = "";
      const items = rows.slice();
      items.push({ key: "reserved", label: i18n("ctx.row.reserved"), tokens: ctxState.maxResponse });
      items.forEach((item) => {
        if (item.tokens <= 0 && item.key !== "reserved") return;
        const row = document.createElement("div");
        row.className = "ctx-row";
        row.innerHTML = `<span class="ctx-swatch"></span><span class="ctx-row-label">${item.label}</span><span class="ctx-row-val">${formatTok(item.tokens)}</span>`;
        // The colour goes on through the CSSOM, never a style attribute in the
        // markup: both hosts serve this page under a CSP without
        // 'unsafe-inline', which drops one and left every swatch unpainted.
        const swatch = row.querySelector(".ctx-swatch");
        if (swatch) {
          swatch.style.background = CTX_COLORS[item.key] || "#888888";
        }
        ctxRows.appendChild(row);
      });
    }
  }

  function showContextPopover(show) {
    ctxPopoverOpen = show;
    contextPopover?.classList.toggle("hidden", !show);
    contextBtn?.classList.toggle("open", show);
  }

  function updateStatusFooter() {
    /* tokens shown in context popover */
  }

  function setLspStatus(st) {
    if (!statusLsp) return;
    const label = (st || "").trim();
    statusLsp.textContent = label ? `LSP ${label}` : "";
    statusLsp.classList.toggle("active", label === "active");
  }

  function imageGalleryFromList(fileList, activePath) {
    if (!Array.isArray(fileList)) return [];
    return fileList
      .filter((f) => f && f.kind === "image")
      .map((f) => ({
        name: f.name || "Image",
        path: f.path,
        previewUri: f.previewUri,
      }));
  }

  function renderImagePreviewAt(index) {
    if (!imagePreview || !imagePreviewImg || !imagePreviewState.items.length) return;
    const total = imagePreviewState.items.length;
    const idx = ((index % total) + total) % total;
    imagePreviewState.index = idx;
    const item = imagePreviewState.items[idx];
    if (imagePreviewTitle) {
      imagePreviewTitle.textContent = item.name || "Image";
    }
    if (imagePreviewCounter) {
      imagePreviewCounter.textContent = total > 1 ? `${idx + 1} / ${total}` : "";
    }
    if (imagePreviewPrevBtn) {
      imagePreviewPrevBtn.classList.toggle("hidden", total <= 1);
    }
    if (imagePreviewNextBtn) {
      imagePreviewNextBtn.classList.toggle("hidden", total <= 1);
    }
    imagePreviewImg.src = item.previewUri || "";
    imagePreviewImg.alt = item.name || "Preview";
    imagePreview.classList.remove("hidden");
  }

  function showImagePreview(name, previewUri, filePath, galleryItems) {
    if (!imagePreview || !imagePreviewImg) return;
    const items = Array.isArray(galleryItems) && galleryItems.length
      ? galleryItems.filter((g) => g && g.previewUri)
      : [{ name: name || "Image", path: filePath || "", previewUri: previewUri || "" }];
    imagePreviewState.items = items;
    const startIdx = Math.max(
      0,
      items.findIndex((g) => g.path && filePath && g.path === filePath)
    );
    renderImagePreviewAt(startIdx >= 0 ? startIdx : 0);
  }

  function hideImagePreview() {
    imagePreview?.classList.add("hidden");
    if (imagePreviewImg) {
      imagePreviewImg.src = "";
    }
    imagePreviewState = { items: [], index: 0 };
  }

  function shiftImagePreview(delta) {
    if (!imagePreviewState.items.length) return;
    renderImagePreviewAt(imagePreviewState.index + delta);
  }

  async function readFileAsBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === "string" ? reader.result : "";
        const comma = result.indexOf(",");
        resolve(comma >= 0 ? result.slice(comma + 1) : result);
      };
      reader.onerror = () => reject(reader.error || new Error("read failed"));
      reader.readAsDataURL(file);
    });
  }

  async function attachFilesFromDataTransfer(dt) {
    if (!dt) return;
    const fileItems = [];
    if (dt.files && dt.files.length) {
      for (let i = 0; i < dt.files.length; i++) {
        fileItems.push(dt.files[i]);
      }
    }
    for (const file of fileItems) {
      if (!file) continue;
      if (file.size > MAX_ATTACH_BYTES) {
        appendMsg("system", `Skipped ${file.name || "file"}: exceeds 20 MB limit`);
        continue;
      }
      try {
        const dataBase64 = await readFileAsBase64(file);
        host.postMessage({
          type: "attachBytes",
          name: file.name || "attachment",
          mime: file.type || undefined,
          dataBase64,
        });
      } catch {
        /* ignore single file failure */
      }
    }
  }

  function onAttachmentClick(f, _isImage, e) {
    if (!f?.path) return;
    openExternalFile(f.path, Boolean(e?.shiftKey));
  }

  function renderMsgAttachments(parent, fileList) {
    if (!parent || !fileList || !fileList.length) return;
    const wrap = document.createElement("div");
    wrap.className = "msg-attachments";
    fileList.forEach((f) => {
      if (!f || typeof f.name !== "string") return;
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "msg-attach" + (isImage ? " msg-attach-image" : "");
      card.title = (f.path || f.name) + " · click: open in editor · Shift+click: focus";

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "msg-attach-thumb";
        img.src = f.previewUri || "";
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "msg-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "msg-attach-meta";
      const name = document.createElement("span");
      name.className = "msg-attach-name";
      name.textContent = f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        onAttachmentClick(f, isImage, e);
      });
      wrap.appendChild(card);
    });
    parent.appendChild(wrap);
  }

  /** @param {string} text @param {{ reasoning?: string; toolBlocks?: any[] }} opts */
  function appendHistoryAssistantTurn(text, opts) {
    if (!messagesEl) {
      return;
    }
    resetTurnState();
    assistantTurn = document.createElement("div");
    assistantTurn.className = "msg assistant-turn";
    assistantTurnInner = document.createElement("div");
    assistantTurnInner.className = "assistant-turn-inner";
    assistantTurn.appendChild(assistantTurnInner);
    messagesEl.appendChild(assistantTurn);

    const reasoning = (opts?.reasoning || "").trim();
    if (reasoning) {
      reasoningDetails = document.createElement("details");
      reasoningDetails.className = "reasoning-trace trace-details";
      const sum = document.createElement("summary");
      sum.className = "trace-summary";
      sum.textContent = i18n("reason.brief");
      reasoningBody = document.createElement("pre");
      reasoningBody.className = "trace-body reasoning-body";
      reasoningBody.textContent = reasoning;
      reasoningDetails.appendChild(sum);
      reasoningDetails.appendChild(reasoningBody);
      assistantTurnInner.appendChild(reasoningDetails);
      reasoningDetails.open = false;
    }

    const tools = Array.isArray(opts?.toolBlocks) ? opts.toolBlocks : [];
    for (const tb of tools) {
      const id = tb.id || `${tb.name}-${toolBlocks.size}`;
      handleToolBlock({ phase: "start", toolCallId: id, toolName: tb.name || "tool", restored: true });
      if (tb.argsRaw) {
        handleToolBlock({
          phase: "update",
          toolCallId: id,
          toolName: tb.name || "tool",
          argsDelta: tb.argsRaw,
          restored: true,
        });
      }
      handleToolBlock({
        phase: "complete",
        toolCallId: id,
        toolName: tb.name || "tool",
        content: tb.result || "",
        diagnostics: tb.diagnostics,
        durationMs: tb.durationMs,
        restored: true,
      });
      if (toolKind(tb.name) === "write" && (tb.diffBefore !== undefined || tb.diffAfter !== undefined)) {
        const block = toolBlocks.get(id);
        const fp = toolPathFromArgs(tb.name || "", tb.argsRaw || "", tb.result || "");
        if (block && fp) {
          rememberBlockDiff(block, tb.diffBefore || "", tb.diffAfter || "");
          void attachInlineToolDiff(block, fp, tb.diffBefore || "", tb.diffAfter || "");
        }
      }
    }

    if (text) {
      assistantBubble = document.createElement("div");
      applyAssistantMarkdown(assistantBubble, text);
      assistantTurnInner.appendChild(assistantBubble);
    }

    if (toolTraceEl) {
      toolTraceEl.open = false;
    }
    resetTurnState();
    void syncToolDiffPreviews();
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  /** @param {string} role @param {string} text @param {{ uiIndex?: number; files?: any[]; reasoning?: string; toolBlocks?: any[] }} [opts] */
  function appendMsg(role, text, opts) {
    if (!messagesEl) {
      return null;
    }
    const el = document.createElement("div");
    el.className = `msg ${role}`;
    if (role === "user") {
      if (typeof opts?.uiIndex === "number") {
        el.dataset.uiIndex = String(opts.uiIndex);
      }
      if (opts?.files?.length) {
        renderMsgAttachments(el, opts.files);
      }
      const wrap = document.createElement("div");
      wrap.className = "user-wrap";
      const body = document.createElement("div");
      body.className = "user-text";
      body.textContent = text;
      wrap.appendChild(body);
      if (typeof opts?.uiIndex === "number") {
        const rewind = document.createElement("button");
        rewind.type = "button";
        rewind.className = "rewind-btn";
        rewind.title = i18n("msg.rewind_title");
        rewind.textContent = i18n("msg.rewind");
        rewind.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          const idx = typeof opts.uiIndex === "number" ? opts.uiIndex : Number(el.dataset.uiIndex);
          if (!Number.isFinite(idx) || idx < 0) {
            return;
          }
          rewind.disabled = true;
          host.postMessage({ type: "rewindToMessage", uiIndex: idx });
        });
        wrap.appendChild(rewind);

        // Rewind truncates this chat; branching keeps it and continues the
        // conversation in a copy, which is what you want when the question is
        // "what if I had asked differently" rather than "undo that".
        //
        // Not on the first message: the branch is everything BEFORE the point
        // (sessionfile.ForkSnapshot), so branching there would produce an
        // empty chat — the core rejects it, and a button that can only fail is
        // worse than no button. Starting over from nothing is /clear.
        if (opts.uiIndex > 0) {
          const fork = document.createElement("button");
          fork.type = "button";
          fork.className = "fork-btn";
          fork.title = i18n("msg.branch_title");
          fork.textContent = i18n("msg.branch");
          fork.addEventListener("click", (e) => {
            e.preventDefault();
            e.stopPropagation();
            const idx = typeof opts.uiIndex === "number" ? opts.uiIndex : Number(el.dataset.uiIndex);
            if (!Number.isFinite(idx) || idx < 1) {
              return;
            }
            fork.disabled = true;
            host.postMessage({ type: "forkFromMessage", uiIndex: idx });
          });
          wrap.appendChild(fork);
        }
      }
      if (text || wrap.querySelector(".rewind-btn")) {
        el.appendChild(wrap);
      }
    } else if (role === "system") {
      el.className = "msg system";
      el.textContent = text;
    } else if (role === "assistant" && (opts?.toolBlocks?.length || opts?.reasoning)) {
      appendHistoryAssistantTurn(text, opts);
      return null;
    } else if (role === "assistant") {
      applyAssistantMarkdown(el, text);
    } else {
      el.textContent = text;
    }
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  /** @param {{ phase: string; toolCallId?: string; toolName: string; content?: string; argsDelta?: string; step?: number; diagnostics?: any[]; restored?: boolean; durationMs?: number }} msg */
  function handleToolBlock(msg) {
    if (!messagesEl) return;
    const id = toolBlockKey(msg);
    const kind = toolKind(msg.toolName);
    const host = messagesHostFor(msg);

    if (msg.phase === "start") {
      if (msg.scope !== "child") {
        commitPreToolText();
        bumpToolTraceCount(msg.toolName);
      } else {
        assistantBubble = null;
      }
      toolArgs.set(id, "");
      const block = document.createElement("div");
      block.className = `tool-block running kind-${kind}` + (msg.scope === "child" ? " child-tool" : "");
      block.dataset.toolId = id;
      // Restored history replays start/complete back to back, so timing it
      // would measure the replay. Only live tools carry a start stamp.
      if (!msg.restored) {
        block.dataset.startedAt = String(Date.now());
        noteTurnToolStart();
      }
      if (msg.taskId) block.dataset.taskId = msg.taskId;
      if (typeof msg.step === "number") {
        block.dataset.step = String(msg.step);
        if (msg.scope !== "child") {
          execSteps.set(msg.step, block);
        }
      }
      if (kind === "write" && msg.scope !== "child") {
        block.classList.add("write-card-only");
        (host || messagesEl).appendChild(block);
        toolBlocks.set(id, block);
        if (msg.scope === "child" && msg.taskId) {
          const node = subagentByTask.get(msg.taskId);
          if (node) {
            node.toolCount = (node.toolCount || 0) + 1;
            renderSubagents();
          }
        } else {
          trackSubagent(msg, "start", "");
        }
        // A restored turn is a replay, not work in progress: narrating it
        // leaves "Ls…" in the chrome strip of a session that is sitting idle.
        if (!msg.restored) {
          setChromeHint(toolDisplayName(msg.toolName) + "…", false);
        }
        if (host) host.scrollTop = host.scrollHeight;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        return;
      }
      const head = document.createElement("button");
      head.type = "button";
      head.className = "tool-head";
      head.innerHTML =
        `<span class="tool-icon" data-icon-for="${escapeAttr(msg.toolName || "")}">${toolIconMarkup(msg.toolName)}</span>` +
        `<span class="tool-label">${escapeAttr(toolDisplayName(msg.toolName))}</span>` +
        `<span class="tool-sub"></span>` +
        `<span class="tool-dur"></span>` +
        `<span class="tool-stats"></span>` +
        `<span class="tool-spinner"></span>` +
        (kind === "write" ? "" : `<span class="tool-chev">${orchIconMarkup("chevron-down", { size: "sm" })}</span>`);
      let body = null;
      if (kind !== "write") {
        // A wrapper, not the <pre> itself: the result needs a strip of its
        // own (how much there is, how to read it, how to copy it) and a
        // footer that unfolds the rest. The <pre> is one child of it.
        body = document.createElement("div");
        body.className = "tool-body hidden";
        body.innerHTML =
          `<div class="tool-body-bar">` +
          `<span class="tool-body-meta"></span>` +
          `<span class="tool-body-acts">` +
          `<button type="button" class="tool-body-btn" data-body-action="format" hidden></button>` +
          `<button type="button" class="tool-body-btn" data-body-action="copy" data-i18n="tool.body.copy">${escapeAttr(i18n("tool.body.copy"))}</button>` +
          `</span></div>` +
          `<pre class="tool-body-pre"></pre>` +
          `<button type="button" class="tool-body-more" data-body-action="expand" hidden></button>`;
        head.addEventListener("click", (e) => {
          const stats = e.target.closest?.(".tool-stats");
          const fp = block.dataset.filePath || "";
          if (stats && fp && stats.textContent.trim()) {
            e.preventDefault();
            e.stopPropagation();
            const d = findDiffForPath(fp);
            openDiffMessage(fp, d?.before || "", d?.after || "", e.shiftKey);
            return;
          }
          body.classList.toggle("hidden");
          head.classList.toggle("open");
        });
        // The body's own controls. Delegated from the wrapper so the three
        // buttons need no separate bookkeeping, and stopped here so a click
        // inside the result never reaches the head and folds it shut.
        body.addEventListener("click", (e) => {
          const btn = e.target.closest?.("[data-body-action]");
          if (!btn) return;
          e.preventDefault();
          e.stopPropagation();
          const action = btn.dataset.bodyAction;
          if (action === "format") {
            block.dataset.bodyFormat = block.dataset.bodyFormat === "raw" ? "pretty" : "raw";
          } else if (action === "expand") {
            block.dataset.bodyExpanded = block.dataset.bodyExpanded === "1" ? "0" : "1";
          } else if (action === "copy") {
            copyToolBody(block, btn);
            return;
          }
          renderToolBody(block);
        });
      } else {
        bindWriteToolHead(block, head);
      }
      block.appendChild(head);
      if (body) block.appendChild(body);
      (host || messagesEl).appendChild(block);
      toolBlocks.set(id, block);
      if (msg.scope === "child" && msg.taskId) {
        const node = subagentByTask.get(msg.taskId);
        if (node) {
          node.toolCount = (node.toolCount || 0) + 1;
          renderSubagents();
        }
      } else {
        trackSubagent(msg, "start", "");
      }
      // A restored turn is a replay, not work in progress: narrating it
        // leaves "Ls…" in the chrome strip of a session that is sitting idle.
        if (!msg.restored) {
          setChromeHint(toolDisplayName(msg.toolName) + "…", false);
        }
      if (host) host.scrollTop = host.scrollHeight;
      messagesEl.scrollTop = messagesEl.scrollHeight;
      return;
    }

    const block = toolBlocks.get(id);
    if (!block) return;

    if (msg.phase === "update" && msg.argsDelta) {
      const prev = toolArgs.get(id) || "";
      const next = prev + msg.argsDelta;
      toolArgs.set(id, next);
      updateToolHead(block, msg.toolName, next, "", true);
      if (toolKind(msg.toolName) === "write") {
        tryShowWriteDiff(block, msg.toolName, next, "");
      }
      if (msg.scope !== "child") {
        trackSubagent(msg, "update", next);
      }
      syncToolDiffStats();
      return;
    }

    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    const argsRaw = toolArgs.get(id) || "";

    if (msg.phase === "complete") {
      block.classList.remove("running");
      block.classList.add("done", `kind-${kind}`);
      const spinner = block.querySelector(".tool-spinner");
      if (spinner) spinner.remove();
      if (block.dataset.startedAt) {
        block.dataset.durationMs = String(Date.now() - Number(block.dataset.startedAt));
        noteTurnToolEnd();
      } else if (typeof msg.durationMs === "number" && msg.durationMs > 0) {
        // Restored from the session snapshot: the tool was timed when it ran,
        // by whichever surface ran it (sessionfile.UIToolBlock.duration_ms).
        block.dataset.durationMs = String(msg.durationMs);
      }
      updateToolHead(block, msg.toolName, argsRaw, msg.content || "", false);
      if (head && kind !== "write") head.classList.remove("open");

      if (body && msg.content && kind !== "write") {
        // The whole result, not a slice of it. It used to be cut at 8000
        // characters with an ellipsis and no way back to the rest; the
        // body now folds instead, and unfolds on request.
        setToolBodyContent(block, msg.content);
        body.classList.add("hidden");
      }

      if (msg.diagnostics && msg.diagnostics.length) {
        const diag = document.createElement("div");
        diag.className = "tool-diags";
        msg.diagnostics.slice(0, 6).forEach((d) => {
          const line = document.createElement("div");
          line.className = "tool-diag " + (d.severity || "");
          line.textContent = `L${d.start_line}: ${d.message}`;
          diag.appendChild(line);
        });
        block.appendChild(diag);
      }

      if (kind === "write") {
        tryShowWriteDiff(block, msg.toolName, argsRaw, msg.content || "");
      }

      if (msg.scope !== "child") {
        trackSubagent(msg, "complete", argsRaw);
      }
      syncToolDiffStats();
      toolArgs.delete(id);
      assistantBubble = null;
      if (busy) {
        setChromeHint("", false);
      }
    }
    if (host) host.scrollTop = host.scrollHeight;
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function appendExecChunk(step, chunk) {
    const block = execSteps.get(step);
    if (!block) return;
    const body = block.querySelector(".tool-body");
    const head = block.querySelector(".tool-head");
    if (!body) return;
    body.classList.remove("hidden");
    if (head) head.classList.add("open");
    appendToolBodyContent(block, chunk);
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
  }
  // ---- Trajectory view ------------------------------------------------------
  //
  // The tab is a pure function from a session's event log to a tree of rows,
  // drawn by a thin renderer below. The log is what session.trajectory returns
  // and what the core's tee records: `{ seq, time_ms, type, data }` where
  // `type` is the notification method and `data` its params. A live event
  // forwarded mid-turn has the same shape minus seq and time_ms.

  /**
   * @typedef {{
   *   kind: "turn"|"step"|"tool"|"text"|"reasoning"|"error"|"stage"|"pending"|"route"|"other",
   *   depth: number,
   *   key: string,
   *   label: string,
   *   turnId: string,
   *   step?: number,
   *   startMs?: number,
   *   offsetMs?: number,
   *   durationMs?: number,
   *   tokensIn?: number,
   *   tokensOut?: number,
   *   outcome?: string,
   *   input?: string,
   *   output?: string,
   *   live: boolean,
   *   seq?: number
   * }} TrajRow
   */

  /**
   * buildTrajectoryTree turns recorded (or live) events into flat rows with a
   * depth. Pure: no DOM, no fragment state, no clock. Everything it knows
   * about time comes from the events' own core-stamped `time_ms`; a live
   * event has none, and its row says so rather than borrowing the client's.
   *
   * Grouping: turn (by data.turn_id, in order of first appearance) > step (by
   * data.step) > items. Tool calls are keyed by tool_call_id; a child-scoped
   * tool call nests under its parent_tool_call_id. Consecutive text deltas of
   * one kind coalesce into one row. Workflow stages, mode routes and steps sit
   * directly under the turn, in the order they first appeared — which for a recorded log is time order, and needs no timestamp so it holds for live rows too. Token columns come from step_usage only.
   *
   * Self-contained on purpose: trajectory-test.mjs evaluates it standalone.
   *
   * @param {Array<{seq?: number, time_ms?: number, type: string, data?: any}>|null|undefined} events
   * @returns {TrajRow[]}
   */
  function buildTrajectoryTree(events) {
    /** @type {TrajRow[]} */
    const rows = [];
    if (!Array.isArray(events)) return rows;

    const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const str = (v) => (typeof v === "string" ? v : "");
    const obj = (v) => (v && typeof v === "object" ? v : {});

    // Turns in order of first appearance. Events without a turn_id (older
    // cores, one-shot agent.run) share one unnamed turn so nothing is dropped.
    const turns = new Map();
    let turnOrdinal = 0;
    const turnFor = (id) => {
      let t = turns.get(id);
      if (!t) {
        turnOrdinal++;
        t = {
          key: "turn:" + id,
          id,
          ordinal: turnOrdinal,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: "open",
          steps: new Map(), // step number -> step node, for lookup
          depth1: [], // steps, stages and routes in the order they first appeared — which is time order
        };
        turns.set(id, t);
      }
      return t;
    };
    const stepFor = (t, n) => {
      const key = String(n);
      let s = t.steps.get(key);
      if (!s) {
        s = {
          kind: "step",
          key: t.key + "/step:" + key,
          n,
          startMs: undefined,
          endMs: undefined,
          live: false,
          outcome: undefined,
          tokensIn: undefined,
          tokensOut: undefined,
          items: [], // rows under the step, in order
          tools: new Map(), // tool_call_id -> tool node (parent and child scope alike)
          openTool: null, // last parent-scope tool started and not yet completed
          lastText: null, // for coalescing message_delta / reasoning_delta
        };
        t.steps.set(key, s);
        t.depth1.push(s);
      }
      return s;
    };
    // Widen a node's [startMs, endMs] to include ms, and mark it live if ms is unknown.
    const touch = (node, ms, live) => {
      if (live) node.live = true;
      if (ms === undefined) return;
      if (node.startMs === undefined || ms < node.startMs) node.startMs = ms;
      if (node.endMs === undefined || ms > node.endMs) node.endMs = ms;
    };

    for (const e of events) {
      if (!e || typeof e !== "object") continue;
      const method = str(e.type);
      const d = obj(e.data);
      const ms = num(e.time_ms);
      const live = ms === undefined;
      const seq = num(e.seq);
      const t = turnFor(str(d.turn_id));
      touch(t, ms, live);

      // The turn's own boundary. It exists so a turn that emitted nothing at
      // all is still a turn, and so the turn's duration is the core's
      // measurement rather than the span of whatever events bracketed it. It
      // sets the turn's edges and adds no row of its own — turnFor/touch above
      // have already taken the timestamp.
      if (method === "turn/start" || method === "turn/end") {
        if (method === "turn/end") {
          const d2 = num(d.duration_ms);
          if (d2 !== undefined) t.durationMs = d2;
          if (t.outcome === "open") t.outcome = "done";
        }
        continue;
      }

      if (method === "workflow/stage_start" || method === "workflow/stage_done") {
        const stageKey = t.key + "/stage:" + str(d.stage_id) + "#" + (num(d.attempt) ?? 0);
        let st = t.depth1.find((c) => c.kind === "stage" && c.key === stageKey);
        if (!st) {
          st = {
            kind: "stage",
            key: stageKey,
            label: str(d.stage_id) || str(d.name) || "stage",
            startMs: undefined,
            endMs: undefined,
            live: false,
            outcome: undefined,
            seq,
          };
          t.depth1.push(st);
        }
        touch(st, ms, live);
        if (method === "workflow/stage_done") st.outcome = str(d.marker) || str(d.action) || "done";
        continue;
      }

      const s = stepFor(t, num(d.step) ?? 0);
      touch(s, ms, live);

      if (method === "exec/output_chunk") {
        const target = s.openTool;
        if (target) {
          target.output += str(d.chunk);
          touch(target, ms, live);
        } else {
          s.items.push({ kind: "other", key: s.key + "/exec:" + s.items.length, label: i18n("traj.shell_output"), startMs: ms, endMs: ms, live, output: str(d.chunk), seq });
        }
        s.lastText = null;
        continue;
      }

      if (method !== "agent/event") {
        // A notification this view has never heard of stays visible as a
        // plain row: the log outlives the code that reads it.
        s.items.push({ kind: "other", key: s.key + "/" + method + ":" + s.items.length, label: method || "event", startMs: ms, endMs: ms, live, seq });
        s.lastText = null;
        continue;
      }

      const kind = str(d.type);
      const isChild = d.scope === "child";
      const parentToolId = str(d.parent_tool_call_id);

      switch (kind) {
        case "message_delta":
        case "reasoning_delta": {
          if (isChild) break; // a subagent's text belongs to its own trace, not this step's
          const k = kind === "reasoning_delta" ? "reasoning" : "text";
          if (s.lastText && s.lastText.kind === k) {
            s.lastText.output += str(d.content);
            touch(s.lastText, ms, live);
          } else {
            const row = { kind: k, key: s.key + "/" + k + ":" + s.items.length, label: k === "reasoning" ? "reasoning" : "response", startMs: ms, endMs: ms, live, output: str(d.content), seq };
            s.items.push(row);
            s.lastText = row;
          }
          break;
        }
        case "tool_call_start": {
          const id = str(d.tool_call_id) || "idx" + (num(d.tool_call_index) ?? s.items.length);
          const row = { kind: "tool", key: s.key + "/tool:" + id, id, label: str(d.tool_call_name) || "tool", startMs: ms, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
          const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
          if (parent) parent.subrows.push(row);
          else s.items.push(row);
          s.tools.set(id, row);
          if (!isChild) s.openTool = row;
          s.lastText = null;
          break;
        }
        case "tool_call_delta": {
          const row = s.tools.get(str(d.tool_call_id));
          if (row) {
            row.input += str(d.args_delta);
            touch(row, ms, live);
          }
          break;
        }
        case "tool_call_completed": {
          const id = str(d.tool_call_id);
          let row = s.tools.get(id);
          const wasFound = !!row;
          if (!row) {
            // Completed without a recorded start: still a row, with no
            // duration to claim.
            row = { kind: "tool", key: s.key + "/tool:" + (id || String(s.items.length)), id, label: str(d.tool_call_name) || "tool", startMs: undefined, endMs: undefined, live, input: "", output: "", outcome: undefined, seq, subrows: [] };
            const parent = isChild && parentToolId ? s.tools.get(parentToolId) : null;
            if (parent) parent.subrows.push(row);
            else s.items.push(row);
            if (id) s.tools.set(id, row);
          }
          row.output += str(d.content);
          row.outcome = "done";
          if (wasFound) touch(row, ms, live);
          if (s.openTool === row) s.openTool = null;
          s.lastText = null;
          break;
        }
        case "step_usage": {
          if (isChild) break; // a worker's usage is its own; the step's columns are the main agent's
          const u = obj(d.data);
          if (num(u.prompt_tokens) !== undefined) s.tokensIn = num(u.prompt_tokens);
          if (num(u.completion_tokens) !== undefined) s.tokensOut = num(u.completion_tokens);
          break;
        }
        case "context_estimate":
        case "todos_updated":
        case "child_started":
        case "child_done":
          // Deliberately no row. The estimate is not a measurement and must
          // never reach a token column; todos and child lifecycle are drawn
          // elsewhere in the chat.
          break;
        case "step_done":
          s.outcome = str(d.content) || "done";
          if (s.outcome === "final") t.outcome = "final";
          s.lastText = null;
          break;
        case "done":
          if (t.outcome === "open") t.outcome = "done";
          break;
        case "recoverable_error":
        case "error": {
          s.items.push({ kind: "error", key: s.key + "/err:" + s.items.length, label: kind === "error" ? "error" : "retry", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          if (kind === "error") t.outcome = "error";
          s.lastText = null;
          break;
        }
        case "pending_ops": {
          const p = obj(d.data);
          const n = Array.isArray(p.ops) ? p.ops.length : 0;
          s.items.push({ kind: "pending", key: s.key + "/pending:" + s.items.length, label: n + " pending change" + (n === 1 ? "" : "s") + (p.applied ? " (applied)" : ""), startMs: ms, endMs: ms, live, seq });
          s.lastText = null;
          break;
        }
        case "mode_route": {
          const r = obj(d.data);
          t.depth1.push({ kind: "route", key: t.key + "/route:" + t.depth1.length, label: "mode " + str(r.from) + " → " + str(r.to), startMs: ms, endMs: ms, live, seq, output: str(r.reason) });
          break;
        }
        default:
          s.items.push({ kind: "other", key: s.key + "/" + (kind || "event") + ":" + s.items.length, label: kind || "event", startMs: ms, endMs: ms, live, output: str(d.content), seq });
          s.lastText = null;
      }
    }

    // Flatten to rows. Offsets are relative to the row's own turn.
    const dur = (n) => (n.startMs !== undefined && n.endMs !== undefined ? n.endMs - n.startMs : undefined);
    const off = (t, n) => (t.startMs !== undefined && n.startMs !== undefined ? n.startMs - t.startMs : undefined);
    const push = (t, n, depth, kind, extra) => {
      rows.push(
        Object.assign(
          { kind, depth, key: n.key, label: n.label, turnId: t.id, startMs: n.startMs, offsetMs: off(t, n), durationMs: dur(n), live: Boolean(n.live), seq: n.seq },
          extra || {}
        )
      );
    };
    const pushTool = (t, s, node, depth) => {
      push(t, node, depth, "tool", { step: s.n, input: node.input || undefined, output: node.output || undefined, outcome: node.outcome });
      for (const sub of node.subrows) pushTool(t, s, sub, depth + 1);
    };

    for (const t of turns.values()) {
      push(t, { key: t.key, label: "turn " + t.ordinal, startMs: t.startMs, endMs: t.endMs, live: t.live }, 0, "turn",
        // The core measured this turn where it ran. Prefer that over the span
        // between the first and last event we happened to receive; extra is
        // merged last, so it wins over the derived value.
        t.durationMs === undefined ? { outcome: t.outcome } : { outcome: t.outcome, durationMs: t.durationMs });
      for (const n of t.depth1) {
        if (n.kind !== "step") {
          push(t, n, 1, n.kind, { outcome: n.outcome, output: n.output || undefined });
          continue;
        }
        push(t, { key: n.key, label: "step " + n.n, startMs: n.startMs, endMs: n.endMs, live: n.live }, 1, "step", { step: n.n, tokensIn: n.tokensIn, tokensOut: n.tokensOut, outcome: n.outcome });
        for (const it of n.items) {
          if (it.kind === "tool") pushTool(t, n, it, 2);
          else push(t, it, 2, it.kind, { step: n.n, output: it.output || undefined, outcome: it.outcome });
        }
      }
    }
    return rows;
  }

  // ---- rendering ------------------------------------------------------------

  const trajectorySummary = document.getElementById("trajectory-summary");
  const trajectoryRowsEl = document.getElementById("trajectory-rows");
  const trajTimelineEl = document.getElementById("traj-timeline");
  const trajMetricEl = document.getElementById("traj-metric");
  const trajSearchEl = document.getElementById("traj-search");
  const trajPanelEl = document.getElementById("traj-panel");
  const trajPanelKindEl = document.getElementById("traj-panel-kind");
  const trajPanelLocEl = document.getElementById("traj-panel-loc");
  const trajPanelBodyEl = document.getElementById("traj-panel-body");
  const trajPanelTabsEl = document.getElementById("traj-panel-tabs");
  const trajPanelCloseBtn = document.getElementById("traj-panel-close");
  const viewChatBtn = document.getElementById("view-chat-btn");
  const viewTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const appViewEl = document.getElementById("app");

  /** @type {Array<{seq?: number, time_ms?: number, type: string, data?: any}>} */
  let trajEvents = [];
  /** null until the host has answered once; then the log's own recorded flag. */
  let trajRecorded = null;
  /** Set when the host could not fetch the log; shown instead of a false "not recorded". */
  let trajError = "";
  let trajRenderQueued = false;
  /** The row whose details the side panel is showing, by key. */
  let trajSelectedKey = "";
  /** Which of the panel's three tabs is open. */
  let trajTab = "summary";
  /** What the timeline measures: elapsed time, one block per turn, or per call. */
  let trajMetric = "duration";
  /** The row filter typed into the toolbar's search box. */
  let trajQuery = "";
  /** @type {TrajRow[]} The rows of the last render, for the panel to look up. */
  let trajRowsCache = [];

  const TRAJ_BADGE = { turn: "TURN", step: "STEP", tool: "TOOL", text: "TEXT", reasoning: "THINK", error: "ERROR", stage: "STAGE", pending: "DIFF", route: "ROUTE", other: "EVENT" };
  /** Inline diffs run an O(n·m) alignment, so a big file gets its stats and the full viewer instead. */
  const TRAJ_DIFF_LINE_BUDGET = 1200;

  function currentView() {
    return appViewEl && appViewEl.dataset.view === "trajectory" ? "trajectory" : "chat";
  }

  /** @param {string} view */
  function setView(view) {
    const v = view === "trajectory" ? "trajectory" : "chat";
    if (appViewEl) appViewEl.dataset.view = v;
    if (viewChatBtn) viewChatBtn.setAttribute("aria-selected", v === "chat" ? "true" : "false");
    if (viewTrajectoryBtn) viewTrajectoryBtn.setAttribute("aria-selected", v === "trajectory" ? "true" : "false");
  }

  function bindViewSwitch() {
    if (viewChatBtn) viewChatBtn.addEventListener("click", () => setView("chat"));
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("click", () => setView("trajectory"));
    // Arrow keys move between the two segments, as a tablist does. The
    // listeners sit on each button rather than on a shared ancestor found via
    // closest(): the test harness's stub closest() returns null, which is what
    // left C1's rail keyboard path with no automated coverage.
    const onKey = (e) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      const next = currentView() === "chat" ? "trajectory" : "chat";
      setView(next);
      const btn = next === "chat" ? viewChatBtn : viewTrajectoryBtn;
      if (btn) btn.focus();
    };
    if (viewChatBtn) viewChatBtn.addEventListener("keydown", onKey);
    if (viewTrajectoryBtn) viewTrajectoryBtn.addEventListener("keydown", onKey);
  }

  function scheduleTrajectoryRender() {
    if (trajRenderQueued) return;
    trajRenderQueued = true;
    requestAnimationFrame(() => {
      trajRenderQueued = false;
      renderTrajectory();
    });
  }

  /** Session switch or clear: forget everything and wait for the host. */
  function resetTrajectory() {
    trajEvents = [];
    trajRecorded = null;
    trajError = "";
    trajSelectedKey = "";
    trajRowsCache = [];
    scheduleTrajectoryRender();
  }

  /**
   * The host fetched session.trajectory: this is now the whole truth.
   *
   * With one exception, and it is the common case rather than a corner. A
   * failed fetch reaches here as an error carrying no events, and both hosts
   * send it at turn end — exactly when the pane is full of live rows the user
   * has been watching go by. Replacing those with "Trajectory unavailable"
   * would throw a correct view away because one re-read hiccuped, so an empty
   * answer that failed keeps what is on screen and reports the failure beside
   * it. An empty answer that succeeded still replaces: that one is the core
   * saying the session genuinely has no events.
   */
  function replaceTrajectory(recorded, events, error) {
    const list = Array.isArray(events) ? events.slice() : [];
    const failed = typeof error === "string" && error !== "";
    trajError = failed ? error : "";
    if (failed && list.length === 0 && trajEvents.length > 0) {
      scheduleTrajectoryRender();
      return;
    }
    trajRecorded = Boolean(recorded);
    trajEvents = list;
    scheduleTrajectoryRender();
  }

  /** One live notification. A turn is being recorded now even if the session
   * predated the log, so the "not recorded" answer no longer applies. */
  function appendTrajectoryEvent(ev) {
    if (!ev || typeof ev.type !== "string") return;
    trajEvents.push({ type: ev.type, data: ev.data });
    trajRecorded = true;
    scheduleTrajectoryRender();
  }

  /** Empty the timeline and fold the panel away: there is nothing to point at. */
  function clearTrajChrome() {
    if (trajTimelineEl) trajTimelineEl.innerHTML = "";
    if (trajPanelEl) trajPanelEl.hidden = true;
  }

  /** @param {TrajRow} r @param {string} q */
  function trajRowMatches(r, q) {
    if (!q) return true;
    const haystack = r.kind + " " + r.label + " " + (r.outcome || "") + " " + (r.input || "") + " " + (r.output || "");
    return haystack.toLowerCase().indexOf(q) !== -1;
  }

  function renderTrajectory() {
    if (!trajectoryRowsEl || !trajectorySummary) return;
    const rows = buildTrajectoryTree(trajEvents);
    trajRowsCache = rows;
    trajectoryRowsEl.innerHTML = "";
    if (trajError && rows.length === 0) {
      trajectorySummary.textContent = i18n("traj.unavailable", { detail: trajError });
      clearTrajChrome();
      return;
    }
    if (trajRecorded === false && rows.length === 0) {
      trajectorySummary.textContent = i18n("traj.not_recorded");
      clearTrajChrome();
      return;
    }
    if (rows.length === 0) {
      trajectorySummary.textContent = i18n(
        trajRecorded === null ? "traj.loading" : "traj.empty"
      );
      clearTrajChrome();
      return;
    }
    let turns = 0;
    let liveCount = 0;
    for (const r of rows) {
      if (r.kind === "turn") turns++;
      if (r.live) liveCount++;
    }
    const q = trajQuery.trim().toLowerCase();
    const shown = q ? rows.filter((r) => trajRowMatches(r, q)) : rows;
    const summaryBits = [
      i18n(turns === 1 ? "traj.turns_one" : "traj.turns_n", { n: turns }),
      i18n("traj.rows_n", { n: rows.length }),
    ];
    if (liveCount) summaryBits.push(i18n("traj.live_n", { n: liveCount }));
    if (q) summaryBits.push(i18n("traj.matching_n", { n: shown.length }));
    // These rows survived a failed re-read (see replaceTrajectory). Say so:
    // they are what the pane saw live, not what the core has on disk.
    if (trajError) summaryBits.push(i18n("traj.refresh_failed", { detail: trajError }));
    trajectorySummary.textContent = summaryBits.join(" · ");
    renderTrajTimeline(rows);
    const frag = document.createDocumentFragment();
    for (const r of shown) frag.appendChild(renderTrajRow(r));
    trajectoryRowsEl.appendChild(frag);
    renderTrajPanel();
  }

  /**
   * The timeline. "duration" lays turns, steps and tool calls on three tracks
   * against one elapsed-time scale, which is the only view where a gap means
   * idle time; "turns" and "calls" give every turn (or every call) the same
   * width, for reading a long session by structure rather than by clock.
   * @param {TrajRow[]} rows
   */
  function renderTrajTimeline(rows) {
    if (!trajTimelineEl) return;
    trajTimelineEl.innerHTML = "";
    const track = (label, items, span) => {
      const line = document.createElement("div");
      line.className = "traj-tl-track";
      const name = document.createElement("span");
      name.className = "traj-tl-name";
      name.textContent = label;
      const bar = document.createElement("div");
      bar.className = "traj-tl-bar";
      items.forEach((r, i) => {
        const block = document.createElement("button");
        block.type = "button";
        block.className = "traj-tl-block";
        block.dataset.kind = r.kind;
        block.dataset.key = r.key;
        if (r.key === trajSelectedKey) block.dataset.selected = "true";
        block.title = r.label + (r.durationMs === undefined ? "" : " · " + formatToolDuration(r.durationMs));
        block.setAttribute("aria-label", r.kind + " " + r.label);
        const box = span(r, i);
        block.style.setProperty("left", box.left + "%");
        block.style.setProperty("width", box.width + "%");
        bar.appendChild(block);
      });
      line.append(name, bar);
      trajTimelineEl.appendChild(line);
    };

    if (trajMetric === "turns" || trajMetric === "calls") {
      const wanted = trajMetric === "turns" ? "turn" : "tool";
      const items = rows.filter((r) => r.kind === wanted);
      const each = items.length ? 100 / items.length : 100;
      track(trajMetric === "turns" ? "Turns" : "Calls", items, (_r, i) => ({
        left: +(i * each).toFixed(3),
        width: +Math.max(each - 0.4, 0.6).toFixed(3),
      }));
      return;
    }

    // Elapsed time: offsets are relative to the row's own turn, so shift each
    // turn by where it starts to put every track on one session-wide scale.
    const turnStart = new Map();
    let base = Infinity;
    for (const r of rows) {
      if (r.startMs === undefined) continue;
      if (r.kind === "turn") turnStart.set(r.turnId, r.startMs);
      if (r.startMs < base) base = r.startMs;
    }
    if (!isFinite(base)) base = 0;
    const at = (r) => {
      if (r.startMs !== undefined) return r.startMs - base;
      const s = turnStart.get(r.turnId);
      return s === undefined ? 0 : s - base + (r.offsetMs || 0);
    };
    let total = 0;
    for (const r of rows) {
      const end = at(r) + (r.durationMs || 0);
      if (end > total) total = end;
    }
    if (total <= 0) total = 1;
    const span = (r) => {
      const left = Math.max(0, Math.min(100, (at(r) / total) * 100));
      const raw = ((r.durationMs || 0) / total) * 100;
      return { left: +left.toFixed(3), width: +Math.max(Math.min(raw, 100 - left), 0.6).toFixed(3) };
    };
    track("Turns", rows.filter((r) => r.kind === "turn"), span);
    track("Steps", rows.filter((r) => r.kind === "step"), span);
    track("Tools", rows.filter((r) => r.kind === "tool"), span);
  }

  /** @param {string} key */
  function selectTrajRow(key) {
    trajSelectedKey = trajSelectedKey === key ? "" : key;
    scheduleTrajectoryRender();
  }

  /** The recorded event a row was built from, when it has one. @param {TrajRow} r */
  function trajSourceEvent(r) {
    if (!r || r.seq === undefined) return null;
    for (const e of trajEvents) {
      if (e && e.seq === r.seq) return e;
    }
    return null;
  }

  function renderTrajPanel() {
    if (!trajPanelEl || !trajPanelBodyEl) return;
    const row = trajRowsCache.find((r) => r.key === trajSelectedKey);
    if (!row) {
      trajPanelEl.hidden = true;
      return;
    }
    trajPanelEl.hidden = false;
    if (trajPanelKindEl) {
      trajPanelKindEl.textContent = TRAJ_BADGE[row.kind] || TRAJ_BADGE.other;
      trajPanelKindEl.dataset.kind = row.kind;
    }
    if (trajPanelLocEl) {
      const bits = [];
      if (row.turnId) bits.push("turn " + row.turnId);
      if (row.step !== undefined) bits.push("step " + row.step);
      trajPanelLocEl.textContent = bits.join(" · ") || row.label;
    }
    if (trajPanelTabsEl && trajPanelTabsEl.querySelectorAll) {
      trajPanelTabsEl.querySelectorAll(".traj-panel-tab").forEach((el) => {
        el.setAttribute("aria-selected", el.getAttribute("data-tab") === trajTab ? "true" : "false");
      });
    }
    trajPanelBodyEl.innerHTML = "";
    const ev = trajSourceEvent(row);
    if (trajTab === "raw") {
      renderTrajRaw(row, ev);
    } else if (trajTab === "preview") {
      renderTrajPreview(row, ev);
    } else {
      renderTrajSummaryTab(row, ev);
    }
  }

  /** @param {string} head @param {string} text */
  function trajPanelPre(head, text) {
    const h = document.createElement("div");
    h.className = "traj-panel-section";
    h.textContent = head;
    const pre = document.createElement("pre");
    pre.className = "traj-pre";
    pre.textContent = text;
    trajPanelBodyEl.append(h, pre);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajSummaryTab(row, ev) {
    const dl = document.createElement("div");
    dl.className = "traj-facts";
    const fact = (k, v) => {
      if (v === "" || v === undefined || v === null) return;
      const key = document.createElement("span");
      key.className = "traj-fact-key";
      key.textContent = k;
      const val = document.createElement("span");
      val.className = "traj-fact-val";
      val.textContent = String(v);
      dl.append(key, val);
    };
    fact(i18n("traj.fact.kind"), row.kind);
    fact(i18n("traj.fact.label"), row.label);
    fact(i18n("traj.fact.outcome"), row.outcome);
    fact(i18n("traj.fact.offset"), row.offsetMs === undefined ? "" : "+" + formatToolDuration(row.offsetMs));
    fact(i18n("traj.fact.duration"), row.durationMs === undefined ? "" : formatToolDuration(row.durationMs));
    fact(i18n("traj.fact.tokens_in"), row.tokensIn);
    fact(i18n("traj.fact.tokens_out"), row.tokensOut);
    fact(i18n("traj.fact.live"), row.live ? i18n("traj.fact.yes") : "");
    fact(i18n("traj.fact.event"), ev && ev.type ? ev.type + (ev.data && ev.data.type ? " · " + ev.data.type : "") : "");
    fact(i18n("traj.fact.seq"), row.seq);
    trajPanelBodyEl.appendChild(dl);
    const hasDiff = ev && ev.data && ev.data.data && Array.isArray(ev.data.data.diff) && ev.data.data.diff.length > 0;
    if (!row.input && !row.output && !hasDiff) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = i18n("traj.no_payload");
      trajPanelBodyEl.appendChild(note);
    }
  }

  /**
   * Preview shows what the row actually holds: file diffs for a pending-ops
   * row (the only event that carries before/after content), otherwise the
   * tool's arguments and its result. Tool results are truncated by the core
   * at 256 bytes, so the pane says so rather than looking complete.
   * @param {TrajRow} row @param {any} ev
   */
  function renderTrajPreview(row, ev) {
    // The notification nests its own payload: params are {turn_id, step, type,
    // data:{...}}, so a pending_ops diff lives at data.data.diff.
    const payload = ev && ev.data && ev.data.data && typeof ev.data.data === "object" ? ev.data.data : null;
    const diffs = payload && Array.isArray(payload.diff) ? payload.diff : null;
    if (diffs && diffs.length) {
      for (const d of diffs) {
        renderTrajFileDiff(String(d.path || ""), String(d.before || ""), String(d.after || ""));
      }
      return;
    }
    if (row.input) {
      let text = row.input;
      try {
        text = JSON.stringify(JSON.parse(row.input), null, 2);
      } catch (e) {
        // Arguments stream in as fragments, so a mid-turn row holds partial
        // JSON. Showing it verbatim beats showing nothing.
      }
      trajPanelPre("arguments", text);
    }
    if (row.output) {
      trajPanelPre(i18n("traj.result"), row.output);
    }
    if (!row.input && !row.output) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = i18n("traj.no_preview");
      trajPanelBodyEl.appendChild(note);
    }
  }

  /** @param {string} path @param {string} before @param {string} after */
  function renderTrajFileDiff(path, before, after) {
    const stats = countDiffStats(before, after);
    const head = document.createElement("div");
    head.className = "traj-diff-head";
    const name = document.createElement("span");
    name.className = "traj-diff-path";
    name.textContent = path || "(unnamed file)";
    const count = document.createElement("span");
    count.className = "traj-diff-stats";
    count.textContent = "+" + stats.add + " −" + stats.del;
    const open = document.createElement("button");
    open.type = "button";
    open.className = "traj-diff-open";
    open.textContent = i18n("traj.open_full_diff");
    open.addEventListener("click", () => showDiffViewer(path, before, after, ""));
    head.append(name, count, open);
    trajPanelBodyEl.appendChild(head);

    const lineCount = before.split("\n").length + after.split("\n").length;
    if (lineCount > TRAJ_DIFF_LINE_BUDGET) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = i18n("traj.too_large", { n: lineCount });
      trajPanelBodyEl.appendChild(note);
      return;
    }
    const block = document.createElement("div");
    block.className = "traj-diff";
    for (const line of alignDiffLines(before, after)) {
      if (line.type === "same") continue;
      const el = document.createElement("div");
      el.className = "traj-diff-line traj-diff-" + line.type;
      el.textContent = (line.type === "add" ? "+ " : "− ") + (line.type === "add" ? line.right : line.left);
      block.appendChild(el);
    }
    if (!block.childNodes || block.childNodes.length === 0) {
      const note = document.createElement("p");
      note.className = "traj-panel-note";
      note.textContent = i18n("traj.no_line_changed");
      trajPanelBodyEl.appendChild(note);
      return;
    }
    trajPanelBodyEl.appendChild(block);
  }

  /** @param {TrajRow} row @param {any} ev */
  function renderTrajRaw(row, ev) {
    if (ev) {
      trajPanelPre(i18n("traj.recorded_event"), JSON.stringify(ev, null, 2));
      return;
    }
    // A live row has no envelope yet: it arrived as a forwarded notification
    // with no seq or time_ms, so the row itself is the whole truth.
    trajPanelPre(i18n("traj.live_row"), JSON.stringify(row, null, 2));
  }

  /**
   * One row. Every row opens the side panel — the payload is no longer folded
   * out in place, so a row's height never changes and the list stays scannable.
   * @param {TrajRow} r
   */
  function renderTrajRow(r) {
    const el = document.createElement("div");
    el.className = "traj-row traj-" + r.kind + (r.live ? " traj-live" : "");
    el.dataset.depth = String(r.depth);
    el.dataset.key = r.key;
    el.dataset.kind = r.kind;
    if (r.key === trajSelectedKey) el.dataset.selected = "true";
    el.style.setProperty("--traj-depth", String(r.depth));
    el.tabIndex = 0;
    el.setAttribute("role", "button");

    const off = document.createElement("span");
    off.className = "traj-off";
    off.textContent = r.offsetMs === undefined ? "" : "+" + formatToolDuration(r.offsetMs);
    const badge = document.createElement("span");
    badge.className = "traj-badge";
    badge.dataset.kind = r.kind;
    badge.textContent = TRAJ_BADGE[r.kind] || TRAJ_BADGE.other;
    const label = document.createElement("span");
    label.className = "traj-label";
    label.textContent = r.label + (r.outcome && r.kind !== "tool" ? " · " + r.outcome : "");
    const dur = document.createElement("span");
    dur.className = "traj-dur";
    dur.textContent = r.durationMs === undefined ? "" : formatToolDuration(r.durationMs);
    const tok = document.createElement("span");
    tok.className = "traj-tok";
    // Blank unless a provider actually reported a number. Never an estimate.
    tok.textContent =
      (r.tokensIn === undefined ? "" : r.tokensIn + "↑") +
      (r.tokensOut === undefined ? "" : (r.tokensIn === undefined ? "" : " ") + r.tokensOut + "↓");
    el.append(off, badge, label, dur, tok);
    el.setAttribute("aria-label", r.kind + " " + r.label + (r.live ? " (live)" : ""));
    el.addEventListener("click", () => selectTrajRow(r.key));
    el.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectTrajRow(r.key);
      }
    });
    return el;
  }

  function bindTrajectoryChrome() {
    if (trajMetricEl && trajMetricEl.addEventListener) {
      trajMetricEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-metric]") : null;
        if (!btn) return;
        trajMetric = btn.getAttribute("data-metric") || "duration";
        if (trajMetricEl.querySelectorAll) {
          trajMetricEl.querySelectorAll("[data-metric]").forEach((el) => {
            el.setAttribute("aria-selected", el.getAttribute("data-metric") === trajMetric ? "true" : "false");
          });
        }
        scheduleTrajectoryRender();
      });
    }
    if (trajSearchEl && trajSearchEl.addEventListener) {
      trajSearchEl.addEventListener("input", () => {
        trajQuery = trajSearchEl.value || "";
        scheduleTrajectoryRender();
      });
    }
    if (trajTimelineEl && trajTimelineEl.addEventListener) {
      trajTimelineEl.addEventListener("click", (e) => {
        const block = e.target && e.target.closest ? e.target.closest(".traj-tl-block") : null;
        if (!block) return;
        selectTrajRow(block.getAttribute("data-key") || "");
      });
    }
    if (trajPanelTabsEl && trajPanelTabsEl.addEventListener) {
      trajPanelTabsEl.addEventListener("click", (e) => {
        const btn = e.target && e.target.closest ? e.target.closest("[data-tab]") : null;
        if (!btn) return;
        trajTab = btn.getAttribute("data-tab") || "summary";
        renderTrajPanel();
      });
    }
    if (trajPanelCloseBtn && trajPanelCloseBtn.addEventListener) {
      trajPanelCloseBtn.addEventListener("click", () => {
        trajSelectedKey = "";
        scheduleTrajectoryRender();
      });
    }
  }

  bindTrajectoryChrome();
  bindViewSwitch();
  setView("chat");
  scheduleTrajectoryRender();
  function initModeMenu() {
    if (!modeMenu) {
      return;
    }
    modeMenu.innerHTML = "";
    const byId = new Map(MODES.map((m) => [m.id, m]));
    MODE_GROUPS.forEach((group) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      head.textContent = i18n(group.labelKey);
      modeMenu.appendChild(head);
      group.ids.forEach((id) => {
        const m = byId.get(id);
        if (!m) {
          return;
        }
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item";
        btn.dataset.id = m.id;
        btn.title = m.mode;
        btn.innerHTML =
          `<span class="mi mode-icon mode-${escapeAttr(m.id)}">${orchIconMarkup(m.icon, { size: "sm" })}</span>${escapeAttr(m.label)}`;
        modeMenu.appendChild(btn);
      });
    });
  }

  function syncModeUi() {
    const m = currentMode();
    if (modeLabel) {
      modeLabel.textContent = m.label;
    }
    const icon = document.getElementById("mode-icon");
    if (icon) {
      icon.innerHTML = orchIconMarkup(m.icon, { size: "sm" });
      icon.className = `ico mode-icon mode-${m.id}`;
    }
    if (modeBtn) {
      modeBtn.dataset.mode = modeId;
    }
    const app = document.getElementById("app");
    if (app) {
      app.dataset.mode = modeId;
    }
    modeMenu?.querySelectorAll(".menu-item").forEach((el) => {
      const id = el.getAttribute("data-id");
      el.classList.toggle("selected", id === modeId);
    });
    if (orchConfigBtn) {
      orchConfigBtn.hidden = modeId !== "orchestra";
    }
    // Orchestra routes work across tiers, so a single-model pill would lie
    // about which model actually runs. Swap it for the tier breakdown.
    if (modeId === "orchestra") {
      renderOrchestraPill();
      host.postMessage({ type: "listOrchestraRoles" });
    } else if (modelLabelEl) {
      setModelLabel(currentModel);
      if (modelPill) modelPill.title = i18n("model.title");
    }
  }

  /** The six roles the catalogue names; anything else keeps the core's label. */
  const ORCH_ROLE_KEYS = ["planner", "lead", "complex", "focused", "micro", "embed"];

  /** @param {any} r */
  function orchRoleName(r) {
    const key = r && r.key;
    if (key && ORCH_ROLE_KEYS.indexOf(key) >= 0) {
      return i18n("orch.role." + key);
    }
    return (r && (r.label || r.key)) || "";
  }

  /** Models actually configured for one orchestra role. @param {any} r */
  function orchRoleModels(r) {
    if (Array.isArray(r?.models) && r.models.length) return r.models.filter(Boolean);
    return r?.model ? [r.model] : [];
  }

  /** Footer pill in orchestra mode: "L5 <planner> +N" + full map in tooltip. */
  function renderOrchestraPill() {
    if (!modelLabelEl) return;
    const roles = orchestraRolesInfo?.roles || [];
    const planner = roles.find((r) => r.key === "planner");
    const plannerModels = planner ? orchRoleModels(planner) : [];
    const others = roles.filter((r) => r.key !== "planner" && orchRoleModels(r).length > 0);
    if (!roles.length) {
      modelLabelEl.textContent = i18n("orch.tiers");
      modelLabelEl.title = i18n("orch.loading_map");
      if (modelPill) modelPill.title = i18n("orch.tier_models");
      return;
    }
    const base = plannerModels.length
      ? `L5 ${shortModel(plannerModels[0])}`
      : i18n("orch.l5_not_set");
    modelLabelEl.textContent = others.length ? `${base} +${others.length}` : base;
    const lines = roles.map((r) => {
      const models = orchRoleModels(r);
      const tier = r.tier ? `${r.tier} · ` : "";
      return `${tier}${orchRoleName(r)}: ${models.length ? models.join(", ") : i18n("orch.fallback_main")}`;
    });
    modelLabelEl.title = lines.join("\n");
    if (modelPill) modelPill.title = i18n("orch.tier_models");
  }

  /** Read-only tier → models breakdown inside the model dropdown. */
  function renderOrchestraRolesMenu() {
    if (!modelMenuList) return;
    if (modelMenuTitle) modelMenuTitle.textContent = i18n("orch.tiers");
    if (modelMenuSearch) modelMenuSearch.style.display = "none";
    modelMenuList.innerHTML = "";
    const roles = orchestraRolesInfo?.roles || [];
    if (!roles.length) {
      const hint = document.createElement("div");
      hint.className = "menu-hint";
      hint.textContent = i18n("orch.loading_map");
      modelMenuList.appendChild(hint);
    }
    roles.forEach((r) => {
      const head = document.createElement("div");
      head.className = "menu-section";
      const roleName = orchRoleName(r);
      head.textContent = r.tier ? `${roleName} · ${r.tier}` : roleName;
      modelMenuList.appendChild(head);
      const models = orchRoleModels(r);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = i18n("orch.not_set");
        modelMenuList.appendChild(empty);
        return;
      }
      models.forEach((id, i) => {
        const row = document.createElement("div");
        row.className = "menu-hint orch-tier-model";
        row.textContent = i === 0 ? id : i18n("orch.failover_n", { id, n: i + 1 });
        row.title = id;
        modelMenuList.appendChild(row);
      });
    });
    const cfg = document.createElement("button");
    cfg.type = "button";
    cfg.className = "menu-item";
    cfg.setAttribute("data-model-action", "configure-orchestra");
    cfg.textContent = i18n("orch.configure");
    modelMenuList.appendChild(cfg);
  }

  function effortMeterHtml(id) {
    const bars = id === "low" ? 1 : id === "medium" ? 2 : 3;
    let inner = "";
    for (let b = 0; b < bars; b++) {
      inner += "<i></i>";
    }
    return `<span class="effort-meter effort-${escapeAttr(id)}">${inner}</span>`;
  }

  /** @param {HTMLElement} el @param {string} id @param {boolean} [pill] */
  function setEffortIconEl(el, id, pill) {
    el.className = pill ? `ico effort-icon effort-${id}` : `mi effort-icon effort-${id}`;
    el.innerHTML = effortMeterHtml(id);
  }

  function initEffortMenu() {
    if (!effortMenu) {
      return;
    }
    effortMenu.innerHTML = "";
    const head = document.createElement("div");
    head.className = "menu-section";
    head.textContent = i18n("effort.head");
    effortMenu.appendChild(head);
    EFFORTS.forEach((e) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.dataset.effort = e.id;
      btn.innerHTML = `<span class="mi effort-icon effort-${escapeAttr(e.id)}">${effortMeterHtml(e.id)}</span>${escapeAttr(i18n(e.labelKey))}`;
      effortMenu.appendChild(btn);
    });
    const optHead = document.createElement("div");
    optHead.className = "menu-section";
    optHead.textContent = i18n("effort.options");
    effortMenu.appendChild(optHead);
    const fastRow = document.createElement("div");
    fastRow.className = "menu-row menu-row-fast";
    fastRow.innerHTML =
      '<span class="menu-row-label"><span class="mi effort-icon effort-fast" aria-hidden="true">⚡</span>Fast</span>' +
      '<button type="button" id="fast-toggle" class="toggle" role="switch" aria-checked="false"></button>';
    effortMenu.appendChild(fastRow);
    fastToggleRef.el = /** @type {HTMLButtonElement | null} */ (document.getElementById("fast-toggle"));
  }

  function syncEffortUi() {
    const e = currentEffort();
    const effortName = i18n(e.labelKey);
    if (effortLabel) {
      effortLabel.textContent = effortName;
    }
    const icon = document.getElementById("effort-icon");
    if (icon) {
      setEffortIconEl(icon, e.id, true);
    }
    if (effortBtn) {
      effortBtn.dataset.effort = effortId;
      effortBtn.title = fastOn ? `${effortName} · Fast profile` : effortName;
    }
    const fastMark = document.getElementById("effort-fast-mark");
    if (fastMark) {
      fastMark.hidden = !fastOn;
    }
    effortMenu?.querySelectorAll("[data-effort]").forEach((el) => {
      const id = el.getAttribute("data-effort");
      el.classList.toggle("selected", id === effortId);
    });
    const toggle = fastToggleRef.el;
    if (toggle) {
      toggle.classList.toggle("on", fastOn);
      toggle.setAttribute("aria-checked", fastOn ? "true" : "false");
    }
    effortMenu?.querySelector(".menu-row-fast")?.classList.toggle("fast-on", fastOn);
  }

  function ensureAssistant() {
    const inner = ensureTurn();
    if (!inner) {
      return null;
    }
    if (!assistantBubble) {
      assistantBubble = document.createElement("div");
      assistantBubble.className = "turn-text";
      inner.appendChild(assistantBubble);
    }
    return assistantBubble;
  }

  function renderFiles() {
    if (!filesEl) {
      return;
    }
    filesEl.innerHTML = "";
    filesEl.classList.toggle("has-files", files.length > 0);
    files.forEach((f, idx) => {
      const card = document.createElement("div");
      const isImage = f.kind === "image";
      card.className = "file-attach" + (isImage ? " file-attach-image" : "");

      if (isImage && f.previewUri) {
        const img = document.createElement("img");
        img.className = "file-attach-thumb";
        img.src = f.previewUri;
        img.alt = f.name;
        img.loading = "lazy";
        card.appendChild(img);
      } else if (isImage) {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-image";
        icon.textContent = "IMG";
        card.appendChild(icon);
      } else {
        const icon = document.createElement("div");
        icon.className = "file-attach-icon kind-" + (f.kind || "binary");
        icon.textContent = fileExtLabel(f.ext || f.name);
        card.appendChild(icon);
      }

      const meta = document.createElement("div");
      meta.className = "file-attach-meta";
      const name = document.createElement("span");
      name.className = "file-attach-name";
      name.textContent = f.name;
      name.title = f.path || f.name;
      meta.appendChild(name);
      card.appendChild(meta);

      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "file-attach-remove";
      rm.setAttribute("aria-label", i18n("attach.remove"));
      rm.textContent = "×";
      rm.addEventListener("click", (e) => {
        e.stopPropagation();
        files.splice(idx, 1);
        renderFiles();
      });
      card.appendChild(rm);
      card.style.cursor = "pointer";
      card.addEventListener("click", (e) => {
        if (e.target === rm || rm.contains(/** @type {Node} */ (e.target))) return;
        onAttachmentClick(f, isImage, e);
      });
      filesEl.appendChild(card);
    });
  }

  function send() {
    if (busy) {
      host.postMessage({ type: "cancelTurn" });
      return;
    }
    if (!inputEl) {
      return;
    }
    let text = inputEl.value.trim();
    if (!text && files.length === 0) {
      return;
    }
    if (text.startsWith("/") && !text.includes("\n")) {
      const cmd = text.split(/\s+/)[0].toLowerCase();
      const arg = text.slice(cmd.length).trim() || undefined;
      inputEl.value = "";
      hidePalette();
      autoGrow();
      host.postMessage({ type: "slashCommand", cmd, arg });
      return;
    }
    const payload = {
      type: "send",
      text,
      mode: currentMode().mode,
      profile: effectiveProfile(),
      apply: false,
      allowExec: accessId === "auto",
      allowBrowser: browserOn,
      allowBrowserDrive: browserDriveOn,
      allowBrowserEval: browserEvalOn,
      files: files.map((f) => ({
        name: f.name,
        path: f.path,
        ext: f.ext,
        kind: f.kind,
        previewUri: f.previewUri,
      })),
    };
    inputEl.value = "";
    autoGrow();
    files = [];
    renderFiles();
    closeMenus();
    host.postMessage(payload);
  }

  function autoGrow() {
    if (!inputEl) {
      return;
    }
    inputEl.style.height = "auto";
    inputEl.style.height = Math.min(140, Math.max(44, inputEl.scrollHeight)) + "px";
  }

  function positionAccessMenu() {
    if (!accessMenu || !accessBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = accessBtn.getBoundingClientRect();
    accessMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    accessMenu.style.right = "auto";
  }

  function positionEffortMenu() {
    if (!effortMenu || !effortBtn || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = effortBtn.getBoundingClientRect();
    effortMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    effortMenu.style.right = "auto";
  }

  function positionModelMenu() {
    if (!modelMenu || !modelPill || !composerWrap) {
      return;
    }
    const wrapRect = composerWrap.getBoundingClientRect();
    const btnRect = modelPill.getBoundingClientRect();
    modelMenu.style.left = Math.max(12, btnRect.left - wrapRect.left) + "px";
    modelMenu.style.right = "auto";
  }

  function renderProviderModels(catalog) {
    if (!modelMenuList) return;
    providerModelsCatalog = catalog;
    modelMenuList.innerHTML = "";
    const providers = Array.isArray(catalog.providers) ? catalog.providers : [];
    const activeModel = catalog.activeModel || currentModel;
    const q = (modelMenuFilter || "").trim().toLowerCase();
    if (!providers.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = i18n("model.no_providers");
      modelMenuList.appendChild(btn);
      return;
    }
    let shown = 0;
    providers.forEach((p) => {
      const models = Array.isArray(p.models) ? p.models : [];
      const filtered = q
        ? models.filter((m) => (m.id || "").toLowerCase().includes(q))
        : models;
      if (!filtered.length && models.length && q) {
        return;
      }
      const head = document.createElement("div");
      head.className = "menu-section";
      const count = filtered.length || models.length;
      head.textContent = `${p.name || p.key}${count ? ` (${count})` : ""}`;
      modelMenuList.appendChild(head);
      if (!models.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = p.models_error || i18n(p.ready ? "model.none" : "model.not_configured");
        modelMenuList.appendChild(empty);
        return;
      }
      if (!filtered.length) {
        const empty = document.createElement("div");
        empty.className = "menu-hint";
        empty.textContent = i18n("palette.no_matches");
        modelMenuList.appendChild(empty);
        return;
      }
      filtered.forEach((m) => {
        shown++;
        const id = m.id || "";
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "menu-item" + (id === activeModel && p.active ? " selected" : "");
        btn.setAttribute("data-model", id);
        btn.setAttribute("data-provider", p.key);
        // textContent, not innerHTML: model ids come from a remote catalog.
        btn.textContent = id;
        btn.title = id;
        modelMenuList.appendChild(btn);
      });
    });
    if (shown === 0 && q) {
      const empty = document.createElement("div");
      empty.className = "menu-hint";
      empty.textContent = i18n("model.no_match", { q: modelMenuFilter });
      modelMenuList.appendChild(empty);
    }
  }

  function renderModelList(models, current) {
    if (!modelMenuList) {
      return;
    }
    if (current) {
      setModelLabel(current);
    }
    modelMenuList.innerHTML = "";
    if (!models.length) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item";
      btn.setAttribute("data-model-action", "refresh");
      btn.textContent = i18n("model.retry");
      modelMenuList.appendChild(btn);
      return;
    }
    models.forEach((m) => {
      const id = typeof m === "string" ? m : m.id;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "menu-item" + (id === currentModel ? " selected" : "");
      btn.setAttribute("data-model", id);
      // textContent, not innerHTML: model ids come from a remote catalog.
      btn.textContent = id;
      btn.title = id;
      modelMenuList.appendChild(btn);
    });
  }

  contextBtn?.addEventListener("mouseenter", () => showContextPopover(true));
  contextBtn?.addEventListener("mouseleave", () => {
    window.setTimeout(() => {
      if (!contextPopover?.matches(":hover")) showContextPopover(false);
    }, 120);
  });
  contextPopover?.addEventListener("mouseenter", () => showContextPopover(true));
  contextPopover?.addEventListener("mouseleave", () => showContextPopover(false));
  contextBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    showContextPopover(!ctxPopoverOpen);
  });

  // ---- Spend / balance chip (provider-reported cost, OpenRouter balance) ----

  /** Format a USD amount compactly: $1.24, $0.031, <$0.001. */
  function formatUsd(v) {
    if (!(typeof v === "number") || !isFinite(v) || v <= 0) {
      return "$0.00";
    }
    if (v < 0.001) {
      return "<$0.001";
    }
    if (v < 0.1) {
      return "$" + v.toFixed(3);
    }
    return "$" + v.toFixed(2);
  }

  function shortModelName(id) {
    const parts = String(id || "").split("/");
    return parts[parts.length - 1] || String(id || "");
  }

  function renderCostChip() {
    if (!costWrap) {
      return;
    }
    const liveTotal = sessionCostUSD + turnCostAccum;
    const hasSpend = liveTotal > 0 || (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0);
    const hasBalance = !!(creditsInfo && creditsInfo.supported);
    if (!hasSpend && !hasBalance) {
      costWrap.classList.add("hidden");
      return;
    }
    costWrap.classList.remove("hidden");
    if (costLabelEl) {
      costLabelEl.textContent = hasSpend ? formatUsd(liveTotal) : formatUsd(creditsInfo?.balance || 0);
      costLabelEl.title = i18n(hasSpend ? "cost.session_spend" : "cost.balance");
    }
    if (costBalanceEl) {
      costBalanceEl.textContent = hasBalance
        ? i18n("cost.balance_prefix", { amount: formatUsd(creditsInfo?.balance || 0) })
        : "";
    }
    if (costSummaryEl) {
      const bits = [];
      bits.push(i18n("cost.session", { amount: formatUsd(liveTotal) }));
      if (turnCostAccum > 0) {
        bits.push(i18n("cost.current_turn", { amount: formatUsd(turnCostAccum) }));
      } else if (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0) {
        bits.push(i18n("cost.last_turn", { amount: formatUsd(lastTurnUsage.cost_usd) }));
      }
      costSummaryEl.textContent = bits.join(" · ");
    }
    if (costRowsEl) {
      costRowsEl.innerHTML = "";
      const entries = (lastTurnUsage && Array.isArray(lastTurnUsage.entries)) ? lastTurnUsage.entries : [];
      entries.forEach((en) => {
        const row = document.createElement("div");
        row.className = "cost-row";
        const name = document.createElement("span");
        name.className = "cost-row-model";
        name.textContent = shortModelName(en.model);
        name.title = (en.provider ? en.provider + " / " : "") + (en.model || "");
        const meta = document.createElement("span");
        meta.className = "cost-row-meta";
        const calls = typeof en.calls === "number" && en.calls > 0 ? en.calls + "× · " : "";
        const toks = typeof en.total_tokens === "number" && en.total_tokens > 0
          ? Math.round(en.total_tokens / 1000) + "k tok · "
          : "";
        meta.textContent = calls + toks + formatUsd(en.cost_usd || 0);
        row.appendChild(name);
        row.appendChild(meta);
        costRowsEl.appendChild(row);
      });
    }
  }

  let costPopoverOpen = false;
  function showCostPopover(show) {
    costPopoverOpen = show;
    costPopover?.classList.toggle("hidden", !show);
    costBtn?.classList.toggle("open", show);
  }
  costBtn?.addEventListener("mouseenter", () => showCostPopover(true));
  costBtn?.addEventListener("mouseleave", () => {
    window.setTimeout(() => {
      if (!costPopover?.matches(":hover")) showCostPopover(false);
    }, 120);
  });
  costPopover?.addEventListener("mouseenter", () => showCostPopover(true));
  costPopover?.addEventListener("mouseleave", () => showCostPopover(false));
  costBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    showCostPopover(!costPopoverOpen);
  });

  sendBtn?.addEventListener("click", send);
  inputEl?.addEventListener("keydown", (e) => {
    if (paletteMode && paletteItems.length && (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Tab")) {
      e.preventDefault();
      if (e.key === "ArrowDown") {
        paletteIndex = (paletteIndex + 1) % paletteItems.length;
      } else if (e.key === "ArrowUp") {
        paletteIndex = (paletteIndex - 1 + paletteItems.length) % paletteItems.length;
      }
      paletteMenu?.querySelectorAll(".palette-item").forEach((el, i) => {
        el.classList.toggle("selected", i === paletteIndex);
      });
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (paletteMode && paletteItems.length) {
        applyPaletteItem(paletteIndex);
        return;
      }
      send();
    }
    if (e.key === "Escape") {
      closeMenus();
    }
  });
  inputEl?.addEventListener("input", () => {
    autoGrow();
    onInputPalette();
  });

  modeBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modeMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      modeMenu?.classList.add("open");
      modeBtn.classList.add("open");
    }
  });

  accessBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !accessMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionAccessMenu();
      accessMenu?.classList.add("open");
      accessBtn.classList.add("open");
    }
  });

  accessMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    if (/** @type {HTMLElement} */ (e.target).closest("#browser-toggle")) {
      browserOn = !browserOn;
      // Nothing below survives the browser being off.
      if (!browserOn) {
        browserDriveOn = false;
        browserEvalOn = false;
      }
      syncAccessUi();
      return;
    }
    if (/** @type {HTMLElement} */ (e.target).closest("#browser-drive-toggle")) {
      browserDriveOn = !browserDriveOn;
      if (browserDriveOn) browserOn = true;
      else browserEvalOn = false;
      syncAccessUi();
      return;
    }
    if (/** @type {HTMLElement} */ (e.target).closest("#browser-eval-toggle")) {
      browserEvalOn = !browserEvalOn;
      if (browserEvalOn) {
        browserOn = true;
        browserDriveOn = true;
      }
      syncAccessUi();
      return;
    }
    const item = /** @type {HTMLElement | null} */ (e.target.closest("[data-access]"));
    if (!item || !accessMenu.contains(item)) {
      return;
    }
    const id = item.getAttribute("data-access");
    if (id) {
      accessId = id;
      syncAccessUi();
      closeMenus();
    }
  });

  effortBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !effortMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      positionEffortMenu();
      effortMenu?.classList.add("open");
      effortBtn.classList.add("open");
    }
  });

  modeMenu?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest(".menu-item");
    if (!item) {
      return;
    }
    const id = item.getAttribute("data-id");
    if (!id) {
      return;
    }
    modeId = id;
    syncModeUi();
    host.setState({ ...(host.getState() || {}), modeId });
    closeMenus();
  });

  effortMenu?.addEventListener("click", (e) => {
    const effortItem = /** @type {HTMLElement | null} */ (e.target.closest("[data-effort]"));
    if (effortItem && effortMenu.contains(effortItem)) {
      const id = effortItem.getAttribute("data-effort");
      if (id) {
        effortId = id;
        syncEffortUi();
        closeMenus();
      }
      return;
    }
    if (/** @type {HTMLElement} */ (e.target).closest("#fast-toggle")) {
      e.stopPropagation();
      fastOn = !fastOn;
      syncEffortUi();
    }
  });

  attachBtn?.addEventListener("click", () => {
    host.postMessage({ type: "attach" });
  });

  sessionNewBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    host.postMessage({ type: "newSession" });
  });

  sessionHistoryBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !sessionMenu?.classList.contains("open");
    closeMenus();
    if (open) {
      sessionMenu?.classList.add("open");
      sessionHistoryBtn.classList.add("open");
      host.postMessage({ type: "listSessions" });
    }
  });

  sessionTabsEl?.addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    const closeEl = t.closest("[data-close-session]");
    if (closeEl) {
      e.stopPropagation();
      const sid = closeEl.getAttribute("data-close-session");
      if (sid) {
        host.postMessage({ type: "closeSession", sessionId: sid });
      }
      return;
    }
    const tab = t.closest(".session-tab[data-session-id]");
    if (!tab) {
      return;
    }
    const sid = tab.getAttribute("data-session-id");
    if (sid && sid !== activeSessionId) {
      host.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  pendingApplyBtn?.addEventListener("click", () => applyPendingChanges());
  pendingRejectBtn?.addEventListener("click", () => discardPendingChanges());

  diffViewerCloseBtn?.addEventListener("click", hideDiffViewer);
  diffViewerEditorBtn?.addEventListener("click", () => {
    if (!diffViewerState.path) return;
    openDiffMessage(diffViewerState.path, diffViewerState.before, diffViewerState.after, false);
  });
  diffViewer?.addEventListener("click", (e) => {
    if (e.target === diffViewer) hideDiffViewer();
  });

  imagePreviewCloseBtn?.addEventListener("click", hideImagePreview);
  imagePreviewPrevBtn?.addEventListener("click", () => shiftImagePreview(-1));
  imagePreviewNextBtn?.addEventListener("click", () => shiftImagePreview(1));
  imagePreviewOpenBtn?.addEventListener("click", () => {
    const item = imagePreviewState.items[imagePreviewState.index];
    if (!item?.path) return;
    hideImagePreview();
    openExternalFile(item.path, true);
  });
  imagePreview?.addEventListener("click", (e) => {
    if (e.target === imagePreview) hideImagePreview();
  });

  composerWrap?.addEventListener("dragover", (e) => {
    e.preventDefault();
    composerWrap.classList.add("drag-over");
  });
  composerWrap?.addEventListener("dragleave", () => {
    composerWrap.classList.remove("drag-over");
  });
  composerWrap?.addEventListener("drop", (e) => {
    e.preventDefault();
    composerWrap.classList.remove("drag-over");
    void attachFilesFromDataTransfer(e.dataTransfer);
  });

  inputEl?.addEventListener("paste", (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    let handledImage = false;
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (!file) continue;
        if (file.size > MAX_ATTACH_BYTES) {
          appendMsg("system", i18n("paste.too_big"));
          continue;
        }
        handledImage = true;
        e.preventDefault();
        void (async () => {
          try {
            const dataBase64 = await readFileAsBase64(file);
            host.postMessage({
              type: "attachBytes",
              name: file.name || "paste.png",
              mime: item.type,
              dataBase64,
            });
          } catch {
            /* ignore */
          }
        })();
      }
    }
    if (handledImage) {
      e.preventDefault();
    }
  });

  modelPill?.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = !modelMenu?.classList.contains("open");
    closeMenus();
    if (open && modeId === "orchestra") {
      renderOrchestraRolesMenu();
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      host.postMessage({ type: "listOrchestraRoles" });
      return;
    }
    if (open) {
      if (modelMenuTitle) modelMenuTitle.textContent = i18n("model.menu_title");
      if (modelMenuSearch) modelMenuSearch.style.display = "";
      modelMenuFilter = "";
      if (modelMenuSearch) {
        modelMenuSearch.value = "";
      }
      positionModelMenu();
      modelMenu?.classList.add("open");
      modelPill.classList.add("open");
      host.postMessage({ type: "listProviderModels" });
      setTimeout(() => modelMenuSearch?.focus(), 30);
    }
  });

  modelMenuSearch?.addEventListener("input", () => {
    modelMenuFilter = modelMenuSearch?.value || "";
    if (providerModelsCatalog) {
      renderProviderModels(providerModelsCatalog);
    }
  });

  modelMenuSearch?.addEventListener("click", (e) => e.stopPropagation());

  orchConfigBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    closeMenus();
    host.postMessage({ type: "openOrchestraSettings" });
  });

  settingsBtn?.addEventListener("click", () => {
    host.postMessage({ type: "openSettings" });
  });

  todosChip?.addEventListener("click", (e) => {
    e.stopPropagation();
    todosExpanded = !todosExpanded;
    renderTodos();
  });

  sessionMenu?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    // Delete is nested inside the row — check it first so the click does not
    // also open the session. The menu stays open; the host pushes a fresh list.
    const delEl = t.closest("[data-delete-session]");
    if (delEl) {
      const delId = delEl.getAttribute("data-delete-session");
      if (delId) {
        host.postMessage({ type: "deleteSession", sessionId: delId });
      }
      return;
    }
    const item = t.closest("[data-session-action], [data-session-id]");
    if (!item) {
      return;
    }
    const action = item.getAttribute("data-session-action");
    if (action === "new") {
      closeMenus();
      host.postMessage({ type: "newSession" });
      return;
    }
    const sid = item.getAttribute("data-session-id");
    if (sid) {
      closeMenus();
      host.postMessage({ type: "openSession", sessionId: sid });
    }
  });

  modelMenuList?.addEventListener("click", (e) => {
    e.stopPropagation();
    const t = /** @type {HTMLElement} */ (e.target);
    const item = t.closest("[data-model], [data-model-action]");
    if (!item) {
      return;
    }
    if (item.getAttribute("data-model-action") === "refresh") {
      host.postMessage({ type: "listProviderModels" });
      return;
    }
    if (item.getAttribute("data-model-action") === "configure-orchestra") {
      closeMenus();
      host.postMessage({ type: "openOrchestraSettings" });
      return;
    }
    const model = item.getAttribute("data-model");
    const provider = item.getAttribute("data-provider") || undefined;
    if (model) {
      closeMenus();
      host.postMessage({ type: "setModel", model, provider });
    }
  });

  paletteMenu?.addEventListener("click", (e) => e.stopPropagation());
  document.addEventListener("click", () => closeMenus());
  modeMenu?.addEventListener("click", (e) => e.stopPropagation());
  effortMenu?.addEventListener("click", (e) => e.stopPropagation());
  sessionMenu?.addEventListener("click", (e) => e.stopPropagation());
  modelMenu?.addEventListener("click", (e) => e.stopPropagation());
  window.addEventListener("message", (event) => {
    const msg = event.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    switch (msg.type) {
      case "status": {
        const st = msg.status || "";
        if (st === "error") {
          setChromeHint(msg.detail || i18n("conn.error"), true);
        } else if (st === "connecting") {
          busyStatusText = msg.detail || i18n("conn.connecting");
          setChromeHint(msg.detail || i18n("conn.connecting"), false);
        } else if (st === "running") {
          busyStatusText = i18n("turn.working");
          setChromeHint(i18n("turn.working"), false);
        } else {
          setChromeHint("", false);
        }
        setBusy(st === "running" || st === "connecting");
        break;
      }
      case "ready":
        setChromeHint("", false);
        setBusy(false);
        break;
      case "turnInFlight":
        // Webview was rebuilt (e.g. returning from Settings) while the agent
        // turn is still running — re-arm the busy UI before the replayed
        // projection arrives.
        setBusy(true);
        break;
      case "header":
        if (msg.sessionId && msg.sessionId !== activeSessionId) {
          // Session switch: drop the previous session's turn breakdown.
          lastTurnUsage = null;
          turnCostAccum = 0;
        }
        activeSessionId = msg.sessionId || activeSessionId;
        updateActiveTabTitle(msg.title || i18n("chrome.new_chat"));
        setModelLabel(msg.model || "");
        if (modelLabelEl && msg.provider) {
          modelLabelEl.title = `${msg.provider} · ${msg.model || ""}`;
        }
        if (typeof msg.sessionCost === "number") {
          sessionCostUSD = msg.sessionCost;
          renderCostChip();
        }
        if (modeId === "orchestra") {
          // Keep the tier breakdown pill; header carries the single main model.
          renderOrchestraPill();
        }
        break;
      case "sessionList": {
        if (!sessionMenuList) {
          break;
        }
        sessionMenuList.innerHTML = "";
        const sessions = Array.isArray(msg.sessions) ? msg.sessions : [];
        if (sessions.length === 0) {
          const empty = document.createElement("div");
          empty.className = "menu-section";
          empty.textContent = i18n("session.none");
          sessionMenuList.appendChild(empty);
          break;
        }
        /** Timestamp for grouping: prefer updated_at, fall back to the id (YYYYMMDDTHHMMSS-xxxx). */
        const sessionDate = (s) => {
          const iso = s.updated_at || s.created_at || "";
          const d = iso ? new Date(iso) : null;
          if (d && !Number.isNaN(d.getTime())) {
            return d;
          }
          const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s.id || "");
          return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : null;
        };
        const dayKey = (d) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
        const now = new Date();
        const todayKey = dayKey(now);
        const yesterdayKey = dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1));
        const dayLabel = (d) => {
          const k = dayKey(d);
          if (k === todayKey) {
            return "Today";
          }
          if (k === yesterdayKey) {
            return "Yesterday";
          }
          return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
        };
        let lastGroup = "";
        sessions.slice(0, 100).forEach((s) => {
          const d = sessionDate(s);
          const group = d ? dayLabel(d) : "Older";
          if (group !== lastGroup) {
            lastGroup = group;
            const header = document.createElement("div");
            header.className = "menu-section";
            header.textContent = group;
            sessionMenuList.appendChild(header);
          }
          const row = document.createElement("div");
          row.className = "menu-item session-row";
          row.setAttribute("role", "button");
          row.setAttribute("tabindex", "0");
          row.setAttribute("data-session-id", s.id);
          row.title = [s.model, s.msg_count ? `${s.msg_count} messages` : ""].filter(Boolean).join(" · ");

          const label = document.createElement("span");
          label.className = "session-row-title";
          label.textContent = s.title || s.id;
          row.appendChild(label);

          if (d) {
            const time = document.createElement("span");
            time.className = "session-row-time";
            time.textContent = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
            row.appendChild(time);
          }

          const del = document.createElement("span");
          del.className = "session-row-del";
          del.setAttribute("data-delete-session", s.id);
          del.title = i18n("session.delete");
          del.innerHTML = orchIconMarkup("close", { size: "sm" });
          row.appendChild(del);

          sessionMenuList.appendChild(row);
        });
        break;
      }
      case "sessionTabs": {
        renderSessionTabs(msg.tabs, msg.activeId);
        break;
      }
      case "models": {
        renderModelList(Array.isArray(msg.models) ? msg.models : [], msg.current || currentModel);
        break;
      }
      case "providerModels":
        renderProviderModels(msg);
        break;
      case "orchestraRoles": {
        orchestraRolesInfo = {
          roles: Array.isArray(msg.roles) ? msg.roles : [],
          defaultTier: msg.defaultTier || "",
        };
        if (modeId === "orchestra") {
          renderOrchestraPill();
          if (modelMenu?.classList.contains("open")) {
            renderOrchestraRolesMenu();
          }
        }
        break;
      }
      case "pendingOps": {
        const payload = msg.payload || {};
        pendingState = {
          ops: Array.isArray(payload.ops) ? payload.ops : [],
          diff: Array.isArray(payload.diff) ? payload.diff : [],
        };
        diffReviewCursor = 0;
        renderPendingBar();
        void syncToolDiffPreviews();
        break;
      }
      // The host decides the language — the editor's display language, the
      // browser's, or what the user chose — and says so here. Sent once on
      // startup and again whenever it changes, so this has to redraw what was
      // already built rather than only affect what is built next.
      case "uiLang": {
        if (!setUiLang(msg.lang)) {
          break;
        }
        applyStaticI18n();
        initModeMenu();
        initAccessMenu();
        initEffortMenu();
        syncModeUi();
        syncAccessUi();
        syncEffortUi();
        // The context popover is built from labels, not from markup, so it
        // keeps the old language until something recomputes it.
        renderContextUi();
        if (!busy) {
          busyStatusText = i18n("turn.working");
        }
        break;
      }
      case "pendingCleared":
        pendingState = { ops: [], diff: [] };
        diffReviewCursor = 0;
        renderPendingBar();
        syncToolDiffStats();
        break;
      // Changes the turn wrote to disk itself. Nothing to approve, so the bar
      // stays down — but the tool blocks get the core's own before/after and
      // upgrade to the same inline diff a dry run shows.
      case "appliedOps": {
        const incoming = Array.isArray(msg.diff) ? msg.diff : [];
        if (incoming.length === 0) break;
        const byPath = new Map(appliedDiffs.map((d) => [String(d.path || ""), d]));
        for (const d of incoming) {
          byPath.set(String(d.path || ""), d);
        }
        appliedDiffs = Array.from(byPath.values());
        void syncToolDiffPreviews();
        break;
      }
      case "permissionRequest":
        showPermissionOverlay(msg.request || {});
        break;
      case "questionAsk":
        showQuestionOverlay(Array.isArray(msg.questions) ? msg.questions : []);
        break;
      case "clearMessages":
        resetTrajectory();
        sendQueue = [];
        renderSendQueue();
        resetContextUsage();
        if (messagesEl) {
          messagesEl.innerHTML = "";
        }
        assistantBubble = null;
        resetTurnState();
        toolBlocks.clear();
        toolArgs.clear();
        execSteps.clear();
        // The blocks these described are gone with the transcript.
        appliedDiffs = [];
        todos = [];
        todosExpanded = false;
        todosHadOpen = false;
        subagents = [];
        subagentByTask.clear();
        renderSubagents();
        renderTodos();
        setWorkflow("", false);
        break;
      case "trajectory":
        replaceTrajectory(msg.recorded, msg.events, msg.error);
        break;
      case "trajectoryEvent":
        appendTrajectoryEvent(msg.event);
        break;
      case "history": {
        const list = Array.isArray(msg.messages) ? msg.messages : [];
        const renderOne = (m) => {
          const role = m.role === "user" ? "user" : m.role === "system" ? "error" : "assistant";
          const opts = {
            uiIndex: typeof m.uiIndex === "number" ? m.uiIndex : undefined,
            files: Array.isArray(m.files) ? m.files : undefined,
            reasoning: typeof m.reasoning === "string" ? m.reasoning : undefined,
            toolBlocks: Array.isArray(m.toolBlocks) ? m.toolBlocks : undefined,
          };
          appendMsg(role, m.text || "", opts);
        };
        // Long sessions: render only the tail eagerly. Hundreds of markdown
        // bubbles + diff cards freeze the webview for seconds; older messages
        // expand on demand.
        const HISTORY_RENDER_CAP = 60;
        if (list.length > HISTORY_RENDER_CAP) {
          const hidden = list.slice(0, list.length - HISTORY_RENDER_CAP);
          const shown = list.slice(list.length - HISTORY_RENDER_CAP);
          const moreBtn = document.createElement("button");
          moreBtn.type = "button";
          moreBtn.className = "history-more";
          moreBtn.textContent = i18n("msg.show_older", { n: hidden.length });
          moreBtn.addEventListener("click", () => {
            moreBtn.remove();
            const frag = document.createDocumentFragment();
            const anchor = messagesEl?.firstChild || null;
            // Temporarily redirect appendMsg output by rendering then moving:
            // simplest correct approach — render into the live container and
            // reinsert before the previously-first node in order.
            const beforeCount = messagesEl ? messagesEl.childNodes.length : 0;
            hidden.forEach(renderOne);
            if (messagesEl && anchor) {
              const added = [];
              for (let i = beforeCount; i < messagesEl.childNodes.length; i++) {
                added.push(messagesEl.childNodes[i]);
              }
              added.forEach((n) => frag.appendChild(n));
              messagesEl.insertBefore(frag, anchor);
            }
            void syncToolDiffPreviews();
          });
          messagesEl?.appendChild(moreBtn);
          shown.forEach(renderOne);
        } else {
          list.forEach(renderOne);
        }
        void syncToolDiffPreviews();
        break;
      }
      case "queueUpdate":
        sendQueue = Array.isArray(msg.items) ? msg.items : [];
        renderSendQueue();
        break;
      case "turnStart":
        beginTurn();
        break;
      case "userEcho":
        appendMsg("user", msg.text, {
          uiIndex: typeof msg.uiIndex === "number" ? msg.uiIndex : undefined,
          files: Array.isArray(msg.files) ? msg.files : undefined,
        });
        break;
      case "reasoningDelta": {
        const body = ensureReasoning();
        if (body && typeof msg.content === "string") {
          body.textContent = (body.textContent || "") + msg.content;
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "delta":
      case "deltaSync": {
        if (busy) {
          busyStatusText = i18n("turn.writing");
          updateBusyUi();
        }
        const bubble = ensureAssistant();
        if (bubble && typeof msg.content === "string") {
          if (msg.type === "deltaSync") {
            streamRawText = msg.content;
          } else {
            streamRawText += msg.content;
          }
          scheduleAssistantMarkdown(
            bubble,
            sanitizeAssistantStream(stripFinalEnvelope(streamRawText))
          );
          if (messagesEl) {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }
        }
        break;
      }
      case "discardAssistantBubble": {
        flushAssistantMarkdown();
        if (assistantBubble) {
          assistantBubble.remove();
          assistantBubble = null;
        }
        streamRawText = "";
        break;
      }
      case "attachmentPreview":
        if (msg.path) {
          openExternalFile(msg.path, false);
        } else {
          appendMsg("system", `Cannot open attachment: ${msg.name || "file"}`);
        }
        break;
      case "childLifecycle":
        handleChildLifecycle(msg);
        break;
      case "diffViewer":
        showDiffViewer(msg.path || "", msg.before || "", msg.after || "", msg.language || "");
        break;
      case "highlightResult": {
        const cb = highlightWaiters.get(msg.requestId || "");
        if (cb) {
          highlightWaiters.delete(msg.requestId || "");
          cb(Array.isArray(msg.lines) ? msg.lines : []);
        }
        break;
      }
      case "toolBlock":
        handleToolBlock(msg);
        break;
      case "execChunk":
        if (typeof msg.step === "number" && typeof msg.chunk === "string") {
          appendExecChunk(msg.step, msg.chunk);
        }
        break;
      case "todosUpdate":
        todos = Array.isArray(msg.todos) ? msg.todos : [];
        renderTodos();
        break;
      case "stepUsage": {
        const u = msg.usage || {};
        if (msg.scope !== "child") {
          // Context gauge tracks the main agent only; worker windows differ.
          ctxState.prompt = typeof u.prompt_tokens === "number" ? u.prompt_tokens : 0;
          ctxState.completion = typeof u.completion_tokens === "number" ? u.completion_tokens : 0;
          ctxState.estimated = u.source === "estimate";
          if (Array.isArray(u.breakdown) && u.breakdown.length > 0) {
            ctxState.breakdown = u.breakdown;
          }
          renderContextUi();
        }
        // Live spend: each finished LLM call (planner step, worker step)
        // reports its cost here — update the chip mid-turn instead of
        // waiting for the end-of-turn turnUsage summary.
        if (typeof u.cost_usd === "number" && u.cost_usd > 0) {
          turnCostAccum += u.cost_usd;
          renderCostChip();
        }
        break;
      }
      case "turnUsage": {
        // Server total is authoritative — drop the live per-step estimate.
        turnCostAccum = 0;
        lastTurnUsage = msg.usage || null;
        if (typeof msg.sessionCost === "number" && msg.sessionCost > 0) {
          sessionCostUSD = msg.sessionCost;
        } else if (lastTurnUsage && (lastTurnUsage.cost_usd || 0) > 0) {
          sessionCostUSD += lastTurnUsage.cost_usd;
        }
        renderCostChip();
        appendTurnCostNote(lastTurnUsage);
        break;
      }
      case "credits": {
        creditsInfo = {
          supported: msg.supported === true,
          provider: msg.provider || "",
          balance: typeof msg.balance === "number" ? msg.balance : 0,
        };
        renderCostChip();
        break;
      }
      case "contextInfo": {
        const info = msg.info || {};
        if (typeof info.contextLimit === "number" && info.contextLimit > 0) {
          ctxState.limit = info.contextLimit;
        }
        if (typeof info.maxResponseTokens === "number" && info.maxResponseTokens > 0) {
          ctxState.maxResponse = info.maxResponseTokens;
        }
        renderContextUi();
        break;
      }
      case "workflowStage": {
        const st = msg.stage || {};
        const stageId = st.stage_id || st.stageId || "";
        if (msg.phase === "start") {
          if (st.name && st.name !== workflowActiveName) {
            workflowActiveName = st.name;
            workflowStages.clear();
          }
          setWorkflow(st.name || "workflow", true);
          if (stageId) {
            workflowStages.set(stageId, {
              id: stageId,
              name: stageId,
              state: "running",
              attempt: st.attempt || 0,
            });
            renderWorkflowStages();
          }
        } else if (msg.phase === "done" && stageId) {
          const slot = workflowStages.get(stageId) || {
            id: stageId,
            name: stageId,
            state: "pending",
            attempt: st.attempt || 0,
          };
          const action = (st.action || "").toLowerCase();
          if (action.startsWith("redo")) {
            slot.state = "redo";
          } else if (action === "fail") {
            slot.state = "fail";
          } else {
            slot.state = "done";
          }
          workflowStages.set(stageId, slot);
          renderWorkflowStages();
        }
        break;
      }
      case "healthStatus":
        setLspStatus(msg.lspStatus || "");
        break;
      case "systemNote":
        appendMsg("system", msg.text || "");
        break;
      case "skillsList":
        SKILL_CMDS = (Array.isArray(msg.skills) ? msg.skills : []).map((s) => ({
          cmd: "/" + (s.name || ""),
          desc: s.description || "",
        }));
        break;
      case "mentionResults": {
        const hit = inputEl ? detectPaletteQuery(inputEl.value) : null;
        if (!hit || hit.mode !== "mention" || hit.query !== (msg.query ?? "")) {
          break;
        }
        renderPalette("mention", Array.isArray(msg.files) ? msg.files : []);
        break;
      }
      case "tool": {
        const toolName = msg.toolName || "tool";
        const line = msg.done
          ? `✓ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`
          : `→ ${toolName}${msg.detail ? `: ${msg.detail}` : ""}`;
        appendMsg("tool", line);
        assistantBubble = null;
        break;
      }
      case "error":
        appendMsg("error", msg.message || "error");
        break;
      case "turnComplete":
        flushAssistantMarkdown();
        finalizeReasoningSummary();
        if (toolTraceEl) {
          toolTraceEl.open = false;
        }
        if (!msg.queuedNext) {
          setBusy(false);
        }
        assistantBubble = null;
        resetTurnState();
        if (!msg.ok) {
          setChromeHint(i18n("turn.failed"), true);
        }
        break;
      case "filesPicked": {
        if (Array.isArray(msg.files)) {
          for (const f of msg.files) {
            addFileRef(f);
          }
        }
        break;
      }
      default:
        break;
    }
  });

  /**
   * Small grey transcript note with the turn cost. In orchestra mode (or any
   * multi-model turn) each model gets its own "model $cost · N×" segment so
   * it is clear what each tier position cost.
   */
  function appendTurnCostNote(usage) {
    if (!messagesEl || !usage) {
      return;
    }
    const cost = typeof usage.cost_usd === "number" ? usage.cost_usd : 0;
    if (cost <= 0) {
      return;
    }
    const entries = Array.isArray(usage.entries) ? usage.entries : [];
    const parts = ["turn " + formatUsd(cost)];
    // Prompt-cache hit, when the provider reports one (Anthropic via a
    // gateway or native). Local models never set this. Without it, a long
    // Anthropic-through-OpenRouter turn that re-billed its whole transcript
    // every step looked the same as one that cached it — this is what would
    // have shown the field run's $2.18 turn was paying full price.
    const cached = typeof usage.cached_prompt_tokens === "number" ? usage.cached_prompt_tokens : 0;
    const promptTotal = typeof usage.prompt_tokens === "number" ? usage.prompt_tokens : 0;
    if (cached > 0 && promptTotal > 0) {
      parts.push("cache " + Math.round((cached / promptTotal) * 100) + "%");
    }
    if (entries.length > 1 || (modeId === "orchestra" && entries.length > 0)) {
      entries.forEach((en) => {
        const calls = typeof en.calls === "number" && en.calls > 1 ? " ×" + en.calls : "";
        parts.push(shortModelName(en.model) + " " + formatUsd(en.cost_usd || 0) + calls);
      });
    }
    const el = document.createElement("div");
    el.className = "usage-note";
    el.textContent = parts.join("   ·   ");
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  document.addEventListener("keydown", (e) => {
    if (!diffReviewActive()) return;
    const n = pendingState.diff.length;
    if (!n) return;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      diffReviewCursor = Math.max(0, diffReviewCursor - 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      diffReviewCursor = Math.min(n - 1, diffReviewCursor + 1);
      renderPendingReviewList();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      applyPendingChanges();
      return;
    }
    // Per-file decisions: keep this one, throw this one away. Everything else
    // in the turn stays pending, which is the whole point of the review list.
    if (e.key === "a" || e.key === "A") {
      e.preventDefault();
      settleSelectedPendingFile(true);
      return;
    }
    if (e.key === "x" || e.key === "X") {
      e.preventDefault();
      settleSelectedPendingFile(false);
    }
  });

  // The environment's own language is the starting point, so the first frame
  // is already right for most people; the host may correct it (a VS Code
  // setting, a saved choice) with a "uiLang" message straight after.
  setUiLang("");
  applyStaticI18n();
  initModeMenu();
  initEffortMenu();
  initAccessMenu();
  syncModeUi();
  syncEffortUi();
  syncAccessUi();
  renderContextUi();
  autoGrow();
  host.postMessage({ type: "ready" });
})();
