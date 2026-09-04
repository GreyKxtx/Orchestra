package working

import (
	"strings"
	"testing"
)

func TestLastTurnDigest_ReturnsMostRecentEntry(t *testing.T) {
	root := t.TempDir()
	const sid = "s1"
	first := New("first goal")
	first.ObserveTool("edit", []byte(`{"path":"a.go"}`), nil, nil)
	second := New("second goal")
	second.ObserveTool("edit", []byte(`{"path":"b.go"}`), nil, nil)
	if err := PersistTurnDigest(root, sid, first.BuildTurnDigest(0)); err != nil {
		t.Fatal(err)
	}
	if err := PersistTurnDigest(root, sid, second.BuildTurnDigest(0)); err != nil {
		t.Fatal(err)
	}

	got := LastTurnDigest(root, sid)

	if !strings.Contains(got, "second goal") {
		t.Fatalf("expected the most recent digest, got: %q", got)
	}
	if strings.Contains(got, "first goal") {
		t.Fatalf("expected exactly one digest, got: %q", got)
	}
}

func TestLastTurnDigest_MissingSessionIsEmpty(t *testing.T) {
	if got := LastTurnDigest(t.TempDir(), "nope"); got != "" {
		t.Fatalf("expected empty for a session with no digests, got %q", got)
	}
}

func TestMemoryNoteFromDigest_KeepsDurableLinesOnly(t *testing.T) {
	digest := strings.Join([]string{
		"[turn_digest]",
		"step: 6",
		"goal: wire the weather panel",
		"done: edit src/App.jsx; add utils/weather.js",
		"open: hook up the icons",
		"files: src/App.jsx, src/utils/weather.js",
		"tools: read×20 edit×2",
		"errors: undefined: Foo",
	}, "\n")

	note := MemoryNoteFromDigest(digest)

	for _, want := range []string{"wire the weather panel", "edit src/App.jsx", "src/utils/weather.js"} {
		if !strings.Contains(note, want) {
			t.Errorf("note must carry %q, got: %q", want, note)
		}
	}
	// Tool counts, step numbers and one turn's transient errors say nothing
	// about the project a week later — they would just crowd out real facts.
	for _, unwanted := range []string{"tools:", "step:", "read×20", "undefined: Foo"} {
		if strings.Contains(note, unwanted) {
			t.Errorf("note must drop %q, got: %q", unwanted, note)
		}
	}
}

func TestMemoryNoteFromDigest_SkipsTurnsThatChangedNothing(t *testing.T) {
	// A turn that only read around produced no durable fact. Writing it would
	// refill agent.md with the same noise the grep notes used to.
	digest := "[turn_digest]\ngoal: what does this project do?\ntools: read×3\n"

	if note := MemoryNoteFromDigest(digest); note != "" {
		t.Fatalf("expected no note for a read-only turn, got %q", note)
	}
}

func TestMemoryNoteFromDigest_SkipsTurnsThatOnlyReadFiles(t *testing.T) {
	// files: is filled by read/grep/explore too, so a file list alone does not
	// mean anything changed. Only done: (write/edit/bash) marks a real change.
	digest := strings.Join([]string{
		"[turn_digest]",
		"goal: understand the search flow",
		"files: src/App.jsx, src/components/CitySearch.jsx",
		"tools: read×4 grep×2",
	}, "\n")

	if note := MemoryNoteFromDigest(digest); note != "" {
		t.Fatalf("reading files is not a change worth remembering, got %q", note)
	}
}

func TestMemoryNoteFromDigest_EmptyInput(t *testing.T) {
	if note := MemoryNoteFromDigest("  \n "); note != "" {
		t.Fatalf("expected empty note, got %q", note)
	}
}
