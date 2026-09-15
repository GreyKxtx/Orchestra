package agent

import "testing"

// Every mode the router can pick: with the browser on, the ones without
// browser tools keep the turn in agent mode; build already has them. Without
// the browser, routing is left alone.
func TestModeForRoutedTurn_KeepsAgentModeWhereTheBrowserWouldBeLost(t *testing.T) {
	for _, tc := range []struct {
		routed   string
		withMode string
		kept     bool
	}{
		{"build", "build", false},
		{"ask", "agent", true},
		{"explore", "agent", true},
		{"plan", "agent", true},
	} {
		if mode, kept := ModeForRoutedTurn(tc.routed, true); mode != tc.withMode || kept != tc.kept {
			t.Errorf("browser on, routed %s: got (%s, %v), want (%s, %v)", tc.routed, mode, kept, tc.withMode, tc.kept)
		}
		if mode, kept := ModeForRoutedTurn(tc.routed, false); mode != tc.routed || kept {
			t.Errorf("browser off, routed %s: got (%s, %v), want it unchanged", tc.routed, mode, kept)
		}
	}
	// Agent mode itself must offer the browser, or keeping it would not help.
	if !modeOffersBrowser(string(ModeAgent)) {
		t.Fatal("agent mode has no browser tools")
	}
}
