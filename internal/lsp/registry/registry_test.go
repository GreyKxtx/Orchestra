package registry_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/registry"
)

func TestCatalog_AtLeastTwelve(t *testing.T) {
	all := registry.All()
	if len(all) < 12 {
		t.Fatalf("catalog size=%d want >= 12", len(all))
	}
}

func TestByLanguage_DotnetAlias(t *testing.T) {
	e, ok := registry.ByLanguage("dotnet")
	if !ok || e.ID != "csharp-ls" {
		t.Fatalf("dotnet → %+v ok=%v", e, ok)
	}
	e2, ok := registry.ByLanguage("csharp")
	if !ok || e2.ID != "csharp-ls" {
		t.Fatalf("csharp → %+v ok=%v", e2, ok)
	}
}

func TestByExtension_CS(t *testing.T) {
	e, ok := registry.ByExtension(".cs")
	if !ok || e.BinaryName != "csharp-ls" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
}

func TestByExtension_YAML(t *testing.T) {
	e, ok := registry.ByExtension(".yml")
	if !ok || e.ID != "yaml-language-server" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
}
