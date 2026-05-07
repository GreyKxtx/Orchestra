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

Connects to a child 'orchestra core' subprocess via stdio JSON-RPC.
Configure model and project_root via .orchestra.yml in the current
directory (create with 'orchestra init').`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}

		// Find ourselves on disk so the spawned subprocess can be `orchestra core`.
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot resolve own executable path: %w", err)
		}

		model := "(none)"
		if cfg, loadErr := config.Load(filepath.Join(cwd, ".orchestra.yml")); loadErr == nil && cfg != nil {
			if cfg.LLM.Model != "" {
				model = cfg.LLM.Model
			}
		}

		return tui.Run(tui.Config{
			Binary:        self,
			WorkspaceRoot: cwd,
			Model:         model,
			Mode:          "code",
			CWD:           filepath.Base(cwd),
		})
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
