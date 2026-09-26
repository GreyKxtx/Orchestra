package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/patches"
)

// TestVerifyHelper is the acceptance check the preview below runs: the test
// binary printing the file ORCHESTRA_VERIFY_HELPER_FILE names.
func TestVerifyHelper(t *testing.T) {
	file := os.Getenv("ORCHESTRA_VERIFY_HELPER_FILE")
	if file == "" {
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		os.Stdout.WriteString("ERR " + err.Error())
		return
	}
	os.Stdout.WriteString(string(b))
}

// An acceptance check of a preview runs in the worker's shadow workspace
// against its staged edits (ORC-12); it used to be skipped in every core
// turn, and the report still said verified_success.
func TestAcceptanceChecks_RunInThePreviewsShadow(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true, BlockExecInDryRun: true, ShadowExec: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	ctx := context.Background()
	if err := tr.ApplyPatchesToStaged(ctx, []patches.Patch{{Type: patches.TypeFileSearchReplace, Path: "a.txt", Search: "disk", Replace: "staged"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCHESTRA_VERIFY_HELPER_FILE", "a.txt")
	r := &TaskRunner{toolRunner: tr}
	r.child.Caps.Exec = true
	cmd := os.Args[0] + " -test.run=TestVerifyHelper$ -test.count=1"
	out := r.runAcceptanceChecksAt(ctx, []AcceptanceCheck{{Cmd: cmd, ExpectExit: 0, ExpectStdout: "staged"}})
	if len(out) != 1 || out[0].Skip || !out[0].OK {
		t.Fatalf("the check must run in the shadow and see the staged edit: %+v", out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "disk\n" {
		t.Fatalf("the workspace changed under a preview: %q", b)
	}

	// Without a shadow the preview skips the check, as it always did.
	plain, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true, BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	r2 := &TaskRunner{toolRunner: plain}
	r2.child.Caps.Exec = true
	out = r2.runAcceptanceChecksAt(ctx, []AcceptanceCheck{{Cmd: cmd, ExpectExit: 0}})
	if len(out) != 1 || !out[0].Skip || !strings.Contains(out[0].Detail, "dry-run") {
		t.Fatalf("a preview without a shadow must skip: %+v", out)
	}
}

// In the shadow a staged go.mod is no obstacle: the build runs against the
// tree as it will be, where the overlay file had to skip.
func TestVerifyStagedGo_RunsInTheShadow(t *testing.T) {
	tr := stagedModule(t)
	_ = tr
	root := tr.WorkspaceRoot()
	shadowed, err := tools.NewRunner(root, tools.RunnerOptions{ShadowExec: true, BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shadowed.Close() })
	shadowed.SetDryRun(true)
	stage(t, shadowed, "go.mod", "go 1.21", "go 1.22")
	stage(t, shadowed, "lib/lib.go", "func Sum(a, b int) int { return a + b }", "func Sum(a int) int { return a }")
	report := VerifyWorkerOutcome(t.Context(), shadowed, []string{"lib/lib.go", "api/api.go"}, WorkerVerifyOptions{})
	c, ok := findCheck(report, "go_build")
	if !ok || c.Skip {
		t.Fatalf("the build must run in the shadow, staged go.mod and all: %+v", report.Checks)
	}
	if report.Passed {
		t.Fatalf("the staged break must fail the build: %+v", report.Checks)
	}
}

// An integration check the turn ended before is reported skipped, not
// dropped: a silent nil read as "the pieces fit".
func TestIntegrationVerify_SaysWhenItSkipped(t *testing.T) {
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	r := &TaskRunner{toolRunner: tr}
	entries := []*taskEntry{
		{worker: true, status: "done", edited: []string{"a.go"}},
		{worker: true, status: "done", edited: []string{"b.go"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := r.integrationVerify(ctx, entries)
	if raw == nil {
		t.Fatal("an integration check the turn ended before vanished")
	}
	var rep IntegrationReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Status != "skipped" || rep.Workers != 2 || !strings.Contains(rep.Summary, "skipped") {
		t.Fatalf("report = %+v, want skipped with the reason", rep)
	}
}
