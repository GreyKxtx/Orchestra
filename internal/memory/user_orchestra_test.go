package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeUserOrchestra puts a personal instructions file in the fake home.
func writeUserOrchestra(t *testing.T, home, content string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".orchestra", "ORCHESTRA.md"), content)
}

// ~/.orchestra/memory.md was the only global layer, and both the human and the
// agent wrote to it. "How I want you to work" and "what you remembered" are
// different things with different lifetimes: the agent compacts and rewrites
// its memory, and a user's standing instructions must not be caught in that.
func TestInject_IncludesUserInstructions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "always answer in Russian\n")

	block := NewStore(t.TempDir(), "", DefaultConfig()).FormatInject(4096)

	if !strings.Contains(block, "always answer in Russian") {
		t.Fatalf("user instructions did not reach the prompt:\n%s", block)
	}
}

// The layer is instructions, not memory. memory.global gates the agent's own
// global notes; a user who turned those off still means their own standing
// instructions to apply.
func TestInject_UserInstructionsAreNotGatedByGlobalMemory(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "always answer in Russian\n")
	writeFile(t, filepath.Join(home, ".orchestra", "memory.md"), "user prefers tabs\n")

	cfg := DefaultConfig()
	cfg.GlobalEnabled = false
	block := NewStore(t.TempDir(), "", cfg).FormatInject(4096)

	if !strings.Contains(block, "always answer in Russian") {
		t.Errorf("user instructions were dropped with global memory disabled:\n%s", block)
	}
	if strings.Contains(block, "user prefers tabs") {
		t.Errorf("global memory leaked in with GlobalEnabled=false:\n%s", block)
	}
}

// Project instructions are the more specific ones, so they sit closest to the
// task: general first, specific last.
func TestInject_ProjectInstructionsFollowUserInstructions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "USERRULE always answer in Russian\n")

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ORCHESTRA.md"), "PROJECTRULE this repo is Go\n")

	block := NewStore(root, "", DefaultConfig()).FormatInject(4096)

	iUser := strings.Index(block, "USERRULE")
	iProject := strings.Index(block, "PROJECTRULE")
	if iUser < 0 || iProject < 0 {
		t.Fatalf("one of the two instruction layers is missing:\n%s", block)
	}
	if iUser > iProject {
		t.Errorf("project instructions came before the user's; the more specific "+
			"layer must sit closest to the task:\n%s", block)
	}
}

// Lazy mode injects instructions only and leaves memory to memory_read. User
// instructions are instructions, so they belong in that minimal block — a user
// on a small local model is exactly who cannot afford them to be skipped.
func TestInject_LazyModeCarriesUserInstructions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "always answer in Russian\n")

	cfg := DefaultConfig()
	cfg.Mode = ModeLazy
	block := NewStore(t.TempDir(), "", cfg).FormatInject(4096)

	if !strings.Contains(block, "always answer in Russian") {
		t.Fatalf("lazy mode dropped the user's instructions:\n%s", block)
	}
}

// No file, no change: every existing budget split stays exactly as it was for
// the overwhelmingly common case of a user who never made one.
func TestInject_NoUserFileLeavesTheBlockUnchanged(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ORCHESTRA.md"), "this repo is Go\n")
	writeFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"),
		"\n---\n*2026-01-01T00:00:00Z*\n\nbuilds with vite\n")

	store := NewStore(root, "", DefaultConfig())
	_, detail, _ := store.FormatInjectReport(4096)

	if strings.Contains(detail, "orchestra-user") {
		t.Errorf("a layer with no file reported a slice: %q", detail)
	}
}

func TestRead_UserInstructionsLayer(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "always answer in Russian\n")

	res := NewStore(t.TempDir(), "", DefaultConfig()).Read("orchestra-user", "", 4096)
	if !strings.Contains(res.Content, "always answer in Russian") {
		t.Fatalf("memory_read layer=orchestra-user returned %q", res.Content)
	}
	if res.Layer != "orchestra-user" {
		t.Errorf("Layer = %q, want orchestra-user", res.Layer)
	}
}

// The unknown-layer error is the only place the model learns which layers
// exist; a layer missing from it is a layer the model never asks for.
func TestRead_UnknownLayerMessageListsUserInstructions(t *testing.T) {
	res := NewStore(t.TempDir(), "", DefaultConfig()).Read("nonsense", "", 4096)
	if !strings.Contains(res.Content, "orchestra-user") {
		t.Errorf("unknown-layer help does not mention the new layer: %q", res.Content)
	}
}

func TestList_IncludesUserInstructions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeUserOrchestra(t, home, "always answer in Russian\n")

	var found bool
	for _, l := range NewStore(t.TempDir(), "", DefaultConfig()).List() {
		if l.Layer == "orchestra-user" {
			found = true
			if l.Bytes == 0 {
				t.Error("layer listed with 0 bytes despite having content")
			}
		}
	}
	if !found {
		t.Error("List() omits orchestra-user, so /memory cannot show it")
	}
}
