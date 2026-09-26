package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// applyFlagsForTest resets the apply command's flags for one test and
// restores them after it.
func applyFlagsForTest(t *testing.T) {
	t.Helper()
	saved := []*bool{&applyFlag, &planOnly, &allowExec, &allowWeb, &allowBrowser, &viaCore, &pipelineMode}
	savedVals := make([]bool, len(saved))
	for i, p := range saved {
		savedVals[i] = *p
		*p = false
	}
	strs := []*string{&fromPlan, &agentMode, &applyProvider, &applySkill, &outputPatch, &applyProfile, &applyWorktree, &applyResume}
	savedStrs := make([]string, len(strs))
	for i, p := range strs {
		savedStrs[i] = *p
		*p = ""
	}
	t.Cleanup(func() {
		for i, p := range saved {
			*p = savedVals[i]
		}
		for i, p := range strs {
			*p = savedStrs[i]
		}
	})
}

// resolveProject writes cfg as the project of a temp dir, chdirs into it and
// resolves an apply run there.
func resolveProject(t *testing.T, mutate func(*config.ProjectConfig), args ...string) (*applyRun, error) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig(dir)
	if mutate != nil {
		mutate(cfg)
	}
	if err := config.Save(filepath.Join(dir, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	SetTestClient(&applyScriptLLM{})
	t.Cleanup(ResetTestClient)
	return resolveApplyRun(applyCmd, args)
}

// The flags and the config resolve into one applyRun before anything runs:
// dry-run unless --apply, consent from the config's confirm settings, patch
// output forcing a dry run.
func TestResolveApplyRun(t *testing.T) {
	applyFlagsForTest(t)
	off := false
	r, err := resolveProject(t, func(cfg *config.ProjectConfig) {
		cfg.Exec.Confirm = &off
	}, "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if r.query != "do the thing" || !r.dryRun || r.backup {
		t.Errorf("a plain apply is a dry run without backups: %+v", r)
	}
	if !r.allowExec || r.allowWeb || r.allowBrowser {
		t.Errorf("exec.confirm: false stands for --allow-exec, nothing else: exec=%v web=%v browser=%v", r.allowExec, r.allowWeb, r.allowBrowser)
	}
	if r.applyOutput != config.ApplyOutputDisk || r.mode != "" {
		t.Errorf("output=%q mode=%q", r.applyOutput, r.mode)
	}

	applyFlag = true
	r, err = resolveProject(t, nil, "write it")
	if err != nil {
		t.Fatal(err)
	}
	if r.dryRun || !r.backup {
		t.Errorf("--apply writes with backups: %+v", r)
	}

	// apply.output: patch is a dry run whatever --apply says — and refuses --apply.
	if _, err := resolveProject(t, func(cfg *config.ProjectConfig) { cfg.Apply.Output = config.ApplyOutputPatch }, "patch it"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("patch output with --apply: err = %v", err)
	}
	applyFlag = false
	r, err = resolveProject(t, func(cfg *config.ProjectConfig) { cfg.Apply.Output = config.ApplyOutputPatch }, "patch it")
	if err != nil {
		t.Fatal(err)
	}
	if r.applyOutput != config.ApplyOutputPatch || !r.dryRun || r.backup {
		t.Errorf("patch output: %+v", r)
	}
}

// What refuses a command is decided before it runs: no query, an unknown
// profile, a subagent-only mode, a mode nobody defined.
func TestResolveApplyRun_Refusals(t *testing.T) {
	applyFlagsForTest(t)
	if _, err := resolveProject(t, nil); err == nil || !strings.Contains(err.Error(), "missing query") {
		t.Errorf("no query: err = %v", err)
	}
	applyProfile = "nonsense"
	if _, err := resolveProject(t, nil, "q"); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Errorf("unknown profile: err = %v", err)
	}
	applyProfile = ""
	agentMode = "worker"
	if _, err := resolveProject(t, nil, "q"); err == nil || !strings.Contains(err.Error(), "subagent") {
		t.Errorf("a subagent-only mode: err = %v", err)
	}
	agentMode = "nobody"
	if _, err := resolveProject(t, nil, "q"); err == nil || !strings.Contains(err.Error(), "unknown agent mode") {
		t.Errorf("an undefined mode: err = %v", err)
	}
}
