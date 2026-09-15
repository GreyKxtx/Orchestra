package rpcclient

import "testing"

func TestTurnParams_CarryAllowBrowserOnlyWhenOn(t *testing.T) {
	on := map[string]any{}
	addTurnFlags(on, AgentRunOptions{Apply: true, AllowExec: true, AllowBrowser: true})
	if on["allow_browser"] != true || on["allow_exec"] != true || on["apply"] != true || on["backup"] != true {
		t.Errorf("flags lost: %v", on)
	}
	off := map[string]any{}
	addTurnFlags(off, AgentRunOptions{})
	if _, sent := off["allow_browser"]; sent {
		t.Errorf("allow_browser sent for a turn without the browser: %v", off)
	}
}
