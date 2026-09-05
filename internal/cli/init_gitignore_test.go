package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignore_CreatesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	path := filepath.Join(dir, ".gitignore")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".orchestra.local.yml", ".orchestra/*", "!.orchestra/state.md", "*.orchestra.bak", "*.bak", "*.tmp", ".orchestra/*.db*", ".orchestra/playbooks/local/", ".orchestra/plans/local/"} {
		if !strings.Contains(string(first), want) {
			t.Errorf("gitignore missing %q:\n%s", want, first)
		}
	}

	// Second run must not duplicate the block.
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("second ensureGitignore: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(second) != string(first) {
		t.Errorf("gitignore changed on repeated init:\n%s", second)
	}
}

func TestSuggestLocalOverlay_DetectsInlineKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte("llm:\n  api_key: sk-123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Smoke test: must not panic and must be silent once the overlay exists.
	suggestLocalOverlay(dir, cfgPath)
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.local.yml"), []byte("llm:\n  api_key: sk-123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	suggestLocalOverlay(dir, cfgPath)
}

func TestEnsureGitignore_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "node_modules\n") {
		t.Errorf("existing content lost:\n%s", got)
	}
	if !strings.Contains(string(got), gitignoreMarker) {
		t.Errorf("orchestra block not appended:\n%s", got)
	}
}

func TestEnsureGitignore_MigratesLocalPlaybooksIgnore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	old := gitignoreMarker + " (added by orchestra init)\n.orchestra.local.yml\n.orchestra/*\n!.orchestra/playbooks/\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), gitignoreLocalPlaybooks) {
		t.Errorf("missing local playbooks ignore:\n%s", got)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(got) {
		t.Errorf("migration not idempotent:\n%s", again)
	}
}

func TestEnsureGitignore_IncludesOrchestraLocalMD(t *testing.T) {
	dir := t.TempDir()
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(got), "ORCHESTRA.local.md") {
		t.Errorf("gitignore missing ORCHESTRA.local.md:\n%s", got)
	}
}

func TestEnsureGitignore_MigratesOrchestraLocalMD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	old := gitignoreMarker + " (added by orchestra init)\n.orchestra.local.yml\n.orchestra/*\n!.orchestra/playbooks/\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "ORCHESTRA.local.md") {
		t.Errorf("missing ORCHESTRA.local.md backfill:\n%s", got)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(got) {
		t.Errorf("migration not idempotent:\n%s", again)
	}
}

func TestEnsureLearningDirs(t *testing.T) {
	dir := t.TempDir()
	if err := ensureLearningDirs(dir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		filepath.Join(".orchestra", "memory", "lessons"),
		filepath.Join(".orchestra", "playbooks", "local"),
	} {
		st, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || !st.IsDir() {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}
