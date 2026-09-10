package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	"github.com/orchestra/orchestra/internal/webtransport"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
	webui "github.com/orchestra/orchestra/ui/web"
	"github.com/spf13/cobra"
)

var (
	webWorkspaceRoot string
	webPort          int
	webToken         string
	webNoOpen        bool
	webDebug         bool
	webInit          bool
	webAnnounce      bool
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve the Orchestra web UI on localhost",
	Long: `Starts an Orchestra core, serves the web UI, and opens it in a browser.

The UI talks to the core over a WebSocket at /ws — a supported transport, see
docs/PROTOCOL.md. Binds to 127.0.0.1 only and requires a bearer token, which the
page receives from the server that serves it. Several projects can be open at
once (one core each, see /api/projects); one browser tab per project: a second
connection to the same project is refused while the first is live.

--init initialises the workspace first when it has no .orchestra.yml, so a
freshly picked folder works without a separate orchestra init.`,
	Args: cobra.NoArgs,
	RunE: runWeb,
}

func init() {
	webCmd.Flags().StringVar(&webWorkspaceRoot, "workspace-root", "", "Workspace root (default: current directory)")
	webCmd.Flags().IntVar(&webPort, "port", 0, "Port (0 = auto)")
	webCmd.Flags().StringVar(&webToken, "token", "", "Bearer token (auto-generated if empty)")
	webCmd.Flags().BoolVar(&webNoOpen, "no-open", false, "Do not open a browser")
	webCmd.Flags().BoolVar(&webDebug, "debug", false, "Enable debug logs to stderr")
	webCmd.Flags().BoolVar(&webInit, "init", false, "Initialise the workspace when it has no .orchestra.yml (same as orchestra init)")
	webCmd.Flags().BoolVar(&webAnnounce, "announce", false, "Sidecar mode: print one JSON line (the discovery object) to stdout when listening; exit when stdin closes")
	rootCmd.AddCommand(webCmd)
}

// webDiscovery is written to .orchestra/web.json while the server is running,
// mirroring core --http's discovery file (internal/cli/core.go:115-146). The
// token is plaintext, so the file is 0600 and removed on exit.
type webDiscovery struct {
	ProtocolVersion int    `json:"protocol_version"`
	WorkspaceRoot   string `json:"workspace_root"`
	URL             string `json:"url"`
	Port            int    `json:"port"`
	Token           string `json:"token"`
	PID             int    `json:"pid"`
	StartedAtUnix   int64  `json:"started_at_unix"`
	WrittenAtUnix   int64  `json:"written_at_unix"`
}

func webDiscoveryPath(workspace string) string {
	return filepath.Join(workspace, ".orchestra", "web.json")
}

func writeWebDiscovery(workspace string, d webDiscovery) (string, error) {
	now := time.Now().Unix()
	if d.StartedAtUnix == 0 {
		d.StartedAtUnix = now
	}
	if d.WrittenAtUnix == 0 {
		d.WrittenAtUnix = now
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	path := webDiscoveryPath(workspace)
	if err := fsutil.AtomicWriteFile(path, b, 0600); err != nil {
		return "", err
	}
	return path, nil
}

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

	streams := webIO{}
	if webAnnounce {
		// stdout is the announce channel and nothing else. Everything that
		// wrote to stdout before — init's messages, mostly — goes to stderr
		// for the rest of the process. The parent reads one line and no more.
		realStdout := os.Stdout
		os.Stdout = os.Stderr
		streams = webIO{Announce: realStdout, Stdin: os.Stdin}
	}
	return serveWeb(ctx, webRunConfig{
		Workspace: workspace,
		Port:      webPort,
		Token:     webToken,
		NoOpen:    webNoOpen || webAnnounce, // a sidecar never opens a browser
		Debug:     webDebug,
		Init:      webInit,
	}, streams)
}

// serveWeb is the body of `orchestra web`. It returns when ctx is cancelled.
func serveWeb(ctx context.Context, cfg webRunConfig, streams webIO) error {
	workspace, err := filepath.Abs(cfg.Workspace)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Sidecar mode: the parent owns our stdin. EOF means it has gone or wants
	// us gone; either way we shut down through the normal path so the deferred
	// discovery-file cleanup runs. Windows has no SIGTERM.
	if streams.Announce != nil && streams.Stdin != nil {
		go func() {
			_, _ = io.Copy(io.Discard, streams.Stdin)
			cancel()
		}()
	}

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
		Known:    known,
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
	now := time.Now().Unix()
	disc.StartedAtUnix, disc.WrittenAtUnix = now, now
	discPath, err := writeWebDiscovery(workspace, disc)
	if err == nil {
		defer func() { _ = os.Remove(discPath) }()
	}
	if streams.Announce != nil {
		b, err := json.Marshal(disc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] could not encode the announce line: %v\n", err)
		} else {
			_, _ = streams.Announce.Write(append(b, '\n'))
		}
	}

	pageURL := baseURL + "/?token=" + token
	if streams.Announce != nil {
		// Under --announce the parent already has the token from the
		// announce line; this stderr line is only ever surfaced back to the
		// user via the shell's error dialog (it echoes every child stderr
		// line), so the token must not ride along in it.
		fmt.Fprintf(os.Stderr, "[orchestra] web UI: %s\n", baseURL)
	} else {
		fmt.Fprintf(os.Stderr, "[orchestra] web UI: %s\n", pageURL)
	}
	if !cfg.NoOpen {
		if err := openBrowser(pageURL); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] could not open a browser (%v); open the URL above\n", err)
		}
	}

	<-ctx.Done()
	return nil
}

// openBrowser is best-effort: a failure prints the URL rather than aborting.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
