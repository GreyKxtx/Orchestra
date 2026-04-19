package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/store"
)

func getDaemonClient(ctx context.Context, cfg *config.ProjectConfig) (*daemon.Client, bool) {
	cfgAddr := cfg.Daemon.Address
	cfgPort := cfg.Daemon.Port
	// If daemon is not enabled in config, do not guess URL from config (avoid slow connect attempts).
	// Discovery file and env var still work regardless of this flag.
	if !cfg.Daemon.Enabled {
		cfgAddr = ""
		cfgPort = 0
	}

	info, ok, err := daemon.DiscoverDaemonInfo(cfg.ProjectRoot, cfgAddr, cfgPort)
	if err != nil || !ok || info == nil || strings.TrimSpace(info.URL) == "" {
		return nil, false
	}

	client := daemon.NewClientWithToken(info.URL, info.Token)
	return client, true
}

func validateDaemonClient(ctx context.Context, client *daemon.Client, cfg *config.ProjectConfig) bool {
	health, err := client.Health(ctx)
	if err != nil {
		return false
	}
	if health.ProtocolVersion != daemon.ProtocolVersion {
		fmt.Fprintf(os.Stderr, "[orchestra] WARNING: daemon protocol mismatch (daemon=%d, cli=%d). Falling back to direct mode.\n", health.ProtocolVersion, daemon.ProtocolVersion)
		return false
	}

	localID, err := store.ComputeProjectID(cfg.ProjectRoot)
	if err != nil {
		return false
	}
	if health.ProjectID != localID {
		fmt.Fprintf(os.Stderr, "[orchestra] WARNING: daemon serves different project (daemon project_id=%s). Falling back to direct mode.\n", health.ProjectID)
		return false
	}

	return true
}
