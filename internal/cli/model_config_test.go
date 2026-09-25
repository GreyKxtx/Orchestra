package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// orchestra model rewrites llm.model and llm.extra_body.num_ctx in place —
// comments and the other keys stay — and does it under the config's lock,
// like every other writer of .orchestra.yml (ARCH-11).
func TestUpdateModelInConfig_RewritesInPlaceUnderTheLock(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	const before = "# my settings\nproject_root: .\nllm:\n  model: old-model\n  extra_body:\n    num_ctx: 100\nagent:\n  max_steps: 3\n"
	if err := os.WriteFile(cfgPath, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	// Hold the lock the way a concurrent Save does: the write must wait.
	lockPath := cfgPath + ".lock"
	if err := os.WriteFile(lockPath, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- updateModelInConfig(root, "new-model", 4096) }()
	select {
	case err := <-done:
		t.Fatalf("the config was written (%v) while another writer held its lock", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("updateModelInConfig: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("updateModelInConfig never proceeded after the lock was released")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# my settings", "model: new-model", "num_ctx: 4096", "max_steps: 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("config lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old-model") {
		t.Errorf("old model still there:\n%s", got)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Name() != ".orchestra.yml" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}
