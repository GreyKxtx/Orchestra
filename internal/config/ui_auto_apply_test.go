package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ui.auto_apply was advertised in docs/commands-and-modes.md as read from
// .orchestra.yml and had no reader anywhere in the codebase: the field was
// parsed and discarded. A key that is accepted and ignored is worse than an
// unsupported one — the user believes they changed something.
//
// It is tri-state so that unset keeps the behaviour the TUI already had.

func TestResolvedAutoApply_UnsetKeepsCommitting(t *testing.T) {
	if !(UIConfig{}).ResolvedAutoApply() {
		t.Fatal("an unset ui.auto_apply must keep the established behaviour")
	}
}

func TestResolvedAutoApply_HonoursBothExplicitValues(t *testing.T) {
	no, yes := false, true
	if (UIConfig{AutoApply: &no}).ResolvedAutoApply() {
		t.Error("ui.auto_apply: false must turn auto-commit off")
	}
	if !(UIConfig{AutoApply: &yes}).ResolvedAutoApply() {
		t.Error("ui.auto_apply: true must keep auto-commit on")
	}
}

// The value has to survive a real load: an explicit false must not read back
// as "unset", which is exactly how a *bool field goes wrong.
func TestAutoApplyFalseSurvivesLoad(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	body := "project_root: .\nllm:\n    api_base: http://127.0.0.1:1234/v1\n    model: m\nui:\n    auto_apply: false\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.AutoApply == nil {
		t.Fatal("an explicit auto_apply: false was read back as unset")
	}
	if cfg.UI.ResolvedAutoApply() {
		t.Fatal("auto_apply: false loaded, but resolves to auto-commit")
	}
}

func TestAutoApplyAbsentReadsAsUnset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	body := "project_root: .\nllm:\n    api_base: http://127.0.0.1:1234/v1\n    model: m\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.AutoApply != nil {
		t.Fatalf("a config with no ui block produced auto_apply = %v", *cfg.UI.AutoApply)
	}
	if !cfg.UI.ResolvedAutoApply() {
		t.Fatal("a config with no ui block must keep auto-commit on")
	}
}
