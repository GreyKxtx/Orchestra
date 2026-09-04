package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/embedindex"
	"github.com/spf13/cobra"
)

var ckgCmd = &cobra.Command{
	Use:   "ckg",
	Short: "Code Knowledge Graph admin commands",
	Long:  "Subcommands for inspecting and indexing the CKG store under .orchestra/ckg.db.",
}

var (
	ckgEmbedRebuild   bool
	ckgEmbedLimit     int
	ckgEmbedBatchSize int
)

var ckgEmbedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Embed CKG nodes for semantic_search",
	Long: `Index CKG nodes (functions, methods, types) into vector embeddings using
the OpenAI-compatible provider configured under embed: in .orchestra.yml.
Reads the source range of each node from disk; embeddings are stored
alongside the CKG in the same SQLite database.

By default only nodes missing an embedding for the configured model are
processed. --rebuild re-embeds everything (useful after switching model).`,
	Args: cobra.NoArgs,
	RunE: runCKGEmbed,
}

func init() {
	ckgEmbedCmd.Flags().BoolVar(&ckgEmbedRebuild, "rebuild", false, "Re-embed all nodes, not just missing ones")
	ckgEmbedCmd.Flags().IntVar(&ckgEmbedLimit, "limit", 0, "Stop after N nodes (0 = no limit)")
	ckgEmbedCmd.Flags().IntVar(&ckgEmbedBatchSize, "batch-size", 0, "Override embed.batch_size (0 = use config)")
	ckgCmd.AddCommand(ckgEmbedCmd)
	rootCmd.AddCommand(ckgCmd)
}

func runCKGEmbed(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return fmt.Errorf("config: %w (run 'orchestra init' first)", err)
	}
	emb := cfg.ResolvedEmbed()
	if emb.Model == "" {
		return fmt.Errorf("embed.model is empty — pick an embedding model in General (or set embed.provider + embed.model in .orchestra.yml)")
	}
	if ckgEmbedBatchSize > 0 {
		emb.BatchSize = ckgEmbedBatchSize
	}
	dbPath := filepath.Join(cfg.ProjectRoot, ".orchestra", "ckg.db")
	store, err := ckg.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("open ckg store: %w", err)
	}
	defer store.Close()

	w := cmd.OutOrStdout()

	start := time.Now()
	res, err := embedindex.Run(cmd.Context(), embedindex.Options{
		ProjectRoot: cfg.ProjectRoot,
		Store:       store,
		Embed:       emb,
		Limit:       ckgEmbedLimit,
		Rebuild:     ckgEmbedRebuild,
		Progress: func(done, total int, fqn string) {
			fmt.Fprintf(w, "  [%d/%d] %s … %s\n", done, total, fqn, time.Since(start).Round(time.Millisecond))
		},
	})
	if err != nil {
		return err
	}
	if res.Total == 0 {
		fmt.Fprintf(w, "No nodes need embedding for model %q.\n", res.Model)
		return nil
	}
	if res.Skipped > 0 {
		fmt.Fprintf(w, "Skipped %d node(s) whose source could not be read.\n", res.Skipped)
	}

	total, _ := store.CountEmbeddings(cmd.Context(), res.Model)
	fmt.Fprintf(w, "Done. %d embeddings total for model %q (elapsed %s).\n", total, res.Model, time.Since(start).Round(time.Millisecond))
	return nil
}
