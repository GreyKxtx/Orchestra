package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// TestShadowHelper is the command the preview below runs: the test binary
// printing the file ORCHESTRA_SHADOW_HELPER_FILE names, from the working
// directory it was given.
func TestShadowHelper(t *testing.T) {
	file := os.Getenv("ORCHESTRA_SHADOW_HELPER_FILE")
	if file == "" {
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		os.Stdout.WriteString("ERR " + err.Error())
		return
	}
	os.Stdout.WriteString(string(b))
}

// shadowScriptLLM stages a file, runs the helper on it, and closes; it keeps
// what the tool reported back for the command.
type shadowScriptLLM struct {
	mu    sync.Mutex
	calls int
	saw   string
}

func (s *shadowScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *shadowScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	switch s.calls {
	case 1:
		return toolCall("write", `{"path":"a.txt","content":"staged\n","must_not_exist":true}`), nil
	case 2:
		return toolCall("bash", `{"command":`+jsonString(os.Args[0])+`,"args":["-test.run=TestShadowHelper$","-test.count=1"]}`), nil
	default:
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == llm.RoleTool {
				s.saw = req.Messages[i].Content
				break
			}
		}
		return finalText("done"), nil
	}
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// A session's preview turn runs its command in a shadow of the workspace
// (LLM-11): the command reads the edit the model just staged, and the disk
// stays untouched. The core used to refuse every command in a preview.
func TestSessionMessage_APreviewsCommandRunsInTheShadow(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	off := false
	cfg.LSP.Enabled = &off
	cfg.LSP.AutoInstall = "false"
	cfg.Exec.Confirm = &off
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCHESTRA_SHADOW_HELPER_FILE", "a.txt")
	client := &shadowScriptLLM{}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: start.SessionID,
		Content:   "stage a.txt and read it back with the helper",
		AllowExec: true,
	}); err != nil {
		t.Fatalf("session.message: %v", err)
	}
	if client.calls < 3 {
		t.Fatalf("the model was called %d times; the command never ran", client.calls)
	}
	if strings.Contains(client.saw, "cannot run in this turn") || strings.Contains(client.saw, "cannot run in this preview") {
		t.Fatalf("the preview refused the command:\n%s", client.saw)
	}
	if !strings.Contains(client.saw, "staged") {
		t.Fatalf("the command did not read the staged edit:\n%s", client.saw)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err == nil {
		t.Fatal("a preview wrote a.txt to the workspace")
	}
}
