package tools_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// verifier, product and documentation are the three modes the coverage map
// had as empty. Their write scopes are tested (worker_scope_test.go,
// docs_scope_test.go) and their prompts exist; what nothing asserted is the
// tool SURFACE — which tools each mode is handed in the first place.
//
// That is the cheaper of the two guards and the one that cannot be argued
// with at runtime: a scope check refuses a call the model already decided to
// make and spent a step on, while a tool absent from the schema is a call the
// model never makes. For verifier it is not an optimisation but the mode's
// entire reason to exist.

func toolNames(mode string, caps tools.Capabilities) map[string]bool {
	out := map[string]bool{}
	for _, d := range tools.ListToolsForMode(mode, caps, false, false) {
		out[d.Function.Name] = true
	}
	return out
}

func namesOfMode(mode string, caps tools.Capabilities) string {
	var got []string
	for n := range toolNames(mode, caps) {
		got = append(got, n)
	}
	return strings.Join(got, " ")
}

// A verifier that can write can fix what it was asked to judge, and its
// verdict stops meaning anything. This is the mode's defining property and
// nothing asserted it.
func TestChildModes_VerifierCannotChangeAnything(t *testing.T) {
	// Every capability on, because the point is that no FLAG unlocks a way to
	// write. bash is the documented exception — listToolsVerifier says
	// "read-only + diagnostics + optional bash", because verifying a goal
	// usually means running the test suite. It is a real hole in the
	// read-only claim (a shell can write) and it is the user's to open with
	// --allow-exec, so it is not asserted against here.
	got := toolNames("verifier", tools.Capabilities{Exec: true, Web: true, Browser: true})
	for _, forbidden := range []string{
		"write", "edit", "fs.delete", "fs.rename", "ast_rename", "lsp.rename",
		"memory_write",
		"git.commit", "git.branch", "git.checkout", "git.push",
	} {
		if got[forbidden] {
			t.Errorf("verifier is handed %q: a checker that can change the code can make "+
				"its own verdict come true", forbidden)
		}
	}
	// And it must still be able to look, or it cannot verify anything.
	for _, needed := range []string{"read", "grep", "ls"} {
		if !got[needed] {
			t.Errorf("verifier has no %q, so it cannot inspect what it is judging: %s",
				needed, namesOfMode("verifier", tools.Capabilities{}))
		}
	}
}

// Every child needs a way to hand its result back. general mode once
// advertised task_result to MAIN runs, where the runtime refuses it, and the
// model was left with no way to finish the turn; the mirror of that bug is a
// child with no task_result at all.
//
// verifier's own list does not carry one — tasks.ensureTaskResult appends it
// at launch. That indirection is exactly why this is worth pinning: the mode
// list looks wrong on its own and is right in the only place it is used.
func TestChildModes_EveryChildCanReportItsResult(t *testing.T) {
	for _, mode := range []string{"product", "documentation", "worker"} {
		if !toolNames(mode, tools.Capabilities{})["task_result"] {
			t.Errorf("child mode %q has no task_result, so it cannot answer the parent "+
				"that spawned it: %s", mode, namesOfMode(mode, tools.Capabilities{}))
		}
	}
}

// A child shares the parent's working tree, so a git.checkout in one child
// switches the branch under every sibling and the parent. Committing and
// publishing are the user's to do, and a child must not spawn children of its
// own — the depth is what makes a runaway run expensive.
//
// bash is deliberately not in this list: verifier and worker get it under
// --allow-exec so they can run the test suite they exist to run.
func TestChildModes_NoChildGetsNestedSpawnOrRepoMutatingGit(t *testing.T) {
	full := tools.Capabilities{Exec: true, Web: true, Browser: true}
	for _, mode := range []string{"verifier", "product", "documentation", "worker"} {
		got := toolNames(mode, full)
		for _, forbidden := range []string{
			"task", "task_spawn", "task_wait", "task_cancel",
			"git.commit", "git.checkout", "git.push", "git.branch",
			"git.worktree.add", "git.worktree.remove",
		} {
			if got[forbidden] {
				t.Errorf("child mode %q is handed %q even with every capability on; a child "+
					"shares the parent's working tree, and committing and publishing are the "+
					"user's to do", mode, forbidden)
			}
		}
	}
}

// product is the one mode that gets the web tools whether or not --allow-web
// was passed, because product discovery needs market research (registry.go,
// listToolsProduct — it does not take Capabilities at all).
//
// This pins that deliberate exception so it stays deliberate, and pins that it
// is the ONLY one: any other mode growing unconditional web access would be a
// capability flag that quietly stopped meaning anything.
func TestChildModes_OnlyProductGetsWebWithoutTheFlag(t *testing.T) {
	none := tools.Capabilities{}
	if got := toolNames("product", none); !got["webfetch"] || !got["websearch"] {
		t.Errorf("product lost its unconditional web access; if that was intended, the "+
			"comment on listToolsProduct needs to go too: %s", namesOfMode("product", none))
	}
	for _, mode := range []string{"verifier", "documentation", "worker", "build", "ask", "plan", "explore", "general"} {
		got := toolNames(mode, none)
		if got["webfetch"] || got["websearch"] {
			t.Errorf("mode %q is handed web tools with --allow-web off, so the flag no "+
				"longer decides anything", mode)
		}
	}
}
