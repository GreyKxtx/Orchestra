// Package embedindex fills the CKG embedding table so semantic_search has
// something to search. It is shared by `orchestra ckg embed` and the core's
// background warmup: an index nobody remembers to build is an index that
// stays empty, and an empty index makes semantic_search look broken rather
// than unprepared.
package embedindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/embed"
)

// DefaultBatchSize is used when embed.batch_size is unset.
const DefaultBatchSize = 32

// Options configures one indexing pass.
type Options struct {
	ProjectRoot string
	Store       *ckg.Store
	Embed       config.EmbedConfig

	// Limit caps how many nodes one pass embeds (0 = all pending).
	Limit int
	// Rebuild drops existing vectors for the model before indexing.
	Rebuild bool
	// Progress, when set, is called after each batch with the running count.
	Progress func(done, total int, fqn string)
}

// Result reports what a pass did.
type Result struct {
	Model string
	// Total is how many nodes needed embedding when the pass started.
	Total int
	// Indexed is how many vectors were written.
	Indexed int
	// Skipped counts nodes whose source could not be read or was empty.
	Skipped int
}

// Run embeds every CKG node that lacks a vector for the configured model.
// It is incremental: nodes already embedded are not re-sent, so calling it
// on an unchanged repo costs nothing.
func Run(ctx context.Context, opts Options) (Result, error) {
	var res Result
	if opts.Store == nil {
		return res, fmt.Errorf("embedindex: no CKG store")
	}
	if strings.TrimSpace(opts.Embed.Model) == "" {
		return res, fmt.Errorf("embed.model is empty — set embed.model (and embed.provider) in .orchestra.yml")
	}

	client := embed.New(opts.Embed)
	model := client.Model()
	res.Model = model

	if opts.Rebuild {
		if _, err := opts.Store.DB().ExecContext(ctx,
			`DELETE FROM node_embeddings WHERE model = ?`, model); err != nil {
			return res, fmt.Errorf("rebuild: clear embeddings: %w", err)
		}
	}

	pending, err := opts.Store.MissingEmbeddings(ctx, model, opts.Limit)
	if err != nil {
		return res, fmt.Errorf("list missing: %w", err)
	}
	res.Total = len(pending)
	if res.Total == 0 {
		return res, nil
	}

	batch := opts.Embed.BatchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}

	for i := 0; i < len(pending); i += batch {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		end := i + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]

		inputs := make([]string, 0, len(chunk))
		valid := make([]ckg.MissingEmbedding, 0, len(chunk))
		for _, m := range chunk {
			text, readErr := ReadNodeSource(opts.ProjectRoot, m)
			if readErr != nil || strings.TrimSpace(text) == "" {
				res.Skipped++
				continue
			}
			inputs = append(inputs, text)
			valid = append(valid, m)
		}
		if len(inputs) == 0 {
			continue
		}

		vecs, err := client.Embed(ctx, inputs)
		if err != nil {
			return res, fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
		}
		if len(vecs) != len(valid) {
			return res, fmt.Errorf("embed batch [%d:%d]: got %d vectors for %d inputs", i, end, len(vecs), len(valid))
		}

		items := make([]ckg.EmbeddingItem, len(valid))
		for j, m := range valid {
			items[j] = ckg.EmbeddingItem{NodeID: m.NodeID, Vector: vecs[j]}
		}
		if err := opts.Store.SaveEmbeddings(ctx, model, items); err != nil {
			return res, fmt.Errorf("save batch [%d:%d]: %w", i, end, err)
		}
		res.Indexed += len(items)

		if opts.Progress != nil {
			opts.Progress(res.Indexed+res.Skipped, res.Total, chunk[len(chunk)-1].FQN)
		}
	}
	return res, nil
}

// ReadNodeSource returns the source range for a node, prefixed with its FQN so
// the embedding carries the name as well as the body.
func ReadNodeSource(projectRoot string, m ckg.MissingEmbedding) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, m.Path))
	if err != nil {
		return "", err
	}
	if m.LineStart <= 0 || m.LineEnd < m.LineStart {
		// Defensive fallback — embed FQN + path only.
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	lines := strings.Split(string(data), "\n")
	start := m.LineStart - 1
	end := m.LineEnd
	if start >= len(lines) {
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	if end > len(lines) {
		end = len(lines)
	}
	return fmt.Sprintf("// %s\n%s\n", m.FQN, strings.Join(lines[start:end], "\n")), nil
}
