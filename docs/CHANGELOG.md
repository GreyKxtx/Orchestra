# Changelog

All notable changes to Orchestra are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — vNext

### Changed — less to carry (2026-09)

- **What nothing reaches is gone.** 126 functions no code path reached, four shim files, the v0.3 file cache (`patch/cache`; `patch/fsutil` keeps its two hash helpers), `--no-daemon`, `cli/metrics.go` and the `plan_enter` stub are removed. What remains unreachable is test infrastructure. The docs name the packages that exist.
- **One atomic writer.** The applier, the worktree registry, the prompt override and the instrumenter each wrote a temp file and renamed it their own way; all of them go through `fsutil.AtomicWriteFile`, and an import-rules test keeps it so. `orchestra model` edits `.orchestra.yml` under the config's lock. A `plan.json` that could not be written is the command's error instead of a silent "Plan saved to:".
- **Stores that stop growing.** `retention:` in `.orchestra.yml` — `sessions` (200), `session_max_age_days`, `patches` (50); `-1` lifts a bound. Session snapshots are pruned on `session.start`, sessions idle for half an hour leave the core's memory, the patch directory keeps its newest exports, `decisions.md` archives its older half past 512 KB, the lessons signal logs keep their tail. A session's event log rotates past 32 MB and a payload past 1 MB is recorded as a truncation marker; one over-long line no longer ends recording for the session.
- **One refresh per grant.** Two Orchestra processes sharing an OAuth grant both refreshed it; with a rotating refresh token the second one got `invalid_grant` and was logged out. A refresh now takes the token file's lock and adopts the token another process already refreshed.

### Changed — the code graph keeps up without getting in the way (2026-09)

- **A refresh costs what changed.** Every explore and every agent run — every child of a fan-out — refreshed the code graph under its write lock, and the refresh hashed every file and re-resolved every dangling call edge with up to three queries each: on Orchestra's own tree 3.2 s per pass on an unchanged tree, 49 s for the first index. The graph now keeps each file's mtime and size beside its hash and reads only files whose stamp moved; a pass relinks only the edges that name a symbol it inserted, in one transaction; the walk and the parsing run outside the write lock, which is taken for the writes alone; the database runs in WAL mode so the TUI, the extension and ckg-ui read while a refresh writes. Step-1 context answers from the graph as it is and refreshes it behind the answer. On this repository: empty refresh 45 ms, one changed file 169 ms, first index 41 s with readers held out for 14 s of it.
- **explore answers in milliseconds.** The one-hop queries behind it scanned every edge on every hop; both lookups are index lookups now, and a test reads the query plan to keep them so. Depth-2 explore p95: 303 ms → 7 ms. `grep` enriches the matches it returns instead of every match in the tree, and `rg` is killed when the turn is cancelled.
- **semantic_search follows the tree, and a query decodes nothing.** Vectors are cached under the hash of the symbol's text, so a re-index sends the endpoint only what changed; a symbol spanning a generated file no longer fails the pass (input capped at 8 KB); the model's vectors live in memory as one normalized matrix and a query is a pass of dot products into a bounded heap — every query used to decode every vector from the database; and a refresh that changed the graph runs an embedding pass behind it, where the index used to be filled once at warmup.
- **Language servers stop drifting.** Dropping a turn's staged edits closes their documents in the servers, which kept answering from drafts that no longer existed; past 64 open documents the least recently used one is closed.

### Changed — one wire contract, a handshake by range (2026-09)

- **The client↔core contract has one definition** (`protocol/wire`). Every client kept its own copy of the JSON shapes by hand — the TUI declared 28 types again, the VS Code extension mirrored the TUI, the web mirrored the extension — and the core built its notifications as untyped maps, so nothing held the copies together. The names of every method, notification and request, the params and results of the methods, and the payloads of `agent/event` and `exec/output_chunk` are Go structs now: the core answers and notifies with them, the TUI and the CLI import them, and the TypeScript for the extension (`ui/vscode/src/protocol/wire.generated.ts`), the constants for the web page and a JSON Schema are generated from the same source, with a test that fails while they are stale. The 25 types that carry an internal op, a patch, a config or a session snapshot stay in `internal/core`; `docs/PROTOCOL.md` describes them.
- **A client and a core one release apart connect** (ProtocolVersion 24). `initialize` used to compare three numbers for equality, so an extension one commit behind the core could not connect at all — and `tools_version`, which moves with tools no client calls, failed the handshake the same way. The client now names the range of protocol versions it speaks, the core answers with the newest both have, and `tools_version` is informational: the answer carries the core's. The support window is one version. `core.health` reports the range before `initialize`, so the TUI, the extension and the web pick the version to ask for; a core from before v24 names its own version in the refusal, and a client that still speaks it asks for exactly that. The answer also lists the core's capabilities — the methods, notifications and requests it serves — so a client checks for the feature it needs by name.
- **Typed events.** `agent/event` is `wire.AgentEvent` everywhere it is made: the stream events, `mode_route`, the child lifecycle, agent messages, relayed work orders and integration verdicts. Pending ops and usage go out as their structs. Same fields and values as before; a field with no value is absent rather than empty.

### Fixed — the LLM pipeline (2026-09)

- **A stream cut off is an error, not the answer.** An OpenAI-compatible stream that ended without `[DONE]` and an Anthropic stream without `message_stop` were taken as the model's whole answer — half a sentence, or a tool call with its arguments cut short. With neither a terminal event nor a stop reason the stream is now a retryable error, and the agent retries the step even after text has streamed (the client is told the partial answer was dropped). The stop reason travels with every answer (`end`, `max_tokens`, `tool_use`, `filtered`); an answer cut at `max_tokens` is never taken — the lenient JSON repair used to make one parse — and the retry says why. A server that ignores `stream: true` is read as one ordinary completion; its words were dropped.
- **Retries that listen.** 429s wait out the `Retry-After` / `retry-after-ms` the server sends (a request for more than a minute is not waited for inside a step). The Anthropic client now retries overloaded and 5xx answers and has the same stall watchdog as the OpenAI-compatible one. A client marks the error it already retried, and the agent does not retry it again — that made up to nine requests of one step. Compaction runs under the step timeout, and the auto-router under a 30s limit after which the heuristic decides.
- **Anthropic thinking works with tools.** Thinking blocks and their signatures were streamed to the UI and dropped; on any model that thinks — every Claude 5 model does by default — the step after a tool call was a 400. They are kept with the history and sent back unmodified. Claude 4.7 and later reject `{type:"enabled", budget_tokens}` and 4.6 deprecates it: from 4.6 on the client sends `{type:"adaptive"}` and the effort level, and those models get a 32k default `max_tokens`. Images — screenshots, MCP images, `--image` — reach Anthropic as image blocks, and the query text that rode along with them does too.
- **The request log for every provider.** With `provider: anthropic`, `llm_log.jsonl` stayed empty: the logger was reachable only through the concrete OpenAI-compatible client. Callers now ask the client stack (`llm.LoggerOf`, `ContextTokensOf`, `DiscoverLimits`). A model switch at runtime (`runtime.set_model`) no longer drops the log, the fallback provider and the router for the rest of the process.
- **Edits.** The resolver's anchor passes check the lines between their anchors: a search block with a different body between two unique lines replaced that body silently. A wrapped final the schema refused no longer loses its patches. A staged `edit` checks the `file_hash` it is given. A dry run's `grep` searches the staged files as `read` and `edit` see them.
- **Turns.** `session.message` refuses a mode a turn cannot run in, as `agent.run` does. `MaxFinalFailures` trips on a loop of failed finals — a successful read no longer resets it, and refused finals count. `general` and `explore` run as the main agent end without `task_result`.

### Added — a run survives its core (2026-09)

- **`agent.run` keeps a checkpoint and can be resumed** (ProtocolVersion 23). A turn used to live only in memory: a core killed mid-way took every finished task's result and every staged edit with it. Now `.orchestra/runs/<run_id>.checkpoint.json` is rewritten atomically after each step of the top-level agent and each change of its task graph. It holds the history, the turn's staged edits with the disk version each was made against, and the graph. `agent.run {resume: <run_id>|"last"}` — `orchestra apply --resume last` — puts it back: the staged edits return, finished tasks keep their results without running again, interrupted ones start again under the ids their spawner is waiting on, and the agent goes on from its last completed step with a `<resume_notice>` of what came back. A run keeps its own query, mode and `apply`; consent comes only from the resuming request, since a checkpoint is a file in the project. The result carries `run_id`; a failed `orchestra apply` prints the command that resumes it.

### Added — the agent can look at the page you are on (2026-09)

- **`browser.*` act on the Browser view, not on a browser of their own** (ProtocolVersion 21). Until now those ten tools started `npx @playwright/mcp` — a second browser, its own profile, nothing signed in. A turn sent with `browser_panel: true` gets the panel instead: the page the person is actually looking at, with their session. The core reaches it through a server-initiated request (`browser/call`) on the connection that asked for the turn, which is the same rail `question/ask` and `permission/request` ride. Deliberately not an MCP server in the shell and not the devtools port straight from the core: either would let a model drive a logged-in browser with no window open to show what it was doing.
- **What the model is shown** — `snapshot` is the page's accessibility tree, flattened to one line per element with a `[ref]` to address it by, with the text a run is laid out in and the labels a node already carries dropped: Google's home page is 34 lines rather than hundreds. `screenshot` is the page as a PNG. Read-only this far; a ref belongs to the panel that made it and means nothing to any other browser.
- **Three permissions, not one** — looking at the page, acting in it (`allow_browser_drive`: click, type, fill, select, navigate) and running the model's own script in it (`allow_browser_eval`) are asked for separately in the composer's access menu, and script is off even when driving. They gate the panel alone: a browser the core starts for itself is empty and ours, and `--allow-browser` still means all ten tools against it. Both sides check — the core, and the window that owns the browser, which is the half that cannot be talked out of it.
- **A click is a click.** The panel finds the element's box and dispatches a real press where it is, so a page listening for a press, a hover or a focus gets what it is listening for; typing focuses the field and inserts text, then fires `input` and `change` for the frameworks that watch the property. `browser.close` is refused: the view belongs to the person, not to the tools.
- **Edit, reload, look** — `browser.navigate` to the page already open reloads it past the cache and answers once the page says it has loaded, so `browser.screenshot` on the next step shows the edit rather than the old paint or a white frame. It is also the one op that works before there is a page: it opens the Browser view and brings it up, because a browser being driven should be one the person can see.
- **Nothing it does is invisible** — while an op is in flight the stage is outlined in the mode accent (dashed while acting) and the toolbar says whether the agent is looking or acting.
- **A refusal is an answer.** The view being closed, consent taken back mid-turn, or an op above what the turn was granted comes back as the tool's error with the reason in it — never as silence, which would be a tool waiting for the rest of the turn.

### Added — the Browser view, with an element picker and a console (2026-09)

- **A fourth segment beside Chat, Trajectory and Graph** (`ui/web/src/65-browser-panel.js`, `ui/desktop/src-tauri/src/browser.rs`) — a real browser inside the desktop app. The page draws the chrome and leaves a stage empty; what fills it is a child webview the shell keeps over that rectangle (`Window::add_child`, tauri's `unstable` feature). Not an iframe: the sites worth inspecting refuse to be framed. Leaving the view hides the webview rather than closing it, so coming back does not reload the page.
- **The eyedropper** (`ui/desktop/picker.js`) — armed from the toolbar, it highlights what the mouse is over and turns a click into an attachment in the composer: the element's markup and the CSS rules that apply to it, which is what F12 shows. The injected script is handed no token, no core address and no IPC bridge — it shares a world with the page's own scripts, so arming is an `eval` and collecting is a poll, and what comes back is text the user still sends by hand.
- **The address bar is a search box** — anything that is not somewhere to go is looked up with the chosen engine (Google, Bing, DuckDuckGo, Yandex). `localhost:5173` is a host and a port, not a scheme; covered by a test.
- **The browser's own developer tools, docked beside the page** — the `>_` button puts the real Chromium DevTools — the ones behind F12 in Chrome and in Edge, which WebView2 is — in a pane to the right of the stage: Elements with the live DOM, Console, Network, Sources, everything. Not the separate window WebView2 opens by itself, and not a hand-drawn copy of a few of its panes. Their frontend is a page served by the browser process the panel runs in, so it is shown in a webview of ours over the dock's rectangle; the page gives up the width rather than being covered, because both are OS views and nothing drawn here can sit on top of them. They open on the DOM, without Edge's welcome tab and without the picture of the page beside it that a remote target otherwise gets: the page is right there to the left, so that picture only costs width. They are painted in this app's colours, light or dark as the window has it: a painter inside their page reads every colour they define as a token — Chromium's and Edge's own layer over them — and restates it, a grey as our grey of the same depth, the browser's blue as our accent; errors, diffs and syntax keep their colours. Colour only: their type, icons and the arrangement of the panels stay Chromium's. A menu opened over the page no longer takes the page away: it leaves its picture behind for as long as the menu is open, and a menu over the tools leaves the page alone. The line between the two is dragged to share the width, and where it is left is remembered; the panes step aside for the drag, since the mouse belongs to whichever view is under it.
- **The panel has a WebView2 profile of its own**, which is what makes that safe: the tools need a debugging port, a debugging port drives every page of its environment, and the page holding this app's capabilities must not be one of them. What is reachable on it is the site being browsed, from this machine only. Two more things it needs: `--remote-allow-origins` for its own address, or the protocol server answers 403 to the frontend's WebSocket, and the same arguments on every webview of that profile, or WebView2 refuses the second one.
- **Superseded: the page's own developer tools** — the `>_` button opens WebView2's F12 for the page (elements, console, network) in a window of its own; a drawer of ours was a lesser copy. The picker's console tap stays in the page (`drainLog`), for the day a page's log is handed to the model.
- **Popups step over the webview** — the webview is an OS view over the page, so nothing drawn here can appear on top of it; while a menu or the bar's list is open the webview is hidden and comes back untouched when it closes. The stage's rectangle is sent in physical pixels (`devicePixelRatio`), so the display's scaling and the app's own zoom no longer put the webview off its stage.
- **A ⋯ menu** — screenshot of the page or of a dragged area (both land in the composer as PNG), reload past the cache, copy the address, open the page in one of the machine's own browsers (read out of the registry), zoom, the two choices above, and clearing cookies, cache or the site's data. WebView2 has no API for most of these: they are devtools-protocol calls (`browser_cdp`). Clearing the whole profile is deliberately not offered; the three above cover what a page leaves behind.
- **Saved links** — a bookmark menu beside the reload button: the page open now is kept with one
  click and comes back with another. Newest first, one row per address, at most 60, in this
  browser's own storage beside the theme and the language.
- **The bar offers what it knows** — typing into the address bar drops a list under it: the saved
  links first, because they were kept on purpose, then where the panel has been (the last 200
  addresses). Arrows walk it, Enter takes the highlighted row or the typed text, and a query that
  is not an address is offered as a search with the chosen engine.
- **`ORCH_DEBUG_PORT=<port>`** opens WebView2's remote debugging on loopback for the whole shell, so a
  script can drive it the way a person would and look at the result with `PrintWindow` — which is
  how the resize, the menu and the bar's list above were checked. WebView2 runs one browser process
  per user-data folder and refuses a second environment with different arguments, so the panel is
  built with exactly the main window's (`browser::browser_args`); with the flag on only one side its
  controller never came up and the stage stayed dark.
- **Every command is `async`** — a plain Tauri command runs on the main thread and creating a webview waits for that same thread, so the first version deadlocked the window on open.

### Fixed — one parallel step is one step for the breaker; children log their tools (2026-09)

- **A parallel tool batch moves the consecutive-error counter once** — `CircuitBreaker.RecordToolErrorBatch`. Every failed call in a batch used to count as a separate consecutive error, so a Lead that read nine files in one step to see which existed yet (six NotFound) tripped the limit of six on that single step and the run stopped. Each failure is still classified into the log; a batch with any success resets the counter, as a serial success does.
- **The Lead sizes the job before it spawns anything** — `orchestra.txt` now opens the phase machine with a choice: a brief that names its own deliverables and fits a handful of files is declared `phase: execution` with `waivers: [prd, contract]` and goes straight to workers; a new product or a goal it cannot restate in one sentence runs the full `discovery → … → delivery`; unsure asks. The depth of ceremony is the model's call, not a mode the user configures. No runtime change was needed — the quick shape already passes every guard, now held by `TestQuickShape_ExecutionWithWaiversAdmitsAWorker` — but the `prd` / `contract` refusals no longer call them "user waiver", since the Lead grants those itself.
- **`grep` accepts a bare string for `paths` / `exclude_dirs`** — `fs.PathList`; a Lead wrote `"paths": "internal/api"` and the whole search was lost to "cannot unmarshal string into Go struct field". A number is still an error.
- **`acceptance_checks[]` accepts bare command strings** — `AcceptanceCheck.UnmarshalJSON`; a string is a command expected to exit 0. The prompt lists the field without a shape and a 27B Lead wrote `["go build ./..."]`, which refused two whole WorkOrders.
- **Children write tool_call / tool_result to llm_log.jsonl** — `tasks.ChildAgentConfig.AgentLogger`, set by `apply` and core. A worker's LLM requests were logged and nothing it did with the answers.
- **Nested-directory instructions are not the root's twice** — `Runner.discoverInstructions` only credits a directory with a file that is in it; `LazyOrchestraFile` walks up on its own, so a read under `internal/api/` injected the root ORCHESTRA.md under a label naming a file that did not exist, then again under its own.

### Fixed — a 27B Lead can keep its workers alive and its history sendable (2026-09)

- **`agent.child_timeout_s` (default 600)** — the child lifetime and the sync `task` wait when the model omits `timeout_ms` were hard-coded to 120 s. A worker on a local 27B model spends 20-100 s per step, so every worker that read two files before writing one was cancelled mid-write and the Lead respawned it into the same wall: eight children lost in one 50-minute run. `agent.Options.ChildTimeoutMS` carries the value; the `task` / `task_spawn` schemas and `task.txt` say ten minutes.
- **Tool arguments that are not JSON are wrapped on the wire** — `llm.ToolArguments.MarshalJSON` sends `{"_invalid_json": "<text>"}` for arguments that do not parse (a garbled or cut-short tool call). vLLM's Qwen chat template json-loads every historic tool call, so one broken entry made every later request fail with HTTP 400 "Unterminated string" and the run could never recover. `Raw()` still hands the tool the original text, so the model still gets "invalid input".
- **An expired context is not "LLM Endpoint unreachable"** — `llm.IsUnreachableError` returns false for `context.DeadlineExceeded` / `context.Canceled`; a child whose lifetime ran out mid-POST reported the server down while it was idle.

### Fixed — cache counters reach the UI on the streaming path (2026-08)

- **`Agent.emitStepUsage` now fires on the streaming path too** — it was gated on `!canStream`, and core synthesised a `step_usage` notification from `StreamEventDone` instead, rebuilding the payload field by field. That hand-built copy never learned about `cached_prompt_tokens` / `cache_write_tokens`, so the prompt-cache observability added with the cache work was invisible on the path every interactive run takes. The synthesis in `core/agent_events.go` is gone (it would now only duplicate the agent's own event) and `rpcclient.UsageTurnPayload` carries the two counters.
- **Wire-level tests** — `llm/anthropic_wire_test.go` inspects the JSON that actually leaves the Anthropic client (system/tools/rolling cache breakpoints, alternating roles, cache counters through the SSE parser); `tests/e2e_agent/e2e_prompt_cache_wire_test.go` drives a real `OpenAIClient` over httptest for four steps and compares the serialized request prefixes. The second one is what caught the defect above.
- **`TestCompactThresholdRoundTrip`** covers the `0 = auto` / `-1 = disabled` sentinel across Save→Load→Save.

### Changed — family tuning reaches every mode, and children resolve their own (2026-08)

- **Shared family addendum** (`prompt/files/addendum-local.txt`) — family-specific prompts existed only for build (`build-anthropic/gpt/gemini/kimi/local`) and plan, so a worker, verifier or debug run on a local model got the family-neutral text; `build-local.txt`, the most detailed prompt in the set, was lost the moment the agent left build mode. A mode without its own `{mode}-{family}.txt` now gets a short shared addendum appended. Internal single-shot contracts (compaction/title/summary) are excluded — tool discipline does not apply to them.
- **Child agents resolve their prompt family from their own model** — `internal/tasks` never set `PromptFamily` at all, so every subagent used the neutral prompt regardless of the tier model it was running on. Worker, verifier (`worker_verify.go`) and workflow stages (`stageinvoke`) now resolve it.

### Changed — prompts are English throughout (2026-08)

- **20 prompt files translated** — the set was split down the middle: build\*, plan\*, explore, ask, debug, general, architecture, verifier, compaction, summary, title and the reminders were Russian (50-100% Cyrillic) while orchestra, worker, product, documentation, task, todowrite and auto-router were English, so one pipeline (Lead → worker → verifier → compaction) switched language twice. A prompt's language also pulls the model's output with it — Russian comments and commit messages in an English codebase — and Cyrillic costs more tokens on the local tokenizers these prompts target. Markers and placeholders are unchanged (`{{PLAN_PATH}}`, `## VERIFICATION PASSED/FAILED`, `Context Manager`, `{"patches":[]}`). Guarded by `TestPromptFilesAreEnglish`.

### Changed — tool catalog only where the model needs it (2026-08)

- **`<available_tools>` is now family-conditional** (`agent.needsToolCatalog`) — the block restates `tools[]` in prose for models that under-use the schema, and on build mode it is ~5 KB, 2.5× the base prompt, on top of ~32 KB of schemas already on the wire. Anthropic / GPT / Gemini / Kimi skip it (build-mode system prompt 6.7 KB → ~1.9 KB); local and unknown families keep it, as does anything with an unset family.
- **Prompt files no longer point at a block that may be absent** — build-anthropic/gpt/gemini/kimi, build.txt, explore, general and plan referred the model to `<available_tools>`; those that can run with a catalog-free family now say `tools[]`. Guarded by `TestPromptsDoNotReferenceAbsentCatalog` over every mode × family pair.

### Changed — exec consent no longer implies commit/push/PR rights (2026-08)

- **`appendExecTools` split** (`internal/tools/registry.go`) — one `caps.Exec` flag used to advertise `bash` *and* `git.commit/branch/checkout/push`, worktree management and `gh.pr.create` together. Command execution and read-only GitHub queries stay under `appendExecTools`; the repo-mutating set moved to `appendRepoMutatingTools`, which only build, debug, general and the maximal `ListTools` surface use.
- **Subagents lost git-mutating tools** — worker (31 → 22 tools) and verifier (30 → 22). Children share the parent's `tools.Runner` and therefore its working tree, so `git.checkout` in one child switched the branch under its siblings; and verifier's own prompt and reminder say read-only while its schema offered commit and push. Both keep `bash`, `git.diff`/`git.status` and the read-only `gh` queries.

### Fixed — prompt/mode wiring (2026-08)

- **`{{PLAN_PATH}}` no longer leaks to the model** — `buildSystemPrompt` substituted only for `ModePlan` or an explicit `PlanPath`, so architecture mode shipped the literal placeholder while its own reminder showed the resolved path. Substitution now runs for every mode.
- **One mode registry** (`config.builtInAgentModes` → `map[string]ModeKind`) — `product` and `documentation` existed as real modes with their own tool sets and write scopes but were missing from the reserved-name list, so a custom agent or skill could take those names and shadow them. `agent.IsKnownMode` now derives from the same registry instead of a second hardcoded list.
- **Child-only modes are refused top-level with the reason** — `--mode worker|verifier|product|documentation` and the equivalent `agent.run` used to report "unknown agent mode"; they now say the mode runs only as a subagent and list the selectable ones (`config.IsUserSelectableMode`).
- **`.orchestra/system.txt` no longer overrides child-only prompts** — an override written for build mode silently replaced the worker/verifier prompt that carries the WorkOrder + `task_result` contract. Per-mode `.orchestra/system.<mode>.txt` is the supported way to override those.

### Changed — a failed subagent reports what it got done (2026-08)

- **`FormatSubagentFiles` / `FormatSubagentProgress`** (`internal/agent/history/subagent.go`) — the child's history was discarded on failure, so the Lead saw an error string and redid the task from nothing. A failing child's error now carries the files it touched (with the tools used on each), the findings it had, and what it was doing last. The success summary for explore subagents gains the same "Files touched" section.

### Changed — first-step token estimate follows the script of the prompt (2026-08)

- **`detectBytesPerToken`** (`internal/agent/context_estimate.go`) — before the provider reports any usage, bytes-per-token is estimated from the prompt itself: a predominantly non-ASCII prompt (≥30% non-ASCII bytes — Cyrillic, CJK) uses 3 instead of the Latin-shaped default of 4. Applies to step 1 only; `calibrateFromRealPrompt` supersedes it from the first response on, and an explicit `agent.bytes_per_context_token` still wins when it is more pessimistic.

### Changed — the cheap model does the compacting everywhere (2026-08)

- **Child agents, pipeline stages and workflow stages now inherit the compaction client** (`tasks.ChildAgentConfig`, `pipeline.Options`, `stageinvoke.Config`) — only the top-level agent got `llm.router.fast_provider` / `providers.fast`, so every worker, stage and skill run paid the main model to summarise its own transcript. CLI paths share one resolver, `compactionClientFor`, mirroring `Core.compactionClientWithContext`.
- **`llm.ContextTokensFromConfig`** now resolves a model preset and falls back to the static model catalog instead of only reading `extra_body.num_ctx` — a cloud fast-provider reported 0, so the compaction corpus was sized against the *main* model's window and could overflow the smaller model answering it.

### Changed — failed edits explain themselves (2026-08)

- **`nearest` region on StaleContent** (`patch/resolver/nearest.go`) — a search block that matches nothing now comes back with a numbered excerpt of the file text it most resembles (Levenshtein line similarity, ±6/10 lines, ≤1.2 KB), so the model can fix the block instead of re-reading the whole file. Silent when nothing in the file resembles the search.
- **Tool errors keep their structured detail** (`internal/agent/tool_parallel.go`) — `formatToolErrorJSON` dropped `protocol.Error.Data` entirely, so the resolver's nearest-region excerpt and the ambiguous-match line numbers never reached the model. Details are now passed through with per-value clipping.
- **`ApplyErrorCompact`** appends the nearest region to the StaleContent hint on the final-patch path.

### Changed — truncation leaves a visible gap marker (2026-08)

- **`TruncateMessages` no longer drops the middle silently** (`internal/agent/history/history.go`) — it inserts a `[history trimmed: N earlier step(s) …]` user message naming the dropped step count and the files those steps touched, plus an explicit "re-read before editing" instruction. Markers replace each other instead of stacking, and the byte budget reserves room for one.

### Changed — prompt cache: stable prefix + Anthropic breakpoints (2026-08)

- **Volatile context moved behind the history** (`internal/agent/agent_step.go`) — todos, `<working_state>`, turn digests and the mode reminder were rebuilt into the *leading* user message on every step, so the common prefix broke at message #2 and no provider prompt cache could match past the system block. They are now appended after the history, leaving a stable append-only prefix (and landing in the freshest part of the attention window).
- **Anthropic cache breakpoints** (`llm/anthropic.go`) — added `cache_control` on the last tool schema and a rolling breakpoint on the last message before the volatile tail, next to the existing system-block marker. `convertToAnthropic` now folds a trailing user message into the preceding one (the API requires alternating roles).
- **Cache observability** — `TokenUsage.CachedPromptTokens` / `CacheWriteTokens` parsed from Anthropic `cache_read_input_tokens` / `cache_creation_input_tokens` and OpenAI-compatible `prompt_tokens_details.cached_tokens`; both are emitted in the per-step usage event. Previously a cache hit was invisible.
- **Docs** — `docs/architecture/prompt-cache.md`.

### Changed — agent working memory: real context window, later & partial compaction (2026-08)

- **Static model-window catalog** (`llm/model_context.go`, `llm.ResolveModelLimits`) — cloud providers do not report a window through `/v1/models`, so the history budget silently fell back to the flat `limits.context_kb` (128 KB ≈ 30k tokens) even on a 200k-token model. Discovery now falls back to a per-family catalog (Claude, GPT, Gemini, DeepSeek, Grok, Kimi, Mistral, Qwen, Llama…).
- **`apply` resolves the window too** — the direct CLI path never called discovery; only `core` did. Both now go through `llm.ResolveModelLimits`.
- **`compact_threshold_pct: 0` = auto** — the trigger scales with the window (60% under 32k, 75% under 100k, 85% above) instead of a fixed 60%. A positive value still pins it; `-1` disables compaction.
- **Compaction keeps the recent tail verbatim** (`splitHistoryForCompaction`) — it summarises only the older history and carries over the tail (30% of the prompt budget, min `history_prune_keep_recent` tool atoms, capped at half the history). Previously the whole transcript was replaced by a summary plus 2 tool atoms.
- **`history_prune_keep_recent` default 2 → 6**, **`child_max_steps` default 12 → 24** — a worker that reads, edits and then validates its own change ran out of steps mid-task.

### Added — Orchestra Lead token budget, CKG v5, LLM fail-fast (2026-08)

- **Lead tool isolation** — `listToolsOrchestra()` allowlist of **14** tools (orchestration + read-only research + plan write + memory/promote). Worker extras (`edit`, `lsp_*`, `bash`, `task_result`, MCP) are stripped from the Lead schema.
- **Step-1 budget ≤ 8k tokens** — compact Lead schemas, capped lessons (5/dept, ~1000 tok) and playbooks (~1000 tok), CKG subgraph ≤ 1500, no duplicate `<available_tools>` / skills dump on Orchestra Lead.
- **LLM unreachable fail-fast** — `dial tcp` / `connection refused` / `i/o timeout` abort the turn with `LLM Endpoint unreachable at <url>…`; LLM compaction is skipped after infrastructure errors.
- **CKG v5** — multi-hop traversal (`explore` depth/direction), TUI Subagent Bar, `ToolsVersion` **14** handshake.

### Added — Phase 11 (local model stability)

- **Forgiving edit 7-pass** — `patch/resolver`: passes 4–6 (whitespace/escape/trimmed-boundary) + **BlockAnchor** strict (A2); `strategy` in ambiguous errors.
- **`orchestra eval`** — column **RESOLVE** (`resolve_failed` from `llm_log.jsonl`); avg summary line.
- **`permissions.rules` `ask`** — interactive consent for matched tools (edit/write/…) via TUI/IDE `PermissionRequester`.
- **`orchestra auth list` / `orchestra auth set-key`** — provider API key management (E2 lite).
- **`orchestra session list|export|import`** — portable session bundles (`orchestra.session.v1`) for backup/transfer (E1).
- **`orchestra worktree list|add|remove|prune|path`** — orchestra-managed git worktrees under `.orchestra/worktrees/` (E3); agent tools `git.worktree.*`; `orchestra apply --worktree <name>`.
- **Resolver 9-pass forgiving edit** — passes 8–9: `fuzzy-block` (bounded Levenshtein) + `double-anchor` (E4).
- **Grep** — default cap 200 matches, sort by file mtime (newer first).
- **Prompts** — `build-local.txt` / `orchestra.txt`: repo_map → explore before blind grep; Lead delegates broad search to `explore` subagent.
- **Docs** — `docs/architecture/forgiving-edit.md`, Phase 11 section in `ROADMAP.md`, CKG embed ops in `planner-worker.md`.

### Changed — Phase 11 streaming (C5)

- **Core `agent/event` debounce** — `buildAgentOnEvent` batches consecutive `message_delta` / `reasoning_delta` (~30 ms default) before JSON-RPC notify; tool boundaries flush immediately. Env: `ORCH_STREAM_DEBOUNCE_MS` (`0` = disable).

### Added — Attachments & vision (protocol v13)

Multimodal user messages: images (PNG/JPEG/GIF/WebP), SVG, PDF; staging under `.orchestra/attachments/`; RPC `session.message` / `agent.run` param `attachments[]`.

- **`internal/attachments`** — path validation, MIME detection, workspace-safe staging copy.
- **TUI** — `/attach <path>`, chips in user bubble, persist in session v4 `UIMessage.attachments`.
- **VS Code extension** (`ui/vscode/`) — drag-drop / picker, open attachment or diff in workspace editor (not webview), LSP install modal, per-file diff review (↑↓ a x Enter), `@`-mention without webview reload.
- **VS Code Marketplace packaging** — bundled `orchestra` core in `bin/<platform>-<arch>/`; CI workflow `.github/workflows/vscode-vsix.yml` (matrix: win/linux/mac → merge → `.vsix` artifact; optional `VSCE_PAT` publish on tag `vscode-v*`).
- **Session roundtrip** — `llm.Message` JSON unmarshals multimodal `parts` for reload.
- **E2E** — `tests/e2e_real_llm/vision_test.go` (gated by `ORCH_E2E_LLM=1`).

### Changed — Phase 0 stabilization

- **`code.symbols`** — documented three-tier resolution (LSP → tree-sitter when CGO → regex); method vs function kinds in regex fallback; CGO integration test.
- **`orchestra core --http`** — explicitly debug-only in CLI help and stderr notice; stdio remains the supported transport.

### Changed — Phase 2 streaming ✅

- **Always-stream agent path** — `CompleteStream` whenever client implements `Streamer`; `OnEvent` controls UI only (fixes LM Studio tool_calls without dummy callback).
- **Unified `Complete()`** — OpenAI + Anthropic clients drain `CompleteStream`; removed separate non-streaming OpenAI POST from production path.
- **Anthropic streaming** — `ParseAnthropicSSEStream` + `AnthropicClient.CompleteStream`.
- **CLI** — `→ tool` / `← preview` rendering; TTY detection on stdout **or** stderr.
- **Docs** — `docs/architecture/streaming.md` (architecture + operational notes: backpressure, cancel, partial streams).

### Changed — Roadmap / product scope

- **Dropped `orchestra chat` REPL** from scope — multi-turn UX is TUI + VS Code (+ future IDE); `session.*` API remains the contract. ROADMAP Phase 3 rewritten accordingly.

### Changed — Architecture cleanup (pre field-test)

- **Removed `internal/daemon`** — v0.3 HTTP daemon; benchmarks use direct search only.
- **Docs** — new `docs/architecture/paths.md`; updated live paths (`protocol/`, `patch/`, `llm/`); fixed stale OpenCode gap list in `commands-and-modes.md`.
- **CI** — `TestNoLegacyInternalSubmodules` also bans `internal/daemon` imports.

- **Documented complete** — `protocol/`, `patch/`, `llm/` sub-modules + `go.work`; `internal/tools` split; legacy `internal/{protocol,llm,ops,...}` removed.
- **CI** — `TestNoLegacyInternalSubmodules` bans reintroducing deleted import paths.

### Changed — Phase 1 local-model hardening ✅

- **Provider-aware retry limits** — `FillRetryLimits` / `RetryLimitsForProvider`; config `0` = auto (frontier → 1, local → 5). Wired in core, apply, pipeline.
- **`ResolveResponseFormat`** — shared helper; **auto `json_schema` for local providers** unless `supports_json_schema: false`.
- **Prompts** — deprioritize `file.unified_diff` in build-gpt/gemini/kimi; per-family templates via `internal/prompt/files/*-{family}.txt`.
- **Eval harness** — 5 Phase-1 tasks (`rename_func`, `add_func`, `fix_bug`, `add_test`, `refactor`); `ParseLLMLog` metrics; `orchestra eval` RETRIES column + avg summary.

- **`call_dispatch.go`** — table-driven tool dispatch via `toolDispatchTable` + generic `dispatchRunnerTool`.
- **Test colocation** — browser tests moved to `internal/tools/web/browser_test.go`; root duplicates removed.
- **Removed `orchestra daemon` CLI** — `internal/cli/daemon.go` and daemon client discovery in `orchestra search`; `internal/daemon` package kept for in-repo benchmarks only.


`internal/core` split: `runtime_agents.go`, `runtime_llm.go`, `runtime_mcp.go`, `runtime_providers.go`, `runtime_prompt.go`, `runtime_index.go`, `message_attachments.go`. CI adds `vscode-extension` job (`npm ci` + `compile`).

### Changed — Phase 0 module layout (`internal/uimodel`)

Neutral chat DTOs moved to **`internal/uimodel`**. `internal/sessionstore` no longer imports `ui/tui/state`. Layer rules: `docs/architecture/modules.md`.

### Changed — Phase 4 tools subpackages (`internal/tools/*`)

Split tool implementations into subpackages while keeping the registry and public `tools` API at package root:

- **`internal/tools/exec/`** — `Run`, background bash registry, tool defs (`ToolExecRun`, …)
- **`internal/tools/git/`** — git + `gh` CLI tools, tool defs
- **`internal/tools/web/`** — `webfetch`, `websearch`, Playwright browser tools, tool defs
- **`internal/tools/toolslsp/`** — LSP tool implementations + defs (avoids clash with `internal/lsp`)
- **`internal/tools/toolpath/`**, **`internal/tools/toolschema/`** — shared path + JSON-schema helpers
- **`internal/tools/fs/`** — filesystem tools (`Client`, `Overlay`, staging, list/read/write/edit/glob/grep, delete/rename, diff.preview, ast_rename) + tool defs
- Root **`registry.go`** — `ListTools*` / `allToolDefsMap` / parallel flags; delegates to subpackage `Tool*` constructors
- Root **`aliases.go`**, **`*_delegate.go`** — backward-compatible types and `Runner` methods

### Changed — Phase 4b tools nav/session (`internal/tools/nav`, `internal/tools/session`)

- **`internal/tools/nav/`** — explore, symbols, semantic_search, repo_map, CKG admin; tool defs in `nav/registry.go`
- **`internal/tools/session/`** — todo types/validation, memory_*, runtime_query, question; tool defs in `session/registry.go`
- Root **`nav_delegate.go`**, **`session_delegate.go`**; monolith **`registry.go`** delegates tool defs to subpackages

### Changed — Phase 4c tools task subpackage (`internal/tools/task`)

- **`internal/tools/task/`** — subagent tools (`task`, `task_spawn/wait/cancel/result`), plan mode (`plan_enter/exit`), `ToolSkillInvoke`
- Removed root **`path_shim.go`**; skill tests moved to `task/skill_invoke_test.go`

### Changed — Phase 6 tools cleanup (partial)

- Split **`internal/tools/call.go`** → `call_dispatch.go`, `call_decode.go`
- Moved tests: **`git/github_test.go`**, **`web/webfetch_ssrf_test.go`**, **`session/memory_write_test.go`**
- **`internal/daemon/doc.go`** — explicit deprecated package doc


- **`tests/importrules/`** + CI job — enforce layer import direction from `modules.md`
- **`internal/core/session_rpc.go`** — Session JSON-RPC surface extracted from `core.go`
- **`internal/core/core_agent.go`** — `AgentRun`, `ToolCall`, usage/MCP/custom-agent helpers
- **`internal/agent/tool_parallel.go`** — parallel tool batch + JSON error/denial helpers
- **`internal/agent/agent_run.go`**, **`agent_step.go`**, **`agent_prompt.go`** — split from monolith `agent.go`

### Changed — Phase 3 LLM module (`llm/`)

Extracted **`github.com/orchestra/orchestra/llm`** (clients, streaming, catalog, `LLMConfig`/`RouterConfig`/`ModelPreset`, `lmstudio/`). `internal/config` type-aliases LLM types; `ProjectConfig.LLMRegistry()` feeds provider resolution without config↔llm import cycle.

### Changed — Phase 2 patch module (`patch/`)

Extracted **`github.com/orchestra/orchestra/patch`** (`ops`, `patches`, `resolver`, `applier`, `fsutil`, `cache`, `relpath`); depends on `protocol/`; added to `go.work`.

### Changed — Phase 1 wire module (`protocol/`)

Extracted **`github.com/orchestra/orchestra/protocol`** sub-module (`protocol`, `jsonrpc`, `schema`); root **`go.work`**; main module `replace => ./protocol`.

### Added — Skill packs: install/uninstall + per-skill review (2026-05-18)

Third-party skill bundles can be installed from git URLs, HTTP(S) zip/tar archives, or local directories. Installed packs live under `~/.orchestra/packs/<id>/`; `Discover` adds a third tier (project > user > pack) and tags each skill with its `Origin`.

**Security gate** — a skill body becomes a system prompt for a child agent with full tool access. Install therefore prints every skill's metadata + full body and prompts `y/N` per skill. `--yes` bypasses the prompt with a visible warning.

- `internal/packs/` — `ParseSource` (git/http/local), `Fetch` (git clone --depth 1, http archive download with zip + tar/tar.gz support and zip-slip guard, local recursive copy that skips .git and symlinks). Deterministic `Source.ID()` via sanitised name + SHA1 prefix.
- `internal/skills/` — `Skill.Origin` field; `DiscoverFromAll(packsRoot, userDir, projectDir)`; project > user > pack precedence; per-pack duplicate-name check; recursive `*.md` scan inside each pack.
- `orchestra skills install <source> [--yes]` — fetch, validate, per-skill review.
- `orchestra skills uninstall <pack-id>` — `rm -rf` the pack dir.
- `orchestra skills list` now has an `ORIGIN` column (`project` / `user` / `pack:<id>`).
- Out-of-scope follow-ups: `update` (git pull + re-confirm changed skills), version pinning, integrity hashes, signature verification.

### Added — Image input: --image flag + browser.screenshot pipe (2026-05-18)

The agent now accepts images. Two entry points:

1. **CLI: `orchestra apply --image foo.png "what's wrong here?"`** — repeatable flag, supports PNG/JPEG/GIF/WebP, attached to the first user message as multimodal Parts.
2. **`browser.screenshot` pipe** — when the configured LLM is multimodal, the base64 PNG returned by `browser.screenshot` is automatically injected as a `PartImage` in a synthetic follow-up user message, so the model can actually "see" the page on the next step.

- `llm.Message` gains `Parts []ContentPart`; when non-empty, OpenAI-compatible clients serialise the array form (`[{type:"text"}, {type:"image_url"}]`) via a custom `MarshalJSON`. Existing text-only path unchanged.
- `llm.ContentPart{Kind, Text, ImageURL, ImageData, ImageMIME}` — PartImage accepts either a remote/data URI or raw bytes (the client builds the data URI).
- `Message.TextLen()` + `HasImages()` — compaction/truncation count text bytes only; image parts get a fixed 4 KB per-part budget so massive base64 doesn't dominate `estimateMessageSize`.
- `config.LLMConfig.Multimodal bool` — must be set true for image flows to fire; CLI fails fast on `--image` against a text-only LLM.
- `agent.Options.UserImages []llm.ContentPart` + `MultimodalLLM bool` — agent gates both initial-message images and screenshot piping on the flag.
- Anthropic encoding, TUI clipboard paste, and other multimodal tool returns are documented follow-ups.

### Added — Semantic search over CKG (sub-project 4 of CKG roadmap, 2026-05-18)

Long-tail concept search complements text grep and CKG explore. Embed all
function/method/type nodes, then query by natural-language concept:

- `internal/embed/` — OpenAI-compatible embeddings HTTP client (works
  with OpenAI, Ollama, LM Studio, Voyage) + cosine similarity helper.
- `internal/ckg/embed_store.go` — `node_embeddings(node_id, model, dim,
  vector BLOB)` table; `SaveEmbeddings`/`SearchSimilar`/`MissingEmbeddings`
  with brute-force cosine. Schema bumped v3→v4 (drop+recreate as before,
  local cache rebuilds).
- `orchestra ckg embed` (`--rebuild`, `--limit`, `--batch-size`) — reads
  the source range of every indexable node and embeds it in batches.
- `semantic_search {query, top_k, snippet}` tool — embeds the query,
  returns top-K nearest CKG nodes (`fqn`, `kind`, `path`, line range,
  cosine score, optional snippet). Registered only when `embed.model`
  is set in `.orchestra.yml`.
- New config block: `embed: {api_base, api_key, model, dimensions,
  batch_size, timeout_s}`.

Closes CKG runtime roadmap sub-project 4 (Vector DB / semantic search).

### Added — Background bash: run_in_background, bash.output, bash.kill (2026-05-18)

Long-running commands (build, test loops, dev servers, watch processes) no longer block the agent. The existing `bash` tool gains an optional `run_in_background: true` parameter; when set, it returns immediately with a `bg_id` instead of waiting for completion.

- `bash {run_in_background: true, command, args, workdir, timeout_ms}` — spawns under context cancellation, registers in a per-Runner registry, returns `{bg_id, status, command, started}`.
- `bash.output {bg_id, peek}` — returns *new* stdout/stderr since the last poll (per-process cursor), plus current status (`running`/`done`/`killed`/`timed_out`/`error`), exit code when finished, and per-stream truncation flags. `peek: true` reads without advancing the cursor.
- `bash.kill {bg_id}` — terminates the process; no-op (returns current status) when already finished.
- Per-stream buffer cap: 256 KB (truncates oldest content, flagged in response).
- `Runner.Close()` kills every still-running background process — no leaks between runs.
- All three tools gated by `--allow-exec` (same as `bash` itself).
- No protocol/tools version bump: pure additive extension, same `bash` schema with one new optional field.

### Added — Skills follow-ups: user-global, $ARGUMENTS, skill_invoke (2026-05-18)

- `Discover` now scans both `<userHome>/.orchestra/skills/` and `<project>/.orchestra/skills/`; project skills override user skills with the same `name`. Internal: `DiscoverFrom(userDir, projectDir)` is the testable entry point.
- `$ARGUMENTS` in a skill body is replaced with the user query (`--skill` flow) or with the `task` parameter (`skill_invoke` flow). Skills without the marker are unchanged.
- New `skill_invoke` tool: when any skill is discovered, every `apply` run exposes `skill_invoke{skill, task}` to the model and advertises available skills in the system prompt via a `<available_skills>` block. The handler runs a fresh child agent synchronously with the skill's prompt + tool filter + model + provider, returning its result. Child runs with no SubtaskRunner / SkillRunner (no recursion).
- `internal/cli/skill_runner.go` — `cliSkillRunner` implementing `agent.SkillRunner`; resolves per-skill LLM client via `cfg.FindProvider` + `llm.NewClient`, mirroring the `--skill`/`--agent` model/provider override semantics.
- `internal/tools/registry.go` — `ToolSkillInvoke(names)` builds the JSON-Schema with an `enum` of allowed skill names (when supplied), tagged Mutating.
- No ToolsVersion bump: `skill_invoke` is registered dynamically per-run (only when skills exist) and is dispatched in-process — it never crosses the JSON-RPC tools surface.

### Added — Skills loader (2026-05-17)

File-based, discoverable agent skills loaded from `<project>/.orchestra/skills/*.md`. A skill is a Markdown file with a YAML frontmatter header (`name`, `description`, `tools`, `model`, `provider`) and a Markdown body used as the agent system prompt. Skills are the shareable, file-based form of the inline `agents:` block in `.orchestra.yml` — same merge semantics into `agent.Options`.

- `internal/skills/` — `Skill` struct, `Parse(source, r)`, `Load(path)`, `Discover(projectRoot)`, `Find`. Discovery validates required `name` and rejects duplicate names across files.
- `orchestra skills list` — prints discovered skills as a NAME/DESCRIPTION table.
- `orchestra skills show <name>` — prints full metadata + prompt body.
- `orchestra apply --skill <name> "<task>"` — runs apply with the skill's prompt, tool filter, model and provider overrides. Mutually exclusive with `--mode`.
- Tool names in a skill's `tools:` list are validated against the same allow-list used by inline `agents:` (new exported `config.ValidAgentTool`).
- No protocol/tools version bump — skills are a CLI-side loader on top of existing `agent.Options`; the LLM tool surface is unchanged.

See `docs/skills.md`.

### Added — Staging overlay: dry-run write/edit safety (2026-05-15)

До этого `write`/`edit` писали напрямую на диск даже в режиме `--plan-only`, обходя `--apply`-флаг. Теперь в dry-run режиме все записи накапливаются в памяти (overlay) и на диск не попадают.

#### Архитектура (Variant C — full staging)

- **`internal/tools/staging.go`** (новый файл) — `stagedFile`, методы `stageFile`, `stagedContent`, `currentHash`, `StagedOps`, `ApplyPatchesToStaged`, `ClearStaged`, `HasStagedChanges`. `sync.RWMutex` — read-методы используют `RLock`.
- **`internal/tools/runner.go`** — поля `dryRun bool`, `staged map[string]*stagedFile`, `stagedMu sync.RWMutex`; опция `RunnerOptions.DryRun`; метод `SetDryRun`; в `FSRead` — overlay-check перед чтением диска.
- **`internal/tools/write.go`** — dry-run ветка: проверяет `must_not_exist` и `file_hash` в overlay, пишет в `staged`.
- **`internal/tools/edit.go`** — dry-run ветка: читает из overlay или диска, применяет search/replace через `resolver.ApplySearchReplace`, пишет результат в `staged`.
- **`internal/agent/agent.go`** — `StepFinal` разделён на два пути:
  - **DRY-RUN:** `ApplyPatchesToStaged(finalPatches)` → `StagedOps()` → `FSApplyOps(DryRun=true)`. `plan.json` получает `write_atomic` ops с `diskHash`-условиями для stale-detection при `--from-plan`.
  - **APPLY:** старый путь `ResolveExternalPatches` → `FSApplyOps(DryRun=false)`.
- **`internal/cli/apply.go`** — `DryRun: dryRun` передаётся во все три `NewRunner`-вызова (from-plan, via-core, direct).
- **`internal/core/core.go`** — перед каждым `agent.Run`: `SetDryRun(!params.Apply)` + `ClearStaged()`.

#### Что проверяется

- `--plan-only` создаёт `plan.json` с `ops` (не пустой) и НЕ изменяет файлы на диске.
- `--from-plan --apply` применяет план; повторный вызов даёт `StaleContent` без побочных эффектов.
- `TestRealLLMMinimalFlow` и `TestStaleScenario` — оба проходят с реальной моделью.

---

### Added — LSP включён + полное тестовое покрытие инструментов (2026-05-15)

#### LSP (gopls)

- **`.orchestra.yml`** — добавлена секция `lsp.servers` с gopls: `command: ["gopls", "serve"]`, extensions `[".go"]`, `diagnostics_timeout_ms: 5000`. Агент теперь может использовать все 5 LSP-инструментов на реальном gopls.

#### Новые тесты

**`internal/tools/webfetch_test.go`:**
- Пустой URL, неверная схема (`ftp://`)
- SSRF блокировка: `127.0.0.1` (loopback), `10.x` (private A), `192.168.x` (private C), `[::1]` (IPv6 loopback), `169.254.x` (link-local)
- HTML extraction (`extractTextFromHTML`) — script-теги вырезаются, `<title>` извлекается
- `isLikelyHTML` — 6 cases (html/doctype/head/body vs JSON/plain text)

**`internal/tools/lsp_tools_test.go`** (с реальным gopls, skip при `-short`):
- `TestLSP_NoServerConfigured_ReturnsError` — без конфига → "no servers configured"
- `TestLSP_Definition` → возвращает позицию объявления функции `Greet`
- `TestLSP_Hover` → возвращает сигнатуру и doc-комментарий
- `TestLSP_References` → 2 локации (объявление + вызов)
- `TestLSP_Diagnostics_ValidFile` → 0 error-диагностик на корректном файле
- `TestLSP_Diagnostics_BrokenFile` → compiler error на type mismatch
- `TestLSP_Rename` → edits для переименования `Add` → `Sum`

#### Покрытие инструментов после этой сессии

Все инструменты покрыты тестами. Единственный пробел — `question` (интерактивный stdin, тест невозможен без рефакторинга).

---

### Added — Parallel tool execution (2026-05-13)

The agent now executes ParallelSafe tool calls (read-only: `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, `lsp.*`, `todoread`, `task.result`, `runtime.query`, `webfetch`) **concurrently** when the model emits several in a single response. On read-heavy "analyze the project" turns this collapses what used to be 10-20 sequential LLM round-trips into a single batch with a worker-pool fan-out — typically 5-10× speedup.

#### `llm.ToolDef` — declarative concurrency flags

- **`ParallelSafe bool`** — pure reads with no shared-state risk; can run alongside other ParallelSafe tools in one batch.
- **`Mutating bool`** — has observable side effects (file writes, shell commands); always runs one-at-a-time.

Both fields are in-process metadata (`json:"-"`) — the wire format is unchanged.

#### `internal/tools/registry.go` — central classifier

- **`applyParallelFlags(defs []llm.ToolDef) []llm.ToolDef`** — switch keyed on tool name; called by every `ListToolsXXX` constructor. Single source of truth — adding a new tool means one switch update, not edits across every list function.
- Read-only set (ParallelSafe): `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, `todoread`, `task.result`, `runtime.query`, `webfetch`, `lsp.definition`, `lsp.references`, `lsp.hover`, `lsp.diagnostics`.
- Mutating set: `write`, `edit`, `bash`, `todowrite`, `memory_write`, `lsp.rename`, `plan.enter`, `plan.exit`, `task.spawn`, `task.wait`, `task.cancel`, `question`.

#### `internal/agent/types.go` — `Step.Tools`

- **`Step.Tools []ToolCall`** — parallel batch slot. Populated by `NormalizeLLMWithDefs` only when the response carries ≥2 calls **and** every one is ParallelSafe.
- **`Step.Tool *ToolCall`** — preserved for the legacy single-tool serial path; also used as fallback when a batch mixes read-only with mutating (we execute the first call and drop the rest).
- **`ToolCall.ID string`** — now propagated through the type so per-tool replies use the original `tool_call_id`.

#### `internal/agent/step_adapter.go`

- **`NormalizeLLMWithDefs(v, resp, defs []llm.ToolDef)`** — new flag-aware variant. Picks parallel-batch path when `allParallelSafe(calls, defs)` holds, else falls back to first-tool-only.
- **`NormalizeLLM`** — thin wrapper for callers that don't have a tool definition slice.
- **`allParallelSafe(calls, defs)`** — defensive default (unknown tool ⇒ NOT parallel-safe) so MCP/plugin tools never get raced unintentionally.

#### `internal/agent/agent.go` — `runParallelToolBatch`

- Worker pool capped by **`parallelBatchWorkerLimit = 16`** (`chan struct{}` semaphore).
- **PreTool hooks run SERIALLY** before the parallel fan-out. Most hooks aren't reentrant (shared log file, slow subprocess startup); 16 concurrent `powershell -Command Add-Content` invocations would file-lock against each other and time out at the 5 s hook timeout. Serial hooks + parallel tools keeps correctness while still capturing the I/O speedup that actually matters.
- Results collected by index → tool replies are stitched into history in the same order as the assistant message's `tool_calls`, satisfying the OpenAI contract.

#### `internal/agent/agent.go` — serial-fallback orphan cleanup

When the model emits a mixed batch (e.g. `[read, read, edit]`), `NormalizeLLM` keeps only the first call. The SSE parser already surfaced `tool_call_start` for every call, so the TUI would have stranded "running" blocks for the dropped extras. The agent emits synthetic `tool_call_completed` events with a `"skipped: …"` prefix for those orphans (see `ToolBlockSkipped` below).

#### `internal/llm/stream.go` — full tool-call surfacing

- Removed `primaryToolIdx` suppression that hid all but the first parallel `tool_call`. The stream now emits `tool_call_start`/`tool_call_delta` for every call so the TUI can render the full batch.
- Added **`reasoning_content` / `thinking_content` field parsing** for reasoning models (Qwen3, DeepSeek-R1 via LM Studio). When present, the parser wraps these chunks in synthetic `<think>…</think>` tags and feeds them into the same `MessageDelta` channel the TUI's `ReasoningSplitter` already understands — no new event kind needed.
- Added `ORCH_STREAM_DEBUG=path/to/file` env-flag SSE-tap for diagnosing what providers actually send.

#### Prompt updates (`internal/prompt/files/build*.txt`)

- All six build-mode prompts (`build.txt`, `build-local.txt`, `build-anthropic.txt`, `build-gemini.txt`, `build-gpt.txt`, `build-kimi.txt`) rewritten: **explicit encouragement of multi-call batches** for independent reads, with the rule "mutating (write/edit/bash) — strictly one per step, don't mix with other tools in the same batch". Replaces the old "не более одного tool call за шаг" wording that prevented the model from using the new parallel path.

### Changed — TUI: visual polish & UX (2026-05-12 / 2026-05-13)

#### Tool rendering (`ui/tui/view/`)

- **`renderBlockTool`** — removed `BackgroundSecondary` fill from completed exec/write tool panels. Now a plain left `┃` thick-border in `TextMuted` with no panel chrome. Body lines truncated to 20 (was 50).
- **`renderBlockTool`** — title is `<icon> <preview>`, not `# <preview>`.
- **`isBlockStyleTool(tb, streaming)`** — stricter: only `exec.run`, `fs.write`, `file.write_atomic` ever get block style; only when not streaming and not currently running. `read`/`list`/`search`/`symbols`/`glob` stay inline forever — their preview already carries a useful summary (`(N entries)`, `(N matches)`).
- **`renderToolGroup`** — collapsed view shows the **inline per-tool list** plus a compact muted footer (`└ N toolcalls · 7.0s · 1 failed · 2 skipped`); the "Build Task — query" header removed (the user's request panel above already shows the query).
- **`renderInlineTool`** — when `tb.Status == ToolBlockSkipped`, the label renders muted + `Faint` + `Strikethrough` so the user immediately recognizes "intentionally not executed" vs "errored".
- **`renderReasoning`** — wraps the "Thinking: …" block in a muted thick `┃` left-border (no fill) matching the user-message panel style.
- **Icon set unified** (`toolIcon`) — every tool's icon is a single-width single-rune from the same stylistic family. Glyph map: `→` read, `≡` list, `←` write, `✱` grep, `✦` glob (new), `◈` symbols, `$` exec, `▣` task, `•` unknown (was `⚙`). Added Claude-Code aliases to `toolKind` (`Read`, `Write`, `Edit`, `MultiEdit`, `Glob`, `LS`, `TodoWrite`).

#### Render cache (`ui/tui/view/render_cache.go`)

- **`renderCache.delete(key int64)`** — new method. `Chat.ExpandTurn` now invalidates the cached entry for that turn so Ctrl+T actually re-renders with the new expand state. Before this fix, Ctrl+T on a non-last completed assistant message was a visual no-op because the cache returned the old un-expanded render.
- **`Chat.SetMessages`** — expanded turns are never cached. Previously the cache would store an expanded render then return it after the user collapsed.

#### Streaming cursor removed (`view/message_assistant.go`)

- Dropped the `▋` cursor appended to the last assistant token while streaming. With `cursorBlink` toggling every ~500 ms, the cursor would alternately push text past the wrap point and back, causing visible screen jitter on every blink. The status bar's animated busy block now signals "agent is working" without reflowing chat content.

#### Text-from-non-final-step truncation (`app_rpc.go`)

- **`stepTextLen int`** field on `App` tracks the assistant text length at the start of each LLM step. On `EventStepDone(reason != "final")` the assistant message's `Text` is truncated back to `stepTextLen`, dropping pre-tool chatter and invalid-retry scratch output. Previously, when the model said "let me check that" then made a tool call then said "actually here's the answer", both texts ended up concatenated.
- Critical ordering fix: `stepTextLen` is updated in `EventStepDone` **after** truncation, not in `EventDone` (which fires earlier in the stream lifecycle).

#### Status bar redesign (`view/statusbar.go`)

- Two-row layout: top row is a blank gap (visual breathing room from the input box), bottom row is the actual status content.
- Left side reserved for live agent state — info parts (project, tokens, ctx) when idle, **OpenCode-style busy block** (`⠋ ▰▱▱ Read internal/agent/agent.go`) when `agentBusy`. Three accent glyphs cycle in/out with `spinFrame % 3` for a moving-block animation.
- Right side now hosts the context-sensitive hint ("Ctrl+K commands", "Esc cancel", …) — previously these took over the entire bar.
- **`SetActiveTool(name, path string)`** — `app_rpc.go` calls this on `EventToolCallStart`/`EventToolCallDelta` and clears it on `EventToolCallCompleted` / `EventStepDone`.

#### Chat area gutter (`app_view.go`)

- New `chatVerticalPad = 1` constant — 1-row blank gutter above and below the chat viewport so the first message doesn't kiss the top edge and the last message has breathing room from the input box. `layout()` subtracts `2*chatVerticalPad` from `chatHeight` so the input box doesn't get pushed off-screen.

#### Mouse wheel scroll (`ui/tui/app.go`)

- `tea.NewProgram` now starts with `tea.WithMouseCellMotion()`. The `MouseMsg` handler was already wired (`ScrollUp(3)` / `ScrollDown(3)` on wheel-up/down), it just never received events because the program wasn't subscribed to mouse motion.

### Added — Session `ToolBlockSkipped` status

- **`state.ToolBlockSkipped ToolBlockStatus = "skipped"`** — distinct from `Failed`. Set when `tool_call_completed.content` starts with `"skipped: "` (the prefix the agent uses for serial-fallback orphan extras). The TUI renders these muted + strikethrough so the user distinguishes "intentionally not executed" from "errored out".
- Removed the legacy auto-expand logic in `Session.UpdateToolBlock` that set `Expanded = true` on any tool whose result had ≤10 lines. With the new compact inline list as the default view, auto-expanding short results just put noise back in the chat.
- New helpers: `Session.TruncateAssistantText(n)`, `Session.AssistantTextLen()`, `Session.FindToolBlock(id)`, `renderCache.delete(key)`.

---

### Added — Sub-project G: Native LSP integration

#### New package `internal/lsp`

- **`framing.go`** — Content-Length framing: `ReadMessage(r *bufio.Reader) ([]byte, error)` and `WriteMessage(w io.Writer, body []byte) error`.
- **`protocol.go`** — LSP wire types: `Position`, `Range`, `Location`, `LocationLink`, `Diagnostic`, `TextEdit`, `WorkspaceEdit`, `MarkupContent`, `DiagnosticSeverity` (1–4 with `String()`).
- **`positions.go`** — Coordinate helpers: `PathToURI`, `URIToPath`, `ToolPosition` (1-based), `ToLSP`/`ToolPositionFrom` with UTF-16↔byte offset conversion.
- **`client.go`** — `Client` — persistent LSP subprocess client over stdio JSON-RPC 2.0. Methods: `Start`, `StartFromConn`, `Request`, `Notify`, `DidOpen/DidChange/DidClose`, `Close`. `readLoop` routes responses to pending channels and notifications to `notifyCh`.
- **`diagnostics.go`** — `DiagnosticsCache` — push-based diagnostics store. `Update/Get` for cached reads; `WaitForUpdate(ctx, uri)` channel-per-waiter pattern for async `publishDiagnostics` notifications.
- **`manager.go`** — `Manager` — multi-server routing by file extension. Inline `LSPConfig`/`LSPServerConfig` (no import cycle). Methods: `Definition`, `References`, `Hover`, `GetDiagnostics`, `Rename`, `SyncAndDiagnose`. Returns `ToolLocation`, `ToolDiagnostic`, `ProposedEdit` output types. Graceful degradation if no server handles the file.

#### New package `internal/lsp/lsptest`

- **`server.go`** — `Server` — in-process mock LSP server for tests. `New(conn)` / `NewConn()`, `SetHandler(method, fn)`, `PushDiagnostics(uri, diags)`. Auto-handles `initialize` (returns `utf-8` posEncoding), `shutdown`, `exit`.

#### New tools (5 LSP tools)

- **`lsp.definition`** — jump to definition via LSP.
- **`lsp.references`** — find all references via LSP.
- **`lsp.hover`** — hover documentation/type via LSP.
- **`lsp.diagnostics`** — get compiler/linter diagnostics for a file via LSP.
- **`lsp.rename`** — project-wide rename; returns `[]ProposedEdit` for the agent to apply via `fs.edit`/`fs.write`.

Added to: `listToolsBuild` (all 5), `listToolsPlan` / `listToolsExplore` (4, no rename), `listToolsGeneral` (all 5), `ListTools` base, `allToolDefsMap`. `ListToolsForChild` unchanged.

#### Auto-diagnostics on write/edit

- **`FSWriteResponse.Diagnostics []lsp.ToolDiagnostic`** — populated (when LSP is configured) by `SyncAndDiagnose` after every successful `fs.write`.
- **`FSEditResponse.Diagnostics []lsp.ToolDiagnostic`** — same for `fs.edit`. Gives the agent immediate error feedback without an extra tool call.

#### Config (`internal/config/config.go`)

- **`LSPServerConfig`** — `language`, `extensions`, `command`, `env`, `disabled`, `init_options`.
- **`LSPConfig`** — `enabled`, `servers`, `diagnostics_timeout_ms`.
- **`LSP LSPConfig`** field added to `ProjectConfig`.
- 5 LSP tool names added to `validAgentToolNames`.

#### Protocol bump

- `ToolsVersion` **4 → 5**: 5 new LSP tools + `diagnostics` field on write/edit responses.

#### Init template

- `orchestra init` now appends a commented-out `lsp:` block with gopls, typescript-language-server, pylsp, rust-analyzer examples.

### Added — Sub-project D: Custom agents in `.orchestra.yml`

#### Config (`internal/config/config.go`)

- **`AgentDefinition`** struct — `name`, `system_prompt`, `tools []string`, `model` per agent.
- **`agents:`** field on `ProjectConfig`; validated at `config.Load` time via `validateAgents()`:
  - empty name → error; collision with built-in mode name → error; duplicate names → error.
  - `tools: []` (explicit empty) → error ("omit to inherit"); `tools: null` → inherit full build toolset.
  - unknown tool name → error (guards against typos without import cycle).
- **`FindAgent(name) *AgentDefinition`** — O(n) lookup.
- **`IsBuiltInMode(name) bool`** — public predicate over the reserved-names map.

#### Tools registry (`internal/tools/registry.go`)

- **`ResolveToolNames(names []string) ([]llm.ToolDef, error)`** — maps short tool names to `llm.ToolDef` slices; returns error on unknown name.

#### Agent (`internal/agent/agent.go`)

- **`Options.SystemPromptOverride string`** — when non-empty, replaces the built-in mode system prompt before `.orchestra/system.txt` override.

#### Core (`internal/core/core.go`)

- **`AgentRunParams.Mode` / `SessionMessageParams.Mode`** — new optional field; enables custom agent by name on the JSON-RPC path.
- **`resolveCustomAgentOpts`** helper — centralises model override + tool resolution + MCP auto-append for both `AgentRun` and `SessionMessage`.
- Unknown `Mode` → `InvalidLLMOutput` protocol error.

#### CLI (`internal/cli/apply.go`)

- `--mode X` validation: unknown mode that is neither built-in nor in `agents:` → early error with helpful message.
- Direct mode: custom agent system_prompt + tool override + model override wired in.
- `--via-core` path: `Mode` forwarded in `agent.run` params.

#### Protocol bump

- `ProtocolVersion` **1 → 2**: `mode` field added to `agent.run` and `session.message` params (additive, `omitempty`).

#### Init template (`internal/cli/init.go`)

- `.orchestra.yml` generated by `orchestra init` now includes a commented-out advisor example in `agents:`.

### Added — Sub-project E: Permission ruleset per tool + glob

#### `permissions:` config block (`internal/config/config.go`, `internal/agent/permissions.go`)

- **`PermissionRule`** — ordered per-tool rule: `tool` (name or `*`), `pattern` (glob against subject), `action` (`allow` | `deny`).
- **`PermissionsConfig`** — list of rules, added as `permissions:` to `ProjectConfig`.
- **Subject table**: `bash` → command string; `webfetch` → URL; `write/edit/read/ls/grep/symbols` → file path; `glob` → glob pattern; `explore` → symbol name.
- **Glob semantics**: file-path subjects use `path.Match` (`*` does not cross `/`); non-path subjects (bash, webfetch, explore) use a simple wildcard where `*` matches any sequence including `/`.
- **First-match-wins** evaluation order (like iptables): rules are evaluated in order; the first matching rule's action wins.
- **`allow` → bypasses `--allow-exec` / `--allow-web` gates for that specific call only** — does not mutate `agent.Options`.
- **`deny` → always TOOL_DENIED**, regardless of `--allow-exec` / `--allow-web`.
- **No rules → no change** in behavior (existing consent gates are unchanged).
- Propagated through `apply.go`, `core.go`, `pipeline.go` (all three execution modes).

Example config:
```yaml
permissions:
  rules:
    - tool: bash
      pattern: "go test *"
      action: allow
    - tool: bash
      action: deny
    - tool: write
      pattern: "*.go"
      action: allow
    - tool: write
      action: deny
```

### Added — Sub-projects C+G: Compaction agent & Memory tool

#### Memory tool (`internal/tools/memory_write.go`)

- **`memory_write` tool** — агент записывает факты в `.orchestra/memory/agent.md` с ISO-timestamp. Файл создаётся автоматически; записи аппендятся.
- **`LoadProjectMemory` — аддитивный режим** — теперь читает ВСЕ три источника (ORCHESTRA.md + `.orchestra/memory/*.md` + `~/.orchestra/memory.md`) и конкатенирует их (ранее первый непустой выигрывал). Лимит поднят с 2 KB до 8 KB.
- `memory_write` добавлен в `ListTools`, `listToolsBuild`, `listToolsGeneral`.

#### Compaction agent (`internal/agent/compact.go`)

- **`historyBytes(history)`** — подсчёт размера истории в байтах (content + tool call args).
- **`compactHistory(ctx, userQuery, history)`** — вызывает LLM в режиме `ModeCompaction` (`compaction.txt` промпт, до 600 слов), сжимает историю в один `user`-message. Сбой — non-fatal (fallback на truncation).
- **`CompactThresholdPct int`** добавлен в `agent.Options` и `config.AgentConfig` (`compact_threshold_pct`). 0 = выключено, рекомендуется 70.
- Триггер срабатывает **только в начале итерации цикла** (история в консистентном состоянии — нет orphan tool_calls без tool_results).
- `CompactThresholdPct` пробрасывается через `cli/apply.go`, `internal/core/core.go`, `internal/pipeline/pipeline.go`.

### Added — Sub-project F: WebFetch tool

#### `webfetch` (`internal/tools/webfetch.go`)

- **`webfetch` tool** — HTTP GET любого `http://` или `https://` URL; возвращает `{url, title, content, truncated}`.
- **SSRF-защита** — custom `DialContext` резолвит DNS сам и блокирует private, loopback, link-local, multicast и unspecified адреса перед установкой соединения; raw IP-литералы проверяются напрямую.
- **HTML → текст** — `golang.org/x/net/html` парсит DOM; пропускаются `<script>`, `<style>`, `<noscript>`, `<iframe>`, `<svg>`, `<canvas>`; `<title>` извлекается отдельно.
- **Consent-гейт** — `--allow-web` CLI-флаг (зеркалит `--allow-exec`); дефолт `web.confirm: true`; отключается через `web.confirm: false` в `.orchestra.yml`.
- **Лимиты** — `web.fetch_timeout_s` (дефолт 30 с), `web.max_content_bytes` (дефолт 512 КБ); оба настраиваемы в конфиге.
- **`WebConfig`** добавлен в `internal/config/config.go`.
- **`AllowWeb bool`** добавлен в `agent.Options`; защитный check в агент-луп аналогичен bash/AllowExec.
- **`golang.org/x/net v0.53.0`** добавлен в go.mod.

### Added — Post Phase 9: Prompt pipeline, tool aliases, line numbers, forgiving resolver

#### Prompt pipeline (`internal/prompt/`)

- **go:embed промпты** — все промпты перенесены в `internal/prompt/files/*.txt` и встраиваются через `//go:embed files/*.txt`; никаких захардкоженных строк в Go-коде.
- **Маршрутизация по семейству модели** — `BuildSystemPromptForMode(mode, family)` ищет `{mode}-{family}.txt → {mode}.txt → build.txt`; `DetectPromptFamily(modelName)` автоматически определяет семейство.
- **Поддерживаемые семейства:** `anthropic`, `gpt`, `gemini`, `kimi` (Moonshot), `local` (qwen/llama/mistral/deepseek/phi).
- **7 режимов агента** — добавлены константы `ModeGeneral`, `ModeCompaction`, `ModeTitle`, `ModeSummary` к уже существующим `ModeBuild`, `ModePlan`, `ModeExplore`; промпты для каждого встроены через embed.
- **Max-steps reminder** — при достижении 2/3 лимита шагов в историю инжектируется синтетическое `role: assistant` сообщение из `max-steps.txt`, предотвращающее расходование последних шагов на исследование.
- **Lazy ORCHESTRA.md discovery** — `Runner.discoverInstructions` обходит от директории читаемого файла до `workspaceRoot` и инжектирует `<system-reminder>` в ответ `fs.read`; `seenInstructionDirs sync.Map` исключает повторы в рамках сессии.
- **Workspace system prompt override** — `.orchestra/system.txt` полностью заменяет встроенный системный промпт; `LoadSystemOverride(workspaceRoot)` читается в начале каждого шага.
- **Промпты разделены по файлам** — `system.go`, `family.go`, `reminders.go`, `snapshot.go`, `user.go` вместо монолитного `agent_prompt.go`.

#### Anthropic prompt caching (`internal/llm/anthropic.go`)

- Системный промпт оборачивается в `[]anthropicSystemBlock` с `cache_control: {type:"ephemeral"}`.
- Заголовок `anthropic-beta: prompt-caching-2024-07-31` добавлен к каждому запросу.
- Экономия: кэш-запись стоит ~25% дороже, но кэш-чтение экономит ~90% токенов; на сессии из 24 шагов это окупается со шага 2.

#### Tool aliases / short names (`internal/tools/registry.go`)

- Переименованы tool-имена, видимые LLM, в соответствии с конвенцией OpenCode:
  `fs.list` → `ls`, `fs.read` → `read`, `fs.glob` → `glob`, `fs.write` → `write`, `fs.edit` → `edit`, `search.text` → `grep`, `code.symbols` → `symbols`, `explore_codebase` → `explore`, `exec.run` → `bash`.
- `task.spawn/wait/cancel/result` → `task_spawn/wait/cancel/result`.
- `ToolsVersion` bumped `3 → 4`.

#### fs.read line numbers (`internal/tools/fs_read.go`)

- Каждая строка возвращается с префиксом `N: ` (例: `1: package main`).
- Модель видит номера строк для точных ссылок в `edit`; сами префиксы не входят в файл.
- `ToolsVersion` bumped `2 → 3`.

#### Forgiving resolver (`internal/resolver/`)

- При `StaleContent` резолвер делает второй проход с `lineTrimmedFind` (игнорирует хвостовые пробелы) перед тем как вернуть ошибку модели.
- **Pass 3 — IndentationFlexible**: третий проход `indentFlexibleFind` нормализует ведущие отступы (табы → 4 пробела), закрывая разрыв когда файл использует `\t`, а LLM прислал пробелы или наоборот.
- **Защита от ложных срабатываний**: совпадение принимается только если начинается на границе строки (`absJ==0 || normHay[absJ-1]=='\n'`), что не даёт 4-пробельной игле матчиться внутри 8-пробельной строки.
- Сокращает число «рибаундов» к LLM при незначительных расхождениях форматирования, сохраняя `file_hash`-гарантию.

#### Прочие изменения

- **`.gitignore`** — паттерн `orchestra` заменён на `/orchestra` и `/orchestra.exe`, чтобы директория `cmd/orchestra/` не исключалась из git.
- **`cmd/orchestra/main.go`** добавлен в tracking (ранее не коммитился из-за неверного gitignore).
- Удалены легаси-пакеты: `internal/applier`, `internal/parser`, пустые переходные пакеты, `testdata/`, `.eval_test/`.

### Added — Phase 9: Eval harness & provider support

- **Anthropic provider** (`internal/llm/anthropic.go`) — full OpenAI↔Anthropic message conversion; system prompt extracted separately; consecutive `role:tool` messages grouped into a single `tool_result` user message per API requirements; provider selected via `cfg.LLM.Provider = "anthropic"`.
- **Eval harness** (`tests/eval/`) — YAML task definitions, isolated temp workspaces, file-based checks (`file_exists`, `file_not_exists`, `file_contains`, `file_not_contains`), `LoadTasks()`, `Runner.RunTask()`.
- **`orchestra eval [tasks-dir]`** CLI command — runs eval tasks against the configured LLM, tab-formatted pass/fail report.
- Example eval tasks: `tests/eval/tasks/rename_func.yaml`, `add_func.yaml`.

### Added — Phase 8: MCP bridge

- **`internal/mcp/client.go`** — stdio subprocess JSON-RPC 2.0 MCP client; async pending map with channels; `Start()` → initialize handshake → `tools/list`.
- **`internal/mcp/manager.go`** — multi-server manager; `ListToolDefs()` prefixes tools as `mcp:<server>:<tool>`; `Call()` routes via server name; non-fatal per-server startup errors.
- `MCPCaller` interface on `tools.Runner`; routing via `strings.HasPrefix(name, "mcp:")` in `tools/call.go`.
- `MCPConfig` in config (servers with `command`, `env`, `disabled`).
- MCP tools appear as `ExtraTools` in `agent.Options`; `Core.mcpManager` started in `New()`, stopped in `Close()`.

### Added — Phase 7: Project memory

- **`internal/prompt/memory.go`** — `LoadProjectMemory(workspaceRoot, maxBytes)` reads from `ORCHESTRA.md` → `.orchestra/memory/*.md` (sorted, concatenated) → `~/.orchestra/memory.md`; caps at `maxBytes`; wraps in `<project_memory>` block.
- Memory automatically injected into the system prompt at each agent step.

### Added — Phase 6: Hooks

- **`internal/hooks/hooks.go`** — `Runner` executes pre/post tool call shell commands as subprocesses; `RunPreTool` non-zero exit → `TOOL_DENIED`; `RunPostTool` non-zero exit → warning log only (never blocks).
- `HooksConfig` in config (`enabled`, `pre_tool`, `post_tool`, `timeout_ms`).
- `HooksRunner` interface in `agent.Options`; nil-safe assignment in `core.go` prevents non-nil interface with nil pointer.
- Env vars set for hook scripts: `ORCH_TOOL_NAME`, `ORCH_TOOL_INPUT`, `ORCH_WORKSPACE_ROOT`.

### Added — Phase 5: Subagents

- **`internal/tasks/tasks.go`** — `TaskRunner` implements `agent.SubtaskRunner`; `Spawn()` starts child agent in a goroutine with optional timeout; `Wait()` blocks until done or times out; `Cancel()` cancels the child context.
- Child agents run with a read-only tool set (`ListToolsForChild`: `fs.list/read/glob`, `search.text`, `code.symbols`, `task.result`) and `SubtaskRunner: nil` to prevent recursive spawning.
- `task.result` tool — child calls it to return a string; parent agent intercepts and exits the loop with `Result.SubtaskResult`.
- `task.spawn / task.wait / task.cancel` tool definitions in `internal/tools/registry.go`.
- `ToolsVersion` bumped to `2` in `internal/protocol/version.go`.

### Added — New tools

- **`fs.write`** (`internal/tools/fs_write.go`) — atomic file write with optional backup.
- **`fs.edit`** (`internal/tools/fs_edit.go`) — search-and-replace within a file.
- **`fs.glob`** (`internal/tools/fs_glob.go`) — glob pattern file listing.
- **`todo.read / todo.write`** (`internal/tools/todo.go`) — in-process session task list (no filesystem).

### Added — Phase 3: Session API

- `internal/core/session/` — session state: history, todos, last result.
- Stateless `Agent.Run` — takes and returns `[]llm.Message` history slice; Core owns session.
- `OnEvent` callback for streaming events; `AgentLogger` writes `tool_call/tool_result` to `llm_log.jsonl`.

### Added — Phases 1–2: Streaming & grammar

- SSE stream parser, `StreamAccumulator` for tool call assembly across chunks.
- Grammar-constrained sampling (`ResponseFormat`); retry/circuit-breaker config; prompt families.

### Added — Phase 0: vNext core

- JSON-RPC 2.0 over stdio (`internal/jsonrpc`); `orchestra core --workspace-root .` server.
- `Core` + `RPCHandler` (`internal/core`): `initialize`, `agent.run`, `tool.call`, `core.health`.
- `internal/resolver` — `ExternalPatch` → `InternalOp` conversion; `file_hash` consistency checks.
- `internal/patches`, `internal/ops` — two-layer patch model.
- `orchestra daemon` — legacy v0.3 HTTP daemon (loopback-only, for backwards compatibility).

### Changed

- `ToolsVersion` → `2` (was `1`) due to new tool additions.
- Config: added `mcp`, `hooks`, `tasks` sections; `llm.provider` field.
- All disk writes go through atomic temp-file → fsync → rename.

### Tests

- New test packages for all vNext additions:
  - `internal/hooks` — pre/post subprocess, env vars, timeout, nil runner.
  - `internal/prompt` — all 3 memory sources, priority, truncation.
  - `internal/llm` — Anthropic conversion (system extraction, tool_result grouping, schema defaults).
  - `internal/mcp` — tool name parsing, nil-safe Manager, invalid routes.
  - `internal/tasks` — Spawn/Wait/Cancel lifecycle, mock LLM with `task.result`.
  - `tests/eval` — all check types, `LoadTasks`, `RunTask` with mock agent.

---

## [0.2.0] — Initial release

- v0.2 architecture: `pkg/cli`, `internal/context` builder, `internal/gitutil`, plan/apply pipeline.
- `orchestra apply`, `orchestra search`, `orchestra init`.
- OpenAI-compatible LLM client.
- Search with exclusion rules, diff-based apply with backup.
