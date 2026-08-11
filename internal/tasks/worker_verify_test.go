package tasks

import (
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestParseWorkerTaskResult(t *testing.T) {
	st, path := ParseWorkerTaskResult(`{"status":"success","path":"a.go"}`)
	if st != "success" || path != "a.go" {
		t.Fatalf("got status=%q path=%q", st, path)
	}
}

func TestWorkerTaskResultSuccess(t *testing.T) {
	if !workerTaskResultSuccess(`{"status":"success"}`) {
		t.Fatal("expected success")
	}
	if workerTaskResultSuccess(`{"status":"error"}`) {
		t.Fatal("expected false for error")
	}
}

func TestCollectEditedPaths(t *testing.T) {
	hist := []llm.Message{{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			Function: llm.ToolCallFunc{
				Name:      "edit",
				Arguments: llm.ToolArguments([]byte(`{"path":"internal/foo.go"}`)),
			},
		}},
	}}
	paths := CollectEditedPaths(hist, "handler.go")
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestGoBuildPackages(t *testing.T) {
	pkgs := goBuildPackages([]string{"internal/api/handler.go", "internal/api/util.go", "README.md"})
	if len(pkgs) != 1 || pkgs[0] != "./internal/api" {
		t.Fatalf("pkgs=%v", pkgs)
	}
}

func TestVerifyWorkerOutcome_EmptyPaths(t *testing.T) {
	report := VerifyWorkerOutcome(t.Context(), nil, nil)
	if !report.Passed {
		t.Fatal("empty paths should pass")
	}
}

func TestWrapWorkerVerifyFailure(t *testing.T) {
	raw := wrapWorkerVerifyFailure(`{"status":"success","path":"a.go"}`, WorkerVerifyReport{
		Passed: false,
		Checks: []WorkerVerifyCheck{{Name: "lsp", Path: "a.go", OK: false, Detail: "undefined: Foo"}},
	})
	if raw == "" || !workerTaskResultSuccess(`{"status":"success"}`) {
		t.Fatal("sanity")
	}
	if !containsAll(raw, "verification_failed", "undefined: Foo") {
		t.Fatalf("unexpected payload: %s", raw)
	}
}

func TestVerifierResultPassed(t *testing.T) {
	if !VerifierResultPassed("All good\n## VERIFICATION PASSED") {
		t.Fatal("expected pass")
	}
	if VerifierResultPassed("## VERIFICATION FAILED\nmissing test") {
		t.Fatal("expected fail marker")
	}
	if VerifierResultPassed("no marker here") {
		t.Fatal("inconclusive should not pass")
	}
	if VerifierResultPassed("## VERIFICATION PASSED\n## VERIFICATION FAILED") {
		t.Fatal("FAILED wins over PASSED")
	}
}

func TestFormatLLMVerifierPrompt(t *testing.T) {
	got := formatLLMVerifierPrompt(`{"intent":"x"}`, `{"status":"success"}`, []string{"a.go"}, WorkerVerifyReport{Passed: true})
	if !containsAll(got, "WorkOrder", "acceptance", "a.go", "VERIFICATION PASSED") {
		t.Fatalf("prompt missing sections:\n%s", got)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !containsStr(s, sub) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
