package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

func TestDeptScratchpadRelPath_Agent(t *testing.T) {
	cases := map[string]string{
		"frontend":       ".orchestra/depts/frontend.md",
		"frontend@web":   ".orchestra/depts/frontend@web.md",
		"backend@api-v2": ".orchestra/depts/backend@api-v2.md",
		"":               "",
		"Frontend":       "", // uppercase
		"../evil":        "",
		"a/b":            "",
		"@web":           "",
	}
	for in, want := range cases {
		if got := DeptScratchpadRelPath(in); got != want {
			t.Errorf("DeptScratchpadRelPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateWorkingState_DeptWritesInstanceFile(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	a := &Agent{opts: Options{Mode: ModeOrchestra}, tools: tr}

	out, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nFE web work","dept":"frontend@web"}`))
	if err != nil {
		t.Fatalf("handleUpdateWorkingState: %v", err)
	}
	var resp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Path != ".orchestra/depts/frontend@web.md" {
		t.Fatalf("path = %q", resp.Path)
	}
	b, err := os.ReadFile(filepath.Join(root, ".orchestra", "depts", "frontend@web.md"))
	if err != nil {
		t.Fatalf("dept file not written: %v", err)
	}
	if !strings.Contains(string(b), "FE web work") {
		t.Fatalf("unexpected content: %q", string(b))
	}
	// state.md must be untouched.
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "state.md")); !os.IsNotExist(err) {
		t.Fatalf("state.md must not be created on dept write: %v", err)
	}

	// Invalid instance id is rejected.
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"x","dept":"../evil"}`)); err == nil {
		t.Fatal("invalid dept must be rejected")
	}

	// Without dept the write still goes to state.md.
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nglobal"}`)); err != nil {
		t.Fatalf("state.md write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "state.md")); err != nil {
		t.Fatalf("state.md missing after default write: %v", err)
	}
}
