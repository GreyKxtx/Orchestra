package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// execCmdTree synchronously executes a tea.Cmd and every command nested in
// tea.Batch results, so tests can drive App flows without a running program.
func execCmdTree(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			execCmdTree(c)
		}
	}
}

// testCoreApp is testChromeApp wired to a fake core client.
func testCoreApp(t *testing.T) (*App, *fakeCore) {
	t.Helper()
	a := testChromeApp(t)
	f := newFakeCore()
	a.rpc = f
	return a, f
}

func TestSubmitUserMessage_SendsSessionMessageViaCore(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1" // normally assigned during session bootstrap

	cmd := a.submitUserMessage("почини баг")
	if cmd == nil {
		t.Fatal("submitUserMessage must return a command")
	}
	if !a.turn.IsRunning() {
		t.Fatal("turn FSM must be running after submit")
	}
	execCmdTree(cmd)

	msgs := f.recordedSessionMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 session.message, got %d", len(msgs))
	}
	if msgs[0].Query != "почини баг" {
		t.Fatalf("query=%q", msgs[0].Query)
	}
	if msgs[0].SessionID == "" || msgs[0].SessionID != a.coreSessionID {
		t.Fatalf("session id mismatch: call=%q app=%q", msgs[0].SessionID, a.coreSessionID)
	}
}

func TestPermissionAutoApprove_AnswersWithReqID(t *testing.T) {
	a, f := testCoreApp(t)
	a.sessionToolAllow["bash"] = true

	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", Description: "ls", ReqID: 42},
	})

	if a.permModal != nil {
		t.Fatal("session-allowed tool must not open a modal")
	}
	answers := f.recordedPermAnswers()
	if len(answers) != 1 || answers[0].ReqID != 42 || !answers[0].Decision.Approved {
		t.Fatalf("answers=%+v", answers)
	}
}

func TestPermissionModal_AnswerCorrelatesReqID(t *testing.T) {
	a, f := testCoreApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", Description: "rm -rf /tmp/x", ReqID: 7},
	})
	if a.permModal == nil {
		t.Fatal("permission modal must be shown")
	}
	if cur, ok := a.perms.Current(); !ok || cur.ReqID != 7 {
		t.Fatalf("current perm=%+v ok=%v, want ReqID 7", cur, ok)
	}

	a.respondShellPermission(true, false, false)

	answers := f.recordedPermAnswers()
	if len(answers) != 1 || answers[0].ReqID != 7 || !answers[0].Decision.Approved {
		t.Fatalf("answers=%+v", answers)
	}
	if _, ok := a.perms.Current(); a.permModal != nil || ok {
		t.Fatal("modal state must be cleared after answering")
	}
}

func TestPermissionQueue_SecondRequestWaitsBehindModal(t *testing.T) {
	a, f := testCoreApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", Description: "first", ReqID: 1},
	})
	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "webfetch", Description: "second", ReqID: 2},
	})

	if cur, ok := a.perms.Current(); !ok || cur.ReqID != 1 {
		t.Fatalf("first request must be presented, got %+v ok=%v", cur, ok)
	}
	if a.perms.Waiting() != 1 {
		t.Fatalf("second request must wait, waiting=%d", a.perms.Waiting())
	}

	// Answer the first — the second must be promoted into a fresh modal.
	a.respondShellPermission(true, false, false)

	if cur, ok := a.perms.Current(); !ok || cur.ReqID != 2 {
		t.Fatalf("second request must be presented next, got %+v ok=%v", cur, ok)
	}
	if a.permModal == nil {
		t.Fatal("modal must be shown for the promoted request")
	}
	answers := f.recordedPermAnswers()
	if len(answers) != 1 || answers[0].ReqID != 1 {
		t.Fatalf("only the first request must be answered so far: %+v", answers)
	}
}

func TestUpdate_DropsStaleGenerationEvents(t *testing.T) {
	a, _ := testCoreApp(t)
	a.rpcGen = 3 // simulate a respawn having happened

	_, cmd := a.Update(rpcEventMsg{
		gen: 2, // event from the pre-respawn listener
		ev: rpcclient.Event{
			Kind:    rpcclient.EventPermissionRequest,
			PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", ReqID: 1},
		},
	})

	if a.permModal != nil {
		t.Fatal("stale event must not mutate state")
	}
	if cmd != nil {
		t.Fatal("stale listener must not be re-armed")
	}
}
