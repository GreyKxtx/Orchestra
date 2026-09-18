package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// A model that cut its tool call short leaves arguments that are not JSON.
// They still have to reach the tool (which reports the invalid input), but
// what goes back to the provider must parse: vLLM json-loads every historic
// tool call while rendering the chat template and answered HTTP 400 for the
// rest of the run when one did not (2026-09-18).
func TestToolArguments_InvalidJSONIsWrappedOnTheWire(t *testing.T) {
	garbled := `{"goal": "Write api_test.go", "tier": "?agent_type>\nworker`
	tc := ToolCall{ID: "c1", Type: "function", Function: ToolCallFunc{
		Name:      "task",
		Arguments: ToolArguments([]byte(garbled)),
	}}
	if string(tc.Function.Arguments.Raw()) != garbled {
		t.Fatalf("Raw must keep the original text for the tool to report:\n%s", tc.Function.Arguments.Raw())
	}

	wire, err := json.Marshal(tc)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Function struct {
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("tool call is not valid JSON on the wire: %v\n%s", err, wire)
	}
	if !json.Valid([]byte(back.Function.Arguments)) {
		t.Fatalf("arguments must parse as JSON for the provider's chat template, got %q", back.Function.Arguments)
	}
	if !strings.Contains(back.Function.Arguments, `"_invalid_json"`) || !strings.Contains(back.Function.Arguments, "agent_type") {
		t.Fatalf("the original text should travel inside the wrapper, got %q", back.Function.Arguments)
	}

	// Valid arguments are untouched.
	ok := ToolArguments([]byte(`{"path":"a.go"}`))
	b, err := json.Marshal(ok)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"{\"path\":\"a.go\"}"` {
		t.Fatalf("valid arguments changed on the wire: %s", b)
	}
}
