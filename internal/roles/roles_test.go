package roles

import (
	"testing"

	"github.com/orchestra/orchestra/internal/toolspec"
)

// The registry's facts about a mode must agree with each other: they are
// what config, the tool registry, the dispatcher and the task runner read.
func TestRegistryIsConsistent(t *testing.T) {
	for _, s := range All() {
		if s.Prompt == "" {
			t.Errorf("%s: no prompt file", s.Name)
		}
		if s.Spawnable != (s.Summary != "") {
			t.Errorf("%s: a spawnable role needs a card, and only a spawnable role has one", s.Name)
		}
		if s.Kind == Internal && (s.Spawnable || len(s.Tools.Names) > 0 || s.Write != WriteNone) {
			t.Errorf("%s: an internal mode has no tools, writes nothing and is never spawned", s.Name)
		}
		if s.Kind == ChildOnly && !s.Spawnable {
			t.Errorf("%s: a child-only mode that cannot be spawned cannot run at all", s.Name)
		}
		for _, to := range s.CanSpawn {
			if !IsSpawnable(to) {
				t.Errorf("%s: CanSpawn names %q, which is not a spawnable role", s.Name, to)
			}
		}
		if len(s.CanSpawn) > 0 && !s.Spawnable {
			t.Errorf("%s: default flows are edges between subagents", s.Name)
		}
		if s.Tools.RepoMutating && (s.Kind != TopLevel || !s.WritesCode()) {
			t.Errorf("%s: the git mutators belong to top-level modes that change code", s.Name)
		}
		if s.Tier == TierWorker && s.Write != WriteTargets {
			t.Errorf("%s: a worker-band role works from a WorkOrder", s.Name)
		}
	}
}

// A mode's tool list and its write policy say the same thing: a read-only
// mode is offered nothing that changes files, and a mode that writes is
// offered write.
func TestToolListsMatchWritePolicy(t *testing.T) {
	changes := map[string]bool{"write": true, "edit": true, "fs.delete": true, "fs.rename": true, "ast_rename": true, "lsp.rename": true}
	for _, s := range All() {
		offersWrite := false
		for _, n := range s.Tools.Names {
			if changes[n] {
				offersWrite = true
			}
			if spec, _ := toolspec.Lookup(n); spec.Group != toolspec.GroupBase && spec.Group != toolspec.GroupWeb {
				t.Errorf("%s: %s belongs to a group; take the group (Caps, RepoMutating, Subtasks) rather than naming it", s.Name, n)
			}
		}
		switch {
		case s.Kind == Internal:
		case s.ReadOnly() && offersWrite:
			t.Errorf("%s is read-only but is offered a tool that changes files", s.Name)
		case !s.ReadOnly() && !offersWrite:
			t.Errorf("%s may write but is offered no tool to write with", s.Name)
		}
		if s.ReadOnly() && s.Tools.RepoMutating {
			t.Errorf("%s is read-only but takes the git mutators", s.Name)
		}
	}
}
