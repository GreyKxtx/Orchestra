package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	coreWorkspaceRoot string
	coreDebug         bool
	coreHTTP          bool
	coreHTTPPort      int
	coreHTTPToken     string
)

var coreCmd = &cobra.Command{
	Use:   "core",
	Short: "Start Orchestra core (JSON-RPC over stdio)",
	Long:  "Runs the Orchestra core as a JSON-RPC 2.0 server over stdio (LSP-style framing).",
	Args:  cobra.NoArgs,
	RunE:  runCore,
}

func init() {
	coreCmd.Flags().StringVar(&coreWorkspaceRoot, "workspace-root", "", "Workspace root (default: current directory)")
	coreCmd.Flags().BoolVar(&coreDebug, "debug", false, "Enable debug logs to stderr")
	coreCmd.Flags().BoolVar(&coreHTTP, "http", false, "Enable local HTTP JSON-RPC server (debug)")
	coreCmd.Flags().IntVar(&coreHTTPPort, "http-port", 0, "HTTP port (0 = auto)")
	coreCmd.Flags().StringVar(&coreHTTPToken, "http-token", "", "HTTP token (auto-generated if empty)")
	rootCmd.AddCommand(coreCmd)
}

func runCore(cmd *cobra.Command, args []string) error {
	workspace := coreWorkspaceRoot
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get cwd: %w", err)
		}
		workspace = cwd
	}
	workspace, _ = filepath.Abs(workspace)

	c, err := core.New(workspace, core.Options{Debug: coreDebug})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	handler := core.NewRPCHandler(c)
	srv := jsonrpc.NewServer(handler, os.Stdin, os.Stdout)
	handler.SetNotifier(srv)
	handler.SetRequester(srv.Request)

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Optional HTTP debug server.
	if coreHTTP {
		// Clean up stale discovery file before starting.
		_ = cleanupStaleHTTPDiscovery(workspace)

		token := coreHTTPToken
		if token == "" {
			token = mustToken()
		}
		addr := fmt.Sprintf("127.0.0.1:%d", coreHTTPPort)
		baseURL, stopHTTP, err := jsonrpc.StartHTTP(ctx, handler, jsonrpc.HTTPOptions{
			Addr:   addr,
			Token:  token,
			Health: c.Health(),
		})
		if err != nil {
			return err
		}
		// Extract port from baseURL.
		httpPort := coreHTTPPort
		if httpPort == 0 {
			// Parse port from baseURL (e.g., "http://127.0.0.1:12345")
			if _, err := fmt.Sscanf(baseURL, "http://127.0.0.1:%d", &httpPort); err != nil {
				httpPort = 0
			}
		}
		// Generate instance ID (UUID-like, 16 bytes = 32 hex chars) for protection against PID reuse.
		instanceIDBytes := make([]byte, 16)
		_, _ = rand.Read(instanceIDBytes)
		instanceID := hex.EncodeToString(instanceIDBytes)

		// Write debug discovery file (best-effort) and remove on exit.
		// Note: Token is stored in plain text because this is debug-only mode and file is protected (0600, .gitignore).
		type coreHTTPDiscoveryInfo struct {
			ProtocolVersion int    `json:"protocol_version"` // JSON-RPC protocol version (same as protocol.ProtocolVersion)
			WorkspaceRoot   string `json:"workspace_root"`
			ProjectRoot     string `json:"project_root"` // Legacy alias for compatibility
			ProjectID       string `json:"project_id"`
			URL             string `json:"url"`
			HTTPPort        int    `json:"http_port"`
			Token           string `json:"token"`       // Plain token for debug mode (file is 0600, .gitignore)
			InstanceID      string `json:"instance_id"` // UUID-like identifier to detect PID reuse
			PID             int    `json:"pid"`
			StartedAtUnix   int64  `json:"started_at_unix"` // Unix timestamp (seconds) when core process started
			WrittenAtUnix   int64  `json:"written_at_unix"` // Unix timestamp (seconds) when discovery file was written
		}
		nowUnix := time.Now().Unix()
		discPath := filepath.Join(workspace, ".orchestra", "core.http.json")
		if b, err := json.MarshalIndent(coreHTTPDiscoveryInfo{
			ProtocolVersion: protocol.ProtocolVersion,
			WorkspaceRoot:   workspace,
			ProjectRoot:     workspace, // Legacy alias
			ProjectID:       c.Health().ProjectID,
			URL:             baseURL,
			HTTPPort:        httpPort,
			Token:           token,
			InstanceID:      instanceID,
			PID:             os.Getpid(),
			StartedAtUnix:   nowUnix, // Process start time (same as written_at_unix on first write)
			WrittenAtUnix:   nowUnix,
		}, "", "  "); err == nil {
			b = append(b, '\n')
			_ = daemon.AtomicWriteFile(discPath, b, 0600)
			defer func() { _ = os.Remove(discPath) }()
		}
		fmt.Fprintf(os.Stderr, "[orchestra] HTTP debug server enabled: %s (discovery: %s)\n", baseURL, discPath)
		defer func() { _ = stopHTTP() }()
	}

	return srv.Serve(ctx)
}

func mustToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// isProcessAlive checks if a process with the given PID is still running.
// Returns false if the process doesn't exist or if we can't determine its state.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, Signal(0) checks if process exists without sending a signal.
	// On Windows, it may return an error if process doesn't exist.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// cleanupStaleHTTPDiscovery removes core.http.json if it exists and the process is dead.
//
// Cleanup logic:
// - If PID is valid and process is alive → keep file (age doesn't matter).
// - If PID is dead → remove file.
// - If PID check fails (invalid/not found) → fallback to age-based cleanup (1 hour).
func cleanupStaleHTTPDiscovery(workspace string) error {
	discPath := filepath.Join(workspace, ".orchestra", "core.http.json")
	data, err := os.ReadFile(discPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file, nothing to clean up.
		}
		return fmt.Errorf("failed to read discovery file: %w", err)
	}

	var info struct {
		PID           int   `json:"pid"`
		StartedAtUnix int64 `json:"started_at_unix"` // Support both old and new field names
		StartedAt     int64 `json:"started_at"`      // Legacy field name (for backward compatibility)
		WrittenAtUnix int64 `json:"written_at_unix"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		// Malformed JSON - remove it.
		_ = os.Remove(discPath)
		return nil
	}

	// Normalize timestamp: prefer new field name, fallback to legacy.
	startedAt := info.StartedAtUnix
	if startedAt == 0 && info.StartedAt > 0 {
		startedAt = info.StartedAt
	}

	// Primary check: if PID is valid, check if process is alive.
	if info.PID > 0 {
		if isProcessAlive(info.PID) {
			// Process is alive, keep file regardless of age.
			return nil
		}
		// Process is dead, remove file.
		_ = os.Remove(discPath)
		return nil
	}

	// Fallback: PID is invalid/zero, use age-based cleanup.
	// If file is older than 1 hour, assume it's stale.
	if startedAt > 0 {
		age := time.Now().Unix() - startedAt
		if age > 3600 {
			_ = os.Remove(discPath)
			return nil
		}
	}

	return nil
}
