package tools_test

import (
	"strings"
	"testing"
)

// The memory tools, from the model's side. memory_write was reached by the
// eval and memory_read/memory_search by one task, and none of the three had a
// test that read the answer.
//
// The eval is structurally unable to cover the part that matters here. Project
// memory is injected into the system prompt before the turn starts, so a task
// that asks a question answerable from memory can be answered without calling
// any memory tool at all — memory_recalls_a_decision deliberately has no
// tool_used check for exactly that reason. What the eval cannot ask is whether
// a note written in one turn can be FOUND again, which is the entire promise
// of the tool group.

// A model that writes a note and is told only "written: 99" cannot check its
// own work: it has no path to read, and nothing to say to the user about where
// the decision was recorded.
func TestModelView_MemoryWriteSaysWhereTheNoteWent(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "memory_write", map[string]any{
		"content": "the dial timeout is 5s because the gateway drops idle connections at 6s",
		"type":    "project",
	})
	if !strings.Contains(out, ".orchestra/memory") {
		t.Errorf("memory_write did not name the file it wrote, so the model cannot verify "+
			"the note or tell the user where it lives:\n%s", out)
	}
	if !strings.Contains(out, "project") {
		t.Errorf("memory_write must echo the entry type: it decides how the note is ranked "+
			"when memory is sliced into a prompt, and a silently changed type is a note "+
			"that quietly stops being injected:\n%s", out)
	}
}

// The round trip is the whole point, and it is the one thing the eval cannot
// ask: a note written now must be readable and searchable afterwards.
func TestModelView_AWrittenNoteIsReadableAndSearchableAfterwards(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	const fact = "the dial timeout is 5s because the gateway drops idle connections at 6s"
	mustCall(t, r, "memory_write", map[string]any{"content": fact, "type": "project"})

	read := mustCall(t, r, "memory_read", map[string]any{})
	if !strings.Contains(read, "gateway") {
		t.Errorf("the note was written and memory_read cannot see it:\n%s", read)
	}
	if !strings.Contains(read, "[project]") {
		t.Errorf("the entry type did not survive the round trip:\n%s", read)
	}

	found := mustCall(t, r, "memory_search", map[string]any{"query": "gateway"})
	if !strings.Contains(found, "gateway") {
		t.Errorf("memory_search cannot find a note memory_write just made; a model told to "+
			"check what the project remembers will conclude it remembers nothing:\n%s", found)
	}
}

// A search that matches nothing must not look like a broken tool. This is the
// same rule glob already has — no matches is an answer, not an error — and the
// cost of getting it wrong is a retry loop on a question that has no answer.
func TestModelView_MemorySearchWithNoMatchesIsAnAnswerNotAnError(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "memory_search", map[string]any{"query": "nothing-was-ever-written-about-this"})
	if err != nil {
		t.Errorf("a search with no matches came back as an error:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "failed") {
		t.Errorf("an empty result reads as a failure:\n%s", out)
	}
}

// Reading memory before anything has been written happens on the first turn of
// every fresh project, so it must be an ordinary empty answer rather than a
// failure the model tries to work around.
func TestModelView_MemoryReadOnAFreshProjectIsEmptyNotBroken(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "memory_read", map[string]any{})
	if err != nil {
		t.Errorf("reading memory before anything was written is not an error:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "no such file") {
		t.Errorf("the model is shown a filesystem error for the normal case of a project "+
			"that has not recorded anything yet:\n%s", out)
	}
}

// An empty note is the one input the tool refuses, and the refusal has to say
// which argument was wrong — the model's only other option is to guess.
func TestModelView_MemoryWriteRefusesAnEmptyNoteAndSaysWhy(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "memory_write", map[string]any{"content": ""})
	if err == nil {
		t.Fatalf("an empty note was accepted; memory now holds a blank entry:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "content") {
		t.Errorf("the refusal does not name the argument at fault:\n%s", out)
	}
}
