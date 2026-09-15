package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

func userTurn(q string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: "<user_info>\nos: test\n</user_info>\n\n<user_query>\n" + q + "\n</user_query>\n"}
}

func filler(n int) []llm.Message {
	var out []llm.Message
	for i := 0; i < n; i++ {
		out = append(out,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c" + string(rune('a'+i)), Type: "function",
				Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"big.go"}`)}}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c" + string(rune('a'+i)), Content: strings.Repeat("x", 3000)},
		)
	}
	return out
}

// The summary is written by the model, and a 9B summariser dropped what the
// user had said. Seen live: turn one said "the release codename is
// BLUE-HERON-7, we decided to keep payment retries unchanged"; after two
// compactions the checkpoint held only the latest request and a function the
// summariser invented. The user's own words are therefore carried into every
// checkpoint verbatim, next to the summary, and survive the next compaction;
// the agent's own notes (validation errors, hints) are not the user's words.
func TestCompactHistory_KeepsTheUsersWordsVerbatim(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	runner, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	ag, err := New(&compactionLLM{}, v, runner, Options{MaxPromptBytes: 20000, HistoryPruneKeepRecent: 1})
	if err != nil {
		t.Fatal(err)
	}

	hist := []llm.Message{userTurn("Remember: the release codename is BLUE-HERON-7, and we keep payment retries unchanged.")}
	hist = append(hist, llm.Message{Role: llm.RoleAssistant, Content: "Noted."})
	hist = append(hist, llm.Message{Role: llm.RoleUser, Content: "VALIDATION_ERROR message=Task requires code changes"})
	hist = append(hist, userTurn("Read inventory.go and tell me what inventoryStep7 adds."))
	hist = append(hist, filler(6)...)

	first, err := ag.compactHistory(context.Background(), "Read ledger.go", hist)
	if err != nil {
		t.Fatal(err)
	}
	cp := first[0].Content
	for _, want := range []string{"BLUE-HERON-7", "keep payment retries unchanged", "what inventoryStep7 adds"} {
		if !strings.Contains(cp, want) {
			t.Errorf("the checkpoint lost the user's words %q:\n%s", want, cp)
		}
	}
	if strings.Contains(cp, "VALIDATION_ERROR") {
		t.Errorf("an agent note was kept as if the user had said it:\n%s", cp)
	}

	// The next compaction summarises the checkpoint itself; the words stay.
	second := append(append([]llm.Message{}, first...), userTurn("Now read courier.go."))
	second = append(second, filler(6)...)
	again, err := ag.compactHistory(context.Background(), "Read courier.go", second)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BLUE-HERON-7", "what inventoryStep7 adds", "Now read courier.go."} {
		if !strings.Contains(again[0].Content, want) {
			t.Errorf("after a second compaction the user's words %q are gone:\n%s", want, again[0].Content)
		}
	}
	if n := strings.Count(again[0].Content, "BLUE-HERON-7"); n != 1 {
		t.Errorf("the carried words were duplicated (%d copies):\n%s", n, again[0].Content)
	}
}
