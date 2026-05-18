package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/repomap"
	"github.com/spf13/cobra"
)

var (
	repoMapBudget   int
	repoMapMaxFiles int
)

var repoMapCmd = &cobra.Command{
	Use:   "repo-map",
	Short: "Print a tree-sitter outline of the project",
	Long: `Walks the project root, parses every supported source file via tree-sitter,
and prints a compact outline (functions, types, methods per file). Stable on
cold start — no SQLite, no embeddings.

Use --budget to cap output size. When the budget is too small for the full
outline, private symbols are dropped first, then whole small files.`,
	RunE: runRepoMap,
}

func init() {
	repoMapCmd.Flags().IntVar(&repoMapBudget, "budget", 8192, "Max output bytes (0 = unlimited)")
	repoMapCmd.Flags().IntVar(&repoMapMaxFiles, "max-files", 0, "Hard cap on files scanned (0 = no cap)")
	rootCmd.AddCommand(repoMapCmd)
}

func runRepoMap(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}
	rm, err := repomap.Build(cmd.Context(), cfg.ProjectRoot, repomap.Options{
		ExcludeDirs: cfg.ExcludeDirs,
		MaxFiles:    repoMapMaxFiles,
	})
	if err != nil {
		return err
	}
	fmt.Print(repomap.Format(rm, repoMapBudget))
	fmt.Fprintf(os.Stderr, "[repo-map] %d files outlined, %d skipped\n", len(rm.Files), rm.Skipped)
	return nil
}
