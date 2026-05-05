package pipeline

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestParseVerdict_JSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
		accept  bool
		hasText bool
	}{
		{"accept JSON", `{"status":"accept","reason":"looks good"}`, true, true},
		{"Accept uppercase", `{"status":"Accept","reason":"fine"}`, true, false},
		{"reject JSON", `{"status":"reject","reason":"missing tests","issues":["no coverage"]}`, false, true},
		{"reject no issues", `{"status":"reject","reason":"bad code"}`, false, true},
		{"empty", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accept, text := parseVerdict(tt.content)
			if accept != tt.accept {
				t.Errorf("accept=%v want %v (text=%q)", accept, tt.accept, text)
			}
			if tt.hasText && text == "" {
				t.Errorf("expected non-empty text, got empty")
			}
		})
	}
}

func TestParseVerdict_Lenient(t *testing.T) {
	tests := []struct {
		name    string
		content string
		accept  bool
	}{
		{"plain accept", "The implementation looks good. ACCEPT.", true},
		{"plain reject", "REJECT: missing error handling", false},
		{"reject takes precedence", "partially accept but REJECT overall", false},
		{"unknown fallback", "implementation is fine overall", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accept, _ := parseVerdict(tt.content)
			if accept != tt.accept {
				t.Errorf("parseVerdict(%q) accept=%v want %v", tt.content, accept, tt.accept)
			}
		})
	}
}

func TestBuildCoderGoal_FirstAttempt(t *testing.T) {
	goal := buildCoderGoal("fix the bug", "found issue in foo.go", "", 1)
	if !strings.Contains(goal, "fix the bug") {
		t.Error("goal missing query")
	}
	if !strings.Contains(goal, "<investigation>") {
		t.Error("goal missing investigation block")
	}
	if strings.Contains(goal, "<critique>") {
		t.Error("first attempt should not have critique block")
	}
	if strings.Contains(goal, "попытка") {
		t.Error("first attempt should not mention retry")
	}
}

func TestBuildCoderGoal_RetryWithCritique(t *testing.T) {
	goal := buildCoderGoal("fix the bug", "found issue in foo.go", "missing nil check", 2)
	if !strings.Contains(goal, "<critique>") {
		t.Error("retry should include critique block")
	}
	if !strings.Contains(goal, "попытка") {
		t.Error("retry should mention attempt number")
	}
	if !strings.Contains(goal, "missing nil check") {
		t.Error("retry should include critique text")
	}
}

func TestSummarizePatches(t *testing.T) {
	patches := []externalpatch.Patch{
		{Type: externalpatch.TypeFileSearchReplace, Path: "foo.go"},
		{Type: externalpatch.TypeFileWriteAtomic, Path: "bar.go", Content: "package bar"},
		{Type: externalpatch.TypeFileUnifiedDiff, Path: "baz.go"},
	}
	summary := summarizePatches(patches)
	if !strings.Contains(summary, "foo.go") {
		t.Error("summary missing foo.go")
	}
	if !strings.Contains(summary, "bar.go") {
		t.Error("summary missing bar.go")
	}
	if summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestSummarizePatches_Empty(t *testing.T) {
	if summarizePatches(nil) != "" {
		t.Error("empty patches should produce empty summary")
	}
}

func TestApplyDefaults(t *testing.T) {
	var opts Options
	applyDefaults(&opts)
	if opts.MaxCoderAttempts != 2 {
		t.Errorf("MaxCoderAttempts default=%d want 2", opts.MaxCoderAttempts)
	}
	if opts.MaxStepsCoder != 24 {
		t.Errorf("MaxStepsCoder default=%d want 24", opts.MaxStepsCoder)
	}
	if opts.MaxStepsInvestigator != 10 {
		t.Errorf("MaxStepsInvestigator default=%d want 10", opts.MaxStepsInvestigator)
	}
	if opts.MaxStepsCritic != 8 {
		t.Errorf("MaxStepsCritic default=%d want 8", opts.MaxStepsCritic)
	}
}

func TestBuildInvestigatorGoal_NoEvidence(t *testing.T) {
	goal := buildInvestigatorGoal("add logging to handler", "")
	if !strings.Contains(goal, "add logging to handler") {
		t.Error("goal missing query")
	}
	if strings.Contains(goal, "<runtime_evidence>") {
		t.Error("goal should not contain runtime_evidence block when empty")
	}
	if !strings.Contains(goal, "task_result") {
		t.Error("goal should mention task_result")
	}
}

func TestBuildInvestigatorGoal_WithEvidence(t *testing.T) {
	evidence := "<runtime_evidence>\n[ERROR] foo → CKG:pkg/foo.Bar\n</runtime_evidence>"
	goal := buildInvestigatorGoal("fix the bug", evidence)
	if !strings.Contains(goal, evidence) {
		t.Error("goal missing runtime_evidence block")
	}
	if !strings.Contains(goal, "CKG-нодам") {
		t.Error("goal should instruct model to correlate CKG nodes")
	}
	if !strings.Contains(goal, "fix the bug") {
		t.Error("goal missing query")
	}
}

func TestBuildCriticGoal_NoEvidence(t *testing.T) {
	res := fakeCoderResult()
	goal := buildCriticGoal("fix the bug", "investigation text", "", &res)
	if !strings.Contains(goal, "fix the bug") {
		t.Error("goal missing query")
	}
	if strings.Contains(goal, "<runtime_evidence>") {
		t.Error("goal should not contain runtime_evidence block when empty")
	}
	if !strings.Contains(goal, "task_result") {
		t.Error("goal should mention task_result")
	}
}

func TestBuildCriticGoal_WithEvidence(t *testing.T) {
	evidence := "<runtime_evidence>\n[ERROR] bar → CKG:pkg/bar.Baz | err: nil ptr\n</runtime_evidence>"
	res := fakeCoderResult()
	goal := buildCriticGoal("fix the bug", "investigation text", evidence, &res)
	if !strings.Contains(goal, evidence) {
		t.Error("goal missing runtime_evidence block")
	}
}

func TestFormatRuntimeEvidence_Empty(t *testing.T) {
	if formatRuntimeEvidence(nil) != "" {
		t.Error("nil response should return empty string")
	}
	if formatRuntimeEvidence(&tools.RuntimeQueryResponse{}) != "" {
		t.Error("response with no spans should return empty string")
	}
}

func TestFormatRuntimeEvidence_WithSpans(t *testing.T) {
	resp := &tools.RuntimeQueryResponse{
		TraceID: "trace-1",
		Service: "api",
		Spans: []tools.RuntimeSpanResult{
			{
				Status:    "ERROR",
				Name:      "handler.Create",
				NodeFQN:   "github.com/foo/bar.Create",
				NodeKind:  "func",
				ErrorMsg:  "nil pointer dereference",
				CodeFile:  "handler.go",
				CodeLineno: 42,
			},
			{
				Status: "OK",
				Name:   "db.Query",
			},
		},
	}
	out := formatRuntimeEvidence(resp)
	if !strings.Contains(out, "<runtime_evidence>") {
		t.Error("missing opening tag")
	}
	if !strings.Contains(out, "</runtime_evidence>") {
		t.Error("missing closing tag")
	}
	if !strings.Contains(out, "Service: api") {
		t.Error("missing service line")
	}
	if !strings.Contains(out, "CKG:github.com/foo/bar.Create") {
		t.Error("missing CKG FQN")
	}
	if !strings.Contains(out, "err: nil pointer dereference") {
		t.Error("missing error message")
	}
	if !strings.Contains(out, "handler.go:42") {
		t.Error("missing code location")
	}
	if !strings.Contains(out, "[OK] db.Query") {
		t.Error("missing OK span")
	}
}

// fakeCoderResult returns a minimal agent.Result for use in Critic goal builder tests.
func fakeCoderResult() agent.Result {
	return agent.Result{}
}
