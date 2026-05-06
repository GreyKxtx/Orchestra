package tools

import (
	"strings"
	"testing"
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
	want := []string{"ls", "read", "glob", "write", "edit", "grep", "symbols", "bash", "explore", "runtime.query", "todo.write", "todo.read"}
	for _, n := range want {
		if !names[n] {
			t.Errorf("ListTools(allowExec=true): missing tool %q", n)
		}
	}
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
