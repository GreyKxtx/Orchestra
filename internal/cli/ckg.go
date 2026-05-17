package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/embed"
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
	if cfg.Embed.Model == "" {
		return fmt.Errorf("embed.model is empty in .orchestra.yml — set provider/model/api_base/api_key under embed:")
	}
	if ckgEmbedBatchSize > 0 {
		cfg.Embed.BatchSize = ckgEmbedBatchSize
	}
	dbPath := filepath.Join(cfg.ProjectRoot, ".orchestra", "ckg.db")
	store, err := ckg.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("open ckg store: %w", err)
	}
	defer store.Close()

	w := cmd.OutOrStdout()
	client := embed.New(cfg.Embed)
	model := client.Model()

	if ckgEmbedRebuild {
		if _, err := store.DB().ExecContext(cmd.Context(),
			`DELETE FROM node_embeddings WHERE model = ?`, model); err != nil {
			return fmt.Errorf("rebuild: clear embeddings: %w", err)
		}
		fmt.Fprintf(w, "Cleared existing embeddings for model %q.\n", model)
	}

	pending, err := store.MissingEmbeddings(cmd.Context(), model, ckgEmbedLimit)
	if err != nil {
		return fmt.Errorf("list missing: %w", err)
	}
	if len(pending) == 0 {
		fmt.Fprintf(w, "No nodes need embedding for model %q.\n", model)
		return nil
	}
	fmt.Fprintf(w, "Embedding %d node(s) under model %q...\n", len(pending), model)

	start := time.Now()
	batch := cfg.Embed.BatchSize
	if batch <= 0 {
		batch = 32
	}

	for i := 0; i < len(pending); i += batch {
		end := i + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]
		inputs := make([]string, 0, len(chunk))
		valid := make([]ckg.MissingEmbedding, 0, len(chunk))
		for _, m := range chunk {
			text, err := readNodeSource(cfg.ProjectRoot, m)
			if err != nil {
				fmt.Fprintf(w, "  skip %s: %v\n", m.FQN, err)
				continue
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			inputs = append(inputs, text)
			valid = append(valid, m)
		}
		if len(inputs) == 0 {
			continue
		}
		vecs, err := client.Embed(cmd.Context(), inputs)
		if err != nil {
			return fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
		}
		items := make([]ckg.EmbeddingItem, len(valid))
		for j, m := range valid {
			items[j] = ckg.EmbeddingItem{NodeID: m.NodeID, Vector: vecs[j]}
		}
		if err := store.SaveEmbeddings(cmd.Context(), model, items); err != nil {
			return fmt.Errorf("save batch [%d:%d]: %w", i, end, err)
		}
		fmt.Fprintf(w, "  [%d/%d] %s … %s\n", end, len(pending), chunk[len(chunk)-1].FQN, time.Since(start).Round(time.Millisecond))
	}

	total, _ := store.CountEmbeddings(cmd.Context(), model)
	fmt.Fprintf(w, "Done. %d embeddings total for model %q (elapsed %s).\n", total, model, time.Since(start).Round(time.Millisecond))
	return nil
}

// readNodeSource loads lines [LineStart..LineEnd] from <projectRoot>/<path>.
// Lines are 1-based and inclusive on both ends.
func readNodeSource(projectRoot string, m ckg.MissingEmbedding) (string, error) {
	full := filepath.Join(projectRoot, m.Path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	src := string(data)
	if m.LineStart <= 0 || m.LineEnd < m.LineStart {
		// Defensive fallback — embed FQN + path only.
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	// Split and slice.
	lines := strings.Split(src, "\n")
	start := m.LineStart - 1
	end := m.LineEnd
	if start >= len(lines) {
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	if end > len(lines) {
		end = len(lines)
	}
	// Prefix with FQN for embedding context.
	body := strings.Join(lines[start:end], "\n")
	return fmt.Sprintf("// %s\n%s\n", m.FQN, body), nil
}

// embedQuery is a small helper shared by the semantic_search tool to
// embed a single query string and return the vector. Kept here (CLI
// package) so internal/tools doesn't import internal/embed directly —
// the tool dispatch goes through a callback set on the Runner.
func embedQuery(ctx context.Context, cfg config.EmbedConfig, query string) ([]float32, error) {
	client := embed.New(cfg)
	vecs, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed: server returned no vectors")
	}
	return vecs[0], nil
}
