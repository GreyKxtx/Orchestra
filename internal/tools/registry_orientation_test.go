package tools

import "testing"

// Asking "what is this project?" must not mean reading files one by one: the
// read-only modes get the repository map, which answers it in one call.
func TestReadOnlyModesCarryTheProjectMap(t *testing.T) {
	for _, mode := range []string{"ask", "explore"} {
		defs := ListToolsForMode(mode, Capabilities{}, false, false)
		names := make(map[string]bool, len(defs))
		for _, d := range defs {
			names[d.Function.Name] = true
		}
		if !names["repo_map"] {
			t.Errorf("mode %q has no repo_map: %v", mode, names)
		}
		// And it stays read-only.
		for _, forbidden := range []string{"write", "edit", "bash", "fs.delete"} {
			if names[forbidden] {
				t.Errorf("mode %q must stay read-only, but carries %q", mode, forbidden)
			}
		}
	}
}

// Orientation is not only an Ask-mode need. A build or debug turn that starts
// with "where does X live" had no cheap way to find out either, so it reached
// for read and walked the tree a file at a time.
func TestWorkingModesCarryTheProjectMapToo(t *testing.T) {
	for _, mode := range []string{"build", "plan", "debug", "general"} {
		defs := ListToolsForMode(mode, Capabilities{}, false, false)
		var found bool
		for _, d := range defs {
			if d.Function.Name == "repo_map" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mode %q has no repo_map", mode)
		}
	}
}
