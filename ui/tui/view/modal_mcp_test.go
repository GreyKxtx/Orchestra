package view

import (
	"strings"
	"testing"
)

// An MCP server asking for the model is not a shell command. The modal must
// say which server is asking and what for, and must not offer the shell
// modal's "[a] на сессию" — that key flips shell·allow, which has nothing to
// do with the server.
func TestModal_MCPSamplingRender(t *testing.T) {
	m := NewPermissionModal("mcp:linear", "2 message(s), up to 4096 tokens: summarise this issue", "mcp.sampling")
	m.SetSize(80)
	out := m.Render()

	for _, want := range []string{"MCP", "linear", "модел", "summarise this issue", "[y]", "[a]", "[n]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mcp.sampling modal missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"shell", "Команда:", "на сессию"} {
		if strings.Contains(out, reject) {
			t.Fatalf("mcp.sampling modal rendered as a shell prompt (%q):\n%s", reject, out)
		}
	}
}

func TestModal_MCPElicitationRender(t *testing.T) {
	m := NewPermissionModal("mcp:scaffold", "Need a few details to scaffold.", "mcp.elicitation")
	m.SetSize(80)
	out := m.Render()

	for _, want := range []string{"MCP", "scaffold", "вопрос", "Need a few details"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mcp.elicitation modal missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "модел") {
		t.Fatalf("elicitation is a question to the user, not a model call:\n%s", out)
	}
}
