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
	for _, want := range []string{".orchestra.local.yml", ".orchestra/*", "!.orchestra/state.md", "*.orchestra.bak"} {
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
