package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The field run left exactly one durable fact across 52 sessions and no way
// to tell from any artifact whether memory had tried and failed, or never
// tried. memory.note events in llm_log.jsonl (cc41475) answer that
// question; ParseNoteStats is how a human reads the answer without
// grepping JSON by hand.

func writeLLMLog(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "llm_log.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseNoteStats_CountsOutcomesAndSources(t *testing.T) {
	dir := t.TempDir()
	path := writeLLMLog(t, dir,
		`{"event":"llm_request"}`,
		`{"event":"memory.note","kind":"written","source":"model","detail":"[session:s1] ..."}`,
		`{"event":"memory.note","kind":"written","source":"digest","detail":"[session:s2] ..."}`,
		`{"event":"memory.note","kind":"written","source":"digest","detail":"[session:s3] ..."}`,
		`{"event":"memory.note","kind":"skipped","detail":"turn changed no files"}`,
		`{"event":"memory.note","kind":"failed","source":"model","detail":"disk full"}`,
		`{"event":"tool_call","tool_name":"read"}`,
	)

	stats, err := ParseNoteStats(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Written != 3 || stats.Skipped != 1 || stats.Failed != 1 {
		t.Fatalf("stats = %+v, want written=3 skipped=1 failed=1", stats)
	}
	if stats.FromModel != 1 || stats.FromDigest != 2 {
		t.Fatalf("stats = %+v, want model=1 digest=2", stats)
	}
	if stats.Total() != 5 {
		t.Errorf("Total() = %d, want 5", stats.Total())
	}
}

func TestParseNoteStats_MissingFileIsNotAnError(t *testing.T) {
	stats, err := ParseNoteStats(filepath.Join(t.TempDir(), "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("missing log must not be an error: %v", err)
	}
	if stats.Total() != 0 {
		t.Errorf("Total() = %d, want 0", stats.Total())
	}
}

// /memory refresh in the TUI reads the LAST memory.inject event back — what
// actually got injected on the most recent turn — the same file
// ParseNoteStats already reads for memory.note events.

func TestLastInjectDetail_ReturnsTheMostRecentOne(t *testing.T) {
	dir := t.TempDir()
	path := writeLLMLog(t, dir,
		`{"event":"memory.inject","detail":"orchestra=100B total=100B/2048B"}`,
		`{"event":"llm_request"}`,
		`{"event":"memory.inject","detail":"orchestra=200B repo=50B total=250B/2048B"}`,
	)

	got, err := LastInjectDetail(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "orchestra=200B repo=50B total=250B/2048B" {
		t.Errorf("got %q, want the last memory.inject event's detail", got)
	}
}

func TestLastInjectDetail_MissingFileIsNotAnError(t *testing.T) {
	got, err := LastInjectDetail(filepath.Join(t.TempDir(), "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("missing log must not be an error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty for a project with no turns yet", got)
	}
}

func TestLastInjectDetail_IgnoresOtherEvents(t *testing.T) {
	dir := t.TempDir()
	path := writeLLMLog(t, dir,
		`{"event":"memory.note","kind":"written","detail":"unrelated"}`,
	)

	got, err := LastInjectDetail(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty when no memory.inject event exists", got)
	}
}

func TestParseNoteStats_IgnoresMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := writeLLMLog(t, dir,
		`not json at all`,
		`{"event":"memory.note","kind":"written","source":"model"}`,
	)

	stats, err := ParseNoteStats(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Written != 1 {
		t.Errorf("Written = %d, want 1 (malformed line must be skipped, not fatal)", stats.Written)
	}
}
