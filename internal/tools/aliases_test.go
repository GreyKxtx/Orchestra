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
	defs := ListTools(Capabilities{Exec: true, Web: false, Browser: false})
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
	general := ListToolsForMode("general", Capabilities{Exec: false, Web: false, Browser: false}, false, false)
	generalNames := toolNameSet(general)
	for _, want := range []string{"read", "write", "edit", "grep", "task_result"} {
		if !generalNames[want] {
			t.Errorf("general mode: missing tool %q", want)
		}
	}
	if generalNames["todowrite"] {
		t.Error("general mode: must not include todowrite")
	}

	// ask: read-only, no write/edit; task_result only on child (not top-level)
	askNames := toolNameSet(ListToolsForMode("ask", Capabilities{}, false, true))
	for _, want := range []string{"read", "grep", "explore", "question"} {
		if !askNames[want] {
			t.Errorf("ask mode: missing %q", want)
		}
	}
	if askNames["write"] || askNames["edit"] || askNames["bash"] || askNames["task_result"] {
		t.Error("ask mode must not include write/edit/bash/task_result")
	}

	// explore: CKG explore tool, no task_result at top-level
	exploreNames := toolNameSet(ListToolsForMode("explore", Capabilities{}, false, false))
	if !exploreNames["explore"] {
		t.Error("explore mode: missing explore tool")
	}
	if exploreNames["task_result"] || exploreNames["write"] {
		t.Error("explore mode must not include task_result/write")
	}

	// architecture: write ok, edit no, plan_exit for finishing
	archNames := toolNameSet(ListToolsForMode("architecture", Capabilities{}, true, true))
	if !archNames["write"] || archNames["edit"] {
		t.Error("architecture: want write, no edit")
	}
	if !archNames["task"] {
		t.Error("architecture with subtasks: want task")
	}
	if !archNames["plan_exit"] {
		t.Error("architecture: want plan_exit")
	}

	// debug: edit + task
	dbgNames := toolNameSet(ListToolsForMode("debug", Capabilities{Exec: true}, true, false))
	for _, want := range []string{"edit", "write", "task", "lsp.diagnostics"} {
		if !dbgNames[want] {
			t.Errorf("debug mode: missing %q", want)
		}
	}

	// compaction/title/summary: no tools at all
	for _, mode := range []string{"compaction", "title", "summary"} {
		defs := ListToolsForMode(mode, Capabilities{Exec: true, Web: true, Browser: false}, true, true)
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
	without := ListTools(Capabilities{Exec: false, Web: false, Browser: false})
	for _, d := range without {
		if d.Function.Name == "bash" {
			t.Error("ListTools(allowExec=false) must not include bash")
		}
	}
	with := ListTools(Capabilities{Exec: true, Web: false, Browser: false})
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

// TestListTools_WebGating verifies webfetch is absent without allowWeb.
func TestListTools_WebGating(t *testing.T) {
	without := ListTools(Capabilities{Exec: false, Web: false, Browser: false})
	for _, d := range without {
		if d.Function.Name == "webfetch" {
			t.Error("ListTools(allowWeb=false) must not include webfetch")
		}
	}
	with := ListTools(Capabilities{Exec: false, Web: true, Browser: false})
	found := false
	for _, d := range with {
		if d.Function.Name == "webfetch" {
			found = true
		}
	}
	if !found {
		t.Error("ListTools(allowWeb=true) must include webfetch")
	}
}

// TestListTools_NewToolsPresent verifies all v7 tools are registered.
func TestListTools_NewToolsPresent(t *testing.T) {
	// Read-only git tools + fs extras are always present (no allowExec needed).
	always := ListTools(Capabilities{Exec: false, Web: false, Browser: false})
	alwaysNames := toolNameSet(always)
	for _, name := range []string{"fs.delete", "fs.rename", "git.status", "git.log", "git.diff"} {
		if !alwaysNames[name] {
			t.Errorf("ListTools(allowExec=false): missing tool %q", name)
		}
	}

	// Write git tools require allowExec=true.
	withExec := ListTools(Capabilities{Exec: true, Web: false, Browser: false})
	withExecNames := toolNameSet(withExec)
	for _, name := range []string{"git.commit", "git.branch", "git.checkout", "git.push"} {
		if !withExecNames[name] {
			t.Errorf("ListTools(allowExec=true): missing tool %q", name)
		}
	}

	// Write git tools must NOT appear without allowExec.
	for _, name := range []string{"git.commit", "git.branch", "git.checkout", "git.push"} {
		if alwaysNames[name] {
			t.Errorf("ListTools(allowExec=false): must not include %q", name)
		}
	}
}

// TestListTools_BrowserGating verifies browser tools are absent without allowBrowser.
func TestListTools_BrowserGating(t *testing.T) {
	browserTools := []string{
		"browser.navigate", "browser.snapshot", "browser.screenshot",
		"browser.click", "browser.type", "browser.fill",
		"browser.select", "browser.eval", "browser.wait", "browser.close",
	}

	// Without allowBrowser: browser tools must not appear.
	without := ListTools(Capabilities{Exec: false, Web: false, Browser: false})
	withoutNames := toolNameSet(without)
	for _, name := range browserTools {
		if withoutNames[name] {
			t.Errorf("ListTools(allowBrowser=false): must not include %q", name)
		}
	}

	// With allowBrowser: all browser tools must appear.
	with := ListTools(Capabilities{Exec: false, Web: false, Browser: true})
	withNames := toolNameSet(with)
	for _, name := range browserTools {
		if !withNames[name] {
			t.Errorf("ListTools(allowBrowser=true): missing tool %q", name)
		}
	}
}
