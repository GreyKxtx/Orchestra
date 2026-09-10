package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

// The VS Code extension pins its own copy of the protocol version and throws
// on any mismatch with core.health *before* initialize
// (ui/vscode/src/coreSession.ts). Nothing else compares the two, which is how
// the extension came to sit at 14 against a core at 15 — unable to connect at
// all, with an error message blaming orchestra.exe. This test is the guard
// that was missing.
func TestVSCodeExtensionPinsCurrentProtocolVersion(t *testing.T) {
	// Only the TypeScript source is checked: ui/vscode/out/ is untracked local
	// build output, absent in a fresh clone and in CI, so a test that read it
	// would fail everywhere the extension has not been compiled.
	rel := filepath.Join("..", "..", "ui", "vscode", "src", "coreSession.ts")
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	m := regexp.MustCompile(`PROTOCOL_VERSION\s*=\s*(\d+)`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s: no PROTOCOL_VERSION assignment found", rel)
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("%s: PROTOCOL_VERSION is not a number: %v", rel, err)
	}
	if got != protocol.ProtocolVersion {
		t.Errorf("%s pins PROTOCOL_VERSION = %d, core is %d — the extension "+
			"refuses to connect on mismatch, so these must move together",
			rel, got, protocol.ProtocolVersion)
	}
}
