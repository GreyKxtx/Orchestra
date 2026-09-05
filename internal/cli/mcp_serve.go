package cli

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

	if !mcpServeHTTP {
		return srv.Run(ctx, &mcpsdk.StdioTransport{})
	}

	token, err := resolveMCPToken(mcpServeToken)
	if err != nil {
		return err
	}
	return serveMCPHTTP(ctx, srv, mcpServeHTTPAddr, token)
}

// resolveMCPToken picks the bearer token an --http server will require:
// --mcp-token if set, otherwise $ORCH_MCP_TOKEN, otherwise an error. HTTP
// mode never starts without one - unlike `orchestra core --http` (debug-only,
// auto-generates a token if omitted), this server's whole point is real
// remote/multi-client use, so silently generating a token nobody was told
// about is the wrong default here.
func resolveMCPToken(flagToken string) (string, error) {
	token := flagToken
	if token == "" {
		token = os.Getenv("ORCH_MCP_TOKEN")
	}
	if token == "" {
		return "", fmt.Errorf("mcp serve --http requires a token: pass --mcp-token or set ORCH_MCP_TOKEN")
	}
	return token, nil
}

// requireBearerToken wraps next with a constant-time bearer-token check.
func requireBearerToken(token string, next http.Handler) http.Handler {
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
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

	httpSrv := &http.Server{Handler: requireBearerToken(token, handler)}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
