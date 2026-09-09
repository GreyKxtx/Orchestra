# The Multi-Project Window (C1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A rail down the left edge of the window listing every remembered
project with its live state, so an agent can work in one project while you read
another and you can see which project needs you without switching into it.

**Architecture:** The remembered project list becomes a first-class type
(`projects.Store`) and `GET /api/projects` returns closed entries beside open
ones, so the rail can draw a list the core does not hold. Startup stops
reopening everything; projects open on click. In the browser layer the single
module-level WebSocket becomes a per-project connection created by a factory,
with `wsSend`/`wsNotify`/`wsReply` keeping their signatures and routing to the
active project — so the three existing adapter fragments change only where they
hold per-turn state. The shared VS Code renderer is not touched.

**Tech Stack:** Go 1.26 (`internal/projects`, `internal/webtransport`,
`internal/cli`), plain ES2020 in one IIFE (`ui/web/src/*.js`, bundled by
`ui/web/scripts/bundle-web.mjs`), `node:test` via a `vm` sandbox
(`ui/web/scripts/adapter-test.mjs`), Rust/Tauri v2 for notifications only.

**Spec:** `docs/superpowers/specs/2026-09-09-multi-project-window-design.md`
(read the whole thing; C2 — the trajectory view — is **not** in this plan).

## Global Constraints

- **The VS Code extension's bundle must not change by a single byte.** Nothing
  under `ui/vscode/` may be edited in this plan. `ui/vscode/media/chat.css` is
  copied into the web bundle, so it counts as VS Code's — new styles go in a
  new web-only stylesheet.
- `ProtocolVersion` stays **15**. Nothing in this plan changes the JSON-RPC
  wire contract. `/api/projects` is HTTP, not JSON-RPC, and is versioned by
  this repository alone.
- Project identity is always `cache.ComputeProjectID(abs)` — never a raw path
  comparison, never `strings.EqualFold`. It case-folds on Windows and that is
  the only dedupe rule allowed.
- Every disk write of the project list goes through `fsutil.AtomicWriteFile`
  with mode `0600`. The file holds paths only, never a token.
- Errors wrap with `fmt.Errorf("...: %w", err)`. No panics for expected
  failures.
- Go: `go vet ./...` and `go test ./...` must pass. Windows is a first-class
  target; do not assume `/` separators or a case-sensitive filesystem.
- JS: no dependencies, no build step beyond `bundle-web.mjs`. The bundle is one
  IIFE — fragments share scope, so a `function` declaration in one fragment is
  callable from another (hoisting already makes `dispatchToCore` work this way).
- After any change under `ui/web/`, run `node ui/web/scripts/bundle-web.mjs`
  and commit the regenerated `ui/web/static/*`. They are checked in.

---

## File Structure

**Go — the remembered list and the API**

- `internal/projects/store.go` (modify) — gains a `Store` type wrapping the
  existing `LoadPaths`/`SavePaths` functions: the remembered list as a live
  object with `Paths`, `Add`, `Forget`, `PathForID`.
- `internal/projects/registry.go` (modify) — adds `StateClosed` and
  `ClosedProject`; removes `AddErrored`, which only existed for the startup
  restore this plan deletes.
- `internal/webtransport/server.go` (modify) — `GET /api/projects` merges
  closed entries; `DELETE /api/projects/{id}?forget=1` forgets; `POST` records
  what it opened.
- `internal/cli/web.go` (modify) — builds the `Store`, passes it to the server,
  and no longer calls `restoreProjects` (which is deleted).

**Browser — connections, state, and the rail**

- `ui/web/src/00-web-prelude.js` (modify) — `createConn(projectId, handlers)`
  factory; `setActiveConn`/`activeConn`; `wsSend`/`wsSendCancellable`/
  `wsNotify`/`wsReply` keep their signatures and delegate to the active
  connection.
- `ui/web/src/10-adapter-session.js` (modify) — session id becomes per project;
  `onConnected(projectId)`; new `activateProject(projectId)` repaints.
- `ui/web/src/20-adapter-events.js` (modify) — turn text and live tool blocks
  become per project; a background project's events update its state only.
- `ui/web/src/30-adapter-asks.js` (modify) — pending permission/question ids
  become per project, so a background project's prompt survives a switch.
- `ui/web/src/40-projects.js` (create) — the projects module: fetch the list,
  hold one connection per open project, switch, and own the rail's DOM.
- `ui/web/rail.css` (create) — web-only styles. **Not** `chat.css`.
- `ui/web/index.src.html` (modify) — the rail element and a `<link>` to
  `rail.css`.
- `ui/web/scripts/bundle-web.mjs` (modify) — add `40-projects.js` to `order`,
  copy `rail.css` into `static/`.
- `ui/web/scripts/adapter-test.mjs` (modify) — the harness gains several
  sockets and a `fetch` stub; new tests for routing, switching and states.

**Desktop — notifications only**

- `ui/desktop/src-tauri/capabilities/core-page.json` (create) — the narrow
  remote grant.
- `ui/desktop/src-tauri/tauri.conf.json` (modify) — `withGlobalTauri`.
- `ui/desktop/src-tauri/Cargo.toml` (modify) — `tauri-plugin-notification`.
- `ui/desktop/src-tauri/src/main.rs` (modify) — register the plugin.

**Docs**

- `docs/PROTOCOL.md` (modify) — the `/api/projects` shape, including
  `state: "closed"` and `?forget=1`.
- `ui/desktop/README.md` (modify) — what the rail does and what the remote
  capability grants.

---

## Task 1: Prove the served page can call the shell

The spec names this the design's largest risk and requires it settled before
anything is built on it. The page comes from `http://127.0.0.1:<random>`, which
Tauri treats as a remote URL with no access to the shell. This task finds out
whether a capability with a wildcard port grants it.

**This task is a verification, not a feature.** Its deliverable is a written
answer plus, if the answer is yes, the capability file that will be used.

**Files:**
- Create: `ui/desktop/src-tauri/capabilities/core-page.json`
- Modify: `ui/desktop/src-tauri/tauri.conf.json`
- Modify: `ui/desktop/src-tauri/Cargo.toml`
- Modify: `ui/desktop/src-tauri/src/main.rs`

**Interfaces:**
- Consumes: nothing.
- Produces: a yes/no answer recorded in the task report. If yes,
  `window.__TAURI__.notification.sendNotification({title, body})` is callable
  from the served page and Task 9 uses it. If no, Task 9 uses
  `window.request_user_attention` from Rust instead and the rail is the only
  in-page signal.

- [ ] **Step 1: Add the notification plugin**

In `ui/desktop/src-tauri/Cargo.toml`, under `[dependencies]`, beside the
existing `tauri-plugin-dialog = "2"`:

```toml
tauri-plugin-notification = "2"
```

- [ ] **Step 2: Register it**

In `ui/desktop/src-tauri/src/main.rs`, find the `tauri::Builder` chain that
already calls `.plugin(tauri_plugin_dialog::init())` and add one line after it:

```rust
        .plugin(tauri_plugin_notification::init())
```

- [ ] **Step 3: Turn on the global API and write the probe capability**

In `ui/desktop/src-tauri/tauri.conf.json`, add `withGlobalTauri` to the `app`
object (the page is plain JS with no bundler, so it needs `window.__TAURI__`):

```json
  "app": {
    "withGlobalTauri": true,
    "windows": [],
    "security": {
      "csp": null
    }
  },
```

Create `ui/desktop/src-tauri/capabilities/core-page.json` with **one probe-only
permission**. `core:window:allow-set-title` is here because the window title is
the only channel out of the page that can be read without looking at a screen —
it is removed again in Step 8.

```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "identifier": "core-page",
  "description": "PROBE STAGE. Replaced in Step 8.",
  "windows": ["main"],
  "remote": {
    "urls": ["http://127.0.0.1:*"]
  },
  "permissions": [
    "core:window:allow-set-title"
  ]
}
```

Leave `capabilities/default.json` alone except for its `description`, which now
claims the page uses no Tauri API and is no longer true:

```json
  "description": "Baseline window permissions. The served page's own grants are in core-page.json.",
```

- [ ] **Step 4: Build and check the capability compiles**

Run from `ui/desktop/src-tauri`:

```bash
cargo build
```

Expected: success. A malformed capability fails the build in `tauri-build`
with a schema error naming the offending field — if that happens, the field
names above are wrong for the installed Tauri version, and the fix is to read
`ui/desktop/src-tauri/gen/schemas/desktop-schema.json` for the accepted shape
rather than guessing. Do the same if `core:window:allow-set-title` is not a
known permission id: the schema file lists the real ones.

- [ ] **Step 5: Write the title reader**

Nobody involved in this task can see a screen, so the probe must report through
something readable from a shell. The window title is that channel. Write this
to `$env:TEMP\window-titles.ps1` (use the Write tool; do not paste it into a
shell):

```powershell
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public class WinTitles {
  private delegate bool EnumProc(IntPtr hWnd, IntPtr lParam);
  [DllImport("user32.dll")] private static extern bool EnumWindows(EnumProc cb, IntPtr lParam);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] private static extern int GetWindowTextW(IntPtr hWnd, StringBuilder s, int n);
  [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr hWnd);
  public static List<string> Visible() {
    var found = new List<string>();
    EnumWindows(delegate(IntPtr h, IntPtr l) {
      if (IsWindowVisible(h)) {
        var sb = new StringBuilder(512);
        GetWindowTextW(h, sb, 512);
        var t = sb.ToString();
        if (t.Length > 0) found.Add(t);
      }
      return true;
    }, IntPtr.Zero);
    return found;
  }
}
'@
[WinTitles]::Visible() | Where-Object { $_ -like "*PROBE*" -or $_ -eq "Orchestra" }
```

Why this and not `Get-Process | Select MainWindowTitle`: that picks whichever
window Windows considers the process's main one, which for this shell is an
untitled helper, so it reports an empty string even when the real window is up.
`EnumWindows` enumerates them all.

- [ ] **Step 6: Probe stage A — does the IPC reach a remote page at all?**

Append this to `ui/web/index.src.html`, just before `</body>`:

```html
  <script>
    // TEMPORARY probe — removed in Step 8.
    (async () => {
      const t = window.__TAURI__;
      if (!t || !t.window || !t.window.getCurrentWindow) {
        return; // No channel out. The title stays "Orchestra", which is the answer.
      }
      try {
        await t.window.getCurrentWindow().setTitle("PROBE-IPC-OK");
      } catch (e) {
        // The API is injected but the call was refused. Nothing to report
        // through, so the title stays "Orchestra" — stage A cannot separate
        // this from "absent", which is why stage B exists.
      }
    })();
  </script>
```

Build in this order — the probe lives in the page that `ui/web/embed.go`
compiles into the Go binary, so the core must be rebuilt **after** re-bundling
or the window shows the old page:

```bash
node ui/web/scripts/bundle-web.mjs
node ui/desktop/scripts/build-sidecar.mjs
cargo run --manifest-path ui/desktop/src-tauri/Cargo.toml -- .
```

Run `cargo run` in the background, give it time to open, then read the titles:

```powershell
powershell -ExecutionPolicy Bypass -File $env:TEMP\window-titles.ps1
```

- If a title reads `PROBE-IPC-OK`: the IPC does reach a remote page on a
  wildcard-port origin. Go to Step 7.
- If the only title is `Orchestra`: the mechanism is unavailable. **Skip Step 7**
  and record that in Step 8; Task 9 takes its fallback branch.
- If no matching title appears at all, the window did not open — that is a
  different failure. Report BLOCKED with the `cargo run` output rather than
  guessing.

Stop the shell by sending `WM_CLOSE` to the window (a `taskkill /IM` posts
`WM_CLOSE` to every top-level window including untitled helpers, which is not
the same thing):

```powershell
Add-Type -Name W -Namespace P -MemberDefinition '[DllImport("user32.dll")] public static extern IntPtr FindWindowW(string c, string n); [DllImport("user32.dll")] public static extern bool PostMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);'
$h = [P.W]::FindWindowW($null, "PROBE-IPC-OK"); if ($h -eq [IntPtr]::Zero) { $h = [P.W]::FindWindowW($null, "Orchestra") }
[void][P.W]::PostMessageW($h, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)
```

Then confirm nothing is left running:

```powershell
Get-Process orchestra-desktop,orchestra -ErrorAction SilentlyContinue | Select-Object Name,Id
```

- [ ] **Step 7: Probe stage B — is the notification permission usable?**

Only run this if stage A printed `PROBE-IPC-OK`. Now that the title channel is
known to work, the notification result can be reported through it.

Change `core-page.json`'s permissions to:

```json
  "permissions": [
    "core:window:allow-set-title",
    "notification:default",
    "dialog:allow-open"
  ]
```

Replace the probe's body with:

```html
  <script>
    // TEMPORARY probe — removed in Step 8.
    (async () => {
      const t = window.__TAURI__;
      const w = t && t.window && t.window.getCurrentWindow ? t.window.getCurrentWindow() : null;
      if (!w) {
        return;
      }
      try {
        await t.notification.sendNotification({ title: "Orchestra", body: "probe" });
        await w.setTitle("PROBE-NOTIFY-OK");
      } catch (e) {
        await w.setTitle("PROBE-NOTIFY-FAIL " + String(e).slice(0, 120));
      }
    })();
  </script>
```

Re-bundle, rebuild the core, run, and read the titles the same way. Expected:
`PROBE-NOTIFY-OK`, and a Windows toast. A `PROBE-NOTIFY-FAIL …` title carries
the refusal message, which in a debug build names the capability and command
it wanted — copy it verbatim. Close the shell the same way (the title to
`FindWindowW` is now whichever one you read).

- [ ] **Step 8: Record the answer, restore the real capability, remove the probe**

Write into the task report, verbatim, the exact window titles observed at each
stage and which of these three outcomes holds. Do not soften a negative result;
Task 9's implementer reads only this.

1. `PROBE-NOTIFY-OK` — notifications work from the served page. Task 9 uses
   `window.__TAURI__.notification.sendNotification`.
2. `PROBE-NOTIFY-FAIL …` — the IPC reaches the page but the notification
   permission was refused. Quote the message.
3. Stage A never produced `PROBE-IPC-OK` — the IPC does not reach a remote
   page on this origin. Task 9 takes its fallback branch.

Then set `core-page.json` to the capability the app actually ships — the probe
permission is gone, and the description is the real one:

```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "identifier": "core-page",
  "description": "The page is served by orchestra web on loopback. It may raise a notification when a background project needs an answer, and open a folder picker when adding a project. Nothing else.",
  "windows": ["main"],
  "remote": {
    "urls": ["http://127.0.0.1:*"]
  },
  "permissions": [
    "notification:default",
    "dialog:allow-open"
  ]
}
```

If outcome 3 held, ship this file anyway with the same two permissions: it
grants nothing that works, costs nothing, and leaves the decision recorded in
one place rather than in a commit message.

Delete the temporary `<script>` block from `ui/web/index.src.html`, re-bundle,
and confirm the page and bundle are back to their committed state:

```bash
node ui/web/scripts/bundle-web.mjs
git status --short ui/web
```

Expected: no modifications under `ui/web` at all. Anything left there is probe
residue, and Task 8 edits the same file.

- [ ] **Step 9: Build clean and commit**

```bash
cd ui/desktop/src-tauri && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test && cd ../../..
git add ui/desktop/src-tauri/Cargo.toml ui/desktop/src-tauri/Cargo.lock ui/desktop/src-tauri/tauri.conf.json ui/desktop/src-tauri/capabilities ui/desktop/src-tauri/src/main.rs
git commit -m "feat(desktop): grant the served page exactly two shell calls"
```

---

## Task 2: The remembered project list as a type

`store.go` today has three free functions and the list is read once at startup.
The rail needs it live: a project is added when opened and removed only when
explicitly forgotten, and closing a project must **not** remove it.

**Files:**
- Modify: `internal/projects/store.go`
- Test: `internal/projects/store_test.go` (exists; add to it)

**Interfaces:**
- Consumes: `cache.ComputeProjectID(abs string) (string, error)` — the single
  identity, already imported by `registry.go`.
- Produces:
  - `func NewStore(path string) (*Store, error)`
  - `func (s *Store) Paths() []string`
  - `func (s *Store) Add(path string) error`
  - `func (s *Store) Forget(path string) (bool, error)`
  - `func (s *Store) PathForID(id string) (string, bool)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/projects/store_test.go`:

```go
func TestStore_AddIsIdempotentByProjectID(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add again: %v", err)
	}
	if got := s.Paths(); len(got) != 1 {
		t.Fatalf("want 1 remembered path, got %d: %v", len(got), got)
	}
}

func TestStore_AddPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	again, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	got := again.Paths()
	if len(got) != 1 || got[0] != filepath.Clean(proj) {
		t.Fatalf("reloaded list is %v, want [%s]", got, filepath.Clean(proj))
	}
}

func TestStore_ForgetRemovesAndReportsWhetherItWasThere(t *testing.T) {
	file := filepath.Join(t.TempDir(), "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	had, err := s.Forget(proj)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if !had {
		t.Fatal("Forget reported the path was not remembered, but it was added")
	}
	if got := s.Paths(); len(got) != 0 {
		t.Fatalf("want an empty list after Forget, got %v", got)
	}

	had, err = s.Forget(proj)
	if err != nil {
		t.Fatalf("Forget again: %v", err)
	}
	if had {
		t.Fatal("Forget reported a path it had already removed")
	}
}

func TestStore_PathForIDResolvesARememberedProject(t *testing.T) {
	file := filepath.Join(t.TempDir(), "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	abs, _ := filepath.Abs(proj)
	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	got, ok := s.PathForID(id)
	if !ok {
		t.Fatalf("PathForID(%q) found nothing; list is %v", id, s.Paths())
	}
	if got != filepath.Clean(abs) {
		t.Fatalf("PathForID returned %q, want %q", got, filepath.Clean(abs))
	}

	if _, ok := s.PathForID("nope"); ok {
		t.Fatal("PathForID invented a path for an unknown id")
	}
}

func TestNewStore_UnreadableListIsAnError(t *testing.T) {
	// A directory where the file should be: readable path, unreadable content.
	// The caller must learn this rather than silently starting with an empty
	// list and overwriting a list it never saw.
	dir := t.TempDir()
	inTheWay := filepath.Join(dir, "projects.json")
	if err := os.Mkdir(inTheWay, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := NewStore(inTheWay); err == nil {
		t.Fatal("NewStore accepted an unreadable list")
	}
}
```

Make sure the file's imports include `os`, `path/filepath`, `testing` and
`github.com/orchestra/orchestra/patch/cache`.

- [ ] **Step 2: Run them and watch them fail**

```bash
go test ./internal/projects -run TestStore -v
```

Expected: compile failure — `undefined: NewStore`.

- [ ] **Step 3: Implement `Store`**

Append to `internal/projects/store.go` (and add `sync` and
`github.com/orchestra/orchestra/patch/cache` to its imports):

```go
// Store is the remembered project list — the projects the interface shows
// whether or not the core currently holds them. Closing a project keeps it
// here; only Forget removes it. That distinction is the whole reason this
// type exists instead of the server saving Registry.Paths() on the way out.
type Store struct {
	path string

	mu    sync.Mutex
	paths []string // cleaned absolute paths, deduped by project id
}

// NewStore loads the list at path. An absent or corrupt file is an empty list
// (LoadPaths' contract); any other read failure is returned, because a list we
// could not read is a list we must not overwrite.
func NewStore(path string) (*Store, error) {
	loaded, err := LoadPaths(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: path}
	for _, p := range loaded {
		abs, aerr := filepath.Abs(p)
		if aerr != nil {
			continue // a path we cannot resolve is not a project we can open
		}
		s.insert(filepath.Clean(abs))
	}
	return s, nil
}

// identity is the dedupe key: the same project id the registry and the HTTP
// API use, which case-folds on Windows. Falling back to the path keeps a
// project that cannot be hashed visible rather than dropping it.
func identity(abs string) string {
	if id, err := cache.ComputeProjectID(abs); err == nil {
		return id
	}
	return abs
}

// insert adds abs unless an entry with the same identity is already present.
// Caller holds no lock on the first call from NewStore (no other reference
// exists yet); every other caller holds s.mu.
func (s *Store) insert(abs string) bool {
	key := identity(abs)
	for _, p := range s.paths {
		if identity(p) == key {
			return false
		}
	}
	s.paths = append(s.paths, abs)
	sort.Strings(s.paths)
	return true
}

// snapshotLocked copies the list; the caller holds s.mu.
func (s *Store) snapshotLocked() []string {
	out := make([]string, len(s.paths))
	copy(out, s.paths)
	return out
}

// Paths returns the remembered paths in a stable order.
func (s *Store) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Add remembers a path and writes the list. Adding one that is already
// remembered writes nothing and is not an error.
//
// The write happens while the lock is held, on purpose. Snapshotting under the
// lock and writing outside it lets two concurrent Adds persist in either
// order, leaving the file disagreeing with memory until the next write. This
// path runs when a user opens a project — holding a mutex across one small
// atomic write costs nothing worth measuring, and the alternative is a bug
// that only shows up as a project missing after a restart.
func (s *Store) Add(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve project path %q: %w", path, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.insert(filepath.Clean(abs)) {
		return nil
	}
	return SavePaths(s.path, s.snapshotLocked())
}

// Forget drops a path and writes the list. The bool says whether it was
// remembered, so a caller can answer 404 for an id nobody knows. The write is
// under the lock for the same reason as Add.
func (s *Store) Forget(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve project path %q: %w", path, err)
	}
	key := identity(filepath.Clean(abs))

	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.paths))
	found := false
	for _, p := range s.paths {
		if identity(p) == key {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return false, nil
	}
	s.paths = kept
	return true, SavePaths(s.path, s.snapshotLocked())
}

// PathForID resolves a remembered path by project id, so the API can act on a
// closed project the client knows only by id.
func (s *Store) PathForID(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.paths {
		if identity(p) == id {
			return p, true
		}
	}
	return "", false
}
```

`sort` and `fmt` are already imported by `store.go`? Check: `store.go` imports
`encoding/json`, `errors`, `fmt`, `os`, `path/filepath` and `fsutil`. Add
`sort` and `sync` and the `cache` import; `fmt` is there.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/projects -run TestStore -v
go test ./internal/projects
```

Expected: PASS, and the package's existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/projects/store.go internal/projects/store_test.go
git commit -m "feat(projects): the remembered list as a type, not a startup read"
```

---

## Task 3: A closed project is a project the API can name

The rail needs entries for projects the core does not hold. `Registry.List()`
returns only open ones by construction, so the merge belongs above it.

**Files:**
- Modify: `internal/projects/registry.go`
- Test: `internal/projects/registry_test.go` (exists; add to it)

**Interfaces:**
- Consumes: `Store.Paths()` from Task 2 (used by Task 4, not here).
- Produces:
  - `const StateClosed State = "closed"`
  - `func ClosedProject(path string) (Project, bool)`
  - `AddErrored` is **removed**; nothing may call it after Task 4.

- [ ] **Step 1: Write the failing test**

Append to `internal/projects/registry_test.go`:

```go
func TestClosedProject_CarriesTheIDAnOpenOneWouldHave(t *testing.T) {
	dir := t.TempDir()

	p, ok := ClosedProject(dir)
	if !ok {
		t.Fatal("ClosedProject refused a real directory")
	}
	if p.State != StateClosed {
		t.Fatalf("state is %q, want %q", p.State, StateClosed)
	}
	if p.OpenedAt != 0 {
		t.Fatalf("OpenedAt is %d; a closed project is not open and must not sort among those that are", p.OpenedAt)
	}
	if p.Name != filepath.Base(filepath.Clean(dir)) {
		t.Fatalf("name is %q, want %q", p.Name, filepath.Base(filepath.Clean(dir)))
	}

	abs, _ := filepath.Abs(dir)
	want, err := cache.ComputeProjectID(abs)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	if p.ID != want {
		t.Fatalf("id is %q, want %q — the client acts on a closed project by this id", p.ID, want)
	}
}

func TestClosedProject_DoesNotStatTheDirectory(t *testing.T) {
	// A remembered path on an unreachable network share must not block a list
	// request. ClosedProject therefore never touches the filesystem: a path
	// that does not exist still yields an entry.
	missing := filepath.Join(t.TempDir(), "gone", "deeper")
	p, ok := ClosedProject(missing)
	if !ok {
		t.Fatal("ClosedProject refused a path that does not exist; it must not check")
	}
	if p.State != StateClosed {
		t.Fatalf("state is %q, want %q", p.State, StateClosed)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/projects -run TestClosedProject -v
```

Expected: compile failure — `undefined: StateClosed`, `undefined: ClosedProject`.

- [ ] **Step 3: Implement**

In `internal/projects/registry.go`, extend the state constants:

```go
const (
	StateReady  State = "ready"
	StateError  State = "error"
	StateClosed State = "closed"
)
```

and add, next to the `Project` type:

```go
// ClosedProject is the wire shape for a remembered project the core does not
// hold. It deliberately does not touch the filesystem: a remembered path on an
// unreachable share would otherwise make GET /api/projects hang, which is the
// same fragility this design set out to remove from startup. Whether the
// directory is still there is discovered when the user clicks it, and the
// error travels back on that request.
func ClosedProject(path string) (Project, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, false
	}
	abs = filepath.Clean(abs)
	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		return Project{}, false
	}
	return Project{
		ID:    id,
		Path:  abs,
		Name:  filepath.Base(abs),
		State: StateClosed,
	}, true
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/projects -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/projects/registry.go internal/projects/registry_test.go
git commit -m "feat(projects): a closed project the API can name"
```

---

## Task 4: The API lists closed projects, remembers, and forgets

**Files:**
- Modify: `internal/webtransport/server.go` (the `Options` struct near line 30;
  the `/api/projects` handlers near line 231; `handleOpenProject` near line 382)
- Test: `internal/webtransport/projects_api_test.go` (exists — part A's tests
  for this endpoint; add to it and reuse its helpers)

**Interfaces:**
- Consumes: `projects.Store` (Task 2), `projects.ClosedProject` (Task 3).
- Produces:
  - `Options.Known *projects.Store` — nil keeps today's open-only behaviour.
  - `GET /api/projects` → `{"projects":[…]}` with open entries first (ordered
    by `opened_at` as today) then closed ones ordered by path.
  - `DELETE /api/projects/{id}` → closes, stays remembered. 204.
  - `DELETE /api/projects/{id}?forget=1` → closes if open and forgets. 204 even
    when the project was closed already; 404 only when neither the registry nor
    the store knows the id.
  - `POST /api/projects` → unchanged responses, and remembers what it opened.

- [ ] **Step 1: Give the existing test helper a `Known` parameter**

`internal/webtransport/projects_api_test.go` already has the three helpers
these tests need:

- `initWS(t) string` — a `t.TempDir()` with a real `.orchestra.yml` written by
  `config.Save`, which is what `Registry.Open` checks for.
- `startRegistryServer(t) (base string, reg *projects.Registry)` — the real
  server with a real registry, token `secret`.
- `doJSON(t, method, url string, body any) (int, map[string]any)` — an
  authenticated request; the body decodes into a map, so numbers arrive as
  `float64`.

Change the helper's signature to accept the store, and pass `nil` at its five
existing call sites — all in this one file, verified with
`grep -rn "startRegistryServer(t)" internal/webtransport/`:
`TestAPI_OpenListClose`, `TestAPI_TypedErrors`,
`TestAPI_InitFlagCreatesTheConfig`, `TestAPI_RequiresAuth` and
`TestAPI_CrossOriginRequestIsRejected`:

```go
// startRegistryServer stands up the real server with a real registry. `known`
// may be nil, which is the open-only behaviour part A shipped.
func startRegistryServer(t *testing.T, known *projects.Store) (base string, reg *projects.Registry) {
```

and inside the `Options` literal, beside `Registry: reg,`:

```go
		Known:    known,
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/webtransport/projects_api_test.go`:

```go
func TestAPI_ListsRememberedProjectsAsClosed(t *testing.T) {
	closedDir := initWS(t)
	openDir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(closedDir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": openDir}); st != http.StatusCreated {
		t.Fatalf("open %s: status %d, body %v", openDir, st, body)
	}

	st, body := doJSON(t, "GET", base+"/api/projects", nil)
	if st != http.StatusOK {
		t.Fatalf("GET status %d, body %v", st, body)
	}
	list, _ := body["projects"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 projects (one open, one closed), got %d: %v", len(list), body)
	}

	first, _ := list[0].(map[string]any)
	second, _ := list[1].(map[string]any)
	if first["state"] != "ready" {
		t.Fatalf("first entry state is %v; open projects must come first", first["state"])
	}
	if second["state"] != "closed" {
		t.Fatalf("second entry state is %v, want \"closed\"", second["state"])
	}
	if second["path"] != filepath.Clean(closedDir) {
		t.Fatalf("closed entry path is %v, want %q", second["path"], filepath.Clean(closedDir))
	}
	if second["opened_at"] != float64(0) {
		t.Fatalf("closed entry opened_at is %v, want 0 — it must not sort among open projects", second["opened_at"])
	}
}

func TestAPI_AnOpenProjectIsNotListedTwice(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Remembered AND open — the common case, and the one that would duplicate.
	if err := store.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	base, _ := startRegistryServer(t, store)
	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir}); st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}

	_, body := doJSON(t, "GET", base+"/api/projects", nil)
	list, _ := body["projects"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 project, got %d: %v", len(list), body)
	}
	entry, _ := list[0].(map[string]any)
	if entry["state"] != "ready" {
		t.Fatalf("state is %v, want \"ready\" — open outranks remembered", entry["state"])
	}
}

func TestAPI_CloseKeepsThePathRemembered(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir})
	if st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("open returned no id: %v", body)
	}

	if st, body := doJSON(t, "DELETE", base+"/api/projects/"+id, nil); st != http.StatusNoContent {
		t.Fatalf("close: status %d, body %v", st, body)
	}

	found := false
	for _, p := range store.Paths() {
		if p == filepath.Clean(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("closing dropped %q from the remembered list %v; closing and forgetting are different actions", dir, store.Paths())
	}
}

func TestAPI_ForgetRemovesAProjectThatWasNeverOpened(t *testing.T) {
	gone := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(gone); err != nil {
		t.Fatalf("Add: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	p, ok := projects.ClosedProject(gone)
	if !ok {
		t.Fatal("ClosedProject refused a real directory")
	}
	// The registry has never held it, so Detach fails — forget must still work.
	if st, body := doJSON(t, "DELETE", base+"/api/projects/"+p.ID+"?forget=1", nil); st != http.StatusNoContent {
		t.Fatalf("forget: status %d, body %v", st, body)
	}

	for _, remembered := range store.Paths() {
		if remembered == filepath.Clean(gone) {
			t.Fatalf("forget left %q in the list: %v", gone, store.Paths())
		}
	}
}

func TestAPI_ForgetAnUnknownIDIs404(t *testing.T) {
	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "DELETE", base+"/api/projects/nobody-knows-this?forget=1", nil); st != http.StatusNotFound {
		t.Fatalf("status %d, body %v; an id neither the registry nor the list knows is a 404", st, body)
	}
}

func TestAPI_OpeningRemembers(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir}); st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}

	found := false
	for _, p := range store.Paths() {
		if p == filepath.Clean(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("opening %q did not remember it (%v); the rail would lose it on restart", dir, store.Paths())
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

```bash
go test ./internal/webtransport -run TestAPI_ -v
```

Expected: compile failure — `unknown field Known in struct literal` (from the
helper change in Step 1), then, once the field exists, failures in the six new
tests.

- [ ] **Step 4: Add the option and the merge**

In `Options` (after the `NewProjectHandler` field):

```go
	// Known, when non-nil, is the remembered project list. GET /api/projects
	// returns its entries as closed projects beside the open ones, POST records
	// what it opened, and DELETE ?forget=1 removes an entry. Nil keeps the
	// open-only behaviour, which is what a caller with no persistence wants.
	Known *projects.Store
```

Add the merge as a package-level function in the same file:

```go
// listProjects returns the open projects followed by the remembered ones the
// core does not hold. Open outranks remembered: a project that is both appears
// once, as open, because that entry carries its real state and opened_at.
func listProjects(opts Options) []projects.Project {
	open := opts.Registry.List()
	if opts.Known == nil {
		return open
	}
	seen := make(map[string]bool, len(open))
	for _, p := range open {
		seen[p.ID] = true
	}
	closed := make([]projects.Project, 0, len(opts.Known.Paths()))
	for _, path := range opts.Known.Paths() {
		p, ok := projects.ClosedProject(path)
		if !ok || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		closed = append(closed, p)
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].Path < closed[j].Path })
	return append(open, closed...)
}
```

Add `sort` to the file's imports if it is not already there.

Change the GET arm from `opts.Registry.List()` to `listProjects(opts)`:

```go
			case http.MethodGet:
				writeJSONStatus(w, http.StatusOK, map[string]any{"projects": listProjects(opts)})
```

- [ ] **Step 5: Rework the DELETE handler**

Replace the body of the `/api/projects/` handler. The existing close ordering —
detach, evict, then close — is load-bearing and must not be reordered; the only
additions are the `forget` branch and the fact that a closed project is no
longer automatically a 404.

```go
		mux.HandleFunc("/api/projects/", api(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
			forget := r.URL.Query().Get("forget") == "1"

			// Detach first, so no dial that starts from here on can obtain the
			// core; then drop the tab (the client sees a normal disconnect); and
			// only when its handler has returned — nothing can be running on the
			// core any more — close the core. Order matters: closing while a
			// handler is live would nil the core's tools under it.
			c, derr := opts.Registry.Detach(id)
			if derr == nil && c != nil {
				settled, done := evict(id, 5*time.Second)
				if settled {
					_ = c.Close()
				} else {
					// The connection did not wind down in time. The project is
					// already gone from the registry; its core closes the moment
					// the connection finally ends, not before. Accepted: such a
					// core is no longer reachable by Registry.Shutdown, so at
					// process exit it closes when the server ctx ends the
					// connection — possibly after runWeb has returned.
					go func() { <-done; _ = c.Close() }()
				}
			}

			// Closing leaves the project remembered; only forget removes it.
			// A project that was already closed is not in the registry, so
			// Detach failed above — forgetting it must still succeed.
			forgot := false
			if forget && opts.Known != nil {
				path, ok := opts.Known.PathForID(id)
				if ok {
					var ferr error
					forgot, ferr = opts.Known.Forget(path)
					if ferr != nil {
						writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
							"error": "forget_failed", "detail": ferr.Error(),
						})
						return
					}
				}
			}

			if derr != nil && !forgot {
				writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "project_not_open"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
```

- [ ] **Step 6: Remember what POST opened**

In `handleOpenProject`, both places that answer `http.StatusCreated` must
record the path first. Add this helper above the function:

```go
// remember records a freshly opened project so the rail still shows it after a
// restart. A write failure is reported on stderr rather than failing the open:
// the project is open, and the user's next action should not be blocked by a
// list that could not be saved.
func remember(opts Options, path string) {
	if opts.Known == nil {
		return
	}
	if err := opts.Known.Add(path); err != nil {
		fmt.Fprintf(os.Stderr, "[orchestra] could not remember %s: %v\n", path, err)
	}
}
```

and call it at both success points:

```go
	p, err := opts.Registry.Open(ctx, req.Path)
	if err == nil {
		remember(opts, p.Path)
		writeJSONStatus(w, http.StatusCreated, p)
		return
	}
```

```go
		p, err = opts.Registry.Open(ctx, req.Path)
		if err == nil {
			remember(opts, p.Path)
			writeJSONStatus(w, http.StatusCreated, p)
			return
		}
```

Add `os` to the imports if absent.

- [ ] **Step 7: Run the tests**

```bash
go test ./internal/webtransport -run TestAPI_ -v
go test ./internal/webtransport
go vet ./...
```

Expected: PASS, including the package's existing project tests — in particular
`ws_project_test.go`'s dial/DELETE race test, which the reordering above must
not disturb.

- [ ] **Step 8: Commit**

```bash
git add internal/webtransport/server.go internal/webtransport/projects_api_test.go
git commit -m "feat(web): the projects API lists closed projects, remembers, and forgets"
```

---

## Task 5: Startup stops reopening everything

This is the task that retires the defect part B's review left open: one
unreachable remembered path currently makes the app fail to start, identically
every launch, inside the shell's 15-second announce budget.

**Files:**
- Modify: `internal/cli/web.go` (the block near lines 196-220, and
  `restoreProjects` near line 310)
- Modify: `internal/projects/registry.go` (delete `AddErrored`)
- Modify: `internal/projects/registry_test.go` (delete its `AddErrored` tests)
- Test: `internal/cli/web_serve_test.go` (exists — part B's `serveWeb` tests;
  add to it and reuse `initialisedDir`, `startServeWeb`, `readAnnounce`)

**Interfaces:**
- Consumes: `projects.NewStore` (Task 2), `Options.Known` (Task 4).
- Produces: nothing new. `restoreProjects` and `Registry.AddErrored` cease to
  exist; `StateError` stays, because an errored entry is still a shape the API
  can return and removing it would widen this task.

- [ ] **Step 1: Write the failing test**

The assertion has to discriminate old behaviour from new, and "the server came
up" does not: today an unreachable path fails fast on Windows and
`restoreProjects` records it via `AddErrored`, so startup survives and the path
stays in the list either way. What actually changes is **what the registry
holds**: today the unreachable path becomes an errored entry in the registry,
and after this task it is merely remembered and closed. So the test reads the
API.

Append to `internal/cli/web_serve_test.go`:

```go
// A remembered project is a list entry, not a startup workload. Today
// restoreProjects opens every remembered path during startup and records a
// failure as an errored registry entry; after this change startup opens only
// the workspace it was given and the rest are closed entries.
func TestServeWeb_RememberedProjectsAreClosedNotOpenedAtStartup(t *testing.T) {
	workspace := initialisedDir(t)
	unreachable := filepath.Join(t.TempDir(), "not-there")

	store := filepath.Join(t.TempDir(), "projects.json")
	if err := projects.SavePaths(store, []string{unreachable}); err != nil {
		t.Fatalf("SavePaths: %v", err)
	}

	annR, annW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	_, done := startServeWeb(t, webRunConfig{
		Workspace: workspace, NoOpen: true, StorePath: store,
	}, webIO{Announce: annW, Stdin: stdinR})
	d := readAnnounce(t, annR)

	req, _ := http.NewRequest(http.MethodGet, d.URL+"/api/projects", nil)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/projects: %v", err)
	}
	var body struct {
		Projects []struct {
			Path  string `json:"path"`
			State string `json:"state"`
		} `json:"projects"`
	}
	derr := json.NewDecoder(resp.Body).Decode(&body)
	_ = resp.Body.Close()
	if derr != nil {
		t.Fatalf("decode /api/projects: %v", derr)
	}

	var got string
	for _, p := range body.Projects {
		if filepath.Clean(p.Path) == filepath.Clean(unreachable) {
			got = p.State
		}
	}
	if got == "" {
		t.Fatalf("the remembered path is missing from the list: %+v", body.Projects)
	}
	if got != string(projects.StateClosed) {
		t.Fatalf("remembered path state is %q, want %q — startup must not try to open it",
			got, projects.StateClosed)
	}

	_ = stdinW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveWeb after stdin EOF: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("stdin EOF did not shut the server down")
	}

	// And the list still holds both: startup neither dropped the unreachable
	// path nor forgot the workspace it opened.
	remembered, lerr := projects.LoadPaths(store)
	if lerr != nil {
		t.Fatalf("LoadPaths: %v", lerr)
	}
	foundUnreachable, foundWorkspace := false, false
	for _, p := range remembered {
		switch filepath.Clean(p) {
		case filepath.Clean(unreachable):
			foundUnreachable = true
		case filepath.Clean(workspace):
			foundWorkspace = true
		}
	}
	if !foundUnreachable {
		t.Fatalf("the unreachable path was dropped from %v; only Forget may remove an entry", remembered)
	}
	if !foundWorkspace {
		t.Fatalf("the startup workspace is not remembered: %v", remembered)
	}
}
```

The file already imports `bufio`, `encoding/json`, `io`, `net/http`, `os`,
`path/filepath`, `strings`, `testing`, `time`, `config` and `protocol`; add
the `projects` package import if it is not there.

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/cli -run TestServeWeb_RememberedProjects -v
```

Expected: FAIL with `remembered path state is "error", want "closed"` — today
`restoreProjects` tried to open it and `AddErrored` put it in the registry.
Record the observed message verbatim in the report; a different message means
the test is measuring something other than intended and the implementation
step must not start until that is understood.

- [ ] **Step 3: Replace the restore with a store**

In `internal/cli/web.go`, replace the whole remembered-projects block (from the
comment beginning "Remembered projects reopen alongside it" through the
`saveOpen()`/`defer saveOpen()` pair) with:

```go
	// The remembered list is the user's list of projects, not a startup
	// workload: the rail shows every entry and opens one when clicked. Startup
	// opens only the workspace it was given, so an unreachable remembered path
	// is a closed entry in the list rather than a failure to start.
	storePath := cfg.StorePath
	if storePath == "" {
		p, serr := projects.StorePath()
		if serr != nil {
			fmt.Fprintln(os.Stderr, "[orchestra] "+serr.Error()+"; open projects will not be remembered this run")
		}
		storePath = p
	}
	var known *projects.Store
	if storePath != "" {
		s, kerr := projects.NewStore(storePath)
		if kerr != nil {
			// A list we could not read is a list we must not overwrite.
			fmt.Fprintln(os.Stderr, "[orchestra] "+kerr.Error()+"; open projects will not be remembered this run")
		} else {
			known = s
			if aerr := known.Add(startup.Path); aerr != nil {
				fmt.Fprintln(os.Stderr, "[orchestra] could not remember the startup project: "+aerr.Error())
			}
		}
	}
```

Pass it to the server, in the `webtransport.Options` literal, next to
`Registry: reg,`:

```go
		Known:    known,
```

- [ ] **Step 4: Delete what is now dead**

Delete the `restoreProjects` function from `internal/cli/web.go` entirely,
along with the `errors` import if nothing else in the file uses it (check with
`go build ./internal/cli` — an unused import is a compile error, which is the
check).

Delete `Registry.AddErrored` from `internal/projects/registry.go` and every
test of it from `internal/projects/registry_test.go`. `StateError` stays.

- [ ] **Step 5: Run everything**

```bash
go build ./... && go vet ./... && go test ./...
```

Expected: PASS across all packages. `go test ./internal/cli` in particular
covers part B's announce tests, which this change is near.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/web.go internal/cli/web_serve_test.go internal/projects/registry.go internal/projects/registry_test.go
git commit -m "fix(web): the remembered list is a list, not a startup workload"
```

---

## Task 6: One connection per project

A pure refactor: the module-level socket becomes a factory, and the four RPC
helpers keep their names and signatures so the adapter fragments barely move.
The existing 13 adapter tests are the net — they must stay green with no
changes to their assertions.

**Files:**
- Modify: `ui/web/src/00-web-prelude.js`
- Modify: `ui/web/scripts/adapter-test.mjs` (harness only, no new tests yet)

**Interfaces:**
- Consumes: `toRenderer(msg)`, `dispatchToCore(msg)` — both already in scope.
- Produces, all callable from later fragments:
  - `createConn(projectId, handlers)` → a `Conn`, where `handlers` is
    `{onOpen(projectId), onClose(projectId), onError(projectId), onNotification(projectId, msg), onServerRequest(projectId, msg)}`
  - `Conn` = `{projectId, send(method, params), sendCancellable(method, params), notify(method, params), reply(id, result), close(), isOpen()}`
  - `setActiveConn(conn)` / `activeConn()` → `Conn | null`
  - `wsSend`, `wsSendCancellable`, `wsNotify`, `wsReply` — unchanged
    signatures, now delegating to `activeConn()`

- [ ] **Step 1: Extend the test harness for several sockets**

In `ui/web/scripts/adapter-test.mjs`, the `FakeWebSocket` class keeps only the
last socket in `socket`. Replace that with a registry, keeping `socket` as the
most recent one so every existing test keeps working:

```js
  const sockets = [];
  let socket = null;

  class FakeWebSocket {
    static OPEN = 1;
    constructor(url) {
      this.url = url;
      this.readyState = 1;
      this.listeners = {};
      socket = this;
      sockets.push(this);
    }
    addEventListener(type, fn) {
      (this.listeners[type] ||= []).push(fn);
    }
    send(data) {
      sent.push({ url: this.url, ...JSON.parse(data) });
    }
    emit(type, ev) {
      for (const fn of this.listeners[type] || []) fn(ev);
    }
  }
```

Note `send` now tags each frame with the socket's url. Every existing
assertion reads `.method`/`.params`, so tagging is additive.

Add socket lookup and a `fetch` stub to the returned handle. `fetch` is needed
by Task 8 and harmless now:

```js
  const fetchCalls = [];
  let fetchResponder = () => ({ projects: [] });
  sandbox.fetch = async (url, init) => {
    fetchCalls.push({ url: String(url), init: init || {} });
    const body = fetchResponder(String(url), init || {});
    return {
      ok: true,
      status: 200,
      json: async () => body,
      text: async () => JSON.stringify(body),
    };
  };
```

Add `sandbox.fetch` before `vm.createContext(sandbox)`, and extend the returned
object:

```js
  return {
    sent,
    inbound,
    fetchCalls,
    setFetchResponder: (fn) => {
      fetchResponder = fn;
    },
    socketFor: (projectId) =>
      sockets.find((s) => s.url.includes(`project=${encodeURIComponent(projectId)}`)) || null,
    open: () => socket.emit("open", {}),
    openFor: (projectId) => {
      const s = sockets.find((x) => x.url.includes(`project=${encodeURIComponent(projectId)}`));
      assert.ok(s, `no socket for project ${projectId}; urls: ${sockets.map((x) => x.url).join(", ")}`);
      s.emit("open", {});
    },
    deliver: (obj) => socket.emit("message", { data: JSON.stringify(obj) }),
    deliverTo: (projectId, obj) => {
      const s = sockets.find((x) => x.url.includes(`project=${encodeURIComponent(projectId)}`));
      assert.ok(s, `no socket for project ${projectId}; urls: ${sockets.map((x) => x.url).join(", ")}`);
      s.emit("message", { data: JSON.stringify(obj) });
    },
    post: (msg) => sandbox.window.postMessage(msg),
    close: () => socket.emit("close", {}),
    get socketURL() {
      return socket ? socket.url : "";
    },
  };
```

- [ ] **Step 2: Run the existing tests to confirm the harness change is inert**

```bash
node ui/web/scripts/adapter-test.mjs
```

Expected: all 13 tests pass. If any fail, the harness change broke them — fix
the harness, not the tests.

- [ ] **Step 3: Turn the socket into a factory**

In `ui/web/src/00-web-prelude.js`, replace everything from `let ws = null;`
through the end of the `connect` function with the following. `socketURL`,
`toRenderer` and the `host` object above it are unchanged.

```js
  // ---- JSON-RPC over one socket per project -------------------------------
  //
  // A project's core is reached through its own socket (part A's guard is per
  // project, so several are allowed). The four helpers below keep the
  // signatures the adapter fragments already use and route to whichever
  // project is active, so "which socket" is a question only this file and
  // 40-projects.js answer.

  /** @type {any} */
  let active = null;

  /**
   * @param {string} projectId "" for the project-less socket
   * @param {{onOpen?: Function, onClose?: Function, onError?: Function, onNotification?: Function, onServerRequest?: Function}} handlers
   */
  function createConn(projectId, handlers) {
    const h = handlers || {};
    let nextRpcId = 1;
    /** @type {Map<number, {resolve: Function, reject: Function}>} */
    const pendingCalls = new Map();
    const ws = new WebSocket(socketURL(projectId));

    const conn = {
      projectId,
      isOpen: () => ws.readyState === WebSocket.OPEN,
      /** @param {string} method @param {any} params @returns {Promise<any>} */
      send(method, params) {
        return new Promise((resolve, reject) => {
          if (ws.readyState !== WebSocket.OPEN) {
            reject(new Error("not connected"));
            return;
          }
          const id = nextRpcId++;
          pendingCalls.set(id, { resolve, reject });
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, method, params: params || {} }));
        });
      },
      /**
       * Like send, but hands back the request id so the caller can cancel it
       * later with $/cancelRequest. Reading nextRpcId here is safe: send
       * allocates it synchronously, with no await in between.
       * @param {string} method @param {any} params
       * @returns {{id: number, done: Promise<any>}}
       */
      sendCancellable(method, params) {
        const id = nextRpcId;
        return { id, done: conn.send(method, params) };
      },
      /** @param {string} method @param {any} params */
      notify(method, params) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", method, params: params || {} }));
        }
      },
      /** Reply to a server-initiated request. @param {any} id @param {any} result */
      reply(id, result) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ jsonrpc: "2.0", id, result }));
        }
      },
      close() {
        try {
          ws.close();
        } catch (e) {
          // Already closing. Nothing to do.
        }
      },
    };

    ws.addEventListener("open", () => {
      if (h.onOpen) h.onOpen(projectId);
    });
    ws.addEventListener("close", () => {
      for (const { reject } of pendingCalls.values()) {
        reject(new Error("disconnected"));
      }
      pendingCalls.clear();
      if (h.onClose) h.onClose(projectId);
    });
    ws.addEventListener("error", () => {
      if (h.onError) h.onError(projectId);
    });
    ws.addEventListener("message", (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (msg.id !== undefined && msg.method === undefined) {
        const p = pendingCalls.get(msg.id);
        if (!p) {
          return;
        }
        pendingCalls.delete(msg.id);
        if (msg.error) {
          p.reject(new Error(msg.error.message || "rpc error"));
        } else {
          p.resolve(msg.result);
        }
        return;
      }
      if (msg.id !== undefined && msg.method) {
        if (h.onServerRequest) h.onServerRequest(projectId, msg);
        return;
      }
      if (msg.method && h.onNotification) {
        h.onNotification(projectId, msg);
      }
    });

    return conn;
  }

  /** @param {any} conn */
  function setActiveConn(conn) {
    active = conn;
  }

  function activeConn() {
    return active;
  }

  // The four helpers the adapter fragments call. Same signatures as before;
  // the destination is now "whichever project is active".

  /** @param {string} method @param {any} params @returns {Promise<any>} */
  function wsSend(method, params) {
    if (!active) {
      return Promise.reject(new Error("not connected"));
    }
    return active.send(method, params);
  }

  /** @param {string} method @param {any} params @returns {{id: number, done: Promise<any>}} */
  function wsSendCancellable(method, params) {
    if (!active) {
      return { id: -1, done: Promise.reject(new Error("not connected")) };
    }
    return active.sendCancellable(method, params);
  }

  /** @param {string} method @param {any} params */
  function wsNotify(method, params) {
    if (active) active.notify(method, params);
  }

  /** @param {any} id @param {any} result */
  function wsReply(id, result) {
    if (active) active.reply(id, result);
  }

  // Test seam: adapter-test.mjs drives the outbound path by posting a window
  // message, because `host` lives inside this IIFE and nothing outside can
  // reach it. Harmless in a real page — no renderer fragment posts this type.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "__host_dispatch__") {
      host.postMessage(ev.data.payload);
    }
  });
```

Note two deletions: the module-level `onServerRequest`/`onNotification`
variables are gone (each connection carries its own handlers), and the
`connect()` call at the foot of `10-adapter-session.js` will be replaced in
Task 7. The test seam moves out of `connect` and runs at load time.

- [ ] **Step 4: Keep the single-project path alive for now**

`10-adapter-session.js` ends with `connect();`, and `20-`/`30-` assign to
`onNotification`/`onServerRequest`. Until Task 7 lands, keep the bundle working
by replacing that last line of `10-adapter-session.js` with:

```js
  // Superseded in 40-projects.js, which owns connections once it exists. Until
  // then this preserves the single-project behaviour the tests describe.
  setActiveConn(
    createConn(new URLSearchParams(location.search).get("project") || "", {
      onOpen: () => {
        toRenderer({ type: "status", status: "ok" });
        void onConnected();
      },
      onClose: () => {
        // A dropped socket ends the session on the core side, so say so plainly
        // rather than reconnecting into what looks like the same conversation.
        toRenderer({
          type: "status",
          status: "error",
          detail: "disconnected — reload to start a new session",
        });
      },
      onError: () => {
        toRenderer({ type: "status", status: "error", detail: "connection error" });
      },
      onNotification: (_projectId, msg) => handleNotification(msg),
      onServerRequest: (_projectId, msg) => handleServerRequest(msg),
    })
  );
```

and delete the two `onNotification = handleNotification;` /
`onServerRequest = handleServerRequest;` assignment lines from
`20-adapter-events.js` and `30-adapter-asks.js`.

- [ ] **Step 5: Re-bundle and run the tests**

```bash
node ui/web/scripts/bundle-web.mjs
node ui/web/scripts/adapter-test.mjs
node ui/web/scripts/check-web.mjs
```

Expected: all 13 adapter tests pass unchanged, and `check-web.mjs` reports
`web checks passed`. This is the whole point of the task: a refactor the
existing tests cannot tell apart.

- [ ] **Step 6: Prove the VS Code bundle did not move**

```bash
node ui/vscode/scripts/bundle-chat.mjs
git status --short ui/vscode
```

Expected: no modifications under `ui/vscode`. If anything appears, a shared
fragment was edited — revert it; the constraint is absolute.

- [ ] **Step 7: Commit**

```bash
git add ui/web/src ui/web/static ui/web/scripts/adapter-test.mjs
git commit -m "refactor(web): one connection per project, behind the same four helpers"
```

---

## Task 7: Per-project state, and switching between projects

The three adapter fragments hold per-turn state in module-level variables. With
several projects live, each needs its own.

**Files:**
- Modify: `ui/web/src/10-adapter-session.js`
- Modify: `ui/web/src/20-adapter-events.js`
- Modify: `ui/web/src/30-adapter-asks.js`
- Test: `ui/web/scripts/adapter-test.mjs`

**Interfaces:**
- Consumes: `createConn`, `setActiveConn`, `activeConn`, `wsSend`,
  `wsSendCancellable`, `wsNotify`, `wsReply` (Task 6).
- Produces:
  - `onConnected(projectId)` — the handshake, recording this project's session
  - `activateProject(projectId)` — make it the one the renderer shows; repaints
    from `session.get` and re-raises a remembered prompt
  - `projectState(projectId)` → `{status, sessionId, pendingAsk}` where
    `status` is one of `"idle" | "working" | "asking"`
  - `noteProjectEvent(projectId, msg)` — called for every notification on every
    connection, active or not; returns `true` when the project's status changed
  - `forgetProjectState(projectId)` — drop a closed project's state

- [ ] **Step 1: Write the failing tests**

Append to `ui/web/scripts/adapter-test.mjs`. These drive two projects through
one bundle.

```js
test("a background project's tool call does not enter the active transcript", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();

  await handshakeFor(b, "A");
  await openBackground(b, "B");

  // B streams assistant text. A is active, so nothing may reach the renderer's
  // transcript.
  const before = b.inbound.filter((m) => m.type === "deltaSync").length;
  b.deliverTo("B", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "message_delta", content: "hello from B", step: 1 },
  });
  await tick();
  const after = b.inbound.filter((m) => m.type === "deltaSync").length;
  assert.equal(after, before, "a background project's text was folded into the active transcript");
});

test("a background project that starts a tool call reports itself as working", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    method: "agent/event",
    params: { type: "tool_call_start", tool_call_id: "t1", tool_call_name: "read", step: 1 },
  });
  await tick();

  const railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.ok(railed, "no projectList message was posted after a background event");
  const bEntry = railed.projects.find((p) => p.id === "B");
  assert.equal(bEntry.status, "working");
});

test("a permission request in a background project marks it asking and survives a switch", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 77,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  let railed = b.inbound.filter((m) => m.type === "projectList").pop();
  assert.equal(railed.projects.find((p) => p.id === "B").status, "asking");

  // No overlay while B is in the background.
  assert.equal(
    b.inbound.filter((m) => m.type === "permissionRequest").length,
    0,
    "a background project raised an overlay over the active project"
  );

  // Switching to B raises it, and answering replies on B's socket with B's id.
  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();
  answerOn(b, "B", "session.get", { ui_messages: [] });
  await tick();
  assert.equal(
    b.inbound.filter((m) => m.type === "permissionRequest").length,
    1,
    "switching to a project that is asking did not raise its prompt"
  );

  dispatch(b, { type: "permissionReply", approved: true, always: false });
  await tick();
  const reply = b.sent.filter((m) => m.id === 77 && m.result !== undefined).pop();
  assert.ok(reply, "the permission reply never went out");
  assert.match(reply.url, /project=B/, "the reply went to the wrong project's socket");
});

test("switching repaints from the core rather than a buffer", async () => {
  const b = loadBundle({ search: "?project=A" });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  dispatch(b, { type: "switchProject", projectId: "B" });
  await tick();

  const get = b.sent.filter((m) => m.method === "session.get").pop();
  assert.ok(get, "switching did not ask the core for the session");
  assert.match(get.url, /project=B/);
  answerOn(b, "B", "session.get", { ui_messages: [{ role: "user", text: "earlier" }] });
  await tick();

  const history = b.inbound.filter((m) => m.type === "history").pop();
  assert.deepEqual(history.messages, [{ role: "user", text: "earlier" }]);
});
```

Add these helpers beside the existing `handshake`:

```js
/** Drive one project's socket through the handshake to a started session. */
async function handshakeFor(b, projectId) {
  b.openFor(projectId);
  answerOn(b, projectId, "core.health", {
    workspace_root: "/" + projectId,
    project_id: projectId,
    protocol_version: 15,
    ops_version: 1,
    tools_version: 14,
  });
  await tick();
  answerOn(b, projectId, "initialize", {});
  await tick();
  answerOn(b, projectId, "session.start", { session_id: "s-" + projectId, restored: false });
  await tick();
}

/** Open a project's socket without making it active. */
async function openBackground(b, projectId) {
  dispatch(b, { type: "openProjectConnection", projectId });
  await tick();
  await handshakeFor(b, projectId);
}

/**
 * Answer a pending request on one project's socket.
 *
 * The answered-ids set hangs off the bundle handle, not off the module. A
 * module-level set would be shared by every test in the file, and the keys
 * collide across tests: the socket url is fixed (`127.0.0.1:9`), project ids
 * repeat (`A`, `B`), and each bundle's rpc ids restart at 1 — so the second
 * test's first request would look already answered and this helper would fail
 * with "no unanswered core.health".
 */
function answerOn(b, projectId, method, result) {
  if (!b.__answered) {
    b.__answered = new Set();
  }
  const req = b.sent.find(
    (m) =>
      m.method === method &&
      m.id !== undefined &&
      String(m.url).includes(`project=${encodeURIComponent(projectId)}`) &&
      !b.__answered.has(m.url + ":" + m.id)
  );
  assert.ok(
    req,
    `no unanswered ${method} on project ${projectId}; sent: ${b.sent
      .map((m) => m.method + "@" + m.url)
      .join(", ")}`
  );
  b.__answered.add(req.url + ":" + req.id);
  b.deliverTo(projectId, { jsonrpc: "2.0", id: req.id, result });
  return req;
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
node ui/web/scripts/adapter-test.mjs
```

Expected: the four new tests fail — `switchProject` and
`openProjectConnection` are unknown message types today, so the bundle answers
them with a `systemNote` and no `projectList` is ever posted.

- [ ] **Step 3: Make session state per project in `10-adapter-session.js`**

Replace the three module-level variables

```js
  let currentSessionId = "";
  let inFlightTurnId = null;
  let workspaceRoot = "";
```

with a per-project record:

```js
  /**
   * Per-project session state. The renderer shows one project at a time, so
   * exactly one of these is "current"; the others are what a switch restores.
   * @type {Map<string, {sessionId: string, inFlightTurnId: any, workspaceRoot: string, status: string, pendingAsk: any}>}
   */
  const perProject = new Map();
  let currentProjectId = "";

  /** @param {string} projectId */
  function projectState(projectId) {
    let st = perProject.get(projectId);
    if (!st) {
      st = { sessionId: "", inFlightTurnId: null, workspaceRoot: "", status: "idle", pendingAsk: null };
      perProject.set(projectId, st);
    }
    return st;
  }

  /** @param {string} projectId */
  function forgetProjectState(projectId) {
    perProject.delete(projectId);
  }

  /** The state the renderer is currently showing. */
  function current() {
    return projectState(currentProjectId);
  }
```

Rewrite `onConnected` to take the project id and write into that project's
record:

```js
  /** @param {string} projectId */
  async function onConnected(projectId) {
    const st = projectState(projectId);
    const conn = connFor(projectId);
    try {
      // core.health is answerable before initialize — it and initialize are the
      // only two methods exempt from the gate (internal/core/rpc_handler.go:76)
      // — and it is where project_root and project_id come from.
      const health = await conn.send("core.health", {});
      st.workspaceRoot = health.workspace_root || "";
      await conn.send("initialize", {
        project_root: st.workspaceRoot,
        project_id: health.project_id || "",
        protocol_version: health.protocol_version,
        ops_version: health.ops_version,
        tools_version: health.tools_version,
      });
      const started = await conn.send("session.start", {});
      st.sessionId = started.session_id || "";
      if (projectId === currentProjectId) {
        toRenderer({
          type: "header",
          model: health.model || "",
          provider: health.provider || "",
          sessionId: st.sessionId,
        });
        toRenderer({ type: "ready" });
        await refreshSessionList();
      }
    } catch (err) {
      const message = String(err && err.message ? err.message : err);
      if (projectId === currentProjectId) {
        toRenderer({ type: "error", message });
      }
      st.status = "idle";
    }
    renderProjects();
  }
```

Add the switch entry point:

```js
  /**
   * Make projectId the one the renderer shows. Repaints from the core rather
   * than from a buffer — session.get is what makes holding no background
   * scrollback affordable — and re-raises a prompt the project was waiting on.
   * @param {string} projectId
   */
  async function activateProject(projectId) {
    currentProjectId = projectId;
    const st = projectState(projectId);
    setActiveConn(connFor(projectId));

    toRenderer({ type: "clearMessages" });
    if (st.sessionId) {
      try {
        const view = await wsSend("session.get", { session_id: st.sessionId });
        toRenderer({ type: "history", messages: view.ui_messages || [] });
      } catch (err) {
        toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
      }
    }
    toRenderer({ type: "header", sessionId: st.sessionId });
    toRenderer({ type: "turnInFlight", inFlight: st.inFlightTurnId !== null });
    if (st.pendingAsk) {
      toRenderer(st.pendingAsk.rendererMessage);
    }
    await refreshSessionList();
    renderProjects();
  }
```

Everywhere else in the file, replace `currentSessionId` with
`current().sessionId` and `inFlightTurnId` with `current().inFlightTurnId`. In
`sendTurn`, also mark and clear the project's status so the rail shows it:

```js
  /** @param {any} msg */
  async function sendTurn(msg) {
    const st = current();
    if (!st.sessionId) {
      toRenderer({ type: "error", message: "no session — reload the page" });
      return;
    }
    toRenderer({ type: "userEcho", text: msg.text || "" });
    toRenderer({ type: "turnStart" });
    toRenderer({ type: "turnInFlight", inFlight: true });
    st.status = "working";
    renderProjects();

    const turn = wsSendCancellable("session.message", {
      session_id: st.sessionId,
      content: msg.text || "",
      // The web host has no editor to stage changes in, so a turn writes to
      // disk. Access mode still gates the shell (allow_exec below).
      apply: true,
      allow_exec: Boolean(msg.allowExec),
      profile: msg.profile || "",
    });
    st.inFlightTurnId = turn.id;
    try {
      await turn.done;
    } catch (err) {
      toRenderer({ type: "error", message: String(err && err.message ? err.message : err) });
    } finally {
      st.inFlightTurnId = null;
      st.status = "idle";
      renderProjects();
      toRenderer({ type: "turnInFlight", inFlight: false });
      toRenderer({ type: "turnComplete" });
    }
  }
```

In `dispatchToCore`, add the two new message types before the `default` arm:

```js
      case "switchProject":
        void switchProject(msg.projectId || "");
        return;

      case "openProjectConnection":
        void ensureConn(msg.projectId || "");
        return;
```

Finally, delete the `connect(...)`/`setActiveConn(...)` block added at the foot
of this file in Task 6 — Task 7's `40-projects.js` owns startup now. Also
replace `startSession`'s use of `currentSessionId` the same way, and remove the
`connect();` line entirely.

- [ ] **Step 4: Make turn accumulation per project in `20-adapter-events.js`**

Replace the two module-level variables

```js
  let turnText = "";
  const liveToolBlocks = new Map();
```

with per-project maps and a router:

```js
  /** @type {Map<string, string>} */
  const turnTextByProject = new Map();
  /** @type {Map<string, Map<string, any>>} */
  const liveToolBlocksByProject = new Map();

  /** @param {string} projectId */
  function toolBlocks(projectId) {
    let m = liveToolBlocksByProject.get(projectId);
    if (!m) {
      m = new Map();
      liveToolBlocksByProject.set(projectId, m);
    }
    return m;
  }

  /**
   * Every notification from every connection lands here first. A project the
   * renderer is not showing contributes its state to the rail and nothing to
   * the transcript: folding a background project's text into the visible
   * bubble is the bug this routing exists to prevent.
   * @param {string} projectId @param {any} msg
   */
  function noteProjectEvent(projectId, msg) {
    const st = projectState(projectId);
    const before = st.status;

    if (msg.method === "agent/event") {
      const ev = msg.params || {};
      switch (ev.type) {
        case "tool_call_start":
        case "message_delta":
        case "reasoning_delta":
          if (st.status === "idle") st.status = "working";
          break;
        case "done":
        case "error":
          if (st.status === "working") st.status = "idle";
          break;
        default:
          break;
      }
    }

    if (projectId === currentProjectId) {
      handleNotification(projectId, msg);
    }
    return st.status !== before;
  }
```

Change `handleNotification(msg)` to `handleNotification(projectId, msg)` and,
inside it, replace `turnText` with the project's entry:

```js
  /** @param {string} projectId @param {any} msg */
  function handleNotification(projectId, msg) {
    if (msg.method === "exec/output_chunk") {
      toRenderer({ type: "execChunk", chunk: (msg.params && msg.params.chunk) || "" });
      return;
    }
    if (msg.method !== "agent/event") {
      return;
    }
    const ev = msg.params || {};
    const isChild = ev.scope === "child";
    const blocks = toolBlocks(projectId);

    switch (ev.type) {
      case "message_delta":
        if (ev.content && !isChild) {
          const acc = (turnTextByProject.get(projectId) || "") + ev.content;
          turnTextByProject.set(projectId, acc);
          toRenderer({ type: "deltaSync", content: acc });
        }
        break;
```

and in the three tool-call arms replace `liveToolBlocks` with `blocks`. The
rest of the switch is unchanged.

Change the turn-reset listener at the foot of the file to reset only the
current project:

```js
  // A new turn starts with an empty transcript — for the project whose turn it
  // is, which is always the one the renderer is showing.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "turnStart") {
      turnTextByProject.set(currentProjectId, "");
      toolBlocks(currentProjectId).clear();
    }
  });
```

- [ ] **Step 5: Make pending asks per project in `30-adapter-asks.js`**

Replace the two module-level ids with the project record's `pendingAsk`, and
make the handler take a project id:

```js
  /**
   * @param {string} projectId @param {any} msg
   */
  function handleServerRequest(projectId, msg) {
    const st = projectState(projectId);
    switch (msg.method) {
      case "permission/request":
        st.pendingAsk = {
          kind: "permission",
          id: msg.id,
          rendererMessage: { type: "permissionRequest", request: msg.params || {} },
        };
        st.status = "asking";
        break;
      case "question/ask":
        st.pendingAsk = {
          kind: "question",
          id: msg.id,
          rendererMessage: {
            type: "questionAsk",
            questions: (msg.params && msg.params.questions) || [],
          },
        };
        st.status = "asking";
        break;
      default:
        // An unknown server request must still be answered, or the core waits.
        connFor(projectId).reply(msg.id, { error: "unsupported" });
        return;
    }
    // Only the project on screen may raise an overlay. A background project's
    // prompt waits in its record and is raised by activateProject.
    if (projectId === currentProjectId) {
      toRenderer(st.pendingAsk.rendererMessage);
    }
    renderProjects();
  }
```

and answer against the current project's record:

```js
  window.addEventListener("message", (ev) => {
    const msg = ev.data;
    if (!msg || typeof msg !== "object") {
      return;
    }
    if (msg.type === "__host_dispatch__" && msg.payload) {
      const p = msg.payload;
      const st = projectState(currentProjectId);
      if (p.type === "permissionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "permission") {
          return; // stale click; answering some other id would be worse
        }
        connFor(currentProjectId).reply(st.pendingAsk.id, {
          approved: Boolean(p.approved),
          always: Boolean(p.always),
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        renderProjects();
      } else if (p.type === "questionReply") {
        if (!st.pendingAsk || st.pendingAsk.kind !== "question") {
          return;
        }
        connFor(currentProjectId).reply(st.pendingAsk.id, {
          answers: Array.isArray(p.answers) ? p.answers : [],
        });
        st.pendingAsk = null;
        st.status = st.inFlightTurnId !== null ? "working" : "idle";
        renderProjects();
      }
    }
  });
```

`connFor` and `renderProjects` come from Task 8's fragment; they are referenced
here and defined there, which is the same hoisting the bundle already relies on
for `dispatchToCore`. The tests in this task will therefore only pass once
Task 8's fragment exists — so **Task 7 and Task 8 land in one commit**, and
Task 7's own step 6 below runs the tests after Task 8's fragment is written.

- [ ] **Step 6: Hold here and complete Task 8, then run**

Task 8 creates `40-projects.js` with `connFor`, `ensureConn`, `switchProject`,
`renderProjects` and the startup sequence. Write it, then:

```bash
node ui/web/scripts/bundle-web.mjs
node ui/web/scripts/adapter-test.mjs
```

Expected: all 17 tests pass.

---

## Task 8: The projects module and the rail

**Files:**
- Create: `ui/web/src/40-projects.js`
- Create: `ui/web/rail.css`
- Modify: `ui/web/index.src.html`
- Modify: `ui/web/scripts/bundle-web.mjs`

**Interfaces:**
- Consumes: `createConn`, `setActiveConn` (Task 6); `projectState`,
  `activateProject`, `onConnected`, `forgetProjectState`, `noteProjectEvent`,
  `handleServerRequest` (Task 7); `toRenderer` (prelude).
- Produces:
  - `connFor(projectId)` → the `Conn` for an open project, creating none
  - `ensureConn(projectId)` → creates the connection if absent, returns it
  - `switchProject(projectId)` → opens if closed, then `activateProject`
  - `renderProjects()` → posts a `projectList` renderer message and repaints
    the rail element
  - `refreshProjects()` → `GET /api/projects`, then `renderProjects()`

- [ ] **Step 1: Add the rail to the page**

In `ui/web/index.src.html`, add the stylesheet beside the existing one:

```html
  <link rel="stylesheet" href="chat.css" />
  <link rel="stylesheet" href="rail.css" />
```

and make the rail the first child of `#app`, immediately before
`<header id="chrome-strip">`:

```html
    <nav id="project-rail" class="project-rail" aria-label="Projects">
      <div id="project-rail-list" class="project-rail-list" role="tablist"></div>
      <button type="button" id="project-add-btn" class="project-rail-add" title="Add a project folder" aria-label="Add a project folder">+</button>
    </nav>
```

- [ ] **Step 2: Write the stylesheet**

Create `ui/web/rail.css`. It must not reference any `--vscode-*` variable —
this page is not a webview — and it must not restyle anything `chat.css` owns.

```css
/* The project rail. Web-only: chat.css is shared with the VS Code webview and
   must not learn about projects. */

#app {
  display: grid;
  grid-template-columns: 52px 1fr;
  grid-template-rows: 100%;
}

/* Everything chat.css lays out sits in the second column. */
#app > *:not(.project-rail) {
  grid-column: 2;
}

.project-rail {
  grid-column: 1;
  grid-row: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 8px 0;
  border-right: 1px solid rgba(127, 127, 127, 0.25);
  overflow-y: auto;
  overflow-x: hidden;
}

.project-rail-list {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  width: 100%;
}

.project-chip {
  position: relative;
  width: 36px;
  height: 36px;
  border: 1px solid rgba(127, 127, 127, 0.35);
  border-radius: 9px;
  background: transparent;
  color: inherit;
  font: 600 13px/1 system-ui, sans-serif;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* Active: a left marker, not only a tint, so the current project reads at a
   glance and without relying on hue. */
.project-chip[data-active="true"]::before {
  content: "";
  position: absolute;
  left: -8px;
  top: 4px;
  bottom: 4px;
  width: 3px;
  border-radius: 2px;
  background: currentColor;
}

/* State is carried by a corner badge whose SHAPE differs per state, so the
   rail is readable in either theme and without colour vision. */
.project-chip::after {
  content: "";
  position: absolute;
  right: -2px;
  bottom: -2px;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: transparent;
}

.project-chip[data-status="working"]::after {
  content: "";
  background: #d18616;
  animation: project-pulse 1.2s ease-in-out infinite;
}

.project-chip[data-status="asking"]::after {
  content: "!";
  background: #c8372d;
  color: #fff;
  font: 700 8px/10px system-ui, sans-serif;
  text-align: center;
  border-radius: 3px;
}

.project-chip[data-state="closed"] {
  opacity: 0.45;
  border-style: dashed;
}

@keyframes project-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

@media (prefers-reduced-motion: reduce) {
  .project-chip[data-status="working"]::after { animation: none; }
}

.project-rail-add {
  margin-top: auto;
  width: 36px;
  height: 36px;
  border: 1px dashed rgba(127, 127, 127, 0.5);
  border-radius: 9px;
  background: transparent;
  color: inherit;
  font: 400 18px/1 system-ui, sans-serif;
  cursor: pointer;
}

.project-menu {
  position: fixed;
  z-index: 60;
  min-width: 180px;
  padding: 4px;
  border: 1px solid rgba(127, 127, 127, 0.35);
  border-radius: 8px;
  background: Canvas;
  color: CanvasText;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.25);
}

.project-menu[hidden] { display: none; }

.project-menu button {
  display: block;
  width: 100%;
  padding: 6px 8px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
```

- [ ] **Step 3: Write the projects module**

Create `ui/web/src/40-projects.js`:

```js
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
      },
      onNotification: (id, msg) => {
        if (noteProjectEvent(id, msg)) {
          renderProjects();
        }
      },
      onServerRequest: (id, msg) => handleServerRequest(id, msg),
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
      const opened = await openProject(entry.path, false);
      if (!opened) {
        return;
      }
    }
    ensureConn(projectId);
    await activateProject(projectId);
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
      const err = new Error(body.error || `request failed (${res.status})`);
      // @ts-ignore — the caller distinguishes not_initialized from the rest.
      err.code = body.error || "";
      // @ts-ignore
      err.path = body.path || "";
      throw err;
    }
    return body;
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
  }

  // ---- the rail ----------------------------------------------------------

  /**
   * Repaint the rail and tell the renderer what the list looks like. The
   * renderer message exists for the tests and for any future consumer; the
   * rail's own DOM is written here because it is web-only.
   */
  function renderProjects() {
    const rows = known.map((p) => {
      const st = projectState(p.id);
      return {
        id: p.id,
        name: p.name || p.path,
        path: p.path,
        state: p.state,
        status: p.state === "closed" ? "closed" : st.status,
        active: p.id === currentProjectId,
      };
    });
    toRenderer({ type: "projectList", projects: rows });

    const list = document.getElementById("project-rail-list");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    for (const row of rows) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "project-chip";
      chip.dataset.projectId = row.id;
      chip.dataset.state = row.state;
      chip.dataset.status = row.status;
      chip.dataset.active = row.active ? "true" : "false";
      chip.title = row.path + (row.status === "asking" ? " — waiting for you" : "");
      chip.setAttribute("aria-label", row.name + " (" + row.status + ")");
      chip.textContent = (row.name || "?").slice(0, 2);
      list.appendChild(chip);
    }
  }

  // ---- input -------------------------------------------------------------

  const railMenu = (() => {
    const el = document.createElement("div");
    el.className = "project-menu";
    el.hidden = true;
    if (document.body && document.body.appendChild) {
      document.body.appendChild(el);
    }
    return el;
  })();

  /** @param {string} projectId @param {number} x @param {number} y */
  function openRailMenu(projectId, x, y) {
    railMenu.innerHTML = "";
    const entry = known.find((p) => p.id === projectId);
    if (entry && entry.state !== "closed") {
      const close = document.createElement("button");
      close.type = "button";
      close.textContent = "Close project";
      close.addEventListener("click", () => {
        railMenu.hidden = true;
        void closeProject(projectId);
      });
      railMenu.appendChild(close);
    }
    const forget = document.createElement("button");
    forget.type = "button";
    forget.textContent = "Remove from list";
    forget.addEventListener("click", () => {
      railMenu.hidden = true;
      void forgetProject(projectId);
    });
    railMenu.appendChild(forget);
    railMenu.style.left = x + "px";
    railMenu.style.top = y + "px";
    railMenu.hidden = false;
  }

  const railEl = document.getElementById("project-rail-list");
  if (railEl && railEl.addEventListener) {
    railEl.addEventListener("click", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      void switchProject(chip.dataset.projectId || "");
    });
    railEl.addEventListener("contextmenu", (ev) => {
      const chip = ev.target && ev.target.closest ? ev.target.closest(".project-chip") : null;
      if (!chip) {
        return;
      }
      if (ev.preventDefault) ev.preventDefault();
      openRailMenu(chip.dataset.projectId || "", ev.clientX || 0, ev.clientY || 0);
    });
  }
  if (document.addEventListener) {
    document.addEventListener("click", (ev) => {
      if (!railMenu.hidden && ev.target !== railMenu) {
        railMenu.hidden = true;
      }
    });
  }

  const addBtn = document.getElementById("project-add-btn");
  if (addBtn && addBtn.addEventListener) {
    addBtn.addEventListener("click", () => {
      void addProject();
    });
  }

  /**
   * Ask for a folder and open it. The native picker is only available when the
   * page is inside the desktop shell, which grants exactly this call; a plain
   * browser gets a path prompt instead.
   */
  async function addProject() {
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
    path = String(path || "").trim();
    if (!path) {
      return;
    }
    if (await openProject(path, false)) {
      return;
    }
    // The one recoverable refusal: the folder is not an Orchestra project yet.
    const agreed = window.confirm
      ? window.confirm(path + " is not an Orchestra project yet. Initialise it?")
      : false;
    if (agreed) {
      await openProject(path, true);
    }
  }

  // ---- startup -----------------------------------------------------------
  //
  // The shell passes exactly one ?project=; a plain `orchestra web` passes
  // none and the project-less socket serves the startup core. Either way the
  // list arrives from the API and the rail draws it.

  (async () => {
    const startupId = new URLSearchParams(location.search).get("project") || "";
    currentProjectId = startupId;
    setActiveConn(ensureConn(startupId));
    await refreshProjects();
    if (!startupId) {
      // No id in the URL: adopt whichever project the API reports as open, so
      // the rail's active marker matches the socket that is actually serving.
      const first = known.find((p) => p.state === "ready");
      if (first) {
        currentProjectId = first.id;
        renderProjects();
      }
    }
  })();
```

- [ ] **Step 4: Wire the fragment and the stylesheet into the bundler**

In `ui/web/scripts/bundle-web.mjs`, add the fragment to `order`, after
`30-adapter-asks.js`:

```js
  [webDir, "30-adapter-asks.js"],
  [webDir, "40-projects.js"],
  [sharedDir, "99-footer.txt"],
```

and copy the stylesheet beside the others:

```js
fs.copyFileSync(path.join(root, "rail.css"), path.join(outDir, "rail.css"));
```

- [ ] **Step 5: Bundle and run every check**

```bash
node ui/web/scripts/bundle-web.mjs
node ui/web/scripts/adapter-test.mjs
node ui/web/scripts/check-web.mjs
node ui/vscode/scripts/bundle-chat.mjs
git status --short ui/vscode
```

Expected: 17 adapter tests pass, `web checks passed`, and **nothing modified
under `ui/vscode`**.

- [ ] **Step 6: Look at it**

```bash
go build -o orchestra.exe ./cmd/orchestra
./orchestra.exe web --workspace-root . --no-open
```

Open the printed URL. Expected: a rail on the left with one chip for this
project, marked active; the chat works as before; sending a message makes the
chip show the working badge and clears it when the turn ends. Record in the
task report what you saw, including anything that looked wrong.

- [ ] **Step 7: Commit Tasks 7 and 8 together**

```bash
git add ui/web/src ui/web/rail.css ui/web/index.src.html ui/web/scripts ui/web/static
git commit -m "feat(web): a rail of projects, each with its own connection and state"
```

---

## Task 9: Notifications, docs, and the boundary check

**Files:**
- Modify: `ui/web/src/40-projects.js`
- Modify: `docs/PROTOCOL.md`
- Modify: `ui/desktop/README.md`
- Test: `ui/web/scripts/adapter-test.mjs`

**Interfaces:**
- Consumes: Task 1's recorded answer; `noteProjectEvent`/`projectState`
  (Task 7); `renderProjects` (Task 8).
- Produces: nothing further.

- [ ] **Step 1: Write the failing test**

Append to `ui/web/scripts/adapter-test.mjs`:

```js
test("a background project that starts asking raises one notification", async () => {
  const b = loadBundle({ search: "?project=A" });
  const notified = [];
  b.setTauri({
    notification: {
      sendNotification: (opts) => {
        notified.push(opts);
      },
    },
  });
  b.setFetchResponder(() => ({
    projects: [
      { id: "A", path: "/a", name: "a", state: "ready", error: "", opened_at: 1 },
      { id: "B", path: "/b", name: "b", state: "ready", error: "", opened_at: 2 },
    ],
  }));
  await tick();
  await handshakeFor(b, "A");
  await openBackground(b, "B");

  b.deliverTo("B", {
    jsonrpc: "2.0",
    id: 5,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();

  assert.equal(notified.length, 1, "a background project's prompt raised no notification");
  assert.match(notified[0].body || "", /b/i, "the notification does not name the project");

  // The active project's own prompt must NOT notify — it is already on screen.
  b.deliverTo("A", {
    jsonrpc: "2.0",
    id: 6,
    method: "permission/request",
    params: { tool: "bash", command: "ls" },
  });
  await tick();
  assert.equal(notified.length, 1, "the active project's prompt raised a notification");
});
```

Add the `setTauri` seam to `loadBundle`'s return value, and default
`sandbox.window.__TAURI__` to `undefined`:

```js
    setTauri: (api) => {
      sandbox.__TAURI__ = api;
    },
```

- [ ] **Step 2: Run it and watch it fail**

```bash
node ui/web/scripts/adapter-test.mjs
```

Expected: FAIL — `notified.length` is 0.

- [ ] **Step 3: Notify, but only for a project that is not on screen**

Add to `ui/web/src/40-projects.js`, above `renderProjects`:

```js
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
```

and call it from `onServerRequest`, which is where a prompt first arrives:

```js
      onServerRequest: (id, msg) => {
        handleServerRequest(id, msg);
        if (projectState(id).status === "asking") {
          notifyAsking(id);
        }
      },
```

**If Task 1's answer was "absent" or "rejected":** skip the `window.__TAURI__`
call above — keep `notifyAsking` and its guard exactly as written, since it
already degrades to doing nothing — and instead add a taskbar highlight on the
Rust side. In `ui/desktop/src-tauri/src/main.rs`, after the window is built,
there is nothing to hook here from the page, so the fallback is narrower: the
rail badge is the only signal, and the README must say so. Record which branch
you took.

- [ ] **Step 4: Run the tests**

```bash
node ui/web/scripts/bundle-web.mjs
node ui/web/scripts/adapter-test.mjs
```

Expected: 18 tests pass.

- [ ] **Step 5: Document the contract change**

In `docs/PROTOCOL.md`, find the `/api/projects` section and add, in the same
style as its neighbours:

```markdown
`GET /api/projects` returns open projects first, ordered by `opened_at`, then
the remembered projects the core does not hold, ordered by path. A remembered
entry has `state: "closed"`, `opened_at: 0`, and the same `id` it would have
when open, so a client can act on it by id alone. The list is not checked
against the filesystem: a remembered path on an unreachable share must not make
this request hang, so whether the directory still exists is discovered when the
client opens it.

`DELETE /api/projects/{id}` closes a project and leaves it remembered.
`DELETE /api/projects/{id}?forget=1` closes it if open and removes it from the
remembered list; it answers 204 for a project that was already closed, and 404
only when neither the registry nor the remembered list knows the id.

`POST /api/projects` records what it opened, so the list survives a restart.

`orchestra web` no longer reopens remembered projects at startup. It opens the
workspace it was given; the rest open when the client asks.
```

In `ui/desktop/README.md`, add a section:

```markdown
## The project rail

The window lists every remembered project down its left edge. A chip shows
whether that project is working, waiting for your answer, open and idle, or
merely remembered and closed. Clicking a closed project opens it; right-click
offers "Close project" (it stays in the list) and "Remove from list".

`~/.orchestra/projects.json` is that list. Closing a project does not change
it; only "Remove from list" does.

The page is served by `orchestra web` on loopback, so it is a remote URL as far
as Tauri is concerned and has no access to the shell by default.
`src-tauri/capabilities/core-page.json` grants it exactly two things: raise a
notification when a background project needs an answer, and open a folder
picker when adding a project. Nothing else.
```

- [ ] **Step 6: Run everything one last time**

```bash
go build ./... && go vet ./... && go test ./...
node ui/web/scripts/bundle-web.mjs
node ui/web/scripts/adapter-test.mjs
node ui/web/scripts/check-web.mjs
node ui/vscode/scripts/bundle-chat.mjs
git status --short ui/vscode
cd ui/desktop/src-tauri && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test && cd ../../..
```

Expected: everything passes and `git status --short ui/vscode` is empty.

- [ ] **Step 7: Verify by hand, with two projects**

```bash
node ui/desktop/scripts/build-sidecar.mjs
cargo run --manifest-path ui/desktop/src-tauri/Cargo.toml -- .
```

Then, in the window: add a second project folder, send it a request that needs
permission (a message that will run a shell command), switch to the first
project, and confirm the second project's chip shows the asking badge, a
notification arrives naming it, clicking the chip raises the prompt, and
answering it unblocks that project and not the other. Record each of those five
observations in the task report, individually. A summary saying "worked" is not
a report.

- [ ] **Step 8: Commit**

```bash
git add ui/web docs/PROTOCOL.md ui/desktop/README.md
git commit -m "feat(web): tell you when a project you are not watching needs an answer"
```

---

## Self-Review

**Spec coverage.**

| Spec requirement | Task |
|---|---|
| Rail of remembered projects, one icon each | 8 |
| Five states, distinguishable without colour alone | 8 (shape badges + dashed border + active marker) |
| Click closed to open, click open to switch | 8 (`switchProject`) |
| Add-folder control; context menu with close vs remove as separate items | 8 |
| Adding an uninitialised folder offers to initialize | 8 (`addProject` → `openProject(path, true)`) |
| One socket per open project; background sockets only listen | 6, 7 (`noteProjectEvent` routes) |
| Switching repaints from `session.get`, not a buffer | 7 (`activateProject`) |
| `GET /api/projects` returns closed entries with a state field | 4 |
| Startup no longer restores every remembered project | 5 |
| Per-project state derived from the event stream, no status endpoint | 7 |
| Narrow remote capability: notification + folder dialog only | 1 |
| Remote IPC verified first, in isolation, with a stated fallback | 1 |
| Adapter tests cover routing, switching, repaint, icon state | 7, 9 |
| "VS Code bundle did not change" checked by building and comparing | 6, 8, 9 |
| Two projects by hand, background agent, notification, click lands right | 9 |

No gaps.

**Placeholder scan.** Every code step carries the code. One place defers to a
prior result rather than to a placeholder: Task 9 Step 3's branch depends on
Task 1's recorded answer, which is a real input this plan produces. Task 7's
steps 5-6 explicitly land with Task 8 because their identifiers are defined
there; that is stated, not left to be discovered.

**Helper names verified against the tree, not assumed.** The Go tests append to
files that exist and call helpers that exist:
`internal/webtransport/projects_api_test.go` provides `initWS`,
`startRegistryServer` and `doJSON`; `internal/cli/web_serve_test.go` provides
`initialisedDir` (British spelling), `startServeWeb`, `waitFor` and
`readAnnounce`. `internal/projects/store_test.go` and `registry_test.go` exist.
`startRegistryServer` gains one parameter in Task 4 Step 1, which is why that
step exists at all. `TestServeWeb_SavesTheOpenListToTheGivenStore` keeps
passing after Task 5: it asserts the workspace is in the list, and
`known.Add(startup.Path)` puts it there at startup instead of at shutdown.

**Type consistency.** `projectState(projectId)` returns
`{sessionId, inFlightTurnId, workspaceRoot, status, pendingAsk}` in Task 7 and
is read with those exact names in Tasks 7, 8 and 9. `Conn` exposes
`send`/`sendCancellable`/`notify`/`reply`/`close`/`isOpen`/`projectId` in Task 6
and is used with only those in 7, 8 and 9. `status` is `"idle" | "working" |
"asking"` throughout, with `"closed"` substituted only in the rail row, where
`state` already carries it. Go side: `Options.Known *projects.Store`,
`Store.Paths/Add/Forget/PathForID`, `projects.ClosedProject`,
`projects.StateClosed` — each defined in Task 2 or 3 and used with the same
signature in Tasks 4 and 5.
