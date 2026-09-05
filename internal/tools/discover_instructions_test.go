package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverInstructions_LabelsActualFallbackFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// No ORCHESTRA.md in this package — only AGENTS.md, which LazyOrchestra
	// now falls back to. The label handed to the model must name the file
	// that actually supplied the text, not a file that doesn't exist.
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, dir)
	got := r.discoverInstructions(sub)

	if !strings.Contains(got, "AUTH PACKAGE RULES") {
		t.Fatalf("missing fallback content: %q", got)
	}
	if !strings.Contains(got, "pkg/auth/AGENTS.md") {
		t.Errorf("label must name the actual file (AGENTS.md), got: %q", got)
	}
	if strings.Contains(got, "pkg/auth/ORCHESTRA.md") {
		t.Errorf("label must not claim a file that does not exist: %q", got)
	}
}
