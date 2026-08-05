package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestFormatToolsCatalog(t *testing.T) {
	defs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "read", Description: "Читает файл. Возвращает content+hash."}},
		{Function: llm.ToolFunctionDef{Name: "edit", Description: "Search-and-replace.\nSecond line ignored."}},
		{Function: llm.ToolFunctionDef{Name: "explore", Description: ""}},
	}
	out := formatToolsCatalog(defs)
	for _, want := range []string{"<available_tools>", "read —", "edit —", "explore", "mutating"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Second line") {
		t.Fatal("should keep first sentence/line only")
	}
}

func TestFormatToolsCatalog_Empty(t *testing.T) {
	if got := formatToolsCatalog(nil); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
