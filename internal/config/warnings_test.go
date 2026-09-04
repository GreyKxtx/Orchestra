package config

import (
	"strings"
	"testing"
)

func TestWarnings_EmbedProviderWithoutModel(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Embed.Provider = "openrouter"
	cfg.Embed.Model = ""

	warns := cfg.Warnings()

	// This exact shape shipped in the field config: a provider was set, so it
	// looked configured, but semantic_search is registered only when a model
	// is present. The tool simply never appeared and nothing said why.
	if len(warns) == 0 {
		t.Fatal("embed.provider without embed.model must warn")
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "embed.model") {
		t.Errorf("warning must name the missing key, got: %s", joined)
	}
	if !strings.Contains(joined, "semantic_search") {
		t.Errorf("warning must name what stays disabled, got: %s", joined)
	}
}

func TestWarnings_SilentWhenEmbedFullyConfigured(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Embed.Provider = "lmstudio"
	cfg.Embed.Model = "nomic-embed-text"

	for _, w := range cfg.Warnings() {
		if strings.Contains(w, "embed") {
			t.Errorf("a complete embed config must not warn, got: %s", w)
		}
	}
}

func TestWarnings_SilentWhenEmbedAbsent(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())

	for _, w := range cfg.Warnings() {
		if strings.Contains(w, "embed") {
			t.Errorf("embeddings are opt-in; not configuring them is not a problem, got: %s", w)
		}
	}
}

func TestWarnings_NilConfigIsSafe(t *testing.T) {
	var cfg *ProjectConfig
	if got := cfg.Warnings(); len(got) != 0 {
		t.Fatalf("nil config must not panic or invent warnings, got %v", got)
	}
}
