package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

// pinnedConstant reads `NAME = <number>` from a TypeScript source.
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

// The VS Code extension used to pin its own copy of the protocol version
// and throw on any mismatch with core.health *before* initialize. Nothing
// compared the two copies, which is how the extension came to sit at 14
// against a core at 15 — unable to connect at all, with an error message
// blaming orchestra.exe. Its versions now come from the contract generated
// out of protocol/wire (ui/vscode/src/protocol/wire.generated.ts), which
// protocol/wire's own test keeps current. This checks the extension takes
// them from there and pins no copy of its own.
//
// Only the TypeScript source is checked: ui/vscode/out/ is untracked local
// build output, absent in a fresh clone and in CI.
func TestVSCodeExtensionTakesVersionsFromTheGeneratedContract(t *testing.T) {
	session := filepath.Join("..", "..", "ui", "vscode", "src", "coreSession.ts")
	data, err := os.ReadFile(session)
	if err != nil {
		t.Fatalf("read %s: %v", session, err)
	}
	src := string(data)
	if !strings.Contains(src, `from "./protocol/wire.generated"`) {
		t.Errorf("%s does not import its protocol versions from ./protocol/wire.generated", session)
	}
	if m := regexp.MustCompile(`(?m)^\s*(?:export\s+)?const\s+(MIN_)?PROTOCOL_VERSION\s*=`).FindString(src); m != "" {
		t.Errorf("%s pins a copy of the protocol version by hand (%q); it comes from the generated contract", session, strings.TrimSpace(m))
	}

	generated := filepath.Join("..", "..", "ui", "vscode", "src", "protocol", "wire.generated.ts")
	for name, want := range map[string]int{
		"PROTOCOL_VERSION":     protocol.ProtocolVersion,
		"MIN_PROTOCOL_VERSION": protocol.MinProtocolVersion,
		"OPS_VERSION":          protocol.OpsVersion,
		"TOOLS_VERSION":        protocol.ToolsVersion,
	} {
		if got := pinnedConstant(t, generated, name); got != want {
			t.Errorf("%s: %s = %d, core is %d — run: go generate ./protocol/wire/...", generated, name, got, want)
		}
	}
}
