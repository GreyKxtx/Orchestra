package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// Already initialized: never touch the existing config, but refresh the
	// supplementary artifacts (idempotent re-run migrates older projects to
	// the current .gitignore layout and secrets guidance).
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf(".orchestra.yml already exists — leaving it untouched.\n")
		if err := ensureGitignore(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
		}
		suggestLocalOverlay(cwd, configPath)
		return nil
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
		"# Secrets: put api_key / personal overrides into .orchestra.local.yml (gitignored);\n" +
		"# it is deep-merged over this file at load time and never written back here.\n" +
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

	if err := ensureGitignore(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	if initInstrument {
		if err := runInstrument(cwd, initDryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: instrument failed: %v\n", err)
		}
	}

	return nil
}

// gitignoreMarker makes the bootstrap idempotent: init appends the block only
// when the marker line is absent from an existing .gitignore.
const gitignoreMarker = "# Orchestra: local secrets & runtime artifacts"

// gitignoreBlock ignores runtime/secret artifacts while keeping the knowledge
// files (state, decisions, plans, specs, playbooks, product docs) tracked.
const gitignoreBlock = gitignoreMarker + ` (added by orchestra init)
.orchestra.local.yml
*.orchestra.bak
.orchestra/*
!.orchestra/state.md
!.orchestra/decisions.md
!.orchestra/system.txt
!.orchestra/plans/
!.orchestra/specs/
!.orchestra/playbooks/
!.orchestra/product/
!.orchestra/docs/
`

// ensureGitignore creates or appends the Orchestra ignore block. Secrets can
// live in .orchestra.local.yml and runtime logs under .orchestra/, so a bare
// `git add .` after init must not be able to commit them.
func ensureGitignore(projectRoot string) error {
	path := filepath.Join(projectRoot, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), gitignoreMarker) {
		return nil
	}
	block := gitignoreBlock
	if len(existing) > 0 {
		sep := "\n"
		if !strings.HasSuffix(string(existing), "\n") {
			sep = "\n\n"
		}
		block = sep + block
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	fmt.Printf("Updated .gitignore: ignoring .orchestra runtime artifacts and .orchestra.local.yml.\n")
	return nil
}

// suggestLocalOverlay warns when the committed config still carries API keys
// while no .orchestra.local.yml exists — the one migration step init cannot
// do safely on its own (moving a secret means editing the user's config).
func suggestLocalOverlay(projectRoot, configPath string) {
	if _, err := os.Stat(filepath.Join(projectRoot, config.LocalOverlayName)); err == nil {
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if key, val, ok := strings.Cut(trimmed, ":"); ok &&
			strings.TrimSpace(key) == "api_key" && strings.Trim(strings.TrimSpace(val), `"'`) != "" {
			fmt.Printf("Hint: .orchestra.yml contains an api_key. Move secrets to %s (gitignored):\n", config.LocalOverlayName)
			fmt.Printf("  llm:\n    api_key: <your key>\n")
			fmt.Printf("It is merged over .orchestra.yml at load time and never written back.\n")
			return
		}
	}
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
