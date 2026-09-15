package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// "shell · allow" approved every permission request, whatever it was for:
// with the switch on, a language server was installed, an MCP server ran the
// user's model or asked them a question, and an MCP tool that writes ran — all
// without a modal. The switch is about shell commands.
func TestShellAllow_ApprovesShellAndNothingElse(t *testing.T) {
	asks := []rpcclient.PermissionRequestPayload{
		{Tool: "lsp.install", Kind: "lsp.install", Description: "gopls"},
		{Tool: "mcp:linear", Kind: "mcp.sampling", Description: "1 message(s)"},
		{Tool: "mcp:linear", Kind: "mcp.elicitation", Description: "a question"},
		{Tool: "mcp:fs:write_file", Kind: "mcp.tool", Description: `{"path":"notes.txt"}`},
		{Tool: "read", Kind: "read", Description: "secrets.env"},
	}
	for i, ask := range asks {
		a, f := testCoreApp(t)
		a.allowExec = true
		ask.ReqID = int64(i + 1)
		a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventPermissionRequest, PermReq: &ask})
		if a.permModal == nil {
			t.Errorf("%s (%s) was answered without a modal under shell · allow", ask.Tool, ask.Kind)
		}
		if got := f.recordedPermAnswers(); len(got) != 0 {
			t.Errorf("%s (%s) answered on the user's behalf: %+v", ask.Tool, ask.Kind, got)
		}
	}

	for i, ask := range []rpcclient.PermissionRequestPayload{
		{Tool: "bash", Description: "go test ./..."},
		{Tool: "bash", Kind: "exec", Description: "go test ./..."},
	} {
		a, f := testCoreApp(t)
		a.allowExec = true
		ask.ReqID = int64(100 + i)
		a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventPermissionRequest, PermReq: &ask})
		got := f.recordedPermAnswers()
		if a.permModal != nil || len(got) != 1 || !got[0].Decision.Approved {
			t.Errorf("shell · allow did not approve %q (kind %q): modal=%v answers=%+v", ask.Description, ask.Kind, a.permModal != nil, got)
		}
	}
}

// The same holds for a request queued behind a modal and promoted later.
func TestShellAllow_QueuedMCPRequestStillGetsItsModal(t *testing.T) {
	a, f := testCoreApp(t)
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", Description: "ls", ReqID: 1}})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "mcp:fs:write_file", Kind: "mcp.tool", ReqID: 2}})

	a.respondShellPermission(true, true, false) // [a] on the shell prompt: shell · allow

	if cur, ok := a.perms.Current(); !ok || cur.ReqID != 2 || a.permModal == nil {
		t.Fatalf("the queued MCP tool request was not put to the user: current=%+v ok=%v", cur, ok)
	}
	if got := f.recordedPermAnswers(); len(got) != 1 || got[0].ReqID != 1 {
		t.Fatalf("only the shell request may be answered: %+v", got)
	}
}
