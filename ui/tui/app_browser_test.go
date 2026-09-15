package tui

import "testing"

// /browser gives the chat's turns browser tools (allow_browser, protocol 19).
// It is not persisted: letting an agent drive a browser is asked for per run
// of the TUI, not remembered in .orchestra.yml.
func TestBrowserCommand_GivesTheNextTurnTheBrowser(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"

	if a.agentRunOptions().AllowBrowser {
		t.Fatal("the browser is on before anyone asked for it")
	}
	a.executePaletteCmd("/browser")
	if !a.allowBrowser {
		t.Fatal("/browser did not turn the browser on")
	}

	execCmdTree(a.submitUserMessage("открой страницу"))
	msgs := f.recordedSessionMessages()
	if len(msgs) != 1 || !msgs[0].Opts.AllowBrowser {
		t.Fatalf("the turn after /browser was sent without the browser: %+v", msgs)
	}

	a.executePaletteCmd("/browser")
	if a.allowBrowser || a.agentRunOptions().AllowBrowser {
		t.Error("a second /browser did not turn it off")
	}
}
