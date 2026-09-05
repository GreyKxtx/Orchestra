package view_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestSlashPalette_Filter_Empty(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("")
	if len(p.Items) != len(view.AllSlashCmds) {
		t.Fatalf("empty query should show all %d commands, got %d", len(view.AllSlashCmds), len(p.Items))
	}
}

func TestSlashPalette_Filter_Partial(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("cl")
	if len(p.Items) == 0 {
		t.Fatal("'cl' should match /clear")
	}
	if p.Items[0].Cmd != "/clear" {
		t.Fatalf("want /clear, got %s", p.Items[0].Cmd)
	}
}

func TestSlashPalette_Filter_NoMatch(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("zzznomatch")
	if len(p.Items) != 0 {
		t.Fatalf("no-match query should return 0 items, got %d", len(p.Items))
	}
}

func TestSlashPalette_CursorUp(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("")
	p.CursorDown()
	p.CursorUp()
	if p.Cursor != 0 {
		t.Fatalf("cursor should be 0 after down+up, got %d", p.Cursor)
	}
}

func TestSlashPalette_CursorClampsAtBounds(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("")
	p.CursorUp() // already at 0
	if p.Cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", p.Cursor)
	}
	for i := 0; i < 100; i++ {
		p.CursorDown()
	}
	if p.Cursor != len(p.Items)-1 {
		t.Fatalf("cursor should clamp at last, got %d (len %d)", p.Cursor, len(p.Items))
	}
}

func TestSlashPalette_Selected(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("")
	got := p.Selected()
	if got == "" {
		t.Fatal("Selected should return the first command when items exist")
	}
	if !strings.HasPrefix(got, "/") {
		t.Fatalf("Selected should return a slash command, got %q", got)
	}
}

func TestSlashPalette_Selected_Empty(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("zzznomatch")
	if p.Selected() != "" {
		t.Fatal("Selected on empty list should return empty string")
	}
}

func TestSlashPalette_Render_NotEmpty(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.Filter("")
	out := p.Render()
	if !strings.Contains(out, "/help") {
		t.Errorf("rendered palette should contain /help, got:\n%s", out)
	}
}

func TestMentionPalette_Render_NotEmpty(t *testing.T) {
	p := view.NewMentionPalette(80)
	p.SetItems([]string{"internal/resolver.go", "ui/tui/app.go", "go.mod"})
	out := p.Render()
	if !strings.Contains(out, "internal/resolver.go") {
		t.Errorf("rendered mention palette should contain file path, got:\n%s", out)
	}
}

func TestMentionPalette_Render_Empty(t *testing.T) {
	p := view.NewMentionPalette(80)
	p.SetItems(nil)
	out := p.Render()
	if out != "" {
		t.Errorf("empty mention palette should render empty string, got:\n%s", out)
	}
}

func TestSlashPalette_ExtraCommandsAreOfferedAndFiltered(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.SetExtraMCP([]view.SlashCmd{
		{Cmd: "/mcp:linear:triage", Desc: "Triage an issue"},
		{Cmd: "/mcp:linear:standup", Desc: "Draft a standup note"},
	})

	p.Filter("triage")
	if len(p.Items) != 1 || p.Items[0].Cmd != "/mcp:linear:triage" {
		t.Fatalf("filter by prompt name gave %+v", p.Items)
	}

	// The server name is part of the command, so it is searchable too.
	p.Filter("linear")
	if len(p.Items) != 2 {
		t.Fatalf("filter by server gave %+v", p.Items)
	}

	// Built-ins still work and come first: they are the ones a user reaches
	// for constantly, and a server must not be able to bury /quit.
	p.Filter("")
	if len(p.Items) != len(view.AllSlashCmds)+2 {
		t.Fatalf("unfiltered list = %d items", len(p.Items))
	}
	if p.Items[0].Cmd != view.AllSlashCmds[0].Cmd {
		t.Errorf("first item = %q, want a built-in", p.Items[0].Cmd)
	}
}

func TestSlashPalette_SetExtraReplacesRatherThanAccumulates(t *testing.T) {
	// The list is refreshed when servers restart; appending would show
	// commands from servers that are gone.
	p := view.NewSlashPalette(80)
	p.SetExtraMCP([]view.SlashCmd{{Cmd: "/mcp:a:one"}})
	p.SetExtraMCP([]view.SlashCmd{{Cmd: "/mcp:b:two"}})
	// "/mcp" is also a built-in, so filter on the part only a prompt has.
	p.Filter("mcp:")
	if len(p.Items) != 1 || p.Items[0].Cmd != "/mcp:b:two" {
		t.Fatalf("items = %+v, want only the second SetExtraMCP call's command", p.Items)
	}
}

// MCP prompts and skills are two independent runtime sources that refresh on
// their own schedules. One's refresh must not erase the other's commands —
// SetExtraMCP and SetExtraSkills each own their own slot in the palette.
func TestSlashPalette_MCPAndSkillExtrasCoexist(t *testing.T) {
	p := view.NewSlashPalette(80)
	p.SetExtraMCP([]view.SlashCmd{{Cmd: "/mcp:linear:triage", Desc: "Triage"}})
	p.SetExtraSkills([]view.SlashCmd{{Cmd: "/refactor-go", Desc: "Refactor Go code"}})

	p.Filter("")
	if len(p.Items) != len(view.AllSlashCmds)+2 {
		t.Fatalf("unfiltered list = %d items, want built-ins + 2 extras: %+v", len(p.Items), p.Items)
	}

	// Refreshing one source leaves the other untouched.
	p.SetExtraMCP([]view.SlashCmd{{Cmd: "/mcp:linear:standup", Desc: "Standup"}})
	p.Filter("")
	if len(p.Items) != len(view.AllSlashCmds)+2 {
		t.Fatalf("after refreshing MCP extras, list = %d items, want built-ins + 2 extras: %+v", len(p.Items), p.Items)
	}
	p.Filter("refactor")
	if len(p.Items) != 1 || p.Items[0].Cmd != "/refactor-go" {
		t.Fatalf("skill extra survived MCP refresh? items = %+v", p.Items)
	}
}
