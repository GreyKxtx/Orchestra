package tools

import (
	"testing"
)

func TestRunnerCloseIdempotent(t *testing.T) {
	tmp := t.TempDir()
	runner, err := NewRunner(tmp, RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
