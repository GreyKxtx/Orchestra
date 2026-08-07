package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/view"
)

func newTestAppForPaste(t *testing.T) *App {
	t.Helper()
	in := view.NewInput(80)
	return &App{input: in}
}

func TestTryIngestPaste_BracketedPasteInsertsNewlinesWithoutSubmit(t *testing.T) {
	a := newTestAppForPaste(t)
	text := "line1\nline2\nline3"
	_, _, handled := a.tryIngestPaste(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(text),
		Paste: true,
	})
	if !handled {
		t.Fatal("expected paste handled")
	}
	if got := a.input.Value(); got != text {
		t.Fatalf("value=%q want %q", got, text)
	}
	// Enter during burst must insert newline, not clear input.
	_, _, handled = a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("enter during burst should be ingested")
	}
	if !strings.HasSuffix(a.input.Value(), "\n") {
		t.Fatalf("expected trailing newline, got %q", a.input.Value())
	}
}

func TestTryIngestPaste_PlainEnterStillSubmitsPath(t *testing.T) {
	a := newTestAppForPaste(t)
	// Slow typing — no burst.
	a.lastRuneAt = time.Now().Add(-time.Second)
	_, _, handled := a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyEnter})
	if handled {
		t.Fatal("plain Enter must not be swallowed by paste handler")
	}
}

func TestTryIngestPaste_MultiRuneChunk(t *testing.T) {
	a := newTestAppForPaste(t)
	_, _, handled := a.tryIngestPaste(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("hello world"),
	})
	if !handled {
		t.Fatal("multi-rune should be treated as paste")
	}
	if a.input.Value() != "hello world" {
		t.Fatalf("got %q", a.input.Value())
	}
}

func TestTryIngestPaste_FastTypingEnterSubmits(t *testing.T) {
	a := newTestAppForPaste(t)
	// 5 instant runes = fast typing burst (teatest / letter rolls), below the
	// flood arm threshold — Enter right after must NOT be swallowed.
	for _, r := range "hello" {
		_, _, handled := a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if handled {
			t.Fatalf("rune %q consumed as paste — short bursts are typing", r)
		}
	}
	_, _, handled := a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyEnter})
	if handled {
		t.Fatal("Enter after fast typing must submit, not turn into newline")
	}
}

func TestTryIngestPaste_SustainedFloodArmsBurst(t *testing.T) {
	a := newTestAppForPaste(t)
	armed := false
	for _, r := range "0123456789abcdef" {
		_, _, handled := a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if handled {
			armed = true
		}
	}
	if !armed {
		t.Fatal("sustained 16-rune flood should arm the paste burst")
	}
	_, _, handled := a.tryIngestPaste(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter inside an armed flood burst must be ingested as newline")
	}
}

func TestTryIngestPaste_CRLFNormalized(t *testing.T) {
	a := newTestAppForPaste(t)
	_, _, _ = a.ingestPasteChunk("a\r\nb\rc")
	if got := a.input.Value(); got != "a\nb\nc" {
		t.Fatalf("got %q", got)
	}
}
