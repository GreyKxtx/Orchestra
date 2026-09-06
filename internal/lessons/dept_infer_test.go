package lessons

import "testing"

func TestInferDeptFromFiles_NoFiles_ReturnsEmpty(t *testing.T) {
	if got := InferDeptFromFiles(nil); got != "" {
		t.Errorf("InferDeptFromFiles(nil) = %q, want empty", got)
	}
	if got := InferDeptFromFiles([]string{}); got != "" {
		t.Errorf("InferDeptFromFiles([]) = %q, want empty", got)
	}
}

func TestInferDeptFromFiles_UnrecognizedExtensions_ReturnsEmpty(t *testing.T) {
	got := InferDeptFromFiles([]string{"README.md", "docs/notes.yml", "config.json"})
	if got != "" {
		t.Errorf("InferDeptFromFiles(unrecognized) = %q, want empty", got)
	}
}

func TestInferDeptFromFiles_SingleGoFile(t *testing.T) {
	got := InferDeptFromFiles([]string{"internal/agent/agent.go"})
	if got != "go_engineering" {
		t.Errorf("got %q, want go_engineering", got)
	}
}

func TestInferDeptFromFiles_MajorityWins(t *testing.T) {
	got := InferDeptFromFiles([]string{
		"a.go", "b.go", "c.go",
		"ui/app.tsx",
	})
	if got != "go_engineering" {
		t.Errorf("got %q, want go_engineering (3 go files vs 1 tsx)", got)
	}
}

func TestInferDeptFromFiles_TieBreaksOnFirstFile(t *testing.T) {
	// Equal counts (1 python, 1 go); files list is the tie-break order.
	got := InferDeptFromFiles([]string{"a.py", "b.go"})
	if got != "python_engineering" {
		t.Errorf("got %q, want python_engineering (first file wins the tie)", got)
	}
}

func TestInferDeptFromFiles_IgnoresUnrecognizedWhenCountingMajority(t *testing.T) {
	got := InferDeptFromFiles([]string{"README.md", "main.py", "notes.txt"})
	if got != "python_engineering" {
		t.Errorf("got %q, want python_engineering (only recognized file)", got)
	}
}

func TestInferDeptFromFiles_TypeScriptAndTSX(t *testing.T) {
	if got := InferDeptFromFiles([]string{"src/index.ts"}); got != "typescript_engineering" {
		t.Errorf(".ts: got %q, want typescript_engineering", got)
	}
	if got := InferDeptFromFiles([]string{"src/App.tsx"}); got != "typescript_engineering" {
		t.Errorf(".tsx: got %q, want typescript_engineering", got)
	}
}
