package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestDiscoverInstructions_LabelsActualFallbackFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// No ORCHESTRA.md in this package — only AGENTS.md, which LazyOrchestra
	// now falls back to. The label handed to the model must name the file
	// that actually supplied the text, not a file that doesn't exist.
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, dir)
	got := r.discoverInstructions(context.Background(), sub)

	if !strings.Contains(got, "AUTH PACKAGE RULES") {
		t.Fatalf("missing fallback content: %q", got)
	}
	if !strings.Contains(got, "pkg/auth/AGENTS.md") {
		t.Errorf("label must name the actual file (AGENTS.md), got: %q", got)
	}
	if strings.Contains(got, "pkg/auth/ORCHESTRA.md") {
		t.Errorf("label must not claim a file that does not exist: %q", got)
	}
}

// A package directory without a file of its own must not be credited with
// the root's: LazyOrchestraFile walks up, and so does discoverInstructions,
// so the root text arrived twice — once labelled "internal/api/ORCHESTRA.md",
// a file that never existed (site-sandbox run 2, 2026-09-18).
func TestDiscoverInstructions_NestedDirWithoutFileDoesNotRepeatTheRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "internal", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ORCHESTRA.md"), []byte("ROOT RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, dir)
	got := r.discoverInstructions(context.Background(), sub)

	if strings.Count(got, "ROOT RULES") != 1 {
		t.Fatalf("root text must appear exactly once, got:\n%s", got)
	}
	if strings.Contains(got, "internal/api/ORCHESTRA.md") {
		t.Fatalf("a nested directory without a file must not be labelled as having one:\n%s", got)
	}
	if !strings.Contains(got, "Instructions from ORCHESTRA.md:") {
		t.Fatalf("the root file keeps its own label:\n%s", got)
	}
}

// Every agent of a run gets a directory's rules once — not only the first
// agent of the process to read there (audit §3.4).
func TestDiscoverInstructions_EachAgentGetsTheRulesOnce(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"), []byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := testMemoryRunner(t, dir)
	lead := llm.WithTrace(context.Background(), llm.Trace{RunID: "r1"})
	worker := llm.WithTrace(context.Background(), llm.Trace{RunID: "r1", TaskID: "task_1", Depth: 1})
	nextTurn := llm.WithTrace(context.Background(), llm.Trace{RunID: "r2"})

	if !strings.Contains(r.discoverInstructions(lead, sub), "AUTH PACKAGE RULES") {
		t.Fatal("the first agent gets the rules")
	}
	if got := r.discoverInstructions(lead, sub); got != "" {
		t.Fatalf("the same agent gets them once: %q", got)
	}
	if !strings.Contains(r.discoverInstructions(worker, sub), "AUTH PACKAGE RULES") {
		t.Fatal("a worker of the same run gets them too")
	}
	if !strings.Contains(r.discoverInstructions(nextTurn, sub), "AUTH PACKAGE RULES") {
		t.Fatal("the next turn gets them again")
	}
}

func TestInstructionSeen_IsBounded(t *testing.T) {
	var s instructionSeen
	for i := 0; i < maxInstructionAgents+10; i++ {
		s.firstTime(fmt.Sprintf("r/%d", i), "/x")
	}
	if len(s.sets) != maxInstructionAgents || len(s.order) != maxInstructionAgents {
		t.Fatalf("kept %d sets, want %d", len(s.sets), maxInstructionAgents)
	}
}
