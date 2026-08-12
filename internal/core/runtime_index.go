package core

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol"
)

// IndexStatusParams is empty (reserved).
type IndexStatusParams struct{}

// IndexStatusResult exposes CKG graph stats and index-related config.
type IndexStatusResult struct {
	ProjectRoot    string              `json:"project_root"`
	ExcludeDirs    []string            `json:"exclude_dirs"`
	ContextLimitKB int                 `json:"context_limit_kb"`
	Limits         config.LimitsConfig `json:"limits"`
	Embed          config.EmbedConfig  `json:"embed"`
	Graph          toolsCKGView        `json:"graph"`
	GraphUIPort    int                 `json:"graph_ui_port"`
}

type toolsCKGView struct {
	Available         bool           `json:"available"`
	DBPath            string         `json:"db_path,omitempty"`
	Files             int            `json:"files"`
	Nodes             int            `json:"nodes"`
	Edges             int            `json:"edges"`
	Embeddings        int            `json:"embeddings"`
	MissingEmbeddings int            `json:"missing_embeddings"`
	Funcs             int            `json:"funcs"`
	Types             int            `json:"types"`
	Packages          int            `json:"packages"`
	Tests             int            `json:"tests"`
	Langs             map[string]int `json:"langs,omitempty"`
}

// ckgViewToRPC maps the tools-layer CKG snapshot onto the RPC view.
func ckgViewToRPC(view tools.CKGIndexView) toolsCKGView {
	return toolsCKGView{
		Available:         view.Available,
		DBPath:            view.DBPath,
		Files:             view.Files,
		Nodes:             view.Nodes,
		Edges:             view.Edges,
		Embeddings:        view.Embeddings,
		MissingEmbeddings: view.MissingEmb,
		Funcs:             view.Funcs,
		Types:             view.Types,
		Packages:          view.Packages,
		Tests:             view.Tests,
		Langs:             view.Langs,
	}
}

// IndexConfigureParams updates scope + embed settings in .orchestra.yml.
type IndexConfigureParams struct {
	ExcludeDirs             []string `json:"exclude_dirs,omitempty"`
	ContextLimitKB          *int     `json:"context_limit_kb,omitempty"`
	LimitsContextKB         *int     `json:"limits_context_kb,omitempty"`
	LimitsMaxFiles          *int     `json:"limits_max_files,omitempty"`
	LimitsMaxBytesPerFile   *int64   `json:"limits_max_bytes_per_file,omitempty"`
	EmbedAPIBase            string   `json:"embed_api_base,omitempty"`
	EmbedAPIKey             string   `json:"embed_api_key,omitempty"`
	EmbedModel              string   `json:"embed_model,omitempty"`
	EmbedBatchSize          *int     `json:"embed_batch_size,omitempty"`
	EmbedTimeoutS           *int     `json:"embed_timeout_s,omitempty"`
	SemanticAutoExplore     *bool    `json:"semantic_auto_explore,omitempty"`
	SemanticAutoExploreTopK *int     `json:"semantic_auto_explore_top_k,omitempty"`
	Persist                 *bool    `json:"persist,omitempty"`
}

// IndexConfigureResult mirrors saved index settings.
type IndexConfigureResult struct {
	ExcludeDirs    []string            `json:"exclude_dirs"`
	ContextLimitKB int                 `json:"context_limit_kb"`
	Limits         config.LimitsConfig `json:"limits"`
	Embed          config.EmbedConfig  `json:"embed"`
	Persisted      bool                `json:"persisted"`
}

// IndexRebuildParams triggers a synchronous CKG rescan.
type IndexRebuildParams struct{}

// IndexRebuildResult reports post-rebuild stats.
type IndexRebuildResult struct {
	Graph toolsCKGView `json:"graph"`
}

// IndexEmbedParams runs vector indexing for CKG nodes.
type IndexEmbedParams struct {
	Rebuild *bool `json:"rebuild,omitempty"`
	Limit   int   `json:"limit,omitempty"`
}

// IndexEmbedResult summarizes the embed pass.
type IndexEmbedResult struct {
	Model     string `json:"model"`
	Embedded  int    `json:"embedded"`
	Total     int    `json:"total"`
	Remaining int    `json:"remaining"`
	Elapsed   string `json:"elapsed"`
}

// IndexStatus returns graph counters and index configuration.
func (c *Core) IndexStatus(_ IndexStatusParams) (*IndexStatusResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	ctx := context.Background()
	view, err := c.tools.CKGIndexStatus(ctx)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	graph := ckgViewToRPC(view)
	exclude := append([]string(nil), c.cfg.ExcludeDirs...)
	ctxKB := c.cfg.ContextLimit
	if c.cfg.Limits.ContextKB > 0 {
		ctxKB = c.cfg.Limits.ContextKB
	}
	emb := c.cfg.ResolvedEmbed()
	emb.APIKey = ""
	return &IndexStatusResult{
		ProjectRoot:    c.cfg.ProjectRoot,
		ExcludeDirs:    exclude,
		ContextLimitKB: ctxKB,
		Limits:         c.cfg.Limits,
		Embed:          emb,
		Graph:          graph,
		GraphUIPort:    6061,
	}, nil
}

// IndexConfigure hot-saves index scope + embed settings.
func (c *Core) IndexConfigure(params IndexConfigureParams) (*IndexConfigureResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	persist := true
	if params.Persist != nil {
		persist = *params.Persist
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	if params.ExcludeDirs != nil {
		out := make([]string, 0, len(params.ExcludeDirs))
		for _, d := range params.ExcludeDirs {
			t := strings.TrimSpace(d)
			if t != "" {
				out = append(out, t)
			}
		}
		c.cfg.ExcludeDirs = out
	}
	if params.ContextLimitKB != nil && *params.ContextLimitKB > 0 {
		c.cfg.ContextLimit = *params.ContextLimitKB
		c.cfg.Limits.ContextKB = *params.ContextLimitKB
	}
	if params.LimitsContextKB != nil && *params.LimitsContextKB > 0 {
		c.cfg.Limits.ContextKB = *params.LimitsContextKB
	}
	if params.LimitsMaxFiles != nil && *params.LimitsMaxFiles > 0 {
		c.cfg.Limits.MaxFiles = *params.LimitsMaxFiles
	}
	if params.LimitsMaxBytesPerFile != nil && *params.LimitsMaxBytesPerFile > 0 {
		c.cfg.Limits.MaxBytesPerFile = *params.LimitsMaxBytesPerFile
	}

	if base := strings.TrimSpace(params.EmbedAPIBase); base != "" {
		c.cfg.Embed.APIBase = base
	}
	if key := strings.TrimSpace(params.EmbedAPIKey); key != "" {
		c.cfg.Embed.APIKey = key
	}
	if model := strings.TrimSpace(params.EmbedModel); model != "" {
		c.cfg.Embed.Model = model
	}
	if params.EmbedBatchSize != nil && *params.EmbedBatchSize > 0 {
		c.cfg.Embed.BatchSize = *params.EmbedBatchSize
	}
	if params.EmbedTimeoutS != nil && *params.EmbedTimeoutS > 0 {
		c.cfg.Embed.TimeoutS = *params.EmbedTimeoutS
	}
	if params.SemanticAutoExplore != nil {
		c.cfg.Embed.SemanticAutoExplore = params.SemanticAutoExplore
	}
	if params.SemanticAutoExploreTopK != nil && *params.SemanticAutoExploreTopK > 0 {
		c.cfg.Embed.SemanticAutoExploreTopK = *params.SemanticAutoExploreTopK
	}

	c.applyEmbedRuntime()

	persisted := false
	cfgPath := c.configFilePath()
	if persist && strings.TrimSpace(cfgPath) != "" {
		if err := config.Save(cfgPath, c.cfg); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist index config: "+err.Error(), nil)
		}
		persisted = true
		c.noteConfigMTime()
	}

	ctxKB := c.cfg.ContextLimit
	if c.cfg.Limits.ContextKB > 0 {
		ctxKB = c.cfg.Limits.ContextKB
	}
	emb := c.cfg.ResolvedEmbed()
	emb.APIKey = ""
	return &IndexConfigureResult{
		ExcludeDirs:    append([]string(nil), c.cfg.ExcludeDirs...),
		ContextLimitKB: ctxKB,
		Limits:         c.cfg.Limits,
		Embed:          emb,
		Persisted:      persisted,
	}, nil
}

// IndexRebuild rescans the workspace into the CKG store.
func (c *Core) IndexRebuild(ctx context.Context, _ IndexRebuildParams) (*IndexRebuildResult, error) {
	if c == nil || c.tools == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if err := c.tools.RebuildCKG(ctx); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	view, err := c.tools.CKGIndexStatus(ctx)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &IndexRebuildResult{Graph: ckgViewToRPC(view)}, nil
}

// IndexEmbed vectorizes CKG nodes for semantic_search.
func (c *Core) IndexEmbed(ctx context.Context, params IndexEmbedParams) (*IndexEmbedResult, error) {
	if c == nil || c.tools == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	rebuild := false
	if params.Rebuild != nil {
		rebuild = *params.Rebuild
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	res, err := c.tools.RunCKGEmbed(ctx, rebuild, params.Limit)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &IndexEmbedResult{
		Model:     res.Model,
		Embedded:  res.Embedded,
		Total:     res.Total,
		Remaining: res.Remaining,
		Elapsed:   res.Elapsed,
	}, nil
}
