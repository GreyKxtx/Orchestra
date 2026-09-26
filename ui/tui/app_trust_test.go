package tui

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// A workspace whose own settings the core leaves out until it is trusted is
// noticed when the session opens, and /trust hands the core the trust; a
// trusted one is not mentioned (docs/security.md).
func TestTrust_AnUntrustedWorkspaceIsNoticedAndTrusted(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	f.trust = &rpcclient.WorkspaceTrust{Enforced: true, Trusted: false, Ignored: []string{"mcp.servers", "hooks"}}

	started := a.startCoreSession()().(coreSessionStartedMsg)
	a.handleCoreSessionStarted(started)
	last := a.session.Messages[len(a.session.Messages)-1]
	if !strings.Contains(last.Text, "не доверена") || !strings.Contains(last.Text, "mcp.servers, hooks") || !strings.Contains(last.Text, "/trust") {
		t.Fatalf("the chat names what is ignored and how to trust: %q", last.Text)
	}

	cmd := a.executePaletteCmd("/trust")
	if cmd == nil {
		t.Fatal("/trust must ask the core")
	}
	msg, ok := cmd().(workspaceTrustMsg)
	if !ok || msg.err != nil || msg.revoke {
		t.Fatalf("workspace.trust answered: %#v", msg)
	}
	if len(f.trustCalls) != 1 || f.trustCalls[0] {
		t.Fatalf("one workspace.trust without revoke: %v", f.trustCalls)
	}
	a.handleWorkspaceTrust(msg)
	if last := a.session.Messages[len(a.session.Messages)-1]; !strings.Contains(last.Text, "доверена") {
		t.Fatalf("the chat says the workspace is trusted: %q", last.Text)
	}

	msg = a.executePaletteCmd("/trust revoke")().(workspaceTrustMsg)
	if !msg.revoke || len(f.trustCalls) != 2 || !f.trustCalls[1] {
		t.Fatalf("/trust revoke forgets: %#v %v", msg, f.trustCalls)
	}
	a.handleWorkspaceTrust(msg)
	if last := a.session.Messages[len(a.session.Messages)-1]; !strings.Contains(last.Text, "снято") {
		t.Fatalf("the chat says the trust is gone: %q", last.Text)
	}

	// Trusted, or not enforced: nothing to say.
	for _, tr := range []*rpcclient.WorkspaceTrust{
		{Enforced: true, Trusted: true},
		{Enforced: false, Trusted: false, Ignored: []string{"hooks"}},
		{Enforced: true, Trusted: false},
	} {
		f.trust = tr
		n := len(a.session.Messages)
		a.handleCoreSessionStarted(a.startCoreSession()().(coreSessionStartedMsg))
		for _, m := range a.session.Messages[n:] {
			if strings.Contains(m.Text, "/trust") {
				t.Fatalf("nothing is ignored, yet the chat asks for trust (%+v): %q", tr, m.Text)
			}
		}
	}
}
