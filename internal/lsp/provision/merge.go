package provision

import (
	"strings"

	"github.com/orchestra/orchestra/internal/lsp/registry"
)

// ServerSpec is a config-shaped LSP server (mirrors lsp.LSPServerConfig without importing lsp).
type ServerSpec struct {
	Language   string
	Extensions []string
	Command    []string
	Disabled   bool
}

// SpecFromEntry builds a default server command from a registry recipe.
func SpecFromEntry(e registry.Entry) ServerSpec {
	cmd := append([]string{e.BinaryName}, e.DefaultArgs...)
	return ServerSpec{
		Language:   e.Language,
		Extensions: append([]string(nil), e.Extensions...),
		Command:    cmd,
	}
}

// MergeServers unions configured servers with detected recipes.
// Configured entries win on language clash; detected fill gaps.
func MergeServers(configured []ServerSpec, detected []registry.Entry) []ServerSpec {
	byLang := map[string]ServerSpec{}
	order := make([]string, 0, len(configured)+len(detected))

	for _, s := range configured {
		if s.Disabled || len(s.Command) == 0 {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(s.Language))
		if lang == "" {
			lang = strings.ToLower(filepathBaseSafe(s.Command[0]))
		}
		if _, ok := byLang[lang]; !ok {
			order = append(order, lang)
		}
		byLang[lang] = s
	}
	for _, e := range detected {
		lang := e.Language
		if _, ok := byLang[lang]; ok {
			continue
		}
		byLang[lang] = SpecFromEntry(e)
		order = append(order, lang)
	}
	out := make([]ServerSpec, 0, len(order))
	for _, lang := range order {
		out = append(out, byLang[lang])
	}
	return out
}

// MergeServersForWorkspace merges yaml config with workspace detect (phase C).
// When detect finds languages, servers for undetected languages are dropped so
// polyglot init fallback on a TS-only repo does not keep gopls/pyright registered.
// Empty detect (brand-new repo) keeps configured servers unchanged.
func MergeServersForWorkspace(configured []ServerSpec, workspaceRoot string) []ServerSpec {
	detected := Detect(workspaceRoot)
	merged := MergeServers(configured, detected)
	if len(detected) == 0 {
		return merged
	}
	hit := make(map[string]bool, len(detected))
	for _, e := range detected {
		hit[e.Language] = true
	}
	out := make([]ServerSpec, 0, len(merged))
	for _, s := range merged {
		if hit[s.Language] {
			out = append(out, s)
		}
	}
	return out
}

func filepathBaseSafe(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
