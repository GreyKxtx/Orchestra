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

	// Append LSP and custom-agent examples as commented blocks.
	comment := "\n" +
		"# ── LSP (native language server integration) ────────────────────────────────────────────\n" +
		"# Native LSP integration: auto-diagnostics on write/edit, plus lsp.definition,\n" +
		"# lsp.references, lsp.hover, lsp.diagnostics, lsp.rename tools.\n" +
		"#\n" +
		"# lsp:\n" +
		"#   diagnostics_timeout_ms: 1500\n" +
		"#   servers:\n" +
		"#     - language: go\n" +
		"#       extensions: [.go]\n" +
		"#       command: [gopls]        # gopls is included with the Go toolchain\n" +
		"#     - language: typescript\n" +
		"#       extensions: [.ts, .tsx]\n" +
		"#       command: [typescript-language-server, --stdio]\n" +
		"#     - language: python\n" +
		"#       extensions: [.py]\n" +
		"#       command: [pylsp]\n" +
		"#     - language: rust\n" +
		"#       extensions: [.rs]\n" +
		"#       command: [rust-analyzer]\n" +
		"\n" +
		"# ── Custom agents ──────────────────────────────────────────────────────────────────────────\n" +
		"# Define named agents with custom prompts, tool sets, and model overrides.\n" +
		"# Usage: orchestra apply --mode advisor \"review the recent changes\"\n" +
		"#\n" +
		"# agents:\n" +
		"#   - name: advisor\n" +
		"#     # system_prompt replaces the built-in mode prompt (.orchestra/system.txt wins).\n" +
		"#     system_prompt: |\n" +
		"#       You are a senior code reviewer. Analyze the codebase and report issues\n" +
		"#       of correctness, performance, and maintainability. Do NOT modify files.\n" +
		"#     # tools: null → inherit build toolset; [] → config error; [list] → exact set.\n" +
		"#     tools: [read, glob, grep, symbols, explore]\n" +
		"#     # model: override model name within the same provider (api_base/api_key inherited).\n" +
		"#     # model: claude-opus-4-7\n"

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(comment)
		_ = f.Close()
	}

	fmt.Printf("Created .orchestra.yml with default settings.\n")
	return nil
}
