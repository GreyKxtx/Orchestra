package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/memory"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// whatever it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestRunMemoryStats_ReadsFromProjectRoot(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	logDir := filepath.Join(dir, ".orchestra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logLine := `{"event":"memory.note","kind":"written","source":"digest"}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "llm_log.jsonl"), []byte(logLine), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runMemoryStats(memoryStatsCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "written: 1") || !strings.Contains(out, "digest: 1") {
		t.Errorf("output = %q", out)
	}
}

func TestFormatMemoryStats_Empty(t *testing.T) {
	got := formatMemoryStats(memory.NoteStats{})
	if !strings.Contains(got, "No memory.note events") {
		t.Errorf("empty stats message = %q", got)
	}
}

func TestFormatMemoryStats_ReportsOutcomesAndSources(t *testing.T) {
	got := formatMemoryStats(memory.NoteStats{
		Written: 3, FromModel: 1, FromDigest: 2,
		Skipped: 10, Failed: 1,
	})
	for _, want := range []string{"written: 3", "model: 1", "digest: 2", "skipped: 10", "failed:  1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
