package tools

import (
	"testing"

	"github.com/orchestra/orchestra/internal/toolspec"
)

// Every tool any surface can produce is in toolspec, so it is classified as
// parallel or serial and knows its consent. A new built-in tool added without
// a toolspec entry fails here, and so does an entry for a tool that is gone.
// MCP and plugin tools come through ExtraTools, stay out of the table and keep
// the conservative default by design.
func TestParallelFlags_AllBuiltinsClassified(t *testing.T) {
	caps := Capabilities{Exec: true, Web: true, Browser: true}
	maximal := ListTools(caps)
	maximal = append(maximal, ListToolsWithSubtasks(caps)...)
	for _, mode := range []string{"build", "plan", "explore", "ask", "debug", "architecture", "general", "orchestra", "worker", "verifier", "product", "documentation", "scout"} {
		maximal = append(maximal, ListToolsForMode(mode, caps, true, true)...)
	}
	// Built by the agent for the turn, not by a mode list.
	maximal = append(maximal, ToolSkillInvoke([]string{"sample"}), ToolSendMessage(), ToolAgentPost(), ToolTaskBoard())
	for _, d := range allToolDefsMap() {
		maximal = append(maximal, d)
	}

	seen := map[string]bool{}
	for _, d := range applyParallelFlags(maximal) {
		name := d.Function.Name
		seen[name] = true
		spec, ok := toolspec.Lookup(name)
		if !ok {
			t.Errorf("built-in tool %q has no toolspec entry", name)
			continue
		}
		if d.ParallelSafe != spec.Parallel || d.Mutating == spec.Parallel {
			t.Errorf("%q: ParallelSafe=%v Mutating=%v, toolspec says parallel=%v", name, d.ParallelSafe, d.Mutating, spec.Parallel)
		}
	}
	for _, s := range toolspec.All() {
		if !seen[s.Name] {
			t.Errorf("toolspec lists %q but no surface produces it", s.Name)
		}
	}
}

// builtinDefs is the one constructor table; it must cover exactly the tools
// toolspec says are named statically.
func TestBuiltinDefsMatchToolspec(t *testing.T) {
	for _, s := range toolspec.All() {
		_, has := builtinDefs[s.Name]
		static := s.Offer == toolspec.ByName || s.Offer == toolspec.LeadOnly
		if static != has {
			t.Errorf("%q: offer=%v, constructor present=%v", s.Name, s.Offer, has)
		}
		if has && builtinDefs[s.Name]().Function.Name != s.Name {
			t.Errorf("builtinDefs[%q] builds %q", s.Name, builtinDefs[s.Name]().Function.Name)
		}
	}
	for name := range builtinDefs {
		if _, ok := toolspec.Lookup(name); !ok {
			t.Errorf("builtinDefs has %q, toolspec does not", name)
		}
	}
}

// A custom agent naming a gated tool gets it only with that consent. The
// read-only gh queries and the git mutators past commit/branch/checkout/push
// used to resolve without exec consent, and bash.output / bash.kill did too:
// the gate named bash_output and bash_kill, which are no tools.
func TestResolveToolNamesWithPolicy_EveryGatedToolNeedsItsConsent(t *testing.T) {
	var names []string
	for _, s := range toolspec.All() {
		if toolspec.Nameable(s.Name) {
			names = append(names, s.Name)
		}
	}
	got, err := ResolveToolNamesWithPolicy(names, Capabilities{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		spec, _ := toolspec.Lookup(d.Function.Name)
		if spec.Consent() != toolspec.GroupBase {
			t.Errorf("%q resolved with no consent at all", d.Function.Name)
		}
	}
	for _, name := range []string{"bash.output", "bash.kill", "gh.pr.list", "git.worktree.add", "gh.pr.create"} {
		for _, d := range got {
			if d.Function.Name == name {
				t.Errorf("%q must need exec consent", name)
			}
		}
	}
}
