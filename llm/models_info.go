package llm

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:generate go run ./internal/modelsgen

// modelsDataJSON is a filtered snapshot of models.dev — context window, list
// price and capability flags for the models of the providers Orchestra can
// talk to. Regenerate with `go generate ./...` from the llm module; see
// internal/modelsgen for the filtering rules.
//
//go:embed models_data.json
var modelsDataJSON []byte

// ModelInfo is what the snapshot knows about one model. Zero values mean
// "the snapshot does not say", never "zero": a model with no published price
// has InputPer1M == 0, and callers must check the ok return, not the field.
type ModelInfo struct {
	ID   string // the normalized key it was found under
	Name string // human label from models.dev ("Claude Sonnet 4.5")

	// ContextWindow is the vendor's advertised maximum. It is NOT the
	// budgeting authority — ModelContextWindow is, and it prefers the
	// curated table, which accounts for tiers Orchestra cannot reach (e.g.
	// Anthropic's 1M beta needs a header we do not send).
	ContextWindow int
	MaxOutput     int

	InputPer1M      float64
	OutputPer1M     float64
	CacheReadPer1M  float64
	CacheWritePer1M float64

	Vision     bool // accepts image input
	Tools      bool // supports tool / function calling
	Reasoning  bool // exposes a thinking or reasoning-effort control
	JSONSchema bool // supports structured output
}

type snapshotModel struct {
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	Ctx        int     `json:"ctx"`
	MaxOut     int     `json:"max_out"`
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Caps       string  `json:"caps"`
}

var (
	modelsOnce sync.Once
	modelsByID map[string]snapshotModel
)

func loadModels() map[string]snapshotModel {
	modelsOnce.Do(func() {
		var doc struct {
			Models map[string]snapshotModel `json:"models"`
		}
		// A corrupt embedded file leaves an empty table rather than panicking
		// at init: every caller already handles "unknown model".
		_ = json.Unmarshal(modelsDataJSON, &doc)
		modelsByID = doc.Models
	})
	return modelsByID
}

// normalizeModelKey reduces a model string as written in config, in a gateway
// response or in a usage record to the snapshot's key form.
//
// It must stay byte-identical to the copy in internal/modelsgen: the two
// halves of the lookup have to agree or the table silently never matches.
func normalizeModelKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:] // "openrouter/anthropic/claude-…" → "claude-…"
	}
	if i := strings.IndexAny(s, ":@"); i >= 0 {
		s = s[:i] // ":free", ":nitro", LM Studio's "@q4_k_m"
	}
	return strings.ReplaceAll(s, ".", "-")
}

// isVersionNoise reports whether a trailing id segment carries no pricing
// meaning and can be trimmed when looking for the base model. Only dates,
// bare numbers and release channels qualify — trimming a word would resolve
// "gpt-4o-mini" to "gpt-4o", which is a different model at 16x the price.
func isVersionNoise(seg string) bool {
	switch seg {
	case "latest", "preview", "beta", "exp":
		return true
	}
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// LookupModelInfo looks up a model in the models.dev snapshot. It tolerates gateway
// vendor prefixes, ":free"-style suffixes, "." vs "-" in version numbers and
// a trailing release date, and reports a miss rather than guessing.
func LookupModelInfo(model string) (ModelInfo, bool) {
	key := normalizeModelKey(model)
	if key == "" {
		return ModelInfo{}, false
	}
	table := loadModels()
	for {
		if m, ok := table[key]; ok {
			return toModelInfo(key, m), true
		}
		i := strings.LastIndex(key, "-")
		if i <= 0 || !isVersionNoise(key[i+1:]) {
			return ModelInfo{}, false
		}
		key = key[:i]
	}
}

func toModelInfo(key string, m snapshotModel) ModelInfo {
	return ModelInfo{
		ID:              key,
		Name:            m.Name,
		ContextWindow:   m.Ctx,
		MaxOutput:       m.MaxOut,
		InputPer1M:      m.In,
		OutputPer1M:     m.Out,
		CacheReadPer1M:  m.CacheRead,
		CacheWritePer1M: m.CacheWrite,
		Vision:          strings.ContainsRune(m.Caps, 'v'),
		Tools:           strings.ContainsRune(m.Caps, 't'),
		Reasoning:       strings.ContainsRune(m.Caps, 'r'),
		JSONSchema:      strings.ContainsRune(m.Caps, 's'),
	}
}

// ModelListPrice returns the snapshot's per-1M input/output price for a model,
// or false when it publishes none. Callers use it only as a fallback behind a
// provider-reported cost and behind the user's own pricing table.
func ModelListPrice(model string) (inputPer1M, outputPer1M float64, ok bool) {
	mi, found := LookupModelInfo(model)
	if !found || (mi.InputPer1M == 0 && mi.OutputPer1M == 0) {
		return 0, 0, false
	}
	return mi.InputPer1M, mi.OutputPer1M, true
}
