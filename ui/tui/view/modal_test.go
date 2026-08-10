package view

import (
	"strings"
	"testing"
)

// LSP install consent modal — automated smoke for the manual checklist in ui/tui/README.md.
func TestModal_LSPInstallRender(t *testing.T) {
	m := NewPermissionModal("lsp.install", "Install gopls for Go language support", "lsp.install")
	m.SetSize(80)
	out := m.Render()

	for _, want := range []string{
		"Language server",
		"gopls",
		"установить",
		"auto",
		"пропустить",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("LSP modal missing %q:\n%s", want, out)
		}
	}
}

func TestModal_ShellRender(t *testing.T) {
	m := NewModal("bash", "go test ./...")
	m.SetSize(80)
	out := m.Render()
	if strings.Contains(out, "Language server") {
		t.Fatal("shell modal must not use LSP title")
	}
	if !strings.Contains(out, "shell") {
		t.Fatalf("expected shell permission UI:\n%s", out)
	}
}
