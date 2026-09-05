package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every other list in the config replaces its inherited value, and for
// providers or agents that is right. Hooks are the exception: a user-wide
// audit hook exists precisely so that no project has to remember it, and a
// project that adds its own formatter hook has not asked for that gate to stop
// running.
func TestLoad_HookListsMergeAcrossLevels(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, `
llm:
  api_base: http://global/v1
  model: global-model
hooks:
  enabled: true
  pre_tool: ["./user-wide-audit.sh"]
`)
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+`
hooks:
  enabled: true
  pre_tool:
    - match: "write|edit"
      command: ["./project-formatter.sh"]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks.PreTool) != 2 {
		t.Fatalf("want both hooks, got %d: %#v", len(cfg.Hooks.PreTool), cfg.Hooks.PreTool)
	}
	// The user-wide gate runs first: it is the outer rule.
	if cfg.Hooks.PreTool[0].Command[0] != "./user-wide-audit.sh" {
		t.Fatalf("global hook must run first, got %#v", cfg.Hooks.PreTool)
	}
	if cfg.Hooks.PreTool[1].Match != "write|edit" {
		t.Fatalf("project hook lost its matcher: %#v", cfg.Hooks.PreTool[1])
	}
}

// .orchestra.yml is committed. A settings round-trip must not publish the hook
// a user configured in their home directory — that leaks a local script path
// into the repository and turns a personal gate into everyone's.
func TestSave_DoesNotPublishInheritedHooks(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, `
llm:
  api_base: http://global/v1
  model: global-model
hooks:
  enabled: true
  pre_tool: ["/home/me/private-audit.sh"]
`)
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+`
hooks:
  enabled: true
  pre_tool: ["./project-formatter.sh"]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "private-audit.sh") {
		t.Fatalf("the inherited hook was written into the committed config:\n%s", saved)
	}
	if !strings.Contains(string(saved), "project-formatter.sh") {
		t.Fatalf("the project's own hook was dropped:\n%s", saved)
	}
}

func TestLoad_HookListsFromOneLevelOnlyAreUnchanged(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, "llm:\n  api_base: http://global/v1\n  model: global-model\n")
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+`
hooks:
  enabled: true
  pre_tool: ["./only.sh"]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks.PreTool) != 1 || cfg.Hooks.PreTool[0].Command[0] != "./only.sh" {
		t.Fatalf("hooks = %#v", cfg.Hooks.PreTool)
	}
}
