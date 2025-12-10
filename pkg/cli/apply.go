package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/context"
	"github.com/orchestra/orchestra/internal/gitutil"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/parser"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/search"
	"github.com/spf13/cobra"
)

var (
	applyFlag bool
	gitStrict bool
	gitCommit bool
	planOnly  bool
)

var applyCmd = &cobra.Command{
	Use:   "apply [query]",
	Short: "Apply changes suggested by LLM",
	Long:  "Analyzes the project and applies LLM-suggested changes",
	Args:  cobra.ExactArgs(1),
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&applyFlag, "apply", false, "Actually apply changes (default is dry-run)")
	applyCmd.Flags().BoolVar(&gitStrict, "git-strict", false, "Fail if git repo has uncommitted changes")
	applyCmd.Flags().BoolVar(&gitCommit, "git-commit", false, "Create git commit after applying changes (requires --apply)")
	applyCmd.Flags().BoolVar(&planOnly, "plan-only", false, "Show only plan of changes, without generating code")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	query := args[0]

	// 1. Load config
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, ".orchestra.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}

	// 1.5. Check git status (if in git repo)
	if gitutil.IsRepo(cfg.ProjectRoot) {
		clean, status, err := gitutil.IsClean(cfg.ProjectRoot)
		if err == nil && !clean {
			if gitStrict {
				return fmt.Errorf("git repo has uncommitted changes:\n%s\n\nCommit or stash changes before running orchestra, or remove --git-strict flag", status)
			}
			fmt.Fprintf(os.Stderr, "[orchestra] WARNING: git repo has uncommitted changes:\n%s\n\n", status)
		}
	}

	// 2. Search for relevant files to prioritize in context
	var focusFiles []string
	searchOpts := search.DefaultOptions()
	searchOpts.MaxMatchesPerFile = 2 // Limit to avoid too many matches

	matches, err := search.SearchInProject(cfg.ProjectRoot, query, cfg.ExcludeDirs, searchOpts)
	if err == nil && len(matches) > 0 {
		// Collect unique file paths from search results
		focusSet := make(map[string]struct{})
		for _, match := range matches {
			if _, ok := focusSet[match.FilePath]; !ok {
				focusSet[match.FilePath] = struct{}{}
				// Convert to relative path
				relPath, err := filepath.Rel(cfg.ProjectRoot, match.FilePath)
				if err == nil {
					focusFiles = append(focusFiles, relPath)
				} else {
					focusFiles = append(focusFiles, match.FilePath)
				}
			}
		}
	}
	// If search failed, continue without focus files (non-fatal)

	// 3. Build context (with focus files prioritized)
	ctxParams := context.BuildParams{
		ProjectRoot: cfg.ProjectRoot,
		LimitKB:     cfg.ContextLimit,
		ExcludeDirs: cfg.ExcludeDirs,
		FocusFiles:  focusFiles,
	}

	ctxResult, err := context.BuildContext(ctxParams, query)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// 4. Call LLM - Phase 1: Generate plan
	// Use test client if set (for integration tests), otherwise create real client
	var llmClient llm.Client
	if testLLMClient != nil {
		llmClient = testLLMClient
	} else {
		llmClient = llm.NewOpenAIClient(cfg.LLM)
	}

	planPrompt := context.BuildPlanPrompt(ctxResult.Files, query)
	planJSON, err := llmClient.Plan(cmd.Context(), planPrompt)
	if err != nil {
		return fmt.Errorf("failed to get plan from LLM: %w", err)
	}

	// Parse plan
	p, err := plan.ParsePlan(planJSON)
	if err != nil {
		return fmt.Errorf("failed to parse plan: %w", err)
	}

	// Print plan
	fmt.Println("📋 Plan of changes:")
	fmt.Println()
	for i, step := range p.Steps {
		fmt.Printf("%d. %s: %s\n", i+1, step.Action, step.FilePath)
		if step.Summary != "" {
			fmt.Printf("   %s\n", step.Summary)
		}
	}
	fmt.Println()

	// If plan-only, stop here
	if planOnly {
		fmt.Println("Plan generated. Run without --plan-only to apply changes.")
		return nil
	}

	// Phase 2: Generate code changes
	raw, err := llmClient.Complete(cmd.Context(), ctxResult.Prompt)
	if err != nil {
		return fmt.Errorf("failed to get LLM response: %w", err)
	}

	// 5. Parse response
	parsed, err := parser.Parse(raw)
	if err != nil {
		return fmt.Errorf("failed to parse LLM response: %w\n\nRaw response:\n%s", err, raw)
	}

	// 6. Apply changes
	result, err := applier.ApplyChanges(cfg.ProjectRoot, parsed.Files, applier.ApplyOptions{
		DryRun:       !applyFlag,
		Backup:       true,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		// Show raw response for debugging if apply failed
		return fmt.Errorf("failed to apply changes: %w\n\nLLM response was:\n%s", err, raw)
	}

	// 7. Output results
	if applyFlag {
		fmt.Printf("✓ Applied changes to %d file(s). Backups created with .orchestra.bak suffix.\n", len(result.Diffs))

		// 8. Git commit (if requested)
		if gitCommit {
			if !gitutil.IsRepo(cfg.ProjectRoot) {
				return fmt.Errorf("--git-commit requires a git repository")
			}

			commitMsg := fmt.Sprintf("feat(orchestra): %s", query)
			if err := gitutil.CommitAll(cfg.ProjectRoot, commitMsg); err != nil {
				// Don't fail the whole operation if commit fails
				fmt.Fprintf(os.Stderr, "[orchestra] WARNING: failed to create git commit: %v\n", err)
			} else {
				fmt.Printf("✓ Created git commit: %s\n", commitMsg)
			}
		}
	} else {
		fmt.Printf("Dry-run mode. Would apply changes to %d file(s):\n\n", len(result.Diffs))
		for _, diff := range result.Diffs {
			fmt.Printf("--- %s\n", diff.Path)
			fmt.Printf("+++ %s\n", diff.Path)
			// Simple diff output (can be improved later)
			if diff.Before != diff.After {
				fmt.Println("(file would be modified)")
			} else {
				fmt.Println("(file would be created)")
			}
			fmt.Println()
		}
		fmt.Println("Run with --apply flag to actually apply changes.")
	}

	return nil
}
