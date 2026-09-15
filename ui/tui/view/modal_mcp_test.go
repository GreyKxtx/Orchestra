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

// An MCP tool that can write is neither a shell command nor a server asking a
// question: the modal names the server, the tool and its arguments, and says
// the call is real even in a turn that does not apply changes.
func TestModal_MCPToolRender(t *testing.T) {
	m := NewPermissionModal("mcp:fs:write_file", `{"path":"notes/todo.txt","content":"buy milk"}`, "mcp.tool")
	m.SetSize(100)
	out := m.Render()

	for _, want := range []string{"write_file", "fs", "notes/todo.txt", "по-настоящему", "[y]", "[a] до конца хода", "[t]", "[n]"} {
		if !strings.Contains(out, want) {
			t.Errorf("mcp.tool modal missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"вопрос", "модел", "shell", "Команда:"} {
		if strings.Contains(out, reject) {
			t.Errorf("mcp.tool modal reads as another kind of request (%q):\n%s", reject, out)
		}
	}
}
