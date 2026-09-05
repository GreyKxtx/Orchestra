package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// setHome points os.UserHomeDir at a temp dir on both Windows and Unix so the
// global layer can be exercised without touching the real profile.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestFormatInject_AgentHeaderIsNotDoubleEncoded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "agent.md"),
		"\n---\n*2026-01-01T00:00:00Z*\n\nproject builds with vite\n")

	block := NewStore(dir, "", DefaultConfig()).FormatInject(4096)

	if !strings.Contains(block, "project builds with vite") {
		t.Fatalf("agent entry missing: %s", block)
	}
	// "вЂ" is the signature of a UTF-8 em dash decoded as cp1251 and re-encoded.
	// This header goes into every system prompt, so corruption here is served
	// to the model on every single step.
	if strings.Contains(block, "вЂ") {
		t.Fatalf("double-encoded text reaches the prompt: %q", block)
	}
}

func TestAppendSession_ErrorIsNotDoubleEncoded(t *testing.T) {
	store := NewStore(t.TempDir(), "", DefaultConfig())

	_, _, err := store.Append("session", "fact")

	if err == nil {
		t.Fatal("expected an error when no session is active")
	}
	if strings.Contains(err.Error(), "вЂ") {
		t.Fatalf("double-encoded text in error surfaced to the model: %q", err.Error())
	}
}

func TestAppend_KeepsMemoryValidUTF8(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, "", DefaultConfig())
	// Raw cp1251 bytes: how a Windows tool error reached a session memory file
	// in the field run. Memory is injected into every later prompt, so invalid
	// bytes here are re-sent to the model on every step of every session.
	invalid := "ls failed: \xcd\xe5\xf2 \xf4\xe0\xe9\xeb\xe0"

	if _, _, err := store.Append("project", invalid); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatalf("invalid UTF-8 persisted into memory: %q", data)
	}
	if !strings.Contains(string(data), "ls failed:") {
		t.Fatalf("sanitising must keep the readable part: %q", data)
	}
}

func TestFormatInject_HybridSkipsGlobalAndNonAgentRepoFiles(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	setHome(t, home)
	writeFile(t, filepath.Join(home, ".orchestra", "memory.md"), "GLOBAL-PREF")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "PROJECT-RULES")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "agent.md"),
		"\n---\n*2026-01-01T00:00:00Z*\n\nAGENT-FACT\n")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "notes.md"), "OTHER-REPO-FILE")

	cfg := DefaultConfig()
	cfg.Mode = ModeHybrid
	block := NewStore(dir, "", cfg).FormatInject(4096)

	if !strings.Contains(block, "PROJECT-RULES") {
		t.Errorf("hybrid must inject ORCHESTRA.md: %s", block)
	}
	if !strings.Contains(block, "AGENT-FACT") {
		t.Errorf("hybrid must inject recent agent.md entries: %s", block)
	}
	// hybrid is documented as "ORCHESTRA + session + recent agent entries; rest
	// via memory_read". Injecting everything is what eager is for.
	if strings.Contains(block, "GLOBAL-PREF") {
		t.Errorf("hybrid must leave the global layer to memory_read: %s", block)
	}
	if strings.Contains(block, "OTHER-REPO-FILE") {
		t.Errorf("hybrid must leave non-agent repo files to memory_read: %s", block)
	}
	if !strings.Contains(block, "<memory_hint>") {
		t.Errorf("hybrid must tell the model memory_read exists: %s", block)
	}
}

func TestFormatInject_EagerIncludesGlobalAndRepoFiles(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	setHome(t, home)
	writeFile(t, filepath.Join(home, ".orchestra", "memory.md"), "GLOBAL-PREF")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "PROJECT-RULES")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "agent.md"),
		"\n---\n*2026-01-01T00:00:00Z*\n\nAGENT-FACT\n")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "notes.md"), "OTHER-REPO-FILE")

	cfg := DefaultConfig()
	cfg.Mode = ModeEager
	block := NewStore(dir, "", cfg).FormatInject(8192)

	for _, want := range []string{"PROJECT-RULES", "AGENT-FACT", "OTHER-REPO-FILE", "GLOBAL-PREF"} {
		if !strings.Contains(block, want) {
			t.Errorf("eager must inject every layer, missing %q in: %s", want, block)
		}
	}
}

// FormatInjectReport is what /memory refresh and llm_log.jsonl's
// memory.inject event answer "what actually got injected" from — a byte
// breakdown per layer, otherwise only re-derivable by guessing file sizes.
func TestFormatInjectReport_BreaksDownBytesPerLayer(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	setHome(t, home)
	writeFile(t, filepath.Join(home, ".orchestra", "memory.md"), "GLOBAL-PREF")
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "PROJECT-RULES")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "agent.md"),
		"\n---\n*2026-01-01T00:00:00Z*\n\nAGENT-FACT\n")

	cfg := DefaultConfig()
	cfg.Mode = ModeEager
	block, detail, total := NewStore(dir, "", cfg).FormatInjectReport(8192)

	if !strings.Contains(block, "PROJECT-RULES") {
		t.Fatalf("report must still return the same block FormatInject would: %s", block)
	}
	for _, want := range []string{"orchestra=", "repo=", "global=", "total="} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q: %q", want, detail)
		}
	}
	if total <= 0 {
		t.Errorf("total = %d, want > 0", total)
	}
	if !strings.Contains(detail, fmt.Sprintf("total=%dB/8192B", total)) {
		t.Errorf("detail total/budget mismatch: %q (total=%d)", detail, total)
	}
}

func TestFormatInjectReport_EmptyProjectStillReportsZeroes(t *testing.T) {
	dir := t.TempDir()
	setHome(t, t.TempDir())

	_, detail, total := NewStore(dir, "", DefaultConfig()).FormatInjectReport(4096)

	if total != 0 {
		t.Errorf("total = %d, want 0 for an empty project", total)
	}
	if !strings.Contains(detail, "total=0B/4096B") {
		t.Errorf("detail = %q, want it to report zero bytes against the budget", detail)
	}
}

// FormatInject (the plain form every existing caller uses) must return
// exactly the block half of the report — no behavior change from adding
// the report.
func TestFormatInject_MatchesReportBlock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "PROJECT-RULES")

	plain := NewStore(dir, "", DefaultConfig()).FormatInject(4096)
	block, _, _ := NewStore(dir, "", DefaultConfig()).FormatInjectReport(4096)

	if plain != block {
		t.Errorf("FormatInject = %q, want it to equal FormatInjectReport's block %q", plain, block)
	}
}

func TestRead_AllLayerStaysCompleteUnderHybrid(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	setHome(t, home)
	writeFile(t, filepath.Join(home, ".orchestra", "memory.md"), "GLOBAL-PREF")
	writeFile(t, filepath.Join(dir, ".orchestra", "memory", "notes.md"), "OTHER-REPO-FILE")

	cfg := DefaultConfig()
	cfg.Mode = ModeHybrid
	// memory_read layer=all is the escape hatch hybrid points the model at, so
	// it must return what the eager inject would have.
	res := NewStore(dir, "", cfg).Read("all", "", 8192)

	if !strings.Contains(res.Content, "GLOBAL-PREF") {
		t.Errorf("memory_read all must include global: %s", res.Content)
	}
	if !strings.Contains(res.Content, "OTHER-REPO-FILE") {
		t.Errorf("memory_read all must include repo files: %s", res.Content)
	}
}
