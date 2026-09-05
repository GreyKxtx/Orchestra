package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMemorySlashCommand(t *testing.T) {
	cases := []struct {
		in, verb string
		ok       bool
	}{
		{"/memory open", "open", true},
		{"/memory refresh", "refresh", true},
		{"  /memory   open  ", "open", true},
		{"/memory", "", false},
		{"/memory list", "", false},
		{"/memories open", "", false},
		{"not a command", "", false},
	}
	for _, c := range cases {
		verb, ok := parseMemorySlashCommand(c.in)
		if ok != c.ok || verb != c.verb {
			t.Errorf("parseMemorySlashCommand(%q) = %q/%v, want %q/%v", c.in, verb, ok, c.verb, c.ok)
		}
	}
}

func TestResolveEditor_PrefersEnvVar(t *testing.T) {
	t.Setenv("EDITOR", "myeditor --flag")
	if got := resolveEditor(); got != "myeditor --flag" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEditor_FallsBackWhenUnset(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); got == "" {
		t.Error("must return a non-empty fallback")
	}
}

func TestMemoryOpenTarget_UsesExistingFallbackFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := memoryOpenTarget(dir)
	want := filepath.Join(dir, "AGENTS.md")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestMemoryOpenTarget_DefaultsToOrchestraMD(t *testing.T) {
	dir := t.TempDir()
	got := memoryOpenTarget(dir)
	want := filepath.Join(dir, "ORCHESTRA.md")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// $EDITOR may itself carry arguments ("code --wait"), so only its first
// field is the binary — the rest are flags that must survive into Args
// before the target file is appended.
func TestEditorCommand_SplitsEnvVarArgsAndAppendsTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EDITOR", "code --wait")

	c := editorCommand(dir)

	want := []string{"code", "--wait", filepath.Join(dir, "ORCHESTRA.md")}
	if len(c.Args) != len(want) {
		t.Fatalf("args = %v, want %v", c.Args, want)
	}
	for i := range want {
		if c.Args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, c.Args[i], want[i])
		}
	}
}

func TestMaybeRunMemoryCommand_UnrecognisedTextIsNotHandled(t *testing.T) {
	a := testChromeApp(t)
	if cmd := a.maybeRunMemoryCommand("/memory"); cmd != nil {
		t.Error("bare /memory is the existing view-only command, not a subcommand")
	}
	if cmd := a.maybeRunMemoryCommand("hello"); cmd != nil {
		t.Error("plain chat text must not be captured")
	}
}

func TestMaybeRunMemoryCommand_OpenReturnsACommand(t *testing.T) {
	a := testChromeApp(t)
	if cmd := a.maybeRunMemoryCommand("/memory open"); cmd == nil {
		t.Error("expected a command to open the editor")
	}
}

func TestMaybeRunMemoryCommand_RefreshShowsTheLastInject(t *testing.T) {
	a := testChromeApp(t)
	root := t.TempDir()
	a.cfg.WorkspaceRoot = root
	logDir := filepath.Join(root, ".orchestra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logLine := `{"event":"memory.inject","detail":"orchestra=42B total=42B/2048B"}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "llm_log.jsonl"), []byte(logLine), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := a.maybeRunMemoryCommand("/memory refresh")
	execCmdTree(cmd)

	found := false
	for _, m := range a.session.Messages {
		if strings.Contains(m.Text, "orchestra=42B") && strings.Contains(m.Text, "total=42B/2048B") {
			found = true
		}
	}
	if !found {
		t.Fatalf("refresh must show the last inject detail, messages: %+v", a.session.Messages)
	}
}
