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
