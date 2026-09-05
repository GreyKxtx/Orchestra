package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactAgentFile_KeepsFeedbackOverNewerProjectFacts(t *testing.T) {
	// Typing injection is not enough on its own: if compaction still trims by
	// recency, the correction is gone from the file before the ordering ever
	// gets to protect it.
	dir := t.TempDir()
	cfg := Config{MaxAgentKB: 1}
	cfg.Normalize()
	s := NewStore(dir, "sess-1", cfg)

	if _, _, err := s.AppendTyped("project", TypeFeedback, "FEEDBACK-MARKER never reformat untouched files"); err != nil {
		t.Fatal(err)
	}
	// Distinct on purpose: identical notes are now merged on write, so
	// repeating one would never fill the file.
	for i := 0; i < 12; i++ {
		if _, _, err := s.AppendTyped("project", TypeProject, distinctNote("PROJECT-MARKER", i)); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if len(body) > 4096 {
		t.Fatalf("compaction did not run: %d bytes", len(body))
	}
	if !strings.Contains(body, "FEEDBACK-MARKER") {
		t.Fatalf("the feedback entry was compacted away:\n%s", body)
	}
	// It should have cost some project facts to keep it.
	if strings.Count(body, "PROJECT-MARKER") >= 12 {
		t.Errorf("nothing was trimmed at all:\n%s", body)
	}
}

func TestCompactAgentFile_StillDropsReferenceFirst(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{MaxAgentKB: 1}
	cfg.Normalize()
	s := NewStore(dir, "sess-1", cfg)

	if _, _, err := s.AppendTyped("project", TypeReference, distinctNote("REFERENCE-MARKER", 0)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if _, _, err := s.AppendTyped("project", TypeFeedback, distinctNote("FEEDBACK-MARKER", i)); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "memory", "agent.md"))
	body := string(data)
	if strings.Contains(body, "REFERENCE-MARKER") {
		t.Errorf("a reference outlived feedback under pressure:\n%s", body)
	}
}

// distinctNote builds a note of a realistic size whose wording shares little
// with the others, so write-time merging leaves it alone.
func distinctNote(marker string, i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s note%d", marker, i)
	for w := 0; w < 30; w++ {
		fmt.Fprintf(&b, " word%d_%d", i, w)
	}
	return b.String()
}
