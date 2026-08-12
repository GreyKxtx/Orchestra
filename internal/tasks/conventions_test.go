package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
)

func writeConventions(t *testing.T, root, body string) {
	t.Helper()
	p := filepath.Join(root, ".orchestra", "playbooks", "conventions.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectConventions(t *testing.T) {
	root := t.TempDir()

	// Missing file → empty for every mode.
	if got := loadProjectConventions(root, agent.ModeArchitecture); got != "" {
		t.Fatalf("missing file must yield empty, got %q", got)
	}

	writeConventions(t, root, "## Stack\nGo 1.22, no new deps without approval\n")

	// Lead-grade modes receive the block.
	for _, m := range []agent.Mode{agent.ModeArchitecture, agent.ModeGeneral, agent.ModeDebug} {
		got := loadProjectConventions(root, m)
		if !strings.HasPrefix(got, `<project_conventions source=".orchestra/playbooks/conventions.md">`) {
			t.Fatalf("mode %s: missing wrapper, got %q", m, got)
		}
		if !strings.Contains(got, "Go 1.22") {
			t.Fatalf("mode %s: missing body", m)
		}
	}

	// Workers, scouts and the Docs Lead itself are exempt.
	for _, m := range []agent.Mode{agent.ModeWorker, agent.ModeExplore, agent.ModeAsk, agent.ModeDocs, agent.ModeProduct, agent.ModeVerifier} {
		if got := loadProjectConventions(root, m); got != "" {
			t.Fatalf("mode %s must not receive conventions, got %q", m, got)
		}
	}

	// Oversized file is truncated with a pointer to the full path.
	writeConventions(t, root, strings.Repeat("x", conventionsInjectMaxBytes+100))
	got := loadProjectConventions(root, agent.ModeArchitecture)
	if !strings.Contains(got, "truncated; read .orchestra/playbooks/conventions.md") {
		t.Fatal("expected truncation marker")
	}
	if len(got) > conventionsInjectMaxBytes+300 {
		t.Fatalf("injected block too large: %d bytes", len(got))
	}
}
