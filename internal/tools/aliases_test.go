package tools

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

// TestListTools_NoDotsInCommodityNames verifies flat commodity tools have no dots.
func TestListTools_NoDotsInCommodityNames(t *testing.T) {
	commodity := []string{"ls", "read", "glob", "write", "edit", "grep", "symbols", "bash", "explore", "question"}
	for _, name := range commodity {
		if strings.Contains(name, ".") {
			t.Errorf("commodity tool %q must not contain a dot", name)
		}
	}
}

// TestListTools_AllNamesPresent verifies ListTools returns expected names.
func TestListTools_AllNamesPresent(t *testing.T) {
	defs := ListTools(true)
	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	want := []string{"ls", "read", "glob", "write", "edit", "grep", "symbols", "bash", "explore", "runtime_query", "todowrite", "todoread"}
	for _, n := range want {
		if !names[n] {
			t.Errorf("ListTools(allowExec=true): missing tool %q", n)
		}
	}
}

// TestListToolsForMode_NewModes verifies tool sets for the four new agent modes.
func TestListToolsForMode_NewModes(t *testing.T) {
	// general: has write+edit+task_result, no todowrite
	general := ListToolsForMode("general", false, false, false)
	generalNames := toolNameSet(general)
	for _, want := range []string{"read", "write", "edit", "grep", "task_result"} {
		if !generalNames[want] {
			t.Errorf("general mode: missing tool %q", want)
		}
	}
	if generalNames["todowrite"] {
		t.Error("general mode: must not include todowrite")
	}

	// compaction/title/summary: no tools at all
	for _, mode := range []string{"compaction", "title", "summary"} {
		defs := ListToolsForMode(mode, true, true, true)
		if len(defs) != 0 {
			t.Errorf("mode %q: expected 0 tools, got %d", mode, len(defs))
		}
	}
}

func toolNameSet(defs []llm.ToolDef) map[string]bool {
	m := make(map[string]bool, len(defs))
	for _, d := range defs {
		m[d.Function.Name] = true
	}
	return m
}

// TestListTools_ExecGating verifies bash is absent without allowExec.
func TestListTools_ExecGating(t *testing.T) {
	without := ListTools(false)
	for _, d := range without {
		if d.Function.Name == "bash" {
			t.Error("ListTools(allowExec=false) must not include bash")
		}
	}
	with := ListTools(true)
	found := false
	for _, d := range with {
		if d.Function.Name == "bash" {
			found = true
		}
	}
	if !found {
		t.Error("ListTools(allowExec=true) must include bash")
	}
}
