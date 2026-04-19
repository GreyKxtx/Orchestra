package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	daemonProjectRoot string
	daemonAddress     string
	daemonPort        int
	daemonScanSeconds int
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start local Orchestra daemon",
	Long:  "Starts a local daemon for a single project_root (v0.3)",
	Args:  cobra.NoArgs,
	RunE:  runDaemon,
}

func init() {
	daemonCmd.Flags().StringVar(&daemonProjectRoot, "project-root", "", "Project root to serve (required)")
	daemonCmd.Flags().StringVar(&daemonAddress, "address", "", "Address to bind (default from config or 127.0.0.1)")
	daemonCmd.Flags().IntVar(&daemonPort, "port", 0, "Port to bind (default from config or 8080)")
	daemonCmd.Flags().IntVar(&daemonScanSeconds, "scan-interval", 0, "Periodic scan interval in seconds (default from config or 10)")
	_ = daemonCmd.MarkFlagRequired("project-root")

	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	projectRoot := daemonProjectRoot

	// Load config from project root
	cfgPath := filepath.Join(projectRoot, ".orchestra.yml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config from %s: %w (run 'orchestra init' in project root first)", cfgPath, err)
	}

	address := cfg.Daemon.Address
	if daemonAddress != "" {
		address = daemonAddress
	}
	port := cfg.Daemon.Port
	if daemonPort != 0 {
		port = daemonPort
	}
	if address == "" {
		address = daemon.DefaultAddress
	}
	// vNext safety: HTTP daemon binds only to localhost.
	if address != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "[orchestra] WARNING: daemon address overridden to 127.0.0.1 (vNext safety)")
		address = "127.0.0.1"
	}
	if port == 0 {
		port = daemon.DefaultPort
	}

	scanInterval := time.Duration(cfg.Daemon.ScanInterval) * time.Second
	if daemonScanSeconds > 0 {
		scanInterval = time.Duration(daemonScanSeconds) * time.Second
	}
	if scanInterval <= 0 {
		scanInterval = daemon.DefaultScanInterval
	}

	cacheEnabled := true
	if cfg.Daemon.CacheEnabled != nil {
		cacheEnabled = *cfg.Daemon.CacheEnabled
	}

	srv, err := daemon.NewServer(daemon.ServerConfig{
		ProjectRoot:       projectRoot,
		Address:           address,
		Port:              port,
		ExcludeDirs:       cfg.ExcludeDirs,
		SkipBackups:       true,
		ScanInterval:      scanInterval,
		MaxCacheFileBytes: daemon.DefaultMaxCacheFileBytes,
		CacheEnabled:      cacheEnabled,
		CachePath:         cfg.Daemon.CachePath,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[orchestra] daemon starting for %s on http://%s:%d\n", projectRoot, address, port)

	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(sigCtx)
}
