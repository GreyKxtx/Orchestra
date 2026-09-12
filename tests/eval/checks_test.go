package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A check that cannot fail proves nothing, and an eval suite built out of
// checks like that reports a number that means nothing either. So each test
// below feeds a check the FAKE it exists to catch — work that passes every
// substring check while being wrong — and requires the check to reject it.
//
// Every fake here came out of a real run against a local model.

// goWorkspace writes a module and returns the env a check is evaluated
// against, with `original` holding exactly what was written.
func goWorkspace(t *testing.T, files map[string]string) checkEnv {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return checkEnv{root: root, original: files}
}

// overwrite replaces a file, standing in for what the agent did to it.
func overwrite(t *testing.T, env checkEnv, rel, body string) {
	t.Helper()
	if err := os.WriteFile(env.abs(rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustPass(t *testing.T, env checkEnv, c Check) {
	t.Helper()
	if failure, _ := evaluateMechanicalCheck(env, c); failure != "" {
		t.Errorf("%s should have passed: %s", c.Type, failure)
	}
}

func mustFail(t *testing.T, env checkEnv, c Check, because string) {
	t.Helper()
	failure, handled := evaluateMechanicalCheck(env, c)
	if !handled {
		t.Fatalf("%s is not a known check type", c.Type)
	}
	if failure == "" {
		t.Errorf("%s accepted %s", c.Type, because)
	}
}

const goodModule = "module evalws\n\ngo 1.21\n"

// ---- go_build --------------------------------------------------------------

func TestCheck_GoBuildAcceptsAWorkspaceThatCompiles(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":   goodModule,
		"mathx.go": "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc main() {}\n",
	})
	mustPass(t, env, Check{Type: "go_build"})
}

// The defect that cost two of three eval rounds: a file written back with the
// module's import path where its package name belongs. Every substring check
// passes — the method is there, the import is there — and the workspace does
// not build.
func TestCheck_GoBuildCatchesAPackageClauseThatDoesNotBelong(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":  goodModule,
		"main.go": "package main\n\nfunc main() {}\n",
		"item.go": "package main\n\ntype Item struct{ Name string }\n",
	})
	overwrite(t, env, "item.go", "package evalws\n\ntype Item struct{ Name string }\n")
	mustFail(t, env, Check{Type: "go_build"}, "a file whose package clause is the import path")
}

// A file emptied, or one that never got its package clause at all.
func TestCheck_GoBuildCatchesAFileWithNoPackageClause(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":   goodModule,
		"mathx.go": "package main\n\nfunc main() {}\n",
	})
	overwrite(t, env, "mathx.go", "")
	mustFail(t, env, Check{Type: "go_build"}, "an empty .go file")
}

// An int concatenated onto a string reads correctly and does not compile.
func TestCheck_GoBuildCatchesTheIntConcatenatedOntoAString(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod": goodModule,
		"item.go": "package main\n\ntype Item struct {\n\tName string\n\tQty  int\n}\n\n" +
			"func (i Item) Label() string {\n\treturn i.Name + \" x\" + i.Qty\n}\n\nfunc main() {}\n",
	})
	mustFail(t, env, Check{Type: "go_build"}, "a string + int expression")
}

// A rename applied where the symbol is defined and not where it is called.
func TestCheck_GoBuildCatchesAHalfFinishedRename(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":   goodModule,
		"mathx.go": "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"main.go":  "package main\n\nfunc main() {\n\tprintln(Add(1, 2))\n}\n",
	})
	overwrite(t, env, "mathx.go", "package main\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n")
	mustFail(t, env, Check{Type: "go_build"}, "a rename that left its caller behind")
}

// ---- go_test ---------------------------------------------------------------

func TestCheck_GoTestAcceptsATestThatPasses(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":        goodModule,
		"mathx.go":      "package evalws\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"mathx_test.go": "package evalws\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"want 3\")\n\t}\n}\n",
	})
	mustPass(t, env, Check{Type: "go_test"})
}

// A test whose assertion is wrong is a failing test, and go_test is the only
// check that can say so: the file contains "func TestAdd" either way.
func TestCheck_GoTestCatchesAnAssertionThatDoesNotHold(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":        goodModule,
		"mathx.go":      "package evalws\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"mathx_test.go": "package evalws\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 4 {\n\t\tt.Fatal(\"want 4\")\n\t}\n}\n",
	})
	mustFail(t, env, Check{Type: "go_test"}, "a test whose assertion is false")
}

// ---- file_matches ----------------------------------------------------------

// "It wrote a test" is a substring away from true. A test function that
// asserts nothing satisfies file_contains "func Test"; only a shape says
// whether anything is being checked.
func TestCheck_FileMatchesSeesTheShapeNotTheWords(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"x_test.go": "package evalws\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\t_ = Add(1, 2)\n}\n",
	})
	asserts := Check{Type: "file_matches", Path: "x_test.go", Pattern: `t\.(Fatal|Error|Fatalf|Errorf)`}
	mustFail(t, env, asserts, "a test function that asserts nothing")

	overwrite(t, env, "x_test.go",
		"package evalws\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatalf(\"want 3\")\n\t}\n}\n")
	mustPass(t, env, asserts)
}

func TestCheck_FileMatchesRejectsABadPatternRatherThanPassing(t *testing.T) {
	env := goWorkspace(t, map[string]string{"a.go": "package main\n"})
	mustFail(t, env, Check{Type: "file_matches", Path: "a.go", Pattern: "("}, "an unparseable pattern")
}

func TestCheck_FileNotMatches(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"item.go": "package main\n\nfunc (i Item) Label() string {\n\treturn i.Name + string(rune(i.Qty))\n}\n",
	})
	// The number must be converted, not coerced through rune.
	mustFail(t, env,
		Check{Type: "file_not_matches", Path: "item.go", Pattern: `string\(rune\(`},
		"a number smuggled into a string through rune()")
}

// ---- file_unchanged / workspace_unchanged ----------------------------------

// A read-only task can only be graded by proving nothing moved.
func TestCheck_WorkspaceUnchangedProvesAReadOnlyTaskWroteNothing(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"go.mod":    goodModule,
		"mathx.go":  "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"README.md": "# evalws\n",
	})
	mustPass(t, env, Check{Type: "workspace_unchanged"})

	overwrite(t, env, "README.md", "# evalws\n\nNow with a paragraph nobody asked for.\n")
	mustFail(t, env, Check{Type: "workspace_unchanged"}, "an edit to a file the task asked about but did not ask to change")
}

func TestCheck_FileUnchanged(t *testing.T) {
	env := goWorkspace(t, map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n",
	})
	mustPass(t, env, Check{Type: "file_unchanged", Path: "a.go"})
	overwrite(t, env, "a.go", "package main\n\nfunc x() {}\n")
	mustFail(t, env, Check{Type: "file_unchanged", Path: "a.go"}, "a file the agent rewrote")
}

// Asserting that an undefined file is unchanged would pass for free, and a
// check that cannot fail is worse than no check: it reads as coverage.
func TestCheck_FileUnchangedRefusesAFileTheTaskNeverWrote(t *testing.T) {
	env := goWorkspace(t, map[string]string{"a.go": "package main\n"})
	mustFail(t, env, Check{Type: "file_unchanged", Path: "never-defined.go"},
		"a path the task does not define, which would pass vacuously")
}

// ---- reporting -------------------------------------------------------------

// The first line of `go build` output is often just "# evalws", which names
// nothing. A failure the reader cannot act on wastes the run.
func TestFirstToolchainError_SkipsThePackageBanner(t *testing.T) {
	out := "# evalws\n./item.go:9:23: invalid operation: i.Name + i.Qty (mismatched types)\n"
	got := firstToolchainError(out)
	if !strings.Contains(got, "mismatched types") {
		t.Errorf("the reported line must be the error, got %q", got)
	}
}

// An unknown type must still be reported by the caller, not swallowed here.
func TestMechanicalChecks_LeaveUnknownTypesToTheCaller(t *testing.T) {
	env := goWorkspace(t, map[string]string{"a.go": "package main\n"})
	if _, handled := evaluateMechanicalCheck(env, Check{Type: "file_contains", Path: "a.go"}); handled {
		t.Error("a substring check must fall through to the simple evaluator")
	}
}

// ---- answer checks ---------------------------------------------------------
//
// A question is graded on what the model said, and nothing was written, so
// every file check passes whatever it answered. These are the only checks
// that can see an ask-mode failure at all.

func answerEnv(t *testing.T, answer string, files map[string]string) checkEnv {
	t.Helper()
	env := goWorkspace(t, files)
	env.answer = answer
	return env
}

func TestCheck_AnswerNotEmpty(t *testing.T) {
	files := map[string]string{"a.go": "package main\n"}
	mustFail(t, answerEnv(t, "   \n\t ", files), Check{Type: "answer_not_empty"}, "whitespace as an answer")
	mustPass(t, answerEnv(t, "It reverses the string.", files), Check{Type: "answer_not_empty"})
}

func TestCheck_AnswerMatches(t *testing.T) {
	files := map[string]string{"a.go": "package main\n"}
	c := Check{Type: "answer_matches", Pattern: `(?i)(revers|backward|opposite order)`}
	mustPass(t, answerEnv(t, "Reverse returns the string backwards.", files), c)
	mustFail(t, answerEnv(t, "It is a helper in strutil.go.", files), c,
		"an answer that never says what the function does")
}

// The fluent invention: a confident list of functions for a package that is
// not there. Nothing is written, so only the answer can be judged.
func TestCheck_AnswerNotMatchesCatchesInventedFunctions(t *testing.T) {
	files := map[string]string{"a.go": "package main\n"}
	c := Check{Type: "answer_not_matches", Pattern: `func\s+[A-Z]\w*\(`}
	mustFail(t, answerEnv(t, "The package exposes func Track(event string) and func Flush().", files), c,
		"a list of functions for a package that does not exist")
	mustPass(t, answerEnv(t, "There is no such package in this project.", files), c)
}

// ---- answer_invents_no_path ------------------------------------------------

func inventEnv(t *testing.T, answer string) checkEnv {
	t.Helper()
	return answerEnv(t, answer, map[string]string{
		"go.mod":              goodModule,
		"main.go":             "package main\n\nfunc main() {}\n",
		"internal/store/s.go": "package store\n",
	})
}

func TestCheck_AnswerInventsNoPath(t *testing.T) {
	mustPass(t, inventEnv(t, "The code lives in internal/store and main.go."),
		Check{Type: "answer_invents_no_path"})
	mustFail(t, inventEnv(t, "Telemetry is under internal/telemetry, next to the store."),
		Check{Type: "answer_invents_no_path"},
		"a directory the project does not have")
}

// The check has to survive contact with ordinary prose, or it gets switched
// off and stops catching anything. An import path is not a claim about this
// workspace: its first segment is not an entry of the project.
func TestCheck_AnswerInventsNoPathIgnoresImportPaths(t *testing.T) {
	for _, answer := range []string{
		"It uses net/http and encoding/json.",
		"The dependency is github.com/spf13/cobra.",
		"See golang.org/x/tools/go/packages for the details.",
		"Run go test ./... to check.",
	} {
		if failure, _ := evaluateMechanicalCheck(inventEnv(t, answer), Check{Type: "answer_invents_no_path"}); failure != "" {
			t.Errorf("flagged ordinary prose as an invented path: %q → %s", answer, failure)
		}
	}
}

// A path named with a trailing slash is the same claim.
func TestCheck_AnswerInventsNoPathSeesTrailingSlashes(t *testing.T) {
	mustFail(t, inventEnv(t, "Handlers go in internal/api/."),
		Check{Type: "answer_invents_no_path"}, "internal/api/, which does not exist")
}

func TestExcerpt_KeepsAFailureLineReadable(t *testing.T) {
	long := strings.Repeat("word ", 100)
	got := excerpt(long)
	if len(got) > 200 {
		t.Errorf("excerpt must stay short, got %d chars", len(got))
	}
	if excerpt("") != "(nothing)" {
		t.Errorf("an empty answer must read as such, got %q", excerpt(""))
	}
}
