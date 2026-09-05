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

// ORCHESTRA.local.md is a personal, gitignored overlay — "my" rules layered
// onto (not instead of) whatever the team's project-instructions file is.

func TestReadLayerRaw_AppendsOrchestraLocalMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "TEAM RULES")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.local.md"), "MY PERSONAL RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "TEAM RULES") || !strings.Contains(got, "MY PERSONAL RULES") {
		t.Fatalf("must include both team and personal rules, got: %q", got)
	}
}

func TestReadLayerRaw_LocalMDAloneCountsAsProjectInstructions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.local.md"), "ONLY PERSONAL RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "ONLY PERSONAL RULES") {
		t.Fatalf("a personal file with no team file must still be picked up, got: %q", got)
	}
}

func TestReadLayerRaw_LocalMDAppliesOnFallbackToo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "AGENTS RULES")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.local.md"), "MY PERSONAL RULES")

	got := NewStore(dir, "", DefaultConfig()).sliceLayer(layerOrchestra, 4096)

	if !strings.Contains(got, "AGENTS RULES") || !strings.Contains(got, "MY PERSONAL RULES") {
		t.Fatalf("personal notes must layer onto a fallback file too, got: %q", got)
	}
}

func TestList_ReportsOrchestraMDNameEvenWithLocalOverlay(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "TEAM RULES")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.local.md"), "MY PERSONAL RULES")

	entries := NewStore(dir, "", DefaultConfig()).List()

	for _, e := range entries {
		if e.Layer == layerOrchestra {
			if e.Path != "ORCHESTRA.md" {
				t.Errorf("Path = %q, want ORCHESTRA.md — the local overlay is not a file the operator switches to", e.Path)
			}
			return
		}
	}
	t.Fatal("orchestra layer missing from List()")
}

func TestReadByPath_OrchestraLocalMDIsAnAliasForTheOrchestraLayer(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "TEAM RULES")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.local.md"), "MY PERSONAL RULES")

	res := NewStore(dir, "", DefaultConfig()).Read("", "ORCHESTRA.local.md", 4096)

	if !strings.Contains(res.Content, "MY PERSONAL RULES") {
		t.Errorf("content = %q", res.Content)
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

func TestLazyOrchestra_AppliesLocalOverlayInNestedDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	writeFile(t, filepath.Join(sub, "ORCHESTRA.md"), "PKG RULES")
	writeFile(t, filepath.Join(sub, "ORCHESTRA.local.md"), "PKG PERSONAL RULES")

	got := NewStore(dir, "", DefaultConfig()).LazyOrchestra(sub)

	if !strings.Contains(got, "PKG RULES") || !strings.Contains(got, "PKG PERSONAL RULES") {
		t.Fatalf("nested local overlay must apply too, got: %q", got)
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
