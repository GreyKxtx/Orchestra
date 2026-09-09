package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// InitOptions mirrors the flags of `orchestra init`.
type InitOptions struct {
	Instrument bool
	DryRun     bool
}

// initProject is the body of `orchestra init` with the directory made explicit,
// so a server can initialise whichever project the user picked without
// depending on, or changing, the process cwd. It is idempotent: an existing
// .orchestra.yml is never touched, only the supplementary artifacts refreshed.
func initProject(ctx context.Context, root string, opts InitOptions) error {
	configPath := filepath.Join(root, ".orchestra.yml")

	// Already initialized: never touch the existing config, but refresh the
	// supplementary artifacts (idempotent re-run migrates older projects to
	// the current .gitignore layout, secrets guidance, and ORCHESTRA.md).
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf(".orchestra.yml already exists — leaving it untouched.\n")
		if err := ensureGitignore(root); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
		}
		if err := ensureLearningDirs(root); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create learning dirs: %v\n", err)
		}
		if action, foundFallback, err := ensureOrchestraMD(root, opts.DryRun, detectedLanguages(root)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create ORCHESTRA.md: %v\n", err)
		} else {
			reportOrchestraMD(action, foundFallback)
		}
		suggestLocalOverlay(root, configPath)
		return nil
	}

	// Create default config
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://localhost:1234/v1"
	cfg.LLM.Model = "qwen2.5-coder-7b"
	cfg.ContextLimit = 50
	cfg.Limits.ContextKB = 50

	// Replace those two guesses with the local server that is actually running,
	// when there is one. Bounded by llm.localProbeTimeout and probed in
	// parallel, so a machine with nothing listening pays three refused
	// connections and moves on.
	detectCtx := ctx
	if detectCtx == nil {
		detectCtx = context.Background()
	}
	srv, found := llm.DetectLocalServer(detectCtx)
	if line := applyDetectedLocalServer(cfg, srv, found); line != "" {
		fmt.Println(line)
	}

	lspEnabled := true
	cfg.LSP = config.LSPConfig{
		Enabled:              &lspEnabled,
		AutoInstall:          "ask",
		DiagnosticsTimeoutMS: 1500,
		Servers:              lspServersFromInit(root),
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
		"#     api_base: http://localhost:1234/v1\n" +
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

	if err := ensureGitignore(root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}
	if err := ensureLearningDirs(root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create learning dirs: %v\n", err)
	}
	if action, foundFallback, err := ensureOrchestraMD(root, opts.DryRun, detectedLanguages(root)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create ORCHESTRA.md: %v\n", err)
	} else {
		reportOrchestraMD(action, foundFallback)
	}

	if opts.Instrument {
		if err := runInstrument(root, opts.DryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: instrument failed: %v\n", err)
		}
	}

	return nil
}
