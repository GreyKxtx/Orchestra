package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive Orchestra terminal UI",
	Long: `Open the Orchestra terminal UI.

Phase 1: echo-only skeleton (no core connection yet). Use Ctrl+C to quit.

Configure model and project_root via .orchestra.yml in the current
directory (create with 'orchestra init').`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}

		model := "(none)"
		// Best-effort config load — TUI runs even without .orchestra.yml.
		cfgPath := filepath.Join(cwd, ".orchestra.yml")
		if cfg, loadErr := config.Load(cfgPath); loadErr == nil && cfg != nil {
			if cfg.LLM.Model != "" {
				model = cfg.LLM.Model
			}
		}

		return tui.Run(tui.Config{
			Model: model,
			Mode:  "code",
			CWD:   filepath.Base(cwd),
		})
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
