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
	d.commitEdit()
	if d.temperature < 0.69 || d.temperature > 0.71 {
		t.Fatalf("temperature = %v, want ~0.7", d.temperature)
	}

	// Local providers: Tokens field edits window (ctx); answer is auto ~20%.
	d.cursor = 1
	d.commitEdit()
	d.adjustField("ctx", +1)
	if d.numCtx != 20480+2048 {
		t.Fatalf("numCtx = %d, want %d", d.numCtx, 20480+2048)
	}
	wantAns := autoAnswerBudget(d.numCtx)
	if d.maxTokens != wantAns {
		t.Fatalf("maxTokens = %d, want auto %d", d.maxTokens, wantAns)
	}
}

func TestSettingsDialog_TimeoutField(t *testing.T) {
	d := NewSettingsDialog(DialogProviders[0], ModelEntry{ID: "m"})
	d.SetInitial(0.2, 8192, 40960, 900, false, "")
	if d.timeoutS != 900 {
		t.Fatalf("timeoutS=%d want 900", d.timeoutS)
	}
	// Local: 0 temp, 1 Tokens(ctx), 2 timeout
	d.cursor = 2
	d.adjustField("timeout", +1)
	if d.timeoutS != 960 {
		t.Fatalf("timeoutS after step=%d want 960", d.timeoutS)
	}
	d.editBuf = "120"
	d.commitEdit()
	if d.timeoutS != 120 {
		t.Fatalf("timeoutS typed=%d want 120", d.timeoutS)
	}
}

func TestSettingsDialog_AllProvidersHideAnswerMax(t *testing.T) {
	for _, key := range []string{"vllm", "lmstudio", "openai", "anthropic", "openrouter"} {
		p, ok := FindProviderByKey(key)
		if !ok {
			t.Fatalf("missing provider %s", key)
		}
		d := NewSettingsDialog(p, ModelEntry{ID: "m"})
		for _, f := range d.fields() {
			if f.kind == "tokens" {
				t.Fatalf("%s must not expose Answer max field", key)
			}
		}
		if !p.AutoAnswerBudget() {
			t.Fatalf("%s AutoAnswerBudget=false, want true", key)
		}
	}
}

func TestSettingsDialog_LocalHidesAnswerMax(t *testing.T) {
	d := NewSettingsDialog(DialogProviders[2], ModelEntry{ID: "qwen"}) // vllm
	for _, f := range d.fields() {
		if f.kind == "tokens" {
			t.Fatal("vLLM settings must not expose Answer max field")
		}
	}
	d.SetInitial(0.35, 99999, 122880, 780, false, "")
	if d.maxTokens != autoAnswerBudget(122880) {
		t.Fatalf("maxTokens=%d want auto %d", d.maxTokens, autoAnswerBudget(122880))
	}
}

