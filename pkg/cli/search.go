package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/search"
	"github.com/spf13/cobra"
)

var (
	searchCaseInsensitive bool
	searchMaxPerFile      int
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search for text in project files",
	Long:  "Searches for the given query in all project files (excluding directories from config)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().BoolVarP(&searchCaseInsensitive, "insensitive", "i", false, "Case-insensitive search")
	searchCmd.Flags().IntVar(&searchMaxPerFile, "max-per-file", 10, "Maximum matches per file")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	// Load config
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, ".orchestra.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}

	// Search
	opts := search.DefaultOptions()
	opts.CaseInsensitive = searchCaseInsensitive
	opts.MaxMatchesPerFile = searchMaxPerFile

	matches, err := search.SearchInProject(cfg.ProjectRoot, query, cfg.ExcludeDirs, opts)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	// Output results
	if len(matches) == 0 {
		fmt.Printf("No matches found for \"%s\"\n", query)
		return nil
	}

	fmt.Printf("Found %d match(es) for \"%s\":\n\n", len(matches), query)

	currentFile := ""
	for _, match := range matches {
		// Show file header if it's a new file
		if match.FilePath != currentFile {
			if currentFile != "" {
				fmt.Println()
			}
			relPath, _ := filepath.Rel(cfg.ProjectRoot, match.FilePath)
			fmt.Printf("📄 %s\n", relPath)
			currentFile = match.FilePath
		}

		// Show context before
		for _, ctxLine := range match.ContextBefore {
			fmt.Printf("  %s\n", ctxLine)
		}

		// Show matching line
		fmt.Printf("→ %d: %s\n", match.Line, match.LineText)

		// Show context after
		for _, ctxLine := range match.ContextAfter {
			fmt.Printf("  %s\n", ctxLine)
		}
		fmt.Println()
	}

	return nil
}

