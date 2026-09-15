package tools

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// A CI runner has no business downloading language servers because a test
// workspace holds a .go file; ORCHESTRA_LSP_AUTO_INSTALL says so for the whole
// process, over whatever each workspace's config says.
func TestLSPAutoInstallEnvOverridesTheConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORCHESTRA_LSP_AUTO_INSTALL", "") // CI sets it for every test
	if got := mergeLSPConfig(root, config.LSPConfig{AutoInstall: "true"}).EffectiveAutoInstall(); got != "true" {
		t.Fatalf("without the variable the config decides: got %q", got)
	}
	t.Setenv("ORCHESTRA_LSP_AUTO_INSTALL", "false")
	if got := mergeLSPConfig(root, config.LSPConfig{AutoInstall: "true"}).EffectiveAutoInstall(); got != "false" {
		t.Errorf("ORCHESTRA_LSP_AUTO_INSTALL=false left auto_install at %q", got)
	}
}
