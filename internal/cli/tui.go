package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui"
)

var tuiAllowExec bool

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive Orchestra terminal UI",
	Long: `Open the Orchestra terminal UI (same as running orchestra with no subcommand).

Connects to a child 'orchestra core' subprocess via stdio JSON-RPC.
Configure model and project_root via .orchestra.yml in the current
directory (create with 'orchestra init').

Agent edits run through staging + LSP validation during each turn; staged
changes are committed to disk when the agent completes successfully.`,
	Args: cobra.NoArgs,
	RunE: runTUI,
}

func runTUI(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve own executable path: %w", err)
	}

	projectID, err := cache.ComputeProjectID(cwd)
	if err != nil {
		return fmt.Errorf("compute project_id: %w", err)
	}

	cfgPath := filepath.Join(cwd, ".orchestra.yml")
	model := ""
	themeName := ""
	profile := ""
	allowExec := false
	needsOnboarding := false

	if cfg, loadErr := config.Load(cfgPath); loadErr == nil && cfg != nil {
		model = cfg.LLM.Model
		themeName = cfg.UI.Theme
		profile = strings.TrimSpace(cfg.Agent.Profile)
		allowExec = cfg.UI.AllowExec
	}
	if cmd.Flags().Changed("allow-exec") {
		allowExec = tuiAllowExec
	}
	if model == "" {
		needsOnboarding = true
	}

	return tui.Run(tui.Config{
		Binary:          self,
		WorkspaceRoot:   cwd,
		ProjectID:       projectID,
		Model:           model,
		Mode:            "build",
		CWD:             filepath.Base(cwd),
		NeedsOnboarding: needsOnboarding,
		ConfigPath:      cfgPath,
		Theme:           themeName,
		Profile:         profile,
		AllowExec:       allowExec,
	})
}

func addTUIFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&tuiAllowExec, "allow-exec", false, "Allow bash/exec.run in TUI agent runs")
}

func init() {
	rootCmd.RunE = runTUI
	rootCmd.Args = cobra.NoArgs
	addTUIFlags(rootCmd)

	addTUIFlags(tuiCmd)
	rootCmd.AddCommand(tuiCmd)
}
