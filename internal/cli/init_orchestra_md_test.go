package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOrchestraMD_CreatesWithDetectedLanguages(t *testing.T) {
	dir := t.TempDir()
	action, err := ensureOrchestraMD(dir, false, []string{"go", "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if action != "created" {
		t.Fatalf("action = %q, want created", action)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ORCHESTRA.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "Language / runtime: go, typescript") {
		t.Errorf("missing detected languages:\n%s", content)
	}
	// The rest of the template (navigation strategy, memory guidance) must
	// survive the fill-in — this is not a from-scratch generation.
	if !strings.Contains(content, "memory_write") {
		t.Errorf("template must keep the Memory section:\n%s", content)
	}
}

func TestEnsureOrchestraMD_NoLanguagesLeavesLineBlank(t *testing.T) {
	dir := t.TempDir()
	if _, err := ensureOrchestraMD(dir, false, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ORCHESTRA.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows checkouts of this repo carry CRLF (pre-existing, not something
	// tests should be sensitive to) — normalize before checking line content.
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Contains(content, "{{LANGUAGES}}") {
		t.Errorf("placeholder leaked into output:\n%s", content)
	}
	if !strings.Contains(content, "Language / runtime:\n") {
		t.Errorf("empty detection must leave a blank field for the human to fill, got:\n%s", content)
	}
}

func TestEnsureOrchestraMD_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	custom := "# My own rules\ndo not touch\n"
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := ensureOrchestraMD(dir, false, []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if action != "exists" {
		t.Fatalf("action = %q, want exists", action)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ORCHESTRA.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("existing ORCHESTRA.md must never be overwritten, got:\n%s", data)
	}
}

func TestEnsureOrchestraMD_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	action, err := ensureOrchestraMD(dir, true, []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if action != "would-create" {
		t.Fatalf("action = %q, want would-create", action)
	}
	if _, err := os.Stat(filepath.Join(dir, "ORCHESTRA.md")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write a file")
	}
}

func TestRunInit_CreatesOrchestraMDOnFreshProject(t *testing.T) {
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
	if _, err := os.Stat(filepath.Join(dir, "ORCHESTRA.md")); err != nil {
		t.Fatalf("ORCHESTRA.md not created by orchestra init: %v", err)
	}
}

func TestRunInit_ReRunOnOlderProjectBackfillsOrchestraMD(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Simulate a project that ran `orchestra init` before ORCHESTRA.md
	// existed: config is there, the project file is not.
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.yml"), []byte("llm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ORCHESTRA.md")); err != nil {
		t.Fatalf("re-running init on an older project must backfill ORCHESTRA.md: %v", err)
	}
}
