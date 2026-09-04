package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// deadLLM is the failure the field run actually hit: the configured endpoint
// was unreachable for a whole day (183 llm_error events).
type deadLLM struct{}

func (deadLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return nil, errors.New("dial tcp 127.0.0.1:1234: connect: connection refused")
}
func (deadLLM) Plan(context.Context, string) (string, error) {
	return "", errors.New("unreachable")
}

type proseLLM struct{ text string }

func (f proseLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: f.text}}, nil
}
func (f proseLLM) Plan(context.Context, string) (string, error) { return "", nil }

func newSummaryCore(t *testing.T, client llm.Client) (*Core, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, root
}

func persistEditDigest(t *testing.T, root, sessionID, goal, path string) {
	t.Helper()
	st := working.New(goal)
	st.ObserveTool("edit", []byte(`{"path":"`+path+`"}`), nil, nil)
	if err := working.PersistTurnDigest(root, sessionID, st.BuildTurnDigest(0)); err != nil {
		t.Fatal(err)
	}
}

func readAgentMemory(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatalf("agent.md was never written: %v", err)
	}
	return string(data)
}

func longHistory(n int) []llm.Message {
	hist := make([]llm.Message, n)
	for i := range hist {
		hist[i] = llm.Message{Role: llm.RoleUser, Content: "step"}
	}
	return hist
}

func TestAutoSummaryMemory_FallsBackToTurnDigestWhenLLMIsDown(t *testing.T) {
	c, root := newSummaryCore(t, deadLLM{})
	const sid = "sess-1"
	persistEditDigest(t, root, sid, "add the weather panel", "src/App.jsx")

	c.maybeAutoSummaryMemory(context.Background(), sid, longHistory(12), &agent.Result{Steps: 5})

	// Losing the endpoint must not also lose the memory of the run: the turn
	// digest is already on disk and needs no model to turn into a note.
	got := readAgentMemory(t, root)
	if !strings.Contains(got, sid) {
		t.Errorf("note must name its session, got: %s", got)
	}
	if !strings.Contains(got, "src/App.jsx") {
		t.Errorf("note must carry the touched file, got: %s", got)
	}
}

func TestAutoSummaryMemory_ShortTurnStillRecordsEdits(t *testing.T) {
	c, root := newSummaryCore(t, deadLLM{})
	const sid = "sess-short"
	persistEditDigest(t, root, sid, "fix the typo", "README.md")

	// "продолжай" turns are short by nature; the edit they made is still the
	// thing the next session needs to know.
	c.maybeAutoSummaryMemory(context.Background(), sid, longHistory(2), &agent.Result{Steps: 2})

	if got := readAgentMemory(t, root); !strings.Contains(got, "README.md") {
		t.Errorf("short turn that edited a file must be remembered, got: %s", got)
	}
}

func TestAutoSummaryMemory_ReadOnlyTurnWritesNothing(t *testing.T) {
	c, root := newSummaryCore(t, deadLLM{})
	const sid = "sess-readonly"
	st := working.New("what does this project do?")
	st.ObserveTool("read", []byte(`{"path":"main.go"}`), nil, nil)
	if err := working.PersistTurnDigest(root, sid, st.BuildTurnDigest(0)); err != nil {
		t.Fatal(err)
	}

	c.maybeAutoSummaryMemory(context.Background(), sid, longHistory(12), &agent.Result{Steps: 3})

	if _, err := os.Stat(filepath.Join(root, ".orchestra", "memory", "agent.md")); !os.IsNotExist(err) {
		data, _ := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "agent.md"))
		t.Fatalf("a look-around turn is not a durable fact, wrote: %s", data)
	}
}

func TestAutoSummaryMemory_PrefersModelProseWhenAvailable(t *testing.T) {
	c, root := newSummaryCore(t, proseLLM{text: "Wired the weather panel into the city sidebar."})
	const sid = "sess-2"
	persistEditDigest(t, root, sid, "add the weather panel", "src/App.jsx")

	c.maybeAutoSummaryMemory(context.Background(), sid, longHistory(12), &agent.Result{Steps: 5})

	if got := readAgentMemory(t, root); !strings.Contains(got, "Wired the weather panel") {
		t.Errorf("model summary must win when the endpoint answers, got: %s", got)
	}
}
