package cli

import (
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
)

// cliRunnerOptions is the Runner every CLI run builds from the project config.
// allowBrowser must be the run's --allow-browser: the browser client it adds is
// what lets the browser.* tools the agent offers on that flag actually run.
func cliRunnerOptions(cfg *config.ProjectConfig, dryRun, allowBrowser bool) tools.RunnerOptions {
	return tools.RunnerOptions{
		ExcludeDirs:        cfg.ExcludeDirs,
		ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
		ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
		WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
		WebMaxContentBytes: cfg.Web.MaxContentBytes,
		WebSearch:          cfg.Web.Search,
		LSP:                cfg.LSP,
		DryRun:             dryRun,
		Browser:            cfg.Browser,
		AllowBrowser:       allowBrowser,
		Embed:              cfg.ResolvedEmbed(),
	}
}
