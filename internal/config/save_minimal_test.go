package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The TUI saves a preference by loading the config, changing one field and
// calling Save. Save marshalled the whole struct, so a five-line
// .orchestra.yml came back as forty-five: every default Load had filled in
// (limits, agent caps, exec, hooks, web, browser), and none of the user's
// comments. One Shift+Tab was enough. Save now writes what changed and leaves
// the rest of the file as the user wrote it.

func writeMinimalConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalConfig = `# shared project config
project_root: .
llm:
  api_base: http://127.0.0.1:1234/v1
  # the model everyone uses
  model: qwen/qwen3.8-27b
  timeout_s: 600
`

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSave_WritesOnlyWhatChanged(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LLM.Model = "qwen/qwen3.5-9b"
	cfg.UI.AllowExec = true
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	for _, want := range []string{"# shared project config", "# the model everyone uses", "model: qwen/qwen3.5-9b", "timeout_s: 600", "allow_exec: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("saved config lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"max_steps", "exclude_dirs", "context_kb", "max_tokens", "browser", "hooks", "web:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("saved config gained the default %q:\n%s", unwanted, got)
		}
	}
	if n := strings.Count(got, "\n"); n > 9 {
		t.Errorf("a two-field change grew the file to %d lines:\n%s", n, got)
	}

	back, err := Load(path)
	if err != nil {
		t.Fatalf("the saved config does not load: %v\n%s", err, got)
	}
	if back.LLM.Model != "qwen/qwen3.5-9b" || !back.UI.AllowExec || back.LLM.TimeoutS != 600 {
		t.Errorf("round trip lost a value: model=%q allow_exec=%v timeout=%d", back.LLM.Model, back.UI.AllowExec, back.LLM.TimeoutS)
	}
}

func TestSave_UnchangedConfigLeavesTheFileAsItWas(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != minimalConfig {
		t.Errorf("saving an unchanged config rewrote the file:\n%s", got)
	}
}

func TestSave_ClearingAValueRemovesItsKey(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig+"ui:\n  allow_exec: true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UI.AllowExec = false
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "allow_exec") {
		t.Errorf("a cleared value is still in the file:\n%s", got)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.UI.AllowExec {
		t.Error("allow_exec still loads as true")
	}
}

// A file that does not exist yet has nothing to preserve: Save writes the
// whole config, as init does.
func TestSave_NewFileIsWrittenWhole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	cfg := DefaultConfig(root)
	cfg.LLM.Model = "m"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "model: m") || !strings.Contains(got, "max_steps") {
		t.Errorf("a new config is not written whole:\n%s", got)
	}
}
