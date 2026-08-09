package tools

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
)

// CKGIndexView is a snapshot of the code knowledge graph index for settings UI.
type CKGIndexView struct {
	Available bool   `json:"available"`
	DBPath    string `json:"db_path,omitempty"`
	ckg.IndexStats
}

// CKGIndexStatus returns current graph / embedding counters.
func (r *Runner) CKGIndexStatus(ctx context.Context) (CKGIndexView, error) {
	out := CKGIndexView{}
	if r == nil {
		return out, nil
	}
	r.ckgMu.RLock()
	store := r.ckgStore
	model := strings.TrimSpace(r.embedCfg.Model)
	r.ckgMu.RUnlock()
	if store == nil {
		return out, nil
	}
	out.Available = true
	out.DBPath = filepath.Join(r.workspaceRoot, ".orchestra", "ckg.db")
	stats, err := store.IndexStats(ctx, model)
	if err != nil {
		return out, err
	}
	out.IndexStats = stats
	return out, nil
}

// RebuildCKG runs a synchronous incremental CKG scan + parse pass.
func (r *Runner) RebuildCKG(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runner is nil")
	}
	r.ckgMu.RLock()
	store := r.ckgStore
	root := r.workspaceRoot
	r.ckgMu.RUnlock()
	if store == nil {
		return fmt.Errorf("ckg store unavailable")
	}
	orch := ckg.NewOrchestrator(store, root)
	return orch.UpdateGraph(ctx)
}

// SetIndexRuntime updates in-memory exclude list and embed config after hot config save.
func (r *Runner) SetIndexRuntime(excludeDirs []string, embedCfg config.EmbedConfig) {
	if r == nil {
		return
	}
	r.ckgMu.Lock()
	defer r.ckgMu.Unlock()
	if len(excludeDirs) > 0 {
		r.excludeDirs = append([]string(nil), excludeDirs...)
	}
	r.embedCfg = embedCfg
}

// CKGEmbedResult summarizes an embed pass.
type CKGEmbedResult struct {
	Model      string `json:"model"`
	Embedded   int    `json:"embedded"`
	Total      int    `json:"total"`
	Remaining  int    `json:"remaining"`
	Elapsed    string `json:"elapsed"`
}

// RunCKGEmbed indexes missing CKG nodes (or all when rebuild=true) via embed.model.
func (r *Runner) RunCKGEmbed(ctx context.Context, rebuild bool, limit int) (*CKGEmbedResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	r.ckgMu.RLock()
	store := r.ckgStore
	root := r.workspaceRoot
	embedCfg := r.embedCfg
	r.ckgMu.RUnlock()
	if store == nil {
		return nil, fmt.Errorf("ckg store unavailable")
	}
	if strings.TrimSpace(embedCfg.Model) == "" {
		return nil, fmt.Errorf("embed.model is empty in .orchestra.yml")
	}

	client := embed.New(embedCfg)
	model := client.Model()

	if rebuild {
		if _, err := store.DB().ExecContext(ctx, `DELETE FROM node_embeddings WHERE model = ?`, model); err != nil {
			return nil, fmt.Errorf("rebuild: clear embeddings: %w", err)
		}
	}

	pending, err := store.MissingEmbeddings(ctx, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list missing: %w", err)
	}
	if len(pending) == 0 {
		total, _ := store.CountEmbeddings(ctx, model)
		return &CKGEmbedResult{Model: model, Total: total, Remaining: 0, Elapsed: "0s"}, nil
	}

	start := time.Now()
	batch := embedCfg.BatchSize
	if batch <= 0 {
		batch = 32
	}
	embedded := 0

	for i := 0; i < len(pending); i += batch {
		end := i + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]
		inputs := make([]string, 0, len(chunk))
		valid := make([]ckg.MissingEmbedding, 0, len(chunk))
		for _, m := range chunk {
			text, err := readNodeSourceForEmbed(root, m)
			if err != nil {
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
		vecs, err := client.Embed(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
		}
		items := make([]ckg.EmbeddingItem, len(valid))
		for j, m := range valid {
			items[j] = ckg.EmbeddingItem{NodeID: m.NodeID, Vector: vecs[j]}
		}
		if err := store.SaveEmbeddings(ctx, model, items); err != nil {
			return nil, fmt.Errorf("save batch [%d:%d]: %w", i, end, err)
		}
		embedded += len(valid)
	}

	total, _ := store.CountEmbeddings(ctx, model)
	missing, _ := store.MissingEmbeddings(ctx, model, 0)
	return &CKGEmbedResult{
		Model:     model,
		Embedded:  embedded,
		Total:     total,
		Remaining: len(missing),
		Elapsed:   time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

func readNodeSourceForEmbed(projectRoot string, m ckg.MissingEmbedding) (string, error) {
	full := filepath.Join(projectRoot, m.Path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if m.LineStart <= 0 || m.LineEnd < m.LineStart {
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
	body := strings.Join(lines[start:end], "\n")
	return fmt.Sprintf("// %s\n%s\n", m.FQN, body), nil
}
