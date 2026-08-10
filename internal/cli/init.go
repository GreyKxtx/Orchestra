package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/instrument"
	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/spf13/cobra"
)

var initInstrument bool
var initDryRun bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Orchestra project",
	Long:  "Creates .orchestra.yml configuration file in the project root",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initInstrument, "instrument", false, "автоматически добавить OTel SDK инструментацию в проект")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "показать что будет сделано без записи файлов")
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

	lspEnabled := true
	cfg.LSP = config.LSPConfig{
		Enabled:              &lspEnabled,
		AutoInstall:          "ask",
		DiagnosticsTimeoutMS: 1500,
		Servers:              lspServersFromInit(cwd),
	}

	// Save config
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Append custom-agent examples as commented blocks.
	comment := "\n" +
		"# LSP: active servers from workspace detect (or go+ts+py fallback). See orchestra lsp list.\n" +
		"# Optional extras (uncomment under lsp.servers): csharp-ls, rust-analyzer — docs/architecture/lsp-auto-provision.md\n" +
		"\n" +
		"# ── Custom agents ──────────────────────────────────────────────────────────────────────────\n" +
		"# Define named agents with custom prompts, tool sets, and model overrides.\n" +
		"# Usage: orchestra apply --mode advisor \"review the recent changes\"\n" +
		"#\n" +
		"# Planner–Worker (recommended for local models):\n" +
		"#   orchestra apply --mode orchestra \"…\"   # Lead delegates via task(worker)\n" +
		"# providers:\n" +
		"#   fast:                          # Worker / compaction / auto-router\n" +
		"#     api_base: http://localhost:8000/v1\n" +
		"#     model: nemotron-4b\n" +
		"# llm:\n" +
		"#   model: qwen-27b                 # Lead (main)\n" +
		"#   extra_body:\n" +
		"#     num_ctx: 20000\n" +
		"# llm.router.fast_provider: fast\n" +
		"# orchestra:\n" +
		"#     planner:\n" +
		"#       provider: fast          # reasoning / strong model for Lead\n" +
		"#       model: …\n" +
		"#     default_tier: focused\n" +
		"#     max_worker_retries: 3\n" +
		"#     tiers:\n" +
		"#       - name: complex\n" +
		"#         provider: …\n" +
		"#         model: …\n" +
		"#       - name: focused\n" +
		"#         provider: …\n" +
		"#         model: qwen2.5-coder-7b\n" +
		"#       - name: micro\n" +
		"#         provider: …\n" +
		"#         model: …\n" +
		"# Docs: docs/architecture/planner-worker.md\n" +
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

	if initInstrument {
		if err := runInstrument(cwd, initDryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: instrument failed: %v\n", err)
		}
	}

	return nil
}

func lspServersFromInit(projectRoot string) []config.LSPServerConfig {
	specs := provision.InitServerSpecs(projectRoot)
	out := make([]config.LSPServerConfig, len(specs))
	for i, s := range specs {
		out[i] = config.LSPServerConfig{
			Language:   s.Language,
			Extensions: append([]string(nil), s.Extensions...),
			Command:    append([]string(nil), s.Command...),
		}
	}
	return out
}

func runInstrument(dir string, dryRun bool) error {
	langs := instrument.Detect(dir, instrument.Phase1Langs)
	if len(langs) == 0 {
		fmt.Println("[instrument] No supported languages detected.")
		return nil
	}

	prefix := ""
	if dryRun {
		prefix = "[dry-run] "
	}

	results, err := instrument.Instrument(dir, langs, dryRun)
	for _, r := range results {
		if r.Skipped {
			fmt.Printf("[instrument] %s: skipped — %s\n", r.Lang, r.SkipReason)
			continue
		}
		fmt.Printf("[instrument] %s%s: wrote %s\n", prefix, r.Lang, r.TelemetryFile)
		if r.Patched {
			fmt.Printf("[instrument] %s%s: patched %s\n", prefix, r.Lang, r.PatchedFile)
		}
		if r.InstallOutput != "" {
			fmt.Printf("[instrument] %s%s: install output:\n%s\n", prefix, r.Lang, r.InstallOutput)
		}
	}
	return err
}
