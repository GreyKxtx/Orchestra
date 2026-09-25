// Package embedindex fills the CKG embedding table so semantic_search has
// something to search. It is shared by `orchestra ckg embed` and the core's
// background warmup: an index nobody remembers to build is an index that
// stays empty, and an empty index makes semantic_search look broken rather
// than unprepared.
package embedindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// DefaultMaxInputBytes caps the text one symbol is embedded from. A symbol
// that spans a generated file used to exceed the endpoint's input limit
// and fail its whole batch, and the pass with it (DATA-5).
const DefaultMaxInputBytes = 8 << 10

// Options configures one indexing pass.
type Options struct {
	ProjectRoot string
	Store       *ckg.Store
	Embed       config.EmbedConfig

	// Limit caps how many nodes one pass embeds (0 = all pending).
	Limit int
	// Rebuild drops existing vectors for the model before indexing. The
	// content cache is kept: symbols whose text did not change are not sent
	// again.
	Rebuild bool
	// MaxInputBytes caps the text per symbol (0 = DefaultMaxInputBytes).
	MaxInputBytes int
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
	// Reused counts vectors taken from the content cache instead of the
	// endpoint: symbols whose text is what it was when they were embedded.
	Reused int
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
		if err := opts.Store.ClearEmbeddings(ctx, model); err != nil {
			return res, fmt.Errorf("rebuild: %w", err)
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
	maxInput := opts.MaxInputBytes
	if maxInput <= 0 {
		maxInput = DefaultMaxInputBytes
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

		type input struct {
			node ckg.MissingEmbedding
			text string
			hash string
		}
		texts := make([]input, 0, len(chunk))
		hashes := make([]string, 0, len(chunk))
		for _, m := range chunk {
			text, readErr := ReadNodeSource(opts.ProjectRoot, m)
			if readErr != nil || strings.TrimSpace(text) == "" {
				res.Skipped++
				continue
			}
			text = CapInput(text, maxInput)
			h := ContentHash(text)
			texts = append(texts, input{node: m, text: text, hash: h})
			hashes = append(hashes, h)
		}
		if len(texts) == 0 {
			continue
		}

		// Symbols whose text was embedded before, under this model, get the
		// vector they had: a re-index renumbers every node of a changed file,
		// but most of its symbols did not change.
		cached, err := opts.Store.CachedEmbeddings(ctx, model, hashes)
		if err != nil {
			return res, err
		}
		var reused []ckg.EmbeddingItem
		inputs := make([]string, 0, len(texts))
		fresh := make([]input, 0, len(texts))
		for _, in := range texts {
			if v, ok := cached[in.hash]; ok {
				reused = append(reused, ckg.EmbeddingItem{NodeID: in.node.NodeID, Vector: v, ContentHash: in.hash})
				continue
			}
			inputs = append(inputs, in.text)
			fresh = append(fresh, in)
		}
		if len(reused) > 0 {
			if err := opts.Store.SaveEmbeddings(ctx, model, reused); err != nil {
				return res, fmt.Errorf("save cached batch [%d:%d]: %w", i, end, err)
			}
			res.Reused += len(reused)
			res.Indexed += len(reused)
		}

		if len(inputs) > 0 {
			vecs, err := client.Embed(ctx, inputs)
			if err != nil {
				return res, fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
			}
			if len(vecs) != len(fresh) {
				return res, fmt.Errorf("embed batch [%d:%d]: got %d vectors for %d inputs", i, end, len(vecs), len(fresh))
			}
			items := make([]ckg.EmbeddingItem, len(fresh))
			for j, in := range fresh {
				items[j] = ckg.EmbeddingItem{NodeID: in.node.NodeID, Vector: vecs[j], ContentHash: in.hash}
			}
			if err := opts.Store.SaveEmbeddings(ctx, model, items); err != nil {
				return res, fmt.Errorf("save batch [%d:%d]: %w", i, end, err)
			}
			res.Indexed += len(items)
		}

		if opts.Progress != nil {
			opts.Progress(res.Indexed+res.Skipped, res.Total, chunk[len(chunk)-1].FQN)
		}
	}
	return res, nil
}

// CapInput cuts text to at most max bytes, on a line boundary when it can,
// and says so on the last line. The first line — the symbol's name — stays.
func CapInput(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	cut := text[:max]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i+1]
	}
	return cut + "// … truncated\n"
}

// ContentHash is the key a vector is cached under: the hash of the text it
// was made from.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
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
