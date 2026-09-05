package lessons

import "testing"

// The dept-level counter in signals.go feeds the LLM-facing lesson_promote
// pipeline (a dept playbook overlay). A human-facing "add a rule to
// ORCHESTRA.md?" suggestion needs a file dimension the plan's own example
// depends on ("3× StaleContent on src/App.jsx") — AntiPatternKey alone
// can't express that, and reusing signals.go's per-dept log risks one
// feature's clear wiping the other's still-accumulating count. This is why
// rule signals get their own key shape and their own log.

func TestFileAntiPatternKey_IncludesTheFile(t *testing.T) {
	a := FileAntiPatternKey("src/App.jsx", "StaleContent", "wire the sidebar")
	b := FileAntiPatternKey("src/Other.jsx", "StaleContent", "wire the sidebar")
	if a == b {
		t.Fatalf("keys for different files must differ, both = %q", a)
	}
}

func TestBumpRuleSignal_CountsUpToThreshold(t *testing.T) {
	root := t.TempDir()
	key := FileAntiPatternKey("src/App.jsx", "StaleContent: search block not found", "")
	for i := 0; i < RuleSuggestThreshold; i++ {
		if got := BumpRuleSignal(root, "engineering", key); got != i+1 {
			t.Fatalf("bump %d = %d", i+1, got)
		}
	}
}

func TestBumpRuleSignal_DifferentFilesCountSeparately(t *testing.T) {
	root := t.TempDir()
	keyA := FileAntiPatternKey("src/App.jsx", "StaleContent", "")
	keyB := FileAntiPatternKey("src/Other.jsx", "StaleContent", "")

	BumpRuleSignal(root, "engineering", keyA)
	BumpRuleSignal(root, "engineering", keyA)
	if got := BumpRuleSignal(root, "engineering", keyB); got != 1 {
		t.Fatalf("a different file's count leaked in: got %d, want 1", got)
	}
}

func TestClearRuleSignal_OnlyRemovesTheGivenKey(t *testing.T) {
	root := t.TempDir()
	keyA := FileAntiPatternKey("src/App.jsx", "StaleContent", "")
	keyB := FileAntiPatternKey("src/Other.jsx", "StaleContent", "")
	BumpRuleSignal(root, "engineering", keyA)
	BumpRuleSignal(root, "engineering", keyA)
	BumpRuleSignal(root, "engineering", keyB)

	ClearRuleSignal(root, "engineering", keyA)

	if got := BumpRuleSignal(root, "engineering", keyA); got != 1 {
		t.Fatalf("cleared key should restart at 1, got %d", got)
	}
	if got := BumpRuleSignal(root, "engineering", keyB); got != 2 {
		t.Fatalf("clearing keyA must not touch keyB's count, got %d want 2", got)
	}
}

// signals.go's dept-level ClearAntiPatternSignals must not be able to reset
// this counter, and vice versa — they track unrelated features and share
// no state.
func TestRuleSignal_IndependentFromDeptLevelSignal(t *testing.T) {
	root := t.TempDir()
	dept := "engineering"
	deptKey := AntiPatternKey("StaleContent", "")
	fileKey := FileAntiPatternKey("src/App.jsx", "StaleContent", "")

	BumpAntiPatternSignal(root, dept, deptKey)
	BumpAntiPatternSignal(root, dept, deptKey)
	BumpRuleSignal(root, dept, fileKey)

	ClearAntiPatternSignals(root, dept)

	if got := BumpRuleSignal(root, dept, fileKey); got != 2 {
		t.Fatalf("clearing the dept-level signal must not reset the rule signal, got %d want 2", got)
	}
}

func TestFormatRuleSuggestion_NamesFileCountAndVerify(t *testing.T) {
	text := FormatRuleSuggestion("src/App.jsx", 3, "StaleContent: search block not found")
	for _, want := range []string{"src/App.jsx", "3", "StaleContent"} {
		if !contains(text, want) {
			t.Errorf("suggestion text %q missing %q", text, want)
		}
	}
}

func TestFormatRuleLine_IsAppendableMarkdown(t *testing.T) {
	line := FormatRuleLine("src/App.jsx", "StaleContent: search block not found")
	if !contains(line, "src/App.jsx") {
		t.Errorf("rule line %q must name the file", line)
	}
}
