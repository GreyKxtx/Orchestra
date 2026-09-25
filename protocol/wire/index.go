package wire

// index.status, index.configure, index.rebuild, index.embed, index.graph, index.outline. The results that carry config or CKG types stay in internal/core.

// IndexStatusParams is empty (reserved).
type IndexStatusParams struct{}

type CKGView struct {
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

// IndexRebuildParams triggers a synchronous CKG rescan.
type IndexRebuildParams struct{}

// IndexRebuildResult reports post-rebuild stats.
type IndexRebuildResult struct {
	Graph CKGView `json:"graph"`
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

// IndexGraphParams selects the granularity of index.graph: "file" (default)
// — folders, files and weighted file-to-file relations, what the Graph view
// draws — or "symbol", every indexed symbol with its relations.
type IndexGraphParams struct {
	Level string `json:"level,omitempty"`
}

// IndexOutlineParams asks for one file's symbols, by workspace-relative path
// with forward slashes — the id of a file node in index.graph.
type IndexOutlineParams struct {
	Path string `json:"path"`
	// Preview off returns the symbol list without reading the file.
	Preview *bool `json:"preview,omitempty"`
}
