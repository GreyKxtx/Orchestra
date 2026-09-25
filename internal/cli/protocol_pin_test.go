package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

// pinnedConstant reads `NAME = <number>` from a TypeScript source. Only the
// source is checked: ui/vscode/out/ is untracked local build output, absent
// in a fresh clone and in CI, so a test that read it would fail everywhere
// the extension has not been compiled.
func pinnedConstant(t *testing.T, rel, name string) int {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	m := regexp.MustCompile(`\b` + name + `\s*=\s*(\d+)`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s: no %s assignment found", rel, name)
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("%s: %s is not a number: %v", rel, name, err)
	}
	return got
}

// The VS Code extension pins its own copy of the protocol versions and
// checks them against core.health *before* initialize
// (ui/vscode/src/coreSession.ts). Nothing else compares the two, which is how
// the extension came to sit at 14 against a core at 15 — unable to connect at
// all, with an error message blaming orchestra.exe. Since ProtocolVersion 24
// the two sides negotiate within a range, so the extension in this tree must
// speak the core's range: the same numbers, kept together by this test.
func TestVSCodeExtensionPinsCurrentProtocolVersion(t *testing.T) {
	rel := filepath.Join("..", "..", "ui", "vscode", "src", "coreSession.ts")
	if got := pinnedConstant(t, rel, "PROTOCOL_VERSION"); got != protocol.ProtocolVersion {
		t.Errorf("%s pins PROTOCOL_VERSION = %d, core is %d — the extension "+
			"speaks the core's range, so these must move together",
			rel, got, protocol.ProtocolVersion)
	}
	if got := pinnedConstant(t, rel, "MIN_PROTOCOL_VERSION"); got != protocol.MinProtocolVersion {
		t.Errorf("%s pins MIN_PROTOCOL_VERSION = %d, core is %d",
			rel, got, protocol.MinProtocolVersion)
	}
}

// The extension sends its TOOLS_VERSION in initialize. The core no longer
// refuses a different one (it is informational since ProtocolVersion 24),
// but the number says which tools the extension was written against, so it
// is kept current.
func TestVSCodeExtensionPinsCurrentToolsVersion(t *testing.T) {
	rel := filepath.Join("..", "..", "ui", "vscode", "src", "coreSession.ts")
	if got := pinnedConstant(t, rel, "TOOLS_VERSION"); got != protocol.ToolsVersion {
		t.Errorf("%s pins TOOLS_VERSION = %d, core is %d", rel, got, protocol.ToolsVersion)
	}
}
