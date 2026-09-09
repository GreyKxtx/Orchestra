# Desktop Shell (Tauri, part B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Tauri window that starts `orchestra web` as a child process, opens the
served page, and shuts the child down cleanly when the window closes.

**Architecture:** A thin shell: `orchestra web` gains `--init` and `--announce`
(one JSON line on stdout when listening; EOF on stdin means shut down). The Rust
side is three files — pure decision logic (`boot.rs`), process handling
(`sidecar.rs`), Tauri glue (`main.rs`) — with no IPC, no bundled frontend and no
change to `ui/web` or the transport.

**Tech Stack:** Go 1.25 (cobra), Rust stable (1.98), tauri 2.11, tauri-build 2.6,
tauri-plugin-dialog 2.7, serde 1, serde_json 1; Node ≥ 20 for the
dependency-free sidecar build script.

**Spec:** `docs/superpowers/specs/2026-09-09-desktop-shell-design.md` (commits
`f1780c9`, `43dec64`)

---

## Global Constraints

Every task's requirements implicitly include this section.

1. **`ui/web`, `internal/webtransport`, `internal/projects`, `internal/core` and
   `protocol/version.go` are not modified.** Protocol stays 15. Go changes are
   confined to `internal/cli/web.go` and its tests.
2. **The `--announce` contract is exact** (spec, "The contract"): under
   `--announce`, stdout carries exactly one line — the JSON of `webDiscovery`
   (`protocol_version`, `workspace_root`, `url`, `port`, `token`, `pid`,
   `started_at_unix`, `written_at_unix`) — and nothing else, ever. Stdin EOF
   cancels the server context and `runWeb` returns `nil` after its deferred
   cleanup. Without `--announce`, behaviour is unchanged and stdin is not read.
3. **`--init` initialises only when `<dir>/.orchestra.yml` is absent**, via the
   existing `initProject(ctx, root, InitOptions{})` (`internal/cli/init_project.go`).
   An existing config is never touched.
4. **The shell spawns exactly**
   `orchestra web --workspace-root <dir> --no-open --port 0 --init --announce`.
5. **Sidecar lookup order:** `<dir of current_exe>/orchestra[.exe]`, then
   `orchestra` on `PATH`. No `bundle.externalBin` in `tauri.conf.json` in this
   part (tauri-build would fail every `cargo build` on a tree without the Go
   binary); it is added in part C.
6. **Project resolution order:** command-line argument → `last_project` in
   `~/.orchestra/desktop.json` → first entry of `~/.orchestra/projects.json` →
   native folder picker → exit 0 if cancelled. A candidate that is not an
   existing directory is skipped.
7. **Blocking dialogs never run on the main thread** (the dialog plugin
   dispatches to it and would deadlock). The boot sequence runs on a worker
   thread; the window is created through `run_on_main_thread`.
8. **Exit sequence:** window closed → drop the child's stdin → wait up to 3 s →
   kill. Announce timeout is 15 s. Failures show a native error dialog with the
   last stderr lines, then exit 1.
9. **Tests must not touch the user's home.** Go tests pass an explicit store
   path or set `USERPROFILE`/`HOME` for subprocesses to a temp dir. Rust tests
   take paths as parameters; nothing reads `~` in a unit test.
10. **Toolchain on this machine:** Rust is at `%USERPROFILE%\.cargo\bin`
    (rustup installed with `--no-modify-path`). Every cargo command in this plan
    is prefixed with `export PATH="$HOME/.cargo/bin:$PATH"` (Git Bash). MSVC
    Build Tools 14.44 and WebView2 152 are installed.
11. **Per-task verification by exit code.** Go:
    ```bash
    cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra" && go build ./... && go vet ./... && go test -count=1 -timeout 300s ./internal/cli/... && echo GREEN || echo RED
    ```
    Rust (from `ui/desktop/src-tauri`):
    ```bash
    export PATH="$HOME/.cargo/bin:$PATH" && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test && cargo build && echo GREEN || echo RED
    ```
    Before the final commit of the branch, the full Go suite
    (`go test -count=1 -timeout 300s ./...`) once.
12. **TDD and mutation checks as in part A:** write the failing test, run it,
    read the failure, write the minimal code, then break it the way the step
    says and watch the test catch it. Restore before committing.
13. **Work in a worktree** (`.worktrees/feat-desktop-shell`, branch
    `feat/desktop-shell`), as part A did. Line endings: the repo has
    `core.autocrlf=true`; ignore the LF/CRLF warnings.

---

### Task 1: `serveWeb` — lift `runWeb`'s body behind a config struct, add `--init`

`runWeb` reads five package-level flag variables and `cmd.Context()`. Nothing
in it can be unit-tested without a cobra command. This task extracts the body
into `serveWeb(ctx, cfg, io)` and adds `--init`; `--announce` comes in Task 2.

**Files:**
- Modify: `internal/cli/web.go` (`runWeb`, flags, `webCmd.Long`)
- Test: `internal/cli/web_serve_test.go` (create)

**Interfaces:**
- Consumes: `initProject(ctx context.Context, root string, opts InitOptions) error`
  (`init_project.go`); `projects.NewRegistry`, `projects.StorePath/LoadPaths/SavePaths`;
  `webtransport.Serve`; `writeWebDiscovery`, `cleanupStaleDiscovery`, `mustToken`,
  `openBrowser` (all existing in `internal/cli`).
- Produces:
  ```go
  type webRunConfig struct {
      Workspace string // absolute; required
      Port      int
      Token     string // "" → generated
      NoOpen    bool
      Debug     bool
      Init      bool   // initialise Workspace when it has no .orchestra.yml
      StorePath string // "" → projects.StorePath(); tests pass a temp file
  }
  type webIO struct {
      Announce io.Writer // nil → not in announce mode (Task 2)
      Stdin    io.Reader // watched for EOF only when Announce != nil (Task 2)
  }
  func serveWeb(ctx context.Context, cfg webRunConfig, streams webIO) error
  ```
  `runWeb` becomes a thin wrapper building `webRunConfig` from the flags.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/web_serve_test.go`:

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// startServeWeb runs serveWeb in the background against a temp store and
// returns a cancel and a done channel carrying its error.
func startServeWeb(t *testing.T, cfg webRunConfig, streams webIO) (cancel func(), done <-chan error) {
	t.Helper()
	if cfg.StorePath == "" {
		cfg.StorePath = filepath.Join(t.TempDir(), "projects.json")
	}
	ctx, c := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() { ch <- serveWeb(ctx, cfg, streams) }()
	t.Cleanup(func() {
		c()
		select {
		case <-ch:
		case <-time.After(15 * time.Second):
			t.Error("serveWeb did not return after cancel")
		}
	})
	return c, ch
}

func initialisedDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return root
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A bare directory is fatal without --init and initialised with it. This is
// what lets the desktop shell hand the server a folder the user just picked.
func TestServeWeb_InitCreatesTheConfigOnlyWhenAsked(t *testing.T) {
	bare := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := serveWeb(ctx, webRunConfig{Workspace: bare, NoOpen: true,
		StorePath: filepath.Join(t.TempDir(), "p.json")}, webIO{})
	if err == nil {
		t.Fatal("a bare workspace without --init must be an error, as today")
	}

	cancel2, done := startServeWeb(t, webRunConfig{Workspace: bare, NoOpen: true, Init: true}, webIO{})
	waitFor(t, ".orchestra.yml", func() bool {
		_, err := os.Stat(filepath.Join(bare, ".orchestra.yml"))
		return err == nil
	})
	cancel2()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb with --init: %v", err)
	}
}

// --init must leave an existing config byte-for-byte alone.
func TestServeWeb_InitLeavesAnExistingConfigUntouched(t *testing.T) {
	root := initialisedDir(t)
	before, _ := os.ReadFile(filepath.Join(root, ".orchestra.yml"))

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, Init: true}, webIO{})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if string(before) != string(after) {
		t.Fatal("--init rewrote an existing .orchestra.yml")
	}
}

// The store path is honoured: the started workspace lands in it on shutdown.
func TestServeWeb_SavesTheOpenListToTheGivenStore(t *testing.T) {
	root := initialisedDir(t)
	store := filepath.Join(t.TempDir(), "projects.json")

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, StorePath: store}, webIO{})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("store not written: %v", err)
	}
	if !strings.Contains(string(b), filepath.Base(root)) {
		t.Fatalf("store does not list the workspace: %s", b)
	}
	if _, err := os.Stat(webDiscoveryPath(root)); err == nil {
		t.Fatal("discovery file survived shutdown")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go test ./internal/cli/ -run TestServeWeb -v 2>&1 | head
```

Expected: FAIL to build — `undefined: serveWeb`, `undefined: webRunConfig`, `undefined: webIO`.

- [ ] **Step 3: Extract `serveWeb` and add `--init`**

In `internal/cli/web.go`:

Add the flag variable and registration:

```go
var (
	webWorkspaceRoot string
	webPort          int
	webToken         string
	webNoOpen        bool
	webDebug         bool
	webInit          bool
)
```

in `init()`:

```go
	webCmd.Flags().BoolVar(&webInit, "init", false, "Initialise the workspace when it has no .orchestra.yml (same as orchestra init)")
```

Add the types above `runWeb`:

```go
// webRunConfig is everything runWeb used to read from flags, so the server can
// be started from a test (and, in Task 2, from a parent process) without cobra.
type webRunConfig struct {
	Workspace string // absolute path; required
	Port      int    // 0 = auto
	Token     string // "" = generated
	NoOpen    bool
	Debug     bool
	Init      bool   // initialise Workspace when it has no .orchestra.yml
	StorePath string // "" = projects.StorePath(); tests pass a temp file
}

// webIO carries the parent-process streams. Announce == nil means "not a
// sidecar": nothing is written to it and Stdin is not watched.
type webIO struct {
	Announce io.Writer
	Stdin    io.Reader
}
```

Replace `runWeb` with the wrapper plus the extracted body:

```go
func runWeb(cmd *cobra.Command, args []string) error {
	workspace := webWorkspaceRoot
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get cwd: %w", err)
		}
		workspace = cwd
	}
	workspace, _ = filepath.Abs(workspace)

	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	return serveWeb(ctx, webRunConfig{
		Workspace: workspace,
		Port:      webPort,
		Token:     webToken,
		NoOpen:    webNoOpen,
		Debug:     webDebug,
		Init:      webInit,
	}, webIO{})
}

// serveWeb is the body of `orchestra web`. It returns when ctx is cancelled.
func serveWeb(ctx context.Context, cfg webRunConfig, streams webIO) error {
	workspace, err := filepath.Abs(cfg.Workspace)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// A folder the user picked in a dialog has no config yet; --init runs the
	// same initialisation the API runs for POST /api/projects {"init":true}.
	if cfg.Init {
		if _, err := os.Stat(filepath.Join(workspace, ".orchestra.yml")); err != nil {
			if err := initProject(ctx, workspace, InitOptions{}); err != nil {
				return fmt.Errorf("init workspace: %w", err)
			}
		}
	}

	reg := projects.NewRegistry(core.Options{Debug: cfg.Debug})
	defer reg.Shutdown()

	// The workspace the command was started in is the first project, and its
	// failure is still fatal: `orchestra web` in a directory that cannot be
	// opened has nothing to show.
	startup, err := reg.Open(ctx, workspace)
	if err != nil {
		return err
	}

	// Remembered projects reopen alongside it; the list is saved once now and
	// again on shutdown, which captures anything opened or closed through the
	// API. A convenience, not a transaction log.
	storePath, serr := cfg.StorePath, error(nil)
	if storePath == "" {
		storePath, serr = projects.StorePath()
	}
	persist := serr == nil
	if persist {
		remembered, lerr := projects.LoadPaths(storePath)
		if lerr != nil {
			// A list we could not read is a list we must not overwrite.
			fmt.Fprintln(os.Stderr, "[orchestra] "+lerr.Error()+"; open projects will not be remembered this run")
			persist = false
		} else {
			restoreProjects(ctx, reg, remembered)
		}
	}
	saveOpen := func() {
		if persist {
			_ = projects.SavePaths(storePath, reg.Paths())
		}
	}
	saveOpen()
	defer saveOpen()

	startupCore, _ := reg.Get(startup.ID)

	_ = cleanupStaleDiscovery(webDiscoveryPath(workspace))

	token := cfg.Token
	if token == "" {
		token = mustToken()
	}

	baseURL, stop, err := webtransport.Serve(ctx, webtransport.Options{
		Addr:     fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		Token:    token,
		Health:   startupCore.Health(),
		Assets:   webui.Assets(),
		Registry: reg,
		InitProject: func(ctx context.Context, root string) error {
			return initProject(ctx, root, InitOptions{})
		},
		NewProjectHandler: func(c *core.Core) (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(c)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(startupCore)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = stop() }()

	port := cfg.Port
	if port == 0 {
		_, _ = fmt.Sscanf(baseURL, "http://127.0.0.1:%d", &port)
	}
	disc := webDiscovery{
		ProtocolVersion: protocol.ProtocolVersion,
		WorkspaceRoot:   workspace,
		URL:             baseURL,
		Port:            port,
		Token:           token,
		PID:             os.Getpid(),
	}
	discPath, err := writeWebDiscovery(workspace, disc)
	if err == nil {
		defer func() { _ = os.Remove(discPath) }()
	}

	pageURL := baseURL + "/?token=" + token
	fmt.Fprintf(os.Stderr, "[orchestra] web UI: %s\n", pageURL)
	if !cfg.NoOpen {
		if err := openBrowser(pageURL); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] could not open a browser (%v); open the URL above\n", err)
		}
	}

	<-ctx.Done()
	return nil
}
```

Add `"io"` to the imports. `streams` is accepted but unused until Task 2 — Go
allows an unused parameter, so this compiles.

Update `webCmd.Long` (the sentence about tabs is already right; add the flag):

```go
	Long: `Starts an Orchestra core, serves the web UI, and opens it in a browser.

The UI talks to the core over a WebSocket at /ws — a supported transport, see
docs/PROTOCOL.md. Binds to 127.0.0.1 only and requires a bearer token, which the
page receives from the server that serves it. Several projects can be open at
once (one core each, see /api/projects); one browser tab per project: a second
connection to the same project is refused while the first is live.

--init initialises the workspace first when it has no .orchestra.yml, so a
freshly picked folder works without a separate orchestra init.`,
```

- [ ] **Step 4: Run the tests**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go build ./... && go vet ./internal/cli/ && go test -count=1 ./internal/cli/ -run "TestServeWeb|TestRestoreProjects|TestWriteWebDiscovery|TestCleanupStaleDiscovery|TestWebAssets" -v 2>&1 | grep -E "^(--- |ok|FAIL)"
```

Expected: PASS, all of them.

- [ ] **Step 5: Mutation-verify `--init`**

In `serveWeb`, change `if cfg.Init {` to `if false {`. Run
`-run TestServeWeb_InitCreatesTheConfigOnlyWhenAsked`; expected FAIL with
"serveWeb with --init:" (reg.Open returns not_initialized). Restore, re-run.

Then change the `os.Stat` guard to `if true {` (always init). Run
`-run TestServeWeb_InitLeavesAnExistingConfigUntouched`; expected: still PASS —
because `initProject` itself leaves an existing config alone (its first branch).
That is fine and worth knowing: the guard in `serveWeb` is a fast path, the
safety lives in `initProject`. Restore.

- [ ] **Step 6: Verify and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go build ./... && go vet ./... && go test -count=1 -timeout 300s ./internal/cli/... && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add internal/cli/web.go internal/cli/web_serve_test.go && git commit -m "feat(cli): serveWeb behind a config struct; orchestra web --init"
```

---

### Task 2: `--announce` — one JSON line on stdout, EOF on stdin shuts down

**Files:**
- Modify: `internal/cli/web.go` (`serveWeb`, `runWeb`, flags)
- Test: `internal/cli/web_serve_test.go` (append), `internal/cli/web_announce_test.go` (create)

**Interfaces:**
- Consumes: `serveWeb`, `webRunConfig`, `webIO` (Task 1); `webDiscovery` (existing).
- Produces: the `--announce` behaviour of Global Constraint 2. Nothing else
  depends on new Go symbols.

- [ ] **Step 1: Write the failing in-process tests**

Append to `internal/cli/web_serve_test.go`:

```go
// readAnnounce blocks until one line arrives on the announce pipe.
func readAnnounce(t *testing.T, r *io.PipeReader) webDiscovery {
	t.Helper()
	sc := bufio.NewScanner(r)
	lineCh := make(chan string, 1)
	go func() {
		if sc.Scan() {
			lineCh <- sc.Text()
		}
		close(lineCh)
	}()
	select {
	case line, ok := <-lineCh:
		if !ok {
			t.Fatal("announce pipe closed without a line")
		}
		var d webDiscovery
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("announce line is not the discovery object: %v\n%s", err, line)
		}
		return d
	case <-time.After(20 * time.Second):
		t.Fatal("no announce line within 20s")
	}
	return webDiscovery{}
}

// The announce line is the discovery object, and the token in it works.
func TestServeWeb_AnnounceLineIsTheDiscoveryObject(t *testing.T) {
	root := initialisedDir(t)
	annR, annW := io.Pipe()
	stdinR, stdinW := io.Pipe()

	_, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true},
		webIO{Announce: annW, Stdin: stdinR})
	d := readAnnounce(t, annR)

	if d.URL == "" || d.Token == "" || d.PID != os.Getpid() || d.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("announce = %+v, want url, token, our pid and protocol version", d)
	}
	req, _ := http.NewRequest(http.MethodGet, d.URL+"/health", nil)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health with the announced token = %d, want 200", resp.StatusCode)
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
}

// EOF on stdin is a *clean* shutdown: the list is saved, the discovery file gone.
func TestServeWeb_StdinEOFIsACleanShutdown(t *testing.T) {
	root := initialisedDir(t)
	store := filepath.Join(t.TempDir(), "projects.json")
	annR, annW := io.Pipe()
	stdinR, stdinW := io.Pipe()

	_, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, StorePath: store},
		webIO{Announce: annW, Stdin: stdinR})
	_ = readAnnounce(t, annR)
	_ = stdinW.Close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("no shutdown on stdin EOF")
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("open list not saved on EOF shutdown: %v", err)
	}
	if _, err := os.Stat(webDiscoveryPath(root)); err == nil {
		t.Fatal("discovery file survived EOF shutdown")
	}
}

// Without announce, stdin is nobody's business: a never-closed stdin must not
// keep the server from stopping on ctx cancel, and nothing is read from it.
func TestServeWeb_WithoutAnnounceStdinIsIgnored(t *testing.T) {
	root := initialisedDir(t)
	stdinR, stdinW := io.Pipe()
	defer func() { _ = stdinW.Close() }()

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true},
		webIO{Announce: nil, Stdin: stdinR})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	// Writing must not block: nobody is reading stdin in this mode. A reader
	// would make this Write return; we assert it does NOT complete.
	wrote := make(chan struct{})
	go func() { _, _ = stdinW.Write([]byte("x")); close(wrote) }()
	select {
	case <-wrote:
		t.Fatal("stdin was read without --announce")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
}
```

Add `"bufio"`, `"encoding/json"`, `"io"`, `"net/http"` and
`"github.com/orchestra/orchestra/protocol"` to the test file's imports.

- [ ] **Step 2: Write the failing real-process test**

Create `internal/cli/web_announce_test.go`. It re-executes the test binary as
the helper (the standard `os/exec` idiom), so the assertion is on the real
process's stdout — the only place the stdout redirection can be checked.

```go
package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a test: when ORCH_WEB_HELPER=1 it runs `orchestra
// web` with the arguments after "--" and exits with its status.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("ORCH_WEB_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		os.Stderr.WriteString("helper: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(0)
}

// Under --announce, stdout carries exactly one line — the discovery JSON — and
// nothing else, even though --init on a bare folder prints several messages
// (they must land on stderr). Closing stdin must end the process cleanly.
func TestWebAnnounce_StdoutIsExactlyOneLine(t *testing.T) {
	bare := t.TempDir()
	home := t.TempDir() // the helper must not touch the real ~/.orchestra

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--",
		"web", "--workspace-root", bare, "--no-open", "--port", "0", "--init", "--announce")
	cmd.Env = append(os.Environ(), "ORCH_WEB_HELPER=1", "USERPROFILE="+home, "HOME="+home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	var first string
	select {
	case first = <-lines:
	case <-time.After(30 * time.Second):
		t.Fatalf("no announce line within 30s; stderr:\n%s", stderr.String())
	}
	var d webDiscovery
	if err := json.Unmarshal([]byte(first), &d); err != nil {
		t.Fatalf("first stdout line is not the discovery JSON: %v\n%q", err, first)
	}
	if d.URL == "" || d.Token == "" {
		t.Fatalf("announce lacks url/token: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(bare, ".orchestra.yml")); err != nil {
		t.Fatalf("--init did not create the config: %v", err)
	}

	_ = stdin.Close()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("helper exited with error after stdin EOF: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("helper did not exit after stdin EOF; stderr:\n%s", stderr.String())
	}

	var extra []string
	for l := range lines {
		extra = append(extra, l)
	}
	if len(extra) != 0 {
		t.Fatalf("stdout carried %d extra line(s) besides the announce: %q", len(extra), extra)
	}
	// The one thing that must survive: the test binary's own "PASS" is printed
	// by the testing package only when the helper returns from the test
	// function — os.Exit above prevents it. If this assertion ever fires with
	// a "PASS" line, the helper stopped exiting early.
}
```

- [ ] **Step 3: Run to verify they fail**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go test -count=1 ./internal/cli/ -run "TestServeWeb_Announce|TestServeWeb_StdinEOF|TestServeWeb_WithoutAnnounce|TestWebAnnounce" -v 2>&1 | grep -E "^(--- |ok|FAIL)|_test.go:[0-9]+:"
```

Expected: `TestServeWeb_AnnounceLineIsTheDiscoveryObject` and
`TestServeWeb_StdinEOFIsACleanShutdown` FAIL with "no announce line within
20s"; `TestServeWeb_WithoutAnnounceStdinIsIgnored` PASSES already (nothing reads
stdin today — keep it, it guards the mode boundary); `TestWebAnnounce_...`
FAILS: `unknown flag: --announce` on stderr, then "no announce line".

- [ ] **Step 4: Implement `--announce`**

In `internal/cli/web.go`, add the flag:

```go
	webAnnounce      bool
```

```go
	webCmd.Flags().BoolVar(&webAnnounce, "announce", false, "Sidecar mode: print one JSON line (the discovery object) to stdout when listening; exit when stdin closes")
```

In `runWeb`, build the streams:

```go
	streams := webIO{}
	if webAnnounce {
		// stdout is the announce channel and nothing else. Everything that
		// wrote to stdout before — init's messages, mostly — goes to stderr
		// for the rest of the process. The parent reads one line and no more.
		realStdout := os.Stdout
		os.Stdout = os.Stderr
		streams = webIO{Announce: realStdout, Stdin: os.Stdin}
		webNoOpen = true // a sidecar never opens a browser
	}
	return serveWeb(ctx, webRunConfig{
		Workspace: workspace,
		Port:      webPort,
		Token:     webToken,
		NoOpen:    webNoOpen,
		Debug:     webDebug,
		Init:      webInit,
	}, streams)
```

In `serveWeb`, right after `defer cancel()`:

```go
	// Sidecar mode: the parent owns our stdin. EOF means it has gone or wants
	// us gone; either way we shut down through the normal path so the deferred
	// cleanup (list saved, discovery removed) runs. Windows has no SIGTERM.
	if streams.Announce != nil && streams.Stdin != nil {
		go func() {
			_, _ = io.Copy(io.Discard, streams.Stdin)
			cancel()
		}()
	}
```

and right after `writeWebDiscovery` (the `disc` variable from Task 1):

```go
	if streams.Announce != nil {
		b, err := json.Marshal(disc)
		if err == nil {
			_, _ = streams.Announce.Write(append(b, '\n'))
		}
	}
```

Note `disc` is the value passed to `writeWebDiscovery`; that function fills
`StartedAtUnix`/`WrittenAtUnix` on its own copy, so set them here too so the
announce equals the file:

```go
	now := time.Now().Unix()
	disc.StartedAtUnix, disc.WrittenAtUnix = now, now
```

placed before `writeWebDiscovery(workspace, disc)` (it keeps a non-zero
`StartedAtUnix` as given).

- [ ] **Step 5: Run the tests**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go build ./... && go vet ./internal/cli/ && go test -count=1 ./internal/cli/ -run "TestServeWeb|TestWebAnnounce" -v 2>&1 | grep -E "^(--- |ok|FAIL)|_test.go:[0-9]+:"
```

Expected: all PASS.

- [ ] **Step 6: Mutation-verify**

(a) Remove `os.Stdout = os.Stderr` in `runWeb`. Run `-run TestWebAnnounce`;
expected FAIL with "stdout carried N extra line(s)" — the init messages. Restore.

(b) Remove the stdin goroutine in `serveWeb`. Run
`-run TestServeWeb_StdinEOFIsACleanShutdown`; expected FAIL "no shutdown on
stdin EOF". Restore.

(c) Change `if streams.Announce != nil && streams.Stdin != nil` to
`if streams.Stdin != nil`. Run `-run TestServeWeb_WithoutAnnounceStdinIsIgnored`;
expected FAIL "stdin was read without --announce". Restore.

- [ ] **Step 7: Verify and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go build ./... && go vet ./... && go test -count=1 -timeout 300s ./internal/cli/... && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add internal/cli/web.go internal/cli/web_serve_test.go internal/cli/web_announce_test.go && git commit -m "feat(cli): orchestra web --announce — one JSON line on stdout, stdin EOF shuts down"
```

---

### Task 3: Rust scaffold and `boot.rs` — the pure decisions, tested

**Files:**
- Create: `ui/desktop/src-tauri/Cargo.toml`
- Create: `ui/desktop/src-tauri/build.rs`
- Create: `ui/desktop/src-tauri/tauri.conf.json`
- Create: `ui/desktop/src-tauri/capabilities/default.json`
- Create: `ui/desktop/src-tauri/placeholder/index.html`
- Create: `ui/desktop/src-tauri/icons/` — generated (see Step 1)
- Create: `ui/desktop/src-tauri/src/lib.rs` (`pub mod boot; pub mod sidecar;` — the
  library crate `orchestra_desktop`, so integration tests in `tests/` can reach
  the modules and `CARGO_BIN_EXE_orchestra-desktop`)
- Create: `ui/desktop/src-tauri/src/main.rs` (minimal; real glue in Task 5)
- Create: `ui/desktop/src-tauri/src/boot.rs`
- Create: `ui/desktop/src-tauri/.gitignore`

**Interfaces:**
- Produces (`boot.rs`, all `pub`):
  ```rust
  pub struct Announce { pub url: String, pub token: String, pub pid: i64, pub protocol_version: i64 }
  pub fn parse_announce(line: &str) -> Result<Announce, String>;
  pub fn sidecar_args(workspace: &Path) -> Vec<String>;   // exactly the Global Constraint 4 arguments
  pub struct Sources<'a> { pub arg: Option<&'a str>, pub desktop_json: Option<&'a str>, pub projects_json: Option<&'a str> }
  pub fn resolve_project(src: Sources, is_dir: &dyn Fn(&Path) -> bool, pick: &mut dyn FnMut() -> Option<PathBuf>) -> Option<PathBuf>;
  pub fn remember_project_json(path: &Path) -> String; // {"last_project": "..."}
  pub fn sidecar_candidates(exe_dir: Option<&Path>) -> Vec<PathBuf>; // [<exe_dir>/orchestra[.exe], PathBuf::from("orchestra[.exe]")]
  ```

- [ ] **Step 1: Scaffold**

`ui/desktop/src-tauri/Cargo.toml`:

```toml
[package]
name = "orchestra-desktop"
version = "0.1.0"
description = "Orchestra desktop shell"
edition = "2021"
rust-version = "1.77"
publish = false

[lib]
name = "orchestra_desktop"
path = "src/lib.rs"

[[bin]]
name = "orchestra-desktop"
path = "src/main.rs"

[build-dependencies]
tauri-build = { version = "2", features = [] }

[dependencies]
tauri = { version = "2", features = [] }
tauri-plugin-dialog = "2"
serde = { version = "1", features = ["derive"] }
serde_json = "1"

[profile.release]
strip = true
lto = true
codegen-units = 1
```

`ui/desktop/src-tauri/build.rs`:

```rust
fn main() {
    tauri_build::build()
}
```

`ui/desktop/src-tauri/tauri.conf.json` (no default window: it is created after
the announce; `frontendDist` must exist, so it points at a placeholder that is
never shown):

```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "productName": "Orchestra",
  "version": "0.1.0",
  "identifier": "dev.orchestra.desktop",
  "build": {
    "frontendDist": "placeholder"
  },
  "app": {
    "windows": [],
    "security": {
      "csp": null
    }
  },
  "bundle": {
    "active": false,
    "icon": [
      "icons/32x32.png",
      "icons/128x128.png",
      "icons/128x128@2x.png",
      "icons/icon.icns",
      "icons/icon.ico"
    ]
  }
}
```

`ui/desktop/src-tauri/capabilities/default.json`:

```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "identifier": "default",
  "description": "The main window uses no Tauri JS API; the page is served by orchestra web.",
  "windows": ["main"],
  "permissions": ["core:default"]
}
```

`ui/desktop/src-tauri/placeholder/index.html`:

```html
<!doctype html><title>Orchestra</title><p>Starting…</p>
```

Icons: tauri-build requires the icon files listed in `bundle.icon` to exist on
Windows (the `.ico` becomes the exe icon). Generate them from the existing logo
with the Tauri CLI once, via npx (no repo dependency):

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && npx --yes @tauri-apps/cli@^2 icon ../../web/static/logo.png -o icons 2>&1 | tail -3 && ls icons | head
```

Expected: `icons/32x32.png`, `128x128.png`, `128x128@2x.png`, `icon.icns`,
`icon.ico` (plus others; keep them all, they are small). If `logo.png` is not
square the CLI pads it; that is acceptable for B.

`ui/desktop/src-tauri/.gitignore`:

```
/target
/binaries
/gen
```

`ui/desktop/src-tauri/src/lib.rs` (Task 4 adds `pub mod sidecar;`):

```rust
//! The desktop shell's logic lives in the library so integration tests can
//! drive it against the real binary; main.rs is Tauri glue only.
pub mod boot;
```

`ui/desktop/src-tauri/src/main.rs` (minimal for this task — no window yet; Task 5
replaces it):

```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .run(tauri::generate_context!())
        .expect("error while running Orchestra desktop");
}
```

- [ ] **Step 2: Write the failing tests in `boot.rs`**

Create `ui/desktop/src-tauri/src/boot.rs` with ONLY the tests and stubs that do
not compile yet — the point is to see the red first. Write the whole file as it
will be in Step 4 but with the function bodies as `todo!()`:

```rust
//! Pure decisions of the shell: which project to open, what to spawn, what the
//! sidecar's announce line means. No I/O, no Tauri — so every branch is a unit
//! test.

use serde::Deserialize;
use std::path::{Path, PathBuf};

/// The one line `orchestra web --announce` prints: the discovery object.
#[derive(Debug, Deserialize, PartialEq)]
pub struct Announce {
    pub url: String,
    pub token: String,
    #[serde(default)]
    pub pid: i64,
    #[serde(default)]
    pub protocol_version: i64,
}

pub fn parse_announce(line: &str) -> Result<Announce, String> {
    todo!()
}

/// Exactly the arguments the spec fixes; a test pins them byte for byte.
pub fn sidecar_args(workspace: &Path) -> Vec<String> {
    todo!()
}

/// Where a project may come from, in priority order. All optional: a missing
/// file is `None`, a present one is its raw content.
pub struct Sources<'a> {
    pub arg: Option<&'a str>,
    pub desktop_json: Option<&'a str>,
    pub projects_json: Option<&'a str>,
}

/// argument → desktop.json last_project → first projects.json entry → pick().
/// Candidates that are not directories (per `is_dir`) are skipped.
pub fn resolve_project(
    src: Sources,
    is_dir: &dyn Fn(&Path) -> bool,
    pick: &mut dyn FnMut() -> Option<PathBuf>,
) -> Option<PathBuf> {
    todo!()
}

/// The content of ~/.orchestra/desktop.json after a successful start.
pub fn remember_project_json(path: &Path) -> String {
    todo!()
}

/// Where to look for the core: next to our executable first (that is where
/// every Tauri bundler puts an externalBin), then whatever `orchestra` PATH
/// resolves — the development fallback.
pub fn sidecar_candidates(exe_dir: Option<&Path>) -> Vec<PathBuf> {
    todo!()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dir_set(dirs: &[&str]) -> impl Fn(&Path) -> bool {
        let owned: Vec<PathBuf> = dirs.iter().map(PathBuf::from).collect();
        move |p: &Path| owned.iter().any(|d| d == p)
    }

    #[test]
    fn announce_is_the_discovery_object() {
        let line = r#"{"protocol_version":15,"workspace_root":"C:\\w","url":"http://127.0.0.1:5123","port":5123,"token":"abc","pid":42,"started_at_unix":1,"written_at_unix":1}"#;
        let a = parse_announce(line).unwrap();
        assert_eq!(a.url, "http://127.0.0.1:5123");
        assert_eq!(a.token, "abc");
        assert_eq!(a.pid, 42);
        assert_eq!(a.protocol_version, 15);
    }

    #[test]
    fn announce_rejects_a_log_line_and_a_missing_token() {
        assert!(parse_announce("[orchestra] web UI: http://127.0.0.1:1/?token=x").is_err());
        assert!(parse_announce(r#"{"url":"http://127.0.0.1:1"}"#).is_err(), "no token");
        assert!(parse_announce(r#"{"url":"http://127.0.0.1:1","token":""}"#).is_err(), "empty token");
        assert!(parse_announce("").is_err());
    }

    #[test]
    fn sidecar_args_are_the_contract() {
        let args = sidecar_args(Path::new("C:\\repo"));
        assert_eq!(
            args,
            vec!["web", "--workspace-root", "C:\\repo", "--no-open", "--port", "0", "--init", "--announce"]
        );
    }

    #[test]
    fn argument_beats_memory_beats_dialog() {
        let is_dir = dir_set(&["A", "B", "C"]);
        let mut picked = false;
        let mut pick = || {
            picked = true;
            Some(PathBuf::from("D"))
        };
        let got = resolve_project(
            Sources {
                arg: Some("A"),
                desktop_json: Some(r#"{"last_project":"B"}"#),
                projects_json: Some(r#"{"projects":["C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("A")));
        assert!(!picked, "the dialog must not open when an argument is given");
    }

    #[test]
    fn last_project_beats_the_registry_list() {
        let is_dir = dir_set(&["B", "C"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources {
                arg: None,
                desktop_json: Some(r#"{"last_project":"B"}"#),
                projects_json: Some(r#"{"projects":["C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("B")));
    }

    #[test]
    fn vanished_directories_and_corrupt_files_fall_through() {
        let is_dir = dir_set(&["C"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources {
                arg: Some("gone"),
                desktop_json: Some("{not json"),
                projects_json: Some(r#"{"projects":["also-gone","C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("C")));
    }

    #[test]
    fn the_dialog_is_last_and_its_cancel_is_none() {
        let is_dir = dir_set(&["D"]);
        let mut calls = 0;
        let mut pick = || {
            calls += 1;
            Some(PathBuf::from("D"))
        };
        let got = resolve_project(Sources { arg: None, desktop_json: None, projects_json: None }, &is_dir, &mut pick);
        assert_eq!(got, Some(PathBuf::from("D")));
        assert_eq!(calls, 1);

        let mut cancel = || None;
        let none = resolve_project(Sources { arg: None, desktop_json: None, projects_json: None }, &is_dir, &mut cancel);
        assert_eq!(none, None);
    }

    #[test]
    fn a_picked_non_directory_is_none_too() {
        // The dialog returned something that is not a directory (deleted
        // between pick and use): do not start a server on it.
        let is_dir = dir_set(&[]);
        let mut pick = || Some(PathBuf::from("ghost"));
        assert_eq!(
            resolve_project(Sources { arg: None, desktop_json: None, projects_json: None }, &is_dir, &mut pick),
            None
        );
    }

    #[test]
    fn remembered_json_round_trips_through_resolve() {
        let json = remember_project_json(Path::new("C:\\my repo"));
        let is_dir = dir_set(&["C:\\my repo"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources { arg: None, desktop_json: Some(&json), projects_json: None },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("C:\\my repo")));
    }

    #[test]
    fn candidates_are_next_to_exe_then_path() {
        let c = sidecar_candidates(Some(Path::new("C:\\app")));
        let exe = if cfg!(windows) { "orchestra.exe" } else { "orchestra" };
        assert_eq!(c, vec![PathBuf::from("C:\\app").join(exe), PathBuf::from(exe)]);
        assert_eq!(sidecar_candidates(None), vec![PathBuf::from(exe)]);
    }
}
```

- [ ] **Step 3: Run to verify they fail**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo test 2>&1 | grep -E "^test |panicked|not yet implemented|test result" | head -20
```

Expected: the first `cargo test` compiles the Tauri dependency tree (several
minutes); then every test panics with `not yet implemented`, `test result:
FAILED`.

- [ ] **Step 4: Implement `boot.rs`**

Replace the `todo!()` bodies:

```rust
pub fn parse_announce(line: &str) -> Result<Announce, String> {
    let a: Announce = serde_json::from_str(line.trim())
        .map_err(|e| format!("announce line is not the discovery object: {e}"))?;
    if a.url.is_empty() || a.token.is_empty() {
        return Err("announce line lacks url or token".to_string());
    }
    Ok(a)
}

pub fn sidecar_args(workspace: &Path) -> Vec<String> {
    vec![
        "web".into(),
        "--workspace-root".into(),
        workspace.to_string_lossy().into_owned(),
        "--no-open".into(),
        "--port".into(),
        "0".into(),
        "--init".into(),
        "--announce".into(),
    ]
}

#[derive(Deserialize)]
struct DesktopJson {
    last_project: Option<String>,
}

#[derive(Deserialize)]
struct ProjectsJson {
    #[serde(default)]
    projects: Vec<String>,
}

pub fn resolve_project(
    src: Sources,
    is_dir: &dyn Fn(&Path) -> bool,
    pick: &mut dyn FnMut() -> Option<PathBuf>,
) -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Some(a) = src.arg {
        candidates.push(PathBuf::from(a));
    }
    if let Some(d) = src.desktop_json {
        if let Ok(DesktopJson { last_project: Some(p) }) = serde_json::from_str::<DesktopJson>(d) {
            candidates.push(PathBuf::from(p));
        }
    }
    if let Some(p) = src.projects_json {
        if let Ok(list) = serde_json::from_str::<ProjectsJson>(p) {
            if let Some(first) = list.projects.first() {
                candidates.push(PathBuf::from(first));
            }
        }
    }
    if let Some(found) = candidates.into_iter().find(|c| is_dir(c)) {
        return Some(found);
    }
    // Last resort: ask. A cancelled dialog, or a pick that is not a directory
    // by the time we look, means there is nothing to start.
    pick().filter(|p| is_dir(p))
}

pub fn remember_project_json(path: &Path) -> String {
    serde_json::json!({ "last_project": path.to_string_lossy() }).to_string()
}

pub fn sidecar_candidates(exe_dir: Option<&Path>) -> Vec<PathBuf> {
    let name = if cfg!(windows) { "orchestra.exe" } else { "orchestra" };
    let mut out = Vec::new();
    if let Some(dir) = exe_dir {
        out.push(dir.join(name));
    }
    out.push(PathBuf::from(name));
    out
}
```

- [ ] **Step 5: Run the tests**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo test 2>&1 | grep -E "^test |test result"
```

Expected: 10 tests, `test result: ok`.

- [ ] **Step 6: Mutation-verify the resolution order**

Swap the two `candidates.push` blocks for `desktop_json` and `projects_json`.
Run `cargo test last_project_beats_the_registry_list`; expected FAIL
(`left: Some("C")`). Restore.

Remove the `.filter(|p| is_dir(p))` on the pick result. Run
`cargo test a_picked_non_directory_is_none_too`; expected FAIL. Restore.

- [ ] **Step 7: Verify and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test && cargo build && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add ui/desktop/src-tauri && git commit -m "feat(desktop): Tauri scaffold and the shell's pure decisions (boot.rs)"
```

`boot` is a public module of the library crate, so clippy raises no
`dead_code` for items `main.rs` does not use yet.

---

### Task 4: `sidecar.rs` — spawn with fallback, announce with timeout, stderr tail, graceful stop

**Files:**
- Create: `ui/desktop/src-tauri/src/sidecar.rs` (`Tail` unit tests inside)
- Create: `ui/desktop/src-tauri/tests/sidecar_process.rs` (process tests against
  the real binary via `CARGO_BIN_EXE_orchestra-desktop`, which Cargo sets for
  integration tests only)
- Modify: `ui/desktop/src-tauri/src/lib.rs` (add `pub mod sidecar;`)
- Modify: `ui/desktop/src-tauri/src/main.rs` (call the fake-sidecar hook first)

**Interfaces:**
- Consumes: `boot::{sidecar_candidates, sidecar_args, parse_announce, Announce}`.
- Produces:
  ```rust
  pub struct Sidecar { /* child + stderr tail */ }
  pub struct StartError { pub message: String, pub stderr_tail: String }
  pub fn start(exe_dir: Option<&Path>, workspace: &Path, announce_timeout: Duration) -> Result<(Sidecar, Announce), StartError>;
  pub fn spawn_and_announce(program: &Path, args: &[String], timeout: Duration) -> Result<(Sidecar, Announce), StartError>; // pub for the process tests
  pub fn maybe_run_fake_sidecar() -> bool;  // main() calls it first; true = this process was the fake
  impl Sidecar {
      pub fn stop(self, grace: Duration);      // close stdin, wait ≤ grace, then kill
      pub fn stderr_tail(&self) -> String;
  }
  pub struct Tail { /* ring buffer of the last N lines */ }
  impl Tail { pub fn new(max_lines: usize) -> Self; pub fn push(&mut self, line: String); pub fn text(&self) -> String; }
  ```

- [ ] **Step 1: Write the failing tests**

Create `ui/desktop/src-tauri/src/sidecar.rs` with every body `todo!()` and the
`Tail` unit tests inside it. Process behaviour is exercised from
`tests/sidecar_process.rs` with a throwaway child: the **real**
`orchestra-desktop` binary in "fake sidecar" mode (env `FAKE_SIDECAR` +
argument `--fake-sidecar`) — the Rust equivalent of Go's helper-process idiom —
so the tests run on every platform without an `orchestra` binary. Cargo sets
`CARGO_BIN_EXE_orchestra-desktop` for integration tests of a package with that
`[[bin]]`, and builds the bin before running them.

```rust
//! The core as a child process: find it, start it, read its one announce line,
//! keep the last lines of its stderr for error dialogs, and stop it cleanly.

use crate::boot::{parse_announce, sidecar_args, sidecar_candidates, Announce};
use std::collections::VecDeque;
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

/// Ring buffer of the last `max_lines` lines.
pub struct Tail {
    max_lines: usize,
    lines: VecDeque<String>,
}

impl Tail {
    pub fn new(max_lines: usize) -> Self {
        todo!()
    }
    pub fn push(&mut self, line: String) {
        todo!()
    }
    pub fn text(&self) -> String {
        todo!()
    }
}

pub struct StartError {
    pub message: String,
    pub stderr_tail: String,
}

pub struct Sidecar {
    child: Child,
    stdin: Option<ChildStdin>,
    tail: Arc<Mutex<Tail>>,
}

/// Spawn `program args…` with piped stdio and read exactly one stdout line
/// within `timeout`. On any failure the child (if any) is killed. Public so the
/// process tests can drive it against the fake sidecar.
pub fn spawn_and_announce(
    program: &Path,
    args: &[String],
    timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    todo!()
}

/// Try each candidate in order; the first that spawns wins. A candidate that
/// cannot be spawned (not found) is skipped with a note on stderr; one that
/// spawns but fails to announce is an error — that is the core misbehaving,
/// not a missing binary.
pub fn start(
    exe_dir: Option<&Path>,
    workspace: &Path,
    announce_timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    todo!()
}

impl Sidecar {
    pub fn stderr_tail(&self) -> String {
        self.tail.lock().map(|t| t.text()).unwrap_or_default()
    }

    /// Close stdin (the shutdown signal under --announce), wait up to `grace`
    /// for a clean exit, then kill.
    pub fn stop(mut self, grace: Duration) {
        todo!()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tail_keeps_only_the_last_lines_in_order() {
        let mut t = Tail::new(3);
        for l in ["a", "b", "c", "d", "e"] {
            t.push(l.to_string());
        }
        assert_eq!(t.text(), "c\nd\ne");
    }

    #[test]
    fn tail_of_zero_lines_is_empty() {
        let mut t = Tail::new(0);
        t.push("x".into());
        assert_eq!(t.text(), "");
    }
}
```

Create `ui/desktop/src-tauri/tests/sidecar_process.rs`:

```rust
//! Process behaviour of the sidecar module, driven against the real
//! orchestra-desktop binary running as a fake core (see
//! sidecar::maybe_run_fake_sidecar). Tests that touch the process environment
//! serialise on FAKE_LOCK.

use orchestra_desktop::sidecar::{spawn_and_announce, start};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant};

static FAKE_LOCK: Mutex<()> = Mutex::new(());

fn fake(mode: &str) -> (PathBuf, Vec<String>) {
    std::env::set_var("FAKE_SIDECAR", mode);
    (PathBuf::from(env!("CARGO_BIN_EXE_orchestra-desktop")), vec!["--fake-sidecar".into()])
}

    #[test]
    fn announce_is_read_and_stop_closes_cleanly() {
        let _g = FAKE_LOCK.lock().unwrap();
        let (exe, args) = fake("announce-then-wait-eof");
        let (sc, a) = spawn_and_announce(&exe, &args, Duration::from_secs(10)).map_err(|e| e.message).unwrap();
        assert_eq!(a.url, "http://127.0.0.1:1");
        assert_eq!(a.token, "t");
        let started = Instant::now();
        sc.stop(Duration::from_secs(5));
        assert!(started.elapsed() < Duration::from_secs(4), "stop waited for the kill instead of the EOF exit");
    }

    #[test]
    fn a_child_that_never_announces_is_a_timeout_with_its_stderr() {
        let _g = FAKE_LOCK.lock().unwrap();
        let (exe, args) = fake("stderr-then-hang");
        let err = spawn_and_announce(&exe, &args, Duration::from_millis(800)).err().expect("must time out");
        assert!(err.message.contains("15") || err.message.contains("announce"), "message: {}", err.message);
        assert!(err.stderr_tail.contains("boom"), "stderr tail was not captured: {:?}", err.stderr_tail);
    }

    #[test]
    fn a_child_that_prints_garbage_is_an_error() {
        let _g = FAKE_LOCK.lock().unwrap();
        let (exe, args) = fake("garbage");
        let err = spawn_and_announce(&exe, &args, Duration::from_secs(10)).err().expect("must fail");
        assert!(err.message.contains("discovery object"), "{}", err.message);
    }

    #[test]
    fn a_missing_binary_is_skipped_and_the_next_candidate_used() {
        // start() with an exe_dir that has no orchestra falls through to PATH.
        // The developer machine may well have a real orchestra on PATH, so the
        // test empties PATH for its duration (under the lock) — both candidates
        // must then fail, and the error must say what was looked for.
        let _g = FAKE_LOCK.lock().unwrap();
        let saved = std::env::var_os("PATH");
        std::env::set_var("PATH", "");
        let empty = std::env::temp_dir().join("orchestra-desktop-empty-dir");
        let _ = std::fs::create_dir_all(&empty);
        let result = start(Some(&empty), Path::new("."), Duration::from_millis(500));
        if let Some(p) = saved {
            std::env::set_var("PATH", p);
        }
        let err = result.err().expect("no orchestra anywhere here");
        assert!(err.message.contains("orchestra"), "{}", err.message);
    }
```

The four process tests above are written with the same four-space indentation
as they had inside a module; `cargo fmt` will flatten them — run it before
committing. Back in `src/sidecar.rs`, after the `tests` module, add the fake's
entry point:

```rust
/// Entry point of the fake sidecar. main() calls this first; it returns false
/// when FAKE_SIDECAR is not set (the normal case).
pub fn maybe_run_fake_sidecar() -> bool {
    let Ok(mode) = std::env::var("FAKE_SIDECAR") else { return false };
    if !std::env::args().any(|a| a == "--fake-sidecar") {
        return false;
    }
    let out = std::io::stdout();
    match mode.as_str() {
        "announce-then-wait-eof" => {
            let mut o = out.lock();
            let _ = writeln!(o, r#"{{"url":"http://127.0.0.1:1","token":"t","pid":1,"protocol_version":15}}"#);
            let _ = o.flush();
            // Block until the parent closes our stdin, then exit 0.
            let mut sink = String::new();
            let _ = std::io::stdin().read_line(&mut sink);
            let stdin = std::io::stdin();
            for _ in stdin.lock().lines() {}
        }
        "stderr-then-hang" => {
            eprintln!("boom: something went wrong");
            thread::sleep(Duration::from_secs(30));
        }
        "garbage" => {
            let mut o = out.lock();
            let _ = writeln!(o, "[orchestra] web UI: http://127.0.0.1:1/?token=x");
            let _ = o.flush();
            thread::sleep(Duration::from_secs(30));
        }
        _ => {}
    }
    std::process::exit(0);
}
```

Why the real binary and not the test harness: a `cargo test` binary parses its
own arguments as libtest flags and would reject `--fake-sidecar`, and the
harness's `main` is not ours. The real `orchestra-desktop` checks for the fake
mode first thing in `main()`. The child inherits `FAKE_SIDECAR` from the test
process's environment, which is why every test that sets it holds `FAKE_LOCK`.

- [ ] **Step 2: Wire the fake into `main.rs` and run to verify failure**

In `src/lib.rs` add `pub mod sidecar;`. In `main.rs`:

```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use orchestra_desktop::sidecar;

fn main() {
    if sidecar::maybe_run_fake_sidecar() {
        return;
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .run(tauri::generate_context!())
        .expect("error while running Orchestra desktop");
}
```

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo test 2>&1 | grep -E "^test |panicked|not yet implemented|test result" | head -20
```

Expected: the `Tail` unit tests and the four process tests panic with `not yet
implemented`.

- [ ] **Step 3: Implement**

```rust
impl Tail {
    pub fn new(max_lines: usize) -> Self {
        Tail { max_lines, lines: VecDeque::with_capacity(max_lines) }
    }
    pub fn push(&mut self, line: String) {
        if self.max_lines == 0 {
            return;
        }
        if self.lines.len() == self.max_lines {
            self.lines.pop_front();
        }
        self.lines.push_back(line);
    }
    pub fn text(&self) -> String {
        self.lines.iter().cloned().collect::<Vec<_>>().join("\n")
    }
}

const STDERR_TAIL_LINES: usize = 40;

fn spawn_and_announce(
    program: &Path,
    args: &[String],
    timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    let mut child = Command::new(program)
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| StartError { message: format!("cannot start {}: {e}", program.display()), stderr_tail: String::new() })?;

    let tail = Arc::new(Mutex::new(Tail::new(STDERR_TAIL_LINES)));
    if let Some(stderr) = child.stderr.take() {
        let tail = Arc::clone(&tail);
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                eprintln!("[orchestra] {line}");
                if let Ok(mut t) = tail.lock() {
                    t.push(line);
                }
            }
        });
    }

    let stdout = child.stdout.take().expect("stdout is piped");
    let (tx, rx) = mpsc::channel::<Option<String>>();
    thread::spawn(move || {
        let mut line = String::new();
        let got = BufReader::new(stdout).read_line(&mut line).ok().filter(|n| *n > 0).map(|_| line);
        let _ = tx.send(got);
    });

    let stdin = child.stdin.take();
    let mut sidecar = Sidecar { child, stdin, tail };

    let line = match rx.recv_timeout(timeout) {
        Ok(Some(line)) => line,
        Ok(None) => return Err(fail(sidecar, "the core exited before announcing its address".to_string())),
        Err(_) => return Err(fail(sidecar, format!("the core did not announce its address within {}s", timeout.as_secs()))),
    };
    match parse_announce(&line) {
        Ok(a) => Ok((sidecar, a)),
        Err(e) => Err(fail(sidecar, e)),
    }
}

/// Kill the child and turn the situation into a StartError with its stderr.
fn fail(mut sidecar: Sidecar, message: String) -> StartError {
    let stderr_tail = sidecar.stderr_tail();
    let _ = sidecar.child.kill();
    let _ = sidecar.child.wait();
    StartError { message, stderr_tail }
}

pub fn start(
    exe_dir: Option<&Path>,
    workspace: &Path,
    announce_timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    let args = sidecar_args(workspace);
    let candidates = sidecar_candidates(exe_dir);
    let mut last_not_found = String::new();
    for (i, program) in candidates.iter().enumerate() {
        match spawn_and_announce(program, &args, announce_timeout) {
            Ok(ok) => {
                if i > 0 {
                    eprintln!("[orchestra-desktop] no bundled core next to the app; using {} from PATH", program.display());
                }
                return Ok(ok);
            }
            Err(e) if e.message.starts_with("cannot start ") => {
                last_not_found = e.message;
                continue;
            }
            Err(e) => return Err(e),
        }
    }
    Err(StartError {
        message: format!("orchestra was not found next to the app or on PATH ({last_not_found})"),
        stderr_tail: String::new(),
    })
}

impl Sidecar {
    pub fn stop(mut self, grace: Duration) {
        drop(self.stdin.take()); // EOF: the shutdown signal under --announce
        let deadline = Instant::now() + grace;
        loop {
            match self.child.try_wait() {
                Ok(Some(_)) => return,
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(50)),
                _ => break,
            }
        }
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
```

- [ ] **Step 4: Run the tests**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo test 2>&1 | grep -E "^test |test result"
```

Expected: all boot, `Tail` and process tests pass (`cargo test` builds the bin
target before the integration tests, so `CARGO_BIN_EXE_orchestra-desktop`
resolves).

- [ ] **Step 5: Mutation-verify the graceful stop**

In `stop`, remove `drop(self.stdin.take());`. Run
`cargo test announce_is_read_and_stop_closes_cleanly`; expected FAIL: "stop
waited for the kill instead of the EOF exit" (the fake never sees EOF, stop
kills after 5 s). Restore.

In `spawn_and_announce`, replace the stderr thread body with `for _ in … {}`
(drop the `tail.push`). Run `cargo test a_child_that_never_announces…`;
expected FAIL "stderr tail was not captured". Restore.

- [ ] **Step 6: Verify and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test && cargo build && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add ui/desktop/src-tauri && git commit -m "feat(desktop): sidecar start with PATH fallback, announce timeout, stderr tail, graceful stop"
```

---

### Task 5: `main.rs` — the shell: boot thread, window, exit, error dialogs

**Files:**
- Modify: `ui/desktop/src-tauri/src/main.rs` (replace)

**Interfaces:**
- Consumes: `boot::{resolve_project, Sources, remember_project_json}`,
  `sidecar::{start, Sidecar, StartError}`; Tauri: `Builder`, `Manager`,
  `RunEvent`, `WebviewWindowBuilder`, `WebviewUrl`; dialog plugin:
  `DialogExt`, `MessageDialogKind`.
- Produces: the running application. No new symbols for later tasks.

- [ ] **Step 1: Write `main.rs`**

There is no unit test for the glue (it needs a display); Step 3 is the hand
smoke the spec requires, recorded in the task report.

```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::Duration;

use orchestra_desktop::{boot, sidecar};
use sidecar::Sidecar;
use tauri::{AppHandle, Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};
use tauri_plugin_dialog::{DialogExt, MessageDialogKind};

/// The running core, if any. Taken (and stopped) on exit.
struct CoreSlot(Mutex<Option<Sidecar>>);

const ANNOUNCE_TIMEOUT: Duration = Duration::from_secs(15);
const STOP_GRACE: Duration = Duration::from_secs(3);

fn main() {
    if sidecar::maybe_run_fake_sidecar() {
        return;
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(CoreSlot(Mutex::new(None)))
        .setup(|app| {
            // Everything that may block — the folder picker, waiting for the
            // announce — runs off the main thread: the dialog plugin's blocking
            // calls dispatch to the main thread and would deadlock it.
            let handle = app.handle().clone();
            std::thread::spawn(move || boot(handle));
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building Orchestra desktop")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                if let Some(core) = app.state::<CoreSlot>().0.lock().ok().and_then(|mut s| s.take()) {
                    core.stop(STOP_GRACE);
                }
            }
        });
}

fn orchestra_home() -> Option<PathBuf> {
    // Same location as the Go side: %USERPROFILE% / $HOME + .orchestra.
    std::env::var_os("USERPROFILE")
        .or_else(|| std::env::var_os("HOME"))
        .map(|h| PathBuf::from(h).join(".orchestra"))
}

fn boot(app: AppHandle) {
    let arg = std::env::args().nth(1);
    let home = orchestra_home();
    let read = |name: &str| home.as_ref().and_then(|h| std::fs::read_to_string(h.join(name)).ok());
    let desktop_json = read("desktop.json");
    let projects_json = read("projects.json");

    let is_dir = |p: &Path| p.is_dir();
    let mut pick = || {
        app.dialog()
            .file()
            .set_title("Choose a project folder")
            .blocking_pick_folder()
            .and_then(|f| f.into_path().ok())
    };
    let Some(workspace) = boot::resolve_project(
        boot::Sources {
            arg: arg.as_deref(),
            desktop_json: desktop_json.as_deref(),
            projects_json: projects_json.as_deref(),
        },
        &is_dir,
        &mut pick,
    ) else {
        app.exit(0); // nothing to show
        return;
    };
    let workspace = std::fs::canonicalize(&workspace).unwrap_or(workspace);

    let exe_dir = std::env::current_exe().ok().and_then(|p| p.parent().map(Path::to_path_buf));
    let (core, announce) = match sidecar::start(exe_dir.as_deref(), &workspace, ANNOUNCE_TIMEOUT) {
        Ok(ok) => ok,
        Err(e) => {
            fatal(&app, &e);
            return;
        }
    };
    if let Ok(mut slot) = app.state::<CoreSlot>().0.lock() {
        *slot = Some(core);
    }

    if let Some(h) = &home {
        let _ = std::fs::create_dir_all(h);
        let _ = write_atomic(&h.join("desktop.json"), boot::remember_project_json(&workspace).as_bytes());
    }

    let url = format!("{}/?token={}", announce.url.trim_end_matches('/'), announce.token);
    let Ok(url) = url.parse::<tauri::Url>() else {
        fatal(&app, &sidecar::StartError { message: format!("bad url from the core: {url}"), stderr_tail: String::new() });
        return;
    };
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        let built = WebviewWindowBuilder::new(&handle, "main", WebviewUrl::External(url))
            .title("Orchestra")
            .inner_size(1200.0, 800.0)
            .build();
        if let Err(e) = built {
            eprintln!("[orchestra-desktop] cannot create the window: {e}");
            handle.exit(1);
        }
    });
}

/// Show what went wrong (with the core's last stderr lines) and exit 1.
fn fatal(app: &AppHandle, e: &sidecar::StartError) {
    let mut text = e.message.clone();
    if !e.stderr_tail.is_empty() {
        text.push_str("\n\n");
        text.push_str(&e.stderr_tail);
    }
    eprintln!("[orchestra-desktop] {text}");
    app.dialog()
        .message(text)
        .kind(MessageDialogKind::Error)
        .title("Orchestra could not start")
        .blocking_show();
    app.exit(1);
}

/// Temp file then rename, so a crash never leaves a half-written memory file.
fn write_atomic(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, data)?;
    std::fs::rename(&tmp, path)
}
```

If the compiler reports that `FilePath::into_path` does not exist in the
installed `tauri-plugin-dialog`, use `f.as_path().map(Path::to_path_buf)`
(the other accessor the type offers) — one or the other exists in every 2.x.

- [ ] **Step 2: Build, lint, test**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && export PATH="$HOME/.cargo/bin:$PATH" && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test 2>&1 | grep -E "test result|error" && cargo build && echo GREEN || echo RED
```

Expected: GREEN.

- [ ] **Step 3: Hand smoke on Windows**

Build the Go binary from the worktree (it has `--init`/`--announce`) and put it
on `PATH` for the run — this exercises the development fallback:

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && S="C:/Users/KORSUN~1/AppData/Local/Temp/claude/c--Users-KorsunAndrii-Desktop-Project-Orchestra/6f5ca275-8033-4683-b237-1c4420a6f084/scratchpad" && mkdir -p "$S/bin" "$S/proj-b" && go build -o "$S/bin/orchestra.exe" ./cmd/orchestra && export PATH="$S/bin:$HOME/.cargo/bin:$PATH" && cd ui/desktop/src-tauri && USERPROFILE="$S/home" cargo run -- "$S/proj-b" 2> "$S/desktop.log" & sleep 25; tasklist | grep -iE "orchestra" ; cat "$S/desktop.log" | head -20; cat "$S/home/.orchestra/desktop.json"
```

Expected: a window titled "Orchestra" showing the chat; the log has
`[orchestra-desktop] no bundled core next to the app; using orchestra.exe from
PATH` and the core's stderr lines; `desktop.json` names `proj-b`;
`proj-b/.orchestra.yml` exists (init). **Close the window by hand**, then:

```bash
sleep 5; tasklist | grep -iE "orchestra" || echo "no orchestra processes"; ls "$S/proj-b/.orchestra/web.json" 2>/dev/null || echo "discovery file removed"
```

Expected: no `orchestra.exe` / `orchestra-desktop.exe` processes, no
`web.json`. Record both outputs in the task report. Then a second run **without
an argument**: `USERPROFILE="$S/home" cargo run` must open `proj-b` again with
no dialog (memory works). Then with `rm "$S/home/.orchestra/desktop.json"
"$S/home/.orchestra/projects.json"` a third run must show the folder picker;
cancel it; the app must exit 0 without a window.

- [ ] **Step 4: Commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add ui/desktop/src-tauri && git commit -m "feat(desktop): the shell — pick or remember a project, start the core, open the window, stop cleanly"
```

---

### Task 6: `build-sidecar.mjs` and the sidecar-next-to-exe path

**Files:**
- Create: `ui/desktop/scripts/build-sidecar.mjs`

**Interfaces:**
- Consumes: `go build ./cmd/orchestra`; `rustc --print host-tuple`.
- Produces: `ui/desktop/src-tauri/binaries/orchestra-<triple>[.exe]` (the name
  the part-C bundler expects) and a copy at
  `ui/desktop/src-tauri/target/<profile>/orchestra[.exe]` when that directory
  exists, so `cargo run` finds the bundled path first.

- [ ] **Step 1: Write the script**

```javascript
// Builds the Go core for the host and places it where the desktop shell looks.
//
//   node ui/desktop/scripts/build-sidecar.mjs [--profile debug|release]
//
// Two outputs:
//   src-tauri/binaries/orchestra-<host-triple>[.exe]  — the name Tauri's bundler
//       expects for bundle.externalBin (declared in part C).
//   src-tauri/target/<profile>/orchestra[.exe]        — next to the debug/release
//       shell executable, so `cargo run` uses the freshly built core instead of
//       falling back to PATH. Only written when that target dir exists.
//
// No dependencies; needs `go` and `rustc` on PATH.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const desktop = path.join(here, "..");
const repo = path.join(desktop, "..", "..");
const srcTauri = path.join(desktop, "src-tauri");

const profile = (() => {
  const i = process.argv.indexOf("--profile");
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : "debug";
})();
if (!["debug", "release"].includes(profile)) {
  console.error(`unknown profile ${profile}; use debug or release`);
  process.exit(2);
}

const ext = process.platform === "win32" ? ".exe" : "";
const triple = execFileSync("rustc", ["--print", "host-tuple"], { encoding: "utf8" }).trim();
if (!triple) {
  console.error("rustc --print host-tuple returned nothing; is Rust on PATH?");
  process.exit(1);
}

const binaries = path.join(srcTauri, "binaries");
fs.mkdirSync(binaries, { recursive: true });
const out = path.join(binaries, `orchestra-${triple}${ext}`);

execFileSync("go", ["build", "-o", out, "./cmd/orchestra"], { cwd: repo, stdio: "inherit" });
console.log(`built ${path.relative(repo, out)}`);

const targetDir = path.join(srcTauri, "target", profile);
if (fs.existsSync(targetDir)) {
  const beside = path.join(targetDir, `orchestra${ext}`);
  fs.copyFileSync(out, beside);
  console.log(`copied next to the shell: ${path.relative(repo, beside)}`);
} else {
  console.log(`no ${path.relative(repo, targetDir)} yet — run cargo build first to get the copy next to the shell`);
}
```

- [ ] **Step 2: Run it and prove the shell prefers the bundled core**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && export PATH="$HOME/.cargo/bin:$PATH" && node ui/desktop/scripts/build-sidecar.mjs && ls ui/desktop/src-tauri/binaries ui/desktop/src-tauri/target/debug | grep -i orchestra
```

Expected: `orchestra-x86_64-pc-windows-msvc.exe` in `binaries/` and
`orchestra.exe` in `target/debug/`. Then run the shell **with `PATH` stripped
of any `orchestra`** and confirm the fallback note does NOT appear:

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell/ui/desktop/src-tauri" && S="C:/Users/KORSUN~1/AppData/Local/Temp/claude/c--Users-KorsunAndrii-Desktop-Project-Orchestra/6f5ca275-8033-4683-b237-1c4420a6f084/scratchpad" && export PATH="$HOME/.cargo/bin:/usr/bin:/c/Windows/System32" && (which orchestra || echo "orchestra not on PATH — good") && USERPROFILE="$S/home" ./target/debug/orchestra-desktop.exe "$S/proj-b" 2> "$S/desktop2.log" & sleep 20; grep -c "using orchestra" "$S/desktop2.log" || echo "0 fallback notes — the bundled core was used"
```

Close the window by hand. Expected: the window opened, `0 fallback notes`.

- [ ] **Step 3: Commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add ui/desktop/scripts/build-sidecar.mjs && git commit -m "build(desktop): build-sidecar.mjs — the Go core next to the shell, named for the bundler"
```

---

### Task 7: CI job `desktop`

**Files:**
- Modify: `.github/workflows/ci.yml` (append a job)

- [ ] **Step 1: Add the job**

Append after the `vscode-extension` job:

```yaml
  desktop:
    strategy:
      fail-fast: false
      matrix:
        os: [windows-latest, ubuntu-latest]
    runs-on: ${{ matrix.os }}
    defaults:
      run:
        working-directory: ui/desktop/src-tauri
    steps:
      - uses: actions/checkout@v6

      # Tauri's Linux prerequisites (WebKitGTK 4.1 and friends).
      - name: Linux dependencies
        if: matrix.os == 'ubuntu-latest'
        run: |
          sudo apt-get update
          sudo apt-get install -y libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev libssl-dev libayatana-appindicator3-dev librsvg2-dev

      - uses: dtolnay/rust-toolchain@stable
        with:
          components: rustfmt, clippy

      - uses: swatinem/rust-cache@v2
        with:
          workspaces: ui/desktop/src-tauri -> target

      - name: Format
        run: cargo fmt --check

      - name: Clippy
        run: cargo clippy --all-targets -- -D warnings

      - name: Test
        run: cargo test

      - name: Build
        run: cargo build
```

- [ ] **Step 2: Validate the YAML locally**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && python -c "import yaml,sys; d=yaml.safe_load(open('.github/workflows/ci.yml')); print(sorted(d['jobs'].keys()))" 2>/dev/null || node -e "console.log('no pyyaml; eyeball indentation')"; grep -nE "^  desktop:|working-directory: ui/desktop|cargo (fmt|clippy|test|build)" .github/workflows/ci.yml
```

Expected: `desktop` listed among the jobs (or, without pyyaml, the four cargo
steps present under the job). The job itself runs on the first push.

- [ ] **Step 3: Commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add .github/workflows/ci.yml && git commit -m "ci: desktop job — fmt, clippy, test, build on windows and ubuntu"
```

---

### Task 8: Documentation

**Files:**
- Modify: `ui/desktop/README.md` (replace)
- Modify: `docs/PROTOCOL.md` (WebSocket subsection: `--announce`, `--init`)
- Modify: `README.md`, `README.ru.md` (Web UI section: the two flags; a Desktop paragraph)

- [ ] **Step 1: `ui/desktop/README.md`**

```markdown
# Orchestra Desktop

Tauri-оболочка поверх `orchestra web`: приложение запускает ядро как дочерний
процесс, открывает окно на его адрес и штатно гасит ядро при закрытии окна.
Внутри окна — тот же `ui/web`, что и в браузере; в оболочке нет ни IPC, ни
своего фронтенда. Спека: `docs/superpowers/specs/2026-09-09-desktop-shell-design.md`.

## Как это работает

1. Проект: аргумент командной строки → `last_project` из
   `~/.orchestra/desktop.json` → первая запись `~/.orchestra/projects.json` →
   нативный диалог выбора папки (отмена — выход).
2. Ядро: `orchestra[.exe]` рядом с exe оболочки, иначе `orchestra` из `PATH`
   (dev-режим, о чём пишется в stderr). Запуск:
   `orchestra web --workspace-root <dir> --no-open --port 0 --init --announce`.
3. Ядро печатает одну JSON-строку (объект discovery) в stdout — оболочка берёт
   из неё `url` и `token` и открывает окно на `<url>/?token=…`; сервер ставит
   cookie и редиректит на `/`.
4. Закрытие окна: оболочка закрывает stdin ядра (сигнал завершения под
   `--announce`), ждёт до 3 с, затем kill.

## Сборка и запуск

Нужны Rust stable (rustup), на Windows — MSVC Build Tools и WebView2; на Linux —
пакеты из `.github/workflows/ci.yml` (job `desktop`).

```bash
# ядро рядом с оболочкой (иначе берётся из PATH)
node ui/desktop/scripts/build-sidecar.mjs
cd ui/desktop/src-tauri
cargo run -- /path/to/project      # или без аргумента
cargo test                         # boot.rs и sidecar.rs
```

## Части

- **A — реестр проектов** (сделано): несколько ядер в одном `orchestra web`,
  `/api/projects`, `/ws?project=`, cookie-аутентификация.
- **B — оболочка** (этот каталог): окно, sidecar, выбор проекта.
- **C — упаковка**: `bundle.externalBin`, инсталляторы, подпись, автообновление.
  До C `cargo build` собирает только оболочку; ядро кладётся рядом скриптом.

Переключатель проектов, список сессий по проектам и настройки живут в `ui/web`
и делаются там.
```

- [ ] **Step 2: `docs/PROTOCOL.md`**

In the WebSocket subsection, after the Discovery paragraph, add:

```markdown
**Sidecar-режим (`--announce`, `--init`).** `orchestra web --announce` печатает
в stdout ровно одну строку — JSON того же объекта, что пишется в
`.orchestra/web.json` (`protocol_version`, `workspace_root`, `url`, `port`,
`token`, `pid`, `started_at_unix`, `written_at_unix`) — сразу после того, как
сервер начал слушать; больше в stdout ничего не пишется (всё остальное уходит
в stderr). EOF на stdin в этом режиме — сигнал завершения: сервер закрывается
штатно, список открытых проектов сохраняется, discovery-файл удаляется. Так
desktop-оболочка (`ui/desktop`) узнаёт адрес и токен и гасит ядро без
`SIGTERM`, которого на Windows нет. `--init` инициализирует стартовый
воркспейс без `.orchestra.yml` (тем же кодом, что `orchestra init` и
`POST /api/projects {"init":true}`); существующий конфиг не трогается. Без
этих флагов поведение прежнее.
```

- [ ] **Step 3: READMEs**

`README.md`, Web UI section flag list — add:

```markdown
- `--init` — initialise the workspace first when it has no `.orchestra.yml`
- `--announce` — sidecar mode: print the discovery JSON as one stdout line when
  listening, and shut down when stdin closes (what the desktop app uses)
```

and after the Web UI section a short one:

```markdown
## Desktop

`ui/desktop/` is a Tauri shell over `orchestra web`: it starts the core as a
child process, opens a window on it and stops it when the window closes. Build
and run from `ui/desktop/src-tauri` with `cargo run`; see `ui/desktop/README.md`.
Installers and auto-update are not built yet.
```

`README.ru.md` — the same two flags and paragraph in Russian, matching the
file's tone:

```markdown
- `--init` — сначала инициализировать воркспейс, если в нём нет `.orchestra.yml`
- `--announce` — sidecar-режим: одна JSON-строка discovery в stdout при старте
  и завершение по закрытию stdin (так работает desktop-приложение)
```

```markdown
## Desktop

`ui/desktop/` — Tauri-оболочка поверх `orchestra web`: запускает ядро дочерним
процессом, открывает на него окно и гасит при закрытии окна. Сборка и запуск —
`cargo run` из `ui/desktop/src-tauri`, подробности в `ui/desktop/README.md`.
Инсталляторов и автообновления пока нет.
```

- [ ] **Step 4: Verify references and commit**

Every path named in the new prose must exist:

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && ls ui/desktop/scripts/build-sidecar.mjs ui/desktop/src-tauri/src/boot.rs ui/desktop/src-tauri/src/sidecar.rs docs/superpowers/specs/2026-09-09-desktop-shell-design.md && grep -n "desktop:" .github/workflows/ci.yml && echo refs-ok
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && git add ui/desktop/README.md docs/PROTOCOL.md README.md README.ru.md && git commit -m "docs: desktop shell — how it starts the core, --announce/--init, build and run"
```

---

### Task 9: Final verification

- [ ] **Step 1: Everything, once**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && go build ./... && go vet ./... && go test -count=1 -timeout 300s ./... > /tmp/t-final.log 2>&1 && (cd ui/desktop/src-tauri && export PATH="$HOME/.cargo/bin:$PATH" && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test > /tmp/t-rust.log 2>&1) && node ui/web/scripts/check-web.mjs && node ui/web/scripts/adapter-test.mjs > /tmp/t-web.log 2>&1 && echo GREEN || echo RED
```

Expected: GREEN. `internal/core` has a known flake under full-suite load — if
it is the only failure, re-run that test alone and confirm it passes.

- [ ] **Step 2: Tick every checkbox in this plan and commit it**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-desktop-shell" && sed -i 's/^- \[ \] \*\*Step/- [x] **Step/' docs/superpowers/plans/2026-09-09-desktop-shell.md && git add docs/superpowers/plans/2026-09-09-desktop-shell.md && git commit -m "docs: desktop shell plan — all tasks done"
```

Then request code review before merging (as part A did — the reviewer gets the
spec, this plan, and the branch range).

---

## Notes for the executor

**What is fixed and what is free.** The `--announce` line, the stdin-EOF
semantics, the sidecar arguments, the lookup order and the resolution order are
the contract (Global Constraints 2–8); the Rust internals are not. If the
installed `tauri-plugin-dialog` names an accessor differently, adapt the one
call and say so in the task report.

**The dialogs deadlock the main thread.** If the folder picker or the error
dialog ever hangs the app, something moved back onto the main thread. `boot()`
must run on `std::thread::spawn`; only `WebviewWindowBuilder::build` goes
through `run_on_main_thread`.

**First `cargo build` is slow** (the Tauri tree, a few minutes). Subsequent
builds are incremental. Do not "fix" it by trimming dependencies.

**Not in this part:** `bundle.externalBin`, installers, updater, signing, tray,
multi-window, the project switcher in the web UI.
