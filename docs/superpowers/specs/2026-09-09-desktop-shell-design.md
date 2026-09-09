# Desktop Shell (Tauri) — Design

**Status:** approved design, part B of four (A — multi-project registry, done;
C — the multi-project window; D — packaging, updater, signing).
**Depends on:** `docs/superpowers/specs/2026-09-09-project-registry-design.md`
(the registry, `/api/projects`, cookie auth, `orchestra web`).

## Problem

`orchestra web` is a server that opens a browser tab. A user who wants "the
Orchestra app" gets a terminal window they must keep alive, a URL with a token
in it, and a tab that dies with the terminal. The VS Code extension does not
have this problem because VS Code is the shell. A standalone application needs
its own shell: something that owns the process, owns the window, and shows the
user a folder picker instead of a `--workspace-root` flag.

This spec covers the shell only: starting the core, opening the window, and
shutting both down cleanly. The project switcher, the cross-project session
list and the settings screens stay in the web UI and are not part of this
piece. Installers, code signing and auto-update are part D.

## Decisions

**Tauri v2, as recorded in part A.** The shell is a Rust binary that embeds
nothing but a system webview (WebView2 on Windows, WebKit on macOS and Linux).
Its cost is a Rust toolchain on developer machines and in CI. Wails (Go) was
weighed as the no-new-language alternative and declined by the owner in favour
of Tauri's packaging and updater ecosystem, which part D will lean on.

**A thin shell: process manager plus window, nothing else.** The shell spawns
`orchestra web` and points a window at the URL it announces. There are no Tauri
commands, no IPC, no frontend assets inside the Tauri bundle. The page is served
by the Go server exactly as in a browser, so the cookie flow, the `Origin` check
and the per-project WebSocket from part A work unchanged. Everything the user
sees is `ui/web`; everything the shell does fits in one Rust file.

**The core is a sidecar, with `PATH` as the development fallback.** The shell
looks for `orchestra` (`orchestra.exe` on Windows) next to its own executable
— which is where every Tauri bundler places an `externalBin` — and, when it is
not there, spawns `orchestra` from `PATH` with the same arguments and says so
on stderr. A dependency-free Node script builds the Go binary into
`src-tauri/binaries/orchestra-<target-triple>` (the name the bundler expects)
and copies it next to the debug executable for local runs. The `externalBin`
declaration itself belongs to part D: `tauri-build` validates and copies it on
every `cargo build`, so declaring it in B would make `cargo test` fail on a
tree without the Go binary. A user's installation is self-contained; a
developer's loop needs only `go install`.

**The startup project is: argument, else the last one, else a dialog.** A path
given on the command line wins. Otherwise the shell reopens the project it
started last time, which it records in `~/.orchestra/desktop.json`
(`{"last_project": "<abs path>"}`, written after a successful announce).
Part A's `~/.orchestra/projects.json` cannot serve here: it is sorted by path,
not by recency. It is the fallback when `desktop.json` is absent — its first
entry is better than a dialog on a machine that has used `orchestra web`.
Otherwise a native folder picker asks, once. A cancelled picker exits the app;
there is nothing to show without a project. A remembered directory that no
longer exists is skipped, and the next source is tried. `orchestra web` restores
every other remembered project itself, so the shell passes exactly one
`--workspace-root`.

**`orchestra web --init` initialises the startup workspace when it has none.**
Today `orchestra web` exits when its startup workspace has no `.orchestra.yml`
— right for a CLI run in a repository, wrong for a folder the user just picked
in a dialog. With `--init` the server runs the same `initProject` the API uses
for `POST /api/projects {"init":true}` before opening the workspace; a
workspace that already has a config is left untouched. The shell always passes
`--init`. Without the flag nothing changes.

**`orchestra web --announce` prints one JSON line to stdout when it listens.**
The shell needs the URL and the token; today they live in a human log line on
stderr and in `.orchestra/web.json`. Parsing a log line is fragile and polling
a file is a race. With `--announce` the server writes, to stdout and only
there, exactly one line — the `webDiscovery` object it already writes to
`.orchestra/web.json` — and nothing else ever goes to stdout in that mode. The
shell reads one line and has its contract.

**Under `--announce`, EOF on stdin is the shutdown signal.** Windows has no
`SIGTERM`. Killing the child skips its deferred cleanup: the discovery file
stays, the open-project list is not saved. Language servers do exactly what the
shell needs: they exit when their parent closes the pipe. So `--announce` also
means "watch stdin; when it closes, cancel the server context and return from
`runWeb` normally". The shell closes the child's stdin on exit, waits up to three
seconds, and only then kills.

**One window, closing it exits.** Title "Orchestra", 1200×800, the OS decides
where. The window loads `<url>/?token=<token>`; the server sets the cookie and
redirects to `/`, after which the webview holds the credential the same way a
browser would. Multiple windows, tray icons and "keep running in background"
are not in v1.

**Loopback, token, cookie — unchanged.** The shell adds no network surface. The
token is generated by the server, read once by the shell from the announce
line, and used once to load the page.

## Rejected

**A Tauri-native chrome (project picker, menus) talking to the core over IPC.**
The frontend would live on the `tauri://` origin, so the cookie would not be
sent and the `Origin` check would refuse `/api/*`; the fix is CORS plus a second
authentication path, and a second UI surface to keep in step with `ui/web`.
The web UI already owns its chrome; the shell should not compete with it.

**Parsing `[orchestra] web UI: <url>` from stderr.** It works today and breaks
the day someone rewords the log line. A single JSON line on an otherwise silent
stdout is a contract; a log line is not.

**Polling `.orchestra/web.json`.** It exists, but it is written after the
listener is up, in the workspace, which the shell would have to know is
initialised. And it is a discovery file for tools, not a readiness signal for
a parent process.

**Killing the sidecar on exit.** Loses the shutdown save from part A and leaves
`.orchestra/web.json` behind, which the next start then has to detect as stale.
Closing stdin costs one goroutine and gives a clean exit.

**Electron.** Rejected for size and for shipping a second browser when every
target platform already has one.

## The contract

### `orchestra web --announce`

```
orchestra web --workspace-root <dir> --no-open --port 0 --init --announce
```

- stdout: exactly one line, then nothing. The line is the JSON of
  `webDiscovery` — `protocol_version`, `workspace_root`, `url`, `port`,
  `token`, `pid`, `started_at_unix`, `written_at_unix` — written immediately
  after the listener is bound and the discovery file is written. Everything
  that used to go to stdout in `runWeb` goes to stderr in this mode.
- stdin: read until EOF. EOF cancels the server context; `runWeb` returns `nil`
  after its deferred cleanup (registry shutdown, list saved, discovery file
  removed).
- Without `--announce` nothing changes: same stderr line, same discovery file,
  stdin untouched.
- `--init`: when `<dir>/.orchestra.yml` is absent, run project initialisation
  (the same code as `orchestra init` and `POST /api/projects {"init":true}`)
  before opening the workspace. Its output goes to stderr under `--announce`.

### Shell behaviour

```
orchestra-desktop [<project-dir>]
```

1. Resolve the project: argument → `last_project` from
   `~/.orchestra/desktop.json` → first entry of `~/.orchestra/projects.json`
   → folder picker → exit(0) if cancelled. A candidate that is not an existing
   directory is skipped.
2. Spawn the sidecar (`binaries/orchestra-<triple>`; fallback `orchestra` on
   `PATH`) with the arguments above.
3. Read one stdout line within 15 seconds. Parse it as JSON; take `url` and
   `token`.
4. Open the window at `<url>/?token=<token>`; record the project in
   `~/.orchestra/desktop.json`.
5. On window close: close the child's stdin, wait up to 3 s for exit, then kill.

Any failure in steps 2–3 (no binary, exit before announcing, timeout, bad JSON)
shows a native error dialog with the last lines of the child's stderr, then
exits with code 1. The child, if running, is stopped first.

## Structure

```
ui/desktop/
  README.md                     the decision record (exists), updated
  scripts/build-sidecar.mjs     go build → src-tauri/binaries/orchestra-<triple>[.exe]
  src-tauri/
    Cargo.toml                  tauri 2, tauri-plugin-dialog, serde, serde_json (no shell plugin: std::process)
    tauri.conf.json             no default window; frontendDist = a placeholder dir; externalBin comes in part D
    capabilities/default.json   core:default for the main window (no JS API is used)
    build.rs
    src/main.rs                 glue: Tauri builder, boot thread, exit handling
    src/boot.rs                 pure logic: project resolution, announce parsing, arguments, memory file
    src/sidecar.rs              process: candidates (next to exe, then PATH), spawn, announce with timeout, stderr tail, graceful stop
internal/cli/web.go             --init and --announce flags; stdout JSON line; stdin EOF → cancel
.github/workflows/ci.yml        job `desktop`: cargo fmt --check, clippy, build (windows, ubuntu)
```

No changes to `ui/web`, `internal/webtransport`, `internal/projects` or
`internal/core`. The protocol version does not move.

## Lifecycle and failure

- **Start**: dialogs and the sidecar wait happen on a worker thread, never on
  the main thread — the dialog plugin's blocking calls dispatch to the main
  thread and would deadlock it. The window is created via
  `run_on_main_thread`. The shell blocks on the announce line, not on a fixed
  sleep. A core
  that takes long to open (large CKG scan) delays the window, not its
  correctness; warmup continues after the window is up, as in part A.
- **Second instance**: two shells on the same project both start a server; the
  second gets its own port and its own token and works. The cookie caveat from
  part A applies (one `orchestra web` per browser profile) — but the Tauri
  webview has its own cookie jar per app, so the desktop app and a browser tab
  do not clobber each other.
- **Sidecar dies while the window is open**: the page shows "disconnected —
  reload to start a new session", as it does in a browser today. The shell does
  not restart it in v1; the user restarts the app.
- **Exit**: window close → stdin close → server shutdown save → child exit →
  shell exit. If the child ignores EOF for 3 s it is killed, and the next start
  cleans the stale discovery file as `orchestra web` already does.

## Testing

Go (`internal/cli`):
- `--announce` writes exactly one line to stdout and it round-trips through
  `webDiscovery`; nothing else reaches stdout in that mode — checked on the real
  process (the test re-executes its own binary as `orchestra web`) with a fresh
  `--init` workspace, so the init messages that go to stdout today would show
  up as extra lines. Mutation: removing the stdout redirection fails the test.
- `--init` creates `.orchestra.yml` in a bare directory and leaves an existing
  one untouched. Mutation: dropping the init call fails the test.
- Closing stdin returns `runWeb` within the shutdown budget with the project
  list saved. Mutation: dropping the EOF watcher makes the test time out.
- Without `--announce`, stdin is not read (a test that never closes stdin still
  returns on context cancel).

Rust (`ui/desktop/src-tauri`):
- Project resolution: argument beats `desktop.json` beats `projects.json` beats
  dialog; a missing or corrupt file and a vanished directory both fall through;
  the dialog is behind a trait so the test injects an answer.
- `desktop.json` round trip: written after announce, read on the next start.
- Announce parsing: the exact `webDiscovery` shape parses; a log line does not;
  a missing `token` is an error.
- Sidecar argument construction is byte-for-byte the contract above.
- Fallback selection: sidecar missing → `PATH` candidate, with the stderr note.

End to end, by hand and recorded in the plan's task report: `cargo tauri dev`
on Windows opens the window, the chat works, closing the window leaves no
`orchestra` process and no `.orchestra/web.json`.

CI: `cargo fmt --check`, `cargo clippy -- -D warnings`, `cargo build` on
`windows-latest` and `ubuntu-latest` (with the WebKitGTK dev packages Tauri
documents). No bundle in CI until part D.

## Risks

- **Rust in a Go repository.** One file, three plugins, no business logic; but a
  toolchain nobody on the project uses daily. Mitigated by keeping the shell
  thin enough that part D is packaging, not more Rust.
- **`--announce` becomes a public flag.** It is part of the CLI contract from
  the moment the shell depends on it; documented in `docs/PROTOCOL.md` next to
  the discovery file, whose shape it reuses.
- **WebView2 absence on older Windows.** Windows 10/11 ship it; the Tauri
  installer in part D can bootstrap it. Not addressed in B.
- **Sidecar build per platform.** `externalBin` wants one binary per target
  triple. The build script produces the host's; cross-building is a release
  (part D) concern and `release.yml` already builds four platforms natively.
