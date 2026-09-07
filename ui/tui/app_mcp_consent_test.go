package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// "[a]" on an MCP consent means "stop asking for this server": the core
// remembers it when the answer carries always=true. It must not flip shell ·
// allow, which is what the same key does on a shell prompt.
func TestPermissionModal_MCPAlwaysIsSentToCoreNotShellAllow(t *testing.T) {
	a, f := testCoreApp(t)
	a.allowExec = false

	a.handleRPCEvent(rpcclient.Event{
		Kind: rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{
			Tool: "mcp:linear", Description: "1 message(s), up to 512 tokens: hi", Kind: "mcp.sampling", ReqID: 9,
		},
	})
	if a.permModal == nil {
		t.Fatal("consent modal must be shown")
	}

	a.respondShellPermission(true, true, false)

	if a.allowExec {
		t.Fatal("answering an MCP consent flipped shell · allow")
	}
	answers := f.recordedPermAnswers()
	if len(answers) != 1 || answers[0].ReqID != 9 {
		t.Fatalf("answers=%+v", answers)
	}
	if !answers[0].Decision.Approved || !answers[0].Decision.Always {
		t.Fatalf("decision=%+v, want approved+always so the core stops asking for this server", answers[0].Decision)
	}
}

// A plain [y] approves once: no always, nothing remembered.
func TestPermissionModal_MCPOnceDoesNotSetAlways(t *testing.T) {
	a, f := testCoreApp(t)
	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "mcp:linear", Kind: "mcp.elicitation", ReqID: 10},
	})
	a.respondShellPermission(true, false, false)

	answers := f.recordedPermAnswers()
	if len(answers) != 1 || !answers[0].Decision.Approved || answers[0].Decision.Always {
		t.Fatalf("answers=%+v, want approved once without always", answers)
	}
}
