// Command modelsgen regenerates llm/models_data.json from models.dev.
//
// Run it from the llm module root (or via `go generate ./...`):
//
//	go run ./internal/modelsgen
//
// models.dev publishes one JSON with context window, price and capability
// flags for every model of 200+ providers (4.5 MB). We keep a filtered
// snapshot in the repo instead of fetching at runtime: the agent must work
// offline, and a price table that changes under the user without a commit is
// worse than a slightly stale one.
//
// Filtering rules, in this order:
//
//   - Only providers in providerPrecedence are considered. Everything else is
//     a reseller of the same models at a different markup.
//   - A model id is keyed once, by the first provider that has it, so the
//     vendor's own price wins over any gateway's.
//   - Keys are normalized exactly as llm.normalizeModelKey does at lookup
//     time. Both sides must agree or the table silently never matches.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const sourceURL = "https://models.dev/api.json"

// providerPrecedence lists the models.dev provider ids we snapshot, vendors
// first. Order is the whole point: "claude-sonnet-4-5" exists under anthropic
// (list price) and under a dozen gateways (markup), and we want the former.
var providerPrecedence = []string{
	// Vendors, in the order Orchestra's own catalog lists them.
	"anthropic", "openai", "google", "mistral", "deepseek", "xai",
	"moonshotai", "zhipuai", "minimax", "alibaba", "llama",
	// Gateways and clouds: fill in models no vendor above published.
	"groq", "cerebras", "togetherai", "fireworks-ai",
	"azure", "amazon-bedrock", "google-vertex",
	"openrouter", "lmstudio",
}

// upstream mirrors the parts of models.dev's schema we consume. Unknown
// fields are ignored, so an upstream addition can never break the generator.
type upstream struct {
	Models map[string]struct {
		Name       string `json:"name"`
		Attachment bool   `json:"attachment"`
		Reasoning  bool   `json:"reasoning"`
		ToolCall   bool   `json:"tool_call"`
		Structured bool   `json:"structured_output"`
		Modalities struct {
			Input []string `json:"input"`
		} `json:"modalities"`
		Limit struct {
			Context int `json:"context"`
			Output  int `json:"output"`
		} `json:"limit"`
		Cost struct {
			Input      float64 `json:"input"`
			Output     float64 `json:"output"`
			CacheRead  float64 `json:"cache_read"`
			CacheWrite float64 `json:"cache_write"`
		} `json:"cost"`
	} `json:"models"`
}

// snapshotModel is one row of models_data.json. Field names are short because
// the file carries ~800 of them; they are still words, not letters, because a
// human reviews this file in a diff.
type snapshotModel struct {
	Name       string  `json:"name,omitempty"`
	Provider   string  `json:"provider"`
	Ctx        int     `json:"ctx,omitempty"`
	MaxOut     int     `json:"max_out,omitempty"`
	In         float64 `json:"in,omitempty"`
	Out        float64 `json:"out,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	Caps       string  `json:"caps,omitempty"`
}

type snapshot struct {
	Source      string                   `json:"source"`
	GeneratedAt string                   `json:"generated_at"`
	Providers   []string                 `json:"providers"`
	Models      map[string]snapshotModel `json:"models"`
}

// normalizeModelKey must stay byte-identical to llm.normalizeModelKey. It is
// duplicated rather than imported because the generator is a main package
// inside the same module and importing llm from it would make `go generate`
// depend on the package it generates for.
func normalizeModelKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexAny(s, ":@"); i >= 0 {
		s = s[:i]
	}
	return strings.ReplaceAll(s, ".", "-")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "modelsgen:", err)
		os.Exit(1)
	}
}

func run() error {
	raw, err := fetch()
	if err != nil {
		return err
	}
	var all map[string]upstream
	if err := json.Unmarshal(raw, &all); err != nil {
		return fmt.Errorf("decode %s: %w", sourceURL, err)
	}

	out := snapshot{
		Source:      sourceURL,
		GeneratedAt: time.Now().UTC().Format("2006-01-02"),
		Models:      map[string]snapshotModel{},
	}
	for _, pid := range providerPrecedence {
		prov, ok := all[pid]
		if !ok {
			fmt.Fprintf(os.Stderr, "modelsgen: WARN provider %q is gone from models.dev\n", pid)
			continue
		}
		out.Providers = append(out.Providers, pid)
		for id, m := range prov.Models {
			key := normalizeModelKey(id)
			if key == "" {
				continue
			}
			if _, taken := out.Models[key]; taken {
				continue // an earlier, more canonical provider already claimed it
			}
			if m.Limit.Context == 0 && m.Cost.Input == 0 {
				continue // no window and no price: nothing this table could answer
			}
			out.Models[key] = snapshotModel{
				Name:       m.Name,
				Provider:   pid,
				Ctx:        m.Limit.Context,
				MaxOut:     m.Limit.Output,
				In:         m.Cost.Input,
				Out:        m.Cost.Output,
				CacheRead:  m.Cost.CacheRead,
				CacheWrite: m.Cost.CacheWrite,
				Caps:       caps(m.Attachment, m.Modalities.Input, m.ToolCall, m.Reasoning, m.Structured),
			}
		}
	}
	if len(out.Models) == 0 {
		return fmt.Errorf("no models selected — refusing to write an empty snapshot")
	}
	sort.Strings(out.Providers)

	data, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile("models_data.json", data, 0o644); err != nil {
		return err
	}
	fmt.Printf("modelsgen: wrote models_data.json — %d models from %d providers, %d KB\n",
		len(out.Models), len(out.Providers), len(data)/1024)
	return nil
}

// caps encodes the capability flags as a short letter set: v=vision,
// t=tools, r=reasoning, s=structured output (json_schema).
func caps(attachment bool, inputs []string, tool, reasoning, structured bool) string {
	var b strings.Builder
	vision := attachment
	for _, in := range inputs {
		if in == "image" {
			vision = true
		}
	}
	if vision {
		b.WriteByte('v')
	}
	if tool {
		b.WriteByte('t')
	}
	if reasoning {
		b.WriteByte('r')
	}
	if structured {
		b.WriteByte('s')
	}
	return b.String()
}

func fetch() ([]byte, error) {
	if local := os.Getenv("MODELSGEN_INPUT"); local != "" {
		return os.ReadFile(local) // offline regeneration from a saved api.json
	}
	c := &http.Client{Timeout: 90 * time.Second}
	resp, err := c.Get(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", sourceURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
