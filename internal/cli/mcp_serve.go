package cli

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/internal/core"
)

var (
	mcpServeWorkspaceRoot string
	mcpServeHTTP          bool
	mcpServeHTTPAddr      string
	mcpServeToken         string
)

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve Orchestra's code-intelligence tools over MCP",
	Long: `Exposes explore, semantic_search, symbols, repo_map, runtime_query and the
lsp.* tools as MCP tools, so any MCP-capable client (Claude Code, Claude
Desktop, Cursor, ...) can use Orchestra's code understanding directly,
without going through Orchestra's own agent loop.

Binds to one workspace root for the whole process lifetime. stdio is the
default transport, matching how MCP hosts expect a locally configured
server to behave; --http switches to Streamable HTTP and always requires
a token (--mcp-token or $ORCH_MCP_TOKEN).`,
	Args: cobra.NoArgs,
	RunE: runMCPServe,
}

func init() {
	mcpServeCmd.Flags().StringVar(&mcpServeWorkspaceRoot, "workspace-root", "", "Workspace root (default: current directory)")
	mcpServeCmd.Flags().BoolVar(&mcpServeHTTP, "http", false, "Serve over Streamable HTTP instead of stdio")
	mcpServeCmd.Flags().StringVar(&mcpServeHTTPAddr, "http-addr", "127.0.0.1:0", "HTTP bind address; change only to expose the server beyond localhost")
	mcpServeCmd.Flags().StringVar(&mcpServeToken, "mcp-token", "", "Bearer token required on every HTTP request (or set ORCH_MCP_TOKEN); mandatory with --http")
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, _ []string) error {
	// Resolve the HTTP token before doing anything expensive: if --http is set
	// with no token available, fail fast here rather than after core.New has
	// already paid the cost of opening the CKG SQLite store and starting
	// background goroutines (or, worse, surfacing core.New's own error instead
	// of this more actionable one when both would have failed).
	var token string
	if mcpServeHTTP {
		var err error
		token, err = resolveMCPToken(mcpServeToken)
		if err != nil {
			return err
		}
	}

	workspace := mcpServeWorkspaceRoot
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get cwd: %w", err)
		}
		workspace = cwd
	}
	workspace, _ = filepath.Abs(workspace)

	c, err := core.New(workspace, core.Options{ToolsOnly: true})
	if err != nil {
		return fmt.Errorf("start core: %w", err)
	}
	defer func() { _ = c.Close() }()

	srv := c.MCPToolServer()

	// Wire the OS interrupt/terminate signals into ctx ourselves, rather than
	// relying on cmd.Context(): Execute() (internal/cli/root.go) calls plain
	// rootCmd.Execute() with no ExecuteContext/signal wiring at the root, so
	// cmd.Context() is never cancelled by Ctrl+C/SIGTERM. Without this, the
	// graceful-shutdown paths below (both stdio's srv.Run and serveMCPHTTP's
	// httpSrv.Shutdown) would be dead code and an interrupt would hard-kill
	// the process instead of draining in-flight requests. Same pattern as
	// runtimeServeCmd in internal/cli/runtime.go.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Same pattern as `orchestra core`: pre-populate the CKG in the background
	// so the first explore/semantic_search call doesn't pay the full initial
	// scan cost, and detect languages / auto-install missing language servers.
	// Without these, semantic_search against a workspace Orchestra's own agent
	// has never opened returns an empty index instead of real results - and
	// that tool is exactly what this feature is meant to showcase to external
	// MCP callers. Both calls are non-blocking (WarmupCKG triggers
	// UpdateGraphAsync; WarmupLSP spawns a goroutine).
	c.WarmupCKG(ctx)
	c.WarmupLSP(ctx)

	if !mcpServeHTTP {
		if err := srv.Run(ctx, &mcpsdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}

	return serveMCPHTTP(ctx, srv, mcpServeHTTPAddr, token)
}

// resolveMCPToken picks the bearer token an --http server will require:
// --mcp-token if set, otherwise $ORCH_MCP_TOKEN, otherwise an error. HTTP
// mode never starts without one - unlike `orchestra core --http` (debug-only,
// auto-generates a token if omitted), this server's whole point is real
// remote/multi-client use, so silently generating a token nobody was told
// about is the wrong default here. Both sources are trimmed of surrounding
// whitespace before the empty check, so a whitespace-only value (e.g.
// ORCH_MCP_TOKEN=" ") is correctly treated as no token rather than becoming
// a "working" one-space bearer token.
func resolveMCPToken(flagToken string) (string, error) {
	token := strings.TrimSpace(flagToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("ORCH_MCP_TOKEN"))
	}
	if token == "" {
		return "", fmt.Errorf("mcp serve --http requires a token: pass --mcp-token or set ORCH_MCP_TOKEN")
	}
	return token, nil
}

// requireBearerToken wraps next with a bearer-token check. The auth-scheme
// token ("Bearer") is compared case-insensitively per RFC 7235; only the
// credential portion needs a constant-time comparison since it's the secret.
func requireBearerToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, credential, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") ||
			subtle.ConstantTimeCompare([]byte(credential), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveMCPHTTP serves srv over Streamable HTTP at addr, gated by token, until
// ctx is cancelled.
func serveMCPHTTP(ctx context.Context, srv *mcpsdk.Server, addr, token string) error {
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "[orchestra] mcp serve: listening on http://%s\n", ln.Addr())

	httpSrv := &http.Server{
		Handler: requireBearerToken(token, handler),
		// Slowloris-class DoS mitigation (gosec G112): bound header reads and
		// idle keep-alive connections. Deliberately no ReadTimeout/WriteTimeout
		// on the whole request - Streamable HTTP legitimately holds long-lived
		// SSE response streams open, and a short whole-request timeout would
		// break that.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		// Streamable HTTP can hold long-lived SSE responses open, so Shutdown
		// may legitimately hit its timeout during a normal stop - that's a
		// clean exit, not a failure.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}
