package cli

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/daemon"
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
