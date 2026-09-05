package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func typedStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{}
	cfg.Normalize()
	return NewStore(dir, "sess-1", cfg), dir
}

func agentFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAppendTyped_WritesTheMarker(t *testing.T) {
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeFeedback, "Do not reformat untouched files."); err != nil {
		t.Fatal(err)
	}
	body := agentFile(t, root)
	if !strings.Contains(body, "[feedback]") {
		t.Errorf("agent.md = %q", body)
	}
	if !strings.Contains(body, "Do not reformat untouched files.") {
		t.Errorf("content missing: %q", body)
	}
}

func TestAppend_StaysProjectTyped(t *testing.T) {
	// The old two-argument call must keep working and keep meaning what it
	// always meant.
	s, root := typedStore(t)
	if _, _, err := s.Append("project", "Build runs via make."); err != nil {
		t.Fatal(err)
	}
	body := agentFile(t, root)
	if !strings.Contains(body, "[project]") {
		t.Errorf("agent.md = %q", body)
	}
}

func TestAppendTyped_UnknownTypeBecomesProject(t *testing.T) {
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", "nonsense", "x"); err != nil {
		t.Fatal(err)
	}
	if body := agentFile(t, root); !strings.Contains(body, "[project]") {
		t.Errorf("agent.md = %q", body)
	}
}

func TestInject_PutsFeedbackAboveOlderProjectFacts(t *testing.T) {
	// This is the whole point of typing: a correction written early must not
	// sink below a pile of newer file facts.
	s, _ := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeFeedback, "FEEDBACK-MARKER never reformat"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := s.AppendTyped("project", TypeProject, "PROJECT-MARKER touched a file"); err != nil {
			t.Fatal(err)
		}
	}
	got := s.sliceRepoMemory(8192, false)
	fi := strings.Index(got, "FEEDBACK-MARKER")
	pi := strings.Index(got, "PROJECT-MARKER")
	if fi < 0 || pi < 0 {
		t.Fatalf("injected slice is missing a marker:\n%s", got)
	}
	if fi > pi {
		t.Errorf("feedback came after project facts:\n%s", got)
	}
}
