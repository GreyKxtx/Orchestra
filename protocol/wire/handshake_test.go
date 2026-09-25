package wire

import (
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

func TestNegotiate(t *testing.T) {
	for _, tc := range []struct {
		name         string
		client, core VersionRange
		want         int
		ok           bool
	}{
		{"same version", VersionRange{23, 23}, VersionRange{23, 23}, 23, true},
		{"old client, new core", VersionRange{23, 23}, VersionRange{23, 24}, 23, true},
		{"new client, old core", VersionRange{23, 24}, VersionRange{23, 23}, 23, true},
		{"both ranges: the newest shared", VersionRange{22, 24}, VersionRange{23, 25}, 24, true},
		{"client too old", VersionRange{21, 22}, VersionRange{23, 24}, 0, false},
		{"client too new", VersionRange{25, 26}, VersionRange{23, 24}, 0, false},
		{"zero", VersionRange{0, 0}, VersionRange{23, 24}, 0, false},
	} {
		got, ok := Negotiate(tc.client, tc.core)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: Negotiate(%v, %v) = %d, %v; want %d, %v", tc.name, tc.client, tc.core, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClientRange(t *testing.T) {
	if r := (InitializeParams{ProtocolVersion: 24}).ClientRange(); r != (VersionRange{24, 24}) {
		t.Fatalf("a client before v24 speaks one version: %v", r)
	}
	if r := (InitializeParams{ProtocolVersion: 24, MinProtocolVersion: 22}).ClientRange(); r != (VersionRange{22, 24}) {
		t.Fatalf("range: %v", r)
	}
	if r := (InitializeParams{ProtocolVersion: 24, MinProtocolVersion: 30}).ClientRange(); r != (VersionRange{24, 24}) {
		t.Fatalf("a min above max is ignored: %v", r)
	}
}

// The support window is one version: the core connects the previous
// release's clients, and MinProtocolVersion moves with ProtocolVersion.
func TestCoreRangeIsOneVersionWide(t *testing.T) {
	r := CoreRange()
	if r.Max != protocol.ProtocolVersion || r.Min != protocol.ProtocolVersion-1 {
		t.Fatalf("CoreRange() = %v, want [%d, %d]", r, protocol.ProtocolVersion-1, protocol.ProtocolVersion)
	}
}

func TestCapabilitiesHas(t *testing.T) {
	c := CoreCapabilities()
	for _, name := range []string{MethodSessionFork, NotifyAgentEvent, RequestQuestionAsk} {
		if !c.Has(name) {
			t.Errorf("core capabilities lack %q", name)
		}
	}
	if c.Has("session.teleport") {
		t.Error("a method that does not exist is not a capability")
	}
}
