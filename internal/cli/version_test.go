package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

func TestVersionString_CarriesTheHandshakeContract(t *testing.T) {
	got := versionString()

	if !strings.Contains(got, "orchestra") {
		t.Errorf("version must name the binary, got: %q", got)
	}
	// initialize() hard-fails on a protocol/ops/tools mismatch, so when a TUI
	// or extension refuses to attach these three numbers are the first thing
	// anyone needs. Reporting a bug without them is reporting nothing.
	for _, want := range []string{
		fmt.Sprintf("protocol %d", protocol.ProtocolVersion),
		fmt.Sprintf("ops %d", protocol.OpsVersion),
		fmt.Sprintf("tools %d", protocol.ToolsVersion),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("version must contain %q, got: %q", want, got)
		}
	}
}

func TestVersionString_PrefersLdflagsBuildVersion(t *testing.T) {
	prev := buildVersion
	t.Cleanup(func() { buildVersion = prev })
	buildVersion = "v0.3.1"

	if got := versionString(); !strings.Contains(got, "v0.3.1") {
		t.Fatalf("release builds stamp the version with -ldflags, got: %q", got)
	}
}

func TestVersionString_FallsBackWhenUnstamped(t *testing.T) {
	prev := buildVersion
	t.Cleanup(func() { buildVersion = prev })
	buildVersion = ""

	// A `go build` with no ldflags is the normal developer path and must still
	// produce something identifiable rather than an empty field.
	got := versionString()
	if strings.Contains(got, "orchestra \n") || strings.Contains(got, "orchestra  ") {
		t.Fatalf("unstamped build left an empty version: %q", got)
	}
	if !strings.Contains(got, protocol.CoreVersion) {
		t.Fatalf("expected %q as the fallback, got: %q", protocol.CoreVersion, got)
	}
}

func TestRootCommand_ExposesVersionFlag(t *testing.T) {
	if strings.TrimSpace(rootCmd.Version) == "" {
		t.Fatal("rootCmd.Version is what gives cobra the --version flag")
	}
}
