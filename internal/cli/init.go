package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Orchestra project",
	Long:  "Creates .orchestra.yml configuration file in the project root",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, ".orchestra.yml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf(".orchestra.yml already exists in %s", cwd)
	}

	// Create default config
	cfg := config.DefaultConfig(cwd)
	cfg.LLM.APIBase = "http://localhost:8000/v1"
	cfg.LLM.Model = "qwen2.5-coder-7b"
	cfg.ContextLimit = 50
	cfg.Limits.ContextKB = 50

	// Save config
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Created .orchestra.yml with default settings.\n")
	return nil
}
