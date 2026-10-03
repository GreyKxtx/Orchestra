package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// A local model often sends an array or object argument as a JSON string —
// {"todos": "[{...}]"}. The tool refused it, the model sent it again, and a
// turn lost a step (todowrite) or asked the user twice (question).

var todoDef = llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{
	Name:       "todowrite",
	Parameters: json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array"},"note":{"type":"string"}}}`),
}}

func TestCoerceStringifiedArgs_UnwrapsAnArraySentAsAString(t *testing.T) {
	in := json.RawMessage(`{"todos":"[{\"id\":\"1\",\"content\":\"draw\",\"status\":\"pending\"}]","note":"[keep me]"}`)
	out := coerceStringifiedArgs(in, todoDef)
	var got struct {
		Todos []map[string]string `json:"todos"`
		Note  string              `json:"note"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("coerced args do not decode: %v (%s)", err, out)
	}
	if len(got.Todos) != 1 || got.Todos[0]["content"] != "draw" {
		t.Fatalf("todos = %+v", got.Todos)
	}
	if got.Note != "[keep me]" {
		t.Fatalf("a string parameter must stay a string: %q", got.Note)
	}
}

// The same call came with its Cyrillic read as Latin-1 ("Ð¡Ð¾Ð·Ð´Ð°Ñ‚ÑŒ" for
// "Создать"). Unwrapped as is, the checklist would show that to the user.
func TestCoerceStringifiedArgs_RepairsUTF8ReadAsLatin1(t *testing.T) {
	mangled := string([]rune{0xd0, 0xa1, 0xd0, 0xbe, 0xd0, 0xb7}) // "Соз" as Latin-1
	inner, _ := json.Marshal([]map[string]string{{"id": "1", "content": mangled, "status": "pending"}})
	in, _ := json.Marshal(map[string]string{"todos": string(inner)})
	out := coerceStringifiedArgs(in, todoDef)
	if !strings.Contains(string(out), "Соз") {
		t.Fatalf("the mangled Cyrillic was not repaired: %s", out)
	}
}

func TestCoerceStringifiedArgs_LeavesGoodArgsAlone(t *testing.T) {
	in := json.RawMessage(`{"todos":[{"id":"1"}]}`)
	if out := coerceStringifiedArgs(in, todoDef); string(out) != string(in) {
		t.Fatalf("well-formed args changed: %s", out)
	}
	bad := json.RawMessage(`{"todos":"not json"}`)
	if out := coerceStringifiedArgs(bad, todoDef); string(out) != string(bad) {
		t.Fatalf("a string that is no JSON must be left for the tool to refuse: %s", out)
	}
}

func TestRun_ATodoListSentAsAStringIsTaken(t *testing.T) {
	llmClient := &inProcessScriptLLM{calls: []struct{ name, args string }{
		{"todowrite", `{"todos":"[{\"id\":\"1\",\"content\":\"draw the disk\",\"status\":\"done\"}]"}`},
	}}
	ag, _ := newTestAgent(t, llmClient, Options{})
	_, res, err := ag.Run(t.Context(), nil, "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil || len(res.Todos) != 1 || res.Todos[0].Content != "draw the disk" {
		t.Fatalf("the stringified list was not taken: %+v", res)
	}
}

// Seen live: grep called with {"path": ...} and a top-level "context_lines".
// The schema has "paths" (an array) and options.context_lines; the strict
// decoder refused both and the model spent three steps guessing.
var grepDef = llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{
	Name: "grep",
	Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{
		"query":{"type":"string"},
		"paths":{"type":"array","items":{"type":"string"}},
		"options":{"type":"object","additionalProperties":false,"properties":{"context_lines":{"type":"integer"},"case_insensitive":{"type":"boolean"}}}}}`),
}}

func TestCoerceStringifiedArgs_MapsNearMissNamesOntoTheSchema(t *testing.T) {
	out := coerceStringifiedArgs(json.RawMessage(`{"query":"frame","path":"index.html","context_lines":2}`), grepDef)
	var got struct {
		Query   string   `json:"query"`
		Paths   []string `json:"paths"`
		Options struct {
			ContextLines int `json:"context_lines"`
		} `json:"options"`
		Path any `json:"path"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, out)
	}
	if len(got.Paths) != 1 || got.Paths[0] != "index.html" || got.Path != nil {
		t.Errorf("path must become paths: %s", out)
	}
	if got.Options.ContextLines != 2 {
		t.Errorf("context_lines must move into options: %s", out)
	}
	// A name the schema has no home for is left for the tool to refuse.
	odd := json.RawMessage(`{"query":"x","colour":"red"}`)
	if out := coerceStringifiedArgs(odd, grepDef); string(out) != string(odd) {
		t.Errorf("an unknown name was rewritten: %s", out)
	}
}
