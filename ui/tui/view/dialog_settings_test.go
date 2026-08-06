package view

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsDialog_TypeAndStep(t *testing.T) {
	d := NewSettingsDialog(DialogProviders[0], ModelEntry{ID: "test"})
	d.Update(tea.KeyMsg{Runes: []rune{'0'}})
	d.Update(tea.KeyMsg{Runes: []rune{'.'}})
	d.Update(tea.KeyMsg{Runes: []rune{'7'}})
	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// commit on enter saves - check via adjust after commit
	d.commitEdit()
	if d.temperature < 0.69 || d.temperature > 0.71 {
		t.Fatalf("temperature = %v, want ~0.7", d.temperature)
	}

	d.cursor = 1
	d.commitEdit()
	d.adjustField("tokens", +1)
	if d.maxTokens < 8192 {
		t.Fatalf("maxTokens = %d", d.maxTokens)
	}
}

func TestSettingsDialog_BackspaceEdit(t *testing.T) {
	d := NewSettingsDialog(DialogProviders[0], ModelEntry{ID: "m"})
	d.editBuf = "123"
	d.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if d.editBuf != "12" {
		t.Fatalf("editBuf = %q", d.editBuf)
	}
}
