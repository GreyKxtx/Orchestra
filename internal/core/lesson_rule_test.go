package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/lessons"
)

func TestRPC_LessonRuleRespond_AcceptAppendsToOrchestraMD(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ORCHESTRA.md"), []byte("Team rules.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	params := RuleSuggestionRespondParams{
		Accept:   true,
		Dept:     "engineering",
		File:     "src/App.jsx",
		Verify:   "StaleContent: search block not found",
		RuleLine: "Перед правкой src/App.jsx — прочитать файл целиком",
	}
	raw, _ := json.Marshal(params)
	out, err := h.Handle(context.Background(), "lesson.rule_respond", raw)
	if err != nil {
		t.Fatalf("lesson.rule_respond: %v", err)
	}
	res, ok := out.(*RuleSuggestionRespondResult)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if !res.Applied {
		t.Fatal("Applied = false, want true on accept")
	}

	data, err := os.ReadFile(filepath.Join(root, "ORCHESTRA.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Team rules.") {
		t.Errorf("existing content lost: %q", data)
	}
	if !strings.Contains(string(data), "src/App.jsx") {
		t.Errorf("rule line not appended: %q", data)
	}
}

func TestRPC_LessonRuleRespond_AcceptCreatesFileWhenMissing(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	params := RuleSuggestionRespondParams{
		Accept:   true,
		Dept:     "engineering",
		File:     "src/App.jsx",
		Verify:   "StaleContent",
		RuleLine: "Перед правкой src/App.jsx — прочитать файл целиком",
	}
	raw, _ := json.Marshal(params)
	if _, err := h.Handle(context.Background(), "lesson.rule_respond", raw); err != nil {
		t.Fatalf("lesson.rule_respond: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "ORCHESTRA.md"))
	if err != nil {
		t.Fatalf("ORCHESTRA.md must be created, got: %v", err)
	}
	if !strings.Contains(string(data), "src/App.jsx") {
		t.Errorf("rule line missing: %q", data)
	}
}

// A2's fallback chain must be respected: appending must land in whichever
// file is actually feeding the agent (AGENTS.md here), not create a second,
// competing ORCHESTRA.md the fallback logic would then ignore.
func TestRPC_LessonRuleRespond_AcceptRespectsFallbackFile(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Agents rules.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	params := RuleSuggestionRespondParams{Accept: true, Dept: "engineering", File: "a.go", Verify: "x", RuleLine: "rule for a.go"}
	raw, _ := json.Marshal(params)
	if _, err := h.Handle(context.Background(), "lesson.rule_respond", raw); err != nil {
		t.Fatalf("lesson.rule_respond: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "ORCHESTRA.md")); err == nil {
		t.Fatal("must not create a competing ORCHESTRA.md when AGENTS.md already exists")
	}
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "rule for a.go") {
		t.Errorf("rule line not appended to the fallback file: %q", data)
	}
}

func TestRPC_LessonRuleRespond_DeclineDoesNotTouchAnyFile(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	params := RuleSuggestionRespondParams{Accept: false, Dept: "engineering", File: "src/App.jsx", Verify: "x", RuleLine: "should never be written"}
	raw, _ := json.Marshal(params)
	out, err := h.Handle(context.Background(), "lesson.rule_respond", raw)
	if err != nil {
		t.Fatalf("lesson.rule_respond: %v", err)
	}
	if out.(*RuleSuggestionRespondResult).Applied {
		t.Error("Applied = true, want false on decline")
	}
	if _, err := os.Stat(filepath.Join(root, "ORCHESTRA.md")); err == nil {
		t.Fatal("declining must not create ORCHESTRA.md")
	}
}

// Whichever way the human answers, the signal must reset — otherwise the
// very next occurrence re-prompts immediately instead of needing three more.
func TestRPC_LessonRuleRespond_ClearsTheSignalEitherWay(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	key := lessons.FileAntiPatternKey("src/App.jsx", "StaleContent", "")
	lessons.BumpRuleSignal(root, "engineering", key)
	lessons.BumpRuleSignal(root, "engineering", key)
	if got := lessons.BumpRuleSignal(root, "engineering", key); got != 3 {
		t.Fatalf("setup: count = %d, want 3", got)
	}

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	params := RuleSuggestionRespondParams{Accept: false, Dept: "engineering", File: "src/App.jsx", Verify: "StaleContent"}
	raw, _ := json.Marshal(params)
	if _, err := h.Handle(context.Background(), "lesson.rule_respond", raw); err != nil {
		t.Fatalf("lesson.rule_respond: %v", err)
	}

	if got := lessons.BumpRuleSignal(root, "engineering", key); got != 1 {
		t.Errorf("count after respond = %d, want 1 (cleared then bumped once)", got)
	}
}

// The turn result's RuleSuggestion must reach the RPC client as JSON, not
// get dropped on the floor between agent.Result and SessionMessageResult.
func TestRuleSuggestionPayload_MirrorsAgentRuleSuggestion(t *testing.T) {
	sug := &agent.RuleSuggestion{
		Dept: "engineering", File: "src/App.jsx", Count: 3,
		Verify: "StaleContent", RuleLine: "read first", Text: "3x StaleContent on src/App.jsx",
	}
	got := ruleSuggestionPayload(sug)
	if got == nil {
		t.Fatal("payload must not be nil for a non-nil suggestion")
	}
	if got.File != sug.File || got.Count != sug.Count || got.RuleLine != sug.RuleLine || got.Text != sug.Text || got.Dept != sug.Dept || got.Verify != sug.Verify {
		t.Errorf("payload = %+v, want a field-for-field mirror of %+v", got, sug)
	}
	if ruleSuggestionPayload(nil) != nil {
		t.Error("payload for a nil suggestion must be nil")
	}
}
