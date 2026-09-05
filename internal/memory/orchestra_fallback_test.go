package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

// Most repos already carry instructions for another agent by the time
// Orchestra shows up. Ignoring AGENTS.md/CLAUDE.md/.cursorrules means
// starting blind on a repo that already has guidance — exactly the demo
// project's situation (no ORCHESTRA.md, no fallback, agent got nothing).

func TestReadLayerRaw_FallsBackToAgentsMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "AGENTS RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "AGENTS RULES") {
		t.Fatalf("must fall back to AGENTS.md, got: %q", got)
	}
}

func TestReadLayerRaw_FallsBackToClaudeMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "CLAUDE RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "CLAUDE RULES") {
		t.Fatalf("must fall back to CLAUDE.md, got: %q", got)
	}
}

func TestReadLayerRaw_FallsBackToCursorrules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".cursorrules"), "CURSOR RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "CURSOR RULES") {
		t.Fatalf("must fall back to .cursorrules, got: %q", got)
	}
}

func TestReadLayerRaw_OrchestraMDWinsOverFallbacks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "ORCHESTRA RULES")
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "AGENTS RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "ORCHESTRA RULES") || strings.Contains(got, "AGENTS RULES") {
		t.Fatalf("ORCHESTRA.md must win when both exist, got: %q", got)
	}
}

func TestReadLayerRaw_AgentsWinsOverClaudeMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "AGENTS RULES")
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "CLAUDE RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "AGENTS RULES") || strings.Contains(got, "CLAUDE RULES") {
		t.Fatalf("AGENTS.md must win over CLAUDE.md, got: %q", got)
	}
}

func TestList_ReportsActualFallbackFilename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "AGENTS RULES")

	entries := NewStore(dir, "", DefaultConfig()).List()

	found := false
	for _, e := range entries {
		if e.Layer == layerOrchestra {
			found = true
			if e.Path != "AGENTS.md" {
				t.Errorf("Path = %q, want AGENTS.md — operator must see which file is actually feeding the agent", e.Path)
			}
		}
	}
	if !found {
		t.Fatal("orchestra layer missing from List() despite AGENTS.md present")
	}
}

func TestRead_LayerOrchestra_ReportsActualFilename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "CLAUDE RULES")

	res := NewStore(dir, "", DefaultConfig()).Read("orchestra", "", 4096)

	if res.Path != "CLAUDE.md" {
		t.Errorf("Path = %q, want CLAUDE.md", res.Path)
	}
	if !strings.Contains(res.Content, "CLAUDE RULES") {
		t.Errorf("content missing: %q", res.Content)
	}
}

func TestLazyOrchestra_FallsBackInNestedDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "AUTH PACKAGE RULES")

	got := NewStore(dir, "", DefaultConfig()).LazyOrchestra(sub)

	if !strings.Contains(got, "AUTH PACKAGE RULES") {
		t.Fatalf("nested lazy discovery must fall back too, got: %q", got)
	}
}

func TestLazyOrchestra_OrchestraMDWinsOverAgentsInSameDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	writeFile(t, filepath.Join(sub, "ORCHESTRA.md"), "PKG ORCHESTRA RULES")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "PKG AGENTS RULES")

	got := NewStore(dir, "", DefaultConfig()).LazyOrchestra(sub)

	if !strings.Contains(got, "PKG ORCHESTRA RULES") || strings.Contains(got, "PKG AGENTS RULES") {
		t.Fatalf("ORCHESTRA.md must win in the same directory, got: %q", got)
	}
}
