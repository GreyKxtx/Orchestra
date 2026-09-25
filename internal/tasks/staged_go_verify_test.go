package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/patches"
)

// stagedModule is a dry-run runner over a two-package module whose api
// package calls into lib. The disk copy builds.
func stagedModule(t *testing.T) *tools.Runner {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":          "module stagedmod\n\ngo 1.21\n",
		"lib/lib.go":      "package lib\n\n// Sum adds.\nfunc Sum(a, b int) int { return a + b }\n",
		"api/api.go":      "package api\n\nimport \"stagedmod/lib\"\n\n// Total calls lib.\nfunc Total() int { return lib.Sum(1, 2) }\n",
		"api/api_test.go": "package api\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tif Total() != 3 {\n\t\tt.Fatal(Total())\n\t}\n}\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	tr.SetDryRun(true)
	return tr
}

func stage(t *testing.T, tr *tools.Runner, path, search, replace string) {
	t.Helper()
	if err := tr.ApplyPatchesToStaged([]patches.Patch{{Type: patches.TypeFileSearchReplace, Path: path, Search: search, Replace: replace}}); err != nil {
		t.Fatalf("stage %s: %v", path, err)
	}
}

func findCheck(r WorkerVerifyReport, name string) (WorkerVerifyCheck, bool) {
	for _, c := range r.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return WorkerVerifyCheck{}, false
}

// Two workers each changed one package; each change compiles on its own. The
// staged pair does not: lib.Sum lost a parameter that api still passes. Only
// a build of the overlay — what the workspace will be once the turn is
// applied — sees it; the disk copy is untouched and still builds.
func TestVerifyStagedGo_CatchesABreakAcrossStagedFiles(t *testing.T) {
	tr := stagedModule(t)
	stage(t, tr, "lib/lib.go", "func Sum(a, b int) int { return a + b }", "func Sum(a int) int { return a }")

	report := VerifyWorkerOutcome(t.Context(), tr, []string{"lib/lib.go", "api/api.go"}, WorkerVerifyOptions{})
	if report.Passed {
		t.Fatalf("the staged change breaks api; verification must fail: %+v", report.Checks)
	}
	var sawAPI bool
	for _, c := range report.Checks {
		if c.Name == "go_build" && c.Path == "./api" && !c.OK && strings.Contains(c.Detail, "Sum") {
			sawAPI = true
		}
	}
	if !sawAPI {
		t.Fatalf("go build of ./api over the overlay must report the broken call: %+v", report.Checks)
	}
	if _, err := os.Stat(filepath.Join(tr.WorkspaceRoot(), ".orchestra", "tmp")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(tr.WorkspaceRoot(), ".orchestra", "tmp"))
		if len(entries) != 0 {
			t.Fatalf("overlay scratch files must be removed: %v", entries)
		}
	}
}

func TestVerifyStagedGo_TestsNeedExecConsent(t *testing.T) {
	tr := stagedModule(t)
	stage(t, tr, "lib/lib.go", "return a + b", "return b + a")

	noConsent := VerifyWorkerOutcome(t.Context(), tr, []string{"api/api.go"}, WorkerVerifyOptions{AffectedTests: true})
	build, ok := findCheck(noConsent, "go_build")
	if !ok || build.Skip || !build.OK {
		t.Fatalf("go build runs in dry-run without consent: %+v", noConsent.Checks)
	}
	test, _ := findCheck(noConsent, "go_test")
	if !test.Skip || !strings.Contains(test.Detail, "exec consent") {
		t.Fatalf("go test runs model-written code and needs consent: %+v", test)
	}

	withConsent := VerifyWorkerOutcome(t.Context(), tr, []string{"api/api.go"}, WorkerVerifyOptions{AffectedTests: true, AllowExec: true})
	if test, _ := findCheck(withConsent, "go_test"); test.Skip || !test.OK {
		t.Fatalf("with consent the affected tests run over the overlay: %+v", withConsent.Checks)
	}
	if !withConsent.Passed {
		t.Fatalf("report: %+v", withConsent.Checks)
	}
}

func TestVerifyStagedGo_StagedGoModIsSkipped(t *testing.T) {
	tr := stagedModule(t)
	stage(t, tr, "go.mod", "go 1.21", "go 1.22")
	report := VerifyWorkerOutcome(t.Context(), tr, []string{"lib/lib.go"}, WorkerVerifyOptions{})
	c, _ := findCheck(report, "go_build")
	if !c.Skip || !strings.Contains(c.Detail, "go.mod is staged") {
		t.Fatalf("a staged go.mod cannot be overlaid; the build must be skipped, not wrong: %+v", report.Checks)
	}
}
